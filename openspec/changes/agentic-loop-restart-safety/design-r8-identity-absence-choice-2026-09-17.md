# R8 identity-absence correction: bounded owner choice

base: 68c14c8eb25c512e988f740cbf7ea14b6815976f  
status: Design draft for independent pre-owner review; not an implementation authorization.

## Conclusion

Remove dispatch’s unnecessary repeat publication when its exact retained task already proves commitment. This is a small, concrete correction.

It does **not** complete R8. The present contract permits retrying the same serialized TaskMessage after uncertain PubAck. That can refresh its stream retention without refreshing earlier loop authority or request evidence. Existing absence observations cannot always distinguish:

- A task never dispatched from an expired dispatch mapping.
- A legitimate first loop birth from an expired earlier loop.
- A valid partial birth from progressed work whose request expired.

No implementation predicate currently closes those distinctions. Do not call one “safe ordering,” infer it from `Iterations == 0`, or silently classify every absence as new work.

## Options

| Option | Benefit | Cost / limitation |
|---|---|---|
| Leave the correction open | No behavior or contract change. | Demonstrated identity/reconstruction gaps remain; R8 stays incomplete. |
| **Stop republishing an already-proven dispatch task** | Removes the demonstrated dispatch-created ordering inversion; uses existing exact evidence and owners. | Same-byte uncertain retries and remaining absence cases still require a policy decision. |
| Define an agentic-only origin-age contract using existing envelope `created_at` | Potentially permits bounded absence decisions without another store or identifier. | **New policy, not an existing invariant.** Must establish validation, preservation, first-publication meaning, clock assumptions and continuation semantics. Legitimate delayed first delivery can be refused. Not ready for implementation. |
| Change retained identity/evidence policy | Can preserve evidence instead of predicting whether it expired. | Extending or removing retention carries storage/capacity and lifecycle costs. Changing only request retention does not solve expiring loop authority. Requires explicit owner approval. |
| Refuse every retained nonterminal loop whose request is absent | Prevents original-prompt reconstruction after evidence loss. | Also rejects valid partial birth. Refusing every absent authority would reject all legitimate first births. Therefore this is **not** a conforming blanket fix. |

Recommend the small proven-publication correction now. Keep the remainder explicitly blocked on a bounded owner choice—not another recovery runtime.

## Exact next-slice behavior

Both dispatch entry points already use `findRetainedDispatchTask`: durable USER handling and HTTP submission.

| Observed branch | Required behavior |
|---|---|
| Durable USER, valid retained task | Skip only task publication. Complete the existing required user-response publication; its failure preserves Retry before source ACK. |
| HTTP, valid retained task | Skip only task publication. Preserve the existing synchronous response and optional stream-mirror behavior, including its current ignored mirror error. |
| Exact-read failure | Preserve each caller’s existing classification: durable USER follows its settlement error path; HTTP returns its existing refusal response. No identity minting or task publication. |
| Retained task conflicts or is invalid | Preserve existing refusal/quarantine. No replacement identity. |
| Typed task absence | Leave current preparation/publication behavior unchanged in this slice. **Its retention safety obligation remains open.** |

The retained record proves this specific task publication committed. It does not prove consumer processing, terminal completion, or an ordinary user-response publication committed.

This simplification requires a narrow spec amendment. The current “Every dispatch durable input settles through its owner” requirement demands synchronous PubAck for each required task publication. An exact retained task proves commitment even when the original PubAck was lost, but does not satisfy that wording unchanged. Amend it only for task publication: **Dispatch MAY satisfy the required task-publication obligation through a successfully decoded and validated exact retained TaskMessage whose TaskID, LoopID and source correlation match the submission. In that case dispatch SHALL reuse that commitment without republishing the task. All other required publications—including user responses, cancellation and approval messages—still require their existing synchronous PubAck before source settlement.** This remains a proposed amendment pending owner approval.

Do not add an output ledger or broaden exact reads to ordinary publications.

### Proof obligations

Use existing unit and native task-replay seams:

1. First durable USER delivery publishes one task; required response fails or source settlement is withheld.
2. Replacement processes the same source after the duplicate window.
3. It emits no second task, retains the original identity, and completes the required response path.
4. Unreadable/conflicting evidence still prevents minting.
5. Genuine typed absence still follows current birth behavior.
6. Exercise both USER and HTTP callers.
7. Preserve terminal suppression, partial-birth controls and the existing failing evidence-loss contrast. Do not label that contrast fixed by this slice.

No E2E-wide redesign or additional recovery state is part of this correction.

Prove both callers skip a validated retained task without changing their respective response/error semantics. For durable USER, prove required response failure prevents source ACK and replacement completes the remaining response. For HTTP, preserve synchronous response, exact-read refusal and optional-mirror behavior. Skipping publication neither deletes the retained task nor advances its destination consumer. This slice does not repair a late DeliverNew consumer, exhausted delivery budget, or other downstream-consumption condition.

## Precisely blocked remainder

### Dispatch mapping absent

The exact lookup supplies a typed absence, not evidence that this stable source identity never produced a task.

The native short-evidence test establishes that the **original** USER delivery can survive its mapping and mint a different LoopID. Observing actual source/evidence retention remains necessary. A relative retention check must name which publication identity it protects; it cannot silently cover arbitrary later republication.

### Loop authority absent

Typed KV absence is shared by initial birth and expired authority. Existing TaskMessage identity alone does not distinguish them.

Blanket Retry would stall legitimate births. Blanket quarantine would halt them. Blanket Terminate would discard them. None is a substitute for establishing permission to create.

### Nonterminal authority present, request absent

Present authority does not establish unpublished partial birth. `running` and zero iterations can also describe already-dispatched first-round work. Reconstructing original prompt/Iteration1 is therefore not universally justified.

The approval path provides a useful **existing ownership pattern**: observe evidence, recheck authority, then durably publish a failed outcome before settling. Its age predicate cannot simply be copied:

- `StartedAt` is rewritten by `SetTimeout`.
- Task continuation calls the metadata configuration path containing that setter.
- A later source publication timestamp is not an earlier loop-birth timestamp.
- BaseMessage `created_at` exists, but current validation does not establish the proposed agentic origin-age meaning.

A future accepted unavailable-recovery branch must distinguish:

- **Transient read uncertainty:** Retry through existing settlement.
- **Confirmed unavailable recovery with authoritative loop state:** existing failed-outcome owner, required PubAck/final KV completion, then ACK.
- **Unresolved safety without sufficient authority:** quarantine stops the consumer owner without ACK/NAK/TERM; operator action is required. It is not a hidden retry loop or successful recovery.

This draft does not claim that the current observations provide the missing classifier.

## Adopter consequences

For the recommended small slice, adopters change nothing. The same task remains durably committed; only an unnecessary duplicate publication disappears. Required submission responses remain.

For the unresolved policy choice, the important cost must be explicit: bounded recovery can become unavailable. A proposed origin-age contract may reject old **first** deliveries as well as replays. A retained-fact solution charges storage and lifecycle complexity instead. Neither cost should be disguised as a framework timer formula.

Provider reinvocation, governance typed-absence reevaluation, approval absence handling, PubAck ordering, DiscardNew requirements and existing KV protections remain unchanged.

## Measurements for this draft

- `processor/agentic-dispatch/task_recovery.go:107` — `return preparedDispatchTask{task: retained, data: retainedData, subject: subject}, slot, true, nil`
- `processor/agentic-dispatch/component.go:982` — `if err := c.natsClient.PublishToStream(ctx, prepared.subject, prepared.data); err != nil {`
- `processor/agentic-dispatch/http.go:327` — `prepared, vacant, found, err := c.findRetainedDispatchTask(ctx, msg)`
- `processor/agentic-dispatch/http.go:392` — `if err := c.natsClient.PublishToStream(ctx, prepared.subject, prepared.data); err != nil {`
- Dispatch delta `specs/agentic-dispatch/spec.md:241–269`: retained commitment recovers LoopID; the separate durable-input settlement requirement at lines 70–73 still requires synchronous PubAck and needs the narrow proposed task-only amendment above.
- Current `design.md:1063–1068`: direct producers retry uncertain publication with the same serialized bytes.
- `processor/agentic-loop/settlement_recovery.go:388` — `if !found {`
- `processor/agentic-loop/settlement_recovery.go:398` — `if entity.State.IsTerminal() {`
- `processor/agentic-loop/settlement_recovery.go:421` — `request = c.handler.newTaskRequest(entity.ID, task, messages, tools)`
- `processor/agentic-loop/handlers.go:937` — `h.configureLoopMetadata(loopID, task)`
- `processor/agentic-loop/handlers.go:525` — `if err := h.loopManager.SetTimeout(loopID, timeout); err != nil {`
- `processor/agentic-loop/state.go:1186` — `entity.StartedAt = now`
- `message/base_message.go:63` — `func WithTime(createdAt time.Time) Option {`
- `message/base_message.go:200` — `if m.meta == nil {`
- `message/base_message.go:236` — `"created_at":  timestamp.ToUnixMs(m.meta.CreatedAt()),`
- `natsclient/delivery_settlement.go:413` — `if result.quarantined || result.ownerStopNeeded {`

Bounded follow-up reading covered these callers, the existing approval absence owner, envelope metadata validation and the explicit same-byte producer contract. No tests or mutations were performed.

`orchestration-check` keeps these decisions in the existing dispatch/loop components. No rule, graph orchestration layer, store or runtime is introduced.

## Binding evidence appendices

Preserve the following complete accepted inventories **verbatim**, rather than replacing them with this summary. They accompany this design as its unchanged evidence appendices:

1. `inventory-r8-retention-premise-2026-09-17.md`  
   SHA256 `fb937d0ef9c3c53e3a85deed5ee3d426f7cff3b786cc930767aa56d1881b2a40`

2. `inventory-r8-loop-retention-reachability-2026-09-17.md`  
   SHA256 `cd9c12ae52a35c8f7eb10f04892cba44b01ab1b9ba82ab96f98ecc9cdba89cec`

Both reside under `openspec/changes/agentic-loop-restart-safety/` and have independent inventory review PASS. The source-backed loop witness is not a native expiry reproduction; the separately recorded dispatch proof is native.

**Owner decision sought after review:** approve only the proven-publication simplification, and choose whether the remaining absence distinction should be established through an explicit bounded origin-age policy or a retained-fact policy. No all-three completion is claimed.
