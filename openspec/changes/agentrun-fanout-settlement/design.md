# #1249 design draft, round 4 — AgentRun milestone fanout on the typed settlement API; removal of `ConsumeWithHeartbeat`

base: 0053183d02669aa8e159bc28b36be256ffb075e2 (`origin/claude/gh1327-settle-after-effect` at round 1). Status: owner
rulings O1–O5 landed 2026-09-18 ("as recommended on all five", comment on #1249); no owner question is open; awaiting the
coordinator's verification pass. Inventory `gh1249/inventory.md`, 162 pins, `inventory-verify` exit 0 from
`semstreams-wt/verify-0053183d`. Every unmarked `file:line` is a BASE pin. Three pinned files differ at L1 head
`c2a9cef6` — `natsclient/delivery_settlement.go`, `processor/agentic-governance/component.go`,
`processor/agentic-loop/component.go` — so every number in them is `base :n (L1 :m)`, the L1 line found by exact-text
match and printed with `sed -n` from that tree (inventory § Adjacent claims). L0 head `759bd596` (requirement anchors
unchanged: consumer-policy `:73`, nats-streaming `:61`).

## 1. Options; the rulings that shape the design

A keep and B mechanical typed conversion — forbidden (owner 2026-09-18; item 6; B still ratifies partial fanout,
`agentrun.go:621`). C whole-fanout idempotent replay — recommended: every attempt runs every handler, settlement is the
aggregate, same identity every attempt, no new durable state. D receipts — the anti-goal (`agentic-tools/outcomes.go:21`).
E arity-1 — breaks exported `AddHandler` (`agentrun.go:559`).

Coordinator rulings (2026-09-18), both departing from the literal "deterministic → Terminate":
- R1 `ErrWorkflowNotRegistered` → Quarantine. `WorkflowName` is a package constant (`agentrun.go:105`), never wire data;
  both roots call `agentrun.Register` before constructing the subscriber (`main.go:334`→`:347`; e2e `:261`→`:272`), so
  it fires for every message on both lanes or for none. Terminate would be silent drop at scale (a WARN per message);
  Quarantine leaves every delivery unacked and redeliverable once composition is fixed, visible in `/health`.
- R2 `ErrEntityNotLifecycleManaged` → bounded Retry, not Terminate, not nil-run. Its doc names it the ADR-049 question-5
  forward-reference case (`errors.go:40`, `:42`), resolved by a later `Manager.Create`. nil-run misreports an entity that
  exists; Terminate drops a documented-transient condition; bounded Retry self-resolves within ≈2 min or becomes a
  counted drop on the exhaustion counter (§ 2.8).

Owner rulings (2026-09-18, "as recommended on all five", recorded on #1249) — these were the open docket; none remains:
- O1 An all-invalid attempt Terminates with WARN + counter; the `:641`/`:646` rows already use that disposition.
- O2 `MaxDeliver 5` and `DelayedDeliveryRetry(30s)` on both lanes, the MAX_DELIVERIES advisory counter as the
  stuck-milestone signal, I6 guarding drift to 0. Reverses round 2's 0: the attempt histogram could not be built (no
  attempt at L0 head) and 0 would have disabled the framework's registered signal (`observer.go:148`, `config/streams.go:187`).
- O3 All four exports: `LoopTerminalEvent.SourceMessageID`, `MilestoneSubscriber.DeliveryFatal() error`,
  `MilestoneSubscriber.RegisterMetrics(metric.MetricsRegistrar) error`, and the counter
  `semstreams_agentrun_milestone_decisions_total{lane,decision,reason}`; no histogram; `milestoneStarter` unchanged.
- O4 The env-gated test-only handler is admissible. **AMENDED 2026-09-22** (owner on #1249, after the placement was
  reopened as a design question and an architect inventory + docket were taken): the ruled root was wrong for this
  tier, because `task e2e:agentic` boots `cmd/semstreams` through Dockerfile target `e2e-process-barrier`, not
  `cmd/e2e-semstreams`. The amended rule, ratified as docket option B: *an E2E-only hook lands in the binary its tier
  boots, gated by that tier's build tag and, where it must stay inert in the tier's other stages, an env var.* The
  probe stays in `cmd/semstreams/milestone_probe_e2e.go`; the rule lands with it (`tasks.md` § 11). The two
  hand-copied roots themselves remain #1301's, sequenced after #1362 and gating the tag.
- O5 #1155's "matching milestone transition" is amended to re-invocation + idempotent effect count (ADR-053 D5).

## 2. Target shape (both lanes; one `DeliveryWork`, two validated policies, two latches, two exact handles)

2.1 Identity. `LoopTerminalEvent.SourceMessageID string` (new), copied at `agentrun.go:586` from `agentterminal.Event`
(`terminal.go:67`). Additive (`…compat_test.go:75`); `agentic-terminal-events/spec.md:247` protects the callback TYPE, not its fields — not MODIFIED.

2.2 Handler done. `OnLoopTerminal` returns nil only after its durable consequence for this identity is committed (or it has
nothing to do). Spec: new capability `agent-run-milestones` in change `agentrun-fanout-settlement` (ADR-053 D6 `053…:188`).

2.3 Decision matrix. `DeliveryWork` at L0 head is `func(context.Context, []byte) (DeliveryDecision, error)` (no attempt
visible to work). Classification is on the AgentRun side: lifecycle sentinels by `errors.Is` first, then `errs.Classify`
(`errs.go:280`: unknown → Transient = bounded Retry under § 2.8). Origin wraps, per site: `agentrun.go:393`, `:405`,
`:436`, `:455`, `:462` and `nats_reader.go:68` ARE the AgentRun side — wrapped `errs.WrapInvalid` (`errs.go:435`; § 7 for
`ResolveRun`'s callers). `pkg/lifecycle/projection.go:114`, `:136` are NOT wrapped: `pkg/lifecycle` is Tier 1 (`tier1-packages.txt:69`), `projectTriples` has three production
callers outside `Get` (`manager.go:599`, `:1034`, `manager_query.go:336`; `Get`'s is `:249`), no sentinel exists, and
the wrap is not the only honest classification — unknown → bounded Retry is what R2 gives a deterministic-for-now
error (cost and alternative: § 6.3). Every attempt runs every handler in registration order under a per-handler recover.

| Input (pin) | Today | Decision (reason) |
|---|---|---|
| decode/normalize fails (`:576`, `:814`) | Term | Terminate (`decode`) |
| `ErrEntityNotFound` (`manager.go:205`), either path (`:637`, `:653`) | run=nil | continue, run=nil (`agentrun_test.go:730`) |
| `ErrEntityNotLifecycleManaged` (`manager.go:244`, `errors.go:42`) | run=nil / WARN+ACK (`:598`) | Retry, bounded (`not_managed`) — R2 |
| `ErrWorkflowNotRegistered` (`manager.go:169`) | same | Quarantine (`composition`) — R1 |
| nil exact reader (`manager.go:198`; `errors.New`, no sentinel) | same | unknown → Retry, bounded: a counted drop (process-wide like R1, but unmatchable) |
| exact-read wrap (`manager.go:207`; reader forwards its class, `exact_entity.go:65`); `ResolveRun` reader I/O (`nats_reader.go:59`) | same / WARN+ACK | Invalid→Terminate, Fatal→Quarantine, else Retry bounded |
| projection failure (`manager.go:250` ← `projection.go:114`, `:136`) | same | unknown → Retry, bounded: a counted drop (2.3 prose) |
| non-`*AgentRun` (`:641`) | WARN+ACK | Terminate (`resolution_type`) |
| no `RunEntityID`, no `LoopID` (`:646-647`; dead — `terminal.go:119` rejects empty `LoopID`) | silent ACK | Terminate, cause `errs.WrapInvalid(errors.New("terminal names no run and no loop"), "agentrun", "HandleEvent", "resolve")` |
| `ResolveRun` grammar/parent/hop/non-string (`:393`…`:462`, `nats_reader.go:68`) | WARN+ACK | Terminate (`resolution_invalid`) |
| handler transient / `ctx.Err()` | ACK | Retry (`DelayedDeliveryRetry(30s)`, as dispatch `component.go:530`) |
| handler `errs.IsInvalid` | ACK | aggregate (§ 2.4, O1) (`handler_invalid`) |
| handler fatal / panic / unclassified | ACK | Quarantine → latch → health (#759 fail-closed) |
| InProgress failure, no handler involved (`delivery_settlement.go` base `:361`, `:366`; L1 `:383`, `:388`) | WARN, lane runs on (`heartbeat.go:113`) | owner stop → latch → health; redelivered after AckWait |
| delivery metadata unavailable (base `:379`, checked `:329`; L1 `:401`, `:351`) | n/a | Quarantine + owner stop → latch → health |
Every non-Ack decision logs once (identity, `loop_id`, `category`, `lane`, `reason`) and increments § 2.7;
`interpretDeliveryWork` (base `:392`, `:396`; L1 `:414`, `:418`) needs cause nil for Ack, non-nil otherwise.

2.4 Aggregate, highest wins: any fatal → Quarantine; else any transient → Retry; else any invalid → Terminate (O1); else
Ack. A handler's own permanent rejection is a durable negative outcome it records and returns nil for. One Retry blocks
Ack for all N and re-runs the other N−1 each attempt; at exhaustion all N lose the delivery and the counter fires (§ 6.1).

2.5 Crash windows. W0 before decode: no effect, AckWait redelivers. W1 after handler k: no Ack; the same `SourceMessageID`
returns; 1..k no-op by 2.2; k+1..n proceed. W2 before `Ack()` and W3 `Ack()` returned locally (unconfirmed evidence; no
`ServerConfirmed`): as W1. W4 quarantined then replaced: unacked; the replacement rebinds and re-runs all — loud, no hot loop.

2.6 Latch and handle owner — AMENDED (owner ruling 2026-09-19 on #1341 Q2: "Amend"). No
`agentic/agentrun/delivery_owner.go`; there is no sixth copy. `agentrun` imports `internal/deliverylane` (#1341; same
module, internal package, precedents `internal/lifecyclecleanup`, `internal/maxdelivery`) and per lane constructs a
`deliverylane.Admission` (`NewAdmission(onFatal, nil)` — the milestone lanes declare no refusal today; wiring
`onRefused` is #1342's scope), a `deliverylane.Binding` around the exact `ConsumeContext`, and runs
`deliverylane.Observe(runCtx, lane.binding, lane.admission, react)` where `react` only logs the lost lane (`lane`,
cause). Recording is `onFatal` — `s.recordDeliveryOwnerFatal`, passed to `NewAdmission` and run synchronously inside
`Latch` before the result is buffered (`deliverylane.go:78-81`), which is what `DeliveryFatal()` reads — and `Observe`
itself drains the exact handle after `react` returns (`:240-241`); `react` is required non-nil (`:231-232`).
`milestoneConsumerOwner` (`agentrun.go:679`, `:683`) stays the SOLE owner of both lanes' bindings (and, through them, both `ConsumeContext`s);
`milestoneConsumerOwner` replaces `complete`/`failed` (`:681-682`) and the drained flags (`:683-684`) with two
`*deliverylane.Binding`; `stop()` calls `Drain()` on both (both-drain-first, `:718`, holds; `Drain` is once-only, so a
lane the observer already drained is a no-op), awaits both `Closed()` (`:727`, `:730`), calls `o.cancel()` (`:743`,
ending the observers' `runCtx`), then joins both `Done()`, which is never nil (`deliverylane.go:216`) — the join #1357's
design § 6 names as the one draft 4 never specified. That join is bounded by the Stop context, not unconditional:
`waitMilestoneLane` selects on the signal or `ctx.Done()` (`agentrun.go:873-879`), so a Stop whose context has already
expired returns just after `o.cancel()` (`:851`) with a non-nil `stopErr` naming the lane signal it outlived. The closure at `:810` becomes
`result, admitted := deliverylane.Consume(msgCtx, msg, policy, lane.admission)` followed by `if !admitted { return }` —
a refusal returns the zero `DeliveryResult`, whose `Err()` is non-nil by construction
(`internal/deliverylane/deliverylane.go:126-132`), so an unguarded `result.Err() != nil` branch would log a refused
delivery as a settlement failure. Every § 2.3 log line and § 2.7 increment is emitted inside the `DeliveryWork`, keyed
on the decision it returns, never on `result`; the only branch after `Consume` logs a settlement-method error
(`admitted && result.Err() != nil && !result.OwnerStopRequired()`, as loop `component.go:1111`). Sequencing: #1329 →
#1341 → #1249; the implementation task list re-pins against the merged #1341 commit. The pre-amendment text (verbatim
copy of `processor/agentic-loop/delivery_owner.go:29-63`, `:74-86`) is superseded, not deleted from history: see round-4
§ 2.6 at the 2026-09-18 ruling.

2.7 Health and metrics. `MilestoneSubscriber.DeliveryFatal() error` (new). `MilestoneService.Health()` override
(`service/base.go:209`) type-asserts `interface{ DeliveryFatal() error }` on its `milestoneStarter`
(`milestone_service.go:21-22` stays `Start` only; `service` is Tier 1, `tier1-packages.txt:89`) and returns
`health.NewUnhealthy("milestone", …)` at all three read sites (`service_manager.go:1302`→`:1722`→`:1736`; `:790`; `:1933`).
Metric: ONE counter `semstreams_agentrun_milestone_decisions_total{lane,decision,reason}`, registered as components
register — at construction via `metric.MetricsRegistrar.RegisterCounterVec` (`registry.go:216`; `component_manager.go:1208`;
dispatch `component.go:185`) — NOT `Service.RegisterMetrics`, which nothing calls (`base.go:389`;
`storage_observability.go:248`). Shape: additive `RegisterMetrics(r metric.MetricsRegistrar) error` on the subscriber;
vec built in the constructor (`agentrun.go:521`), so unregistered increments are local no-ops. Wiring: one line per root,
`cmd/semstreams/main.go:347`, `cmd/e2e-semstreams/main.go:272` (`metricsRegistry` in scope `:170`, `:159`). Cost: two
hand-copied roots (#1301); semteams' root (`main.go:939`) is silent until it adds the call. No histogram (L0 `design.md:309-310`).
The `reason` label set is CLOSED at ten, one per § 2.3 row, and a later label is a spec change, never a rename:
`decode` (undecodable bytes), `not_managed` (R2), `composition` (R1), `resolution_type` (non-`*AgentRun`),
`resolution_invalid` (grammar, parent-type, hop bound, non-string value, and the no-run-no-loop guard),
`resolution_fatal` (a Fatal-classified resolution failure — in production, graph's exact-response error
`exact_entity.go:97`), `resolution_transient` (the unclassifiable remainder, bounded by `MaxDeliver`),
`handler_invalid` (O1), `handler_transient`, `handler_fatal` (panic, `errs` Fatal, or unplaceable).

2.8 Consumer policy (O2). Both lanes keep `MaxDeliver 5` (`agentrun.go:832`), `AckWait 30s` (`:833`),
no BackOff, heartbeat 10s (ceiling 15s, `delivery_settlement.go:191`, unchanged at L1), semantic
`DelayedDeliveryRetry(30s)`. The fifth drop fires MAX_DELIVERIES →
`semstreams_nats_max_delivery_exhaustions_total{consumer="agentrun-milestone-complete"|"…-failed"}`, the stuck-milestone
signal. Siblings: dispatch terminal lanes `0` by a stated choice, "unlimited,
retention-bounded settlement" (`component.go:575`, `:684`, `:688`; bound = eviction rule
`agentic-terminal-events/spec.md:242`); tools and loop take the port default 3 (`port_jetstream.go:130`, tools `:413`;
loop `validateLoopRetryPolicy` base `:1129`, L1 `:1154`; auto-floor deleted at `657c4734`). Dispatch's 0 rests on an
identity-keyed publish with an eviction bound; AgentRun's outputs are adopter side effects with none.

2.9 Cancellation: the typed API joins; `ctx.Err()` from a handler is transient → Retry; no legacy 5s NAK
(`heartbeat.go:122`). Unchanged: serial per lane (`stream.go:524`), stream-absent skip (`agentrun.go:863`), `Mint`.

## 3. Invariants (spec home: `agent-run-milestones`)

I1 Acked only on an attempt where every registered handler returned nil. I2 every attempt of one delivery presents the
same `SourceMessageID` to every handler. I3 a quarantined delivery is never Acked/Naked/Termed by this owner; its lane
admits no later work; `DeliveryFatal()` is non-nil. I4 the aggregate is a pure function of the ordered outcome list (one
rapid property). I5 each non-Ack decision: one identity-carrying log line, one counter increment. I6 `MaxDeliver` finite
on both lanes. I7 only deterministic AND matchable failures (decode, identity, `resolution_type`, `resolution_invalid`)
Terminate on first sight; not-managed and unclassifiable errors retry under the finite ceiling.

## 4. Removal of `ConsumeWithHeartbeat`; Tier 1; spec deltas

- Caller set at the rebase base: re-run `git grep -n -E 'ConsumeWithHeartbeat\(' -- '*.go' ':!**/*_test.go'` first;
  expected `agentrun.go:812` only (true at L1 head; six on `origin/main` incl. `consume_durable.go:39`). Else: stop.
- Delete the function (`heartbeat.go:79`) and `nonCancellationWorkError` (`:37`); delete the `Deprecated:` notice (`:75`)
  IF present at the rebase base (present at base and L1 head, absent at L0 head). Keep `ErrHeartbeatFailed`,
  `PermanentDeliveryError`, `TerminateDelivery`; rewrite `:18` to "a binding maps it to `DeliveryDecisionTerminate`".
- Ratchet: flip exact-declaration (`consumer_policy_callsite_test.go:426`) and exact-caller-set (`:444`) to "absent
  everywhere, zero references" in the `NewDurableHandler` retirement shape (`:395`); keep the surface guard (`:423`).
  Delete `heartbeat_test.go` (407) and `heartbeat_integration_test.go` (133) after porting any claim without a twin.
- Tier 1. `natsclient` is frozen (`tier1-packages.txt:57`; `agentic/agentrun:40`). `scripts/api-compat.sh` has NO
  allowlist, waiver, or exemption (`grep -i 'allow|waiver|exempt'` → 0); `task api:compat:report` at `b7ce8727` lists 15
  incompatible Tier 1 packages against `v1.0.0-beta.162`, `natsclient` (`NewDurableHandler: removed`) and
  `agentic/agentrun` (`EntityIDPattern`, `Mint`) among them; this layer adds one line, `ConsumeWithHeartbeat: removed`,
  under the already-counted `natsclient` and only compatible additions under `agentic/agentrun`, so the package count
  does not move. The commit is still `!`; the posture is still ADR-106's pre-RC descending count. CI runs report mode
  (`ci.yml:238`, `taskfiles/apicompat.yml:10`): exit 0 with the count (`:33`, `:190`); strict exits 1 (`:193`). A
  counted Tier 1 break, not a red — ADR-106 expects the pre-RC count non-zero and descending (`106…:78`; freeze `:50`).
  Declaration: `refactor(natsclient)!: remove ConsumeWithHeartbeat`, named in PR body and `tasks.md` with #759's ruling;
  e2e agentic tier green before merge (`02-e2e-tests.md:299`).
- Spec deltas in `agentrun-fanout-settlement`, targeting L0's HEAD text as L1 does (`5ca936ac`, `design.md` D8). (1)
  `jetstream-consumer-policy` MODIFIED "semantic heartbeat settlement has one permanent exported surface" (L0
  `:73-103`): `:78-79` → "`NewDurableHandler` and `ConsumeWithHeartbeat` SHALL NOT exist or have an alias"; `:82-84` →
  "The caller ratchet SHALL assert zero declarations and zero references"; ALL THREE scenarios restated — "public
  surface at this layer" (fourth bullet → "`ConsumeWithHeartbeat` is absent: no declaration, alias, or production
  caller"), "binding migration requires semantic authority" and "fast lane lacks an admitted settlement route" verbatim.
  (2) `nats-streaming` REMOVED "the legacy helper is a shrinking remainder, never a compatibility surface" (L0
  `:61-77`), Reason: the remainder reached zero here; the removal is recorded in the doc it names. (2b) `nats-streaming`
  REMOVED "Heartbeat consumption SHALL expose settlement failure" (live `:158-181`, with its two scenarios "transient
  work fails and delayed NAK fails" and "shutdown NAK fails"). Reason: the requirement's own last paragraph (`:169`)
  assigns its deletion to this PR; L0 landed it MODIFIED, not REMOVED, so the removal is owed here. (3) ADDED
  `agent-run-milestones`.
- Docs: rewrite `migration-beta162-to-beta163.md:1050-1065` (L0 head) from "still exported at this tag"; replace
  `migration-restart-safe-nats-client.md:95`; update `33-semantic-settlement.md:99`; reword two test comments
  (`component_ack_integration_test.go:41`, `outcomes_integration_test.go:202`). Text: "`ConsumeWithHeartbeat` is removed
  without alias (#1249/#759). Bindings compose `ValidateHeartbeatDeliveryPolicy` + `ConsumeDeliveryWithHeartbeat` (or
  `SettleDelivery`/`SettleDeliveryWithRetry`); nil-means-Ack is gone. `agentrun.ResolveRun` errors now carry the `errs`
  Invalid class (chains kept). Direct callers at beta.160: SemDev `internal/conversationchannel/component.go:476`,
  `internal/intake/component.go:378`; three SemDev comments size `max_deliver 10` on the removed 30s NAK —
  `DelayedDeliveryRetry(30*time.Second)` keeps that budget."

## 5. What the #1155 stage-D proof must show (both lanes) — ruled O4/O5, not open; unit tasks from R1/R2/O1

Complete and failed each: publish a terminal; the proof handler commits its durable effect; the process is replaced
BEFORE Ack (test-only handler in the binary the agentic tier boots, gated by tag and env — O4 as amended); the
replacement redelivers; the handler sees the same `SourceMessageID`;
effect count 1; ack-pending 0. Quarantine: panic on first attempt; no Ack/Nak/Term; `/health` (`:1736`) shows `milestone`
unhealthy; that lane drains while the other consumes; replacement redelivers; Ack. Exhaustion: transient five times →
`semstreams_nats_max_delivery_exhaustions_total{consumer="agentrun-milestone-complete"}` = 1 in `verify-streaming-metrics`.
Assert re-invocation and idempotent effect count, not a lifecycle transition (ADR-053 D5 `053…:171`; O5). Unit tasks (`tasks.md` § 3, § 9; all ruled):
not-managed → Retry on attempt 1, `Manager.Create`, Ack on attempt 2 (I7); nil-reader and projection failures → Retry
with reason labels; `ErrWorkflowNotRegistered` → Quarantine + `DeliveryFatal()`.

## 6. Strongest case against

1. Shared failure domain: N handlers, one settlement, no isolation; one Retry-class handler bug re-runs the other N−1
   every attempt and at exhaustion drops the milestone for all N — still better than today's silent ACK.
2. Naive handler: today silent loss (at-most-once); under C silent duplication (at-least-once); neither
   framework-detectable. The identity makes idempotence one line; the proof handler shows it; rank doc/test. OPEN.
3. Unwrapped projection errors cost ≤4 extra attempts (≈2 min) per malformed entity before a counted drop. Alternative if
   Terminate on first sight is preferred: `errs.WrapInvalid` at `manager.go:250` (`Get`'s `%w` re-wrap), a declared Tier 1 change.
4. A sixth copy: declared, ≈75 lines. 5. Metric wiring rides two hand-copied roots (#1301); semteams' root is silent.

## 7. Size; behavior behind unchanged signatures

`agentrun.go` ≈ +170/−40; `delivery_owner.go` copy +75 → 0 after the 2026-09-19 amendment (consumes `internal/deliverylane`, ~+15 wiring); `milestone_service.go` +30; two mains +2; metric +40; unit +240;
integration +150; natsclient production −116, tests −540, ratchet ±80; e2e handler + scenario + stage ≈ +260; docs ≈ 40;
OpenSpec ≈ 300. ≈ +1,320 / −700 over ≈ 25-30 files, 3x inside the ~100-file breaker. `pkg/lifecycle` untouched. TWO changes `api-compat.sh` cannot see, both behind
unchanged signatures, and both owed to 8.3's migration note: (1) `ResolveRun`'s errors gain the `errs` Invalid class
(one caller `:651`, no sister callers; § 4); (2) `HandleEvent`'s return contract flips from "an error only for
infrastructure failures (decode, NATS)", with handler errors logged and not propagated (`b7ce8727` `agentrun.go:572-574`),
to "nil exactly when the attempt would be acknowledged" — it now returns the classified cause of every non-Ack decision
(`agentrun.go:605-613`). Zero present sister callers: semteams composes the subscriber and registers no handler
(`cmd/semteams/main.go:939`), so (2) is migration-note material, not a break.

## Round 4 changes

HIGH: header sentence struck; three moving files named; every number in them is `base :n (L1 :m)` at `c2a9cef6` (§ 2.3
H3 rows and `interpretDeliveryWork`; § 2.8 `:191`, `:1129`/`:1154`; § 2.6 loop `delivery_owner.go` noted unchanged).
MEDIUM: § 2.3 per-site choice — AgentRun-side wraps kept, `projection.go` wraps dropped, `pkg/lifecycle` untouched, three
other callers named, alternative § 6.3, declaration § 7 + migration text. NITs: `errs.WrapInvalid(...)` replaces
`NewInvalid`; `manager.go:198` row. R1/R2 recorded in § 1; not-managed row → bounded Retry; I7; § 5 unit tasks; old
§ 6.3/6.4 removed. Inventory [R4]: 12 pins added (`:198`, `:249`, `:599`, `:1034`, `manager_query.go:336`, `errors.go:40`,
`errs.go:435`, `:651`, `tier1:69`, `agentrun.go:105`, `main.go:334`, e2e `:261`); L1-head map in Adjacent claims; § 3
governance fixed at L1 head; header rewritten; 15 uncited pins dropped for the cap. § 0 removed (facts recur in § 2).
Final edit (owner ruling): Q1/Q2/Q3/Q5/Q6 folded into § 1 as O1–O5; § 8 deleted; § 5 and the tasks-facing text
marked ruled; L0 head re-attributed to `759bd596`. Inventory untouched.

## Round 5 — amendment only (2026-09-19)

Owner ruled "Amend" on #1341 Q2: § 2.6 consumes `internal/deliverylane` instead of copying the latch; § 7 size line adjusted. Everything else in the 2026-09-18 ruling (Q1–Q6, `DeliveryFatal()`, `RegisterMetrics`, the matrix) is unchanged. Blocked by #1341.
