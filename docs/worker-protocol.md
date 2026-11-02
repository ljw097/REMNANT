# REMNANT Worker Protocol v1

The supervisor (`internal/sandbox`) starts `remnant-worker run` inside the sandbox described under *Required enforcement*, writes one request line to its stdin and reads protocol lines from its stdout.

Transport: one JSON object per line over stdin/stdout (JSONL). `stderr` is reserved for diagnostic logs. Stdout must contain protocol messages only. The Go supervisor owns process creation, timeouts, cancellation and artifact registration.

## Request

```json
{
  "protocol": "remnant.worker.v1",
  "message": "request",
  "request_id": "req_01J...",
  "run_id": 42,
  "operation": "static.semgrep",
  "workspace": {
    "mount_id": "target_12",
    "relative_root": ".",
    "revision": "<commit sha or sha256:tree digest>"
  },
  "inputs": {
    "rule_pack": "builtin-c-reviewed-v1",
    "file_globs": ["**/*.c", "**/*.h"]
  },
  "limits": {
    "wall_seconds": 180,
    "memory_mb": 2048,
    "cpu_cores": 2,
    "max_output_bytes": 5000000,
    "max_processes": 32
  },
  "policy": {
    "filesystem": "workspace_read_only_output_write_only",
    "network": "none",
    "subprocess": "allowlisted_tool_only"
  }
}
```

The caller does not provide an executable path, shell command, arbitrary environment variables, arbitrary mount path or network destination. `operation` resolves through the hard-coded allowlist in `internal/worker/protocol.go`:

| Operation | Needs `approval_id` |
|---|---|
| `index.symbols`, `static.patterns`, `static.semgrep`, `static.codeql`, `static.tree_sitter` | no |
| `lab.unit_test`, `lab.asan_test`, `lab.ubsan_test`, `lab.libfuzzer` | yes: an `approved` approval for this run |

`policy` must equal the fixed value above. `worker.ValidateRequest` rejects anything else before a worker is started.

## Progress event

```json
{
  "protocol": "remnant.worker.v1",
  "message": "progress",
  "request_id": "req_01J...",
  "stage": "semgrep",
  "percent": 45,
  "summary": "Analyzed 45 of 100 source files"
}
```

## Final result

```json
{
  "protocol": "remnant.worker.v1",
  "message": "result",
  "request_id": "req_01J...",
  "status": "completed",
  "exit_code": 0,
  "summary": "Analysis completed; 2 candidate diagnostics emitted",
  "observations": [
    {
      "kind": "static_finding",
      "rule_id": "REMNANT-C-STATE-001",
      "file_path": "src/parser.c",
      "line_start": 120,
      "line_end": 133,
      "message": "State publication and cleanup ordering requires review",
      "tool_confidence": "medium"
    }
  ],
  "artifacts": [
    {
      "name": "semgrep.sarif",
      "media_type": "application/sarif+json",
      "sha256": "<sha256>",
      "size_bytes": 18392
    }
  ]
}
```

## Error result

```json
{
  "protocol": "remnant.worker.v1",
  "message": "error",
  "request_id": "req_01J...",
  "code": "RESOURCE_LIMIT",
  "retryable": false,
  "summary": "Worker exceeded the configured memory limit"
}
```

## Required enforcement

- Mount source snapshots read-only; provide a separate write-only output directory.
- Disable network at the container/network-namespace layer, not only by prompt or environment variable.
- Use a non-root UID, dropped Linux capabilities, seccomp/AppArmor or equivalent, PID/CPU/memory/time limits, and no Docker socket mount.
- Do not mount host home directories, SSH agents, cloud credentials, browser profiles, or REMNANT configuration, database credentials or `REMNANT_MASTER_KEY`.
- Worker must reject symlink traversal outside the mounted source root.
- The backend validates every returned path, digest, line range and artifact size before accepting output (`worker.ValidateResult`): paths must be clean, relative and inside the root; `status` is `completed`, `finding` or `failed`; artifact names match `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`; digests are lowercase SHA-256 hex; total artifact size is within `max_output_bytes`.
- Dynamic tests are local-lab-only and require a separate human approval bound to the lab scope hash (run scope, target revision, limits). The worker protocol does not support remote host/URL targets.
- Cancellation: supervisor sends SIGTERM, waits a short grace period, then kills the container/cgroup. The run state records cancellation and the final resource accounting.

## Operations implemented by remnant-worker

| Operation | Behaviour |
|---|---|
| `index.symbols` | heuristic symbol index of the workspace → `symbols.json` |
| `static.patterns` | built-in, reviewed line rules (unbounded copies, non-literal format strings, shell execution, `pickle.loads`, `eval`, `unsafe`, …) → observations and `patterns.json`; `finding` if any rule matched |
| `static.semgrep` | `semgrep scan` with `rule_pack` from the mounted rules directory only (`--metrics off`, no registry); SARIF parsed into observations. `UNSUPPORTED` when semgrep is not installed |
| `static.codeql`, `static.tree_sitter` | `UNSUPPORTED` in this build |
| `lab.*` | runs the snapshot's lab harness (below) |

`remnant-worker capabilities` prints the operations the environment supports; remnantd drops configured static operations the worker cannot run.

## Lab harness contract

- The harness is the file `.remnant/lab/<name>` in the target snapshot, where `<name>` is the operation without `lab.` (`unit_test`, `asan_test`, `ubsan_test`, `libfuzzer`). It is part of the pinned revision, so approving a scope approves that exact harness.
- It runs with `/bin/sh` in a writable copy of the workspace (at most 512 MiB, `.git` excluded) inside the sandbox's tmpfs, with `TMPDIR`, `REMNANT_REQUEST` (JSON with the operation and inputs: hypothesis ID, affected symbols, preconditions, expected observation), `REMNANT_OUT` (files written there become `lab-<name>` artifacts), and sanitizer options set.
- Outcome: a sanitizer or libFuzzer report in the output, or a `crash-*`/`leak-*`/`timeout-*`/`oom-*` file in `REMNANT_OUT`, is a `finding` with observations located at the first stack frame inside the workspace. Otherwise exit 10 is a `finding` (the harness observed the expected behaviour itself), exit 0 is `completed` (not reproduced), anything else is `failed`. The combined output is `lab.log` (at most 8 MiB).
- The harness gets `wall_seconds − 10 s` (at least 5 s); on timeout its whole process group is killed.

## Error codes

| Code | Meaning | Orchestrator treatment |
|---|---|---|
| `UNSUPPORTED` | operation or tool not available | not run |
| `INVALID_INPUT` | bad input, e.g. no harness, unknown rule pack | not run |
| `WORKSPACE_UNAVAILABLE` | snapshot missing or changed since pinning | not run |
| `RESOURCE_LIMIT` | wall clock, memory/task limit, output size | `resource_limited` |
| `CANCELLED` | request cancelled | `cancelled` |
| `INTERNAL` | anything else | `failed` |

## Implementation of the enforcement

`internal/sandbox` runs `systemd-run --user --scope` with `MemoryMax`, `MemorySwapMax=0`, `TasksMax` and `CPUQuota` from the request, around `bwrap --unshare-all --unshare-user --disable-userns --die-with-parent --new-session --clearenv --cap-drop ALL`. The sandbox sees `/usr` (and the `/bin`, `/lib*` links) and the dynamic-linker configuration read-only, the snapshot read-only at `/workspace`, an empty `/out`, a size-capped tmpfs at `/tmp`, and the worker binary — nothing else from the host. The worker sets `RLIMIT_CPU`, `RLIMIT_FSIZE`, `RLIMIT_CORE=0` and `RLIMIT_NOFILE` before running anything. The supervisor caps protocol output at 16 MiB, kills the process group on cancel or after `wall_seconds + 15 s`, re-hashes every artifact and refuses symlinks or size mismatches before importing them into the artifact store. Without a usable user cgroup scope remnantd refuses to start the worker runtime unless `REMNANT_WORKER_ALLOW_NO_CGROUP=1`.

`internal/sandbox/sandbox_test.go` runs an isolation probe inside the sandbox (network, loopback, writable workspace, home directories, `/etc`, user bus, container socket, capabilities, nested user namespaces, inherited environment) and also on the host, to show the probe detects what it checks for.
