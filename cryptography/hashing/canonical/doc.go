/*
Package canonical hashes Go values by content, producing the same digest for
semantically identical values regardless of which process built them, in what
order, or how their types declare fields.

hashing.Hasher answers "which digest"; this package answers "digest of what
bytes" — the half where the correctness risk lives. Values are encoded to a
canonical JSON form: encoding/json performs the initial encoding (so struct
tags, omitempty, and MarshalJSON implementations are honored), and the result
is re-emitted with every object's keys sorted in lexicographic byte order and
no insignificant whitespace. Struct field declaration order therefore never
affects the digest, and map iteration order never could.

The rules, stated explicitly rather than inherited by stdlib accident:

  - Object keys (from maps and struct fields alike) sort lexicographically by
    byte order. This is deterministic but not RFC 8785 (JCS), which sorts by
    UTF-16 code units; digests are canonical within this package, not across
    ecosystems.
  - Array order is preserved: slice order is treated as semantic. A caller
    whose slices are sets must sort them before hashing (or accept that
    reordering changes the digest).
  - A nil slice or map encodes as null, an empty one as [] or {} — they are
    different canonical values. Normalize before hashing if your domain
    considers them equal.
  - Numbers keep encoding/json's representation (shortest round-trip form for
    floats). NaN and infinities are not representable and return an error, as
    do values encoding/json cannot marshal.

Use WithoutKeys to exclude top-level fields from the digest — typically a
struct's own content-hash field, which cannot participate in its own
computation. This replaces the fragile rebuild-a-literal-without-the-hash
idiom, which silently under-hashes when a newly added field is forgotten.
*/
package canonical
