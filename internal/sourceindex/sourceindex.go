// Package sourceindex reads source snapshots as text and splits files into
// symbol regions. Nothing from the snapshot is executed.
package sourceindex

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The chunker is heuristic and language-aware, not a parser: it finds where
// functions start and treats the lines up to the next start as that symbol's
// region.

const (
	MaxFileBytes  = 1 << 20
	MaxFiles      = 20000
	MaxChunkLines = 200
	windowLines   = 120
)

var extLanguage = map[string]string{
	".c": "c", ".h": "c",
	".cc": "cpp", ".cpp": "cpp", ".cxx": "cpp", ".hpp": "cpp", ".hh": "cpp", ".hxx": "cpp",
	".go": "go", ".rs": "rust", ".py": "python",
	".js": "javascript", ".mjs": "javascript", ".cjs": "javascript", ".jsx": "javascript",
	".ts": "typescript", ".tsx": "typescript",
	".java": "java", ".cs": "csharp", ".kt": "kotlin", ".swift": "swift",
	".php": "php", ".rb": "ruby",
}

func LanguageOf(path string) string { return extLanguage[strings.ToLower(filepath.Ext(path))] }

var (
	goFunc   = regexp.MustCompile(`^func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)`)
	pyDef    = regexp.MustCompile(`^\s*(?:async\s+)?(?:def|class)\s+([A-Za-z_]\w*)`)
	rustFn   = regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:(?:async|unsafe|const|extern(?:\s+"[^"]*")?)\s+)*fn\s+([A-Za-z_]\w*)`)
	jsFunc   = regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s*\*?\s*([A-Za-z_$][\w$]*)`)
	rubyDef  = regexp.MustCompile(`^\s*def\s+(?:self\.)?([A-Za-z_]\w*[?!=]?)`)
	cDef     = regexp.MustCompile(`^(?:[A-Za-z_][\w:<>,\*&\[\] ]*?[\s\*&]+)?\**([A-Za-z_~][\w:~]*)\s*\(`)
	modified = regexp.MustCompile(`^[\t ]{0,8}(?:@\w+\s+)*(?:(?:public|private|protected|internal|static|final|abstract|synchronized|native|override|virtual|async|open|suspend|inline|fun|func|function)\s+)+(?:[\w<>\[\],.?]+\s+)*?([A-Za-z_]\w*)\s*[(<]`)
	keywords = map[string]bool{
		"if": true, "for": true, "while": true, "switch": true, "return": true, "sizeof": true,
		"catch": true, "else": true, "do": true, "case": true, "new": true, "delete": true, "throw": true,
	}
)

// SymbolAt returns the symbol defined on line, if the line starts one.
func SymbolAt(lang, line string) (string, bool) {
	var m []string
	switch lang {
	case "go":
		m = goFunc.FindStringSubmatch(line)
	case "python":
		m = pyDef.FindStringSubmatch(line)
	case "rust":
		m = rustFn.FindStringSubmatch(line)
	case "javascript", "typescript":
		m = jsFunc.FindStringSubmatch(line)
	case "ruby":
		m = rubyDef.FindStringSubmatch(line)
	case "c", "cpp":
		// C/C++ definitions start at column 0; statements are indented.
		if line == "" || line[0] == ' ' || line[0] == '\t' || strings.ContainsAny(line, ";#") {
			return "", false
		}
		if i := strings.IndexByte(line, '('); i < 0 || strings.Contains(line[:i], "=") {
			return "", false
		}
		m = cDef.FindStringSubmatch(line)
	case "java", "csharp", "kotlin", "swift", "php":
		if strings.Contains(line, ";") && !strings.HasSuffix(strings.TrimSpace(line), "{") {
			return "", false
		}
		m = modified.FindStringSubmatch(line)
	}
	if m == nil || keywords[m[1]] {
		return "", false
	}
	return m[1], true
}

// Chunk is a 1-based inclusive line range of a file.
type Chunk struct {
	Path     string `json:"path"`
	Language string `json:"language"`
	Symbol   string `json:"symbol"`
	Start    int    `json:"line_start"`
	End      int    `json:"line_end"`
}

// ChunkFile splits lines into symbol regions of at most MaxChunkLines. A file
// with no recognised symbol is split into fixed windows with no symbol name.
func ChunkFile(path, lang string, lines []string) []Chunk {
	type start struct {
		line int
		name string
	}
	var starts []start
	for i, l := range lines {
		if name, ok := SymbolAt(lang, l); ok {
			starts = append(starts, start{i + 1, name})
		}
	}
	var out []Chunk
	if len(starts) == 0 {
		for s := 1; s <= len(lines); s += windowLines {
			out = append(out, Chunk{path, lang, "", s, min(s+windowLines-1, len(lines))})
		}
		return out
	}
	for i, st := range starts {
		end := len(lines)
		if i+1 < len(starts) {
			end = starts[i+1].line - 1
		}
		end = min(end, st.line+MaxChunkLines-1)
		out = append(out, Chunk{path, lang, st.name, st.line, end})
	}
	return out
}

type File struct {
	Path     string // slash-separated, relative to the snapshot root
	Language string
	Lines    []string
	SHA256   string
}

// Walk calls fn for every indexable file under root in lexical order:
// regular files only (symlinks are never followed), known languages, at most
// MaxFileBytes, no NUL bytes, .git excluded. It stops after MaxFiles and
// reports truncation.
func Walk(root string, fn func(File) error) (truncated bool, err error) {
	n := 0
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		lang := LanguageOf(p)
		if !d.Type().IsRegular() || lang == "" {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > MaxFileBytes {
			return err
		}
		if n == MaxFiles {
			truncated = true
			return fs.SkipAll
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if bytes.IndexByte(data, 0) >= 0 {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		n++
		sum := sha256.Sum256(data)
		text := strings.TrimSuffix(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		return fn(File{Path: filepath.ToSlash(rel), Language: lang, Lines: strings.Split(text, "\n"), SHA256: hex.EncodeToString(sum[:])})
	})
	return truncated, err
}
