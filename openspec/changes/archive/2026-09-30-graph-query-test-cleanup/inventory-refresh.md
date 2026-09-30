# Graph-query cleanup inventory refresh

base: 80fab70ab6e9c2d8ab1afa460f95fea80ca46f20

Change: `graph-query-test-cleanup`
Issue/claim: #1433 / draft PR #1434
Phase: inventory refresh only; independent refresh review pending.

## Checkpoint and scope

The accepted inventory remains byte-identical at SHA-256
`990e720cca7b45f62bd7400674bda662e39390ad7c9b38b65b5092517ab5eaa2`.
Its base is `f9cb87600c2c713247399d005bd0a98dce12c5b6`; its independent review is
`review/inventory-review.md`, preserved by checkpoint `fe424c96`.

This addendum updates intervening dependency facts and manifest totals. It does not replace or re-sweep the
accepted surface inventory, adopter seam inventory, B01–B39 ledger, ordinary-call inventory or helper caller sets.

The coordinator ran `task inventory:verify` and reported all 151 recognized pins valid, with no changed pinned
files. This refresh independently compared literal line-text pins and their source paths: no mismatch and no
changed pinned source file was found.

The worktree HEAD includes main `1b1accf4`, whose commit is
`fix(natsclient): finalize filtered KV watcher delivery (#1435)`.
The other intervening main change is `0121a535`, the tiered E2E assertion change #1427.
The coordinator owns concurrent edits to `proposal.md` and `tasks.md`; both were reread after reconciliation.
No graph-query source changes were present.

## Surface inventory refresh

### 1. Claimed gap and exact identities

Read-only JSON comparison against the accepted inventory base established:

| Measurement | Accepted base | Refreshed base |
|---|---:|---:|
| Global legacy entries | 273 | 273 |
| Reviewed resolutions | 94 | 96 |
| Graph-query legacy identities | 39 | 39 |
| Graph-query resolutions | 0 | 0 |
| Files containing graph-query legacy identities | 5 | 5 |

All 273 legacy records remain equal, including every field of B01–B39.
The existing first 94 resolution records also remain equal.
The only additions are two graph-index observer resolutions, both classified `non-lifecycle-stop`, owned by #1421:

- `test/testinfra/cleanup_baseline.json:5476` — `"identity": "processor/graph-index/owner_filter_load_observer_test.go|ownerLoadAttemptScope|defer|reportCleanup|unresolved callback|unknown|1",`
- `test/testinfra/cleanup_baseline.json:5523` — `"identity": "processor/graph-index/owner_filter_load_observer_test.go|ownerLoadObservationScope|defer|publish|unresolved callback|unknown|1",`

The exact graph-query identities remain the accepted B01–B39 ledger, in the same order, with the same source
paths, enclosing declarations, cleanup origins, callable, receiver, provenance and ordinal.
Per-file counts remain attack 11, batch integration 2, component integration 7, component unit 18,
and summary-late-attach integration 1.

The five source SHA-256 values still match the accepted inventory:

| Source under `processor/graph-query/` | SHA-256 |
|---|---|
| attack_test.go | `469bc121cf909ba6ff73188f27f267e83e99a8738918de35c303ab3a46eafbfe` |
| batch_passthrough_integration_test.go | `5c1e08e289c549de4ed9fdbce6e0f5072f1266da4bf706d9a3d979cacf5e00ca` |
| component_integration_test.go | `cf1dfa39868e276e28f448160738455d9884ad08b0085f9fb2fc92ef83029146` |
| component_test.go | `9c50628590c45705312760cfee7a64197eebdf3fe74590af3a4518bce066655c` |
| summary_bucket_late_attach_integration_test.go | `151ed7fa46d175a98a58a054e3190d139d427b80ae4b235185637a42140869e7` |

The refreshed manifest SHA-256 is
`83e50bdd44f8e5d909acc274898b66e3570342634f5d0a1003508384ebb165ed`.

The eight ordinary Stop sites and the accepted-Start-without-Stop timeout case remain unchanged.
Their original evidence and deliberate lifecycle/abort meanings remain part of the accepted inventory.
Neither unchanged debt nor an unchanged probe is newly certified as compliant.

### 2. Current ownership spellings and the changed dependency

#1435 changes the full-capability `KVStore.KeysByFilter` path and its existing `KeysByPrefix` alias.
The current path constructs a native filtered watcher, collects its initial snapshot, calls Stop once
synchronously, then consumes Updates through closure under a separate five-second terminal window.

- `natsclient/kv.go:526` — `return kv.KeysByFilter(ctx, prefix+">")`
- `natsclient/kv.go:531` — `const filteredWatcherDrainTimeout = 5 * time.Second`
- `natsclient/kv.go:550` — `watcher, constructErr := kv.bucket.WatchFiltered(ctx, []string{pattern}, jetstream.IgnoreDeletes(), jetstream.MetaOnly())`
- `natsclient/kv.go:599` — `stopErr := watcher.Stop()`
- `natsclient/kv.go:600` — `terminalCtx, cancelTerminal := context.WithTimeout(context.WithoutCancel(ctx), filteredWatcherDrainTimeout)`
- `natsclient/kv.go:607` — `case _, ok := <-watcher.Updates():`
- `natsclient/kv.go:610` — `cleanupErr = errFilteredWatcherDeliveryIncomplete`
- `natsclient/kv.go:617` — `if err := errors.Join(primaryErr, cleanupErr); err != nil {`

Cancellation and errors do not authorize successful partial keys. With a live context, direct
`ErrNoKeysFound` without a watcher retains `(nil, nil)` compatibility.
The terminal window begins after contextless Stop returns; it does not bound that Stop or total call duration.

The minimal-reader path remains distinct:

- `natsclient/kv.go:629` — `lister, err := kv.ListKeysFiltered(ctx, pattern)`
- `natsclient/kv.go:649` — `defer func() { _ = lister.Stop() }()`

The complete scope statement is at specification lines 285–286 in openspec/specs/nats-kv-keys/spec.md:
minimal-reader FilteredKeys and CatalogReader method sets remain unchanged. This prose reference is not counted
as a literal line-text pin because that source statement contains inline backticks.

Default-build typed queries found four production direct KeysByFilter call sites, its prefix alias, and test
callers. Production sites are graph-index deletion, reconciliation and predicate queries.
KeysByPrefix references additionally reach graph-index name/incoming operations and graph-ingest prefix access:

- `processor/graph-index/component.go:2099` — `set.keys, listErr = set.bucket.KeysByFilter(ctx, set.filter)`
- `processor/graph-index/owner_reconcile.go:47` — `existingKeys, err := bucket.KeysByFilter(ctx, ownerFilter)`
- `processor/graph-index/query.go:428` — `keys, err := c.predicateBucket.KeysByFilter(ctx, filter)`
- `processor/graph-index/query.go:567` — `keys, err := c.predicateBucket.KeysByFilter(ctx, filter)`
- `processor/graph-ingest/component.go:462` — `return a.component.entityBucket.KeysByPrefix(ctx, prefix+".")`
- `processor/graph-ingest/query.go:294` — `keys, err := c.entityBucket.KeysByPrefix(ctx, prefixDot)`

These results establish adjacent production exposure, not isolation of graph-query from every transitive request.
The queries used the default build selection and do not claim a fresh all-tag caller census.

The graph-ingest test bucket now implements WatchFiltered using its existing matcher/fault hook and a watcher
whose Stop closes Updates. Its private cleanup owner did not change.

- `processor/graph-ingest/component_test.go:181` — `return &mockFilteredWatcher{updates: updates}, nil`
- `processor/graph-ingest/component_test.go:194` — `func (w *mockFilteredWatcher) Stop() error {`

Graph-query's inventoried community WatchAll and summary-view ownership are unchanged:

- `processor/graph-query/community_cache.go:69` — `watcher, err := reader.WatchAll(ctx)`
- `processor/graph-query/community_cache.go:73` — `defer watcher.Stop()`
- `processor/graph-query/summary_view.go:184` — `view.Stop()`
- `pkg/graphview/view.go:302` — `v.wg.Wait()`

Its responder drain, cancellation, runtime wait and synchronous contextless Close sequence are unchanged:

- `processor/graph-query/component.go:661` — `if err := sub.Drain(ctx); err != nil {`
- `processor/graph-query/component.go:672` — `c.cancel()`
- `processor/graph-query/component.go:676` — `case <-c.runtimeDone:`
- `processor/graph-query/component.go:681` — `case <-ctx.Done():`
- `processor/graph-query/component.go:686` — `if err := c.llmClient.Close(); err != nil {`
- `processor/graph-query/component.go:693` — `if err := c.answerSynthesizer.Close(); err != nil {`

Consequently, the accepted limitations remain: finite Stop authority does not interrupt a contextless operation,
a returned error does not establish joined children, and ordinary fixture finalization has no generic rejoin right.

### 3. Adjacent claims and governing constraints

The current test-cleanup-policy, component-lifecycle, runtime-context-ownership and graph-query specifications
are unchanged from the accepted base and were read fully. The architect contract, project purpose/product boundary,
shared protocol and testing policy were also read.

New adjacent requirements are:

- `openspec/specs/nats-kv-keys/spec.md:259` — `### Requirement: Filtered KVStore listings finalize native watcher delivery`
- `openspec/specs/nats-kv-keys/spec.md:274` — `SemStreams SHALL invoke Stop once synchronously for a returned watcher, then consume Updates through closure using`
- `openspec/specs/nats-kv-keys/spec.md:279` — `Updates closure SHALL establish a delivery boundary only, not native goroutine joining or successful consumer`
- `openspec/specs/graph-index/spec.md:666` — `### Requirement: Predicate-forward measurements retain bounded phase evidence`
- `openspec/specs/graph-index/spec.md:689` — `Operation duration SHALL include synchronous Stop and terminal drain and SHALL be recorded before diagnostic`

The graph-index observer requirement distinguishes diagnostic callback completion, delegated Stop return,
operation return and native delivery closure. Its two new resolutions do not approve graph-query cleanup paths.

The #1435 native test directly observes residual Updates after the public operation returns:

- `natsclient/kv_watcher_ownership_integration_test.go:46` — `func TestIntegration_KVStoreFilteredNativeDeliveryClosure(t *testing.T) {`
- `natsclient/kv_watcher_ownership_integration_test.go:148` — `case _, ok := <-updates:`
- `natsclient/kv_watcher_ownership_integration_test.go:150` — `t.Error("native Updates retained an entry after public return")`
- `natsclient/kv_watcher_ownership_integration_test.go:153` — `t.Error("native Updates remained open after public return")`

The archived native review reports a passing exercised delivery-completion repair with explicit limits.
This refresh did not rerun that evidence or verify its retained log packet.
It does not establish graph-query cleanup success, whole-SDK joining, consumer deletion or historical CI causation.

#1427 changes E2E assertions and their documentation. The intervening diff contains no graph-query fixture,
private fixture-owner, graphview, NATS client-cleanup or governing lifecycle/cleanup-spec change.

The coordinator reports the prior hold released after #1435 merged with the owner's PR-specific waiver:
https://github.com/C360Studio/semstreams/pull/1435#issuecomment-5907843099
This refresh did not independently refetch GitHub status. #1421 remains open in the supplied coordination record;
neither the #1432 nor #1435 waiver transfers to #1434.

### 4. Consumer at birth

No new exported symbol, port, subject, bucket, configuration field or framework primitive is proposed by this
refresh. Its consumers remain the same graph-query fixture authors and review/guard workflow.
The reconciled proposal and tasks preserve test-only scope.

### 5. Problem shape and collision applicability

The accepted shape remains lexical fixture ownership across fallible setup, concrete terminal attempts and
substrate teardown. Existing owners in component, graph-ingest and rule retain identical source.

The accepted collision table remains applicable. The changed dependency adds an existing full-KVStore
delivery-finalization responsibility to the observed lifecycle/recovery column; it does not transfer graph-query
fixture ownership, introduce a test owner, or prove the component's independent joins.

No durable, communication or production coordination primitive is proposed here.
No establishing-side adoption sweep or decision-skill trigger is introduced by this inventory refresh.

## Adopter seam refresh

For a component composer, the accepted inventory's Start/Stop knowledge burden remains unchanged:
controlled Stop preserves live accepted Start authority; abort results can be nonnil; failed-Start retry differs
from running Stop; finite context supply alone does not prove joined work.

For a caller of the already-merged full KVStore filtered path:

1. Required knowledge remains the existing operation context and filter. There is no new cleanup-budget knob or
   caller-owned watcher.
2. Doing nothing additional uses production-owned synchronous Stop and terminal Updates drain. A failed cleanup
   produces an error and nil keys; a successful returned-watcher listing requires snapshot completion and closure.
3. Failure is observable through the existing runtime error return. The five-second post-Stop window is not a
   whole-call or goroutine-join guarantee.
4. The framework now observes native delivery closure itself. This refresh proposes no further caller obligation.

For graph-query test authors, the accepted ownership gap remains unchanged: terminal protection is installed after
fallible setup at the 39 sites, results are usually discarded, and ordinary probes/omitted Stop remain separate
from the guard baseline. No replacement helper or implementation choice is selected here.

No sister repository was inspected. This batch proposes no outward-facing behavior change.
Any later expansion into production behavior reopens that boundary.

## Searches and measurements

All commands ran read-only in the claim worktree. No tests, native runs, guard execution, mutation experiment,
artifact write or Git state mutation was performed by this architect.

Change discovery:

```sh
git status --short
git rev-parse HEAD
rg --files openspec/changes/graph-query-test-cleanup
git diff --name-status f9cb8760..HEAD
git log --oneline f9cb8760..HEAD
git diff f9cb8760..HEAD -- natsclient/kv.go test/testinfra/cleanup_baseline.json \
  openspec/specs/nats-kv-keys/spec.md openspec/specs/graph-index/spec.md \
  processor/graph-ingest/component_test.go
git diff f9cb8760..HEAD -- docs/contributing/02-e2e-tests.md
git diff --name-only f9cb8760..HEAD -- processor/graph-query \
  component/lifecycle_test_suite.go processor/graph-ingest/test_owner_support_test.go \
  processor/rule/test_owner_support_test.go natsclient/test_client.go natsclient/client.go \
  pkg/graphview/view.go .agents/contracts/semstreams-architect.md docs/contributing/01-testing.md \
  openspec/specs/test-cleanup-policy/spec.md openspec/specs/component-lifecycle/spec.md \
  openspec/specs/runtime-context-ownership/spec.md openspec/specs/graph-query/spec.md
```

The final diff command returned no paths. Required documents were read fully; changed source, spec additions and
archived evidence limits were read using located ranges. The full changed-file output was large; no omission or
absence claim relies on its displayed truncation.

Typed structural queries:

```sh
gopls call_hierarchy natsclient/kv.go:543:20
gopls references natsclient/kv.go:523:20
```

Both succeeded with authorized Go-cache access. The initial call at `541:20` failed on sandboxed cache access
and used a comment location; it establishes no structural fact.

Additional searches:

```sh
git grep -n -E 'func Test|func \(.*WatchFiltered|func \(.*Stop|spec:' \
  -- natsclient/kv_watcher_ownership_test.go
```

An attempted search using unquoted `natsclient/catalog*.go` failed in zsh before Git ran.
No absence claim relies on it.

Read-only Python loaded both manifest revisions, compared the full entry arrays and existing resolution prefix,
selected every record containing `processor/graph-query/`, printed all 39 identities, counted source paths,
and computed the manifest/inventory/five-source SHA-256 values.
A separate literal-pin comparison checked the accepted inventory against current source lines and the changed-file
set. Exact identity equality can be reproduced without executing Go:

```sh
python3 - <<'PY'
import json
import subprocess
from collections import Counter
from pathlib import Path

base = "f9cb87600c2c713247399d005bd0a98dce12c5b6"
path = "test/testinfra/cleanup_baseline.json"
old = json.loads(subprocess.check_output(["git", "show", base + ":" + path]))
new = json.loads(Path(path).read_text())
select = lambda data: [
    row for row in data["entries"]
    if "processor/graph-query/" in json.dumps(row)
]
assert old["entries"] == new["entries"]
assert new["resolutions"][:len(old["resolutions"])] == old["resolutions"]
assert select(old) == select(new)
print(len(new["entries"]), len(new["resolutions"]), len(select(new)))
print(Counter(row["identity"].split("|")[0] for row in select(new)))
for ordinal, row in enumerate(select(new), 1):
    print(f"B{ordinal:02}", row["identity"])
PY
```

Observed totals: `273 96 39`.

## Evidence limits and review boundary

This refresh establishes preserved source/identity evidence plus the changed shared dependency's current behavior.
It establishes no executed graph-query cleanup, setup-failure behavior, race freedom, complete join or merge readiness.

The accepted inventory's eight ordinary Stop probes, accepted-Start-without-Stop case, setup escape paths,
unstarted helper boundaries, mock/native distinction and contextless-operation limits remain explicit.

The next boundary is independent review of this exact materialized addendum together with the unchanged accepted
inventory. Content hashing and checkpointing belong to the coordinator.
Options, target state, artifact deltas and implementation tasks remain withheld until refreshed INVENTORY PASS.
Binding scope decisions remain with the owner.
