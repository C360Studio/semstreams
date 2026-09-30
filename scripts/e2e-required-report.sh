#!/usr/bin/env bash
# Serialization helpers for the existing Task-owned E2E lifecycle. Callers
# still execute every command, comparison, child and cleanup in their Task body.

e2e_report_begin() {
  local selection=$1 profiles=$2 invocation=$3 initialized
  E2E_REPORT_SELECTION=$selection
  E2E_REPORT_PROFILES=$profiles
  E2E_REPORT_DIR=${E2E_REPORT_DIR:-"$PWD/test/e2e/results"}
  E2E_REPORT_BIN="$PWD/cmd/e2e/e2e"
  mkdir -p "$E2E_REPORT_DIR" || return
  E2E_REPORT_DIR=$(cd "$E2E_REPORT_DIR" && pwd -P) || return
  E2E_REPORT_FILES='[]'
  E2E_REPORT_APPS='[]'
  initialized=$("$E2E_REPORT_BIN" --report-init --report-selection "$selection" \
    --output-dir "$E2E_REPORT_DIR" \
    --report-argv-json "$(jq -nc --arg target "$invocation" '["task",$target]')") || return
  E2E_REPORT_RUN_ID=$(printf '%s' "$initialized" | jq -er '.run_id') || return
  E2E_REPORT_RUN_PATH=$(printf '%s' "$initialized" | jq -er '.path') || return
  E2E_REPORT_LOG="$E2E_REPORT_DIR/e2e-task-$E2E_REPORT_RUN_ID.log"
  : > "$E2E_REPORT_LOG" || return
}

e2e_report_add_file() {
  local role=$1 path=$2
  [ -f "$path" ] || return 1
  path=$(cd "$(dirname "$path")" && pwd -P)/$(basename "$path") || return
  E2E_REPORT_FILES=$(printf '%s' "$E2E_REPORT_FILES" |
    jq -c --arg role "$role" --arg path "$path" '. + [{role:$role,path:$path}]') || return
}

e2e_report_capture_app() {
  local name=$1 container=$2 image_id image_digest created binary_path
  image_id=$(docker inspect "$container" | jq -er '.[0].Image') || return
  created=$(docker image inspect "$image_id" | jq -er '.[0].Created') || return
  image_digest=$(docker image inspect "$image_id" |
    jq -r '.[0].RepoDigests[0] // "unavailable: locally built image has no registry digest"') || return
  binary_path="$E2E_REPORT_DIR/e2e-app-$E2E_REPORT_RUN_ID-$name.bin"
  docker cp "$container:/app/semstreams" "$binary_path" || return
  E2E_REPORT_APPS=$(printf '%s' "$E2E_REPORT_APPS" |
    jq -c --arg name "$name" --arg image_id "$image_id" --arg image_digest "$image_digest" \
      --arg binary_path "$binary_path" --arg build "image $image_id created $created" \
      '. + [{name:$name,image_id:$image_id,image_digest:$image_digest,binary_path:$binary_path,build:$build}]') || return
}

e2e_report_write_input() {
  local path=$1 selection=$2 parent_id=$3 parent_slot=$4 files=${5:-$E2E_REPORT_FILES}
  jq -nc --arg output_dir "$E2E_REPORT_DIR" --arg selection "$selection" \
    --arg parent_id "$parent_id" --arg parent_member_id "$parent_slot" \
    --arg profiles "$E2E_REPORT_PROFILES" --argjson files "$files" \
    --argjson app_phases "$E2E_REPORT_APPS" \
    '{output_dir:$output_dir,selection:$selection,parent_id:$parent_id,parent_member_id:$parent_member_id,profiles:$profiles,files:$files,app_phases:$app_phases}' \
    > "$path"
}

e2e_report_child_input() {
  local slot=$1 selection=$2 path="$E2E_REPORT_DIR/e2e-child-input-$E2E_REPORT_RUN_ID-$slot.json"
  e2e_report_write_input "$path" "$selection" "$E2E_REPORT_RUN_ID" "$slot" || return
  printf '%s\n' "$path"
}

e2e_report_record() {
  local member=$1 status=$2 reason=$3 expected=$4 observed=$5 input
  input="$E2E_REPORT_DIR/e2e-observation-$E2E_REPORT_RUN_ID-$member.json"
  jq -nc --arg id "$member" --arg run_id "$E2E_REPORT_RUN_ID" \
    --arg member_id "$member" --arg status "$status" --arg reason "$reason" \
    --arg expected "$expected" --arg observed "$observed" \
    '{id:$id,run_id:$run_id,member_id:$member_id,status:$status,reason:$reason,evidence:{expected:$expected,observed:$observed}}' \
    > "$input" || return
  "$E2E_REPORT_BIN" --report-record --report-run-path "$E2E_REPORT_RUN_PATH" --report-input "$input"
}

e2e_report_child() {
  local slot=$1 output=$2 exit_code=$3 child_path
  child_path=$(sed -n 's/^E2E_RESULT_PATH=//p' "$output" | tail -n 1) || return
  "$E2E_REPORT_BIN" --report-child --report-run-path "$E2E_REPORT_RUN_PATH" \
    --report-member "$slot" --report-child-path "$child_path" \
    --report-child-exit "$exit_code" --report-log-path "$output"
}

e2e_report_finalize() {
  local command_exit=$1 cleanup_exit=$2 input files result status
  input="$E2E_REPORT_DIR/e2e-task-input-$E2E_REPORT_RUN_ID.json"
  files=$(printf '%s' "$E2E_REPORT_FILES" |
    jq -c --arg path "$E2E_REPORT_LOG" '. + [{role:"task_log",path:$path}]') || files=''
  if [ -n "$files" ]; then
    e2e_report_write_input "$input" "$E2E_REPORT_SELECTION" "" "" "$files" || true
  fi
  result=$("$E2E_REPORT_BIN" --report-finalize --report-run-path "$E2E_REPORT_RUN_PATH" \
    --report-command-exit "$command_exit" --report-cleanup-exit "$cleanup_exit" \
    --report-manifest-input "$input")
  status=$?
  if [ -n "$result" ]; then
    printf 'E2E_TASK_RESULT_PATH=%s\n' "$(printf '%s\n' "$result" | tail -n 1)"
  fi
  return "$status"
}
