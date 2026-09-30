# Observe the actual repeated predicate-forward workload

base: f5603895b1f225735d6138000b276c505adfaa08

Status: proposed measurement slice for #1421 / PR #1435. No production repair or issue closure is established.

## Preserved evidence

Keep the accepted inventory verbatim in the review packet:

- `inventory.md`: SHA-256 `6f3479feb76d24d9768fe05abd815df2187b9baf6c89ea01e0fa15d1d2ecfc5a`.
- First measurement design: `5e1e18c10f1d0aacda7d6b95e1042e44cc0c73d97862ce772997e2b891afd1f4`.
- First diagnostic source: `f50543bbd69effa1cb29f0e45b0e3c2c59a1bd401aee05598eec794ef8d16d71`.
- Evidence disposition: `review/measurement-evidence-final.md`.

Those hashes match the current checkout. The actual harness, its ownership helpers and `natsclient/kv.go` also match
the source hashes in the accepted inventory.

The first native pass observed prompt construction and Stop in a different, 1,024-key fixture. It did not reproduce the
ten-second CI return. The next question is which phase consumes time in the actual repeated predicate-forward workload,
including a failing attempt.

## Refreshed seam evidence

No new production seam is needed.

| Fact | Current source pin |
|---|---|
| Actual CI fixture is 5,000 entities and five repetitions | `processor/graph-index/owner_filter_load_integration_test.go:56` — `name: "ci", entities: 5_000, nameContext: 5_000, spread: 20,` |
| File-backed buckets already exist | `processor/graph-index/owner_filter_load_integration_test.go:195` — `raw, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: bucketName, Storage: jetstream.FileStorage})` |
| Raw handles enter the production wrapper here | `processor/graph-index/owner_filter_load_integration_test.go:197` — `stores[bucketName] = nc.NewKVStore(raw)` |
| Repeated measurement uses production listing | `processor/graph-index/owner_filter_load_integration_test.go:474` — `keys, err := store.KeysByFilter(ctx, filter)` |
| Timing currently precedes failure reporting | `processor/graph-index/owner_filter_load_integration_test.go:475` — `duration := time.Since(started)` |
| Fatal assertions can prevent later distribution output | `processor/graph-index/owner_filter_load_integration_test.go:484` — `require.NoError(t, err, label)` |
| Construction boundary | `natsclient/kv.go:541` — `lister, err := kv.bucket.ListKeysFiltered(ctx, pattern)` |
| Collection boundary | `natsclient/kv.go:550` — `keys, err := collectFilteredKeys(ctx, lister)` |
| Synchronous terminal boundary | `natsclient/kv.go:583` — `defer func() { _ = lister.Stop() }()` |
| Native Keys channel is consumed directly | `natsclient/kv.go:590` — `case key, ok := <-lister.Keys():` |
| Failed partial results are refused | `natsclient/kv.go:589` — `return nil, ctx.Err()` |

Typed references confirm `measureOwnerLoadFilter` has only its owner/forward callers at harness lines 289 and 292.
`createAndSeedOwnerLoadBuckets` has one caller at line 159. The five predicate-forward attempts precede
`ownerLoadConcurrent` at line 309.

The first diagnostic already provides the applicable transparent shape: its bucket decorator delegates the exact
context and filters, its control returns a KeyLister observer without the forwarding facade, and its Stop observer
delegates synchronously. Its bounded completion join and early-exit reporting corrections remain relevant.

The inherited surface/adopter inventory remains applicable: this changes private tests, supplies no new application
contract and imposes no downstream migration. There is no new durable or communication primitive. Existing test
ownership remains split between the listing caller, its diagnostic callback and canonical NATS infrastructure.

## Options

| Option | Benefit | Cost or limit |
|---|---|---|
| Retain current diagnostics | No additional observer overhead | Failing calls still lack construction/collection/Stop separation; prior successful attempts remain unpublished on an early failure. |
| Temporary instrumented copy of the actual harness | Removable experiment; can retain an exact diff | A healthy local pass may again miss the rare failure. A copied harness can drift; future CI recurrence remains unobserved after removal. |
| Persistent private phase observer in the existing harness | Captures the actual failing workload whenever it next occurs; no duplicate workload | Adds small timestamp/synchronization overhead to five calls and one conditional failure callback. Requires explicit ownership and evidence-retention tests. |
| Attach SDK tracing and fault machinery now | More request detail | Per-request stack capture perturbs the measured path and adds attribution machinery before the slow phase is known. |

Recommend the persistent private phase observer, followed by one focused native run. Defer SDK tracing and transport
faults until phase evidence establishes a specific remaining question.

## Scope and implementation ownership

One developer owns:

- `processor/graph-index/owner_filter_load_integration_test.go`: connect observation to the existing CI-profile
  predicate-forward measurement.
- One private `processor/graph-index/owner_filter_load_observer_test.go`: transparent decorator, bounded records,
  callback ownership and focused tests.

No production file, SDK, canonical runner, shared test framework, public API, configuration field or cleanup baseline
changes are included.

Instrument only the five initial predicate-forward attempts in the default CI profile. Other filters, seeding, maxima,
cancellation/recreate, fresh handles, concurrent workload, convergence and the full profile retain their existing paths
and assertions.

Preserve:

- The real file-backed fixture, all 15,020 seed rows and existing ordering.
- `Client.NewKVStore` with its unchanged five-second default.
- Exact caller context and filter forwarding.
- Production collection and error wrapping.
- Existing count, latency, convergence and resource assertions.
- Typed deadline errors and nil partial results.
- Existing substrate cleanup.

## Transparent observation

Use a private KeyValue decorator at the existing raw-handle composition point. Its ordinary methods remain delegated
through the embedded real KeyValue.

The recorder is explicitly armed by the existing predicate-forward measurement loop for one sequential attempt.
Outside that bounded scope, `ListKeysFiltered` delegates without adding an observer. An unexpected overlapping attempt
is a diagnostic error, not permission to attach a record to the wrong operation.

For an observed attempt:

1. Record operation entry immediately before the existing `KeysByFilter` call.
2. At decorated `ListKeysFiltered` entry, record the exact framework child deadline and construction entry.
3. Delegate once with the exact received context and filters; record construction return and its error.
4. On construction success, return a KeyLister observer embedding the native lister. **Do not override Keys:** the
   production collector receives the exact native channel.
5. The observer records Stop entry, invokes native Stop once synchronously, and records its return/error unchanged.
6. Immediately after production `KeysByFilter` returns, record the operation-return marker, result count, nil-slice
   status, error and `errors.Is` cancellation/deadline classifications.
7. Finish diagnostic callback ownership before admitting the next attempt.

Do not copy the synthetic facade, add a forwarding channel, consume a native key, drain the lister independently,
retry Stop or alter Stop's result.

The interval from successful construction return to delegated Stop entry approximates the collection phase, including
the collector's final context check and small wrapper overhead. Call it that interval, not an exact per-key measurement.
Construction failure legitimately has no collection or delegated Stop record.

This seam cannot observe the native initial-snapshot marker or partial key count without additional interception.
Both remain explicitly unknown.

## Failure-time snapshot and ownership

Register one `context.AfterFunc` on the **actual framework child received by the decorator**. The callback captures a
bounded all-goroutine snapshot only when that context reports `context.DeadlineExceeded`. Ordinary cancellation caused
by the production function's deferred cancel does not trigger a stack capture.

Use the existing 64 KiB stack limit and explicit truncation reporting. The callback performs no stdout, testing calls,
network requests, filesystem operations or waits on the listing. Hold the recorder mutex only to copy or publish small
metadata; release it before `runtime.Stack`.

Record:

- Child deadline.
- Callback-start and snapshot-start/end timestamps.
- Observed child error/cause.
- Recorded phase before and after capture.
- Whether the operation-return marker was already present before capture and after capture.
- Snapshot truncation.

Classify the snapshot as:

- **Before the recorded return marker**.
- **After the recorded return marker**.
- **Straddling the recorded return marker**.
- **Unknown/incomplete**, if the required observation is missing.

The marker is recorded immediately after the call returns, but is still a caller observation. Do not equate “context
done” or “return marker absent” with proof that a particular SDK frame was blocked. The actual stack supplies that
evidence. Callback scheduling time is not the deadline instant.

After each attempt, stop the callback if it has not begun. Otherwise join its completion with existing finite terminal
authority, using the `ownerLoadTerminalBudget` ownership pattern. Install lexical finalization before native construction
can fail. A missed callback join stops further admission, reports unresolved diagnostic ownership and preserves the
primary operation failure.

The observer never gains native lister cleanup authority. Production remains the sole owner of its delegated Stop;
canonical `NewTestClient` remains the substrate owner.

## Retention before assertions

Allocate fixed capacity for exactly five attempt records; never reset or overwrite prior attempts during the loop.
Retain records on constructor error, collection error, count failure, diagnostic failure and fatal assertion exit.

Capture the existing operation duration immediately after `KeysByFilter`, before callback joining, formatting or output.
Keep the current latency assertions on that duration.

Emit records through a checked lexical finalizer around the measurement loop. This runs on normal return and
`require`/`FailNow` exits. On a failure it includes all completed earlier attempts plus the failing attempt; on success
it includes all five.

Avoid logging between successful attempts so the diagnostic does not introduce file/stdout pacing into the repetition
sequence. Keep the existing failure logger and percentile recording.

Each record distinguishes:

- `not_entered`
- `entered_without_observed_return`
- `returned`
- `not_applicable`
- `unknown/incomplete`

Do not fill an absent timestamp with zero and imply instantaneous completion. No Stop record is expected after
constructor failure; a successfully constructed lister without one observed delegated Stop is a diagnostic finding.

Report diagnostic errors without replacing the original listing/count failure. Record overflow, inconsistent attempt
association, missing required observations and unresolved callback ownership fail observation integrity. Stack truncation
makes that snapshot incomplete; it cannot support absence claims.

## Perturbation and completion limits

This adds a fixed number of timestamps, brief synchronization, a transparent wrapper and callback registration to five
calls. It adds no per-key processing or per-request trace stack capture.

It is not literally non-perturbing. Record that the measured distribution includes wrapper overhead. A deadline-triggered
all-goroutine snapshot can affect terminal scheduling after expiry and therefore the observed Stop/return tail. It cannot
identify how much of that tail existed without observation.

No extra A/B run is authorized merely to quantify small overhead. Focused delegation tests establish semantic transparency;
the single native pass supplies its actual observed timings. Existing thresholds remain unchanged.

Keep distinct:

- Production return.
- Native delegated Stop return/error.
- Diagnostic callback join.
- NATS cleanup result.
- Native producer/callback completion.

This slice does not expose a native watcher join and must not infer one from channel closure, a snapshot or successful
harness cleanup. It does not identify one of the four SDK deletion origins merely from a slow Stop interval.

## Focused verification and sensitivity

Use deterministic fake KeyValue/KeyLister edges while invoking the real `Client.NewKVStore(...).KeysByFilter` path.

Required fast checks:

1. **Transparent success:** exact context/filter forwarding; returned native Keys channel identity; unchanged keys;
   one native Stop; separately preserved Stop error even though current production semantics discard it.
2. **Constructor failure:** unchanged wrapped typed error, nil result, retained entry/constructor-error record,
   no fabricated collection or Stop.
3. **Collection expiry:** an already-expired caller deadline gives a real ended framework child; production returns
   typed deadline error and nil keys despite queued partial input; one Stop remains observed.
4. **Early-exit retention:** run the measurement/reporting scope through the real assertion-exit mechanism in an isolated
   test case, or its existing supported equivalent; verify prior and failing records remain available and callback
   finalization runs. A helper returning an error alone does not prove `FailNow` behavior.
5. **Snapshot ordering:** explicit channels hold native construction or Stop while a deadline callback captures;
   separately exercise delayed capture after the return marker and a capture straddling it. Verify labels follow
   evidence rather than `ctx.Done()` alone.
6. **Finite callback ownership:** unresolved completion under an already-ended terminal context reports the join failure
   and admits no next attempt.
7. **Record integrity:** absent/not-applicable phases and overflow are reported without invented completion.

Use channels for ordering, not sleeps. Do not add a randomized property harness.

Select one current-source sensitivity mutation: remove failure-path record publication or falsely classify a post-return
snapshot as in-flight. The corresponding independent assertion must fail for the intended reason. Preserve `cp` backup,
original/mutated/restored hashes and cleanup evidence. Restore before native execution; do not run a native mutation matrix.

## One-pass execution

After independent source review and focused checks:

```sh
scripts/run-integration-tests.sh -timeout=180s -run '^TestIntegration_OwnerFilterLoadHarness$' -v ./processor/graph-index
```

Run the existing complete harness once, with `GRAPH_INDEX_OWNER_FILTER_FULL` unset. The runner provides the host lock and
race selection. Do not start another heavy local operation alongside it.

Use a fresh maximum **five-minute aggregate local execution allowance** for this reviewed slice, counting focused tests,
sensitivity check, runner/build/lock waits, native execution and cleanup. Record debits. Before native admission, at least
210 seconds must remain: the 180-second test alarm plus 30 seconds for supervising exit and checking ownership. If that
allowance or the host slot is unavailable, report it without launching or repeating.

The Go test alarm contains test execution; it does not prove cleanup ran on a timeout. Supervise concrete process/lock/
container state. Preserve output and report unresolved ownership if the alarm or an external stop prevents ordinary
teardown. Do not restart to obtain green evidence.

Record source/diff hashes, exact command, actual Go version, SDK hash/version, server digest, host/runtime, start/completion
observations, all five available records and final cleanup status.

## Decision after the pass

| Observation | Supported next question |
|---|---|
| Construction consumes the interval | Examine actual acquisition frames and construction behavior. |
| Construction is prompt; collection reaches the deadline; Stop is prompt | Investigate delivery/snapshot progress; synchronous Stop does not explain this attempt's extra time. |
| Delegated Stop consumes the extra interval | Examine that native terminal path; targeted request-origin evidence may become justified. |
| Deadline snapshot runs after return | Retain it as post-return evidence; it does not locate the earlier blockage. |
| Healthy five-attempt sequence | Observer wiring is exercised; recurrence cause remains unresolved. Keep the useful failure diagnostics rather than run blindly again. |
| Evidence is incomplete or callback ownership unresolved | Fix only the demonstrated diagnostic defect before further execution. |

A healthy local pass neither explains the CI failure nor authorizes closing #1421. No production spec delta, timeout
increase, relaxed assertion, relay, SDK change or full CI rerun belongs to this slice.
