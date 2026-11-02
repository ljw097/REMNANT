// Package config loads remnantd settings from the process environment.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ljw097/remnant/internal/worker"
)

// Config is the complete runtime configuration. Every field has a single
// environment variable; there is no config file to keep secrets out of the
// working tree.
type Config struct {
	ListenAddr  string   // REMNANT_LISTEN, default 127.0.0.1:8080
	DatabaseURL string   // REMNANT_DATABASE_URL, required
	MasterKey   []byte   // REMNANT_MASTER_KEY, base64 of 32 random bytes, required
	DataDir     string   // REMNANT_DATA_DIR, default ./data
	RepoHosts   []string // REMNANT_REPO_HOSTS, comma separated; empty disables remote repositories

	Orchestrator            bool // REMNANT_ORCHESTRATOR, "off" disables the in-process orchestrator
	OrchestratorConcurrency int  // REMNANT_ORCHESTRATOR_CONCURRENCY, runs worked on at once, default 2

	Worker              string   // REMNANT_WORKER: "off" (default) or "bwrap"
	WorkerBin           string   // REMNANT_WORKER_BIN, default: remnant-worker next to remnantd
	WorkerRules         string   // REMNANT_WORKER_RULES, semgrep rule pack directory (optional)
	WorkerAllowNoCgroup bool     // REMNANT_WORKER_ALLOW_NO_CGROUP=1 accepts running without cgroup limits
	StaticOperations    []string // REMNANT_STATIC_OPERATIONS, default static.patterns,static.semgrep

	AutoUpdate     string        // REMNANT_AUTO_UPDATE: off | check (default) | install
	UpdateInterval time.Duration // REMNANT_UPDATE_INTERVAL, default 6h, at least 10m
	UpdateRepo     string        // REMNANT_UPDATE_REPO, default ljw097/REMNANT
	UpdateAPI      string        // REMNANT_UPDATE_API, default https://api.github.com
}

// UpdateStageDir holds a downloaded release while it is verified.
func (c Config) UpdateStageDir() string { return filepath.Join(c.DataDir, "update-stage") }

// WorkDir holds per-request worker output while a request runs.
func (c Config) WorkDir() string { return filepath.Join(c.DataDir, "work") }

// SnapshotDir is the only directory local snapshots may be read from.
func (c Config) SnapshotDir() string { return filepath.Join(c.DataDir, "snapshots") }

// ArtifactDir holds artifact bytes written by remnantd (indexes, reports).
func (c Config) ArtifactDir() string { return filepath.Join(c.DataDir, "artifacts") }

// Load reads the configuration through getenv (os.Getenv in production).
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		ListenAddr:  getenv("REMNANT_LISTEN"),
		DatabaseURL: getenv("REMNANT_DATABASE_URL"),
		DataDir:     getenv("REMNANT_DATA_DIR"),
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:8080"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "data"
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("REMNANT_DATABASE_URL is required")
	}

	rawKey := getenv("REMNANT_MASTER_KEY")
	if rawKey == "" {
		return Config{}, errors.New("REMNANT_MASTER_KEY is required (base64 of 32 random bytes)")
	}
	key, err := base64.StdEncoding.DecodeString(rawKey)
	if err != nil {
		return Config{}, fmt.Errorf("REMNANT_MASTER_KEY: %w", err)
	}
	if len(key) != 32 {
		return Config{}, fmt.Errorf("REMNANT_MASTER_KEY must decode to 32 bytes, got %d", len(key))
	}
	cfg.MasterKey = key

	switch v := getenv("REMNANT_ORCHESTRATOR"); v {
	case "", "on":
		cfg.Orchestrator = true
	case "off":
	default:
		return Config{}, fmt.Errorf("REMNANT_ORCHESTRATOR must be on or off, got %q", v)
	}
	cfg.OrchestratorConcurrency = 2
	if v := getenv("REMNANT_ORCHESTRATOR_CONCURRENCY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 16 {
			return Config{}, fmt.Errorf("REMNANT_ORCHESTRATOR_CONCURRENCY must be 1–16, got %q", v)
		}
		cfg.OrchestratorConcurrency = n
	}

	switch cfg.Worker = getenv("REMNANT_WORKER"); cfg.Worker {
	case "":
		cfg.Worker = "off"
	case "off", "bwrap":
	default:
		return Config{}, fmt.Errorf("REMNANT_WORKER must be off or bwrap, got %q", cfg.Worker)
	}
	cfg.WorkerBin = getenv("REMNANT_WORKER_BIN")
	cfg.WorkerRules = getenv("REMNANT_WORKER_RULES")
	cfg.WorkerAllowNoCgroup = getenv("REMNANT_WORKER_ALLOW_NO_CGROUP") == "1"
	ops := getenv("REMNANT_STATIC_OPERATIONS")
	if ops == "" {
		ops = "static.patterns,static.semgrep"
	}
	for _, op := range strings.Split(ops, ",") {
		op = strings.TrimSpace(op)
		if spec, ok := worker.Operations[op]; !ok || spec.RequiresApproval || !strings.HasPrefix(op, "static.") {
			return Config{}, fmt.Errorf("REMNANT_STATIC_OPERATIONS: %q is not a static operation", op)
		}
		cfg.StaticOperations = append(cfg.StaticOperations, op)
	}

	switch cfg.AutoUpdate = getenv("REMNANT_AUTO_UPDATE"); cfg.AutoUpdate {
	case "":
		cfg.AutoUpdate = "check"
	case "off", "check", "install":
	default:
		return Config{}, fmt.Errorf("REMNANT_AUTO_UPDATE must be off, check or install, got %q", cfg.AutoUpdate)
	}
	cfg.UpdateInterval = 6 * time.Hour
	if v := getenv("REMNANT_UPDATE_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 10*time.Minute {
			return Config{}, fmt.Errorf("REMNANT_UPDATE_INTERVAL must be a duration of at least 10m, got %q", v)
		}
		cfg.UpdateInterval = d
	}
	if cfg.UpdateRepo = getenv("REMNANT_UPDATE_REPO"); cfg.UpdateRepo == "" {
		cfg.UpdateRepo = "ljw097/REMNANT"
	}
	if cfg.UpdateAPI = getenv("REMNANT_UPDATE_API"); cfg.UpdateAPI == "" {
		cfg.UpdateAPI = "https://api.github.com"
	}

	for _, h := range strings.Split(getenv("REMNANT_REPO_HOSTS"), ",") {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			cfg.RepoHosts = append(cfg.RepoHosts, h)
		}
	}
	return cfg, nil
}
