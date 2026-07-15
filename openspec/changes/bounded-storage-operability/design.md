## Context

SemStreams currently has four physically different growth surfaces: event streams, authoritative graph
KV, rebuildable indexes, and out-of-line content. They do not share a capacity inventory or pressure
contract. Several defaults are effectively unbounded, while the NATS ObjectStore component hard-codes a
24-hour TTL without proving that live graph references expire with it. The write consumer also acknowledges
input after calling a storage path that cannot report failure to the caller.

The retention ADRs correctly reject reachability-blind TTL for graph identity, but that rule does not mean
operators must accept infinite growth. V1 needs an operational envelope before full semantic GC: bound
history and blobs where lifecycle is known, reject unsafe growth before hard exhaustion, keep current-state
replacement working, and make rebuildable indexes disposable.

The existing abstraction boundary is valuable. `storage.Store` and `StorageReference.StorageInstance`
allow NATS ObjectStore, filesystem, S3-compatible, and future backends to share reference semantics. NATS
ObjectStore is useful for small and immutable content, but it is not the mandatory backend for large media.

## Goals / Non-Goals

**Goals:**

- Give operators one inventory and pressure model for streams, KV, indexes, and store instances.
- Require finite, reconciled limits for time-shaped data and out-of-line content.
- Bound graph growth without age-evicting live identities or relationships.
- Prevent large-object traffic from starving graph updates, reads, deletes, and recovery.
- Make durable references honest across replacement, expiry, write failure, and backend choice.
- Reclaim derived indexes through a readiness-gated rebuild from current entity state.

**Non-Goals:**

- Physical semantic entity GC, cascade deletion, global mark/sweep, or legal-hold policy.
- A single retention duration or byte budget suitable for every product.
- Requiring NATS ObjectStore for content better served by filesystem or S3-compatible storage.
- A zero-downtime index-generation swap in v1.

## Decisions

### 1. Inventory all JetStream-backed and pluggable stores through one framework model

SemStreams will report logical owner, physical resource, storage class, configured limits, actual bytes and
objects/messages, growth rate, headroom, and time to threshold. JetStream inventory includes ordinary
streams plus `KV_*` and `OBJ_*` backing streams. Non-NATS `storage.Store` implementations expose equivalent
capacity data through an optional capability interface; an unknown capacity is reported as unknown, never
as unlimited or healthy.

This keeps the operational contract framework-wide without coupling it to the NATS ObjectStore API.

### 2. Use explicit storage classes rather than a universal GC algorithm

- `windowed/ephemeral`: time-shaped history with finite TTL and byte budget. References MUST declare their
  expiry and MUST NOT be used as required durable current state.
- `entity-owned/current`: the current blob owned by an entity or facet. It has no age expiry. Replacement is
  an ownership transaction and old versions are released after the new reference commits.
- `retained/audit`: content with an explicit product retention or hold policy. It is isolated so that its
  capacity and delete authority cannot be confused with current state.

ObjectStore TTL and `MaxBytes` are bucket-wide, so NATS implementations use separate buckets/instances for
these classes. Other backends must provide equivalent isolation. Mixing incompatible classes in one
physical quota is rejected in production mode.

### 3. Treat hard limits as circuit breakers and soft limits as admission policy

Every managed firehose stream and content store has a finite hard byte limit. SemStreams derives warning,
high, and critical pressure states from configurable thresholds and projected time to full. At high pressure
it throttles or rejects new large writes, new graph identities, and append-shaped growth while preserving
reads, deletes, cleanup, and bounded replacement of existing state.

Authoritative graph KV remains free of TTL and `DiscardOld`. A byte ceiling is legal only when SemStreams
verifies the backing stream uses `DiscardNew`, reports rejection honestly, and reserves enough headroom for
one configured maximum-size replacement plus recovery bookkeeping. It is an outage containment mechanism,
not reclamation.

### 4. Make current-object replacement an explicit ownership protocol

For an `entity-owned/current` object, the owner performs:

1. stream the candidate to the selected store;
2. verify the stored size and digest;
3. compare-and-set the graph reference from the expected old handle to the new handle;
4. asynchronously release the old object after the reference commit.

If step 3 fails, the new object is recorded for orphan repair and the old reference remains valid. A delete
never removes the old object before the reference mutation succeeds. Nested binary objects are either
committed atomically with their parent reference set or recorded in the same repair manifest.

This is deliberately narrower than global reachability GC: v1 collects only objects with declared owners.

### 5. Extend the store contract for bounded streaming writes

`StreamableStore.Open` already permits streaming reads, but `Store.Put` requires a complete `[]byte`.
Introduce a streaming write capability with expected size, content type, digest, and admission metadata.
Components enforce maximum object size and bounded concurrent bytes, not just a goroutine count. Buffering
adapters are permitted only below a configured small-object threshold.

A JetStream-delivered store request is acknowledged only after the object and every required durable
reference are committed. Transient capacity or backend failures are negatively acknowledged with bounded
backoff; permanent policy rejection is terminated and reported through a typed result/dead-letter path.

### 6. Reconcile declared configuration with existing resources

Get-or-create is insufficient. Startup and config reconciliation inspect existing stream, KV, and NATS
ObjectStore backing-stream configuration. Editable drift is repaired; incompatible or unsafe drift blocks
production readiness with an exact migration command. Post-v1 enforcement begins with a versioned report-only
preflight, followed by operator-approved maintenance, startup validation, and finally write admission. No destructive
step or stricter rejection policy runs before its declared backup, restore, migration, and validation proof passes.

### 7. Rebuild derived indexes instead of making them semantic retention authorities

Indexes remain rebuildable projections of `ENTITY_STATES`. Maintenance mode stops index consumers, clears
the selected derived buckets, replays current entity state, and exposes a readiness watermark. Query and
clustering readers fail closed until the generation is complete. This reclaims stale projection debris
without deciding whether an entity is dead.

### 8. Own post-v1 retained-state upgrades with a versioned manifest

After v1, persisted resources are a production contract rather than disposable beta state. Every breaking storage
or enforcement upgrade begins with a versioned report-only manifest/schema declaring:

- source and target SemStreams binary/configuration versions;
- the authoritative retained-resource inventory and expected data shapes;
- backup/export scope plus restore validation;
- ordered migration, rebuild, and enforcement steps;
- readiness and data/query validation gates for each irreversible boundary;
- the last compatible binary/configuration rollback point and the conditions that make rollback unsafe; and
- the owner and removal deadline for any temporary migration-only compatibility mechanism.

The storage doctor renders observed configuration/data-shape drift against that manifest and makes no changes in
report-only mode. An operator approves the exact maintenance plan before mutation. Rollback is allowed only to the
manifest's last proven compatible binary/configuration while its retained resources remain readable there. Once an
irreversible format or deletion step crosses that point, the plan switches to forward recovery instead of pretending
an old binary can safely read new state.

Temporary compatibility may bridge one declared maintenance sequence only. It cannot relax canonical validation,
become a permissive dual contract, or remain past its removal deadline. No release may ship an indefinite legacy
reader or dual writer.

## Risks / Trade-offs

- **Pressure policy can reject legitimate writes** -> ship report-only diagnostics and explicit override
  windows before enforcement, and preserve capacity for replacement/recovery.
- **A hard graph ceiling can cause a write outage** -> use `DiscardNew`, surface typed capacity errors, and
  never describe the ceiling as lifecycle cleanup.
- **Reference swap failures can create orphan candidates** -> persist repair manifests and provide a
  scrubber that reports dangling references and safe-to-release owned objects.
- **Multiple lifetime buckets add configuration** -> provide generated defaults and require products to
  choose a class instead of exposing raw JetStream knobs everywhere.
- **Backend capacity is not uniformly observable** -> represent unsupported measurements as unknown and
  block strict production enforcement only when the selected policy requires them.
- **Maintenance rebuild pauses topology-dependent queries** -> make readiness visible and provide a
  preflight estimate; generation swaps remain a post-v1 optimization.
- **A post-v1 migration can strand retained state** -> require versioned inventory, verified backup/restore,
  ordered readiness/validation gates, and real-NATS upgrade/rollback evidence before destructive enforcement.
- **Temporary compatibility can become permanent cruft** -> scope it to one manifest, assign an owner and removal
  deadline, prohibit permissive dual contracts, and block release while an expired bridge remains.

## Migration Plan

1. Add inventory, metrics, and a versioned doctor/preflight report without changing admission behavior.
2. Classify retained streams, KV, indexes, and store instances; report configuration and data-shape drift, unbounded
   resources, mixed lifetime classes, dangling references, and the source/target version pair.
3. Generate the operator-approved manifest with backup/export scope, restore proof, ordered migration/rebuild steps,
   readiness and validation gates, safe rollback point, and removal deadline for temporary compatibility.
4. Add streaming writes, honest acknowledgement, per-object limits, and bounded in-flight bytes.
5. Create separate store instances/buckets, migrate durable current references without expiring them, verify every
   reference, and remove old objects only after the declared validation gate.
6. Add the maintenance index rebuild and prove the supported real-NATS upgrade plus safe rollback path.
7. Enable startup validation, soft admission, and hard ceilings in stages only after the report-only preflight,
   backup/restore check, migration, readiness, and validation gates are clean.
8. Run relevant semantic/product e2e, remove temporary migration bridges by their deadline, and reject release with
   an indefinite legacy reader or dual writer.

## Open Questions

- Which API owns cross-backend capacity reporting when a `storage.Store` is remote?
- Should retained/audit deletion authority be a framework policy hook or remain product-owned in v1?
- What defaults should production mode generate for warning thresholds and replacement reserve?
- Is the orphan-repair manifest stored per owner in KV or emitted as an idempotent repair work stream?
