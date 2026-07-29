## ADDED Requirements

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
finite `MaxBytes`, and a discard policy. None of the three may be supplied by a silent framework
default: a bound the operator never chose is indistinguishable in the operator surface from one they
did, which is the condition this capability exists to end. Production readiness MUST fail for an
ordinary stream missing any of the three, naming the stream, its declaration source, and the missing
field. Where the declaration source records an owning component, the diagnostic MUST name it;
declarations that carry no component attribution MUST name the source instead of reporting an owner
they do not know.

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

### Requirement: Unbounded existing streams MUST be admitted only by an expiring override

An existing ordinary stream that predates this contract MUST be admissible through a migration
override that names the resource, its owner, and an expiry date. Readiness MUST report every active
override as a named, time-limited exception, and MUST fail once an override's expiry has passed.
Overrides MUST NOT be open-ended: an override with no expiry, or one whose expiry is absent from the
declaration, MUST be rejected at validation, so a migration bridge cannot silently become permanent.

#### Scenario: An expiring override admits a legacy stream

- **GIVEN** an existing unbounded ordinary stream and a migration override naming it, its owner, and
  a future expiry
- **WHEN** production configuration is validated
- **THEN** readiness passes and the override is reported as a named, time-limited exception

#### Scenario: An expired override fails readiness

- **GIVEN** a migration override whose expiry has passed
- **WHEN** production configuration is validated
- **THEN** readiness fails naming the override, its resource, and its owner

#### Scenario: An override without an expiry is rejected

- **GIVEN** a migration override that declares no expiry
- **WHEN** the configuration is validated
- **THEN** validation fails, so no migration bridge can be declared open-ended

### Requirement: Editable ordinary-stream drift MUST be reconciled or fail readiness

The provisioner MUST inspect an existing ordinary stream's live configuration rather than treating
create-or-open success as sufficient, and MUST reconcile editable `MaxAge`, `MaxBytes`, and
discard-policy drift to the declaration, logging both observed and declared values. Drift in fields
JetStream cannot update in place — notably storage tier and retention policy — MUST fail readiness
reporting both configurations rather than being silently ignored as it is today. Reconciliation MUST
change only the fields the declaration governs, leaving other backing-stream configuration untouched.

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
