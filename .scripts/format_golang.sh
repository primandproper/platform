#!/usr/bin/env bash
set -euo pipefail

# Format all Go files using gofmt
# Usage: format_golang.sh <project_root> <gofmt_command>

PROJECT_ROOT="${1:-$(pwd)}"

while IFS= read -r -d '' file; do
  # GO_FORMAT contains a command with arguments, so we use eval
  # shellcheck disable=SC2086
  eval "gofmt -s -w \"${file}\""
done < <(find "${PROJECT_ROOT}" -type f -not -path '*/vendor/*' -name "*.go" -print0)
