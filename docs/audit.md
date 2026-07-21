# Audit

The broker writes one JSON record per tool call. Recording is always on for
servers whose `audit.enabled` is `true`; the destination is controlled by
the `--audit` flag.

## Destinations

| Mode | Flag | Sink |
|------|------|------|
| Off | `--audit=off` | No records |
| JSONL | `--audit=jsonl` (default) | `~/.neuralgentics/broker_audit.jsonl` |
| JSONL + Postgres | `--audit=jsonl+pg` | JSONL + `broker_audit_log` table |

Override the JSONL path with `--audit-jsonl-path=PATH` and the Postgres DSN
with `--audit-pg-url=DSN`.

## Postgres schema

```sql
CREATE TABLE broker_audit_log (
    id BIGSERIAL PRIMARY KEY,
    ts TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    agent_role TEXT,
    server TEXT NOT NULL,
    tool TEXT NOT NULL,
    args_hash TEXT,
    success BOOLEAN NOT NULL,
    result_size INTEGER,
    duration_ms INTEGER NOT NULL,
    error TEXT
);
```

The `neuralgentics-web` `broker-audit` module can read this table and render
it as a live dashboard.

## Truncation

Tool args and results are truncated before they are hashed/recorded so a
single oversized call cannot blow up the audit log. Defaults:

- `--audit-args-truncate=4096` (bytes)
- `--audit-result-truncate=8192` (bytes)

Per-server overrides live in the `audit` block of the YAML config; see
[Configuration](configuration.md#yaml-config).

## T-117.5 race-fix note

Before v0.1.0 the broker had a data race between the background watcher
goroutine (which nils out a `ServerEntry`'s process and pipes after the
subprocess exits) and the call path (which reads those same fields to send
the next request). The race manifested under load when a server was
restarted mid-session.

The fix is the locked accessors `SetRuntime` and `ClearRuntime` on
`ServerEntry` (in `src/neuralgentics/broker/registry/registry.go`). Every
write of the process handle, stdin pipe, stdout pipe, or tool list goes
through `SetRuntime`; every nil-out goes through `ClearRuntime`; every
read on the call path takes the entry mutex and reads a consistent tuple.
There are no bare field reads of `entry.Process` or `entry.Stdin` on the
hot path anymore.

The race was first reproduced by `TestReload_*` in
`src/neuralgentics/broker/reload_test.go` and the health watcher test in
`src/neuralgentics/broker/launcher/launcher_health_test.go`; both now use
`ClearRuntime` to tear down instead of racing the reader.