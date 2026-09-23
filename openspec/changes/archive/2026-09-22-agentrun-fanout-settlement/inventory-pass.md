# #1249 independent inventory pass — reviewer

INVENTORY FAIL — 1 BLOCKING, 4 MAJOR, 3 MINOR (contract verdict: INVENTORY CHANGES REQUESTED; no target state reviewed)

base 0053183d, all commands run in /Users/coby/Code/c360/semstreams-wt/verify-0053183d unless a sister is named.

## 1. Pins — clean

`./scripts/inventory-verify.sh <abs inventory path>` →
`changed since base (0053183d..HEAD):` / `  (none)` /
`pins=137 ok=137 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0`
No non-OK line to quote. Premise re-checked independently: `git diff --stat main HEAD -- agentic/agentrun/ internal/agentterminal/` → empty.

## 2. Completeness

(a) CONFIRMED. `git grep -n -E 'Consume[A-Za-z]*\(|jetstream\.|StreamConsumerConfig|Subscribe\(|EnsureStream|DurableName|FilterSubject|ConsumeInternalStreamWithConfig' -- agentic/agentrun`
→ exactly two bindings (`ConsumeInternalStreamWithConfig` at agentrun.go:835 and :886) behind one closure (:810-816); handles at :681-682, Closed waits :727/:730. `nats_reader.go` binds nothing.
`git grep -n 'ConsumeWithHeartbeat' -- '*.go' ':!**/*_test.go'` → 4 hits: agentrun.go:812 (the one production call), heartbeat.go:79 (decl), heartbeat.go:18 and :60 (comments). One production caller: CONFIRMED.
Trap worth carrying forward: `natsclient/consume_durable.go:39` calls the helper on **main** and does not exist at base (`git ls-tree -r --name-only HEAD -- natsclient/` → heartbeat.go, heartbeat_test.go, heartbeat_integration_test.go only). Anyone re-deriving on main gets a false second caller. The inventory recorded this (Searches line 341); the design does not.

(b) Both claimed paths CONFIRMED, two more found — see MAJOR-2 and MAJOR-3. Read agentrun.go:556-660. HandleEvent's only non-nil return is the decode error at :578; every other exit is `return nil`.

(c) CONFIRMED (no production handler anywhere). In-tree `git grep -n 'AddHandler(' -- .` → 12 call sites, all tests; `git grep -n 'func .*OnLoopTerminal(' -- .` → 4 implementers, all tests. Sisters: loop over all 28 dirs under /Users/coby/Code/c360 with `git -C <d> grep -n -E 'MilestoneHandler|OnLoopTerminal|AddHandler\(|NewMilestoneSubscriber|LoopTerminalEvent|ConsumeWithHeartbeat' -- '*.go'` → only semteams `cmd/semteams/main.go:939` (constructs, adds nothing) and semdev (helper only). I additionally covered the five dirs a `git -C` loop silently skips because they are not repos (archive, semspec-ui-bmad, semspec-ui-run-visibility, semsummarize, workspaces — 1,483 Go files in the two UI trees) with `grep -rl --include='*.go'` → zero hits. Claim closes.

(d) CONFIRMED exactly as claimed: semdev `internal/conversationchannel/component.go:476`, `internal/intake/component.go:378`. Four further comment-only references the inventory pins only one of (apply.go:113): `internal/conversationchannel/apply.go:202`, `internal/conversationchannel/component.go:435`, `internal/intake/component.go:355` — all three assert the removed helper's fixed 30s NAK delay as the basis of a tuned MaxDeliver, so they size the migration note.

(e) Enumerated: heartbeat.go 3 refs (18, 60, 79); `nonCancellationWorkError` 4 (37 decl, 44/52 self-recursion, 110 sole call — inside the removed body, so "no other caller" holds); consumer_policy_callsite_test.go 17; heartbeat_test.go 26 / 11 Test funcs; heartbeat_integration_test.go 6 / 3 Test funcs; exactly two foreign test comments (storage/objectstore/component_ack_integration_test.go:41, processor/agentic-tools/outcomes_integration_test.go:202) — both pinned. See MINOR-7 and MINOR-8.

## 3. Classification spot-check — all three hold

- Claim A partial-ACK (§1): HOLDS. agentrun.go:812-816 wraps only `HandleEvent`'s return; handler error (:612-617) and panic (:604-610) are consumed inside `HandleEvent`, which returns nil at :621 → work closure returns nil → heartbeat.go:140 `executeTerminalMethod(msg, terminalMethodAck, 0)`. A fanout that ran 1 of n handlers ACKs.
- §2b/§2c swallowed-degrade rows: HOLD as written for :637 (Debug only, `return nil, nil`) and :593-598 (WARN, `return nil`) — but the source set feeding :593-598 is incomplete (MAJOR-2).
- §2c "the 30s NAK path is dead for this caller": HOLDS, and for a stronger reason than stated — the closure wraps **every** `HandleEvent` error in `TerminateDelivery` (:814), so only heartbeat.go:130 Term is reachable, never :135.
- §2c "InProgress failure → error returned, WARN only, no owner stop": HOLDS. heartbeat.go:103-113 returns without any terminal method — the delivery is left unsettled, not NAK'd.

## 4. Findings

BLOCKING processor/agentic-governance/component.go:510 - fifth same-class owner missing from the § 3 collision table
- Mechanism: § 3 names four owners of "stop admitting on a JetStream lane after an unsafe settlement, latch fatal into health" (dispatch/loop/model/tools `delivery_owner.go`) and concludes "the four existing copies are already the enumeration the adoption-sweep rule asks for". agentic-governance is a fifth, in a different spelling: inline `admissionOpen` + mutex latch at :510/:514-516/:521-528, `recordDeliveryOwnerFatal` at :741-749, health degraded to "delivery ownership lost" at :724-727, observer + `streamConsumerBinding.drain()` at :536-539/:752-756. It has no `delivery_owner.go` and no `deliveryLaneAdmission` type, so the inventory's name-anchored search (`^type deliveryLaneAdmission struct|^func newDeliveryLaneAdmission`, Searches line 337) structurally cannot see it — the exact "different names" case the collision-table rule targets.
- Fix: re-derive the Owners/Status/Readers rows by behavior (`OwnerStopRequired|deliveryFatalErr`), not by type name; record that the class has two existing spellings and that AgentRun would be the sixth instance, not the fifth.
- Verification: `git grep -n -E 'deliveryFatalErr|OwnerStopRequired' -- '*.go' ':!**/*_test.go'` → dispatch, loop, model, tools, **governance**; `ls processor/agentic-governance/` → no delivery_owner.go; sed of :510-540 and :715-755. Refutation attempted: governance is not merely a reader of `DeliveryResult` — it owns the latch, the admission gate, the drain and the health degradation, i.e. every dimension the § 3 table enumerates.

MAJOR agentic/agentrun/agentrun.go:639 - a fast-path error source the inventory attributes only to the slow path
- Mechanism: § 2b says the fast path treats ANY `Get` error as a non-run loop and that the slow path "propagates other errors". `:639-641` returns `fmt.Errorf("Manager.Get returned unexpected type %T", participant)` from the FAST path; it reaches the same :593-598 WARN+ACK swallow. Unpinned and unenumerated. Consequence for the design: 2.3 maps "any OTHER resolution error → Retry", so a deterministic type error would be retried to MaxDeliver and dropped.
- Fix: pin :641 and list it as a third input to the :598 swallow, distinguishing deterministic from transient resolution failures.
- Verification: read of agentrun.go:627-660; the only `return nil, err` is :657 but :641 is a second error return.

MAJOR agentic/agentrun/agentrun.go:646 - the one fully silent degrade is absent from the inventory
- Mechanism: `if ev.LoopID == "" { return nil, nil }` — with both `RunEntityID` and `LoopID` empty, every handler is invoked with a nil run and the delivery is ACKed with no log at any level, no metric, no graph condition. § 2b's ordered account of step 3 lists only the fast path, the NotFound mapping and the propagated error; the adopter-seam § names "run may be nil" but not this cause. Design 2.3's resolution matrix has no row for it.
- Fix: pin :646-648 and carry it as a distinct silent-degrade row (it is the one path with no signal at all).
- Verification: read of agentrun.go:627-660; `grep -n 'LoopID == ""' agentic/agentrun/agentrun.go`.

MAJOR service/milestone_service.go:21 - § 2f health enumeration is prose-only and under-counts on three axes
- Mechanism: § 2f pins milestone_service.go:44/45/113 and base.go:202/209 and then asserts in prose "the manager aggregates `Health()` into `/health`" with no pin. Actual: three `service.Health()` read sites (service/service_manager.go:790, :1736, :1933) — the design picks :1736 with no evidence which one serves `/health`. The seam the proposed `DeliveryFatal()` must cross is the `milestoneStarter` interface at service/milestone_service.go:21 (unpinned; the design cites it as if pinned). And "the worked examples latch a fatal delivery result into component health" pins loop/model only — dispatch (component.go:303, :735-736), tools (:503-506, :1394) and governance (:724-747) are three more.
- Fix: pin the aggregation site(s), the `milestoneStarter` seam, and the full latch-to-health set.
- Verification: `grep -n 'Health()' service/service_manager.go`; `sed -n '15,30p' service/milestone_service.go`; the grep in the BLOCKING row. Note: the design header claims every file:line is an inventory pin unless marked (new); `service/milestone_service.go:21`, `service/service_manager.go:1736`, `natsclient/delivery_settlement.go:268` and `agentic/agentrun/agentrun.go:559` are not pins.

MAJOR openspec/specs/jetstream-consumer-policy/spec.md:329 - removal blast radius under-counts the governing current spec
- Mechanism: § 2i pins one sentence (:301) of the current jetstream-consumer-policy spec. The helper is named three times: :301, :329 (`**THEN** ConsumeWithHeartbeat exclusively controls InProgress and terminal settlement`) and :333 (`**WHEN** ConsumeWithHeartbeat returns a nonnil result`). :329/:333 are scenario bullets, and this repo's MODIFIED-block rule requires a delta to restate every scenario — so each is load-bearing for the same-PR removal in design § 5. One of the two refs in the active delta's jetstream-consumer-policy spec is likewise unpinned.
- Fix: pin all three current-spec sentences (and the second delta ref) in § 2i.
- Verification: `grep -n 'ConsumeWithHeartbeat' openspec/specs/jetstream-consumer-policy/spec.md openspec/specs/nats-streaming/spec.md` → 3 + 1.

MINOR inventory.md:342 - two gopls-derived counts do not reproduce
- `AddHandler` is 12 call sites, not 8: agentrun_integration_test.go:224/305/342/449, agentrun_test.go:454/570/616/656/692/697/741, test/compat/semteams/agentrun_terminal_compat_test.go:40. `MilestoneHandler` has 4 implementers, not 2: agentrun_integration_test.go:40/49, agentrun_test.go:812, compat_test.go:25 (the build-tagged compat package is the likely gopls blind spot). Both conclusions — all tests, zero production — stand; only the counts are wrong.
- Verification: `git grep -n 'AddHandler(' -- .`; `git grep -n 'func .*OnLoopTerminal(' -- .`.

MINOR natsclient/heartbeat.go:18 - a retained symbol's doc comment names the removed helper
- `PermanentDeliveryError`'s doc (:17-19) says "ConsumeWithHeartbeat terminates the JetStream delivery instead of retrying it". § 2i lists `ErrHeartbeatFailed`/`PermanentDeliveryError`/`TerminateDelivery` as retained but not that this doc strands on removal.
- Verification: `sed -n '10,45p' natsclient/heartbeat.go`.

MINOR natsclient/consumer_policy_callsite_test.go:423 - the third ratchet assertion family is unpinned
- § 2i pins 5 of 17 refs and names the exact-caller-set (:444-447) and exact-declaration (:435-441) assertions. The third family is the alias/indirect/method/type-alias surface guard (`scan.violations` at :423, helpers :96-136/:162) with its fixture corpus at :455-456, :476-486, :507, :525 — the assertions a removal PR must invert, beyond the `NewDurableHandler` inversion shape § 2i already names at :395.
- Verification: `grep -n 'ConsumeWithHeartbeat\|legacyHeartbeat' natsclient/consumer_policy_callsite_test.go`.
