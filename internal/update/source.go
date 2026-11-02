package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	maxSumsBytes    = 64 << 10
	maxSigBytes     = 1 << 10
	maxArchiveBytes = 256 << 20
	maxBinaryBytes  = 128 << 20
	maxAPIBytes     = 4 << 20
)

// Binaries are the files a release installs.
var Binaries = []string{"remnantd", "remnant-worker"}

// Source reads releases from the GitHub REST API.
type Source struct {
	APIBase string // default https://api.github.com
	Repo    string // owner/name
	Keys    []ed25519.PublicKey
	HTTP    *http.Client
	GOOS    string // default runtime.GOOS
	GOARCH  string // default runtime.GOARCH
}

// Release is the newest published release, with its version taken from the
// signed archive name for this platform.
type Release struct {
	Version   Version
	Tag       string
	URL       string
	Published time.Time
	archive   asset
	sums      []byte
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

var repoRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$`)

func (s *Source) base() (*url.URL, error) {
	b := s.APIBase
	if b == "" {
		b = "https://api.github.com"
	}
	u, err := url.Parse(b)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("update: invalid API base %q", b)
	}
	return u, nil
}

func (s *Source) client() (*http.Client, error) {
	base, err := s.base()
	if err != nil {
		return nil, err
	}
	c := s.HTTP
	if c == nil {
		c = &http.Client{Timeout: 10 * time.Minute}
	}
	cc := *c
	// Release assets redirect to a CDN; never let a redirect downgrade to
	// plain http (tests serving over http stay on http).
	cc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("update: too many redirects")
		}
		if req.URL.Scheme != "https" && req.URL.Scheme != base.Scheme {
			return fmt.Errorf("update: refusing redirect to %s", req.URL.Scheme)
		}
		return nil
	}
	return &cc, nil
}

func (s *Source) get(ctx context.Context, rawURL string, limit int64, accept string) ([]byte, error) {
	base, err := s.base()
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != base.Scheme) {
		return nil, fmt.Errorf("update: refusing to download %q", rawURL)
	}
	c, err := s.client()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "remnantd-updater")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update: GET %s: %s", u.Redacted(), resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("update: %s is larger than %d bytes", path.Base(u.Path), limit)
	}
	return data, nil
}

func (s *Source) platform() (string, string) {
	goos, goarch := s.GOOS, s.GOARCH
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	return goos, goarch
}

// Latest returns the newest published, non-prerelease release after
// verifying its SHA256SUMS signature.
func (s *Source) Latest(ctx context.Context) (Release, error) {
	if !repoRe.MatchString(s.Repo) {
		return Release{}, fmt.Errorf("update: invalid repository %q", s.Repo)
	}
	if len(s.Keys) == 0 {
		return Release{}, errors.New("update: no trusted keys")
	}
	base, err := s.base()
	if err != nil {
		return Release{}, err
	}
	body, err := s.get(ctx, strings.TrimRight(base.String(), "/")+"/repos/"+s.Repo+"/releases/latest", maxAPIBytes, "application/vnd.github+json")
	if err != nil {
		return Release{}, err
	}
	var gh struct {
		Tag        string    `json:"tag_name"`
		HTMLURL    string    `json:"html_url"`
		Draft      bool      `json:"draft"`
		Prerelease bool      `json:"prerelease"`
		Published  time.Time `json:"published_at"`
		Assets     []asset   `json:"assets"`
	}
	if err := json.Unmarshal(body, &gh); err != nil {
		return Release{}, fmt.Errorf("update: decoding release: %w", err)
	}
	if gh.Draft || gh.Prerelease {
		return Release{}, errors.New("update: latest release is a draft or pre-release")
	}
	assets := map[string]asset{}
	for _, a := range gh.Assets {
		assets[a.Name] = a
	}
	sumsAsset, ok1 := assets["SHA256SUMS"]
	sigAsset, ok2 := assets["SHA256SUMS.sig"]
	if !ok1 || !ok2 {
		return Release{}, fmt.Errorf("update: release %s has no signed SHA256SUMS", gh.Tag)
	}
	sums, err := s.get(ctx, sumsAsset.URL, maxSumsBytes, "application/octet-stream")
	if err != nil {
		return Release{}, err
	}
	sig, err := s.get(ctx, sigAsset.URL, maxSigBytes, "application/octet-stream")
	if err != nil {
		return Release{}, err
	}
	if err := Verify(s.Keys, sums, string(sig)); err != nil {
		return Release{}, err
	}

	// Everything below relies only on the signed file.
	goos, goarch := s.platform()
	archiveRe := regexp.MustCompile(`^remnant_(v[^_]+)_` + regexp.QuoteMeta(goos) + `_` + regexp.QuoteMeta(goarch) + `\.tar\.gz$`)
	var rel Release
	for name := range parseSums(sums) {
		if m := archiveRe.FindStringSubmatch(name); m != nil {
			v, err := ParseVersion(m[1])
			if err != nil {
				return Release{}, err
			}
			a, ok := assets[name]
			if !ok {
				return Release{}, fmt.Errorf("update: release %s lists %s but does not provide it", gh.Tag, name)
			}
			rel = Release{Version: v, Tag: gh.Tag, URL: gh.HTMLURL, Published: gh.Published, archive: a, sums: sums}
		}
	}
	if rel.archive.Name == "" {
		return Release{}, fmt.Errorf("update: release %s has no build for %s/%s", gh.Tag, goos, goarch)
	}
	return rel, nil
}

var sumLine = regexp.MustCompile(`^([0-9a-f]{64})  ([A-Za-z0-9._-]{1,200})$`)

func parseSums(b []byte) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		if m := sumLine.FindStringSubmatch(strings.TrimRight(sc.Text(), "\r")); m != nil {
			out[m[2]] = m[1]
		}
	}
	return out
}

// Bundle is a verified release unpacked into a staging directory.
type Bundle struct {
	Version Version
	Paths   map[string]string // binary name → staged file
}

// Download fetches the release archive for this platform, checks it against
// the signed SHA256SUMS and extracts the binaries into stageDir.
func (s *Source) Download(ctx context.Context, rel Release, stageDir string) (Bundle, error) {
	want := parseSums(rel.sums)[rel.archive.Name]
	if want == "" {
		return Bundle{}, errors.New("update: archive missing from SHA256SUMS")
	}
	data, err := s.get(ctx, rel.archive.URL, maxArchiveBytes, "application/octet-stream")
	if err != nil {
		return Bundle{}, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != want {
		return Bundle{}, fmt.Errorf("update: %s does not match its signed checksum", rel.archive.Name)
	}
	if err := os.RemoveAll(stageDir); err != nil {
		return Bundle{}, err
	}
	if err := os.MkdirAll(stageDir, 0o700); err != nil {
		return Bundle{}, err
	}
	paths, err := extract(data, stageDir)
	if err != nil {
		return Bundle{}, err
	}
	return Bundle{Version: rel.Version, Paths: paths}, nil
}

// extract writes only the expected binaries, found by base name anywhere in
// the archive. Other entries are ignored and never written.
func extract(archive []byte, dir string) (map[string]string, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("update: archive: %w", err)
	}
	tr := tar.NewReader(gz)
	paths := map[string]string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("update: archive: %w", err)
		}
		name := path.Base(path.Clean(h.Name))
		if !isBinary(name) {
			continue
		}
		if h.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("update: %s in the archive is not a regular file", name)
		}
		if _, dup := paths[name]; dup {
			return nil, fmt.Errorf("update: %s appears twice in the archive", name)
		}
		if h.Size > maxBinaryBytes {
			return nil, fmt.Errorf("update: %s is larger than %d bytes", name, maxBinaryBytes)
		}
		p := filepath.Join(dir, name)
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
		if err != nil {
			return nil, err
		}
		_, err = io.Copy(f, io.LimitReader(tr, maxBinaryBytes))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return nil, err
		}
		paths[name] = p
	}
	for _, b := range Binaries {
		if paths[b] == "" {
			return nil, fmt.Errorf("update: archive does not contain %s", b)
		}
	}
	return paths, nil
}

func isBinary(name string) bool {
	for _, b := range Binaries {
		if name == b {
			return true
		}
	}
	return false
}
