package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ljw097/remnant/internal/llm"
	"github.com/ljw097/remnant/internal/research"
	"github.com/ljw097/remnant/internal/source"
	"github.com/ljw097/remnant/internal/sourceindex"
	"github.com/ljw097/remnant/internal/store"
)

// maxCallTokens caps max_tokens for one agent call (non-streaming requests
// must stay well inside HTTP timeouts).
const maxCallTokens = 16000

// research runs the automatic source pipeline of a queued run.
func (j *job) research(parent context.Context) (err error) {
	scope, err := j.o.store.WorkScope(parent, j.runID)
	if err != nil {
		return err
	}
	wall := time.Duration(scope.Limits.WallSeconds) * time.Second
	ctx, cancel := context.WithTimeoutCause(parent, wall, errWallTime)
	defer cancel()
	defer func() {
		if err != nil && errors.Is(context.Cause(ctx), errWallTime) && !errors.Is(err, errWallTime) {
			err = fmt.Errorf("%w after %s: %w", errWallTime, wall, err)
		}
	}()

	if err := j.advance(ctx, research.RunCollecting, "Verifying pinned targets and loading patterns"); err != nil {
		return err
	}
	if err := j.collect(ctx, scope); err != nil {
		return err
	}

	if err := j.advance(ctx, research.RunIndexing, "Indexing source snapshots"); err != nil {
		return err
	}
	for _, t := range scope.Targets {
		if err := j.index(ctx, t); err != nil {
			return err
		}
	}

	if err := j.advance(ctx, research.RunSurfaceMapping, "Matching patterns to source regions"); err != nil {
		return err
	}
	cands, err := j.mapSurface(ctx, scope)
	if err != nil {
		return err
	}

	if err := j.advance(ctx, research.RunHypothesisGeneration,
		fmt.Sprintf("Generating hypotheses from %d candidate regions", len(cands))); err != nil {
		return err
	}
	if err := j.generate(ctx, scope, cands); err != nil {
		return err
	}

	if err := j.advance(ctx, research.RunStaticValidation, "Running static analyzers"); err != nil {
		return err
	}
	if err := j.static(ctx, scope); err != nil {
		return err
	}

	return j.advance(ctx, research.RunReviewRequired, "Automatic analysis finished; hypotheses await review")
}

func (j *job) snapshotDir(t store.WorkTarget) (string, error) {
	switch t.Kind {
	case "local_snapshot", "lab_fixture":
		if err := source.ValidateSnapshotKey(t.SnapshotKey); err != nil {
			return "", err
		}
		return filepath.Join(j.o.cfg.SnapshotDir(), t.SnapshotKey), nil
	}
	return "", fmt.Errorf("target %d: %s targets have no local snapshot to read", t.ID, t.Kind)
}

// collect re-verifies every pinned revision: the snapshot on disk must still
// hash to what the run was pinned to.
func (j *job) collect(ctx context.Context, scope store.WorkScope) error {
	for _, t := range scope.Targets {
		if _, err := j.snapshotDir(t); err != nil {
			return err
		}
		rev, _, err := source.LocalSnapshot(j.o.cfg.SnapshotDir(), t.SnapshotKey)
		if err != nil {
			return fmt.Errorf("target %d: %w", t.ID, err)
		}
		if rev != t.Revision {
			return fmt.Errorf("target %d changed on disk since the run was pinned (pinned %s, now %s)", t.ID, t.Revision, rev)
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
func (j *job) mapSurface(ctx context.Context, scope store.WorkScope) ([]candidate, error) {
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
		return nil, nil
	}

	var perPattern [][]candidate
	for _, t := range scope.Targets {
		dir, err := j.snapshotDir(t)
		if err != nil {
			return nil, err
		}
		tops := make([]topK, len(patterns))
		for i := range tops {
			tops[i].k = candidatesPerPattern
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
			return nil, stageErr(ctx, err)
		}
		for _, top := range tops {
			perPattern = append(perPattern, top.items)
		}
	}

	cands := selectCandidates(perPattern)
	for i := range cands {
		c := &cands[i]
		summary := fmt.Sprintf("Region matches signature(s) %s of pattern %d", strings.Join(c.Matched, ", "), c.Pattern.ID)
		if len(c.Countered) > 0 {
			summary += fmt.Sprintf("; also matches counter-signature(s) %s", strings.Join(c.Countered, ", "))
		}
		id, err := j.o.store.InsertEvidence(ctx, j.runID, store.EvidenceInput{
			TargetID: c.TargetID, Kind: "source_location", FilePath: c.Chunk.Path,
			LineStart: c.Chunk.Start, LineEnd: c.Chunk.End, Symbol: c.Chunk.Symbol, Revision: c.Revision,
			ContentSHA256: c.SHA256, Summary: summary, Excerpt: c.Excerpt,
			Metadata: map[string]any{"pattern_id": c.Pattern.ID, "score": c.Score, "matched": c.Matched,
				"countered": c.Countered, "language": c.Chunk.Language},
		})
		if err != nil {
			return nil, err
		}
		c.evidenceID = id
	}
	j.event(ctx, "stage.mapped", fmt.Sprintf("%d candidate region(s) selected from %d pattern(s)", len(cands), len(patterns)),
		map[string]any{"candidates": len(cands)})
	return cands, nil
}

// agent is a model client bound to one role.
type agent struct {
	role   string
	client llm.Client
}

func (j *job) agentFor(ctx context.Context, role string, runProfile *int64, cache map[int64]llm.Client) (*agent, error) {
	enabled, override, err := j.o.store.AgentProfile(ctx, role)
	if err != nil || !enabled {
		return nil, err
	}
	profileID := runProfile
	if override != nil {
		profileID = override
	}
	if profileID == nil {
		return nil, fmt.Errorf("%s: the run has no LLM profile", role)
	}
	if c, ok := cache[*profileID]; ok {
		return &agent{role, c}, nil
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
	return &agent{role, c}, nil
}

var errBudget = errors.New("token budget exhausted")

// call makes one budgeted model call and records token usage.
func (j *job) call(ctx context.Context, a *agent, b *budget, system string, env envelope, schema map[string]any) ([]byte, error) {
	grant := b.reserve(maxCallTokens)
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

// generate runs Researcher then Skeptic for each candidate, MaxWorkers at a
// time. Bad model output and refusals skip a candidate; the run fails only if
// every model call errored.
func (j *job) generate(ctx context.Context, scope store.WorkScope, cands []candidate) error {
	if len(cands) == 0 {
		return nil
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

	b := newBudget(int64(scope.Limits.MaxTokens))
	var stats genStats
	work := make(chan candidate)
	var wg sync.WaitGroup
	for range scope.Limits.MaxWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range work {
				j.investigate(ctx, researcher, skeptic, b, c, &stats)
			}
		}()
	}
feed:
	for _, c := range cands {
		select {
		case work <- c:
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

func (j *job) investigate(ctx context.Context, researcher, skeptic *agent, b *budget, c candidate, stats *genStats) {
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
	raw, err := j.call(ctx, researcher, b, researcherSystem, env, researcherSchema)
	if j.callFailed(ctx, err, c, "researcher", stats) {
		return
	}
	out, err := parseResearcher(raw, allowed)
	if err != nil {
		stats.add(func(s *genStats) { s.invalid++ })
		j.event(ctx, "agent.invalid_output", fmt.Sprintf("Researcher answer for evidence %d rejected: %v", c.evidenceID, err),
			map[string]any{"evidence_id": c.evidenceID})
		return
	}
	if !out.Found {
		stats.add(func(s *genStats) { s.declined++ })
		return
	}

	patternID := c.Pattern.ID
	dedupe := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%s|%s|%d", c.TargetID, patternID, c.Chunk.Path, c.Chunk.Symbol, c.Chunk.Start)))
	hypID, err := j.o.store.InsertHypothesis(ctx, j.runID, store.HypothesisInput{
		TargetID: c.TargetID, PatternID: &patternID, Title: out.Title, Claim: out.Claim,
		VulnerabilityClass: out.VulnerabilityClass, CWEID: out.CWEID, Facts: out.Facts,
		Preconditions: out.Preconditions, Unknowns: out.Unknowns, AffectedSymbols: out.AffectedSymbols,
		ExpectedObservation: out.ExpectedObservation, CounterEvidence: out.CounterEvidenceToCheck,
		TriageConfidence: out.TriageConfidence, DedupeKey: hex.EncodeToString(dedupe[:]),
	}, out.SupportingEvidenceIDs)
	switch {
	case errors.Is(err, store.ErrDuplicate):
		stats.add(func(s *genStats) { s.duplicate++ })
		return
	case err != nil:
		stats.add(func(s *genStats) { s.invalid++ })
		j.event(ctx, "agent.invalid_output", fmt.Sprintf("Hypothesis for evidence %d not stored: %v", c.evidenceID, err), nil)
		return
	}
	stats.add(func(s *genStats) { s.created++ })

	if skeptic == nil {
		j.toTriage(ctx, hypID)
		return
	}
	env.TaskID = fmt.Sprintf("run-%d/hypothesis-%d", j.runID, hypID)
	env.Hypothesis = map[string]any{
		"hypothesis_id": hypID, "title": out.Title, "claim": out.Claim, "facts": out.Facts,
		"preconditions": out.Preconditions, "unknowns": out.Unknowns, "affected_symbols": out.AffectedSymbols,
		"expected_observation": out.ExpectedObservation, "counter_evidence_to_check": out.CounterEvidenceToCheck,
		"triage_confidence": out.TriageConfidence,
	}
	env.Instructions = []string{"Challenge this hypothesis using only the cited excerpt. Check counter_evidence_to_check first."}
	raw, err = j.call(ctx, skeptic, b, skepticSystem, env, skepticSchema)
	if j.callFailed(ctx, err, c, "skeptic", stats) {
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
	conf := finalConfidence(out.TriageConfidence, sk.Verdict, sk.AdjustedConfidence)
	if err := j.o.store.RecordSkeptic(ctx, j.runID, hypID, sk.Verdict, conf, counter, counterEv); err != nil && ctx.Err() == nil {
		j.log.Error("record skeptic", "hypothesis", hypID, "err", err)
	}
}

// callFailed handles a model call error and reports whether the caller
// should stop working on the candidate.
func (j *job) callFailed(ctx context.Context, err error, c candidate, role string, stats *genStats) bool {
	var refusal *llm.RefusalError
	switch {
	case err == nil:
		return false
	case ctx.Err() != nil:
		return true
	case errors.Is(err, errBudget):
		stats.add(func(s *genStats) { s.budgetHit = true })
	case errors.As(err, &refusal):
		stats.add(func(s *genStats) { s.declined++ })
		j.event(ctx, "agent.refused", fmt.Sprintf("Model declined the %s task for evidence %d (%s)", role, c.evidenceID, refusal.Category),
			map[string]any{"evidence_id": c.evidenceID, "category": refusal.Category})
	default:
		stats.add(func(s *genStats) {
			s.err++
			if s.firstErr == nil {
				s.firstErr = err
			}
		})
		j.event(ctx, "agent.error", fmt.Sprintf("%s call for evidence %d failed: %v", role, c.evidenceID, err),
			map[string]any{"evidence_id": c.evidenceID})
	}
	return true
}

func (j *job) toTriage(ctx context.Context, hypID int64) {
	if err := j.o.store.SetHypothesisStatus(ctx, hypID, research.HypothesisDraft, research.HypothesisTriage); err != nil && ctx.Err() == nil {
		j.log.Error("move hypothesis to triage", "hypothesis", hypID, "err", err)
	}
}
