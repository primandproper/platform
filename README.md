# platform-go

[![Go Reference](https://pkg.go.dev/badge/github.com/primandproper/platform-go/v13.svg)](https://pkg.go.dev/github.com/primandproper/platform-go/v13) [![codecov](https://codecov.io/github/primandproper/platform-go/graph/badge.svg?token=69RLLWLJ39)](https://codecov.io/github/primandproper/platform-go)

A Go library providing infrastructure abstractions for cloud-native services. Each package defines a stable interface with one or more provider implementations, selected at runtime via config. Layers that touch the network — HTTP, gRPC, database, messaging — instrument with OpenTelemetry.

**Module:** `github.com/primandproper/platform-go/v13`
**Go:** 1.26

## Project Status & Stability

> **`main` is not a release channel.** Anything on `main` that has not been cut into a tagged release is considered under active development — alpha/beta, unstable, and unsupported. Treat it as such.

This repository follows a deliberately conservative release model:

- **Only tagged releases are supported.** If it isn't behind a version tag, it can change or break without notice, and no support or compatibility is promised for it.
- **`main` moves ahead of the latest release.** New work — including breaking changes — lands on `main` well before it is deemed release-worthy. Two facts locate you at any moment, and both are derived rather than written down here: the module path in `go.mod` is the major that `main` is currently building toward, and the highest version tag is the latest supported release. Whatever is on `main` but not yet in that tag is subject to change — and immediately after a major bump, that is the entire major.
- **Semantic Versioning, enforced by Go's module paths.** Breaking changes increment the major version and the module import path (`/vN` → `/vN+1`), so a major bump can never silently break a consumer that hasn't opted in. The path bump lands in the same change that makes the break, never as a follow-up, which is why `main`'s major is frequently one ahead of anything you can fetch by tag.
- **No stability guarantees on unreleased APIs.** Interfaces, config shapes, and package boundaries on `main` are subject to change until they ship in a release.

If you depend on this library, pin to a released tag — and note that `@latest` against a major that has no tag yet resolves to a commit on `main` rather than to a release. If you want to track upcoming work, `main` is fair game — just don't expect it to hold still.

## Installation

```bash
go get github.com/primandproper/platform-go/v13@latest
```

Because breaking changes ride the major-version import path, upgrading across majors is an explicit, opt-in edit to your import paths — never a surprise from `go get -u`.

## Design Patterns

**Interface + implementations.** Every major concern is defined as an interface (e.g., `cache.Cache[T]`, `logging.Logger`, `secrets.SecretSource`), with provider implementations in subpackages. Swap implementations via config without touching call sites. Most packages ship a `noop` implementation for tests and for cleanly disabling a concern.

**Config structs.** Each package has a `config` subpackage with `env:`-tagged structs and `ValidateWithContext()` (via `go-ozzo/ozzo-validation`). Configuration is the seam that selects an implementation. Most, but not all, also have `EnsureDefaults()` — packages whose defaults are expressible as `envDefault:` tags use those instead.

Selecting an implementation is deliberate: an unrecognized provider name returns `errors.ErrUnknownProvider` rather than a working-looking noop, because a typo that silently discards every message or never limits a request is a production incident that looks like a healthy process. Where a noop is genuinely wanted it has to be asked for by name.

**OpenTelemetry throughout.** HTTP, gRPC, database, and messaging layers emit traces and metrics. Observability primitives (logging, tracing, metrics, profiling) live under `observability/`.

**Error handling.** Uses [`cockroachdb/errors`](https://github.com/cockroachdb/errors) for rich, wrapped error context. Platform-level sentinel errors live in `errors/`, conventionally imported as `platformerrors`. Transport mappings live in `errors/http` and `errors/grpc`, which import the packages whose sentinels they map — so nothing in those packages may import them back.

## Package Catalog

Implementations are listed in parentheses; most concerns also provide a `noop`. Where an implementation is a SQL dialect, [SQL Dialect Support](#sql-dialect-support) is the full matrix and the reasons behind the three exceptions.

### Data & storage
| Package    | Purpose                              | Implementations                       |
|------------|--------------------------------------|---------------------------------------|
| `database` | SQL access + instrumentation         | postgres, mysql, sqlite               |
| `cache`    | Generic key/value cache (`Cache[T]`) | redis, memory                         |
| `uploads`  | Blob/object storage & image handling | objectstorage (S3-compatible), images |
| `files`    | Filesystem & streaming helpers       | —                                     |
| `secrets`  | Secret sourcing (+ caching/rotation) | env, gcp, ssm, kubernetes                |

### Messaging & events
| Package         | Purpose                    | Implementations                                   |
|-----------------|----------------------------|---------------------------------------------------|
| `messagequeue`  | Publish/subscribe & queues | kafka, pubsub, redis, sqs                         |
| `outbox`        | Transactional outbox       | postgres, mysql, sqlite                           |
| `eventstream`   | Server push to clients     | sse, websocket                                    |
| `notifications` | User notifications         | async, mobile                                     |
| `jobs`          | Queue workers & periodic jobs | —                                              |
| `email`         | Transactional email        | mailgun, mailjet, postmark, resend, sendgrid, ses |

### Web & transport
| Package           | Purpose                   | Implementations |
|-------------------|---------------------------|-----------------|
| `server`          | Service servers           | grpc, http      |
| `routing`         | HTTP router abstraction   | chi, stdlib, httprouter, gin |
| `httpclient`      | Instrumented HTTP client  | —               |
| `cookies`         | Cookie management         | —               |
| `encoding`        | Content encoding/decoding | —               |
| `compression`     | Payload compression       | —               |
| `ratelimiting`    | Request rate limiting     | redis           |
| `circuitbreaking` | Circuit breaker           | —               |
| `retry`           | Retry with backoff        | —               |
| `idempotency`     | At-most-once effect for retried requests | http, grpc (server + client) |

### Observability & operations
| Package         | Purpose                              | Implementations                                    |
|-----------------|--------------------------------------|----------------------------------------------------|
| `observability` | Logging, tracing, metrics, profiling | logging (slog, zap, zerolog); OTel tracing/metrics |
| `healthcheck`   | Health/readiness checks              | —                                                  |
| `version`       | Build/version metadata               | —                                                  |
| `metering`      | Durable usage metering & quotas      | postgres, mysql, sqlite                            |
| `webhooks`      | Outbound webhook delivery            | postgres, mysql, sqlite                            |
| `webhooks/inbound` | Inbound webhook receipt: verify, publish, ack | stripe, github, generic HMAC          |
| `clock`         | Injectable time                      | —                                                  |
| `config`        | Config loading & env parsing         | —                                                  |

### Auth & security
| Package          | Purpose                             | Implementations                |
|------------------|-------------------------------------|--------------------------------|
| `authentication` | Password hashing, TOTP, tokens      | argon2, totp, tokens           |
| `authentication/webauthn` | Passkey registration & login, with ceremony state that outlives one replica | database, cache |
| `authentication/passwordreset` | Password reset tokens: digest at rest, single use enforced by the store | postgres, mysql, sqlite |
| `sessions`       | Server-side sessions over cookies   | cache, database (+ http)       |
| `authorization`  | Role/permission policy, enforcement | static (default), database     |
| `links`          | Signed, expiring, single-use action links | cache + distributedlock  |
| `audit`          | Tamper-evident audit log            | postgres, mysql, sqlite        |
| `cryptography`   | Cryptographic primitives            | encryption (aes, kms), hashing |
| `cryptography/requestsigning` | HMAC request signing & verification | v1                             |
| `cryptography/shredding` | Per-subject data keys that can be destroyed | postgres, mysql, sqlite |
| `random`         | Secure randomness                   | —                              |
| `identifiers`    | ID generation                       | —                              |
| `dataprivacy`    | Subject access & erasure requests   | postgres, mysql, sqlite        |
| `retention`      | Policy-driven expiry deletion       | postgres, mysql, sqlite        |

### AI, ML & product
| Package        | Purpose                      | Implementations               |
|----------------|------------------------------|-------------------------------|
| `llm`          | Large language model clients | anthropic, openai             |
| `embeddings`   | Embedding generation         | cohere, ollama, openai        |
| `search`       | Vector / text search         | vector, text                  |
| `analytics`    | Product analytics            | posthog, segment, multisource |
| `featureflags` | Feature flagging             | launchdarkly, posthog         |

### Domain & coordination
| Package           | Purpose                    | Implementations         |
|-------------------|----------------------------|-------------------------|
| `capitalism`      | Payments                   | stripe                  |
| `entitlements`    | Feature access & remaining quota | —                 |
| `settings`        | Per-user and per-account runtime settings: admin-defined definitions, per-subject values | postgres, mysql, sqlite |
| `saga`            | Linear durable sagas with compensations | postgres, mysql, sqlite |
| `distributedlock` | Distributed locking        | memory, postgres, redis |
| `workqueue`       | Leased work queue (`SKIP LOCKED` claim/complete/expire) | postgres |
| `timers`          | Durable one-shot scheduling (run once at time T, fleet-wide) | postgres |
| `operations`      | Long-running operations with durable state, two-tier progress, and streamed updates | postgres |
| `filtering`       | Query filters / pagination | —                       |
| `qrcodes`         | QR code generation         | —                       |
| `waitlists`       | Pre-launch waitlists: signup lifecycle, and an unsubscribe that outlives the address | postgres, mysql, sqlite |
| `eventcapture`    | Recording domain events    | jsonl                   |

### Utilities
`errors`, `pointer`, `numbers`, `bitmask`, `charset`, `reflection`, `panicking`, `testutils`, `fake`.

## SQL Dialect Support

`database` speaks Postgres, MySQL and SQLite, and so does almost every package
that stores anything through it. Three do not. They are Postgres-only by
decision rather than by omission, and this is where that decision is spoken —
once, before you choose packages, rather than package by package as each
constructor refuses at wiring time.

A ✓ means the package ships DDL for that dialect, and — for every package whose
statements have been ported onto the generated tier — executes a querier emitted
against it. Everything unticked returns `dialect.ErrUnsupported` at
construction, never a partial store or a migration that creates nothing.

| Package                                | Postgres | MySQL | SQLite |
|----------------------------------------|----------|-------|--------|
| `audit`                                | ✓        | ✓     | ✓      |
| `authentication/oauth2server/database` | ✓        | ✓     | ✓      |
| `authentication/passwordreset`         | ✓        | ✓     | ✓      |
| `authentication/webauthn/database`     | ✓        | ✓     | ✓      |
| `authorization/database`               | ✓        | ✓     | ✓      |
| `cryptography/shredding`               | ✓        | ✓     | ✓      |
| `dataprivacy`                          | ✓        | ✓     | ✓      |
| `identity`                             | ✓        | ✓     | ✓      |
| `metering`                             | ✓        | ✓     | ✓      |
| `notifications`                        | ✓        | ✓     | ✓      |
| `operations`                           | ✓        | —     | —      |
| `outbox`                               | ✓        | ✓     | ✓      |
| `saga`                                 | ✓        | ✓     | ✓      |
| `sessions/database`                    | ✓        | ✓     | ✓      |
| `settings`                             | ✓        | ✓     | ✓      |
| `timers`                               | ✓        | —     | —      |
| `uploads/registry`                     | ✓        | ✓     | ✓      |
| `waitlists`                            | ✓        | ✓     | ✓      |
| `webhooks`                             | ✓        | ✓     | ✓      |
| `workqueue`                            | ✓        | —     | —      |

### Why the three narrow

One reason, arrived at from three directions. `workqueue`'s claim is a single
statement that selects due rows, locks them with `SKIP LOCKED`, increments
attempts, extends the lease and hands the keys back with `RETURNING`. MySQL 8.0
has `SKIP LOCKED` and CTEs but no `RETURNING`, so the same claim there is a
`SELECT … FOR UPDATE SKIP LOCKED` plus a separate `UPDATE` inside a transaction
held across both round trips — a different concurrency shape with a different
failure model, which is a second implementation rather than a dialect switch.
SQLite is a harder no: single-writer, with no row-level locking to skip.
`timers` claims the same way. `operations` runs on `workqueue`, so its roster is
`workqueue`'s.

Widening any of them is a decision about that claim, not about a missing
translation — the package docs carry the long form. Nothing forecloses it: the
shape to reach for is the package as the interface with a provider subpackage
beneath it, the way `cache` and `cache/redis` sit.

### Narrowings that are not rows

Some dialect-dependence is a capability inside a package that serves all three,
and a row would misreport it either way:

- **`outbox`** stores and relays on all three. Its `LISTEN`/`NOTIFY` wakeup is
  Postgres-only and reported as `outbox.ErrNotifyUnsupported` if configured
  elsewhere; without it a relay polls, which is later rather than wrong. Its
  `SKIP LOCKED` claim mode degrades to a lease on SQLite.
- **`retention`** sweeps all three, and ships no DDL: the table, the timestamp
  column and the batch key arrive from a `Policy` written at run time, so there
  is no schema of this module's to render for a dialect.
- **`distributedlock/postgres`** and **`search/vector/pgvector`** are named
  providers beside `memory`, `redis` and `qdrant`, chosen by config the way
  `cache/redis` is. Picking one is picking Postgres, which is what its name
  says.

The matrix above is not maintained by hand against the tree.
`internal/dialectmatrix` parses this table and fails if a row disagrees with the
DDL a package ships, with the dialects its generated querier was emitted for, or
if a DDL-shipping package has no row at all.

## Development

```bash
make setup          # Install dev tools and download deps
make format         # Format all Go code (imports, field/tag alignment, gofmt)
make lint           # Run golangci-lint (Docker) + shellcheck
make test           # Run tests (race detector, shuffle, failfast)
make build          # Build all packages
make generate       # Regenerate moq mocks after changing a mocked interface
make bench          # Run benchmarks
```

Formatting runs locally with `gci`, `goimports`, `betteralign`, `tagalign`, and `gofmt`. Linting runs in Docker against the `golangci/golangci-lint` image (42+ linters, golangci-lint v2 format).

### Testing conventions

- **`stretchr/testify` is banned** (`assert`, `require`, and `mock`), enforced by `depguard`. Use [`shoenig/test`](https://github.com/shoenig/test) for assertions (`test` for non-fatal, `must` for fatal) and [`matryer/moq`](https://github.com/matryer/moq) for mocks.
- Tests run in parallel by default and use subtests throughout.
- Container-backed tests use `testcontainers-go`, live in-package (typically `containers_test.go`), and gate on `RUN_CONTAINER_TESTS=true`.
- `make test` runs `CGO_ENABLED=1 go test -shuffle=on -race -vet=all -failfast ./...` across every package. `.scripts/test.sh false` runs the suite without container tests.

## Contributing

Because `main` is a development channel and only tagged releases are supported, changes land on `main` freely and are stabilized before release. Follow the existing package layout (interface + config subpackage + provider implementations + `noop`), match the surrounding code, and keep `make format lint test` green.
