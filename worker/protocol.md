# REMNANT Worker Protocol v1

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
    "commit_sha": "<immutable-commit>"
  },
  "inputs": {
    "rule_pack": "builtin-c-reviewed-v1",
    "file_globs": ["**/*.c", "**/*.h"]
  },
  "limits": {
    "wall_seconds": 180,
    "memory_mb": 2048,
    "cpu_cores": 2,
    "max_output_bytes": 5000000
  },
  "policy": {
    "filesystem": "workspace_read_only_output_write_only",
    "network": "none",
    "subprocess": "allowlisted_tool_only",
    "max_processes": 32
  }
}
```

The caller does not provide an executable path, shell command, arbitrary environment variables, arbitrary mount path or network destination. `operation` resolves through a hard-coded backend allowlist.

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
- Do not mount host home directories, SSH agents, cloud credentials, browser profiles or ARTEX configuration files.
- Worker must reject symlink traversal outside the mounted source root.
- The backend validates every returned path, digest, line range and artifact size before accepting output.
- Dynamic tests are local-lab-only and require a separate human approval bound to target commit + policy hash. The worker protocol does not support remote host/URL targets.
- Cancellation: supervisor sends SIGTERM, waits a short grace period, then kills the container/cgroup. The run state records cancellation and the final resource accounting.
