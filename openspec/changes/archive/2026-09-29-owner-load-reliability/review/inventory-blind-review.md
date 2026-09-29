# Independent blind inventory record

base: e811399e950d4eda4bed9f140a0ff73fa6882001

Reviewer: `semstreams-reviewer`, task `/root/gh1421_inventory_review`, 2026-09-29.
The reviewer completed this enumeration before receiving the architect inventory or conclusions. This record is
materialized by the coordinator from the reviewer handoff. No tests or mutations ran; no inventory verdict or
root-cause attribution has yet been made. Line references below describe this base.

## Original incident

The raw job log SHA-256 is `8ac79b8a8359e1d441248dd87f3c12a1cd19aa7bf5728b81ba4b6d33dd553daa`, retained in
`../evidence/incident-and-issue-snapshots.zip`. Lines 3761–3788 show the 28.55-second harness failure, workers-4
12.22-second case, normal preceding distributions, convergence KeysByFilter error at test line 484, then a
14.999987802-second drain failure. Lines 3778–3779 show all 15 results consumed and per-store consumer
baseline/high/after 0/1/0, 0/1/0, 0/2/0 (aggregate 0/3/0) before convergence.

In `processor/graph-index/owner_filter_load_integration_test.go`, churn joins at 423, sampler stops/joins at 428–429,
dispatcher cancels/joins at 434–439, consumers are checked at 448–456, restore Put runs at 469–478 and the failed
listing at 483–485. Early-abort worker cleanup debt is therefore not established as the cause of this occurrence.
No server log, stack, pending-subscription state or exact listing progress accompanies the incident. Runner stall
and SDK deadlock remain hypotheses.

## Production and native SDK path

- Fixture stores use Client.NewKVStore defaults at owner harness 194–199. `natsclient/kv.go:35–43` sets 5 seconds;
  MaxRetries 10 is explicitly CAS-only. `applyTimeout` is at 68–72. KeysByFilter 537–555 invokes SDK
  ListKeysFiltered once. `collectFilteredKeys` 582–600 discards partial results on context error, ignores the Stop
  error at 583 and does not join the native producer. FilteredKeys 560–575 shares the collector. MaxRetries does
  not cause listings to retry.
- Direct production KeysByFilter callers include graph-index/owner_reconcile.go:47, component.go:2099,
  query.go:428,567 and the prefix delegate kv.go:525. Reconciliation propagates transient errors before deletion
  at owner_reconcile.go:52–55; query propagates at 429–430 and 568–569. FilteredKeys consumers include
  graph/clustering/storage.go:244, graph/inference/storage.go:312,560, agentic-loop/trajectory_reader.go:80,
  agentic-tools/executors/register_graph_query.go:111, graph-clustering/anomaly.go:123, component.go:2189,
  query.go:321, graph-index-temporal/query.go:75 and E2E client/nats.go:947. These are blast-radius evidence,
  not authorization to change every caller.
- go.mod:12 pins nats.go v1.52.0. Local module `jetstream/kv.go` SHA-256 is
  `fdd64fbd20753bd658aafbc299278deef3fbd44b6ffea0c476c506633d464c4a`.
  ListKeysFiltered 1432–1458 wraps WatchFiltered with IgnoreDeletes/MetaOnly and a separate 256-entry key channel
  at 1439. The conversion goroutine unconditionally sends at 1451 before checking ctx at 1452. It defers
  watcher.Stop at 1443 and closes the key channel at 1442.
- WatchFiltered creates another 256-entry buffer at 1246–1247. Its callback holds the watcher mutex from 1278
  across unconditional update and marker sends at 1290 and 1299. The closed callback locks that mutex at
  1335–1339. watcher.Stop 1206–1210 unsubscribes; KeyLister.Stop 1465–1466 forwards. Native unsubscribe
  in nats.go:5181–5203 removes the subscription and may delete the consumer; it does not join the producer.
  Native drain at 6077–6172 waits subscriptions then flush; checkDrained 5208–5255 waits pending callbacks;
  waitForMsgs 3579–3585 accounts the callback only after it returns. This is a plausible cancellation/backpressure
  hazard, not a proven attribution of the recorded incident.

## Existing shape and test evidence

`processor/graph-ingest/component.go:1307–1323` already owns a related stop-then-drain watcher shape:
`stopEntityStateGuardWatcher` stops and drains Updates to closure because an unread callback can hold the mutex
and wedge its async dispatcher. Callers are at 1280 and 1297. This private, contextless function is not automatically
an accepted bounded reusable mechanism. Draining only KeyLister.Keys may not drain watcher.Updates after the
converter exits on cancellation; a design must trace both native buffers.

`natsclient/kv_filter_test.go:40–73` covers success and pre-canceled fake channels. Fake Stop at 20–23 only sets a
boolean and provides no active buffered-producer/native-callback join proof. Owner harness cancellation at 715–742
is also pre-canceled. `natsclient/client_close_integration_test.go:15–106` is existing native-drain proof using
DRAINING_SUBS, closed state and a gated in-flight callback.

## Harness ownership and observations

- NewTestClient 146–152 owns the substrate. Execution context at 153 is Background with a 15-minute deadline.
- Seeding creates jobs buffer 256 and errors buffer 32 at 259–260; 32 workers at 262 each send every Put failure
  at 268, but error consumption starts after job submission and wg.Wait at 277–279. More than 32 failures can
  block workers and admission independently of context. This is a source-measured latent deadlock shape.
- Concurrent cancellation at 323/344 lacks deferred owners. Fatal Submit at 397, result at 412–413 or churn at
  426 can bypass cancellation/join at 434. Results 410–411 receive without a bound; comment 404 acknowledges a
  lost result can wait for package timeout. Churn sends at most one error per worker; sampler has one buffered error.
- Info Eventually at 450–453 uses the parent IO context and drops the last error; its nominal five-second polling
  does not narrow the callback IO authority.
- Convergence 485 and measured filters 530 compare lengths only. The sibling predicate-layout smoke harness at
  337–373 and 435–458 compares concrete expected key sets. Exact-set prose overclaims this harness by itself.
- Resource scrape, subscriptions and slow-consumer observations at 488–495 do not execute after the failed listing.

## Substrate and runner

NewTestClient t.Cleanup at test_client.go:862–867 reaches Terminate 932–938 (sync.Once), then
cleanupTestInfrastructure 295–323. Client.Close and container.Terminate get separate 15-second contexts; container
termination proceeds despite a close error. WithTestTimeout is not the cleanup setting; the global constant at
36 supplies it. Client.Close 578–620 marks closed, stops monitors and reaches native drain 680–729. Deadline
narrows the outer drain at 685–689; expiry is visible and force-closes at 713–722. Hard close is not callback-join
proof. Testing policy line 498 describes 10-second cleanup, stale beside the current 15-second source.

CI `.github/workflows/ci.yml:138–156` has an outer 25-minute budget. Canonical runner
`scripts/run-integration-tests.sh:341` uses race, failfast, integration, 20-minute package timeout, count 1 and p 2
with the host lock. Evidence appends at 332–333 but prints only after aggregate command at 344–346. The package
ceiling is transitional (testing policy 303–306). #1054's historical uncapped-parallelism claim is superseded by p 2.

## Authority and scope

#1284 comment 5635299542 explicitly keeps a genuine five-second breach red. Graph-index spec 174–205 and 227–233
requires the framework ceiling, typed failure and no partial successful snapshot; supervised activation is separate
from CI regression guard. #1287 owns percentile/repetition re-derivation. Testing policy 441–446 owns per-resource
cleanup, 454–464 owns explicit synchronization and diagnostic narrow polling, and 482–499 owns test IO authority
and detached bounded finalization.

A seed/early-fatal cleanup repair alone cannot honestly claim to repair #1421's observed convergence failure.
Reproduction or instrumentation of that path is still owed. Retry or deadline inflation would change the earlier
ruling; neither is accepted by this record.

## Searches and identities

The reviewer used gopls references at kv.go:537:20 (returned only the kv.go:525 delegate, incomplete) and then
corroborated with `git grep -n 'KeysByFilter(' -- '*.go'`. gopls at owner harness 288:6 returned the caller at 172.
Literal searches covered FilteredKeys, WatchFiltered/Updates, stopEntityStateGuardWatcher, drain updates,
cleanup/Close/Drain, runner flags and spec clauses. Two unmatched shell globs (`natsclient/kv_filtered*.go`,
`natsclient/drain*`) were replaced with quoted git pathspecs and concrete files; no zero-hit completeness claim
rests on them.

Harness SHA-256: `bbf6b3c82cf01a23822bdbbe7d2ab95e2797847735ad22a8764373cd533a4ee4`.
Framework kv.go SHA-256: `9b9b4c40aa6f65b66bdcb68a7db881e0fdfb4bb8d46f8b2af4d42d86d1696725`.

Awaiting the architect's exact inventory and hash for reconciliation. No INVENTORY PASS yet.
