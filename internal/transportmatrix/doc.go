/*
Package transportmatrix is where the module's store-versus-transport boundary is
checked against the tree it describes, and it holds nothing else.

The boundary itself lives in the module README, under "Stores and Transports",
because the question it answers — "do I write the handlers, or does this?" — is
asked while a consumer is planning a port, before any package has been imported
and long before a doc comment would be reached. Most components that own data
ship a store and stop; a few ship a transport, each for a reason its own doc
explains, and before that section existed the difference was discoverable only
by listing directories.

A boundary stated in one place and implemented in forty is the kind of
documentation that goes quietly wrong: a package grows an http subpackage, or
loses one, and the section goes on reading true. So the table under that heading
is not maintained against the tree by hand. This package parses it and compares
it against what the tree actually ships, in both directions, because a roster
fails both ways:

	a transport in no row       the boundary moved and the README did not
	a row with no transport     a roster outliving its subject, which reads live

The ground truth is deliberately crude, in the manner of internal/dialectmatrix
and internal/sqltier: a directory named http or grpc is a transport. That finds a
package by what it ships rather than by anything it declares, which is the
property that matters — a package that has quietly grown handlers is precisely
the one to catch. webhooks/inbound is the one transport outside the table and is
named in the section's prose rather than in a row, because its Receiver is not an
http subpackage and this walk therefore does not see it.

The other direction is checked too, against the same walk: every package the
section names as shipping a store and no handlers has to still be one. That is
the claim a consumer plans around, and it is the claim that a single new
subdirectory silently reverses.

What this package does not check is the reasons. Each transport's own doc
carries why it is one, and each store's carries why it is not; repeating either
here would be a second copy of an answer rather than a check on this one. The
claim this package makes is narrower and is the one nothing else makes: that the
section in the README is still true.
*/
package transportmatrix
