package update

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// Modes of the in-process updater.
const (
	ModeOff     = "off"     // never contact the release source
	ModeCheck   = "check"   // check periodically and report; install only on request
	ModeInstall = "install" // install new releases and restart automatically
)

// State is what the API reports about updates.
type State struct {
	Current     string     `json:"current"`
	Mode        string     `json:"mode"`
	Latest      string     `json:"latest,omitempty"`
	ReleaseURL  string     `json:"release_url,omitempty"`
	Available   bool       `json:"available"`
	Status      string     `json:"status"` // idle | checking | installing | restarting | disabled
	LastChecked *time.Time `json:"last_checked,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
}

// Manager checks for and applies updates for a running remnantd.
type Manager struct {
	Source    *Source
	Installer Installer
	Current   string // running version; "dev" disables updates
	Mode      string
	Interval  time.Duration
	StageDir  string
	// Restart is called after a successful install. remnantd shuts down
	// gracefully and re-executes the new binary.
	Restart func()
	Log     *slog.Logger

	mu    sync.Mutex
	state State
	busy  bool
}

// ErrDisabled is returned when updates are off or this is a dev build.
var ErrDisabled = errors.New("updates are disabled")

// ErrUpToDate is returned by Apply when there is nothing newer.
var ErrUpToDate = errors.New("already running the latest release")

func (m *Manager) enabled() bool {
	_, err := ParseVersion(m.Current)
	return err == nil && m.Mode != ModeOff && m.Mode != ""
}

// State returns a snapshot of the updater state.
func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.state
	s.Current, s.Mode = m.Current, m.Mode
	if !m.enabled() {
		s.Status = "disabled"
	} else if s.Status == "" {
		s.Status = "idle"
	}
	return s
}

func (m *Manager) set(f func(*State)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f(&m.state)
}

// Run checks shortly after start and then every Interval; in install mode
// it applies what it finds.
func (m *Manager) Run(ctx context.Context) {
	if !m.enabled() {
		return
	}
	delay := min(time.Minute, m.Interval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = m.Interval
		var err error
		if m.Mode == ModeInstall {
			err = m.Apply(ctx)
		} else {
			_, err = m.Check(ctx)
		}
		if err != nil && !errors.Is(err, ErrUpToDate) && ctx.Err() == nil {
			m.Log.Warn("update check failed", "err", err)
		}
	}
}

// Check looks up the latest release and reports whether it is newer.
func (m *Manager) Check(ctx context.Context) (Release, error) {
	if !m.enabled() {
		return Release{}, ErrDisabled
	}
	m.set(func(s *State) { s.Status = "checking" })
	rel, err := m.Source.Latest(ctx)
	now := time.Now().UTC()
	cur, _ := ParseVersion(m.Current)
	m.set(func(s *State) {
		s.Status, s.LastChecked = "idle", &now
		if err != nil {
			s.LastError = err.Error()
			return
		}
		s.LastError, s.Latest, s.ReleaseURL = "", rel.Version.String(), rel.URL
		s.Available = rel.Version.Compare(cur) > 0
		if s.Available {
			m.Log.Info("update available", "current", m.Current, "latest", rel.Version.String(), "url", rel.URL)
		}
	})
	return rel, err
}

// Apply installs the latest release if it is newer, then calls Restart.
// Only one Apply runs at a time.
func (m *Manager) Apply(ctx context.Context) error {
	m.mu.Lock()
	if m.busy {
		m.mu.Unlock()
		return errors.New("an update is already in progress")
	}
	m.busy = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.busy = false; m.mu.Unlock() }()

	rel, err := m.Check(ctx)
	if err != nil {
		return err
	}
	cur, _ := ParseVersion(m.Current)
	if rel.Version.Compare(cur) <= 0 {
		return ErrUpToDate
	}
	m.set(func(s *State) { s.Status = "installing" })
	fail := func(err error) error {
		m.set(func(s *State) { s.Status, s.LastError = "idle", err.Error() })
		return err
	}
	b, err := m.Source.Download(ctx, rel, m.StageDir)
	if err != nil {
		return fail(err)
	}
	if err := m.Installer.Install(ctx, b, m.Current); err != nil {
		return fail(err)
	}
	m.Log.Info("update installed; restarting", "from", m.Current, "to", rel.Version.String())
	m.set(func(s *State) { s.Status = "restarting" })
	if m.Restart != nil {
		m.Restart()
	}
	return nil
}
