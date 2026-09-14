# Design reconciliation: Stage A settlement landing

## Evidence checkpoint

Historical evidence only. Owner ruling #1146 comment `5530950829` supersedes this artifact wherever it forbids the
narrow stateless `SettleDelivery` transport interpreter or implies a universal AckWait-derived work deadline. The
canonical #759 and #1146 proposal, design, tasks, and capability deltas are current target-state authority.

This design incorporates
`openspec/changes/semantic-jetstream-settlement/inventory-rebaseline-2026-09-02.md` unchanged.

- Evidence base: `39444c9de649775a4be6866a946b7d73400f4639`
- Inventory SHA-256: `542458e2e46d5be2ea49e6ec5ab7de64366f58f782d94a32396aaaec38b4f437`
- Independent verdict: `INVENTORY PASS`
- Pin verification: `231/231`
- Materialization commit: `4d3894028d0100a67f2383672f35b42a4befc10e`

The inventory covers the SemStreams surface at lines 15–303 and the external adopter seam at lines 340–359.
SemSpec and SemDragon checkpoint identities are recorded at lines 4–11.

## Problem

The earlier #759 scope keeps nine delivery bindings and final legacy-helper removal in one change. That scope is now
circular: #1146 production implementation requires the typed #759 foundation on `main`, while the held model and loop
bindings require the durable replay and reconciliation authority #1146 exists to establish. AgentRun has an additional
problem: one source delivery fans out to outward-facing handlers, but handler errors and panics are logged and erased,
later handlers continue, the callback returns nil, and no durable per-handler receipt exists.

The accepted direction remains settlement-first and streams-first. This reconciliation changes ownership and landing
sequence; it does not introduce a supervisor, generic state machine, checkpoint, outbox, CQRS layer, event-sourced
loop, receipt bucket, or new communication path.

## Options

| Option | Benefit | Cost and consequence |
|---|---|---|
| Land Stage A and transfer the held lanes | Unblocks #1146; ships the permanent typed API and three proven bindings | Temporarily retains the exact legacy allowlist; requires explicit supersession of the earlier all-nine ruling and two named follow-up owners |
| Keep all nine in #759 | Preserves the old issue boundary | Preserves the circular dependency or creates a stacked mega-change across the same handlers #1231 rewrote |
| Do nothing | Makes no immediate contract change | Leaves #1146 blocked and retains void/log-and-ACK paths plus false gated-DAG PubAck guidance |

### AgentRun ownership alternatives

| Placement | Benefit | Cost and consequence |
|---|---|---|
| Keep AgentRun inside #1146 tasks H.1/H.2 | One restart-safety vertical owns the remaining agentic consumers | Expands an already broad #1146 across an exported handler seam, delays #1146 and #1244, and requires H.1/H.2 to be expanded after design acceptance |
| Give AgentRun a separate design owner | Focuses the exported fanout contract review and lets #1146/#1244 progress | Adds an issue and PR and leaves the characterized legacy binding in place longer |

The existing #1146 H.1/H.2 tasks only hold an implementation location; they do not authorize an AgentRun design. If
the owner retains AgentRun in #1146, those tasks must be expanded after design acceptance to cover source identity,
handler-defined done, replay and partial failure, both consumer migrations, and complete/failed replacement proof.
The recommendation is a separate AgentRun owner because no production product handlers currently exist, the handler
seam is exported, source identity is discarded, and there is no durable receipt authority.

### Final legacy-helper ownership alternatives

| Placement | Benefit | Cost and consequence |
|---|---|---|
| Keep removal as the final #759 gate | Preserves one issue for introduction through retirement | #759 cannot close with Stage A and PR #1156 cannot declare `Closes #759` |
| Give removal a separate zero-caller cleanup issue | Lets the typed foundation and proven bindings close #759 now | Adds one cleanup issue whose merge waits for every held adopter |

The recommendation is a separate cleanup issue. Keeping removal inside #759 is valid only if #759 remains open after
Stage A and PR #1156 no longer declares that it closes #759.

### Gated-DAG capability alternatives

| Placement | Benefit | Cost and consequence |
|---|---|---|
| Keep generic consume mechanics in `gated-dag-dispatch` | One capability text describes the whole current path | Couples a domain contract to natsclient transport mechanics and implies one definition of done across unlike adopters |
| Keep domain done/replay in `gated-dag-dispatch` and move transport mechanics to `jetstream-consumer-policy` | Each capability owns one truth and adopters retain their actual durable completion contract | Requires coordinated deltas and an explicit migration record |

The recommendation is the split boundary because SemSpec and SemDragon have different durable consequences and replay
checks even though both consume JetStream work.

## Recommendation

Explicitly supersede the earlier all-nine #759 ruling and land #759 as Stage A:

1. PR #1156 closes #759 after the gated-DAG truth repair, adopter migration record, and semantic-settlement concept
   document land.
2. Within its full accepted scope, #1146 adds model plus loop task, response, and tool-result heartbeat migrations.
   This does not narrow its intake, commands, signals, approval, projections, governance, replay, or context-lifecycle
   work.
3. A new AgentRun issue owns complete/failed fanout settlement. Keeping that work in #1146 H.1/H.2 remains an owner
   alternative only if those tasks are expanded after design acceptance.
4. A final zero-caller cleanup issue owns `ConsumeWithHeartbeat` removal after #1146, AgentRun, and sister
   reconciliation. Keeping removal in #759 instead means #759 does not close with Stage A.
5. PR #1156 changes `Closes #1155` to `Refs #1155`; Stage A proves only the tools and dispatch acceptance rows.
6. After #759 merges, #1146 rebaselines every fast no-heartbeat lane and selects an existing or reviewed settlement
   route. It neither uses raw direct settlement nor forces heartbeat nor exports a no-heartbeat API by implication.

Temporary API duality is smaller and more truthful than inventing unresolved delivery semantics inside #759.

## Measured premises

| Premise | Evidence |
|---|---|
| Permanent typed settlement is implemented | Accepted inventory lines 17–33: `DeliveryDecision`, `DeliveryAttempt`, `DeliveryResult`, metadata admission, heartbeat, and terminal methods |
| Exactly three SemStreams production legacy call sites remain | Inventory lines 38–99 and exhaustive searches at lines 413–414 |
| Model erases failures and publication outcomes | `processor/agentic-model/component.go:399-402,583,590,777,1049,1082`; inventory lines 38–48 |
| Loop response/tool-result handlers are void and stale correlation is log-and-drop | `processor/agentic-loop/component.go:177,893-896,1393,1448-1450,1780,1808-1810`; inventory lines 52–83 |
| #1231 invalidated #1146's old touched-surface baseline | Inventory lines 166–188; the accepted #1146 design requires reinventory after material touched-surface change |
| AgentRun currently permits ACK after partial fanout | `agentic/agentrun/agentrun.go:602-621`: per-handler panic/error is logged, later handlers run, and the final result is nil |
| AgentRun lacks resumable source and handler identity | `SourceMessageID` exists at `internal/agentterminal/terminal.go:67,123` but is discarded by `LoopTerminalEvent` at `agentic/agentrun/agentrun.go:467,580`; no durable per-handler receipt exists |
| The framework has no production AgentRun product handlers | Inventory lines 124–126 and searches at lines 408 and 454 |
| Failed synchronous publish does not prove absence | ADR-070 lines 61–77; current gated-DAG spec incorrectly claims otherwise at lines 30–32 |
| Gated-DAG server dedupe is bounded | `processor/gated-dag/publisher.go:27-45`; `processor/gated-dag/config.go:391-400`: deterministic `Nats-Msg-Id=unitID` is effective only inside configured `Duplicates`; after longer interruption adopter durable replay/idempotency is load-bearing |
| SemSpec and SemDragon do not share one definition of done | Accepted inventory adopter seam, lines 342–359 |
| PR #1156 replacement proof covers tools and dispatch only | Active tasks 5.3–5.7; no model, loop-continuation, approval, or AgentRun-fanout replacement proof |
| #1146 precedes #1244 and may not legalize log-and-ACK | Binding comments recorded by inventory lines 281 and 289 |

## Decision-skill outcomes

- `kv-or-stream`: no new path. Work remains on JetStream; current loop and projection state remains on existing KV
  and Store authorities.
- `entity-or-bucket`: #759 adds no durable fact. A per-handler AgentRun receipt ledger is not justified without a
  measured failpoint and separate owner-reviewed design.
- `orchestration-check`: semantic settlement is component-local execution and lifecycle discipline, not a workflow,
  supervisor, or state-machine runtime.
- `new-payload`: #759 adds no payload. `ApprovalContinuationV1` remains #1146-owned and must follow the complete
  payload-registry checklist.

## AgentRun boundary

#759 cannot truthfully migrate either AgentRun consumer. Malformed envelopes can map individually to Terminate, but a
successfully decoded terminal event enters outward-facing fanout with no safe universal settlement:

- ACK ratifies partial fanout.
- Retry may repeat handlers that already completed.
- Terminate loses unfinished handlers.
- Quarantine and exact-owner stop fail closed but do not define completion or restart progress.

Even “all handlers returned nil” is not authoritative because the exported handler contract does not state that nil
means its durable consequence committed or that replay is safe.

There are two valid placements. The owner may retain AgentRun in #1146 H.1/H.2 and expand those tasks after accepting
the design, or create a separate `agentrun: make milestone fanout settlement replay-safe without partial ACK` issue.
The separate issue is recommended so the exported handler seam does not delay #1146 and #1244.

Whichever placement the owner selects must preserve source-message identity, define handler done, classify
run-resolution failures, compare whole-fanout replay against stable per-handler receipts and one product-owned
composite consequence, prove complete and failed replacement paths, and only then migrate both AgentRun bindings. It
must not add receipts by default; the simplest contract that survives process replacement wins.

## Gated-DAG boundary

The `gated-dag-dispatch` capability owns each adopter's domain definition of done and replay behavior.
`jetstream-consumer-policy` owns transport settlement, heartbeat, lease, and exact consume-handle mechanics. The
domain capability does not prescribe a generic nil/error callback or one heartbeat API.

A publish error is ambiguous because the server may have persisted before the acknowledgement was lost. Repeated
attempts use deterministic `Nats-Msg-Id=unitID`, but server dedupe is effective only inside the configured
`Duplicates` window. Requiring `Duplicates >= BackstopInterval` covers the ordinary backstop interval; it does not
provide unbounded exactly-once delivery. After a longer interruption, the adopter's durable already-complete or
idempotent replay contract is load-bearing.

SemSpec's enabled execution bridge and SemDragon's unenabled staged `questdag` have different definitions of done and
replay. SemStreams records exact migration instructions for each and mutates neither sister repository.

## User-facing concept

#759 owns `docs/concepts/33-semantic-settlement.md`. It explains the message pump, lease watchdog,
component-defined durable done, idempotent reconciliation, and ACK/Retry/Terminate/Quarantine decisions. It includes
one happy-path example and one process-replacement example, and distinguishes semantic settlement from supervisors and
persistent state machines. #1146 links the concept and documents only its provider and continuation exceptions.

## Proposed OpenSpec changes

### Proposal and design scope

The active proposal and design will be changed to say:

- #759 lands the typed foundation plus tools one and dispatch complete/failed two.
- Model and loop task/response/tool-result heartbeat migration is additive to the full accepted #1146 scope. Intake,
  commands, signals, approval, projections, governance, replay, and context lifecycle remain in that vertical.
- After #759 merges, #1146 rebase/reconciliation inventories every fast no-heartbeat lane and selects an existing or
  reviewed route. It does not use raw direct settlement or export a no-heartbeat API by implication.
- AgentRun complete/failed moves to a separately reviewed fanout issue unless the owner keeps it in #1146 and expands
  H.1/H.2 after design acceptance.
- The exact legacy allowlist remains for those held callers.
- Final helper removal moves to a zero-caller cleanup issue unless the owner keeps #759 open through final removal.
- Gated-DAG owns adopter-specific domain done/replay, while generic settlement, heartbeat, lease, and consume-handle
  mechanics live in `jetstream-consumer-policy`.
- Deterministic `Nats-Msg-Id` dedupe is claimed only inside the configured duplicate window; beyond it, adopter durable
  already-complete or idempotent replay is load-bearing.
- Model, loop, and AgentRun configuration and runtime behavior remain unchanged by #759.
- #1155 stays open because Stage A proves only a subset of its process-replacement matrix.

The #759 non-goals gain:

- no model, loop, or AgentRun binding migration;
- no narrowing of the full accepted #1146 scope;
- no raw direct settlement path or exported no-heartbeat API;
- no final `ConsumeWithHeartbeat` removal;
- no generic gated-DAG nil/error definition of done;
- no unbounded `Nats-Msg-Id` dedupe claim; and
- no positive AgentRun runtime fanout contract, receipt, fanout ledger, or exported handler-contract change.

### Task truth

Replace the blocked archive and held-binding sections with:

```markdown
- [ ] 4.9 Add and review the `gated-dag-dispatch` delta: correct PubAck ambiguity, preserve deterministic
      `Nats-Msg-Id`/dedupe-window authority, and remove generic nil/error and heartbeat mechanics from the domain
      capability.
- [ ] 4.10 Materialize `docs/operations/migration-gated-dag-semantic-settlement.md` from the accepted SemSpec and
       SemDragon checkpoints. Record registration, enablement, current definition of done, exact-handle gap, and
       owner-specific typed migration without sister mutation.

## 6. Follow-on ownership gate

- [ ] 6.1 Obtain an explicit owner ruling superseding the earlier all-nine #759 scope: #759 closes with Stage A;
      #1146 retains its full accepted scope and adds model and loop task/response/tool-result heartbeat migration; and
      the owner selects the AgentRun placement.
- [ ] 6.2 Before #759 merges, record in #1146 and PR #1159 that they will rebase and reconcile after #759 under
      existing tasks 1.1 and 1.4. Do not claim that the post-merge rebase or reconciliation already happened.
- [ ] 6.3 Record the selected AgentRun placement: expand #1146 H.1/H.2 after accepted design, or file and link
      `agentrun: make milestone fanout settlement replay-safe without partial ACK`.
- [ ] 6.4 Record the selected helper placement: keep #759 open through final zero-caller removal, or file and link
      `natsclient: remove ConsumeWithHeartbeat after final semantic-settlement adopters`.
- [ ] 6.5 Change PR #1156 from `Closes #1155` to `Refs #1155`; do not close #1155 until all acceptance rows have
      landed and passed replacement proof.

## 7. Stage A final verification and documentation

- [ ] 7.1 Re-run the exact AST allowlist: legacy remains only model, loop, and AgentRun and has no new caller.
- [ ] 7.2 Verify model, loop, and AgentRun configuration, settlement, cancellation, logs, and health remain unchanged.
- [ ] 7.3 Add `docs/concepts/33-semantic-settlement.md` with message-pump, lease-watchdog, owner-defined done,
      disposition, happy-path, and process-replacement examples; distinguish it from supervisors and state machines.
- [ ] 7.4 Update `docs/operations/migration-restart-safe-nats-client.md` so it does not claim final legacy removal or
      one universal adopter definition of done.
- [ ] 7.5 Verify the #1146 pre-merge handoff preserves the full accepted scope and the promise to rebase/reconcile
      after #759. It must not claim the rebase already happened; the post-merge work must inventory every fast
      no-heartbeat lane and forbid raw direct settlement or an exported no-heartbeat API by implication.
- [ ] 7.6 Run focused race tests, repository lint/race/integration/schema/contracts, gated-DAG contract tests, and the
      serialized agentic E2E tier.
- [ ] 7.7 Reconcile OpenSpec task truth and archive as the final content commit, followed by narrow archive/spec-sync
      review.
```

### `jetstream-consumer-policy`

The additive migration requirement will state that tools and dispatch use the permanent typed entry point while
legacy remains source- and behavior-compatible only for the exact model, loop, and AgentRun allowlist. New production
legacy callers fail conformance. #759 neither removes legacy nor migrates held bindings.

Stage A crash declarations retain tools BackOff 15s/60s with heartbeat 5s and dispatch's accepted 10s heartbeat and
30s effective acknowledgement interval. Model, loop, and AgentRun retain current consumer and heartbeat configuration.

Held bindings remain non-authorizing. Model and loop wait for #1146. AgentRun remains unmigrated on characterized
legacy behavior until an accepted contract defines handler done, replay, and partial failure. #759 makes no positive
normative statement about current AgentRun fanout semantics. No current done row authorizes a new ledger,
rehydration path, handler receipt, inferred disposition, direct no-heartbeat settlement surface, or final helper
removal.

### `nats-streaming`

No additional partial-fanout runtime requirement lands in #759. Current AgentRun behavior is measured evidence that
blocks migration, not positive accepted runtime semantics. The hold belongs in `jetstream-consumer-policy`; the
future AgentRun-owned capability defines positive fanout, replay, and partial-failure behavior after owner acceptance.

### `gated-dag-dispatch`

Modify publish-failure truth to require claim/in-flight rollback after synchronous publish error without treating that
error as proof of non-persistence. Repeated attempts use the same unit ID as `Nats-Msg-Id`; server dedupe is claimed
only inside the configured `Duplicates` window. `Duplicates >= BackstopInterval` covers the ordinary backstop
interval, not an unbounded exactly-once guarantee. After a longer interruption, the adopter's durable
already-complete or idempotent replay contract is load-bearing. If durable `Unclaim` fails, only the local in-flight
hint clears and the stranded-unit detector owns visibility; automatic redispatch is not claimed safe.

Add an owner-specific completion and replay requirement: an adopter positively settles only after its durable
consequence and replay check, including already-complete/idempotent recovery beyond the server dedupe window.
Transient failure retries; poison, already-complete work, ambiguous effects, and partial work follow the adopter's
reviewed contract rather than generic nil/error inference.

Remove the capability requirements named `The framework provides a typed durable-consume primitive` and `Heartbeat
interval is enforced below AckWait`. Their transport mechanics move to `jetstream-consumer-policy`; migration requires
owner-specific `DeliveryWork`, exact consume-handle ownership, and an ACK/Retry/Terminate/Quarantine matrix.

## Sister migration record scope

Create `docs/operations/migration-gated-dag-semantic-settlement.md` which:

- records SemSpec checkpoint `5a9496eecc453747f4bc557b95444db6304c1420` and accepted dirty-state hashes;
- records that its enabled execution bridge succeeds only after completed/failed `ENTITY_STATES` evidence;
- requires exact native-handle retention/join if that bridge remains and does not treat its uncommitted removal plan
  as current truth;
- records SemDragon checkpoint `07f4de9b65887801ff18a7273d14233023049321`;
- distinguishes shipped `questdagexec` from staged and unregistered `questdag`;
- records staged `questdag`'s reservation plus `ClaimAndStartForParty` done definition, invented root context, and
  missing exact-handle join;
- requires each sister owner to define its own decision matrix;
- states that deterministic `Nats-Msg-Id` server dedupe applies only inside `Duplicates`, and requires each adopter to
  document durable already-complete or idempotent replay after that horizon; and
- states that SemStreams does not modify or validate sister repository state.

## PR implications

PR #1156 becomes:

```markdown
Closes #759
Refs #1155
implemented-by: Sol
```

Its body must say Stage A proves the permanent API, tools, dispatch complete/failed, gated-DAG truth, migration
guidance, and concept documentation. It does not prove paid model replay, loop cold recovery, AgentRun fanout, or final
legacy removal.

PR #1159 retains `Closes #1146`, adds `Refs #1155`, and states before #759 merges that it will rebase and reconcile
after #759 under existing tasks 1.1 and 1.4; it does not claim that work already happened. Its full accepted scope
remains intake, commands, model, loop, tools, signals, approval, projections, governance, replay, and context
lifecycle. Model and loop task/response/tool-result heartbeat bindings are additive. The post-merge pass inventories
every fast no-heartbeat lane and selects an existing path, heartbeat route, or new reviewed delta; it creates neither
raw settlement authority nor an exported no-heartbeat API. No handler exit may encode log-and-ACK as a legal terminal
result.

## Conformance and ruling ledger

| Item | Current evidence | Proposed disposition | Authority needed |
|---|---|---|---|
| Typed foundation and Stage A bindings | Implemented and replacement-tested | Land in PR #1156 | Existing C8/C9 and Stage A approval |
| Six held bindings | Exact legacy allowlist | Model/loop add to full #1146; AgentRun uses selected owner | Owner supersession of earlier C6 scope |
| Fast no-heartbeat lanes | #1146 includes lanes beyond model/loop heartbeat consumers | Post-merge inventory and choose existing or reviewed route; no raw or exported no-heartbeat surface | Existing full #1146 scope plus owner acceptance of this handoff |
| Gated-DAG generic mechanics | Stale capability, false PubAck premise, bounded server dedupe | Domain done/replay in gated-DAG; transport in consumer policy; adopter authority after dedupe horizon | Owner acceptance |
| AgentRun partial fanout | Source identity discarded; errors erased; no receipts | Separate issue recommended; expanded #1146 H.1/H.2 is alternate | Owner placement and later design acceptance |
| Final legacy removal | Callers remain | Separate zero-caller cleanup recommended; keeping it in #759 keeps #759 open | Owner placement and later explicit approval |
| #1155 | Stage A rows proven; remaining rows absent | Keep open; PR #1156 references only | Existing acceptance remains binding |
| #1146 baseline | #1231 changed touched files | Rebaseline before implementation | Existing rebaseline ruling |
| #1244 sequencing | State arm follows durability arm | #1146 first; no log-and-ACK | Existing composed-exit ruling |

## Open owner rulings

1. Supersede the earlier all-nine #759 scope and close #759 with Stage A.
2. Make model plus loop task/response/tool-result heartbeat migration additive to the full accepted #1146 scope.
3. Place AgentRun in the recommended separate fanout issue, or retain it in #1146 and expand H.1/H.2 after design
   acceptance.
4. Place final helper removal in the recommended zero-caller cleanup issue, or keep #759 open and remove
   `Closes #759` from PR #1156.
5. Keep #1155 as the multi-stage proof tracker and remove `Closes #1155` from PR #1156.
6. Accept the gated-DAG domain/transport capability boundary and sister migration-record scope.
7. Accept that deterministic `Nats-Msg-Id` server dedupe is bounded by `Duplicates`; beyond it, adopter durable replay
   or idempotency is authoritative.
8. Make the semantic-settlement concept document #759-owned, with #1146 linking and extending it.

No active spec, production file, PR body, or issue state changes until this design receives independent review and
explicit owner acceptance.
