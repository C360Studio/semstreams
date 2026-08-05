# GS-01 native JetStream snapshot/restore probe

> **DESIGN EVIDENCE ONLY.** This probe evaluates whether server-native backing-stream capture can replace the logical
> KV/ObjectStore reconstruction in GS-01 revision 13. It does not accept a dependency, API, recovery contract, or
> target design.

## Baseline and question

- Repository baseline: `d322708a8ec360658d513a077fa99c9fe1ef5a81`
- Date: 2026-08-04
- Existing client: `github.com/nats-io/nats.go v1.52.0`
- Probe helper: `github.com/nats-io/jsm.go v0.4.1`
- External probe: `/private/tmp/gs01-snapshot-probe/main.go`
- Probe SHA-256: `2e7a414e7473434539b6755a2d03e7d670d14753c7206397749ba89d0d84b261`
- Probe size: 187 lines, 5,601 bytes

Revision 13 captures every authority value and ObjectStore object logically. That choice creates multi-pass drift
detection, metadata interpretation, a native-link state machine, server-overhead capacity prediction, and a large
restore protocol. The evaluated alternative snapshots the physical backing streams that already contain those bytes:

- `KV_ENTITY_STATES` for the authority KV bucket;
- `OBJ_<bucket>` for each referenced framework ObjectStore.

The JetStream administrative API exposes `$JS.API.STREAM.SNAPSHOT.<stream>` and
`$JS.API.STREAM.RESTORE.<stream>`. `jsm.go` supplies programmatic buffer/directory helpers over that API. No NATS CLI
was installed or invoked.

## Probe shape

For each server version, the probe:

1. Started the repository's isolated NATS test client with file-backed JetStream.
2. Created H1 `ENTITY_STATES` and wrote two six-part entity keys.
3. Created ObjectStore `CONTENT` and wrote one concrete object.
4. Added native ObjectStore link `document-latest -> CONTENT/document-001`.
5. Snapshotted physical streams `KV_ENTITY_STATES` and `OBJ_CONTENT` to in-memory data and metadata buffers.
6. Deleted the KV bucket and ObjectStore through the normal JetStream API.
7. Restored both physical streams from the snapshot buffers.
8. Reopened the normal KV/ObjectStore abstractions.
9. Verified the exact authority value, KV revision, concrete-object digest, dereferenced link bytes, and native link
   target metadata.

## Results

Both tested servers passed.

### NATS 2.14-alpine

```text
snapshot stream=KV_ENTITY_STATES bytes=1053 expected=242 chunks=1
snapshot stream=OBJ_CONTENT bytes=1419 expected=687 chunks=1
restore stream=KV_ENTITY_STATES messages=2 bytes=242
restore stream=OBJ_CONTENT messages=3 bytes=687
verify kv_revision=2 object_digest=SHA-256=QIO_4iGdtmPJ8OgdOciBArvxa6fFUGOk7f65yhtwZnA=
verify link_bucket=CONTENT link_name=document-001
```

### NATS 2.12.4-alpine

```text
snapshot stream=KV_ENTITY_STATES bytes=1053 expected=242 chunks=2
snapshot stream=OBJ_CONTENT bytes=1407 expected=687 chunks=2
restore stream=KV_ENTITY_STATES messages=2 bytes=242
restore stream=OBJ_CONTENT messages=3 bytes=687
verify kv_revision=2 object_digest=SHA-256=QIO_4iGdtmPJ8OgdOciBArvxa6fFUGOk7f65yhtwZnA=
verify link_bucket=CONTENT link_name=document-001
```

The physical snapshot preserved the framework-visible KV and ObjectStore contracts without decoding an entity,
object envelope, binary child, or link representation. The server enforced stream restore and recreated the standard
backing streams consumed by the pinned NATS Go client.

## Complexity removed if the mechanism is accepted

This evidence makes the following revision-13 machinery unnecessary for captured KV/ObjectStore bytes:

- per-value/object reconstruction;
- four-pass per-object drift comparison;
- supported-versus-unsupported native-link classification;
- link creation/update/verification subphases;
- logical object metadata round-trip code; and
- a version-bound prediction of server storage overhead for those recreated values.

It does not eliminate a bundle manifest, stream inventory, authorization, isolation, hashes, provenance, or recovery
status. Those remain recovery-domain responsibilities.

## Dependency and API options

### Import `jsm.go`

Advantages:

- maintained NATS-owned implementation of snapshot chunk acknowledgement, compressed archive metadata, domain-aware
  administrative subjects, progress, and restore completion;
- public buffer and directory operations already exercised by the probe.

Costs:

- the module is pre-v1;
- importing its root package brings a broad management dependency graph, including NATS server/API packages and
  facilities unrelated to SemStreams recovery;
- SemStreams would need a compatibility pin and a narrow adapter to prevent that API from spreading.

### Implement the narrow raw administrative protocol

Advantages:

- uses the existing pinned `nats.go` core connection;
- exposes only SemStreams' operation-specific snapshot/restore seam;
- avoids the broad management dependency.

Costs:

- SemStreams would own chunk flow control, acknowledgements, metadata schema, API/domain subject construction,
  cancellation, cleanup, restore completion, and compatibility tests;
- an incomplete wrapper could corrupt or hang recovery even though the server mechanism is correct.

The pinned `nats.go/jetstream` package does not expose stream snapshot/restore directly. Dependency selection therefore
remains an explicit design decision, not an implementation detail.

## Remaining gates

The successful round trip does not establish the complete recovery contract. A revised design must still answer:

- **File storage:** `jsm.go` refuses memory-backed stream snapshots. Recovery must state whether file-backed authority
  and referenced ObjectStores are required, or how memory-backed deployments report unsupported recovery.
- **Multi-stream consistency:** each stream snapshot is internally server-owned, but authority and multiple ObjectStore
  streams are not captured atomically as one unit.
- **Writer quiescence:** ObjectStore delete is a public operation. Authority-first ordering alone cannot prove content
  closure while writers remain live. An offline/quiesced checkpoint is the simplest honest contract unless a smaller
  proven coordination rule exists.
- **Crash during snapshot/restore:** the probe covered successful completion only. It did not prove cleanup or restart
  behavior for interrupted chunk transfer.
- **Target isolation:** restore expects the stream name from the archive and refuses an existing same-named stream.
  The recovery command must prove target absence and make partial-attempt cleanup explicit.
- **Account/domain/ACL topology:** the probe used one local account and the default JetStream API prefix. Export/import,
  domain prefixes, credentials, and administrative subject permissions need an explicit matrix.
- **Capacity failure:** the server enforces stream/account limits during restore, but the probe did not force quota
  exhaustion. Post-claim failure must remain fenced, observable, and resumable or wipeable.
- **Consumers:** KV/ObjectStore recovery should not restore unrelated live consumer state. The probe used the helper's
  default no-consumer snapshot behavior.
- **Derived state:** only authority, progress required for authority semantics, and referenced content belong in the
  recovery bundle. Rebuildable derived view streams remain owner-rebuilt rather than restored by authority recovery.
- **Archive durability/security:** local file format, encryption, atomic publication, hash verification, retention, and
  operator custody remain bundle concerns.

## Evidence conclusion

Native backing-stream snapshot/restore is feasible for the two physical substrates GS-01 needs and preserves native
ObjectStore links without interpretation on both NATS versions tested. Revision 13's logical reconstruction premise is
therefore false. The remaining decision is how narrowly to wrap the server API and how much checkpoint quiescence the
pragmatic recovery contract requires.
