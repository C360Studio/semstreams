# agentic-loop Specification

## Purpose

`agentic-loop` governs what an agentic loop **owes an outside observer** about its own work: how much
of it a spawn is permitted to do, and whether any of it is currently in flight.

Two question classes live here. **Budget** — a spawn may narrow its iteration allowance within the
operator ceiling, and exhaustion is reported under one uniform reason rather than a per-call-site
spelling. **In-flight visibility** — whether this deployment currently has outstanding loop work for
a task subject is answerable over the component's NATS request/reply surface, without the caller
knowing, deriving, or supplying the loop's JetStream consumer or stream name.

One invariant binds the second class and is the reason it is specified at all: **an absent
measurement must never render as a measurement of absence.** A missing consumer, a component that is
not answering, and a consumer state that could not be read this attempt are three instances of one
rule — each is *unknown*, and none of them is zero. Mapping unknown onto policy belongs to the
caller, which is the only party that knows the cost of guessing in each direction.

**What it does NOT cover.** Whether the loop's answer is trustworthy *yet* is readiness, and belongs
to the ADR-066 envelope — this capability answers *what* the state is, readiness answers *whether the
answer can be believed*, and a consumer that asks the second without the first has made a cold-start
read. Trajectory content, tool dispatch, and model invocation belong to their own components. The
consumer-name derivation is deliberately private and is specified here only as something callers MUST
NOT need.
## Requirements
### Requirement: A spawn may narrow its loop iteration budget

`agentic.TaskMessage` MUST accept an optional per-spawn `max_iterations`; a nil value uses the component
default, a value below 1 fails task validation, and the effective budget MUST be the minimum of the spawn
value and the component `MaxIterations` ceiling. The `publish_agent` rule action MUST expose this as
`loop_max_iterations` with variable substitution, and a substituted value that is not a positive integer MUST
fail the action with a classified, observable error.

#### Scenario: spawn narrows the budget

- **GIVEN** a component configured with MaxIterations 20
- **WHEN** a task is spawned with max_iterations 2
- **THEN** the loop fails with reason "max_iterations" after 2 iterations

#### Scenario: spawn cannot widen past the operator ceiling

- **GIVEN** a component configured with MaxIterations 5
- **WHEN** a task is spawned with max_iterations 50
- **THEN** the effective budget is 5

#### Scenario: substituted budget from an entity triple

- **GIVEN** a publish_agent action with loop_max_iterations "$entity.triple.task.spec.budget"
- **WHEN** the rule fires on an entity carrying that predicate with value "3"
- **THEN** the spawned task carries max_iterations 3

#### Scenario: non-integer substitution fails loudly

- **GIVEN** a publish_agent action whose loop_max_iterations substitutes to "unbounded"
- **WHEN** the rule fires
- **THEN** the action fails with a classified error and a bounded rejection metric, and no task is published

### Requirement: Iteration exhaustion publishes one uniform reason

Every path that detects iteration-budget exhaustion MUST publish the loop-terminal failure reason
`"max_iterations"`. Internal detection MUST use a typed sentinel error mapped via errors.Is; consumers MUST
NOT need to match error text to distinguish budget exhaustion from other handler failures.

#### Scenario: model-response guard at the cap

- **GIVEN** a loop whose iteration count has reached its budget
- **WHEN** the next model response arrives
- **THEN** the published failure reason is "max_iterations"

#### Scenario: tool drain at the cap

- **GIVEN** a loop at its budget with tool calls still in flight
- **WHEN** the pending tools are drained with synthetic failures
- **THEN** the published failure reason is "max_iterations"

### Requirement: Whether a loop task is in flight MUST be readable without reconstructing the consumer name
The agentic-loop component SHALL answer the in-flight question — "does this deployment currently have
outstanding agentic-loop work for this task subject" — over its NATS request/reply surface, and the
caller SHALL NOT need to know, derive, or supply the loop's JetStream consumer name or its stream
name.

The request subject SHALL carry **deployment identity**. Request/reply subscription is plain
subject subscription, so a single shared subject means every agentic-loop in the NATS account
receives the request and replies, and the requester keeps whichever reply arrives first — an
arbitrary deployment's answer delivered with full confidence, which is the precise permissive
failure this capability exists to remove. The deployment token SHALL be the loop's consumer-name
suffix rather than a separately invented identifier, because that suffix already determines which
durable consumer exists: two loops sharing it bind the SAME consumer and therefore necessarily
report the same count, while two loops with different suffixes are different deployments. The
addressing thereby matches the thing being measured. Supplying that token is a SELECTOR — the
caller states which deployment it is asking about, which is inherent to the question — and is not
the consumer-name reconstruction this capability forbids.

The consumer name and its subject-sanitizing derivation remain **private to the component**. A caller
that must reconstruct a name has taken on a contract the framework never promised: when the derivation
changes, the copy does not fail to compile, it fails to find a consumer, and a not-found consumer is
indistinguishable from an idle one.

The component SHALL answer from the binding it actually created: it records the subject→consumer
association when its consumer setup runs and resolves the query against that record, so the query
cannot address a different consumer than the component bound and no second derivation of the name
exists anywhere. (Corrected 2026-08-02: an earlier text required deriving the name "from the same
helper"; the implementation deliberately removed the derivation instead, which is stronger — a
recorded binding cannot drift from the derivation because there is no derivation to drift from.)
Serving the answer on the wire rather than through an in-process call is what makes the name
*deleted* from callers rather than relocated into their parameter lists: no name, no configuration,
and no component handle crosses the boundary, and a caller in another process is served identically.

#### Scenario: A caller asks about in-flight work by subject

- **GIVEN** a deployment running an agentic-loop bound to a task subject
- **WHEN** a caller issues the in-flight request for that subject
- **THEN** it receives the answer without supplying a consumer name, stream name, or suffix
- **AND** no exported symbol reveals the consumer-name derivation

#### Scenario: An out-of-process caller is served identically

- **GIVEN** a caller in a different process from the agentic-loop component
- **WHEN** it issues the in-flight request over NATS
- **THEN** it receives the same answer an in-process caller would
- **AND** it requires no component handle to do so

#### Scenario: Two deployments in one account are addressed separately

- **GIVEN** two agentic-loop deployments on one NATS account with distinct consumer-name suffixes
- **AND** one holding outstanding work while the other is idle
- **WHEN** a caller addresses each deployment's subject in turn
- **THEN** each answer reflects that deployment's own consumer, deterministically and repeatably
- **AND** asking one deployment about a task subject it does not bind is unknown, never the other
  deployment's count

#### Scenario: A request subscription installed before a failing one is not leaked

- **GIVEN** component start installs more than one request subscription in sequence
- **WHEN** a later one fails and start is abandoned
- **THEN** every already-installed request subscription is unsubscribed during start-failure cleanup
- **AND** a subsequent start attempt leaves exactly one responder per subject

#### Scenario: Outstanding work is visible while tasks are pending or unacknowledged

- **GIVEN** tasks queued for the loop's consumer or delivered and not yet acknowledged
- **WHEN** a caller asks whether work is outstanding for that subject
- **THEN** the answer reports work in flight, sourced from the consumer's pending and
  unacknowledged counts
- **AND** a subject whose consumer has nothing pending and nothing unacknowledged reports zero

(Corrected 2026-08-02: an earlier scenario asserted visibility "across the task's heartbeat
renewals until the task is acked" — the change that shipped this surface measured that premise as
inapplicable to the loop's prompt-ack task handling and declined to assert it; the scenario text
nonetheless survived to publication. This scenario states what the tests actually pin.)

### Requirement: An unknown in-flight state MUST be an error, never a report of no work
An unobserved in-flight state MUST be reported as **unknown** and MUST NOT be reported as zero
outstanding work, on every path that can fail to observe it.

**An absent measurement must never render as a measurement of absence.** This capability has three
instances of that one invariant, and they SHALL be implemented as one rule rather than three
coincidences:

| Condition | Means | Must NOT mean |
|---|---|---|
| `jetstream.ErrConsumerNotFound` | this deployment has no agentic-loop | nothing in flight |
| No responders on the request subject | the loop component is not answering | nothing in flight |
| Consumer state unreadable this attempt | not observed | nothing in flight |

The no-responders case is the most dangerous of the three and is the one introduced by serving the
answer on the wire: a down loop component does not mean the work is gone. Messages may be sitting in
the stream with nobody to answer for them — which is exactly the situation in which a recovery pass is
most likely to be running, and most likely to do harm by concluding a turn is stranded.

Mapping unknown onto policy — defer, retry, treat as busy — belongs to the caller, the only party that
knows the cost of each direction. The requirement is that the caller can tell the cases apart without
string-matching an error message.

**Composition note (normative for consumers, not for this component):** a consumer SHALL gate on the
loop's ADR-066 readiness envelope before treating an in-flight answer as authoritative. Readiness
answers "is this component's answer trustworthy yet"; the in-flight query answers "what is it". Asking
the second without the first is a cold-start read, and it fails closed.

#### Scenario: A deployment with no agentic-loop reports unknown rather than idle

- **GIVEN** a deployment that runs no agentic-loop component
- **WHEN** a caller issues the in-flight request for a task subject
- **THEN** the result is unknown, distinguishable from "consumer exists, nothing outstanding"
- **AND** no zero-valued count is returned alongside it

#### Scenario: A down loop component reports unknown, not idle

- **GIVEN** the agentic-loop component is not running, while task messages remain on the stream
- **WHEN** a caller issues the in-flight request
- **THEN** the no-responders condition surfaces as unknown
- **AND** the caller can distinguish it from an answered "nothing in flight"

#### Scenario: A transient lookup failure does not read as idle

- **GIVEN** the consumer exists but its state cannot be read on this attempt
- **WHEN** a caller issues the in-flight request
- **THEN** the result is unknown rather than a zero count

### Requirement: In-flight state MUST NOT be derived from the acknowledgement floor
The in-flight answer SHALL be sourced from the consumer's outstanding-work bookkeeping
(`NumPending + NumAckPending`) and SHALL NOT be computed from `AckFloor`.

`AckFloor` was measured against both deployed NATS versions and found to misreport in **both**
directions: it does not advance past a `MaxDeliver`-exhausted message, so it sits behind that message
while the consumer is idle; and on the next unrelated ack it leaps *past* the never-applied message.
It therefore never means "everything at or below this is durably handled". The rejection and its
measurement are recorded in ADR-088. This requirement exists so the disproven approach cannot be
reintroduced as an optimization.

A restart-surviving answer SHALL NOT be sourced from loop state records either: only a handler
transitions a loop out of `state=running`, so a crashed process leaves a stale `running` entry
indistinguishable from live work. `published_request_id` and `pending_tool_results` are settlement facts
for classifying a redelivered input; they SHALL NOT be read as an in-flight answer.

#### Scenario: A poison-exhausted message does not freeze the in-flight answer

- **GIVEN** a task message that has exhausted `MaxDeliver` and was never applied
- **WHEN** a caller asks whether work is outstanding
- **THEN** the answer reflects genuine outstanding work, not a floor stalled behind that message

#### Scenario: A crashed process does not read as work in flight

- **GIVEN** a loop record left at `state=running` by a process that crashed mid-task
- **WHEN** a caller asks whether work is outstanding for that subject
- **THEN** the answer is derived from consumer bookkeeping, not from the stale record

#### Scenario: A record naming a published request is not an in-flight answer

- **GIVEN** a loop record with `published_request_id = R` and a non-empty `pending_tool_results`, left by a process
  that crashed after publishing `R`
- **WHEN** a caller asks whether work is outstanding for that subject
- **THEN** the answer is derived from consumer bookkeeping, and neither `published_request_id` nor
  `pending_tool_results` is read to produce it

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

One exception, for dispatch arguments a process cannot know. When a loop is rebuilt from its record for the
result of a call that went through an approval gate, the dispatched arguments are not available: a `modify` may
have replaced them, and the retained model response holds only the proposal. The tool completion evidence's
`dispatch_arguments` and the tool trajectory step's arguments SHALL then be empty, and SHALL NOT be filled from the
proposal. The `tool.requested` observation recorded at dispatch is the record of the arguments that ran. A process
that dispatched the approved call itself, including one that rebuilt the loop to apply the approval, still records
the arguments it dispatched.

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
- **AND** an authorized registered-Store reader can retrieve the full result through its reference

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

#### Scenario: a cold-recovered approved call records no dispatch arguments

- **GIVEN** a gated call approved with `modify`, whose approval is on the loop record and whose process then lost
  the loop
- **WHEN** the approved call's result rebuilds the loop from its record, retained request and retained response
- **THEN** its tool completion evidence carries no `dispatch_arguments` and its trajectory step no arguments
- **AND** neither carries the proposed arguments from the retained model response

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

The prohibition on durable records is a prohibition on FABRICATION, not on observation. A record that reconstructs lost
evidence, names what is missing, asserts a repair, or claims the trajectory is complete SHALL NOT be manufactured. A
classification of a failure the component itself observed is not such a record, and the loop-level evidence-integrity
condition below is REQUIRED rather than forbidden. Nothing in this requirement licenses a durable claim that evidence
IS complete.

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
- **AND** no counter, seal, gap fact, repair record, or reconstruction of the lost evidence is manufactured
- **AND** no durable claim that the trajectory IS complete is written
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

A trajectory reader SHALL accept only `{loopId,limit,cursor}`, hash the loop ID, validate any cursor's loop binding
before KV listing, prefix-list only `v1.<loop_hash>.>`, Get and validate every visible fact, sort by
`(iteration,phase_rank,source_ordinal,attempt_ordinal,attempt_id)`, and apply the result limit only after that complete
visible set is sorted. It SHALL return fact metadata and evidence references only; it SHALL NOT resolve a Store,
hydrate evidence, or carry an evidence body.

An omitted or zero limit SHALL default to 64. Limits 1 through 256 SHALL be accepted. Negative limits and limits above
256 SHALL be rejected, not clamped. A cursor SHALL be unpadded base64url over strict canonical JSON containing version
`v1`, the requested loop digest, and the complete last-emitted causal tuple. Unknown/missing fields, unsupported
versions, invalid tuples, and cross-loop cursors SHALL return canonical `invalid/invalid_cursor`.

The reader SHALL fit the exact encoded typed page against the connected server's observed maximum payload. The result
cap is not a storage-work cap: every page still lists, Gets, validates, and sorts all visible facts because the KV key
is attempt identity rather than causal order.

Every internal and public response SHALL contain:

```text
coverage: observed
observed_totals: <page-local totals derived only from returned visible facts>
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

#### Scenario: every page is explicitly observed-only

- **GIVEN** any successful trajectory page, including one with missing-evidence references
- **WHEN** the response is returned
- **THEN** `coverage` equals `observed`
- **AND** totals appear only as `observed_totals` derived from returned facts
- **AND** the response makes no completeness guarantee

#### Scenario: a cursor is strict and loop-bound

- **GIVEN** a cursor with an unknown field, missing tuple member, unsupported version, invalid tuple, or another loop's
  digest
- **WHEN** the trajectory reader validates the request
- **THEN** it returns `invalid/invalid_cursor` before listing KV
- **AND** it does not repair, ignore, or reinterpret the cursor

#### Scenario: trajectory reads never hydrate evidence

- **GIVEN** visible facts with valid, missing, or unverifiable evidence references
- **WHEN** a trajectory page is requested
- **THEN** the response contains metadata and references but no evidence body
- **AND** agentic-loop does not borrow or resolve a Store while serving the page

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

### Requirement: Observed audit loss MUST be readable from the loop entity as a classified condition

A loop for which audit loss was observed SHALL carry `agent.loop.evidence-integrity` with the value `incomplete` on its
loop execution entity, stamped on the same terminal graph write that carries `agent.loop.outcome`. Audit loss counts as
observed for a loop when at least one trajectory audit failure was observed while recording that loop's evidence, OR
when the component determined at startup that it cannot record trajectory evidence at all. The predicate SHALL be
absent on every other loop, and its absence SHALL mean only that no audit loss was observed — never that evidence is
complete. The predicate SHALL NOT carry a stage, kind, reason, attempt, or any reconstruction of the lost evidence;
those remain in the `ERROR` log and the bounded counter.

A component that cannot record trajectory evidence at all produces no per-loop failure to observe, because nothing is
attempted, and its startup failure report has no loop subject. Such a component SHALL stamp the condition on every loop
it terminates. Without this, the most complete evidence loss the component can suffer would be the one state
indistinguishable from a healthy one.

The condition SHALL be derived from the same observed failure value that already feeds the Health latch, the metric,
and the log, or from the component's own startup determination that recording is unavailable, and SHALL NOT be derived
by re-evaluating any predicate or by reading the counter.

An observation SHALL NOT mark a loop after that loop's terminal write. A late report from an abandoned audit attempt
SHALL NOT re-mark a released loop, so per-loop marking cannot outlive the loop and a later loop reusing the same loop
ID never inherits another loop's condition. Withholding the late MARK loses nothing, because the component already
reported that loss on the path that abandoned the attempt, in time for the terminal write. The late failure itself is
still a trajectory audit failure and SHALL still emit `ERROR`, increment the bounded counter, and latch Health per the
requirement above; only the mark is withheld.

#### Scenario: a loop with observed audit loss is machine-readable as incomplete

- **GIVEN** a loop for which at least one trajectory audit failure was observed at any stage
- **WHEN** the loop reaches its terminal graph write
- **THEN** the loop execution entity carries `agent.loop.evidence-integrity` with value `incomplete`
- **AND** the triple is written on the same mutation that carries `agent.loop.outcome`, not a separate write

#### Scenario: a loop with no observed audit loss carries no claim

- **GIVEN** a loop for which no trajectory audit failure was observed
- **WHEN** the loop reaches its terminal graph write
- **THEN** the loop execution entity carries no `agent.loop.evidence-integrity` triple
- **AND** no predicate asserts that the loop's evidence is complete

#### Scenario: repeated failures at several stages yield one unqualified condition

- **GIVEN** a loop that observed audit failures at more than one stage
- **WHEN** the loop reaches its terminal graph write
- **THEN** exactly one `agent.loop.evidence-integrity` triple with value `incomplete` is written
- **AND** no stage or reason is elected onto the triple

#### Scenario: a component that records no trajectory evidence marks every loop

- **GIVEN** agentic-loop determines at startup that the trajectory fact bucket is unusable and starts with no recorder
- **WHEN** any loop in that process reaches its terminal graph write
- **THEN** that loop's execution entity carries `agent.loop.evidence-integrity` with value `incomplete`
- **AND** this holds for loops for which no per-loop audit failure was ever reported, because none is ever attempted
- **AND** no loop in that process is stamped as though its evidence were intact

#### Scenario: a late report from an abandoned audit attempt does not re-mark a released loop

- **GIVEN** an audit attempt is abandoned when its framework budget expires, and the loss is reported on that path
- **WHEN** the abandoned attempt later reaches its own failure report, after the loop reached its terminal write
- **THEN** the late report does not mark the loop again
- **AND** the late report still emits `ERROR` with its own stage and reason, increments the bounded counter under that
  same stage and reason, and latches Health degraded
- **AND** the loop's condition remains the one derived before its terminal write
- **AND** a later loop reusing the same loop ID does not inherit the earlier loop's condition

#### Scenario: a failed condition write does not fail agent work

- **GIVEN** the terminal graph write carrying the evidence-integrity condition fails
- **WHEN** the work handler continues
- **THEN** the existing state transition, downstream publish, and source ACK still proceed
- **AND** the absence of the triple is not readable as complete evidence

### Requirement: A decide terminal SHALL be carried as a typed decision on the completion event

When a loop completes because a `StopLoop` tool result arrived from the framework decide tool, the loop SHALL populate
`LoopCompletedEvent.Decision` with the decision's `Action` and `Reason` taken from the tool result's typed metadata,
and SHALL leave `Result` unchanged. The loop SHALL identify the tool through its existing name-fallback chain — the
tracked name for the call ID first, then the tool result's own `Name` — so a process restart or cache loss does not
demote a decide terminal. When the terminal tool is any other tool, or the loop completes on model text, `Decision`
SHALL be nil. The loop SHALL NOT infer a decision from the shape of `Result`. `LoopCompletedEvent.Validate` SHALL
reject a present `Decision` whose `Action` or `Reason` is empty; an unknown but nonempty `Action` SHALL remain valid.
When the terminal tool IS the decide tool but its typed metadata cannot supply both a nonempty `Action` and a
nonempty `Reason` — absent, empty, or not a string — the loop SHALL leave `Decision` nil and SHALL warn, rather than
stamp a half-decision: a present `Decision` with an empty field fails validation and is permanently rejected, which
would lose the terminal entirely instead of degrading it to the existing route-ownership behaviour.

#### Scenario: decide terminal carries its decision

- **GIVEN** a loop whose pending tool call is tracked under the decide tool name
- **AND** its tool result has `StopLoop=true` and metadata `action` and `reason`
- **WHEN** the loop completes
- **THEN** the published completion event decodes with `Decision.Action` and `Decision.Reason` equal to that metadata
- **AND** `Result` equals the tool result content

#### Scenario: tracked name absent, result name identifies decide

- **GIVEN** a loop with no tracked tool name for the terminal call ID
- **AND** the tool result's `Name` is the decide tool name with `StopLoop=true` and decision metadata
- **WHEN** the loop completes
- **THEN** the published completion event decodes with `Decision` populated

#### Scenario: non-decide terminal carries no decision

- **GIVEN** a loop whose terminal `StopLoop` tool is tracked under any other name
- **WHEN** the loop completes
- **THEN** the published completion event decodes with a nil `Decision`

#### Scenario: synthesized decision does not populate the field

- **GIVEN** a loop that completes on model text with `decide` in its tool set
- **WHEN** the framework synthesizes a `needs_clarification` decision triple after completion
- **THEN** the published completion event still decodes with a nil `Decision`

#### Scenario: unusable decide metadata leaves the field nil rather than half-stamped

- **GIVEN** a terminal `StopLoop` tool result named for the decide tool
- **AND** its typed metadata has no `action`/`reason`, an empty one, or a non-string one
- **WHEN** the loop completes
- **THEN** the published completion event decodes with a nil `Decision`
- **AND** the completion still validates, so the terminal is delivered under the existing route-ownership behaviour
- **AND** the loop warns that the decide terminal carried no usable typed decision

#### Scenario: present decision with an empty field fails validation

- **GIVEN** a `LoopCompletedEvent` whose `Decision` is present with an empty `Action` or an empty `Reason`
- **WHEN** the payload is validated
- **THEN** validation fails
- **AND** a `Decision` with an unknown but nonempty `Action` and a nonempty `Reason` validates

#### Scenario: additive wire field round-trips

- **GIVEN** a marshalled `agentic.loop_completed.v1` envelope carrying `decision`
- **WHEN** the production decoder decodes it into a fresh value
- **THEN** the concrete payload is `*agentic.LoopCompletedEvent` with `Decision` populated
- **AND** an envelope without `decision` decodes with a nil `Decision`

### Requirement: Creating a loop that already exists is refused; a continuation attaches to it

`LoopManager.CreateLoopWithID` MUST refuse, rather than overwrite, when the loop token it is given already names
a registered loop. It currently writes the loop entity, the pending-tool set, and a freshly constructed context
manager into their maps unconditionally, which destroys the conversation of any loop already registered under
that token. Refusal MUST happen before any of those three writes, and MUST leave all of them exactly as they
were. The refusal MUST be a distinguishable already-exists condition that a caller branches on — the same shape
the framework's Lifecycle harness already uses for create-versus-exists — not a generic invalid error a caller
can only log.

Ordering is fixed: the loop-token FORM check runs first, and the already-exists check second, so a malformed
token is never reported as a collision.

Task intake MUST use that distinction. A task carrying a loop token that already names a registered loop is a
**continuation**: intake attaches to the existing loop and MUST reuse its context manager, so the conversation
accumulated so far is preserved and the new user turn is appended to it rather than replacing it. Attaching MUST
NOT re-seed the conversation with a fresh system prompt, and MUST NOT clear the loop's pending-tool set.
A continuation whose existing loop is in a terminal state MUST be refused rather than attached, and MUST NOT
mint a replacement loop under the same token.

A continuation whose existing loop has **work in flight** MUST also be refused rather than attached. Work is in
flight when the loop holds outstanding tool calls, or when the loop is awaiting a human approval decision.
Attaching in that window is not a continuation of the conversation, it is a second round on top of a half-written
one: the assistant turn carrying `tool_calls` is already in the conversation and the matching `tool` results are
not, so the assembled request carries orphan `tool_calls`; two rounds then advance the one loop and the one
context manager concurrently; and an attach to a loop awaiting approval moves it off that state, so the human's
later decision is dropped as stale and the gated call is abandoned. The refusal MUST be distinguishable from the
terminal refusal, because the two mean opposite things to the caller — terminal is final, in-flight is answerable
once the round finishes — and it MUST leave the loop's conversation, its pending-tool set, and its recorded state
exactly as it found them. Queuing the turn for later delivery is deliberately NOT the answer: a queued turn is new
semantics this capability does not have. The refusal is ordinary user behaviour — someone typed while the agent
was still working — and MUST NOT be reported at a severity reserved for operator-actionable faults; the other
intake failures keep theirs.

Attaching MUST preserve the redelivery-dedup property that intake already relies on: after a continuation is
accepted, a redelivery of that same continuation MUST be recognised as a duplicate rather than processed twice.

**Preserve, do not restore.** This requirement is about a loop still held by the running process. Reconstructing
a conversation whose process was replaced is explicitly NOT in scope and is claimed separately (#1146).

#### Scenario: a continuation preserves the conversation instead of discarding it

- **GIVEN** a running loop whose conversation already holds a system prompt, a user turn, an assistant turn, and
  a completed tool pair, and which holds no outstanding tool call
- **WHEN** a task carrying that loop's token arrives
- **THEN** the loop's existing context manager is the one used, the prior turns are still present, and the new
  prompt is appended after them
- **AND** no second system prompt is added and no other per-loop state is replaced
- **AND** the tests that verify this are `TestContinuationReusesContextManager` and
  `TestContinuationDoesNotReseedSystemPrompt`

#### Scenario: a direct create against an existing token is refused without touching its state

- **GIVEN** a registered loop with a context manager holding conversation turns
- **WHEN** `CreateLoopWithID` is called directly with that same token
- **THEN** it returns the already-exists condition, and the loop entity, its pending-tool set, and its context
  manager are all unchanged and hold the same values as before the call
- **AND** the test that verifies this is `TestCreateLoopWithIDRefusesExistingTokenWithoutMutation`

#### Scenario: a malformed token is reported as malformed, not as a collision

- **GIVEN** a non-canonical loop token
- **WHEN** `CreateLoopWithID` is called with it, whether or not any loop is registered
- **THEN** the refusal names the token form, not an already-exists condition
- **AND** the test that verifies this is `TestFormRefusalPrecedesAlreadyExists`

#### Scenario: a continuation naming a terminal loop is refused rather than silently restarted

- **GIVEN** a registered loop in a terminal state
- **WHEN** a task carrying that loop's token arrives at intake
- **THEN** the task is refused, no new loop is minted under that token, and the terminal loop's recorded outcome
  is unchanged
- **AND** the test that verifies this is `TestContinuationOfTerminalLoopIsRefused`

#### Scenario: a continuation naming a loop with an outstanding tool call is refused

- **GIVEN** a running loop that has dispatched a tool call whose result has not arrived, so its conversation holds
  an assistant turn with `tool_calls` and no matching `tool` result
- **WHEN** a task carrying that loop's token arrives at intake
- **THEN** the task is refused with the in-flight condition, which is distinguishable from the terminal refusal
- **AND** no user turn is appended to the conversation, no request is published, and the outstanding tool call is
  still outstanding
- **AND** the test that verifies this is `TestContinuationOfLoopWithToolsInFlightIsRefused`

#### Scenario: a continuation naming a loop awaiting a human approval is refused

- **GIVEN** a registered loop in `awaiting_approval` holding a pending approval for a gated tool call
- **WHEN** a task carrying that loop's token arrives at intake
- **THEN** the task is refused with the in-flight condition and the loop is still `awaiting_approval`, so the
  human's later decision still resolves the gated call rather than being dropped as stale
- **AND** the test that verifies this is `TestContinuationOfLoopAwaitingApprovalIsRefused`

#### Scenario: the in-flight refusal is not reported as an operator fault

- **GIVEN** a running loop with an outstanding tool call
- **WHEN** a task carrying its token arrives at the intake seam
- **THEN** the seam declares the refusal without raising it to the severity its other intake failures use, and
  the message is acknowledged rather than redelivered
- **AND** the test that verifies this is `TestBusyRefusalIsWarnedNotErrored`

#### Scenario: a redelivered continuation is deduplicated

- **GIVEN** a continuation that has already been accepted and attached
- **WHEN** the same task message is redelivered
- **THEN** it is recognised as a duplicate and does not produce a second attach or a second user turn
- **AND** the test that verifies this is `TestRedeliveredContinuationIsDeduplicated`

### Requirement: Per-loop in-process state is released at terminal, through the one release point

Every per-loop map the loop manager holds MUST be released when a loop reaches a terminal state. Today the loop
entity, its context manager, its pending-tool set, its queued tool calls, its cached tool definitions, tool
choice, metadata, request timeout and response format, its task prompt, and its truncation-retry counter are
retained for the lifetime of the process: the only method that clears them has no production caller. Growth is
therefore unbounded in the number of loops the process has ever run, and each entry is sized by its
conversation. A conversation the loop can no longer advance is not state; it is a leak.

The release MUST happen at the component's existing single terminal-release point — the one that already frees
the trajectory step aggregate and the observed-audit-loss marker after the loop's terminal observation and
terminal graph write have returned. It MUST NOT be a second release site: one home is what stops a future
terminal path from freeing one aggregate and leaking another. The release MUST remain idempotent.

The release is admissible ONLY under this invariant, which MUST hold for every reader: **after a loop settles,
the absence of its in-process entity is indistinguishable from its presence in a terminal state.** A message
that arrives for a settled loop — a late or duplicate approval response, a late tool result, a late model
response — MUST be treated as an expected settled-drop and MUST NOT be reported as an unexpected failure. That
case is already reachable today whenever a process replacement precedes the late message; this requirement makes
an existing path common rather than introducing a new one, and requires it to be handled deliberately rather
than by accident.

The durable loop record remains the authority for a settled loop's result, and reading it MUST NOT depend on
the in-process maps. Approval-timeout sweeping MUST be unaffected, because a candidate is by definition not
terminal. Nothing in this release MAY read agent execution evidence.

The already-exists fence above and this release interact and the interaction is stated so it is not later read
as a defect: once a terminal loop's in-process entity is released, a direct create against its token no longer
observes a collision in process memory. The refusal of an attach to a settled loop is therefore owned by the
admission gate, which decides from the durable record, and the in-process fence is defence in depth for the
window before release.

#### Scenario: a completed loop's per-loop state is released

- **GIVEN** a loop that has run several iterations with a populated conversation, cached tool definitions, and a
  task prompt
- **WHEN** it reaches a terminal state and its terminal observation and terminal graph write have returned
- **THEN** every per-loop entry the loop manager held for that token is gone
- **AND** the tests that verify this are `TestTerminalReleaseClearsEveryPerLoopMap` and
  `TestTerminalReleaseIsIdempotent`

#### Scenario: releasing does not run before the terminal readers have finished

- **GIVEN** a loop reaching a terminal state
- **WHEN** its terminal trajectory observation, its terminal graph write, and its durable persistence run
- **THEN** each of them observes the loop entity it needs, and the release happens after all of them
- **AND** the test that verifies this is `TestTerminalReleaseHappensAfterTerminalReaders`

#### Scenario: a late approval response for a settled loop is a quiet expected drop

- **GIVEN** a loop that has settled and whose per-loop state has been released
- **WHEN** an approval response for it arrives
- **THEN** it is dropped as stale with the same observability a stale response for a still-present terminal loop
  produces, and not reported as an unexpected failure
- **AND** the same holds for a late tool result and a late model response
- **AND** the tests that verify this are `TestLateApprovalResponseForSettledLoopIsExpectedDrop`,
  `TestLateToolResultForSettledLoopIsExpectedDrop`, and
  `TestLateModelResponseForSettledLoopIsExpectedDrop`

#### Scenario: a settled loop's result is still readable from the durable record

- **GIVEN** a completed loop whose per-loop in-process state has been released
- **WHEN** another agent reads that loop's result through the loop-result tool
- **THEN** the full result is returned from the durable loop record
- **AND** the test that verifies this is `TestSettledLoopResultReadableAfterRelease`

#### Scenario: approval-timeout sweeping is unaffected

- **GIVEN** a loop awaiting approval past its timeout and a set of already-settled loops
- **WHEN** the approval sweeper snapshots expired approvals
- **THEN** the awaiting loop is still a candidate and the settled loops contribute nothing
- **AND** the test that verifies this is `TestApprovalSweepUnaffectedByTerminalRelease`

### Requirement: Loop input classes settle after owner-specific durable done

Agentic-loop SHALL classify task, response, tool-result, cancel-signal, approval-response, and governance-verdict
deliveries through their existing binding owners, and SHALL NOT positively acknowledge a delivery on a converted
class before that lane's durable effect has committed. Task intake is classified through the same owner and, since
#1345, settles by the same rule. Task, response, and tool-result SHALL use the permanent typed heartbeat owner. Cancel signal, approval response, approved verdict, and rejected verdict SHALL retain native
settlement only in their four private binding owners and SHALL expose no native message or work-owning no-heartbeat
adapter.

Each non-heartbeat physical subscription SHALL invoke its typed business handler using the callback installed by its
production setup branch. All delivery-derived work SHALL join before the private callback passes its decision and
cause to `natsclient.SettleDelivery`. JetStream consumer configuration owns AckWait and redelivery; agentic-loop
SHALL NOT derive a universal work deadline from AckWait. An operation MAY use an ordinary business timeout.

Decode, correlation, KV, Store, transition, and required publication failures on a converted class SHALL NOT become
successful callback completion. ACK means the lane-specific durable transition or defined refusal and every required
PubAck completed; Retry means stable identity and reconciliation make re-execution safe; Terminate means permanently
invalid with no useful retry; Quarantine means collision, impossible correlation, panic, or invariant failure
prevents a safe choice.

A failure that arrives after a handler has already moved its loop in memory SHALL be treated as a partial effect and
quarantined, never retried: the redelivery does not reach the loop the first attempt left, so the handler answers it
from the loop's new terminal state and the result the first attempt built cannot be rebuilt. A loop's terminal
business failure SHALL be positively acknowledged only once its failed loop state, its terminal record and its
failure events have committed.

An error accompanying a result that carries a completion or failure event SHALL be read as that terminal's cause,
never as the delivery's disposition: the terminal is the loop's settlement, the terminal owner commits it in its order,
and the delivery settles on that commit — acknowledged once it lands, retried on a lost compare-and-swap, quarantined
on any other commit failure. That reading SHALL be the same on every lane that produces such a result, the
approval-timeout sweeper included, and the error's class SHALL NOT be read for it. Every other error keeps its lane's
own disposition, as the scenarios below record it per lane. The carrier order an applied result takes — birth, gate,
ordinary advance or terminal — SHALL be a function of the result's shape alone: the same shape takes the same order on
every lane that produces it.

The input a task delivery was accepted with SHALL be a fact of the loop record it was accepted into, never of the
process that accepted it: `task_prompt` carries the prompt of the task that bore the loop from the birth write on
and is never rewritten, and `pending_continuation_prompt` carries the text of a deferred turn from the write that
sets its marker to the write that clears it. A rebuild that seats the record restores both and replays an uncarried
turn after the retained conversation, so a rebuilt loop's next request SHALL carry it — once, except in the one
window the record cannot tell apart (a carrier minted after the turn whose record write was lost), where it is
carried twice and logged, and never zero. A task delivery that fails before any durable effect SHALL settle by its
error's class — malformed or invalid terminated, transient retried — and SHALL NOT be acknowledged except as one of
the lane's defined refusals, each named below: a duplicate, an applied task, an unheld continuation, and a
continuation of a loop with work in flight, which stays acknowledged because a Retry would park the whole task lane
(MaxAckPending 1) on a turn no redelivery can fix. A task whose loop record exists SHALL resume from that record on
redelivery; the birth-failure and transient-lineage paths of the same lane settle on their durable effect and are not
exempt. The WHOLE record shares the wire's payload ceiling, every field summed; a record the NATS payload ceiling
refuses is not supported, and the refusal is permanent at birth.

#### Scenario: Required output publication fails

- **WHEN** a handler computes a transition
- **AND** a required publication does not receive PubAck
- **THEN** the source is not positively acknowledged
- **AND** its disposition preserves safe redelivery or quarantines an unsafe invariant

#### Scenario: Cancel completes durably

- **WHEN** an admitted cancel signal is handled
- **THEN** current cancellation state and `COMPLETE_<loopID>` commit
- **AND** the deterministic terminal event receives PubAck before source ACK

#### Scenario: A malformed non-heartbeat input is terminated, never acknowledged as done

- **WHEN** a production non-heartbeat callback receives an input it cannot decode or correlate
- **THEN** it returns Terminate with a non-nil cause
- **AND** no warning-only return becomes ACK

#### Scenario: A malformed heartbeat-lane input is terminated, never acknowledged as done

- **WHEN** a production heartbeat-lane callback receives bytes that do not decode, or that decode to a payload type
  the lane does not handle
- **THEN** the failure is classified as a permanent delivery error and the binding terminates the delivery
- **AND** the delivery is neither acknowledged nor retried

#### Scenario: A handler result fails before any of its publications

- **WHEN** a handler has moved its loop in memory and the loop-state, terminal-record or graph write then fails
- **THEN** the callback reports a fatal-classified error and the binding quarantines the delivery
- **AND** the delivery is not retried into a handler whose terminal guard would answer it with an empty result

#### Scenario: A terminal business failure cannot be recorded

- **WHEN** a handler error fails its loop and the failed loop state, terminal record or failure event does not commit
- **THEN** the source is not positively acknowledged
- **AND** a loop that could not be transitioned at all is retried rather than quarantined, because no effect was
  written and the redelivery is settled from the loop record

#### Scenario: A handler result fails after some of its publications have returned PubAck

- **WHEN** a handler result's state has been stamped and its publication phase then fails partway
- **THEN** the callback reports a fatal-classified error and the binding quarantines the delivery
- **AND** the owner latches its health fatal and drains that lane, rather than redelivering a callback that would
  republish results whose PubAcks already returned

#### Scenario: Approval handler panics

- **WHEN** approval work panics
- **THEN** handler recovery returns a non-nil fatal-classified error
- **AND** the production delivery callback returns Quarantine without persistence or settlement
- **AND** the exact owner stops and drains
- **AND** the panic is never rewritten to nil

#### Scenario: Loop delivery metadata is unavailable

- **WHEN** a loop settlement adapter cannot observe native delivery metadata
- **THEN** it invokes no loop work and makes no heartbeat or settlement call
- **AND** quarantines with `delivery_metadata_unavailable`
- **AND** drains the exact consume handle
- **AND** loop health becomes negative with the exact cause and one error-count increment

#### Scenario: The first fatal result latches health before the handle drains

- **WHEN** any loop delivery owner produces its first result requiring owner stop
- **THEN** health synchronously reports `Healthy=false`, status `delivery ownership lost`, the exact cause in
  `LastError`, and exactly one increment of the existing error count, before owner-stop observation drains the
  exact handle
- **AND** a later fatal result in the same or another lane neither overwrites nor recounts that first cause
- **AND** the latch itself adds no metric family, public state, durable state, or communication path

#### Scenario: A populated terminal result that arrives with an error is the loop's settlement

- **GIVEN** a loop past its own deadline
- **WHEN** `HandleToolResult`, `HandleApprovalResponse` or the approval-timeout sweep's auto-reject returns the failed
  state, the loop-failed event and its publication together with a fatal error
- **THEN** the error's class is not read: the terminal owner commits `COMPLETE_<loopID>`, the graph stamp, the event and
  the record in that order, and the delivery is acknowledged on a committed terminal, retried on a lost
  compare-and-swap, and quarantined on any other commit failure
- **AND** the sweeper, which has no delivery to classify, logs a commit that did not land and echoes no rejection

#### Scenario: The model lane re-derives the same failure from the error

- **GIVEN** the same deadline, noticed by `HandleModelResponse`
- **WHEN** it returns the populated failed result together with the timeout error
- **THEN** the lane commits a failure of the same kind and the `timeout` reason through the terminal owner, built from
  the error rather than from the handed result, and the delivery settles on that commit exactly as on the other lanes
- **AND** the handed result's own event is not the one published; the two carry the same reason

#### Scenario: A non-terminal result with an error settles on its lane's own disposition

- **WHEN** a handler returns a result that carries no terminal event together with an error that is not one of the
  model lane's classification sentinels
- **THEN** the tool lane quarantines it whatever the error's class, unless the error proves the cancellation happened
  before any mutation, which is retried
- **AND** the model lane fails the loop through the terminal owner — reason `max_iterations` for the budget sentinel,
  `timeout` for the loop deadline, `handler_error` otherwise — and the delivery settles on that commit; a loop that
  cannot be transitioned at all is retried
- **AND** the approval lane settles by class: fatal is quarantined, invalid is terminated, anything else is retried

#### Scenario: The same result shape takes the same order on every lane

- **WHEN** a handler returns a result with no error
- **THEN** a result that created the loop is written by create-once before its first request is published; a result
  that gates the loop for approval is written before its approval request is published; any other non-terminal result
  publishes every output first and writes the record by compare-and-swap after; a result carrying a completion or
  failure event goes to the terminal owner
- **AND** a gate, which only the tool-result lane creates, is written before it is published, and an ordinary
  advance produced on the model, tool or approval lane or by the sweep publishes before it writes on every one of
  them

#### Scenario: A terminal-guard result is settled by the record, whichever lane produced it

- **WHEN** a handler answers a delivery with an effect-free result because the loop is already terminal in memory
- **THEN** the loop's record decides: absent or terminal is acknowledged and counted as the lane's drop; live or
  unreadable is retried, because the terminal in memory may be a commit still in flight on another lane
- **AND** a guard result is never returned together with an error, so the failed-terminal reading never meets one

#### Scenario: A refusal before any mutation is retried; a refusal naming invalid input is terminated

- **WHEN** a handler refuses before it touched the loop — the delivery context was cancelled first, an approval
  answer fails validation, or a task names a depth at or past its limit
- **THEN** the cancelled model response, tool result or task is retried; the invalid approval answer and the
  over-depth task are terminated; nothing was registered for any of them

#### Scenario: An approval answer whose loop was released after its gate resolved is recovered cold

- **GIVEN** an approval answer that won the resolve, so the gate is cleared in memory
- **WHEN** the loop is released before the handler re-reads it, so the handler returns an empty result with a
  not-found error
- **THEN** the delivery is retried; the redelivery finds no loop in memory, reads the still-gated record, rebuilds the
  loop and applies the answer exactly as the process that gated the loop would have

#### Scenario: A tool result whose loop was released mid-delivery is quarantined

- **GIVEN** a tool result whose routing entry resolved a loop this process holds
- **WHEN** the loop is released between that lookup and the handler's own read of it, so the handler returns an empty
  result with a not-found error
- **THEN** the delivery is quarantined, as any non-terminal handler error on this lane is; the released loop's record
  is what a later cold delivery reads

#### Scenario: The model lane's classification refusals are settled from the record

- **WHEN** `HandleModelResponse` refuses a response as superseded or already applied
- **THEN** it is acknowledged without effect
- **WHEN** it refuses the response as naming a request that is not the loop's
- **THEN** it is quarantined
- **WHEN** it refuses the response as newer than the request the record names
- **THEN** it is retried until the record names it, with the loop left exactly as it was

#### Scenario: The task lane's results settle on their own owner, and its errors stay exempt

- **WHEN** `HandleTask` returns a result that created the loop
- **THEN** the task lane writes the record by create-once, then publishes the first request; a refused create or a
  failed publish releases the loop and is retried, and the redelivery finds the record and republishes the request
  it names rather than birthing a second loop
- **WHEN** it returns only the loop identifier of an active loop
- **THEN** the task is acknowledged as a duplicate
- **WHEN** it returns a deferred continuation
- **THEN** the pending-continuation marker and the turn's text are written together by one compare-and-swap onto the
  record the lane read; a lost compare-and-swap is retried, any other write failure is logged as best-effort and
  the task is acknowledged with the turn held in process memory only
- **WHEN** it returns an error
- **THEN** the delivery takes the disposition the lane's policy derives from the error's class: a refusal naming
  invalid input — an over-depth task, a continuation of a settled loop — is terminated, a fatal-class error is
  quarantined, and every other error — a cancelled delivery context at shutdown or stop among them — is retried,
  except a continuation refused because its loop has work in flight, which stays acknowledged as a defined
  refusal: a Retry parks the whole task lane, which runs at MaxAckPending 1, for the redelivery budget on a turn no
  redelivery can fix, and the turn must be re-sent. No production path fails after a birth registered its loop; one
  that did would be retried into the duplicate acknowledgement of the loop it left registered. The exemption this
  heading names ended with #1345
- **AND** the birth-failure and transient-lineage paths of the same lane settle on their durable effect: a failed
  graph birth establishes the loop's terminal before the delivery is acknowledged, and a transient lineage write
  remembers the spawn result so its redelivery resumes it rather than deduplicating it

#### Scenario: The deferred continuation's replacement behaviour is owed to #1365

- **GIVEN** a deferred continuation whose marker and text reached the record, and whose carrier is still empty
- **WHEN** a replacement process rebuilds the loop from that record and the request the record names
- **THEN** the retained conversation is replayed and the turn's text is appended after it as the user's turn, the
  marker is kept, and the next completion carries the turn into the loop's next request exactly once; the request
  that carries it is named as its carrier, and the marker, its carrier and its text clear when that request settles.
  Carried means placed in the conversation as the user's turn: from then on it is an ordinary message, and
  compaction may summarize it like any user turn; only an emptied context re-injects it (owner 2026-09-26, (a))
- **AND** a record whose marker names a carrier is left as it is, because the retained request carries the turn; a
  record whose marker is uncarried but carries no text is cleared with a warning, as before this change
- **AND** a rebuild that finds a request newer than the one the record names adopts it and leaves the marker as it
  found it, then replays the text after that request's conversation: once when the request was minted before the
  turn (a turn deferred behind a request tracked but not yet on the record), twice when the request already carries
  it (a carrier whose record write was lost) — the record cannot tell the two apart, the replay is logged naming the
  loop and the request, and the turn is never lost

#### Scenario: A durable terminal with a non-terminal record is owed to #1377

- **GIVEN** a terminal whose `COMPLETE_<loopID>` and event landed and whose record write lost its compare-and-swap, or
  an approval-timeout sweep terminal whose marker landed and whose publication failed
- **THEN** the commitment is known for the marker and unknown for the record; a redelivered input meeting the marker
  adopts it by loop identifier and terminal kind through the terminal owner; a timer is never redelivered
- **AND** issue #1377 owns making the record converge on the durable terminal through the declared recovery path,
  adding no row and changing no disposition here

#### Scenario: A rebuilt loop's terminal event carries the prompt its record accepted

- **GIVEN** a loop whose record carries `task_prompt` from its birth write
- **WHEN** a replacement process rebuilds the loop from the record and a retained request, and the loop later
  completes or fails, or its context is emptied by repair
- **THEN** `LoopCompletedEvent.Prompt` and `LoopFailedEvent.Prompt` carry the record's prompt, and the empty-context
  recovery re-injects it rather than its placeholder
- **AND** the prompt is the one that bore the loop: birth renders it and no later write rewrites it — a
  continuation's turn is the record's pending text or its retained request, never its prompt — and a record with no
  prompt leaves the readers reading empty, as before this change

#### Scenario: A deferred turn survives its carrier's truncation retry

- **GIVEN** a deferred turn carried by the loop's outstanding request, its marker naming that request
- **WHEN** that request's answer is `length_truncated` and the loop compacts and retries
- **THEN** the answer settles only the outstanding request: the marker, its carrier and its text are kept, because a
  truncated answer did not answer the turn
- **AND** when compaction empties the context, the recovery re-injects the birth prompt and then the turn; the retry
  is named the turn's carrier, and the retry's answer settles the deferral
- **AND** a rebuild inside the truncation window still reads a carried marker and replays nothing, because the
  retained request holds the turn

#### Scenario: A malformed task is terminated, never acknowledged as done

- **WHEN** the task lane receives bytes that do not decode, or that decode to a payload type the lane does not handle
- **THEN** the delivery is terminated with a non-nil cause and is neither acknowledged nor retried, exactly as the
  response and tool-result lanes terminate theirs
- **AND** no record makes such a delivery resumable: the bytes will never decode

#### Scenario: A task that fails before anything is registered is redelivered into a fresh birth

- **WHEN** a task's handler refuses before it registered a loop — the delivery context was cancelled, or a create
  failed — and the delivery is retried
- **THEN** the redelivery is a birth: it finds no record and no loop in memory, is not deduplicated, and creates the
  loop
- **AND** a task that fails after its record exists is redelivered into the cold fork, which republishes the request
  the record names and writes nothing; the record written before the first publication is the resumable fact, and
  no second one is kept

#### Scenario: A loop record the payload ceiling refuses is not retried

- **WHEN** the NATS client refuses a record write because the whole rendered record — every field summed, the prompt
  and a deferred turn's text included — exceeds the server's payload ceiling
- **THEN** at birth the refusal is permanent: the loop is released and the task is terminated with the loop id and
  the record's size in the cause, never retried into the same refusal on a lane that runs at MaxAckPending 1; the
  loop-execution entity born before the write is stamped failed with the ceiling as its terminal reason, and the
  refusal is counted as a task intake rejection
- **AND** at the deferred turn's marker write the refusal is logged with the size, the text is dropped from the
  in-memory entity so later record writes fit, and the delivery is acknowledged as any other best-effort marker
  write failure: the turn is in the loop's context and is carried by the next request, and it is not durable. A
  deferred turn whose text the record refused for size is not recovered when a later compaction empties the
  context
- **AND** at a carrier write the refusal quarantines the delivery as any other carrier write failure does; the
  record's text fields count toward it, so a turn that fit its own message fits the record unless the record's
  other fields have filled it

#### Scenario: A tool result is cancelled after the loop has advanced

- **WHEN** delivery work for a tool result is cancelled after the handler has stored the result, advanced the
  iteration, or drained accumulated results
- **THEN** the delivery quarantines, because a replay meets a loop that has already moved
- **AND** only a cancellation the handler observed before it touched anything is retried, so a clean stop that
  mutated nothing does not latch delivery ownership lost

#### Scenario: A terminal failure's record is written before its event is published

- **WHEN** a terminal handler result carries a failure state
- **THEN** `COMPLETE_<loopID>` is written before the graph stamp and before any failure event is published
- **AND** a record write that fails quarantines the delivery with nothing published, so no watcher reads a failure
  event with no terminal record behind it

#### Scenario: A loop-execution birth failure is not exempt

- **WHEN** a task's graph birth or lineage write fails
- **THEN** the loop's terminal business failure is established before the delivery is acknowledged
- **AND** a failure that could not be recorded quarantines instead

### Requirement: A loop absent from process memory is settled from its record

A callback that receives an input naming a loop it does not hold in memory SHALL NOT infer from that absence that
the loop is finished. It SHALL classify the loop from the loop record: absent or terminal is stale, non-terminal is
live, and any failed or undecodable read is unknown. A stale loop SHALL be acknowledged and counted as an expected
drop; a live or unknown loop SHALL be retried, because a positive acknowledgement would discard work this process
lost rather than work that completed. The classification SHALL perform no recovery: it reconstructs no state,
re-registers no routing, and reads no retained request. A loop identifier recovered from a structured identifier
SHALL be a framework-minted token, so a provider-authored identifier is never used as a record key.

#### Scenario: A response or tool result arrives for a loop this process does not hold

- **WHEN** a model response or tool result names a loop absent from process memory
- **AND** the loop record shows the loop absent or terminal
- **THEN** the delivery is acknowledged and counted as an expected drop naming a stale identifier

#### Scenario: The named loop is still live

- **WHEN** a model response, tool result, or governance verdict names a loop whose record is non-terminal
- **THEN** the delivery is retried rather than acknowledged
- **AND** no expected-drop count is recorded for it

#### Scenario: The loop record cannot be read

- **WHEN** the loop record read fails, or its value does not decode
- **THEN** the delivery is retried
- **AND** the failure is never reported as a stale loop

#### Scenario: A cancel signal names a loop that cannot be cancelled

- **WHEN** a cancel signal names a loop that is already terminal
- **THEN** the signal is acknowledged effect-free and counted as an already-terminal drop
- **WHEN** a cancel signal names a loop this process does not hold
- **THEN** the signal is settled by the same record classification as any other input

### Requirement: Delivery work joins before settlement

Every goroutine spawned by delivery work SHALL join before its callback returns. A deadline cancels the operation but
SHALL NOT authorize return while work remains live.

#### Scenario: Delivery work exceeds its budget

- **WHEN** bounded work reaches its deadline
- **THEN** the owner cancels and joins before callback return

#### Scenario: Terminal approval rejection reaches a bounded graph write

- **WHEN** an approval rejection produces a terminal result and its bounded graph write reaches cancellation
- **THEN** graph-write work observes the delivery-derived context and joins before the callback returns
- **AND** the approval source is not settled while that work remains live

### Requirement: Long-running loop heartbeat policy is valid before acquisition

Task, response, and tool-result consumers SHALL default to heartbeat 15s against BackOff `[30s,2m]`. They SHALL
validate the exact acquisition config before consumer allocation; heartbeat SHALL be no greater than half the
shortest positive BackOff. MaxDeliver SHALL be at least the number of BackOff entries, so the fixed two-entry BackOff
requires MaxDeliver at least 2. Omitted or zero MaxDeliver SHALL default to 2. An explicit value below 2 SHALL be
refused before consumer allocation; the owner SHALL NOT truncate BackOff or admit a single-delivery posture.

#### Scenario: Legacy loop default is refused before allocation

- **WHEN** setup observes heartbeat 60s and BackOff `[30s,2m]`
- **THEN** it returns a typed error naming the values and 15s ceiling
- **AND** allocates no consumer

#### Scenario: Single delivery is refused before allocation

- **WHEN** setup observes MaxDeliver 1 with BackOff `[30s,2m]`
- **THEN** it returns a typed policy error naming observed 1 and required minimum 2
- **AND** allocates no consumer

#### Scenario: Minimum valid delivery count reaches acquisition

- **WHEN** setup observes MaxDeliver 2, heartbeat 15s, and BackOff `[30s,2m]`
- **THEN** heartbeat and delivery-count validation pass
- **AND** setup may allocate the consumer with the unchanged two-entry BackOff

#### Scenario: Shipped fixtures resolve a valid policy

- **WHEN** every shipped loop configuration fixture resolves its consumer config
- **THEN** each one satisfies the heartbeat ceiling and the delivery floor

#### Scenario: The non-heartbeat lanes are held to the same retry floor

- **WHEN** the cancel-signal, approval-response, approved-verdict, or rejected-verdict lane is set up
- **THEN** it acquires a consumer carrying a non-empty BackOff and a MaxDeliver covering it
- **AND** the same validation refuses a configuration below that floor before allocating a consumer

#### Scenario: Retry on a non-heartbeat lane is delayed, not immediate

- **WHEN** a non-heartbeat callback returns Retry
- **THEN** the binding negatively acknowledges with a delay rather than at line rate

### Requirement: A logical model request has one deterministic identity

Agentic-loop SHALL mint every `agent.request` RequestID as `<loopID>:req:<iteration>:<retry>`, where `iteration` is
the 1-based ordinal of the request within its loop and `retry` is the within-iteration truncation-retry ordinal.
Minting the same logical request twice SHALL yield the same RequestID. The `<loopID>:req:` prefix SHALL be
preserved so loop recovery from a RequestID and the `agent.response.<requestID>` subject are unchanged.

Every `agent.request` publication SHALL stamp its RequestID as `Nats-Msg-Id`. Server-side duplicate suppression
inside the stream's configured window is a bounded convenience and SHALL NOT be treated as the mechanism that
prevents repeated provider work; that mechanism is the retained-response rule in `agentic-model`.

Because the ordinals move only when the loop advances, agentic-loop SHALL hold at most ONE outstanding
`agent.request` per loop ACROSS DELIVERIES IT PROCESSES IN ORDER. A continuation admitted while a request is
outstanding SHALL be neither refused nor published: its turn is added to the loop's context and recorded on the
loop entity as pending, and the outstanding response SHALL carry it into the next iteration's request rather than
settling the loop.

A loop completes on terminal model text OR on a tool result that terminates it, and BOTH are completions for this
rule: a terminal tool answered while a turn is pending SHALL carry the turn rather than settle, and the carried
request SHALL contain that tool's own result, because a request holding an assistant tool call with no answering
tool message is a broken pair. Only a loop with nothing deferred completes on either path.

Every OTHER call in that assistant message SHALL be answered on the carried request too. One unanswered call
invalidates the whole group rather than only itself, so a call that was queued and never dispatched — the terminal
result cancels it — SHALL receive a result naming the terminal tool as the reason. Without it the repair that
protects the provider contract removes the assistant message and the terminal tool's own result with it, and the
carried turn reaches the model with no record of what the agent decided.

The qualifier is the truth about this layer, not a softening. The admission check and the mint are separate
critical sections, and `agent.task` and `agent.response` are separate JetStream consumers, so a continuation
delivered between a response clearing the mark and the carrying request re-taking it is admitted against an empty
mark and both paths mint the same identity. Closing that window needs either per-loop serialization across the
two consumers or a durable check-and-set on the mark; neither is this layer's, and until one exists the guarantee
SHALL NOT be stated unqualified.

A response naming a request other than the loop's CURRENT one — the newest request minted for it — SHALL change
nothing and settle as handled, counted under a reason label. The identity check is required because carrying a turn
leaves the loop non-terminal: the terminal guard that absorbs a redelivered completion does not apply, so without
it a redelivery would settle a loop whose carrying request is still in flight.

Current, not merely outstanding. A tool-call response settles its request while the loop stays on that iteration
waiting for executors or a human approval, so for that whole window the loop is waiting on no model response at
all; a check keyed on that emptiness admits an EARLIER request's redelivered completion, which then settles the
loop with the previous task's answer while the request carrying the user's newer turn is still being worked. A
redelivery of the current request SHALL still be handled, because it is the answer the loop is owed. A response
arriving for a loop this process has minted no request for SHALL also be handled: that is the process-replacement
case, where the routing was rebuilt from the RequestID rather than from a mint, and deciding it needs durable
request identity.

The record of a pending continuation SHALL name the request that carries it, and SHALL NOT be cleared before that
request's response arrives. The loop entity is persisted before its publications are emitted, so a marker cleared
when the carrying request is BUILT is durably clear about a publication whose durability is unknown; naming the
carrier keeps the turn recoverable while still preventing a second carry.

A turn that cannot be carried at all — the loop is at its iteration ceiling when the completion arrives — SHALL
leave the loop completing and SHALL be retained on the record with no carrier named, rather than cleared. A turn
no request ever contained is otherwise recoverable only from a log line. The retained record SHALL NOT make the
settled loop continuable: a task naming a terminal loop is refused as it always was.

#### Scenario: The same logical request is minted twice

- **WHEN** a task redelivers and agentic-loop mints the request for a loop whose iteration and retry ordinals have
  not moved
- **THEN** the RequestID is byte-identical to the one minted the first time
- **AND** a request minted at a different iteration is a different RequestID

#### Scenario: A truncation retry names its own attempt

- **WHEN** a length-truncated response at iteration N triggers the compaction retry
- **THEN** the retry's RequestID is `<loopID>:req:N:1`
- **AND** forward progress clears the retry ordinal back to 0 for the next iteration

#### Scenario: A consumer reads the loop token from a RequestID

- **WHEN** any consumer recovers the loop token from a RequestID, by the `:req:` separator or by the first colon
- **THEN** it reads the same loop token the framework minted
- **AND** the resolved `agent.response.<requestID>` subject is a single valid NATS token

#### Scenario: A continuation is admitted while a request is outstanding

- **WHEN** a task naming a live loop is admitted before that loop's outstanding `agent.request` has been answered
- **THEN** no `agent.request` is published for it, because a request minted now would carry the outstanding
  request's RequestID and `Nats-Msg-Id` with different bytes
- **AND** the continuation is not refused: its turn is added to the loop's context and the loop entity records a
  pending continuation
- **AND** the task delivery is acknowledged, because the loop entity write is the effect it owns

#### Scenario: A completion response meets a deferred continuation

- **WHEN** the outstanding response would complete the loop and a continuation is pending
- **THEN** the loop does NOT complete: no completion record is built and no `agent.complete` is published
- **AND** the loop advances one iteration and publishes `<loopID>:req:N+1:0` carrying the deferred turn
- **AND** the loop entity records that request as the carrier, on this path and on the tool-results path alike, and
  the pending record is cleared when that request's response arrives

#### Scenario: A terminal tool answers a loop that has a deferred continuation

- **WHEN** a tool result that terminates the loop arrives while a continuation is pending
- **THEN** the loop does NOT complete: no completion record is built and no `agent.complete` is published
- **AND** the loop advances one iteration and publishes `<loopID>:req:N+1:0` carrying both the deferred turn and
  the terminal tool's own result
- **AND** a terminal tool answering a loop with nothing deferred completes it exactly as before

#### Scenario: A terminal tool ends a batch that still has queued calls

- **WHEN** a tool result that terminates the loop arrives while a continuation is pending, and the same assistant
  message advertises calls that were queued and never dispatched
- **THEN** each queued call receives a correlated result naming the terminal tool as the reason it was skipped
- **AND** the carried request carries the assistant message, the terminal tool's own result and the skipped
  results as one complete group

#### Scenario: A turn is deferred and its carrying request cannot be confirmed

- **WHEN** the publication carrying a deferred turn fails with unknown durability and the delivery is quarantined
- **THEN** the persisted loop entity still records the turn as pending and names the request that was to carry it
- **AND** no second request is minted for the same turn while that record names a carrier

#### Scenario: A turn is deferred behind a completion the loop has no iteration left to answer

- **WHEN** a completion arrives for a loop at its iteration ceiling with a continuation turn pending
- **THEN** the loop completes and warns that the turn was not carried
- **AND** the persisted loop entity still records the turn as pending, with no carrier named
- **AND** a task naming that settled loop is refused rather than continuing it

#### Scenario: A response arrives for a request the loop has moved on from

- **WHEN** a model response names a request other than the loop's current one, including while the loop waits on
  tools or an approval and is therefore waiting on no model response at all
- **THEN** the loop is not advanced, not completed, and publishes nothing
- **AND** the delivery is acknowledged and counted under a drop reason, because redelivering it cannot help
- **AND** a redelivery of the CURRENT request is still handled, because it is the answer the loop is owed
- **AND** a response for a loop this process has minted no request for is still handled, because refusing it would
  strand a live loop whose process was replaced

#### Scenario: A duplicate request publish meets a configured window

- **WHEN** the same RequestID is published twice to a stream that declares a `Duplicates` window, inside that window
- **THEN** the server rejects the second publish and one message is stored on the subject
- **AND** a publication carrying no `Nats-Msg-Id` still repeats, because at-least-once is unchanged

### Requirement: Tool execution has stable framework correlation

The framework SHALL preserve provider ToolCall ID for conversation semantics and stamp a distinct execution identity
derived from RequestID, provider CallID, and positive call ordinal. Tool, approval, governance, and completed-outcome
correlation SHALL use the framework identity.

A human approval authorises one EXECUTION, not one provider CallID. The approval-pending event SHALL carry the gated
call's execution identity, and an approval response SHALL echo it. A loop SHALL resolve a pending approval only on a
response whose execution identity matches the pending one, and SHALL refuse a response that carries none against a
pending approval that has one — there SHALL be no fallback to provider CallID. Every responder SHALL echo it,
including the approval-timeout sweeper's synthetic rejection.

#### Scenario: Provider repeats a CallID in another request

- **WHEN** two provider responses use the same CallID under different RequestIDs
- **THEN** their execution identities differ and their completed outcomes cannot collide

#### Scenario: An earlier approval is replayed against a later call with the same provider CallID

- **WHEN** a loop gates a second call whose provider CallID repeats an earlier, already-approved call's, and the
  earlier approval response is delivered again
- **THEN** the loop refuses it as stale and dispatches nothing
- **AND** the loop stays awaiting approval with the later call still pending, so a real decision on it can arrive

#### Scenario: An approval response carries no execution identity

- **WHEN** a response naming the pending call's provider CallID arrives with no execution identity, against a
  pending approval that has one
- **THEN** the loop refuses it rather than matching on provider CallID
- **AND** a pending approval minted before execution identity existed still resolves on provider CallID, because it
  carries none to match

### Requirement: Loop task, request, and tool work use only required correlation

For a new task, dispatch SHALL supply a stable TaskID and a random LoopID retained with that task. Agentic-loop SHALL
validate their mapping and SHALL reject a conflicting mapping. Provider work SHALL carry a stable RequestID. Tool
work SHALL carry the framework execution identity derived from RequestID, provider CallID, and positive call ordinal.

Created, request, approval, continuation, and terminal publications are ordinary durable at-least-once outputs.
Their source ACK SHALL wait for required PubAck. `Nats-Msg-Id` MAY provide bounded duplicate suppression but SHALL NOT
be treated as permanent identity or proof of publication. Exact retained reads SHALL exist only at named boundaries
where they prevent repeating non-repeatable work or prove a lane-specific durable transition already applied.

#### Scenario: Task mapping is stable across redelivery

- **WHEN** a task redelivers after its LoopEntity or initial request committed
- **THEN** agentic-loop validates the same TaskID-to-LoopID mapping
- **AND** any required ordinary publication may repeat and receives PubAck before source ACK

#### Scenario: One task identity names two loops

- **WHEN** a task whose TaskID already names a running loop arrives carrying a DIFFERENT LoopID
- **THEN** agentic-loop quarantines the source delivery rather than answering with the loop already running, whose
  conversation the message does not name
- **AND** the comparison reads the token the PRODUCER sent, so the same task carrying no loop token is still
  deduplicated to the running loop — including a lineage task for which intake reserved a fresh prospective
  identity on this delivery, which is a redelivery and not a second loop

#### Scenario: Request or execution correlation conflicts

- **WHEN** one RequestID or framework execution identity names conflicting required correlation
- **THEN** agentic-loop quarantines the source delivery
- **AND** does not advance the loop or choose either mapping

#### Scenario: Ordinary required publication repeats

- **WHEN** PubAck uncertainty causes a created, request, approval, continuation, or terminal publication to repeat
- **THEN** the duplicate is an admitted at-least-once outcome
- **AND** consumers use the lane's required correlation and durable transition rules

### Requirement: Loop-state authority has one port declaration

Agentic-loop SHALL select its loop-state bucket only from the normalized KV-write facts of its admitted output
named `loops`. The default SHALL remain AGENT_LOOPS. `DeclarePorts` and `NewComponent` SHALL share the existing
configuration derivation.

The loop-side exported `Config.LoopsBucket`, JSON setting `loops_bucket`, default and generated-schema entry
SHALL be removed. Any supplied top-level `loops_bucket` key SHALL fail configuration admission regardless of
value, including a value equal to the port bucket. The error SHALL name the retired key and canonical replacement.
No compatibility alias, ignored value or raw-name precedence SHALL remain.

Research common-bucket validation SHALL obtain agentic-loop's effective bucket through its existing `DeclarePorts`
and canonical port facts. It SHALL compare that bucket against tools and research stages using their unchanged
current selections. It SHALL NOT change those owners' provisioning or execution behavior.

#### Scenario: Default and custom port identities agree across entry points

- **WHEN** valid default configuration or a valid custom `loops` KV-write override is decoded
- **THEN** DeclarePorts, NewComponent and loop initialization select the same bucket
- **AND** no raw bucket field or consumer-local default selects another bucket

#### Scenario: Removed JSON key is never ignored

- **WHEN** configuration supplies `loops_bucket`, including null, empty, default-valued or matching-port values
- **THEN** DeclarePorts and NewComponent return a configuration error naming the key and canonical replacement
- **AND** no bucket or dependent work is acquired

#### Scenario: Research compares actual loop declaration

- **GIVEN** a selected research capability whose tools and stages name bucket A
- **WHEN** agentic-loop's effective loops port names bucket B
- **THEN** existing composition validation refuses when A and B differ and identifies the conflicting owners and values
- **AND** matching custom declarations pass without changing research runtime behavior

#### Scenario: Invalid loop port is not repaired

- **WHEN** effective `loops` configuration cannot resolve as a valid output KV-write bucket
- **THEN** configuration admission fails with component and port context
- **AND** no literal fallback, raw configuration value or partial declaration repairs it

### Requirement: Approval lifetime is bounded by loop-state authority

Agentic-loop approval timeout SHALL default to 12h when `approval_timeout` is omitted.
A supplied value SHALL be a JSON string parsing as a Go duration satisfying `0 < timeout <= 12h`.
Explicit empty, null, non-string, malformed, zero, negative and above-12h values SHALL fail configuration admission
before dependent loop work. Invalid values SHALL NOT be defaulted or clamped.

Startup SHALL additionally require the actual loop-state authority policy defined by
"Loop-state authority is acquired and observed before loop work."
Scalar timeout validation SHALL NOT substitute for observed KV policy, nor SHALL a shorter timeout admit a bucket
with a different TTL.

The 12h maximum and observed TTL24h provide nominal grace only. They SHALL NOT be represented as a guarantee of
successful continuation, timeout application or settlement before expiry. This configuration rule SHALL NOT reset,
shorten or otherwise rewrite an already retained pending approval deadline.

#### Scenario: Timeout is omitted

- **WHEN** configuration omits `approval_timeout`
- **THEN** its effective value is 12h
- **AND** startup still observes and admits the actual loop-state authority before dependent work

#### Scenario: Explicit valid duration reaches the inclusive limit

- **WHEN** a supplied duration is positive and no greater than 12h
- **THEN** scalar timeout validation accepts it, including exactly 12h
- **AND** the configured value is preserved without clamping

#### Scenario: Explicit invalid or excessive timeout is refused

- **WHEN** a supplied value is empty, null, non-string, malformed, zero, negative or greater than 12h
- **THEN** configuration admission fails with the field, offending value and allowed duration range
- **AND** no approval wait, bucket acquisition or dependent consumer starts

#### Scenario: Replacement preserves a retained deadline

- **GIVEN** a valid retained pending approval and different valid replacement configuration
- **WHEN** replacement restores its deadline
- **THEN** the retained RequestedAt and Timeout are unchanged
- **AND** this startup configuration rule does not select or apply an approval decision

### Requirement: Loop-state authority is acquired and observed before loop work

Agentic-loop SHALL use its admitted `loops` KV-write bucket and call internal `loopbucket.AcquireOwner`.
The helper SHALL get first, create only for typed `jetstream.ErrBucketNotFound`, propagate every other lookup
failure without creation, and perform exactly one get after typed `jetstream.ErrBucketExists`.
Creation SHALL declare History 10, TTL 24h and nonbinding MaxBytes.

After get, create or race-get, actual status/backing-stream observation SHALL establish History exactly 10,
TTL exactly 24h and MaxBytes `<=0`. Failed or incomplete observation SHALL refuse admission.
Drift SHALL be refused without update or reconciliation, with observed and required policy values in the error.
I/O failures SHALL preserve their cause.

Only after both observed policy and the effective approval-lifetime requirement pass SHALL the component publish
the handle, perform approval-deadline discovery, allocate dependent consumers/query subscriptions or start its sweeper.
The operation SHALL use the Start-derived context and existing failed-Start rollback.
Trajectory-audit degradation SHALL remain a separate nonblocking policy.

#### Scenario: Two owners race to create a fresh bucket

- **WHEN** two processes acquire the same absent bucket with matching declaration
- **THEN** one create wins and the other gets the existing bucket
- **AND** both observe matching actual policy before dependent work

#### Scenario: Retained or race-winning policy drift exists

- **WHEN** actual History, TTL, or MaxBytes differs
- **THEN** startup refuses without updating the bucket
- **AND** publishes no handle and allocates no dependent work

#### Scenario: Lookup fails for a reason other than absence

- **WHEN** initial lookup returns permission, timeout, transport, or another non-not-found error
- **THEN** acquisition returns it and calls CreateKeyValue zero times

#### Scenario: Concurrent create wins between lookup and create

- **WHEN** CreateKeyValue returns typed ErrBucketExists
- **THEN** acquisition performs exactly one KeyValue get and validates the winner

#### Scenario: Policy observation fails

- **WHEN** status or required backing-policy observation fails or supplies incomplete evidence
- **THEN** startup returns an error rather than treating missing values as matching policy
- **AND** no authority handle, deadline discovery or dependent work is published or started

#### Scenario: Admission refusal precedes dependent allocation

- **WHEN** loop authority or approval-lifetime admission fails
- **THEN** the component remains not ready and returns the failure through existing rollback
- **AND** deadline discovery, task/response/result/signal/approval/verdict consumers and query subscriptions have not started
- **AND** no approval sweeper is running

#### Scenario: Trajectory failure retains its separate policy

- **GIVEN** loop authority and approval lifetime are admitted
- **WHEN** trajectory audit storage is incompatible or unavailable
- **THEN** the existing observable audit degradation policy remains nonblocking
- **AND** it does not weaken loop-authority admission

### Requirement: The loop record names its outstanding request
The `AGENT_LOOPS` record of a non-terminal loop SHALL carry `published_request_id`, the `RequestID` of the
`AgentRequest` whose PubAck preceded the KV update that wrote the record, and every redelivered model response, tool
result, approval response, and governance verdict SHALL be classified against that field and against
`pending_tool_results` rather than against retained conversation content.

The following SHALL hold for every record of a non-terminal loop `L`:

- `published_request_id = R` implies, while the record exists, that an `AgentRequest{RequestID: R, LoopID: L}` is
  durably retained on `agent.request.L`.
- Every `pending_tool_results[e]` whose `request_id` is `R` names an execution ID derived from a tool call of the
  retained response for `R` (membership only; no rendered content is compared).
- `iterations` changes only in an update whose `published_request_id` also changes.
- `pending_approval`, when present, names `request_id = published_request_id`.

The non-terminal record SHALL be written with a compare-and-swap update against the revision observed when the
delivery was admitted. On the model-response, tool-result, and approval-response lanes that update SHALL follow the
PubAck of every output the new record implies, and the approval-timeout sweeper's automatic rejection SHALL take the
same carrier, order, and compare-and-swap as an operator's rejection, writing neither the request name nor the
advance when its publication fails; the sweeper's own write failures are logged, not counted. At loop birth the
record SHALL be written before the first request is published. An update that CREATES an approval gate, on whichever
lane produces it, SHALL be written before the gate is published: a gate published before it is written leaves a human
an approval request with no durable gate behind it. A terminal outcome SHALL be committed as `COMPLETE_<loopID>` by
create-once before its terminal event is published, and the loop entity's terminal state SHALL be written after that
event, the approval-timeout sweeper's automatic rejection included. The terminal transition SHALL clear the loop's
approval gate, so a terminal record carries neither `pending_approval` nor `state_before_approval`. A redelivered
terminal input SHALL adopt the loop's durable terminal by loop ID and terminal kind. On that commit path a durable
terminal of a different kind from the one the redelivered input derives SHALL be quarantined, not adopted: the first
terminal wins. A redelivered cancel that reaches a process not holding the loop, whose record is live and whose durable
terminal is a cancel, SHALL adopt that cancel; when that durable terminal is a completion or a failure, the cancel SHALL
be retried, not quarantined, because the loop's own terminal redelivery writes the record terminal. A terminal whose
record update loses its compare-and-swap after `COMPLETE_<loopID>` and its event have landed is not reconciled: the
loop may keep running under a durable terminal and a published event, and its own later terminal is quarantined. An
approval-timeout sweep terminal (its `max_iterations` auto-reject, or the loop's own timeout) that commits
`COMPLETE_<loopID>` and then fails to publish is not reconciled either: a timer is never redelivered, and the record
stays `awaiting_approval`. After a `max_iterations` terminal, a later human answer is applied cold on a loop that
already has a durable failed terminal. After the loop's own timeout, a later answer to that gate re-derives the
timeout on the rebuilt loop and adopts the durable failed terminal, so the loop settles on that answer. A
redelivered input whose `request_id` is older than `published_request_id` SHALL be acknowledged without effect; one whose
`request_id` is newer SHALL be retried until the record names it; one whose `request_id` is not a request of the loop
SHALL be quarantined, except that such a governance verdict SHALL be terminated, so that one misconfigured verdict rule
terminates its own deliveries (JetStream Term: never redelivered, no dead-letter copy) without stopping the verdict lane. A redelivered tool result whose `request_id` equals `published_request_id` and whose execution
is already named in `pending_tool_results` is a replay of applied work and SHALL be acknowledged without effect, with
the batch's unfinished executions left untouched; ordering cannot decide that case, because the batch is the current
request's. An `approval_required` result stored there by an approval gate is a placeholder, not an answer: it counts as
applied only against another `approval_required` result. The approved call's own result SHALL be applied, and SHALL
be retried while the record still holds the gate for that execution. A redelivered `approval_required` result whose
gate the record still holds SHALL re-publish that gate's approval request from the record and be acknowledged; a
re-publication that fails SHALL be retried. A governance verdict that reaches no waiter, and whose `request_id`, when
present, is a request of its loop, SHALL be acknowledged without effect when its execution is named in
`pending_tool_results`, an approval gate's `approval_required` placeholder included, because a gated call is dispatched
only after its verdict was consumed; one whose loop record is absent or terminal SHALL be acknowledged; one with no
`request_id` SHALL be classified on that membership alone; and a current verdict whose execution is not named SHALL be
retried. A process with no memory of the loop SHALL, before classifying a redelivered model response, tool result,
or approval response, read the newest retained request for the loop and, when it is newer than
`published_request_id`, adopt it into the record by identity first. An approval response whose loop's retained
request or its response is confirmed absent SHALL fail the loop with reason `continuation_unavailable`; an unreadable
stream SHALL be retried. Where this requirement classifies a redelivered input as applied or inapplicable, that
classification SHALL take precedence over the live-record retry of "A loop absent from process memory is settled from
its record". Recovery SHALL never compare rendered messages, result content, or terminal content to decide whether an
input was applied.

A deferred continuation is durable as its marker AND its text. Where the record carries `pending_continuation` with an
empty `pending_continuation_request_id`, a rebuild SHALL replay the `pending_continuation_prompt` the record carries
after the retained conversation as the user's turn and SHALL keep the marker; it SHALL clear the marker and warn only
when the record carries no text (a record written before this tag). The record carries the loop's `task_prompt` from
its birth write, so a loop rebuilt FROM that record and its retained request — the model-response and tool-result cold
arms — publishes its terminal event with that `prompt`; a loop rebuilt from a redelivered task runs the ordinary birth
path and keeps the prompt that task carries.
A continuation reaches only a loop some process holds: a task whose `task_id` differs from the one the live record
carries SHALL be refused — acknowledged without effect, with a warning naming both tasks and a counted intake
rejection — and SHALL NOT be acknowledged as an applied task nor used to rebuild the loop's first request, because
the record can answer only for the task it belongs to. The turn must be re-sent once a redelivered input has rebuilt
the loop.

An approval deadline is process-local and is not a durable fact: a replaced process SHALL re-arm no approval deadline
at startup, and a loop in `awaiting_approval` SHALL stay in `awaiting_approval` until the approval is answered or the
loop is cancelled. A loop the replacement later rebuilds for a redelivered input carries its record's own approval
deadline from then on.

#### Scenario: The next request was published but the record was not updated (W4, tool lane)

- **GIVEN** a running loop whose record names request `R` at iteration `N` and whose last tool result of `R`'s batch
  was applied, and the process published `R(N+1)` and crashed before the record update
- **WHEN** that tool result is redelivered
- **THEN** the loop classifies it as current, re-stores it idempotently, mints `R(N+1)`, finds a retained request
  with that exact `RequestID` on `agent.request.<loopID>`, adopts it without republishing, writes the record with
  `iterations = N+1` and `published_request_id = R(N+1)`, and acknowledges

#### Scenario: A cold replacement adopts the newest retained request before classifying a redelivered result

- **GIVEN** a loop whose record names `R` at iteration `N`, whose newest retained request is `R(N+1)` (published before
  the process crashed, record not updated), and a replacement process with no memory of the loop
- **WHEN** the last tool result of `R`'s batch is redelivered to the replacement
- **THEN** the replacement first writes the record to `R(N+1)` under compare-and-swap (`published_request_id`,
  `iterations = N+1`, `pending_tool_results` empty, any `pending_approval` cleared with `state = running`), then
  classifies the result as older, acknowledges it, and publishes nothing

#### Scenario: A rebuilt loop keeps its record's deadline

- **GIVEN** a loop whose record carries a `timeout_at` that has already passed, and a replacement process with no
  memory of the loop
- **WHEN** an input naming the request the record names is delivered to the replacement
- **THEN** the replacement rebuilds the loop with the record's original `timeout_at` — the deadline is neither
  refreshed nor extended by the time the process was down — and the rebuilt loop fails on that delivery, writing the
  record terminal and publishing a loop-failed event carrying the timeout reason, and the delivery is acknowledged

#### Scenario: An approval answer does not outlive its loop's deadline

- **GIVEN** a loop in `awaiting_approval` whose `timeout_at` has already passed, whose approval deadline has not, held
  by this process or by none
- **WHEN** an approve, modify or reject answering its gate is delivered
- **THEN** no tool call is dispatched; the loop fails with the timeout reason through the terminal owner —
  `COMPLETE_<loopID>`, the loop-failed event, and the record written terminal with its gate cleared — and the delivery
  is acknowledged

#### Scenario: A task rebuilt at iteration zero keeps its record's deadline

- **GIVEN** a loop record at `iterations = 0` naming `published_request_id = R1`, whose `timeout_at` has already
  passed, and a replacement process with no memory of the loop
- **WHEN** the task message is redelivered to the replacement and `R1` is rebuilt from it
- **THEN** the rebuilt loop carries the record's own `started_at` and `timeout_at` — this reconstruction runs the
  ordinary birth path, which would otherwise stamp a fresh budget — so the answer to `R1` settles the loop on the
  timeout instead of running it

#### Scenario: A replaced process re-arms no approval deadline

- **GIVEN** a loop whose record is `awaiting_approval` and whose process was replaced
- **WHEN** the replacement starts and holds no in-memory approval deadline for that loop
- **THEN** no approval deadline is re-armed and no record is written, and the loop stays `awaiting_approval` until the
  approval is answered or the loop is cancelled

#### Scenario: An approval timeout whose rejection could not be published leaves the record as it was

- **GIVEN** a loop gated on a human approval whose deadline has passed, whose record names `R` with the gate on it,
  and whose stream retains `R`
- **WHEN** the sweep's auto-reject mints `R(N+1)` and its publication fails while the record is still writable
- **THEN** neither the new request name nor the iteration it implies is written: the record still names `R` with its
  approval gate intact, and a redelivered result of the gated batch is still classified against it — a record naming a
  request the stream does not retain is refused by every later cold read, so the advance is committed only behind its
  PubAck

#### Scenario: A stale tool result is acknowledged without effect

- **GIVEN** a running loop whose record names `R(N+1)`
- **WHEN** a tool result carrying `request_id = R(N)` is redelivered
- **THEN** it is acknowledged, no message is published, the record is not written, and the inapplicable-result metric
  and audit log line are emitted

#### Scenario: A response that outruns the record update retries

- **GIVEN** a loop whose record names `R` while `R(N+1)` has been published and its record update has not landed
- **WHEN** the model response for `R(N+1)` is delivered
- **THEN** the delivery is retried as not yet observable, and no effect is applied

#### Scenario: A second delivery of an applied response is acknowledged without effect

- **GIVEN** a running loop whose record names `R`, whose model response for `R` has been applied by the process
  holding it, and which is therefore waiting on no request
- **WHEN** the response for `R` is delivered to that process a second time
- **THEN** it is acknowledged without effect, no message is published, the record is not written, and the
  model-response drop metric and an audit log line name the loop and the request — ordering cannot decide this case,
  because both deliveries name the request the record names, and re-applying it would append the assistant turn a
  second time and re-dispatch the batch it already dispatched

#### Scenario: A terminal loop receives a result it cannot prove it applied

- **GIVEN** a loop whose record is terminal
- **WHEN** a tool result for that loop is redelivered, whether or not its execution is still named in
  `pending_tool_results`
- **THEN** it is acknowledged without effect, the inapplicable-result metric increments, and an audit log line names
  the loop, execution, and terminal state — membership is deliberately not consulted, because a terminal loop can
  apply nothing either way and re-deriving which side of the settlement this result fell on would change nothing
  this delivery can do

#### Scenario: A task redelivered at iteration zero publishes the first request nothing retains

- **GIVEN** a loop record at `iterations = 0` with `published_request_id = R1`, an empty `pending_tool_results`, a
  `task_id` that is the redelivered task's, and the stream retains no request for the loop
- **WHEN** the task message is redelivered to a process with no memory of the loop
- **THEN** `R1` is rebuilt from the task and published with `Nats-Msg-Id = R1`, the in-process conversation is
  rebuilt, and the task is acknowledged

#### Scenario: A task redelivered over a first request the stream retains is acknowledged without effect

- **GIVEN** a loop record at `iterations = 0` with `published_request_id = R1`, an empty `pending_tool_results`, a
  `task_id` that is the redelivered task's, and a request the stream retains for the loop
- **WHEN** the task message is redelivered to a process with no memory of the loop
- **THEN** it is acknowledged without effect: no request is published, no record is written, and no loop is seated in
  memory — a retained request means `R1` went out, answered or not, so this delivery has nothing to republish, and
  the loop is rebuilt by the lane that owns its outstanding work: its own response, or the first tool result of the
  batch that response dispatched

#### Scenario: A task redelivered over a first batch that already applied something is acknowledged without effect

- **GIVEN** a loop record at `iterations = 0` with `published_request_id = R1` and a non-empty `pending_tool_results`
- **WHEN** the task message is redelivered to a process with no memory of the loop
- **THEN** it is acknowledged without effect: no request is published, no record is written, and no loop is seated in
  memory — `iterations` alone is not evidence of an untouched birth, because a loop advances its iteration only when a
  whole tool batch is in, and seating a fresh loop would leave the batch's remaining results with no execution to
  route to

#### Scenario: A continuation naming a loop no process holds is refused

- **GIVEN** a live loop record whose `task_id` is `T1`, and a replacement process with no memory of the loop
- **WHEN** a task `T2` naming that loop is delivered to the replacement
- **THEN** it is acknowledged without effect whatever the record's `iterations`, `published_request_id` and
  `pending_tool_results` say: no request is published, no record is written, no loop is seated in memory, and a
  warning plus a counted intake rejection name the record's task and the arriving one — the turn's text is in no
  durable place, so it must be re-sent once a redelivered input has rebuilt the loop

#### Scenario: A tool result the record already applied is replayed

- **GIVEN** a loop record naming `published_request_id = R` whose `pending_tool_results` already contains execution
  `e`, and a process with no memory of the loop
- **WHEN** a tool result carrying `request_id = R` and execution `e` is redelivered — its acknowledgement was lost
- **THEN** it is acknowledged without effect before any rebuild is attempted, the inapplicable-result metric and an
  audit log line name the loop and the execution, the record is not written, and the unfinished siblings of `e`'s
  batch are untouched and still recoverable by their own arrival

#### Scenario: A continuation deferred while a request is unpublished writes only its marker

- **GIVEN** a loop whose tool batch has just completed, so the process has advanced the loop in memory and minted its
  next request `R(N+1)` but that request's PubAck has not landed, and a record still naming `R(N)` at `iterations = N`
  with the completed batch's applied executions
- **WHEN** a continuation is admitted to that loop and deferred behind the outstanding request
- **THEN** the record it writes carries `pending_continuation = true` with an empty `pending_continuation_request_id`
  and leaves `published_request_id`, `iterations` and `pending_tool_results` exactly as it read them — the advance is
  committed by the write that follows the PubAck of the request it implies, so no record ever counts an iteration
  against a request the stream does not retain

#### Scenario: A rebuilt loop clears a deferred turn whose text it cannot recover

- **GIVEN** a loop record with `pending_continuation = true` and an empty `pending_continuation_request_id` — a
  continuation admitted while the loop's request was outstanding
- **WHEN** a replacement rebuilds the loop from that record and its retained request
- **THEN** a record that carries the turn's `pending_continuation_prompt` has it replayed after the retained
  conversation and keeps its marker, and the next completion carries the turn rather than settling
- **AND** only a record that carries no text — one written before this tag — has its marker cleared with a warning
  naming the loop; the next completion then settles the loop instead of spending an iteration re-asking the model
  with a context that gained nothing, and that turn must be re-sent by the caller

#### Scenario: A cold replacement adopts past a rejection-minted request and acknowledges the stale approval response

- **GIVEN** a loop `awaiting_approval` at `R` whose gate was rejected, where the rejection minted and published `R(N+1)`
  and the process crashed before the record was updated
- **WHEN** the approval response is redelivered to a replacement process with no memory of the loop
- **THEN** the replacement writes the record to `R(N+1)` with the gate cleared and `state = running` under
  compare-and-swap, classifies the approval response as inapplicable, acknowledges it, and publishes nothing; the
  inapplicable-result metric and an audit log line name the loop

#### Scenario: A governance verdict redelivered after its waiter is gone

- **GIVEN** a loop that restarted after proposing execution `e` under request `R` and later applied `e`'s result
- **WHEN** the verdict for `e` is redelivered and no waiter exists
- **THEN** the loop reads its record, finds `e` in `pending_tool_results` or `R` older than
  `published_request_id`, and acknowledges the verdict without dispatching it

#### Scenario: A redelivered terminal adopts the published terminal by identity

- **GIVEN** a loop whose durable terminal (`COMPLETE_<loopID>`) exists and whose record is not yet terminal, because the
  process crashed after publishing the terminal event and before the record update
- **WHEN** the input that produced the terminal is redelivered and this delivery derives a terminal whose content differs
- **THEN** the loop adopts the durable terminal by loop ID and terminal kind, publishes it, writes the record terminal under
  compare-and-swap, acknowledges, and logs the content difference at the audit line without retrying or quarantining

#### Scenario: A governance verdict naming a request of another loop is terminated and the verdict lane keeps consuming

- **GIVEN** a loop `L` whose record is live and names a non-empty `published_request_id`, and a governance verdict
  that reaches no waiter whose `loop_id` is `L` and whose `request_id` is non-empty, differs from the record's, and is
  not a request of `L` (an empty side orders as unnamed and is decided on membership: `loop_classification.go:58-60`)
- **WHEN** the verdict is delivered
- **THEN** it is terminated (JetStream Term: never redelivered, no dead-letter copy — the Error log line carries its
  execution, loop and request identities), counted once under `missing_waiter` and once under `foreign_request`, and
  never acknowledged as applied, because the request is checked before membership; the verdict consumer is not
  stopped, so one misconfigured verdict rule terminates only its own deliveries and later verdicts are still consumed

