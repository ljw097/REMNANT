package config

import (
	"os"
	"path/filepath"
	"testing"
)

func env(m map[string]string) func(string) string {
	base := map[string]string{"REMNANT_DATABASE_URL": "postgres://x", "REMNANT_MASTER_KEY": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}
	for k, v := range m {
		base[k] = v
	}
	return func(k string) string { return base[k] }
}

func TestDefaults(t *testing.T) {
	c, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.ListenAddr != "127.0.0.1:8080" || !c.Orchestrator || c.Worker != "off" || len(c.StaticOperations) != 2 || len(c.RepoHosts) != 0 ||
		c.AutoUpdate != "check" || c.UpdateInterval.Hours() != 6 || c.UpdateRepo != "ljw097/REMNANT" {
		t.Fatalf("defaults = %+v", c)
	}
}

func TestRejects(t *testing.T) {
	for name, m := range map[string]map[string]string{
		"no database":      {"REMNANT_DATABASE_URL": ""},
		"short key":        {"REMNANT_MASTER_KEY": "AAAA"},
		"bad worker":       {"REMNANT_WORKER": "docker"},
		"lab op as static": {"REMNANT_STATIC_OPERATIONS": "lab.unit_test"},
		"unknown op":       {"REMNANT_STATIC_OPERATIONS": "static.magic"},
		"bad concurrency":  {"REMNANT_ORCHESTRATOR_CONCURRENCY": "0"},
		"bad orchestrator": {"REMNANT_ORCHESTRATOR": "maybe"},
		"bad update mode":  {"REMNANT_AUTO_UPDATE": "always"},
		"short interval":   {"REMNANT_UPDATE_INTERVAL": "1m"},
	} {
		if _, err := Load(env(m)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestMasterKeyFile(t *testing.T) {
	dir := t.TempDir()
	get := env(map[string]string{"REMNANT_MASTER_KEY": "", "REMNANT_DATA_DIR": dir})
	a, err := Load(get)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, "master.key"))
	if err != nil || fi.Mode().Perm() != 0o600 || len(a.MasterKey) != 32 {
		t.Fatalf("key file %v %v, key %d bytes", fi, err, len(a.MasterKey))
	}
	b, err := Load(get)
	if err != nil || string(b.MasterKey) != string(a.MasterKey) {
		t.Fatalf("second load: %v, same key %v", err, string(b.MasterKey) == string(a.MasterKey))
	}
	if err := os.Chmod(filepath.Join(dir, "master.key"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(get); err == nil {
		t.Fatal("a world-readable key file was accepted")
	}
}
