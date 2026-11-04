package advisory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Package names a repository's published package in an OSV ecosystem, so its
// advisories can be looked up. It is derived from a manifest in the tree.
type Package struct {
	Ecosystem string
	Name      string
}

// InferPackages reads well-known manifests at the root of dir and returns the
// packages it can identify. It reads only small, named files and never
// executes anything.
func InferPackages(dir string) []Package {
	var out []Package
	add := func(eco, name string) {
		name = strings.TrimSpace(name)
		if name != "" {
			out = append(out, Package{Ecosystem: eco, Name: name})
		}
	}

	// Go: the module path is the package name.
	if b, err := readRoot(dir, "go.mod"); err == nil {
		if m := reModule.FindSubmatch(b); m != nil {
			add("Go", string(m[1]))
		}
	}
	// npm: package.json "name".
	if b, err := readRoot(dir, "package.json"); err == nil {
		var p struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(b, &p) == nil {
			add("npm", p.Name)
		}
	}
	// Rust: Cargo.toml [package] name.
	if b, err := readRoot(dir, "Cargo.toml"); err == nil {
		if n := tomlName(b, "package"); n != "" {
			add("crates.io", n)
		}
	}
	// PHP: composer.json "name".
	if b, err := readRoot(dir, "composer.json"); err == nil {
		var p struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(b, &p) == nil {
			add("Packagist", p.Name)
		}
	}
	// Python: pyproject.toml [project] name.
	if b, err := readRoot(dir, "pyproject.toml"); err == nil {
		if n := tomlName(b, "project"); n != "" {
			add("PyPI", n)
		}
	}
	return dedupe(out)
}

var (
	reModule = regexp.MustCompile(`(?m)^\s*module\s+(\S+)\s*$`)
	reName   = regexp.MustCompile(`(?m)^\s*name\s*=\s*["']([^"']+)["']`)
)

// tomlName returns the name = "…" that follows the [section] header, reading
// just enough TOML for the manifests above without a full parser.
func tomlName(b []byte, section string) string {
	s := string(b)
	i := strings.Index(s, "["+section+"]")
	if i < 0 {
		return ""
	}
	rest := s[i+len(section)+2:]
	// Stop at the next section header so a name in a later table is not taken.
	if j := strings.Index(rest, "\n["); j >= 0 {
		rest = rest[:j]
	}
	if m := reName.FindStringSubmatch(rest); m != nil {
		return m[1]
	}
	return ""
}

func readRoot(dir, name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(dir, name))
}

func dedupe(ps []Package) []Package {
	seen := map[Package]bool{}
	var out []Package
	for _, p := range ps {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}
