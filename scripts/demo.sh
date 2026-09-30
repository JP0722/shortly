#!/usr/bin/env bash
#
# Walks the whole API against a running server, printing what it does.
#
#   terminal 1:  make run
#   terminal 2:  make demo
#
set -u

BASE="${SHORTLY_BASE:-http://localhost:8080}"
JAR="${TMPDIR:-/tmp}/shortly-demo-cookies.txt"
rm -f "$JAR"
EMAIL="demo-$(date +%s)@example.com"
PASSWORD="correct horse battery staple"

bold() { printf '\n\033[1m== %s\033[0m\n' "$1"; }
run()  { printf '\033[2m$ %s\033[0m\n' "$1"; }

if ! curl -fsS "$BASE/healthz" >/dev/null 2>&1; then
	echo "No server at $BASE — run 'make run' in another terminal first." >&2
	exit 1
fi

bold "1. health"
run "curl $BASE/healthz"
curl -s "$BASE/healthz"; echo

bold "2. register  ($EMAIL)"
run "curl -X POST $BASE/v1/auth/register -d '{\"email\":\"...\",\"password\":\"...\"}'"
curl -s -X POST "$BASE/v1/auth/register" \
	-d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}"; echo

bold "3. register the same email again  (expect 409)"
curl -s -o /dev/null -w 'status: %{http_code}\n' -X POST "$BASE/v1/auth/register" \
	-d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}"

bold "4. weak password  (expect 400)"
curl -s -X POST "$BASE/v1/auth/register" \
	-d '{"email":"weak@example.com","password":"short"}'; echo

bold "5. login  (-c saves the refresh cookie)"
LOGIN=$(curl -s -c "$JAR" -X POST "$BASE/v1/auth/login" \
	-d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}")
ACCESS=$(echo "$LOGIN" | sed 's/.*"access_token":"\([^"]*\)".*/\1/')
echo "access_token: ${ACCESS:0:45}..."
echo "expires_in:   $(echo "$LOGIN" | sed 's/.*"expires_in":\([0-9]*\).*/\1/')s"
echo "refresh cookie (HttpOnly, not readable by JS):"
grep shortly_refresh "$JAR" | awk '{printf "  %s = %.20s...\n", $6, $7}'

bold "6. the access token is readable by anyone — it is signed, not encrypted"
echo "$ACCESS" | cut -d. -f2 | tr '_-' '/+' | base64 -d 2>/dev/null; echo

bold "7. wrong password  (expect 401)"
curl -s -X POST "$BASE/v1/auth/login" \
	-d "{\"email\":\"$EMAIL\",\"password\":\"definitely wrong here\"}"; echo

bold "8. unknown account  (identical response — no account enumeration)"
curl -s -X POST "$BASE/v1/auth/login" \
	-d '{"email":"nobody@example.com","password":"definitely wrong here"}'; echo

bold "9. timing is the same too"
printf "  real account:    "
curl -s -o /dev/null -w '%{time_total}s\n' -X POST "$BASE/v1/auth/login" \
	-d "{\"email\":\"$EMAIL\",\"password\":\"definitely wrong here\"}"
printf "  unknown account: "
curl -s -o /dev/null -w '%{time_total}s\n' -X POST "$BASE/v1/auth/login" \
	-d '{"email":"nobody@example.com","password":"definitely wrong here"}'

bold "10. create a link while signed in"
CREATED=$(curl -s -X POST "$BASE/v1/links" \
	-H "Authorization: Bearer $ACCESS" \
	-d '{"url":"https://go.dev/doc/effective_go"}')
echo "$CREATED"
CODE=$(echo "$CREATED" | sed 's/.*"code":"\([^"]*\)".*/\1/')

bold "11. follow it  (no auth needed — redirects are public)"
run "curl -i $BASE/$CODE"
curl -s -i "$BASE/$CODE" | head -3

bold "12. list my links"
curl -s "$BASE/v1/links" -H "Authorization: Bearer $ACCESS"; echo

bold "13. list without a token  (expect 401)"
curl -s "$BASE/v1/links"; echo

bold "14. a bad URL is rejected  (expect 400)"
curl -s -X POST "$BASE/v1/links" -d '{"url":"javascript:alert(1)"}'; echo

bold "15. a missing code  (expect 404)"
curl -s "$BASE/zzzzzzz"; echo

bold "16. refresh — rotates the cookie and issues a new access token"
OLD=$(grep shortly_refresh "$JAR" | awk '{print $NF}')
curl -s -b "$JAR" -c "$JAR" -X POST "$BASE/v1/auth/refresh" | head -c 60; echo "..."
NEW=$(grep shortly_refresh "$JAR" | awk '{print $NF}')
[ "$OLD" != "$NEW" ] && echo "  cookie rotated: YES" || echo "  cookie rotated: NO (bug!)"

bold "17. REPLAY the old refresh token  (expect 401 — reuse detected)"
curl -s -o /dev/null -w 'status: %{http_code}\n' -X POST "$BASE/v1/auth/refresh" \
	-H "Cookie: shortly_refresh=$OLD"

bold "18. the NEW token is now dead too  (whole family revoked)"
curl -s -o /dev/null -w 'status: %{http_code}\n' -b "$JAR" -X POST "$BASE/v1/auth/refresh"
echo "  ^ we cannot tell the thief from the victim, so both are logged out"

bold "19. the access token still works until it expires (~15 min)"
curl -s -o /dev/null -w 'status: %{http_code}\n' "$BASE/v1/links" -H "Authorization: Bearer $ACCESS"
echo "  ^ this is the tradeoff: a JWT cannot be revoked, so it is kept short-lived"

bold "20. a tampered token is rejected  (expect 401)"
curl -s -o /dev/null -w 'status: %{http_code}\n' "$BASE/v1/links" \
	-H "Authorization: Bearer ${ACCESS%?}X"

rm -f "$JAR"
printf '\n\033[1mdone.\033[0m  Short code created: %s\n' "$CODE"
