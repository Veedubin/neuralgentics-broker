# neuralgentics-broker

A Go MCP (Model Context Protocol) broker with built-in tool-call audit, hot-
reload, and an optional transport swap to route outbound HTTP through a
centralized egress proxy.

`go install github.com/Veedubin/neuralgentics-broker/cmd/broker@latest` gives
you a `broker` command.

## What it is

`neuralgentics-broker` is a long-running daemon that:
- Spawns and supervises MCP server subprocesses (stdio, HTTP/SSE)
- Proxies JSON-RPC calls between a client (e.g., an LLM agent) and the
  servers
- Records every tool call to JSONL and/or Postgres
- Optionally routes the servers' outbound HTTP through the
  `neuralgentics-gateway` egress proxy
- Supports hot-reload of the server config without dropping in-flight
  connections

## What this is NOT

- Not a memory store. (Use [memini-ai](https://github.com/Veedubin/memini-ai-dev)
  for that.)
- Not a proxy / egress gateway. (Use [neuralgentics-gateway](https://github.com/Veedubin/neuralgentics_gateway)
  for that.)
- Not a web UI. (Use [neuralgentics-web](https://github.com/Veedubin/neuralgentics-web)
  for that.)

These products can be used together, but each is independent.

## Install

```bash
go install github.com/Veedubin/neuralgentics-broker/cmd/broker@latest
```

This puts the `broker` binary in `$GOPATH/bin` (or `$HOME/go/bin` by default).
Make sure that's on your `$PATH`.

## Quickstart

```bash
# 1. Create a servers config (see "Config" below)
cat > servers.yaml <<'EOF'
servers:
  - name: filesystem
    command: npx
    args: [-y, @modelcontextprotocol/server-filesystem, /tmp]
  - name: github
    command: npx
    args: [-y, @modelcontextprotocol/server-github]
    env:
      GITHUB_PERSONAL_ACCESS_TOKEN: $GITHUB_TOKEN
EOF

# 2. Start the broker
broker --config=servers.yaml

# 3. The broker exposes an MCP server on stdio. Wire your agent to it.
#    (e.g., set the agent's MCP_SERVER_URL to the broker's listen address)
```

## Config

The broker takes a YAML file describing the MCP servers to spawn:

```yaml
servers:
  - name: my-server
    command: /path/to/mcp-server
    args: [--port, 8080]
    env:
      MY_VAR: my-value
    transport: stdio  # or http/sse
    health_check:
      interval: 30s
      timeout: 5s
    audit:
      enabled: true
      truncate_args: 4096
      truncate_result: 8192
```

## CLI flags

- `--config=PATH` — path to the YAML config (required)
- `--audit=off|jsonl|jsonl+pg` — where to write tool-call audit records
  (default `jsonl`)
- `--audit-jsonl-path=PATH` — JSONL output (default `~/.neuralgentics/broker_audit.jsonl`)
- `--audit-pg-url=DSN` — Postgres DSN for `jsonl+pg` mode
- `--audit-flush-interval=DURATION` — buffered write flush interval (default `1s`)
- `--audit-args-truncate=BYTES` — truncate tool args to this size (default `4096`)
- `--audit-result-truncate=BYTES` — truncate tool results to this size (default `8192`)
- `--egress-gateway-url=URL` — route outbound HTTP through this gateway
  (env var: `EGRESS_GATEWAY_URL`)
- `--rpc-timeout=DURATION` — per-RPC timeout (default `30s`)

## Audit

The broker writes one JSON record per tool call to:

- **JSONL** at `~/.neuralgentics/broker_audit.jsonl` by default
- **Postgres** table `broker_audit_log` (if `--audit-pg-url` is set and `--audit=jsonl+pg`)

The schema is:

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

## Egress gateway integration

If you run a `neuralgentics-gateway` and want the broker's outbound HTTP to
go through it (for policy enforcement + audit), set:

```bash
export EGRESS_GATEWAY_URL=http://localhost:9090
broker --config=servers.yaml
```

The broker's `HTTPClient` detects the env var and swaps the transport to a
proxy-aware one. When `EGRESS_GATEWAY_URL` is empty, the broker uses the
default transport (no proxying).

## Hot-reload

Send the broker process a `SIGHUP` to reload the config without dropping
in-flight connections:

```bash
kill -HUP $(pgrep broker)
```

New servers are started; removed servers are stopped; existing servers with
changed env vars get a restart (with a 5s drain window).

## As a library

Importable as a Go module:

```go
import "github.com/Veedubin/neuralgentics-broker/src/neuralgentics/broker"

b := broker.New(configPath, broker.WithAuditWriter(myWriter))
go b.Start()
defer b.Stop()
```

See `cmd/broker/main.go` for a reference implementation.

## License

MIT — see [LICENSE](LICENSE).