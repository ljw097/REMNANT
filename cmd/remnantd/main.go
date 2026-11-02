// Command remnantd is the REMNANT research server.
//
//	remnantd serve                                   run migrations, then serve the API
//	remnantd migrate                                 apply pending migrations and exit
//	remnantd create-user  -username NAME -role ROLE  create a user and print a first token
//	remnantd create-token -username NAME [-name N] [-ttl 720h]
//	remnantd update [-check]                         install the latest signed release (or only report it)
//	remnantd version                                 print the version
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ljw097/remnant/internal/api"
	"github.com/ljw097/remnant/internal/auth"
	"github.com/ljw097/remnant/internal/buildinfo"
	"github.com/ljw097/remnant/internal/config"
	"github.com/ljw097/remnant/internal/fetch"
	"github.com/ljw097/remnant/internal/orchestrator"
	"github.com/ljw097/remnant/internal/sandbox"
	"github.com/ljw097/remnant/internal/secrets"
	"github.com/ljw097/remnant/internal/store"
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
	case "serve", "migrate", "create-user", "create-token", "update":
	case "-h", "--help", "help":
		fmt.Println("usage: remnantd [serve|migrate|create-user|create-token|update|version] [flags]")
		return nil
	default:
		return fmt.Errorf("unknown command %q", cmd)
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
	case "create-user":
		return createUser(ctx, st, args)
	case "create-token":
		return createToken(ctx, st, args)
	}
	return serve(ctx, log, st, cfg, paths)
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
		Handler:           api.New(st, cfg, log, upd),
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
			opts.Fetcher = &fetch.Fetcher{SnapshotDir: cfg.SnapshotDir(), AllowedHosts: cfg.RepoHosts}
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
	fmt.Fprintf(os.Stderr, "created %s (%s); the token below is shown only once\n", u.Username, u.Role)
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
