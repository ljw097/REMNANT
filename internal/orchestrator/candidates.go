package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/ljw097/remnant/internal/sourceindex"
	"github.com/ljw097/remnant/internal/store"
)

const (
	candidatesPerPattern = 3  // per pattern and target
	maxCandidates        = 24 // per run
	maxExcerptBytes      = 12 << 10
)

// signature is a pattern code signature compiled to a matcher. Identifiers
// match on word boundaries; anything else matches as a literal substring.
type signature struct {
	raw string
	re  *regexp.Regexp
}

var identRe = regexp.MustCompile(`^[A-Za-z_]\w*$`)

func compileSignatures(list []string) []signature {
	var out []signature
	for _, raw := range list {
		raw = strings.TrimSpace(raw)
		if raw == "" || len(raw) > 200 || len(out) == 50 {
			continue
		}
		expr := regexp.QuoteMeta(raw)
		if identRe.MatchString(raw) {
			expr = `\b` + expr + `\b`
		}
		out = append(out, signature{raw, regexp.MustCompile(expr)})
	}
	return out
}

// score rates a sourceindex.Chunk against a pattern: +3 per matched signature, −2 per
// matched counter-signature. Only chunks with a signature hit and a positive
// score qualify.
func score(text string, sigs, counters []signature) (int, []string, []string, bool) {
	var matched, countered []string
	for _, s := range sigs {
		if s.re.MatchString(text) {
			matched = append(matched, s.raw)
		}
	}
	if len(matched) == 0 {
		return 0, nil, nil, false
	}
	for _, s := range counters {
		if s.re.MatchString(text) {
			countered = append(countered, s.raw)
		}
	}
	sc := 3*len(matched) - 2*len(countered)
	return sc, matched, countered, sc > 0
}

type candidate struct {
	TargetID  int64
	Revision  string
	Pattern   store.Pattern
	Chunk     sourceindex.Chunk
	Score     int
	Matched   []string
	Countered []string
	Excerpt   string // line-numbered
	SHA256    string // of the raw sourceindex.Chunk text

	evidenceID int64 // set once recorded as run-level evidence
}

func (c candidate) less(o candidate) bool {
	if c.Score != o.Score {
		return c.Score > o.Score
	}
	if c.Chunk.Path != o.Chunk.Path {
		return c.Chunk.Path < o.Chunk.Path
	}
	return c.Chunk.Start < o.Chunk.Start
}

// topK keeps the k best candidates.
type topK struct {
	k     int
	items []candidate
}

func (t *topK) add(c candidate) {
	i := sort.Search(len(t.items), func(i int) bool { return c.less(t.items[i]) })
	if i >= t.k {
		return
	}
	t.items = append(t.items, candidate{})
	copy(t.items[i+1:], t.items[i:])
	t.items[i] = c
	if len(t.items) > t.k {
		t.items = t.items[:t.k]
	}
}

// numbered renders lines[start-1:end] with line numbers, cut at
// maxExcerptBytes, and returns it with the SHA-256 of the raw text.
func numbered(lines []string, start, end int) (string, string) {
	raw := strings.Join(lines[start-1:end], "\n")
	sum := sha256.Sum256([]byte(raw))
	var b strings.Builder
	for i := start; i <= end; i++ {
		line := fmt.Sprintf("%6d | %s\n", i, lines[i-1])
		if b.Len()+len(line) > maxExcerptBytes {
			fmt.Fprintf(&b, "%6s | … excerpt truncated at line %d of %d\n", "", i, end)
			break
		}
		b.WriteString(line)
	}
	return b.String(), hex.EncodeToString(sum[:])
}

// selectCandidates keeps the best candidates overall, at most
// maxCandidates, preserving the per-pattern selection.
func selectCandidates(perPattern [][]candidate) []candidate {
	var all []candidate
	for _, cs := range perPattern {
		all = append(all, cs...)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].less(all[j]) })
	if len(all) > maxCandidates {
		all = all[:maxCandidates]
	}
	return all
}
