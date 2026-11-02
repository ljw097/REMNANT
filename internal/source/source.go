// Package source validates target sources and computes immutable snapshot
// revisions.
package source

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var snapshotKeyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

// ValidateSnapshotKey accepts server-issued keys only, never filesystem paths.
func ValidateSnapshotKey(key string) error {
	if !snapshotKeyRe.MatchString(key) || strings.Contains(key, "..") {
		return fmt.Errorf("snapshot_key %q must match %s", key, snapshotKeyRe)
	}
	return nil
}

// CanonicalRepoURL validates a remote repository URL against the configured
// host allowlist and returns its canonical form. An empty allowlist disables
// remote repositories entirely.
func CanonicalRepoURL(raw string, allowedHosts []string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("source_uri: %w", err)
	}
	switch {
	case u.Scheme != "https":
		return "", errors.New("source_uri must use https")
	case u.User != nil:
		return "", errors.New("source_uri must not embed credentials")
	case u.Port() != "":
		return "", errors.New("source_uri must not specify a port")
	case u.RawQuery != "" || u.Fragment != "":
		return "", errors.New("source_uri must not have a query or fragment")
	case strings.Trim(u.Path, "/") == "" || strings.Contains(u.Path, ".."):
		return "", errors.New("source_uri must name a repository path")
	}
	host := strings.ToLower(u.Hostname())
	if !slices.Contains(allowedHosts, host) {
		return "", fmt.Errorf("repository host %q is not allowlisted", host)
	}
	return "https://" + host + "/" + strings.Trim(u.Path, "/"), nil
}

// TreeDigest hashes a snapshot directory: every regular file's relative path,
// mode and content, in lexical order. Symlinks are recorded by their link
// text and never followed. The .git directory is excluded.
func TreeDigest(root string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "L %s\x00%s\n", rel, link)
		case info.Mode().IsRegular():
			sum, err := fileSHA256(p)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "F %s\x00%o\x00%s\n", rel, info.Mode().Perm()&0o111, sum)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// LocalSnapshot resolves key inside snapshotDir, refusing anything that is
// not a real directory directly under it, and returns its revision
// ("sha256:<tree digest>") and tree digest.
func LocalSnapshot(snapshotDir, key string) (revision, digest string, err error) {
	if err := ValidateSnapshotKey(key); err != nil {
		return "", "", err
	}
	dir := filepath.Join(snapshotDir, key)
	info, err := os.Lstat(dir)
	if err != nil {
		return "", "", fmt.Errorf("snapshot %q: %w", key, err)
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("snapshot %q is not a directory", key)
	}
	digest, err = TreeDigest(dir)
	if err != nil {
		return "", "", err
	}
	return "sha256:" + digest, digest, nil
}

// VerifySnapshot checks that snapshotDir/key is a real directory whose tree
// digest is wantDigest, and returns its path.
func VerifySnapshot(snapshotDir, key, wantDigest string) (string, error) {
	if err := ValidateSnapshotKey(key); err != nil {
		return "", err
	}
	dir := filepath.Join(snapshotDir, key)
	info, err := os.Lstat(dir)
	if err != nil {
		return "", fmt.Errorf("snapshot %q: %w", key, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("snapshot %q is not a directory", key)
	}
	digest, err := TreeDigest(dir)
	if err != nil {
		return "", err
	}
	if wantDigest == "" || digest != wantDigest {
		return "", fmt.Errorf("snapshot %q changed on disk since it was pinned", key)
	}
	return dir, nil
}
