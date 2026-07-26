#!/usr/bin/env bash
set -euo pipefail

# Format Go imports using goimports
# Usage: goimports.sh <project_root>

PROJECT_ROOT="${1:-$(pwd)}"

# goimports walks into vendor/, which dwarfs the source tree, so enumerate the
# files ourselves and hand them over explicitly.
go_files=()
while IFS= read -r -d '' file; do
  go_files+=("${file}")
done < <(find "${PROJECT_ROOT}" -type f -not -path '*/vendor/*' -name "*.go" -print0)

if [ ${#go_files[@]} -gt 0 ]; then
  go tool goimports -w "${go_files[@]}"
fi
