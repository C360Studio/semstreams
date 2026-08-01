#!/usr/bin/env bash
# openspec-queue.sh — surface WHY each in-flight change is still open.
#
# `openspec list` renders a change as a bare progress fraction ("12/14
# tasks"). That fraction actively misleads when the remaining work is not
# work at all but a HALT condition, a deliberate not-done, or a red gate:
# it reads "almost done, just finish it" when the honest reading is
# "decide whether this change should still exist".
#
# Three costs, all observed in this repo (2026-08-01, session 23):
#
#   1. `[~] 4.2 NOT DONE, deliberately` in lifecycle-operator-create
#      stopped the implementer and did NOT stop the archiver. Its spec
#      delta still REQUIRED the declined behavior, and a scenario that a
#      shipped test disproves came within one command of being published
#      into openspec/specs/ as permanent current truth.
#   2. predicate-raw-key-representation's task 4.3 is a conditional HALT
#      ("if the pre-v1 wipe window closed ... re-file as a post-v1
#      migration instead of executing a second wipe"). It was invisible:
#      the word is lowercase "halt:" mid-sentence, and a purpose-built
#      case-SENSITIVE grep for it returned zero lines. A filter that
#      matches nothing on a file that contains the thing is a broken
#      filter, not a clean file — hence -i throughout below.
#   3. The program baton compensated by re-narrating these caveats every
#      session, which is both a maintenance tax and a staleness tripwire
#      that fires on changes which are correctly parked.
#
# This script reads them from the SOURCE every run, so the baton does not
# have to carry them and cannot drift from them.
#
# Exit status is advisory-by-default and deliberately so: this is a
# reporting aid for humans and session startup, not a merge gate. Use
# --strict to exit non-zero when any caveat is found (for CI or a
# pre-archive hook).
#
# Run from repo root:
#   scripts/openspec-queue.sh [--strict] [--stale-days N]

set -uo pipefail

STRICT=0
STALE_DAYS=7

while [ $# -gt 0 ]; do
  case "$1" in
    --strict) STRICT=1; shift ;;
    --stale-days) STALE_DAYS="${2:-7}"; shift 2 ;;
    -h|--help) sed -n '2,40p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

if ! command -v openspec >/dev/null 2>&1; then
  echo "openspec CLI not found on PATH" >&2
  exit 2
fi

CHANGES_DIR="openspec/changes"
[ -d "$CHANGES_DIR" ] || { echo "no $CHANGES_DIR (run from repo root)" >&2; exit 2; }

# Caveat classes, most severe first. A line matching an earlier pattern is
# reported under that class and not re-reported under a later one.
#
# These are matched ONLY against unchecked/partial task lines, because a
# COMPLETED task mentioning "blocked" is describing history, not a live
# condition — matching it would produce the noise that gets a report
# ignored, which is the failure mode this script exists to avoid.
#
# Word-boundary matching, NOT substring. The first draft used a bare
# `*fail*` glob and classified "converts a posture into a boot failure"
# as RED — the fixture test caught it. Over-broad matching is not a
# harmless surplus here: a report that cries wolf on ordinary prose stops
# being read, which returns us to the invisible-caveat state this script
# exists to fix. "failure" as a noun in prose is not a red gate; "FAILED"
# and "is failing" are.
label_for() {
  local t=$1
  printf '%s' "$t" | grep -qiE '\bhalt(s|ed|ing)?\b'                  && { echo "HALT";    return; }
  printf '%s' "$t" | grep -qiE '\bred\b|\bfailed\b|\bfailing\b'       && { echo "RED";     return; }
  printf '%s' "$t" | grep -qiE '\bhold\b|\bblocked\b|\bblocking\b'    && { echo "BLOCKED"; return; }
  printf '%s' "$t" | grep -qiE '\bdeliberate|\bnot done\b|\bwont ?do\b' && { echo "WONTDO";  return; }
  printf '%s' "$t" | grep -qiE 'still open'                          && { echo "OPEN-Q";  return; }
  echo ""
}

now_epoch=$(date -u +%s)
found_any=0
change_count=0

printf '\n%s\n' "openspec queue — why each in-flight change is still open"
printf '%s\n\n' "-------------------------------------------------------"

json=$(openspec list --json 2>/dev/null)

# Parse with python3 (already a hard dependency of the repo's tooling) so
# a missing jq does not silently degrade this to nothing.
rows=$(printf '%s' "$json" | python3 -c '
import json,sys
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(0)
for c in d.get("changes", []):
    print("\t".join([
        str(c.get("name","")),
        str(c.get("completedTasks","?")),
        str(c.get("totalTasks","?")),
        str(c.get("lastModified","")),
    ]))
' 2>/dev/null)

if [ -z "$rows" ]; then
  printf '  (queue is empty)\n\n'
  exit 0
fi

while IFS=$'\t' read -r name done total modified; do
  [ -n "$name" ] || continue
  change_count=$((change_count + 1))

  age_note=""
  if [ -n "$modified" ]; then
    mod_epoch=$(python3 -c "
import sys,datetime
try:
    s='$modified'.replace('Z','+00:00')
    print(int(datetime.datetime.fromisoformat(s).timestamp()))
except Exception:
    print(0)
" 2>/dev/null)
    if [ "${mod_epoch:-0}" -gt 0 ]; then
      age_days=$(( (now_epoch - mod_epoch) / 86400 ))
      [ "$age_days" -ge "$STALE_DAYS" ] && age_note="   [stale: ${age_days}d]"
    fi
  fi

  printf '  %-42s %s/%s%s\n' "$name" "$done" "$total" "$age_note"

  tasks_file="$CHANGES_DIR/$name/tasks.md"
  if [ ! -f "$tasks_file" ]; then
    printf '      (no tasks.md)\n\n'
    continue
  fi

  # Unchecked "- [ ]" and partial "- [~]" lines only. [~] is ALWAYS a
  # caveat regardless of wording — it means a deliberate decision was
  # recorded, and that decision has to be propagated into the spec delta
  # before this change can be archived.
  caveats=0
  while IFS= read -r line; do
    lineno="${line%%:*}"
    text="${line#*:}"

    marker=""
    case "$text" in
      *'- [~]'*) marker="WONTDO" ;;
    esac
    [ -z "$marker" ] && marker="$(label_for "$text")"
    [ -z "$marker" ] && continue

    # Trim leading list syntax and squeeze whitespace for a compact line.
    clean=$(printf '%s' "$text" \
      | sed -e 's/^[[:space:]]*- \[[^]]*\][[:space:]]*//' \
            -e 's/\*\*//g' \
            -e 's/[[:space:]][[:space:]]*/ /g')
    printf '      %-8s L%-5s %.104s\n' "$marker" "$lineno" "$clean"
    caveats=$((caveats + 1))
    found_any=1
  done < <(grep -nE '^[[:space:]]*- \[( |~)\]' "$tasks_file" 2>/dev/null)

  if [ "$caveats" -eq 0 ]; then
    printf '      %-8s no halt/hold/deliberate marker in the open tasks\n' "ok"
  fi
  printf '\n'
done <<< "$rows"

printf -- '-------------------------------------------------------\n'
printf '  %d change(s) in flight.\n' "$change_count"
if [ "$found_any" -eq 1 ]; then
  printf '  Read the flagged lines before treating any fraction above as "almost done".\n'
fi
printf '\n'

if [ "$STRICT" -eq 1 ] && [ "$found_any" -eq 1 ]; then
  exit 1
fi
exit 0
