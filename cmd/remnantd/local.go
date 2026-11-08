package main

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Local quickstart: run REMNANT on your own machine with one command. It
// fills sensible local defaults, brings up a PostgreSQL in Docker when no
// database is configured, and opens the first-run setup page in a browser.
const (
	localContainer = "remnant-db"
	localVolume    = "remnant-db-data"
	localDBPort    = "5433" // not 5432, to avoid clashing with a system Postgres
	localDBURL     = "postgres://remnant:remnant@127.0.0.1:" + localDBPort + "/remnant?sslmode=disable"
)

// localRepoHosts are the public code hosts a local run may fetch from, so
// "paste a repository URL" works without editing the environment. Setting
// REMNANT_REPO_HOSTS (even to an empty value) overrides it.
const localRepoHosts = "github.com,gitlab.com,codeberg.org,bitbucket.org"

// prepareLocalEnv sets the environment for a local run: a per-user data
// directory, loopback listener, updates off, the public code hosts allowed,
// and a database — the one in REMNANT_DATABASE_URL if set, otherwise a local
// Docker PostgreSQL.
func prepareLocalEnv(log *slog.Logger) error {
	if os.Getenv("REMNANT_DATA_DIR") == "" {
		dir, err := localDataDir()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		_ = os.Setenv("REMNANT_DATA_DIR", dir)
		log.Info("local data directory", "path", dir)
	}
	if os.Getenv("REMNANT_LISTEN") == "" {
		_ = os.Setenv("REMNANT_LISTEN", "127.0.0.1:8080")
	}
	if os.Getenv("REMNANT_AUTO_UPDATE") == "" {
		_ = os.Setenv("REMNANT_AUTO_UPDATE", "off")
	}
	if _, set := os.LookupEnv("REMNANT_REPO_HOSTS"); !set {
		_ = os.Setenv("REMNANT_REPO_HOSTS", localRepoHosts)
		log.Info("remote repositories allowed", "hosts", localRepoHosts)
	}
	if os.Getenv("REMNANT_DATABASE_URL") != "" {
		return nil
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("no database configured and Docker was not found.\n" +
			"Either start PostgreSQL yourself and set REMNANT_DATABASE_URL,\n" +
			"or install Docker so `remnantd local` can start one for you.")
	}
	if err := ensureLocalPostgres(log); err != nil {
		return err
	}
	_ = os.Setenv("REMNANT_DATABASE_URL", localDBURL)
	return nil
}

// localDataDir is a per-user directory for the master key, snapshots and
// artifacts (e.g. ~/.local/share/remnant, ~/Library/Application Support/remnant).
func localDataDir() (string, error) {
	if runtime.GOOS == "linux" {
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "remnant"), nil
		}
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".local", "share", "remnant"), nil
		}
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "remnant"), nil
}

// ensureLocalPostgres starts (or reuses) a local PostgreSQL container and
// waits until it accepts connections.
func ensureLocalPostgres(log *slog.Logger) error {
	exists, running := containerState()
	switch {
	case running:
		log.Info("local database already running", "container", localContainer)
	case exists:
		log.Info("starting local database", "container", localContainer)
		if out, err := docker("start", localContainer); err != nil {
			return fmt.Errorf("starting %s: %v: %s", localContainer, err, out)
		}
	default:
		log.Info("creating local database (Docker postgres:17)", "container", localContainer, "port", localDBPort)
		out, err := docker("run", "-d", "--name", localContainer,
			"-e", "POSTGRES_USER=remnant", "-e", "POSTGRES_PASSWORD=remnant", "-e", "POSTGRES_DB=remnant",
			"-v", localVolume+":/var/lib/postgresql/data",
			"-p", "127.0.0.1:"+localDBPort+":5432",
			"postgres:17")
		if err != nil {
			return fmt.Errorf("creating the database container: %v: %s", err, out)
		}
	}
	// Wait for the port, then for the server to be ready for queries.
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.DialTimeout("tcp", "127.0.0.1:"+localDBPort, time.Second); err == nil {
			_ = c.Close()
			if out, err := docker("exec", localContainer, "pg_isready", "-U", "remnant", "-q"); err == nil {
				return nil
			} else if strings.TrimSpace(out) != "" {
				// still starting up
			}
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("the local database did not become ready within 60s (check: docker logs %s)", localContainer)
}

func containerState() (exists, running bool) {
	out, err := docker("inspect", "-f", "{{.State.Running}}", localContainer)
	if err != nil {
		return false, false
	}
	return true, strings.TrimSpace(out) == "true"
}

func docker(args ...string) (string, error) {
	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// openBrowser best-effort opens url in the default browser.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// openWhenReady waits for the server's health endpoint, then opens the
// browser. Used by `remnantd local`.
func openWhenReady(listen string) {
	url := "http://" + listen + "/"
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", listen, time.Second)
		if err == nil {
			_ = c.Close()
			time.Sleep(300 * time.Millisecond)
			fmt.Fprintf(os.Stderr, "\n  REMNANT is running locally at %s\n  Opening it in your browser…\n\n", url)
			openBrowser(url)
			return
		}
		time.Sleep(400 * time.Millisecond)
	}
}
