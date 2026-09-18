# R8 expiry-first decision: origin age is insufficient

base: 68c14c8eb25c512e988f740cbf7ea14b6815976f  
status: Investigation result for independent pre-owner review; no expiry-policy implementation authorized.

## Decision sought

Do not implement an origin-age gate as the answer to the three identity-loss cases.

The remaining product choice is whether accepted work's identity and necessary recovery evidence must survive independently of ordinary event retention, or whether the framework explicitly ends recovery when that evidence expires.

I recommend preserving the necessary facts through their existing component owners. This is a direction for a separately bounded design—not approval to retain every AGENT message forever, add fields, or introduce a store.

## Measured premises

1. Same serialized bytes preserve BaseMessage `created_at`, but its current validation supplies no age, nonzero-time or clock-skew contract. `WithTime` permits historical values (`message/base_message.go:63,127,200,236`).
2. Dispatch creates a new task envelope rather than propagating UserMessage origin time (`task_recovery.go:122`).
3. The four static rule configurations share `publishAgentOnce`; each execution constructs a fresh task envelope (`processor/rule/actions.go:1965`). Fresh rule execution remains outside same-publication reuse.
4. An attached turn can have a new TaskID/envelope while naming an older LoopID. Attachment rebinds TaskID; `SetTimeout` rewrites StartedAt (`processor/agentic-loop/state.go:309,1186`).
5. Current nonterminal authority does not distinguish unpublished partial birth from progressed work with missing request evidence. The reviewed reachability inventory establishes that ambiguity.
6. Loop authority currently requires TTL24h. Ordinary stream bounds require finite MaxAge; actual stream identity is port-resolved, and required records can share a stream with unrelated output. These are existing policies, not inferred guarantees.

## Does current origin metadata close the three cases?

| Case | What age could establish under an additional contract | What it cannot establish today |
|---|---|---|
| Dispatch mapping absent | The source is older than a chosen supported admission window. | Whether it was never dispatched or its mapping expired. An old first delivery is also old. |
| Loop authority absent | A newly created task envelope is recent. | Whether it starts new work or names an older loop whose authority expired. |
| Nonterminal authority present, request absent | The current task envelope is recent. | Whether missing evidence belongs to partial birth or earlier work/context on that loop. |

The strongest counterexample is a **fresh continuation admitted against a near-expiry older loop**. Its young envelope does not extend the lifetime of the older authority or request. Those records may disappear before delivery. A task-age check can pass while required evidence is gone.

Neither `Iterations == 0`, mutable StartedAt, nor a later broker publication timestamp repairs that distinction.

## Genuine options

### A. Leave the issue open

Preserve current contracts and make no expiry-policy change. The retained-task reuse correction still stands independently.

Cost: the demonstrated absence ambiguity remains unresolved. R8 cannot be declared complete. This does not authorize unsafe reconstruction or redefine its intended RED as acceptable.

### B. Add a minimal origin-age policy

Define agentic-specific timestamp meaning, preservation, validation and skew handling; refuse deliveries outside its accepted window.

Cost to adopters: sufficiently delayed **first** delivery can be refused despite never having run. Historical/custom metadata and retry wrappers acquire new correctness obligations. Expired work would need an observable refusal and deliberate resubmission as new work—not silently become a fresh execution.

Limitation: even a correctly implemented task-age policy does not solve fresh continuation against older evidence. Closing that gap requires another product restriction or retained fact. Consequently this is not the recommended first step.

### C. Extend retention of existing correctness facts

Keep task identity, current/terminal loop authority and necessary reconstruction evidence available through existing owners instead of deriving their existence from an event's age.

This introduces no necessary supervisor or state-machine runtime. It nevertheless has real storage-policy costs:

1. Extending the existing loop record's TTL does not by itself preserve missing request evidence.
2. Making an existing evidence stream non-expiring may retain unrelated co-located AGENT traffic.
3. Finite capacity plus DiscardNew gives visible publication refusal when full; it does not establish deletion, purge or lifecycle safety.
4. Isolating the necessary facts may require an existing-record representation or retention change that has **not** been designed or authorized here.

Adopters would retain the accepted replay semantics without calculating origin-age thresholds. Operators would pay explicit retention/capacity/lifecycle costs. This inventory does not establish their magnitude.

## Recommendation and necessary owner choice

Do not spend implementation effort on an age gate that cannot discharge the obligation.

Prefer a bounded existing-owner retained-fact design, limited to the facts distinguishing these three cases. Its first condition must be that it does **not** retain unrelated AGENT history or introduce a second authority by default. If that cannot be achieved simply, return the concrete cost before implementation.

**Necessary owner choice:** should already-accepted work retain its identity/recovery facts beyond ordinary event expiry, accepting an explicit storage-lifecycle cost; or should expiry end its supported recovery, accepting a separately defined observable refusal/resubmission contract and its effect on attached turns?

The latter is a genuine product change, not a classifier detail. Current metadata alone does not implement it safely.

## Unchanged boundaries and limits

Valid partial birth, retained terminal suppression, provider reinvocation, governance typed-absence reevaluation and the existing approval contract remain unchanged until an explicit ruling says otherwise.

No infinite recovery promise follows from retaining facts. Availability, capacity, conflicting evidence and operator deletion remain distinct concerns. No purge guarantee, cleanup mechanism, new field, identifier, store, threshold or clock protocol is proposed.

## Unchanged evidence appendices

The complete reviewed inventories accompany this decision unchanged:

1. Retention premise: `inventory-r8-retention-premise-2026-09-17.md`, SHA256 `fb937d0ef9c3c53e3a85deed5ee3d426f7cff3b786cc930767aa56d1881b2a40`.
2. Loop reachability: `inventory-r8-loop-retention-reachability-2026-09-17.md`, SHA256 `cd9c12ae52a35c8f7eb10f04892cba44b01ab1b9ba82ab96f98ecc9cdba89cec`.
3. Origin-age supplement: accepted checkpoint `bb968d55…`, independent INVENTORY PASS, 43/43 pins; reviewer independently verified WithTime/SetTimeout references.

Stop for independent pre-owner design review.
