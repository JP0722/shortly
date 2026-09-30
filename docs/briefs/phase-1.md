# Phase 1 — The web application

**Goal:** a working URL shortener.

```
POST /v1/links   {"url": "https://example.com/some/long/path"}
  -> 201 {"code": "aB3xK9p", "short_url": "http://localhost:8080/aB3xK9p"}

GET  /aB3xK9p
  -> 302 Location: https://example.com/some/long/path
```

No auth, no cache, nothing async. Just clean HTTP over MySQL.

Six steps. Each one runnable, in order.

---

## Step 1 — The link package (pure Go, no database)

New package `internal/link`. No database, no HTTP — just the idea of a link.
This makes it trivially testable, which is the point of putting it first.

**1a. The type**

```go
type Link struct {
	Code      string
	TargetURL string
	CreatedAt time.Time
}
```

**1b. Short code generation**

`func NewCode() (string, error)` — 7 random characters from the base62 alphabet
(`a-zA-Z0-9`).

Use **`crypto/rand`, not `math/rand`.** `math/rand` is predictable: given a few
codes an attacker can generate the rest and enumerate everyone's links. Short
codes are effectively capability URLs, so they must be unguessable.

The collision question: 62^7 ≈ 3.5 trillion codes. At 10 million links stored,
the chance any *single* new code collides is about 1 in 350,000. Rare, but not
never — so the design must handle it rather than assume it away. You'll handle it
in step 4 with a unique index and a retry, which is correct regardless of odds.

**1c. URL validation** — `func Normalise(raw string) (string, error)`

- Parse with `net/url`.
- Require an absolute URL with scheme `http` or `https`. Rejecting everything
  else blocks `javascript:` and `data:` URLs, which would otherwise turn your
  redirect into a cross-site scripting vector.
- Reject an empty host.
- Cap the length (2048 is a reasonable bound).

**1d. Sentinel errors**

```go
var (
	ErrNotFound    = errors.New("link not found")
	ErrInvalidURL  = errors.New("invalid url")
)
```

Declared once as package-level values so callers can test for them with
`errors.Is(err, link.ErrNotFound)`. In step 5 these are what decide the HTTP
status code. This is Go's answer to rescuing a specific exception class.

✅ **Done when:** `internal/link` has tests covering code length/charset,
uniqueness across many calls, and a table of good and bad URLs. No other package
imports it yet.

---

## Step 2 — The database table

**2a.** Add the driver:

```bash
go get github.com/go-sql-driver/mysql
```

**2b.** Write `migrations/0001_links.sql`:

```sql
CREATE TABLE links (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    code        VARCHAR(16)     NOT NULL,
    target_url  VARCHAR(2048)   NOT NULL,
    created_at  TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uq_links_code (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

Three things worth understanding:

- **`UNIQUE KEY` on `code` is the real collision defence.** Not the odds, not a
  pre-check — the database refuses duplicates under any amount of concurrency.
  A "check then insert" in application code is a race; a unique index is not.
- **Surrogate `id` plus a unique `code`.** InnoDB stores rows physically ordered
  by primary key, so a random string as PK would scatter writes across the index.
  An auto-increment PK keeps inserts sequential.
- **`VARCHAR(2048)`** matches the length cap from step 1c. Constraints belong in
  both places.

**2c.** Apply it. For now, just:

```bash
make db < migrations/0001_links.sql
```

A proper migration runner (embedded SQL, a `schema_migrations` table, a
`GET_LOCK()` so two servers can't migrate at once) comes later — it's not worth
building before there's a second migration.

---

## Step 3 — Connecting to MySQL

New package `internal/store`.

```go
func Open(ctx context.Context, dsn string) (*sql.DB, error)
```

- `sql.Open` does **not** connect — it only validates the DSN. Call `db.PingContext(ctx)`
  to actually establish a connection and fail fast at startup.
- `*sql.DB` is **a connection pool, not a connection.** Create one, share it for
  the process lifetime, never open one per request.
- Set the pool limits explicitly: `SetMaxOpenConns`, `SetMaxIdleConns`,
  `SetConnMaxLifetime`. The default max-open is unlimited, which under load means
  you open connections until MySQL refuses them.

Wire it into `run()` in `main.go`, with `defer db.Close()`.

✅ **Done when:** `make run` fails loudly and immediately if MySQL is down.

---

## Step 4 — Reading and writing links

```go
type Store struct { db *sql.DB }

func (s *Store) Create(ctx context.Context, l link.Link) error
func (s *Store) ByCode(ctx context.Context, code string) (link.Link, error)
```

- Always the `...Context` variants (`QueryRowContext`, `ExecContext`). They take
  a `context.Context` so a request that is cancelled doesn't leave a query running.
- `QueryRow(...).Scan(...)` returns `sql.ErrNoRows` when nothing matched.
  Translate it: `return link.Link{}, link.ErrNotFound`. The HTTP layer should
  never see a database-specific error.
- On insert, a duplicate `code` returns a `*mysql.MySQLError` with `Number == 1062`.
  Detect it with `errors.As` and translate to a domain error so the caller can retry
  with a fresh code.
- **No `RETURNING` in MySQL** (unlike Postgres). Use `result.LastInsertId()` or
  read the row back.

✅ **Done when:** a small test inserts and reads back a link against real MySQL.

---

## Step 5 — The HTTP endpoints

Two handlers in `internal/httpapi`.

**`POST /v1/links`**
- Decode JSON with a body size limit (`http.MaxBytesReader`) — an unbounded
  decode is a trivial memory-exhaustion DoS.
- Normalise the URL, generate a code, insert; on a duplicate-code error, retry a
  few times with a new code.
- Respond `201` with the code and full short URL (built from `cfg.BaseURL`).

**`GET /{code}`**
- Look up, then `http.Redirect(w, r, target, http.StatusFound)`.
- `404` when `errors.Is(err, link.ErrNotFound)`.

**302 vs 301:** use `302 Found`. A `301` is cached by browsers *permanently* —
you could never change or delete that link, and you'd never see the click again
to count it in Phase 3.

Handlers now need dependencies (store, config, logger), so `NewMux()` becomes
something that carries them — a small struct with handler methods, or closures.

✅ **Done when:** curl round-trips: create a link, follow it, get redirected.

---

## Step 6 — Errors and middleware

- **One place** that maps domain errors to status codes. Handlers return errors;
  a single helper turns `link.ErrNotFound` into 404 and anything unrecognised into
  500 with a generic message. Internal error text never reaches the client.
- JSON error responses with a consistent shape.
- Middleware — `func(http.Handler) http.Handler`:
  - **request ID** (generate one, put it in the context and the response header)
  - **access log** (method, path, status, duration)
  - **panic recovery** (a panic in a handler otherwise kills the whole server)

  Order matters: recovery outermost, then logging, then request ID.

✅ **Phase 1 exit:** create → redirect works end to end, `make test` green,
`make race` clean, a bad URL gives 400 and a missing code gives 404.

---

## Before you start

Delete the temporary `/slow` handler from `internal/httpapi/server.go` — it did
its job proving graceful shutdown.
