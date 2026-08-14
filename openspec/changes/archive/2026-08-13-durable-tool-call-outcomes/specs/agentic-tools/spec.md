## ADDED Requirements

### Requirement: Tool-call completion SHALL be durable before request acknowledgement

`agentic-tools` SHALL own one immutable COMPLETED outcome per logical `ToolCall.ID`. It SHALL read that outcome before
execution, validate its version, stored call ID, complete V1 request fingerprint, and result correlation, and publish a
matching stored result without invoking an executor. Missing state SHALL permit execution. Corrupt, colliding, or
mismatched state SHALL terminate the delivery.

After execution or policy rejection, the component SHALL Create-CAS the complete outcome. On a Create collision it
SHALL read and validate the winner and publish that authoritative winner. A transient read, Create, winner-read, or
result-publication failure SHALL delayed-NAK. The request SHALL ACK only after synchronous result publication receives
its PubAck.

An initial `approval_required` result SHALL be nonterminal coordination and SHALL NOT be persisted as COMPLETED. It
SHALL use a phase-distinct deterministic message ID. An approved re-dispatch retains the original CallID; its approved
arguments and `ApprovedBy` form the terminal fingerprint and its terminal result uses the normal call-derived message
ID.

#### Scenario: completed call is redelivered after result publication failure

- **GIVEN** execution completed and its immutable outcome was created
- **AND** first result publication failed
- **WHEN** the request is redelivered
- **THEN** the stored result is published with the same deterministic message ID
- **AND** the executor invocation count remains one

#### Scenario: same call ID carries different request content

- **GIVEN** a completed outcome for a call ID
- **WHEN** a request with that ID has a different value in any ToolCall field
- **THEN** its V1 fingerprint does not match
- **AND** the delivery is terminated without executor invocation

### Requirement: Tool-result bounds SHALL be observed rather than predicted

The component SHALL first attempt the complete authoritative record and result. A typed observed full-record storage
rejection SHALL cause exactly one attempt to persist and publish a fixed compact correlated authority with
`ErrorKind=internal` and `Error=too_large`. The compact result SHALL retain only call, loop, and trace correlation
and SHALL contain no original content, error, metadata, or measured size. A compact rejection SHALL emit loud bounded
telemetry and terminate. The component SHALL NOT inspect configured payload limits or match error text.

If only publication of an already-stored full authority returns typed oversize, the component SHALL preserve that
authority and make exactly one compact transport-surrogate publication using the same call-derived message ID. A
surrogate PubAck permits request ACK. Surrogate failure SHALL terminate without recursion. Redelivery SHALL repeat the
full attempt followed by at most one surrogate attempt.

#### Scenario: full outcome exceeds the observed KV transport bound

- **GIVEN** the real full Create returns a typed max-payload rejection
- **WHEN** the component handles that observation
- **THEN** it attempts one compact COMPLETED Create and result publication
- **AND** it makes no recursive fallback attempt

### Requirement: Executor panic and ambiguous pre-completion effects SHALL be explicit

An executor panic SHALL be recovered into a compact correlated internal result and follow normal completion. Exported
executor contracts SHALL state that effectful implementations use `ToolCall.ID` for downstream idempotency because a
failure after an effect but before COMPLETED persistence can redeliver the call.

#### Scenario: executor panics

- **WHEN** an executor panics
- **THEN** agentic-tools remains running
- **AND** persists and publishes a compact internal result without panic details

### Requirement: Durable outcome telemetry SHALL use a closed bounded vocabulary

The component SHALL expose exactly these counter families and label values:

- `outcome_total{path}`: `new`, `replay`, `rejection`, `compact`;
- `outcome_store_failures_total{operation,reason}`: operation `get`, `create`, `read_winner`; reason `transport`,
  `oversize`, `corrupt`;
- `outcome_collisions_total` without labels;
- `result_publish_failures_total{reason}`: `transport`, `oversize`, `marshal`;
- `ambiguous_redeliveries_total{cause}`: `store_failure`, `shutdown`, `heartbeat`, `panic`.

Call IDs and tool names SHALL NOT be metric labels. Ambiguous paths SHALL log `ambiguous_effect=true`.

#### Scenario: an effect completes but outcome Create fails

- **WHEN** the executor returns after a possible effect and outcome Create fails transiently
- **THEN** `ambiguous_redeliveries_total{cause="store_failure"}` increments
- **AND** the error log carries `ambiguous_effect=true`
- **AND** the delivery is delayed-NAKed
