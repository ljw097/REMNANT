package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ljw097/remnant/internal/artifact"
	"github.com/ljw097/remnant/internal/worker"
)

var workerBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "remnant-worker-bin-")
	if err != nil {
		panic(err)
	}
	workerBin = filepath.Join(dir, "remnant-worker")
	build := exec.Command("go", "build", "-o", workerBin, "github.com/ljw097/remnant/cmd/remnant-worker")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type fixture struct {
	sup       *Supervisor
	snapshots string
	artifacts string
}

func setup(t *testing.T, files map[string]string) *fixture {
	t.Helper()
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not installed")
	}
	root := t.TempDir()
	f := &fixture{snapshots: filepath.Join(root, "snapshots"), artifacts: filepath.Join(root, "artifacts")}
	for name, body := range files {
		p := filepath.Join(f.snapshots, "demo", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sup, err := New(Config{
		WorkerBin: workerBin, WorkDir: filepath.Join(root, "work"), ArtifactDir: f.artifacts,
		Resolve: func(_ context.Context, mountID, revision string) (string, error) {
			if mountID != "target_1" || revision != "rev-1" {
				return "", fmt.Errorf("unknown mount %s@%s", mountID, revision)
			}
			return filepath.Join(f.snapshots, "demo"), nil
		},
	})
	if err != nil {
		t.Skipf("sandbox unavailable on this host: %v", err)
	}
	f.sup = sup
	return f
}

func request(op string) worker.Request {
	r := worker.Request{Protocol: worker.Protocol, Message: "request", RequestID: "req_" + strings.ReplaceAll(op, ".", "_"), RunID: 9,
		Operation: op, Workspace: worker.Workspace{MountID: "target_1", RelativeRoot: ".", Revision: "rev-1"},
		Inputs: map[string]any{}, Policy: worker.DefaultPolicy(),
		Limits: worker.Limits{WallSeconds: 60, MemoryMB: 512, CPUCores: 1, MaxOutputBytes: 10 << 20, MaxProcesses: 64}}
	if strings.HasPrefix(op, "lab.") {
		r.ApprovalID = 1
	}
	return r
}

func TestCapabilities(t *testing.T) {
	f := setup(t, nil)
	caps, err := f.sup.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(caps, ","), "static.patterns") {
		t.Fatalf("capabilities = %v", caps)
	}
}

func TestStaticPatternsImportsVerifiedArtifacts(t *testing.T) {
	f := setup(t, map[string]string{"src/a.c": "void f(char *s) {\n\tchar b[4];\n\tstrcpy(b, s);\n}\n"})
	var progress int
	res, err := f.sup.Execute(context.Background(), request("static.patterns"), func(worker.Progress) { progress++ })
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "finding" || len(res.Observations) != 1 || res.Observations[0].FilePath != "src/a.c" {
		t.Fatalf("result = %+v", res)
	}
	a := res.Artifacts[0]
	data, err := os.ReadFile(filepath.Join(f.artifacts, artifact.Key(9, a.SHA256, a.Name)))
	if err != nil {
		t.Fatalf("artifact not imported: %v", err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != a.SHA256 {
		t.Fatal("imported artifact does not match its digest")
	}
	if entries, _ := os.ReadDir(f.sup.cfg.WorkDir); len(entries) != 0 {
		t.Errorf("work directory not cleaned up: %v", entries)
	}
}

// The harness probes the sandbox from the inside and exits 10 if anything
// it should not reach was reachable.
const isolationProbe = `
breach() { echo "BREACH: $1"; exit 10; }
# Positive controls: the probes below must be able to run at all.
python3 -c pass || { echo "no python3"; exit 3; }
command -v unshare >/dev/null || { echo "no unshare"; exit 3; }
test -r "$0" || { echo "harness unreadable"; exit 3; }
python3 -c "import socket; socket.create_connection(('1.1.1.1', 53), timeout=2)" 2>/dev/null && breach network
python3 -c "import socket; socket.create_connection(('127.0.0.1', 5432), timeout=2)" 2>/dev/null && breach loopback-postgres
touch /workspace/written 2>/dev/null && breach workspace-writable
ls /home 2>/dev/null | grep -q . && breach home-visible
test -e /root/.ssh && breach root-ssh
test -e /etc/passwd && breach etc-visible
test -S /run/user/$(id -u)/bus && breach user-bus
test -e /var/run/docker.sock && breach docker-socket
grep -q "^CapEff:.*[1-9a-f]" /proc/self/status && breach capabilities
unshare -U true 2>/dev/null && breach nested-userns
env | grep -qi -e REMNANT_DATABASE -e REMNANT_MASTER -e ANTHROPIC && breach environment
echo isolated
exit 0
`

func TestIsolation(t *testing.T) {
	f := setup(t, map[string]string{".remnant/lab/unit_test": isolationProbe})
	t.Setenv("REMNANT_MASTER_KEY", "should-not-leak")
	res, err := f.sup.Execute(context.Background(), request("lab.unit_test"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "completed" {
		t.Fatalf("isolation probe: status %s: %s", res.Status, res.Summary)
	}
	if _, err := os.Stat(filepath.Join(f.snapshots, "demo", "written")); err == nil {
		t.Fatal("the sandbox wrote into the snapshot")
	}
}

// The probe must detect breaches when it is not sandboxed, or a passing
// TestIsolation would mean nothing.
func TestIsolationProbeDetectsHost(t *testing.T) {
	if _, err := os.Stat("/etc/passwd"); err != nil {
		t.Skip("host has no /etc/passwd")
	}
	p := filepath.Join(t.TempDir(), "probe")
	if err := os.WriteFile(p, []byte(isolationProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("/bin/sh", p).CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 10 || !strings.Contains(string(out), "BREACH") {
		t.Fatalf("probe on the host: err=%v output=%s", err, out)
	}
}

func TestMemoryLimit(t *testing.T) {
	f := setup(t, map[string]string{".remnant/lab/unit_test": `python3 -c "b = bytearray(400 * 1024 * 1024); print('allocated')"`})
	if f.sup.systemdRun == "" {
		t.Skip("no cgroup scope; memory is not limited")
	}
	req := request("lab.unit_test")
	req.Limits.MemoryMB = 128
	res, err := f.sup.Execute(context.Background(), req, nil)
	var werr *worker.Error
	switch {
	case errors.As(err, &werr) && werr.Code == "RESOURCE_LIMIT":
	case err == nil && res.Status == "failed":
	default:
		t.Fatalf("over-limit allocation: res=%+v err=%v", res, err)
	}
	logs, _ := filepath.Glob(filepath.Join(f.artifacts, "runs", "9", "*.log"))
	for _, l := range logs {
		if b, _ := os.ReadFile(l); strings.Contains(string(b), "allocated") {
			t.Fatal("allocation beyond MemoryMax succeeded")
		}
	}
}

func TestCancel(t *testing.T) {
	f := setup(t, map[string]string{".remnant/lab/unit_test": "sleep 120"})
	req := request("lab.unit_test")
	go func() {
		time.Sleep(1500 * time.Millisecond)
		_ = f.sup.Cancel(req.RequestID)
	}()
	start := time.Now()
	_, err := f.sup.Execute(context.Background(), req, nil)
	var werr *worker.Error
	if !errors.As(err, &werr) || werr.Code != "CANCELLED" {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 15*time.Second {
		t.Fatalf("cancellation took %s", d)
	}
}

func TestContextCancelStopsSandbox(t *testing.T) {
	f := setup(t, map[string]string{".remnant/lab/unit_test": "sleep 120"})
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := f.sup.Execute(ctx, request("lab.unit_test"), nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 15*time.Second {
		t.Fatalf("stop took %s", d)
	}
}

func TestUnknownWorkspace(t *testing.T) {
	f := setup(t, nil)
	req := request("static.patterns")
	req.Workspace.Revision = "other"
	_, err := f.sup.Execute(context.Background(), req, nil)
	var werr *worker.Error
	if !errors.As(err, &werr) || werr.Code != "WORKSPACE_UNAVAILABLE" {
		t.Fatalf("err = %v", err)
	}
}

func TestImportRejectsTamperedArtifacts(t *testing.T) {
	f := setup(t, nil)
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "x.log"), []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := request("static.patterns")
	res := worker.Result{Artifacts: []worker.Artifact{{Name: "x.log", SHA256: strings.Repeat("0", 64), SizeBytes: 4}}}
	if err := f.sup.importArtifacts(req, out, res); err == nil {
		t.Fatal("digest mismatch accepted")
	}
	res.Artifacts[0].Name = "missing.log"
	if err := f.sup.importArtifacts(req, out, res); err == nil {
		t.Fatal("missing artifact accepted")
	}
	if err := os.Symlink("/etc/hostname", filepath.Join(out, "link.log")); err != nil {
		t.Fatal(err)
	}
	res.Artifacts[0].Name = "link.log"
	if err := f.sup.importArtifacts(req, out, res); err == nil {
		t.Fatal("symlinked artifact accepted")
	}
	req.Limits.MaxOutputBytes = 2
	res.Artifacts = nil
	if err := f.sup.importArtifacts(req, out, res); err == nil {
		t.Fatal("oversized output directory accepted")
	}
}
