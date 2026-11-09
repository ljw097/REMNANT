package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A database address given once is saved and used by later runs.
func TestLocalRemembersDatabase(t *testing.T) {
	dir := t.TempDir()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	t.Setenv("REMNANT_DATA_DIR", dir)
	t.Setenv("REMNANT_DATABASE_URL", "postgres://u:p@127.0.0.1:5433/remnant")
	if err := prepareLocalEnv(log); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, savedDatabaseFile))
	if err != nil || strings.TrimSpace(string(b)) != "postgres://u:p@127.0.0.1:5433/remnant" {
		t.Fatalf("saved %q %v", b, err)
	}
	if fi, _ := os.Stat(filepath.Join(dir, savedDatabaseFile)); fi.Mode().Perm() != 0o600 {
		t.Fatalf("saved with mode %v", fi.Mode().Perm())
	}
	t.Setenv("REMNANT_DATABASE_URL", "")
	os.Unsetenv("REMNANT_DATABASE_URL")
	if err := prepareLocalEnv(log); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("REMNANT_DATABASE_URL"); got != "postgres://u:p@127.0.0.1:5433/remnant" {
		t.Fatalf("next run uses %q", got)
	}
}

func TestDockerNeedsRootHint(t *testing.T) {
	if dockerNeedsRoot("some other failure") != nil {
		t.Fatal("hint for an unrelated failure")
	}
	err := dockerNeedsRoot("docker: permission denied while trying to connect to the Docker daemon socket")
	if err == nil || !strings.Contains(err.Error(), "sudo docker run -d --name remnant-db") || !strings.Contains(err.Error(), "REMNANT_DATABASE_URL='"+localDBURL+"'") {
		t.Fatalf("hint %v", err)
	}
}
