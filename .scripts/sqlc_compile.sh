#!/usr/bin/env bash
set -euo pipefail

# Check every generated .sql in this module against the schema it is written for,
# with sqlc.
# Usage: sqlc_compile.sh <project_root>
#
# `sqlc compile` parses and type-checks the queries against the DDL and emits
# nothing. That is the whole guarantee this module wants from sqlc: the Go it
# would generate never executes — the stores render the same statements through
# database/querygen's Bound methods, with the consumer's table prefix on the
# name — so checking in a generated package nothing imports would be policing an
# artifact with no consumer.
#
# Neither input is written by hand. The queries come from `make generate`, and a
# hand-edit of one is caught by the regeneration diff rather than here; the
# schema is rendered on the fly by the same command, at the empty table prefix,
# because sqlc resolves table names when it runs and an identifier is not a bind
# parameter in any of the three engines.

PROJECT_ROOT="${1:-$(pwd)}"

SQLC_VERSION="$(cat "${PROJECT_ROOT}/.sqlc-version")"

# The components checked, as "<module-relative package> <queries directory>".
# Each package's directory holds one <dialect>_generated.sql per dialect it claims, and
# its `-schema <dialect>` mode prints the DDL those queries are read against.
COMPONENTS=(
  "./identity ./internal/queriesgen internal/queries"
  "./webhooks ./internal/queriesgen internal/queries"
)

# The dialects checked, as "<dialect> <sqlc engine>". Every dialect
# database/dialect names is here; a generator that emits SQL one of the three
# cannot parse is a generator with a broken dialect, not a check with a gap.
DIALECTS=(
  "postgres postgresql"
  "mysql mysql"
  "sqlite sqlite"
)

ensure_sqlc() {
  "${PROJECT_ROOT}/.scripts/ensure_tool_installed.sh" sqlc \
    "go install github.com/sqlc-dev/sqlc/cmd/sqlc@v${SQLC_VERSION}"

  local installed
  installed="$(sqlc version)"

  # A pin nothing enforces is a version number in a file. ensure_tool_installed
  # only installs what is missing, so an older sqlc already on PATH would
  # otherwise decide what this check means.
  if [ "${installed}" != "v${SQLC_VERSION}" ]; then
    echo "sqlc ${installed} is on PATH; installing the pinned v${SQLC_VERSION}"
    go install "github.com/sqlc-dev/sqlc/cmd/sqlc@v${SQLC_VERSION}"
  fi
}

main() {
  ensure_sqlc

  local workspace
  workspace="$(mktemp -d)"
  # shellcheck disable=SC2064
  trap "rm -rf '${workspace}'" EXIT

  local config="${workspace}/sqlc.yaml"
  printf 'version: "2"\nsql:\n' > "${config}"

  local component package generator queries_dir pair d engine target
  for component in "${COMPONENTS[@]}"; do
    # shellcheck disable=SC2086
    set -- ${component}
    package="${1}"
    generator="${2}"
    queries_dir="${3}"

    for pair in "${DIALECTS[@]}"; do
      # shellcheck disable=SC2086
      set -- ${pair}
      d="${1}"
      engine="${2}"

      target="${workspace}/${package//\//_}_${d}"
      mkdir -p "${target}"

      (cd "${PROJECT_ROOT}/${package}" && go run "${generator}" -schema "${d}") > "${target}/schema.sql"
      cp "${PROJECT_ROOT}/${package}/${queries_dir}/${d}_generated.sql" "${target}/queries.sql"

      cat >> "${config}" <<EOF
  - engine: ${engine}
    schema: $(basename "${target}")/schema.sql
    queries: $(basename "${target}")/queries.sql
    gen:
      go:
        package: checked
        out: $(basename "${target}")/gen
EOF
    done
  done

  (cd "${workspace}" && sqlc compile)

  echo "sqlc v${SQLC_VERSION}: every checked dialect compiles"
}

main
