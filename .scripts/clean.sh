#!/usr/bin/env bash
set -euo pipefail

# Clean artifacts directory
# Usage: clean.sh <artifacts_dir>

ARTIFACTS_DIR="${1:-artifacts}"

rm -rf "${ARTIFACTS_DIR}"
