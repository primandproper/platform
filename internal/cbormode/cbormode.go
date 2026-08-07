/*
Package cbormode holds the one CBOR dialect this module speaks, so that the
encoding package and the cache codec cannot drift into two incompatible
spellings of the same format. A cache entry and a queue payload written from
this module decode into each other.

# Why not cbor.Marshal

The package-level cbor.Marshal uses EncOptions{}, whose zero Time is TimeUnix:
time.Time encodes as whole seconds since the epoch. Round-tripping
2026-08-03 12:30:45.123456789Z through it yields 2026-08-03 12:30:45Z, and the
nanoseconds are gone. That is a fidelity failure rather than a representation
choice — cache entries hold timestamps constantly — so this package builds one
EncMode with TimeRFC3339Nano and every caller goes through it. Reaching for the
obvious constructor is the bug.

TimeUnixDynamic is the other candidate this fix is usually written with, and it
does not do the job either: it rounds to the microsecond before encoding, so
nanoseconds are lost there too. RFC3339Nano costs more bytes than a float and
buys back an exact instant and the UTC offset. The named location is still not
carried — no wire format short of embedding IANA names does that, and JSON has
the same property — so a decoded time compares equal under time.Time.Equal but
not under == or reflect.DeepEqual.

# What is deliberately left alone

Map ordering. EncOptions{} sets Sort: SortNone, so encoding the same
map[string]any twice can produce different byte sequences. That is fine: the
contract a codec owes is that encoded bytes decode back into the value they came
from, not that a value has one canonical spelling. Nothing here needs the
stronger property — audit's canonical image hashes the stored bytes rather than
re-encoding, precisely so it does not depend on one. If content-addressed
storage or dedup-by-digest ever wants it, that is a separate canonical EncMode
added then, for that caller.

Struct-as-array (toarray) and integer keys (keyasint) are likewise not used.
They are how CBOR gets really small, but they require the far end to already
know the schema, which trades away the portability that motivates using CBOR at
all here.

Unknown fields decode as ignored rather than rejected, matching the XML, TOML,
and YAML paths in encoding; only the JSON server decoder is strict. Struct tags
come free: fxamacker falls back to json tags for fields carrying no cbor tag, so
existing types encode sensibly with no annotation work.
*/
package cbormode

import (
	"io"
	"reflect"

	"github.com/fxamacker/cbor/v2"
)

// encMode and decMode are built once at package initialization. Both are
// documented as concurrency-safe and are meant to be built once and reused;
// building one per call would cost more than the encoding does.
var (
	encMode = newEncMode()
	decMode = newDecMode()
)

func newEncMode() cbor.EncMode {
	em, err := cbor.EncOptions{
		// See the package doc: the default TimeUnix silently truncates to
		// whole seconds.
		Time: cbor.TimeRFC3339Nano,
		// Emit the standard tag (0 for RFC3339 text) so an encoded time is
		// self-describing to a decoder that does not know the target type.
		// Without it a timestamp is indistinguishable from any other string.
		TimeTag: cbor.EncTagRequired,
	}.EncMode()
	if err != nil {
		// The options above are constants, so this cannot fail on any input; a
		// failure means someone edited them into a combination the library
		// rejects, and the process should not start.
		panic(err)
	}

	return em
}

func newDecMode() cbor.DecMode {
	dm, err := cbor.DecOptions{
		// Decode CBOR maps into map[string]any when the destination is an
		// untyped any, rather than the library default map[any]any. Every
		// other content type in encoding produces map[string]any there, and a
		// map[any]any that cannot be re-marshaled as JSON is a trap nobody
		// asked for.
		DefaultMapType: reflect.TypeFor[map[string]any](),
	}.DecMode()
	if err != nil {
		panic(err)
	}

	return dm
}

// Marshal renders v as CBOR in this module's dialect.
func Marshal(v any) ([]byte, error) {
	return encMode.Marshal(v)
}

// Unmarshal parses CBOR data into v.
func Unmarshal(data []byte, v any) error {
	return decMode.Unmarshal(data, v)
}

// NewDecoder returns a streaming decoder reading CBOR from r, for the callers
// that are handed a reader rather than bytes.
func NewDecoder(r io.Reader) *cbor.Decoder {
	return decMode.NewDecoder(r)
}
