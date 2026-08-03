#!/usr/bin/env bash
set -euo pipefail

# Lock contention fails immediately by default. Set
# SEMSTREAMS_INTEGRATION_LOCK_WAIT_SECONDS to an integer 1-3600 only when a
# caller deliberately chooses a bounded wait budget.
readonly max_wait_seconds=3600
readonly default_lock_dir="/tmp/semstreams-integration.lock"
# SEMSTREAMS_INTEGRATION_LOCK_DIR is reserved for the non-Docker lock contract
# tests; Task and CI always use the fixed host-level default above.
lock_dir="${SEMSTREAMS_INTEGRATION_LOCK_DIR:-$default_lock_dir}"
wait_seconds="${SEMSTREAMS_INTEGRATION_LOCK_WAIT_SECONDS:-0}"
refresh_image="${SEMSTREAMS_INTEGRATION_REFRESH_IMAGE:-0}"
# Contract tests shorten this to prove the watchdog. Production callers use the
# fixed five-minute ceiling and do not need to predict registry latency.
image_pull_timeout_seconds="${SEMSTREAMS_CONTRACT_IMAGE_PULL_TIMEOUT_SECONDS:-300}"

if [[ ! "$wait_seconds" =~ ^[0-9]+$ ]] || (( wait_seconds > max_wait_seconds )); then
  echo "[INTEGRATION] invalid SEMSTREAMS_INTEGRATION_LOCK_WAIT_SECONDS=$wait_seconds (expected 0-$max_wait_seconds)" >&2
  exit 2
fi
if [[ "$refresh_image" != "0" && "$refresh_image" != "1" ]]; then
  echo "[INTEGRATION] invalid SEMSTREAMS_INTEGRATION_REFRESH_IMAGE=$refresh_image (expected 0 or 1)" >&2
  exit 2
fi
if [[ ! "$image_pull_timeout_seconds" =~ ^[1-9][0-9]*$ ]] || (( image_pull_timeout_seconds > 300 )); then
  echo "[INTEGRATION] invalid contract image-pull timeout $image_pull_timeout_seconds (expected 1-300)" >&2
  exit 2
fi

owner_host=$(hostname)
owner_pid=$$
owner_started=$(date +%s)
owner_identity=$(ps -o lstart= -p "$owner_pid" 2>/dev/null | sed 's/^[[:space:]]*//' || true)
if [[ -z "$owner_identity" ]]; then
  owner_identity="unknown"
fi
owner_token="${owner_host}:${owner_pid}:${owner_started}:${RANDOM:-0}"
lock_held=false
image_pull_output_file=""
image_pull_pid=""
image_pull_timed_out=false

read_owner() {
  observed_host="unknown"
  observed_pid="unknown"
  observed_started="0"
  observed_identity="unknown"
  observed_token="unknown"
  observed_command="unknown"
  if [[ ! -f "$lock_dir/owner" ]]; then
    return
  fi
  while IFS='=' read -r key value; do
    case "$key" in
      host) observed_host=$value ;;
      pid) observed_pid=$value ;;
      started) observed_started=$value ;;
      identity) observed_identity=$value ;;
      token) observed_token=$value ;;
      command) observed_command=$value ;;
    esac
  done < "$lock_dir/owner"
}

owner_elapsed() {
  local now elapsed
  now=$(date +%s)
  elapsed=0
  if [[ "$observed_started" =~ ^[0-9]+$ ]] && (( now >= observed_started )); then
    elapsed=$((now - observed_started))
  fi
  printf '%s' "$elapsed"
}

describe_owner() {
  echo "[INTEGRATION] lock owner host=$observed_host pid=$observed_pid elapsed=$(owner_elapsed)s command=$observed_command" >&2
}

owner_is_stale() {
  [[ "$observed_host" == "$owner_host" ]] || return 1
  [[ "$observed_pid" =~ ^[0-9]+$ ]] || return 1
  if ! kill -0 "$observed_pid" 2>/dev/null; then
    return 0
  fi
  local live_identity
  live_identity=$(ps -o lstart= -p "$observed_pid" 2>/dev/null | sed 's/^[[:space:]]*//' || true)
  [[ "$observed_identity" != "unknown" && -n "$live_identity" && "$live_identity" != "$observed_identity" ]]
}

clean_stale_lock() {
  local stale_dir
  stale_dir="${lock_dir}.stale.${owner_pid}.${owner_started}"
  if mv "$lock_dir" "$stale_dir" 2>/dev/null; then
    rm -f "$stale_dir/owner"
    if ! rmdir "$stale_dir"; then
      echo "[INTEGRATION] refused to remove non-empty stale lock quarantine $stale_dir" >&2
      exit 1
    fi
    echo "[INTEGRATION] cleaned stale lock from host=$observed_host pid=$observed_pid elapsed=$(owner_elapsed)s"
    return 0
  fi
  return 1
}

release_lock() {
  if [[ -n "$image_pull_output_file" ]]; then
    rm -f "$image_pull_output_file"
  fi
  $lock_held || return 0
  local current_token=""
  if [[ -f "$lock_dir/owner" ]]; then
    current_token=$(awk -F= '$1 == "token" {sub(/^token=/, ""); print; exit}' "$lock_dir/owner")
  fi
  if [[ "$current_token" != "$owner_token" ]]; then
    echo "[INTEGRATION] lock ownership changed; refusing to remove $lock_dir" >&2
    return 0
  fi
  rm -f "$lock_dir/owner"
  if ! rmdir "$lock_dir"; then
    echo "[INTEGRATION] lock directory is unexpectedly non-empty: $lock_dir" >&2
  fi
}

image_pull_is_running() {
  local job_pid
  for job_pid in $(jobs -pr); do
    if [[ "$job_pid" == "$image_pull_pid" ]]; then
      return 0
    fi
  done
  return 1
}

terminate_and_reap_image_pull() {
  [[ -n "$image_pull_pid" ]] || return 0

  local owned_pid grace_deadline now
  owned_pid=$image_pull_pid
  if image_pull_is_running; then
    kill -TERM "$owned_pid" 2>/dev/null || true
    grace_deadline=$(($(date +%s) + 1))
    while image_pull_is_running; do
      now=$(date +%s)
      if (( now >= grace_deadline )); then
        break
      fi
      sleep 0.05
    done
    # Bash's running-job table is the ownership authority. Recheck it
    # immediately before escalation so a completed, asynchronously reaped job
    # can never turn this into a signal to a recycled process ID.
    if image_pull_is_running; then
      kill -KILL "$owned_pid" 2>/dev/null || true
    fi
  fi

  set +e
  wait "$owned_pid" 2>/dev/null
  set -e
  image_pull_pid=""
}

run_bounded_image_pull() {
  image_pull_output_file=$(mktemp "${TMPDIR:-/tmp}/semstreams-image-pull.XXXXXX")
  image_pull_timed_out=false

  docker pull "$nats_image" >"$image_pull_output_file" 2>&1 &
  image_pull_pid=$!
  local pull_deadline now pull_status
  pull_deadline=$(($(date +%s) + image_pull_timeout_seconds))

  while image_pull_is_running; do
    now=$(date +%s)
    if (( now >= pull_deadline )); then
      image_pull_timed_out=true
      terminate_and_reap_image_pull
      return 124
    fi
    sleep 0.05
  done

  set +e
  wait "$image_pull_pid"
  pull_status=$?
  set -e
  image_pull_pid=""
  return "$pull_status"
}

acquire_lock() {
  local deadline now
  deadline=$((owner_started + wait_seconds))
  while true; do
    if mkdir "$lock_dir" 2>/dev/null; then
      {
        printf 'host=%s\n' "$owner_host"
        printf 'pid=%s\n' "$owner_pid"
        printf 'started=%s\n' "$owner_started"
        printf 'identity=%s\n' "$owner_identity"
        printf 'token=%s\n' "$owner_token"
        printf 'command=%s\n' "scripts/run-integration-tests.sh"
      } > "$lock_dir/owner"
      lock_held=true
      return 0
    fi

    read_owner
    if owner_is_stale && clean_stale_lock; then
      continue
    fi
    now=$(date +%s)
    if (( wait_seconds == 0 )); then
      echo "[INTEGRATION] host lock is busy and no wait budget was requested: $lock_dir" >&2
      describe_owner
      return 1
    fi
    if (( now >= deadline )); then
      echo "[INTEGRATION] host lock wait budget ${wait_seconds}s exhausted: $lock_dir" >&2
      describe_owner
      return 1
    fi
    sleep 1
  done
}

cleanup_runner() {
  terminate_and_reap_image_pull
  release_lock
}

trap cleanup_runner EXIT
trap 'exit 130' INT TERM

acquire_lock

# Explicitly enable Ryuk in every caller. Test cleanup remains primary; Ryuk is
# crash safety. Task and CI both reach testcontainers through this runner.
export TESTCONTAINERS_RYUK_DISABLED=false

if [[ -n "${EPOCHREALTIME:-}" ]]; then
  clock_mode="epochrealtime"
elif date_probe=$(date +%s%3N 2>/dev/null) && [[ "$date_probe" =~ ^[0-9]{13}$ ]]; then
  clock_mode="date-milliseconds"
else
  clock_mode="integer-seconds"
fi

now_milliseconds() {
  case "$clock_mode" in
    epochrealtime)
      local current seconds fraction
      current=$EPOCHREALTIME
      seconds=${current%.*}
      fraction=${current#*.}000
      printf '%s' "$((10#$seconds * 1000 + 10#${fraction:0:3}))"
      ;;
    date-milliseconds)
      date +%s%3N
      ;;
    *)
      printf '%s' "$(($(date +%s) * 1000))"
      ;;
  esac
}

latency_resolution="millisecond clock"
if [[ "$clock_mode" == "integer-seconds" ]]; then
  latency_resolution="integer-second clock fallback"
fi

docker_info_started=$(now_milliseconds)
if ! docker_info_output=$(docker info 2>&1); then
  docker_info_elapsed=$(($(now_milliseconds) - docker_info_started))
  echo "[INTEGRATION] docker info failed after ${docker_info_elapsed}ms ($latency_resolution):" >&2
  echo "$docker_info_output" >&2
  exit 1
fi
docker_info_elapsed=$(($(now_milliseconds) - docker_info_started))
echo "[INTEGRATION] docker info latency: ${docker_info_elapsed}ms ($latency_resolution)"

readonly nats_image="nats:2.14-alpine"
image_cached=false
if docker image inspect "$nats_image" >/dev/null 2>&1; then
  image_cached=true
fi
if $image_cached && [[ "$refresh_image" == "0" ]]; then
  echo "[INTEGRATION] using cached $nats_image (registry pull skipped)"
else
  image_pull_started=$(now_milliseconds)
  if ! run_bounded_image_pull; then
    image_pull_elapsed=$(($(now_milliseconds) - image_pull_started))
    if $image_pull_timed_out; then
      echo "[INTEGRATION] $nats_image pull timed out after ${image_pull_timeout_seconds}s" >&2
    else
      echo "[INTEGRATION] $nats_image pull failed after ${image_pull_elapsed}ms ($latency_resolution):" >&2
    fi
    sed -n '1,120p' "$image_pull_output_file" >&2
    exit 1
  fi
  image_pull_elapsed=$(($(now_milliseconds) - image_pull_started))
  echo "[INTEGRATION] $nats_image pull latency: ${image_pull_elapsed}ms ($latency_resolution)"
fi
echo "[INTEGRATION] running Docker-backed tests (-race, integration tag, uncapped package parallelism)"

packages=("$@")
if (( ${#packages[@]} == 0 )); then
  packages=(./...)
fi
set +e
go test -race -failfast -tags=integration -timeout=20m -count=1 "${packages[@]}"
status=$?
set -e
if (( status != 0 )); then
  echo "[INTEGRATION] tests failed with status $status" >&2
  exit "$status"
fi
echo "[INTEGRATION] tests complete"
