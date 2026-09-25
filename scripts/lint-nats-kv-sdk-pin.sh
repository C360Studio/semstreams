#!/usr/bin/env bash
set -euo pipefail

# Check the two existing SDK evidence pins against the selected module graph
# before test binaries start. Go test binaries omit dependency entries from
# build metadata, and spawning `go list` inside tests can block behind
# concurrent module work.
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

kv_pin=$(sed -n 's/^const pinnedNATSGoContractVersion = "\(v[0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*\)"$/\1/p' natsclient/kv_key_contract_test.go)
if [[ ! "$kv_pin" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "FAIL: expected exactly one literal pinnedNATSGoContractVersion in natsclient/kv_key_contract_test.go" >&2
  exit 1
fi
graph_index_pin=$(sed -n 's/^[[:space:]]*graphIndexNATSGoPin[[:space:]]*=[[:space:]]*"\(v[0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*\)"$/\1/p' processor/graph-index/nats_pin_test.go)
if [[ ! "$graph_index_pin" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "FAIL: expected exactly one literal graphIndexNATSGoPin in processor/graph-index/nats_pin_test.go" >&2
  exit 1
fi

if ! resolved=$(go list -m -f '{{.Path}}|{{.Version}}|{{if .Replace}}{{.Replace.Path}}@{{.Replace.Version}}{{end}}' github.com/nats-io/nats.go); then
  echo "FAIL: could not resolve the selected nats.go module" >&2
  exit 1
fi

kv_expected="github.com/nats-io/nats.go|${kv_pin}|"
graph_index_expected="github.com/nats-io/nats.go|${graph_index_pin}|"
if [[ "$resolved" != "$kv_expected" ]]; then
  echo "FAIL: selected nats.go module = '$resolved', KV contract matrix requires '$kv_expected' (no replacement)" >&2
  exit 1
fi
if [[ "$resolved" != "$graph_index_expected" ]]; then
  echo "FAIL: selected nats.go module = '$resolved', graph-index evidence requires '$graph_index_expected' (no replacement)" >&2
  exit 1
fi

echo "OK: selected nats.go module matches KV and graph-index pins $kv_pin without replacement"
