// Package fetch pins git_repository targets: it checks out one ref of an
// allowlisted https repository into the snapshot directory and reports the
// commit and tree digest.
package fetch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ljw097/remnant/internal/source"
)

type Fetcher struct {
	SnapshotDir  string
	AllowedHosts []string
	// Timeout bounds one fetch. Default 5 minutes.
	Timeout time.Duration
	// MaxBytes bounds the checked-out tree. Default 2 GiB.
	MaxBytes int64
	// LookupIP resolves the repository host. Default net.DefaultResolver.
	LookupIP func(ctx context.Context, host string) ([]netip.Addr, error)
	// GitConfig adds "-c" settings; tests use it to redirect URLs to local
	// repositories.
	GitConfig []string
}

var refRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,199}$`)

// ValidateRef accepts branch, tag and commit names that cannot be read as
// git options or path tricks.
func ValidateRef(ref string) error {
	if ref == "" || ref == "HEAD" {
		return nil
	}
	if !refRe.MatchString(ref) || strings.Contains(ref, "..") || strings.HasSuffix(ref, ".lock") || strings.HasSuffix(ref, "/") {
		return fmt.Errorf("source_ref %q is not a valid branch, tag or commit name", ref)
	}
	return nil
}

// ErrKeyInUse is returned when the snapshot directory already exists.
// Snapshot keys are global under the snapshot directory and a fetch never
// replaces existing content.
var ErrKeyInUse = errors.New("snapshot key already in use on disk")

// Fetch checks out ref (default HEAD) of sourceURI into SnapshotDir/key,
// without the .git directory, and returns the commit SHA and the tree
// digest of the result.
func (f *Fetcher) Fetch(ctx context.Context, sourceURI, ref, key string) (commit, digest string, err error) {
	if err := source.ValidateSnapshotKey(key); err != nil {
		return "", "", err
	}
	if err := ValidateRef(ref); err != nil {
		return "", "", err
	}
	if ref == "" {
		ref = "HEAD"
	}
	canonical, err := source.CanonicalRepoURL(sourceURI, f.AllowedHosts)
	if err != nil {
		return "", "", err
	}
	final := filepath.Join(f.SnapshotDir, key)
	if _, err := os.Lstat(final); err == nil {
		return "", "", ErrKeyInUse
	}

	timeout := f.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	u, _ := url.Parse(canonical)
	ip, err := f.publicAddr(ctx, u.Hostname())
	if err != nil {
		return "", "", err
	}

	if err := os.MkdirAll(f.SnapshotDir, 0o750); err != nil {
		return "", "", err
	}
	tmp, err := os.MkdirTemp(f.SnapshotDir, ".fetch-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(tmp)
	work := filepath.Join(tmp, "tree")

	// Only https, no redirects, no credentials, no hooks, no submodules, no
	// user or system configuration, and the connection pinned to the address
	// that was checked above.
	cfg := []string{
		"protocol.allow=never", "protocol.https.allow=always",
		"http.followRedirects=false", "credential.helper=", "core.hooksPath=/dev/null",
		"transfer.fsckObjects=true", "fetch.recurseSubmodules=false",
		fmt.Sprintf("http.curloptResolve=%s:443:%s", u.Hostname(), ip),
	}
	cfg = append(cfg, f.GitConfig...)
	git := func(args ...string) (string, error) {
		full := []string{}
		for _, c := range cfg {
			full = append(full, "-c", c)
		}
		full = append(full, args...)
		cmd := exec.CommandContext(ctx, "git", full...)
		cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + tmp, "GIT_CONFIG_NOSYSTEM=1",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=/bin/false", "LANG=C"}
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("git %s: %v: %s", args[0], err, lastLine(stderr.String()))
		}
		return strings.TrimSpace(stdout.String()), nil
	}
	if _, err := git("init", "-q", work); err != nil {
		return "", "", err
	}
	if _, err := git("-C", work, "fetch", "-q", "--depth", "1", "--no-tags", "--", canonical, ref); err != nil {
		return "", "", err
	}
	if _, err := git("-C", work, "checkout", "-q", "--detach", "FETCH_HEAD"); err != nil {
		return "", "", err
	}
	if commit, err = git("-C", work, "rev-parse", "FETCH_HEAD^{commit}"); err != nil {
		return "", "", err
	}
	if err := os.RemoveAll(filepath.Join(work, ".git")); err != nil {
		return "", "", err
	}
	if err := f.checkSize(work); err != nil {
		return "", "", err
	}
	if digest, err = source.TreeDigest(work); err != nil {
		return "", "", err
	}
	if err := os.Rename(work, final); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", "", ErrKeyInUse
		}
		return "", "", err
	}
	return commit, digest, nil
}

// publicAddr resolves host and refuses it if any address is not public, so
// an allowlisted name cannot be pointed at internal services.
func (f *Fetcher) publicAddr(ctx context.Context, host string) (netip.Addr, error) {
	lookup := f.LookupIP
	if lookup == nil {
		lookup = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}
	}
	addrs, err := lookup(ctx, host)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("resolving %s: %w", host, err)
	}
	if len(addrs) == 0 {
		return netip.Addr{}, fmt.Errorf("resolving %s: no addresses", host)
	}
	for _, a := range addrs {
		a = a.Unmap()
		if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
			return netip.Addr{}, fmt.Errorf("%s resolves to non-public address %s", host, a)
		}
	}
	a := addrs[0].Unmap()
	if a.Is6() {
		return netip.Addr{}, fmt.Errorf("%s: IPv6-only hosts are not supported yet", host)
	}
	return a, nil
}

func (f *Fetcher) checkSize(dir string) error {
	limit := f.MaxBytes
	if limit <= 0 {
		limit = 2 << 30
	}
	var total int64
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if total += info.Size(); total > limit {
			return fmt.Errorf("checked-out tree is larger than %d bytes", limit)
		}
		return nil
	})
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	l := lines[len(lines)-1]
	if len(l) > 300 {
		l = l[:300]
	}
	return l
}
