# Design: settlement after durable effect

## The design this layer implements is not carried here

The accepted design for the whole restart-safety programme lives on the Codex integration branch at
`origin/codex/gh1146-agentic-loop-restart:openspec/changes/agentic-loop-restart-safety/design.md`, read at
`79b0f29f`. Its inventory, reconciliation and review records sit beside it in that directory. That directory is
deliberately not carried onto `main` by this layer: it is the programme's working evidence, several times the size
of this slice, and most of its decisions govern recovery work that lands later. This file records only the
decisions a reader of *this diff* needs, and cites that document for the rest.

## D1 — the decision point moves, the vocabulary does not

#759 established that a callback returns a `DeliveryDecision` and the binding performs the terminal method. This
layer changes only *when* the callback is entitled to return Ack: after the lane's durable transition or defined
refusal has committed and every required PubAck has returned. The four decisions keep their #759 meanings
(`natsclient/delivery_settlement.go`). Nothing in the wire format, the payload registry or the subject grammar
moves.

## D2 — `SettleDelivery` is the settlement half of the typed owner, with no work half

`ConsumeDeliveryWithHeartbeat` owns work, lease renewal, join and settlement together, which is right for a lane
whose work can outlive an ack interval. The ten non-heartbeat lanes do not need a lease; giving them one would put
a heartbeat goroutine behind every cancel signal. `SettleDelivery(msg, decision, cause)` is therefore the same
interpreter with the work half removed: it validates the closed tuple and attempts at most one terminal method, and
it explicitly does not invoke work, read payload or metadata, create a context, derive a deadline, send a heartbeat
or own a consumer. The caller joins its own work first. The alternative — an exported work-owning no-heartbeat
adapter — was implemented, measured and reverted on the integration branch (`bef0b372`…`16c31f89`); it nets to
zero code and is not carried.

## D3 — the fatal latch is per LANE, and JetStream keeps delivery authority

`deliveryLaneAdmission` is **one per lane**, not one per component: each `setupConsumer` call constructs its own
(`agentic-loop/component.go:1066`, `:1088`; `agentic-dispatch/component.go:547`, `:588`, `:618`, `:659`,
`:702`; `agentic-model/component.go:408`; `agentic-governance/component.go:509`). It is
a mutex-guarded bool plus a one-slot channel. On the first result whose `OwnerStopRequired()` is true it closes that
lane's admission, records the cause on the component's existing health surface exactly once, and wakes a per-binding
observer that drains that exact consume handle. Health is component-wide and the drain is lane-exact, so a fatal on
one lane leaves `Healthy=false` while the other lanes keep admitting work. That is deliberate — the process is
degraded, not dead, and an operator reads the condition — but it is the fact to know before reasoning about the
latch.

It is not a circuit breaker and not a retry policy: it prevents *new local work* after ownership control becomes
unsafe. Redelivery, backoff and max-delivery remain JetStream's. Later fatal results neither overwrite nor recount
the first cause, so health names the cause that actually broke the lane.

## D4 — the lease is validated, never repaired

Heartbeat and delivery-count validation happens before consumer allocation and returns a typed error naming the
observed values and the ceiling. It does not truncate BackOff, lower the heartbeat, or raise `max_deliver` to make
a bad configuration work — a silently repaired lease is the failure mode this requirement exists to stop. The
ceiling is observed from the consumer's own configuration (half the shortest positive BackOff, or half the
effective AckWait when BackOff is empty), not predicted by the caller.

## D5 — "this process does not hold that loop" is one question with one answer

Six call sites asked it in different words and all answered it the same wrong way: an input naming a loop absent
from process memory was a settled-drop, acknowledged and gone. Memory cannot distinguish a loop that finished from
one this process lost, and those settle in opposite directions. `classifyMissingLoop` reads the loop record and
answers stale / live / unknown; a failed read is never stale. It performs no recovery — no reconstruction, no
routing re-registration, no reads of retained requests — because making the live case actually recoverable is L4's
(#1330) subject. The two recovery grammars are `<loopID>:req:<n>` and `<loopID>:tool:<n>`, and the extractor
requires a framework-minted token so a provider-authored call ID is never used as a bucket key.

## D6 — `runWithBudget` is synchronous on purpose

It was a goroutine-plus-select; it now calls `fn(bctx)` directly and reports `bctx.Err() != nil`. The old shape
returned while the work goroutine was still live, so the callback could settle before its own graph write finished —
the exact thing this layer exists to stop. The cost is that the budget binds only a callee that honours
cancellation. That is not an assumption: graph dependencies are lifecycle participants under ADR-049, which
*requires* cancellation to be honoured, and a component that ignores it fails lifecycle review rather than being
defended against here. Declared residual below.

## D7 — a handler result that fails at either phase is Quarantine, not Retry

`persistHandlerResult` stamps the loop entity and then publishes, and both phases are commit-unknown for different
reasons.

The publish phase is the obvious one: it emits N results in a loop, and a failure on result k leaves 1..k-1 already
durable on the stream with no record of how far it got, so redelivering that callback republishes them — including
`tool.execute` messages whose executors are running.

The stamp phase looked safe and is not, which round 1 caught (finding 1). The write itself is a whole-entity upsert
(`component.go:2314`, `:2235`, `:2267` write the full record, not a delta), so replaying the WRITE is harmless. The
delivery is what cannot be replayed. The handler has already moved the loop in memory before `persistHandlerResult`
is called, so a redelivered model response meets `HandleModelResponse`'s terminal guard
(`handlers.go:1179-1185`), which returns an empty result: no completion record, no publication. The second attempt
writes the loop key, publishes nothing and ACKs, and the completion the first attempt built is gone with the
delivery that carried it. Idempotence of the write was never the question; reachability of the result was.

So both phases wrap `errs.WrapFatal` — `"handler result state has unknown durability after the loop was already
mutated"` and `"published results have unknown durability"` — which the heartbeat work func maps to
`DeliveryDecisionQuarantine` **before** the `PermanentDeliveryError` and Retry arms. Quarantine terminates that
delivery, latches the owner's health fatal and drains the lane: an operator sees a stopped lane naming the cause
rather than a silent duplicate storm or a silently missing completion. That is deliberately the blunt answer. L4
(#1330) relaxes it to identity-based replay — once a redelivery can reproduce the original result and each
published result carries a deterministic identity, both phases can go back to Retry.

The failure branch of the stamp phase also had a hole, which round 3 found: it stamped graph triples and left
`COMPLETE_<loopID>` unwritten, because `persistFailureState` had exactly one caller — `publishFailureEvents`
(`component.go:1700`) — and the `settleFailedToolResult` → `persistHandlerResult` route never enters it. The branch
now mirrors the completion branch: record, then triples, error propagated. The census that says this cannot
double-write or double-publish is in `tasks.md` 8e.2 — of the four results that carry a `FailureState`, exactly
one reaches `publishFailureEvents`, and it is the one that never reaches `persistResultState`.

The same rule reaches the two paths that produce a terminal state without going through this function. A loop's
business failure (`handleLoopFailure`) is durable only once its failed loop state, its `COMPLETE_<loopID>` record
and its failure events have committed, so that function reports rather than returning void — with one carve-out: a
loop that could not be transitioned at all wrote nothing, so it is an ordinary Retry and the redelivery is settled
from the loop record. The tool-result handler-error branch (#1343) persists a terminal result through
`persistHandlerResult` and settles on that write; its non-terminal errors quarantine, except a cancellation the
handler can prove preceded every mutation, which stays a Retry so a clean shutdown cannot latch a false
`delivery ownership lost`. "Preceded every mutation" is a marker, not an error class: `HandleToolResult` checks
its context three times, and only the first — `handlers.go:2209`, before it touches anything — returns
`errCancelledBeforeMutation`. The two inside `handleToolsComplete` (`:2472`, `:2555`) run after
`StoreToolResult`, `RemovePendingTool`, `IncrementIteration` and `GetAndClearToolResults`, so they are partial
effects and take the same Quarantine as any other.

The command lane's post-effect response failure splits, and the split is the whole rule stated twice.
`/cancel` publishes its signal at `commands.go:179` and then builds its success response, so a failed response
publication there is a post-effect failure like the task lane's — but only the **named** form,
`/cancel <loop_id>`, can prove its redelivery effect-free. For that form the gate re-reads THAT loop from merged
facts, finds it terminal once the first signal took effect, and answers "Loop … has already settled" without
publishing anything (`commands.go:136-148`); a second signal that does race the loop's own settlement is dropped
effect-free by the loop's cancel owner, which is the loop delta's scenario *A cancel signal names a loop that
cannot be cancelled* (`specs/agentic-loop/spec.md`, requirement *A loop absent from process memory is settled from
its record*). The user has received nothing, so the redelivery is what actually gets them their answer, and unlike
the task lane there is no new identity to mint. Retry, named rather than defaulted.

The **bare** form cannot make that argument, and round 2 of the owner's cross-agent review is where it broke.
`handleCommand:941-951` resolves an omitted target from the tracker, and `GetActiveLoop`
(`loop_tracker.go:204-226`) prefers the channel's loop only while it is non-terminal, then falls back to the
user's most recent one. The effect this delivery had — loop A now terminal — is therefore the very thing that
makes the redelivery resolve to a different live loop B and cancel it; A's terminal guard cannot protect B. The
burden of proof is on the Retry and the message cannot meet it, so the resolved-target form is `errs.WrapFatal` →
Quarantine (`component.go:985-1010`). The alternative — a durable "selected target" record written on every bare
command so a replay could recover it — adds a write to the common path for a rare one; L4 (#1330) is where
identity-preserving replay makes it unnecessary. The rule the two arms share: a delivery Retries only where the
redelivery is provably effect-free, and a target the message does not carry is not provable.

`TestIntegrationPublishedCancelWithFailedResponseRetries` holds the named form (the Retry and the effect-free
redelivery); `TestIntegrationBareCancelWithFailedResponseQuarantines` holds the resolved form with two live loops,
and its redelivery is conditional on the decision, because that is what production does with each.

## D8 — the `jetstream-consumer-policy` MODIFY corrects a requirement L0 just made current

L0 (#759) squash-merged as `f4d66934` and its archive promoted *shared settlement remains stateless and
heartbeat-specific* into the live capability spec, where it now reads "The typed path SHALL use only a private
terminal-method executor. The no-heartbeat interpreter SHALL remain private. #759 SHALL add no exported pull
settlement operation and SHALL not modify OTEL production settlement."
(`openspec/specs/jetstream-consumer-policy/spec.md:644-645`; the requirement heading is `:642`). This change
exports a settlement operation, so that sentence is current truth and contradicts the tree until the MODIFY lands
with it.

The MODIFIED block was written while L0 was still unarchived and the target existed only in L0's delta; it now
targets the live spec, and the requirement's single scenario — *terminal execution is shared privately*
(`:647`) — is restated verbatim, which is what openspec 1.7.0 requires of a MODIFIED block. Line numbers here are
of a file that will move; the requirement heading is the durable handle and the pins are meant to be re-derived
with `sed -n`.

What the MODIFY preserves is the part that is still true and still load-bearing: the interpreter stays private.
What it corrects is the count — one exported settlement operation, reachable through two entry points that differ
only in the retry policy, owned by #1327 and not by #759.

## Not in this layer

`agentic-model`'s request lane still returns Ack from its callback before the response PubAck returns
(`processor/agentic-model/component.go:396-397` — `handleRequest` is called for effect and
`DeliveryDecisionAck` is returned unconditionally). That is the same defect class this change exists to fix, and it
is L2's (`af829616`) subject, not a gap here — L1 touches agentic-model only for its heartbeat lease floor and its
delivery-owner health latch, and this change's `specs/agentic-model/` delta is scoped to exactly those two. Landing
the request-lane half here would split one component's settlement across two changes.

## Declared residual — the cancel signal's own publication is L2's

This change's `agentic-dispatch` delta originally required PubAck for the **cancel signal** alongside the task,
approval-response and user-response publications. It does not hold at this layer's head:
`processor/agentic-dispatch/commands.go:179` publishes the signal with `c.natsClient.Publish` — core NATS, no
JetStream context, no PubAck (`natsclient/client.go:858-864`) — and this change does not touch that file
(`git diff --name-only origin/main..HEAD -- processor/agentic-dispatch/commands.go` is empty). The clause is
struck rather than satisfied here, because spec follows code at each layer and an archived spec asserting a gate
the tree does not have is worse than a recorded gap.

Its home is **L2 `23f7eb08`** (`fix(agentic): require durable cancel settlement`), which changes exactly that line
to `PublishToStream` and adds the publication-semantics tests. The delta clause moves with it.

Two consequences worth naming so they are not rediscovered:

- The sentence "No void, log-only, or core-NATS publication failure SHALL become ACK" stays, and `commands.go:179`
  is the **only** core-NATS publication left in dispatch — every other required publication on these lanes already
  goes through `PublishToStream`. L2 is what makes that sentence literally true of the whole component rather than
  forward-looking. The clause is not weakened here; its one outstanding referent is named.
- A cancel publish that fails today is not silent: the error returns at `commands.go:180` and
  `component.go:963-974` converts it into a `ResponseTypeError` user response, which is itself PubAck-gated before
  the `UserMessage` Acks. What L1 cannot promise is the *success* case — a core-NATS publish to a subject no
  stream is capturing returns nil, the user is told "Cancel signal sent", and nothing was durably enqueued.

The loop-side cancel clauses in `specs/agentic-loop/spec.md:10,32,109-113` are unaffected and stay: they govern how
the loop **handles an admitted cancel signal** — its cancellation state, `COMPLETE_<loopID>`, and the terminal
event's PubAck before source ACK — and claim nothing about how dispatch published it.

## Declared residual — task intake is the one loop input class this layer does not convert

`handleTaskMessage` still answers five failures with a log line and an ACK: an undecodable envelope
(`component.go:1279-1282`), a payload of the wrong type (`:1284-1288`), a `HandleTask` failure (`:1304-1318`), a
failed first publication (`:1387`) and a failed loop-state write (`:1390`). The lane is classified through the same
permanent typed heartbeat owner as response and tool-result, and its birth-failure and transient-lineage paths do
settle on their durable effect, but these five do not.

They are not a classification change, which is why they are recorded rather than fixed here. A redelivered task
deduplicates against the loop its first delivery already created — `HandleTask` returns the existing loop with
`Created=false`, and `:1320-1327` then acknowledges without publishing — so returning Retry from these branches
would produce a redelivery that silently drops the task instead of resuming it. The lane needs resumable intake
first. The mechanism already exists in miniature: `rememberPendingTaskResult` / `pendingTaskResult` retains the
result for the transient-lineage case and `:1320-1331` resumes it, which is exactly the shape the other four need.

The loop delta names this exemption as a requirement rather than leaving it to the absence of a scenario, because
the requirement above it reads as covering the class. It is tracked as **#1345** (beta.163,
`class:swallowed-degrade`, placement candidate L4); sizing and placing the conversion is the owner's.

## Declared residual — a cancelled tool result after mutation rides to its timeout

The R3 ruling costs something and the cost is named here rather than discovered. A clean stop that cancels
`HandleToolResult` after `StoreToolResult` quarantines that delivery: the lane latches, the tool result is gone,
and the loop rides to its own timeout instead of continuing. That is the deliberate trade — a lost iteration
against a silently duplicated one — and L4 (#1330) is what relaxes it, by making the replay reproduce the
interrupted operation rather than re-running from a loop that has already advanced.

Two facts about the topology decide how bad the residual is. They are recorded as FACTS, not as gates; neither
changes the ruling:

- **(a) No durable write is reachable inside `HandleToolResult`.** `loopsBucket` is a `Component` field and
  `handlers.go` never names it; every `Put` site is in `component.go` (`:2267` completion, `:2298` failure,
  `:2322` cancellation, `:2349` loop state) and each is called by a `Component` method after the handler has
  returned. So the mutation a post-mutation cancellation leaves behind — stored tool result, removed pending tool,
  incremented iteration, drained results — is entirely in-process.
- **(b) The loop manager is never reloaded from KV.** The `MessageHandler` and its `LoopManager` are constructed
  once in `NewComponent` (`component.go:297`); `Start` and `initializeKVBuckets` do not restore loops, and the
  package has no restore path at all (`restoreLoops`, `rehydrate`, `loadLoopsFromKV` match nothing). So a
  Stop/Start inside one process reuses the mutated in-memory state, and a replay meets the advanced loop.

Together: the interrupted state is in-memory and survives a restart of the component within the process, so the
replay Codex measured — iterations 0 → 1, zero publications, then terminal `max_iterations` without the request
ever being issued — is the real behaviour, not an artifact of the probe. After a process restart the in-memory
state is gone and the durable record is whatever the last `persistLoopState` wrote, which is a different recovery
problem and also L4's.

The cancellation source is not only shutdown: `natsclient/delivery_settlement.go:345-380` derives the work context
per delivery and cancels it both on `ctx.Done()` and on a failed heartbeat `InProgress` (`:366-373`), the second
in a process that is still running.

## Declared residual — identity-preserving task replay is L2's

R2 made `handleTaskSubmission`'s post-PubAck acknowledgement failure fatal (`component.go:1145-1168`), which is
the bluntest answer in this change: the task is on the stream and its user response is not, so the lane stops
rather than replaying a delivery that would mint a second identity. The blunt part is not the classification, it
is that the alternative does not exist yet — a redelivery mints a fresh task UUID at `:1093` and publishes it with
no deduplication id, and with `auto_continue=false` it creates a second loop as well, so Retry means "accept this
work twice".

Its home is **L2 `15825335`** (`fix(agentic-dispatch): recover task identity on redelivery`). Once a redelivered
`UserMessage` recovers the task identity its first delivery minted, the publication is idempotent downstream and
this branch may be relaxed to Retry there. Recorded here so the relaxation is a decision someone takes with the
reason in front of them, rather than a Quarantine that looks permanent because nothing says otherwise.

## Declared cost

- The nine shipped `configs/**` fixtures are edited in lockstep with the new floor, and a test holds them to it.
  A downstream deployment carrying its own flow JSON with `heartbeat_interval: 60s` or `max_deliver: 1` is refused
  at config validation. That refusal is the point; the migration is a two-key edit and the error names both values.
- `agentic-model`'s `agent.request` port gains explicit `AckWait`/`HeartbeatInterval` defaults where it previously
  inherited the component default. This is a behaviour change for any deployment that relied on the inherited
  values, and is covered by `TestPostFoundationBAgenticModelHeartbeatPolicyAmendmentIsExact`.
- Process-replacement *recovery* remains unimplemented after this layer. The `agentic` E2E tier's recovery stages
  exercise it; their state on this head is recorded in the landing PR rather than predicted here. Every
  `loopPresenceLive` classification added here is a Retry that stays a Retry until L4 (#1330) gives the delivery
  somewhere to go; the bounded MaxDeliver and 30s BackOff are what keep that from being a hot loop in the meantime.
- `runWithBudget` bounds a cooperative callee only. A graph writer that ignores cancellation blocks past the budget
  and, on the 30s-AckWait non-heartbeat lanes, is redelivered while the first attempt still runs. The guard is
  ADR-049 lifecycle review, not code here. Measured only for the cooperative case
  (`TestRunWithBudgetWaitsForCooperativeWorkToJoinAfterCancellation`).
- `graphWriter.WriteLoopCompletion`, `WriteLoopFailure` and `WriteLoopCancellation` return nothing, so their own
  write failures are swallowed inside the writer and the callers can only observe `ctx.Err()` or a budget timeout as
  a proxy. The cancel path's "cancellation graph write has unknown durability" therefore fires on cancellation, not
  on a graph-gateway rejection. Making those three return an error is a signature change across the writer and three
  call sites; it is deliberately not in this layer's scope and is recorded here rather than filed, per the File
  ritual, because it is a residual of this layer's own boundary.
- `agentic-model` has no panic wrapper equivalent to `runLoopDeliveryWork` / `runDispatchDeliveryWork` /
  `runGovernanceDeliveryWork`, so a panic inside `handleRequest` still escapes to the NATS callback. Model request
  settlement is L2's subject (`af829616`), and the wrapper belongs with it rather than half-landed here.
- The delivery-owner trio (`deliveryLaneAdmission`, `newStreamConsumerBinding`, `observeDeliveryLane`) is now
  spelled identically in four components' own `delivery_owner.go` — governance gained one in this round, replacing
  the inline locals that made it a fifth spelling. Hoisting the trio into a shared internal package would move
  ~400 lines across four components and is far past the bound this round set for the refactor, so the duplication
  is recorded, not removed.
