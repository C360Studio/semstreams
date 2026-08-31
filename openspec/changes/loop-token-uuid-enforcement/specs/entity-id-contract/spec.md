## ADDED Requirements

### Requirement: A loop instance token is a framework-minted UUID

Every loop-execution instance token — dispatch conversations, rule-spawned loops, subagent loops, and
research-pipeline loops alike — MUST be minted by the framework as a version 4 UUID and carried in canonical RFC
4122 text form: 36 bytes, lowercase hexadecimal, hyphenated. No component, config, client, or tool call MAY author
a loop instance token, and no adopter-facing knob MAY configure or relax the contract; the validation predicate
MUST live module-internal with no exported surface. Every framework seam that accepts a pre-supplied loop token
MUST refuse a non-canonical one with a classified invalid error — a declared refusal, counted where an intake
counter exists, never a silent skip or truncated fallback: `TaskMessage.Validate` MUST refuse a task whose
`loop_id` is present and non-canonical (enforced by the rule engine before publishing and by agentic-loop intake,
which terminates delivery and counts the intake rejection); `LoopManager.CreateLoopWithID` MUST refuse before
registering any loop state; dispatch MUST refuse a non-canonical `reply_to` with a synchronous error response
naming the field; `agentrun.Mint` MUST refuse a non-canonical firing-loop instance (its scenario lives in the
graph-ingest capability, which owns Mint's refusal behavior). A mint path that composes its token through an
injectable generator MUST validate the generator's OUTPUT at the point of use, so an injected generator cannot
bypass the contract. Seam validation checks canonical form, not the version bits; minting is v4.

#### Scenario: a new conversation mints a full canonical UUID on every dispatch intake path

- **GIVEN** a running agentic-dispatch component
- **WHEN** a user message with no `reply_to` arrives via the HTTP submit path, and another via the channel path
- **THEN** each minted `loop_id` is a canonical 36-byte lowercase hyphenated UUID, with no prefix and no truncation
- **AND** the test that verifies this is `TestNewConversationMintsCanonicalUUID`

#### Scenario: a pre-filled non-UUID loop token is refused at loop intake, loudly

- **GIVEN** a decoded task message whose `loop_id` is `workflow-7`
- **WHEN** agentic-loop intake validates it
- **THEN** the delivery is terminated with a classified invalid error, the intake-rejection counter increments,
  and no loop state or context manager exists for the token
- **AND** a direct `CreateLoopWithID` call with the same token is refused before any state is registered
- **AND** the tests that verify this are `TestNonUUIDLoopIDIsTerminatedAtIntake` and
  `TestCreateLoopWithIDRefusesNonUUIDToken`

#### Scenario: a non-canonical reply_to fails synchronously at the client boundary

- **GIVEN** a client submitting a message whose `reply_to` is `loop_ab12cd34`
- **WHEN** dispatch handles the submission
- **THEN** the client receives a synchronous error response naming `reply_to`, and no task is published
- **AND** the test that verifies this is `TestNonUUIDReplyToGetsSynchronousError`

#### Scenario: the research pipeline mints canonical UUIDs and validates an injected generator

- **GIVEN** the `research_graph` tool with its default configuration
- **WHEN** it creates a research-pipeline loop
- **THEN** the loop token is a canonical UUID (no `rg_` prefix), in the AGENT_LOOPS key, the trigger key, and the
  loop-execution entity instance alike
- **AND WHEN** a generator injected via `WithResearchGraphIDGenerator` returns a non-canonical token
- **THEN** the tool call fails with a tool error and nothing is written
- **AND** the tests that verify this are `TestResearchLoopIDIsCanonicalUUID` and
  `TestInjectedGeneratorOutputIsValidated`
