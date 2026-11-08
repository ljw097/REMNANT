package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ljw097/remnant/internal/llm"
	"github.com/ljw097/remnant/internal/research"
	"github.com/ljw097/remnant/internal/source"
	"github.com/ljw097/remnant/internal/sourceindex"
	"github.com/ljw097/remnant/internal/store"
)

// The two modes and their combination: historical patterns guide which
// regions are read and how; general discovery audits regions directly; with
// both, a region is examined once.
func TestRunModes(t *testing.T) {
	e := setup(t)
	o := e.orchestrator(&fakeModel{}, nil)
	limits := research.Limits{MaxWorkers: 2, WallSeconds: 120, MaxTokens: 200000}
	run := func(patterns []int64, general bool) store.RunDetail {
		r, err := e.st.CreateRun(e.ctx, store.RunInput{ProjectID: e.projectID, TargetIDs: []int64{e.targetID},
			PatternIDs: patterns, Limits: limits, AutoDiscover: general}, e.alice)
		must(t, err)
		e.runOnce(o)
		if s, summary := e.status(r.ID); s != research.RunReviewRequired {
			t.Fatalf("run %d: %s (%s)", r.ID, s, summary)
		}
		return r
	}
	regions := func(runID int64) (byPattern map[int64]int, distinct int) {
		cs, err := e.st.ListCandidates(e.ctx, runID)
		must(t, err)
		byPattern = map[int64]int{}
		seen := map[string]bool{}
		for _, c := range cs {
			byPattern[c.Meta.PatternID]++
			seen[fmt.Sprintf("%s:%d", c.FilePath, *c.LineStart)] = true
		}
		return byPattern, len(seen)
	}
	researcherModes := func(runID int64) (historic, general int) {
		calls, err := e.st.ListModelCalls(e.ctx, store.ModelCallFilter{RunID: &runID})
		must(t, err)
		for _, c := range calls {
			if c.Role != roleResearcher {
				continue
			}
			full, err := e.st.GetModelCall(e.ctx, c.ID)
			must(t, err)
			if strings.Contains(full.Request, `"pattern": {`) {
				historic++
			} else {
				general++
			}
		}
		return
	}

	hist := run([]int64{e.patternID}, false)
	by, n := regions(hist.ID)
	h, g := researcherModes(hist.ID)
	if by[e.patternID] != 2 || by[generalPatternID] != 0 || n != 2 || h != 2 || g != 0 {
		t.Fatalf("historic run: regions %v (%d distinct), researcher calls historic %d general %d", by, n, h, g)
	}
	if e.count(`SELECT count(*) FROM remnant.hypotheses WHERE run_id = $1 AND pattern_id IS DISTINCT FROM $2`, hist.ID, e.patternID) != 0 {
		t.Fatal("a historic-mode hypothesis lacks its pattern")
	}

	gen := run(nil, true)
	by, n = regions(gen.ID)
	h, g = researcherModes(gen.ID)
	if by[generalPatternID] != 2 || len(by) != 1 || h != 0 || g != 2 {
		t.Fatalf("general run: regions %v, researcher calls historic %d general %d", by, h, g)
	}
	if e.count(`SELECT count(*) FROM remnant.hypotheses WHERE run_id = $1 AND pattern_id IS NOT NULL`, gen.ID) != 0 {
		t.Fatal("a general-mode hypothesis carries a pattern")
	}

	// Both regions memcpy appears in also match the pattern: general
	// discovery leaves them to the pattern instead of examining them again.
	both := run([]int64{e.patternID}, true)
	by, n = regions(both.ID)
	h, g = researcherModes(both.ID)
	if by[e.patternID] != 2 || by[generalPatternID] != 0 || n != 2 || h != 2 || g != 0 {
		t.Fatalf("combined run: regions %v (%d distinct), researcher calls historic %d general %d", by, n, h, g)
	}

	// Neither mode has nothing to look for.
	if _, err := e.st.CreateRun(e.ctx, store.RunInput{ProjectID: e.projectID, TargetIDs: []int64{e.targetID}, Limits: limits}, e.alice); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("run with no pattern and no general discovery: %v", err)
	}
	// The mode is part of the scope.
	if hist.ScopeHash == both.ScopeHash {
		t.Fatal("scope hash ignores general discovery")
	}
}

// General discovery gets the candidate slots patterns leave unused.
func TestGeneralDiscoveryUsesFreeSlots(t *testing.T) {
	items := func(n int) []candidate {
		var out []candidate
		for i := range n {
			out = append(out, candidate{Chunk: chunkAt("a.c", i*10+1)})
		}
		return out
	}
	taken := []topK{{k: 3, items: []candidate{{Chunk: chunkAt("a.c", 1)}, {Chunk: chunkAt("a.c", 21)}}}}
	got := withoutRegions(items(6), taken, 3)
	if len(got) != 3 || got[0].Chunk.Start != 11 || got[1].Chunk.Start != 31 || got[2].Chunk.Start != 41 {
		t.Fatalf("withoutRegions = %+v", got)
	}
}

// pausingModel pauses the run during its first call, as a person pressing
// pause would, and answers normally.
type pausingModel struct {
	fakeModel
	once  sync.Once
	pause func()
}

func (p *pausingModel) CompleteJSON(ctx context.Context, req llm.Request) (llm.Response, error) {
	if req.AgentKey == roleResearcher {
		p.once.Do(p.pause)
	}
	return p.fakeModel.CompleteJSON(ctx, req)
}

// After a pause no new model call starts, even before the heartbeat's next
// tick would have noticed it.
func TestPauseStopsNewCallsAtOnce(t *testing.T) {
	e := setup(t)
	runID := e.newRunWorkers(200000, 1)
	m := &pausingModel{pause: func() { must(t, e.st.PauseRun(e.ctx, runID, "alice")) }}
	o := e.orchestrator(m, nil)
	o.opts.LeaseTTL = time.Minute // the heartbeat would notice only after 20s
	start := time.Now()
	e.runOnce(o)
	if s, _ := e.status(runID); s != research.RunPaused || time.Since(start) > 10*time.Second {
		t.Fatalf("status %s after %v", s, time.Since(start))
	}
	if n := m.count(roleResearcher) + m.count(roleSkeptic); n != 1 {
		t.Fatalf("%d model calls after the pause, want only the one in flight", n)
	}
}

// Targets, runs and patterns can be edited and deleted only in ways that
// keep runs in progress working and audit records intact.
func TestManagementRules(t *testing.T) {
	e := setup(t)
	o := e.orchestrator(&fakeModel{}, nil)

	// A target a run used cannot be deleted; a queued (busy) run blocks a re-pin.
	runID := e.newRun(200000)
	if _, err := e.st.DeleteTarget(e.ctx, e.targetID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("delete a used target: %v", err)
	}
	rev, digest, err := source.LocalSnapshot(e.cfg.SnapshotDir(), "demo")
	must(t, err)
	if _, err := e.st.RepinLocalTarget(e.ctx, e.targetID, rev, digest); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("re-pin under a queued run: %v", err)
	}
	if err := e.st.DeleteRun(e.ctx, runID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("delete a queued run: %v", err)
	}
	e.runOnce(o) // now waiting for review: no longer reads the tree
	if _, err := e.st.RepinLocalTarget(e.ctx, e.targetID, rev, digest); err != nil {
		t.Fatalf("re-pin with the run waiting for review: %v", err)
	}
	if use, err := e.st.TargetUse(e.ctx, e.targetID); err != nil || use.Runs != 1 || use.Busy != 0 {
		t.Fatalf("target use %+v %v", use, err)
	}
	must(t, e.st.CancelRun(e.ctx, runID, "alice"))
	must(t, e.st.DeleteRun(e.ctx, runID))
	if e.count(`SELECT count(*) FROM remnant.runs WHERE id = $1`, runID) != 0 ||
		e.count(`SELECT count(*) FROM remnant.model_calls WHERE run_id = $1`, runID) != 0 {
		t.Fatal("deleted run left records behind")
	}
	if _, err := e.st.DeleteTarget(e.ctx, e.targetID); err != nil {
		t.Fatalf("delete a target no run uses any more: %v", err)
	}

	// Patterns: edit while unreviewed, never after review; delete only unused.
	title := "Edited title"
	sigs := []string{"memcpy", "memmove"}
	p, err := e.st.UpdatePattern(e.ctx, e.patternID, store.PatternPatch{Title: &title, CodeSignatures: &sigs})
	must(t, err)
	if p.Title != title || !strings.Contains(string(p.CodeSignatures), "memmove") {
		t.Fatalf("edited pattern %+v", p)
	}
	_, err = e.st.ReviewPattern(e.ctx, e.patternID, "reviewed", e.bob, false)
	must(t, err)
	if _, err := e.st.UpdatePattern(e.ctx, e.patternID, store.PatternPatch{Title: &title}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("edit a reviewed pattern: %v", err)
	}
	must(t, e.st.DeletePattern(e.ctx, e.patternID)) // its only run was deleted
	if err := e.st.DeletePattern(e.ctx, e.patternID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}

	// An archived project takes no new targets.
	archived := "archived"
	_, err = e.st.UpdateProject(e.ctx, e.projectID, store.ProjectPatch{Status: &archived})
	must(t, err)
	if _, err := e.st.CreateTarget(e.ctx, e.projectID, store.TargetInput{Kind: "local_snapshot", DisplayName: "x", SnapshotKey: "demo",
		Revision: rev, TreeSHA256: digest, Status: "ready"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("target on an archived project: %v", err)
	}

	// An intake needs something to hunt with; general discovery alone is enough.
	if _, err := (store.IntakeOptions{AutoDiscover: true}).Normalize(); err != nil {
		t.Fatalf("general-only intake: %v", err)
	}
	if _, err := (store.IntakeOptions{}).Normalize(); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("intake with nothing to hunt with: %v", err)
	}
}

func chunkAt(path string, start int) sourceindex.Chunk {
	return sourceindex.Chunk{Path: path, Start: start, End: start + 9}
}
