package canonical

import (
	"bytes"
	"encoding/json"
	"slices"

	"github.com/primandproper/platform-go/v10/cryptography/hashing"
	"github.com/primandproper/platform-go/v10/cryptography/hashing/sha256"
	"github.com/primandproper/platform-go/v10/errors"
)

// Option configures canonicalization.
type Option func(*options)

type options struct {
	dropKeys map[string]struct{}
}

// WithoutKeys excludes the named top-level object keys (their encoded JSON
// names, i.e. after struct tags apply) from the canonical form. Use it to
// keep a value's own content-hash field out of its own digest. It has no
// effect when the encoded value is not a JSON object.
func WithoutKeys(keys ...string) Option {
	return func(o *options) {
		if o.dropKeys == nil {
			o.dropKeys = make(map[string]struct{}, len(keys))
		}
		for _, k := range keys {
			o.dropKeys[k] = struct{}{}
		}
	}
}

// Sum returns the hex-encoded SHA-256 digest of v's canonical form. Two calls
// with semantically identical values return identical digests; see the
// package documentation for the canonicalization rules.
func Sum(v any, opts ...Option) (string, error) {
	return SumWith(v, sha256.NewSHA256Hasher(), opts...)
}

// SumWith is Sum with a caller-chosen hashing.Hasher, for digests other than
// SHA-256. The Hasher's cryptographic-strength caveats apply unchanged: a
// non-cryptographic Hasher yields a digest suitable for change detection
// among trusted parties, not for tamper resistance.
func SumWith(v any, hasher hashing.Hasher, opts ...Option) (string, error) {
	if hasher == nil {
		return "", errors.New("nil hasher provided")
	}

	canon, err := Marshal(v, opts...)
	if err != nil {
		return "", err
	}

	return hashing.Hex(hasher, canon), nil
}

// Marshal returns v's canonical JSON encoding: encoding/json's output
// re-emitted with all object keys sorted in lexicographic byte order and no
// insignificant whitespace. It is exposed so callers can inspect, log, or
// cross-check the exact bytes a digest was computed over.
func Marshal(v any, opts ...Option) ([]byte, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}

	// encoding/json does the semantic encoding (struct tags, omitempty,
	// MarshalJSON); canonicalization below only reorders and compacts what it
	// produced.
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, errors.Wrap(err, "encoding value")
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	// Preserve each number's encoded text verbatim rather than round-tripping
	// through float64, which could alter the representation.
	dec.UseNumber()

	var parsed any
	if err = dec.Decode(&parsed); err != nil {
		return nil, errors.Wrap(err, "reparsing encoded value")
	}

	if top, ok := parsed.(map[string]any); ok {
		for k := range o.dropKeys {
			delete(top, k)
		}
	}

	var buf bytes.Buffer
	if err = writeCanonical(&buf, parsed); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// writeCanonical emits one parsed JSON value in canonical form.
func writeCanonical(buf *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if t {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case json.Number:
		buf.WriteString(t.String())
	case string:
		encoded, err := json.Marshal(t)
		if err != nil {
			return errors.Wrap(err, "encoding string")
		}
		buf.Write(encoded)
	case []any:
		buf.WriteByte('[')
		for i, elem := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonical(buf, elem); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		slices.Sort(keys)

		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}

			encodedKey, err := json.Marshal(k)
			if err != nil {
				return errors.Wrap(err, "encoding object key")
			}
			buf.Write(encodedKey)
			buf.WriteByte(':')

			if err = writeCanonical(buf, t[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		// Unreachable: json.Decoder with UseNumber produces only the types
		// above. Guarded so a future decoder change fails loudly, not
		// silently mis-hashes.
		return errors.Newf("unexpected parsed JSON type %T", v)
	}

	return nil
}
