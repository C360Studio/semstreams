## ADDED Requirements

### Requirement: A tool definition declares the worst effect it can have

A tool definition MUST be able to declare its effect classification as exactly one
member of a closed enum, and that declaration MUST name the **most severe** effect
the tool can have rather than enumerate everything it does. The members are
`unknown`, `read_only`, `mutating`, and `external_effect`.

`read_only` means the tool observes and changes no state anywhere, inside or
outside the deployment — a read against an external API is `read_only`, because
what a query discloses is a governance concern and not an effect classification.
`mutating` means the tool can change state within the deployment's own boundary.
`external_effect` means the tool can change state or take irrevocable action
outside that boundary, and it DOMINATES `mutating`: a tool that writes to a third
party is `external_effect`, not both. `unknown` is **no claim at all** — it is not
a rung between `read_only` and `mutating`, and a consumer that maps effect onto
policy MUST treat `unknown` as at least as restrictive as `external_effect`.

Worst-effect semantics is what makes one enum sufficient, and it is also the
answer to argument-dependence: a tool whose severity varies with its arguments
declares the worst case it admits.

The enum is **open for extension**. A consumer MUST NOT switch exhaustively over
the members without a default arm resolving to `unknown`, so that a later member
can be added additively without a coordinated release.

#### Scenario: a tool that only observes

- **GIVEN** a tool that queries the graph and writes nothing
- **WHEN** it declares its effect
- **THEN** the declared value is `read_only`

#### Scenario: a tool that writes outside the deployment boundary

- **GIVEN** a tool that issues an arbitrary outbound HTTP request
- **WHEN** it declares its effect
- **THEN** the declared value is `external_effect`
- **AND** it is not additionally required to declare `mutating`

#### Scenario: severity varies with arguments

- **GIVEN** a tool whose effect depends on the arguments it is called with
- **WHEN** it declares its effect
- **THEN** the declared value is the most severe effect any admissible argument set can produce

### Requirement: An absent or unrecognized effect resolves to unknown, never to read_only

An absent, empty, or unrecognized effect value MUST resolve to `unknown` at every
point of use, and MUST NEVER resolve to `read_only` or to any other declared
member. Absence of a classification is not evidence of safety; this is the tool
counterpart of the framework's standing rule that an absent measurement must never
render as a measurement of absence.

Resolution MUST be available to every consumer as a total function over the value
— empty and unrecognized inputs both yield `unknown` — and MUST be applied at the
points where the framework serves a definition, so that a downstream consumer
reading a framework-served definition never has to re-implement the rule.

An unrecognized value arriving on a *task* — a tool definition carried in task
data rather than obtained from the registry — MUST NOT fail the task. The field is
descriptive and has no enforcement consumer, so refusing a task over a typo would
convert description into an availability hazard; the value resolves to `unknown`
at use instead.

#### Scenario: a producer declares nothing

- **GIVEN** a tool definition whose effect is unset
- **WHEN** a consumer resolves its effect
- **THEN** the resolved value is `unknown`
- **AND** it is not `read_only`

#### Scenario: an unrecognized spelling

- **GIVEN** a tool definition whose effect is a value outside the enum, such as `destructive`
- **WHEN** a consumer resolves its effect
- **THEN** the resolved value is `unknown`

#### Scenario: an unrecognized spelling arrives on a task

- **GIVEN** a task carrying a tool definition whose effect is outside the enum
- **WHEN** the task is processed
- **THEN** the task is not rejected on account of the effect value
- **AND** the effect resolves to `unknown` wherever it is used

### Requirement: Executor registration refuses an unrecognized effect declaration

Registering an executor by its tool definitions MUST fail when any definition
declares a non-empty effect outside the enum, and the failure MUST occur before
any of that executor's entries are committed to the registry. An empty effect
remains legal and means undeclared.

Refusal at registration is what makes a typo in a framework-owned or
application-owned executor a boot-time failure rather than a silent demotion to
`unknown`, per the framework rule that enum validation rejects unknown values
explicitly rather than dropping them silently.

Registration-time refusal alone is NOT sufficient to guarantee that every served
definition carries a recognized value, for two reasons that the aggregation
requirement below exists to close: a registry that stores executors and re-invokes
them at read time can serve a definition that differs from the one validated at
registration, and a registration path that takes an executor under a caller-supplied
name never inspects a tool definition at all.

#### Scenario: an executor declaring an invalid effect

- **GIVEN** an executor whose tool definitions include a non-empty effect outside the enum
- **WHEN** it is registered by its definitions
- **THEN** registration fails
- **AND** none of that executor's tool names are committed to the registry

#### Scenario: an executor declaring no effect

- **GIVEN** an executor whose tool definitions leave effect unset
- **WHEN** it is registered by its definitions
- **THEN** registration succeeds

### Requirement: Registry aggregation serves a recognized effect on every definition

Tool definitions served by registry aggregation MUST carry a recognized effect
value, with empty and unrecognized values normalized to `unknown` in the served
copy. Normalization at aggregation — not only refusal at registration — is what
makes the guarantee total, because the aggregation seam is the single choke point
every framework consumer draws from: default-tool resolution, per-loop tool
discovery, and the discovery catalog all read through it.

Normalization MUST NOT mutate the producing executor's own definition; it applies
to the copy the registry returns.

#### Scenario: a definition that never passed registration validation

- **GIVEN** a tool whose executor was registered under a caller-supplied name without its definitions being inspected
- **AND** that executor's definition declares no effect
- **WHEN** the registry aggregates tool definitions
- **THEN** the served definition's effect is `unknown`

#### Scenario: a producer changes its declaration after registration

- **GIVEN** an executor whose returned definition carries an unrecognized effect at read time
- **WHEN** the registry aggregates tool definitions
- **THEN** the served definition's effect is `unknown`

### Requirement: Tool discovery output carries the resolved effect explicitly

The tool discovery response MUST carry each tool's resolved effect as an explicit,
always-present field, including the literal value `unknown` for a tool that
declares nothing. The field MUST NOT be elided when it holds the fail-safe value.

Serving the *resolved* value rather than the raw declaration is deliberate: it
places the absent-means-unknown rule at the framework boundary once, so that every
downstream consumer of discovery reads a plain string and no consumer re-implements
the fail-safe.

The discovery response is a projection of the canonical tool definition and does
not carry every canonical field. Which canonical fields are projected and which are
deliberately dropped MUST be pinned by a structural check that fails when a new
canonical field is neither projected nor explicitly recorded as dropped, so that a
field cannot be silently lost in the projection.

#### Scenario: discovery of an unclassified tool

- **GIVEN** a registered tool that declares no effect
- **WHEN** a discovery request is served
- **THEN** the response entry for that tool carries effect `unknown`
- **AND** the field is present rather than omitted

#### Scenario: discovery of a classified tool

- **GIVEN** a registered tool declaring `external_effect`
- **WHEN** a discovery request is served
- **THEN** the response entry for that tool carries effect `external_effect`

#### Scenario: a new canonical field is added

- **GIVEN** a new field added to the canonical tool definition
- **WHEN** the projection is not updated to either carry or explicitly drop it
- **THEN** the structural check fails

### Requirement: Effect metadata is descriptive and does not alter execution control

Effect metadata MUST NOT change which tool calls are admitted, gated for approval,
or refused. The authoritative controls remain the configured approval-required name
set, the configured allowed-tool name set, and the per-loop advertised-tool
admission check; a tool's declared effect MUST NOT add to, subtract from, or
override any of them.

A tool declaring `read_only` that is named in the approval-required set MUST still
be gated for approval, and a tool declaring `external_effect` that is not named
MUST still execute without an approval gate. This is the increment's contract: the
field changes nothing about execution today.

When a future capability does derive policy from effect, the authoritative value
MUST be the one the registry serves at the dispatch seam, never a copy carried in
task or tool-call data — otherwise a crafted task could downgrade a tool's declared
effect and weaken the control it feeds. Task-carried and discovery-carried copies
are display and discovery grade only.

#### Scenario: a read-only tool named in the approval set

- **GIVEN** a tool declaring `read_only` whose name appears in the approval-required set
- **WHEN** a call to it is filtered
- **THEN** the call is gated for approval

#### Scenario: an external-effect tool absent from the approval set

- **GIVEN** a tool declaring `external_effect` whose name does not appear in the approval-required set
- **WHEN** a call to it is filtered
- **THEN** the call is not gated for approval

#### Scenario: admission is unchanged across every effect value

- **GIVEN** the same tool call evaluated once for each member of the enum and once with no declaration
- **WHEN** the approval filter and the per-loop advertised-tool admission check run
- **THEN** every outcome is identical across all of them
