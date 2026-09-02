#!/usr/bin/env bash
# api-compat.sh — report incompatible API changes in the Tier 1 package set against a base version.
#
# Why: "have we stopped breaking adopters" was answered by grepping commit subjects for a `!` marker. That marker is
# an author convention, applied by hand, and it has been wrong in the direction that matters — beta.162 read as
# "purely additive" from its milestone while the tag range carried 8 `!` commits. A convention nobody can verify is a
# prediction about the diff; this script observes the diff instead (#1246).
#
# Measured when this landed: 27 of 62 comparable Tier 1 packages carried incompatible changes across beta.160..HEAD,
# a ~3-week window. That number is the reason semlink (beta.141), semops (beta.114) and semdragon (beta.21) sat out
# the churn rather than absorbing it repeatedly. It is also the RC-4 exit criterion (ADR-106): RC requires this
# script to report zero for 30 consecutive days with an active sister tracking a tag inside the window.
#
# WHAT COUNTS AS INCOMPATIBLE. apidiff's own classification, plus one case apidiff cannot see:
#
#   - A package present at base and ABSENT at head is a REMOVAL — the hardest break there is, since the adopter
#     cannot even compile the import. apidiff reports this as a load failure on the new side, which reads
#     identically to "the tool broke". Treating it as a skip is how #1116's retirement of engine/flowstore/
#     flowtemplate would go unreported while semteams imports all three. It is a hard finding here.
#   - A package ABSENT at base and present at head is an ADDITION, which is compatible. It is reported, not failed:
#     a new Tier 1 surface still owes a walked path under RC-6, but that is a different gate.
#
# The asymmetry is deliberate. The two directions look the same to the tool and mean opposite things, and the class
# of defect this repo keeps finding is precisely the one where an unobserved skip is read as a pass.
#
# WHAT THIS DOES NOT CHECK. Only the exported Go API of the listed packages. A change can be perfectly compatible
# here and still break every adopter: behaviour behind an unchanged signature, a config-schema change (that is
# `task schema:check-changes`), a subject rename, an entity-ID grammar change, a payload-envelope change. ADR-106
# lists those as separate Tier 1 members with their own guards. A green run of this script is not a green freeze.
#
# Usage: scripts/api-compat.sh                 (base = latest v1.0.0-* tag reachable from HEAD)
#        scripts/api-compat.sh v1.0.0-beta.160 (explicit base version)
# Env:   API_COMPAT_MODE=report  report findings but exit 0 — the pre-RC posture, where the count is expected to be
#                                non-zero and descending. Unset (the default) fails on any incompatible change.
# Exit:  0 no incompatible changes (or report mode) · 1 incompatible changes found
#        2 could not run — no package list, no base version, tooling failure, or zero packages compared.
#        An empty comparison must never look like a clean pass; that is the failure this whole exercise is about.
set -uo pipefail

# Pinned so a CI run and a local run classify identically. golang.org/x/exp has no semver tags; this is the revision
# verified against this module on 2026-09-02. Bump deliberately, never automatically.
APIDIFF_PKG="golang.org/x/exp/cmd/apidiff@v0.0.0-20260824195058-e88cd73687aa"

root="$(git rev-parse --show-toplevel 2>/dev/null)" || {
  echo "api-compat failed: not inside a git work tree" >&2
  exit 2
}
cd "$root" || exit 2

list="release/tier1-packages.txt"
[ -r "$list" ] || {
  echo "api-compat failed: $list is missing or unreadable" >&2
  exit 2
}

base="${1:-}"
if [ -z "$base" ]; then
  # --merged HEAD so a tag cut on another branch cannot become this branch's baseline; the freeze is measured
  # against what this history actually shipped.
  base="$(git tag --list 'v1.0.0-*' --merged HEAD --sort=-version:refname | head -1)"
fi
[ -n "$base" ] || {
  echo "api-compat failed: no base version given and no v1.0.0-* tag is merged into HEAD" >&2
  exit 2
}

# `mapfile` is bash 4+; macOS ships 3.2 and the whole point of this script is that it runs the same locally as in
# CI. Read into a plain array instead.
packages=()
while IFS= read -r line; do
  [ -n "$line" ] && packages+=("$line")
done < <(grep -v '^[[:space:]]*#' "$list" | grep -v '^[[:space:]]*$')
[ "${#packages[@]:-0}" -gt 0 ] || {
  echo "api-compat failed: $list lists no packages" >&2
  exit 2
}

work="$(mktemp -d)" || exit 2
trap 'rm -rf "$work"' EXIT

echo "api-compat: ${#packages[@]} Tier 1 packages, base $base -> HEAD"
echo

# Build apidiff once. `go run` would re-resolve the module on every invocation, turning a 67-package sweep into 200
# module loads.
if ! (cd "$work" && go mod init apicompat >/dev/null 2>&1 && GOFLAGS=-mod=mod go build -o "$work/apidiff" "${APIDIFF_PKG%@*}" 2>"$work/build.err"); then
  # go build wants the module in the probe's own requirements; go run resolves it directly. Fall back rather than
  # fail, but keep the error if both paths are broken.
  if ! GOFLAGS=-mod=mod go build -o "$work/apidiff" "$APIDIFF_PKG" 2>>"$work/build.err"; then
    echo "api-compat failed: could not build apidiff ($APIDIFF_PKG)" >&2
    sed 's/^/  /' "$work/build.err" >&2
    exit 2
  fi
fi

# A throwaway module pinned at the base version, so export data for "old" comes from the module cache rather than
# from a checkout. This is what keeps the script read-only against the working tree: nothing is stashed, nothing is
# checked out, and a dirty tree stays dirty.
probe="$work/probe"
mkdir -p "$probe"
(
  cd "$probe" || exit 2
  go mod init apicompatprobe >/dev/null 2>&1
  GOFLAGS=-mod=mod go get "github.com/c360studio/semstreams@$base" >/dev/null 2>&1
) || {
  echo "api-compat failed: could not fetch github.com/c360studio/semstreams@$base" >&2
  exit 2
}

# Counters kept alongside the arrays: under `set -u` bash 3.2 errors on ${arr[@]} when the array is empty, and the
# guarded expansion is easy to get subtly wrong. The counts are what the summary and the exit status read.
compared=0
n_removed=0
n_added=0
n_incompatible=0
n_clean=0
removed=()
added=()
incompatible=()
clean=()

for pkg in "${packages[@]}"; do
  slug="$(printf '%s' "$pkg" | tr '/.' '__')"
  old="$work/old_$slug.dat"
  new="$work/new_$slug.dat"

  old_ok=0
  (cd "$probe" && GOFLAGS=-mod=mod "$work/apidiff" -w "$old" "$pkg") >/dev/null 2>&1 && old_ok=1
  new_ok=0
  (cd "$root" && GOFLAGS=-mod=readonly "$work/apidiff" -w "$new" "$pkg") >/dev/null 2>&1 && new_ok=1

  if [ "$old_ok" -eq 1 ] && [ "$new_ok" -eq 0 ]; then
    removed+=("$pkg"); n_removed=$((n_removed + 1))
    continue
  fi
  if [ "$old_ok" -eq 0 ] && [ "$new_ok" -eq 1 ]; then
    added+=("$pkg"); n_added=$((n_added + 1))
    continue
  fi
  if [ "$old_ok" -eq 0 ] && [ "$new_ok" -eq 0 ]; then
    # Neither side loads. Not a compatibility statement — the list names a package that does not build at either
    # end, which is a defect in the list or the tree, and silence would hide it.
    removed+=("$pkg (loads at neither $base nor HEAD — check $list)"); n_removed=$((n_removed + 1))
    continue
  fi

  compared=$((compared + 1))
  out="$("$work/apidiff" "$old" "$new" 2>&1)"
  if printf '%s' "$out" | grep -q '^Incompatible changes:'; then
    incompatible+=("$pkg"); n_incompatible=$((n_incompatible + 1))
    printf '%s\n' "--- $pkg"
    printf '%s\n\n' "$out" | sed 's/^/    /'
  else
    clean+=("$pkg"); n_clean=$((n_clean + 1))
  fi
done

if [ "$compared" -eq 0 ] && [ "$n_removed" -eq 0 ]; then
  echo "api-compat failed: zero packages compared — the sweep did not run" >&2
  exit 2
fi

if [ "$n_removed" -gt 0 ]; then
  echo "--- REMOVED (present at $base, absent at HEAD — an adopter cannot compile the import)"
  printf '    %s\n' "${removed[@]}"
  echo
fi
if [ "$n_added" -gt 0 ]; then
  echo "--- ADDED (new Tier 1 surface; compatible here, still owes a walked path under RC-6)"
  printf '    %s\n' "${added[@]}"
  echo
fi

fail_count=$((n_incompatible + n_removed))

echo "api-compat summary (base $base -> HEAD)"
echo "  compared:      $compared"
echo "  clean:         $n_clean"
echo "  incompatible:  $n_incompatible"
echo "  removed:       $n_removed"
echo "  added:         $n_added"
echo "  FAILING TOTAL: $fail_count"

if [ "$fail_count" -eq 0 ]; then
  echo
  echo "Tier 1 is compatible with $base."
  exit 0
fi

if [ "${API_COMPAT_MODE:-}" = "report" ]; then
  echo
  echo "API_COMPAT_MODE=report: $fail_count Tier 1 break(s) reported, exiting 0 (pre-RC posture)."
  exit 0
fi

echo
echo "$fail_count Tier 1 break(s). Under ADR-106 these are semver-binding at 1.0; before RC they must reach zero" >&2
echo "and stay there for 30 days. Set API_COMPAT_MODE=report for the pre-RC reporting posture." >&2
exit 1
