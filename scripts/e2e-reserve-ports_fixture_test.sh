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
FILES=$(printf '%s' "$OUT" | sed -n 's/.*across \([0-9]*\) e2e compose file(s).*/\1/p')
[ -n "$DERIVED" ] && [ "$DERIVED" -gt 0 ] 2>/dev/null \
  && ok "derived a non-empty port set ($DERIVED ports)" \
  || bad "derived no ports (got '${DERIVED:-nothing}')"
[ -n "$FILES" ] && [ "$FILES" -gt 1 ] 2>/dev/null \
  && ok "resolved more than one compose file ($FILES)" \
  || bad "resolved ${FILES:-no} compose file(s) — the union collapsed"

# 3. The at-risk subset is non-empty and no larger than the derived set.
AT_RISK=$(printf '%s' "$OUT" | sed -n 's/.*; \([0-9]*\) inside the ephemeral range.*/\1/p')
if [ -n "$AT_RISK" ] && [ -n "$DERIVED" ]; then
  [ "$AT_RISK" -gt 0 ] 2>/dev/null \
    && ok "found ports inside the ephemeral range ($AT_RISK) — the bug is still real" \
    || skip "no port inside the ephemeral range; the renumbering may have landed"
  [ "$AT_RISK" -le "$DERIVED" ] 2>/dev/null \
    && ok "the at-risk subset is no larger than the derived set" \
    || bad "at-risk ($AT_RISK) exceeds derived ($DERIVED) — the filter is inverted"
else
  bad "could not read the at-risk count from the report"
fi

# 4. Every port it claims to reserve is really inside the range it printed.
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
