# Configuration

The broker reads a YAML file describing the MCP servers to spawn, plus a set
of CLI flags that tune audit, transport, and timeouts.

## YAML config

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

### Server fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Unique server name; used in audit records and routing |
| `command` | string | Executable to launch |
| `args` | []string | Arguments passed to `command` |
| `env` | map[string]string | Environment variables for the subprocess |
| `transport` | string | `stdio` (default), `http`, or `sse` |
| `health_check.interval` | duration | Health probe interval (e.g. `30s`) |
| `health_check.timeout` | duration | Health probe timeout (e.g. `5s`) |
| `audit.enabled` | bool | Record tool calls for this server |
| `audit.truncate_args` | int | Max bytes of tool args to record |
| `audit.truncate_result` | int | Max bytes of tool result to record |

## CLI flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config=PATH` | (required) | Path to the YAML config |
| `--audit=off\|jsonl\|jsonl+pg` | `jsonl` | Where to write tool-call audit records |
| `--audit-jsonl-path=PATH` | `~/.neuralgentics/broker_audit.jsonl` | JSONL output path |
| `--audit-pg-url=DSN` | (none) | Postgres DSN for `jsonl+pg` mode |
| `--audit-flush-interval=DURATION` | `1s` | Buffered write flush interval |
| `--audit-args-truncate=BYTES` | `4096` | Truncate tool args to this size |
| `--audit-result-truncate=BYTES` | `8192` | Truncate tool results to this size |
| `--egress-gateway-url=URL` | (none) | Route outbound HTTP through this gateway |
| `--rpc-timeout=DURATION` | `30s` | Per-RPC timeout |

## Egress gateway

The broker's `HTTPClient` detects the `EGRESS_GATEWAY_URL` environment
variable (or the `--egress-gateway-url` flag) and swaps the transport to a
proxy-aware one. When the value is empty, the broker uses the default
transport (no proxying), preserving pre-v0.13.0 behavior.

```bash
export EGRESS_GATEWAY_URL=http://localhost:9090
broker --config=servers.yaml
```

An invalid gateway URL falls back to the default transport so a
misconfiguration never hard-breaks broker calls. See
[neuralgentics-gateway](https://github.com/Veedubin/neuralgentics_gateway)
for the proxy/audit side.

## Hot-reload

Send the broker process a `SIGHUP` to reload the config without dropping
in-flight connections:

```bash
kill -HUP $(pgrep broker)
```

New servers are started; removed servers are stopped; existing servers with
changed env vars get a restart (with a 5s drain window).