package advisory

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestForPackageParsesFixCommits(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/query" {
			http.Error(w, "nope", 404)
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = w.Write([]byte(`{"vulns":[
			{"id":"GHSA-aaaa-bbbb-cccc","summary":"Overflow in parse","aliases":["CVE-2024-12345"],
			 "affected":[{"ranges":[
				{"type":"SEMVER","events":[{"introduced":"0"},{"fixed":"1.2.3"}]},
				{"type":"GIT","events":[{"introduced":"0"},{"fixed":"ABCDEF1234567890"}]}
			 ]}]},
			{"id":"OSV-2","summary":"No git range","aliases":[],"affected":[{"ranges":[{"type":"ECOSYSTEM","events":[{"fixed":"2.0.0"}]}]}]}
		]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.http = srv.Client() // trust the test server's TLS certificate
	// Point the egress guard at a public address for the test host.
	c.lookupIP = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	advs, err := c.ForPackage(context.Background(), "npm", "parse")
	if err != nil {
		t.Fatal(err)
	}
	if gotBody["package"].(map[string]any)["ecosystem"] != "npm" {
		t.Fatalf("request body = %v", gotBody)
	}
	if len(advs) != 2 {
		t.Fatalf("advisories = %+v", advs)
	}
	a := advs[0]
	if a.CVE() != "CVE-2024-12345" {
		t.Errorf("CVE() = %q", a.CVE())
	}
	if !slices.Equal(a.FixedCommits, []string{"abcdef1234567890"}) {
		t.Errorf("fixed commits = %v", a.FixedCommits)
	}
	if len(advs[1].FixedCommits) != 0 {
		t.Errorf("second advisory should have no git fix: %v", advs[1].FixedCommits)
	}
}

func TestForPackageRefusesPrivateHost(t *testing.T) {
	c := NewClient("https://osv.internal")
	c.lookupIP = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("10.0.0.5")}, nil
	}
	if _, err := c.ForPackage(context.Background(), "npm", "x"); err == nil {
		t.Fatal("private feed host accepted")
	}
}

func TestNilClientAndEmptyPackage(t *testing.T) {
	var c *Client
	if advs, err := c.ForPackage(context.Background(), "npm", "x"); err != nil || advs != nil {
		t.Fatalf("nil client: %v %v", advs, err)
	}
	c = NewClient("")
	if _, err := c.ForPackage(context.Background(), "", ""); err != nil {
		t.Fatalf("empty package should be a no-op, got %v", err)
	}
}

func TestInferPackages(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module github.com/org/thing\n\ngo 1.25\n")
	write("package.json", `{"name":"@org/thing","version":"1.0.0"}`)
	write("pyproject.toml", "[build-system]\nrequires=[\"hatchling\"]\n\n[project]\nname = \"thing-py\"\nversion = \"1\"\n")
	write("Cargo.toml", "[package]\nname = \"thing-rs\"\nversion = \"0.1.0\"\n")

	pkgs := InferPackages(dir)
	want := []Package{
		{"Go", "github.com/org/thing"},
		{"npm", "@org/thing"},
		{"crates.io", "thing-rs"},
		{"PyPI", "thing-py"},
	}
	for _, w := range want {
		if !slices.Contains(pkgs, w) {
			t.Errorf("missing %v in %v", w, pkgs)
		}
	}
}
