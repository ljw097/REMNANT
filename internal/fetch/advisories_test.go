package fetch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// silentFixRepo has a security fix whose message mentions nothing a heuristic
// would flag, so only an external advisory can surface it.
func silentFixRepo(t *testing.T) (dir, fix string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir = t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, body string) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q", "-b", "main")
	write("src/auth.c", "int check(const char *p) { return strcmp(p, secret); }\n")
	run("add", ".")
	run("commit", "-q", "-m", "Initial import")
	// A real fix, but the message gives no hint a heuristic would catch.
	write("src/auth.c", "int check(const char *p) { return constant_time_eq(p, secret); }\n")
	run("commit", "-q", "-am", "auth: tidy up comparison helper")
	fix = run("rev-parse", "HEAD")
	return dir, fix
}

func TestFetchHistoryAdvisoryEnrichment(t *testing.T) {
	repo, fix := silentFixRepo(t)
	f := fetcher(t, repo)
	var gotRepo, gotWork string
	f.AdvisoryLookup = func(_ context.Context, repoURL, work string, _ func(...string) (string, error)) (map[string]AdvisoryNote, error) {
		gotRepo, gotWork = repoURL, work
		return map[string]AdvisoryNote{
			fix:        {IDs: []string{"CVE-2024-99999"}, Summary: "timing side channel in credential check"},
			"deadbeef": {IDs: []string{"CVE-2000-0001"}}, // not in the clone: ignored
		}, nil
	}
	r, err := f.FetchHistory(context.Background(), "https://git.example.test/org/repo", "", "adv", 50)
	if err != nil {
		t.Fatal(err)
	}
	if gotRepo == "" || gotWork == "" {
		t.Fatal("AdvisoryLookup was not called with the repo and work dir")
	}
	// The silent fix is now a candidate purely because the advisory named it.
	var c *Commit
	for i := range r.History {
		if r.History[i].SHA == fix {
			c = &r.History[i]
		}
	}
	if c == nil {
		t.Fatalf("advisory fix not in history: %+v", r.History)
	}
	if !slices.Contains(c.Signals, "advisory") || !slices.Contains(c.Signals, "CVE-2024-99999") {
		t.Errorf("signals = %v", c.Signals)
	}
	if !strings.Contains(c.Body, "CVE-2024-99999") || !strings.Contains(c.Body, "timing side channel") {
		t.Errorf("body = %q", c.Body)
	}
	if !slices.Contains(c.Files, "src/auth.c") || !strings.Contains(c.Diff, "constant_time_eq") {
		t.Errorf("files %v diff %q", c.Files, c.Diff)
	}
}

func TestFetchHistoryAdvisoryAnnotatesExisting(t *testing.T) {
	repo, fix := historyRepo(t) // its fix already scores on the message
	f := fetcher(t, repo)
	f.AdvisoryLookup = func(context.Context, string, string, func(...string) (string, error)) (map[string]AdvisoryNote, error) {
		return map[string]AdvisoryNote{fix: {IDs: []string{"GHSA-xxxx-yyyy-zzzz"}, Summary: "overflow"}}, nil
	}
	r, err := f.FetchHistory(context.Background(), "https://git.example.test/org/repo", "", "adv2", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.History) != 1 {
		t.Fatalf("history = %+v", r.History)
	}
	c := r.History[0]
	// The message-detected candidate keeps its signals and gains the advisory.
	if !slices.Contains(c.Signals, "cve") || !slices.Contains(c.Signals, "advisory") || !slices.Contains(c.Signals, "GHSA-xxxx-yyyy-zzzz") {
		t.Errorf("signals = %v", c.Signals)
	}
	if !strings.Contains(c.Body, "GHSA-xxxx-yyyy-zzzz") {
		t.Errorf("body = %q", c.Body)
	}
}
