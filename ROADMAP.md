# shortly — a senior-level Go curriculum

A URL shortener with analytics. The product is deliberately boring so that nothing
distracts from the engineering. It starts as something you could write in an hour and
ends as a system with a real concurrency story, a real auth story, and a real
performance story.

**How we work:** one component at a time. For each component I give you the design
brief — the idiom, the tradeoffs, the failure modes, and the acceptance criteria.
You write the code. I review it the way a staff engineer reviews a PR: correctness
first, then idiom, then design. You do not move on until the review passes.

Track progress by ticking boxes here.

---

## Phase 0 — Foundations

The bootstrap every serious Go service has. Small, but each piece is a real idiom
you will reuse forever.

- [x] **0.1 Project layout** — `cmd/` vs `internal/`, what `internal/` actually enforces,
      why "pkg/" is cargo cult, package naming (`link`, not `models`).
- [x] **0.2 Configuration** — typed config struct, parse-and-validate at boot, fail fast.
      Why globals and `init()` are a trap. Zero-dependency env parsing.
- [x] **0.3 Structured logging** — `log/slog`, levels, handler selection by env,
      never logging secrets, and *why* you pass a logger rather than reach for one.
- [x] **0.4 Graceful shutdown** — `signal.NotifyContext`, `http.Server.Shutdown`,
      goroutine + channel + `select`, shutdown ordering and timeouts.
      (`errgroup` deferred to Phase 3, where there are really two components to supervise.)

**Phase 0 exit:** ✅ `make run` starts a server, `Ctrl-C` drains in-flight requests
and logs why; an expired shutdown timeout reports `context deadline exceeded` and exits 1.

---

## Phase 1 — The web application

A working shortener. No auth, no cache, no async. Just clean HTTP over MySQL.

- [x] **1.1 Short code generation** — the real decision: random vs counter+base62 vs
      hash. Collision probability math, birthday bound, why you need a retry loop,
      and why `math/rand` is the wrong package here.
- [x] **1.2 Domain package** — the `Link` entity, sentinel errors (`ErrNotFound`,
      `ErrCodeTaken`), URL validation and normalisation, SSRF/open-redirect guards.
      Rich domain types over `string` everywhere.
- [~] **1.3 Migrations** (raw .sql + `make migrate` for now; embedded runner deferred)
- [ ] **1.3b Migration runner** — `embed.FS` + a ~60-line migrator you write yourself.
      **MySQL has no transactional DDL** — a failed migration leaves you half-applied,
      which is why MySQL shops keep one statement per file. A `GET_LOCK()` migration
      lock, and why "down" migrations are mostly a lie.
- [x] **1.4 Storage layer** — `database/sql` + `go-sql-driver/mysql`, a `Store`
      interface defined *by the consumer*, hand-written SQL. **No `RETURNING` in
      MySQL** — `LastInsertId()` or read-back, and the tradeoff. Mapping
      `*mysql.MySQLError` 1062 to a domain error. Pool tuning with `SetMaxOpenConns`
      / `SetMaxIdleConns` / `SetConnMaxLifetime`, and always passing `ctx`.
- [ ] **1.4b Isolation levels** — InnoDB defaults to REPEATABLE READ, not READ
      COMMITTED. Learn what that changes, and meet gap locks before they deadlock
      you in Phase 3.
- [x] **1.5 HTTP layer** — stdlib `net/http` `ServeMux` with Go 1.22+ method+wildcard
      patterns. No framework. Handler-as-closure over dependencies, strict JSON
      decoding, body size limits, `http.Server` timeouts that actually matter.
- [x] **1.6 Error handling** (JSON errors; RFC 9457 problem+json deferred) — one place that maps domain errors to status codes,
      RFC 9457 `application/problem+json`, `errors.Is`/`errors.As`, and the rule that
      handlers never call `http.Error` directly.
- [x] **1.7 Middleware** — the `func(http.Handler) http.Handler` pattern, composing a
      chain, request ID, panic recovery, access logging, and *why order matters*.
- [~] **1.8 Testing** (table-driven + httptest + real MySQL via SHORTLY_TEST_DSN; testcontainers deferred) — table-driven tests, `httptest`, a real MySQL in tests via
      `testcontainers-go`, fixtures, and what is genuinely worth mocking (almost nothing).

**Phase 1 exit:** ✅ `POST /v1/links` → `GET /{code}` → 302. `make test` and `-race` green against real MySQL.

---

## Phase 2 — Auth, done properly

Most tutorials get this wrong. We will not.

- [ ] **2.1 Password hashing** — argon2id, choosing parameters, encoding the hash,
      constant-time comparison, and hashing on a bounded worker pool so login can't
      exhaust your CPU.
- [ ] **2.2 Sessions vs tokens** — the actual tradeoff (revocation vs statelessness),
      and why "JWT for everything" is a smell.
- [ ] **2.3 Access tokens** — JWT with EdDSA, claims design, `kid` for key rotation,
      clock skew, audience/issuer validation, and the `alg=none` class of bug.
- [ ] **2.4 Refresh tokens** — rotation on every use, reuse detection, family
      revocation, storage schema, `HttpOnly`/`Secure`/`SameSite` cookies.
- [ ] **2.5 Auth middleware** — unexported context key types, injecting a `Principal`,
      and keeping authn (middleware) separate from authz (service layer).
- [ ] **2.6 RBAC & ownership** — roles + scopes, per-resource ownership checks,
      and why authorization in the handler leaks.
- [ ] **2.7 API keys** — `sk_live_<id>.<secret>` format, hashed at rest, O(1) lookup
      without a timing leak, scoping, and revocation.
- [ ] **2.8 Hardening** — CORS, CSRF for the cookie flow, login rate limiting and
      lockout, security headers, and a threat model written down.

**Phase 2 exit:** register → login → refresh → rotate → revoke, with tests for each attack.

---

## Phase 3 — Concurrency: the click pipeline

The heart of the project. The redirect is on the hot path and must stay fast, so
click recording goes asynchronous. Everything hard about Go concurrency falls out
of that one requirement.

- [ ] **3.1 The latency budget** — measure the synchronous write first. You need the
      "before" number or the rest is cargo cult.
- [ ] **3.2 Goroutines & channels from first principles** — happens-before, why an
      unbuffered send blocks, buffered channel as a bounded queue.
- [ ] **3.3 The pipeline** — handler (producer) → bounded channel → N workers →
      batch writer. Fan-out/fan-in, and who owns each stage.
- [ ] **3.4 Batching** — flush on size *or* on a timer. The `select` + `time.Timer`
      pattern and the timer-reset bug everyone writes at least once.
- [ ] **3.5 Backpressure** — non-blocking send with `default`, an explicit drop policy,
      and a counter for what you dropped. Unbounded queues are just deferred OOM.
- [ ] **3.6 Graceful drain** — `sync.WaitGroup`, close-from-the-sender, draining on
      shutdown without losing the in-flight batch, and shutdown ordering vs Phase 0.
- [ ] **3.7 Context** — propagation, cancellation, `context.WithoutCancel`, and why the
      request context must *not* cancel the background write.
- [ ] **3.8 The race detector** — write a test that reliably catches a real data race,
      then fix it. `-race` in CI, and what it can and cannot see.
- [ ] **3.9 Choosing a primitive** — `Mutex` vs `RWMutex` vs `atomic` vs channel vs
      sharding. Benchmark all five on the same contended counter and read the numbers.
- [ ] **3.10 The x/sync toolkit** — `errgroup` with limits, `singleflight`, `sync.Once`,
      `sync.Pool` and when it actually helps.
- [ ] **3.11 Worker supervision** — per-worker panic isolation, restart policy, and
      exposing pipeline health on `/readyz`.

**Phase 3 exit:** redirect p99 unchanged under load; zero clicks lost on SIGTERM;
`make race` clean.

---

## Phase 4 — Scale and production

- [ ] **4.1 Redis cache** — read-through for code→URL, TTL + jitter, negative caching
      for 404 floods, and stampede protection with `singleflight`.
- [ ] **4.2 Cache invalidation** — write-through vs delete-on-write, and the race
      between them.
- [ ] **4.3 Rate limiting** — in-process token bucket (`x/time/rate`), then a
      distributed sliding window in Redis with a Lua script for atomicity.
      Per-IP vs per-key vs per-tenant, and the `429` + `Retry-After` contract.
- [ ] **4.4 Distributed locks** — implement one, then learn why you usually shouldn't.
- [ ] **4.5 Counters at scale** — atomic vs sharded counters, Redis `INCR` batching,
      HyperLogLog for unique visitors.
- [ ] **4.6 Analytics rollups** — time-bucketed aggregates, a scheduler goroutine,
      and a query API that doesn't table-scan.
- [ ] **4.7 Idempotency & the outbox** — `Idempotency-Key` on writes, at-least-once
      delivery, exactly-once as a fiction.
- [ ] **4.8 Observability** — OpenTelemetry traces through the async pipeline (the
      hard part), Prometheus metrics, RED/USE dashboards, `net/http/pprof`.
- [ ] **4.9 Load testing & profiling** — k6 or vegeta, then read a CPU and heap
      profile and find the real bottleneck. Escape analysis, allocation reduction.
- [ ] **4.10 Ship it** — distroless multi-stage Dockerfile, `/healthz` vs `/readyz`,
      12-factor config, and a CI pipeline.

**Phase 4 exit:** documented throughput number, a profile-driven optimisation you
can explain, and dashboards that would let you debug it at 3am.

---

## Ground rules

1. **You write the code.** I brief and review. If I write it, you didn't learn it.
2. **No frameworks until the stdlib hurts.** `net/http` and `database/sql` only. Every
   dependency gets justified out loud.
3. **Measure before optimising.** Phase 3 and 4 both start with a number.
4. **`make race` and `make vet` stay green.** Always.
5. **Ask "why" freely.** Understanding beats finishing.
