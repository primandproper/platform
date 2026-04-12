#!/usr/bin/env bash
set -euo pipefail

# Format Go imports using goimports
# Usage: goimports.sh

go tool goimports -w .
