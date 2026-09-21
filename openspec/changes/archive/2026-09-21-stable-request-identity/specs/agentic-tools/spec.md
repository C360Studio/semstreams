# agentic-tools Delta

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

### Requirement: Tool-result publication is durably at-least-once

Every required `ToolResult` publication SHALL carry framework execution identity and receive PubAck before source
ACK. PubAck uncertainty MAY repeat a result. `Nats-Msg-Id` MAY provide bounded duplicate suppression but SHALL NOT be
treated as permanent publication identity.

The exact immutable `TOOL_CALL_OUTCOMES` read exists only at the executor-effect boundary. Before executor invocation,
a matching outcome is replayed and a conflicting fingerprint SHALL terminate the delivery. Ordinary ToolResult
republication requires no second exact output lookup, general stream scan, or second tool authority.

Terminate, not quarantine, is the correct disposition for a fingerprint conflict: the stored outcome and the arriving
call disagree about what one execution identity names, which is a property of that message and cannot change on
redelivery. Only a produced-but-unknown durable effect — an ambiguous outcome `Create` — is a quarantine, because
that is a property of the store rather than the message and it is what the operator must inspect. The framework
producer derives the execution identity from RequestID, provider CallID and call ordinal and so cannot produce a
conflicting fingerprint under one identity; a conflict means a foreign or diverging producer, and terminating one
such delivery leaves the rest of the consumer running.

#### Scenario: Completed result publication repeats

- **WHEN** a completed tool delivery repeats after an uncertain result PubAck
- **THEN** the stored outcome may be published again with the same framework execution identity
- **AND** the executor is not invoked again

#### Scenario: Completed outcome content conflicts

- **WHEN** the expected execution identity names a different canonical result
- **THEN** agentic-tools terminates that delivery without selecting or overwriting either outcome
- **AND** the consumer keeps running, because the conflict is a property of the message and not of the store

#### Scenario: An outcome write leaves a durable effect unknown

- **WHEN** the completed-outcome `Create` neither confirms nor refuses
- **THEN** agentic-tools quarantines, which is the disposition that stops the owner for inspection
- **AND** the executor is not invoked again on that delivery

## MODIFIED Requirements

### Requirement: Tool-call completion SHALL be durable before request acknowledgement

`agentic-tools` SHALL own one immutable COMPLETED outcome per framework execution identity, retaining the provider
`ToolCall.ID` as conversation data rather than as the outcome's key: a provider may reuse a CallID on a later turn
of one conversation, so it names a message and not an execution. It SHALL read that outcome before execution,
validate its version, stored execution identity, complete V1 request fingerprint, and result correlation, and
publish a matching stored result without invoking an executor. Missing state SHALL permit execution. Corrupt,
colliding, or mismatched state SHALL terminate the delivery. A call carrying no execution correlation SHALL
terminate BEFORE the outcome read, because there is no identity to look an outcome up under.

After execution or policy rejection, the component SHALL Create-CAS the complete outcome. On a Create collision it
SHALL read and validate the winner and publish that authoritative winner. A transient read, Create, winner-read, or
result-publication failure SHALL delayed-NAK. The request SHALL ACK only after synchronous result publication receives
its PubAck.

An initial `approval_required` result SHALL be nonterminal coordination and SHALL NOT be persisted as COMPLETED. It
SHALL use a phase-distinct deterministic message ID derived from the execution identity. An approved re-dispatch
retains the original provider CallID AND the original execution identity, so its terminal outcome lands under the
identity the approval was given for; its approved arguments and `ApprovedBy` form the terminal fingerprint and its
terminal result uses the normal execution-derived message ID.

#### Scenario: completed call is redelivered after result publication failure

- **GIVEN** execution completed and its immutable outcome was created
- **AND** first result publication failed
- **WHEN** the request is redelivered
- **THEN** the stored result is published with the same deterministic message ID
- **AND** the executor invocation count remains one

#### Scenario: same call ID carries different request content

- **GIVEN** a completed outcome for an execution identity
- **WHEN** a request under that same execution identity has a different value in any ToolCall field
- **THEN** its V1 fingerprint does not match
- **AND** the delivery is terminated without executor invocation

### Requirement: Tool-result bounds SHALL be observed rather than predicted

The component SHALL first attempt the complete authoritative record and result. A typed observed full-record storage
rejection SHALL cause exactly one attempt to persist and publish a fixed compact correlated authority with
`ErrorKind=internal` and `Error=too_large`. The compact result SHALL retain only framework execution correlation —
RequestID, execution identity and call ordinal — together with call, loop, and trace correlation, and SHALL contain
no original content, error, metadata, or measured size. Dropping the execution correlation would make the compact
result unroutable, since the loop addresses results by execution identity. A compact rejection SHALL emit loud
bounded telemetry and terminate. The component SHALL NOT inspect configured payload limits or match error text.

If only publication of an already-stored full authority returns typed oversize, the component SHALL preserve that
authority and make exactly one compact transport-surrogate publication using the same execution-derived message ID.
A surrogate PubAck permits request ACK. Surrogate failure SHALL terminate without recursion. Redelivery SHALL repeat
the full attempt followed by at most one surrogate attempt.

#### Scenario: full outcome exceeds the observed KV transport bound

- **GIVEN** the real full Create returns a typed max-payload rejection
- **WHEN** the component handles that observation
- **THEN** it attempts one compact COMPLETED Create and result publication
- **AND** it makes no recursive fallback attempt

### Requirement: Executor panic and ambiguous pre-completion effects SHALL be explicit

An executor panic SHALL be recovered into a compact correlated internal result and follow normal completion. Exported
executor contracts SHALL state that effectful implementations use the framework execution identity for downstream
idempotency because a failure after an effect but before COMPLETED persistence can redeliver the call. They SHALL NOT
name `ToolCall.ID` for that purpose: it is the provider's token, it may repeat across turns of one conversation, and
an implementation keyed on it would treat a later, different invocation as an already-performed effect.

#### Scenario: executor panics

- **WHEN** an executor panics
- **THEN** agentic-tools remains running
- **AND** persists and publishes a compact internal result without panic details
