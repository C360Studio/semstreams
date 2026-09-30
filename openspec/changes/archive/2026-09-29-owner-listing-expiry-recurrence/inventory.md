# Owner listing expiry recurrence — inventory refresh

base: b3ef3ef67551cb16852e8f5d88419e0888a98d79

## Scope and preserved evidence

This is an evidence-only refresh for #1421 / PR #1435. It introduces no target state, public API, timeout change, retry policy, or production repair.

The accepted predecessor inventory remains unchanged at:

`openspec/changes/archive/2026-09-29-owner-load-reliability/inventory.md`

SHA256: `d0872671356ee5505087160054d890384c13ceb19d4364d7fc2745a3b01fd3a1`.

Its five inventory categories, exported-listing adopter seam, historical incident attribution limits, and neighboring ownership remain applicable. This refresh changes the observed failure phase and available evidence; it does not repeat the repository or sister-repository census.

The recurrence occurred on main `41d6236a84a8443b284785372126a092331096f6`, CI run **36641021599**, required Test job **109653051745**.

Raw log: `/private/tmp/gh1432-main-failure.log`, 635,745 bytes, SHA256:

`2dbec44c3ae4f8e78d56eefaa6ecade93433af4bff0278cd3670b0736b75ae9b`.

The follow-up claim's initial “fourth” wording is inaccurate: source uses a zero-based counter, so `repetition=4` is the **fifth attempt**.

## Changed and unchanged facts

| Surface | Prior accepted incident | Recurrence |
|---|---|---|
| Failed listing | NAME-forward convergence after concurrent work | Predicate-forward initial measurement |
| Concurrent phase | Completed before failure | Not entered |
| Operation duration | Not recorded | 10.001209581 seconds |
| Caller authority | Insufficient incident detail | Deadline `2026-09-29T23:08:46.853291245Z`; cause nil |
| Framework default | Five seconds | Five seconds |
| Stack evidence | None | Post-return, pre-harness-teardown snapshot |
| Subsequent drain failure | Approximately fifteen seconds observed | Not shown in this failure interval |

`git diff --stat 41d6236a --` returned no changes for the inspected listing, client, harness, diagnostic helper and module files, both in the initial read-only checkout and the follow-up claim checkout.

Current source SHA256s:

| File | SHA256 |
|---|---|
| `natsclient/kv.go` | `9b9b4c40aa6f65b66bdcb68a7db881e0fdfb4bb8d46f8b2af4d42d86d1696725` |
| `processor/graph-index/owner_filter_load_integration_test.go` | `6e6314b0dee72fa66dd5c3891c599dcc49bcfc82f7ad106c1290a2dfacb474bc` |
| `processor/graph-index/owner_filter_load_helpers_test.go` | `bf7de818aac434617315e1b7dad07047591b9a2635e944d650c816b5e78b2c67` |

No test or causal experiment was run during this refresh.

## Exact recurrence observations

The failed-log interval is lines **3421–3633**.

The CI profile used 5,000 entities, 5,000 name contexts, spread 20, five repetitions and worker shape four. Seeding completed 15,020 rows in 976.926348 milliseconds. Maxima, cancellation/empty/recreate and fresh-handle checks reported completion.

Predicate-owner completed all five repetitions, with submitted durations:

`1.726384ms,1.694817ms,1.590764ms,1.5784ms,1.830993ms`.

The next recorded failure identifies:

```text
phase=measured-list
fixture=predicate-forward
bucket=OWNER_LOAD_PREDICATE
filter=robotics.status.ready.*.*.*.*.*.*
operation=repetition=4
elapsed=10.001209581s
caller_deadline=2026-09-29T23:08:46.853291245Z
caller_cause=<nil>
framework_kv_default=5s
error=kv keys by filter "robotics.status.ready.*.*.*.*.*.*": context deadline exceeded
```

The top-level harness failed after 23.51 seconds; its workers-4 subtest after 10.66 seconds; graph-index after 45.010 seconds. These durations do not identify an unlogged cleanup phase.

The first four predicate-forward calls necessarily passed their error/count assertions before the fifth was admitted, but their individual durations were not published: the distribution logger is reached only after the repetition loop completes.

- `processor/graph-index/owner_filter_load_integration_test.go:472` — `for repetition := 0; repetition < profile.repetitions; repetition++ {`
- `processor/graph-index/owner_filter_load_integration_test.go:474` — `keys, err := store.KeysByFilter(ctx, filter)`
- `processor/graph-index/owner_filter_load_integration_test.go:475` — `duration := time.Since(started)`
- `processor/graph-index/owner_filter_load_integration_test.go:484` — `require.NoError(t, err, label)`
- `processor/graph-index/owner_filter_load_integration_test.go:485` — `require.Len(t, keys, want, label)`
- `processor/graph-index/owner_filter_load_integration_test.go:488` — `assertOwnerLoadLatency(t, label, durations, profile)`

The sequential measurement loop precedes `ownerLoadConcurrent`. Consequently the concurrent helper's ten-second terminal budget is not evidence for this ten-second listing duration.

## Framework operation and return boundary

The caller's live fifteen-minute context is distinct from the framework's child context. A nil caller cause therefore does not contradict expiry of the framework child.

- `natsclient/kv.go:39` — `Timeout:               5 * time.Second,`
- `natsclient/kv.go:70` — `return context.WithTimeout(ctx, kv.options.Timeout)`
- `natsclient/kv.go:538` — `ctx, cancel := kv.applyTimeout(ctx)`
- `natsclient/kv.go:541` — `lister, err := kv.bucket.ListKeysFiltered(ctx, pattern)`
- `natsclient/kv.go:550` — `keys, err := collectFilteredKeys(ctx, lister)`
- `natsclient/kv.go:583` — `defer func() { _ = lister.Stop() }()`
- `natsclient/kv.go:588` — `case <-ctx.Done():`
- `natsclient/kv.go:589` — `return nil, ctx.Err()`

The wrapper applies one operation timeout and performs no listing retry. Its identical error prefix covers both native lister-construction failure and collection failure. The incident's error text cannot distinguish those branches.

The measured duration includes deferred synchronous `lister.Stop()`. Its result is discarded. Thus a five-second supplied context does not establish a five-second wall-clock return.

## Concrete SDK path that can add another five seconds

Selected dependency: **github.com/nats-io/nats.go v1.52.0**. Paths below are relative to that exact module.

| SDK source | Observed behavior |
|---|---|
| `jetstream/kv.go:564` | CreateKeyValue obtains `js.legacyJetStream()` |
| `jetstream/kv.go:887–898` | Legacy construction forwards API prefix and client trace; supplies no replacement request timeout |
| `js.go:299` | `defaultRequestWait = 5 * time.Second` |
| `js.go:314` | Legacy JetStream options initialize `wait` from that default |
| `jetstream/kv.go:1465–1466` | KeyLister.Stop delegates to watcher.Stop |
| `jetstream/kv.go:1206–1210` | Watcher.Stop calls Subscription.Unsubscribe |
| `nats.go:5181–5203` | Unsubscribe can synchronously delete a library-created consumer |
| `js.go:1452–1467` | Subscription deletion invokes legacy DeleteConsumer without a supplied context option |
| `jetstream/kv.go:1305` | KV WatchFiltered selects OrderedConsumer |
| `js.go:2279` | resetOrderedConsumer launches asynchronous DeleteConsumer during recovery |
| `jsm.go:606–615` | DeleteConsumer obtains context options and issues its API request |
| `jsm.go:1781–1786` | With no operation context, it uses the default wait and creates a fresh Background timeout |
| `js.go:3550` | API request calls `RequestWithContext` |

This establishes a **reachable five-second listing expiry followed by an independently bounded five-second synchronous deletion request**. It is a source-supported explanation for approximately ten seconds, not proof that this execution selected that path.

Other native Stop callers exist concurrently: the SDK forwarding goroutine defers watcher.Stop, and subscription setup installs a goroutine that calls Unsubscribe after context completion. A fourth native deletion origin is ordered-consumer recovery: resetOrderedConsumer launches `go js.DeleteConsumer(jsi.stream, jsi.consumer)` without waiting for its response. KV WatchFiltered explicitly selects OrderedConsumer, so this is a reachable same-class owner. This asynchronous deletion is not itself evidence of a synchronous return-path five-second wait. A deletion request alone cannot identify Stop as its owner; the current observation identifies no deletion owner.

SDK SHA256s:

| File | SHA256 |
|---|---|
| `jetstream/kv.go` | `fdd64fbd20753bd658aafbc299278deef3fbd44b6ffea0c476c506633d464c4a` |
| `js.go` | `01551a520646ec211cf0b41d764f30fe179d6bc590ff475263fd5a7d803c38e1` |
| `jsm.go` | `d46ab4788b8f002b15dd0e80269f2ad6efca14ac1ca805cfca2e0d9124f13bcb` |
| `nats.go` | `88b6e77a878bc560c7c37974c43f25e0fcc84b98ebd00453cea02a4f7a1871ba` |

Native locks, scheduling and transport behavior remain outside any strict wall-clock inference from these timeout constants.

## What the snapshot establishes

Capture occurs after `KeysByFilter` returns and before the assertion exits the harness.

- `processor/graph-index/owner_filter_load_helpers_test.go:25` — `buf := make([]byte, 64<<10)`
- `processor/graph-index/owner_filter_load_helpers_test.go:26` — `n := runtime.Stack(buf, true)`
- `processor/graph-index/owner_filter_load_helpers_test.go:28` — `if n == len(buf) {`
- `processor/graph-index/owner_filter_load_helpers_test.go:29` — `stacks += "\n[goroutine stacks truncated]"`

The captured interval contains **17 goroutine headers** and no truncation marker. It contains no `jetstream/kv.go`, WatchFiltered, ListKeysFiltered, DeleteConsumer or deleteConsumer frame. The two captured native delivery goroutines are waiting at `nats.go:3589`, where the SDK waits when the subscription has no queued message and is not closed. Native asynchronous callback dispatchers are also waiting.

There is therefore no witnessed blocked KV forwarding producer or watcher callback **at capture time**. Their absence after return does not establish their absence during the operation, producer joining, or the cause of expiry. The snapshot also does not identify the failed temporary consumer or its API requests.

## Relation to the retained native experiment

The reviewer-corrected experiment witnessed two distinct native blocked sends while the framework context remained live: SDK forwarding at `jetstream/kv.go:1451` and watcher delivery at `jetstream/kv.go:1290`.

Under that controlled withholding, cancellation/deadline returned nil keys promptly; the default-deadline case returned at approximately 5.007 seconds. Delegated Stop returned `nats: invalid subscription`, and Client.Close returned promptly. It reproduced neither the historical fifteen-second drain failure nor this ten-second measured listing.

Retained final diagnostic source SHA256:

`cbfd896bf49834ab5278c7c6a8c8fee50eb8fa6a2e301a975c906d7c134c7523`.

The corrected execution/applicability record is `evidence/diagnostic-review-correction.md`, SHA256:

`3382629cd923455a266288237f7f06cee29af86abf212f2355029874f16da508`.

The current snapshot is not the native experiment's blocked-producer witness. Similar deadline text does not make their causal schedules equivalent.

## Inventory gaps and bounded causal questions

1. **Construction versus collection:** Did ListKeysFiltered return a usable lister before the deadline? The wrapper's error text does not answer.
2. **Return-path time:** How much elapsed time occurred before collection completed versus inside deferred Stop? No phase timestamps or Stop result were captured.
3. **Deletion ownership:** Did the framework, SDK forwarding goroutine, context-triggered unsubscriber, or asynchronous ordered-consumer recovery issue a deletion request? Was that request part of synchronous return-path cleanup or independent recovery, and was a response received or did its request deadline expire?
4. **Snapshot progress:** If collection began, how many keys arrived, and did the native initial-snapshot marker arrive? The current result intentionally discards partial keys and records no progress count.
5. **Earlier native blockage:** Were either native producer, a lock, consumer creation, delivery, flow control or ordered-consumer recovery stalled before the post-return snapshot? The snapshot cannot reconstruct that interval.
6. **Server and transport cause:** No failure-time server/consumer state or request trace establishes CPU, storage, transport, consumer state or response loss as the trigger.

A bounded causal experiment must distinguish these branches and report completion separately from post-return stack disappearance. This inventory does not select its implementation.

The current error remains a required guard failure under the existing graph-index typed-deadline requirement. No timeout increase, retry-to-green, broader waiver, or recurrence repair is supported by this evidence.

## Searches and read boundary

Structural queries, each completed with authorized Go-cache access:

```text
gopls workspace_symbol -matcher=fuzzy DeleteConsumer
gopls call_hierarchy /Users/coby/go/pkg/mod/github.com/nats-io/nats.go@v1.52.0/jsm.go:599:15
gopls workspace_symbol -matcher=fuzzy 'kvs.pushJS'
gopls workspace_symbol -matcher=fuzzy defaultRequestWait
gopls references /Users/coby/go/pkg/mod/github.com/nats-io/nats.go@v1.52.0/jetstream/kv.go:439:2
gopls call_hierarchy /Users/coby/go/pkg/mod/github.com/nats-io/nats.go@v1.52.0/jetstream/kv.go:1564:6
gopls workspace_symbol -matcher=fuzzy legacyJetStream
```

Repository reads: full architect contract and project context; archived inventory; diagnostic report and corrected report; harness implementation and corrected report; reproduction and experiment/harness acceptance records; current nats-kv-keys specification; graph-index specification ranges 190–244 and 312–430; full active proposal/tasks.

Source ranges read:

```text
owner_filter_load_integration_test.go: 1–210, 250–323, 452–508
owner_filter_load_helpers_test.go: full, then 1–55 and 18–36
natsclient/kv.go: 20–80, 527–606
SDK jetstream/kv.go: 501–575, 887–932, 1190–1360, 1420–1476, 1550–1583
SDK js.go: 285–340, 1435–1478, 1770–1828, 1900–2070, 2263–2284, 3533–3665
SDK jsm.go: 590–635, 1762–1816
SDK nats.go: 3578–3658, 5160–5220
```

The `js.go:3568–3665` read was not the intended native delivery-loop file; the subsequent `nats.go:3578–3658` read supplied that evidence. No absence inference used the wrong range.

Log reads covered 3380–3423 and 3420–3633. Mechanical Python inspection counted goroutine headers, checked the truncation marker and named SDK frames, and computed source/log SHA256s. No repository source, baseline, spec or task file was edited.

## Independent review correction

Round-one review identified the omitted ordered-consumer recovery deletion origin using the existing
DeleteConsumer call-hierarchy query. The coordinator read SDK jetstream/kv.go:1305 and js.go:2263–2284 and
materialized the narrow correction above. SDK hashes and repository source remain unchanged. No design or
causal attribution was added.
