/*
Package tiermatrix is where the module's primitive-versus-domain sort is checked
against the tree it describes, and it holds nothing else.

The sort itself lives in the module README, under "Primitives and Domains",
because the question it answers — "does this belong here at all?" — is asked
before a package exists, by somebody proposing one. The rule above the table
says what qualifies as a primitive and what qualifies as a domain; the table is
this module read through it, and the two are only useful together. A rule with
no worked example is an opinion, and a list with no rule is an inventory.

A list of seventy packages is a copy of facts that live somewhere else, which is
the kind of documentation that goes quietly wrong: a package is added, or
renamed, or split, and the table goes on reading true. So it is not maintained
against the tree by hand. This package parses it and checks the three things a
directory listing can settle, in both directions, because a roster fails both
ways:

	a package in no tier      the rule was never applied to it
	a tier with no package    a roster outliving its subject, which reads live
	DDL under a primitive     the rule's one hard claim, contradicted

The third is the sentence "Nothing in it owns a table", and it is the only part
of the rule that is mechanical. Whether a package is a provider or a value,
whether an application with no users would still need it — those are judgements,
and a test that scored them would be a second opinion rather than a check. What
ships a migrations directory is not a judgement, so that is what is checked, in
the manner of internal/dialectmatrix: the tree is walked for directories named
migrations, and each one's owning package is classified by the longest entry in
the table that prefixes it. Longest wins because that is what nesting means here
— authentication is a primitive and authentication/passwordreset owns a table,
and both rows are true.

Completeness is checked only at the top level. A tier entry may name a nested
package, and several do, but requiring one for every subdirectory in the module
would be a table of several hundred rows answering a question nobody asks: the
proposal a reader arrives with is a new top-level package, and that is the
granularity the rule is written at.

What this package does not check is the direction of imports between the tiers —
that primitives-go, once it exists, never imports platform-go. That is the
constraint the split turns on, and it is not yet true of this tree: the error
mappers still reach into the domain tier, and untangling them is its own change.
Asserting it here would ship a red test as documentation. The claim this package
makes is narrower and is the one nothing else makes: that the table in the
README still describes this module.
*/
package tiermatrix
