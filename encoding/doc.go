/*
Package encoding turns values into bytes and back, in a content type chosen by
configuration rather than by the call site.

It is not an HTTP package. HTTP was its first consumer and ServerEncoderDecoder
still speaks in http.ResponseWriter and *http.Request, but the rest of the
surface is transport-free and is meant to be used by anything that needs to
encode data — queue payloads, cache entries, database columns, files on disk.
Prefer it to calling json.Marshal directly, so that the content type stays one
decision made in one place.

# Picking a type to depend on

The interfaces are layered so a caller can ask for the narrowest thing that
does its job:

  - Marshaler renders a value as bytes.
  - Unmarshaler parses bytes into a value.
  - Codec is both, plus the content type being spoken.
  - ClientEncoder is a Codec that also streams, via io.Writer and io.Reader.

Depend on Marshaler or Codec unless a transport is genuinely part of the job.

For one-off use there are package-level helpers that build an encoder for you:
Encode and Decode return errors, MustEncode and MustDecode panic, and each has
a JSON-pinned variant (EncodeJSON, DecodeJSON, and so on) for callers whose
wire format is fixed rather than configurable.

# This package does not alter the marshaler's output

Every encode path routes through one byte-oriented marshaler per content type,
so EncodeJSON(v) returns exactly what json.Marshal(v) returns. In particular no
trailing newline is appended — the streaming encoders in the standard library
add one, and this package deliberately does not use them for that reason.

That is the whole of the claim: nothing is added, removed, or reordered on the
way out. It is not a promise that a value has one canonical encoding. Some
marshalers do not offer that — CBOR does not sort map keys, so encoding the same
map twice can produce different bytes — and no caller here needs it. What is
guaranteed is the round trip: bytes produced by one content type decode back
into the value they came from. Code that wants a stable digest should hash the
bytes it stored rather than re-encoding the value to compare.
*/
package encoding
