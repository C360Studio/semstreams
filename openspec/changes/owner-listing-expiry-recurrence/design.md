# Listing recurrence: first measurement pass

Status: proposed diagnostic experiment only; supersedes the earlier four-case relay proposal.

Preserve the accepted inventory unchanged: checkpoint `3d769265f973f7dff967256b6d881b4053569441`, SHA256
`6f3479feb76d24d9768fe05abd815df2187b9baf6c89ea01e0fa15d1d2ecfc5a`.

## Decision and scope

Extend the retained native diagnostic with phase measurements and SDK request tracing. Execute one transparent control
and one retained default-deadline collection schedule, then review the evidence before choosing another experiment or
proposing a production repair.

| Option | Result |
|---|---|
| Do nothing | Leaves construction, collection and Stop timing unresolved |
| Measure the existing two schedules | Smallest useful evidence increment; recommended |
| Add transport faults now | Additional fixture and ownership cost before existing phase measurements; deferred |
| Change production behavior | Unsupported without further evidence |

No relay, SDK modification, production edit, public API, retry, timeout relaxation, owner-load workload change or
recurring CI experiment is included.

## Existing seams

Use the archived diagnostic source identified by SHA256
`cbfd896bf49834ab5278c7c6a8c8fee50eb8fa6a2e301a975c906d7c134c7523`.

Preserve its corrected child ownership, independent Stop observer, first-key witness, native blocked-stack gates and
exact-set oracle. Record the source diff so existing proof applies only to unchanged code and assertions.

Available measurement seams are:

- Private KeyValue decorator around the real `ListKeysFiltered`.
- Private KeyLister observer around the real `Stop`.
- `Client.GetConnection()` and SDK `jetstream.WithClientTrace`.
- Existing framework-return, native-channel, Client.Close and child-containment observations.

Construct a traced SDK JetStream handle on the existing production client connection and acquire the real bucket through
it. Pass its decorator to production `Client.NewKVStore`; leave the client's own stored JetStream handle unchanged.

## Measurements

Record monotonic timestamps and results for:

1. Production operation entry and captured framework context deadline.
2. Native lister-construction entry and return.
3. Existing first-native-key and first-key-consumed witnesses.
4. Framework context completion.
5. Independently observed delegated Stop entry, return and error.
6. Production operation return, returned key count and context error identity.
7. Existing cleanup, native Keys closure and owned-child completion observations.

The framework's five-second deadline remains unchanged. Measurement must not replace its context, insert a timeout,
delay Stop or intercept successful results.

Use SDK trace callbacks to retain API subject, consumer identity where present, timestamp and bounded current-goroutine
stack. Callbacks must not block on logging, gates or network I/O. Store records in a bounded synchronized buffer;
overflow, truncation needed for attribution or concurrent access errors fail the diagnostic.

Separate affirmative deletion-origin evidence for:

- Framework-delegated Stop.
- Native lister forwarding goroutine's deferred Stop.
- Context-completion unsubscribe.
- Asynchronous ordered-consumer recovery.

Keep unmatched or ambiguous origins unknown. Request timing, subject equality or overlap with Stop is insufficient
attribution. Trace reports request entry and successful responses; it does not expose every error return.

## Two-case matrix

Reuse the retained fixture: one pinned NATS container, 1,024 exact matching keys and one fresh child connection per case.
This differs from the incident's 5,000-key file-backed workload and is explicitly mechanism measurement, not incident
reproduction.

| Case | Schedule | Required result |
|---|---|---|
| Transparent control | Unmodified native delivery | Exact seeded set, nil operation error, complete phase evidence and checked cleanup |
| Default-deadline collection | Retained withholding after a real key is consumed; require both native blocked-send witnesses while the framework context is live | `errors.Is(err, context.DeadlineExceeded)`, nil returned keys, observed Stop outcome and checked test-owned cleanup |

Retain the existing causal gate; do not substitute elapsed time for witnessed native blockage. A failed control or unmet
gate stops the pass. Do not repeat until the desired schedule appears.

Do not add construction expiry, transport interruption, ordered-recovery forcing or synchronous-delete-delay injection
in this pass.

## Ownership and budgets

Reuse the corrected lexical owner and sole Cmd.Wait owner. Install finalizers immediately after acquisition. On every
exit, release fixture gates, cancel continuing test work, join test-owned goroutines and report cleanup failure
separately from the primary result.

Each child creates no descendants. Parent observes events alongside EOF and unexpected exit, terminates an over-budget
child and joins its existing Wait owner. Mutable output is inspected only after that join.

| Phase | Maximum |
|---|---:|
| Parent fixture setup/seeding | 20 seconds |
| Child process startup to entry event | 5 seconds |
| Child work, including connection/acquisition | 18 seconds from child entry |
| Child terminal cleanup | 8 seconds shared |
| Child final reporting/normal exit reserve | 2 seconds |
| Parent kill trigger | 35 seconds after Start |
| Reserved parent Wait join | 5 seconds |
| Complete admission allowance per child | 40 seconds |
| Canonical substrate teardown | Existing independent 15-second close and 15-second container bounds |
| Parent test alarm | 180 seconds |

The child's startup, work, cleanup and reporting allowances total 33 seconds, leaving two seconds before the parent
kill trigger and a separate five-second Wait reserve.

Admit a child only if its entire 40-second allowance fits before a cooperative cutoff 120 seconds after parent entry.
Reserve the final 60 seconds for substrate teardown and reporting. Stop admission on unresolved ownership.

Run once through the canonical host-locked integration runner, with explicit opt-in, `-race`, a narrow test selection
and `-timeout=180s`. Coordinate an available host slot first. No other resource-intensive experiment runs alongside it.

Allow at most five minutes of aggregate local execution for focused diagnostic checks and this single native pass.
If setup, building or host availability prevents that budget, report the limitation instead of launching another pass.

## Evidence and completion limits

Keep these outcomes separate:

- Framework return.
- Native Stop return and error.
- Native Keys closure.
- Witnessed native goroutine presence or disappearance.
- Client.Close and native CLOSED status.
- Test-owned goroutine joins.
- Child exit and parent Wait completion.

Public KeyLister exposes neither the initial watcher marker nor a watcher join handle. Keys closure and stack
disappearance do not establish that callback join. Report native completion as unproven wherever the available
observations cannot establish it; child containment is not a substitute claim.

Capture actual Go toolchain, SDK hashes, server digest, source revision and commands. CI used Go 1.26.8; older local
evidence used Go 1.26.4. Do not conflate them.

## Focused verification

Run the retained focused ownership/gate checks applicable to the copied source and small checks for the added recorder's
bounded storage and origin-unknown handling. Preserve the independent Stop invocation oracle.

Existing mutation evidence may be cited only where the observer, asserted behavior and cleanup mechanism remain
unchanged. If those parts change, select one focused sensitivity experiment, preserve `cp` backup/checksum restoration
and prove fixture cleanup after its intended failure. Do not repeat a full native matrix merely to obtain another green
result.

Named schedules and the exact-set oracle suffice; no randomized PBT generator or new testing framework is required.

## Decision after this pass

| Observation | What it informs |
|---|---|
| Construction is prompt; collection reaches the deadline; Stop is prompt | This retained schedule does not explain the extra interval |
| Framework-owned Stop consumes substantial time with corresponding native request evidence | Supports a narrower next question about synchronous deletion/return timing |
| Only asynchronous deletion is observed | Does not attribute synchronous Stop delay |
| Construction consumes the operation window | Directs the next investigation toward acquisition |
| Trace ownership is ambiguous or a required gate fails | Measurement remains inconclusive; no repair authority |
| Native work remains after framework return | Requires explicit ownership investigation, distinct from initial expiry |

Neither a healthy control nor synthetic backpressure explains the incident's initial five-second expiry. This pass may
narrow the next experiment; it cannot promise historical root cause or authorize closing #1421.

No production spec delta is proposed. Existing graph-index typed-deadline/nonpartial-result requirements and
testing-policy ownership rules remain authoritative. Stop after the single pass and independent evidence review.
