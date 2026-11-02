package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func script(version string) []byte {
	return []byte("#!/bin/sh\necho " + version + "\n")
}

type entry struct {
	name string
	body []byte
	typ  byte
}

func archive(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0o755, Size: int64(len(e.body)), Typeflag: e.typ}
		if e.typ == 0 {
			h.Typeflag = tar.TypeReg
		}
		if h.Typeflag == tar.TypeSymlink {
			h.Size, h.Linkname = 0, "/etc/passwd"
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			tw.Write(e.body)
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func binaries(version string) []entry {
	dir := "remnant_" + version + "_linux_amd64/"
	return []entry{
		{name: dir + "remnantd", body: script(version)},
		{name: dir + "remnant-worker", body: script(version)},
		{name: dir + "LICENSE", body: []byte("MIT")},
	}
}

// fakeGitHub serves one release at /repos/o/r/releases/latest.
type fakeGitHub struct {
	srv        *httptest.Server
	files      map[string][]byte
	prerelease bool
	omit       map[string]bool
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{files: map[string][]byte{}, omit: map[string]bool{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/o/r/releases/latest" {
			var assets []map[string]any
			for name, b := range f.files {
				if !f.omit[name] {
					assets = append(assets, map[string]any{"name": name, "size": len(b), "browser_download_url": f.srv.URL + "/dl/" + name})
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"tag_name": "v-whatever", "html_url": "https://example/rel",
				"prerelease": f.prerelease, "assets": assets})
			return
		}
		if b, ok := f.files[strings.TrimPrefix(r.URL.Path, "/dl/")]; ok {
			w.Write(b)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// publish signs and serves a release whose archive holds entries.
func (f *fakeGitHub) publish(t *testing.T, key ed25519.PrivateKey, version string, entries []entry) {
	f.files = map[string][]byte{}
	name := "remnant_" + version + "_linux_amd64.tar.gz"
	f.files[name] = archive(t, entries)
	sum := sha256.Sum256(f.files[name])
	sums := []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n")
	f.files["SHA256SUMS"] = sums
	f.files["SHA256SUMS.sig"] = []byte(Sign(key, sums))
}

func newKey(t *testing.T) ed25519.PrivateKey {
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func source(f *fakeGitHub, key ed25519.PrivateKey) *Source {
	return &Source{APIBase: f.srv.URL, Repo: "o/r", Keys: []ed25519.PublicKey{key.Public().(ed25519.PublicKey)}, GOOS: "linux", GOARCH: "amd64"}
}

func installDir(t *testing.T, version string) Installer {
	dir := t.TempDir()
	in := Installer{Paths: map[string]string{}}
	for _, b := range Binaries {
		p := filepath.Join(dir, b)
		if err := os.WriteFile(p, script(version), 0o755); err != nil {
			t.Fatal(err)
		}
		in.Paths[b] = p
	}
	return in
}

func runVersion(t *testing.T, bin string) string {
	out, err := exec.Command(bin, "version").Output()
	if err != nil {
		t.Fatalf("%s: %v", bin, err)
	}
	return strings.TrimSpace(string(out))
}

func TestVersionCompare(t *testing.T) {
	order := []string{"v0.9.9", "v1.0.0-rc.1", "v1.0.0-rc.2", "v1.0.0", "v1.0.1", "v1.10.0", "v2.0.0"}
	for i := 0; i+1 < len(order); i++ {
		a, _ := ParseVersion(order[i])
		b, _ := ParseVersion(order[i+1])
		if a.Compare(b) != -1 || b.Compare(a) != 1 || a.Compare(a) != 0 {
			t.Errorf("%s vs %s", order[i], order[i+1])
		}
	}
	for _, bad := range []string{"dev", "1.0.0", "v1.0", "v01.0.0", "v1.0.0+meta", "v1.0.0-"} {
		if _, err := ParseVersion(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestSignatureDomainSeparation(t *testing.T) {
	k := newKey(t)
	pub := []ed25519.PublicKey{k.Public().(ed25519.PublicKey)}
	sums := []byte("abc  x\n")
	if err := Verify(pub, sums, Sign(k, sums)); err != nil {
		t.Fatal(err)
	}
	raw := ed25519.Sign(k, sums) // signed without the context prefix
	if err := Verify(pub, sums, string(rawB64(raw))); err == nil {
		t.Fatal("signature without the release context accepted")
	}
	if err := Verify(pub, append(sums, '\n'), Sign(k, sums)); err == nil {
		t.Fatal("signature over different bytes accepted")
	}
	if _, err := TrustedKeys(); err != nil {
		t.Fatalf("compiled-in keys: %v", err)
	}
}

func rawB64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func TestDownloadAndInstall(t *testing.T) {
	key := newKey(t)
	gh := newFakeGitHub(t)
	gh.publish(t, key, "v0.3.0", binaries("v0.3.0"))
	src := source(gh, key)
	ctx := context.Background()

	rel, err := src.Latest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version.String() != "v0.3.0" {
		t.Fatalf("version %s", rel.Version)
	}
	b, err := src.Download(ctx, rel, filepath.Join(t.TempDir(), "stage"))
	if err != nil {
		t.Fatal(err)
	}
	in := installDir(t, "v0.2.0")
	if err := in.Install(ctx, b, "v0.2.0"); err != nil {
		t.Fatal(err)
	}
	for _, name := range Binaries {
		if got := runVersion(t, in.Paths[name]); got != "v0.3.0" {
			t.Errorf("%s now reports %s", name, got)
		}
		if got := runVersion(t, in.Paths[name]+".old"); got != "v0.2.0" {
			t.Errorf("%s.old reports %s", name, got)
		}
	}

	// Two failed starts are tolerated; the third rolls back.
	for i := 1; i <= MaxStartAttempts; i++ {
		m, rollback, err := in.StartAttempt()
		if err != nil || rollback || m == nil || m.Attempts != i || m.To != "v0.3.0" {
			t.Fatalf("attempt %d: %+v %v %v", i, m, rollback, err)
		}
	}
	if _, rollback, _ := in.StartAttempt(); !rollback {
		t.Fatal("no rollback after repeated failed starts")
	}
	if err := in.Rollback(); err != nil {
		t.Fatal(err)
	}
	if got := runVersion(t, in.Paths["remnantd"]); got != "v0.2.0" {
		t.Fatalf("after rollback: %s", got)
	}
	if m, _, _ := in.StartAttempt(); m != nil {
		t.Fatal("marker survived rollback")
	}
}

func TestLatestRejects(t *testing.T) {
	key, other := newKey(t), newKey(t)
	ctx := context.Background()
	cases := map[string]func(gh *fakeGitHub){
		"signed by another key": func(gh *fakeGitHub) { gh.publish(t, other, "v0.3.0", binaries("v0.3.0")) },
		"prerelease":            func(gh *fakeGitHub) { gh.publish(t, key, "v0.3.0", binaries("v0.3.0")); gh.prerelease = true },
		"no signature": func(gh *fakeGitHub) {
			gh.publish(t, key, "v0.3.0", binaries("v0.3.0"))
			gh.omit["SHA256SUMS.sig"] = true
		},
		"listed archive missing": func(gh *fakeGitHub) {
			gh.publish(t, key, "v0.3.0", binaries("v0.3.0"))
			gh.omit["remnant_v0.3.0_linux_amd64.tar.gz"] = true
		},
		"tampered sums": func(gh *fakeGitHub) {
			gh.publish(t, key, "v0.3.0", binaries("v0.3.0"))
			gh.files["SHA256SUMS"] = append(gh.files["SHA256SUMS"], []byte(strings.Repeat("0", 64)+"  extra\n")...)
		},
	}
	for name, setup := range cases {
		gh := newFakeGitHub(t)
		setup(gh)
		if _, err := source(gh, key).Latest(ctx); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	gh := newFakeGitHub(t)
	gh.publish(t, key, "v0.3.0", binaries("v0.3.0"))
	s := source(gh, key)
	s.GOARCH = "riscv64"
	if _, err := s.Latest(ctx); err == nil {
		t.Error("release without a build for this platform accepted")
	}
}

func TestDownloadRejects(t *testing.T) {
	key := newKey(t)
	ctx := context.Background()
	cases := map[string][]entry{
		"symlinked binary": {{name: "remnantd", typ: tar.TypeSymlink}, {name: "remnant-worker", body: script("v0.3.0")}},
		"missing worker":   {{name: "remnantd", body: script("v0.3.0")}},
		"duplicate binary": {{name: "a/remnantd", body: script("v0.3.0")}, {name: "b/remnantd", body: script("v0.3.0")}, {name: "remnant-worker", body: script("v0.3.0")}},
	}
	for name, entries := range cases {
		gh := newFakeGitHub(t)
		gh.publish(t, key, "v0.3.0", entries)
		src := source(gh, key)
		rel, err := src.Latest(ctx)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := src.Download(ctx, rel, filepath.Join(t.TempDir(), "s")); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	// The archive changes after signing.
	gh := newFakeGitHub(t)
	gh.publish(t, key, "v0.3.0", binaries("v0.3.0"))
	src := source(gh, key)
	rel, _ := src.Latest(ctx)
	gh.files["remnant_v0.3.0_linux_amd64.tar.gz"] = archive(t, binaries("v0.3.0")[:2])
	if _, err := src.Download(ctx, rel, filepath.Join(t.TempDir(), "s")); err == nil {
		t.Error("archive not matching its signed checksum accepted")
	}

	// Paths in the archive never escape the staging directory.
	gh.publish(t, key, "v0.3.0", append(binaries("v0.3.0"), entry{name: "../../escape", body: []byte("x")}))
	rel, _ = src.Latest(ctx)
	stage := filepath.Join(t.TempDir(), "s")
	if _, err := src.Download(ctx, rel, stage); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stage, "..", "..", "escape")); err == nil {
		t.Error("archive entry written outside the staging directory")
	}
}

func TestInstallRefusesBinaryWithWrongVersion(t *testing.T) {
	key := newKey(t)
	gh := newFakeGitHub(t)
	// Signed archive name says v0.3.0, but the binary inside claims v9.9.9.
	gh.publish(t, key, "v0.3.0", []entry{{name: "remnantd", body: script("v9.9.9")}, {name: "remnant-worker", body: script("v0.3.0")}})
	src := source(gh, key)
	ctx := context.Background()
	rel, _ := src.Latest(ctx)
	b, err := src.Download(ctx, rel, filepath.Join(t.TempDir(), "s"))
	if err != nil {
		t.Fatal(err)
	}
	in := installDir(t, "v0.2.0")
	if err := in.Install(ctx, b, "v0.2.0"); err == nil {
		t.Fatal("binary reporting another version installed")
	}
	if got := runVersion(t, in.Paths["remnantd"]); got != "v0.2.0" {
		t.Fatalf("installed binary changed: %s", got)
	}
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(in.Paths["remnantd"]), "*.new"))
	if len(left) != 0 {
		t.Fatalf("staging files left: %v", left)
	}
}

func TestManager(t *testing.T) {
	key := newKey(t)
	gh := newFakeGitHub(t)
	gh.publish(t, key, "v0.3.0", binaries("v0.3.0"))
	ctx := context.Background()
	newManager := func(current string) (*Manager, *int) {
		restarts := 0
		return &Manager{Source: source(gh, key), Installer: installDir(t, current), Current: current, Mode: ModeCheck,
			StageDir: filepath.Join(t.TempDir(), "stage"), Restart: func() { restarts++ },
			Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, &restarts
	}

	m, restarts := newManager("v0.2.0")
	if _, err := m.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if s := m.State(); !s.Available || s.Latest != "v0.3.0" || s.Status != "idle" || *restarts != 0 {
		t.Fatalf("after check: %+v", s)
	}
	if err := m.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if *restarts != 1 || m.State().Status != "restarting" || runVersion(t, m.Installer.Paths["remnantd"]) != "v0.3.0" {
		t.Fatalf("apply: restarts=%d state=%+v", *restarts, m.State())
	}

	m, restarts = newManager("v0.3.0")
	if err := m.Apply(ctx); !errors.Is(err, ErrUpToDate) || *restarts != 0 {
		t.Fatalf("same version: %v", err)
	}
	m, _ = newManager("v0.4.0") // the published release is older: never downgrade
	if err := m.Apply(ctx); !errors.Is(err, ErrUpToDate) || runVersion(t, m.Installer.Paths["remnantd"]) != "v0.4.0" {
		t.Fatalf("downgrade: %v", err)
	}
	m, _ = newManager("dev")
	if err := m.Apply(ctx); !errors.Is(err, ErrDisabled) || m.State().Status != "disabled" {
		t.Fatalf("dev build: %v", err)
	}
	m, _ = newManager("v0.2.0")
	m.Mode = ModeOff
	if _, err := m.Check(ctx); !errors.Is(err, ErrDisabled) {
		t.Fatalf("mode off: %v", err)
	}
}
