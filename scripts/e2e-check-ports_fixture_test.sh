#!/usr/bin/env bash
# e2e-check-ports_fixture_test.sh — verifies scripts/e2e-check-ports.sh over
# synthetic compose files. Run from repo root.
#
# The guard it tests replaced one that probed 2 of 46 ports and then printed
# "[OK] All ports available" (gh#1175). The failure mode that matters is not
# "misses a port" but "claims coverage it does not have", so most of these
# cases assert the honesty of the report — the count, the refusal to certify
# an unresolvable or empty set, and the absence of [OK] on every failure path.
#
# Requires: docker (the guard shells out to `docker compose config`), jq.
# Port-holding cases need python3 or nc; they SKIP loudly if neither exists.

set -uo pipefail

GUARD="$(pwd)/scripts/e2e-check-ports.sh"
PASS=0
FAIL=0
SKIP=0

TMPROOT=$(mktemp -d)
cleanup() {
  [ -n "${HOLDER_PID:-}" ] && kill "$HOLDER_PID" 2>/dev/null
  rm -rf "$TMPROOT"
}
trap cleanup EXIT

ok()   { PASS=$((PASS + 1)); }
bad()  { echo "FAIL [$1]: $2"; FAIL=$((FAIL + 1)); }
skip() { echo "SKIP [$1]: $2"; SKIP=$((SKIP + 1)); }

# run_guard <compose-yaml-body> <guard args...> — writes the body to a compose
# file, runs the guard, sets RC and OUT.
run_guard() {
  body=$1; shift
  printf '%s\n' "$body" >"$TMPROOT/compose.yml"
  OUT=$( (cd "$TMPROOT" && bash "$GUARD" "$@") 2>&1 )
  RC=$?
}

assert() {
  desc=$1; want_rc=$2; shift 2
  if [ "$RC" -ne "$want_rc" ]; then
    bad "$desc" "expected exit $want_rc, got $RC. Output:
$OUT"
    return
  fi
  for needle in "$@"; do
    case "$needle" in
      '!'*)
        if printf '%s' "$OUT" | grep -qF -- "${needle#!}"; then
          bad "$desc" "output must NOT contain '${needle#!}'. Output:
$OUT"
          return
        fi
        ;;
      *)
        if ! printf '%s' "$OUT" | grep -qF -- "$needle"; then
          bad "$desc" "output missing '$needle'. Output:
$OUT"
          return
        fi
        ;;
    esac
  done
  ok
}

# --- fixtures ----------------------------------------------------------------

# Ports in the 5xxxx range chosen to not collide with any e2e tier.
FREE_TCP_A=51731
FREE_TCP_B=51732
FREE_UDP=51733
HELD_PORT=51734

THREE_PORTS="services:
  alpha:
    image: alpine:3.22
    ports:
      - \"$FREE_TCP_A:80\"
      - \"$FREE_UDP:81/udp\"
  beta:
    image: alpine:3.22
    ports:
      - \"$FREE_TCP_B:80\""

PROFILED="services:
  base:
    image: alpine:3.22
    ports:
      - \"$FREE_TCP_A:80\"
  extra:
    image: alpine:3.22
    profiles: [heavy]
    ports:
      - \"$FREE_TCP_B:80\""

NO_PORTS="services:
  alpha:
    image: alpine:3.22"

BROKEN="services:
  alpha:
    ports:
      - \"$FREE_TCP_A:80\""

# --- derivation and honest counting ------------------------------------------

run_guard "$THREE_PORTS" compose.yml
assert 'free set passes and reports its count' 0 '[OK] 3/3'

run_guard "$PROFILED" compose.yml
assert 'no profile derives only unprofiled services' 0 '[OK] 1/1'

run_guard "$PROFILED" --profile heavy compose.yml
assert 'profile widens the derived set' 0 '[OK] 2/2'

run_guard "$PROFILED" --profile nosuch compose.yml
assert 'undeclared profile fails closed' 1 \
  "profile 'nosuch' is not declared" 'Refusing to certify' '!ports available'

run_guard "$NO_PORTS" compose.yml
assert 'empty derived set fails closed' 1 \
  'resolved 0 published host ports' 'Refusing to certify' '!ports available'

run_guard "$BROKEN" compose.yml
assert 'unresolvable compose context fails closed' 1 \
  'could not resolve compose context' '!ports available'

run_guard "$THREE_PORTS" nope.yml
assert 'missing compose file fails closed' 1 'compose file not found' '!ports available'

run_guard "$THREE_PORTS" --bogus compose.yml
assert 'unknown flag is a usage error' 2 'unknown flag'

run_guard "$THREE_PORTS" --profile
assert 'dangling --profile is a usage error' 2 'requires a value'

# --- held-port detection and attribution -------------------------------------

hold_tcp_port() {
  hp=$1
  if command -v python3 >/dev/null 2>&1; then
    python3 -c "
import socket, time
s = socket.socket()
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(('0.0.0.0', $hp))
s.listen(1)
time.sleep(120)
" &
    HOLDER_PID=$!
  elif command -v nc >/dev/null 2>&1; then
    nc -l "$hp" >/dev/null 2>&1 &
    HOLDER_PID=$!
  else
    return 1
  fi
  # Confirm the holder really took the port before asserting on the guard —
  # a listener that failed to bind would make this test prove nothing.
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    if command -v ss >/dev/null 2>&1; then
      [ -n "$(ss -H -ln --tcp "sport = :$hp" 2>/dev/null)" ] && return 0
    elif lsof -nP -iTCP:"$hp" -sTCP:LISTEN -t >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.3
  done
  kill "$HOLDER_PID" 2>/dev/null
  HOLDER_PID=""
  return 1
}

HELD_FIXTURE="services:
  alpha:
    image: alpine:3.22
    ports:
      - \"$FREE_TCP_A:80\"
      - \"$HELD_PORT:81\""

if hold_tcp_port "$HELD_PORT"; then
  run_guard "$HELD_FIXTURE" compose.yml
  assert 'held port fails, names port/proto/service, never says OK' 1 \
    "Port $HELD_PORT/tcp is already in use" "service 'alpha'" '[FAIL] 1 of 2' '!ports available'

  # The mutation check for the guard itself: with the holder gone the SAME
  # fixture must pass, so the failure above was the port and not the fixture.
  kill "$HOLDER_PID" 2>/dev/null
  wait "$HOLDER_PID" 2>/dev/null
  HOLDER_PID=""
  sleep 1
  run_guard "$HELD_FIXTURE" compose.yml
  assert 'same fixture passes once the port is released' 0 '[OK] 2/2'
else
  skip 'held port detection' 'no python3 or nc available to hold a port'
  skip 'release restores pass' 'no python3 or nc available to hold a port'
fi

echo
echo "e2e-check-ports fixture test: $PASS passed, $FAIL failed, $SKIP skipped"
[ "$FAIL" -eq 0 ]
