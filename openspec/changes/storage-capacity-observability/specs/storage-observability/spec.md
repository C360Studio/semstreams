## ADDED Requirements

### Requirement: Every account storage resource MUST appear in one inventory

SemStreams MUST expose a single inventory covering every JetStream-backed storage resource in the
account — ordinary streams, `KV_*` bucket backing streams, and `OBJ_*` ObjectStore backing streams —
regardless of whether this process created or has otherwise touched the resource. Each entry MUST
carry the physical resource name, its storage tier, its configured limits, its actual usage, and its
observed growth rate. Enumeration MUST read each resource's configuration and state from the listing
that returns both together, never from a follow-up describe call per resource; the cost bound
forbids per-resource round-trips, not a second paged listing.

A resource the server declines to describe — one carrying an offline reason, which the info listing
omits entirely rather than reporting — MUST still appear in the inventory, named, with unknown tier
and unknown capacity. An inventory that silently omits the resources nobody can read is worse than
no inventory, because it manufactures the appearance of completeness. Detecting them requires
reconciling the info listing against the name listing, which is not a consistent snapshot: a resource
deleted between the two MAY appear once as unknown and MUST resolve on a later collection.

Logical owner attribution is defined for `KV_*` resources only (see the attribution requirement).
There is no owner registry for ordinary streams or ObjectStore backing streams, and the inventory
enumerates the account — so a resource another process declared has no declaration this process can
read. Those kinds MUST therefore report attribution as **not-applicable**, which MUST be distinct
from the **unattributed** state a `KV_*` resource carries when the catalog does not declare its
bucket. Collapsing the two would report "the framework has no owner concept here" and "this bucket
escaped the catalog" as the same fact, and only the second is a finding.

#### Scenario: A resource this process never touched still appears

- **GIVEN** an account containing a stream created by a prior deploy or a sister process, which this
  process has never created, opened, or published to
- **WHEN** the storage inventory is collected
- **THEN** the resource appears in the inventory with its configured limits and actual usage

#### Scenario: A resource the server declines to describe is still named

- **GIVEN** an account containing a stream the server excludes from the info listing because it
  carries an offline reason (e.g. a persisted config requiring a higher API level than the running
  binary, after a server rollback)
- **WHEN** the inventory is collected
- **THEN** the resource appears with its real name, its kind derived from the name, unknown tier, and
  unknown capacity
- **AND** the inventory does not report itself as complete while omitting it

#### Scenario: Collection never blocks start or health

- **GIVEN** an inventory collection that exceeds its configured timeout
- **WHEN** component start and health evaluation run concurrently
- **THEN** neither is blocked or failed by the collection
- **AND** the inventory reports its last successful result with the timestamp it was collected

### Requirement: KV owner attribution MUST derive from the descriptor catalog

The inventory MUST attribute a `KV_*` backing stream to a logical owner by stripping the single
leading `KV_` prefix to recover the bucket name and resolving that name through the bucket descriptor
catalog, which returns an empty owner for any bucket it does not declare. A bucket the catalog does
not declare MUST be reported as unattributed rather than omitted or force-fit, so account-wide
visibility does not depend on framework ownership. Exactly one leading prefix is stripped: a product
bucket whose own name begins `KV_` yields a backing stream with a doubled prefix, and the recovered
name MUST be the bucket's real name rather than a further-stripped one. The inventory MUST NOT
maintain its own bucket-to-owner mapping, so it cannot disagree with the acquisition seam about who
owns a bucket.

#### Scenario: A catalog bucket reports its catalog owner

- **GIVEN** a `KV_*` backing stream whose bucket name, after stripping one leading `KV_`, is declared
  in the bucket descriptor catalog
- **WHEN** the inventory attributes the resource
- **THEN** the reported owner equals the catalog descriptor's declared owner

#### Scenario: Attribution follows the catalog rather than a copy of it

- **GIVEN** a bucket that was previously declared in the descriptor catalog and is no longer
- **WHEN** the inventory is collected after that removal
- **THEN** the resource reports as unattributed
- **AND** no retained mapping continues to report the former owner

#### Scenario: A doubled prefix is not over-stripped

- **GIVEN** a product bucket literally named `KV_FOO`, whose backing stream is therefore `KV_KV_FOO`
- **WHEN** the inventory recovers the bucket name
- **THEN** the recovered name is `KV_FOO`
- **AND** the resource is reported as unattributed rather than mis-attributed to a bucket named `FOO`

### Requirement: Unknown capacity MUST report as unknown

A resource whose configured limit or actual usage cannot be determined MUST be reported as unknown.
It MUST NOT be reported as unlimited, as zero, or as healthy, and it MUST NOT be silently omitted
from the inventory. An unknown capacity MUST suppress any headroom or time-to-threshold projection
for that resource rather than emitting a fabricated one. Unknown, unbounded, and bounded MUST be
three distinct reported states.

#### Scenario: Capacity cannot be read for a resource

- **GIVEN** a storage resource whose limit or usage cannot be read
- **WHEN** the inventory reports it
- **THEN** its capacity reports as unknown, distinctly from unlimited and from healthy
- **AND** no headroom or time-to-threshold value is projected for it

#### Scenario: An unbounded resource is distinguished from an unreadable one

- **GIVEN** one resource with a deliberately unlimited configured limit and one whose limit could not
  be read
- **WHEN** both are reported
- **THEN** the first reports as unbounded and the second as unknown

### Requirement: Pressure state MUST be derived and reported without enforcement

SemStreams MUST derive a pressure state — `normal`, `warning`, `high`, or `critical` — for every
resource with known capacity, from operator-configurable thresholds over both proportional headroom
and projected time-to-threshold, taking the worse of the two and reporting which input raised it, so
that a slowly-filling large resource and a rapidly-filling small one are both surfaced before
exhaustion. Pressure MUST be report-only in this capability: no write is rejected, no component is
throttled, no readiness gate is failed, and no retention is applied as a consequence of pressure.
Pressure MUST be observable through Prometheus metrics, component health status, and logs — health
may report the state, and MUST NOT degrade readiness because of it. Thresholds MUST be read from live
configuration at evaluation time so a post-boot configuration edit takes effect without a restart.

#### Scenario: Projected exhaustion raises pressure before headroom does

- **GIVEN** a resource with substantial proportional headroom but a growth rate whose projected
  time-to-threshold is inside the configured warning horizon
- **WHEN** pressure is evaluated
- **THEN** the resource reports at least `warning`
- **AND** the reported state names the projection as the input that raised it

#### Scenario: Critical pressure rejects nothing

- **GIVEN** a resource evaluated at `critical` pressure
- **WHEN** writes, component starts, and readiness checks proceed against that resource
- **THEN** none are rejected, throttled, degraded, or failed as a consequence of the pressure state
- **AND** the state is visible in metrics, health status, and logs

#### Scenario: A threshold edit applies without a restart

- **GIVEN** a running process whose pressure thresholds are edited in configuration after boot
- **WHEN** pressure is next evaluated
- **THEN** the evaluation uses the edited thresholds

#### Scenario: A resource with unknown capacity has no pressure state

- **GIVEN** a resource whose capacity is unknown
- **WHEN** pressure is evaluated
- **THEN** no pressure state is reported for it, and its unknown capacity is surfaced instead

### Requirement: Growth rate MUST survive process restart or report unknown

Projected time-to-threshold MUST NOT depend on samples held only in process memory, because a
restart, deploy, or crash-loop would blank the projection exactly when it is most needed, and a
longer smoothing window makes that blackout longer. The growth rate MUST be computed from the
difference between successive observations of a resource's size, persisted so it survives a restart.
It MUST NOT be derived by dividing a resource's current size by the span of its own retained
timestamps: that cannot distinguish sustained growth from churn at a stable size, and would report
growth for a compacting KV bucket whose size never changes. Where fewer than two observations exist,
the rate MUST report as unknown rather than be extrapolated from a single observation.

#### Scenario: A churning but stable-size resource reports no growth

- **GIVEN** a resource whose contents are continuously replaced but whose total size is unchanged
  between successive observations
- **WHEN** its growth rate is computed
- **THEN** the rate is zero and no exhaustion is projected
- **AND** the rate is not inferred from the span of the resource's retained timestamps

#### Scenario: Projection survives a restart

- **GIVEN** a resource with an established growth rate
- **WHEN** the process restarts and pressure is evaluated
- **THEN** a growth rate and projection are available without waiting a full smoothing window

#### Scenario: Insufficient history reports unknown rate

- **GIVEN** a resource for which insufficient history exists to compute a rate
- **WHEN** pressure is evaluated
- **THEN** the growth rate reports as unknown and no time-to-threshold is projected

### Requirement: Operators MUST get an actionable storage report

SemStreams MUST publish the storage report to a framework-owned KV bucket, one key per inventoried
resource, carrying that resource's owner or attribution state, configured bound or its absence,
current usage, headroom, pressure state, projected time-to-threshold, and the timestamp it was
collected. Every operator-facing surface — HTTP route, CLI, alerting, metrics exporter — MUST be a
CONSUMER of that bucket rather than a separate report-producing path, so there is one produced truth
and no surface can disagree with another. A resource absent from the current collection MUST be
removed by deleting its key, never by expiring it under a retention policy: reclamation here is a
semantic decision the collector makes, on the same principle that governs the rest of the graph.
Ranging the bucket MUST reconstruct the whole report; consumers MAY observe a mix of revisions
across keys, which is why each key carries its own collection timestamp. The report MUST name resources carrying no
bound at all. The report MUST compare declared bounds against the account limit **within each storage
tier** — memory-backed and file-backed resources have separate account limits and MUST NOT be summed
together — and MUST report the account limit as unbounded when the server reports no limit for that
tier, rather than treating the absence as a zero or omitting the comparison silently.

#### Scenario: Every operator surface reads the published report

- **GIVEN** the collector has published a report and an operator queries it through the HTTP route
  and again through the CLI
- **WHEN** both responses are compared
- **THEN** both are derived from the same published KV state
- **AND** neither surface recomputes the inventory independently

#### Scenario: A disappeared resource is deleted, not expired

- **GIVEN** a resource that was present in a previous collection and is absent from the current one
- **WHEN** the report is published
- **THEN** that resource's key is deleted
- **AND** no retention policy is configured on the report bucket to expire it

#### Scenario: The report names unbounded resources

- **GIVEN** an account containing at least one resource with no configured bound
- **WHEN** the operator requests the storage report
- **THEN** that resource is named as unbounded together with its owner
- **AND** it is not represented as having capacity headroom

#### Scenario: Over-commitment is reported per storage tier

- **GIVEN** an account whose file-backed limit is smaller than the sum of the declared bounds of its
  file-backed resources
- **WHEN** the operator requests the storage report
- **THEN** the report shows a declared-versus-limit comparison for the file-backed tier that
  identifies it as over-committed
- **AND** memory-backed resources are not included in that tier's sum

#### Scenario: An unbounded account limit is reported as such

- **GIVEN** a server that reports no limit for a storage tier
- **WHEN** the operator requests the storage report
- **THEN** that tier's account limit reports as unbounded
- **AND** the over-commitment comparison for that tier reports as not applicable rather than as
  satisfied
