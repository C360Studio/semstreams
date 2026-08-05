# Historical native JetStream snapshot/restore probe

> **OWNER-REJECTED MECHANISM EVIDENCE.** This probe measured whether server-native backing-stream capture could replace
> the logical KV/ObjectStore reconstruction proposed in GS-01 revision 13. The owner later ruled that SemStreams owns
> no operational checkpoint, backup, restore, attestation, recovery-gate, or recovery-orchestration product. Every
> mechanism, option, responsibility, and "gate" below is retained only to explain why revision 35 was rejected. It is
> not a current GS-01 obligation or candidate implementation.

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

## Historical complexity comparison

This evidence makes the following revision-13 machinery unnecessary for captured KV/ObjectStore bytes:

- per-value/object reconstruction;
- four-pass per-object drift comparison;
- supported-versus-unsupported native-link classification;
- link creation/update/verification subphases;
- logical object metadata round-trip code; and
- a version-bound prediction of server storage overhead for those recreated values.

The rejected design still would have needed a bundle manifest, stream inventory, authorization, isolation, hashes,
provenance, and recovery status. Those were proposed recovery-domain responsibilities; none remains a SemStreams
requirement after the owner correction.

## Historical dependency and API alternatives

Neither alternative below is authorized for GS-01. They are retained as evidence of the dependency and ownership cost
created by the rejected operational-recovery premise.

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
- would have exposed only the rejected design's operation-specific snapshot/restore seam;
- avoids the broad management dependency.

Costs:

- SemStreams would own chunk flow control, acknowledgements, metadata schema, API/domain subject construction,
  cancellation, cleanup, restore completion, and compatibility tests;
- an incomplete wrapper could corrupt or hang recovery even though the server mechanism is correct.

At the time of the probe, the pinned `nats.go/jetstream` package did not expose stream snapshot/restore directly. That
made dependency selection part of the rejected design rather than an implementation detail.

## Historical unanswered questions, not current gates

The successful round trip did not establish the proposed recovery contract. Revision 35 would have needed answers to
the questions below. The owner correction removed that product and these questions from GS-01; operators use their
NATS and infrastructure backup procedures instead.

- **File storage:** `jsm.go` refuses memory-backed stream snapshots. Revision 35 would have had to restrict file-backed
  authority/ObjectStores or define how memory-backed deployments reported an unsupported operation.
- **Multi-stream consistency:** each stream snapshot is internally server-owned, but authority and multiple ObjectStore
  streams are not captured atomically as one unit.
- **Writer quiescence:** ObjectStore delete is a public operation. Authority-first ordering could not prove content
  closure while writers remained live, which drove the rejected design toward an offline/quiesced checkpoint.
- **Crash during snapshot/restore:** the probe covered successful completion only. It did not prove cleanup or restart
  behavior for interrupted chunk transfer.
- **Target isolation:** restore expects the stream name from the archive and refuses an existing same-named stream.
  The rejected recovery command would have needed to prove target absence and define partial-attempt cleanup.
- **Account/domain/ACL topology:** the probe used one local account and the default JetStream API prefix. The rejected
  design would have needed an export/import, domain-prefix, credential, and administrative-permission matrix.
- **Capacity failure:** the server enforces stream/account limits during restore, but the probe did not force quota
  exhaustion. Revision 35 would have needed to define observable cleanup after such failure.
- **Consumers:** the rejected bundle would have needed to exclude unrelated live consumer state. The probe used the
  helper's default no-consumer snapshot behavior.
- **Derived state:** revision 35 proposed a bundle containing authority, semantic progress, and referenced content while
  leaving derived views to their owners. No such SemStreams bundle remains in scope.
- **Archive durability/security:** local file format, encryption, atomic publication, hash verification, retention, and
  operator custody were unresolved concerns created by the rejected bundle.

## Evidence conclusion

The probe established one historical measurement: native backing-stream snapshot/restore preserved the tested KV and
ObjectStore bytes and native links on both NATS versions. It refuted revision 13's logical-reconstruction premise but
did not create a framework requirement. There is no remaining GS-01 wrapper, checkpoint-quiescence, recovery-command,
or bundle decision.
