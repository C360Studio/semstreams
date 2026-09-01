#!/usr/bin/env bash
# spec-properties.sh — verify every `// spec:` property citation still resolves to a real requirement heading.
#
# Why: a property test carries `// spec: <capability> / <requirement heading>` as its provenance. Nothing verified the
# right-hand side still existed, so a spec delta could reword a `### Requirement:` heading and every citation to its
# old text kept compiling, kept passing, and kept asserting a provenance that was no longer true (#1235). The manual
# rule this replaces — "a delta rewording a heading greps `// spec:` for the old text" — is a human step in a review
# checklist, exactly the shape that decays.
#
# Resolution follows the contract (.agents/contracts/semstreams-developer.md): a citation resolves against
# `openspec/specs/<capability>/spec.md` OR an active change's delta at `openspec/changes/<id>/specs/<capability>/
# spec.md`. `openspec/changes/archive/` is NOT searched (see resolve_capability) — an archived delta is history, and
# its requirements were folded into `openspec/specs/` on archive, so a citation that only resolves there is stale.
#
# Matching is EXACT against the heading text after `### Requirement: `. Normalizing (collapsing whitespace, stripping
# punctuation) would forgive a real rename, which is the defect this exists to catch. On a miss the report prints the
# cited text and the headings that DO exist in the resolved file, so the fix is a copy-paste.
#
# What this does NOT check: that the property actually exercises the requirement it cites. A citation can resolve
# perfectly on a property that cannot fail — PR #1213's mutation matrix found exactly that, twice. That stays a review
# judgment (see docs/contributing/01-testing.md and the reviewer contract).
#
# Usage: scripts/spec-properties.sh              (run from the repo root; scans tracked *_test.go)
#        scripts/spec-properties.sh <dir>        (limit the scan to a pathspec)
# Exit:  0 every citation resolves · 1 any UNRESOLVED/NOSPEC/MALFORMED · 2 no citations found or unreadable tree
#        (an empty parse must not look like a clean pass)
set -uo pipefail

scope="${1:-.}"

if ! git rev-parse --show-toplevel >/dev/null 2>&1; then
  echo "SCAN failed: not inside a git work tree" >&2
  exit 2
fi

# Every heading text under `### Requirement: ` in a file, one per line, prefix stripped.
requirement_headings() {
  sed -n 's/^###[[:space:]]*Requirement:[[:space:]]*//p' "$1"
}

# Resolve a capability to the spec files that may carry its requirements: current truth first, then any ACTIVE change
# delta. Prints every candidate path that exists, most authoritative first. Newline separated rather than an array —
# this script must run under macOS bash 3.2, which has no `mapfile`.
#
# The archive is excluded STRUCTURALLY, by the shape of the glob: an active change is
# `openspec/changes/<id>/specs/<cap>/spec.md`, while an archived one is
# `openspec/changes/archive/<id>/specs/<cap>/spec.md` — one level deeper, so it never matches. That exclusion is
# intended, not incidental: an archived delta is history, and its requirements were folded into `openspec/specs/` on
# archive, so a citation resolving only there is stale. IF YOU BROADEN THIS GLOB, re-exclude `*/archive/*` explicitly
# — case (g) of the fixture test is what will catch you.
resolve_capability() {
  cap="$1"
  f="openspec/specs/$cap/spec.md"
  [ -f "$f" ] && printf '%s\n' "$f"
  for f in openspec/changes/*/specs/"$cap"/spec.md; do
    [ -f "$f" ] && printf '%s\n' "$f"
  done
}

total=0; ok=0; bad=0; report=""
add() { report="$report$1
"; }

while IFS= read -r hit; do
  [ -z "$hit" ] && continue
  loc="${hit%%// spec:*}"                    # `path:line:` plus any indentation
  loc="${loc%"${loc##*[![:space:]]}"}"       # rtrim
  loc="${loc%:}"
  cite="${hit#*// spec:}"
  cite="${cite#"${cite%%[![:space:]]*}"}"    # ltrim
  cite="${cite%"${cite##*[![:space:]]}"}"    # rtrim
  total=$((total + 1))

  case "$cite" in
    *" / "*) : ;;
    *)
      add "MALFORMED   $loc"
      add "            no ' / ' separator in: // spec: $cite"
      add "            expected: // spec: <capability> / <requirement heading>"
      bad=$((bad + 1)); continue ;;
  esac

  cap="${cite%% / *}"
  req="${cite#* / }"

  files=$(resolve_capability "$cap")
  if [ -z "$files" ]; then
    add "NOSPEC      $loc"
    add "            capability '$cap' has no spec.md under openspec/specs/ or any active change"
    bad=$((bad + 1)); continue
  fi

  found=""
  while IFS= read -r f; do
    [ -z "$f" ] && continue
    while IFS= read -r heading; do
      if [ "$heading" = "$req" ]; then found="$f"; break; fi
    done <<EOF
$(requirement_headings "$f")
EOF
    [ -n "$found" ] && break
  done <<EOF
$files
EOF

  if [ -n "$found" ]; then
    ok=$((ok + 1))
    continue
  fi

  add "UNRESOLVED  $loc"
  add "            cited:  $req"
  while IFS= read -r f; do
    [ -z "$f" ] && continue
    add "            in:     $f"
    while IFS= read -r heading; do
      add "            exists: $heading"
    done <<EOF
$(requirement_headings "$f")
EOF
  done <<EOF
$files
EOF
  bad=$((bad + 1))
done <<EOF
$(git grep -n -e '// spec:' -- '*_test.go' 2>/dev/null | awk -v s="$scope" '
    s == "." { print; next }
    { p = $0; sub(/:.*/, "", p); if (index(p, s) == 1) print }
  ')
EOF

if [ "$total" -eq 0 ]; then
  echo "SCAN found no '// spec:' citations under '$scope' — nothing was verified."
  echo "An empty parse is not a clean pass; check the pathspec, or remove this gate if property citations are gone."
  exit 2
fi

[ -n "$report" ] && printf '%s' "$report"

if [ "$bad" -gt 0 ]; then
  printf '\n%d of %d citations do not resolve.\n' "$bad" "$total"
  printf 'A reworded `### Requirement:` heading leaves its citations silently lying — fix the citation or the delta.\n'
  exit 1
fi

printf 'spec-properties: %d/%d citations resolve.\n' "$ok" "$total"
exit 0
