# Review: operational LoopEntity design draft v1

DESIGN CHANGES REQUESTED

Reviewed exact design SHA-256 `56b87f8857b4e7a553e4bacde18b34781c6b81bee752f0ee6bdb52b1a24664b6`.
Three bounded corrections:

1. BLOCKING design:224 — Unmeasured live-state disposal obligation.
   The draft requires a drain/disposal procedure without evidence of retained deployed state. No in-place rewrite
   does not resolve that unsupported migration requirement. Replace it with the pre-v1 newly provisioned-storage
   contract and relevant cold-start/E2E proof. If retained deployed state is discovered, require its separate
   owner-reviewed migration/recovery design. This follows reviewer-contract lines 171–176.
2. HIGH design:118 — Local approval validity and durable correlation are conflated.
   BeginAwaitingApproval cannot supply RequestID, ExecutionID or ordinal through its preserved signature, while
   ResolveApproval requires a coherent current gate. Existing internal stamping at handlers.go:2453 supports the
   production caller, but does not define the exported API's behavior. Specify local rules for Validate, Begin and
   Resolve so callers need no new identity-stamping sequence. Keep retained request/result correlation in the
   existing private delivery owner; clarify that startup installation does not require otherwise-unavailable
   stream evidence.
3. HIGH design:76 — Direct transition coherence remains undecided.
   The edge prose permits running→awaiting without constructing pending data and awaiting→running without defining
   its removal. Lines 123–128 describe manager installation but leave direct TransitionTo behavior ambiguous.
   Specify its exact mutation/refusal behavior for gate edges and contradictory same-state records, including which
   terminal fields are local versus settlement-owned. Add those direct-call cases to the focused proof requirements.

The operational meanings, explicit import cost, separate AgentRun authority, ordinary-chat behavior and retained
settlement guarantees otherwise support owner review.

No runtime changes or tests performed. Inventory PASS remains unchanged.
DESIGN CHANGES REQUESTED: resolve items 1–3.

Reviewer: `/root/state_contract_review`, semstreams-reviewer. Root materialized the verdict; it is not owner approval.
