package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ljw097/remnant/internal/artifact"
	"github.com/ljw097/remnant/internal/research"
	"github.com/ljw097/remnant/internal/store"
)

const reportLimitations = "Hypotheses are model-generated from heuristic source matching and are not findings until validated. " +
	"A crash or sanitizer report is an observation, not proof of exploitability. " +
	"Regions that matched no pattern signature were not examined."

// report renders the run report as JSON and Markdown artifacts and completes
// the run.
func (j *job) report(ctx context.Context) error {
	rep, err := j.o.store.RunReport(ctx, j.runID)
	if err != nil {
		return err
	}
	generated := time.Now().UTC()
	body, err := json.MarshalIndent(map[string]any{
		"report_version": 1, "generated_at": generated, "limitations": reportLimitations, "report": rep,
	}, "", "  ")
	if err != nil {
		return err
	}
	jsonArt, err := j.writeArtifact(ctx, "report", fmt.Sprintf("remnant-run-%d.json", j.runID), "application/json", body)
	if err != nil {
		return err
	}
	mdArt, err := j.writeArtifact(ctx, "report", fmt.Sprintf("remnant-run-%d.md", j.runID), "text/markdown; charset=utf-8",
		[]byte(renderMarkdown(rep, generated)))
	if err != nil {
		return err
	}
	j.event(ctx, "report.generated", "Report generated", map[string]any{"artifact_ids": []int64{jsonArt.ID, mdArt.ID}})
	return j.advance(ctx, research.RunCompleted, "Run completed")
}

// writeArtifact stores data content-addressed under the artifact directory
// and registers it.
func (j *job) writeArtifact(ctx context.Context, kind, name, mediaType string, data []byte) (store.Artifact, error) {
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	key := artifact.Key(j.runID, digest, name)
	if err := artifact.Write(j.o.cfg.ArtifactDir(), key, data); err != nil {
		return store.Artifact{}, err
	}
	return j.o.store.InsertArtifact(ctx, j.runID, store.ArtifactInput{
		Kind: kind, Name: name, StorageKey: key, SHA256: digest, SizeBytes: int64(len(data)), MediaType: mediaType,
	})
}

var hypothesisOrder = map[string]int{
	"validated": 0, "validation_pending": 1, "accepted": 2, "triage": 3, "draft": 4,
	"not_reproducible": 5, "duplicate": 6, "rejected": 7,
}

func renderMarkdown(r store.RunReport, generated time.Time) string {
	var b strings.Builder
	p := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	p("# REMNANT report — run %d\n\n", r.Run.ID)
	p("Generated %s. Project %d. Scope hash `%s`.\n\n", generated.Format(time.RFC3339), r.Run.ProjectID, r.Run.ScopeHash)
	p("> %s\n\n", reportLimitations)

	p("## Scope\n\n| Target | Kind | Revision |\n|---|---|---|\n")
	for _, t := range r.Targets {
		p("| %d (%s) | %s | `%s` |\n", t.ID, cell(t.SnapshotKey), t.Kind, t.Revision)
	}
	p("\nPatterns: ")
	for i, pat := range r.Patterns {
		if i > 0 {
			p(", ")
		}
		p("%d (%s, %s)", pat.ID, inline(pat.Title), pat.ValidationStatus)
	}
	if len(r.Patterns) == 0 {
		p("none")
	}
	p("\n\nModel usage: %d input / %d output tokens.\n\n", r.Run.InputTokens, r.Run.OutputTokens)

	counts := map[string]int{}
	for _, h := range r.Hypotheses {
		counts[h.Status]++
	}
	p("## Summary\n\n| Status | Hypotheses |\n|---|---|\n")
	for _, s := range []string{"validated", "validation_pending", "accepted", "triage", "draft", "not_reproducible", "duplicate", "rejected"} {
		if counts[s] > 0 {
			p("| %s | %d |\n", s, counts[s])
		}
	}
	if len(r.Hypotheses) == 0 {
		p("| — | 0 |\n")
	}

	hs := append([]store.HypothesisDetail(nil), r.Hypotheses...)
	slices.SortStableFunc(hs, func(a, b store.HypothesisDetail) int { return hypothesisOrder[a.Status] - hypothesisOrder[b.Status] })
	for _, h := range hs {
		p("\n## H%d — %s\n\n", h.ID, inline(h.Title))
		p("Status **%s** · triage priority **%s**", h.Status, h.TriageConfidence)
		if h.SkepticVerdict != "" {
			p(" · skeptic **%s**", h.SkepticVerdict)
		}
		p(" · class %s", inline(h.VulnerabilityClass))
		if h.CWEID != "" {
			p(" · %s", h.CWEID)
		}
		p(" · target %d\n\n%s\n\n", h.TargetID, inline(h.Claim))
		list(&b, "Facts", h.Facts)
		list(&b, "Preconditions", h.Preconditions)
		list(&b, "Unknowns", h.Unknowns)
		list(&b, "Affected symbols", h.AffectedSymbols)
		if h.ExpectedObservation != "" {
			p("**Expected observation:** %s\n\n", inline(h.ExpectedObservation))
		}
		list(&b, "Counter-evidence", h.CounterEvidence)
		if h.ReviewerNote != "" {
			p("**Reviewer note:** %s\n\n", inline(h.ReviewerNote))
		}
		for _, e := range h.Evidence {
			loc := e.FilePath
			if e.LineStart != nil {
				loc += fmt.Sprintf(":%d", *e.LineStart)
				if e.LineEnd != nil && *e.LineEnd != *e.LineStart {
					loc += fmt.Sprintf("-%d", *e.LineEnd)
				}
			}
			p("- E%d `%s` %s", e.ID, e.EvidenceKind, inline(e.Summary))
			if loc != "" {
				p(" — `%s`", loc)
			}
			if e.Symbol != "" {
				p(" in `%s`", e.Symbol)
			}
			if e.ContentSHA256 != "" {
				p(" (sha256 %s…)", e.ContentSHA256[:12])
			}
			p("\n")
			if e.Excerpt != "" {
				fence := strings.Repeat("`", max(3, longestRun(e.Excerpt, '`')+1))
				p("\n%s\n%s%s\n\n", fence, e.Excerpt, fence)
			}
		}
		for _, x := range r.Experiments {
			if x.HypothesisID != nil && *x.HypothesisID == h.ID {
				p("- Experiment %d `%s`: **%s** — %s\n", x.ID, x.Operation, x.Status, inline(x.ResultSummary))
			}
		}
	}

	var static []store.Experiment
	for _, x := range r.Experiments {
		if x.ApprovalID == nil {
			static = append(static, x)
		}
	}
	if len(static) > 0 {
		p("\n## Static analysis\n\n| Experiment | Operation | Target | Status | Summary |\n|---|---|---|---|---|\n")
		for _, x := range static {
			target := "—"
			if x.TargetID != nil {
				target = fmt.Sprint(*x.TargetID)
			}
			p("| %d | %s | %s | %s | %s |\n", x.ID, x.Operation, target, x.Status, cell(x.ResultSummary))
		}
	}
	if len(r.Approvals) > 0 {
		p("\n## Lab approvals\n\n| Approval | Hypothesis | Decision | Requested by | Decided by |\n|---|---|---|---|---|\n")
		for _, a := range r.Approvals {
			decided := "—"
			if a.DecidedBy != nil {
				decided = fmt.Sprint(*a.DecidedBy)
			}
			p("| %d | H%d | %s | %d | %s |\n", a.ID, a.HypothesisID, a.Decision, a.RequestedBy, decided)
		}
	}
	return b.String()
}

func list(b *strings.Builder, title string, raw json.RawMessage) {
	var items []string
	if json.Unmarshal(raw, &items) != nil || len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "**%s**\n\n", title)
	for _, it := range items {
		fmt.Fprintf(b, "- %s\n", inline(it))
	}
	b.WriteString("\n")
}

var spaceRun = regexp.MustCompile(`\s+`)

// inline flattens model or user text onto one line so it cannot open
// headings, lists or fences.
func inline(s string) string { return strings.TrimSpace(spaceRun.ReplaceAllString(s, " ")) }

func cell(s string) string { return strings.ReplaceAll(inline(s), "|", `\|`) }

func longestRun(s string, c byte) int {
	best, cur := 0, 0
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			cur++
			best = max(best, cur)
		} else {
			cur = 0
		}
	}
	return best
}
