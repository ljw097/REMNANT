// Command remnantd is the REMNANT research server.
//
//	remnantd local                                   one-command local run (auto DB, opens the browser)
//	remnantd serve                                   run migrations, then serve the API and web UI
//	remnantd migrate                                 apply pending migrations and exit
//	remnantd setup -profile personal|team|managed -username NAME [-password-stdin]
//	                                                 first-run setup without the web UI
//	remnantd create-user  -username NAME -role ROLE  create a user and print an API token
//	remnantd create-token -username NAME [-name N] [-ttl 720h]
//	remnantd reset-password -username NAME [-password-stdin]
//	                                                 set a sign-in password (temporary if not given)
//	remnantd update [-check]                         install the latest signed release (or only report it)
//	remnantd version                                 print the version
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ljw097/remnant/internal/advisory"
	"github.com/ljw097/remnant/internal/api"
	"github.com/ljw097/remnant/internal/auth"
	"github.com/ljw097/remnant/internal/buildinfo"
	"github.com/ljw097/remnant/internal/config"
	"github.com/ljw097/remnant/internal/fetch"
	"github.com/ljw097/remnant/internal/orchestrator"
	"github.com/ljw097/remnant/internal/sandbox"
	"github.com/ljw097/remnant/internal/secrets"
	"github.com/ljw097/remnant/internal/store"
	webui "github.com/ljw097/remnant/web"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	cmd, args := "serve", os.Args[1:]
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cmd == "version" {
		fmt.Println(buildinfo.Version, buildinfo.Commit)
		return
	}
	paths, err := installedPaths()
	if err != nil {
		fmt.Fprintln(os.Stderr, "remnantd:", err)
		os.Exit(1)
	}
	if cmd == "serve" {
		guardUpdate(log, paths)
	}
	err = run(ctx, log, cmd, args, paths)
	stop()
	if errors.Is(err, errRestart) {
		restart(log, paths)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "remnantd:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger, cmd string, args []string, paths map[string]string) error {
	switch cmd {
	case "serve", "local", "migrate", "setup", "create-user", "create-token", "reset-password", "update":
	case "-h", "--help", "help":
		fmt.Println("usage: remnantd [serve|local|migrate|setup|create-user|create-token|reset-password|update|version] [flags]")
		return nil
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}

	// `local` runs REMNANT on this machine with one command: it fills local
	// defaults, brings up a database if needed, then serves and opens the
	// setup page.
	openLocal := false
	if cmd == "local" {
		if err := prepareLocalEnv(log); err != nil {
			return err
		}
		openLocal = true
		cmd = "serve"
	}

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	if cmd == "update" {
		return updateCommand(ctx, log, cfg, paths, args)
	}
	sealer, err := secrets.NewSealer(cfg.MasterKey)
	if err != nil {
		return err
	}
	st, err := store.Open(ctx, cfg.DatabaseURL, sealer)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer st.Close()

	applied, err := st.Migrate(ctx)
	if err != nil {
		return err
	}
	for _, v := range applied {
		log.Info("migration applied", "version", v)
	}

	switch cmd {
	case "migrate":
		return nil
	case "setup":
		return setupCommand(ctx, st, args)
	case "create-user":
		return createUser(ctx, st, args)
	case "create-token":
		return createToken(ctx, st, args)
	case "reset-password":
		return resetPassword(ctx, st, args)
	}
	if err := announceSetup(ctx, log, st, cfg); err != nil {
		return err
	}
	if openLocal {
		go openWhenReady(cfg.ListenAddr)
	}
	return serve(ctx, log, st, cfg, paths)
}

// announceSetup prints a one-time setup code while the instance has no
// users. Whoever completes setup in the web UI must have read the server's
// log, so reaching the port first is not enough to claim the instance.
func announceSetup(ctx context.Context, log *slog.Logger, st *store.Store, cfg config.Config) error {
	n, err := st.UserCount(ctx)
	if err != nil || n > 0 {
		return err
	}
	code, err := st.NewSetupCode(ctx, 24*time.Hour)
	if err != nil {
		return err
	}
	log.Warn("first-run setup required", "url", "http://"+cfg.ListenAddr+"/", "setup_code", code, "expires_in", "24h")
	fmt.Fprintf(os.Stderr, "\n  REMNANT first-run setup\n  Open http://%s/ and enter this setup code: %s\n  (or run: remnantd setup -profile personal|team|managed -username NAME)\n\n", cfg.ListenAddr, code)
	return nil
}

func setupCommand(ctx context.Context, st *store.Store, args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	profile := fs.String("profile", "", "personal (one researcher, self-approval recorded), team (cross-checked) or managed (administrators approve lab work and accounts)")
	mode := fs.String("mode", "", "older name for -profile (team or personal)")
	username := fs.String("username", "", "first administrator")
	name := fs.String("name", "", "instance name shown in the UI (optional)")
	pwStdin := fs.Bool("password-stdin", false, "read the administrator's password from standard input; without it a temporary password is printed")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *profile == "" {
		*profile = *mode
	}
	pol, ok := store.Presets[*profile]
	if !ok {
		return fmt.Errorf("-profile must be personal, team or managed")
	}
	password, temporary, err := readPassword(*username, *pwStdin)
	if err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	u, err := st.Setup(ctx, store.SetupInput{Policy: pol, Username: *username, PasswordHash: hash, MustChange: temporary, TrustedLocal: true})
	if err != nil {
		return err
	}
	if *name != "" {
		if err := st.SetSetting(ctx, "branding", map[string]string{"name": *name, "accent": ""}, u.ID); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "set up as %s; administrator %s.\n", *profile, u.Username)
	if temporary {
		fmt.Fprintln(os.Stderr, "Sign in with the temporary password below (shown once); you will be asked to change it.")
		fmt.Println(password)
	}
	return nil
}

// readPassword reads a password from standard input, or generates a
// temporary one when fromStdin is false.
func readPassword(username string, fromStdin bool) (password string, temporary bool, err error) {
	if !fromStdin {
		return auth.NewTemporaryPassword(), true, nil
	}
	b := make([]byte, auth.MaxPasswordLength+2)
	n, err := io.ReadFull(os.Stdin, b)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", false, err
	}
	password = strings.TrimRight(string(b[:n]), "\r\n")
	return password, false, auth.CheckPassword(username, password)
}

// resetPassword sets a user's sign-in password from the server, e.g. when
// the last administrator forgot theirs. It signs out their sessions.
func resetPassword(ctx context.Context, st *store.Store, args []string) error {
	fs := flag.NewFlagSet("reset-password", flag.ContinueOnError)
	username := fs.String("username", "", "existing user")
	pwStdin := fs.Bool("password-stdin", false, "read the new password from standard input; without it a temporary password is printed")
	if err := fs.Parse(args); err != nil {
		return err
	}
	u, err := st.UserByName(ctx, *username)
	if err != nil {
		return fmt.Errorf("user %q: %w", *username, err)
	}
	password, temporary, err := readPassword(u.Username, *pwStdin)
	if err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if err := st.SetPassword(ctx, u.ID, hash, temporary, 0); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "password for %s reset; their sessions were signed out.\n", u.Username)
	if temporary {
		fmt.Fprintln(os.Stderr, "Temporary password (shown once; it must be changed at sign-in):")
		fmt.Println(password)
	}
	return nil
}

func serve(ctx context.Context, log *slog.Logger, st *store.Store, cfg config.Config, paths map[string]string) error {
	// An installed update stops the server gracefully and asks main to
	// re-execute the new binary.
	ctx, stopServing := context.WithCancel(ctx)
	defer stopServing()
	var restartRequested atomic.Bool
	upd := newUpdateManager(log, cfg, paths, func() { restartRequested.Store(true); stopServing() })
	go upd.Run(ctx)
	go confirmUpdate(ctx, log, upd.Installer)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           api.New(st, cfg, log, upd, webui.Dist()),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout: SSE streams are long-lived and end with the run.
	}
	// The orchestrator stops with ctx; runs it is working are recorded as
	// interrupted before serve returns.
	orchCtx, stopOrch := context.WithCancel(ctx)
	defer stopOrch()
	orchDone := make(chan struct{})
	if cfg.Orchestrator {
		opts := orchestrator.Options{Concurrency: cfg.OrchestratorConcurrency, StaticOperations: []string{}}
		if len(cfg.RepoHosts) > 0 {
			f := &fetch.Fetcher{SnapshotDir: cfg.SnapshotDir(), AllowedHosts: cfg.RepoHosts}
			if cfg.AdvisoryFeed == "osv" {
				f.AdvisoryLookup = advisoryLookup(advisory.NewClient(cfg.AdvisoryAPI), log)
			}
			opts.Fetcher = f
		}
		if cfg.Worker == "bwrap" {
			sup, ops, err := startWorker(ctx, log, st, cfg)
			if err != nil {
				return err
			}
			opts.Worker, opts.StaticOperations = sup, ops
		} else {
			log.Warn("no worker runtime (REMNANT_WORKER=off): static analysis is skipped and lab experiments do not run")
		}
		o := orchestrator.New(st, cfg, log, opts)
		go func() { defer close(orchDone); o.Run(orchCtx) }()
	} else {
		log.Info("orchestrator disabled")
		close(orchDone)
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.ListenAddr)
		errc <- srv.ListenAndServe()
	}()
	var serveErr error
	select {
	case serveErr = <-errc:
	case <-ctx.Done():
	}
	stopOrch()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) && serveErr == nil {
		serveErr = err
	}
	<-orchDone
	if serveErr == nil && restartRequested.Load() {
		return errRestart
	}
	return serveErr
}

// startWorker builds the sandbox supervisor and keeps the configured static
// operations the sandboxed worker actually supports.
func startWorker(ctx context.Context, log *slog.Logger, st *store.Store, cfg config.Config) (*sandbox.Supervisor, []string, error) {
	bin := cfg.WorkerBin
	if bin == "" {
		self, err := os.Executable()
		if err != nil {
			return nil, nil, err
		}
		bin = filepath.Join(filepath.Dir(self), "remnant-worker")
	}
	sup, err := sandbox.New(sandbox.Config{
		WorkerBin: bin, WorkDir: cfg.WorkDir(), ArtifactDir: cfg.ArtifactDir(), RulesDir: cfg.WorkerRules,
		Resolve: orchestrator.SnapshotResolver(st, cfg), AllowWithoutCgroup: cfg.WorkerAllowNoCgroup, Log: log,
	})
	if err != nil {
		return nil, nil, err
	}
	caps, err := sup.Capabilities(ctx)
	if err != nil {
		return nil, nil, err
	}
	var ops []string
	for _, op := range cfg.StaticOperations {
		if slices.Contains(caps, op) {
			ops = append(ops, op)
		} else {
			log.Warn("static operation not available in the worker environment; skipping it", "operation", op)
		}
	}
	log.Info("worker runtime ready", "enforcement", sup.Enforcement(), "capabilities", caps, "static_operations", ops)
	return sup, ops, nil
}

func createUser(ctx context.Context, st *store.Store, args []string) error {
	fs := flag.NewFlagSet("create-user", flag.ContinueOnError)
	username := fs.String("username", "", "login name (lowercase)")
	role := fs.String("role", "researcher", "admin | researcher | reviewer")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !auth.Role(*role).Valid() {
		return fmt.Errorf("invalid role %q", *role)
	}
	u, err := st.CreateUser(ctx, *username, auth.Role(*role))
	if err != nil {
		return err
	}
	token, err := st.CreateToken(ctx, u.ID, "initial", 0)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "created %s (%s); the API token below is shown only once. For web sign-in run: remnantd reset-password -username %s\n", u.Username, u.Role, u.Username)
	fmt.Println(token)
	return nil
}

func createToken(ctx context.Context, st *store.Store, args []string) error {
	fs := flag.NewFlagSet("create-token", flag.ContinueOnError)
	username := fs.String("username", "", "existing user")
	name := fs.String("name", "cli", "label for the token")
	ttl := fs.Duration("ttl", 0, "lifetime, e.g. 720h (0 = no expiry)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	u, err := st.UserByName(ctx, *username)
	if err != nil {
		return fmt.Errorf("user %q: %w", *username, err)
	}
	token, err := st.CreateToken(ctx, u.ID, *name, *ttl)
	if err != nil {
		return err
	}
	fmt.Println(token)
	return nil
}

// advisoryLookup adapts an OSV client to the fetch hook: it infers the
// repository's packages from the clone, asks the feed for their advisories,
// and returns the fixing commits by SHA. It is best-effort — any error is
// logged and yields no enrichment, never a failed fetch.
func advisoryLookup(c *advisory.Client, log *slog.Logger) fetch.AdvisoryLookup {
	return func(ctx context.Context, repoURL, work string, _ func(...string) (string, error)) (map[string]fetch.AdvisoryNote, error) {
		pkgs := advisory.InferPackages(work)
		if len(pkgs) == 0 {
			return nil, nil
		}
		notes := map[string]fetch.AdvisoryNote{}
		for _, p := range pkgs {
			advs, err := c.ForPackage(ctx, p.Ecosystem, p.Name)
			if err != nil {
				log.Warn("advisory feed lookup failed", "repo", repoURL, "ecosystem", p.Ecosystem, "package", p.Name, "err", err)
				continue
			}
			for _, a := range advs {
				for _, sha := range a.FixedCommits {
					n := notes[sha]
					n.IDs = append(n.IDs, a.CVE())
					if n.Summary == "" {
						n.Summary = a.Summary
					}
					notes[sha] = n
				}
			}
		}
		if len(notes) > 0 {
			log.Info("advisory feed enriched intake", "repo", repoURL, "fix_commits", len(notes))
		}
		return notes, nil
	}
}
