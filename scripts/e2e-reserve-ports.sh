#!/usr/bin/env bash
# e2e-reserve-ports.sh — reserve the e2e host ports against the kernel's
# ephemeral allocator (gh#1279).
#
# The preflight in e2e-check-ports.sh answers "is this port free right now".
# That is a snapshot, and a snapshot is not a reservation: in run
# 34392378822 the guard printed "[OK] 8/8 distinct published host ports
# available" and the bind failed 77 SECONDS later with
#
#   failed to bind host port for 0.0.0.0:38082 ... address already in use
#
# on a commit that touched only openspec/ files. Between probe and bind sits a
# full Go + Docker image build, opening outbound sockets the whole time.
#
# The reason those sockets can take the port is that 29 of the 44 published
# host ports in docker/compose/*.yml fall inside Linux's default
# net.ipv4.ip_local_port_range (32768-60999). A connect() with no explicit
# source port draws from that range, so the kernel is free to hand out the
# very port a tier is about to publish. Nothing has to leak; this is the
# allocator doing its job over a range nobody told it was spoken for.
#
# net.ipv4.ip_local_reserved_ports is the mechanism that actually tells it.
# Ports listed there are excluded from ephemeral allocation while remaining
# bindable explicitly — which is exactly the asymmetry an e2e stack wants.
#
# Renumbering the ports out of the range would also work and is strictly
# better, but it is not a digit shift: the scheme is one stack digit plus a
# four-digit service code, and the only safe band above the range is
# 61000-65535, where 6+8080 = 68080 is not a port. That needs a designed
# allocation table across 33 files. This reserves what exists today.
#
# Usage (from repo root):
#   scripts/e2e-reserve-ports.sh            # derive from the e2e compose union
#   scripts/e2e-reserve-ports.sh --dry-run  # print what it would reserve
#
# Exit codes:
#   0  reserved, verified, and read back — or a deliberate no-op off Linux
#   1  could not derive the set, or the kernel did not accept the write
#   2  usage error
#
# Fail-closed: it never reports success over a set it could not derive or a
# write it could not verify. Additive by construction — it merges with
# whatever is already reserved rather than replacing it.

set -euo pipefail

DRY_RUN=0
while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY_RUN=1 ;;
    -h|--help) sed -n '2,45p' "$0"; exit 0 ;;
    *) echo "[ERROR] unknown argument: $1" >&2; exit 2 ;;
  esac
  shift
done

SYSCTL_KEY=net.ipv4.ip_local_reserved_ports
RANGE_KEY=net.ipv4.ip_local_port_range

command -v docker >/dev/null 2>&1 || { echo "[ERROR] docker is not on PATH; cannot derive the port set" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "[ERROR] jq is not on PATH; cannot derive the port set" >&2; exit 1; }

# --- derive the set ----------------------------------------------------------
#
# Same source of truth as the preflight: `docker compose config` is the
# resolver `docker compose up` uses, and the file list comes from the e2e
# taskfiles, so this tracks what the tiers actually boot. A hand-written list
# is the defect gh#1175 removed from the preflight; it is not reintroduced here.
# `|| true` is load-bearing under `set -euo pipefail`: with no taskfiles the
# glob stays literal, grep exits 2, and pipefail would abort the script HERE —
# fail-closed by accident, with the wrong exit code and no explanation. The
# explicit check below is the one that refuses, and it can only run if this
# assignment is allowed to produce an empty string.
union_files=$(grep -rhoE 'docker/compose/[A-Za-z0-9._-]+\.yml' taskfiles/e2e/*.yml 2>/dev/null | sort -u || true)
if [ -z "$union_files" ]; then
  echo "[ERROR] derived 0 compose files from taskfiles/e2e/ — refusing to claim a reservation" >&2
  exit 1
fi

derived=""
resolved_files=0
for f in $union_files; do
  [ -f "$f" ] || continue
  # --profile '*' resolves every profile, so a tier-specific service cannot be
  # missed the way a profile-less `config` would miss it.
  if ports=$(docker compose -f "$f" --profile '*' config --format json 2>/dev/null \
      | jq -r '.services // {} | to_entries[] | .value.ports // [] | .[]
               | select(.published != null and (.published | tostring) != "")
               | .published | tostring' 2>/dev/null); then
    derived="$derived $ports"
    resolved_files=$((resolved_files + 1))
  fi
done

if [ "$resolved_files" -eq 0 ]; then
  echo "[ERROR] no compose file resolved; derived nothing to reserve" >&2
  exit 1
fi

# Published values can be ranges ("8080-8090"); expand and keep numerics only.
wanted=$(printf '%s\n' $derived | tr ' ' '\n' | while read -r p; do
  [ -n "$p" ] || continue
  if [ "${p#*-}" != "$p" ]; then
    lo=${p%%-*}
    hi=${p##*-}
    if printf '%s%s' "$lo" "$hi" | grep -qE '^[0-9]+$'; then
      seq "$lo" "$hi"
    fi
  elif printf '%s' "$p" | grep -qE '^[0-9]+$'; then
    printf '%s\n' "$p"
  fi
done | sort -un)

[ -n "$wanted" ] || { echo "[ERROR] derived 0 published host ports — refusing to claim a reservation" >&2; exit 1; }

# --- narrow to the ports that are actually at risk ---------------------------
#
# Read the range rather than hardcoding 32768-60999: a runner image is free to
# use a different one, and reserving ports outside it is noise that makes the
# sysctl value harder to read.
# --- platform gate -----------------------------------------------------------
#
# Only Linux has this knob. macOS Docker Desktop publishes through a user-owned
# proxy and has no equivalent, so there is nothing honest to do there: say so
# and exit 0 rather than pretending a reservation happened.
#
# The gate sits HERE, after derivation, not at the top: --dry-run must be
# runnable on any platform or the derivation — the half most likely to break
# when a compose file moves — could only ever be exercised inside CI.
HAVE_KNOB=1
[ "$(uname -s)" = "Linux" ] && [ -e "/proc/sys/${SYSCTL_KEY//.//}" ] || HAVE_KNOB=0

if [ "$HAVE_KNOB" -eq 1 ]; then
  range=$(cat "/proc/sys/${RANGE_KEY//.//}")
else
  # The documented Linux default, used only to make --dry-run meaningful off-Linux.
  range="32768 60999"
  echo "[RESERVE] $SYSCTL_KEY is absent on $(uname -s); assuming the Linux default range for this report."
fi
range_lo=$(printf '%s\n' "$range" | awk '{print $1}')
range_hi=$(printf '%s\n' "$range" | awk '{print $2}')
echo "[RESERVE] kernel ephemeral range: ${range_lo}-${range_hi}"

at_risk=$(printf '%s\n' $wanted | awk -v lo="$range_lo" -v hi="$range_hi" '$1>=lo && $1<=hi' | sort -un)
total_derived=$(printf '%s\n' $wanted | wc -l | tr -d ' ')
total_at_risk=$(printf '%s\n' ${at_risk:-} | grep -c . || true)

echo "[RESERVE] derived $total_derived distinct published host ports across $resolved_files e2e compose file(s); $total_at_risk inside the ephemeral range."

if [ -z "$at_risk" ]; then
  echo "[OK] no published host port falls inside the ephemeral range — nothing to reserve."
  exit 0
fi

# --- merge with whatever is already reserved ---------------------------------
existing=$(cat "/proc/sys/${SYSCTL_KEY//.//}" 2>/dev/null || echo "")
existing_expanded=$(printf '%s' "$existing" | tr ',' '\n' | while read -r e; do
  [ -n "$e" ] || continue
  if [ "${e#*-}" != "$e" ]; then
    lo=${e%%-*}
    hi=${e##*-}
    if printf '%s%s' "$lo" "$hi" | grep -qE '^[0-9]+$'; then
      seq "$lo" "$hi"
    fi
  elif printf '%s' "$e" | grep -qE '^[0-9]+$'; then
    printf '%s\n' "$e"
  fi
done | sort -un)

merged=$(printf '%s\n%s\n' "$at_risk" "${existing_expanded:-}" | grep -E '^[0-9]+$' | sort -un)

# Collapse runs into ranges — the kernel accepts a bounded string and a long
# comma list can exceed it.
value=$(printf '%s\n' $merged | awk '
  NR==1 { start=$1; prev=$1; next }
  $1 == prev+1 { prev=$1; next }
  { printf "%s%s", (out++ ? "," : ""), (start==prev ? start : start "-" prev); start=$1; prev=$1 }
  END { printf "%s%s\n", (out++ ? "," : ""), (start==prev ? start : start "-" prev) }')

echo "[RESERVE] $SYSCTL_KEY = $value"

if [ "$DRY_RUN" -eq 1 ]; then
  echo "[DRY-RUN] not applied."
  exit 0
fi

if [ "$HAVE_KNOB" -eq 0 ]; then
  echo "[RESERVE] not Linux (or $SYSCTL_KEY absent) — nothing reserved."
  echo "[RESERVE] The preflight in e2e-check-ports.sh is the only guard on this platform."
  exit 0
fi

SUDO=""
[ "$(id -u)" -eq 0 ] || SUDO="sudo"
if ! $SUDO sysctl -q -w "$SYSCTL_KEY=$value"; then
  echo "[ERROR] kernel refused the reservation write" >&2
  exit 1
fi

# --- verify by reading back --------------------------------------------------
#
# A sysctl that silently truncated is indistinguishable from one that worked
# unless the value is read back and every requested port confirmed present.
readback=$(cat "/proc/sys/${SYSCTL_KEY//.//}")
missing=""
for p in $at_risk; do
  printf '%s' "$readback" | tr ',' '\n' | awk -v want="$p" '
    /-/ { split($0, r, "-"); if (want+0 >= r[1]+0 && want+0 <= r[2]+0) { found=1 } ; next }
    { if ($0+0 == want+0) found=1 }
    END { exit found ? 0 : 1 }' || missing="$missing $p"
done

if [ -n "$missing" ]; then
  echo "[ERROR] the kernel accepted the write but these ports are not reserved:$missing" >&2
  echo "[ERROR] read back: $readback" >&2
  exit 1
fi

echo "[OK] $total_at_risk port(s) reserved against ephemeral allocation and verified by read-back."
