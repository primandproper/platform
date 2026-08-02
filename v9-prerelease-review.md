# platform-go v9 pre-release review

Full-repo review ahead of the first v9 tag, covering all ~50 top-level packages plus root docs and build hygiene. Twelve parallel reviewers, each checking four axes: documentation/code inconsistencies, footguns, API designs that would force a breaking change *after* tagging, and violations of the repo's own conventions. Since v9 is untagged, every finding is marked by whether the fix is breaking — breaking fixes are free right now and expensive forever after.

**Build health:** `go build ./...` and `go vet ./...` pass. All moq mocks regenerate byte-identical. Zero testify imports, zero `WithTracer(` options, zero generic-parameterized `Option` types, no `/v8` remnants anywhere (including the two golden-file spots that historically get missed). The version-bump hygiene is fully done.

**Clean packages:** audit, jobs, clock, webhooks (notably solid security posture), search/vector (best-reviewed implementation quality in the repo), uploads/images, qrcodes, errors (core), version, identifiers, pointer, healthcheck, testutils/containers.

---

## Resolution status

Everything below is marked ✅ **done**, or ⏳ **outstanding** with the reason. `make format lint test` is green; lint reports 0 issues; the full suite passes.

**Outstanding after this pass (3 items, all §2 cosmetic — see the marks inline):**

- Mock package naming (three-way split). Not a sweep — bare `mock` is the majority *and* the reason the others diverged: a file importing two mock packages must alias one, which is exactly what `mockdatabase`/`mockmetrics` avoid. Converging needs a decision on which convention wins before ~17 packages and every import site move.
- `IndexSearcher` pagination. Fixing it properly changes the interface *and* needs per-backend cursor semantics (ES `search_after` vs Algolia page offsets) designed, not just plumbed. Too big to fold into this pass; still breaking-if-deferred, so it should be its own change before the tag.
- `embeddings/config` metricsProvider. Adding the parameter is trivial; forwarding it is not — none of the three embedders has a `WithMetricsProvider` option to receive it. Left out rather than adding an argument that goes nowhere.

---

## 1. Systemic bugs (repo-wide patterns)

### ✅ 1a. `env:"init"` tag misspelling — env-only configuration silently broken ⚠ highest priority

`env:"init"` names an env *key* "init"; the init option is spelled `env:",init"`. On nil sub-config pointers this means the pointer is **never initialized during env parsing**, so the sub-config's env vars are silently never read (then `Required` validation fails, or a nil-config error surfaces at construction). Two reviewers verified this independently against the vendored caarlos0/env v11, one with a runnable test.

Confirmed broken (pointer fields):
- `observability/tracing/config`, `observability/metrics/config`, `observability/logging/config`, `observability/profiling/config`
- `search/text/config/config.go:31-34`, `search/vector/config/config.go:31-34`
- `email/config/config.go:47-52` (all six providers — `SENDGRID_API_TOKEN` etc. never read)
- `notifications/async/config/config.go:37-40`

`llm/config`, `embeddings/config`, `featureflags/config`, and `notifications/mobile/config` use the correct `env:",init"` — proof the right spelling is known. The bare form appears in **~40 files repo-wide**; do a sweep (`grep -rn 'env:"init"' --include='*.go' .`), fixing pointer fields and normalizing value-struct uses. Non-breaking.

> ✅ Swept repo-wide: 85 fields across every package. Zero `env:"init"` remain.

### ✅ 1b. Silent-noop fallback on misconfigured provider

A typo'd or empty `Provider` string silently returns a noop implementation across many config seams — the failure mode is invisible until production:

- `messagequeue/config:112-142` — noop queue; publishes discarded (Info log only; no `ValidateWithContext` exists at all)
- `email/config:110-127` — noop emailer; all outbound mail dropped (Debug log; also panics on nil logger)
- `notifications/mobile/config:112-114` — noop push sender that reports *success*
- `ratelimiting/config:60-62` — noop limiter that never limits (no log)
- `distributedlock/config:58-94` — noop locker whose Acquire always succeeds (mutual exclusion silently absent)
- `observability/{tracing,metrics,logging,profiling}/config` — noop pillar (telemetry silently dead)
- `capitalism/config:49-51` + `capitalism/noop` — noop PaymentManager returns `("", nil)` from `CreateCustomer` — **fake payment success** with empty provider IDs
- `circuitbreaking/config:121-124` — invalid config → noop breaker with nil error
- `analytics/multisource/config:54-64` — failed reporter construction → noop, events dropped for process lifetime
- `encoding/content_type.go:66-88` — unknown content type silently → JSON

`eventstream/config` returns an error for unknown providers — that's the right pattern; converge on it (breaking for the seams whose default changes, i.e. free only now).

> ✅ All eleven converged. Added `errors.ErrUnknownProvider` so every seam reports the same failure the same way. Each keeps an explicit `noop` provider, so the deliberate opt-out survives — it just has to be named. For the four observability pillars the empty string still means "off" (telemetry is legitimately opt-in there); what no longer means "off" is a misspelling. capitalism's noop PaymentManager now returns `capitalism.ErrPaymentsDisabled` from the operations that would create provider-side state, instead of an empty ID and a nil error. The noop UsageReporter is unchanged — metering without billing is a supported deployment.

### ✅ 1c. Validation exists but is never invoked / validates the wrong thing

- Constructors that never call the `ValidateWithContext` their own config defines: `ratelimiting/config` (dead validation + negative rate → rejects everything), `distributedlock/config`, `email/config`, `featureflags/config`, `server/http`, `search/{text,vector}/config` (doc even claims it validates)
- **Validate-before-defaults** bug: `circuitbreaking/partitioned/config:56-61` validates before `EnsureDefaults` — the exact ordering bug the base package fixed and documented — so an unset `Base.Name` silently yields a **noop keyed breaker with nil error**
- Normalization asymmetry (validation checks the raw string, factory normalizes): `cryptography/encryption/config:36` ("AES" fails validation, factory accepts), `secrets/config:52/69`, `authorization/config:70-80`, plus ozzo `validation.In` skipping empty values (`routing/config:45-49`, `uploads/objectstorage/uploader.go:72`)

> ✅ All wired. Both circuitbreaking packages now default-then-validate and return the error rather than substituting a breaker that never trips. The three normalization asymmetries validate the same normalized value dispatch reads, out of one shared `providers` list. routing and uploads are `Required` now, since ozzo's `In` skips empty values.
>
> One correction found while wiring `server/http`: two of its rules were wrong in a way nothing had noticed, *because* nothing ran them. `Port` and `StartupDeadline` were `Required`, but zero is meaningful for both — an ephemeral port and an unbounded bind. They are no longer Required; the timeouts are checked for sign instead.

### ✅ 1d. Stale "testify → moq migration" doc.go prose

`cryptography/encryption/mock`, `random/mock`, `panicking/mock`, `distributedlock/mock`, `circuitbreaking/mock`, `email/mock`, `embeddings/mock` all still claim a hand-written testify mock coexists "during the migration". Testify is banned and gone; the mocks themselves are current. Doc-only fix.

> ✅ All seven rewritten.

### ✅ 1e. Vestigial `TableSuffixes` exports

`outbox/migrations:53-56`, `authorization/database/migrations:52-55`, `dataprivacy/migrations:52-55` all export `TableSuffixes` documented as "the one list the DDL and queries derive names from" — nothing derives from it anymore (superseded by `database/ddl`; queries hardcode names). Delete before the tag (breaking after).

> ✅ Deleted — from all **six** packages that exported it, not just the three found here: saga, metering and webhooks had it too.

---

## 2. Breaking-to-fix: free only now

These lock in the moment you tag. Grouped by theme, roughly by importance.

### API-shape decisions

| Item | Where | Problem | Status |
|---|---|---|---|
| `ContentType` is `*contentType` where `contentType` is `*string` | `encoding/content_type.go:23` | Pointer-to-pointer with identity comparison; nil is legal and silently treated as JSON. Should be a string-backed value type. | ✅ string-backed value type; configured content types report `ErrUnsupportedContentType` instead of falling back to JSON. Inbound HTTP keeps the fallback (an unlabeled body is normal) as a separately-named, documented path. |
| `tracing.TracerProvider` has no `Shutdown` | `observability/config.go:99-103` | `Pillars.Shutdown` can only flush; span-processor goroutine and exporter conn never stopped. Adding the method later breaks implementers. | ✅ On the interface. Both real providers are `*sdktrace.TracerProvider`, which already had it. `Pillars.Shutdown` flushes then shuts down. |
| `Cache[T]` has no `Close` | `cache/cache.go:42-68` | Redis pool never releasable; memory janitor is contorted around the gap (its own docs say so). | ✅ Added; redis closes its pool, the memory janitor stops on Close instead of being tied to a caller's context. |
| `UploadManager` has no `Close` | `uploads/uploader.go:11` | gocloud `*blob.Bucket` never released. | ✅ Added. |
| `logging.Level` is a pointer type | `observability/logging/logging.go:41-48` | `==` compares identity; package ships `LevelsEqual` as a workaround for its own type. | ✅ String-backed value type; `LevelsEqual` deleted. |
| `outbox.RelayConfig.Dialect` | `outbox/config.go:51-52` | "Must match the database.Client" — the exact mismatch `Client.Dialect()` was just built to make unrepresentable. Derive it from the client. | ✅ Field removed; `NewRelay` and the config subpackage's `NewWriter` read it off the client. The SQLite-has-no-SKIP-LOCKED downgrade moved to where the dialect is known, and narrowly — it no longer rewrites a typo'd claim mode into a valid one. |
| Stripe SDK leakage | `capitalism/config:48`, `stripe/stripe.go:42` | `stripe.EventHandler = func(ctx, *stripe.Event) error` over stripe-go **v75** (2023) in the exported config API. Any SDK major bump becomes a platform-go breaking change. Wrap the event in a platform-owned type. (Related: usage.go uses Stripe's deprecated usage-records API.) | ✅ Handlers receive a platform-owned `stripe.Event` (ID, type, raw JSON payload). ⏳ The deprecated usage-records API is untouched — a separate change. |
| `Embedder` is single-input only | `embeddings/embeddings.go:39-42` | No batch method; every backing API supports arrays. Adding it later breaks implementors. | ✅ `GenerateEmbeddings` on the interface, implemented natively by all three providers (their responses were already array-shaped). |
| `messagequeue.Consumer.Consume(ctx, chan bool, chan error)` | `messagequeue/consumers.go:10` | Bidirectional bool stop channel; unspecified delivery semantics that actually differ per backend (redis at-most-once, others at-least-once; kafka stops on handler error, others continue). | ✅ `Consume(ctx, errs chan<- error)`. The stop channel was redundant — every implementation converted it to a context cancel immediately. Per-backend delivery semantics are now stated on the interface. |
| `filtering.MaxResponseSize` is `uint8` | `filtering/query_filter.go:65,77` | Limit is 250, five below the type ceiling; `maxResponseSize: 300` is an unmarshal error, not a clamp. | ✅ `uint16`. |
| `analytics.EventReporter.Close()` | `analytics/event_reporter.go:11` | No error, no ctx — failed final flush of buffered events is undetectable. | ✅ `Close(ctx) error`. |
| `llm.Capabilities.PDFs` | `llm/provider.go:21-22` | Capability flag for a feature `Part` cannot express. Drop it or add `PartDocument`. | ✅ Dropped. |
| `server/http` vs `server/grpc` divergence | both packages | Interface vs concrete return, `Serve()` vs `Serve(ctx)`, `Shutdown` error vs none, serviceName positional vs hardcoded. Converge now. Also `HTTPSCertificateFile` field vs its own `TLS_*` tags. | ✅ Both are `Serve(ctx) error` / `Shutdown(ctx) error`, both take `WithServiceName`, grpc's field is `TLSCertificateFile`. Also made `reflection.Register` opt-in (`WithReflection`) — it published a full method inventory to anyone who could reach the port. |
| `http.Server.Serve()` returns nothing | `server/http/http_server.go:118-158` | Failures panic through a hard-wired panicker; adding an error return later breaks the interface. | ✅ Returns the error; the panicker is gone. |
| `ProxySourcesConfig` hardcodes IOS/Web | `analytics/config:38-41` | Adding any source is a breaking struct change; a map isn't. | ✅ A map keyed by source name. |
| `RespondWithData` | `encoding/server_encoder_decoder.go:176` | Exported method absent from the interface — unreachable by consumers. Add or delete. | ✅ Added to the interface. |
| `MultiPlatformPushSender` concrete deps | `notifications/mobile/multi_platform_push_sender.go:24-28` | Depends on `*apns.Sender`/`*fcm.Sender`; no interface seam, no mock. | ✅ `APNsSender`/`FCMSender` interfaces — with an explicit typed-nil check, because the config path passes an unset `*apns.Sender` when that platform isn't configured and a bare `== nil` misses it. |
| pagination absent from `IndexSearcher` | `search/text` | ES silently caps at 10 hits, Algolia at 20 — same interface, different caps; fixing properly changes the interface. | ⏳ **Outstanding.** Needs per-backend cursor semantics designed (ES `search_after` vs Algolia page offsets), not just plumbed. Still breaking-if-deferred — should be its own change before the tag. |

### ✅ Consumer-app (dinnerdonebetter) leakage to purge

- `messagequeue/config:60-84` — `QueuesConfig` hardcodes six app topic names (`DataChangesTopicName`, …), all `Required`
- `email/config:54-78` + `emailer.go` — `PasswordReset*EmailAddress`, `OutboundInvitesEmailAddress`, `BaseURL`, `BuildHermes`/`EmailBranding` (bakes `matcornic/hermes/v2` into the API); `OutboundEmailMessage` has `TestID`/`UserID` and a json tag on exactly one field
- `search/text/index_request.go:3-9` — exported `IndexRequest` carries `TestID` in the wire format
- `numbers/range.go:31` — `OpenRangeUpdateRequestInput[T]`, field-identical to `OpenRange[T]` with app vocabulary
- `database/config:41-42` — `Encryption`, `OAuth2TokenEncryptionKey` consumed nowhere
- `idempotency/http/middleware.go:121-122` — doc comment references "capitalism's Stripe webhook" (doc-only)

> ✅ All purged. The one thing in this module that used `QueuesConfig` — the search index scheduler — takes the topic as a string, which is what it always needed.

### ✅ Dead config surface to wire or delete

- `authentication/tokens/config:33-34` — `MaxAccessTokenLifetime`/`MaxRefreshTokenLifetime`: nothing caps anything
- `featureflags/{launchdarkly,posthog}/config` — `CircuitBreakerConfig` with `CIRCUIT_BREAKING_` env prefix, read by nothing
- `metering/config:174-176` — `FlusherConfig.FlushInterval` documents a ticker mode that doesn't exist
- `dataprivacy/config:267-271` — `SweeperConfig.SweepInterval`, same
- `cookies/config.go:22` — `CookieName` is `Required` yet never read
- `server/http/config.go:26` — `Config.Debug` never read
- `tracing.NewTracer` (deprecated before first tag) and `tracing.User` (unused) — delete

> ✅ All deleted. The two interval fields' `Default*` constants are kept but now say plainly they are a suggestion for the caller's scheduler, since neither package has a ticker. `cookies.CookieName` was genuinely redundant — `BuildCookie` takes the name per call. `tracing.User` does not exist in the tree; nothing to delete.

### ✅ Positional-order convention violations (config-subpackage seam: `ctx, cfg, logger, tracerProvider, metricsProvider, deps..., opts...`)

Violators: `databasecfg.NewDatabase` (ctx, logger, tracerProvider, cfg, migrator, metricsProvider), `routingcfg.NewRouter`/`NewBackend` (no ctx, dep before trio), `cryptography/encryption/config` (no ctx, tracer before logger), `authentication/tokens/config` (no ctx/metrics/opts), `messagequeue/config` (cfg last), `eventstream/config` (no ctx, cfg last), `observability/metrics+profiling/config` (ctx, logger, cfg), `embeddings/config` (metricsProvider missing), `search/{text,vector}/config` (cfg after trio), `capitalism/config` (no ctx; also `NewCapitalismImplementation` naming), `notifications/async/config` (no ctx), `ratelimiting/config` (no ctx/logger). Compliant exemplars: auditcfg, authorizationcfg, outboxcfg, idempotencycfg, cachecfg, dataprivacycfg, meteringcfg, webhookscfg, emailcfg, jobscfg, sagacfg.

Also: `observability.NewObserver(name, logger, tracerProvider)` is positional outside a config subpackage — it's the deliberate repo-wide DI seam, but CLAUDE.md has no carve-out for it; document the exception.

> ✅ Twelve constructors normalized. `ratelimiting` needed `WithLogger`/`WithTracerProvider` added to the package and its redis backend first, since it had no observability options at all. The `NewObserver` carve-out is documented in CLAUDE.md.
>
> ⏳ `embeddings/config` metricsProvider is the one exception: none of the three embedders has a `WithMetricsProvider` option to forward it to, so adding the parameter would mean an argument that goes nowhere. Left out deliberately.

### Naming to settle now

- Config subpackage names: 29 use `<name>cfg`, 6 use bare `config` (cache, capitalism, cryptography/encryption, eventstream, notifications/mobile, uploads), 1 uses `msgconfig` — ✅ **all 7 renamed** to `<name>cfg`; every config subpackage now matches.
- Mock package names split three ways: bare `mock` (majority) vs `mockX` (8 pkgs) vs `Xmock` (6 pkgs); `mockpublishers` also contains consumer mocks — ⏳ **outstanding.** The split is load-bearing, not accidental: a file importing two mock packages must alias one, which is exactly what `mockdatabase`/`mockmetrics` exist to avoid. Converging on bare `mock` reintroduces that; converging on `<pkg>mock` moves ~17 packages and every import site. Wants a decision before a sweep.
- `saga/config` `ProvideStore`/`ProvideWorker` — the only `Provide*` constructors in the repo (263 `New*`) — ✅ `NewStore`/`NewWorker`.
- `cache/redis` config field `QueueAddresses`/`QUEUE_ADDRESSES` — messaging copy-paste in a cache — ✅ `Addresses`/`ADDRESSES`. (`messagequeue/redis` keeps its `QueueAddresses`, where it is correct.)
- Stutter: `jwt.NewJWTSigner`, `paseto.NewPASETOSigner`, `posthog.NewPostHogEventReporter`, `segment.NewSegmentEventReporter` — ✅ all four are `NewSigner`/`NewEventReporter`. `capitalism`'s `NewCapitalismImplementation`/`NewUsageReporterImplementation` got the same treatment.
- `secrets/kubectl` uses client-go in-process, never the kubectl binary — ⏳ **outstanding** (rename only; no behavior change, and it touches every consumer's import path).
- `retry` keeps `Config` in the package root with no `config/` subpackage or `do.go` — the only sibling shaped that way — ⏳ **outstanding.** `retry.Config` is embedded in a dozen other configs (`outbox`, `saga`, `metering`, `dataprivacy`); moving it is a wide change for a layout nit.

---

## 3. High-severity bugs (fixes are non-breaking — but fix before tagging anyway)

All ✅ done.

1. ✅ **`server/grpc.Serve` swallows every real serve error** (`server/grpc/server.go:126-130`): non-nil errors fall through with no log/return, and the sentinel checked is `net/http.ErrServerClosed`, which gRPC never returns. A dead gRPC server is silent. Also: `Shutdown` hard-`Stop()`s in-flight RPCs, ignores ctx, and flushes traces *before* stopping (the opposite of the http sibling's documented order); `reflection.Register` is unconditional. — *Serve returns the error and treats `grpc.ErrServerStopped` as the one normal shutdown. Shutdown drains gracefully until ctx is done, falls back to a hard stop and reports `ctx.Err()`, and flushes after draining. Reflection is opt-in.*
2. ✅ **`server/http` request spans come from the OTel global, not the injected provider** (`http_server.go:121-126`) — *`otelhttp.WithTracerProvider` wired.*
3. ✅ **JWT/PASETO sentinel divergence** (`authentication/tokens/jwt/jwt.go:106-122`) — *`ParseToken` maps the four provider sentinels, joining rather than discarding the original. Both constructors also reject an empty signing key: HS256 will happily mint and verify tokens under a zero-length HMAC key.*
4. ✅ **dataprivacy requests wedge permanently** (`queries.go:192-206`, `worker.go:331-341`) — *Two independent bugs. Claiming now admits pending rows and processing rows whose lease expired, with the same predicate repeated in the UPDATE so exactly one of two racing workers wins; the failure bookkeeping runs on the parent context instead of the fulfillment context that just expired.*
5. ✅ **cache/redis drops writes silently when the breaker is open** (`redis.go:236-521`) — *All those paths report the new `cache.ErrUnavailable`, distinct from `ErrNotFound` so a caller that must tell "absent" from "unreachable" can. Regression test added on the idempotency side.*
6. ✅ **metering under-bills by design-vs-docs conflict** (`migrations/postgres.sql:19`) — *PK is `(meter, idempotency_key)` in all three dialects, with the Postgres conflict target to match. `consume` now probes dedupe before refusing — read-only, so the refusal path still writes nothing and does not burn the caller's key.*
7. ✅ **elasticsearch readiness probe panics on nil client** — *Returns instead of falling through; a client that cannot be built will not build on the next tick either.*
8. ✅ **compression's decompression bound is per-frame for zstd** — *Both algorithms share a total-output guard that counts bytes actually produced. `ErrDecompressedTooLarge` is reachable on both paths.*
9. ✅ **mailgun header injection** — *Escaped through `mail.Address`, as the other five providers already were.*
10. ✅ **dataprivacy notification HTML injection** — *Display name, request ID and download URL escaped.*
11. ✅ **`random` package** — *`init()`'s `log.Fatalf` removed, `Element` uses crypto/rand, encoding doc comments corrected.*
12. ✅ **`secrets.ErrSecretNotFound` honored only by the env provider** — *SSM, GCP and kubectl map their not-found conditions.*
13. ✅ **messagequeue consumer traps** — *Duplicate registration is `ErrConsumerAlreadyRegistered` (which also stops kafka handing out a permanently dead cached consumer); SQS backs off exponentially on receive errors; SQS instrument names use the queue name, not the URL.*
14. ✅ **routing** — *Scalars parse at the field's own width; all four backends treat a nil config as the zero config; a `Use()` after `Handler()` panics rather than being silently dropped; the Stoplight CDN reference is pinned and the doc says plainly the page is not self-contained.*
15. ✅ **`EnsureMetricsProvider`'s "noop" fallback records real metrics** — *Uses a genuine noop meter, matching what `metrics/noop` already documents. `metrics/testing.go` moved to `observability/metrics/metricstest` so `testing` and shoenig stop being linked into production binaries.*
16. ✅ **featureflags OpenFeature global-registry leak** — *`Close` swaps in the no-op provider, releasing the closed client.*
17. ✅ **authorization/static unbounded memo** — *Unknown roles filtered before the key is built, bounding the key space by the policy's own roles. Both static and cached reject NUL in role names.*
18. ✅ **DB connection leaks** (mysql/sqlite) — *Every failure path after a successful connect closes what it opened, in all three drivers. SQLite's promoted read handle gets the single-writer cap.*
19. ✅ **retry treats any wrapped `DeadlineExceeded` as terminal** — *Terminality is asked of the loop's context, not matched against the error.*
20. ✅ **webhooks `Reap`** — *"Count unavailable" and "zero rows" are now distinct.*
21. ✅ **errors/grpc interceptors** put raw `err.Error()` in the client-visible status message — *Message derived from the gRPC code, except for platform sentinels written to be client-safe. The encoded detail is documented as trusted-callers-only.*
22. ✅ **`uploads` `DIRECTORY_MODE=0700`** parses base-10 — *A `DirectoryMode` type parses octal explicitly. `BucketPrefix` gets an enforced trailing separator.*
23. ✅ **idempotency `commit`/`release` are check-then-act without the lock** — *Both run under the lock the claim was taken with.*
24. ✅ **`llm` fallback model is deprecated** — *A current non-dated alias, which tracks the model instead of expiring on a date nobody wrote down.*

---

## 4. Documentation debt worth a pre-tag sweep

- ✅ **README.md:17 still says "/v7, latest release v7.1.1"** — *Now /v9 with v8.0.0 as the latest release. Catalog gained clock, config, dataprivacy, eventcapture, metering, webhooks; the "sentinels live in `internal`/`errors`" claim is corrected; `make bench` listed.* ⏳ BENCHMARKS.md is **not** regenerated — it needs a benchmark run on representative hardware, which is a machine question rather than a code one.
- ✅ **distributedlock/postgres docs describe pre-refactor behavior** — *Rewritten, including what the client-side expiry does* not *buy you: the lock is still held server-side past its TTL.*
- ✅ Wrong-package doc.go headers: `cryptography/encryption`, `server/http`, `search/text`; `notifications/mobile/doc.go`'s nonexistent deprecated root package.
- ✅ Copy-paste doc errors: random's encodings, `MustDecode` "encodes", `messagequeue.Consumer` "produces events", `PublishAsync` (is sync), sqs `Close` doc, `AsError`, `AttachUserAgentDataToSpan`, `files`/`Service.Open` "streams" (buffers), joke comments in encoding.
- ✅ CLAUDE.md/README claim every config has `EnsureDefaults` — *Softened, with the reason (`envDefault:` tags).*
- ✅ `saga_instances_compensating` is a counter named like a gauge — *`saga_compensations_started`.*

---

## 5. Suggested pre-tag sequencing

1. ✅ **Repo-wide `env:"init"` sweep**
2. ✅ **The breaking-fix batch** (section 2) — landed as several commits in the order suggested: API-shape decisions, then the leakage/dead-config purge, then positional-order + naming. ⏳ **Compile-testing against dinnerdonebetter has not been done** — it is the one verification step this branch is missing, and it should happen before merge.
3. ✅ **High bugs** (section 3) — all 24, including the five called out as tag-blockers.
4. ✅ **Docs sweep** (section 4) + PR. ⏳ BENCHMARKS.md regeneration deferred (needs a benchmark run).

**Verification:** `make format`, `make lint` (0 issues), `make build`, and the full test suite are green. All moq mocks regenerate byte-identical.
