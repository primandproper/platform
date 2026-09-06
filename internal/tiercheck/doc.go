/*
Package tiercheck is where every package in this module is named with the tier it
belongs to, and it holds nothing else.

The module sorts into two tiers, and the README's "Primitives and Domains"
section states the rule that sorts them and the reason the sort exists: the
primitives are leaving for primitives-go, and primitives-go will import nothing
from platform-go. So an import from a primitives-tier package into a
domain-tier one is not a style preference. It is a package that cannot travel,
and it is invisible until the module is split, at which point it is a build
failure in a repository that does not exist yet.

Until this package there was nothing checking it. The rule lived in the README
and in CLAUDE.md, and `grep -rn "primitives-go" --include="*.go"` over the whole
module returned nothing. Two separate audits of the crossings were published as
complete and neither was: the first missed four config subpackages, and the
second — the one that found those four — missed a roster test that imported
fourteen domain packages from a primitive's own test files. Both were people
reading a tree by hand, which is a survey with a shelf life of one branch.

What lives here is the enumeration, in the one form that cannot quietly fall out
of date: a roster of every package, and a test that walks the module's imports
and fails on a crossing. Three answers:

	primitive  goes to primitives-go — a provider behind an interface, a
	           transport whose shape is not the consumer's, the database and
	           schema tooling, or a cross-cutting value both tiers agree on.
	           It owns no table, and it may import only other primitives.
	domain     stays in platform-go — a noun with a table, its lifecycle, its
	           transport, its permissions and its privacy obligations. It may
	           import anything, primitives included, because that direction is
	           the one the split permits.
	root       neither tier: the composition root that registers both, and the
	           convention tests whose subject is the whole tree. Treated as
	           domain for the purpose of the check, and named separately so
	           that "why is this not a primitive" has an answer other than
	           somebody's omission.

The roster is keyed by directory prefix, longest match wins, which is how the
README's own table is written: `authorization` is a primitive and
`authorization/database` is a domain, and everything under each inherits its
answer. That is what makes a new subpackage classified by construction —
`authorization/database/config` is a domain because the store it configures is
one, without anybody having to add a row for it.

Test files count. A primitives-go test file is compiled by primitives-go, so a
test that reaches into the domain tier is exactly as much of a blocker as
production code that does. That is the rule that catches the roster test the
second audit missed, and it is why internal/configroster exists.

Two directions are checked, as sqltier checks its own roster in both: a package
nobody classified fails, and a roster entry naming a directory that no longer
exists fails. A third test reads the README's table and requires that it and this
file say the same thing, so the prose a reader is pointed at cannot drift from
the enumeration a build enforces.
*/
package tiercheck
