#!/usr/bin/env bash
set -euo pipefail

# Format Go tag alignment
# Usage: format_go_tag_alignment.sh

until go tool tagalign -fix -sort -order "env,envDefault,envPrefix,json,mapstructure,toml,yaml" ./...; do
  true
done > /dev/null
