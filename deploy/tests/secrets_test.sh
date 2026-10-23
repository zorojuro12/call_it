#!/usr/bin/env bash
# Tests deploy/secrets.sh against a fake op (tests/fake-op) and throwaway
# directories, so no test talks to 1Password or touches /run/callit.
set -euo pipefail

src="$(cd "$(dirname "$0")/.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
ok() { echo "ok: $*"; }

cleanup=()
trap 'rm -rf "${cleanup[@]}"' EXIT

jwt="$(printf 'a%.0s' {1..64})"
pg="abcDEF123456789xyz"
duck="tok-1"

# setup prints a fresh work dir holding a fake vault (vault/), an op on
# PATH (bin/op), and no secrets dir yet (secrets/ is created by the run).
setup() {
  local w
  w="$(mktemp -d)"
  cleanup+=("$w")
  mkdir -p "$w/vault" "$w/bin"
  ln -s "$src/tests/fake-op" "$w/bin/op"
  printf '%s' "$jwt" >"$w/vault/jwt-secret"
  printf '%s' "$pg" >"$w/vault/postgres-password"
  printf '%s' "$duck" >"$w/vault/duckdns-token"
  : >"$w/vault/calls.log"
  echo "$w"
}

# run_secrets <work-dir> [env assignments...]: runs secrets.sh with the fake
# op first on PATH and a token in the environment; output goes to
# <work-dir>/out.
run_secrets() {
  local w=$1
  shift
  env PATH="$w/bin:$PATH" FAKE_OP_DIR="$w/vault" CALLIT_SECRETS_DIR="$w/secrets" \
    OP_SERVICE_ACCOUNT_TOKEN=ops_fake "$@" bash "$src/secrets.sh" >"$w/out" 2>&1
}

# A good fetch writes the full set: a 0750 dir with exactly the four
# files, each 0444, none ending in a newline, and the DSN derived from the
# Postgres password.
w="$(setup)"
run_secrets "$w" || fail "good fetch exited non-zero: $(cat "$w/out")"
d="$w/secrets"
[ "$(stat -c %a "$d")" = 750 ] || fail "secrets dir mode is $(stat -c %a "$d"), want 750"
[ "$(ls -A "$d" | sort | tr '\n' ' ')" = "duckdns_token jwt_secret postgres_dsn postgres_password " ] \
  || fail "secrets dir holds: $(ls -A "$d" | tr '\n' ' ')"
for f in jwt_secret postgres_password postgres_dsn duckdns_token; do
  [ "$(stat -c %a "$d/$f")" = 444 ] || fail "$f mode is $(stat -c %a "$d/$f"), want 444"
  [ "$(tail -c 1 "$d/$f" | od -An -c | tr -d ' ')" != '\n' ] || fail "$f ends with a newline"
done
[ "$(cat "$d/jwt_secret")" = "$jwt" ] || fail "jwt_secret has the wrong content"
[ "$(cat "$d/postgres_password")" = "$pg" ] || fail "postgres_password has the wrong content"
[ "$(cat "$d/duckdns_token")" = "$duck" ] || fail "duckdns_token has the wrong content"
[ "$(cat "$d/postgres_dsn")" = "postgres://callit:$pg@postgres:5432/callit?sslmode=disable" ] \
  || fail "postgres_dsn is $(cat "$d/postgres_dsn")"
ok "a good fetch writes all four files, 0444 in a 0750 dir, no trailing newline"

# Any failure leaves the previous set byte-identical, adds no file, and
# prints none of the values involved. Each case starts from a good set,
# then changes the vault so a run that wrote as it read, or skipped
# validation, would change something.
# expect_untouched <case> <work-dir> [env assignments...]
expect_untouched() {
  local name=$1 w=$2 before listing
  shift 2
  before="$(cd "$w/secrets" && sha256sum -- *)"
  listing="$(ls -A "$w/secrets")"
  if run_secrets "$w" "$@"; then
    fail "$name: exited 0, want a failure"
  fi
  [ "$(cd "$w/secrets" && sha256sum -- *)" = "$before" ] || fail "$name: changed a secret file"
  [ "$(ls -A "$w/secrets")" = "$listing" ] || fail "$name: added or removed a file: $(ls -A "$w/secrets" | tr '\n' ' ')"
  for v in "$jwt" "$pg" "$duck" "$(cat "$w/vault/jwt-secret")" "$(cat "$w/vault/postgres-password")" "$(cat "$w/vault/duckdns-token")"; do
    ! grep -qF -- "$v" "$w/out" || fail "$name: output leaks a secret value: $(cat "$w/out")"
  done
}

new_jwt="$(printf 'b%.0s' {1..64})"

w="$(setup)"; run_secrets "$w"
printf '%s' "$new_jwt" >"$w/vault/jwt-secret"
expect_untouched "a failed second read" "$w" FAKE_OP_FAIL_ON=postgres-password

w="$(setup)"; run_secrets "$w"
printf '%s' "${new_jwt:1}" >"$w/vault/jwt-secret"
expect_untouched "a 63-char JWT secret" "$w"

w="$(setup)"; run_secrets "$w"
printf '%s' "$new_jwt" >"$w/vault/jwt-secret"
printf '%s' "abcDEF123456789x@y" >"$w/vault/postgres-password"
expect_untouched "an @ in the Postgres password" "$w"

w="$(setup)"; run_secrets "$w"
printf '%s' "$new_jwt" >"$w/vault/jwt-secret"
printf '%s' "tok;id" >"$w/vault/duckdns-token"
expect_untouched "a ; in the DuckDNS token" "$w"
ok "any failure leaves the previous set untouched and prints no secret"

# With no token at all, it fails before ever running op.
w="$(setup)"
if env -u OP_SERVICE_ACCOUNT_TOKEN -u CREDENTIALS_DIRECTORY PATH="$w/bin:$PATH" FAKE_OP_DIR="$w/vault" \
    CALLIT_SECRETS_DIR="$w/secrets" bash "$src/secrets.sh" >"$w/out" 2>&1; then
  fail "no token: exited 0, want a failure"
fi
grep -qF "secrets: no 1Password service account token" "$w/out" || fail "no token: output was $(cat "$w/out")"
[ ! -s "$w/vault/calls.log" ] || fail "no token: op ran anyway: $(cat "$w/vault/calls.log")"
[ ! -e "$w/secrets" ] || fail "no token: created the secrets dir"
ok "no token fails before op runs"

# A systemd credential (LoadCredential=op-token:...) supplies the token,
# which reaches op only through its environment, never its argv.
w="$(setup)"
mkdir "$w/creds"
printf 'ops_from_credential' >"$w/creds/op-token"
if ! env -u OP_SERVICE_ACCOUNT_TOKEN PATH="$w/bin:$PATH" FAKE_OP_DIR="$w/vault" CALLIT_SECRETS_DIR="$w/secrets" \
    CREDENTIALS_DIRECTORY="$w/creds" bash "$src/secrets.sh" >"$w/out" 2>&1; then
  fail "credential file: exited non-zero: $(cat "$w/out")"
fi
[ "$(grep -c 'token-set$' "$w/vault/calls.log")" = 3 ] || fail "credential file: op did not get the token: $(cat "$w/vault/calls.log")"
! grep -qF ops_from_credential "$w/vault/calls.log" "$w/out" || fail "credential file: the token appeared in argv or output"
ok "a systemd credential supplies the token through op's environment only"

# A directory where a secret file belongs (what Docker Desktop's compose up
# leaves behind when the file was missing; plan Task 1) is refused, and
# nothing is written into it.
w="$(setup)"
mkdir -p "$w/secrets/jwt_secret"
if run_secrets "$w"; then
  fail "directory at jwt_secret: exited 0, want a failure"
fi
grep -qF "secrets: $w/secrets/jwt_secret exists and is not a regular file" "$w/out" \
  || fail "directory at jwt_secret: output was $(cat "$w/out")"
[ -z "$(ls -A "$w/secrets/jwt_secret")" ] || fail "directory at jwt_secret: wrote into it"
ok "a non-file at a secret's path is refused"

# On first boot /run/callit doesn't exist either. Any parent the run
# creates must be traversable (0755): the deploy user and the DuckDNS cron
# job reach the secrets through it, and a root-only 0700 parent would lock
# them out. The secrets dir itself stays 0750.
w="$(setup)"
env PATH="$w/bin:$PATH" FAKE_OP_DIR="$w/vault" CALLIT_SECRETS_DIR="$w/run/callit/secrets" \
  OP_SERVICE_ACCOUNT_TOKEN=ops_fake bash "$src/secrets.sh" >"$w/out" 2>&1 || fail "nested dir: exited non-zero: $(cat "$w/out")"
[ "$(stat -c %a "$w/run/callit")" = 755 ] || fail "created parent mode is $(stat -c %a "$w/run/callit"), want 755"
[ "$(stat -c %a "$w/run/callit/secrets")" = 750 ] || fail "secrets dir mode is $(stat -c %a "$w/run/callit/secrets"), want 750"
ok "parents created on first boot are traversable; the secrets dir stays 0750"
