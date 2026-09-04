## ADDED Requirements

### Requirement: First-party publish-agent output is admitted before action execution

A rule processor whose own resolved `PortFacts` declare a JetStream output covering `agent.task.*` SHALL validate
that output's observed AGENT stream through the shared repo-internal agent-stream admission function before its
action evaluator can execute `publish_agent`. The requirement SHALL be derived only from that processor's resolved
port facts and local producer PubAck dependency. The processor SHALL preserve the configured port name, including
both shipped spellings `agent.task` and `agent_task`; it SHALL NOT infer activation from a factory name, global raw
configuration, another component's configuration, or a scan for current rule definitions.

The six shipped rule-processor configurations with this output SHALL use the same gate. A rule processor without a
resolved AGENT task output SHALL perform zero AGENT admission lookup. The implementation SHALL reuse
`internal/agentstreamadmission.ObserveAndValidate`; it SHALL NOT add another gate, durable state, watcher, or exported
admission API.

#### Scenario: all shipped output-name spellings are admitted

- **GIVEN** each of the six shipped rule processors with its resolved AGENT task output
- **WHEN** the processor prepares to start its action evaluator
- **THEN** its exact local output stream is observed and admitted through the shared validator
- **AND** both `agent.task` and `agent_task` port names retain their configured identity

#### Scenario: non-agentic rule processor pays no lookup

- **GIVEN** a rule processor whose resolved outputs do not include the AGENT task family
- **WHEN** it starts its action evaluator
- **THEN** no AGENT StreamInfo lookup occurs

#### Scenario: refused dependency prevents action admission

- **GIVEN** a rule processor with a resolved AGENT task output whose observed stream violates its caller-local
  producer requirement
- **WHEN** the component starts
- **THEN** its action evaluator does not start
- **AND** no `publish_agent` action is attempted or reported as successfully fired

### Requirement: Publish-agent classification uses canonical wildcard coverage and durable publication

The existing rule `actionPublisher` SHALL classify a fully substituted `publish_agent` subject against its own
resolved JetStream output facts by calling
`component/flowgraph.SubjectCovers(declaredFilter, concreteSubject)` in that exact directional order. A concrete
subject covered by the declared `agent.task.*` output SHALL publish through `PublishToStream` and receive synchronous
PubAck. Exact string equality SHALL NOT classify wildcard declarations, and static composition coverage SHALL NOT
substitute for runtime durable publication.

An uncovered or malformed subject, absent publisher, refused AGENT dependency, marshal failure, or missing PubAck
SHALL fail the action before any post-send `rule.task.spawned` side effect. No core-NATS fallback is allowed for a
`publish_agent` output declared as JetStream. The existing publisher and shared admission validator remain the only
runtime owners. `component/flowgraph` owns the canonical matcher; graph-level composition is its existing caller and
connection owner. This capability SHALL NOT add a second matcher, classifier, gate, or public API.

#### Scenario: static first-party rule receives PubAck

- **GIVEN** any of the eleven static `publish_agent` definitions loaded by the four shipped producer configurations
- **WHEN** its substituted concrete subject is covered by that processor's resolved `agent.task.*` output
- **THEN** the existing publisher uses JetStream and receives PubAck
- **AND** the row-15 durable consumer can rely on a durably admitted source publication

#### Scenario: declaration-only configuration cannot regress to core NATS

- **GIVEN** either shipped declaration-only rule processor and a future loaded `publish_agent` definition
- **WHEN** the concrete subject is covered by its resolved AGENT task output
- **THEN** the same wildcard classifier selects JetStream
- **AND** no exact-subject comparison sends the task through core NATS

#### Scenario: uncovered dynamic subject refuses before side effects

- **GIVEN** a dynamic `publish_agent` subject whose substitution is not covered by the processor's resolved
  JetStream outputs
- **WHEN** the action executes
- **THEN** it returns a typed failure without publisher fallback
- **AND** no spawned-task triple or success metric is written

### Requirement: Publish-agent preserves the registered payload boundary

`publish_agent` SHALL construct and validate `agentic.TaskMessage`, wrap it in a registered `BaseMessage`, and publish
that envelope through the admitted output. `TaskMessage` is a `message.Payload` with the control indexing profile;
it is not `graph.Graphable`, and neither publisher admission nor durable publication SHALL require it to implement
`EntityID` or `Triples`. Repository-wide envelope census and unrelated publisher migrations remain outside this
capability.

#### Scenario: registered non-Graphable task is accepted

- **GIVEN** a valid registered `TaskMessage` that does not implement `graph.Graphable`
- **WHEN** `publish_agent` constructs and publishes its envelope
- **THEN** production payload decoding yields `*agentic.TaskMessage`
- **AND** no graph projection or Graphable assertion is required

#### Scenario: unregistered or malformed task is refused

- **GIVEN** a task envelope that cannot be validated or decoded through the production payload registry
- **WHEN** publication is attempted
- **THEN** the action fails before JetStream publication
- **AND** no raw-envelope fallback is used
