package canonical

import (
	"encoding/json"
	"math"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/primandproper/platform-go/v13/cryptography/hashing/fnv"
	"github.com/primandproper/platform-go/v13/cryptography/hashing/sha256"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestMarshal(T *testing.T) {
	T.Parallel()

	T.Run("sorts struct fields by encoded name", func(t *testing.T) {
		t.Parallel()

		v := struct {
			Apple  string `json:"apple"`
			Zebra  int    `json:"zebra"`
			Middle bool   `json:"middle"`
		}{Zebra: 1, Apple: "x", Middle: true}

		canon, err := Marshal(v)
		must.NoError(t, err)
		test.EqOp(t, `{"apple":"x","middle":true,"zebra":1}`, string(canon))
	})

	T.Run("sorts nested object keys and compacts", func(t *testing.T) {
		t.Parallel()

		v := map[string]any{
			"b": map[string]any{"z": 1, "a": 2},
			"a": []any{map[string]any{"y": nil, "x": "s"}},
		}

		canon, err := Marshal(v)
		must.NoError(t, err)
		test.EqOp(t, `{"a":[{"x":"s","y":null}],"b":{"a":2,"z":1}}`, string(canon))
	})

	T.Run("map output is deterministic across many encodings", func(t *testing.T) {
		t.Parallel()

		v := map[string]int{
			"one": 1, "two": 2, "three": 3, "four": 4, "five": 5,
			"six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10,
		}

		first, err := Marshal(v)
		must.NoError(t, err)

		for range 50 {
			again, marshalErr := Marshal(v)
			must.NoError(t, marshalErr)
			test.EqOp(t, string(first), string(again))
		}
	})

	T.Run("honors MarshalJSON implementations", func(t *testing.T) {
		t.Parallel()

		stamp := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)

		canon, err := Marshal(map[string]any{"at": stamp})
		must.NoError(t, err)
		test.EqOp(t, `{"at":"2026-07-25T12:00:00Z"}`, string(canon))
	})

	T.Run("preserves number representations verbatim", func(t *testing.T) {
		t.Parallel()

		v := map[string]any{"big": int64(9007199254740993), "small": 0.1}

		canon, err := Marshal(v)
		must.NoError(t, err)
		test.EqOp(t, `{"big":9007199254740993,"small":0.1}`, string(canon))
	})

	T.Run("rejects values encoding/json cannot marshal", func(t *testing.T) {
		t.Parallel()

		_, err := Marshal(map[string]any{"nan": math.NaN()})
		test.Error(t, err)

		_, err = Marshal(make(chan int))
		test.Error(t, err)
	})
}

func TestSum(T *testing.T) {
	T.Parallel()

	T.Run("field declaration order does not affect the digest", func(t *testing.T) {
		t.Parallel()

		// Same-width fields so fieldalignment never reorders these and the
		// declarations genuinely differ.
		type ab struct {
			A string `json:"a"`
			B string `json:"b"`
		}
		type ba struct {
			B string `json:"b"`
			A string `json:"a"`
		}

		first, err := Sum(ab{A: "x", B: "y"})
		must.NoError(t, err)
		second, err := Sum(ba{A: "x", B: "y"})
		must.NoError(t, err)

		test.EqOp(t, first, second)

		// The strongest form: a struct and a map with the same content share a
		// canonical form, so they share a digest.
		viaMap, err := Sum(map[string]string{"a": "x", "b": "y"})
		must.NoError(t, err)
		test.EqOp(t, first, viaMap)
	})

	T.Run("slice order is semantic", func(t *testing.T) {
		t.Parallel()

		first, err := Sum([]int{1, 2, 3})
		must.NoError(t, err)
		second, err := Sum([]int{3, 2, 1})
		must.NoError(t, err)

		test.NotEqOp(t, first, second)
	})

	T.Run("nil and empty slices are distinct canonical values", func(t *testing.T) {
		t.Parallel()

		type wrapper struct {
			Items []string `json:"items"`
		}

		first, err := Sum(wrapper{Items: nil})
		must.NoError(t, err)
		second, err := Sum(wrapper{Items: []string{}})
		must.NoError(t, err)

		test.NotEqOp(t, first, second)
	})

	T.Run("distinct values get distinct digests", func(t *testing.T) {
		t.Parallel()

		first, err := Sum(map[string]int{"a": 1})
		must.NoError(t, err)
		second, err := Sum(map[string]int{"a": 2})
		must.NoError(t, err)

		test.NotEqOp(t, first, second)
	})
}

func TestSumWith(T *testing.T) {
	T.Parallel()

	T.Run("with the sha256 hasher matches Sum", func(t *testing.T) {
		t.Parallel()

		v := map[string]string{"hello": "world"}

		viaSum, err := Sum(v)
		must.NoError(t, err)
		viaSumWith, err := SumWith(v, sha256.NewSHA256Hasher())
		must.NoError(t, err)

		test.EqOp(t, viaSum, viaSumWith)
	})

	T.Run("alternate hashers produce their own digests", func(t *testing.T) {
		t.Parallel()

		v := map[string]string{"hello": "world"}

		viaSHA, err := Sum(v)
		must.NoError(t, err)
		viaFNV, err := SumWith(v, fnv.NewFNV64aHasher())
		must.NoError(t, err)

		test.NotEqOp(t, viaSHA, viaFNV)
	})

	T.Run("rejects a nil hasher", func(t *testing.T) {
		t.Parallel()

		_, err := SumWith(map[string]string{}, nil)
		test.Error(t, err)
	})
}

func TestWithoutKeys(T *testing.T) {
	T.Parallel()

	// catalog mirrors the motivating shape: a value carrying its own content
	// hash, which must not participate in its own digest.
	type catalog struct {
		Speeds    map[string]float64 `json:"profiles"`
		Hash      string             `json:"hash,omitempty"`
		Providers []string           `json:"providers"`
	}

	T.Run("excluded keys do not affect the digest", func(t *testing.T) {
		t.Parallel()

		unstamped := catalog{Speeds: map[string]float64{"car": 13.9}, Providers: []string{"a", "b"}}
		stamped := unstamped
		stamped.Hash = "deadbeef"

		first, err := Sum(unstamped, WithoutKeys("hash"))
		must.NoError(t, err)
		second, err := Sum(stamped, WithoutKeys("hash"))
		must.NoError(t, err)

		test.EqOp(t, first, second)

		// Without the exclusion the stamp is content, and the digests diverge.
		bare, err := Sum(stamped)
		must.NoError(t, err)
		test.NotEqOp(t, first, bare)
	})

	T.Run("no effect on non-object values", func(t *testing.T) {
		t.Parallel()

		first, err := Sum([]int{1, 2}, WithoutKeys("hash"))
		must.NoError(t, err)
		second, err := Sum([]int{1, 2})
		must.NoError(t, err)

		test.EqOp(t, first, second)
	})
}

func TestMarshal_Failures(T *testing.T) {
	T.Parallel()

	T.Run("a value encoding/json cannot encode is reported", func(t *testing.T) {
		t.Parallel()

		_, err := Marshal(make(chan int))
		test.Error(t, err)
	})

	T.Run("Sum and SumWith surface the same failure", func(t *testing.T) {
		t.Parallel()

		_, err := Sum(make(chan int))
		test.Error(t, err)

		_, err = SumWith(make(chan int), sha256.NewSHA256Hasher())
		test.Error(t, err)
	})
}

func TestWriteCanonical(T *testing.T) {
	T.Parallel()

	T.Run("emits false for a false boolean", func(t *testing.T) {
		t.Parallel()

		got, err := Marshal(map[string]bool{"on": true, "off": false})
		must.NoError(t, err)
		test.EqOp(t, `{"off":false,"on":true}`, string(got))
	})

	// The default arm is unreachable through Marshal — json.Decoder with
	// UseNumber only ever produces the handled types — so these drive
	// appendCanonical directly. The point is that a future decoder change fails
	// loudly instead of silently mis-hashing, including from inside a nested
	// array or object, where the error has to propagate back out.
	T.Run("an unexpected type is refused", func(t *testing.T) {
		t.Parallel()

		_, err := appendCanonical(nil, struct{}{})
		test.Error(t, err)
	})

	T.Run("an unexpected type inside an array propagates", func(t *testing.T) {
		t.Parallel()

		_, err := appendCanonical(nil, []any{json.Number("1"), struct{}{}})
		test.Error(t, err)
	})

	T.Run("an unexpected type inside an object propagates", func(t *testing.T) {
		t.Parallel()

		_, err := appendCanonical(nil, map[string]any{"bad": struct{}{}})
		test.Error(t, err)
	})

	T.Run("Marshal surfaces an appendCanonical failure", func(t *testing.T) {
		t.Parallel()

		// json.RawMessage decodes back to a plain parsed value, so the only way
		// to reach Marshal's appendCanonical error path is a decoder that
		// yields something unexpected — which is why the guard exists at all.
		// Assert the wiring directly instead.
		_, err := appendCanonical(nil, map[string]any{"nested": []any{struct{}{}}})
		test.Error(t, err)
		test.StrContains(t, err.Error(), "unexpected parsed JSON type")
	})
}

// appendJSONString's output is pinned twice over, because two different things
// can go wrong with it and each needs a claim of its own.
//
// The bytes are pinned by a table. They are this package's escaping rules — see
// the doc comment on appendJSONString — and because digests are stable
// identifiers the table is the point: an edit to any row is an edit to every
// hash ever computed over a string containing that byte, and has to be read as
// one.
//
// The meaning is checked against encoding/json, exhaustively, that being the
// claim the standard library can still hold up: for any input, the two
// encodings must decode to the same string. It was once possible to make the
// stronger claim that they produce the same bytes, and that is how these tests
// read until Go 1.27 began emitting a raw U+FFFD where earlier versions emitted
// \ufffd. Nothing in the canonical form moved — the byte in question cannot
// reach appendJSONString from Marshal — but the claim had been the wrong one
// regardless: byte-identity says this package must follow encoding/json
// wherever it goes, and following it is precisely what would move a digest.

// jsonMarshalString is encoding/json's own encoding of one string.
func jsonMarshalString(t *testing.T, s string) []byte {
	t.Helper()

	encoded, err := json.Marshal(s)
	must.NoError(t, err)

	return encoded
}

// decodeJSONString is the string a JSON string literal denotes.
func decodeJSONString(t *testing.T, encoded []byte) string {
	t.Helper()

	var decoded string
	must.NoError(t, json.Unmarshal(encoded, &decoded))

	return decoded
}

func TestAppendJSONString(T *testing.T) {
	T.Parallel()

	// One row per escaping rule, byte-exact. Every escaped expectation is a raw
	// string literal, so what it spells is what the encoder must emit; the rows
	// whose rune survives unescaped are interpreted literals holding that rune.
	T.Run("emits the pinned escaping", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct {
			name string
			in   string
			want string
		}{
			{"empty", "", `""`},
			{"nothing to escape", "plain text", `"plain text"`},
			{"double quote", `a "quoted" word`, `"a \"quoted\" word"`},
			{"backslash", `a\b`, `"a\\b"`},
			{"backspace", "a\bb", `"a\bb"`},
			{"form feed", "a\fb", `"a\fb"`},
			{"newline", "a\nb", `"a\nb"`},
			{"carriage return", "a\rb", `"a\rb"`},
			{"tab", "a\tb", `"a\tb"`},
			{"every other byte below U+0020", "\x00\x01\x1f", `"\u0000\u0001\u001f"`},
			{"HTML metacharacters", "<a>&", `"\u003ca\u003e\u0026"`},
			{"line separator", "\u2028", `"\u2028"`},
			{"paragraph separator", "\u2029", `"\u2029"`},
			{"a byte that is not valid UTF-8", "a\x80b", `"a\ufffdb"`},
			{"an incomplete multi-byte sequence", "\xe4\xb8", `"\ufffd\ufffd"`},
			{"a surrogate encoded as UTF-8", "\xed\xa0\x80", `"\ufffd\ufffd\ufffd"`},
			{"delete, which JSON does not treat as a control byte", "\x7f", "\"\x7f\""},
			{"multi-byte runes", "h\u00e9llo \u4e16\u754c", "\"h\u00e9llo \u4e16\u754c\""},
			{"astral runes", "\U0001f389", "\"\U0001f389\""},
			{"a replacement character it was handed", "\ufffd", "\"\ufffd\""},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				test.EqOp(t, tc.want, string(appendJSONString(nil, tc.in)))
			})
		}
	})

	// Appending must respect a non-empty destination rather than assuming it
	// owns the slice, which is how it is called from within an object or array.
	T.Run("appends to an existing buffer", func(t *testing.T) {
		t.Parallel()

		got := appendJSONString([]byte("prefix:"), "value")
		test.EqOp(t, `prefix:"value"`, string(got))
	})
}

func TestAppendJSONString_denotesWhatEncodingJSONDenotes(T *testing.T) {
	T.Parallel()

	T.Run("over strings that exercise every escape rule", func(t *testing.T) {
		t.Parallel()

		for _, s := range []string{
			"",
			"plain",
			"with spaces",
			`a "quoted" word`,
			`a\backslash`,
			"tab\there",
			"newline\nhere",
			"carriage\rreturn",
			"\x00\x01\x02\x1f",
			"<script>alert('xss')&</script>",
			"héllo",
			"\u4e16\u754c", // CJK, escaped so the source stays ASCII
			"\U0001f389 emoji \U0001f389",
			"line separator",
			"paragraph separator",
			"� already replacement",
			"trailing backslash\\",
			`{"nested":"json"}`,
		} {
			test.EqOp(t,
				decodeJSONString(t, jsonMarshalString(t, s)),
				decodeJSONString(t, appendJSONString(nil, s)),
			)
		}
	})

	// Every single byte, which is the cheapest way to cover both the control
	// characters and the bytes that are not valid UTF-8 on their own.
	T.Run("over every single byte", func(t *testing.T) {
		t.Parallel()

		for b := range 256 {
			s := string([]byte{byte(b)})
			test.EqOp(t,
				decodeJSONString(t, jsonMarshalString(t, s)),
				decodeJSONString(t, appendJSONString(nil, s)),
			)
		}
	})

	// Every rune in the basic multilingual plane, which covers U+2028/U+2029
	// and the multi-byte encodings around them.
	T.Run("over every rune in the BMP", func(t *testing.T) {
		t.Parallel()

		for r := rune(0); r <= 0xFFFF; r++ {
			if !utf8.ValidRune(r) {
				continue
			}

			s := string(r)

			got, want := decodeJSONString(t, appendJSONString(nil, s)), decodeJSONString(t, jsonMarshalString(t, s))
			if got != want {
				t.Fatalf("rune %U: denotes %q, want %q", r, got, want)
			}
		}
	})
}

// FuzzAppendJSONString drives the same equivalence over arbitrary input,
// including byte sequences that are not valid UTF-8. It costs nothing in an
// ordinary test run — only the seed corpus executes — but is here so the
// equivalence can be fuzzed on demand with -fuzz.
func FuzzAppendJSONString(f *testing.F) {
	for _, s := range []string{"", "plain", `"`, "\x00", "<&>", "héllo", " ", "\xff\xfe"} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		encoded, err := json.Marshal(s)
		if err != nil {
			t.Skip()
		}

		var want string
		if err = json.Unmarshal(encoded, &want); err != nil {
			t.Fatalf("encoding/json produced a literal it cannot read back: %s", encoded)
		}

		got := appendJSONString(nil, s)

		var decoded string
		if err = json.Unmarshal(got, &decoded); err != nil {
			t.Fatalf("appendJSONString(%q) = %s, which is not a readable JSON string", s, got)
		}

		if decoded != want {
			t.Fatalf("appendJSONString(%q) = %s, which denotes %q, want %q", s, got, decoded, want)
		}
	})
}
