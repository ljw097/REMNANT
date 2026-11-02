package workerexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/ljw097/remnant/internal/worker"
)

// Lab harness contract (docs/worker-protocol.md):
//
//   - The harness is .remnant/lab/<name> in the target snapshot, where <name>
//     is the operation without "lab." (unit_test, asan_test, ...). Because it
//     is part of the snapshot, the approved revision pins its content.
//   - It runs with /bin/sh in a writable copy of the workspace, with
//     REMNANT_REQUEST pointing at the request inputs and REMNANT_OUT at a
//     directory whose files become artifacts.
//   - Exit 0: the expected observation was not reproduced. Exit 10: the
//     harness observed it. Anything else: the harness failed. A sanitizer or
//     libFuzzer report counts as an observation whatever the exit code.

const (
	harnessExitFinding = 10
	maxWorkspaceCopy   = 512 << 20
	maxLabLog          = 8 << 20
)

var labOps = map[string]bool{"unit_test": true, "asan_test": true, "ubsan_test": true, "libfuzzer": true}

func lab(ctx context.Context, env Env, req worker.Request, em *emitter) (outcome, error) {
	name := strings.TrimPrefix(req.Operation, "lab.")
	if !labOps[name] {
		return outcome{}, unsupported("operation %s is not implemented by this worker", req.Operation)
	}
	harnessRel := filepath.Join(".remnant", "lab", name)
	fi, err := os.Lstat(filepath.Join(env.Workspace, harnessRel))
	if err != nil || !fi.Mode().IsRegular() {
		return outcome{}, invalidInput("the snapshot has no harness at %s", filepath.ToSlash(harnessRel))
	}

	em.progress("prepare", 5, "copying the workspace")
	work := filepath.Join(env.Scratch, "work")
	if err := copyTree(env.Workspace, work, maxWorkspaceCopy); err != nil {
		return outcome{}, &opError{code: "RESOURCE_LIMIT", summary: "copying the workspace: " + err.Error()}
	}
	labOut := filepath.Join(env.Scratch, "lab-out")
	tmp := filepath.Join(env.Scratch, "tmp")
	for _, d := range []string{labOut, tmp} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return outcome{}, err
		}
	}
	reqFile := filepath.Join(env.Scratch, "request.json")
	if err := writeJSON(reqFile, map[string]any{"operation": req.Operation, "run_id": req.RunID, "inputs": req.Inputs}); err != nil {
		return outcome{}, err
	}

	timeout := time.Duration(req.Limits.WallSeconds)*time.Second - 10*time.Second
	if timeout < 5*time.Second {
		timeout = 5 * time.Second
	}
	hctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(hctx, "/bin/sh", filepath.Join(work, harnessRel))
	cmd.Dir = work
	cmd.Env = []string{
		"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + env.Scratch, "TMPDIR=" + tmp, "LANG=C.UTF-8",
		"REMNANT_REQUEST=" + reqFile, "REMNANT_OUT=" + labOut, "REMNANT_OPERATION=" + req.Operation,
		"ASAN_OPTIONS=detect_leaks=1:abort_on_error=0:symbolize=1",
		"UBSAN_OPTIONS=print_stacktrace=1:halt_on_error=1",
	}
	// Kill the whole harness process group on timeout, not only the shell.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second

	logPath := filepath.Join(env.Out, "lab.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return outcome{}, err
	}
	var logBuf bytes.Buffer
	sink := io.MultiWriter(&capWriter{w: logFile, n: maxLabLog}, &limitedWriter{w: &logBuf, n: maxLabLog})
	cmd.Stdout, cmd.Stderr = sink, sink

	em.progress("harness", 20, "running "+filepath.ToSlash(harnessRel))
	runErr := cmd.Run()
	logFile.Close()
	timedOut := errors.Is(hctx.Err(), context.DeadlineExceeded)
	if ctx.Err() != nil {
		return outcome{}, &opError{code: "CANCELLED", summary: "the request was cancelled"}
	}
	code := 0
	if runErr != nil {
		code = exitCode(runErr)
	}

	crashes, err := copyLabOutputs(labOut, env.Out)
	if err != nil {
		return outcome{}, err
	}
	out := classify(logBuf.String(), code, timedOut, work, crashes)
	if timedOut {
		out.summary = fmt.Sprintf("harness did not finish within %s", timeout)
	}
	return out, nil
}

var (
	sanitizerRe = regexp.MustCompile(`==\d+==\s*ERROR: (AddressSanitizer|LeakSanitizer|MemorySanitizer|ThreadSanitizer|libFuzzer): ?(.*)`)
	ubsanRe     = regexp.MustCompile(`^(\S+?):(\d+):(\d+): runtime error: (.+)$`)
	frameRe     = regexp.MustCompile(`#\d+ 0x[0-9a-f]+ in \S+ (\S+?):(\d+)(?::\d+)?$`)
)

// classify turns the harness log and exit code into an outcome. Sanitizer
// reports become observations located at the first stack frame inside the
// workspace copy.
func classify(log string, code int, timedOut bool, work string, crashes []string) outcome {
	var obs []worker.Observation
	seen := map[string]bool{}
	add := func(o worker.Observation) {
		key := o.Message + "|" + o.FilePath + fmt.Sprint(o.LineStart)
		if !seen[key] && len(obs) < 50 {
			seen[key] = true
			obs = append(obs, o)
		}
	}
	lines := strings.Split(log, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if m := sanitizerRe.FindStringSubmatch(line); m != nil {
			o := worker.Observation{Kind: "sanitizer", RuleID: m[1], Message: truncate(m[1]+": "+m[2], 500), ToolConfidence: "high"}
			for _, f := range lines[i+1 : min(len(lines), i+40)] {
				fm := frameRe.FindStringSubmatch(strings.TrimSpace(f))
				if fm == nil {
					continue
				}
				if p, ok := workspacePath(fm[1], work); ok {
					fmt.Sscan(fm[2], &o.LineStart)
					o.FilePath, o.LineEnd = p, o.LineStart
					break
				}
			}
			add(o)
		}
		if m := ubsanRe.FindStringSubmatch(line); m != nil {
			o := worker.Observation{Kind: "sanitizer", RuleID: "UndefinedBehaviorSanitizer", Message: truncate("runtime error: "+m[4], 500), ToolConfidence: "high"}
			if p, ok := workspacePath(m[1], work); ok {
				fmt.Sscan(m[2], &o.LineStart)
				o.FilePath, o.LineEnd = p, o.LineStart
			}
			add(o)
		}
	}
	for _, c := range crashes {
		add(worker.Observation{Kind: "fuzzer_crash", Message: "libFuzzer saved " + c, ToolConfidence: "high"})
	}

	switch {
	case len(obs) > 0:
		return outcome{status: "finding", exitCode: code, observations: obs,
			summary: fmt.Sprintf("%d sanitizer/fuzzer observation(s): %s", len(obs), obs[0].Message)}
	case timedOut:
		return outcome{status: "failed", exitCode: code}
	case code == harnessExitFinding:
		return outcome{status: "finding", exitCode: code, summary: "harness reported the expected observation",
			observations: []worker.Observation{{Kind: "harness_assertion", Message: truncate(lastLine(log), 500), ToolConfidence: "medium"}}}
	case code == 0:
		return outcome{status: "completed", exitCode: 0, summary: "expected observation not reproduced"}
	default:
		return outcome{status: "failed", exitCode: code, summary: fmt.Sprintf("harness failed with exit code %d: %s", code, lastLine(log))}
	}
}

// copyLabOutputs moves harness outputs to the artifact directory as
// lab-<name>, and returns the names of libFuzzer crash files.
func copyLabOutputs(from, to string) ([]string, error) {
	entries, err := os.ReadDir(from)
	if err != nil {
		return nil, err
	}
	var crashes []string
	for _, e := range entries {
		if !e.Type().IsRegular() || len(e.Name()) > 100 || !artifactName.MatchString(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(to, "lab-"+e.Name()), data, 0o644); err != nil {
			return nil, err
		}
		for _, p := range []string{"crash-", "leak-", "timeout-", "oom-"} {
			if strings.HasPrefix(e.Name(), p) {
				crashes = append(crashes, e.Name())
			}
		}
	}
	return crashes, nil
}

// copyTree copies src to dst (directories, regular files, symlinks as
// links), skipping .git, and fails once more than limit bytes were copied.
func copyTree(src, dst string, limit int64) error {
	var total int64
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			if d.Name() == ".git" && rel != "." {
				return filepath.SkipDir
			}
			return os.MkdirAll(target, 0o755)
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case d.Type().IsRegular():
			info, err := d.Info()
			if err != nil {
				return err
			}
			if total += info.Size(); total > limit {
				return fmt.Errorf("workspace larger than %d bytes", limit)
			}
			return copyFile(p, target, info.Mode().Perm())
		}
		return nil
	})
}

func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm|0o200)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// capWriter writes at most n bytes to w and silently drops the rest.
type capWriter struct {
	w io.Writer
	n int64
}

func (c *capWriter) Write(p []byte) (int, error) {
	if c.n > 0 {
		k := min(int64(len(p)), c.n)
		if _, err := c.w.Write(p[:k]); err != nil {
			return 0, err
		}
		c.n -= k
	}
	return len(p), nil
}
