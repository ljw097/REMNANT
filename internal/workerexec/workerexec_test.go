package workerexec

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ljw097/remnant/internal/worker"
)

func newEnv(t *testing.T, files map[string]string) Env {
	t.Helper()
	root := t.TempDir()
	env := Env{Workspace: filepath.Join(root, "ws"), Out: filepath.Join(root, "out"), Scratch: filepath.Join(root, "scratch")}
	for _, d := range []string{env.Workspace, env.Out, env.Scratch} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range files {
		p := filepath.Join(env.Workspace, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return env
}

func request(op string) worker.Request {
	r := worker.Request{Protocol: worker.Protocol, Message: "request", RequestID: "req_test", RunID: 1, Operation: op,
		Workspace: worker.Workspace{MountID: "target_1", RelativeRoot: ".", Revision: "sha256:x"},
		Inputs:    map[string]any{"hypothesis_id": 7},
		Limits:    worker.Limits{WallSeconds: 60, MemoryMB: 512, CPUCores: 1, MaxOutputBytes: 1 << 20, MaxProcesses: 32},
		Policy:    worker.DefaultPolicy()}
	if strings.HasPrefix(op, "lab.") {
		r.ApprovalID = 3
	}
	return r
}

// final runs req and returns the last protocol message, after checking that
// every line parses and progress lines precede it.
func final(t *testing.T, env Env, req worker.Request) any {
	t.Helper()
	var buf bytes.Buffer
	if err := Run(context.Background(), env, req, &buf); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	var last any
	for i, l := range lines {
		msg, err := worker.ParseLine([]byte(l))
		if err != nil {
			t.Fatalf("line %d %q: %v", i, l, err)
		}
		if _, ok := msg.(*worker.Progress); !ok && i != len(lines)-1 {
			t.Fatalf("final message before line %d", i)
		}
		last = msg
	}
	if res, ok := last.(*worker.Result); ok {
		if err := worker.ValidateResult(req, *res); err != nil {
			t.Fatalf("worker produced an invalid result: %v", err)
		}
	}
	return last
}

func TestStaticPatterns(t *testing.T) {
	env := newEnv(t, map[string]string{
		"src/a.c":  "#include <stdio.h>\nvoid f(char *s) {\n\tchar b[8];\n\tstrcpy(b, s);\n\t// strcpy(b, s); in a comment\n\tprintf(s);\n}\n",
		"tool.py":  "import pickle\nx = pickle.loads(data)\nobj.eval(1)\n",
		"README":   "strcpy(",
		"src/b.go": "package b\nvar p = unsafe.Pointer(nil)\n",
	})
	res, ok := final(t, env, request("static.patterns")).(*worker.Result)
	if !ok || res.Status != "finding" {
		t.Fatalf("result = %#v", res)
	}
	got := map[string]int{}
	for _, o := range res.Observations {
		got[o.RuleID+"@"+o.FilePath] = o.LineStart
	}
	want := map[string]int{"REMNANT-C-STRCPY@src/a.c": 4, "REMNANT-C-FORMAT@src/a.c": 6, "REMNANT-PY-PICKLE@tool.py": 2, "REMNANT-GO-UNSAFE@src/b.go": 2}
	if len(got) != len(want) {
		t.Fatalf("observations = %v, want %v", got, want)
	}
	for k, line := range want {
		if got[k] != line {
			t.Errorf("%s at line %d, want %d", k, got[k], line)
		}
	}
	if len(res.Artifacts) != 1 || res.Artifacts[0].Name != "patterns.json" {
		t.Errorf("artifacts = %+v", res.Artifacts)
	}
}

func TestIndexSymbols(t *testing.T) {
	env := newEnv(t, map[string]string{"main.go": "package main\n\nfunc main() {\n}\n"})
	res := final(t, env, request("index.symbols")).(*worker.Result)
	if res.Status != "completed" || !strings.Contains(res.Summary, "1 symbols") || res.Artifacts[0].Name != "symbols.json" {
		t.Fatalf("result = %+v", res)
	}
}

func TestUnsupportedAndInvalid(t *testing.T) {
	env := newEnv(t, nil)
	t.Setenv("PATH", t.TempDir()) // no semgrep
	if e, ok := final(t, env, request("static.semgrep")).(*worker.Error); !ok || e.Code != "UNSUPPORTED" {
		t.Errorf("semgrep without the tool: %#v", e)
	}
	if e, ok := final(t, env, request("static.codeql")).(*worker.Error); !ok || e.Code != "UNSUPPORTED" {
		t.Errorf("codeql: %#v", e)
	}
	if e, ok := final(t, env, request("lab.unit_test")).(*worker.Error); !ok || e.Code != "INVALID_INPUT" {
		t.Errorf("lab without harness: %#v", e)
	}
	bad := request("static.patterns")
	bad.Policy.Network = "host"
	if e, ok := final(t, env, bad).(*worker.Error); !ok || e.Code != "INVALID_INPUT" {
		t.Errorf("request with a network policy: %#v", e)
	}
}

func TestSemgrepWithFakeTool(t *testing.T) {
	env := newEnv(t, map[string]string{"src/a.c": "int x;\n"})
	env.RulesDir = t.TempDir()
	if err := os.Mkdir(filepath.Join(env.RulesDir, "default"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	// The fake writes a SARIF file to the --output argument.
	script := `#!/bin/sh
while [ $# -gt 0 ]; do [ "$1" = "--output" ] && out=$2; shift; done
cat > "$out" <<EOF
{"runs":[{"results":[
 {"ruleId":"c.x","level":"warning","message":{"text":"bad"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"` + env.Workspace + `/src/a.c"},"region":{"startLine":1,"endLine":1}}}]},
 {"ruleId":"c.y","level":"error","message":{"text":"outside"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"/etc/passwd"},"region":{"startLine":1}}}]}
]}]}
EOF
exit 1
`
	if err := os.WriteFile(filepath.Join(bin, "semgrep"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	res, ok := final(t, env, request("static.semgrep")).(*worker.Result)
	if !ok || res.Status != "finding" || len(res.Observations) != 2 {
		t.Fatalf("result = %#v", res)
	}
	if o := res.Observations[0]; o.FilePath != "src/a.c" || o.LineStart != 1 || o.ToolConfidence != "medium" {
		t.Errorf("first observation = %+v", o)
	}
	if o := res.Observations[1]; o.FilePath != "" {
		t.Errorf("a location outside the workspace was kept: %+v", o)
	}
	bad := request("static.semgrep")
	bad.Inputs = map[string]any{"rule_pack": "../../etc"}
	if e, ok := final(t, env, bad).(*worker.Error); !ok || e.Code != "INVALID_INPUT" {
		t.Errorf("traversing rule pack: %#v", e)
	}
}

func harnessEnv(t *testing.T, name, harness string, extra map[string]string) Env {
	files := map[string]string{".remnant/lab/" + name: harness}
	for k, v := range extra {
		files[k] = v
	}
	return newEnv(t, files)
}

func TestLabExitCodes(t *testing.T) {
	cases := []struct {
		harness, status string
	}{
		{"exit 0", "completed"},
		{"echo 'state observed after failed init'; exit 10", "finding"},
		{"echo boom >&2; exit 3", "failed"},
	}
	for _, c := range cases {
		env := harnessEnv(t, "unit_test", c.harness, nil)
		res := final(t, env, request("lab.unit_test")).(*worker.Result)
		if res.Status != c.status {
			t.Errorf("%q: status %s (%s), want %s", c.harness, res.Status, res.Summary, c.status)
		}
	}
}

func TestLabHarnessEnvironmentAndOutputs(t *testing.T) {
	env := harnessEnv(t, "unit_test", `
set -e
test -f "$REMNANT_REQUEST"
grep -q '"hypothesis_id": 7' "$REMNANT_REQUEST"
echo built > build.out               # the workspace copy is writable
printf 'trace' > "$REMNANT_OUT/trace.txt"
exit 0`, nil)
	res := final(t, env, request("lab.unit_test")).(*worker.Result)
	if res.Status != "completed" {
		t.Fatalf("status %s: %s", res.Status, res.Summary)
	}
	names := []string{}
	for _, a := range res.Artifacts {
		names = append(names, a.Name)
	}
	if strings.Join(names, ",") != "lab-trace.txt,lab.log" {
		t.Errorf("artifacts = %v", names)
	}
	if _, err := os.Stat(filepath.Join(env.Workspace, "build.out")); err == nil {
		t.Error("the harness wrote into the original workspace")
	}
}

func TestLabTimeoutKillsProcessGroup(t *testing.T) {
	env := harnessEnv(t, "unit_test", "sleep 300 & sleep 300", nil)
	req := request("lab.unit_test")
	req.Limits.WallSeconds = 1 // floor of 5 s applies
	res := final(t, env, req).(*worker.Result)
	if res.Status != "failed" || !strings.Contains(res.Summary, "did not finish") {
		t.Fatalf("result = %+v", res)
	}
}

func TestLabASan(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("no C compiler")
	}
	env := harnessEnv(t, "asan_test", `
cc -fsanitize=address -g -O0 -o "$TMPDIR/t" src/parser.c test/main.c
"$TMPDIR/t"`, map[string]string{
		"src/parser.c": "#include <string.h>\nvoid parse(char *dst, const char *src, unsigned long n)\n{\n\tmemcpy(dst, src, n);\n}\n",
		"test/main.c":  "void parse(char *, const char *, unsigned long);\nint main(void)\n{\n\tchar b[4];\n\tparse(b, \"0123456789\", 10);\n\treturn b[0];\n}\n",
	})
	res := final(t, env, request("lab.asan_test")).(*worker.Result)
	if res.Status != "finding" || len(res.Observations) == 0 {
		t.Fatalf("result = %+v", res)
	}
	o := res.Observations[0]
	if o.RuleID != "AddressSanitizer" || !strings.Contains(o.Message, "stack-buffer-overflow") {
		t.Errorf("observation = %+v", o)
	}
	// The first frame inside the workspace may be the memcpy call site or
	// the caller's buffer; either way it must be a workspace path.
	if o.FilePath != "src/parser.c" && o.FilePath != "test/main.c" {
		t.Errorf("observation not located in the workspace: %+v", o)
	}
}

func TestClassifyUBSan(t *testing.T) {
	out := classify("/w/src/a.c:12:5: runtime error: signed integer overflow: 2147483647 + 1\n", 1, false, "/w", nil)
	if out.status != "finding" || out.observations[0].FilePath != "src/a.c" || out.observations[0].LineStart != 12 {
		t.Fatalf("outcome = %+v", out)
	}
}

func TestCopyTreeLimit(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "big"), make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(src, filepath.Join(t.TempDir(), "dst"), 1024); err == nil {
		t.Fatal("copy over the limit succeeded")
	}
}
