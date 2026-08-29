/*
Package config turns files and environment variables into the configuration
structs the rest of this module is built around, and turns them back into files
again.

Every package here with a config subpackage tags its fields the caarlos0/env
way — `env:`, `envPrefix:`, `envDefault:`, `env:",init"` — so an application
composing several of them has one big struct whose leaves are already described.
This package is the application-agnostic place those tags get mounted onto a
value. It knows nothing about any particular configuration, and it is not itself
one of the config subpackages: nothing in it selects an implementation.

# Loading

	cfg, err := config.LoadFromYAMLFile[AppConfig](ctx, "config/production.yaml")

The LoadFrom* functions are one function over four sources — a YAML, JSON or
TOML file, a .env file, or the environment alone — and all of them finish the
same way, by overlaying environment variables onto whatever the file produced.
That ordering is the whole point of the layering: the file is what a deployment
checks in, and the environment is what a deployment overrides per replica, per
region, or per incident, including the secrets that must not be in the file.

The overlay has one edge worth knowing before choosing tags, and it belongs to
caarlos0/env rather than to this package: a field carrying an envDefault whose
variable is unset is written back to that default, overwriting what the file
supplied. A field the file should win is a field with no envDefault.
LoadFromJSONFile carries the long form.

WithPrefix scopes the overlay to one application's variables. WithOnSet reports
each assignment as it happens along with whether the value came from the
environment or from an envDefault — the difference between a setting a
deployment chose and one it inherited, which the loaded struct cannot show.

Validate is separate, and deliberately not called by the LoadFrom* functions. It
runs the ozzo ValidatableWithContext a config implements, if it implements one,
and it is a caller's line rather than a step — a config assembled from several
LoadFrom* calls is validated once it is whole, not four times on the way there.

# Rendering

	config.RenderYAMLFiles(ctx, []config.Environment[AppConfig]{
		{Name: "production", Path: "config/production.yaml", Config: prod},
		{Name: "staging", Path: "config/staging.yaml", Config: staging},
	})

The same three formats go the other way. A service whose per-environment files
are hand-maintained text has no compiler between an edit and a deployment; one
that builds those files from Go values gets the type checking and the validation
before the file exists, and the checked-in file becomes a projection of an object
rather than a second source of truth. Encoding runs through
[github.com/primandproper/platform-go/v13/encoding], so what is written is what
the LoadFrom* side reads.

# The variables themselves

[github.com/primandproper/platform-go/v13/config/envvars] answers what the
overlay can actually read. The `env:` tags reachable from a configuration struct
are a closed, derivable set, and nothing at runtime knows it — a variable one
underscore off its tag is not read, the file value stands, and the process comes
up healthy and wrong. That package derives the set and emits it as Go constants,
so a deployment manifest is built from identifiers a compiler checks.
*/
package config
