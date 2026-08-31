## ADDED Requirements

### Requirement: A loop instance token is a framework-minted UUID

Every loop-execution instance token — dispatch conversations, rule-spawned loops, subagent loops, and
research-pipeline loops alike — MUST be minted by the framework as a version 4 UUID and carried in canonical RFC
4122 text form: 36 bytes, lowercase hexadecimal, hyphenated. No component, config, client, or tool call MAY author
a loop instance token, and no adopter-facing knob MAY configure or relax the contract; the validation predicate
MUST live module-internal with no exported surface. Every framework seam that accepts a pre-supplied loop token
MUST refuse a non-canonical one with a classified invalid error — a declared refusal, counted where an intake
counter exists, never a silent skip or truncated fallback: `TaskMessage.Validate` MUST refuse a task
carrying ANY loop-token field — `loop_id`, `parent_loop_id`, `in_reply_to`, or `run_id` — that is present and
non-canonical (enforced by the rule engine before publishing and by agentic-loop intake, which terminates delivery
and counts the intake rejection), so no client-authored token reaches the graph write path, whose parent and
reply stamping composes through the panicking entity-ID builder; `LoopManager.CreateLoopWithID` MUST refuse
before registering any loop state; dispatch MUST refuse a non-canonical continuation token with a typed error
response naming the field — synchronous on the HTTP submit path, published to the response subject on the channel
path — validating the RESOLVED token after auto-continue resolution and before minting, so both the client's
`reply_to` and an auto-continued value pass one check; `agentrun.Mint` MUST refuse a non-canonical firing-loop
instance (its scenario lives in the graph-ingest capability, which owns Mint's refusal behavior). Seam validation
checks canonical form, not the version bits; minting is v4.

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

#### Scenario: a non-canonical reply_to fails at the client boundary on both intake paths

- **GIVEN** a client submitting a message whose `reply_to` is `loop_ab12cd34`
- **WHEN** dispatch handles it on the HTTP submit path
- **THEN** the client receives a synchronous error response naming `reply_to`, and no task is published
- **AND WHEN** the same message arrives on the channel path
- **THEN** an error response naming `reply_to` is published to the response subject, and no task is published
- **AND** the tests that verify this are `TestNonUUIDReplyToHTTPGetsSynchronousError` and
  `TestNonUUIDReplyToChannelGetsErrorResponse`

#### Scenario: a task carrying any non-canonical loop-token field is refused

- **GIVEN** three decoded tasks: one whose `in_reply_to` is `workflow-7`, one whose `run_id` is `loop_ab12cd34`,
  one whose `parent_loop_id` is `e2e-parent-1`
- **WHEN** `TaskMessage.Validate` runs on each
- **THEN** each is refused with an error naming the offending field, and no loop state, triple, or run
  association is created for any of them
- **AND** the test that verifies this is `TestTaskMessageRefusesNonCanonicalLoopTokenFields`

#### Scenario: the research pipeline mints canonical UUIDs

- **GIVEN** the `research_graph` tool
- **WHEN** it creates a research-pipeline loop
- **THEN** the loop token is a canonical UUID (no `rg_` prefix), in the AGENT_LOOPS key, the trigger key, and the
  loop-execution entity instance alike — the generator-injection option is deleted, so no path can author one
- **AND** the test that verifies this is `TestResearchLoopIDIsCanonicalUUID`
