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

// historyRepo builds a repository whose history mixes a security fix, a
// fix that touches only documentation, and ordinary commits.
func historyRepo(t *testing.T) (dir, fix string) {
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
	write("src/parse.c", "void parse(char *dst, const char *src, int len) {\n\tmemcpy(dst, src, len);\n}\n")
	write("README", "parser\n")
	run("add", ".")
	run("commit", "-q", "-m", "Initial import")
	write("src/parse.c", "void parse(char *dst, const char *src, int len) {\n\tif (len > 16)\n\t\treturn;\n\tmemcpy(dst, src, len);\n}\n")
	run("commit", "-q", "-am", "parse: fix heap buffer overflow on long names\n\nFound by OSS-Fuzz. CVE-2024-12345.")
	fix = run("rev-parse", "HEAD")
	write("README", "parser, now safe from overflow\n")
	run("commit", "-q", "-am", "docs: mention the overflow fix")
	write("src/parse.c", "void parse(char *dst, const char *src, int len) {\n\tif (len > 16)\n\t\treturn;\n\tmemcpy(dst, src, len); /* bounded */\n}\n")
	run("commit", "-q", "-am", "style: comment")
	return dir, fix
}

func TestFetchHistory(t *testing.T) {
	repo, fix := historyRepo(t)
	f := fetcher(t, repo)
	r, err := f.FetchHistory(context.Background(), "https://git.example.test/org/repo", "", "hist", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.History) != 1 {
		t.Fatalf("history = %+v, want only the security fix", r.History)
	}
	c := r.History[0]
	if c.SHA != fix || !slices.Contains(c.Signals, "cve") || !slices.Contains(c.Signals, "memory-safety") || !slices.Contains(c.Signals, "sanitizer") {
		t.Errorf("commit %s signals %v", c.SHA, c.Signals)
	}
	if !slices.Equal(c.Files, []string{"src/parse.c"}) || !strings.Contains(c.Diff, "+\tif (len > 16)") || !strings.Contains(c.Body, "OSS-Fuzz") {
		t.Errorf("files %v diff %q", c.Files, c.Diff)
	}
	if _, err := os.Stat(filepath.Join(f.SnapshotDir, "hist", ".git")); err == nil {
		t.Error(".git left in the snapshot")
	}

	// Without history nothing is read, and a shallow boundary is never
	// mistaken for a fix.
	r, err = f.FetchHistory(context.Background(), "https://git.example.test/org/repo", "", "nohist", 0)
	if err != nil || r.History != nil {
		t.Fatalf("no history: %v %v", r.History, err)
	}
}

func TestClassify(t *testing.T) {
	for msg, keep := range map[string]bool{
		"Fix use-after-free in session teardown":   true,
		"security: reject oversized headers":       true,
		"Bump version to 1.2.3":                    false,
		"fix typo":                                 false,
		"CVE-2023-4863: check huffman table sizes": true,
		"Add tests":                                false,
	} {
		_, score := classify(msg)
		if (score >= minScore) != keep {
			t.Errorf("%q: score %d", msg, score)
		}
	}
	if got := clip("aé", 2); got != "a\n[truncated]" {
		t.Errorf("clip split a rune: %q", got)
	}
}
