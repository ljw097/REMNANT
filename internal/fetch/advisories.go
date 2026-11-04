package fetch

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/ljw097/remnant/internal/sourceindex"
)

// AdvisoryNote is what an external vulnerability feed records about one fixing
// commit: the advisory ids it fixes (CVE/GHSA) and a one-line summary.
type AdvisoryNote struct {
	IDs     []string
	Summary string
}

// AdvisoryLookup, when set on a Fetcher, is called during FetchHistory while
// the clone is still present. It returns the commits (by SHA) that external
// advisories record as fixes for the repository, so a "silent" security fix
// whose message mentions no CVE is still mined. It must not fail the fetch;
// an error only means no enrichment.
type AdvisoryLookup func(ctx context.Context, repoURL, work string, git func(...string) (string, error)) (map[string]AdvisoryNote, error)

// mergeAdvisories folds advisory-recorded fixes into the history: it annotates
// a candidate already present and adds any fix commit that is in the clone but
// was not flagged by the message heuristic. Advisory-added commits sort to the
// front (score bonus) so the Historian reads known CVE patches first.
func mergeAdvisories(ctx context.Context, git func(...string) (string, error), work string, history []Commit, notes map[string]AdvisoryNote) []Commit {
	if len(notes) == 0 {
		return history
	}
	byID := make(map[string]int, len(history))
	for i, c := range history {
		byID[strings.ToLower(c.SHA)] = i
	}
	for sha, note := range notes {
		if ctx.Err() != nil {
			return history
		}
		sha = strings.ToLower(strings.TrimSpace(sha))
		if len(sha) < 7 {
			continue
		}
		if i, ok := byID[sha]; ok {
			history[i].Signals = addAdvisorySignals(history[i].Signals, note.IDs)
			history[i].Body = advisoryBody(note) + history[i].Body
			history[i].Score += 10
			continue
		}
		// Only commits that exist in the fetched clone can be read.
		full, err := git("-C", work, "rev-parse", "--quiet", "--verify", sha+"^{commit}")
		if err != nil || full == "" {
			continue
		}
		c := Commit{SHA: full, Signals: addAdvisorySignals([]string{"advisory"}, note.IDs), Score: 10}
		if iso, err := git("-C", work, "show", "-s", "--format=%cI%x1f%s", full); err == nil {
			if f := strings.SplitN(iso, "\x1f", 2); len(f) == 2 {
				c.CommittedAt, _ = time.Parse(time.RFC3339, f[0])
				c.Subject = clip(f[1], 300)
			}
		}
		c.Body = advisoryBody(note)
		if !readAdvisoryDiff(git, work, &c) {
			continue // the fix touches no source we read
		}
		history = append(history, c)
	}
	sort.SliceStable(history, func(i, j int) bool {
		if history[i].Score != history[j].Score {
			return history[i].Score > history[j].Score
		}
		return history[i].CommittedAt.After(history[j].CommittedAt)
	})
	return history
}

// addAdvisorySignals adds the "advisory" marker and each id, without
// duplicating what is already there.
func addAdvisorySignals(sigs, ids []string) []string {
	has := func(v string) bool {
		for _, s := range sigs {
			if strings.EqualFold(s, v) {
				return true
			}
		}
		return false
	}
	if !has("advisory") {
		sigs = append(sigs, "advisory")
	}
	for _, id := range ids {
		if id != "" && !has(id) {
			sigs = append(sigs, id)
		}
	}
	return sigs
}

func advisoryBody(note AdvisoryNote) string {
	line := "[known advisory: " + strings.Join(note.IDs, ", ")
	if note.Summary != "" {
		line += " — " + note.Summary
	}
	return clip(line, maxBodyBytes) + "]\n\n"
}

// readAdvisoryDiff fills a commit's source Files and Diff the same way the
// history reader does, and reports whether any source file was touched.
func readAdvisoryDiff(git func(...string) (string, error), work string, c *Commit) bool {
	names, err := git("-C", work, "show", "--no-color", "--format=", "--name-only", "--diff-filter=M", "--no-renames", c.SHA)
	if err != nil {
		return false
	}
	for _, n := range strings.Split(names, "\n") {
		if n != "" && sourceindex.LanguageOf(n) != "" && len(c.Files) < maxDiffFiles {
			c.Files = append(c.Files, n)
		}
	}
	if len(c.Files) == 0 {
		return false
	}
	args := append([]string{"-C", work, "show", "--no-color", "--no-ext-diff", "--no-textconv", "--format=", "--unified=3", "--no-renames", c.SHA, "--"}, c.Files...)
	diff, err := git(args...)
	if err != nil {
		return false
	}
	c.Diff = clip(diff, maxDiffBytes)
	return true
}
