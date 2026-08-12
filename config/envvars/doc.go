// Package envvars derives the closed set of environment variables that can
// override a configuration struct, and writes it out as Go constants.
//
// The parent config package loads a value from a file and then overlays
// environment variables onto it through caarlos0/env's struct tags. That makes
// the set of variables a process actually reads a real, finite, derivable
// thing: it is every `env:` tag reachable from a loadable configuration struct,
// with the `envPrefix:` tags along the way concatenated in front of it. Nothing
// at runtime knows that set. A variable whose name is one underscore off the
// tag is not read, the file value stands, and the service comes up healthy with
// the wrong configuration — there is no layer at which the typo is an error.
//
// This package answers what the set is. Collect returns it as data; Generate
// writes a constant per variable, so a deployment manifest can be built from
// identifiers the compiler checks rather than strings nothing checks.
//
// Generate is meant to be run from a `go:generate` directive on a small main
// package in the module whose configuration is being described:
//
//	envvars.Generate(ctx, envvars.Options{
//		Dir:          ".",
//		Prefix:       "DINNER_DONE_BETTER_",
//		UnionKey:     "internal/config.configurations",
//		Dependencies: []string{"github.com/primandproper/platform-go"},
//		OutputPath:   "internal/config/envvars/env_vars.go",
//	})
//
// yielding, per variable:
//
//	// AnalyticsPosthogAPIKeyEnvVarKey is the environment variable name to set to override
//	// `APIServiceConfig.Analytics.Posthog.APIKey`.
//	AnalyticsPosthogAPIKeyEnvVarKey = "DINNER_DONE_BETTER_ANALYTICS_POSTHOG_API_KEY"
//
// # Source, not reflection
//
// The set is read out of parsed source rather than off a live config value,
// because the tool asking is a generator: it runs before the thing it describes
// is built, and it describes packages it does not import. That is
// reflection/ast's remit, and this package is built on it. The consequence is
// that a type is resolved by the name it is written under — a field declared as
// dbconfig.Config resolves through the declaring file's imports — so a
// configuration assembled through an interface, an alias chain this package
// cannot see, or reflection is not visible here.
//
// # Completeness by construction
//
// Options.UnionKey is the reason to prefer this over a hand-kept list. A
// loadable configuration struct is, by definition, a member of the generic
// constraint the Load* functions require of it, so naming that constraint makes
// the output complete by construction: a new configuration struct cannot become
// loadable without appearing here. Options.Roots exists for callers with no
// such constraint, and carries the staleness the constraint removes.
//
// # What the walk mirrors
//
// caarlos0/env recurses into every struct-typed field, not only those carrying
// an envPrefix, and this walk does the same — a nested struct with no prefix
// contributes its tags unprefixed, exactly as the parser will read them. It
// skips unexported fields, which the parser cannot set, and fields tagged
// `env:"-"`, which it is told to ignore.
//
// # What is not derivable
//
// A slice of structs is populated from indexed variables (FOO_0_BAR,
// FOO_1_BAR, ...), whose count is a property of the environment rather than of
// the source. There is no closed set to emit, so a struct reached through a
// slice or a map is not walked, and its variables do not appear.
//
// A configuration struct that refers to itself is the same problem in a
// different shape: how deep the parser goes is decided by how many of those
// pointers the loaded file made non-nil. The walk stops at the first
// repetition, so such a struct's variables are named once, at the depth they
// were first reached.
package envvars
