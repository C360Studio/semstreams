# Graph State Poison Recovery: Stop, Reset, Restart, and Canonical Repopulation

This recovery applies only after graph-index reports a typed `graph_state_reset_required` reason. It is not a stable-
release migration, cutover, or release gate. Stable adoption starts on newly provisioned NATS storage.

## Detect the condition

The readiness envelope — `nats kv get GRAPH_STATUS graph-index` — reports:

```json
{
  "ready": false,
  "state": "reset_required",
  "code": "graph_state_reset_required",
  "reason": "noncanonical_predicate"
}
```

`unreadable_entity_state` is the other bounded reason. Query calls return the same
`graph_state_reset_required` code as a fatal error.

Do not reset graph buckets for an ordinary watch transport failure. DEL and PURGE watch entries are valid entity
tombstones, not unreadable JSON, and projection owners process both as cleanup. A closed or failed watch may keep a
consumer degraded or not-ready while it retries, but it does not produce `graph_state_reset_required`. Use this
runbook only for the typed stored-state poison reasons above.

## Wipe incompatible state

Stop every writer and SemStreams process that uses the target NATS account. Delete the authoritative graph state,
ingest replay guards, and every derived graph bucket used by the deployment:

`CONTEXT_INDEX` is retired by ADR-090. It remains in this destructive list only to remove stale beta state; a fresh
deployment does not create it, and an absent-bucket response is expected.

- `ENTITY_STATES`
- `ENTITY_SUFFIX_INDEX`
- `GRAPH_INGEST_APPLIED_SEQ`
- `OUTGOING_INDEX`
- `INCOMING_INDEX`
- `PREDICATE_INDEX`
- `PREDICATE_CATALOG`
- `NAME_INDEX`
- `CONTEXT_INDEX`
- `ALIAS_INDEX`
- enabled spatial, temporal, embedding, structural, community, and anomaly index buckets

Do not delete unrelated operational or application KV buckets. Bucket names may be overridden by deployment
configuration, so derive the exact destructive set from the rendered configuration and the current
`graph.FrameworkOwnedBuckets()` list before execution.

This scoped poison-recovery procedure does not export, inspect, preserve, translate, or roll back poisoned graph state.
Canonical source systems remain authoritative and must be fixed before reseed.

## Restart and reseed canonical sources

1. Deploy the matching breaking SemStreams and owned producer versions.
2. Start SemStreams so it creates empty graph/index buckets.
3. Reseed from canonical source events or regenerate Graphables from the authoritative source system.
4. Poll `nats kv get GRAPH_STATUS graph-index` until `ready` is true and the indexed revision reaches the target
   revision. The producer republishes the key every 5s, so an operator watching it (`nats kv watch GRAPH_STATUS
   graph-index`) sees each transition as it lands.
5. Run exact and namespace query smoke tests for the renamed predicates before reopening writers.

If reset-required returns, stop and audit the emitting producer. Repeating the reset without fixing the source will
recreate the same poison state.

## Explicit non-features

There is no runtime alias table, permissive flag, dual reader/writer, in-process migration command, beta-state
preservation contract, or rollback path. These are scoped recovery mechanics, not a general release procedure.
