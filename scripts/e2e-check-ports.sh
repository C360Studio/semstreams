#!/usr/bin/env bash
# e2e-check-ports.sh — the e2e host-port preflight (gh#1175).
#
# Answers exactly one question: are the host ports THIS e2e run is about to
# bind free right now, and if not, who holds them?
#
# The set is DERIVED from the compose context the caller is about to boot —
# `docker compose -f <file> [--profile <p>] config` is the same resolver
# `docker compose up` uses, so the guard cannot drift from what gets bound.
# A hand-maintained port list was the defect this replaces: the old guard
# probed 2 ports (38080/tcp, 34550/udp) and then printed "[OK] All ports
# available", while a statistical-tier run died binding 36060 — a port no
# preflight had ever looked at.
#
# Deriving per-context also keeps the guard non-hostile. The union of every
# compose file in this repo includes 3000, 8081, 8083, 9000 and 9090, but
# those come from docker/compose/services.yml (the local dev services stack),
# which no e2e tier boots. Probing the union unconditionally would stop a
# developer with anything ordinary on :3000 from running e2e at all.
#
# Usage (from repo root):
#   scripts/e2e-check-ports.sh [--profile <name>]... <compose-file>...
#   scripts/e2e-check-ports.sh                        # advisory union mode
#
# Exit codes:
#   0  every derived port is free (or advisory mode, which never fails)
#   1  a derived port is held, or the guard could not determine the answer
#   2  usage error
#
# Fail-closed: anything the guard cannot resolve — an unresolvable compose
# context, an empty derived set, a non-numeric published port, no available
# port prober — exits 1. "[OK]" is only ever printed over a fully probed set,
# and always carries the count it probed.

set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/e2e-check-ports.sh [--profile <name>]... <compose-file>...
       scripts/e2e-check-ports.sh                      (advisory union mode)

Probes the host ports the given compose context is about to publish.
With no compose file, probes the union of every compose file the e2e
taskfiles boot and reports holders as warnings without failing.
USAGE
}

PROFILES=()
FILES=()
while [ $# -gt 0 ]; do
  case "$1" in
    --profile)
      shift
      [ $# -gt 0 ] || { echo "[ERROR] --profile requires a value" >&2; exit 2; }
      PROFILES=(${PROFILES[@]+"${PROFILES[@]}"} "$1")
      ;;
    --profile=*)
      PROFILES=(${PROFILES[@]+"${PROFILES[@]}"} "${1#--profile=}")
      ;;
    -h|--help) usage; exit 0 ;;
    --) shift; while [ $# -gt 0 ]; do FILES=(${FILES[@]+"${FILES[@]}"} "$1"); shift; done ;;
    -*) echo "[ERROR] unknown flag: $1" >&2; usage >&2; exit 2 ;;
    *) FILES=(${FILES[@]+"${FILES[@]}"} "$1") ;;
  esac
  shift
done

command -v docker >/dev/null 2>&1 || { echo "[ERROR] docker is not on PATH; cannot derive the port set" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "[ERROR] jq is not on PATH; cannot derive the port set" >&2; exit 1; }

# --- port prober -------------------------------------------------------------
#
# `ss` is preferred over `lsof` and the ordering is load-bearing, not stylistic.
# On Linux, `lsof -i` run as a non-root user cannot see a listening socket owned
# by root — it exits 1 with no output. Docker's docker-proxy listeners are
# root-owned and CI runs task as a non-root user, so an lsof-first guard is
# structurally blind to the exact holder class this preflight exists to catch.
# `ss` reads /proc/net/{tcp,udp} and lists every listener regardless of owner.
# macOS has no `ss`; there Docker Desktop's listener is owned by the invoking
# user, so `lsof` sees it.
PROBER=""
if command -v ss >/dev/null 2>&1; then
  PROBER="ss"
elif command -v lsof >/dev/null 2>&1; then
  PROBER="lsof"
else
  echo "[ERROR] no port prober available (need 'ss' or 'lsof'); cannot certify any port as free" >&2
  exit 1
fi

# port_held <port> <tcp|udp> -> 0 when something holds it, 1 when free
port_held() {
  port_held_port=$1
  port_held_proto=$2
  case "$PROBER" in
    ss)
      if [ "$port_held_proto" = "udp" ]; then
        port_held_out=$(ss -H -ln --udp "sport = :$port_held_port" 2>/dev/null || true)
      else
        port_held_out=$(ss -H -ln --tcp "sport = :$port_held_port" 2>/dev/null || true)
      fi
      [ -n "$port_held_out" ]
      ;;
    lsof)
      if [ "$port_held_proto" = "udp" ]; then
        lsof -nP -iUDP:"$port_held_port" -t >/dev/null 2>&1
      else
        lsof -nP -iTCP:"$port_held_port" -sTCP:LISTEN -t >/dev/null 2>&1
      fi
      ;;
  esac
}

# describe_holder <port> <tcp|udp> — names who holds it, on stdout, indented.
# Container attribution comes first: a leftover container is the failure this
# guard sees most, and `docker ps --filter publish` names it without the
# privileges the process-level probe would need on Linux.
describe_holder() {
  dh_port=$1
  dh_proto=$2
  # The protocol suffix is required: `--filter publish=34550` matches only TCP
  # publishes and silently returns nothing for a UDP holder (measured).
  dh_containers=$(docker ps --filter "publish=$dh_port/$dh_proto" --format '{{.Names}} ({{.Image}}) {{.Ports}}' 2>/dev/null || true)
  if [ -n "$dh_containers" ]; then
    printf '%s\n' "$dh_containers" | while IFS= read -r dh_line; do
      [ -n "$dh_line" ] && printf '          container: %s\n' "$dh_line"
    done
  fi
  case "$PROBER" in
    ss)
      if [ "$dh_proto" = "udp" ]; then
        dh_detail=$(ss -H -lnp --udp "sport = :$dh_port" 2>/dev/null || true)
      else
        dh_detail=$(ss -H -lnp --tcp "sport = :$dh_port" 2>/dev/null || true)
      fi
      ;;
    lsof)
      if [ "$dh_proto" = "udp" ]; then
        dh_detail=$(lsof -nP -iUDP:"$dh_port" 2>/dev/null | tail -n +2 || true)
      else
        dh_detail=$(lsof -nP -iTCP:"$dh_port" -sTCP:LISTEN 2>/dev/null | tail -n +2 || true)
      fi
      ;;
  esac
  if [ -n "${dh_detail:-}" ]; then
    printf '%s\n' "$dh_detail" | while IFS= read -r dh_line; do
      [ -n "$dh_line" ] && printf '          %s: %s\n' "$PROBER" "$dh_line"
    done
  fi
  if [ -z "$dh_containers" ] && [ -z "${dh_detail:-}" ]; then
    printf '          holder not attributable with %s (socket may be owned by another user)\n' "$PROBER"
  fi
}

# --- port derivation ---------------------------------------------------------

# derive_ports <docker compose args...> — prints "<port> <proto> <service>" per
# published mapping. Exits 1 when the compose context will not resolve: a set we
# cannot compute is never reported as an empty set.
derive_ports() {
  dp_raw=$(docker compose "$@" config --format json 2>&1) || {
    echo "[ERROR] could not resolve compose context: docker compose $* config" >&2
    printf '%s\n' "$dp_raw" | head -n 10 >&2
    return 1
  }
  printf '%s' "$dp_raw" | jq -r '
    .services | to_entries[] | .key as $svc | (.value.ports // [])[]
    | if (.published == null or (.published | tostring) == "")
      then "EPHEMERAL - \($svc)"
      else "\(.published | tostring) \(.protocol // "tcp") \($svc)"
      end
  ' || {
    echo "[ERROR] could not parse the resolved compose context as JSON" >&2
    return 1
  }
}

# compose_args <file...> — echoes the -f/--profile argument list.
build_compose_args() {
  ca_out=""
  for ca_f in "$@"; do
    ca_out="$ca_out -f $ca_f"
  done
  for ca_p in ${PROFILES[@]+"${PROFILES[@]}"}; do
    ca_out="$ca_out --profile $ca_p"
  done
  printf '%s' "$ca_out"
}

# --- advisory union mode -----------------------------------------------------

if [ "${#FILES[@]}" -eq 0 ]; then
  echo "[CHECK] No compose context supplied — advisory union mode."
  # The union is derived from the e2e taskfiles themselves, so it tracks what
  # the tiers actually boot and cannot include the dev services stack.
  union_files=$(grep -rhoE 'docker/compose/[A-Za-z0-9._-]+\.yml' taskfiles/e2e/*.yml 2>/dev/null | sort -u)
  if [ -z "$union_files" ]; then
    echo "[ERROR] no compose files found in taskfiles/e2e/ (run from the repo root)" >&2
    exit 1
  fi
  advisory_skipped=""
  advisory_ports=""
  advisory_files=0
  for uf in $union_files; do
    [ -f "$uf" ] || continue
    uf_profiles=$(docker compose -f "$uf" config --profiles 2>/dev/null || true)
    uf_args="-f $uf"
    for up in $uf_profiles; do
      uf_args="$uf_args --profile $up"
    done
    # Overlay files (tiered.8b.yml, tiered.frontier.yml) do not resolve alone.
    # Name every skip; a silently dropped file would overstate the sweep.
    # shellcheck disable=SC2086
    if ! uf_ports=$(derive_ports $uf_args 2>/dev/null); then
      advisory_skipped="$advisory_skipped $uf"
      continue
    fi
    advisory_files=$((advisory_files + 1))
    advisory_ports="$advisory_ports
$(printf '%s\n' "$uf_ports" | grep -v '^EPHEMERAL' | awk -v f="$uf" '{print $1" "$2" "f":"$3}' || true)"
  done
  # One line per distinct port+protocol; the first compose file that wants it
  # is kept as the attribution so a WARN says who cares about the port.
  advisory_ports=$(printf '%s\n' "$advisory_ports" | grep -v '^$' | sort -k1,1n -k2,2 | awk '!seen[$1" "$2]++' || true)
  advisory_total=0
  advisory_held=0
  while IFS=' ' read -r p proto owner; do
    [ -n "${p:-}" ] || continue
    advisory_total=$((advisory_total + 1))
    if port_held "$p" "$proto"; then
      advisory_held=$((advisory_held + 1))
      printf '[WARN] %s/%s is held (wanted by %s)\n' "$p" "$proto" "$owner"
      describe_holder "$p" "$proto"
    fi
  done <<EOF
$advisory_ports
EOF
  echo "[ADVISORY] probed $advisory_total distinct published ports across $advisory_files e2e compose file(s), all profiles enabled; $advisory_held held."
  [ -n "$advisory_skipped" ] && echo "[ADVISORY] not resolvable standalone, skipped:$advisory_skipped"
  echo "[ADVISORY] This is NOT a per-run guarantee and never fails the build — pass the run's compose file (and --profile) to get one."
  exit 0
fi

# --- derived mode ------------------------------------------------------------

for f in "${FILES[@]}"; do
  [ -f "$f" ] || { echo "[ERROR] compose file not found: $f (run from the repo root)" >&2; exit 1; }
done

# A profile the compose context does not define resolves silently to the
# unprofiled services only — the guard would then probe a small set and report
# it as complete. Reject the typo instead of certifying the wrong set.
if [ "${#PROFILES[@]}" -gt 0 ]; then
  declared_profiles=""
  for f in "${FILES[@]}"; do
    declared_profiles="$declared_profiles
$(docker compose -f "$f" config --profiles 2>/dev/null || true)"
  done
  for p in "${PROFILES[@]}"; do
    if ! printf '%s\n' "$declared_profiles" | grep -qx "$p"; then
      echo "[ERROR] profile '$p' is not declared by ${FILES[*]}." >&2
      echo "[ERROR] Declared profiles:$(printf '%s' "$declared_profiles" | tr '\n' ' ')" >&2
      echo "[ERROR] Refusing to certify: an unmatched profile silently narrows the derived set." >&2
      exit 1
    fi
  done
fi

ctx="${FILES[*]}"
if [ "${#PROFILES[@]}" -gt 0 ]; then
  ctx="$ctx (profile: ${PROFILES[*]})"
fi
echo "[CHECK] Deriving published host ports from $ctx ..."

args=$(build_compose_args "${FILES[@]}")
# shellcheck disable=SC2086
resolved=$(derive_ports $args) || exit 1
resolved=$(printf '%s\n' "$resolved" | grep -v '^$' | sort -u || true)

ephemeral=$(printf '%s\n' "$resolved" | grep -c '^EPHEMERAL' || true)
# Collapse to one line per distinct port+protocol. Two services in the same
# context can declare the same host port when a profile swaps one for the other
# (e2e.yml's `fixtures` twin does exactly this); counting both would inflate the
# coverage number the [OK] line reports.
published=$(printf '%s\n' "$resolved" | grep -v '^EPHEMERAL' | sort -k1,1n -k2,2 | awk '!seen[$1" "$2]++' || true)

if [ -z "$published" ]; then
  echo "[ERROR] resolved 0 published host ports from $ctx." >&2
  echo "[ERROR] Refusing to certify: an empty set means a wrong file or an unmatched profile, not a clean host." >&2
  exit 1
fi

total=$(printf '%s\n' "$published" | wc -l | tr -d ' ')
held_report=""
held_count=0
free_count=0

while IFS=' ' read -r port proto svc; do
  [ -n "${port:-}" ] || continue
  case "$port" in
    ''|*[!0-9]*)
      echo "[ERROR] compose resolved a non-numeric published port for $svc: '$port'" >&2
      echo "[ERROR] Refusing to certify a set the guard cannot probe." >&2
      exit 1
      ;;
  esac
  if port_held "$port" "$proto"; then
    held_count=$((held_count + 1))
    held_report="$held_report
[ERROR] Port $port/$proto is already in use (needed by service '$svc')
$(describe_holder "$port" "$proto")"
  else
    free_count=$((free_count + 1))
  fi
done <<EOF
$published
EOF

if [ "$held_count" -gt 0 ]; then
  printf '%s\n' "$held_report"
  echo "[FAIL] $held_count of $total published ports for $ctx are held. Free them (task e2e:clean, or stop the named holder) and retry."
  exit 1
fi

echo "[OK] $free_count/$total distinct published host ports available for $ctx (prober: $PROBER)"
if [ "$ephemeral" -gt 0 ]; then
  echo "[NOTE] $ephemeral mapping(s) publish to an ephemeral host port and are not predeterminable; they are excluded from the count above."
fi
