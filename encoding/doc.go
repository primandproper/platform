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

# Bytes are exact

Every encode path routes through one byte-oriented marshaler per content type,
so EncodeJSON(v) returns exactly what json.Marshal(v) returns. In particular no
trailing newline is appended — the streaming encoders in the standard library
add one, and this package deliberately does not use them for that reason.
Callers that store, compare, or checksum encoded bytes can rely on this.
*/
package encoding
