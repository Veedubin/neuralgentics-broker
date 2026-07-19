# Changelog — neuralgentics broker-go

All notable changes to the `packages/broker-go` Go module are documented
in this file. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.13.0] — 2026-07-19

### Added
- **Egress gateway transport swap (T-BR-001).** The broker's `HTTPClient`
  now routes outbound HTTP through an egress gateway when the
  `EGRESS_GATEWAY_URL` environment variable is set to a non-empty URL
  (e.g. `EGRESS_GATEWAY_URL=http://localhost:9090`). This lets the new
  `neuralgentics-gateway` (T-100) audit / filter MCP calls made by
  hosted HTTP/SSE MCP servers without changes to the broker itself.
- `proxyTransport(gatewayURL string) http.RoundTripper` helper in
  `packages/broker-go/src/neuralgentics/broker/proxy/http_client.go`.
  Returns `http.DefaultTransport` when `gatewayURL` is empty (preserving
  pre-v0.13.0 behavior) and a proxy-aware `*http.Transport` otherwise.
  An invalid `gatewayURL` falls back to the default transport so a
  misconfiguration never hard-breaks broker calls.
- `broker.Version` constant (`packages/broker-go/src/neuralgentics/broker/version.go`)
  set to `"0.13.0"`.
- Bumped the MCP `clientInfo.version` advertised by the HTTP client
  from `0.5.0` to `0.13.0`.

### Tests
- `TestProxyTransportUnset`, `TestProxyTransportSet` — unit tests for
  the transport helper (unset/set/invalid env var paths).
- `TestEgressGatewayE2E` — integration test that spins up an
  `httptest` gateway server + real MCP server, sets
  `EGRESS_GATEWAY_URL`, makes a `Call` through the broker, and
  verifies the gateway's audit log captured the proxied request and
  the session ID propagated back.
- Pre-existing `HTTPClient` tests gained `t.Setenv("EGRESS_GATEWAY_URL", "")`
  so they remain isolated from any ambient env var set by CI.

### Backward Compatibility
- When `EGRESS_GATEWAY_URL` is unset (the default), the broker's
  HTTPClient behaves **identically to v0.12.x**: direct HTTP via
  `http.DefaultTransport`. No code path, no transport, no observable
  behavior changes.