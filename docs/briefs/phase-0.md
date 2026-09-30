# Phase 0 — Foundations (step by step)

**Coming from Rails?** Read this first. It's the single biggest adjustment.

| Rails | Go |
|---|---|
| Framework calls your code | **You** write `main()`. Nothing runs that you didn't call. |
| `app/models`, `app/controllers` | Packages by **domain** (`link`, `auth`), not by layer. There is no `models/` directory. |
| Autoloading | Every file declares its imports. Unused import = **compile error**. |
| ActiveRecord | Hand-written SQL. |
| `raise` / `rescue` | Errors are **return values**. `if err != nil { ... }` everywhere. |
| `nil` | Zero values: `""`, `0`, `nil`. A struct is never "not initialised". |
| Duck typing | Static types, checked at compile time. |
| `Gemfile` / `bundle` | `go.mod` / `go mod tidy` |
| `before_action` | Middleware = a function that wraps another function. Explicit. |
| Puma hides threads | You start goroutines yourself. |

Rails gives you a running app on day one and hides the machinery. Go gives you
nothing and makes you build the machinery. That machinery *is* this phase.

**Goal:** an HTTP server that answers `GET /healthz`, and shuts down cleanly on
`Ctrl-C`. Six steps, each one runnable. Don't skip ahead — run every step.

---

## Step 1 — Hello, Go

Create `cmd/api/main.go`:

```go
package main

import "fmt"

func main() {
	fmt.Println("shortly starting")
}
```

Run it:

```bash
make run
```

**What each line means**

- `package main` — a package is a directory of files. The name `main` is special:
  it means "this compiles to an executable". Every other package is a library.
- `import "fmt"` — `fmt` ("format") is the stdlib printing package. Go's `puts`.
- `func main()` — the entry point. When `main` returns, the process exits.
  There is no Rails boot sequence; this is the whole program.

**Try breaking it** (do this, it teaches you the compiler):

1. Add `import "os"` without using `os`. Run it. Go *refuses to compile* on an
   unused import. Same for unused local variables. This feels hostile for about a
   week and then you'll miss it in every other language.
2. Change `fmt.Println` to `fmt.println`. Run it.
   **Capitalisation is visibility in Go.** Capital = exported (public), lowercase =
   package-private. There's no `public`/`private` keyword — the case of the first
   letter *is* the access modifier. This applies to functions, types, struct fields,
   everything.

Run `gofmt -l .` — it lists misformatted files. Go has exactly one formatting
style and no arguments about it. Set your editor to format on save now.

✅ **Done when:** `make run` prints the message.

---

## Step 2 — An HTTP server

Replace the body of `main.go`:

```go
package main

import (
	"fmt"
	"net/http"
)

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok"}`)
	})

	fmt.Println("listening on :8080")
	err := http.ListenAndServe(":8080", mux)
	if err != nil {
		fmt.Println("server failed:", err)
	}
}
```

Run `make run`, then in another terminal:

```bash
curl -i localhost:8080/healthz
curl -i localhost:8080/nope        # 404
curl -i -X POST localhost:8080/healthz   # 405, because the route said GET
```

**What's happening**

- `mux` is a **router**. `routes.rb` without the DSL.
- `"GET /healthz"` — method + path in the pattern string. This is Go 1.22+;
  older tutorials show `mux.HandleFunc("/healthz", ...)` and check the method by
  hand inside. You don't need to.
- The handler signature `func(w http.ResponseWriter, r *http.Request)` is **the**
  central type in Go web work. `w` is where you write the response (it's a
  writer, not an object you populate). `r` is the request.
- The function is **anonymous**, defined inline — Ruby's block, roughly.
- `err := http.ListenAndServe(...)` — `:=` declares *and* infers the type in one
  step. Inside a function you'll use `:=` almost always.
- `if err != nil` — this is the Go error idiom. No exceptions, no `rescue`. A
  function that can fail returns its error as the last value, and you check it
  right there. You will write this thousands of times.

**Order matters:** `w.Header().Set(...)` must come *before* `w.WriteHeader(...)`.
Once you write the status code the headers are already on the wire. Swap the two
lines and watch the `Content-Type` vanish from the curl output.

✅ **Done when:** all three curl commands behave as described.

---

## Step 3 — Your first package

Everything in one file doesn't scale. Move the route into its own package.

Create `internal/httpapi/server.go`:

```go
package httpapi
```

…and write a function `NewMux()` that returns a `*http.ServeMux` with the
`/healthz` route on it. Then `main.go` imports it and passes it to
`ListenAndServe`.

The import path is the module path from `go.mod` plus the directory:

```go
import "github.com/JP0722/shortly/internal/httpapi"
```

**Things to notice**

- The package name is `httpapi`, not `http` — naming it `http` would collide with
  the stdlib package in every file that needs both.
- `NewMux` is capitalised, so `main` can call it. Make it lowercase and try —
  the compiler will tell you exactly what's wrong. Do that once on purpose.
- `internal/` is special to the Go toolchain: **nothing outside this module can
  import it.** It's a compiler-enforced "private". Default new packages here.
- Directory name and package name should match. Go won't force it; humans will.

**Naming:** `NewMux` returns a `*ServeMux` — the `New` prefix is the Go convention
for constructors. There is no `initialize`. A "constructor" is just a function
that returns a value.

✅ **Done when:** `make run` still serves `/healthz`, and `main.go` no longer
mentions routes.

---

## Step 4 — Config from the environment

Now the thing I wrongly asked you to start with. It makes sense here, because you
now have something to configure.

Create `internal/config/config.go` with:

```go
package config

type Config struct {
	Env      string
	HTTPAddr string
	BaseURL  string
	// ...more later
}

func Load() (Config, error) {
	// read os.Getenv("SHORTLY_..."), apply defaults, return
}
```

**New Go concepts here**

- **Structs.** A plain data type — no inheritance, no `attr_accessor`, no
  ActiveRecord. Fields are capitalised to be visible outside the package.
- **Multiple return values.** `(Config, error)` — Go's answer to exceptions. The
  caller gets both the value and the failure, and must deal with the failure.
- **Zero values.** A `Config{}` with nothing set has `Env == ""`, not nil. Go has
  no "uninitialised" state, which removes a whole class of Rails-style nil errors.

**Requirements**

1. Read env vars prefixed `SHORTLY_` via `os.Getenv`. No library — you'll write
   about 40 lines and understand every one.
2. Defaults for everything except `SHORTLY_DATABASE_DSN`, which is required.
3. `Load` returns an error. It must **not** call `log.Fatal`, `panic`, or print.
   A library that kills the process can't be tested. `main` decides what failure
   means — that's the whole point of returning the error.

Wire it up: `main` calls `config.Load()`, checks the error, and uses
`cfg.HTTPAddr` instead of the hardcoded `":8080"`.

Load your `.env` before running:

```bash
set -a && source .env && set +a
make run
```

✅ **Done when:** `SHORTLY_HTTP_ADDR=:9090 make run` listens on 9090, and unsetting
`SHORTLY_DATABASE_DSN` makes it exit with a clear message and a non-zero code
(`echo $?`).

---

## Step 5 — Typed config and collected errors

Two upgrades to the config you now have. Do them in order; each compiles.

### 5.1 — Add two typed fields

```go
ShutdownTimeout time.Duration
LogLevel        slog.Level
```

Parse them with `time.ParseDuration("15s")` and, for the level:

```go
var lvl slog.Level
err := lvl.UnmarshalText([]byte("debug"))   // accepts debug|info|warn|error
```

**The rule: parse once, at the boundary.** Nothing downstream should ever
receive a string that still needs interpreting. If `ShutdownTimeout` were a
`string`, every caller would have to re-parse it and every caller could fail.
As a `time.Duration` it is impossible to misuse.

### 5.2 — Build the error accumulator

Right now `Load` returns on the first problem. A developer with three bad env
vars should learn all three in one run, not one per run.

```go
type parser struct {
	errs []error
}

func (p *parser) str(key, def string) string
func (p *parser) required(key string) string
func (p *parser) dur(key string, def time.Duration) time.Duration
func (p *parser) level(key string, def slog.Level) slog.Level
func (p *parser) err() error      // errors.Join(p.errs...)
```

Each method: read the env var, return `def` if unset, parse it, and **on failure
append to `p.errs` and return the zero value** — never return early.

Three concepts land here:

- **Methods.** `func (p *parser) str(...)` — the `(p *parser)` bit is the
  *receiver*. Go's `def` inside a class, except the method sits next to the type
  rather than inside it.
- **Pointer receivers.** `*parser` means the method can mutate `p.errs`. With a
  value receiver `(p parser)` you get a **copy**, your appends vanish, and the
  code compiles and silently does nothing. Write it wrong once on purpose.
- **Slices.** `append(p.errs, err)` returns a *new* slice — you must assign it
  back: `p.errs = append(p.errs, err)`.

### 5.3 — Rewrite Load to use it

```go
func Load() (Config, error) {
	var p parser
	cfg := Config{
		Env:      p.str("SHORTLY_ENV", "dev"),
		HTTPAddr: p.str("SHORTLY_HTTP_ADDR", ":8080"),
		// ...
	}
	if err := errors.Join(p.err(), cfg.validate()); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
```

`errors.Join` **ignores nil arguments and returns nil if all of them are nil** —
so that one line covers "parse errors only", "validation errors only", "both",
and "none". Printing a joined error puts each on its own line.

### 5.4 — Make validate accumulate too

Same change: collect into a local `[]error` and return `errors.Join(errs...)`
instead of returning on the first failure.

### 5.5 — Write the test

`internal/config/config_test.go`:

```go
func TestLoad(t *testing.T) {
	t.Setenv("SHORTLY_DATABASE_DSN", "dsn")
	t.Setenv("SHORTLY_SHUTDOWN_TIMEOUT", "not-a-duration")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
```

- Test files end in `_test.go`; functions are `TestXxx(t *testing.T)`.
- `t.Setenv` sets an env var and **restores it automatically** when the test ends.
- **Trap:** your shell may already have `SHORTLY_*` exported from `.env`. A test
  that doesn't set a variable will silently inherit yours and pass on your
  machine while failing in CI. Have each case set every variable it depends on.
- No assertion library in the stdlib. `if got != want { t.Errorf(...) }` is the
  whole idiom. `t.Error` continues, `t.Fatal` stops that test.

Make it **table-driven** — the dominant Go test style: a slice of cases, one
`t.Run(name, func(t *testing.T){...})` per case. Cover: all defaults applied,
missing DSN, bad duration, bad level, bad BaseURL, and **two bad vars at once**.

This is the payoff for `Load` returning an error rather than calling `log.Fatal`:
you can test the failure paths without the test binary dying.

✅ **Done when:** two bad env vars produce two error lines in one run, and
`make test` is green.

---

## Step 6 — Logging and graceful shutdown

The real lesson of Phase 0.

**Logging.** `internal/logging/logging.go`, `func New(level slog.Level, env string) *slog.Logger`.
Text handler in dev, JSON in prod. Return it and pass it down — don't use
`slog.SetDefault` and reach for globals from deep in the code. Log key/value
(`logger.Info("started", "addr", cfg.HTTPAddr)`), never `fmt.Sprintf`.

**Server timeouts.** Build an `*http.Server` explicitly instead of calling
`http.ListenAndServe`. A zero-value `http.Server` has **no read or write
deadline** — one slow client holds a connection forever. Set `ReadHeaderTimeout`,
`ReadTimeout`, `WriteTimeout`, `IdleTimeout`.

**Shutdown.** The sequence:

1. `ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`
   — gives you a context that cancels on Ctrl-C. `defer stop()`.
2. Run `srv.ListenAndServe()` in a goroutine (`go func() { ... }()`).
3. Wait for either the signal or a server error.
4. **On signal, build a *fresh* context:**
   `context.WithTimeout(context.Background(), cfg.ShutdownTimeout)`.
   Not a child of the signal context — that one is already cancelled, so shutdown
   would abort instantly. Nearly everyone writes this bug once.
5. `srv.Shutdown(shutdownCtx)` — stops accepting new connections, waits for
   in-flight handlers to finish.

**Three traps**

- `ListenAndServe` returns `http.ErrServerClosed` on a *clean* shutdown. That is
  **not** an error. Logging it as one is the most common bug in this code. Use
  `errors.Is(err, http.ErrServerClosed)`.
- Put the real work in `run() error` and keep `main` thin:
  `if err := run(); err != nil { ...; os.Exit(1) }`. `os.Exit` skips every
  `defer`, so nothing deferred in `main` runs.
- **Contexts are new to you and central to Go.** A `context.Context` carries
  cancellation across function and goroutine boundaries. Rails has no equivalent.
  Every blocking or long-running call takes one as its first argument.

**Set up for Phase 3.** Later, a background pipeline must drain *after* the HTTP
server stops accepting but *before* the process exits. Use
`golang.org/x/sync/errgroup` to supervise both now and that becomes a small edit:

```bash
go get golang.org/x/sync/errgroup
```

✅ **Done when:** with a temporary `/slow` handler that sleeps 5s — start a
request, hit Ctrl-C mid-flight, and the server waits for it to finish, logs the
shutdown, and exits 0.

---

## Phase 0 acceptance

- [ ] `make run` serves `GET /healthz` → 200
- [ ] Two bad env vars → both reported in one run, exit code 1
- [ ] Ctrl-C during a slow request drains it, then exits 0
- [ ] `ErrServerClosed` never logged as an error
- [ ] `make vet` clean, `gofmt -l .` empty
- [ ] No `log.Fatal` or `panic` outside `main`; no package-level mutable state

## Questions to answer before moving on

1. Why does an unused import fail compilation, but an unused struct field doesn't?
2. What's the difference between `var x Config` and `x := Config{}`?
3. Why must the shutdown context *not* derive from the signal context?
4. Why `ReadHeaderTimeout` specifically, and not just `ReadTimeout`?
