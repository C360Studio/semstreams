## MODIFIED Requirements

### Requirement: Wire tool execution does not infer local parallelism from acknowledgement admission

The agentic-tools `tool.execute.>` path SHALL produce the exact correlated durable terminal outcome and result for each
logical tool call that reaches terminal execution or terminal policy rejection. Redelivery of an already-COMPLETED
logical call SHALL publish that same correlated terminal result without executor re-invocation.

An initial `approval_required` interception is correlated nonterminal coordination, not terminal execution or terminal
policy rejection. It SHALL retain the existing phase-distinct result message ID, SHALL NOT create a COMPLETED outcome,
and SHALL leave the same CallID eligible for approved re-dispatch. The approved re-dispatch enters the terminal
guarantee when it reaches execution or a terminal policy rejection.

The component SHALL NOT claim that `MaxAckPending=3` supplies local executor parallelism. That value governs
delivered-but-unacknowledged admission only. The wire contract SHALL promise neither serialized execution nor execution
overlap to executor authors or direct callers.

Multiple queued calls that reach terminal execution or terminal policy rejection SHALL each produce their exact
correlated durable terminal result. Correctness SHALL be proved by exact call/result causality under a finite liveness
bound, not by elapsed wall-clock classification. The current implementation uses one native callback through outcome
persistence, result publication, and delivery settlement before that callback returns. That is nonnormative
implementation evidence, not a stable serialized-execution contract.

#### Scenario: multiple terminal wire calls settle

- **GIVEN** three wire calls with distinct call IDs
- **AND** none is intercepted for approval
- **AND** each reaches terminal execution or terminal policy rejection
- **WHEN** the wire consumer processes them
- **THEN** each logical call produces its exact correlated durable terminal result
- **AND** the proof uses no elapsed-time threshold

#### Scenario: approval-required is a nonterminal correlated pause

- **GIVEN** a wire call that passes global and per-loop admission
- **AND** `approval_required` intercepts it before execution
- **WHEN** the initial delivery settles
- **THEN** the component publishes the existing correlated approval-required result with its phase-distinct message ID
- **AND** it creates no COMPLETED outcome
- **AND** an approved re-dispatch with the same CallID remains eligible for terminal execution

#### Scenario: acknowledgement admission is three

- **GIVEN** agentic-tools uses its component-owned `MaxAckPending=3`
- **WHEN** the consumer is observed
- **THEN** the value bounds delivered-but-unacknowledged messages
- **AND** no executor-concurrency claim is inferred
