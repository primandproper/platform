#!/usr/bin/env bash
set -euo pipefail

# Vendor Go dependencies
# Usage: vendor.sh

if [ ! -f go.mod ]; then
  go mod init
fi

go mod tidy
go mod vendor
