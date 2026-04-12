#!/usr/bin/env bash
set -euo pipefail

# Lint Go code using golangci-lint
# Usage: golang_lint.sh <container_runner> <linter_image> <linter_command>

CONTAINER_RUNNER="${1:-docker}"
LINTER_IMAGE="${2:-golangci/golangci-lint:v2.7.2}"
LINTER="${3}"

"${CONTAINER_RUNNER}" pull --quiet "${LINTER_IMAGE}"
${LINTER} run --config=.golangci.yml --timeout 30m ./...
