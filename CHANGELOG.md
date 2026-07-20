# Changelog — neuralgentics broker-go

All notable changes to the `packages/broker-go` Go module are documented
in this file. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
- **Module path rename (T-130).** The Go module path changed from the
  bare local path `neuralgentics-broker` to the fully-qualified public
  path `github.com/Veedubin/neuralgentics-broker`. This unblocks
  `go install github.com/Veedubin/neuralgentics-broker/cmd/broker@latest`
  from any machine and is a prerequisite for publishing the broker to
  its own repository (T-119). Every Go import of
  `neuralgentics-broker/src/neuralgentics/broker/...` across the repo
  (including `packages/backend-go/`) was updated to the new path. The
  `replace` directive in `packages/backend-go/go.mod` now points at
  `github.com/Veedubin/neuralgentics-broker v0.0.0 => ../broker-go`.
  The MCP `clientInfo.name` display strings in
  `proxy/http_client.go` and `proxy/proxy.go` are unchanged — those
  are protocol-visible names, not import paths.

### Added
- `packages/broker-go/Makefile` with `install`, `build`, `vet`,
  `test`, `test-short`, and `tidy` targets. `make install` runs
  `go install ./cmd/broker`.
- `packages/broker-go/cmd/broker/install.sh` — convenience script
  that runs `go install ./cmd/broker` and prints the resulting
  `$GOPATH/bin/broker` path.
- `TestImportPathConsistency` — walks every `.go` file under
  `packages/` and fails if any still references the pre-T-130 bare
  module path `neuralgentics-broker/src/...` (excluding the new
  `github.com/Veedubin/`-prefixed path).
- `TestMain_Help` — builds the `broker` binary into a temp dir and
  runs `broker --help`, asserting sane output. Proves the
  `go install ./cmd/broker` path produces a working binary.

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