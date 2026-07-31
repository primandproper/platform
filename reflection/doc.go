/*
Package reflection provides utilities for struct field inspection, tag
extraction, and dynamic method introspection.

The primitives in this package exist because the same three or four steps get
open-coded at every call site that touches reflect, and they get open-coded
slightly differently each time: whether a nil pointer is an error or an absence,
whether a tag's options belong to its name, whether an embedded field is
promoted or named. Each is a decision with a defensible answer, and the point of
gathering them here is that the answer is given once.

StructValue and StructType are the two entry points. They differ in what they
can accept, and the difference is not incidental: a nil *T has no fields to read
but does have fields to describe, so a caller inspecting values needs
StructValue's present report while a caller describing a shape can take
StructType's happy answer.

FieldName resolves the name a field is encoded under for a given tag key,
following encoding/json's convention — name up to the first comma, "-" to omit,
"-," to name a field "-". It reports whether the name was explicit, which is
what a caller needs to decide whether an embedded field is flattened or treated
as a named object.

DerefOrZero descends through an absent pointer by substituting a zero value.
That is right for a walk comparing two values field by field and wrong for a
walk matching on values, where every zero field of the substitute would match a
zero needle; the doc comment says so, and callers should heed it.
*/
package reflection
