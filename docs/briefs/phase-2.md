# Phase 2 — Auth, done properly

**Goal:** users can register and log in; links belong to whoever created them;
programmatic clients use API keys.

```
POST /v1/auth/register  {"email","password"}  -> 201
POST /v1/auth/login     {"email","password"}  -> 200 {access_token} + refresh cookie
POST /v1/auth/refresh                          -> 200 new access token, rotated refresh
POST /v1/auth/logout                           -> 204
POST /v1/links          Authorization: Bearer  -> 201 (link owned by the caller)
GET  /v1/links                                 -> only the caller's links
```

Seven steps.

---

## Step 1 — Users and password hashing

- `migrations/0002_users.sql`: id, email (UNIQUE), password_hash, role, created_at.
- `internal/auth/password.go`: `HashPassword`, `VerifyPassword` using **argon2id**.
- `internal/store/users.go`: `CreateUser`, `UserByEmail`.
- `POST /v1/auth/register`.

**Why argon2id and not bcrypt/sha256.** A password hash must be *deliberately
slow and memory-hungry*, so that stealing the database doesn't hand the attacker
everyone's password. SHA-256 is fast — a GPU does billions per second. argon2id
is the current recommendation because it is also *memory-hard*: it needs 64 MiB
per hash, which is what makes GPU and ASIC cracking expensive rather than free.

**Store the parameters with the hash.** Output is PHC format:

```
$argon2id$v=19$m=65536,t=1,p=4$<salt>$<hash>
```

Everything needed to verify is in the string. When you raise the cost in two
years, old hashes still verify with their old parameters.

**A random salt per user** means two people with the same password get different
hashes, so an attacker can't crack them in one pass, and precomputed tables are
useless.

**Constant-time comparison.** `subtle.ConstantTimeCompare`, never `==`. A normal
comparison returns as soon as it finds a differing byte, and the timing leaks how
much of the value was correct.

**Never log or return the hash**, and registration must not reveal whether an
email already exists (see step 7).

---

## Step 2 — Login and access tokens

`POST /v1/auth/login` → a short-lived **JWT access token** (15 minutes).

- Sign with **EdDSA**, not HS256. HS256 uses one shared secret, so anything that
  can verify a token can also mint one. EdDSA has a private key for signing and a
  public key for verifying.
- Validate `iss`, `aud`, `exp` and `nbf` on every request. Allow ~1 minute of
  clock skew.
- **The `alg=none` bug:** a naive verifier reads the algorithm out of the token's
  own header and trusts it. An attacker sets `alg=none`, strips the signature and
  walks in. Always pin the expected algorithm.
- Login must take the same time whether the email exists or not, or you have an
  account-enumeration oracle. Hash a dummy password when the user is missing.

---

## Step 3 — Refresh tokens

Access tokens are stateless, so they cannot be revoked — that is the trade for
not hitting the database on every request. Keeping them short-lived bounds the
damage. Refresh tokens are the long-lived half, and they *are* stored.

- Opaque random string, **stored hashed** (it is a credential, treat it like a
  password).
- `HttpOnly`, `Secure`, `SameSite=Lax` cookie — JavaScript must not be able to
  read it.
- **Rotation:** every refresh issues a new token and invalidates the old one.
- **Reuse detection:** if an already-used token comes back, someone has a copy.
  Revoke the entire token *family* and force a re-login. This is the single most
  valuable thing in Phase 2 and almost no tutorial does it.

---

## Step 4 — Auth middleware

- Read `Authorization: Bearer <token>`, verify, put a `Principal{UserID, Role}`
  in the request context (unexported key type, as with `requestIDKey`).
- **Authentication in middleware; authorization in the handler or service.**
  Middleware answers "who are you?" Only the handler knows "may you touch *this
  link*?"
- Add `user_id` to `links`; `POST /v1/links` records the owner and `GET /v1/links`
  lists only the caller's.

---

## Step 5 — Roles and ownership

- `role` on the user: `user` or `admin`.
- Ownership checks: a user may only delete their own links; an admin may delete any.
- Return **404, not 403**, for a link the caller does not own. A 403 confirms the
  resource exists.

---

## Step 6 — API keys

For scripts and CI, which cannot do a browser login flow.

- Format `sk_live_<id>.<secret>`. The `id` half is indexed for an O(1) lookup;
  only the `secret` half is compared, in constant time.
- **Store only a hash.** Show the key once at creation and never again.
- Support revocation and a `last_used_at` column.
- The key prefix makes leaked keys detectable in secret scanners.

---

## Step 7 — Hardening

- **Rate limit login** per IP and per account, with lockout after repeated
  failures.
- **Uniform responses:** register and login must not reveal whether an account
  exists.
- Security headers, and CORS configured explicitly rather than `*`.
- A short written threat model: what an attacker gets from a stolen access token
  vs refresh token vs database dump.

---

**Phase 2 exit:** register → login → refresh → rotate → reuse-detected-revoke,
each with a test; links are owned; API keys work; `make test` and `-race` green.
