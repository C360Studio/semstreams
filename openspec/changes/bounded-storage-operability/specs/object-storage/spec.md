## ADDED Requirements

### Requirement: Large-content storage remains backend-agnostic

The large-content contract MUST be expressed through `storage.Store`, a streaming extension, the
store registry, and `message.StorageReference`. A reference MUST identify the registered storage
instance and MUST NOT expose backend-specific addressing as a framework requirement. NATS
ObjectStore MUST be supported as one bounded backend, while filesystem, S3-compatible, and future
backends remain valid implementations for content whose scale or access pattern is not suited to
NATS ObjectStore.

#### Scenario: A consumer resolves content without knowing the backend

- **GIVEN** two references backed by different registered `storage.Store` implementations
- **WHEN** a consumer resolves and opens each reference
- **THEN** it selects the store by `StorageInstance` and reads through the common streaming API
- **AND** its graph/query contract contains no NATS bucket or chunk-subject dependency

### Requirement: Stores support bounded streaming writes and reads

The common storage abstraction MUST support writing from an `io.Reader` and reading through an
`io.ReadCloser` without materializing the complete object in memory. Every backend MUST enforce the
actual streamed byte count against a configured per-object maximum, even when the declared size is
absent or false. Byte-slice compatibility writes MUST obey the same limit. Implementations MUST bound
concurrent uploads and aggregate in-flight bytes and MUST apply backpressure instead of creating an
unbounded goroutine or memory queue.

#### Scenario: A large admitted object is never fully buffered

- **GIVEN** an object larger than the process's configured buffer but smaller than the object limit
- **WHEN** it is written and later read through the streaming APIs
- **THEN** both operations progress in bounded chunks
- **AND** neither operation requires a byte slice containing the complete object

#### Scenario: A lying content length cannot bypass admission

- **GIVEN** an upload declares a size below `max_object_bytes` but its reader produces more bytes
- **WHEN** the actual streamed count crosses the limit
- **THEN** the write fails with a permanent size-admission error
- **AND** no durable reference to the incomplete object is published

### Requirement: Store lifetime classes are isolated

Each storage instance MUST declare exactly one lifetime class: `windowed`, `entity-owned`, or
`retained`. `windowed` storage MUST have a finite TTL/expiry and byte capacity. `entity-owned` and
`retained` storage MUST have no age expiry and MUST have finite fail-closed capacity. Different
lifetime classes MUST use separate backend policy domains and, for NATS ObjectStore, separate
buckets. A windowed reference MUST carry its expiry and MUST NOT be persisted as a durable live graph
fact.

#### Scenario: An expiring store cannot back a durable graph reference

- **GIVEN** an object committed to a `windowed` storage instance
- **WHEN** a graph mutation attempts to persist its reference as a durable entity facet
- **THEN** graph-ingest rejects the mutation with an invalid-lifetime error
- **AND** no live entity is left pointing at content that may expire

#### Scenario: Incompatible bucket reuse blocks startup

- **GIVEN** an existing NATS ObjectStore bucket with TTL enabled
- **WHEN** an `entity-owned` or `retained` component is configured to reuse it
- **THEN** startup fails with migration diagnostics
- **AND** the component does not silently accept the expiring bucket

### Requirement: Object commits precede durable reference publication and input acknowledgement

An ObjectStore component consuming JetStream input MUST positively acknowledge only after the object
commit has succeeded, committed size/digest have been verified, and every required durable reference
output has received a persistence acknowledgement. A transient storage or publication failure MUST
NAK for redelivery. A permanent admission rejection MUST produce a durable failure outcome and
terminate delivery rather than poison-loop. A required reference output MUST NOT use Core NATS in
production.

#### Scenario: Failed storage is redelivered

- **GIVEN** a JetStream input and a transient backend write failure
- **WHEN** the ObjectStore handler processes the message
- **THEN** it does not positively acknowledge the input
- **AND** it NAKs the message for redelivery without publishing a reference

#### Scenario: Reference publication failure does not lose the input

- **GIVEN** an object commit succeeds but the required StoredMessage PubAck fails
- **WHEN** the write handler completes
- **THEN** the input is NAKed for retry
- **AND** retry addresses the same deterministic or content-addressed object key

### Requirement: Entity-owned reference replacement cannot create a durable dangling reference

An `entity-owned` replacement MUST stream and verify the new object before graph-ingest CASes the
owner/facet from the old reference to the new reference. The old exact-owned object MUST remain
readable until that CAS succeeds. After success, release of the exact old object MAY be asynchronous;
after CAS loss, the unreferenced candidate MUST be recorded for exact release. Shared or `retained`
content MUST NOT use this release path. Multi-object content MUST commit all children and a verified
manifest before advertising the manifest reference.

#### Scenario: Failed candidate upload preserves the old reference

- **GIVEN** an entity facet references object A
- **WHEN** streaming replacement object B fails integrity verification
- **THEN** the entity continues to reference readable object A
- **AND** no reference to B is persisted or published

#### Scenario: CAS loss does not delete the winner

- **GIVEN** two concurrent replacements for one entity-owned facet
- **WHEN** one reference CAS wins and the other loses
- **THEN** only the losing candidate is recorded for release
- **AND** the winner and the prior object remain governed by the successful swap sequence

### Requirement: Object capacity and integrity are observable without automatic sweeping

SemStreams MUST report authoritative object count, bytes, limits, rejected writes, in-flight uploads,
growth rate, lifetime class, and configuration drift for every registered store. A bounded integrity
scrubber MUST report missing durable targets, expired references, owner mismatches, and recorded
unreferenced candidates with coverage/freshness. This change MUST NOT automatically delete
scrubber-reported objects or perform graph-wide ObjectStore reachability GC.

#### Scenario: A dangling reference is reported but not erased

- **GIVEN** a durable graph reference whose object is absent
- **WHEN** the integrity scrubber visits the reference
- **THEN** storage status reports the missing target and repair context
- **AND** the scrubber neither deletes the entity nor guesses a replacement
