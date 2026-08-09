package pathvalues

import (
	"testing"

	"github.com/shoenig/test"
)

func TestDecode(T *testing.T) {
	T.Parallel()

	cases := []struct {
		name     string
		raw      string
		expected string
	}{
		{name: "nothing to decode", raw: "plain", expected: "plain"},
		{name: "empty", raw: "", expected: ""},
		{name: "escaped separator stays in the value", raw: "a%2Fb", expected: "a/b"},
		{name: "escaped space", raw: "a%20b", expected: "a b"},
		{name: "escaped escape", raw: "a%25b", expected: "a%b"},
		{name: "escaped at sign", raw: "user%40example.com", expected: "user@example.com"},
		// url.QueryUnescape would answer "a b" here. A path is not a query, and
		// net/http's ServeMux leaves the plus alone.
		{name: "a literal plus is not a space", raw: "a+b", expected: "a+b"},
		// Left alone rather than emptied: routing's binder reads "" as absent.
		{name: "invalid escape is returned as-is", raw: "a%zzb", expected: "a%zzb"},
		{name: "truncated escape is returned as-is", raw: "a%2", expected: "a%2"},
	}

	for _, tc := range cases {
		T.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			test.EqOp(t, tc.expected, Decode(tc.raw))
		})
	}
}
