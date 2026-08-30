#!/usr/bin/env bash
# inventory-verify_fixture_test.sh — verifies scripts/inventory-verify.sh reports the pin states it claims to and,
# equally load-bearing, exits non-zero for every state that is not a clean pass. Run from the repo root.
#
# A verifier that printed nothing on a broken parse would look identical to a clean inventory — which is exactly the
# failure the script's exit-2 "no pins found" branch exists to prevent, so that branch is tested as hard as the
# happy path. Pins are built from REAL lines of this repo at test time (never fixed line numbers) so the test does not
# rot when files move.

set -uo pipefail

SCRIPT="$(pwd)/scripts/inventory-verify.sh"
[ -x "$SCRIPT" ] || { echo "FATAL: $SCRIPT not executable"; exit 2; }

PASS=0; FAIL=0
pass() { PASS=$((PASS+1)); echo "  ok   $1"; }
fail() { FAIL=$((FAIL+1)); echo "  FAIL $1"; [ -n "${2:-}" ] && printf '%s\n' "$2" | sed 's/^/       | /'; }
expect_exit() { # name expected actual output
  [ "$2" = "$3" ] && pass "$1: exit $2" || fail "$1: expected exit $2, got $3" "$4"; }
expect_has() { printf '%s\n' "$3" | grep -qF -- "$2" && pass "$1: has '$2'" || fail "$1: missing '$2'" "$3"; }
expect_not() { printf '%s\n' "$3" | grep -qF -- "$2" && fail "$1: must not contain '$2'" "$3" || pass "$1: no '$2'"; }

TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
HEAD_SHA=$(git rev-parse HEAD)

# real pins, located at test time
pin_of() { # file fixed-string -> "file:line" and the exact line text on stdout (two lines)
  local hit; hit=$(grep -nF -- "$2" "$1" | head -1); [ -n "$hit" ] || { echo "FATAL: no '$2' in $1" >&2; exit 2; }
  printf '%s:%s\n%s\n' "$1" "${hit%%:*}" "${hit#*:}"; }
P1=$(pin_of go.mod 'module '); P1_REF=${P1%%$'\n'*}; P1_TXT=${P1#*$'\n'}
P2=$(pin_of graph/graphable.go 'type Graphable interface'); P2_REF=${P2%%$'\n'*}; P2_TXT=${P2#*$'\n'}
P2_FILE=${P2_REF%:*}; P2_LINE=${P2_REF##*:}

echo "== (a) all pins OK"
cat > "$TMP/ok.md" <<INV
# Inventory: fixture
base: $HEAD_SHA

## Spellings of the fact
- \`$P1_REF\` — \`$P1_TXT\`
- \`$P2_REF\` — \`$P2_TXT\`

## Searches
- \`git grep -n 'NoSuchThingAnywhere'\` → 0
INV
out=$("$SCRIPT" "$TMP/ok.md" 2>&1); rc=$?
expect_exit a 0 "$rc" "$out"; expect_has a "pins=2 ok=2 moved=0 ambiguous=0 drift=0 malformed=0" "$out"
expect_has a "changed since base" "$out"; expect_not a "DRIFT" "$out"; expect_not a "no pins found" "$out"

echo "== (b) DRIFT — text that exists nowhere"
cat > "$TMP/drift.md" <<INV
base: $HEAD_SHA
## X
- \`$P1_REF\` — \`this text is in no file of this repository zq9x\`
INV
out=$("$SCRIPT" "$TMP/drift.md" 2>&1); rc=$?
expect_exit b 1 "$rc" "$out"; expect_has b "DRIFT $P1_REF" "$out"; expect_has b "drift=1" "$out"

echo "== (c) MOVED — right text, wrong line"
WRONG=$((P2_LINE+7))
cat > "$TMP/moved.md" <<INV
base: $HEAD_SHA
## X
- \`$P2_FILE:$WRONG\` — \`$P2_TXT\`
INV
out=$("$SCRIPT" "$TMP/moved.md" 2>&1); rc=$?
expect_exit c 1 "$rc" "$out"; expect_has c "MOVED $P2_FILE:${WRONG}→${P2_LINE}" "$out"; expect_has c "moved=1" "$out"

echo "== (d) MALFORMED entry"
cat > "$TMP/mal.md" <<INV
base: $HEAD_SHA
## X
- \`graph/graphable.go\` — \`no line number here\`
- \`$P1_REF\` — \`$P1_TXT\`
INV
out=$("$SCRIPT" "$TMP/mal.md" 2>&1); rc=$?
expect_exit d 1 "$rc" "$out"; expect_has d "MALFORMED" "$out"; expect_has d "malformed=1" "$out"; expect_has d "ok=1" "$out"

echo "== (e) zero pins"
cat > "$TMP/empty.md" <<INV
base: $HEAD_SHA
## X
(none — see Searches)
## Searches
- \`git grep -n 'Nothing'\` → 0
INV
out=$("$SCRIPT" "$TMP/empty.md" 2>&1); rc=$?
expect_exit e 2 "$rc" "$out"; expect_has e "no pins found" "$out"; expect_has e "pins=0" "$out"

echo "== (e2) missing base line"
printf '## X\n- `%s` — `%s`\n' "$P1_REF" "$P1_TXT" > "$TMP/nobase.md"
out=$("$SCRIPT" "$TMP/nobase.md" 2>&1); rc=$?
expect_exit e2 2 "$rc" "$out"; expect_has e2 "BASE missing" "$out"

echo "== (f) unknown base sha"
cat > "$TMP/badbase.md" <<INV
base: 0000000000000000000000000000000000000000
## X
- \`$P1_REF\` — \`$P1_TXT\`
INV
out=$("$SCRIPT" "$TMP/badbase.md" 2>&1); rc=$?
expect_exit f 1 "$rc" "$out"; expect_has f "BASE unknown 0000000000000000000000000000000000000000" "$out"; expect_has f "ok=1" "$out"

echo "== (g) fixed-string handling: \$, *, and a backtick in pinned text"
G1=$(pin_of scripts/openspec-queue.sh '$'); G1_REF=${G1%%$'\n'*}; G1_TXT=${G1#*$'\n'}
G2=$(pin_of graph/graphable.go '*'); G2_REF=${G2%%$'\n'*}; G2_TXT=${G2#*$'\n'}
G3_HIT=$(grep -rnF --include='*.go' 'json:"' message/ | head -1); G3_REF=${G3_HIT%%:*}:$(printf '%s' "${G3_HIT#*:}" | cut -d: -f1); G3_TXT=${G3_HIT#*:}; G3_TXT=${G3_TXT#*:}
cat > "$TMP/special.md" <<INV
base: $HEAD_SHA
## X
- \`$G1_REF\` — \`$G1_TXT\`
- \`$G2_REF\` — \`$G2_TXT\`
- \`$G3_REF\` — \`$G3_TXT\`
INV
out=$("$SCRIPT" "$TMP/special.md" 2>&1); rc=$?
expect_exit g 0 "$rc" "$out"; expect_has g "pins=3 ok=3" "$out"

echo "== (h) changed-since-base names a pinned file the base predates"
LAST=$(git log -1 --format=%H -- "$P2_FILE")
if git rev-parse -q --verify "${LAST}^" >/dev/null 2>&1; then
  PARENT=$(git rev-parse "${LAST}^")
  cat > "$TMP/since.md" <<INV
base: $PARENT
## X
- \`$P2_REF\` — \`$P2_TXT\`
- \`$P1_REF\` — \`$P1_TXT\`
INV
  out=$("$SCRIPT" "$TMP/since.md" 2>&1); rc=$?
  expect_has h "  $P2_FILE" "$out"
  # go.mod only listed if it changed in that range — assert nothing about it; assert the header is present
  expect_has h "changed since base (${PARENT:0:8}..HEAD):" "$out"
else
  echo "  SKIP h: $P2_FILE's last commit has no parent"
fi

echo "== (i) Searches section is never parsed as pins"
cat > "$TMP/searches.md" <<INV
base: $HEAD_SHA
## X
- \`$P1_REF\` — \`$P1_TXT\`
## Searches
- \`gopls implementation graph/graphable.go:54:6\` → 29
- \`git grep -n 'x'\` → 0
INV
out=$("$SCRIPT" "$TMP/searches.md" 2>&1); rc=$?
expect_exit i 0 "$rc" "$out"; expect_has i "pins=1 ok=1" "$out"; expect_not i "MALFORMED" "$out"

echo
echo "PASS=$PASS FAIL=$FAIL"
[ "$FAIL" = 0 ]
