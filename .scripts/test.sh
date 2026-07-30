#!/usr/bin/env bash
set -euo pipefail

# Run tests
# Usage: test.sh
RUN_CONTAINER_TESTS="${1:-true}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RUN_CONTAINER_TESTS="${RUN_CONTAINER_TESTS}" "${SCRIPT_DIR}/pull_test_containers.sh"

CGO_ENABLED=1 RUN_CONTAINER_TESTS="${RUN_CONTAINER_TESTS}" go test -shuffle=on -race -vet=all -failfast ./...
