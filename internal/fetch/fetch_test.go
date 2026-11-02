package fetch

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ljw097/remnant/internal/source"
)

// upstream creates a repository with two commits on main and a tag on the
// first, and returns its path and both commit SHAs.
func upstream(t *testing.T) (dir, first, second string) {
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
	run("init", "-q", "-b", "main")
	run("config", "uploadpack.allowReachableSHA1InWant", "true")
	write := func(name, body string) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("src/a.c", "int a(void) { return 1; }\n")
	write(".git-hooks-should-not-run", "")
	run("add", ".")
	run("commit", "-q", "-m", "one")
	first = run("rev-parse", "HEAD")
	run("tag", "v1")
	write("src/a.c", "int a(void) { return 2; }\n")
	run("commit", "-q", "-am", "two")
	second = run("rev-parse", "HEAD")
	return dir, first, second
}

func fetcher(t *testing.T, repo string) *Fetcher {
	return &Fetcher{
		SnapshotDir:  t.TempDir(),
		AllowedHosts: []string{"git.example.test"},
		LookupIP: func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		},
		// Route the allowlisted https URL to the local repository.
		GitConfig: []string{"url." + repo + ".insteadOf=https://git.example.test/org/repo", "protocol.file.allow=always"},
	}
}

func TestFetchRefs(t *testing.T) {
	repo, first, second := upstream(t)
	for i, c := range []struct{ ref, want, body string }{
		{"", second, "return 2"},
		{"main", second, "return 2"},
		{"v1", first, "return 1"},
		{first, first, "return 1"},
	} {
		f := fetcher(t, repo)
		key := "repo-" + string(rune('a'+i))
		commit, digest, err := f.Fetch(context.Background(), "https://git.example.test/org/repo", c.ref, key)
		if err != nil {
			t.Fatalf("ref %q: %v", c.ref, err)
		}
		if commit != c.want {
			t.Errorf("ref %q: commit %s, want %s", c.ref, commit, c.want)
		}
		dir := filepath.Join(f.SnapshotDir, key)
		body, _ := os.ReadFile(filepath.Join(dir, "src", "a.c"))
		if !strings.Contains(string(body), c.body) {
			t.Errorf("ref %q: checked out %q", c.ref, body)
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			t.Errorf("ref %q: .git left in the snapshot", c.ref)
		}
		if d, _ := source.TreeDigest(dir); d != digest {
			t.Errorf("ref %q: digest does not match the tree", c.ref)
		}
		if leftovers, _ := filepath.Glob(filepath.Join(f.SnapshotDir, ".fetch-*")); len(leftovers) != 0 {
			t.Errorf("temporary directories left: %v", leftovers)
		}
	}
}

func TestFetchRefusals(t *testing.T) {
	repo, _, _ := upstream(t)
	ctx := context.Background()
	f := fetcher(t, repo)

	for _, ref := range []string{"--upload-pack=touch /tmp/x", "-x", "a..b", "refs/../../x", "main.lock", "a b"} {
		if _, _, err := f.Fetch(ctx, "https://git.example.test/org/repo", ref, "k1"); err == nil {
			t.Errorf("ref %q accepted", ref)
		}
	}
	if _, _, err := f.Fetch(ctx, "https://other.example/org/repo", "", "k2"); err == nil {
		t.Error("host outside the allowlist accepted")
	}
	if _, _, err := f.Fetch(ctx, "file://"+repo, "", "k3"); err == nil {
		t.Error("file URL accepted")
	}

	for _, ip := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "::1", "fd00::1", "0.0.0.0"} {
		g := fetcher(t, repo)
		g.LookupIP = func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr(ip)}, nil
		}
		if _, _, err := g.Fetch(ctx, "https://git.example.test/org/repo", "", "k4"); err == nil {
			t.Errorf("host resolving to %s accepted", ip)
		}
	}

	if _, _, err := f.Fetch(ctx, "https://git.example.test/org/repo", "", "taken"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.Fetch(ctx, "https://git.example.test/org/repo", "", "taken"); !errors.Is(err, ErrKeyInUse) {
		t.Errorf("existing key: %v", err)
	}

	small := fetcher(t, repo)
	small.MaxBytes = 4
	if _, _, err := small.Fetch(ctx, "https://git.example.test/org/repo", "", "big"); err == nil {
		t.Error("tree over MaxBytes accepted")
	}
	if _, err := os.Stat(filepath.Join(small.SnapshotDir, "big")); err == nil {
		t.Error("oversized tree left in place")
	}
}

func TestFetchDoesNotFollowRedirectsToOtherProtocols(t *testing.T) {
	repo, _, _ := upstream(t)
	f := fetcher(t, repo)
	f.GitConfig = f.GitConfig[:1] // without protocol.file.allow, the rewritten file:// URL must be refused
	if _, _, err := f.Fetch(context.Background(), "https://git.example.test/org/repo", "", "k"); err == nil {
		t.Fatal("non-https transport allowed")
	}
}
