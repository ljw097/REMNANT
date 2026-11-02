// Command remnant-worker runs inside the worker sandbox started by remnantd.
//
//	remnant-worker run            read one request line on stdin, answer on stdout
//	remnant-worker capabilities   print the operations this environment supports
//
// It is not meant to be run directly on a host: it trusts that its
// filesystem view, network and resource limits were set up by the sandbox.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ljw097/remnant/internal/worker"
	"github.com/ljw097/remnant/internal/workerexec"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: remnant-worker run|capabilities [flags]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "capabilities":
		_ = json.NewEncoder(os.Stdout).Encode(workerexec.Capabilities())
	case "run":
		if err := run(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "remnant-worker:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "remnant-worker: unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	env := workerexec.Env{}
	fs.StringVar(&env.Workspace, "workspace", "/workspace", "read-only source root")
	fs.StringVar(&env.Out, "out", "/out", "artifact directory")
	fs.StringVar(&env.Scratch, "scratch", "/tmp", "writable scratch directory")
	fs.StringVar(&env.RulesDir, "rules", "/opt/remnant/rules", "semgrep rule packs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := os.Stat(env.RulesDir); err != nil {
		env.RulesDir = ""
	}

	line, err := bufio.NewReaderSize(os.Stdin, 1<<20).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return fmt.Errorf("reading request: %w", err)
	}
	var req worker.Request
	if err := json.Unmarshal(line, &req); err != nil {
		return fmt.Errorf("decoding request: %w", err)
	}
	if err := applyLimits(req.Limits); err != nil {
		return fmt.Errorf("applying limits: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	return workerexec.Run(ctx, env, req, os.Stdout)
}

// applyLimits sets per-process limits that every child inherits. Memory,
// task count and CPU share are enforced by the sandbox's cgroup.
func applyLimits(l worker.Limits) error {
	set := func(res int, v uint64) error {
		var cur syscall.Rlimit
		if err := syscall.Getrlimit(res, &cur); err != nil {
			return err
		}
		v = min(v, cur.Max)
		return syscall.Setrlimit(res, &syscall.Rlimit{Cur: v, Max: v})
	}
	if err := set(syscall.RLIMIT_CORE, 0); err != nil {
		return err
	}
	if l.MaxOutputBytes > 0 {
		if err := set(syscall.RLIMIT_FSIZE, uint64(l.MaxOutputBytes)); err != nil {
			return err
		}
	}
	if l.WallSeconds > 0 && l.CPUCores > 0 {
		if err := set(syscall.RLIMIT_CPU, uint64(l.WallSeconds*l.CPUCores+30)); err != nil {
			return err
		}
	}
	return set(syscall.RLIMIT_NOFILE, 4096)
}
