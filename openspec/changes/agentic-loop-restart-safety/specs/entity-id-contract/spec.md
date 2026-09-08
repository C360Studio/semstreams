## ADDED Requirements

### Requirement: A loop instance token is minted at its framework birth seam

Every loop-execution instance token — dispatch conversations, rule-spawned loops, subagent loops, and
research-pipeline loops alike — MUST be minted at its framework birth seam as a version 4 UUID and carried in
canonical RFC 4122 text form: 36 bytes, lowercase hexadecimal, hyphenated. For a newly published `TaskMessage`, the
task producer is that birth seam and MUST mint before validation, envelope marshal, and publication. A continuation
producer MUST echo the admitted existing LoopID. End-user input, configuration, and tool-call arguments MUST NOT
choose a birth token, and no adopter-facing knob MAY configure or relax the contract. A component that directly
constructs and publishes `TaskMessage` participates in the framework task-production seam. The validation predicate
MUST live module-internal with no exported surface.

**Enforcement is FORM, not provenance — and the difference is load-bearing.** The framework validates that a
supplied token is a canonical UUID. It does not, and with a form predicate cannot, detect who minted it: a client
that authors a fresh canonical UUID and supplies it as `reply_to` is ACCEPTED. Producer-local minting is therefore
the contract asked of direct task producers, not a provenance property any seam verifies. Two consequences a reader
MUST NOT infer away. First, form alone confers no isolation: what a party may DO with a token it holds is decided by
the agentic-dispatch admission gate, which checks existence and ownership at every seam that attaches to a loop — and
those checks are correctness and accident-prevention guards, not authorization, because caller identity on that plane
is asserted by the caller. A multi-tenant deployment MUST NOT rely on loop tokens, or on that gate, for isolation
between mutually untrusted parties; authorization is a separate contract (epic #1205). Second, the backstop against
adopting a token this deployment did not mint is `agentrun.Mint`'s origin-entity-ID mismatch refusal, not the form
predicate.

Every accepting operation boundary that receives a supplied loop token classifies a form-validation failure as
invalid — a declared refusal, counted where an intake counter exists, never a silent skip or truncated fallback. The
validator itself returns an ordinary field-naming error. The census is complete and closed; a new carrier of a loop
token joins it rather than validating non-emptiness of its own:

- `TaskMessage.Validate` MUST first return an ordinary validation error naming `loop_id` when LoopID is absent, then
  MUST refuse a task carrying ANY loop-token field — `loop_id`, `parent_loop_id`, `in_reply_to`, or `run_id` — that
  is present and non-canonical. The rule publish boundary, agentic-loop intake, and direct `MessageHandler.HandleTask`
  boundary MUST classify that validation failure as invalid before side effects. Agentic-loop intake MUST terminate
  the delivery and count the intake rejection, so no absent or NON-CANONICAL token reaches loop state or the graph
  write path, whose parent and reply stamping composes through the panicking entity-ID builder.
- `LoopManager.CreateLoopWithID` MUST refuse before registering any loop state.
- Dispatch MUST refuse a non-canonical resolved continuation token, `run_id`, or `in_reply_to` on an inbound
  submission, with a typed error response naming the offending field — synchronous on the HTTP submit path,
  published to the response subject on the channel path — validating after auto-continue resolution and BEFORE
  minting, before the loop is tracked, and before the loop-started metric is recorded, so that a refused submission
  leaves neither a tracked loop nor a moved active-loops gauge, and so both the client's `reply_to` and an
  auto-continued value pass one check.
- `agentrun.Mint` MUST refuse a non-canonical firing-loop instance (its scenario lives in the graph-ingest capability,
  which owns Mint's refusal behavior).
- Every remaining payload that carries a loop token MUST refuse a non-canonical one in its own `Validate`: the user
  control signal, the approval response, and the approval-pending event. A fourth carrier existed — dispatch's own
  control-signal message, whose `Validate` returned nil unconditionally — and is RETIRED rather than repaired: it had
  one producer, zero consumers, and no identity fields, and the seam that produced it,
  `POST /loops/{id}/signal`, is itself deleted. A carrier with no reader is deleted, not validated.
- Every loop-scoped HTTP endpoint MUST refuse a non-canonical loop id taken from its URL path, before the existence
  check, so a malformed token is answered as malformed rather than as not found.

Seam validation checks requiredness and canonical form, not the version bits or provenance; v4 is asserted at the
framework birth mint sites. For a new task, each producer execution mints once before marshaling. Retry of that same
already-marshaled publication and downstream redelivery of its retained AGENT bytes reuse the same bytes. A fresh
execution of an upstream producer is a separate production attempt outside this identity-reuse claim.

#### Scenario: a new conversation mints a full canonical UUID on every dispatch intake path

- **GIVEN** a running agentic-dispatch component
- **WHEN** a user message with no `reply_to` arrives via the HTTP submit path, and another via the channel path
- **THEN** each minted `loop_id` is a canonical 36-byte lowercase hyphenated UUID, with no prefix and no truncation
- **AND** the test that verifies this is `TestNewConversationMintsCanonicalUUID`

#### Scenario: a task without loop identity is refused before state

- **GIVEN** a decoded TaskMessage whose `loop_id` is absent
- **WHEN** `TaskMessage.Validate` runs
- **THEN** it returns an ordinary validation error naming `loop_id`
- **AND WHEN** the rule publish boundary, durable agentic-loop intake, or direct `MessageHandler.HandleTask` receives it
- **THEN** that operation boundary classifies the validation failure as invalid before any side effect
- **AND** durable intake terminates the delivery and increments the intake-rejection counter
- **AND** no loop, context, graph fact, request, event, map entry, or recovery state is created

#### Scenario: a pre-filled non-UUID loop token is refused at loop intake, loudly

- **GIVEN** a decoded task message whose `loop_id` is `workflow-7`
- **WHEN** agentic-loop intake validates it
- **THEN** the delivery is terminated with a classified invalid error, the intake-rejection counter increments,
  and no loop state or context manager exists for the token
- **AND** a direct `CreateLoopWithID` call with the same token is refused before any state is registered
- **AND** the tests that verify this are `TestNonUUIDLoopIDIsTerminatedAtIntake` and
  `TestCreateLoopWithIDRefusesNonUUIDToken`

#### Scenario: a non-canonical reply_to fails at the client boundary on both intake paths

- **GIVEN** a client submitting a message whose `reply_to` is `loop_ab12cd34`
- **WHEN** dispatch handles it on the HTTP submit path
- **THEN** the client receives a synchronous error response naming `reply_to`, and no task is published
- **AND WHEN** the same message arrives on the channel path
- **THEN** an error response naming `reply_to` is published to the response subject, and no task is published
- **AND** the tests that verify this are `TestNonUUIDReplyToHTTPGetsSynchronousError` and
  `TestNonUUIDReplyToChannelGetsErrorResponse`

#### Scenario: a client-authored run_id or in_reply_to is refused at dispatch, before any state is recorded

- **GIVEN** a client submitting a message whose `reply_to` is absent but whose `run_id` is `run-42`
- **WHEN** dispatch handles it on the HTTP submit path
- **THEN** the client receives a synchronous error response naming `run_id`, no task is published, no loop is
  tracked, and the active-loops gauge does not move
- **AND WHEN** a message whose `in_reply_to` is non-canonical arrives on the channel path
- **THEN** an error response naming `in_reply_to` is published to the response subject rather than the submitter
  being left without an answer, and no loop is tracked
- **AND** the tests that verify this are `TestNonUUIDRunIDHTTPGetsSynchronousError` and
  `TestNonUUIDInReplyToChannelGetsErrorResponse`

#### Scenario: a canonical token is accepted on its form alone, whoever authored it

- **GIVEN** a client submitting a message whose `reply_to` is a canonical UUID that this framework never minted
- **WHEN** the form check runs
- **THEN** it PASSES — form is the whole of this requirement, and provenance is not verified at any seam
- **AND WHEN** the admission gate then runs
- **THEN** the submission is refused as naming no such loop, because a token this framework never minted names no
  loop to continue — the refusal comes from existence, not from form, and the two are different axes
- **AND** the tests that verify this are `TestCanonicalReplyToPassesFormCheck` and
  `TestUnmintedCanonicalReplyToIsRefusedAsNotFound`

#### Scenario: a task carrying any non-canonical loop-token field is refused

- **GIVEN** three decoded tasks: one whose `in_reply_to` is `workflow-7`, one whose `run_id` is `loop_ab12cd34`,
  one whose `parent_loop_id` is `e2e-parent-1`
- **WHEN** `TaskMessage.Validate` runs on each
- **THEN** each is refused with an error naming the offending field, and no loop state, triple, or run association is
  created for any of them
- **AND** the test that verifies this is `TestTaskMessageRefusesNonCanonicalLoopTokenFields`

#### Scenario: the research pipeline mints canonical UUIDs

- **GIVEN** the `research_graph` tool
- **WHEN** it creates a research-pipeline loop
- **THEN** the loop token is a canonical UUID (no `rg_` prefix), in the AGENT_LOOPS key, the trigger key, and the
  loop-execution entity instance alike — the generator-injection option is deleted, so no path can author one
- **AND** the test that verifies this is `TestResearchLoopIDIsCanonicalUUID`

#### Scenario: every remaining loop-token carrier refuses a non-canonical token

- **GIVEN** a user control signal, an approval response, and an approval-pending event, each carrying the loop token
  `loop_ab12cd34`
- **WHEN** each is validated
- **THEN** each is refused with a classified invalid error naming its loop-token field, and none is published or acted
  on
- **AND** no payload type carrying a loop token remains whose validation accepts every input
- **AND** the tests that verify this are `TestUserSignalRefusesNonCanonicalLoopID`,
  `TestApprovalResponseRefusesNonCanonicalLoopID`, `TestApprovalPendingEventRefusesNonCanonicalLoopID`, and
  `TestNoLoopTokenCarrierAcceptsEveryInput`

#### Scenario: a non-canonical loop id in a URL path is refused before the existence check

- **GIVEN** requests to the loop read and loop approval HTTP endpoints whose path loop id is `loop_ab12cd34`
- **WHEN** each handler runs
- **THEN** each answers with a bad-request refusal naming the token form, not a not-found answer, and no approval is
  published
- **AND** the test that verifies this is `TestLoopEndpointsRefuseNonCanonicalPathToken`

## REMOVED Requirements

### Requirement: A loop instance token is a framework-minted UUID

**Reason**: Restart review proved that an empty retained `TaskMessage` lets agentic-loop mint a second identity after
process replacement. The replacement requirement names the actual framework birth seam: every new-task producer mints
before validation and durable publication, while agentic-loop validates and never repairs absent identity.

**Migration**: Every direct `TaskMessage` producer supplies a canonical v4 LoopID before validation and marshal; a
continuation producer echoes the admitted existing LoopID. Retry an uncertain publication with the same serialized
bytes. There is no empty-ID compatibility path, helper API, scan, mapping, ledger, bucket, or consumer recovery owner.
