#!/usr/bin/env bash
# inventory-verify.sh — re-check the line-pinned entries of an inventory file after commits.
#
# Why: an inventory pins facts as `path:line` — `text` against a base commit. Commits move lines, and a refresh that
# re-sweeps the whole surface is the cost this repo measured at ~60 turns per change (#1180). This script reports which
# pins still hold, which moved, and which drifted, and lists the pinned files that changed since `base:` so a refresh
# reads only those. It verifies pins, never completeness — the reviewer's independent re-derivation stays.
#
# Sections: under a category heading every bullet must be a pin; a bullet that is not one is reported UNPARSED, so a
# half-parsed inventory cannot look clean. Two sections differ by contract: `## Searches` is skipped entirely (its
# bullets are commands), and `## Adjacent claims` may hold non-file entries (`- #1180 — title`, `- semmem: <ask>`) —
# pins there are verified, other bullets ignored. Indented and `*`/`+` bullets are accepted.
#
# Usage: scripts/inventory-verify.sh <inventory.md>     (run from the repo root; paths in the file are repo-relative)
# Exit:  0 every pin OK, nothing UNPARSED, base known · 1 any MOVED/AMBIGUOUS/DRIFT/MALFORMED/UNPARSED or unknown base
#        2 unreadable file, missing/malformed `base:` line, or no pins (an empty parse must not look like a clean pass)
set -uo pipefail

f="${1:-}"
if [ -z "$f" ] || [ ! -r "$f" ]; then echo "usage: $0 <inventory.md>" >&2; exit 2; fi

base=$(sed -n 's/^base:[[:space:]]*\([0-9a-fA-F]\{40\}\)[[:space:]]*$/\1/p' "$f" | head -1)
if [ -z "$base" ]; then
  if grep -q '^base:' "$f"; then echo "BASE malformed: $(grep -m1 '^base:' "$f")"; else echo "BASE missing: no 'base: <40-hex sha>' line"; fi
  exit 2
fi

trim() { printf '%s' "$1" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//'; }
# line numbers where a fixed string occurs: tracked content via git grep; untracked files via grep
line_hits() {
  local h; h=$(git grep -nF -e "$2" -- "$1" 2>/dev/null | cut -d: -f2)
  if [ -z "$h" ] && [ -f "$1" ]; then h=$(grep -nF -- "$2" "$1" 2>/dev/null | cut -d: -f1); fi
  printf '%s\n' "$h" | grep .
}

pins=0 ok=0 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0
paths_list=""
mode=strict
while IFS= read -r line || [ -n "$line" ]; do
  case "$line" in
    '## Searches'*)        mode=skip;    continue ;;
    '## Adjacent claims'*) mode=lenient; continue ;;
    '## '*)                mode=strict;  continue ;;
  esac
  [ "$mode" = skip ] && continue
  [[ "$line" =~ ^[[:space:]]*[-*+][[:space:]]+(.*)$ ]] || continue
  body=${BASH_REMATCH[1]}
  if [[ "$body" != '`'* ]]; then
    if [ "$mode" = strict ]; then echo "UNPARSED ${line:0:100}"; unparsed=$((unparsed+1)); fi
    continue
  fi

  # pin grammar:  - `path:line` — `text`      (em dash; text is a fixed string, never a pattern)
  rest=${body#'`'}
  ref=${rest%%'`'*}
  after=${rest#*'`'}
  path=${ref%:*}; num=${ref##*:}
  if [ "$ref" = "$rest" ] || [ -z "$path" ] || [ "$path" = "$ref" ] || ! [[ "$num" =~ ^[0-9]+$ ]] || [[ "$after" != ' — '* ]]; then
    [ "$mode" = lenient ] && continue
    echo "MALFORMED ${line:0:100}"; malformed=$((malformed+1)); pins=$((pins+1)); continue
  fi
  t=$(trim "${after#' — '}"); t=${t#'`'}; t=${t%'`'}; t=$(trim "$t")
  pins=$((pins+1))
  if [ -z "$t" ]; then echo "MALFORMED (empty text) $path:$num"; malformed=$((malformed+1)); continue; fi
  paths_list="${paths_list}${path}
"
  if [ ! -f "$path" ]; then echo "DRIFT $path:$num (file missing)"; drift=$((drift+1)); continue; fi
  a=$(trim "$(sed -n "${num}p" "$path")")
  if [[ "$a" == *"$t"* ]]; then ok=$((ok+1)); continue; fi
  hits=$(line_hits "$path" "$t")
  n=$(printf '%s\n' "$hits" | grep -c .)
  case "$n" in
    0) echo "DRIFT $path:$num"; drift=$((drift+1)) ;;
    1) echo "MOVED $path:${num}→${hits}"; moved=$((moved+1)) ;;
    *) echo "AMBIGUOUS $path:$num ($n hits)"; ambiguous=$((ambiguous+1)) ;;
  esac
done < "$f"

base_ok=0
if git cat-file -e "${base}^{commit}" 2>/dev/null; then
  base_ok=1
  echo "changed since base (${base:0:8}..HEAD):"
  changed=""
  if [ -n "$paths_list" ]; then
    changed=$(printf '%s' "$paths_list" | sort -u | tr '\n' '\0' | xargs -0 git diff --name-only "${base}..HEAD" -- 2>/dev/null)
  fi
  if [ -n "$changed" ]; then printf '%s\n' "$changed" | sed 's/^/  /'; else echo "  (none)"; fi
else
  echo "BASE unknown $base"
fi

[ "$pins" = 0 ] && echo "no pins found"
echo "pins=$pins ok=$ok moved=$moved ambiguous=$ambiguous drift=$drift malformed=$malformed unparsed=$unparsed"
[ "$pins" = 0 ] && exit 2
if [ "$base_ok" = 1 ] && [ "$ok" = "$pins" ] && [ "$unparsed" = 0 ]; then exit 0; fi
exit 1
