package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ljw097/remnant/internal/buildinfo"
	"github.com/ljw097/remnant/internal/config"
	"github.com/ljw097/remnant/internal/update"
)

// errRestart asks main to re-execute the (updated) binary after a clean
// shutdown.
var errRestart = errors.New("restart requested")

// confirmAfter is how long a freshly updated remnantd must serve before the
// update counts as good and its rollback marker is cleared.
const confirmAfter = time.Minute

// installedPaths returns where remnantd and remnant-worker are installed.
// It is resolved once at start: after an update renames the running binary
// to .old, /proc/self/exe follows the old file, but the path stays right.
func installedPaths() (map[string]string, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if self, err = filepath.EvalSymlinks(self); err != nil {
		return nil, err
	}
	worker := os.Getenv("REMNANT_WORKER_BIN")
	if worker == "" {
		worker = filepath.Join(filepath.Dir(self), "remnant-worker")
	}
	return map[string]string{"remnantd": self, "remnant-worker": worker}, nil
}

// guardUpdate runs before anything else in serve. If the last update keeps
// failing to start, it restores the previous binaries and runs them.
func guardUpdate(log *slog.Logger, paths map[string]string) {
	in := update.Installer{Paths: paths}
	m, rollback, err := in.StartAttempt()
	switch {
	case err != nil:
		log.Error("reading update marker", "err", err)
	case rollback:
		log.Error("update failed to start repeatedly; rolling back", "from", m.To, "to", m.From)
		if err := in.Rollback(); err != nil {
			log.Error("rollback failed", "err", err)
			return
		}
		restart(log, paths)
	case m != nil:
		log.Info("starting updated version", "from", m.From, "to", m.To, "attempt", m.Attempts)
	}
}

// confirmUpdate clears the rollback marker once the server has run long
// enough to count as healthy.
func confirmUpdate(ctx context.Context, log *slog.Logger, in update.Installer) {
	select {
	case <-ctx.Done():
	case <-time.After(confirmAfter):
		if err := in.ClearMarker(); err != nil {
			log.Error("clearing update marker", "err", err)
		}
	}
}

// restart replaces this process with the binary at its installed path,
// keeping the PID, arguments and environment. It returns only on failure.
func restart(log *slog.Logger, paths map[string]string) {
	log.Info("re-executing", "path", paths["remnantd"])
	err := syscall.Exec(paths["remnantd"], os.Args, os.Environ())
	log.Error("re-exec failed", "err", err)
	os.Exit(1)
}

func newUpdateManager(log *slog.Logger, cfg config.Config, paths map[string]string, restart func()) *update.Manager {
	m := &update.Manager{
		Installer: update.Installer{Paths: paths},
		Current:   buildinfo.Version,
		Mode:      cfg.AutoUpdate,
		Interval:  cfg.UpdateInterval,
		StageDir:  cfg.UpdateStageDir(),
		Restart:   restart,
		Log:       log,
	}
	keys, err := update.TrustedKeys()
	if err != nil {
		log.Error("update keys", "err", err)
		m.Mode = update.ModeOff
	}
	m.Source = &update.Source{APIBase: cfg.UpdateAPI, Repo: cfg.UpdateRepo, Keys: keys}
	if !buildinfo.IsRelease() {
		log.Info("development build: automatic updates are disabled")
	}
	return m
}

func updateCommand(ctx context.Context, log *slog.Logger, cfg config.Config, paths map[string]string, args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	checkOnly := fs.Bool("check", false, "only report whether a newer release exists")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if cfg.AutoUpdate == update.ModeOff {
		cfg.AutoUpdate = update.ModeCheck // an explicit command overrides "off"
	}
	m := newUpdateManager(log, cfg, paths, nil)
	if *checkOnly {
		if _, err := m.Check(ctx); err != nil {
			return err
		}
		s := m.State()
		if s.Available {
			fmt.Printf("%s is available (running %s): %s\n", s.Latest, s.Current, s.ReleaseURL)
		} else {
			fmt.Printf("%s is the latest release\n", s.Current)
		}
		return nil
	}
	if err := m.Apply(ctx); err != nil {
		if errors.Is(err, update.ErrUpToDate) {
			fmt.Printf("%s is the latest release\n", m.Current)
			return nil
		}
		return err
	}
	fmt.Printf("installed %s; restart remnantd to run it\n", m.State().Latest)
	return nil
}
