package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalRepoURL(t *testing.T) {
	hosts := []string{"github.com"}
	got, err := CanonicalRepoURL("https://GitHub.com/org/repo/", hosts)
	if err != nil || got != "https://github.com/org/repo" {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, bad := range []string{
		"http://github.com/org/repo",
		"https://user:pw@github.com/org/repo",
		"https://github.com:8443/org/repo",
		"https://gitlab.com/org/repo",
		"https://github.com/",
		"https://github.com/org/repo?x=1",
		"https://github.com/org/../../etc",
		"file:///etc/passwd",
		"https://169.254.169.254/latest",
	} {
		if _, err := CanonicalRepoURL(bad, hosts); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, err := CanonicalRepoURL("https://github.com/org/repo", nil); err == nil {
		t.Error("empty allowlist accepted a remote repository")
	}
}

func TestValidateSnapshotKey(t *testing.T) {
	for _, ok := range []string{"libfoo-1.2", "a", "fixture_01"} {
		if err := ValidateSnapshotKey(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "/etc", "../x", "a/b", "A", "a..b", ".hidden"} {
		if ValidateSnapshotKey(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestLocalSnapshotDigest(t *testing.T) {
	root := t.TempDir()
	snap := filepath.Join(root, "demo")
	must(t, os.MkdirAll(filepath.Join(snap, "src"), 0o755))
	must(t, os.MkdirAll(filepath.Join(snap, ".git"), 0o755))
	must(t, os.WriteFile(filepath.Join(snap, "src", "a.c"), []byte("int x;\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(snap, ".git", "HEAD"), []byte("ref"), 0o644))

	rev1, d1, err := LocalSnapshot(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if rev1 != "sha256:"+d1 {
		t.Fatalf("revision %q does not embed digest", rev1)
	}

	must(t, os.WriteFile(filepath.Join(snap, ".git", "HEAD"), []byte("changed"), 0o644))
	if _, d2, _ := LocalSnapshot(root, "demo"); d2 != d1 {
		t.Error(".git contents changed the digest")
	}
	must(t, os.WriteFile(filepath.Join(snap, "src", "a.c"), []byte("int y;\n"), 0o644))
	if _, d3, _ := LocalSnapshot(root, "demo"); d3 == d1 {
		t.Error("source change did not change the digest")
	}

	outside := t.TempDir()
	must(t, os.Symlink(outside, filepath.Join(root, "escape")))
	if _, _, err := LocalSnapshot(root, "escape"); err == nil {
		t.Error("symlinked snapshot accepted")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
