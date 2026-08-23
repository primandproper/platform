#!/usr/bin/env bash
set -euo pipefail

# Lint Go code using golangci-lint
# Usage: golang_lint.sh <container_runner> <linter_image> <linter_command>

CONTAINER_RUNNER="${1:-docker}"
# No default. The version is pinned in .golangci-version and the Makefile turns
# it into this argument; a fallback here would only ever fire for a caller who
# forgot one, and would lint against a version nobody chose.
LINTER_IMAGE="${2:?linter image required, e.g. golangci/golangci-lint:$(cat .golangci-version)}"
LINTER="${3}"

"${CONTAINER_RUNNER}" pull --quiet "${LINTER_IMAGE}"
${LINTER} run --config=.golangci.yml --timeout 30m ./...
