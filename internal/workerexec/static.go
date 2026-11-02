package workerexec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ljw097/remnant/internal/sourceindex"
	"github.com/ljw097/remnant/internal/worker"
)

const maxObservations = 500

func indexSymbols(ctx context.Context, env Env, em *emitter) (outcome, error) {
	type file struct {
		Path     string `json:"path"`
		Language string `json:"language"`
		Lines    int    `json:"lines"`
		SHA256   string `json:"sha256"`
	}
	var files []file
	var symbols []sourceindex.Chunk
	truncated, err := sourceindex.Walk(env.Workspace, func(f sourceindex.File) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		files = append(files, file{f.Path, f.Language, len(f.Lines), f.SHA256})
		for _, c := range sourceindex.ChunkFile(f.Path, f.Language, f.Lines) {
			if c.Symbol != "" {
				symbols = append(symbols, c)
			}
		}
		return nil
	})
	if err != nil {
		return outcome{}, err
	}
	if err := writeJSON(filepath.Join(env.Out, "symbols.json"),
		map[string]any{"truncated": truncated, "files": files, "symbols": symbols}); err != nil {
		return outcome{}, err
	}
	return outcome{status: "completed", summary: fmt.Sprintf("%d files, %d symbols indexed", len(files), len(symbols))}, nil
}

// patternRule is a built-in, reviewed source rule. These flag APIs and
// constructs that warrant a look; they do not prove a defect.
type patternRule struct {
	id         string
	langs      []string
	re         *regexp.Regexp
	message    string
	confidence string
}

var cFamily = []string{"c", "cpp"}

var patternRules = []patternRule{
	{"REMNANT-C-GETS", cFamily, regexp.MustCompile(`\bgets\s*\(`), "gets() cannot bound its input", "high"},
	{"REMNANT-C-STRCPY", cFamily, regexp.MustCompile(`\b(strcpy|strcat|wcscpy|wcscat)\s*\(`), "unbounded string copy; check the destination size", "medium"},
	{"REMNANT-C-SPRINTF", cFamily, regexp.MustCompile(`\bv?sprintf\s*\(`), "unbounded formatted write; prefer snprintf", "medium"},
	{"REMNANT-C-FORMAT", cFamily, regexp.MustCompile(`\b(printf|syslog)\s*\(\s*[A-Za-z_][\w.>-]*\s*\)|\bfprintf\s*\(\s*\w+\s*,\s*[A-Za-z_][\w.>-]*\s*\)`), "format string is not a literal", "medium"},
	{"REMNANT-C-MEMCPY-LEN", cFamily, regexp.MustCompile(`\bmem(cpy|move)\s*\([^,;]+,[^,;]+,\s*[A-Za-z_][\w.>-]*\s*\)`), "copy length is a variable; check it is bounded by the destination size", "low"},
	{"REMNANT-C-ALLOCA", cFamily, regexp.MustCompile(`\balloca\s*\(`), "stack allocation of a possibly unbounded size", "low"},
	{"REMNANT-C-SHELL", cFamily, regexp.MustCompile(`\b(system|popen)\s*\(`), "command executed through a shell", "medium"},
	{"REMNANT-PY-EVAL", []string{"python"}, regexp.MustCompile(`(^|[^\w.])(eval|exec)\s*\(`), "dynamic code evaluation", "medium"},
	{"REMNANT-PY-PICKLE", []string{"python"}, regexp.MustCompile(`\bpickle\.loads?\s*\(`), "unpickling can execute code from its input", "medium"},
	{"REMNANT-PY-SHELL", []string{"python"}, regexp.MustCompile(`\bshell\s*=\s*True\b`), "subprocess runs through a shell", "medium"},
	{"REMNANT-JS-EVAL", []string{"javascript", "typescript"}, regexp.MustCompile(`(^|[^\w.])eval\s*\(|\bnew\s+Function\s*\(`), "dynamic code evaluation", "medium"},
	{"REMNANT-GO-UNSAFE", []string{"go"}, regexp.MustCompile(`\bunsafe\.Pointer\b`), "unsafe pointer conversion", "low"},
	{"REMNANT-RS-UNSAFE", []string{"rust"}, regexp.MustCompile(`\bunsafe\s*\{`), "unsafe block", "low"},
}

func isComment(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "*") || strings.HasPrefix(t, "#")
}

func staticPatterns(ctx context.Context, env Env, em *emitter) (outcome, error) {
	var obs []worker.Observation
	files := 0
	_, err := sourceindex.Walk(env.Workspace, func(f sourceindex.File) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		files++
		for _, r := range patternRules {
			if !contains(r.langs, f.Language) {
				continue
			}
			for i, line := range f.Lines {
				if len(obs) == maxObservations {
					return nil
				}
				// '#' starts preprocessor lines in C, comments elsewhere;
				// neither is code these rules are about.
				if isComment(line) || !r.re.MatchString(line) {
					continue
				}
				obs = append(obs, worker.Observation{Kind: "static_finding", RuleID: r.id, FilePath: f.Path,
					LineStart: i + 1, LineEnd: i + 1, Message: r.message, ToolConfidence: r.confidence})
			}
		}
		return nil
	})
	if err != nil {
		return outcome{}, err
	}
	if err := writeJSON(filepath.Join(env.Out, "patterns.json"), obs); err != nil {
		return outcome{}, err
	}
	out := outcome{status: "completed", observations: obs,
		summary: fmt.Sprintf("%d files checked against %d built-in rules; %d diagnostic(s)", files, len(patternRules), len(obs))}
	if len(obs) > 0 {
		out.status = "finding"
	}
	if len(obs) == maxObservations {
		out.summary += " (limit reached)"
	}
	return out, nil
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

var packRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// semgrep runs a local rule pack. The registry is never contacted: rules
// come only from RulesDir and the sandbox has no network.
func semgrep(ctx context.Context, env Env, req worker.Request, em *emitter) (outcome, error) {
	bin, err := exec.LookPath("semgrep")
	if err != nil {
		return outcome{}, unsupported("semgrep is not installed in the worker environment")
	}
	pack, _ := req.Inputs["rule_pack"].(string)
	if pack == "" {
		pack = "default"
	}
	if !packRe.MatchString(pack) || strings.Contains(pack, "..") {
		return outcome{}, invalidInput("rule_pack %q is not a valid pack name", pack)
	}
	if env.RulesDir == "" {
		return outcome{}, unsupported("no semgrep rule packs are installed")
	}
	rules := filepath.Join(env.RulesDir, pack)
	if fi, err := os.Lstat(rules); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		return outcome{}, invalidInput("rule pack %q is not installed", pack)
	}
	sarif := filepath.Join(env.Out, "semgrep.sarif")
	cmd := exec.CommandContext(ctx, bin, "scan", "--config", rules, "--sarif", "--output", sarif,
		"--metrics", "off", "--disable-version-check", "--quiet", "--timeout", "60",
		fmt.Sprintf("--jobs=%d", max(1, req.Limits.CPUCores)), env.Workspace)
	cmd.Dir = env.Scratch
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + env.Scratch, "XDG_CACHE_HOME=" + env.Scratch,
		"XDG_CONFIG_HOME=" + env.Scratch, "SEMGREP_SEND_METRICS=off", "LANG=C.UTF-8"}
	var stderr bytes.Buffer
	cmd.Stderr = &limitedWriter{w: &stderr, n: 64 << 10}
	em.progress("semgrep", 10, "running semgrep with pack "+pack)
	err = cmd.Run()
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1) {
		return outcome{status: "failed", exitCode: exitCode(err), summary: "semgrep failed: " + lastLine(stderr.String())}, nil
	}
	data, err := os.ReadFile(sarif)
	if err != nil {
		return outcome{status: "failed", summary: "semgrep produced no SARIF output"}, nil
	}
	obs, err := parseSARIF(data, env.Workspace)
	if err != nil {
		return outcome{status: "failed", summary: "unreadable SARIF: " + err.Error()}, nil
	}
	out := outcome{status: "completed", observations: obs, summary: fmt.Sprintf("semgrep pack %s: %d diagnostic(s)", pack, len(obs))}
	if len(obs) > 0 {
		out.status = "finding"
	}
	return out, nil
}

// parseSARIF converts SARIF 2.1 results into observations. Locations that do
// not resolve to a clean path inside the workspace are dropped.
func parseSARIF(data []byte, workspace string) ([]worker.Observation, error) {
	var doc struct {
		Runs []struct {
			Results []struct {
				RuleID  string `json:"ruleId"`
				Level   string `json:"level"`
				Message struct {
					Text string `json:"text"`
				} `json:"message"`
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct {
							URI string `json:"uri"`
						} `json:"artifactLocation"`
						Region struct {
							StartLine int `json:"startLine"`
							EndLine   int `json:"endLine"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	confidence := map[string]string{"error": "high", "warning": "medium", "note": "low"}
	obs := []worker.Observation{}
	for _, run := range doc.Runs {
		for _, r := range run.Results {
			if len(obs) == maxObservations {
				return obs, nil
			}
			o := worker.Observation{Kind: "static_finding", RuleID: truncate(r.RuleID, 200),
				Message: truncate(r.Message.Text, 1000), ToolConfidence: confidence[r.Level]}
			if o.Message == "" {
				o.Message = "semgrep rule " + o.RuleID
			}
			if len(r.Locations) > 0 {
				loc := r.Locations[0].PhysicalLocation
				if p, ok := workspacePath(loc.ArtifactLocation.URI, workspace); ok {
					o.FilePath = p
					o.LineStart, o.LineEnd = loc.Region.StartLine, max(loc.Region.EndLine, loc.Region.StartLine)
					if o.LineStart < 0 {
						o.LineStart, o.LineEnd = 0, 0
					}
				}
			}
			obs = append(obs, o)
		}
	}
	return obs, nil
}

// workspacePath maps a tool-reported path (relative, absolute or file://)
// to a clean path relative to the workspace.
func workspacePath(raw, workspace string) (string, bool) {
	if u, err := url.Parse(raw); err == nil && u.Scheme == "file" {
		raw = u.Path
	}
	if path.IsAbs(raw) {
		rel, err := filepath.Rel(workspace, raw)
		if err != nil {
			return "", false
		}
		raw = filepath.ToSlash(rel)
	}
	raw = strings.TrimPrefix(raw, "./")
	if worker.ValidateRelPath(raw) != nil {
		return "", false
	}
	return raw, true
}

type limitedWriter struct {
	w *bytes.Buffer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if room := l.n - l.w.Len(); room > 0 {
		l.w.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return truncate(strings.TrimSpace(lines[len(lines)-1]), 500)
}

func exitCode(err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}
