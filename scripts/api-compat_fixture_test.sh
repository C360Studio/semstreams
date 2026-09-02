#!/usr/bin/env bash
# api-compat_fixture_test.sh — prove scripts/api-compat.sh classifies each case correctly, and that its failure
# modes fail rather than pass quietly.
#
# Why a fixture test: this script is a release gate. A gate that reports "clean" because it silently did nothing is
# the exact defect class it exists to catch (12 open class:unobserved-skip issues at the time of writing). Every
# assertion below is therefore about the DENOMINATOR — did the sweep actually run — as much as the verdict.
#
# These cases are hermetic: they exercise argument handling, list parsing and the empty-scan guards without a
# network fetch. The real end-to-end classification is exercised by `task api:compat` itself, which is cheap to run
# and whose output is the number the release gate reads.
#
# Usage: scripts/api-compat_fixture_test.sh
# Exit:  0 all cases pass · 1 any case fails
set -uo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$script_dir/api-compat.sh"
root="$(cd "$script_dir/.." && pwd)"

failures=0
cases=0

check() {
  local name="$1" want="$2" got="$3"
  cases=$((cases + 1))
  if [ "$want" = "$got" ]; then
    printf 'ok   %s (exit %s)\n' "$name" "$got"
  else
    printf 'FAIL %s: want exit %s, got %s\n' "$name" "$want" "$got"
    failures=$((failures + 1))
  fi
}

# --- exit 2: not a git work tree -------------------------------------------------------------------------------
tmp="$(mktemp -d)"
( cd "$tmp" && "$script" >/dev/null 2>&1 )
check "outside a git work tree exits 2" 2 "$?"
rm -rf "$tmp"

# --- exit 2: package list missing ------------------------------------------------------------------------------
# A gate whose input file vanished must fail, not report a clean sweep over zero packages.
sandbox="$(mktemp -d)"
git -C "$sandbox" init -q .
mkdir -p "$sandbox/scripts"
cp "$script" "$sandbox/scripts/api-compat.sh"
( cd "$sandbox" && ./scripts/api-compat.sh v1.0.0-beta.160 >/dev/null 2>&1 )
check "missing release/tier1-packages.txt exits 2" 2 "$?"

# --- exit 2: package list present but empty of packages --------------------------------------------------------
# The comment-only case. `grep -v '^#'` yields nothing, and nothing must never read as "all clear".
mkdir -p "$sandbox/release"
printf '# only comments here\n#\n' > "$sandbox/release/tier1-packages.txt"
( cd "$sandbox" && ./scripts/api-compat.sh v1.0.0-beta.160 >/dev/null 2>&1 )
check "comment-only package list exits 2" 2 "$?"
rm -rf "$sandbox"

# --- exit 2: no base version resolvable ------------------------------------------------------------------------
# A repo with no v1.0.0-* tag merged into HEAD cannot state a baseline; guessing one would silently move the gate.
sandbox="$(mktemp -d)"
git -C "$sandbox" init -q .
git -C "$sandbox" config user.email t@t && git -C "$sandbox" config user.name t
mkdir -p "$sandbox/scripts" "$sandbox/release"
cp "$script" "$sandbox/scripts/api-compat.sh"
printf 'github.com/c360studio/semstreams/message\n' > "$sandbox/release/tier1-packages.txt"
git -C "$sandbox" add -A >/dev/null 2>&1
git -C "$sandbox" commit -qm init >/dev/null 2>&1
( cd "$sandbox" && ./scripts/api-compat.sh >/dev/null 2>&1 )
check "no merged v1.0.0-* tag and no argument exits 2" 2 "$?"
rm -rf "$sandbox"

# --- the real list parses to a non-empty set -------------------------------------------------------------------
# Guards the header growing until it swallows the payload: the file is mostly prose, and a stray edit that
# comments out the package block would leave every later run reporting a clean zero-package sweep.
listed=$(grep -vc '^[[:space:]]*#' "$root/release/tier1-packages.txt" 2>/dev/null | tr -d ' ')
cases=$((cases + 1))
if [ "${listed:-0}" -ge 50 ]; then
  printf 'ok   release/tier1-packages.txt lists %s packages\n' "$listed"
else
  printf 'FAIL release/tier1-packages.txt lists only %s packages (expected >= 50)\n' "${listed:-0}"
  failures=$((failures + 1))
fi

# --- every listed package resolves at HEAD ---------------------------------------------------------------------
# Tier 1 is the frozen set, so a name in it that does not exist is a list defect. The two known-absent packages
# (pkg/ownership, input/github-webhook) are documented exclusions and must stay out.
cases=$((cases + 1))
missing=""
while IFS= read -r pkg; do
  [ -z "$pkg" ] && continue
  rel="${pkg#github.com/c360studio/semstreams/}"
  [ -d "$root/$rel" ] || missing="$missing $rel"
done < <(grep -v '^[[:space:]]*#' "$root/release/tier1-packages.txt" | grep -v '^[[:space:]]*$')
if [ -z "$missing" ]; then
  printf 'ok   every listed Tier 1 package exists at HEAD\n'
else
  printf 'FAIL listed Tier 1 packages absent at HEAD:%s\n' "$missing"
  failures=$((failures + 1))
fi

printf '\n%s case(s), %s failure(s)\n' "$cases" "$failures"
[ "$failures" -eq 0 ] || exit 1
