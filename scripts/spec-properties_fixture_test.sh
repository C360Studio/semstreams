#!/usr/bin/env bash
# spec-properties_fixture_test.sh — verifies scripts/spec-properties.sh reports the citation states it claims to and,
# equally load-bearing, exits non-zero for every state that is not a clean pass. Run from the repo root.
#
# A citation checker that silently found nothing would be indistinguishable from a clean corpus — the exact shape of
# defect it exists to catch — so the exit-2 "no citations" branch is tested as hard as the happy path. Four behaviours
# are load-bearing beyond the obvious ones, each pinned by a case that a mutation of the script actually kills:
#
#   (c)     matching is EXACT — a near-miss heading must not resolve, or a real rename slips through
#   (g)     the ARCHIVE never resolves — its requirements were folded into openspec/specs/ on archive
#   (k)(l)  EFFECTIVE target state — an active REMOVED (and a RENAMED FROM) suppresses the baseline heading it names,
#           while ADDED/MODIFIED/TO stay eligible. The union of every heading in every file would accept exactly the
#           stale citation this script exists to reject, because during a rename the baseline still carries the old
#           heading. Found in review of the first implementation.
#   (n)     scope matches on a DIRECTORY BOUNDARY — a raw string prefix let the nonexistent `pkg/type` report a clean
#           pass over `pkg/types/...`, turning a typo into the positive signal. The scope must be a TRUNCATION of a
#           real path for this to bite; a longer sibling is rejected by prefix matching anyway.
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

# A rename in flight: the baseline still carries the old heading, and an ACTIVE delta removes it and adds the new one.
# The union of both files would accept the stale citation — which is the whole defect this script exists to catch.
mkdir -p openspec/specs/cap-rename openspec/changes/live-change/specs/cap-rename
cat > openspec/specs/cap-rename/spec.md <<'SPEC'
# cap-rename

### Requirement: Old heading before the rename

Body.

### Requirement: A heading the delta does not touch

Body.
SPEC
cat > openspec/changes/live-change/specs/cap-rename/spec.md <<'SPEC'
# cap-rename

## REMOVED Requirements

### Requirement: Old heading before the rename

**Reason**: reworded.

## ADDED Requirements

### Requirement: New heading after the rename

Body.
SPEC

# OpenSpec's RENAMED section is a different shape entirely: list items carrying the headings inline in backticks,
# which no `### ` scan sees at all.
mkdir -p openspec/specs/cap-renamed openspec/changes/live-change/specs/cap-renamed
cat > openspec/specs/cap-renamed/spec.md <<'SPEC'
# cap-renamed

### Requirement: Heading as it was

Body.
SPEC
cat > openspec/changes/live-change/specs/cap-renamed/spec.md <<'SPEC'
# cap-renamed

## RENAMED Requirements

- FROM: `### Requirement: Heading as it was`
- TO: `### Requirement: Heading as it now reads`
SPEC

mkdir -p openspec/changes/live-change/specs/cap-modified
cat > openspec/changes/live-change/specs/cap-modified/spec.md <<'SPEC'
# cap-modified

## MODIFIED Requirements

### Requirement: A restated requirement

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
# Remove it before any later case: write_test runs `git add -A`, which would TRACK this file and let it leak into
# every subsequent assertion.
rm -f pkg/a/untracked_test.go

echo "== (k) an ACTIVE REMOVED suppresses the baseline heading it names"
write_test pkg/a/a_test.go '// spec: cap-rename / Old heading before the rename'
out=$("$SCRIPT" 2>&1); rc=$?
expect_exit "(k) removed" 1 "$rc" "$out"
expect_has "(k) removed" "UNRESOLVED" "$out"
expect_not "(k) removed" "exists: Old heading before the rename" "$out"

echo "== (k2) its ADDED replacement resolves, and an untouched baseline heading still resolves"
write_test pkg/a/a_test.go '// spec: cap-rename / New heading after the rename'
out=$("$SCRIPT" 2>&1); rc=$?
expect_exit "(k2) added" 0 "$rc" "$out"
write_test pkg/a/a_test.go '// spec: cap-rename / A heading the delta does not touch'
out=$("$SCRIPT" 2>&1); rc=$?
expect_exit "(k2) untouched" 0 "$rc" "$out"

echo "== (l) RENAMED: the FROM heading is suppressed, the TO heading resolves"
write_test pkg/a/a_test.go '// spec: cap-renamed / Heading as it was'
out=$("$SCRIPT" 2>&1); rc=$?
expect_exit "(l) FROM" 1 "$rc" "$out"
write_test pkg/a/a_test.go '// spec: cap-renamed / Heading as it now reads'
out=$("$SCRIPT" 2>&1); rc=$?
expect_exit "(l) TO" 0 "$rc" "$out"

echo "== (m) a MODIFIED heading is eligible"
write_test pkg/a/a_test.go '// spec: cap-modified / A restated requirement'
out=$("$SCRIPT" 2>&1); rc=$?
expect_exit "(m)" 0 "$rc" "$out"

echo "== (n) scope matches on a DIRECTORY BOUNDARY — a TRUNCATION of a real path is not a match"
# The reported shape: `pkg/type` is a string prefix of `pkg/types/...`, so a raw prefix test made a nonexistent path
# report a clean pass. The scope must be SHORTER than the real directory for this to bite.
rm -f pkg/a/a_test.go
write_test pkg/alpha/alpha_test.go '// spec: cap-current / A shipped requirement with a stable heading'
out=$("$SCRIPT" pkg/alph 2>&1); rc=$?
expect_exit "(n) truncation 'pkg/alph'" 2 "$rc" "$out"
expect_has "(n) truncation 'pkg/alph'" "nothing was verified" "$out"
out=$("$SCRIPT" pkg/alpha 2>&1); rc=$?
expect_exit "(n) exact dir 'pkg/alpha'" 0 "$rc" "$out"

echo "== (o) ordinary relative spellings of a valid scope still match"
out=$("$SCRIPT" ./pkg/alpha 2>&1); rc=$?
expect_exit "(o) './pkg/alpha'" 0 "$rc" "$out"
out=$("$SCRIPT" pkg/alpha/ 2>&1); rc=$?
expect_exit "(o) 'pkg/alpha/'" 0 "$rc" "$out"

echo
echo "passed=$PASS failed=$FAIL"
[ "$FAIL" -eq 0 ] || exit 1
