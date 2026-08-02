# platform-go v9 pre-release review

Full-repo review ahead of the first v9 tag, covering all ~50 top-level packages plus root docs and build hygiene. Twelve parallel reviewers, each checking four axes: documentation/code inconsistencies, footguns, API designs that would force a breaking change *after* tagging, and violations of the repo's own conventions. Since v9 is untagged, every finding is marked by whether the fix is breaking — breaking fixes are free right now and expensive forever after.

**Build health:** `go build ./...` and `go vet ./...` pass. All moq mocks regenerate byte-identical. Zero testify imports, zero `WithTracer(` options, zero generic-parameterized `Option` types, no `/v8` remnants anywhere (including the two golden-file spots that historically get missed). The version-bump hygiene is fully done.

**Clean packages:** audit, jobs, clock, webhooks (notably solid security posture), search/vector (best-reviewed implementation quality in the repo), uploads/images, qrcodes, errors (core), version, identifiers, pointer, healthcheck, testutils/containers.

---

## 1. Systemic bugs (repo-wide patterns)

### 1a. `env:"init"` tag misspelling — env-only configuration silently broken ⚠ highest priority

`env:"init"` names an env *key* "init"; the init option is spelled `env:",init"`. On nil sub-config pointers this means the pointer is **never initialized during env parsing**, so the sub-config's env vars are silently never read (then `Required` validation fails, or a nil-config error surfaces at construction). Two reviewers verified this independently against the vendored caarlos0/env v11, one with a runnable test.

Confirmed broken (pointer fields):
- `observability/tracing/config`, `observability/metrics/config`, `observability/logging/config`, `observability/profiling/config`
- `search/text/config/config.go:31-34`, `search/vector/config/config.go:31-34`
- `email/config/config.go:47-52` (all six providers — `SENDGRID_API_TOKEN` etc. never read)
- `notifications/async/config/config.go:37-40`

`llm/config`, `embeddings/config`, `featureflags/config`, and `notifications/mobile/config` use the correct `env:",init"` — proof the right spelling is known. The bare form appears in **~40 files repo-wide**; do a sweep (`grep -rn 'env:"init"' --include='*.go' .`), fixing pointer fields and normalizing value-struct uses. Non-breaking.

### 1b. Silent-noop fallback on misconfigured provider

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

### 1c. Validation exists but is never invoked / validates the wrong thing

- Constructors that never call the `ValidateWithContext` their own config defines: `ratelimiting/config` (dead validation + negative rate → rejects everything), `distributedlock/config`, `email/config`, `featureflags/config`, `server/http`, `search/{text,vector}/config` (doc even claims it validates)
- **Validate-before-defaults** bug: `circuitbreaking/partitioned/config:56-61` validates before `EnsureDefaults` — the exact ordering bug the base package fixed and documented — so an unset `Base.Name` silently yields a **noop keyed breaker with nil error**
- Normalization asymmetry (validation checks the raw string, factory normalizes): `cryptography/encryption/config:36` ("AES" fails validation, factory accepts), `secrets/config:52/69`, `authorization/config:70-80`, plus ozzo `validation.In` skipping empty values (`routing/config:45-49`, `uploads/objectstorage/uploader.go:72`)

### 1d. Stale "testify → moq migration" doc.go prose

`cryptography/encryption/mock`, `random/mock`, `panicking/mock`, `distributedlock/mock`, `circuitbreaking/mock`, `email/mock`, `embeddings/mock` all still claim a hand-written testify mock coexists "during the migration". Testify is banned and gone; the mocks themselves are current. Doc-only fix.

### 1e. Vestigial `TableSuffixes` exports

`outbox/migrations:53-56`, `authorization/database/migrations:52-55`, `dataprivacy/migrations:52-55` all export `TableSuffixes` documented as "the one list the DDL and queries derive names from" — nothing derives from it anymore (superseded by `database/ddl`; queries hardcode names). Delete before the tag (breaking after).

---

## 2. Breaking-to-fix: free only now

These lock in the moment you tag. Grouped by theme, roughly by importance.

### API-shape decisions

| Item | Where | Problem |
|---|---|---|
| `ContentType` is `*contentType` where `contentType` is `*string` | `encoding/content_type.go:23` | Pointer-to-pointer with identity comparison; nil is legal and silently treated as JSON. Should be a string-backed value type. |
| `tracing.TracerProvider` has no `Shutdown` | `observability/config.go:99-103` | `Pillars.Shutdown` can only flush; span-processor goroutine and exporter conn never stopped. Adding the method later breaks implementers. |
| `Cache[T]` has no `Close` | `cache/cache.go:42-68` | Redis pool never releasable; memory janitor is contorted around the gap (its own docs say so). |
| `UploadManager` has no `Close` | `uploads/uploader.go:11` | gocloud `*blob.Bucket` never released. |
| `logging.Level` is a pointer type | `observability/logging/logging.go:41-48` | `==` compares identity; package ships `LevelsEqual` as a workaround for its own type. |
| `outbox.RelayConfig.Dialect` | `outbox/config.go:51-52` | "Must match the database.Client" — the exact mismatch `Client.Dialect()` was just built to make unrepresentable. Derive it from the client. |
| Stripe SDK leakage | `capitalism/config:48`, `stripe/stripe.go:42` | `stripe.EventHandler = func(ctx, *stripe.Event) error` over stripe-go **v75** (2023) in the exported config API. Any SDK major bump becomes a platform-go breaking change. Wrap the event in a platform-owned type. (Related: usage.go uses Stripe's deprecated usage-records API.) |
| `Embedder` is single-input only | `embeddings/embeddings.go:39-42` | No batch method; every backing API supports arrays. Adding it later breaks implementors. |
| `messagequeue.Consumer.Consume(ctx, chan bool, chan error)` | `messagequeue/consumers.go:10` | Bidirectional bool stop channel; unspecified delivery semantics that actually differ per backend (redis at-most-once, others at-least-once; kafka stops on handler error, others continue). |
| `filtering.MaxResponseSize` is `uint8` | `filtering/query_filter.go:65,77` | Limit is 250, five below the type ceiling; `maxResponseSize: 300` is an unmarshal error, not a clamp. |
| `analytics.EventReporter.Close()` | `analytics/event_reporter.go:11` | No error, no ctx — failed final flush of buffered events is undetectable. |
| `llm.Capabilities.PDFs` | `llm/provider.go:21-22` | Capability flag for a feature `Part` cannot express. Drop it or add `PartDocument`. |
| `server/http` vs `server/grpc` divergence | both packages | Interface vs concrete return, `Serve()` vs `Serve(ctx)`, `Shutdown` error vs none, serviceName positional vs hardcoded. Converge now. Also `HTTPSCertificateFile` field vs its own `TLS_*` tags. |
| `http.Server.Serve()` returns nothing | `server/http/http_server.go:118-158` | Failures panic through a hard-wired panicker; adding an error return later breaks the interface. |
| `ProxySourcesConfig` hardcodes IOS/Web | `analytics/config:38-41` | Adding any source is a breaking struct change; a map isn't. |
| `RespondWithData` | `encoding/server_encoder_decoder.go:176` | Exported method absent from the interface — unreachable by consumers. Add or delete. |
| `MultiPlatformPushSender` concrete deps | `notifications/mobile/multi_platform_push_sender.go:24-28` | Depends on `*apns.Sender`/`*fcm.Sender`; no interface seam, no mock. |
| pagination absent from `IndexSearcher` | `search/text` | ES silently caps at 10 hits, Algolia at 20 — same interface, different caps; fixing properly changes the interface. |

### Consumer-app (dinnerdonebetter) leakage to purge

- `messagequeue/config:60-84` — `QueuesConfig` hardcodes six app topic names (`DataChangesTopicName`, …), all `Required`
- `email/config:54-78` + `emailer.go` — `PasswordReset*EmailAddress`, `OutboundInvitesEmailAddress`, `BaseURL`, `BuildHermes`/`EmailBranding` (bakes `matcornic/hermes/v2` into the API); `OutboundEmailMessage` has `TestID`/`UserID` and a json tag on exactly one field
- `search/text/index_request.go:3-9` — exported `IndexRequest` carries `TestID` in the wire format
- `numbers/range.go:31` — `OpenRangeUpdateRequestInput[T]`, field-identical to `OpenRange[T]` with app vocabulary
- `database/config:41-42` — `Encryption`, `OAuth2TokenEncryptionKey` consumed nowhere
- `idempotency/http/middleware.go:121-122` — doc comment references "capitalism's Stripe webhook" (doc-only)

### Dead config surface to wire or delete

- `authentication/tokens/config:33-34` — `MaxAccessTokenLifetime`/`MaxRefreshTokenLifetime`: nothing caps anything
- `featureflags/{launchdarkly,posthog}/config` — `CircuitBreakerConfig` with `CIRCUIT_BREAKING_` env prefix, read by nothing
- `metering/config:174-176` — `FlusherConfig.FlushInterval` documents a ticker mode that doesn't exist
- `dataprivacy/config:267-271` — `SweeperConfig.SweepInterval`, same
- `cookies/config.go:22` — `CookieName` is `Required` yet never read
- `server/http/config.go:26` — `Config.Debug` never read
- `tracing.NewTracer` (deprecated before first tag) and `tracing.User` (unused) — delete

### Positional-order convention violations (config-subpackage seam: `ctx, cfg, logger, tracerProvider, metricsProvider, deps..., opts...`)

Violators: `databasecfg.NewDatabase` (ctx, logger, tracerProvider, cfg, migrator, metricsProvider), `routingcfg.NewRouter`/`NewBackend` (no ctx, dep before trio), `cryptography/encryption/config` (no ctx, tracer before logger), `authentication/tokens/config` (no ctx/metrics/opts), `messagequeue/config` (cfg last), `eventstream/config` (no ctx, cfg last), `observability/metrics+profiling/config` (ctx, logger, cfg), `embeddings/config` (metricsProvider missing), `search/{text,vector}/config` (cfg after trio), `capitalism/config` (no ctx; also `NewCapitalismImplementation` naming), `notifications/async/config` (no ctx), `ratelimiting/config` (no ctx/logger). Compliant exemplars: auditcfg, authorizationcfg, outboxcfg, idempotencycfg, cachecfg, dataprivacycfg, meteringcfg, webhookscfg, emailcfg, jobscfg, sagacfg.

Also: `observability.NewObserver(name, logger, tracerProvider)` is positional outside a config subpackage — it's the deliberate repo-wide DI seam, but CLAUDE.md has no carve-out for it; document the exception.

### Naming to settle now

- Config subpackage names: 29 use `<name>cfg`, 6 use bare `config` (cache, capitalism, cryptography/encryption, eventstream, notifications/mobile, uploads), 1 uses `msgconfig`
- Mock package names split three ways: bare `mock` (majority) vs `mockX` (8 pkgs) vs `Xmock` (6 pkgs); `mockpublishers` also contains consumer mocks
- `saga/config` `ProvideStore`/`ProvideWorker` — the only `Provide*` constructors in the repo (263 `New*`)
- `cache/redis` config field `QueueAddresses`/`QUEUE_ADDRESSES` — messaging copy-paste in a cache
- Stutter: `jwt.NewJWTSigner`, `paseto.NewPASETOSigner`, `posthog.NewPostHogEventReporter`, `segment.NewSegmentEventReporter`
- `secrets/kubectl` uses client-go in-process, never the kubectl binary
- `retry` keeps `Config` in the package root with no `config/` subpackage or `do.go` — the only sibling shaped that way

---

## 3. High-severity bugs (fixes are non-breaking — but fix before tagging anyway)

1. **`server/grpc.Serve` swallows every real serve error** (`server/grpc/server.go:126-130`): non-nil errors fall through with no log/return, and the sentinel checked is `net/http.ErrServerClosed`, which gRPC never returns. A dead gRPC server is silent. Also: `Shutdown` hard-`Stop()`s in-flight RPCs, ignores ctx, and flushes traces *before* stopping (the opposite of the http sibling's documented order); `reflection.Register` is unconditional.
2. **`server/http` request spans come from the OTel global, not the injected provider** (`http_server.go:121-126`): `otelhttp.NewHandler` gets no `WithTracerProvider`; if the global isn't set, request tracing silently no-ops while `Shutdown` flushes a provider that produced none of the spans.
3. **JWT/PASETO sentinel divergence** (`authentication/tokens/jwt/jwt.go:106-122`): JWT returns golang-jwt's sentinels, PASETO returns `tokens.*` — the interface promises provider-independent `errors.Is`. A refresh flow branching on `tokens.ErrTokenExpired` breaks on a config change the design calls safe. Map in `ParseToken`. (Also: both constructors accept empty signing keys — JWT will mint HS256 tokens under an empty HMAC key.)
4. **dataprivacy requests wedge permanently** (`queries.go:192-206`, `worker.go:331-341`): nothing reclaims rows stuck in `processing` (the lease predicate is dead code — three comments assume a mechanism that doesn't exist), and on fulfillment timeout the failure bookkeeping runs on the already-expired context, so it's guaranteed to fail too. Crash or timeout ⇒ permanent wedge.
5. **cache/redis drops writes silently when the breaker is open** (`redis.go:236-521`): `Set`/`Delete`/`DeleteByPrefix` return nil while doing nothing — a dropped `Delete` serves stale data for its full TTL. This also **defeats idempotency's `FailClosed`**: during a redis outage the breaker converts unavailability into clean misses and no-op write "successes", so duplicate side effects run with zero signal — exactly what FailClosed exists to prevent, and the default breaker config trips exactly then.
6. **metering under-bills by design-vs-docs conflict** (`migrations/postgres.sql:19`): dedupe PK is `idempotency_key` alone, global across meters, while the docs recommend request IDs as keys — one request feeding two meters silently drops the second. Scope the PK to `(meter, idempotency_key)` (schema change — free now). Also `consume` decides before the dedupe probe, falsely denying retried requests near the limit.
7. **elasticsearch readiness probe panics on nil client** (`search/text/elasticsearch/elasticsearch.go:101-107`): client-construction failure is debug-logged, then `Do()` is called on nil. Also ES error bodies are decoded then discarded (errors are built from usually-empty `Warnings()`), and the Algolia backend never propagates ctx and ignores the `id` parameter to `Index`.
8. **compression's decompression bound is per-frame for zstd** (`compressor.go:20,121-123`): N concatenated frames each under 64 MiB decompress to N×64 MiB, bypassing the documented cap; the s2 path's `io.CopyN(limit+1)` guard is the fix. `ErrDecompressedTooLarge` is also only ever returned on the s2 path.
9. **mailgun header injection** (`email/mailgun/mailgun.go:116-124`): From/To built with raw Sprintf; a comma in an attacker-influenced display name injects extra recipients (mailgun accepts comma-separated lists). Siblings escape via `mail.Address`.
10. **dataprivacy notification HTML injection** (`notifier.go:162-205`): user display name interpolated into HTML mail unescaped.
11. **`random` package**: `init()` calls `log.Fatalf` (library kills host process; `crypto/rand.Read` can't fail on modern Go); `Element` uses `math/rand/v2` inside a package documented "cryptographically secure"; hex/base32 functions documented as "base64".
12. **`secrets.ErrSecretNotFound` honored only by the env provider** — SSM/GCP/kubectl return raw provider errors, breaking the cross-provider contract the sentinel exists for.
13. **messagequeue consumer traps**: `NewConsumer` topic cache silently ignores a second handler; kafka's first handler error permanently kills the consumer (dead reader stays cached); SQS hot-spins on receive errors with no backoff; SQS instrument names contain `:` (queue URL) and fail construction only with a real metrics provider.
14. **routing**: `setScalar` has no overflow checks — `?count=300` into an `int8` field silently wraps instead of 400; all four backends nil-deref when the provider sub-config is nil; `Use()` after first `Handler()` is a silent no-op; `MountOpenAPI`'s "self-contained" docs page actually loads Stoplight from unpkg (unpinned CDN, breaks air-gapped deploys).
15. **`EnsureMetricsProvider`'s "noop" fallback records real metrics** once anything sets a global meter provider (which this repo's own otelgrpc setup does) — opposite behavior from `metrics/noop`. Also `metrics/testing.go` links `testing` + shoenig into production binaries.
16. **featureflags OpenFeature global-registry leak**: each construct registers a named provider in the process-global registry; `Close()` never unregisters — reload cycles accumulate registrations pointing at closed clients.
17. **authorization/static unbounded memo** (`static.go:104-124`): multi-role memo keyed by caller-supplied (attacker-influenceable) role names, stored forever; filter to known roles. Both static and cached also join role names on `\x00` with no NUL rejection (cache-entry collisions).
18. **DB connection leaks** (mysql/sqlite): read pool leaked when write-connect fails (postgres got the fix); sqlite's read-only fallback skips the single-writer `SetMaxOpenConns(1)` cap → `SQLITE_BUSY`; metrics-registration failure paths leak pools in all three.
19. **retry treats any wrapped `DeadlineExceeded` as terminal** — per-attempt timeouts (the classic transient case) abort the loop on attempt 1.
20. **webhooks `Reap`**: `RowsAffected` error zeroes the count and skips the attempts/deliveries DELETEs — unbounded table growth on drivers without row counts.
21. **errors/grpc interceptors** put raw `err.Error()` in the client-visible status message, contradicting the package's own documented sanitization stance.
22. **`uploads` `DIRECTORY_MODE=0700`** parses base-10 → decimal 700 = `0o1274` (sticky bit + garbage); `BucketPrefix` without trailing `/` glues keys and collides tenants in List.
23. **idempotency `commit`/`release` are check-then-act without the lock** — the "only its owner may complete" guarantee isn't atomic; do the pair under `WithLock`.
24. **`llm` fallback model is deprecated** (`claude-sonnet-4-20250514`, retiring 2026-06-15) — silently-selected default that will start 404ing mid-2026 with no code change.

---

## 4. Documentation debt worth a pre-tag sweep

- **README.md:17 still says "/v7, latest release v7.1.1"** — two majors stale, contradicting line 7 of the same file. Catalog omits clock, config, dataprivacy, eventcapture, metering, webhooks; "sentinels live in `internal`/`errors`" is wrong; `make bench` unlisted; BENCHMARKS.md predates the recent refactors.
- **distributedlock/postgres docs describe pre-refactor behavior** (TTL "advisory only", Refresh "does not extend") — the code now enforces client-side expiry and Refresh extends.
- Wrong-package doc.go headers: `cryptography/encryption` ("Package cryptography"), `server/http` ("Package http2"), `search/text` ("Package search"); `notifications/mobile/doc.go` references a nonexistent deprecated `notifications` root package.
- Copy-paste doc errors: random's encodings, `MustDecode` "encodes", `messagequeue.Consumer` "produces events", `PublishAsync` (is sync), sqs `Close` doc, `AsError`, `AttachUserAgentDataToSpan`, metering's backlog gauge, `files` stream-cancellation guarantee, `Service.Open` "streams" (buffers up to 512 MiB), joke comments in encoding ("weenie hut jr's").
- CLAUDE.md/README claim every config has `EnsureDefaults` — ~22 don't (many use `envDefault:` tags); soften or backfill.
- `saga_instances_compensating` is a counter named like a gauge — metric names lock into dashboards at tag time.

---

## 5. Suggested pre-tag sequencing

1. **Repo-wide `env:"init"` sweep** — mechanical, high-impact, non-breaking.
2. **The breaking-fix batch** (section 2) as one or a few commits: API-shape decisions first (`ContentType`, `TracerProvider.Shutdown`, `Close` on Cache/UploadManager, `logging.Level`, outbox Dialect, Stripe wrapper, Consumer signature, uint8), then the leakage/dead-config purge, then the positional-order + naming normalization (saga `Provide*`, config/mock package names). Compile-test against dinnerdonebetter after this batch.
3. **High bugs** (section 3) — all non-breaking; the grpc Serve, cache-breaker/FailClosed, dataprivacy wedge, metering PK, and JWT sentinel items are the ones I'd block the tag on.
4. **Docs sweep** (section 4) + regenerate BENCHMARKS.md, create PR.
