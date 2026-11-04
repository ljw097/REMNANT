// Package advisory enriches a repository's history with known published
// vulnerabilities. It queries an external feed (OSV, https://osv.dev) for the
// advisories of the package a repository ships, and returns the commits that
// fixed them. This lets the Historian learn from real CVE patches even when
// the fixing commit's message never mentioned a CVE ("silent" fixes).
//
// The feed is contacted only during repository intake (never during analysis
// or lab validation), only over https to the configured host, and only after
// a public-address check that refuses private or loopback targets (SSRF
// guard). It is off unless an administrator configures it.
package advisory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const defaultBase = "https://api.osv.dev"

// Advisory is one published vulnerability for a package.
type Advisory struct {
	ID           string   // the feed's own id (e.g. GHSA-… or the OSV id)
	Aliases      []string // cross-ids, including CVE-… when known
	Summary      string
	FixedCommits []string // commit SHAs recorded as the fix in a GIT range
}

// CVE returns the advisory's CVE alias if it has one, else its primary id.
func (a Advisory) CVE() string {
	for _, id := range append([]string{a.ID}, a.Aliases...) {
		if strings.HasPrefix(id, "CVE-") {
			return id
		}
	}
	return a.ID
}

// Client queries the OSV feed.
type Client struct {
	http     *http.Client
	base     string
	lookupIP func(ctx context.Context, host string) ([]netip.Addr, error)
}

// NewClient builds a client for the feed at base (default https://api.osv.dev).
func NewClient(base string) *Client {
	if base == "" {
		base = defaultBase
	}
	return &Client{http: &http.Client{Timeout: 20 * time.Second}, base: strings.TrimRight(base, "/")}
}

// ForPackage returns the advisories OSV records for one package. A nil client
// returns nothing, so callers need not branch on whether the feed is enabled.
func (c *Client) ForPackage(ctx context.Context, ecosystem, name string) ([]Advisory, error) {
	if c == nil || ecosystem == "" || name == "" {
		return nil, nil
	}
	if err := c.guardHost(ctx); err != nil {
		return nil, err
	}
	body, _ := json.Marshal(map[string]any{"package": map[string]string{"ecosystem": ecosystem, "name": name}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/query", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("advisory feed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("advisory feed: %s", resp.Status)
	}
	var out struct {
		Vulns []struct {
			ID       string   `json:"id"`
			Summary  string   `json:"summary"`
			Aliases  []string `json:"aliases"`
			Affected []struct {
				Ranges []struct {
					Type   string `json:"type"`
					Events []struct {
						Fixed string `json:"fixed"`
					} `json:"events"`
				} `json:"ranges"`
			} `json:"affected"`
		} `json:"vulns"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("advisory feed: %w", err)
	}
	var advs []Advisory
	for _, v := range out.Vulns {
		a := Advisory{ID: v.ID, Aliases: v.Aliases, Summary: v.Summary}
		seen := map[string]bool{}
		for _, aff := range v.Affected {
			for _, rg := range aff.Ranges {
				if rg.Type != "GIT" {
					continue
				}
				for _, e := range rg.Events {
					if sha := strings.ToLower(strings.TrimSpace(e.Fixed)); len(sha) >= 7 && !seen[sha] {
						seen[sha] = true
						a.FixedCommits = append(a.FixedCommits, sha)
					}
				}
			}
		}
		advs = append(advs, a)
	}
	return advs, nil
}

// guardHost refuses a feed host that resolves to a private or loopback
// address, so a misconfigured base URL cannot reach internal services.
func (c *Client) guardHost(ctx context.Context) error {
	u, err := url.Parse(c.base)
	if err != nil || u.Scheme != "https" {
		return fmt.Errorf("advisory feed: base URL must be https")
	}
	lookup := c.lookupIP
	if lookup == nil {
		lookup = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}
	}
	addrs, err := lookup(ctx, u.Hostname())
	if err != nil {
		return fmt.Errorf("advisory feed: resolving %s: %w", u.Hostname(), err)
	}
	for _, a := range addrs {
		a = a.Unmap()
		if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
			return fmt.Errorf("advisory feed: %s resolves to non-public address %s", u.Hostname(), a)
		}
	}
	if len(addrs) == 0 {
		return fmt.Errorf("advisory feed: %s did not resolve", u.Hostname())
	}
	return nil
}
