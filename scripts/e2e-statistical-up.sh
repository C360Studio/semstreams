#!/usr/bin/env bash
# gh#1317: retain bind-failure evidence before statistical.yml's existing deferred down.
# Deliberately specific to this tier: no retries, port reservation, or cleanup authority.
set -uo pipefail

# Probe output is capped at 64 KiB; every external probe gets 2s plus 1s kill grace.
# At most five probes: sockets, proxy processes, sysctl, Docker IDs, bindings (15s probe budget).
capture() {
  local label=$1 rc LC_ALL=C
  shift
  echo "[BIND-DIAG] $label"
  PROBE_OUT=$(timeout --kill-after=1s 2s "$@" 2>&1 | head -c 65537)
  rc=$?
  if [ "${#PROBE_OUT}" -gt 65536 ]; then
    printf '%s\n' "${PROBE_OUT:0:65536}"
    echo "[BIND-DIAG] UNKNOWN: $label output truncated at 64 KiB"
    return 1
  fi
  if [ "$rc" -ne 0 ]; then
    printf '%s\n' "$PROBE_OUT"
    echo "[BIND-DIAG] UNKNOWN: $label failed or timed out (exit $rc)"
    return 1
  fi
  if [ -z "$PROBE_OUT" ]; then
    echo "[BIND-DIAG] UNKNOWN: $label returned no rows; holder unresolved"
    return 1
  fi
}

diagnose() {
  local port=$1 ids rc matches
  local socket_args=(-H -a -n -p -t -u)
  echo "[BIND-DIAG] failed host port: $port"
  echo '[BIND-DIAG] Observations are after the bind failure; absence does not establish a free port.'
  if [ "$port" != UNKNOWN ]; then
    socket_args+=("sport = :$port")
  fi
  if command -v ss >/dev/null 2>&1; then
    # Include established and TIME-WAIT sockets, not only listeners. Unprivileged ss may omit process owners.
    if capture 'socket states/processes (ss; process visibility may be restricted)' ss "${socket_args[@]}"; then
      printf '%s\n' "$PROBE_OUT"
    fi
  elif command -v lsof >/dev/null 2>&1; then
    local lsof_args=(-nP -i)
    [ "$port" = UNKNOWN ] || lsof_args=(-nP "-i:$port")
    if capture 'socket endpoints/processes (lsof; visibility may be restricted)' lsof "${lsof_args[@]}"; then
      printf '%s\n' "$PROBE_OUT"
    fi
  else
    echo '[BIND-DIAG] UNKNOWN: socket probe unavailable (need ss or lsof)'
  fi

  if [ "$(uname -s)" = Linux ]; then
    echo '[BIND-DIAG] Proxy process candidates are metadata, not proof of socket ownership.'
    echo '[BIND-DIAG] Process visibility may omit holders; disabled/renamed proxies and non-proxy holders are not covered.'
    if capture 'docker-proxy candidate processes (unprivileged, full argv)' \
      ps -ww -C docker-proxy -o pid=,ppid=,user=,args=; then
      printf '%s\n' "$PROBE_OUT"
    fi
  fi

  if capture 'kernel reservation/range readback (read-only)' sysctl \
    net.ipv4.ip_local_reserved_ports net.ipv4.ip_local_port_range; then
    printf '%s\n' "$PROBE_OUT"
  fi

  echo '[BIND-DIAG] requested_bindings are configuration, not proof of the current holder.'
  echo '[BIND-DIAG] observed_mappings are Docker-reported host mappings, not an atomic socket-owner snapshot.'
  if ! command -v jq >/dev/null 2>&1; then
    echo '[BIND-DIAG] UNKNOWN: jq unavailable; cannot select container port evidence'
    return
  fi
  if capture 'container IDs (all states, no publish filter)' docker ps --all --quiet --no-trunc; then
    ids=$PROBE_OUT
    # Inspect at most 100 containers. Do not silently call this an exhaustive attribution if capped.
    if [ "$(printf '%s\n' "$ids" | awk 'END {print NR}')" -gt 100 ]; then
      echo '[BIND-DIAG] UNKNOWN: container inventory truncated to first 100 IDs'
      ids=$(printf '%s\n' "$ids" | head -n 100)
    fi
    # Docker IDs contain no whitespace. Project only these fields: raw inspect could expose environment secrets.
    # shellcheck disable=SC2086
    if capture 'container requested and observed port bindings' docker inspect --format \
      '{"id":{{json .Id}},"name":{{json .Name}},"status":{{json .State.Status}},"pid":{{json .State.Pid}},"error":{{json .State.Error}},"requested_bindings":{{json .HostConfig.PortBindings}},"observed_mappings":{{json .NetworkSettings.Ports}}}' $ids; then
      matches=$(printf '%s\n' "$PROBE_OUT" | jq -c --arg port "$port" '
        def has_host_port: [. // {} | .[]? | .[]? | .HostPort] | index($port) != null;
        select($port == "UNKNOWN" or (.requested_bindings | has_host_port) or (.observed_mappings | has_host_port))')
      rc=$?
      if [ "$rc" -ne 0 ]; then
        echo "[BIND-DIAG] UNKNOWN: could not decode container port evidence (exit $rc)"
      elif [ -z "$matches" ]; then
        echo '[BIND-DIAG] UNKNOWN: no matching container port records; holder unresolved'
      else
        printf '%s\n' "$matches"
      fi
    fi
  fi
  echo '[BIND-DIAG] Capture complete; matching records alone do not establish the bind failure root cause.'
}

log=$(mktemp) || {
  echo '[BIND-DIAG] UNKNOWN: cannot retain compose output; running unchanged command without diagnostics' >&2
  exec docker compose -f docker/compose/tiered.yml --profile statistical up -d --wait --build
}
trap 'rm -f "$log"' EXIT
# Stream build/start output as before while retaining the actual daemon error; never re-derive Compose's port set.
docker compose -f docker/compose/tiered.yml --profile statistical up -d --wait --build 2>&1 | tee "$log"
compose_status=${PIPESTATUS[0]}
if [ "$compose_status" -ne 0 ] && grep -Eq 'failed to bind host port|Bind for .* failed: port is already allocated|ports are not available:.*address already in use' "$log"; then
  # Docker's two common bind errors identify the failed HOST port before the target address/port.
  # Unrecognized text remains UNKNOWN and widens the bounded snapshot instead of inventing a port.
  port=$(sed -nE \
    -e 's/.*failed to bind host port for (\[[^]]*\]|[^: ]+):([0-9]+):.*/\2/p' \
    -e 's/.*Bind for .*:([0-9]+) failed:.*/\1/p' "$log" | head -n 1)
  [ -n "$port" ] || port=UNKNOWN
  if command -v timeout >/dev/null 2>&1; then
    diagnose "$port"
  else
    echo '[BIND-DIAG] UNKNOWN: timeout unavailable; bounded diagnostics cannot run'
  fi
fi
exit "$compose_status"
