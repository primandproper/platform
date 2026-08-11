package cfgnorm

import (
	"testing"

	"github.com/shoenig/test"
)

func TestProvider(T *testing.T) {
	T.Parallel()

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "already canonical", input: "postgres", want: "postgres"},
		{name: "mixed case", input: "PostGres", want: "postgres"},
		{name: "surrounding whitespace", input: "  redis\t", want: "redis"},
		{name: "both", input: " LaunchDarkly ", want: "launchdarkly"},
		{name: "empty stays empty", input: "", want: ""},
		{name: "whitespace only becomes empty", input: "   ", want: ""},
		// Interior spacing is left alone: no provider name has one, so
		// collapsing it would only turn a typo into a silent match.
		{name: "interior whitespace is kept", input: "cloud trace", want: "cloud trace"},
	}

	for _, tc := range cases {
		T.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			test.EqOp(t, tc.want, Provider(tc.input))
		})
	}
}
