// Package sandbox runs remnant-worker in an isolated sandbox and implements
// worker.Worker for the orchestrator.
//
// Isolation, from the outside in:
//
//   - systemd-run --user --scope puts the sandbox in its own cgroup with
//     MemoryMax, MemorySwapMax=0, TasksMax and CPUQuota from the request.
//   - bwrap unshares every namespace (user, network, PID, mount, IPC, UTS,
//     cgroup), so there is no network at all, drops all capabilities,
//     forbids nested user namespaces and dies with its parent.
//   - The filesystem is /usr (and the /bin, /lib links) read-only, the target
//     snapshot read-only at /workspace, an empty per-request /out, a
//     size-capped tmpfs at /tmp and the worker binary. Nothing else from the
//     host: no home directories, credentials, sockets or REMNANT config.
//   - remnant-worker applies RLIMIT_CPU, RLIMIT_FSIZE, RLIMIT_CORE and
//     RLIMIT_NOFILE before running anything.
//
// Everything the worker returns is validated, and every artifact is
// re-hashed before it is imported into the artifact store.
package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ljw097/remnant/internal/artifact"
	"github.com/ljw097/remnant/internal/worker"
)

const (
	workerPath     = "/opt/remnant/bin/remnant-worker"
	rulesPath      = "/opt/remnant/rules"
	maxStdoutBytes = 16 << 20
	maxTmpfsBytes  = 1 << 30
	killGrace      = 5 * time.Second
)

type Config struct {
	// WorkerBin is the remnant-worker executable on the host.
	WorkerBin string
	// WorkDir holds per-request output directories; it is cleaned up after
	// each request.
	WorkDir string
	// ArtifactDir receives verified artifacts (artifact.Key layout).
	ArtifactDir string
	// RulesDir holds semgrep rule packs; optional.
	RulesDir string
	// Resolve maps a request's mount ID and revision to the host directory
	// to mount read-only. It must verify the revision.
	Resolve func(ctx context.Context, mountID, revision string) (string, error)
	// AllowWithoutCgroup runs without systemd-run when no user cgroup
	// scope is available. Memory, task and CPU-share limits are then not
	// enforced; only rlimits and the wall-clock limit are.
	AllowWithoutCgroup bool
	Log                *slog.Logger
}

type Supervisor struct {
	cfg        Config
	bwrap      string
	systemdRun string // empty when running without a cgroup scope

	mu      sync.Mutex
	running map[string]context.CancelCauseFunc
}

var errCancelled = errors.New("cancelled by the supervisor")

// New checks the host can provide the sandbox and returns a supervisor.
func New(cfg Config) (*Supervisor, error) {
	if cfg.Resolve == nil || cfg.WorkDir == "" || cfg.ArtifactDir == "" {
		return nil, errors.New("sandbox: Resolve, WorkDir and ArtifactDir are required")
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	bw, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, errors.New("sandbox: bubblewrap (bwrap) is not installed")
	}
	fi, err := os.Stat(cfg.WorkerBin)
	if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
		return nil, fmt.Errorf("sandbox: worker binary %q is not an executable file", cfg.WorkerBin)
	}
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		return nil, err
	}
	s := &Supervisor{cfg: cfg, bwrap: bw, running: map[string]context.CancelCauseFunc{}}
	if sr, err := exec.LookPath("systemd-run"); err == nil {
		probe := exec.Command(sr, "--user", "--scope", "--quiet", "--collect", "-p", "MemoryMax=32M", "-p", "TasksMax=4", "--", "true")
		if probe.Run() == nil {
			s.systemdRun = sr
		}
	}
	if s.systemdRun == "" && !cfg.AllowWithoutCgroup {
		return nil, errors.New("sandbox: cannot create a user cgroup scope with systemd-run; memory and task limits would not be enforced " +
			"(set REMNANT_WORKER_ALLOW_NO_CGROUP=1 to accept that)")
	}
	return s, nil
}

// Enforcement describes which limits are enforced, for logs.
func (s *Supervisor) Enforcement() string {
	if s.systemdRun == "" {
		return "namespaces + rlimits + wall clock (no cgroup: memory/tasks/cpu share NOT enforced)"
	}
	return "namespaces + cgroup (memory, tasks, cpu) + rlimits + wall clock"
}

// command builds the sandboxed invocation of remnant-worker <mode>.
// workspace may be empty (capabilities probe).
func (s *Supervisor) command(ctx context.Context, mode, workspace, out string, l worker.Limits) *exec.Cmd {
	var args []string
	bin := s.bwrap
	if s.systemdRun != "" {
		bin = s.systemdRun
		args = append(args, "--user", "--scope", "--quiet", "--collect",
			"-p", fmt.Sprintf("MemoryMax=%dM", l.MemoryMB),
			"-p", "MemorySwapMax=0",
			"-p", fmt.Sprintf("TasksMax=%d", l.MaxProcesses),
			"-p", fmt.Sprintf("CPUQuota=%d%%", 100*l.CPUCores),
			"--", s.bwrap)
	}
	args = append(args,
		"--unshare-all", "--unshare-user", "--disable-userns", "--die-with-parent", "--new-session", "--clearenv",
		"--cap-drop", "ALL",
		"--ro-bind", "/usr", "/usr")
	for _, d := range []string{"/bin", "/sbin", "/lib", "/lib32", "/lib64", "/libx32"} {
		if target, err := os.Readlink(d); err == nil {
			args = append(args, "--symlink", target, d)
		} else {
			args = append(args, "--ro-bind-try", d, d)
		}
	}
	for _, f := range []string{"/etc/ld.so.cache", "/etc/ld.so.conf", "/etc/ld.so.conf.d", "/etc/alternatives"} {
		args = append(args, "--ro-bind-try", f, f)
	}
	args = append(args,
		"--proc", "/proc", "--dev", "/dev",
		"--size", fmt.Sprint(min(int64(maxTmpfsBytes), int64(l.MemoryMB)<<20)), "--tmpfs", "/tmp",
		"--bind", out, "/out",
		"--ro-bind", s.cfg.WorkerBin, workerPath)
	if workspace != "" {
		args = append(args, "--ro-bind", workspace, "/workspace")
	}
	if s.cfg.RulesDir != "" {
		args = append(args, "--ro-bind-try", s.cfg.RulesDir, rulesPath)
	}
	args = append(args,
		"--setenv", "PATH", "/usr/local/bin:/usr/bin:/bin",
		"--setenv", "HOME", "/tmp", "--setenv", "LANG", "C.UTF-8",
		"--chdir", "/tmp",
		"--", workerPath, mode)

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = []string{}
	if s.systemdRun != "" {
		// systemd-run needs the user bus to create the scope; nothing of
		// this reaches the sandbox, which starts with --clearenv.
		for _, k := range []string{"XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS"} {
			if v, ok := os.LookupEnv(k); ok {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = killGrace
	return cmd
}

// Capabilities asks a sandboxed worker which operations it supports.
func (s *Supervisor) Capabilities(ctx context.Context) ([]string, error) {
	out, err := os.MkdirTemp(s.cfg.WorkDir, "caps-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(out)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := s.command(ctx, "capabilities", "", out, worker.Limits{MemoryMB: 256, MaxProcesses: 16, CPUCores: 1})
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("sandbox: capabilities probe failed: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	var caps []string
	if err := json.Unmarshal(data, &caps); err != nil {
		return nil, fmt.Errorf("sandbox: capabilities probe: %w", err)
	}
	return caps, nil
}

// Execute implements worker.Worker.
func (s *Supervisor) Execute(ctx context.Context, req worker.Request, onProgress func(worker.Progress)) (worker.Result, error) {
	if err := worker.ValidateRequest(req); err != nil {
		return worker.Result{}, err
	}
	workspace, err := s.cfg.Resolve(ctx, req.Workspace.MountID, req.Workspace.Revision)
	if err != nil {
		return worker.Result{}, &worker.Error{Code: "WORKSPACE_UNAVAILABLE", Summary: err.Error()}
	}
	dir, err := os.MkdirTemp(s.cfg.WorkDir, "req-")
	if err != nil {
		return worker.Result{}, err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "out")
	if err := os.Mkdir(out, 0o700); err != nil {
		return worker.Result{}, err
	}

	wall := time.Duration(req.Limits.WallSeconds)*time.Second + 15*time.Second
	wctx, cancelWall := context.WithTimeoutCause(ctx, wall, errWallClock)
	defer cancelWall()
	rctx, cancel := context.WithCancelCause(wctx)
	defer cancel(nil)
	s.mu.Lock()
	if _, dup := s.running[req.RequestID]; dup {
		s.mu.Unlock()
		return worker.Result{}, fmt.Errorf("sandbox: request %s is already running", req.RequestID)
	}
	s.running[req.RequestID] = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.running, req.RequestID)
		s.mu.Unlock()
	}()

	final, stderr, runErr := s.run(rctx, cancel, req, workspace, out, onProgress)
	switch cause := context.Cause(rctx); {
	case ctx.Err() != nil:
		return worker.Result{}, ctx.Err()
	case errors.Is(cause, errWallClock):
		return worker.Result{}, &worker.Error{Code: "RESOURCE_LIMIT", Summary: fmt.Sprintf("wall-clock limit of %ds exceeded", req.Limits.WallSeconds)}
	case errors.Is(cause, errCancelled):
		return worker.Result{}, &worker.Error{Code: "CANCELLED", Summary: "cancelled"}
	case errors.Is(cause, errOutputLimit):
		return worker.Result{}, &worker.Error{Code: "RESOURCE_LIMIT", Summary: "worker wrote more protocol output than allowed"}
	}
	if werr, ok := final.(*worker.Error); ok {
		return worker.Result{}, werr
	}
	res, ok := final.(*worker.Result)
	if !ok {
		return worker.Result{}, s.noResult(runErr, stderr)
	}
	if err := worker.ValidateResult(req, *res); err != nil {
		return worker.Result{}, err
	}
	if err := s.importArtifacts(req, out, *res); err != nil {
		return worker.Result{}, err
	}
	return *res, nil
}

var (
	errWallClock   = errors.New("wall clock exceeded")
	errOutputLimit = errors.New("protocol output limit exceeded")
)

// run starts the sandbox, sends the request and reads protocol lines until
// the worker exits. It returns the final message (Result or Error), if any.
func (s *Supervisor) run(ctx context.Context, cancel context.CancelCauseFunc, req worker.Request, workspace, out string, onProgress func(worker.Progress)) (any, string, error) {
	cmd := s.command(ctx, "run", workspace, out, req.Limits)
	line, err := json.Marshal(req)
	if err != nil {
		return nil, "", err
	}
	cmd.Stdin = bytes.NewReader(append(line, '\n'))
	var stderr tailBuffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", err
	}
	if err := cmd.Start(); err != nil {
		return nil, "", err
	}

	var final any
	var protoErr error
	sc := bufio.NewScanner(io.LimitReader(stdout, maxStdoutBytes+1))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	var read int
	for sc.Scan() {
		read += len(sc.Bytes()) + 1
		if read > maxStdoutBytes {
			cancel(errOutputLimit)
			protoErr = errOutputLimit
			break
		}
		msg, err := worker.ParseLine(sc.Bytes())
		if err != nil || final != nil {
			if err == nil {
				err = errors.New("worker sent output after its final message")
			}
			protoErr = err
			break
		}
		switch m := msg.(type) {
		case *worker.Progress:
			if m.RequestID == req.RequestID && onProgress != nil {
				onProgress(*m)
			}
		case *worker.Result, *worker.Error:
			final = m
		}
	}
	_, _ = io.Copy(io.Discard, stdout)
	waitErr := cmd.Wait()
	if protoErr != nil {
		if errors.Is(protoErr, errOutputLimit) {
			return nil, stderr.String(), protoErr
		}
		return nil, stderr.String(), fmt.Errorf("sandbox: protocol violation: %w", protoErr)
	}
	return final, stderr.String(), waitErr
}

// noResult explains a worker that exited without a final message.
func (s *Supervisor) noResult(runErr error, stderr string) error {
	var exit *exec.ExitError
	if errors.As(runErr, &exit) {
		if ws, ok := exit.Sys().(syscall.WaitStatus); ok && ws.Signaled() && ws.Signal() == syscall.SIGKILL {
			return &worker.Error{Code: "RESOURCE_LIMIT", Summary: "the sandbox was killed (memory or task limit): " + lastLine(stderr)}
		}
		if exit.ExitCode() == 137 {
			return &worker.Error{Code: "RESOURCE_LIMIT", Summary: "the sandbox was killed (memory or task limit): " + lastLine(stderr)}
		}
	}
	return fmt.Errorf("sandbox: worker exited without a result (%v): %s", runErr, lastLine(stderr))
}

// importArtifacts verifies each reported artifact against the bytes in out
// and copies it into the artifact store. Total output is capped.
func (s *Supervisor) importArtifacts(req worker.Request, out string, res worker.Result) error {
	var total int64
	err := filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return err
	}
	if total > req.Limits.MaxOutputBytes {
		return &worker.Error{Code: "RESOURCE_LIMIT", Summary: fmt.Sprintf("output directory holds %d bytes, limit %d", total, req.Limits.MaxOutputBytes)}
	}
	for _, a := range res.Artifacts {
		p := filepath.Join(out, a.Name)
		fi, err := os.Lstat(p)
		if err != nil || !fi.Mode().IsRegular() {
			return fmt.Errorf("sandbox: artifact %q is not a regular file in the output directory", a.Name)
		}
		sum, err := digest(p)
		if err != nil {
			return err
		}
		if sum != a.SHA256 || fi.Size() != a.SizeBytes {
			return fmt.Errorf("sandbox: artifact %q does not match its reported digest or size", a.Name)
		}
		if err := artifact.Import(s.cfg.ArtifactDir, artifact.Key(req.RunID, a.SHA256, a.Name), p); err != nil {
			return err
		}
	}
	return nil
}

// Cancel implements worker.Worker.
func (s *Supervisor) Cancel(requestID string) error {
	s.mu.Lock()
	cancel, ok := s.running[requestID]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("sandbox: no running request %s", requestID)
	}
	cancel(errCancelled)
	return nil
}

func digest(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// tailBuffer keeps the last 64 KiB written to it.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - 64<<10; over > 0 {
		t.buf = t.buf[over:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	l := strings.TrimSpace(lines[len(lines)-1])
	if len(l) > 500 {
		l = l[:500]
	}
	return l
}
