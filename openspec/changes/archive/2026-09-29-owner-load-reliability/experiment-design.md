# Bounded KV listing lifecycle experiment

Status: proposed diagnostic experiment; no production repair authorized by this document.

Inventory checkpoint: inventory.md, SHA-256
`d0872671356ee5505087160054d890384c13ceb19d4364d7fc2745a3b01fd3a1`, base
`e811399e950d4eda4bed9f140a0ff73fa6882001`, independently passed with 164 verified pins.
The accepted inventory remains unchanged. Execution proceeds on the updated claim branch, including merged #1429.

## Decision and alternatives

| Alternative | Evidence gained | Cost or limitation |
|---|---|---|
| Do nothing beyond inventory | Preserves historical uncertainty | Active cancellation/completion remain unmeasured |
| Extend fake-lister tests | Cheap typed cancellation and partial-result proof | Cannot establish native lifecycle |
| Adopt graph-ingest Stop-then-drain immediately | Existing repository precedent | Premature: it drains Updates, while listing adds a Keys producer |
| Run one deterministic native diagnostic matrix | Separates framework return, SDK completion and native drain | Controlled backpressure/process containment; not historical runner conditions |

Recommend the final alternative. Preserve production code, SDK/server selections, default five-second timeout,
error semantics and owner-load workload. No retries or repeated stress runs.

## Question and premises

Once a real filtered listing is active and native producers encounter backpressure, what completes after
cancellation/deadline expiry: the framework call, native producers and connection drain?

Accepted inventory premises: natsclient/kv.go:537–595 applies the default timeout, rejects canceled snapshots and
defers Stop; SDK jetstream/kv.go:1290,1451 has blocking producer sends; graph-ingest/component.go:1307–1323 owns
stop-then-drain of native watcher updates; client_close_integration_test.go:15–105 already observes native drain
status and callback gates. Graph-index spec:227–233 requires genuine framework deadline breaches to remain red.
None establishes this mechanism as the historical cause.

## Allowed files and seams

Add only `natsclient/kv_filter_lifecycle_diagnostic_integration_test.go` and diagnostic design/tasks/evidence
beneath this OpenSpec change. Use production Client.NewKVStore, KVStore.KeysByFilter, Client.Close and the pinned
native SDK. No production hooks, exported APIs, SDK edits, runner changes or graph-index changes.

A private test decorator embeds real jetstream.KeyValue and overrides only ListKeysFiltered. It delegates
creation to the real bucket with the exact received context/filter, then records the native lister and operation
deadline. For injected cases a private lister facade exposes one real native key through a buffered channel and
withholds subsequent delivery. A second Keys invocation establishes that production collection consumed the first
key. Stop delegates to the native lister and separately records entry, return and error.

This is an explicit stalled-consumer fault model. It does not establish that the unchanged harness stalls its collector.

## Cases and causal gates

Use one parent-owned minimal NATS container and a static fixture of 1,024 distinct matching keys, exceeding the
two native 256-entry buffers. Run three child processes sequentially, each with a fresh connection and one active listing.

| Case | Delivery | Trigger | Required framework result |
|---|---|---|---|
| Control | Unmodified native lister | Natural completion | Exact seeded key set; successful close |
| Active cancellation | First key, then withholding | Explicit cancellation after witnessed native blockage | context.Canceled identity; nil result |
| Default deadline | Same withholding | Unchanged production five-second deadline | context.DeadlineExceeded identity; nil result |

For each injected case:

1. Observe native lister acquisition and first-key collection.
2. Obtain a bounded goroutine snapshot showing native ListKeysFiltered forwarding send and watcher callback send
   blocked. Record their goroutine identities and SDK frames; the isolated child contains only this listing.
3. Only after these witnesses, cancel or await the existing deadline. Record the deadline passed by KVStore;
   no earlier operation timeout is added.
4. Observe framework return and native Stop outcome separately.
5. If the framework returned, invoke production Client.Close with fresh finite authority while withholding
   remains active. Observe native drain/closed statuses and close return independently.
6. Release withholding by consuming retained native Keys to closure under bounded cleanup authority. Record
   whether previously witnessed native goroutines disappear.

A closed keys channel does not prove the watcher callback joined. Stop return, connection closure and process
exit are separate events. Missing gates report GATE_NOT_REACHED with last observation/error, never hazard absent.
No sleep establishes causal order; timers contain observation/failure.

## Ownership and containment

The parent owns one container and each child. Install cleanup immediately after acquisition. Each child has one
Wait owner; observe events alongside EOF/unexpected exit. Preserve output before reporting failure.

The child owns its operation goroutine, private channels and connection. Test-owned goroutines must join.
Record native goroutines remaining after cleanup rather than claiming connection closure joined them.

| Containment scope | Bound |
|---|---|
| Shared fixture setup and seeding | 20 seconds |
| Each child experiment | 20 seconds, then at most 10 seconds cleanup |
| Canonical substrate teardown | Independent 15-second client-close and 15-second container-termination budgets |
| Parent test process alarm, including substrate cleanup | 180 seconds |

The nominal maximum is 20 + 3 × (20 + 10) + 30 = 140 seconds. The 180-second alarm leaves 40 seconds for
coordination/reporting. Set a cooperative experiment deadline 140 seconds after parent entry, reserving the final
40 seconds for 30 seconds of substrate teardown plus 10 seconds of reporting/slack. Admit a child only when its
complete 30-second work/cleanup allowance fits before the cooperative deadline.

Stop child admission after a control failure, failed causal gate or unresolved owned work. Attempt bounded cleanup,
preserve evidence and name skipped cases. A child that cannot be terminated/joined leaves ownership unresolved;
never launch its successor. Cleanup gets fresh finite authority within its reserved allowance.

These are diagnostic containment bounds, not product latency budgets. A native producer may be impossible to
release through public KeyLister. Preserve evidence, terminate and join that owned child, label process containment
rather than producer completion, and never wait for the normal 20-minute package alarm.

Keep this diagnostic opt-in and skipped in normal CI. It adds no container to the normal package baseline.
Retaining permanent coverage requires subsequent evidence/cost review.

## Oracles and result categories

Hard diagnostic failures: wrong control set/failed close; partial successful cancellation snapshot or lost error
identity; unobserved required gate; unaccounted test-owned work/child within containment; missing evidence.
Injected cases report lifecycle outcomes without predicting them:

| Outcome | Supported conclusion |
|---|---|
| Framework returns, producers finish, close succeeds | This schedule retained no native work |
| Framework returns, native work remains, close succeeds | Producer completion differs from connection drain; not explanation of historical close timeout |
| Framework returns, native work remains, close times out | Synthetic schedule links cancellation and close failure; historical attribution unproven |
| Framework blocks inside native Stop | Cancellation return failure differs from incident's returned error |
| Native keys close, witnessed watcher remains | Keys closure does not establish watcher completion |
| Gate/control fails | Experiment/environment inconclusive; no repair decision |

Record monotonic elapsed times, case/filter/bucket identity, acquisition, first collection, operation deadline/error,
Stop entry/exit/error, framework return, connection statuses, close outcome, native Keys closure and matching stacks.
Retain source revision, actual SDK/server versions and command.

## Verification and mutation decision

Run once through the canonical runner:

```bash
SEMSTREAMS_KV_LIFECYCLE_DIAGNOSTIC=1 \
  ./scripts/run-integration-tests.sh \
  ./natsclient \
  -run '^TestIntegration_KVFilterLifecycleDiagnostic$' \
  -timeout=180s \
  -v
```

Parent launches the narrowly selected child entry point in the same binary; it must not recursively create fixtures.
Named deterministic cases suffice for these three schedules and lifecycle events; property generation adds no needed
input/history class. Exact seeded-set comparison supplies the independent positive oracle.

The lister chain is mutable delivery facade → private invocation observer → real native lister. The observer's
Stop records entry immediately before delegating to native Stop; it is separate from facade telemetry and stays
unchanged during mutation. After the framework call returns, require exactly one invocation through that observer.
Context-triggered SDK unsubscribe cannot satisfy it because it does not traverse the observer.

Temporarily mutate only the facade's delegation to return without calling the observer. Its count must remain zero
and the invocation assertion must fail even if context cancellation releases the native subscription. Restore the
cp backup, verify checksum, and retain the narrow mutation diff showing the observer stayed unchanged. This establishes
sensitivity to delegated Stop invocation, not native producer completion or join. No production mutation is needed
at this diagnostic stage. Further execution requires changed instrumentation, a rejected mutation or failed gate,
not a desire to obtain a failure.

## Stop and subsequent decision

Stop after matrix, mutation check and evidence review. A production repair needs a demonstrated production/native
violation, identified lifecycle owner and focused regression oracle, without relaxing deadlines. Synthetic
backpressure may establish a native hazard; linking it to #1421 additionally requires matching unchanged-path
evidence or an independently justified framework ownership invariant.

Even a successful synthetic reproduction leaves the original five-second expiry trigger, historical host pressure
and original blocked subscription identity unknown. Seed deadlock, early-exit ownership, Eventually callback bounds
and exact-set gaps remain recorded; they are not extra implementation scope for this experiment.
