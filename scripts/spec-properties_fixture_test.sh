#!/usr/bin/env bash
# spec-properties_fixture_test.sh — verifies scripts/spec-properties.sh reports the citation states it claims to and,
# equally load-bearing, exits non-zero for every state that is not a clean pass. Run from the repo root.
#
# A citation checker that silently found nothing would be indistinguishable from a clean corpus — the exact shape of
# defect it exists to catch — so the exit-2 "no citations" branch is tested as hard as the happy path. Two behaviours
# are load-bearing beyond the obvious ones and are pinned here: matching is EXACT (a near-miss heading must NOT
# resolve, or a real rename slips through), and the ARCHIVE is excluded (an archived delta is history; its
# requirements were folded into openspec/specs/ on archive, so a citation resolving only there is stale).
#
# Every fixture is built in a scratch git repo, because the script scans tracked files with `git grep`. The worktree
# is never touched and no fixture depends on a line number in this repository.

set -uo pipefail

SCRIPT="$(pwd)/scripts/spec-properties.sh"
[ -x "$SCRIPT" ] || { echo "FATAL: $SCRIPT not executable"; exit 2; }

PASS=0; FAIL=0
pass() { PASS=$((PASS+1)); echo "  ok   $1"; }
fail() { FAIL=$((FAIL+1)); echo "  FAIL $1"; [ -n "${2:-}" ] && printf '%s\n' "$2" | sed 's/^/       | /'; }
expect_exit() { [ "$2" = "$3" ] && pass "$1: exit $2" || fail "$1: expected exit $2, got $3" "$4"; }
expect_has() { printf '%s\n' "$3" | grep -qF -- "$2" && pass "$1: has '$2'" || fail "$1: missing '$2'" "$3"; }
expect_not() { printf '%s\n' "$3" | grep -qF -- "$2" && fail "$1: must not contain '$2'" "$3" || pass "$1: no '$2'"; }

TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
REPO="$TMP/repo"
mkdir -p "$REPO"; cd "$REPO" || exit 2
git init -q .; git config user.email t@t; git config user.name t

mkdir -p openspec/specs/cap-current
cat > openspec/specs/cap-current/spec.md <<'SPEC'
# cap-current

### Requirement: A shipped requirement with a stable heading

Body.

### Requirement: Another heading entirely

Body.
SPEC

mkdir -p openspec/changes/live-change/specs/cap-active
cat > openspec/changes/live-change/specs/cap-active/spec.md <<'SPEC'
# cap-active

### Requirement: A requirement that only exists in an active delta

Body.
SPEC

mkdir -p openspec/changes/archive/2026-01-01-done/specs/cap-archived
cat > openspec/changes/archive/2026-01-01-done/specs/cap-archived/spec.md <<'SPEC'
# cap-archived

### Requirement: A requirement that only exists in the archive

Body.
SPEC

write_test() { mkdir -p "$(dirname "$1")"; printf '%s\n' "$2" > "$1"; git add -A >/dev/null; }

echo "== (a) every citation resolves against openspec/specs/"
write_test pkg/a/a_test.go '// spec: cap-current / A shipped requirement with a stable heading'
out=$("$SCRIPT" 2>&1); rc=$?
expect_exit "(a)" 0 "$rc" "$out"
expect_has "(a)" "1/1 citations resolve" "$out"

echo "== (b) a reworded heading leaves the citation UNRESOLVED"
write_test pkg/a/a_test.go '// spec: cap-current / A heading that was reworded away'
out=$("$SCRIPT" 2>&1); rc=$?
expect_exit "(b)" 1 "$rc" "$out"
expect_has "(b)" "UNRESOLVED" "$out"
expect_has "(b)" "cited:  A heading that was reworded away" "$out"
expect_has "(b)" "exists: A shipped requirement with a stable heading" "$out"

echo "== (c) EXACT matching: a near miss must not resolve"
write_test pkg/a/a_test.go '// spec: cap-current / A shipped requirement with a stable heading.'
out=$("$SCRIPT" 2>&1); rc=$?
expect_exit "(c) trailing period" 1 "$rc" "$out"
write_test pkg/a/a_test.go '// spec: cap-current / a shipped requirement with a stable heading'
out=$("$SCRIPT" 2>&1); rc=$?
expect_exit "(c) case change" 1 "$rc" "$out"

echo "== (d) a capability with no spec anywhere is NOSPEC"
write_test pkg/a/a_test.go '// spec: cap-does-not-exist / Anything at all'
out=$("$SCRIPT" 2>&1); rc=$?
expect_exit "(d)" 1 "$rc" "$out"
expect_has "(d)" "NOSPEC" "$out"

echo "== (e) a citation with no ' / ' separator is MALFORMED, never skipped"
write_test pkg/a/a_test.go '// spec: cap-current no separator here'
out=$("$SCRIPT" 2>&1); rc=$?
expect_exit "(e)" 1 "$rc" "$out"
expect_has "(e)" "MALFORMED" "$out"

echo "== (f) an ACTIVE change delta resolves"
write_test pkg/a/a_test.go '// spec: cap-active / A requirement that only exists in an active delta'
out=$("$SCRIPT" 2>&1); rc=$?
expect_exit "(f)" 0 "$rc" "$out"

echo "== (g) the ARCHIVE does NOT resolve — an archived delta is history"
write_test pkg/a/a_test.go '// spec: cap-archived / A requirement that only exists in the archive'
out=$("$SCRIPT" 2>&1); rc=$?
expect_exit "(g)" 1 "$rc" "$out"
expect_has "(g)" "NOSPEC" "$out"

echo "== (h) no citations at all is exit 2, never a clean pass"
rm -f pkg/a/a_test.go; git add -A >/dev/null
out=$("$SCRIPT" 2>&1); rc=$?
expect_exit "(h)" 2 "$rc" "$out"
expect_has "(h)" "nothing was verified" "$out"
expect_not "(h)" "citations resolve" "$out"

echo "== (i) an indented citation inside a function body is still found"
write_test pkg/a/a_test.go 'func T() {
				// spec: cap-current / Another heading entirely
}'
out=$("$SCRIPT" 2>&1); rc=$?
expect_exit "(i)" 0 "$rc" "$out"
expect_has "(i)" "1/1 citations resolve" "$out"

echo "== (j) an untracked test file is not scanned (git grep scans tracked content)"
printf '%s\n' '// spec: cap-nope / Untracked' > pkg/a/untracked_test.go
out=$("$SCRIPT" 2>&1); rc=$?
expect_exit "(j)" 0 "$rc" "$out"

echo
echo "passed=$PASS failed=$FAIL"
[ "$FAIL" -eq 0 ] || exit 1
