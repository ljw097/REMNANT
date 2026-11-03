package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ljw097/remnant/internal/llm"
	"github.com/ljw097/remnant/internal/research"
	"github.com/ljw097/remnant/internal/source"
	"github.com/ljw097/remnant/internal/sourceindex"
	"github.com/ljw097/remnant/internal/store"
)

// research runs the automatic source pipeline of a queued run. A run that
// was paused or interrupted resumes at runs.resume_stage: collecting always
// runs again (the snapshots are re-verified), stages before the resume stage
// are skipped, and work recorded inside the resume stage is not repeated.
func (j *job) research(parent context.Context) (err error) {
	scope, err := j.o.store.WorkScope(parent, j.runID)
	if err != nil {
		return err
	}
	// wall_seconds limits time spent working, so paused time is not counted.
	wall := time.Duration(scope.Limits.WallSeconds) * time.Second
	remaining := wall - time.Duration(scope.Run.ActiveMs)*time.Millisecond
	if remaining <= 0 {
		return fmt.Errorf("%w: %s of work already used", errWallTime, wall)
	}
	started := time.Now()
	defer func() {
		bg, done := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
		defer done()
		if aerr := j.o.store.AddActiveTime(bg, j.runID, time.Since(started)); aerr != nil {
			j.log.Error("record active time", "err", aerr)
		}
	}()
	ctx, cancel := context.WithTimeoutCause(parent, remaining, errWallTime)
	defer cancel()
	defer func() {
		if err != nil && errors.Is(context.Cause(ctx), errWallTime) && !errors.Is(err, errWallTime) {
			err = fmt.Errorf("%w after %s: %w", errWallTime, wall, err)
		}
	}()

	resumeAt := research.RunStatus(scope.Run.ResumeStage)
	if resumeAt != "" {
		j.event(ctx, "run.resumed", fmt.Sprintf("Resuming at %s (attempt %d)", resumeAt, scope.Run.Attempts+1),
			map[string]any{"resume_stage": resumeAt, "attempts": scope.Run.Attempts})
	}
	stages := []struct {
		status  research.RunStatus
		message string
		run     func(context.Context) error
	}{
		{research.RunCollecting, "Verifying pinned targets and loading patterns", func(ctx context.Context) error { return j.collect(ctx, scope) }},
		{research.RunIndexing, "Indexing source snapshots", func(ctx context.Context) error {
			for _, t := range scope.Targets {
				if err := j.index(ctx, t); err != nil {
					return err
				}
			}
			return nil
		}},
		{research.RunSurfaceMapping, "Matching patterns to source regions", func(ctx context.Context) error { return j.mapSurface(ctx, scope) }},
		{research.RunHypothesisGeneration, "Generating hypotheses", func(ctx context.Context) error { return j.generate(ctx, scope) }},
		{research.RunStaticValidation, "Running static analyzers", func(ctx context.Context) error { return j.static(ctx, scope) }},
	}
	for _, st := range stages {
		if err := j.advance(ctx, st.status, st.message); err != nil {
			return err
		}
		if st.status != research.RunCollecting && stageBefore(st.status, resumeAt) {
			j.event(ctx, "stage.skipped", fmt.Sprintf("%s already completed before the run was interrupted", st.status), nil)
			continue
		}
		if err := st.run(ctx); err != nil {
			return err
		}
	}
	if scope.Run.Auto {
		// Autopilot: reach review_required, then triage the hypotheses and
		// move to report_ready, which the orchestrator claims and renders.
		// Lab experiments are not run here — code execution still needs a
		// person's approval.
		if err := j.advance(ctx, research.RunReviewRequired, "Automatic analysis finished"); err != nil {
			return err
		}
		res, err := j.o.store.AutoTriage(ctx, j.runID)
		if err != nil {
			return err
		}
		j.event(ctx, "run.autopiloted", fmt.Sprintf("Autopilot accepted %d and rejected %d hypotheses", res.Accepted, res.Rejected),
			map[string]any{"accepted": res.Accepted, "rejected": res.Rejected})
		return nil
	}
	return j.advance(ctx, research.RunReviewRequired, "Automatic analysis finished; hypotheses await review")
}

// stageBefore reports whether stage a comes before stage b in a source run.
func stageBefore(a, b research.RunStatus) bool {
	ia, ib := slices.Index(research.SourceStages, a), slices.Index(research.SourceStages, b)
	return ia >= 0 && ib >= 0 && ia < ib
}

// snapshotDir is where a ready target's tree lives: a local snapshot placed
// by an operator or a git checkout made by the fetcher.
func (j *job) snapshotDir(t store.WorkTarget) (string, error) {
	if err := source.ValidateSnapshotKey(t.SnapshotKey); err != nil {
		return "", err
	}
	return filepath.Join(j.o.cfg.SnapshotDir(), t.SnapshotKey), nil
}

// collect re-verifies every target: it must still be pinned to the revision
// the run was created with, and its tree on disk must still hash to the
// digest recorded when it was pinned.
func (j *job) collect(ctx context.Context, scope store.WorkScope) error {
	for _, t := range scope.Targets {
		if t.CurrentRevision != t.Revision {
			return fmt.Errorf("target %d was re-pinned since the run was created (run %s, target now %s)", t.ID, t.Revision, t.CurrentRevision)
		}
		if _, err := source.VerifySnapshot(j.o.cfg.SnapshotDir(), t.SnapshotKey, t.TreeSHA256); err != nil {
			return fmt.Errorf("target %d: %w", t.ID, err)
		}
	}
	var withSigs int
	for _, p := range scope.Patterns {
		if len(signaturesOf(p)) > 0 {
			withSigs++
		}
	}
	j.event(ctx, "stage.collected", fmt.Sprintf("%d target(s) verified; %d of %d pattern(s) have code signatures",
		len(scope.Targets), withSigs, len(scope.Patterns)), nil)
	return nil
}

func signaturesOf(p store.Pattern) []signature {
	var list []string
	_ = json.Unmarshal(p.CodeSignatures, &list)
	return compileSignatures(list)
}

func counterSignaturesOf(p store.Pattern) []signature {
	var list []string
	_ = json.Unmarshal(p.CounterSignatures, &list)
	return compileSignatures(list)
}

type indexFile struct {
	Path     string `json:"path"`
	Language string `json:"language"`
	Lines    int    `json:"lines"`
	SHA256   string `json:"sha256"`
}

// index writes a symbol index of one target as a source_index artifact.
func (j *job) index(ctx context.Context, t store.WorkTarget) error {
	dir, err := j.snapshotDir(t)
	if err != nil {
		return err
	}
	var files []indexFile
	var symbols []sourceindex.Chunk
	truncated, err := sourceindex.Walk(dir, func(f sourceindex.File) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		files = append(files, indexFile{f.Path, f.Language, len(f.Lines), f.SHA256})
		for _, c := range sourceindex.ChunkFile(f.Path, f.Language, f.Lines) {
			if c.Symbol != "" {
				symbols = append(symbols, c)
			}
		}
		return nil
	})
	if err != nil {
		return stageErr(ctx, err)
	}
	body, err := json.Marshal(map[string]any{
		"target_id": t.ID, "revision": t.Revision, "truncated": truncated,
		"files": files, "symbols": symbols,
	})
	if err != nil {
		return err
	}
	a, err := j.writeArtifact(ctx, "source_index", fmt.Sprintf("index-target-%d.json", t.ID), "application/json", body)
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("Target %d: %d files, %d symbols indexed", t.ID, len(files), len(symbols))
	if truncated {
		msg += fmt.Sprintf(" (stopped at %d files)", sourceindex.MaxFiles)
	}
	j.event(ctx, "stage.indexed", msg, map[string]any{"target_id": t.ID, "artifact_id": a.ID, "truncated": truncated})
	return nil
}

// mapSurface scores every source region against every pattern, keeps the
// best few per pattern and target, and records each as run-level evidence.
// Recording is idempotent, so the stage can be repeated after an
// interruption.
func (j *job) mapSurface(ctx context.Context, scope store.WorkScope) error {
	type compiled struct {
		p        store.Pattern
		sigs     []signature
		counters []signature
	}
	var patterns []compiled
	for _, p := range scope.Patterns {
		if sigs := signaturesOf(p); len(sigs) > 0 {
			patterns = append(patterns, compiled{p, sigs, counterSignaturesOf(p)})
		}
	}
	if len(patterns) == 0 {
		j.event(ctx, "stage.mapped", "No pattern with code signatures; nothing to map", nil)
		return nil
	}

	var perPattern [][]candidate
	for _, t := range scope.Targets {
		dir, err := j.snapshotDir(t)
		if err != nil {
			return err
		}
		tops := make([]topK, len(patterns))
		for i := range tops {
			tops[i].k = j.settings.CandidatesPerPattern
		}
		_, err = sourceindex.Walk(dir, func(f sourceindex.File) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			for _, c := range sourceindex.ChunkFile(f.Path, f.Language, f.Lines) {
				text := strings.Join(f.Lines[c.Start-1:c.End], "\n")
				for i, p := range patterns {
					sc, matched, countered, ok := score(text, p.sigs, p.counters)
					if !ok {
						continue
					}
					cand := candidate{TargetID: t.ID, Revision: t.Revision, Pattern: p.p, Chunk: c,
						Score: sc, Matched: matched, Countered: countered}
					if len(tops[i].items) < tops[i].k || cand.less(tops[i].items[len(tops[i].items)-1]) {
						cand.Excerpt, cand.SHA256 = numbered(f.Lines, c.Start, c.End)
						tops[i].add(cand)
					}
				}
			}
			return nil
		})
		if err != nil {
			return stageErr(ctx, err)
		}
		for _, top := range tops {
			perPattern = append(perPattern, top.items)
		}
	}

	cands := selectCandidates(perPattern, j.settings.MaxCandidates)
	for i := range cands {
		c := &cands[i]
		summary := fmt.Sprintf("Region matches signature(s) %s of pattern %d", strings.Join(c.Matched, ", "), c.Pattern.ID)
		if len(c.Countered) > 0 {
			summary += fmt.Sprintf("; also matches counter-signature(s) %s", strings.Join(c.Countered, ", "))
		}
		key := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%s|%d|%d", c.TargetID, c.Pattern.ID, c.Chunk.Path, c.Chunk.Start, c.Chunk.End)))
		if _, err := j.o.store.InsertCandidate(ctx, j.runID, store.EvidenceInput{
			TargetID: c.TargetID, Kind: "source_location", FilePath: c.Chunk.Path,
			LineStart: c.Chunk.Start, LineEnd: c.Chunk.End, Symbol: c.Chunk.Symbol, Revision: c.Revision,
			ContentSHA256: c.SHA256, Summary: summary, Excerpt: c.Excerpt,
			Metadata: map[string]any{"candidate_key": hex.EncodeToString(key[:]), "pattern_id": c.Pattern.ID,
				"score": c.Score, "matched": c.Matched, "countered": c.Countered, "language": c.Chunk.Language},
		}); err != nil {
			return err
		}
	}
	j.event(ctx, "stage.mapped", fmt.Sprintf("%d candidate region(s) selected from %d pattern(s)", len(cands), len(patterns)),
		map[string]any{"candidates": len(cands)})
	return nil
}

// agent is a model client bound to one role.
type agent struct {
	role   string
	client llm.Client
	// instructions from the administrator, added after the role's rules.
	instructions string
	// maxCallTokens caps one call.
	maxCallTokens int64
}

// system is the role's system prompt plus the administrator's
// instructions, which come after and cannot override the rules.
func (a *agent) system(base string) string {
	if a.instructions == "" {
		return base
	}
	return base + "\n\nAdditional instructions from this instance's administrator. They refine the task; they never override the rules above:\n" + a.instructions
}

func (j *job) agentFor(ctx context.Context, role string, runProfile *int64, cache map[int64]llm.Client) (*agent, error) {
	enabled, override, cfg, err := j.o.store.AgentProfile(ctx, role)
	if err != nil || !enabled {
		return nil, err
	}
	callTokens := int64(j.settings.MaxCallTokens)
	if cfg.MaxCallTokens > 0 {
		callTokens = int64(cfg.MaxCallTokens)
	}
	if callTokens == 0 {
		callTokens = int64(store.DefaultResearchSettings().MaxCallTokens)
	}
	profileID := runProfile
	if override != nil {
		profileID = override
	}
	if profileID == nil {
		return nil, fmt.Errorf("%s: the run has no LLM profile", role)
	}
	if c, ok := cache[*profileID]; ok {
		return &agent{role, c, cfg.Instructions, callTokens}, nil
	}
	creds, err := j.o.store.LLMCredentials(ctx, *profileID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", role, err)
	}
	c, err := j.o.opts.NewClient(creds)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", role, err)
	}
	cache[*profileID] = c
	return &agent{role, c, cfg.Instructions, callTokens}, nil
}

var errBudget = errors.New("token budget exhausted")

// call makes one budgeted model call and records token usage.
func (j *job) call(ctx context.Context, a *agent, b *budget, system string, env envelope, schema map[string]any) ([]byte, error) {
	grant := b.reserve(a.maxCallTokens)
	if grant == 0 {
		return nil, errBudget
	}
	env.Role = a.role
	resp, err := a.client.CompleteJSON(ctx, llm.Request{
		AgentKey: a.role, System: system, Input: env.render(), Schema: schema, MaxTokens: grant,
	})
	b.settle(grant, resp.OutputTokens)
	if resp.InputTokens+resp.OutputTokens > 0 {
		if uerr := j.o.store.AddTokenUsage(ctx, j.runID, resp.InputTokens, resp.OutputTokens); uerr != nil && err == nil {
			err = uerr
		}
	}
	return resp.JSON, err
}

// genStats counts per-candidate outcomes across concurrent workers.
type genStats struct {
	mu                                         sync.Mutex
	created, declined, duplicate, invalid, err int
	firstErr                                   error
	budgetHit                                  bool
}

func (s *genStats) add(f func(*genStats)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(s)
}

// workItem is a candidate region to examine, or a drafted hypothesis whose
// Skeptic step did not run before an interruption.
type workItem struct {
	c     candidate
	draft *store.HypothesisDetail
}

// pendingWork rebuilds the generation queue from the database, so a resumed
// run continues where it stopped: candidates without an outcome, and draft
// hypotheses that were never challenged.
func (j *job) pendingWork(ctx context.Context, scope store.WorkScope) ([]workItem, error) {
	stored, err := j.o.store.ListCandidates(ctx, j.runID)
	if err != nil {
		return nil, err
	}
	patterns := map[int64]store.Pattern{}
	for _, p := range scope.Patterns {
		patterns[p.ID] = p
	}
	byID := map[int64]candidate{}
	var items []workItem
	for _, sc := range stored {
		p, ok := patterns[sc.Meta.PatternID]
		if !ok || sc.LineStart == nil || sc.LineEnd == nil {
			continue
		}
		c := candidate{TargetID: sc.TargetID, Revision: sc.Revision, Pattern: p,
			Chunk: sourceindex.Chunk{Path: sc.FilePath, Language: sc.Meta.Language, Symbol: sc.Symbol,
				Start: int(*sc.LineStart), End: int(*sc.LineEnd)},
			Score: sc.Meta.Score, Matched: sc.Meta.Matched, Countered: sc.Meta.Countered,
			Excerpt: sc.Excerpt, SHA256: sc.ContentSHA256, evidenceID: sc.ID}
		byID[sc.ID] = c
		if sc.Outcome == "" {
			items = append(items, workItem{c: c})
		}
	}
	hs, err := j.o.store.ListHypotheses(ctx, j.runID)
	if err != nil {
		return nil, err
	}
	for _, h := range hs {
		if h.Status != string(research.HypothesisDraft) {
			continue
		}
		d, err := j.o.store.GetHypothesis(ctx, h.ID)
		if err != nil {
			return nil, err
		}
		found := false
		for _, ev := range d.Evidence {
			var m struct {
				CopiedFrom int64 `json:"copied_from"`
			}
			if json.Unmarshal(ev.Metadata, &m) == nil {
				if c, ok := byID[m.CopiedFrom]; ok {
					items = append(items, workItem{c: c, draft: &d})
					found = true
					break
				}
			}
		}
		if !found {
			j.toTriage(ctx, h.ID)
		}
	}
	return items, nil
}

// generate runs Researcher then Skeptic for each pending item, MaxWorkers at
// a time. Bad model output and refusals skip a candidate; the run fails only
// if every model call errored. The token budget counts what earlier attempts
// of the run already spent.
func (j *job) generate(ctx context.Context, scope store.WorkScope) error {
	items, err := j.pendingWork(ctx, scope)
	if err != nil || len(items) == 0 {
		return err
	}
	clients := map[int64]llm.Client{}
	researcher, err := j.agentFor(ctx, roleResearcher, scope.Run.LLMProfileID, clients)
	if err != nil {
		return err
	}
	if researcher == nil {
		j.event(ctx, "stage.skipped", "Hypothesis Researcher is disabled in agent_profiles", nil)
		return nil
	}
	skeptic, err := j.agentFor(ctx, roleSkeptic, scope.Run.LLMProfileID, clients)
	if err != nil {
		return err
	}
	if skeptic == nil {
		j.event(ctx, "stage.notice", "Skeptic is disabled in agent_profiles; hypotheses go to triage unchallenged", nil)
	}

	b := newBudget(max(0, int64(scope.Limits.MaxTokens)-scope.Run.OutputTokens))
	var stats genStats
	work := make(chan workItem)
	var wg sync.WaitGroup
	for range scope.Limits.MaxWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for it := range work {
				j.investigate(ctx, researcher, skeptic, b, it, &stats)
			}
		}()
	}
feed:
	for _, it := range items {
		select {
		case work <- it:
		case <-ctx.Done():
			break feed
		}
	}
	close(work)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return stageErr(ctx, err)
	}

	msg := fmt.Sprintf("%d hypothesis(es) drafted; %d region(s) without a credible hypothesis, %d duplicate, %d invalid answer(s), %d failed call(s); %d output tokens used",
		stats.created, stats.declined, stats.duplicate, stats.invalid, stats.err, b.spent())
	if stats.budgetHit {
		msg += "; token budget exhausted before all regions were examined"
	}
	j.event(ctx, "stage.generated", msg, map[string]any{"created": stats.created, "output_tokens": b.spent()})
	if stats.err > 0 && stats.created+stats.declined+stats.duplicate+stats.invalid == 0 {
		return fmt.Errorf("every model call failed; first error: %w", stats.firstErr)
	}
	return nil
}

// mark records a candidate's outcome so a resumed run does not ask again.
// Transient failures are not marked and are retried on resume.
func (j *job) mark(ctx context.Context, c candidate, outcome string) {
	if err := j.o.store.MarkCandidate(ctx, c.evidenceID, outcome); err != nil && ctx.Err() == nil {
		j.log.Error("mark candidate", "evidence", c.evidenceID, "err", err)
	}
}

func (j *job) investigate(ctx context.Context, researcher, skeptic *agent, b *budget, it workItem, stats *genStats) {
	c := it.c
	ev := evidenceView{c.evidenceID, c.Chunk.Path, c.Chunk.Start, c.Chunk.End, c.Chunk.Symbol, c.SHA256, c.Excerpt}
	allowed := map[int64]evidenceView{ev.EvidenceID: ev}
	env := envelope{
		TaskID:       fmt.Sprintf("run-%d/evidence-%d", j.runID, c.evidenceID),
		Target:       map[string]any{"target_id": c.TargetID, "revision": c.Revision, "language": c.Chunk.Language},
		Capabilities: []string{"source.read", "evidence.read"},
		Pattern:      viewPattern(c.Pattern),
		Evidence:     []evidenceView{ev},
		Instructions: []string{
			"Decide whether this region plausibly contains the pattern's root cause.",
			fmt.Sprintf("The region matched signature(s): %s.", strings.Join(c.Matched, ", ")),
		},
	}

	var hypID int64
	var hypothesis map[string]any
	var researcherConfidence string
	if d := it.draft; d != nil {
		hypID, researcherConfidence = d.ID, d.TriageConfidence
		hypothesis = map[string]any{
			"hypothesis_id": d.ID, "title": d.Title, "claim": d.Claim, "facts": d.Facts,
			"preconditions": d.Preconditions, "unknowns": d.Unknowns, "affected_symbols": d.AffectedSymbols,
			"expected_observation": d.ExpectedObservation, "counter_evidence_to_check": d.CounterEvidence,
			"triage_confidence": d.TriageConfidence,
		}
	} else {
		raw, err := j.call(ctx, researcher, b, researcher.system(researcherSystem), env, researcherSchema)
		if kind := j.callFailed(ctx, err, c, "researcher", stats); kind != "" {
			if kind == "refused" {
				j.mark(ctx, c, "refused")
			}
			return
		}
		out, err := parseResearcher(raw, allowed)
		if err != nil {
			stats.add(func(s *genStats) { s.invalid++ })
			j.mark(ctx, c, "invalid")
			j.event(ctx, "agent.invalid_output", fmt.Sprintf("Researcher answer for evidence %d rejected: %v", c.evidenceID, err),
				map[string]any{"evidence_id": c.evidenceID})
			return
		}
		if !out.Found {
			stats.add(func(s *genStats) { s.declined++ })
			j.mark(ctx, c, "declined")
			return
		}
		patternID := c.Pattern.ID
		dedupe := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%s|%s|%d", c.TargetID, patternID, c.Chunk.Path, c.Chunk.Symbol, c.Chunk.Start)))
		// InsertHypothesis also marks the candidate, in the same transaction.
		hypID, err = j.o.store.InsertHypothesis(ctx, j.runID, store.HypothesisInput{
			TargetID: c.TargetID, PatternID: &patternID, Title: out.Title, Claim: out.Claim,
			VulnerabilityClass: out.VulnerabilityClass, CWEID: out.CWEID, Facts: out.Facts,
			Preconditions: out.Preconditions, Unknowns: out.Unknowns, AffectedSymbols: out.AffectedSymbols,
			ExpectedObservation: out.ExpectedObservation, CounterEvidence: out.CounterEvidenceToCheck,
			TriageConfidence: out.TriageConfidence, DedupeKey: hex.EncodeToString(dedupe[:]),
		}, out.SupportingEvidenceIDs)
		switch {
		case errors.Is(err, store.ErrDuplicate):
			stats.add(func(s *genStats) { s.duplicate++ })
			j.mark(ctx, c, "duplicate")
			return
		case err != nil:
			if ctx.Err() == nil {
				stats.add(func(s *genStats) { s.invalid++ })
				j.mark(ctx, c, "invalid")
				j.event(ctx, "agent.invalid_output", fmt.Sprintf("Hypothesis for evidence %d not stored: %v", c.evidenceID, err), nil)
			}
			return
		}
		stats.add(func(s *genStats) { s.created++ })
		researcherConfidence = out.TriageConfidence
		hypothesis = map[string]any{
			"hypothesis_id": hypID, "title": out.Title, "claim": out.Claim, "facts": out.Facts,
			"preconditions": out.Preconditions, "unknowns": out.Unknowns, "affected_symbols": out.AffectedSymbols,
			"expected_observation": out.ExpectedObservation, "counter_evidence_to_check": out.CounterEvidenceToCheck,
			"triage_confidence": out.TriageConfidence,
		}
	}

	if skeptic == nil {
		j.toTriage(ctx, hypID)
		return
	}
	env.TaskID = fmt.Sprintf("run-%d/hypothesis-%d", j.runID, hypID)
	env.Hypothesis = hypothesis
	env.Instructions = []string{"Challenge this hypothesis using only the cited excerpt. Check counter_evidence_to_check first."}
	raw, err := j.call(ctx, skeptic, b, skeptic.system(skepticSystem), env, skepticSchema)
	switch kind := j.callFailed(ctx, err, c, "skeptic", stats); kind {
	case "":
	case "stopped", "budget":
		return // stays a draft; challenged when the run resumes
	default:
		j.toTriage(ctx, hypID)
		return
	}
	sk, err := parseSkeptic(raw, allowed)
	if err != nil {
		stats.add(func(s *genStats) { s.invalid++ })
		j.event(ctx, "agent.invalid_output", fmt.Sprintf("Skeptic answer for hypothesis %d rejected: %v", hypID, err),
			map[string]any{"hypothesis_id": hypID})
		j.toTriage(ctx, hypID)
		return
	}
	counter := make([]string, 0, len(sk.CounterEvidence))
	var counterEv []store.EvidenceInput
	for _, ce := range sk.CounterEvidence {
		counter = append(counter, ce.Summary)
		in := store.EvidenceInput{TargetID: c.TargetID, Kind: "counter_evidence", Revision: c.Revision,
			Summary: ce.Summary, Metadata: map[string]any{"agent": roleSkeptic, "cites_evidence_id": ce.EvidenceID}}
		if ce.EvidenceID != 0 {
			in.FilePath, in.Symbol = ev.FilePath, ev.Symbol
			in.LineStart, in.LineEnd = ce.LineStart, ce.LineEnd
		}
		counterEv = append(counterEv, in)
	}
	counterEv = append(counterEv, store.EvidenceInput{TargetID: c.TargetID, Kind: "counter_evidence", Revision: c.Revision,
		Summary: "Skeptic rationale (" + sk.Verdict + "): " + sk.Rationale, Metadata: map[string]any{"agent": roleSkeptic}})
	conf := finalConfidence(researcherConfidence, sk.Verdict, sk.AdjustedConfidence)
	if err := j.o.store.RecordSkeptic(ctx, j.runID, hypID, sk.Verdict, conf, counter, counterEv); err != nil && ctx.Err() == nil {
		j.log.Error("record skeptic", "hypothesis", hypID, "err", err)
	}
}

// callFailed classifies a model call error: "" (no error), "stopped" (the
// run is stopping), "budget", "refused" or "error".
func (j *job) callFailed(ctx context.Context, err error, c candidate, role string, stats *genStats) string {
	var refusal *llm.RefusalError
	switch {
	case err == nil:
		return ""
	case ctx.Err() != nil:
		return "stopped"
	case errors.Is(err, errBudget):
		stats.add(func(s *genStats) { s.budgetHit = true })
		return "budget"
	case errors.As(err, &refusal):
		stats.add(func(s *genStats) { s.declined++ })
		j.event(ctx, "agent.refused", fmt.Sprintf("Model declined the %s task for evidence %d (%s)", role, c.evidenceID, refusal.Category),
			map[string]any{"evidence_id": c.evidenceID, "category": refusal.Category})
		return "refused"
	default:
		stats.add(func(s *genStats) {
			s.err++
			if s.firstErr == nil {
				s.firstErr = err
			}
		})
		j.event(ctx, "agent.error", fmt.Sprintf("%s call for evidence %d failed: %v", role, c.evidenceID, err),
			map[string]any{"evidence_id": c.evidenceID})
		return "error"
	}
}

func (j *job) toTriage(ctx context.Context, hypID int64) {
	if err := j.o.store.SetHypothesisStatus(ctx, hypID, research.HypothesisDraft, research.HypothesisTriage); err != nil && ctx.Err() == nil {
		j.log.Error("move hypothesis to triage", "hypothesis", hypID, "err", err)
	}
}
