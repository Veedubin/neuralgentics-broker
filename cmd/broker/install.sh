#!/usr/bin/env bash
# Install the neuralgentics broker binary via `go install`.
#
# Usage:
#   ./install.sh                # installs to $GOPATH/bin (default)
#   GOFLAGS=-mod=mod ./install.sh
#
# After install, the binary is available as:
#   $GOPATH/bin/broker --help
#
# Or, once this module is published, from any machine:
#   go install github.com/Veedubin/neuralgentics-broker/cmd/broker@latest
set -euo pipefail

cd "$(dirname "$0")/.."

if ! command -v go >/dev/null 2>&1; then
	echo "error: go toolchain not found in PATH" >&2
	exit 1
fi

echo "Installing github.com/Veedubin/neuralgentics-broker/cmd/broker ..."
go install ./cmd/broker

GOPATH="${GOPATH:-$(go env GOPATH)}"
echo "Installed: ${GOPATH}/bin/broker"
echo "Verify:    ${GOPATH}/bin/broker --help  (or just 'broker --help' if \$GOPATH/bin is on PATH)"