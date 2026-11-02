// Package artifact stores artifact bytes content-addressed under the
// artifact directory. Keys are derived, never taken from input.
package artifact

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

var extRe = regexp.MustCompile(`^\.[A-Za-z0-9]{1,10}$`)

// Key is the storage key of an artifact of a run: runs/<run>/<sha256><ext>.
func Key(runID int64, sha256Hex, name string) string {
	ext := filepath.Ext(name)
	if !extRe.MatchString(ext) {
		ext = ""
	}
	return fmt.Sprintf("runs/%d/%s%s", runID, sha256Hex, ext)
}

// Write stores data at dir/key atomically.
func Write(dir, key string, data []byte) error {
	return place(dir, key, func(f *os.File) error {
		_, err := f.Write(data)
		return err
	})
}

// Import copies the file at src to dir/key atomically. Copying (not
// renaming) works across filesystems and leaves src to its owner.
func Import(dir, key, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return place(dir, key, func(f *os.File) error {
		_, err := io.Copy(f, in)
		return err
	})
}

func place(dir, key string, fill func(*os.File) error) error {
	path := filepath.Join(dir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := fill(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o640); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
