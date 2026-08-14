# agentic-tools Specification

## Purpose

Defines the **tool catalog contract** — how a tool declares what it is, how the framework
serves that declaration to consumers, and what a consumer may conclude from it.

Today the capability covers exactly one declared property: **effect**, a worst-effect claim
(`unknown` / `read_only` / `mutating` / `external_effect`) answering "how bad can this get"
rather than enumerating what a tool does. Two rules carry the weight:

- **Absence is never evidence of safety.** An absent, empty, or unrecognized effect resolves
  to `unknown` at every point of use and never to `read_only`. `unknown` is *no claim at
  all*, not a middle rung — a consumer mapping effect onto policy treats it as at least as
  restrictive as `external_effect`.
- **Two seams, because neither alone is total.** Registration *refuses* an unrecognized
  non-empty value at boot; aggregation *normalizes* on the served copy. Registration alone
  is insufficient because the registry stores executors and re-invokes `ListTools()` live on
  every aggregation — so what boot validated is not necessarily what is served — and because
  a registration path taking an executor under a caller-supplied name never inspects a
  definition at all.

The enum is **open for extension**: a consumer must not switch exhaustively over its members
without a default arm resolving to `unknown`, so a member can be added without a coordinated
cross-repo release.

### What this capability does NOT cover

- **Effect does not control execution, and adopting it changes no enforcement.** The
  configured approval-required set, the configured allowed-tool set, and the per-loop
  advertised-tool admission check remain the sole authorities. A `read_only` tool named in
  the approval set is still gated; an `external_effect` tool absent from it still is not.
  Proving the field changes nothing is this capability's current contract.
- **Effect does not cross the provider wire.** The model is never shown it. Sending it would
  manufacture the appearance of a control the framework does not implement.
- **It does not define a default approval policy.** Deriving one from effect is deferred
  (gh#808) with a binding constraint already recorded: the authoritative value must be read
  from the registry at the dispatch seam, never from a copy carried in `TaskMessage.Tools`
  or `ToolCall`, or a crafted task could downgrade a declared effect. Task-carried and
  discovery-carried copies are display and discovery grade only.
- **It does not classify per-argument or dynamic severity.** Worst-effect semantics is the
  answer to argument-dependence; per-argument classification is permanently out of scope
  absent a real consumer.
- **It does not govern disclosure.** What a query *reveals* is a governance concern, not an
  effect classification — which is why a read against an external API is `read_only`.
- **It says nothing about tool definitions beyond effect.** `Parameters`, `Strict`, and
  `Paginated` are deliberately not projected into discovery; the projection is pinned by a
  structural check so a new canonical field cannot be silently dropped.
- **Application-registered tools are outside the classification guarantee.** The "every tool
  is classified" claim is enforced by an AST scrape over *framework-owned* packages only. An
  application registering its own executor is subject to the same resolution rules but
  nothing checks that it declared anything.
## Requirements
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

### Requirement: Framework-owned shared builtins exclude the unowned graph-query wrappers

The framework SHALL NOT supply shared builtin tools named `search_graph` or
`summarize_graph`. Their framework-owned shared registrations,
`BuiltinGroupKeys`, accepted `SkipBuiltins` keys, registration functions,
implementations, exported executor/option/constructor/querier symbols, tests,
schemas, documentation, discovery defaults, operation-consumer claims, and
alternate framework category entries `graph_search`/`graph_summary` SHALL be
absent.

This requirement does not reserve either former name or prohibit an application
from registering its own component-local executor under that name through the
existing general extension seam. An application-local executor SHALL remain
subject to the existing allowlist, per-loop advertised set, approval, retry,
local-over-shared discovery, and local-first dispatch behavior. SemStreams SHALL
add no shared alias, compatibility executor, reserved-name rule, dependency
inference, or special configuration behavior for such a local tool.

GraphQL `searchGraph` and `graphSummary`, their graph-query responders, research
consumers, exact reads, fusion, projection, classifier/search options, direct
`query_*` tools, and selected `research_graph` SHALL remain.

Open-vocabulary `allowed_tools`, `default_tools`, `approval_required`, and
`tool_retries` SHALL NOT become a closed framework-tool enum. Nil or empty
`AllowedTools` SHALL remain permissive for surviving or application-local
registered tools, but SHALL NOT create an absent executor. Stale deleted
`SkipBuiltins` values SHALL fail through existing closed-set validation.

#### Scenario: framework shared discovery excludes the deleted wrappers

- **WHEN** framework shared builtin registration and discovery run
- **THEN** neither former name has a framework-supplied definition or executor
- **AND** neither shared registration, skip key, exported implementation, or
  alternate category entry is present

#### Scenario: permissive allowlist does not create a deleted executor

- **GIVEN** nil or empty `AllowedTools`
- **AND** no application-local executor uses the former name
- **WHEN** shared discovery runs
- **THEN** the former name is absent
- **AND** an admitted direct call that is not intercepted for approval reaches
  the registries and returns the existing typed not-found outcome

#### Scenario: approval interception precedes registry miss

- **GIVEN** a former name remains in `approval_required`
- **AND** the wire call passes global and per-loop admission
- **AND** no executor is registered under that name
- **WHEN** the unapproved call is handled
- **THEN** ApprovalFilter produces the existing approval-required permission and
  pause behavior before registry dispatch
- **AND** a later approved or bypassed dispatch returns typed not-found if no
  local executor exists

#### Scenario: application-local reuse remains ordinary local extension

- **GIVEN** an application registers a local executor under a former name
- **WHEN** discovery and dispatch run
- **THEN** the local definition is discovered through existing local precedence
- **AND** existing admission, approval, retry, and dispatch rules apply
- **AND** no shared alias, reservation, or compatibility executor participates

#### Scenario: stale skip configuration fails visibly

- **GIVEN** `SkipBuiltins` contains either deleted key
- **WHEN** builtin configuration is validated
- **THEN** existing closed-set validation rejects it
- **AND** the framework does not silently accept a compatibility no-op

### Requirement: Tool discovery has one request/reply address

The agentic-tools component MUST retain the logical input port name `tool.list`. That port MUST have kind
`nats-request` and default subject `discovery.tool.list`.

At startup, the runtime MUST resolve the logical port's request/reply facts and subscribe only to the resulting
subject. It MUST NOT also subscribe to the former default `tool.list`, create an alias, or fall back to a hard-coded
address when port resolution fails.

#### Scenario: Default discovery uses the new address

- **GIVEN** agentic-tools uses its default port configuration
- **WHEN** the component starts
- **THEN** logical port `tool.list` resolves as kind `nats-request`
- **AND** the runtime subscribes to `discovery.tool.list`
- **AND** it does not subscribe to subject `tool.list`

#### Scenario: A same-kind custom subject is authoritative

- **GIVEN** logical port `tool.list` is configured as kind `nats-request` with a custom subject
- **WHEN** the component starts
- **THEN** the runtime subscribes only to the custom subject
- **AND** neither default nor former-default subscription is added

### Requirement: An incompatible discovery-port kind fails startup

An override for logical port `tool.list` MUST retain kind `nats-request`. Kind `nats`, `jetstream`, or any other
incompatible port facts MUST fail component startup with an actionable error. The framework MUST NOT repair,
reinterpret, or silently accept the incompatible declaration.

#### Scenario: A legacy nats override is rejected

- **GIVEN** logical port `tool.list` is explicitly configured with kind `nats`
- **WHEN** agentic-tools starts
- **THEN** startup fails
- **AND** the error names port `tool.list`, expected kind `nats-request`, and the observed incompatible kind
- **AND** no discovery subscription is installed

### Requirement: Discovery subscription is startup-atomic and fail-closed

Before allocating any runtime subscription, agentic-tools MUST resolve and validate the discovery request port and all
JetStream input facts required by that startup attempt. The component MUST set `running=true` only after discovery and
every required local input consumer have started successfully.

A discovery-subscription failure MUST return a transient observable startup error with component, start, and
discovery-subscribe context. The returned error MUST preserve the underlying cause for `errors.Is`. The failed attempt
MUST leave no discovery subscription, active local consumer, or tracked startup resource and MUST leave
`running=false`.

If a later input-consumer setup fails, startup MUST roll back the discovery subscription and every local consumer
started by that attempt, clear the tracked subscription and consumer state, leave `running=false`, and return the
setup error. Rollback MUST NOT delete a durable consumer or its delivery position. After either failure, a subsequent
`Start` MUST begin from clean local state and be able to succeed when its dependencies are healthy.

#### Scenario: Discovery subscription failure leaves no false running state

- **GIVEN** valid discovery and JetStream input facts
- **AND** the discovery request subscription fails with `natsclient.ErrNotConnected`
- **WHEN** agentic-tools starts
- **THEN** startup returns a transient error with `Component`, `Start`, and discovery-subscribe context
- **AND** `errors.Is(err, natsclient.ErrNotConnected)` is true
- **AND** no discovery subscription, local consumer, or tracked startup resource remains
- **AND** `running` remains false
- **AND** a later `Start` can succeed cleanly after the transport recovers

#### Scenario: A later consumer failure rolls back the startup attempt

- **GIVEN** discovery subscription succeeds
- **AND** one or more local input consumers start during the same attempt
- **WHEN** a later required consumer setup fails
- **THEN** startup returns the consumer setup error
- **AND** discovery and every local consumer started by that attempt are stopped
- **AND** no discovery subscription or tracked local consumer remains
- **AND** `running` remains false
- **AND** no durable consumer or durable delivery position is deleted
- **AND** a later `Start` can succeed cleanly

### Requirement: The breaking discovery cutover has two live gates

The breaking cutover MUST NOT integrate until both the crud-tools and agentic E2E paths pass on the current corrected
tree. Crud-tools MUST prove a nonempty effect-bearing catalog at `discovery.tool.list`. Agentic E2E MUST prove live
tool execution and result return with stream coverage limited to `tool.execute.>` and `tool.result.>` for tool traffic.
After a startup-ordering or rollback correction, both E2Es MUST be rerun; earlier logs MUST NOT satisfy the gate.

#### Scenario: Crud-tools proves the discovery address

- **GIVEN** the shipped agentic-tools configuration
- **WHEN** the crud-tools E2E requests `discovery.tool.list`
- **THEN** it receives a nonempty tool catalog
- **AND** the existing effect metadata assertions pass

#### Scenario: Agentic execution survives narrowed streams

- **GIVEN** the AGENT stream covers `tool.execute.>` and `tool.result.>` rather than `tool.>`
- **WHEN** the agentic E2E executes a tool call
- **THEN** the tool request is executed
- **AND** its result returns to the loop

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

