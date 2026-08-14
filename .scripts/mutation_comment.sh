#!/usr/bin/env bash
set -euo pipefail

# Post the mutation survivors from a gremlins report onto a pull request, as one
# comment that is edited in place rather than a new comment per push.
#
# The gate itself does not fail on survivors (see mutation.sh for why), so the
# comment is where the signal actually lands. A run with nothing to say edits an
# existing comment to say so and never creates one.
#
# Usage: mutation_comment.sh <pr_number> <report_path>
#
# Expects GH_TOKEN and GITHUB_REPOSITORY in the environment.

PR_NUMBER="${1:?missing pull request number}"
REPORT="${2:-artifacts/gremlins.json}"
REPOSITORY="${GITHUB_REPOSITORY:?missing GITHUB_REPOSITORY}"

# Anchors the comment so repeated runs edit rather than accumulate. Invisible in
# rendered markdown.
MARKER='<!-- gremlins-mutation-report -->'

if [[ ! -s "${REPORT}" ]]; then
	echo "mutation_comment: no report at ${REPORT}, nothing to post"
	exit 0
fi

body="$(
	MARKER="${MARKER}" jq -r '
		# Sorted by position rather than as text, so the mutants in a file read in
		# the order they appear in it: a plain sort puts line 11 above line 7.
		def rows($want):
			[ .files[]? as $file
			| $file.mutations[]?
			| select(.status == $want)
			| {file: $file.file_name, line: .line, column: .column, type: .type}
			]
			| sort_by([.file, .line, .column])
			| map("| `\(.file):\(.line):\(.column)` | `\(.type)` |");

		def section($title; $blurb; $want):
			rows($want) as $r
			| if ($r | length) == 0 then ""
			  else "\n**\($title)** (\($r | length)) — \($blurb)\n\n| location | mutator |\n| --- | --- |\n" + ($r | join("\n")) + "\n"
			  end;

		# mutants_total counts only what gremlins ran — killed plus lived plus
		# not viable. NOT COVERED sits outside it, so it is reported separately
		# rather than as a fourth addend that does not fit the total.
		(env.MARKER) + "\n### Mutation testing\n\n"
		+ "`\(.mutants_killed)` killed, `\(.mutants_lived)` lived, `\(.mutants_not_viable)` not viable "
		+ "of `\(.mutants_total)` mutants run on the changed lines; `\(.mutants_not_covered)` not covered "
		+ "(\(.elapsed_time | floor)s, \(.test_efficacy | . * 10 | round / 10)% efficacy).\n"
		+ section("Survived"; "the tests pass with these lines changed, so either an assertion is missing or the mutant is equivalent"; "LIVED")
		+ section("Not covered"; "no test reaches these lines"; "NOT COVERED")
		+ section("Timed out"; "excluded from the efficacy figure, so this run proved less than it reports"; "TIMED OUT")
		+ (if .mutants_total == 0 then "\nNothing mutable on the changed lines — gremlins does not mutate test files.\n"
		   elif (.mutants_lived + .mutants_not_covered) == 0 then "\nEvery mutant on the changed lines was killed.\n"
		   else "" end)
	' "${REPORT}"
)"

existing="$(
	gh api "repos/${REPOSITORY}/issues/${PR_NUMBER}/comments" --paginate \
		--jq "[.[] | select(.body | startswith(\"${MARKER}\")) | .id] | first // empty"
)"

if [[ -n "${existing}" ]]; then
	gh api --method PATCH "repos/${REPOSITORY}/issues/comments/${existing}" \
		--raw-field body="${body}" >/dev/null
	echo "mutation_comment: updated comment ${existing} on #${PR_NUMBER}"
	exit 0
fi

# Nothing survived and there is no comment to correct, so stay quiet rather than
# opening one that only says everything is fine.
clean="$(jq -r '(.mutants_lived + .mutants_not_covered) == 0' "${REPORT}")"
if [[ "${clean}" == "true" ]]; then
	echo "mutation_comment: nothing to report on #${PR_NUMBER}"
	exit 0
fi

gh api --method POST "repos/${REPOSITORY}/issues/${PR_NUMBER}/comments" \
	--field body="${body}" >/dev/null
echo "mutation_comment: commented on #${PR_NUMBER}"
