# Shared KV filtered-listing abort repair

base: 81a4deb8
Status: architect draft for independent design review. No implementation authorized by this draft.
Accepted repair inventory SHA-256: f94d303cd3adc300a0586abd63528cfd672f5734402640187a1ad40bfbfaea1b.
Author: semstreams-architect gh1433_inventory_design; root materialization.

## Decision, scope and explicit costs

Recommend a private WatchFiltered implementation for KVStore.KeysByFilter and its KeysByPrefix alias. Consume native
Updates directly, request Stop synchronously, and observe Updates closure with a finite terminal drain. Preserve the
existing operation timeout/default5s, filter semantics, typed cancellation, nil failed results, minimal-reader API and
workload assertions. This repairs the observed terminal delivery defect; #1421's initial stall, hosted15s drain and
historical attribution remain unresolved.

Acceptance explicitly includes two behavior changes: successful completion requires snapshot completion plus Updates
closure, and cleanup errors are returned; after synchronous Stop returns a separate private5s drain window applies.
That window is containment, not a higher operation deadline or performance allowance. Whole public-call duration
remains measured; no CI/percentile/deadline/fixture assertion is relaxed.

| Option | Benefit | Cost/limit |
|---|---|---|
| Do nothing | No implementation cost | Observed blocked callback remains. |
| Keep KeyLister and drain Keys | Small apparent change/minimal interface unchanged | Cannot establish watcher completion: forwarder can exit independently of Updates. |
| Copy ingest Stop/drain | Existing callback-backpressure shape | Ignored Stop errors and unbounded drain are unsuitable unchanged. |
| Upgrade/patch SDK | Could address all native listing callers | No verified applicable fix established; fork/replacement needs different dependency/ownership decision. |
| Private full-capability WatchFiltered | Removes measured forwarding layer; exposes delivery closure | Full adapters/mocks and observer need reconciliation; minimal/unfiltered paths remain limited; Stop remains contextless. |

No optional capability detection with silent KeyLister fallback: KVStore already requires full jetstream.KeyValue.
WatchFiltered failure must remain visible. No new public symbol, adopter knob, root context, stored context or detached
continuing work. Applied kv-or-stream: finite current-state KV read, no work queue/new communication. Applied
orchestration-check: one operation's mechanics, no workflow/durable coordinator.

## Evidence and production behavior

Inventory pins: kv.go48 full native bucket;525 prefix alias;561 minimal-reader contract; nativekv1433 uses WatchFiltered
with IgnoreDeletes and MetaOnly;1452 forwarding can end independently of callback1290; Updates closure1338 follows
closed handler after synchronous callback delivery. Ingest component1315/1317 is precedent, not approved unbounded code.
The native counterexample is accepted; its overall PASS is not regression evidence.

Keep applyTimeout unchanged. Call:

```go
WatchFiltered(ctx, []string{pattern}, jetstream.IgnoreDeletes(), jetstream.MetaOnly())
```

Use a fresh filter slice because SDK mutates it internally. Add no grammar/encoding/sorting/deduplication/retry changes.
Consume exact native Updates synchronously: non-nil entry contributes its key; nil entry completes initial snapshot
only after checking operation context. Cancellation gives nil keys/original typed error. Closed Updates before marker
is an incomplete-snapshot failure unless cancellation supplies the primary error. When construction returns no watcher,
first check the operation context: a non-nil ctx.Err() takes precedence and returns nil keys with that typed error.
Otherwise, only the existing direct err == jetstream.ErrNoKeysFound case returns (nil, nil). Other constructor errors
retain wrapping; (nil, nil) from the constructor is an invalid missing-watcher result. No watcher means no Stop or drain.
Reject nil context at the edited public boundary; never substitute Background.

Record primary collection outcome before finalization. Later cancellation during cleanup does not retroactively
relabel a completed snapshot as an earlier collection timeout, consistent with existing deferred-Stop outcome handling.

For each returned watcher:
1. Invoke Stop synchronously exactly once from SemStreams owner.
2. After Stop returns derive context.WithTimeout(context.WithoutCancel(operationCtx), 5*time.Second).
3. Discard Updates until closure or terminal timeout; cancel terminal context before returning.

One private constant defines the drain interval, independent of mutable KVOptions.Timeout, so operation-timeout
zero does not remove terminal containment. No Stop goroutine, relay, drain worker or background continuation.
The terminal context cannot preempt contextless Stop; SDK unsubscribe/deletion latency remains outside this bound.
Total call may include collection, Stop and up to5s post-Stop drain; do not promise total5s return.

Except for the live-context, no-watcher ErrNoKeysFound compatibility case above, successful keys require snapshot
completion and Updates closure. A returned watcher never qualifies for that exception. Every failure returns nil keys. Preserve
primary and cleanup errors via wrapping/joining, including errors.Is(Canceled/DeadlineExceeded). Drain timeout says
watcher delivery cleanup incomplete; do not wrap its private timer as operation DeadlineExceeded on a completed
snapshot. Stop errors remain visible except nats.ErrBadSubscription or nats.ErrConnectionClosed after actual Updates
closure. Such Stop errors alone never prove cleanup; other Stop errors remain errors even if Updates closes, because
closure does not prove consumer deletion. No retry, fallback, global connection close or caller-resource destruction.

## Compatibility and persistent observer

FilteredKeys, collectFilteredKeys and CatalogReader stay unchanged: their existing typed-cancel honesty remains, but
inaccessible native watcher ownership is not claimed repaired. Unfiltered Keys, direct native ListKeys and long-lived
Watch are outside this unit. Full input method set stays unchanged, but full-bucket adapters need working WatchFiltered
for filtered KVStore calls. Update only directly reached mocks, preserving matcher and fault behavior. No empty-success
watchers replacing semantic proof. Stronger error/completion behavior and changed adapter call are explicit costs.

Move ownerLoadObservedBucket to WatchFiltered and KeyWatcher; forward exact context, filter values and watch options.
Embed native Updates unchanged; wrap Stop synchronously only. Retain five-attempt capacity/order/prior failure records,
constructor/Stop/caller timestamps, actual child deadlines, ordered snapshots, unknown states and finite callback ownership.
Public-call duration precedes diagnostic joining/reporting. after_stop_awaiting_return_marker can include production
terminal drain, but is not independent observation of Updates closure. Observer neither consumes nor rescues Updates.

Update transparency proofs for native Updates identity, forwarded options/context, construction failure, cancellation,
Stop errors and callback ordering. Non-benign Stop error now fails production and must be recorded transparently.
Reconcile full graph-index normative requirement, preserving archived observer design as history. If two publication
resolutions' fingerprints change, refresh only reviewed metadata in cleanup_baseline.json; preserve273legacy/96resolutions
if no sites added/removed. No guard relaxation or new debt.

## Fast permanent regression and mutation

Ordinary natsclient regression calls real NewKVStore.KeysByFilter. Deterministic small bucket/watcher fixture uses
explicit channels: constructor entered while context live; producer has pending Updates send that must finish before
channel closes; test cancels caller and releases constructor; Stop records but does not complete pending send. After
public return assert typed cancellation, nil keys, Stop once, Updates closure AND producer completion.

Initial RED against current production must model the native forwarding/cancellation relationship through existing
ListKeysFiltered sufficiently to fail the owning completion assertion. Compilation, wrong-method/setup error or a
classifier log is not RED evidence. Every fixture goroutine has explicit done and bounded ownership. Rescue occurs only
after capturing terminal assertion failure, remains test-only and cannot turn result green. Bounded model is not SDK
scheduling reproduction.

Named histories: successful/empty snapshot; post-marker discard; constructor error/noStop; premature closure;
non-benignStop failure with closed drain; already-terminalStop with/withoutclosure; never-closingdrain; cancellation plus
cleanupfailure; gated contextlessStop proving call does not return while Stop still owned; minimal-reader compatibility.
Use deterministic handshakes rather than randomized scheduling: finite construction/marker/cancel/Stop/closure/expiry
boundaries. Existing conformance owns filter/key grammar; no duplicated parser/property suite.

Cover the live-context sentinel, canceled/deadline sentinel, wrapped-sentinel error, and missing-watcher cases
explicitly. Use testing/synctest for private five-second expiry histories, following
processor/graph-index/reconciliation_model_test.go:27, so unit tests retain the production interval without real
five-second waits. Explicit handshakes still establish ordering; native validation uses real time.

Required mutation bypasses production drain/completion enforcement. The public-path test must fail on terminal
ownership, and never-closing watcher must not become success. cp backup/checksums; actual intended assertion; exact
restoration. This must validate the owning result path, unlike the temporary classifier-only proof.

## One bounded native validation after repair

After ordinary RED/GREEN, restored mutation and source review, one focused native validation precedes expensive required
CI. Reuse existing TestClient, file-backed5000-key shape and conformance; do not promote the approximately900-line
experimental process/stack framework to CI.

Transparent WatchFiltered constructor decorator holds real returned watcher until unchanged framework child expires,
then returns exact watcher/Updates. Observe native Updates buffer capacity reached as concrete exercised condition,
not a callback-stack claim. Public call must return typeddeadline/nilkeys; immediately require native Updates closed
and drained. Residual entry/openchannel fails. No facade, relay, independent native drain or Client.Close supplies the
asserted completion. Existing conformance supplies successful filter semantics.

Finite test/process timeout and bounded fixture finalization; preserve primary failure before necessary test cleanup.
If check fails/hangs, retain and stop; no repeat, timeout relaxation or fullCI as diagnostic substitute. This validates
post-repair delivery boundary, not native goroutine joins, consumer-deletion completion or historical causation. It is
separate from the completed once-only diagnostic, not a rerun of that experiment.

## Ownership and tasks

Developer: natsclient implementation/ordinary/native tests; graph-index observer/directly affected mocks; reached
full-bucket graph-ingest test doubles; exact reviewed cleanup fingerprints if needed. Root: OpenSpec/evidence/GH/git.
1. Accept reviewed private-path/error/budget choice and materialize deltas.
2. Add public-path regression and retain meaningful RED.
3. Implement watcher collection/finalization; reconcile reached mocks.
4. Reconcile observer/unitproof/normative requirement/exact guard metadata.
5. Focused race and owning-assertion mutation; restore source.
6. Independent source review, then one bounded native validation.
7. Appropriate final gates with actual durations and unresolved CI history retained.

## Draft nats-kv-keys delta

### ADDED Requirement: Filtered KVStore listings finalize native watcher delivery

KVStore.KeysByFilter and its KeysByPrefix alias SHALL preserve existing operation timeout and native filter semantics.
When construction returns no watcher, operation cancellation or deadline expiry SHALL take precedence and return nil
keys with the typed context error. With a live operation context, a direct jetstream.ErrNoKeysFound constructor result
SHALL retain the existing (nil, nil) compatibility behavior without Stop or drain; this is the sole exception to
snapshot-completion and Updates-closure requirements.

For a returned watcher, successful results SHALL require initial snapshot completion and native Updates closure after
terminal cleanup. Post-marker entries SHALL be discarded. Other constructor failures, a missing watcher without the
sentinel, premature closure, non-benign Stop errors, or failure to observe closure SHALL NOT produce successful keys.
Cleanup failure SHALL remain distinguishable without replacing the primary failure.

For returned watcher, SemStreams SHALL invokeStop once synchronously then consumeUpdates throughclosure using separate
5s terminalwindow derived from operationcontext without its cancellation. Window SHALL bound postStopwait only, not
Stop or totalcall. Updatesclosure SHALL establish deliveryboundary only, not nativegoroutinejoin/consumerdeletion.
Drain timeout SHALL report incompletecleanup. No backgroundfinalizer/retry/hiddenfallback/adoptercleanupknob introduced.
Requirement applies full-capability KVStorefiltered path; minimalFilteredKeys/CatalogReader methodsets stay unchanged.

#### Scenario: Cancellation with pending native delivery

- GIVEN a filtered KVStore listing returned watcher with pending Updates delivery
- WHEN operation context is canceled
- THEN return nil keys retaining typed error, request Stop synchronously, consume Updates through closure
- AND inability to observe closure is an additional cleanup failure.

#### Scenario: Completed snapshot with queued later updates

- WHEN initial marker arrives before operation cancellation
- THEN only snapshot keys are eligible, later updates are discarded
- AND success requires delivery closure and acceptable Stop completion.

#### Scenario: Terminal drain does not complete

- WHEN Updates does not close within5s after Stop returns
- THEN return nil keys/delivery-cleanup failure, make no completion claim
- AND no SemStreams-created asynchronous finalizer remains.

#### Scenario: Stop has not returned

- WHEN contextless Stop remains in progress
- THEN drain window does not certify or preempt it, and public-call duration includes it.

#### Scenario: No-match constructor result respects cancellation

- GIVEN construction returns no watcher and direct jetstream.ErrNoKeysFound
- WHEN the operation context is still live
- THEN the result is (nil, nil) without Stop or drain
- BUT WHEN the operation context is canceled or expired
- THEN the result is nil keys with the corresponding typed context error, without Stop or drain.

## Draft graph-index delta instruction

MODIFIED: carry complete current Predicate-forward measurements retain bounded phase evidence requirement and ALL
scenarios from spec.md666–726. Preserve text except:
- Replace native Keys channel with native Updates channel used by production filtered watcher.
- Observation-preservation scenario: collection and terminal cleanup consume exact native Updates under production
  ownership without observer relay/independent drain.
- Add complete public-call duration including Stop/terminaldrain stays in existing measurements/assertions.
- Preservation paragraph: interval afterStop beforeoperationmarker remainsidentifiable without beingrepresented as
  independent observation of Updatesclosure or nativegoroutinejoin.

This reconciles active observer contract, not historical archive or workload. Independent design review and owner
acceptance precede implementation.
