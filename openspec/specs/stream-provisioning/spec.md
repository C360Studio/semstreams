# stream-provisioning Specification

## Purpose

Which JetStream streams SemStreams creates, with what bounds, and what it refuses to touch.

An ORDINARY stream is one the framework provisions to carry time-shaped events. This capability owns
their whole provisioning lifecycle: the declaration an operator or a component port writes, the bounds
that declaration must carry, the two escapes from those bounds (a permanent `archival_streams`
classification and an expiring `stream_migration_overrides` bridge), what happens when a live stream
has drifted from its declaration, and what a seam reports when it binds a stream it does not own.

It also owns a prohibition, which is the load-bearing part: a KV bucket's backing stream (`KV_`) and an
ObjectStore's (`OBJ_`) are NOT ordinary streams. They are acquired through the bucket descriptor
catalog and the content-store constructor, their retention contract belongs to `graph-retention`
(ADR-068/073), and age or size eviction stamped onto one is reachability-blind and destroys live graph
state. The provisioner refuses them by NAME PREFIX at every seam, and that refusal is what makes
reconciling the rest safe.

The animating principle throughout: a bound the operator never chose is indistinguishable, in the
operator surface, from one they did. So there are no silent framework defaults for retention, size or
discard policy — every one is a declaration at a named source, and the escapes are declarations of
their own rather than the absence of one.

It does NOT cover how a message gets onto a stream once it exists (`nats-streaming`), nor how full the
account is (`storage-observability`), nor the retention contract of KV and ObjectStore buckets
(`graph-retention`).
## Requirements
### Requirement: The provisioner MUST refuse to govern KV and ObjectStore backing streams

The stream provisioner MUST govern **ordinary streams** only. An ordinary stream is a JetStream
stream SemStreams provisions to carry time-shaped events. A KV bucket backing stream (name prefixed
`KV_`) and an ObjectStore backing stream (`OBJ_`) are NOT ordinary streams: they are acquired
through the bucket descriptor catalog's acquisition seam and the content-store constructor
respectively, and their retention contract is owned by `graph-retention`. The provisioner MUST refuse
to create, bound, or reconcile any stream whose name carries either prefix, and MUST fail closed
naming the resource and the owner that legitimately provisions it. Refusal MUST NOT depend on whether
the framework happens to own the named bucket: a product or sister-repo `KV_*` bucket outside the
descriptor catalog MUST be refused on the same prefix rule, because those resources have no
acquisition seam and no retention backstop to repair a stamp the provisioner applied.

This prohibition is the load-bearing one in this capability. Age or size eviction stamped onto a
graph KV or content backing stream is reachability-blind and silently destroys live graph state, and
the framework's retention reconciler cannot be relied on to undo it: it clears only `MaxAge` and
`MaxBytes` — never a discard policy — it does not run at all for a descriptor declared
retention-unmanaged, and it does not cover buckets outside the catalog.

#### Scenario: A declaration naming a KV backing stream fails closed

- **GIVEN** a stream declaration whose name is `KV_ENTITY_STATES`
- **WHEN** streams are provisioned
- **THEN** provisioning fails naming the resource and the owner that legitimately provisions it
- **AND** no create, update, or reconcile call is issued against that stream

#### Scenario: A non-catalog KV bucket is refused on the same rule

- **GIVEN** a stream declaration naming a `KV_*` backing stream for a product bucket that is outside
  the framework descriptor catalog
- **WHEN** streams are provisioned
- **THEN** provisioning fails on the prefix rule
- **AND** the refusal does not depend on the bucket being framework-owned

#### Scenario: An ObjectStore backing stream is refused

- **GIVEN** a stream declaration whose name carries the `OBJ_` prefix
- **WHEN** streams are provisioned
- **THEN** provisioning fails naming the resource
- **AND** no retention or capacity field is written to that stream

### Requirement: Ordinary streams MUST declare their bounds and discard policy explicitly

Every ordinary stream SemStreams provisions MUST carry an explicitly declared finite `MaxAge`, a
finite `MaxBytes`, and a discard policy, unless it is declared archival (see the archival
requirement). None of the three may be supplied by a silent framework default: a bound the operator
never chose is indistinguishable in the operator surface from one they did, which is the condition
this capability exists to end. Production readiness MUST fail for an ordinary stream missing any of
the three, naming the stream, its declaration source, and the missing field. Where the declaration
source records an owning component, the diagnostic MUST name it; declarations that carry no component
attribution MUST name the source instead of reporting an owner they do not know.

This requirement binds at **creation, through every provisioning seam** — not only declarations
processed by the configuration-driven provisioner. A caller that creates a stream directly through
the client's ensure-stream seam is provisioning, and the backing-stream prefix refusal already
follows it there; bounds MUST follow to the same seam, or a direct caller becomes the one supported
way to create the unbounded streams this requirement exists to prevent.

Both seams MUST refuse with the SAME error identity, so a caller — including a sister repo — can test
for the requirement without knowing which door refused it. One requirement enforced at two seams is
still one requirement.

At the programmatic seam the enforceable set is the two SIZE bounds, and the discard policy is
asked for rather than required. This is a structural limit, not a relaxation: the seam takes the
JetStream stream configuration type directly, whose discard field is an integer whose zero value IS
delete-oldest, so a caller who chose that policy deliberately and one who never considered it produce
byte-identical configuration and no check can separate them. The declarative path CAN require it,
because its discard field is a string where empty is distinguishable from a choice — which is one of
the reasons the declarative path is the one an operator should be given. The programmatic seam's
diagnostic MUST still name the field and what it decides, so an author is told what the check cannot
enforce.

The framework MUST hold its own stream declarations to this requirement, and MUST keep them reachable
by a test rather than buried in a boot path. A framework that exempts itself cannot ask a sister repo
to comply. The historical violation was the now-retired `COMPONENT_CAPABILITIES` stream: it was created
through the ensure-stream seam with a finite age, no byte bound, and no discard policy, so a running
server chose both on the framework's behalf. That provenance remains evidence for the general guard;
it does not describe a stream the framework currently provisions.

Operator-reachable stream declarations MUST expose every required bound as operator configuration. A
component that lets an operator name a stream while fixing its bounds in code has moved the
declaration out of the operator's reach, which is the same invisibility this requirement removes.

#### Scenario: Both seams refuse with one error identity

- **GIVEN** an ordinary stream missing a required bound
- **WHEN** it is refused by the configuration path, and again by the programmatic ensure-stream seam
- **THEN** both refusals carry the same error identity
- **AND** a caller can test for the requirement without knowing which seam refused

#### Scenario: A framework-declared stream satisfies the requirement it enforces

- **GIVEN** a stream the framework itself creates through the ensure-stream seam
- **WHEN** its declaration is checked against the bounds requirement
- **THEN** it declares a finite age and a finite size, and states its discard policy
- **AND** the declaration is reachable by a test, so it cannot drift back to leaving fields unset

Binding to an **existing** stream is a different act and MUST NOT re-assert bounds: a caller that is
not the stream's owner silently restamping another owner's configuration is worse than the drift it
would be correcting. Instead, a seam that returns an existing stream whose live configuration
diverges from the caller's declaration MUST report that divergence — naming the stream and the
declared-versus-observed fields — rather than discarding the declaration in silence. Without this,
a stream two components declare has its limits decided permanently by boot order, with no diagnostic
on either side.

The comparison MUST cover only the fields the caller actually DECLARED. A zero field is silence, not
a declaration of zero: a caller that omits a retention window is not asking for unlimited retention,
so reporting its absent value against a live one would report a divergence the caller never expressed
— and would fire on nearly every bind, which is how a real signal gets tuned out. This matters most
for exactly the callers the create-versus-bind split sends down this path: an under-declared caller
legitimately binds an existing stream, and reporting each of its unset fields as drift would make the
report useless where it is needed.

Where a configuration field's zero value is itself a meaningful value, the divergence is reportable in
ONE direction only, and this MUST be stated rather than left to look exhaustive. Declaring the
non-default value is observable; declaring the default is indistinguishable from declaring nothing.
It is the same limit that stops the creation check from requiring a discard policy, and it has the
same cause.

The report MUST fire on EVERY bind rather than only the first. A divergence suppressed after its first
occurrence erases the only locally available evidence that two processes are contesting one stream: a
report that reappears on every boot with the observed value alternating between two callers' values is
contested ownership, not one stale stream. A seam cannot distinguish the two from its own side — it
sees its declaration and the live configuration, never another process's declaration — so making the
repetition visible is the whole of what it can do.

#### Scenario: A bind that discards a declaration reports what it discarded

- **GIVEN** an existing ordinary stream whose live retention and size differ from a second caller's
  declaration
- **WHEN** that caller binds the stream through the ensure-stream seam
- **THEN** the bind succeeds and returns the existing stream unchanged
- **AND** the divergence is reported naming the stream and each field's declared and observed values
- **AND** no field of the live stream is rewritten

#### Scenario: An undeclared field is not reported as divergence

- **GIVEN** an existing stream carrying limits a binding caller says nothing about
- **WHEN** that caller binds it
- **THEN** nothing is reported, because omitting a field is not declaring its zero value

#### Scenario: A contested stream reports on every bind

- **GIVEN** two processes declaring one stream with different limits, each binding it in turn
- **WHEN** each of them binds
- **THEN** every bind reports the divergence it sees
- **AND** the reports are not suppressed after the first, so the alternating observed value is visible
  as contention rather than as a single stale stream

#### Scenario: A direct ensure-stream caller cannot create an unbounded stream

- **GIVEN** a caller creating a new ordinary stream directly through the client's ensure-stream seam,
  with no declared `MaxBytes`
- **WHEN** the stream is created
- **THEN** creation fails naming the missing bound, exactly as a configuration-declared stream would
- **AND** the seam is not a supported route around the bounds requirement

#### Scenario: Binding to an existing stream reports divergence instead of restamping it

- **GIVEN** an existing stream whose live configuration differs from the declaration a non-owning
  caller passes to the ensure-stream seam
- **WHEN** the caller binds to it
- **THEN** the caller receives the stream handle and the divergence is reported, naming the stream
  and the declared-versus-observed fields
- **AND** the existing stream's configuration is left unchanged, because the binder does not own it

#### Scenario: A stream missing an explicit byte bound fails readiness

- **GIVEN** an ordinary stream declaration with no `MaxBytes`
- **WHEN** production configuration is validated
- **THEN** readiness fails naming the stream, its declaration source, and the missing bound

#### Scenario: A silent default does not satisfy the requirement

- **GIVEN** an ordinary stream declaration that omits `MaxAge`, for which the framework previously
  substituted a default
- **WHEN** production configuration is validated
- **THEN** readiness fails rather than accepting the substituted value
- **AND** the diagnostic states the field the operator must declare

#### Scenario: The declared discard policy is the one applied

- **GIVEN** an ordinary stream declaration carrying an explicit discard policy
- **WHEN** the stream is created
- **THEN** the created stream's discard policy equals the declared value

### Requirement: A shared stream's limits MUST have one stated owner

A stream's limits MUST belong to the component that DECLARES it. A component that only reads a stream
MUST bind it by name without declaring limits — through the get-stream seam, not the get-or-create one
— and MUST treat the stream's absence as a real answer it handles rather than as a reason to create
the stream itself.

This is load-bearing rather than documentation hygiene, and it became so when the provisioner learned
to reconcile retention. Two processes declaring one stream differently no longer merely resolve by
first boot: they FLAP, each repairing the other's value on every boot, forever. No provisioner can
detect that from its own side — it sees its own declaration and the live configuration, never the
other declaration — so the ownership statement is the only thing that PREVENTS the situation, and the
repeated-repair and repeated-divergence reports are the only things that reveal it afterwards.

The statement MUST be discoverable where a stream is provisioned, not only in a specification. It was
previously inferrable only from a single in-repo precedent that a sister repo had to reverse-engineer,
which is how a convention becomes something each consumer guesses at separately.

#### Scenario: A read-only consumer does not declare a stream's limits

- **GIVEN** a component that only consumes from a stream another component declares
- **WHEN** it starts
- **THEN** it binds the stream by name without declaring limits
- **AND** an absent stream is handled as a real condition rather than created by the consumer

#### Scenario: An auto-create loses the race for an already-provisioned stream

- **GIVEN** a consumer auto-create whose pre-check found the stream absent
- **WHEN** the create is refused because the stream already exists
- **THEN** the caller binds the live stream instead of failing
- **AND** the discarded declaration is reported with observed and declared values, since the refusal
  occurs only when the live configuration differs

#### Scenario: Two declarers of one stream are diagnosable

- **GIVEN** two components that both declare the same stream with different limits
- **WHEN** each boots and reconciles or binds
- **THEN** each occurrence is reported with observed and declared values
- **AND** the repetition across boots is what identifies the situation as contested ownership, since
  neither process can observe the other's declaration

### Requirement: A stream whose contract is permanence MUST be declarable as archival

An ordinary stream whose contract is that nothing may ever be evicted MUST be declarable as
**archival** — a permanent classification naming the stream, its owner, and why permanence is the
contract. An archival stream is exempt from the finite-bounds requirement by declaration, and
readiness MUST report it as a named permanent exception that is structurally distinct from a
time-limited migration override, so an operator surface never blurs "this is forever" with "this
expires in March".

An archival stream MUST NOT be expressible only as a renewed migration override. An override's value
comes from being rare and alarming; an archive whose override can only be renewed forever trains an
operator to renew without reading, which is precisely what makes the genuinely time-limited overrides
invisible. Nor is the backing-stream prefix refusal the right instrument: that exempts resources
whose retention contract belongs to another owner, and an archival stream has no other owner —
refusing it would leave it unprovisioned rather than correctly classified.

Archival streams MUST remain fully inventoried. Unbounded is not unmeasured, and capacity reporting
matters MORE for a stream that can never evict, because capacity is then the only lever an operator
has. Since such a stream has no limit of its own, its only ceiling is the account tier limit, so its
pressure MUST be evaluated against that ceiling rather than reported as unevaluable — otherwise
declaring a stream archival would silently remove it from the very surface that would warn about it.

This is a property of the HOLE CLASS, not of the archival classification. Archival is a configuration
concept the reporting surface never sees; what it sees is a resource with no bound of its own, whether
declared archival, admitted by a migration override, or created by another process entirely. The
evaluation MUST therefore key on the absent bound, since a rule keyed on the classification would be
satisfiable by every other route to the same shape.

The evaluation MUST keep the two ceilings structurally distinct, and this is what reconciles it with
the storage-observability requirement that an unbounded resource is not represented as having capacity
headroom. Those are the same rule stated from two sides: no per-resource headroom or projection may be
synthesized for a resource that declares no bound, because the tier's headroom is shared with every
resource in the tier and is not any one of theirs. What the resource carries is the tier's PRESSURE
STATE, labelled with the ceiling it was evaluated against; the headroom and projection numbers behind
it are published once, on the account tier row. A consumer MUST be able to tell the two bases apart,
because they have different fixes: own-bound pressure is relieved by raising that bound or by
retention, and account-tier pressure can be relieved by neither.

The inherited state MUST NOT be scaled by the resource's share of the tier, and the tier's projection
MUST NOT be computed from any single resource's growth rate. Both would read as calm for a small
resource in a tier that is about to exhaust, which is the false-confidence class this capability
exists to remove: an archive is precisely the resource for which exhaustion is unrecoverable.

#### Scenario: An unbounded resource inherits its tier's pressure state, not a fabricated headroom

- **GIVEN** an archival stream holding a small fraction of a file tier whose account ceiling is
  nearly exhausted and still filling
- **WHEN** the report is published
- **THEN** the stream reports an evaluated pressure state equal to its tier's, naming the account
  tier as the basis it was evaluated against
- **AND** it reports no headroom or time-to-threshold of its own, because it declares no bound to
  have them against
- **AND** the state is not reduced to normal on the grounds that the stream is individually small

#### Scenario: A tier that offers no ceiling either is reported as such

- **GIVEN** an unbounded stream whose storage tier's account limit is itself unbounded or unreadable
- **WHEN** its pressure is evaluated
- **THEN** it reports no pressure state, naming which of the two cases applies
- **AND** an unreadable ceiling is distinguished from an absent one, since only one of them is a gap
  in what the process can see

#### Scenario: An archival stream satisfies readiness without finite bounds

- **GIVEN** an ordinary stream declared archival, naming its owner and the reason permanence is its
  contract, with no finite `MaxAge` or `MaxBytes`
- **WHEN** production configuration is validated
- **THEN** readiness passes and the stream is reported as a named permanent exception
- **AND** it is reported distinctly from any time-limited migration override

#### Scenario: An archival declaration without a stated reason is rejected

- **GIVEN** a stream declared archival with no owner or no stated reason for permanence
- **WHEN** the configuration is validated
- **THEN** validation fails, so archival cannot become a silent way to opt out of bounds

#### Scenario: An archival stream is still measured against the account ceiling

- **GIVEN** an archival stream growing against a known account tier limit
- **WHEN** its pressure is evaluated
- **THEN** headroom and projection for that ceiling are computed and published on the account tier row
- **AND** the stream is not reported as unevaluable merely because it declares no limit of its own

### Requirement: Unbounded existing streams MUST be admitted only by an expiring override

An EXISTING ordinary stream that predates this contract MUST be admissible through a migration
override that names the resource, its owner, and an expiry date. Readiness MUST report every active
override as a named, time-limited exception. Overrides MUST NOT be open-ended: an override with no
expiry, or one whose expiry is absent from the declaration, MUST be rejected at validation, so a
migration bridge cannot silently become permanent. An override is for a stream being migrated TO
bounds; a stream whose contract is permanence is archival and MUST use that classification instead,
so the two never share an instrument.

An override MUST NOT create a stream. It admits something that already exists and predates the
contract, and a bridge that provisions is not a bridge — it is the supported route to a brand-new
unbounded stream, with a deadline attached to something that never needed migrating. Provisioning
MUST fail when an override names a stream that is absent, naming the stream and pointing at the
archival classification, which is what an operator reaching for an override on a fresh deployment
actually wants.

Expiry MUST fail validation and provisioning once the deadline has passed, and a RUNNING instance
MUST report a bridge that lapsed while it was up — continuously, and through a surface an alert can
key on, naming the stream and its owner. Enforcement is deliberately at validation and provisioning
rather than at runtime readiness: the stream a lapsed override admits keeps working, so it is a
hygiene failure, and taking a healthy fleet out of service simultaneously because a calendar date
passed would convert that into a self-inflicted outage. The refusal lands at the next boot, which is
when an operator can act on it in any case. Operator messaging MUST say where enforcement lands
rather than implying a running instance will stop serving.

#### Scenario: An expiring override admits a legacy stream

- **GIVEN** an existing unbounded ordinary stream and a migration override naming it, its owner, and
  a future expiry
- **WHEN** production configuration is validated
- **THEN** readiness passes and the override is reported as a named, time-limited exception

#### Scenario: An expired override fails validation and provisioning

- **GIVEN** a migration override whose expiry has passed
- **WHEN** production configuration is validated, or streams are provisioned
- **THEN** it fails naming the override, its resource, and its owner

#### Scenario: A bridge that lapses while an instance is running is reported

- **GIVEN** an instance that started while an override was still active
- **WHEN** the expiry passes without the process restarting
- **THEN** the instance reports the lapse continuously, naming the stream and its owner, through a
  surface an alert can key on
- **AND** it keeps serving, because the admitted stream still works and the refusal lands at the next
  boot

#### Scenario: An override cannot provision a stream that does not exist

- **GIVEN** a migration override naming a stream absent from the account
- **WHEN** streams are provisioned
- **THEN** provisioning fails naming the stream, and no stream is created
- **AND** the diagnostic points at the archival classification, which is the declaration for a stream
  that is permanently unbounded by contract

#### Scenario: An override without an expiry is rejected

- **GIVEN** a migration override that declares no expiry
- **WHEN** the configuration is validated
- **THEN** validation fails, so no migration bridge can be declared open-ended

### Requirement: Editable ordinary-stream drift MUST be reconciled or fail readiness

The provisioner MUST inspect an existing ordinary stream's live configuration rather than treating
create-or-open success as sufficient, and MUST reconcile editable `MaxAge`, `MaxBytes`, and
discard-policy drift to the declaration, logging both observed and declared values. Drift in fields
JetStream cannot update in place — notably storage tier and retention policy — MUST fail readiness
reporting both configurations rather than being silently ignored as it is today. Auto-repair of those
fields is structurally impossible without delete-and-recreate, which is data loss; detect-and-report
is therefore the ceiling of correct behavior, not a weaker fallback. Reconciliation MUST change only
the fields the declaration governs, leaving other backing-stream configuration untouched.

Reconciliation MUST never reach a KV or ObjectStore backing stream. The prefix refusal is what makes
widening the reconciler safe at all: an unfiltered reconciler that learned to write retention fields
is precisely how an operator typo would stamp age eviction onto authoritative graph state. The
refusal therefore MUST be enforced upstream of reconciliation in control flow, not merely alongside
it.

**Every repair MUST be observable, because a repair that repeats is the only locally-available signal
of contested ownership.** A provisioner sees its own declaration and the live configuration; it
cannot see another process's declaration. So two processes declaring the same stream with different
limits is indistinguishable, locally, from ordinary drift — and reconciliation would silently convert
"first boot wins" into "last reconcile wins", flapping the stream between two intents forever. Since
the framework cannot resolve that locally, it MUST make it visible: a stream repaired on every boot,
with the observed value alternating, is contested rather than drifting. Ownership of a shared
stream's limits MUST be stated (see the seam requirement) so the situation is avoidable by
convention rather than only detectable after the fact.

#### Scenario: A repair reports both configurations every time it fires

- **GIVEN** an existing ordinary stream whose live retention differs from the declaration
- **WHEN** the provisioner reconciles it
- **THEN** the repair reports the observed and the declared values together
- **AND** it does so on every occurrence rather than only the first, so a stream being repaired
  repeatedly across boots is visible as contested rather than appearing as a one-time fix

#### Scenario: Editable retention drift is repaired

- **GIVEN** an existing ordinary stream whose live `MaxAge`, `MaxBytes`, or discard policy differs
  from its declaration
- **WHEN** the stream is provisioned
- **THEN** the editable fields are updated to the declared values and the repair is logged with both
  observed and declared configuration

#### Scenario: Non-editable drift fails readiness instead of being ignored

- **GIVEN** an existing ordinary stream whose live storage tier or retention policy differs from its
  declaration
- **WHEN** the stream is provisioned
- **THEN** readiness fails reporting both the observed and the declared configuration
- **AND** the divergence is not silently accepted

#### Scenario: Reconciliation leaves ungoverned configuration alone

- **GIVEN** an existing ordinary stream carrying drifted retention plus configuration the declaration
  does not govern
- **WHEN** the drift is reconciled
- **THEN** only the declared retention and capacity fields change

### Requirement: Port-derived stream declarations consume canonical normalized facts

Raw component configuration SHALL be decoded through canonical `PortConfig` and resolved before stream provisioning.

Only canonical `jetstream` output ports with normalized stream facts SHALL contribute generic stream declarations.

When a canonical JetStream output omits `stream_name`, only the canonical generic provisioner MAY derive the physical
stream name from its declared subjects. No input consumer, component-local helper, or specialized non-provisioning
path may use that derivation.

Provisioners SHALL NOT infer stream identity or policy from retired flat fields, unresolved configuration, concrete
configuration type switches, or consumer-local defaults.

#### Scenario: Named canonical JetStream output contributes exact facts

- **GIVEN** a valid canonical `jetstream` output with an explicit `stream_name`
- **WHEN** component-derived stream declarations are collected
- **THEN** its stream name, subjects, storage, retention, size, replicas, and consumer policy are taken from its
  normalized stream facts
- **AND** those values are not reconstructed independently by the provisioner

#### Scenario: Generic provisioner derives omitted output stream name

- **GIVEN** a valid canonical JetStream output with non-empty subjects and no `stream_name`
- **WHEN** the canonical generic provisioner collects component-derived stream declarations
- **THEN** it derives the physical stream name from the output subjects
- **AND** no other consumer or component-local helper performs that derivation

#### Scenario: Non-JetStream output does not contribute a stream

- **GIVEN** a valid output whose canonical kind is not `jetstream`
- **WHEN** component-derived stream declarations are collected
- **THEN** that output contributes no generic stream declaration
- **AND** its concrete configuration type is not inspected for stream-like fields

#### Scenario: Invalid output fails without fallback derivation

- **GIVEN** an output that cannot be decoded or normalized
- **WHEN** stream declarations are collected
- **THEN** collection fails with typed component, port, kind, and field context
- **AND** no stream declaration is inferred from partial or legacy fields

#### Scenario: Gated-DAG specialization remains narrow

- **GIVEN** the approved gated-DAG specialized provisioning path
- **WHEN** its work-queue stream is provisioned
- **THEN** canonical port facts own resource identity, stream name, subjects, storage, and work-queue retention
- **AND** only the local specialized provisioner owns its exact `MaxBytes`, discard-new behavior, `MaxAge`, and
  deduplication policy
- **AND** that exception does not authorize generic provisioners or other consumers to infer port meaning

### Requirement: Trajectory KV and evidence ObjectStore bypass ordinary stream provisioning

`AGENT_TRAJECTORIES` SHALL be acquired as a KV bucket with history `1` and no TTL. Its immutable per-attempt keys make
each successful Create both the current fact and the watch notification. Prefix listing and watch initial replay SHALL
rehydrate visible facts after restart. The bucket SHALL NOT be declared, bounded, reconciled, or repaired as an
ordinary JetStream stream.

The shipped `AGENT_CONTENT` evidence bucket SHALL be owned by the registered ObjectStore component. Its `OBJ_` backing
stream SHALL remain outside ordinary stream provisioning. Agentic-loop's `kv-write` fact output and StoreRegistry
evidence writes SHALL NOT contribute generic JetStream declarations.

Foundation B SHALL apply no automatic fact TTL, evidence expiry, or guessed retention horizon. Reference-aware
evidence reclamation, treatment of deliberately reclaimed evidence, and legal/privacy deletion policy remain a later
retention ruling. The absence of that policy SHALL NOT block the history-1/no-TTL foundation.

The `kv-or-stream` classification is KV fact/watch, not JetStream queued work: readers need visible immutable facts
after restart, multiple readers may observe them, and no processing ACK/redelivery contract belongs to the audit log.
Existing agent task/model/tool requests remain on their separately declared JetStream work paths.

#### Scenario: trajectory facts provision through the KV owner

- **GIVEN** the canonical `AGENT_TRAJECTORIES` descriptor
- **WHEN** the trajectory store is acquired
- **THEN** the KV bucket has history 1 and no TTL
- **AND** the ordinary stream provisioner never creates or reconciles its `KV_` backing stream

#### Scenario: evidence provisions through the ObjectStore owner

- **GIVEN** a shipped `objectstore` component configured with bucket `AGENT_CONTENT`
- **WHEN** storage starts
- **THEN** the ObjectStore component owns acquisition of its physical bucket
- **AND** the ordinary stream provisioner never creates, bounds, or reconciles its `OBJ_` backing stream

#### Scenario: restart replay exposes current immutable facts

- **GIVEN** trajectory facts committed before a process restart
- **WHEN** a reader prefix-lists the loop or starts a matching KV watch
- **THEN** current per-attempt facts are returned without cache hydration
- **AND** the watch's initial replay is treated as fact rehydration rather than queued-work redelivery

#### Scenario: Foundation B guesses no retention horizon

- **WHEN** trajectory KV and evidence Store configuration are inspected
- **THEN** no fact TTL or automatic evidence expiry is present
- **AND** no caller-computed retention knob is required to use the audit path

### Requirement: Provisioning intent and accepted runtime declaration are distinct facts

The config-time stream planner SHALL run before component construction from canonical `PortConfig`, shared resolution,
and normalized facts. A Registry generation snapshot SHALL represent admitted runtime declaration state.

Neither owner SHALL consume or import the other's policy. Registry snapshots SHALL NOT create, reconcile, or repair
streams. The stream planner SHALL NOT infer component readiness, lifecycle, or admitted runtime state.

#### Scenario: Provisioning may precede later component failure

- **GIVEN** valid configured stream-provisioning intent
- **WHEN** provisioning succeeds and the component later fails construction or Start
- **THEN** the provisioned stream remains truthful configured intent
- **AND** no Registry readiness claim is inferred

#### Scenario: Runtime admission does not provision

- **GIVEN** a component generation is added to Registry
- **WHEN** its snapshot becomes observable
- **THEN** that snapshot does not create, reconcile, or repair a stream

#### Scenario: Both owners use canonical classification

- **GIVEN** one port declaration
- **WHEN** config-time planning and runtime admission classify it
- **THEN** both consume the canonical resolver and normalized facts
- **AND** neither inspects a concrete port-config type or imports the other's policy response

### Requirement: Default-only JetStream outputs require explicit preconstruction coverage

The shipped-flow structural census SHALL contain exactly 61 effective factory-default JetStream output rows absent
from raw component output configuration. All 61 SHALL be explicitly covered by `AGENT` / `agent.>` preconstruction
declarations, and zero SHALL be uncovered.

A future default-only output without explicit preconstruction coverage SHALL fail structural validation. The planner
SHALL NOT guess a stream from a component factory default or runtime Registry snapshot.

#### Scenario: Shipped coverage remains 61 of 61

- **WHEN** all shipped enabled configurations are structurally analyzed
- **THEN** exactly 61 default-only JetStream output rows are found
- **AND** all 61 are covered by `AGENT` / `agent.>`
- **AND** zero are uncovered

#### Scenario: Future uncovered default fails

- **GIVEN** a new factory-default JetStream output omitted from raw config and not covered by explicit
  preconstruction stream policy
- **WHEN** structural validation runs
- **THEN** validation fails
- **AND** the planner does not guess a stream from runtime declarations

### Requirement: The framework MUST provision its MaxDeliver advisory ledger without adopter configuration

The framework MUST own the name, subject, bounds, storage, retention, discard, and replica declaration of the
MaxDeliver occurrence ledger. Operator `config.streams` MUST NOT override that declaration, because it is failure-path
infrastructure whose identity must remain identical across replicas. The ledger MUST be provisioned after ordinary
stream resolution and before component consumers start.

#### Scenario: An adopter provides no advisory configuration

- **GIVEN** a valid SemStreams configuration that says nothing about MaxDeliver advisories
- **WHEN** framework streams are provisioned
- **THEN** the fixed bounded ledger is provisioned
- **AND** no application component must predict its subject, name, durable identity, or retention values

#### Scenario: Configuration collides with the fixed ledger

- **GIVEN** `config.streams` or a component-derived port declares the name `MAX_DELIVERY_EVENTS`
- **WHEN** stream declarations are resolved before NATS I/O
- **THEN** boot rejects the collision even when its values equal the fixed declaration
- **AND** the operator is told to remove the declaration

#### Scenario: A restrictive deployment lacks framework permissions

- **GIVEN** the runtime principal lacks required JetStream stream API, reply-inbox, or fixed-consumer permission
- **WHEN** the framework provisions streams or binds the observer
- **THEN** boot fails loudly
- **AND** the framework does not claim MaxDeliver visibility
