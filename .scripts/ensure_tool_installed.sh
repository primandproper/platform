#!/usr/bin/env bash
set -euo pipefail

# Ensure a tool is installed, install if missing
# Usage: ensure_tool_installed.sh <tool_name> <install_command>

TOOL_NAME="${1}"
INSTALL_COMMAND="${2}"

if ! command -v "${TOOL_NAME}" &> /dev/null; then
  # shellcheck disable=SC2086
  ${INSTALL_COMMAND}
fi
