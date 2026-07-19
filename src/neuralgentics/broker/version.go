package broker

// Version is the semantic version of the neuralgentics broker-go package.
//
// Bumped as part of T-BR-001 (broker transport swap) for the v0.13.0
// release: outbound HTTP from the broker's HTTPClient is now routed
// through an optional egress gateway when EGRESS_GATEWAY_URL is set.
// When the env var is unset the broker behaves exactly as before.
const Version = "0.13.0"
