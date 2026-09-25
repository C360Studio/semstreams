#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
fixture_root=$(mktemp -d)
trap 'rm -rf "$fixture_root"' EXIT
mkdir -p "$fixture_root/scripts" "$fixture_root/natsclient" "$fixture_root/processor/graph-index" "$fixture_root/bin"
cp "$repo_root/scripts/lint-nats-kv-sdk-pin.sh" "$fixture_root/scripts/"
cp "$repo_root/natsclient/kv_key_contract_test.go" "$fixture_root/natsclient/"
cp "$repo_root/processor/graph-index/nats_pin_test.go" "$fixture_root/processor/graph-index/"

cat > "$fixture_root/bin/go" <<'EOF'
#!/usr/bin/env bash
if [[ "$*" != "list -m -f {{.Path}}|{{.Version}}|{{if .Replace}}{{.Replace.Path}}@{{.Replace.Version}}{{end}} github.com/nats-io/nats.go" ]]; then
  echo "unexpected go invocation: $*" >&2
  exit 2
fi
if [[ "${FAKE_GO_FAIL:-}" == "1" ]]; then
  exit 2
fi
printf '%s\n' "${FAKE_GO_OUTPUT:-}"
EOF
chmod +x "$fixture_root/bin/go"

guard="$fixture_root/scripts/lint-nats-kv-sdk-pin.sh"
run_case() {
  local name=$1 want_success=$2 output=$3
  local actual
  if actual=$(PATH="$fixture_root/bin:$PATH" FAKE_GO_OUTPUT="$output" bash "$guard" 2>&1); then
    if [[ "$want_success" != true ]]; then
      echo "FAIL: $name accepted: $actual" >&2
      exit 1
    fi
  elif [[ "$want_success" == true ]]; then
    echo "FAIL: $name rejected: $actual" >&2
    exit 1
  fi
  echo "OK: $name"
}

pin=$(sed -n 's/^const pinnedNATSGoContractVersion = "\([^"]*\)"$/\1/p' "$fixture_root/natsclient/kv_key_contract_test.go")
run_case "selected direct pin" true "github.com/nats-io/nats.go|${pin}|"
run_case "version drift" false "github.com/nats-io/nats.go|v1.49.0|"
run_case "same-version fork replacement" false "github.com/nats-io/nats.go|${pin}|example.com/fork/nats.go@${pin}"
run_case "local replacement" false "github.com/nats-io/nats.go|${pin}|../nats.go@"
run_case "wrong module path" false "example.com/fork/nats.go|${pin}|"
run_case "empty output" false ""
run_case "malformed output" false "github.com/nats-io/nats.go ${pin}"

if PATH="$fixture_root/bin:$PATH" FAKE_GO_FAIL=1 bash "$guard" >/dev/null 2>&1; then
  echo "FAIL: module lookup failure accepted" >&2
  exit 1
fi
echo "OK: module lookup failure"

graph_pin_file="$fixture_root/processor/graph-index/nats_pin_test.go"
printf 'graphIndexNATSGoPin = "v1.49.0"\n' > "$graph_pin_file"
run_case "graph-index pin drift" false "github.com/nats-io/nats.go|${pin}|"
printf 'package graphindex\n' > "$graph_pin_file"
run_case "absent graph-index pin" false "github.com/nats-io/nats.go|${pin}|"
printf 'graphIndexNATSGoPin = "v1.52"\n' > "$graph_pin_file"
run_case "malformed graph-index pin" false "github.com/nats-io/nats.go|${pin}|"
printf 'graphIndexNATSGoPin = "%s"\ngraphIndexNATSGoPin = "%s"\n' "$pin" "$pin" > "$graph_pin_file"
run_case "multiple graph-index pins" false "github.com/nats-io/nats.go|${pin}|"
cp "$repo_root/processor/graph-index/nats_pin_test.go" "$graph_pin_file"

printf 'package natsclient\n' > "$fixture_root/natsclient/kv_key_contract_test.go"
run_case "absent pin" false "github.com/nats-io/nats.go|${pin}|"
printf 'const pinnedNATSGoContractVersion = "%s"\nconst pinnedNATSGoContractVersion = "%s"\n' "$pin" "$pin" > "$fixture_root/natsclient/kv_key_contract_test.go"
run_case "multiple pins" false "github.com/nats-io/nats.go|${pin}|"
