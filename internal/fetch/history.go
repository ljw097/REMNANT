package fetch

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ljw097/remnant/internal/sourceindex"
)

// Commit is a commit from a fetched repository's history that looks like a
// fix. Every field is untrusted repository text.
type Commit struct {
	SHA         string
	CommittedAt time.Time
	Subject     string
	Body        string
	Files       []string
	Diff        string
	Signals     []string
	Score       int
}

const (
	// maxHistoryCommits is how many fix candidates keep their diff.
	maxHistoryCommits = 60
	maxDiffBytes      = 16 << 10
	maxDiffFiles      = 20
	maxBodyBytes      = 2 << 10
)

// signal is a hint in a commit message that it fixes a vulnerability.
type signal struct {
	name   string
	re     *regexp.Regexp
	weight int
}

var signals = []signal{
	{"cve", regexp.MustCompile(`\bCVE-\d{4}-\d{4,7}\b`), 10},
	{"cwe", regexp.MustCompile(`\bCWE-\d{1,5}\b`), 6},
	{"security", regexp.MustCompile(`(?i)\b(security|vulnerab\w*|exploit\w*|advisory|GHSA-[\w-]+)\b`), 6},
	{"memory-safety", regexp.MustCompile(`(?i)\b(buffer overflow|heap overflow|stack overflow|out[- ]of[- ]bounds|\bOOB\b|use[- ]after[- ]free|\bUAF\b|double[- ]free|overread|overrun|underflow|integer overflow|wrap[- ]?around)\b`), 5},
	{"sanitizer", regexp.MustCompile(`(?i)\b(ASan|UBSan|MSan|KASAN|AddressSanitizer|sanitizer|oss-fuzz|syzbot|fuzz\w*|clusterfuzz)\b`), 4},
	{"null-deref", regexp.MustCompile(`(?i)\b(null (pointer )?deref\w*|NULL pointer|segfault|crash\w*)\b`), 3},
	{"injection", regexp.MustCompile(`(?i)\b(injection|XSS|CSRF|SSRF|traversal|deserializ\w*|RCE|privilege|auth(entication|orization)? bypass)\b`), 5},
	{"bounds", regexp.MustCompile(`(?i)\b(bounds? check\w*|length check\w*|validate \w+ (length|size)|missing check|unchecked)\b`), 3},
	{"race", regexp.MustCompile(`(?i)\b(race condition|data race|TOCTOU|deadlock)\b`), 3},
	{"leak", regexp.MustCompile(`(?i)\b(memory leak|info(rmation)? leak|uninitiali[sz]ed)\b`), 2},
}

var fixWord = regexp.MustCompile(`(?i)\b(fix|fixes|fixed|prevent|avoid|harden)\b`)

// minScore keeps commits with at least one strong hint or two weak ones.
const minScore = 4

// classify scores a commit message.
func classify(msg string) (names []string, score int) {
	for _, s := range signals {
		if s.re.MatchString(msg) {
			names = append(names, s.name)
			score += s.weight
		}
	}
	if fixWord.MatchString(msg) {
		score++
	}
	return names, score
}

// readHistory lists up to depth commits before HEAD, keeps those whose
// message looks like a fix, and reads their source diffs. Commits at the
// shallow boundary are skipped: without a parent their diff is the whole
// tree.
func readHistory(ctx context.Context, git func(...string) (string, error), work string, depth int) ([]Commit, error) {
	shallow := map[string]bool{}
	if b, err := os.ReadFile(filepath.Join(work, ".git", "shallow")); err == nil {
		for _, l := range strings.Fields(string(b)) {
			shallow[l] = true
		}
	}
	out, err := git("-C", work, "log", "--no-color", "--no-show-signature", "-n", strconv.Itoa(depth),
		"--format=%H%x1f%cI%x1f%s%x1f%b%x1e", "HEAD")
	if err != nil {
		return nil, err
	}
	var cands []Commit
	for _, rec := range strings.Split(out, "\x1e") {
		f := strings.SplitN(strings.TrimLeft(rec, "\n"), "\x1f", 4)
		if len(f) != 4 || shallow[f[0]] {
			continue
		}
		names, score := classify(f[2] + "\n" + f[3])
		if score < minScore {
			continue
		}
		at, _ := time.Parse(time.RFC3339, f[1])
		cands = append(cands, Commit{SHA: f[0], CommittedAt: at, Subject: clip(f[2], 300), Body: clip(strings.TrimSpace(f[3]), maxBodyBytes), Signals: names, Score: score})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Score != cands[j].Score {
			return cands[i].Score > cands[j].Score
		}
		return cands[i].CommittedAt.After(cands[j].CommittedAt)
	})
	if len(cands) > maxHistoryCommits {
		cands = cands[:maxHistoryCommits]
	}
	var kept []Commit
	for _, c := range cands {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		names, err := git("-C", work, "show", "--no-color", "--format=", "--name-only", "--diff-filter=M", "--no-renames", c.SHA)
		if err != nil {
			continue
		}
		for _, n := range strings.Split(names, "\n") {
			if n != "" && sourceindex.LanguageOf(n) != "" && len(c.Files) < maxDiffFiles {
				c.Files = append(c.Files, n)
			}
		}
		if len(c.Files) == 0 {
			continue // a fix that touches no source we read, e.g. docs or build files
		}
		args := append([]string{"-C", work, "show", "--no-color", "--no-ext-diff", "--no-textconv", "--format=", "--unified=3", "--no-renames", c.SHA, "--"}, c.Files...)
		diff, err := git(args...)
		if err != nil {
			continue
		}
		c.Diff = clip(diff, maxDiffBytes)
		kept = append(kept, c)
	}
	return kept, nil
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && (s[cut]&0xC0) == 0x80 { // do not split a UTF-8 sequence
		cut--
	}
	return s[:cut] + "\n[truncated]"
}
