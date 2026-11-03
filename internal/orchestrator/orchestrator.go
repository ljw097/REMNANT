// Package orchestrator drives research runs through the state machine. It
// claims runs with a database lease, so any number of remnantd instances can
// run it; a heartbeat renews the lease and observes cancellation.
//
//	queued              → collecting → indexing → surface_mapping →
//	                      hypothesis_generation → static_validation → review_required
//	validation_approved → lab_validation → review_required
//	report_ready        → completed
package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ljw097/remnant/internal/config"
	"github.com/ljw097/remnant/internal/fetch"
	"github.com/ljw097/remnant/internal/llm"
	"github.com/ljw097/remnant/internal/research"
	"github.com/ljw097/remnant/internal/store"
	"github.com/ljw097/remnant/internal/worker"
)

type Options struct {
	// Concurrency is the number of runs worked on at once. Default 2.
	Concurrency int
	// Worker executes analyzer and lab operations in a sandbox. nil means no
	// worker runtime: static analysis is skipped and approved lab
	// experiments are recorded as not run.
	Worker worker.Worker
	// StaticOperations run per target during static_validation. Default
	// static.patterns.
	StaticOperations []string
	// NewClient builds a model client from a decrypted profile. Default
	// llm.NewClient.
	NewClient func(llm.Profile) (llm.Client, error)
	// PollInterval between claim attempts. Default 2s.
	PollInterval time.Duration
	// LeaseTTL is how long a claim lasts without a heartbeat. Default 30s.
	LeaseTTL time.Duration
	// Fetcher pins pending git_repository targets. nil leaves them pending.
	Fetcher *fetch.Fetcher
}

type Orchestrator struct {
	store    *store.Store
	cfg      config.Config
	log      *slog.Logger
	opts     Options
	owner    string
	fetching atomic.Bool
	intaking atomic.Bool
}

func New(st *store.Store, cfg config.Config, log *slog.Logger, opts Options) *Orchestrator {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 2
	}
	if opts.StaticOperations == nil {
		opts.StaticOperations = []string{"static.patterns"}
	}
	if opts.NewClient == nil {
		opts.NewClient = llm.NewClient
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 2 * time.Second
	}
	if opts.LeaseTTL <= 0 {
		opts.LeaseTTL = 30 * time.Second
	}
	host, _ := os.Hostname()
	return &Orchestrator{store: st, cfg: cfg, log: log, opts: opts,
		owner: fmt.Sprintf("%s/%d/%s", host, os.Getpid(), randomHex(4))}
}

// Run claims and works runs until ctx is cancelled, then waits for the runs
// in progress to stop.
func (o *Orchestrator) Run(ctx context.Context) {
	o.log.Info("orchestrator started", "owner", o.owner, "concurrency", o.opts.Concurrency, "worker", o.opts.Worker != nil)
	sem := make(chan struct{}, o.opts.Concurrency)
	var wg sync.WaitGroup
	defer wg.Wait()
	tick := time.NewTicker(o.opts.PollInterval)
	defer tick.Stop()
	for {
		requeued, failed, err := o.store.ReapExpiredLeases(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			o.log.Error("reap expired leases", "err", err)
		case len(requeued)+len(failed) > 0:
			o.log.Warn("recovered runs whose orchestrator died", "requeued", requeued, "failed", failed)
		}
		o.fill(ctx, sem, &wg)
		o.startFetching(ctx, &wg)
		o.startIntakes(ctx, &wg)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (o *Orchestrator) fill(ctx context.Context, sem chan struct{}, wg *sync.WaitGroup) {
	for {
		select {
		case sem <- struct{}{}:
		default:
			return
		}
		id, status, err := o.store.ClaimRun(ctx, o.owner, o.opts.LeaseTTL)
		if err != nil {
			<-sem
			if !errors.Is(err, store.ErrNotFound) && ctx.Err() == nil {
				o.log.Error("claim run", "err", err)
			}
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			o.work(ctx, id, status)
		}()
	}
}

// startIntakes moves repository intakes forward in the background, one at
// a time, so a long history read never holds up runs.
func (o *Orchestrator) startIntakes(ctx context.Context, wg *sync.WaitGroup) {
	if !o.intaking.CompareAndSwap(false, true) {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer o.intaking.Store(false)
		for ctx.Err() == nil {
			ok, err := o.IntakeOnce(ctx)
			if err != nil && ctx.Err() == nil {
				o.log.Error("repository intake", "err", err)
			}
			if !ok || err != nil {
				return
			}
		}
	}()
}

// startFetching pins pending git targets in the background, one at a time,
// so slow clones never hold up run claiming.
func (o *Orchestrator) startFetching(ctx context.Context, wg *sync.WaitGroup) {
	if o.opts.Fetcher == nil || !o.fetching.CompareAndSwap(false, true) {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer o.fetching.Store(false)
		for ctx.Err() == nil {
			ok, err := o.FetchOnce(ctx)
			if err != nil && ctx.Err() == nil {
				o.log.Error("fetch target", "err", err)
			}
			if !ok || err != nil {
				return
			}
		}
	}()
}

// FetchOnce pins one pending git target and reports whether there was one.
// A fetch that fails marks the target unavailable with the reason.
func (o *Orchestrator) FetchOnce(ctx context.Context) (bool, error) {
	f := o.opts.Fetcher
	if f == nil {
		return false, nil
	}
	timeout := f.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	t, err := o.store.ClaimPendingFetch(ctx, o.owner, timeout+time.Minute)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	log := o.log.With("target", t.ID, "source", t.SourceURI, "ref", t.SourceRef)
	res, ferr := f.FetchHistory(ctx, t.SourceURI, t.SourceRef, t.SnapshotKey, t.HistoryDepth)
	commit, digest := res.Commit, res.Digest
	bg, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer done()
	if ferr != nil {
		if ctx.Err() != nil {
			return true, nil // shutting down; the lease expires and another fetch retries
		}
		log.Warn("target fetch failed", "err", ferr)
		return true, o.store.FailFetch(bg, t.ID, o.owner, ferr.Error())
	}
	if len(res.History) > 0 {
		recs := make([]store.CommitRecord, len(res.History))
		for i, c := range res.History {
			recs[i] = store.CommitRecord{SHA: c.SHA, CommittedAt: c.CommittedAt, Subject: c.Subject, Body: c.Body,
				Files: c.Files, Diff: c.Diff, Signals: c.Signals, Score: c.Score}
		}
		if err := o.store.SaveCommitRecords(bg, t.ID, recs); err != nil {
			return true, err
		}
	}
	log.Info("target pinned", "commit", commit, "fix_candidates", len(res.History))
	return true, o.store.CompleteFetch(bg, t.ID, o.owner, commit, digest)
}

// RunOnce claims one run and works it to the end of its current phase. It
// reports whether there was a run to work. Used by tests and tooling.
func (o *Orchestrator) RunOnce(ctx context.Context) (bool, error) {
	id, status, err := o.store.ClaimRun(ctx, o.owner, o.opts.LeaseTTL)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	o.work(ctx, id, status)
	return true, nil
}

var (
	errCancelled = errors.New("run cancelled")
	errPaused    = errors.New("run paused")
	errLeaseLost = errors.New("run lease lost")
	errWallTime  = errors.New("run wall time exceeded")
)

// job is one claimed run being worked; status tracks the run's status as
// this job last set it.
type job struct {
	o      *Orchestrator
	runID  int64
	status research.RunStatus
	log    *slog.Logger
	// settings are the instance's research settings when the job started.
	settings store.ResearchSettings
}

func (o *Orchestrator) work(parent context.Context, runID int64, status research.RunStatus) {
	j := &job{o: o, runID: runID, status: status, log: o.log.With("run", runID), settings: store.DefaultResearchSettings()}
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)
	stop := j.heartbeat(ctx, cancel)
	defer func() {
		stop()
		release, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if err := o.store.ReleaseLease(release, runID, o.owner); err != nil {
			j.log.Error("release lease", "err", err)
		}
	}()

	j.log.Info("working run", "status", status)
	// Anything still "running" belongs to a worker that is gone.
	if n, err := o.store.CancelStaleExperiments(ctx, runID); err != nil {
		j.finish(ctx, err)
		return
	} else if n > 0 {
		j.log.Warn("cancelled interrupted experiments", "count", n)
	}
	settings, err := o.store.ResearchSettings(ctx)
	if err != nil {
		j.finish(ctx, err)
		return
	}
	j.settings = settings
	switch status {
	case research.RunQueued:
		err = j.research(ctx)
	case research.RunValidationApproved:
		err = j.lab(ctx)
	case research.RunReportReady:
		err = j.report(ctx)
	default:
		err = fmt.Errorf("claimed run in unexpected status %s", status)
	}
	j.finish(ctx, err)
}

// heartbeat renews the lease every third of its TTL and cancels the work if
// the lease is lost or the run was cancelled or paused.
func (j *job) heartbeat(ctx context.Context, cancel context.CancelCauseFunc) (stop func()) {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(j.o.opts.LeaseTTL / 3)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
			}
			status, err := j.o.store.RenewLease(ctx, j.runID, j.o.owner, j.o.opts.LeaseTTL)
			switch {
			case errors.Is(err, store.ErrLeaseLost):
				cancel(errLeaseLost)
				return
			case err != nil:
				j.log.Error("renew lease", "err", err) // retried next tick; the TTL leaves room
			case status == research.RunPaused:
				cancel(errPaused)
				return
			case status.Terminal():
				cancel(errCancelled)
				return
			}
		}
	}()
	return func() { close(done); wg.Wait() }
}

// finish records how the job ended. Cancellation, pausing and lost leases
// are not errors of this job: the run's status already says what happened.
// A shutdown requeues the run at its resume point instead of failing it.
func (j *job) finish(ctx context.Context, err error) {
	if err == nil {
		return
	}
	cause := context.Cause(ctx)
	if errors.Is(cause, errCancelled) || errors.Is(cause, errPaused) || errors.Is(cause, errLeaseLost) {
		j.log.Info("run stopped", "cause", cause)
		return
	}
	bg, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	if current, serr := j.o.store.RunStatus(bg, j.runID); serr == nil && current != j.status {
		j.log.Info("run changed elsewhere", "status", current)
		return
	}
	if errors.Is(cause, context.Canceled) {
		if j.status.Automatic() {
			if rerr := j.o.store.RequeueRun(bg, j.runID, j.status, "remnantd shut down", false); rerr != nil {
				j.log.Error("requeue run", "err", rerr)
			}
		}
		return
	}
	to, summary := research.RunFailed, err.Error()
	if errors.Is(err, errWallTime) {
		to = research.RunTimedOut
	}
	j.log.Warn("run did not complete", "status", j.status, "to", to, "err", err)
	if ferr := j.o.store.FailRun(bg, j.runID, j.status, to, summary); ferr != nil {
		j.log.Error("record run failure", "err", ferr)
	}
}

func (j *job) advance(ctx context.Context, to research.RunStatus, message string) error {
	if err := ctx.Err(); err != nil {
		return stageErr(ctx, err)
	}
	if err := j.o.store.TransitionRun(ctx, j.runID, j.status, to, message); err != nil {
		return err
	}
	j.status = to
	return nil
}

func (j *job) event(ctx context.Context, eventType, message string, payload map[string]any) {
	if err := j.o.store.AppendEvent(ctx, j.runID, eventType, string(j.status), message, payload); err != nil && ctx.Err() == nil {
		j.log.Error("append event", "type", eventType, "err", err)
	}
}

// stageErr turns a context error into the cause that explains it.
func stageErr(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, err) {
		return fmt.Errorf("%w (%w)", cause, err)
	}
	return err
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
