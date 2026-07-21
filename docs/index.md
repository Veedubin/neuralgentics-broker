# Neuralgentics Broker

A Go MCP (Model Context Protocol) broker with built-in tool-call audit,
hot-reload, role-based server catalogs, an egress-proxy transport swap, and
a skill catalog with provenance tracking.

## Why

An LLM agent that talks to many MCP servers needs a single front door that
spawns the servers, proxies JSON-RPC, records every call, and stays up while
config changes. The broker is that front door.

## Features

- Spawns and supervises MCP server subprocesses (stdio, HTTP/SSE)
- Proxies JSON-RPC between a client and the servers
- Records every tool call to JSONL and/or Postgres
- Optional egress-gateway transport swap for outbound HTTP
- Hot-reload of server config via `SIGHUP` (no dropped connections)
- Role-based server catalog and intent matcher (3-layer design)
- Skill catalog: local + external skills with provenance manifests and LRU
  body cache

## Quickstart

```bash
go install github.com/Veedubin/neuralgentics-broker/cmd/broker@v0.1.0
```

Then create a `servers.yaml` and run the broker:

```bash
cat > servers.yaml <<'EOF'
servers:
  - name: filesystem
    command: npx
    args: [-y, @modelcontextprotocol/server-filesystem, /tmp]
EOF

broker --config=servers.yaml
```

The broker exposes an MCP server on stdio — wire your agent to it.

## What this is NOT

- Not a memory store — use [memini-ai](https://github.com/Veedubin/memini-ai-dev)
- Not an egress gateway — use
  [neuralgentics-gateway](https://github.com/Veedubin/neuralgentics_gateway)
- Not a web UI — use [neuralgentics-web](https://github.com/Veedubin/neuralgentics-web)

## Documentation

- [Getting Started](getting-started.md) — install, minimal config, first call
- [Configuration](configuration.md) — YAML config + CLI flags
- [Architecture](architecture.md) — 3-layer design, JSON-RPC proxy, lifecycle
- [Audit](audit.md) — JSONL + Postgres schema, T-117.5 race-fix note
- [Skills](skills.md) — SkillCatalog, provenance, LRU body cache
- [Changelog](changelog.md)

## License

MIT — see [LICENSE](https://github.com/Veedubin/neuralgentics-broker/blob/main/LICENSE).