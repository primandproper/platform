#!/usr/bin/env bash
set -euo pipefail

# Run shellcheck on all shell scripts
# Usage: shellcheck.sh <container_runner> <shellcheck_image> <scripts_dir>

CONTAINER_RUNNER="${1:-docker}"
SHELLCHECK_IMAGE="${2:-koalaman/shellcheck:stable}"
SCRIPTS_DIR="${3:-.scripts}"

# Get the project root directory (where Makefile is)
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Pull the shellcheck image
"${CONTAINER_RUNNER}" pull --quiet "${SHELLCHECK_IMAGE}"

# Find all shell scripts and check them (relative to project root)
shell_scripts=()
while IFS= read -r file; do
  shell_scripts+=("${file}")
done < <(find "${PROJECT_ROOT}/${SCRIPTS_DIR}" -type f -name "*.sh" | sort)

if [ ${#shell_scripts[@]} -eq 0 ]; then
  echo "No shell scripts found in ${SCRIPTS_DIR}"
  exit 0
fi

# Run shellcheck on each script
for script in "${shell_scripts[@]}"; do
  # Get relative path from project root for display and container
  rel_path="${script#"${PROJECT_ROOT}"/}"
  echo "Checking ${rel_path}..."
  "${CONTAINER_RUNNER}" run --rm \
    --volume "${PROJECT_ROOT}:/workdir" \
    --workdir /workdir \
    "${SHELLCHECK_IMAGE}" \
    "${rel_path}"
done

echo "All shell scripts passed shellcheck!"
