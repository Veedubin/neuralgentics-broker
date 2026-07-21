# Getting Started

## Prerequisites

- Go 1.22 or later (for `go install`)
- Any MCP server you want to proxy (e.g.
  [`@modelcontextprotocol/server-filesystem`](https://github.com/modelcontextprotocol/servers))

## Install

```bash
go install github.com/Veedubin/neuralgentics-broker/cmd/broker@v0.1.0
```

This puts the `broker` binary in `$GOPATH/bin` (or `$HOME/go/bin` by
default). Make sure that directory is on your `$PATH`.

Verify:

```bash
broker --help
```

## Minimal config

Create a `servers.yaml` listing the MCP servers the broker should spawn:

```yaml
servers:
  - name: filesystem
    command: npx
    args: [-y, @modelcontextprotocol/server-filesystem, /tmp]
  - name: github
    command: npx
    args: [-y, @modelcontextprotocol/server-github]
    env:
      GITHUB_PERSONAL_ACCESS_TOKEN: $GITHUB_TOKEN
```

See [Configuration](configuration.md) for the full schema.

## First call

Start the broker:

```bash
broker --config=servers.yaml
```

The broker exposes an MCP server on stdio. Point your agent (or any MCP
client) at the broker as if it were a single MCP server; the broker will
fan requests out to the configured servers, return results, and write one
audit record per tool call to `~/.neuralgentics/broker_audit.jsonl`.

To enable Postgres audit alongside JSONL:

```bash
broker --config=servers.yaml --audit=jsonl+pg --audit-pg-url=postgres://user:pass@host/db
```

See [Audit](audit.md) for the schema and the T-117.5 race-fix note.

## Next steps

- [Configuration](configuration.md) — full YAML schema and every CLI flag
- [Architecture](architecture.md) — the 3-layer design and lifecycle
- [Egress gateway](configuration.md#egress-gateway) — route outbound HTTP
  through a policy/audit proxy