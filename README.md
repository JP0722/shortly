# shortly

A URL shortener with analytics — built as a guided, senior-level Go curriculum.
See [ROADMAP.md](./ROADMAP.md) for the full arc and [docs/briefs](./docs/briefs)
for the design brief of each component.

## Quick start

```bash
cp .env.example .env
make up      # MySQL on :3307, Redis on :6383
make run
```

`make help` lists everything.

## Stack

Go 1.26 · MySQL 8.4 · Redis 7 · stdlib `net/http` + `database/sql`. No web framework,
no ORM — every dependency has to justify itself.
