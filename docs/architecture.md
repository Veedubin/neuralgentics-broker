# Architecture

The broker is a long-running daemon that sits between an MCP client (an LLM
agent) and one or more MCP servers. It owns three responsibilities:
spawning/supervising servers, proxying JSON-RPC, and recording every tool
call.

First-class MCP servers (memini-ai) are registered directly in
`opencode.json` and bypass the broker entirely. Everything else sits behind
the broker: catalog-advertised, access-controlled, brokered on demand.

## Three-layer design

The broker is organized as three cooperating layers.

1. **Server catalog** — builds a role-filtered view of every available
   server (and skill). Lives in `src/neuralgentics/broker/catalog/`. The
   catalog is what the routing layer consults to decide which server can
   satisfy a request.
2. **Intent matcher** — given a natural-language intent and a role, picks
   the best server/tool pair to handle it. Lives in
   `src/neuralgentics/broker/intent/`. The matcher is the front door for
   free-text routing.
3. **Access control** — gates which roles can see which servers and call
   which tools. Lives in `src/neuralgentics/broker/access/`. Every catalog
   read and every `Call` go through access control before reaching a
   server.

<!-- mermaid: call-flow -->
```mermaid
flowchart LR
    subgraph Client["LLM Agent"]
        A["Agent sends<br/>JSON-RPC over stdio"]
    end

    subgraph Broker["neuralgentics-broker"]
        B["Broker<br/>single MCP server<br/>on stdio"]
        AC["Access Control<br/>role → server<br/>permissions"]
        IM["Intent Matcher<br/>NL intent →<br/>server/tool pair"]
        SC["Server Catalog<br/>~600 tokens<br/>role-filtered view"]
        SK["SkillCatalog<br/>secondary flow"]
        L["Launcher<br/>spawn if cold<br/>SetRuntime / ClearRuntime"]
        PR["Proxy<br/>JSON-RPC framing<br/>per-server HTTPClient<br/>session-ID propagation"]
        AU["Audit<br/>per-call record"]
    end

    subgraph Servers["MCP Servers"]
        S1["Server A<br/>stdio"]
        S2["Server B<br/>HTTP"]
        S3["Server C<br/>SSE"]
    end

    A -->|"JSON-RPC"| B
    B --> AC
    AC -->|"authorized"| IM
    AC -->|"denied"| DENY["403 / error"]
    IM -->|"lookup"| SC
    IM -->|"skill lookup"| SK
    IM -->|"matched server/tool"| L
    L -->|"cold start"| S1
    L -->|"cold start"| S2
    L -->|"cold start"| S3
    L -->|"warm"| PR
    PR -->|"proxy call"| S1
    PR -->|"proxy call"| S2
    PR -->|"proxy call"| S3
    S1 -->|"response"| PR
    S2 -->|"response"| PR
    S3 -->|"response"| PR
    PR -->|"result"| B
    B -->|"JSON-RPC response"| A
    B -->|"per call"| AU
    AU -->|"write"| AUDIT["Audit Store<br/>PostgreSQL / JSONL"]
```

## JSON-RPC stdio proxy

The broker itself presents as a single MCP server on stdio. A client (the
LLM agent) opens one JSON-RPC connection to the broker; the broker fans
requests out to the configured servers over their own transports (stdio,
HTTP, or SSE) and returns results on the shared stdio connection.

The proxy layer lives in `src/neuralgentics/broker/proxy/`. It owns:

- the JSON-RPC framing on the client-facing side
- per-server `HTTPClient` instances that respect `EGRESS_GATEWAY_URL`
- session-ID propagation so a downstream egress gateway can correlate
  requests back to the originating session

## Launcher lifecycle

The launcher (`src/neuralgentics/broker/launcher/`) owns the process
lifecycle of each server subprocess. Responsibilities:

- spawn the subprocess and connect its stdio / HTTP transport
- stamp the resulting `ServerEntry` with the process handle and pipes via
  `SetRuntime` (locked accessor)
- watch the subprocess; on exit, call `ClearRuntime` to atomically nil out
  the process and pipes
- honor `SIGHUP` by draining and restarting only the servers whose config
  changed (5s drain window for in-flight connections)

The locked accessors `SetRuntime` and `ClearRuntime` are the fix for the
T-117.5 data race — see [Audit](audit.md#t-1175-race-fix-note) for the
background and why every read of the process handle and pipes goes through
the entry mutex.