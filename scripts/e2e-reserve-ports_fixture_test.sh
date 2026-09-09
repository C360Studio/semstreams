#!/usr/bin/env bash
# e2e-reserve-ports_fixture_test.sh — verify the reservation derives a real
# set and refuses to claim success over one it could not (gh#1279).
#
# The regression risk is the same one gh#1175 found in the preflight: a guard
# that reports "[OK]" while covering nothing. A reservation is worse than a
# preflight in that respect, because nothing downstream fails loudly when it
# silently covers zero ports — the tier just flakes again weeks later.
#
# Runs anywhere: every assertion here uses --dry-run, which is deliberately
# platform-independent so the derivation can be exercised outside CI.

set -uo pipefail

SCRIPT_UNDER_TEST="scripts/e2e-reserve-ports.sh"
PASS=0; FAIL=0; SKIP=0

ok()   { PASS=$((PASS+1)); echo "  [PASS] $1"; }
bad()  { FAIL=$((FAIL+1)); echo "  [FAIL] $1"; }
skip() { SKIP=$((SKIP+1)); echo "  [SKIP] $1"; }

[ -f "$SCRIPT_UNDER_TEST" ] || { echo "[ERROR] run from the repo root"; exit 2; }

echo "e2e-reserve-ports fixture test"

if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
  echo "  docker unavailable — the derivation cannot be exercised"
  echo "e2e-reserve-ports fixture test: 0 passed, 0 failed, 1 skipped"
  exit 0
fi

OUT=$(bash "$SCRIPT_UNDER_TEST" --dry-run 2>&1); RC=$?

# 1. It succeeds and applies nothing.
[ $RC -eq 0 ] && ok "dry run exits 0" || bad "dry run exited $RC: $OUT"
printf '%s' "$OUT" | grep -q '\[DRY-RUN\] not applied' \
  && ok "dry run says explicitly that it applied nothing" \
  || bad "dry run did not report that it applied nothing"

# 2. It derived a NON-EMPTY set from a plural number of compose files. A
#    reservation over zero ports is the failure this test exists for, and it
#    looks identical to a working one unless the count is asserted.
DERIVED=$(printf '%s' "$OUT" | sed -n 's/.*derived \([0-9]*\) distinct published host ports.*/\1/p')
RESOLVED=$(printf '%s' "$OUT" | sed -n 's/.*across \([0-9]*\) of [0-9]* e2e compose file(s).*/\1/p')
ATTEMPTED=$(printf '%s' "$OUT" | sed -n 's/.*across [0-9]* of \([0-9]*\) e2e compose file(s).*/\1/p')
[ -n "$DERIVED" ] && [ "$DERIVED" -gt 0 ] 2>/dev/null \
  && ok "derived a non-empty port set ($DERIVED ports)" \
  || bad "derived no ports (got '${DERIVED:-nothing}')"
[ -n "$RESOLVED" ] && [ "$RESOLVED" -gt 1 ] 2>/dev/null \
  && ok "resolved more than one compose file ($RESOLVED)" \
  || bad "resolved ${RESOLVED:-no} compose file(s) — the union collapsed"

# 2b. A PARTIAL derivation must be as loud as an empty one.
#
# This is the assertion the first version of this suite was missing, and the
# omission was found by mutation, not by reading: narrowing the derivation
# from 9 compose files to 2 — leaving 17 of 31 at-risk ports unreserved,
# including ports the statistical tier binds — left every other assertion here
# green, because each one is satisfied by any non-empty subset. An empty
# reservation is the obvious failure; a quietly narrowed one is the reachable
# failure, and it wears the same [OK].
#
# Two things are pinned. First: every attempted file either resolved or is
# NAMED as skipped, so a file can never be dropped in silence. Second: the
# skips are only ever the overlay files that genuinely cannot resolve
# standalone. A new name appearing there is a real signal — most likely a
# compose file that lost an env var — and it should stop the build rather than
# silently shrink the reservation.
SKIPLINE=$(printf '%s' "$OUT" | sed -n 's/.*not resolvable standalone, skipped://p')
SKIPCOUNT=$(printf '%s' "$SKIPLINE" | tr ' ' '\n' | grep -c . || true)
if [ -n "$RESOLVED" ] && [ -n "$ATTEMPTED" ]; then
  [ $((RESOLVED + SKIPCOUNT)) -eq "$ATTEMPTED" ] 2>/dev/null \
    && ok "every attempted file is accounted for ($RESOLVED resolved + $SKIPCOUNT named = $ATTEMPTED)" \
    || bad "$ATTEMPTED attempted but only $RESOLVED resolved and $SKIPCOUNT named — a file was dropped silently"
else
  bad "the report does not state resolved-of-attempted; a narrowed set would be invisible"
fi

UNEXPECTED=$(printf '%s' "$SKIPLINE" | tr ' ' '\n' | grep -v '^$' \
  | grep -vE 'tiered\.(8b|frontier)\.yml' || true)
[ -z "$UNEXPECTED" ] \
  && ok "the only unresolvable files are the known standalone overlays" \
  || bad "unexpected file(s) failed to resolve: $(echo $UNEXPECTED)"

# 2c. A floor on the derived set.
#
# A floor, not an equality: adding a port must not break this test, but losing
# a third of them must. If a tier is deliberately retired and these drop, that
# is a considered edit to this line — which is the point. Today: 41 derived,
# 31 at-risk, 9 of 11 files.
[ "$DERIVED" -ge 38 ] 2>/dev/null \
  && ok "derived set is at or above its floor ($DERIVED >= 38)" \
  || bad "derived only $DERIVED ports (floor 38) — the derivation narrowed"

# 3. The at-risk subset is non-empty and no larger than the derived set.
AT_RISK=$(printf '%s' "$OUT" | sed -n 's/.*; \([0-9]*\) inside the ephemeral range.*/\1/p')
if [ -n "$AT_RISK" ] && [ -n "$DERIVED" ]; then
  [ "$AT_RISK" -gt 0 ] 2>/dev/null \
    && ok "found ports inside the ephemeral range ($AT_RISK) — the bug is still real" \
    || skip "no port inside the ephemeral range; the renumbering may have landed"
  [ "$AT_RISK" -le "$DERIVED" ] 2>/dev/null \
    && ok "the at-risk subset is no larger than the derived set" \
    || bad "at-risk ($AT_RISK) exceeds derived ($DERIVED) — impossible unless the subset is not a subset"
  # An inverted range filter keeps at-risk <= derived, so the containment check
  # above cannot see it; assertion 4 is what catches that. This floor is what
  # catches a filter that merely narrows.
  [ "$AT_RISK" -ge 28 ] 2>/dev/null \
    && ok "at-risk set is at or above its floor ($AT_RISK >= 28)" \
    || bad "only $AT_RISK port(s) at risk (floor 28) — the range filter narrowed the set"
else
  bad "could not read the at-risk count from the report"
fi

# 4. Every port it claims to reserve is really inside the range it printed.
#
# Note the one benign false red: the value printed is the MERGED set, so a
# workstation already carrying an out-of-range reservation of its own would
# trip this. Unreachable on a fresh runner, and narrowing the assertion to the
# script's own subset would cost the detector that catches an inverted range
# filter — so it stays, documented.
RANGE=$(printf '%s' "$OUT" | sed -n 's/.*kernel ephemeral range: \([0-9]*\)-\([0-9]*\).*/\1 \2/p')
VALUE=$(printf '%s' "$OUT" | sed -n 's/.*ip_local_reserved_ports = \(.*\)$/\1/p')
if [ -n "$RANGE" ] && [ -n "$VALUE" ]; then
  LO=${RANGE% *}; HI=${RANGE#* }
  OUTSIDE=$(printf '%s' "$VALUE" | tr ',' '\n' | tr '-' '\n' \
    | awk -v lo="$LO" -v hi="$HI" 'NF && ($1 < lo+0 || $1 > hi+0)' | head -5)
  [ -z "$OUTSIDE" ] \
    && ok "every reserved port lies inside the reported range ${LO}-${HI}" \
    || bad "reserved ports outside ${LO}-${HI}: $(echo $OUTSIDE)"
else
  bad "could not read the range or the reserved value from the report"
fi

# 5. It refuses to claim a reservation when the derivation finds nothing —
#    fail-closed, checked by running it somewhere with no taskfiles.
TMP=$(mktemp -d)
cp "$SCRIPT_UNDER_TEST" "$TMP/" 2>/dev/null
mkdir -p "$TMP/taskfiles/e2e"
EMPTY_OUT=$(cd "$TMP" && bash "$(basename "$SCRIPT_UNDER_TEST")" --dry-run 2>&1); EMPTY_RC=$?
[ $EMPTY_RC -eq 1 ] \
  && ok "refuses (exit 1) when it can derive no compose file" \
  || bad "exited $EMPTY_RC over an empty derivation instead of failing closed"
printf '%s' "$EMPTY_OUT" | grep -q 'refusing to claim a reservation' \
  && ok "says why it refused rather than printing a bare error" \
  || bad "refused without naming the reason: $EMPTY_OUT"
rm -rf "$TMP"

echo "e2e-reserve-ports fixture test: $PASS passed, $FAIL failed, $SKIP skipped"
[ $FAIL -eq 0 ]
