#!/usr/bin/env bash
# inventory-verify.sh — re-check the line-pinned entries of an inventory file after commits.
#
# Why: an inventory pins facts as `path:line` — `text` against a base commit. Commits move lines, and a refresh that
# re-sweeps the whole surface is the cost this repo measured at ~60 turns per change (#1180). This script reports which
# pins still hold, which moved, and which drifted, and lists the pinned files that changed since `base:` so a refresh
# reads only those. It verifies pins, never completeness — the reviewer's independent re-derivation stays.
#
# Usage: scripts/inventory-verify.sh <inventory.md>     (run from the repo root; paths in the file are repo-relative)
# Exit:  0 every pin OK and base known · 1 any MOVED/AMBIGUOUS/DRIFT/MALFORMED pin or unknown base
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

pins=0 ok=0 moved=0 ambiguous=0 drift=0 malformed=0
paths_list=""
in_searches=0
while IFS= read -r line || [ -n "$line" ]; do
  case "$line" in
    '## Searches'*) in_searches=1; continue ;;
    '## '*)         in_searches=0; continue ;;
  esac
  [ "$in_searches" = 1 ] && continue
  case "$line" in '- `'*) ;; *) continue ;; esac

  # pin grammar:  - `path:line` — `text`      (em dash; text is a fixed string, never a pattern)
  rest=${line#'- `'}
  ref=${rest%%'`'*}
  after=${rest#*'`'}
  path=${ref%:*}; num=${ref##*:}
  if [ "$ref" = "$rest" ] || [ -z "$path" ] || [ "$path" = "$ref" ] || ! [[ "$num" =~ ^[0-9]+$ ]] || [[ "$after" != ' — '* ]]; then
    echo "MALFORMED ${line:0:100}"; malformed=$((malformed+1)); pins=$((pins+1)); continue
  fi
  text=${after#' — '}
  text=${text#'`'}; text=${text%'`'}
  t=$(trim "$text")
  pins=$((pins+1)); paths_list="${paths_list}${path}
"
  if [ ! -f "$path" ]; then echo "DRIFT $path:$num (file missing)"; drift=$((drift+1)); continue; fi
  a=$(trim "$(sed -n "${num}p" "$path")")
  if [ -n "$t" ] && [[ "$a" == *"$t"* ]]; then ok=$((ok+1)); continue; fi
  hits=$(git grep -nF -e "$t" -- "$path" 2>/dev/null | cut -d: -f2)
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
echo "pins=$pins ok=$ok moved=$moved ambiguous=$ambiguous drift=$drift malformed=$malformed"
[ "$pins" = 0 ] && exit 2
if [ "$base_ok" = 1 ] && [ "$ok" = "$pins" ]; then exit 0; fi
exit 1
