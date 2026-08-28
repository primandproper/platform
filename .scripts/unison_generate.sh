#!/usr/bin/env bash
set -euo pipefail

# Regenerate every sqlc-gen-unison-generated package in this module.
# Usage: unison_generate.sh <project_root>
#
# unison is the emitter, never the analyzer: it shells out to the pinned sqlc
# once per dialect and turns the analysis into one shared set of Go types plus
# per-dialect statements. Both halves of the toolchain are pinned — sqlc by
# .sqlc-version, unison by .unison-version — because an unpinned either one
# turns the generated-files check into a report on which machine ran it. The
# pins are enforced right here, by installing exactly what they name: the
# version header unison writes carries only the sqlc pin, since a go-installed
# unison stamps `dev` — its release artifacts alone carry a real version.

PROJECT_ROOT="${1:-$(pwd)}"

UNISON_VERSION="$(cat "${PROJECT_ROOT}/.unison-version")"
SQLC_VERSION="$(cat "${PROJECT_ROOT}/.sqlc-version")"

BIN_DIR="${PROJECT_ROOT}/artifacts/bin"
UNISON="${BIN_DIR}/unison"

# sqlc, pinned the way sqlc_compile.sh pins it: installed when absent, and
# reinstalled when the one on PATH is not the pinned release.
"${PROJECT_ROOT}/.scripts/ensure_tool_installed.sh" sqlc \
  "go install github.com/sqlc-dev/sqlc/cmd/sqlc@v${SQLC_VERSION}"

installed="$(sqlc version 2>/dev/null || echo none)"
if [ "${installed}" != "v${SQLC_VERSION}" ]; then
  echo "sqlc ${installed} is on PATH; installing the pinned v${SQLC_VERSION}"
  go install "github.com/sqlc-dev/sqlc/cmd/sqlc@v${SQLC_VERSION}"
fi

# unison, installed unconditionally at the pin — a pin nothing enforces is a
# version number in a file — into the gitignored artifacts/ rather than onto
# the developer's PATH. The command lives at cmd/unison, so `go install` names
# the binary for its package directory and there is nothing to rename.
mkdir -p "${BIN_DIR}"
GOBIN="${BIN_DIR}" go install "github.com/primandproper/sqlc-gen-unison/cmd/unison@v${UNISON_VERSION}"

# The components generated, as "<package dir>". Each holds a unison.yaml, and
# its per-dialect schema files are rendered first, from the package's own
# migrations at the empty table prefix — the single source, so there is no
# hand-written schema copy to drift. unison substitutes the consumer's real
# prefix at construction; sqlc analyzes the canonical names because an
# identifier is not a bind parameter in any of the three engines.
COMPONENTS=(
  "dataprivacy"
  "identity"
  "authentication/webauthn/database"
  "cryptography/shredding"
  "webhooks"
  "authentication/oauth2server/database"
  "saga"
  "sessions/database"
)

for component in "${COMPONENTS[@]}"; do
  for d in postgres mysql sqlite; do
    (cd "${PROJECT_ROOT}" &&
      go run "./${component}/internal/queriesgen" -schema "${d}" \
        > "${component}/migrations/schema/${d}.sql")
  done

  (cd "${PROJECT_ROOT}/${component}" && "${UNISON}" generate)
done

echo "unison v${UNISON_VERSION}: every generated package is current"
