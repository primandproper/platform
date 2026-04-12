#!/usr/bin/env bash
set -euo pipefail

# Clean vendor directory and go.sum
# Usage: clean_vendor.sh

rm -rf vendor go.sum
