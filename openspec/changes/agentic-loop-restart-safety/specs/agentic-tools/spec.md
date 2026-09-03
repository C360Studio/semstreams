## ADDED Requirements

### Requirement: Tool outcomes preserve framework execution correlation

Agentic-tools SHALL preserve RequestID and framework execution identity from `ToolCall` onto every `ToolResult`,
including approval-required, compacted, panic, failure, and completed-outcome replay results.

#### Scenario: Executor returns a result without correlation fields

- **WHEN** an executor hosted by agentic-tools returns its domain result
- **THEN** agentic-tools stamps the originating RequestID and execution identity
- **AND** the executor author is not required to manage settlement correlation

### Requirement: Completed tool outcome identity is globally unambiguous

`TOOL_CALL_OUTCOMES` SHALL key and fingerprint completed outcomes using framework execution identity while retaining
provider CallID as conversation data.

#### Scenario: Provider CallID repeats across turns

- **WHEN** two calls share provider CallID but have different RequestIDs
- **THEN** they create distinct completed-outcome identities
- **AND** replay returns only the result matching the exact execution identity

### Requirement: Tool replay remains the sole tool-effect recovery authority

Agentic-tools SHALL NOT add a claimed, started, checkpoint, or second outcome ledger for #1146. Post-effect and
pre-completion ambiguity remains governed by the executor's operation-specific idempotency contract.

#### Scenario: Delivery repeats after a completed tool outcome

- **WHEN** a tool delivery repeats and its exact completed outcome exists
- **THEN** agentic-tools replays that outcome
- **AND** does not consult or create another tool-effect ledger

### Requirement: Tool delivery retains the permanent typed owner contract

The existing `tool.execute` binding SHALL continue to use the permanent typed heartbeat owner, retain
`TOOL_CALL_OUTCOMES` as its sole completed authority, drain its exact consume handle, await exact `Closed`, and join
its owner-stop observer. Correlation changes SHALL NOT introduce a second outcome owner or expose native settlement
to executors.

#### Scenario: Correlation migration preserves completed replay

- **WHEN** a completed outcome is read under framework execution identity
- **THEN** exact ToolResult replay occurs without executor invocation
- **AND** provider CallID remains present as conversation data

### Requirement: Tool-result publication identity is deterministic and reconcilable

Every required `ToolResult` publication SHALL derive its identity from framework execution identity and canonical
result content and SHALL use deterministic `Nats-Msg-Id`. On redelivery, agentic-tools SHALL reconcile the exact
immutable `TOOL_CALL_OUTCOMES` entry before executor invocation or republication. A matching outcome is replayed; a
conflicting fingerprint quarantines. No general stream scan or second tool authority is introduced.

#### Scenario: Completed result publication repeats

- **WHEN** a completed tool delivery repeats after an uncertain result PubAck
- **THEN** exact outcome replay uses the same execution-derived `Nats-Msg-Id`
- **AND** the executor is not invoked again

#### Scenario: Completed outcome content conflicts

- **WHEN** the expected execution identity names a different canonical result
- **THEN** agentic-tools quarantines without selecting or overwriting either outcome

## MODIFIED Requirements

### Requirement: Tool-call completion SHALL be durable before request acknowledgement

`agentic-tools` SHALL own one immutable COMPLETED outcome per framework execution identity derived from RequestID,
provider CallID, and positive call ordinal. Provider `ToolCall.ID` SHALL remain conversation data and SHALL NOT be
the completed-outcome key. Before execution, agentic-tools SHALL read the exact outcome and validate its version,
execution identity, RequestID, provider CallID, ordinal, complete V1 request fingerprint, and result correlation. A
matching outcome SHALL be published without executor invocation. Missing state SHALL permit execution only under the
executor's admitted retry contract. Corrupt, colliding, or mismatched state SHALL quarantine the delivery.

After execution or terminal policy rejection, the component SHALL Create-CAS the complete outcome. On a Create
collision it SHALL read and validate the winner and publish that authoritative winner. A transient read, Create,
winner-read, or result-publication failure SHALL return Retry. The request SHALL positively settle only after
synchronous result publication receives PubAck.

An initial `approval_required` result SHALL be nonterminal coordination and SHALL NOT be persisted as COMPLETED. It
SHALL use a phase-distinct deterministic `Nats-Msg-Id` derived from execution identity. An approved redispatch
retains RequestID, provider CallID, ordinal, and execution identity; its approved arguments and `ApprovedBy` form the
terminal fingerprint, and its terminal result uses the stable execution-derived terminal `Nats-Msg-Id`.

#### Scenario: completed call is redelivered after result publication failure

- **GIVEN** execution completed and its immutable outcome was created
- **AND** first result publication failed
- **WHEN** the request is redelivered
- **THEN** the stored result is published with the same execution-derived deterministic message ID
- **AND** the executor invocation count remains one

#### Scenario: same provider call ID carries different execution content

- **GIVEN** a completed outcome for one execution identity
- **WHEN** a request repeats its provider CallID under a different RequestID, ordinal, or canonical ToolCall content
- **THEN** it resolves to a distinct execution identity or a non-matching V1 fingerprint
- **AND** no completed outcome is selected by provider CallID alone
- **AND** an identity collision is quarantined without executor invocation

### Requirement: Tool-result bounds SHALL be observed rather than predicted

The component SHALL first attempt the complete authoritative record and result. A typed observed full-record storage
rejection SHALL cause exactly one attempt to persist and publish a fixed compact correlated authority with
`ErrorKind=internal` and `Error=too_large`. The compact result SHALL retain only RequestID, execution identity,
provider call, loop, and trace correlation and SHALL contain no original content, error, metadata, or measured size.
A compact rejection SHALL emit loud bounded telemetry and terminate. The component SHALL NOT inspect configured
payload limits or match error text.

If only publication of an already-stored full authority returns typed oversize, the component SHALL preserve that
authority and make exactly one compact transport-surrogate publication using the same execution-derived
deterministic `Nats-Msg-Id`. A surrogate PubAck permits request ACK. Surrogate failure SHALL terminate without
recursion. Redelivery SHALL repeat the full attempt followed by at most one surrogate attempt, after exact completed-
outcome reconciliation.

#### Scenario: full outcome exceeds the observed KV transport bound

- **GIVEN** the real full Create returns a typed max-payload rejection
- **WHEN** the component handles that observation
- **THEN** it attempts one compact COMPLETED Create and result publication under the exact execution identity
- **AND** it makes no recursive fallback attempt

### Requirement: Executor panic and ambiguous pre-completion effects SHALL be explicit

An executor panic SHALL be recovered into a compact correlated internal result and follow normal completion.
Effectful executor contracts SHALL declare their operation-specific idempotency or reconciliation key and behavior;
the framework SHALL NOT claim that provider `ToolCall.ID` alone makes an external effect idempotent. If an executor
cannot reconcile an effect after failure between the effect and COMPLETED persistence, the ambiguity SHALL remain a
typed, metered retry risk and SHALL NOT be presented as exactly-once execution.

#### Scenario: executor panics

- **WHEN** an executor panics
- **THEN** agentic-tools remains running
- **AND** persists and publishes a compact internal result carrying RequestID and execution identity without panic
  details

#### Scenario: effect completed but durable outcome is unknown

- **WHEN** an effectful executor returns and COMPLETED persistence is transiently unresolved
- **AND** the executor declares no operation-specific effect reconciliation
- **THEN** agentic-tools returns Retry and records the ambiguity
- **AND** it does not claim `ToolCall.ID` prevented a repeated external effect
