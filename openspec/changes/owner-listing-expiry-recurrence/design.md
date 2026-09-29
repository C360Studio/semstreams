# Bounded causal experiment: listing expiry and native Stop delay

Status: proposed experiment only. Production repair requires measured evidence and separate design review.

Accepted inventory: `3d769265f973f7dff967256b6d881b4053569441:openspec/changes/owner-listing-expiry-recurrence/inventory.md`, SHA256 `6f3479feb76d24d9768fe05abd815df2187b9baf6c89ea01e0fa15d1d2ecfc5a`. Preserve it unchanged.

## Question and alternatives

The initial five-second operation expiry and the approximately five additional seconds may have different causes. The experiment must distinguish native construction, collection and synchronous Stop time, while identifying independent asynchronous deletion.

| Option | Evidence gained | Cost or limit |
|---|---|---|
| Do nothing | Preserves current uncertainty | Cannot support a repair |
| Add phase/SDK tracing and run one unchanged control | Measures ordinary phase attribution | A healthy run cannot explain the recurrence |
| Extend the retained native experiment with a bounded fault matrix | Separates construction expiry, collection expiry and native deletion delay | Adds private diagnostic instrumentation and controlled faults |
| Change production listing or timeout behavior immediately | None establishing cause | Premature; rejected |

Recommend the bounded diagnostic extension. It reuses the retained process, output, Stop-observer and finalizer design. It does not rerun a full suite or stress the owner-load harness.

## Measured seams and limits

The following seams already exist:

| Evidence | Available seam |
|---|---|
| `natsclient/kv.go:55–65` | NewKVStore accepts a real KeyValue or a private delegating decorator |
| `natsclient/kv.go:537–595` | Unchanged production listing, five-second child context, partial-result rejection and deferred Stop |
| `natsclient/client.go:205–209` | GetConnection exposes the existing native connection |
| SDK `jetstream/jetstream_options.go:33–37` | WithClientTrace installs request/response callbacks |
| SDK `jetstream/kv.go:887–898` | KV legacy management propagates those trace callbacks |
| SDK `js.go:3543–3561` | Request trace executes synchronously before RequestWithContext; response trace runs only after successful response |
| Retained diagnostic, `kvDiagnosticBucketDecorator` and `kvDiagnosticStopObserver` | Exact native construction and delegated Stop boundaries |
| Retained diagnostic, `kvDiagnosticCaseOwner` | Lexical cancellation, gate release, bounded joins and connection cleanup |
| `natsclient/client_close_integration_test.go:15–105` | Separate native drain/closed observations and actual callback-completion evidence |

Trace does **not** report request error returns. Public KeyLister exposes neither its underlying watcher's initial marker nor a watcher join handle. A closed Keys channel, vanished stack, Stop return and connection closure remain distinct observations.

The SDK provides CustomDialer, but the measured framework surface has no `WithNATSOptions` symbol. The experiment will not add a production connection option or manually replace Client internals.

## Allowed implementation

Use one temporary, opt-in integration diagnostic under `natsclient/`, plus its focused proof and retained evidence. Reuse the archived diagnostic's corrected ownership behavior; copied or changed code requires current proof.

Keep production source, SDK source, module selections, normal tests, graph-index workload and runner unchanged. Retain final diagnostic source as an archive artifact rather than silently installing recurring native coverage.

Each child uses production Client.Connect and Client.NewKVStore. Construct a traced native JetStream handle on `client.GetConnection()`, acquire the real bucket, and pass its private decorator to NewKVStore. Do not replace the connected client's own JetStream handle.

For controlled transport faults only, the child connects through a private, single-connection loopback byte relay to its owned NATS fixture. The relay:

- forwards outbound bytes unchanged;
- can hold inbound bytes after an explicit gate is armed;
- preserves byte order on release;
- owns one listener, one connection pair and the two forwarding directions;
- has immediate close/release cleanup and explicit completion signals.

This is a whole-inbound-stream fault, not selective NATS response loss. No protocol parser, subject rewrite, packet corruption, multiplexing, host-wide pause or reusable proxy framework is included. If implementation requires those additions, stop for design reconciliation.

## Observation record

Record monotonic times for operation entry, native construction entry/return, first real-key delivery, framework deadline observation, delegated Stop entry/return, framework return and cleanup completion.

Every record carries child/case identity, filter/bucket, actual toolchain, SDK version/source hashes and server image/digest. CI used Go 1.26.8; prior local diagnostics used Go 1.26.4. New execution reports its actual version without equating environments.

The bucket decorator forwards the exact framework context and filter. It records the received deadline; it neither replaces nor extends it.

The Stop observer records the native call and its error independently of the delivery facade. Stop errors remain observations; they are not substituted for the production wrapper's result.

Trace callbacks capture API subject, consumer identity when present, monotonic time and bounded current-goroutine stack. They do no blocking logging, waiting or network I/O. Store records in a bounded buffer; overflow or truncated attribution evidence fails the diagnostic.

Classify deletion origins only from affirmative stack evidence:

1. Framework observer → native Stop → Unsubscribe.
2. Native KeyLister forwarding goroutine's deferred watcher.Stop.
3. Subscription context-completion unsubscriber.
4. Asynchronous ordered-consumer recovery.

Retain unmatched or ambiguous origins as unknown. An overlapping Stop interval or matching DELETE subject alone cannot establish ownership. Successful response callbacks can be correlated within a witnessed synchronous request call; absent callbacks do not by themselves prove timeout.

In injected collection cases, distinguish native keys received, facade keys delivered and the existing first-key-consumed witness. Do not label Keys-call counts as exact framework collection counts.

## Bounded matrix

One parent-owned pinned NATS container, one static file-backed bucket, 5,000 distinct canonical keys matching the recurrence's predicate-forward filter. One fresh child connection and one measured listing per case; cases run sequentially.

| Case | Controlled schedule | Required distinction |
|---|---|---|
| Transparent control | Relay open; real native lister without delivery withholding | Exact complete seeded set, nil operation error, phase/Stop evidence and checked cleanup |
| Construction expiry | Arm inbound gate at the traced consumer-create request, then keep it closed until the unchanged framework deadline is observed | Construction must return error without a usable lister; record zero admitted collector/Stop-observer calls |
| Collection expiry | Reuse the retained first-key withholding schedule; require both native blocked-send witnesses while framework context remains live | Native construction succeeded; collector received a real key; default deadline returns errors.Is DeadlineExceeded with nil keys |
| Synchronous deletion delay | Same witnessed active collection; arm inbound gating for an affirmatively framework-owned deletion request during delegated Stop | Separate initial expiry from additional synchronous delete/Stop time, including native Stop error |

A RequestSent callback may arm the relay gate but must return immediately. The relay emits a separate "bytes actually withheld" witness; arming alone is not a reached fault.

For the last case, native unsubscribe owners race. If another owner deletes first, record it and report `GATE_NOT_REACHED` for the synchronous framework-delete hypothesis. Do not rerun until the preferred interleaving appears. An asynchronously owned deletion cannot satisfy this case.

Do not manufacture ten seconds by delaying a trace callback or facade.Stop. The additional interval must be observed inside the real native delegation with its independent request authority.

Release each transport gate after the relevant operation outcome or during failure cleanup. A missing outcome is contained by the child deadline, not a guessed readiness sleep.

## Oracles and interpretation

Every expired operation must preserve errors.Is-compatible context failure and nil returned keys. No partial successful snapshot is acceptable. The five-second framework deadline remains unchanged and is checked against the captured child context.

| Observation | Supported conclusion |
|---|---|
| Construction fails before lister acquisition | Controlled loss reached the construction branch |
| Construction succeeds; witnessed collection stalls and expires | Controlled backpressure explains this experiment's initial expiry |
| Framework-owned delete starts during Stop and its real request consumes the additional interval | Demonstrated synchronous return-path extension for this schedule |
| Asynchronous recovery deletion occurs while framework Stop returns promptly | Recovery deletion is independent evidence, not an explanation of synchronous Stop delay |
| Operation takes longer but Stop is prompt | Extra time lies elsewhere; five-plus-five Stop attribution is unsupported |
| Gate, trace attribution or control fails | Inconclusive experiment; no repair authority |
| Native work persists after framework return | Return differs from native completion; further ownership evidence is required |

Synthetic faults identify reachable mechanisms. They do not establish which mechanism caused CI run 36641021599, or what initially delayed its operation.

The experiment cannot expose the native initial-snapshot marker through public KeyLister. Exact-set success proves the control result; injected-case channel closure does not prove complete initial replay.

## Ownership and completion

Install finalization immediately after each acquired resource and before any assertion or spawned work.

Each child owns its operation goroutine, relay directions, delivery facade when present, connection and trace state. Every test-owned goroutine has an explicit done signal and joins under one shared terminal allowance. Cleanup first releases gates, cancels continuing test work and closes blocked sockets when needed; it preserves the primary failure alongside cleanup failures.

Observe separately:

- framework operation return;
- delegated Stop return/error;
- native Keys closure;
- witnessed native goroutine presence;
- production Client.Close result and native CLOSED status;
- test-owned goroutine joins;
- child process exit and parent Wait completion.

Native Keys closure shows the forwarding path reached its closing defer after deferred watcher.Stop. It does not provide a watcher-callback join handle. Stack disappearance is supporting observation, not joining. If native completion cannot be established, report that limit and use owned child exit as containment—not native join proof.

Parent uses the same executable and one narrowly selected child entry point. One goroutine owns Cmd.Wait; finalization is installed immediately after Start. Observe events with unexpected exit/EOF. Read mutable captured output only after Wait joins. Children create no subprocess descendants.

## Budgets and admission

| Scope | Maximum |
|---|---:|
| Fixture setup and seeding | 20 seconds |
| Child work | 14 seconds |
| Child terminal cleanup | 8 seconds shared |
| Parent kill trigger | 22 seconds after child start |
| Reserved Wait join | 3 seconds |
| Four children | 100 seconds total admission allowance |
| Canonical substrate teardown | Existing independent 15-second close and 15-second container bounds |
| Parent test alarm | 180 seconds |

The 14-second work allowance accommodates the hypothesized five-plus-five path plus four seconds for bounded acquisition/observation. It changes no operation deadline. Slow setup or missing gates produce explicit inconclusive failures.

Set the cooperative child-admission cutoff at 130 seconds after parent entry. Admit only when a complete 25-second child allowance fits. Reserve the remaining 50 seconds for substrate cleanup and reporting.

Stop admission after control failure, unmet causal gate, unexpected assertion, lost evidence or unresolved owned work. No retry loops, parallel matrix, repeated full suite or fresh CI run is part of this experiment.

Execute one matrix through the canonical host-locked runner with `-race`, explicit diagnostic opt-in, narrow `-run` and `-timeout=180s`. Serialize with other Docker-backed work. Native matrix plus selected proof/mutation/restoration work has a six-minute aggregate local execution ceiling; exceeding it stops execution for review.

## Focused proof and sensitivity

Before native execution, focused NATS-free checks must establish relay byte preservation, gate release/close on early failure, trace-overflow refusal, origin-unknown handling and bounded Wait ownership. Use channels and finite containment, never sleeps as readiness.

Named schedules and exact-set comparison cover this finite experiment; random PBT histories add no required input class.

Required bounded mutations:

1. Omit facade-to-independent-Stop-observer delegation. The invocation assertion must fail; SDK cancellation cannot satisfy it.
2. Omit one relay completion join while a fixture-owned gate holds that direction. The completion assertion must fail, then independent fixture cleanup must release and join it.

Reuse unchanged prior evidence only where source and asserted behavior still match. Preserve `cp` backups and checksums; mutants must compile, reach the intended assertion and retain successful failure cleanup. Restore and rerun only the affected focused checks. No second full native matrix solely to obtain green.

## Spec applicability and handoff

Existing graph-index deadline/error and nonpartial-snapshot requirements remain authoritative. Existing testing-policy context, causal synchronization, failure visibility and subprocess ownership rules govern the fixture.

No normative production spec delta is proposed for a diagnostic. Retain this accepted experiment, exact source, commands, identities, outcomes and limitations in the change evidence. Any subsequent production repair or new permanent test surface requires its own evidence-based design and applicable delta.

Stop after matrix and sensitivity evidence review. Do not infer historical cause, close #1421, or hand a speculative production repair to implementation.

## Additional seam queries

```text
gopls workspace_symbol -matcher=fuzzy WithClientTrace
gopls workspace_symbol -matcher=fuzzy 'Client.GetConnection'
gopls workspace_symbol -matcher=fuzzy CustomDialer
gopls workspace_symbol -matcher=fuzzy WithNATSOptions
gopls workspace_symbol -matcher=fuzzy ClientTrace
```

The WithNATSOptions query returned zero matches; this is bounded spelling evidence, not a repository-wide absence claim. The other queries resolved the source locations above. No tests, source edits or Git mutations were performed by the architect.
