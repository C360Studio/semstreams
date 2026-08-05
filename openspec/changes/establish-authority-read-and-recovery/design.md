# Design — authority read and recovery (GS-01)

> **DESIGN REVIEW PASS — OWNER ACCEPTANCE PENDING.** Revision 35 has independent reviewer approval. This change
> remains
> design-only: no capability delta or runtime implementation is authorized until explicit owner acceptance.

## Durable evidence

- [Reviewed recovery contract](reviewed-recovery-contract-r35.md): exact normative r27/r28/r29/r31/r32/r33/r34/r35
  stack,
  hashes, precedence, and reviewer verdict.
- [Reviewed fifth-pass inventory](reviewed-fifth-pass-inventory.md): fresh GS-01 collision inventory and independent
  `INVENTORY PASS` at SHA-256 `3cf290469ba6cb79dd211e89554d15c5788b51ef08564458a7cadb7eefd5317c`.
- [Frozen program inventory](../../../docs/proposals/graph-state-read-write-inventory.md): broader historical graph
  read/write evidence.
- [Suffix inventory addendum](suffix-inventory-addendum.md) and [review disposition](suffix-inventory-review.md):
  current-state suffix evidence only; future ownership remains an owner ruling.
- [Suffix measurement](suffix-measurement.md): current request-time suffix cost and behavior.
- [Native snapshot probe](native-snapshot-probe.md): KV, ObjectStore, and native-link round trips on pinned NATS
  versions
  without the NATS CLI binary.

Revoked revision 13 remains in Git history. Reviewed intermediate artifacts needed by revision 35 are embedded in the
content-addressed contract; omitted exploratory drafts are not normative.

## Program boundary

GS-01 now has one narrow result:

```text
source bytes stable under an offline maintenance fence
→ native whole-stream snapshots plus raw digests
→ physically verified target restore
→ exact reader-only inspection
→ complete_readonly
```

GS-01 does not reactivate a source, consumer, publisher, component, or writer. GS-02 owns semantic admission, restored
guard validation, failed/parked disposition, consumer binding, source release, and local/cross-host write exclusivity.

This relocation requires owner acceptance. It prevents recovery mechanics from silently becoming a new runtime
orchestration framework.

## Foundational choices

### Options and costs

- **Do nothing:** preserves current complexity and leaves authority disaster recovery unsolved; rejected for GS-01.
- **Logical object-by-object recovery:** appears library-local but recreates link interpretation, drift passes, capacity
  prediction, and plan/WAL machinery; rejected after the native-snapshot probe.
- **In-process checkpoint coordination:** can reduce downtime but adds runtime buckets, phases, manager endpoints,
  participant interfaces, and handler-join protocols; rejected as framework sprawl.
- **Offline native single-node checkpoint:** accepts downtime and a narrow initial topology while keeping recovery
  mechanics outside normal runtime; selected for owner ruling.

The design extends existing NATS physical ownership rather than adding another graph-state owner. Unsupported clustered
or dynamically authorized deployments fail closed until a later bounded design proves them.

### Decision-skill outcomes

- **kv-or-stream:** `AUTHORITY_RECOVERY` is owner state with current value, recovery observation, and history, so KV is
  the existing primitive. Source checkpointing adds no source KV because the external maintenance provider owns restart
  authority. No request stream is added.
- **orchestration-check:** checkpoint/restore is one bounded offline operator transaction, not a reactive rule,
  workflow,
  component, or general lifecycle coordinator. The rejected in-process coordinator would have crossed that boundary.
- **query-pattern:** recovery inspection is an operator-only exact NATS management/read path. It is not an adopter query
  API and adds no GraphQL, MCP, or embedded general client surface.

### Native physical recovery

Capture backing streams, not object-by-object logical reconstructions:

- `KV_ENTITY_STATES`.
- `KV_GRAPH_INGEST_APPLIED_SEQ`.
- Transitional `KV_ENTITY_SUFFIX_INDEX`.
- Policy-selected governed `OBJ_*` streams.

Native snapshot/restore preserves server-owned bytes and ObjectStore links without interpreting them. Raw stored-message
digests and normalized physical tuples independently verify the result. No NATS CLI executable is required.

### Exact authority reader

The recovery reader returns validated `graph.EntityState` values and observed KV revisions with typed per-item outcomes:
found, absent, poison, unavailable, canceled, or invalid. Duplicate outputs are independently decoded. It creates no
manager, component, consumer, watch, gateway, index, writer, readiness resource, or graph-status resource.

### Honest consistency boundary

Physical quiescence proves stable bytes, not that every source message was semantically applied. Missing, NAKed,
terminated, panicked, MaxDeliver-exhausted, or parked work remains explicit evidence for GS-02 and never defaults to
success. This preserves SemStreams' pragmatic eventual-consistency model without accepting dirty physical recovery.

### Offline source checkpoint

Initial support is deliberately narrow:

- One NATS 2.14.x server.
- File-backed JetStream, one source account, replicas 1.
- No cluster, route, gateway, leaf node, mirror/source, dynamic auth, token/shared auth, or mTLS-derived identity.
- Every writer and the normal NATS service are controlled by one durable provider maintenance fence.
- The same JetStream store is reopened by one locked, default-deny maintenance NATS process containing only two
  checkpoint NKeys.

The offline tool proves provider/store/config identity and bootstraps collision-free inboxes through provider-local
loopback
`/jsz`, retains exact raw NATS envelopes, snapshots through an owned one-connection native wire adapter, and signs the
final evidence. Clustered and unverifiable deployments fail as unsupported rather than approximating safety.

### Target startup fence

`AUTHORITY_RECOVERY` is separate from `GRAPH_STATUS`. Its presence fail-closes built-in captured-store writers at their
own `Start` boundary and all manager/binary creation/start seams. Normal runtime credentials gain read-only recovery
discovery only; recovery writes stay isolated. `complete_readonly` remains fenced until GS-02 advances admission.

## Complexity result

The rejected in-process source design would have added a source bucket, phase machine, manager endpoint, coordinator,
participant registry, component checkpoint interfaces, handler-join protocol, and snapshot capability protocol.
Revision 35 adds none of those runtime concepts.

The retained source surface is one offline recovery binary, one internal recovery package, one provider adapter seam,
one closed maintenance configuration, and one signed evidence envelope. Normal component authors learn nothing new.

## Reviewer gate

The project reviewer read and verified the exact content-addressed stack through revision 35. Final verdict:

```text
No blocking or high findings.
DESIGN REVIEW PASS
APPROVE
```

The final correction established that:

- FastState omission of `lost` is unknown; only bounded detailed state proves zero loss/deletion completeness.
- Source snapshots exclude consumers and set `jsck:false`, avoiding source mutation.
- Small snapshot archives accept a reply-less headerless terminal; full-chunk archives require exact 204.

## Owner rulings required

1. Accept the GS-01/GS-02 gate relocation and `complete_readonly` boundary.
2. Accept offline single-node source checkpoint as initial support.
3. Approve removal of all proposed in-process source coordination surfaces.
4. Select the first supported maintenance provider adapter.
5. Approve the closed maintenance NATS configuration and checkpoint NKey approval/signing mechanism.
6. Accept the target `RecoveryStartupGate` at built-in writer, manager, and binary seams.
7. Approve the normal-runtime and pre-stop-observer credential migration.
8. Accept `AUTHORITY_RECOVERY` replicas 1 for target-local offline recovery.
9. Select governed ObjectStore capture policy and source provider evidence freshness bounds.
10. Define manual handling for `orphan_restore_unresolved` and explicit post-checkpoint source release.
11. Confirm graph-index owns eventual suffix resolution and transitional suffix storage retires in a later increment.

Only explicit owner acceptance authorizes capability specs, TDD tasks, or runtime changes in this change.
