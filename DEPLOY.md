# Deploying shortly

The app is a single 16 MB container with no runtime dependencies beyond MySQL.
The UI and the migrations are compiled into the binary, so a deploy is "run the
new image" — there is no asset directory to sync and no migration step to
remember.

## Before you deploy anything

**Generate a signing key.** In production `config.Load` refuses to start without
one, because an ephemeral key would log every user out on every restart.

```bash
go run ./cmd/genkey
# SHORTLY_JWT_PRIVATE_KEY=DJLpKZh2WVp08e7wqd8Qp13buttri+Ksy3D0Syf8KNQ
```

Put it in your host's secret store. It is never committed and never logged.

**Required environment:**

| Variable | Example | Notes |
|---|---|---|
| `SHORTLY_ENV` | `prod` | Turns on JSON logs and `Secure` cookies |
| `SHORTLY_DATABASE_DSN` | `user:pass@tcp(host:3306)/shortly?parseTime=true&loc=UTC&tls=true` | **Not a URL.** `parseTime=true` is required |
| `SHORTLY_JWT_PRIVATE_KEY` | from `genkey` | Required when `ENV=prod` |
| `SHORTLY_BASE_URL` | `https://s.yourdomain.com` | Used to build short URLs — must be the public address |
| `SHORTLY_HTTP_ADDR` | `:8080` | Many platforms inject `PORT` instead; see below |

**`SHORTLY_ENV=prod` must be set**, not just implied. It is what turns on the
`Secure` flag on the refresh cookie. Without it the long-lived credential can
travel over plain HTTP.

---

## Option 1 — Railway (recommended to start)

The least friction of anything that offers **managed MySQL**, which is the only
awkward requirement this app has.

1. Push to GitHub (see the repo README).
2. railway.com → New Project → Deploy from GitHub repo. It detects the
   `Dockerfile` automatically.
3. Add a **MySQL** database to the project (one click).
4. In the app service → Variables, set the table above. Railway exposes the
   database as `MYSQL_URL` etc.; you have to compose the Go DSN by hand because
   the MySQL driver does not take a URL:

   ```
   SHORTLY_DATABASE_DSN=${{MySQL.MYSQLUSER}}:${{MySQL.MYSQLPASSWORD}}@tcp(${{MySQL.MYSQLHOST}}:${{MySQL.MYSQLPORT}})/${{MySQL.MYSQLDATABASE}}?parseTime=true&loc=UTC
   ```

5. Generate a domain, then set `SHORTLY_BASE_URL` to it and redeploy.

Railway injects `PORT`, which this app does not read. Either set
`SHORTLY_HTTP_ADDR=:8080` and tell Railway the target port is 8080, or add
`PORT` support to `config.Load` (about four lines).

Cost: roughly $5/month. Deploys on every push to main.

## Option 2 — Fly.io

Better if you want to learn infrastructure. Go binaries are Fly's ideal payload
and the free allowance is generous. The catch: **no managed MySQL.** Either run
MySQL yourself on a Fly volume (fine for this; back it up yourself) or use a
hosted MySQL such as Aiven's free tier.

```bash
fly launch --no-deploy          # detects the Dockerfile, writes fly.toml
fly secrets set SHORTLY_JWT_PRIVATE_KEY=... SHORTLY_DATABASE_DSN=...
fly deploy
```

In `fly.toml`, point the health check at `/readyz` — it verifies MySQL, so Fly
stops routing to an instance that has lost its database.

## Option 3 — A $5 VPS (most educational)

Hetzner or DigitalOcean, Docker + Caddy for automatic HTTPS. You learn the most
and you own everything, at the price of patching the box yourself.

```bash
# on the server
git clone <your repo> && cd shortly
docker compose --profile full up -d --build
```

Put Caddy in front for TLS — two lines of Caddyfile gets you an automatic
certificate:

```
s.yourdomain.com {
    reverse_proxy localhost:8080
}
```

## Option 4 — Cloud Run + Cloud SQL

Scales to zero, production-grade, and the most setup. Cloud Run injects `PORT`
(so the same four-line change as Railway) and Cloud SQL wants the socket-based
connector rather than TCP. Worth doing once for the experience; overkill for a
first deploy.

---

## What is already deployment-ready

- **Migrations run at startup**, guarded by a MySQL `GET_LOCK` so two instances
  in a rolling deploy cannot both apply them.
- **`/healthz`** — "the process is alive". No dependencies, so a database blip
  does not trigger a restart loop.
- **`/readyz`** — "this instance can serve traffic". Pings MySQL. This is the one
  a load balancer should poll.
- **Graceful shutdown** on SIGTERM, draining in-flight requests within
  `SHORTLY_SHUTDOWN_TIMEOUT`. Kubernetes and Fly both send SIGTERM then wait.
- **Distroless non-root image** — no shell, no package manager. Nothing to
  execute if someone finds a way to execute something.
- **Structured JSON logs** to stdout when `ENV=prod`.

## What is NOT ready yet, and matters

- **No rate limiting.** `/v1/auth/register` costs 64 MiB and ~50 ms per call and
  is unauthenticated. That is a cheap denial of service. This is Phase 2 step 7.
- **No TLS in the app.** Terminate it at the platform or at Caddy/nginx. Do not
  expose the container directly.
- **Registration leaks which emails exist** (409 on a duplicate).
- **No backups.** Whatever database you use, turn on automated backups before
  you have data you would miss.
- **No CI.** Nothing runs `make test` before a deploy yet.

## Local check before shipping

```bash
make image      # build the production image
make up-full    # run the real container against local MySQL, on :18080
make logs       # follow it
```

`make run` on :8080 stays available for day-to-day work; the container uses
18080 so both can run at once.
