#!/usr/bin/env bash
# inventory-verify_fixture_test.sh — verifies scripts/inventory-verify.sh reports the pin states it claims to and,
# equally load-bearing, exits non-zero for every state that is not a clean pass. Run from the repo root.
#
# A verifier that printed nothing on a broken parse would look identical to a clean inventory — which is exactly the
# failure the script's exit-2 "no pins found" and UNPARSED branches exist to prevent, so those are tested as hard as
# the happy path. Pins are built from REAL lines of this repo at test time (never fixed line numbers) so the test does
# not rot when files move; history-dependent behaviour is exercised in a scratch git repo so the test needs no clone
# depth and never touches the worktree.

set -uo pipefail

SCRIPT="$(pwd)/scripts/inventory-verify.sh"
[ -x "$SCRIPT" ] || { echo "FATAL: $SCRIPT not executable"; exit 2; }

PASS=0; FAIL=0
pass() { PASS=$((PASS+1)); echo "  ok   $1"; }
fail() { FAIL=$((FAIL+1)); echo "  FAIL $1"; [ -n "${2:-}" ] && printf '%s\n' "$2" | sed 's/^/       | /'; }
expect_exit() { [ "$2" = "$3" ] && pass "$1: exit $2" || fail "$1: expected exit $2, got $3" "$4"; }
expect_has() { printf '%s\n' "$3" | grep -qF -- "$2" && pass "$1: has '$2'" || fail "$1: missing '$2'" "$3"; }
expect_not() { printf '%s\n' "$3" | grep -qF -- "$2" && fail "$1: must not contain '$2'" "$3" || pass "$1: no '$2'"; }
CLEAN="moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0"

TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
HEAD_SHA=$(git rev-parse HEAD)

pin_of() { # file fixed-string -> "file:line" newline exact-line-text
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
expect_exit a 0 "$rc" "$out"; expect_has a "pins=2 ok=2 $CLEAN" "$out"
expect_has a "changed since base" "$out"; expect_not a "DRIFT" "$out"; expect_not a "no pins found" "$out"

echo "== (b) DRIFT — text that exists nowhere"
printf 'base: %s\n## X\n- `%s` — `this text is in no file of this repository zq9x`\n' "$HEAD_SHA" "$P1_REF" > "$TMP/drift.md"
out=$("$SCRIPT" "$TMP/drift.md" 2>&1); rc=$?
expect_exit b 1 "$rc" "$out"; expect_has b "DRIFT $P1_REF" "$out"; expect_has b "drift=1" "$out"

echo "== (c) MOVED — right text, wrong line"
WRONG=$((P2_LINE+7))
printf 'base: %s\n## X\n- `%s:%s` — `%s`\n' "$HEAD_SHA" "$P2_FILE" "$WRONG" "$P2_TXT" > "$TMP/moved.md"
out=$("$SCRIPT" "$TMP/moved.md" 2>&1); rc=$?
expect_exit c 1 "$rc" "$out"; expect_has c "MOVED $P2_FILE:${WRONG}→${P2_LINE}" "$out"; expect_has c "moved=1" "$out"

echo "== (d) MALFORMED entry"
printf 'base: %s\n## X\n- `graph/graphable.go` — `no line number here`\n- `%s` — `%s`\n' "$HEAD_SHA" "$P1_REF" "$P1_TXT" > "$TMP/mal.md"
out=$("$SCRIPT" "$TMP/mal.md" 2>&1); rc=$?
expect_exit d 1 "$rc" "$out"; expect_has d "MALFORMED" "$out"; expect_has d "malformed=1" "$out"; expect_has d "ok=1" "$out"

echo "== (e) zero pins"
printf 'base: %s\n## X\n(none — see Searches)\n## Searches\n- `git grep -n x` → 0\n' "$HEAD_SHA" > "$TMP/empty.md"
out=$("$SCRIPT" "$TMP/empty.md" 2>&1); rc=$?
expect_exit e 2 "$rc" "$out"; expect_has e "no pins found" "$out"; expect_has e "pins=0" "$out"

echo "== (e2) missing base line"
printf '## X\n- `%s` — `%s`\n' "$P1_REF" "$P1_TXT" > "$TMP/nobase.md"
out=$("$SCRIPT" "$TMP/nobase.md" 2>&1); rc=$?
expect_exit e2 2 "$rc" "$out"; expect_has e2 "BASE missing" "$out"

echo "== (f) unknown base sha"
printf 'base: 0000000000000000000000000000000000000000\n## X\n- `%s` — `%s`\n' "$P1_REF" "$P1_TXT" > "$TMP/badbase.md"
out=$("$SCRIPT" "$TMP/badbase.md" 2>&1); rc=$?
expect_exit f 1 "$rc" "$out"; expect_has f "BASE unknown 0000000000000000000000000000000000000000" "$out"; expect_has f "ok=1" "$out"

echo "== (g) fixed-string handling: \$, *, and a backtick in pinned text"
G1=$(pin_of scripts/openspec-queue.sh '$'); G1_REF=${G1%%$'\n'*}; G1_TXT=${G1#*$'\n'}
G2=$(pin_of graph/graphable.go '*'); G2_REF=${G2%%$'\n'*}; G2_TXT=${G2#*$'\n'}
G3_HIT=$(grep -rnF --include='*.go' 'json:"' message/ | head -1); G3_REF=${G3_HIT%%:*}:$(printf '%s' "${G3_HIT#*:}" | cut -d: -f1); G3_TXT=${G3_HIT#*:}; G3_TXT=${G3_TXT#*:}
printf 'base: %s\n## X\n- `%s` — `%s`\n- `%s` — `%s`\n- `%s` — `%s`\n' "$HEAD_SHA" "$G1_REF" "$G1_TXT" "$G2_REF" "$G2_TXT" "$G3_REF" "$G3_TXT" > "$TMP/special.md"
out=$("$SCRIPT" "$TMP/special.md" 2>&1); rc=$?
expect_exit g 0 "$rc" "$out"; expect_has g "pins=3 ok=3" "$out"

echo "== (h) changed-since-base names a pinned file the base predates (scratch repo, no clone depth needed)"
R="$TMP/repo"; git init -q "$R" 2>/dev/null
G="git -C $R -c user.name=fixture -c user.email=fixture@example.invalid -c commit.gpgsign=false"
printf 'alpha\nbeta\n' > "$R/f.txt"; printf 'unchanged\n' > "$R/u.txt"; $G add f.txt u.txt; $G commit -q -m one; B1=$($G rev-parse HEAD)
printf 'alpha\nbeta\ngamma\n' > "$R/f.txt"; $G commit -qam two
printf 'base: %s\n## X\n- `f.txt:3` — `gamma`\n- `u.txt:1` — `unchanged`\n' "$B1" > "$TMP/since.md"
out=$(cd "$R" && "$SCRIPT" "$TMP/since.md" 2>&1); rc=$?
expect_exit h 0 "$rc" "$out"; expect_has h "changed since base (${B1:0:8}..HEAD):" "$out"; expect_has h "  f.txt" "$out"; expect_not h "  u.txt" "$out"

echo "== (i) Searches section is never parsed as pins"
printf 'base: %s\n## X\n- `%s` — `%s`\n## Searches\n- `gopls implementation graph/graphable.go:54:6` → 29\n- `git grep -n x` → 0\n' "$HEAD_SHA" "$P1_REF" "$P1_TXT" > "$TMP/searches.md"
out=$("$SCRIPT" "$TMP/searches.md" 2>&1); rc=$?
expect_exit i 0 "$rc" "$out"; expect_has i "pins=1 ok=1" "$out"; expect_not i "MALFORMED" "$out"; expect_not i "UNPARSED" "$out"

echo "== (j) strict section: indented pin accepted, non-pin bullet is UNPARSED and fails"
printf 'base: %s\n## X\n  - `%s` — `%s`\n* not a pin at all\n' "$HEAD_SHA" "$P1_REF" "$P1_TXT" > "$TMP/unparsed.md"
out=$("$SCRIPT" "$TMP/unparsed.md" 2>&1); rc=$?
expect_exit j 1 "$rc" "$out"; expect_has j "UNPARSED * not a pin at all" "$out"; expect_has j "pins=1 ok=1" "$out"; expect_has j "unparsed=1" "$out"

echo "== (k) Adjacent claims: non-file entries ignored, in-tree pins still verified"
printf 'base: %s\n## Adjacent claims\n- #1180 — Cut agent token traffic\n- semmem: wants a typed adapter\n- `#1180` — `backticked issue ref`\n- `%s` — `%s`\n' "$HEAD_SHA" "$P1_REF" "$P1_TXT" > "$TMP/adjacent.md"
out=$("$SCRIPT" "$TMP/adjacent.md" 2>&1); rc=$?
expect_exit k 0 "$rc" "$out"; expect_has k "pins=1 ok=1 $CLEAN" "$out"; expect_not k "UNPARSED" "$out"; expect_not k "MALFORMED" "$out"

echo "== (l) trailing whitespace after the closing backtick still verifies"
printf 'base: %s\n## X\n- `%s` — `%s`   \n' "$HEAD_SHA" "$P1_REF" "$P1_TXT" > "$TMP/trail.md"
out=$("$SCRIPT" "$TMP/trail.md" 2>&1); rc=$?
expect_exit l 0 "$rc" "$out"; expect_has l "pins=1 ok=1" "$out"

echo "== (m) empty pinned text is MALFORMED, not AMBIGUOUS"
printf 'base: %s\n## X\n- `%s` — ``\n' "$HEAD_SHA" "$P1_REF" > "$TMP/emptytext.md"
out=$("$SCRIPT" "$TMP/emptytext.md" 2>&1); rc=$?
expect_exit m 1 "$rc" "$out"; expect_has m "MALFORMED (empty text) $P1_REF" "$out"; expect_not m "AMBIGUOUS" "$out"

echo "== (n) untracked file: a moved line is reported MOVED via the grep fallback"
printf 'x\ngamma-untracked\n' > "$R/g.txt"
printf 'base: %s\n## X\n- `g.txt:1` — `gamma-untracked`\n' "$B1" > "$TMP/untracked.md"
out=$(cd "$R" && "$SCRIPT" "$TMP/untracked.md" 2>&1); rc=$?
expect_exit n 1 "$rc" "$out"; expect_has n "MOVED g.txt:1→2" "$out"

echo
echo "PASS=$PASS FAIL=$FAIL"
[ "$FAIL" = 0 ]
