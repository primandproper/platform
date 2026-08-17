#!/usr/bin/env bash
set -euo pipefail

# Format Go field alignment
# Usage: format_go_fieldalignment.sh [max_passes]
#
# Uses betteralign rather than x/tools' fieldalignment, for two reasons:
#
#   - fieldalignment silently deletes every comment inside a struct it rewrites.
#     betteralign preserves them and moves them with their fields.
#   - fieldalignment has no notion of generated code, so it spent all of its effort on
#     moq's *_mock.go — reporting diagnostics it would not fix, leaving every file
#     byte-identical while still exiting non-zero. The `until cmd; do true; done` this
#     used to be could therefore never terminate. betteralign skips generated files by
#     default, which is the half of that behaviour worth keeping — golangci-lint excludes
#     them too (`exclusions: generated: lax`), so neither tool reports them.
#
# It also skips test files by default, and that half has to be turned back off with
# -test_files: golangci-lint runs with `tests: true` and govet's `enable-all`, so it does
# report fieldalignment in _test.go. Without the flag a test struct is diagnosed by lint
# and never fixed by format, which is a make target that cannot make its own lint pass.
#
# betteralign is single-pass by design, so a loop is still wanted. It is bounded: a pass
# that rewrites nothing ends it, because the remaining diagnostics have no applicable fix.
# Note that -apply exits non-zero even when it succeeds, so exit status alone is not a
# progress signal.

MAX_PASSES="${1:-5}"

marker="$(mktemp)"
# shellcheck disable=SC2064
trap "rm -f '${marker}'" EXIT

for ((pass = 1; pass <= MAX_PASSES; pass++)); do
  touch "${marker}"

  # Exit 0 means nothing left to report: we are done.
  if go tool betteralign -apply -test_files ./... > /dev/null 2>&1; then
    break
  fi

  # Otherwise keep going only if this pass actually rewrote something.
  if [ -z "$(find . -type f -name '*.go' -not -path '*/vendor/*' -newer "${marker}" -print -quit)" ]; then
    break
  fi
done
