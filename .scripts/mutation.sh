#!/usr/bin/env bash
set -euo pipefail

# Run gremlins over the lines this branch changes and report the survivors.
#
# `go test` says the cases we thought of pass and `-cover` says which lines ran;
# neither answers whether a test would notice if the code were wrong. gremlins
# answers that by editing the source and re-running the tests: a mutant that
# survives is a line whose behaviour no assertion depends on.
#
# The gate is deliberately asymmetric.
#
#   LIVED        reported, does not fail. gremlins has no per-line suppression —
#                the only exclusion mechanism is --exclude-files, a path regexp —
#                so an equivalent mutant would be re-reported on every pull
#                request that touches its line, forever, with no way to mark it
#                adjudicated. A blocking gate that cannot be made permanently
#                green trains people to click through it.
#   NOT COVERED  reported, does not fail. That a mutant's line is unreached is a
#                coverage fact, and coverage already has its own gate.
#   TIMED OUT    fails the run. The JSON summary carries no mutants_timed_out
#                field, so a run where everything timed out still reports a
#                healthy efficacy figure while having proved nothing. Silence
#                there is worse than red. See TIMEOUT_COEFFICIENT for what the
#                deadline is derived from, and why the derivation needs help.
#
# Usage: mutation.sh <mutate_command> [diff_ref] [report_path]
#
# GREMLINS_WORKERS caps the worker count; see the flag below for why it is not
# left to default.

MUTATE="${1:?missing mutate command}"
DIFF_REF="${2:-origin/main}"
REPORT="${3:-artifacts/gremlins.json}"
WORKERS="${GREMLINS_WORKERS:-4}"

# Each mutant's deadline is the elapsed time of the coverage run times this,
# and the default of 3 is calibrated for a run where those two things are
# comparable. Here they are not, in both directions:
#
#   - Every worker mutates its own full copy of the module, at a path the
#     restored build cache has never seen, so the first mutant to reach a
#     package pays for compiling and linking that package's test binary there.
#     All the workers pay it at once, on the same package, and it is a cost the
#     coverage run — which compiled once, in the checkout — never measured.
#   - The coverage run is the thing the deadline scales with, and a warm cache
#     is exactly what makes it quick. The gate therefore got *tighter* the
#     better the cache worked, and a run that restored its cache on the nose
#     timed out eight mutants whose tests fail in under four seconds.
#
# Ten was calibrated against packages whose test binaries link quickly, and
# database/querygen's does not: its container suite pulls testcontainers in, so
# the first wave of workers spends longer copying and linking than the entire
# warm coverage run takes.
#
# The run that showed it up is worth writing down, because the shape of it is
# what the number has to cover. Coverage gathered in sixteen seconds, putting
# the deadline at a hundred and sixty. The four mutants in the first wave hit
# that deadline exactly. The three behind them ran in the same worker copies,
# now warm, finished in thirty-five seconds each, and were killed. Nothing was
# slow about the code under test: the gate failed on the cost of arriving.
#
# Forty puts a warm run's deadline above ten minutes, which covers a first wave
# that has to copy the module and link a container-linked test binary four times
# at once. It is still well short of the workflow's own cap, so a mutant that
# genuinely hangs is still reported as TIMED OUT there — which is the signal —
# rather than taking the whole job down with it, which is not.
TIMEOUT_COEFFICIENT="${GREMLINS_TIMEOUT_COEFFICIENT:-40}"

# Path regexps, which is the only kind of exclusion gremlins offers.
#
# vendor/ is the load-bearing one. gremlins analyses it like any other source,
# and it is 18k files of other people's code: leaving it in produces 1.4M
# skipped mutants against this module's ~7k, a 99MB JSON report in place of a
# 562KB one, and roughly triple the analysis time, in exchange for nothing —
# a mutant in a vendored dependency is not a fact about this module's tests.
#
# Generated moq output and the test helpers go for the same reason coverage.sh
# drops them from the profile: nothing asserts on them directly, so every
# mutant there is noise.
EXCLUDED_PATHS=(
	'^vendor/'
	'/mock/'
	'^testutils/'
)

if ! command -v jq &>/dev/null; then
	echo "mutation: jq is required to read ${REPORT}" >&2
	exit 1
fi

if ! git rev-parse --verify --quiet "${DIFF_REF}" >/dev/null; then
	echo "mutation: cannot resolve '${DIFF_REF}'" >&2
	echo "mutation: gremlins shells out to 'git diff --merge-base', so the ref has to" >&2
	echo "mutation: be present locally — CI needs fetch-depth: 0 on the checkout." >&2
	exit 1
fi

# gremlins will do this itself, but doing it up front means a pull request that
# touches no Go source finishes in a second instead of paying for a coverage run
# to discover it has nothing to mutate.
exclude_expression="$(
	IFS='|'
	echo "${EXCLUDED_PATHS[*]}"
)"
changed="$(git diff --merge-base "${DIFF_REF}" --name-only -- '*.go' | grep -Ev "${exclude_expression}" || true)"
if [[ -z "${changed}" ]]; then
	echo "mutation: no mutable Go source changed against ${DIFF_REF}, nothing to do"
	exit 0
fi

echo "mutation: $(printf '%s\n' "${changed}" | wc -l | tr -d ' ') changed file(s) against ${DIFF_REF}"

mkdir -p "$(dirname "${REPORT}")"
rm -f "${REPORT}"

exclude_flags=()
for path in "${EXCLUDED_PATHS[@]}"; do
	exclude_flags+=(--exclude-files "${path}")
done

status=0
# --workers defaults to the CPU count, and must not be left there. Each worker
# gets its own full copy of the module, and gremlins v0.6.0's copy routine
# closes neither the source nor the destination file — it leaks two descriptors
# per file copied. At ~21k files in this module that is ~42k descriptors per
# worker, so a sixteen-core machine reliably hits EMFILE partway through the
# copy. gremlins then reports it as `panic: error, this is temporary`, having
# discarded the underlying error, which makes it look like a flake rather than
# arithmetic. Fewer workers is also simply faster here: the per-worker copy
# costs more than the mutant test runs it parallelises.
#
# --output-statuses takes one letter per status: l)ived, not c)overed, t)imed
# out, k)illed, not v)iable, s)kipped, r)unnable. Everything outside the diff is
# SKIPPED, which is ~7k mutants even with vendor/ excluded — printing them
# buries the handful the run is actually about.
#
# MUTATE arrives as a single string holding the whole container invocation, the
# same way LINTER does, so it has to go through word splitting.
# shellcheck disable=SC2086
${MUTATE} unleash \
	--diff "${DIFF_REF}" \
	--output "${REPORT}" \
	--output-statuses lctkv \
	--workers "${WORKERS}" \
	--timeout-coefficient "${TIMEOUT_COEFFICIENT}" \
	"${exclude_flags[@]}" \
	. || status=$?

if [[ ! -s "${REPORT}" ]]; then
	echo "mutation: gremlins exited ${status} without writing ${REPORT}" >&2
	exit 1
fi

# --output-statuses does not reach the JSON, which carries every skipped mutant
# in the module and so weighs ~560KB on a diff of fifteen. Drop them, leaving a
# report that is all signal and small enough to attach to a build. The summary
# counters already exclude SKIPPED, so what is left is self-consistent.
compacted="${REPORT}.compact"
jq -c '
	.files = ([
		.files[]?
		| .mutations = [.mutations[]? | select(.status != "SKIPPED")]
		| select((.mutations | length) > 0)
	])
' "${REPORT}" >"${compacted}"
mv "${compacted}" "${REPORT}"

# Every listing below is (file:line:column, mutator), which is the identity a
# survivor has to be adjudicated by. There is no stabler handle: gremlins has no
# per-mutant identifier.
#
# Sorted by file then numerically by position, so a file's mutants read in the
# order they appear in it. A plain sort puts line 11 above line 7.
mutants_with_status() {
	jq -r --arg want "${1}" '
		.files[]? as $file
		| $file.mutations[]?
		| select(.status == $want)
		| "  \($file.file_name):\(.line):\(.column)  \(.type)"
	' "${REPORT}" | sort -t: -k1,1 -k2,2n -k3,3n
}

count_lines() {
	if [[ -z "${1}" ]]; then
		echo 0
	else
		printf '%s\n' "${1}" | wc -l | tr -d ' '
	fi
}

timed_out="$(mutants_with_status 'TIMED OUT')"
lived="$(mutants_with_status 'LIVED')"
not_covered="$(mutants_with_status 'NOT COVERED')"

timed_out_count="$(count_lines "${timed_out}")"
lived_count="$(count_lines "${lived}")"
not_covered_count="$(count_lines "${not_covered}")"

echo

# A diff of nothing but _test.go files produces no mutants at all, which is the
# common case on a test-only change. Say that, rather than reporting 0% efficacy
# and a clean sweep of an empty set.
if [[ "$(jq -r '.mutants_total' "${REPORT}")" == "0" ]]; then
	echo "mutation: nothing mutable on the changed lines — gremlins does not mutate test files"
	exit 0
fi

# mutants_total counts only the mutants gremlins actually ran — killed plus
# lived plus not viable. NOT COVERED sits outside it, so the two are reported as
# two figures rather than as four addends that do not sum to the total.
jq -r '
	"mutation: \(.mutants_killed) killed, \(.mutants_lived) lived, " +
	"\(.mutants_not_viable) not viable of \(.mutants_total) run; " +
	"\(.mutants_not_covered) not covered (\(.elapsed_time | floor)s)"
' "${REPORT}"

# The efficacy figure is printed but never gated on: --threshold-efficacy drops
# timed-out mutants from both sides of the ratio, and on a three-mutant diff the
# percentage is noise either way. The survivor list is the signal.
jq -r '"mutation: \(.test_efficacy | . * 10 | round / 10)% efficacy, \(.mutations_coverage | . * 10 | round / 10)% mutant coverage"' "${REPORT}"

if [[ -n "${not_covered}" ]]; then
	echo
	echo "mutation: ${not_covered_count} mutant(s) NOT COVERED — no test reaches these lines:"
	printf '%s\n' "${not_covered}"
fi

if [[ -n "${lived}" ]]; then
	echo
	echo "mutation: ${lived_count} mutant(s) LIVED — the tests pass with these lines changed:"
	printf '%s\n' "${lived}"
	echo
	echo "mutation: each survivor is either a missing assertion or an equivalent mutant."
	echo "mutation: this does not fail the run. Read them."
fi

if [[ -n "${timed_out}" ]]; then
	echo
	echo "mutation: ${timed_out_count} mutant(s) TIMED OUT:" >&2
	printf '%s\n' "${timed_out}" >&2
	echo >&2
	echo "mutation: timeouts are excluded from the efficacy figure, so this run proved" >&2
	echo "mutation: less than it reports. Two causes, and they are told apart by" >&2
	echo "mutation: running the mutated line's package: a mutant whose tests fail in" >&2
	echo "mutation: seconds timed out on build cost, and wants a larger" >&2
	echo "mutation: GREMLINS_TIMEOUT_COEFFICIENT; a mutant that hangs there hangs" >&2
	echo "mutation: because some wait in those tests has no bound, and wants that." >&2
	exit 1
fi

if [[ -z "${lived}" && -z "${not_covered}" ]]; then
	echo
	echo "mutation: every mutant on the changed lines was killed"
fi
