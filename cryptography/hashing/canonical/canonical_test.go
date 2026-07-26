package canonical

import (
	"math"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v7/cryptography/hashing/fnv"
	"github.com/primandproper/platform-go/v7/cryptography/hashing/sha256"

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
		viaFNV, err := SumWith(v, fnv.NewFNVHasher())
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
