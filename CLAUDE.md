# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Go library/framework (`github.com/primandproper/platform-go/v10`) providing infrastructure abstractions for cloud-native services: database, caching, messaging, observability, secrets, uploads, email, payments, and more. Single module, ~40 packages, Go 1.26.

## Common Commands

```bash
make format         # Format all Go code (imports, field alignment, tag alignment, gofmt)
make lint           # Run golangci-lint (Docker) + shellcheck
make format lint    # Typical workflow: format then lint
make test           # Run tests (race detector, shuffle, failfast)
make build          # Build all packages
make generate       # Regenerate moq mocks after changing any mocked interface
make setup          # Install dev tools + vendor deps
make revendor       # Clean and re-vendor dependencies
```

Run a single test:
```bash
go test -run TestFunctionName ./package/path/...
```

Run tests for a single package:
```bash
go test -race ./cache/...
```

Linting runs in Docker (`golangci/golangci-lint` image). Formatting runs locally with `gci`, `goimports`, `betteralign`, `tagalign`, and `gofmt`.

## Import Ordering

Import ordering uses `gci` with four sections, separated by blank lines:

1. Standard library
2. `github.com/primandproper/platform-go/v10` (this module)
2. `github.com/primandproper` (org-level packages)
4. Everything else (third-party)

The Makefile `THIS` variable must be the full module path (`github.com/primandproper/platform-go/v10`). `format_imports.sh` derives the org prefix from it by stripping any trailing major-version suffix (e.g. `/v2`) and then taking `dirname`, yielding `github.com/primandproper`. If `THIS` is too short, the org prefix collapses toward `github.com`, creating a spurious `prefix(github.com)` gci section.

## Architecture Patterns

**Interface + multi-implementation:** Most packages define an interface with multiple implementations selected by config. Examples: `cache.Cache[T]` (Redis, memory), `logging.Logger` (slog, zap, zerolog), `secrets.SecretSource` (env, GCP, AWS SSM), `uploads` (S3, GCS, filesystem).

**Config structs:** Each major package has a `config` subpackage using `env:` struct tags and `ValidateWithContext()` via `go-ozzo/ozzo-validation`. Most, but not all, also have `EnsureDefaults()` — packages whose defaults are expressible as `envDefault:` tags use those instead.

Constructors call the validation their config defines, and apply defaults *before* validating: an unset field that has a documented default is not a validation failure, and validating first turns the common case into one. Selecting an implementation is deliberate — an unrecognized provider name returns `platformerrors.ErrUnknownProvider` rather than a working-looking noop. Where a noop is genuinely wanted it has to be named.

**OpenTelemetry throughout:** Database, HTTP, gRPC, and messaging all instrument with OTel for traces, metrics, and logs.

**Error handling:** Uses `cockroachdb/errors` for rich error context. Platform-level sentinels are defined in `errors/`, conventionally imported as `platformerrors`. Transport mappings live in `errors/http` and `errors/grpc`, which import the packages whose sentinels they map — so nothing in those packages may import `errors/http` or `errors/grpc` back.

**Options vs. config seams:** Constructors take `logger`/`tracerProvider`/`metricsProvider` as `WithX` options, never positionally — the `config` subpackages included. A config subpackage constructor reads `ctx, cfg, deps..., opts ...Option`, where `deps` are the things it genuinely cannot build (a `database.Client`, an `*http.Client`, a handler) and everything optional is an option.

Each config subpackage declares its own `Option` type, mirroring the leaf packages, with `WithLogger`/`WithTracerProvider`/`WithMetricsProvider` and a `WithPillars(*observability.Pillars)` that supplies all three at once. Options apply in order, so `WithPillars(p)` followed by `WithMetricsProvider(nil)` leaves that one component unmetered. Absent means noop: every constructor resolves what it was not given through `logging.EnsureLogger` / `tracing.EnsureTracerProvider` / `metrics.EnsureMetricsProvider`, so a caller that wants no observability names none of it. The four pillar subpackages (`observability/{logging,metrics,tracing,profiling}/config`) have no `WithPillars` and cannot: `observability` imports them to build a `Pillars`.

Go allows one variadic per function, and that slot belongs to the config package's own `Option`. A constructor that also passes options through to what it builds exposes them as `WithXOptions(...)` on that same type — `auditcfg.WithRecorderOptions`, `authorizationcfg.WithStaticOptions` — which is also how one wiring site carries options for whichever of several components it builds.

`do.Provide` registrations resolve observability through `observability.InvokePillars(i)` rather than `do.MustInvoke`, so a container that registers none still wires up. It distinguishes "nobody registered one" (absent, fine) from "the registered one failed to build" (an error), so a misconfigured exporter surfaces instead of degrading to a noop that looks configured.

There is one deliberate exception outside the config subpackages: `observability.NewObserver(name, logger, tracerProvider)` is positional everywhere. It is the repo-wide DI seam every package's constructor funnels its options into, called once per constructor and never by consumers, and giving it options of its own would mean every package threading options through to build the thing that consumes its options.

Every package uses `WithTracerProvider(tracing.TracerProvider)` — never a ready-made `tracing.Tracer`, which would let a span's instrumentation scope come from the caller instead of the component. `Option` types are **not** parameterized on their package's generic type, even in generic packages (`cache`, `idempotency`, `eventcapture`): Go cannot infer a type argument from a call's result type, so an `Option[T]` forces every call site to spell `T` out forever. Options that genuinely need the type parameter (`WithCodec`, `WithRecordable`, `WithTransform`, `WithObserver`, `WithKeyOrder`) stay generic but infer it from their argument, and the constructor type-asserts and reports a mismatch.

## Testing

- **`stretchr/testify` is banned in its entirety** (`assert`, `require`, and `mock`).
  The `depguard` linter enforces this — see `.golangci.yml`. Do not reintroduce
  any testify import.
  - Non-fatal assertions: `github.com/shoenig/test` (package `test`).
    `test.EqOp` for comparable types, `test.Eq` for slices/maps/deep comparison.
    Length/contains helpers have FLIPPED argument order: `test.SliceLen(t, n, slice)`.
  - Fatal assertions: `github.com/shoenig/test/must` (package `must`).
    Same function names as `test`.
  - Mocks: `matryer/moq`, generated from interfaces. See any `<pkg>/mock/doc.go`
    for the `//go:generate` directive pattern (e.g. `authentication/tokens/mock/doc.go`).
- Tests call `t.Parallel()` by default
- Container-backed tests use `testcontainers-go`, live in-package (typically `containers_test.go`),
  and gate on `RUN_CONTAINER_TESTS=true` — they skip otherwise. Stand containers up through the
  `testutils/containers` helpers (`pgtest`, `mysqltest`, `redistest`), not raw testcontainers calls.
- `make test` runs container tests by default and therefore needs a Docker daemon;
  `.scripts/test.sh false` runs the suite without them
- `make test` runs every package (`./...`)
- Test command: `CGO_ENABLED=1 RUN_CONTAINER_TESTS=<true|false> go test -shuffle=on -race -vet=all -failfast`

## Linting

- 42+ linters enabled via `.golangci.yml` (golangci-lint v2 format)
- Formatters: `gci` and `gofmt` (configured in the `formatters:` section)
- Notable strictness: `errcheck`, `errorlint`, `gosec`, `forcetypeassert`, `unconvert`, `unparam`
- Many linters relaxed for `_test.go` files (gosec, goconst, forcetypeassert, unparam, etc.)
