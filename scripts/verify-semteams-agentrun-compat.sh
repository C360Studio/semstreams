#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 /path/to/semteams" >&2
  exit 2
fi

semteams_source="$(cd "$1" && pwd)"
semstreams_source="$(git rev-parse --show-toplevel)"
compat_root="$(mktemp -d)"
trap 'rm -rf "$compat_root"' EXIT

git clone --quiet --local --no-hardlinks "$semteams_source" "$compat_root/semteams"
(
  cd "$compat_root/semteams"
  go mod edit -replace "github.com/c360studio/semstreams=$semstreams_source"
  cp "$semstreams_source/test/compat/semteams/agentrun_terminal_compat_test.go" test/contract/
  go test test/contract/agentrun_terminal_compat_test.go
)
