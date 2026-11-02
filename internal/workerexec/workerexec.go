// Package workerexec is the program that runs inside the worker sandbox. It
// reads one request, executes one allowlisted operation against the
// read-only workspace, writes artifacts to the output directory and reports
// with protocol lines on stdout.
package workerexec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/ljw097/remnant/internal/worker"
)

// Env names the sandbox directories. In production they are fixed mount
// points (/workspace, /out, /tmp, /opt/remnant/rules); tests point them at
// temporary directories.
type Env struct {
	Workspace string // read-only source root
	Out       string // artifact directory; every regular file here is reported
	Scratch   string // writable scratch space
	RulesDir  string // semgrep rule packs; may be empty
}

// opError ends an operation with a protocol error message.
type opError struct {
	code      string
	summary   string
	retryable bool
}

func (e *opError) Error() string { return e.code + ": " + e.summary }

func unsupported(format string, a ...any) error {
	return &opError{code: "UNSUPPORTED", summary: fmt.Sprintf(format, a...)}
}

func invalidInput(format string, a ...any) error {
	return &opError{code: "INVALID_INPUT", summary: fmt.Sprintf(format, a...)}
}

// outcome is what an operation reports; artifacts are collected from Out.
type outcome struct {
	status       string // completed | finding | failed
	exitCode     int
	summary      string
	observations []worker.Observation
}

type emitter struct {
	mu  sync.Mutex
	enc *json.Encoder
	id  string
}

func (e *emitter) send(v any) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.enc.Encode(v)
}

func (e *emitter) progress(stage string, percent int, summary string) {
	_ = e.send(worker.Progress{Protocol: worker.Protocol, Message: "progress", RequestID: e.id,
		Stage: stage, Percent: percent, Summary: summary})
}

// Run executes req and writes protocol lines to w. Operation failures are
// reported as protocol error messages; only a failure to write to w is
// returned.
func Run(ctx context.Context, env Env, req worker.Request, w io.Writer) error {
	em := &emitter{enc: json.NewEncoder(w), id: req.RequestID}
	fail := func(err error) error {
		oe, ok := err.(*opError)
		if !ok {
			oe = &opError{code: "INTERNAL", summary: err.Error()}
		}
		return em.send(worker.Error{Protocol: worker.Protocol, Message: "error", RequestID: req.RequestID,
			Code: oe.code, Retryable: oe.retryable, Summary: truncate(oe.summary, 2000)})
	}
	// The supervisor validated the request; check again, since nothing
	// outside the sandbox is trusted in here either.
	if err := worker.ValidateRequest(req); err != nil {
		return fail(invalidInput("%v", err))
	}

	var out outcome
	var err error
	switch op := req.Operation; {
	case op == "index.symbols":
		out, err = indexSymbols(ctx, env, em)
	case op == "static.patterns":
		out, err = staticPatterns(ctx, env, em)
	case op == "static.semgrep":
		out, err = semgrep(ctx, env, req, em)
	case strings.HasPrefix(op, "lab."):
		out, err = lab(ctx, env, req, em)
	default:
		err = unsupported("operation %s is not implemented by this worker", op)
	}
	if err != nil {
		return fail(err)
	}
	artifacts, err := collectArtifacts(env.Out)
	if err != nil {
		return fail(err)
	}
	if out.observations == nil {
		out.observations = []worker.Observation{}
	}
	return em.send(worker.Result{Protocol: worker.Protocol, Message: "result", RequestID: req.RequestID,
		Status: out.status, ExitCode: out.exitCode, Summary: truncate(out.summary, 2000),
		Observations: out.observations, Artifacts: artifacts})
}

var artifactName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

var mediaTypes = map[string]string{
	".json": "application/json", ".sarif": "application/sarif+json",
	".log": "text/plain; charset=utf-8", ".txt": "text/plain; charset=utf-8",
}

// collectArtifacts reports every top-level regular file in dir.
func collectArtifacts(dir string) ([]worker.Artifact, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []worker.Artifact
	for _, e := range entries {
		if !e.Type().IsRegular() || !artifactName.MatchString(e.Name()) {
			continue
		}
		sum, size, err := fileDigest(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		mt := mediaTypes[strings.ToLower(filepath.Ext(e.Name()))]
		if mt == "" {
			mt = "application/octet-stream"
		}
		out = append(out, worker.Artifact{Name: e.Name(), MediaType: mt, SHA256: sum, SizeBytes: size})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func fileDigest(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Capabilities lists the operations this worker can run with the tools
// present in its environment.
func Capabilities() []string {
	caps := []string{"index.symbols", "static.patterns"}
	if _, err := exec.LookPath("semgrep"); err == nil {
		caps = append(caps, "static.semgrep")
	}
	if _, err := os.Stat("/bin/sh"); err == nil {
		caps = append(caps, "lab.unit_test")
	}
	if _, err := exec.LookPath("cc"); err == nil {
		caps = append(caps, "lab.asan_test", "lab.ubsan_test")
	}
	if _, err := exec.LookPath("clang"); err == nil {
		caps = append(caps, "lab.libfuzzer")
	}
	return caps
}
