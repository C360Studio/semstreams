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
indistinguishable from live work.

#### Scenario: A poison-exhausted message does not freeze the in-flight answer

- **GIVEN** a task message that has exhausted `MaxDeliver` and was never applied
- **WHEN** a caller asks whether work is outstanding
- **THEN** the answer reflects genuine outstanding work, not a floor stalled behind that message

#### Scenario: A crashed process does not read as work in flight

- **GIVEN** a loop record left at `state=running` by a process that crashed mid-task
- **WHEN** a caller asks whether work is outstanding for that subject
- **THEN** the answer is derived from consumer bookkeeping, not from the stale record

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
class before that lane's durable effect has committed. Task intake is classified through the same owner but is not
converted here; the requirement below names it. Task, response, and tool-result SHALL use the permanent typed
heartbeat owner. Cancel signal, approval response, approved verdict, and rejected verdict SHALL retain native
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

### Requirement: Task intake is the one loop input class this layer does not convert

Task intake SHALL be named as an exemption rather than left to the absence of a scenario, because the rule above
reads as covering it. An undecodable task envelope, a task payload of the wrong type, a `HandleTask` failure, and a
failed first publication or loop-state write SHALL keep their pre-existing log-and-acknowledge settlement. Converting
them is not a classification change: a redelivered task deduplicates against the loop its first delivery already
created and acknowledges without publishing, so the lane needs resumable intake before a Retry can mean anything.
The exemption is tracked as issue #1345 and is not a permanent property of the lane.
The birth-failure and transient-lineage paths of the same lane are NOT exempt — they settle on their durable effect
today.

#### Scenario: A task delivery fails after its loop exists

- **WHEN** task intake fails to decode its envelope, fails in its handler, or cannot publish its first request or
  write its loop state
- **THEN** the failure is logged and the delivery is positively acknowledged
- **AND** the exemption is recorded here and tracked as issue #1345, with resumable intake named as its
  precondition

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

