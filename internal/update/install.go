package update

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Installer replaces the installed binaries in place. Both live in
// directories the service user can write; the previous versions are kept
// next to them as <name>.old.
type Installer struct {
	Paths map[string]string // binary name → installed path
}

// Marker records an installed update until the new version has proven it
// starts. If it keeps failing to start, the previous version is restored.
type Marker struct {
	From        string    `json:"from"`
	To          string    `json:"to"`
	Attempts    int       `json:"attempts"`
	InstalledAt time.Time `json:"installed_at"`
}

// MaxStartAttempts is how many starts a new version gets before rollback.
const MaxStartAttempts = 2

func (in Installer) markerPath() string {
	return filepath.Join(filepath.Dir(in.Paths["remnantd"]), ".remnantd-update.json")
}

// Install checks each staged binary reports the bundle's version, then swaps
// all of them in. On any failure the previous binaries are restored.
func (in Installer) Install(ctx context.Context, b Bundle, from string) error {
	for _, name := range Binaries {
		if in.Paths[name] == "" {
			return fmt.Errorf("update: no install path for %s", name)
		}
		if err := checkVersion(ctx, b.Paths[name], b.Version.String()); err != nil {
			return fmt.Errorf("update: staged %s: %w", name, err)
		}
	}
	// Copy next to the destination first, so the swap is a rename within
	// one directory.
	for _, name := range Binaries {
		if err := copyFile(b.Paths[name], in.Paths[name]+".new"); err != nil {
			in.cleanupNew()
			return err
		}
	}
	var swapped []string
	for _, name := range Binaries {
		dst := in.Paths[name]
		if _, err := os.Stat(dst); err == nil {
			if err := os.Rename(dst, dst+".old"); err != nil {
				in.restore(swapped)
				in.cleanupNew()
				return err
			}
		}
		if err := os.Rename(dst+".new", dst); err != nil {
			_ = os.Rename(dst+".old", dst)
			in.restore(swapped)
			in.cleanupNew()
			return err
		}
		swapped = append(swapped, name)
	}
	return in.writeMarker(Marker{From: from, To: b.Version.String(), InstalledAt: time.Now().UTC()})
}

func (in Installer) restore(names []string) {
	for _, name := range names {
		_ = os.Rename(in.Paths[name]+".old", in.Paths[name])
	}
}

func (in Installer) cleanupNew() {
	for _, name := range Binaries {
		_ = os.Remove(in.Paths[name] + ".new")
	}
}

// Rollback puts the .old binaries back and clears the marker.
func (in Installer) Rollback() error {
	var errs []error
	for _, name := range Binaries {
		dst := in.Paths[name]
		if _, err := os.Stat(dst + ".old"); err != nil {
			continue
		}
		if err := os.Rename(dst+".old", dst); err != nil {
			errs = append(errs, err)
		}
	}
	errs = append(errs, in.ClearMarker())
	return errors.Join(errs...)
}

// StartAttempt is called when remnantd starts. With a pending marker it
// counts the attempt and reports whether the update must be rolled back.
func (in Installer) StartAttempt() (m *Marker, rollback bool, err error) {
	data, err := os.ReadFile(in.markerPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	m = &Marker{}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, false, err
	}
	m.Attempts++
	if m.Attempts > MaxStartAttempts {
		return m, true, nil
	}
	return m, false, in.writeMarker(*m)
}

// ClearMarker confirms the installed version.
func (in Installer) ClearMarker() error {
	err := os.Remove(in.markerPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (in Installer) writeMarker(m Marker) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp := in.markerPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, in.markerPath())
}

// checkVersion runs "<bin> version" and expects the first word to be want.
func checkVersion(ctx context.Context, bin, want string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, "version")
	cmd.Stdout = &out
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("does not run: %w", err)
	}
	if got := strings.Fields(out.String()); len(got) == 0 || got[0] != want {
		return fmt.Errorf("reports version %q, want %s", strings.TrimSpace(out.String()), want)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
