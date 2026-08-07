<!-- markdownlint-disable MD041 -->

## ADDED Requirements

### Requirement: Trajectory audit records are immutable per-attempt KV facts

Agentic-loop SHALL record trajectory observations in KV bucket `AGENT_TRAJECTORIES`, configured with history `1` and
no TTL. Every fact key SHALL have exactly this bounded NATS-safe form:

```text
v1.<base32-sha256(loop_id)>.<attempt_id>
```

At the beginning of each fact-recording invocation, agentic-loop SHALL allocate one framework-owned bounded NATS-safe
`attempt_id` and one monotonically increasing `attempt_ordinal` under the active per-loop manager's synchronization.
Raw source identifiers SHALL NOT enter the key. On restart, the next ordinal SHALL begin after the maximum visible
ordinal under that loop's prefix.

Fact writes SHALL use KV `Create`, never `Put` or `Update`. Store/KV retries and lost-reply verification inside one
invocation SHALL reuse that invocation's exact attempt ID, key, and canonical bytes. A key-exists or ambiguous reply
SHALL be verified by Get: byte-identical content is success, while different content under the same attempt ID is an
integrity audit failure. A same-process or cross-process redelivery SHALL allocate a new attempt identity and append a
new fact, even when it carries the same optional source correlation.

Source correlation SHALL be optional advisory linkage derived only from request, call, signal, or message identity
already present in the decoded payload. Fact creation SHALL NOT require JetStream delivery metadata or introduce
metadata acquisition merely for audit.

#### Scenario: fact keys are deterministic and NATS-safe within an invocation

- **GIVEN** one loop and one allocated attempt identity
- **WHEN** its trajectory key is constructed repeatedly
- **THEN** every construction produces the same bounded NATS-safe key
- **AND** the key contains the loop digest and framework attempt ID but no raw external identity

#### Scenario: repeated source delivery creates another observed fact

- **GIVEN** two processing invocations carrying the same request or call correlation
- **WHEN** each invocation records a trajectory observation
- **THEN** two immutable facts exist with distinct attempt IDs and ordinals
- **AND** repeated delivery remains visible rather than being deduplicated or reported as an integrity conflict

#### Scenario: a lost Create reply is idempotent only within one invocation

- **GIVEN** KV committed an invocation's canonical fact but its Create reply was lost
- **WHEN** that invocation verifies the same key by Get
- **THEN** byte-identical content is accepted as committed success
- **AND** different bytes under that same attempt ID produce integrity degradation without replacing the original

#### Scenario: missing source correlation does not reject a fact

- **GIVEN** a decoded domain payload with no request, call, signal, message, or delivery identity
- **WHEN** agentic-loop records its observation
- **THEN** the fact is created with no source correlation
- **AND** no JetStream delivery metadata is fetched to fill the optional field

### Requirement: Trajectory facts are finite and causally ordered

`TrajectoryFactV1` SHALL carry only schema version, loop digest, attempt identity/ordinal, closed fact kind, optional
bounded source linkage, causal iteration/phase/source ordinal, observation timing, fixed status/error enums, bounded
counters and previews, evidence digest/size/capture/failure fields, and an optional `message.StorageReference`.

The v1 fact kinds SHALL be exactly `loop.started`, `model.requested`, `model.completed`, `tool.requested`,
`tool.completed`, `context.compacted`, and `loop.terminal`. Request and completion SHALL remain separate facts.

The envelope SHALL NOT embed prompts, responses, messages, tool-call arrays, arguments, results, URLs, arbitrary
metadata maps, raw error strings, or any unbounded collection. Its canonical marshaled size SHALL remain below an
internal 8 KiB limit. The encoder SHALL hash or truncate bounded previews before marshal; the limit SHALL NOT become
an adopter-configured prediction knob.

Readers SHALL order facts by
`(iteration, phase_rank, source_ordinal, attempt_ordinal, attempt_id)`. Tool source ordinals SHALL come from the model
response's tool-call order, so concurrent results retain logical order. `observed_at` and `elapsed_ms` describe the
particular attempt and MAY differ across redeliveries.

#### Scenario: adversarial metadata cannot exceed the fact bound

- **GIVEN** maximum-length previews, counters, enums, and evidence-reference metadata
- **WHEN** `TrajectoryFactV1` is canonically encoded
- **THEN** the marshaled fact remains below 8 KiB
- **AND** no body or unbounded collection can enter the fact envelope

#### Scenario: parallel tool completion follows source order

- **GIVEN** tool results complete concurrently in a different order from the model's tool-call list
- **WHEN** their facts are read
- **THEN** they sort by source ordinal before attempt ordinal
- **AND** arrival timestamp does not become the ordering authority

### Requirement: Full trajectory evidence uses the registered Store

For every trajectory body, agentic-loop SHALL canonically encode full `TrajectoryEvidenceV1` before operational
truncation or compaction, digest the exact bytes with SHA-256, and use this logical key:

```text
trajectory-evidence/v1/sha256/<hex-digest>
```

Full evidence SHALL include all model request messages, tools, arguments, reasoning carriers, and request parameters;
the full decoded model completion and usage; full tool dispatch metadata and arguments; the original tool result before
`tool_result_max_bytes` truncation; full before/after compaction evidence; and terminal result/error evidence when one
exists. `trajectory_detail` SHALL NOT gate fidelity.

Each operation SHALL lazily resolve the configured `trajectory_evidence_storage_instance` through `StoreRegistry`,
defaulting to `objectstore`. Agentic-loop SHALL NOT construct, cache, close, or claim ownership of the borrowed store.
The writer SHALL Get first, accept identical bytes, reject mismatching bytes as integrity failure, Put on not-found,
and Get-verify an ambiguous Put reply. A stored fact SHALL carry the configured logical instance, digest, exact size,
logical key, and content type `application/vnd.semstreams.agentic-trajectory-evidence.v1+json` in its
`StorageReference`.

Multiple attempt facts MAY reference one digest-addressed body. A body stored before fact-write failure MAY remain an
unreferenced content-addressed object for a future reference-aware retention policy; Foundation B SHALL NOT introduce
a transaction, general CAS service, repair worker, or automatic evidence expiry.

#### Scenario: a full tool result is captured before execution truncation

- **GIVEN** a tool result larger than `tool_result_max_bytes`
- **WHEN** the result enters the agentic loop
- **THEN** canonical evidence contains the original full result before execution/context truncation
- **AND** a query that hydrates its reference retrieves the full result

#### Scenario: full model and tool inputs remain retrievable

- **GIVEN** a model request carrying messages, tool calls, arguments, reasoning carriers, and parameters
- **WHEN** its fact and evidence are recorded
- **THEN** the fact remains bounded while the complete canonical body is retrievable through its reference

#### Scenario: digest-addressed evidence is verified on retry

- **GIVEN** one canonical body and an ambiguous Store Put reply
- **WHEN** the writer retries within the invocation
- **THEN** it reuses the same digest/key and verifies the stored bytes by Get
- **AND** it never creates a timestamp-derived logical key

#### Scenario: redeliveries may share one evidence body

- **GIVEN** two attempt facts derived from the same canonical body
- **WHEN** evidence storage completes for both
- **THEN** both references MAY carry the same digest and logical key
- **AND** the attempt facts remain distinct observations

#### Scenario: provider reconfiguration is observed lazily

- **GIVEN** the configured Store provider is stopped and later replaced under the same logical instance
- **WHEN** a later evidence operation runs
- **THEN** agentic-loop resolves the current handle through `StoreRegistry`
- **AND** no cached or closed borrowed handle is used

### Requirement: Audit loss degrades loudly and never fails agent work

Every trajectory audit failure SHALL emit `ERROR` with loop ID, attempt ID, bounded kind/stage/reason, and no evidence
body; increment `semstreams_agentic_loop_trajectory_audit_failures_total{stage,kind,reason}` using closed label sets;
and latch the existing component Health degraded with `ErrorCount` and bounded `LastError`. Stages SHALL be exactly
`provider_resolve`, `evidence_get`, `evidence_put`, `evidence_verify`, `fact_encode`, `fact_create`, and `fact_verify`.
Raw backend errors SHALL NOT become metric labels.

Missing configured provider SHALL NOT fail agentic-loop Start. Start SHALL record provider-resolve degradation,
install subscriptions, and continue work. Health SHALL check current provider presence on each call; restoration MAY
clear the live dependency condition, but any prior audit-loss latch SHALL remain degraded for the process lifetime.

If required evidence cannot be resolved, stored, or verified while KV remains usable, agentic-loop SHALL attempt an
ordinary fact with `evidence_capture="missing"`, the computed digest/size when available, a bounded failure reason, and
no fabricated reference. If encoding or immutable fact Create/verification fails, no durable fact or reconstructed gap
claim is required. Logs, metrics, and Health remain the operational evidence.

No audit failure SHALL reject, NAK, cancel, or fail the agent work. The existing state transition, downstream publish,
and source ACK SHALL proceed with their original work result.

#### Scenario: evidence failure records an honest observation when KV is usable

- **GIVEN** Store resolution, Get, Put, or verification fails for required evidence
- **WHEN** the fact bucket remains usable
- **THEN** agentic-loop attempts a fact with `evidence_capture="missing"` and no fabricated reference
- **AND** the failure logs, increments bounded metrics, degrades Health, and does not block work publication or ACK

#### Scenario: fact failure leaves no invented durable gap

- **GIVEN** fact encoding, size validation, Create, or verification ultimately fails
- **WHEN** the work handler continues
- **THEN** ERROR, bounded metric, and degraded Health report the audit loss
- **AND** no counter, seal, gap fact, repair record, or completeness claim is manufactured
- **AND** the existing work transition, publication, and ACK still occur

#### Scenario: missing provider starts degraded and continues work

- **GIVEN** agentic-loop's configured evidence provider is absent after provider startup
- **WHEN** agentic-loop starts
- **THEN** it installs subscriptions with Health degraded and provider-resolve telemetry emitted
- **AND** later work still publishes and ACKs despite failed evidence capture

### Requirement: Terminal trajectory facts are ordinary observations

An ordinary processing attempt SHALL attempt evidence storage, then immutable fact Create, then continue its existing
state transition, downstream publication, and ACK regardless of audit outcome.

A terminal-processing invocation SHALL finish all other audit attempts it knows, attempt terminal evidence, and
attempt its ordinary `loop.terminal` fact last before the existing `COMPLETE_<loopID>` write, terminal event publish,
and source ACK. Failure to record the terminal observation SHALL NOT block those adjacent completion surfaces.

A `loop.terminal` fact SHALL mean only that one terminal outcome was observed and recorded. Redelivery SHALL allocate
a new attempt identity and MAY append another terminal fact. No terminal fact SHALL be a seal, summary, manifest,
membership proof, watermark, checkpoint, or completeness claim. `COMPLETE_` polymorphism/collisions and terminal-event
correctness SHALL remain separate and out of scope.

#### Scenario: terminal audit occurs before adjacent completion surfaces

- **GIVEN** a terminal-processing invocation
- **WHEN** it reaches its terminal work
- **THEN** the terminal evidence/fact attempt is its last trajectory write
- **AND** that attempt precedes `COMPLETE_`, terminal event publication, and source ACK
- **AND** audit failure does not block any adjacent completion surface

#### Scenario: terminal redelivery creates another terminal observation

- **GIVEN** a terminal fact committed before the work ACK was lost
- **WHEN** the work is redelivered
- **THEN** the new invocation MAY append a second ordered terminal fact with a new attempt identity
- **AND** neither fact is treated as a seal or conflict

#### Scenario: crash before terminal recording proves no terminal state

- **GIVEN** a process crashes before its terminal fact is created
- **WHEN** trajectory facts are read after restart
- **THEN** no terminal observation is inferred from `COMPLETE_`, terminal events, cache, process memory, or graph state

### Requirement: Trajectory reads expose observed facts without completeness claims

A trajectory reader SHALL hash the requested loop ID, prefix-list only `v1.<loop_hash>.>`, Get and validate each
returned fact, sort by causal/attempt order, derive totals only from returned visible facts, and hydrate evidence only
when requested. It SHALL honestly report missing/unverifiable bodies.

Every internal and public response SHALL contain:

```text
coverage: observed
observed_totals: <totals derived only from returned visible facts>
```

No response SHALL return or imply complete, partial, unknown-completeness, fully captured, gap free, or an equivalent
coverage guarantee. A prefix with no visible facts SHALL return not-found. Facts with no terminal observation SHALL
report `terminal_observed: false`; one or more visible terminal facts SHALL report `terminal_observed: true` and expose
every terminal observation in causal/attempt order.

Reads SHALL use the KV fact log as authority after restart, with no cache hydration and no fallback to
`TrajectoryManager`, `COMPLETE_`, terminal events, process memory, or graph state. `TrajectoryManager` MAY remain only
for active execution mechanics.

#### Scenario: restart reads use only visible immutable facts

- **GIVEN** a restarted process with empty trajectory memory and visible loop facts in KV
- **WHEN** the loop is queried or its prefix watch performs initial replay
- **THEN** the response is reconstructed from current immutable facts
- **AND** no cache or graph reconstruction is required

#### Scenario: every response is explicitly observed-only

- **GIVEN** any successful trajectory query, including one with visible missing-evidence observations
- **WHEN** the response is returned
- **THEN** `coverage` equals `observed`
- **AND** totals appear only as `observed_totals` derived from returned facts
- **AND** the response makes no completeness guarantee

#### Scenario: terminal visibility is not completion proof

- **GIVEN** zero, one, or multiple visible terminal facts
- **WHEN** the loop is queried
- **THEN** `terminal_observed` reflects only whether any terminal fact is visible
- **AND** every visible terminal fact remains ordered and exposed
- **AND** coverage remains `observed`

### Requirement: Retired trajectory authority is removed

Foundation B SHALL delete aggregate `Trajectory` as the durable/public representation, terminal trajectory cache and
`trajectory_cache_ttl`, cache/manager query fallback, no-op `SaveTrajectory`, `trajectory_detail`, private
`content_bucket` ObjectStore construction/lifecycle, timestamp-derived evidence keys, terminal batch trajectory graph
emission, and direct trajectory HTTP/OpenAPI handlers and paths.

Graph trajectory entities and projection SHALL remain outside Foundation B correctness. Any later graph trace SHALL
be a separately approved derived index consuming the durable fact log. No projector state, graph-pending flag, repair
worker, terminal seal, audit counter set, manifest, membership proof, or completeness state machine SHALL be added.

#### Scenario: static surfaces contain no retired trajectory authority

- **WHEN** agentic-loop schemas, types, configs, handlers, stores, and tests are inspected
- **THEN** no aggregate/cache/private ObjectStore/direct HTTP/trajectory graph-write authority remains
- **AND** no terminal seal, attempted/recorded/gap counts, `counts_known`, manifest, projector, or completeness proof
  exists
