#!/usr/bin/env bash
set -euo pipefail

evidence=/tmp/semstreams-gh1292-lintfix-evidence.688Kp8
cd "$evidence/copy"
export GOCACHE=/tmp/semstreams-gh1292-model-mutations.TILVxn/gocache

current_file=
backup=
cleanup() {
  if [[ -n "$current_file" ]]; then
    cp "$backup" "$current_file"
  fi
}
trap cleanup EXIT

run_full() {
  go test ./processor/graph-index \
    -run '^(TestPropGraphIndexReconciliation|TestGraphIndexModel)' \
    -count=1 -race -timeout=120s -v \
    -rapid.checks=100 -rapid.seed=1292 -rapid.shrinktime=3s -rapid.nofailfile
}

run_property() {
  go test ./processor/graph-index \
    -run '^TestPropGraphIndexReconciliation$' \
    -count=1 -race -timeout=120s -v \
    -rapid.checks=100 -rapid.seed=1292 -rapid.shrinktime=3s -rapid.nofailfile
}

exercise() {
  local name=$1 file=$2 old=$3 new=$4 selector=${5:-full}
  current_file=$file
  backup="$evidence/$name.bak"
  cp "$file" "$backup"
  local before
  before=$(md5 -q "$file")
  python3 - "$file" "$old" "$new" <<'PY'
from pathlib import Path
import sys
path = Path(sys.argv[1])
old, new = sys.argv[2:]
source = path.read_text()
assert source.count(old) == 1, (path, source.count(old))
path.write_text(source.replace(old, new))
PY
  diff -u "$backup" "$file" > "$evidence/$name.patch" || [[ $? == 1 ]]
  if [[ "$selector" == property ]]; then
    if run_property > "$evidence/$name.mutant.log" 2>&1; then
      echo "$name survived" >&2
      exit 1
    fi
  else
    if run_full > "$evidence/$name.mutant.log" 2>&1; then
      echo "$name survived" >&2
      exit 1
    fi
  fi
  if ! rg -q '^FAIL|--- FAIL:' "$evidence/$name.mutant.log"; then
    echo "$name did not produce a test assertion" >&2
    exit 1
  fi
  cp "$backup" "$file"
  [[ "$(md5 -q "$file")" == "$before" ]]
  current_file=
  if [[ "$selector" == property ]]; then
    run_property > "$evidence/$name.restored.log" 2>&1
  else
    run_full > "$evidence/$name.restored.log" 2>&1
  fi
  printf '%s before=%s after=%s mutant=failed restored=passed\n' "$name" "$before" "$(md5 -q "$file")" >> "$evidence/checksums.txt"
}

exercise stale processor/graph-index/owner_reconcile.go \
  $'\t\tdelErr := bucket.Delete(ctx, key)' \
  $'\t\tdelErr := error(nil) // mutation: omit stale-row deletion'

exercise bootstrap processor/graph-index/watermark.go \
  'if status.Ready || c.initialBuildApplied(status.IndexedRevision) {' \
  'if status.Ready || c.initialEnumerationComplete.Load() { // mutation: latch before work'

exercise failure processor/graph-index/component.go \
  $'\t\tc.markEntityFailed(resolvedID)\n\t\treturn errs.WrapTransient(writeErr' \
  $'\t\t// mutation: suppress required-write failure tracking\n\t\treturn errs.WrapTransient(writeErr'

exercise watermark processor/graph-index/component.go \
  'c.watermark.Complete(work.entityID, work.completionRevision)' \
  'c.watermark.Complete(work.entityID, c.watermark.Observed()) // mutation: premature high completion'

exercise synthetic processor/graph-index/reconciliation_model_helpers_test.go \
  $'\tif err := result.activation.complete(); err != nil {\n\t\treturn fail("activation", err)\n\t}\n\treturn result' \
  $'\tif err := result.activation.complete(); err != nil {\n\t\treturn fail("activation", err)\n\t}\n\tif len(actions) > 0 { // synthetic model mismatch for shrink/replay\n\t\tghost := f.facts[3]\n\t\tghost.literals[0] = !ghost.literals[0]\n\t\tf.facts[3] = ghost\n\t\tif err := f.parity(); err != nil {\n\t\t\treturn fail("synthetic oracle mismatch", err)\n\t\t}\n\t}\n\treturn result' \
  property

# Reapply the same synthetic mismatch and replay the fixed seed before restoring.
current_file=processor/graph-index/reconciliation_model_helpers_test.go
backup="$evidence/synthetic.bak"
python3 - "$current_file" <<'PY'
from pathlib import Path
import sys
path = Path(sys.argv[1])
source = path.read_text()
old = '\tif err := result.activation.complete(); err != nil {\n\t\treturn fail("activation", err)\n\t}\n\treturn result'
new = '\tif err := result.activation.complete(); err != nil {\n\t\treturn fail("activation", err)\n\t}\n\tif len(actions) > 0 { // synthetic model mismatch for shrink/replay\n\t\tghost := f.facts[3]\n\t\tghost.literals[0] = !ghost.literals[0]\n\t\tf.facts[3] = ghost\n\t\tif err := f.parity(); err != nil {\n\t\t\treturn fail("synthetic oracle mismatch", err)\n\t\t}\n\t}\n\treturn result'
assert source.count(old) == 1
path.write_text(source.replace(old, new))
PY
if run_property > "$evidence/synthetic.replay.log" 2>&1; then
  echo "synthetic replay unexpectedly passed" >&2
  exit 1
fi
cp "$backup" "$current_file"
[[ "$(md5 -q "$current_file")" == "$(md5 -q "$backup")" ]]
current_file=
run_full > "$evidence/synthetic.replay-restored.log" 2>&1

shasum -a 256 \
  processor/graph-index/owner_reconcile.go \
  processor/graph-index/watermark.go \
  processor/graph-index/component.go \
  processor/graph-index/reconciliation_model_helpers_test.go \
  processor/graph-index/reconciliation_model_test.go \
  processor/graph-index/reconciliation_prop_test.go \
  > "$evidence/final-copy-hashes.txt"
