// Package config loads remnantd settings from the process environment.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
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
}

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

	for _, h := range strings.Split(getenv("REMNANT_REPO_HOSTS"), ",") {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			cfg.RepoHosts = append(cfg.RepoHosts, h)
		}
	}
	return cfg, nil
}
