package config

import "testing"

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
