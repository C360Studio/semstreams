# Predicate Cutover: Export, Reset, and Reingest

The canonical predicate release is a beta breaking change. It does not translate old predicate identities or
mixed-format graph indexes in place.

## Detect the condition

`graph.index.query.status` reports:

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

## Preserve data if required

Before deleting buckets, stop writers and export any beta ENTITY_STATES values or upstream source data required by
your retention policy. The export is for operator recovery and audit; do not feed unchanged incompatible entity
JSON back into the canonical deployment.

## Reset incompatible state

With all SemStreams components stopped, delete the authoritative and derived graph buckets used by the deployment:

- `ENTITY_STATES`
- `OUTGOING_INDEX`
- `INCOMING_INDEX`
- `PREDICATE_INDEX`
- `PREDICATE_CATALOG`
- `NAME_INDEX`
- `CONTEXT_INDEX`
- `ALIAS_INDEX`
- enabled spatial, temporal, embedding, structural, community, and anomaly index buckets

Do not delete unrelated operational or application KV buckets. Use the deployment's normal NATS administration
and backup procedure; bucket names may be overridden by deployment configuration.

## Reingest canonical sources

1. Deploy the matching breaking SemStreams and owned producer versions.
2. Start SemStreams so it creates empty graph/index buckets.
3. Reingest from canonical source events or regenerate Graphables from the authoritative source system.
4. Poll `graph.index.query.status` until `ready` is true and the indexed revision reaches the target revision.
5. Run exact and namespace query smoke tests for the renamed predicates before reopening writers.

If reset-required returns, stop and audit the emitting producer. Repeating the reset without fixing the source will
recreate the same poison state.

## Explicit non-features

There is no runtime alias table, permissive flag, dual reader/writer, or in-process migration command. This is the
intentional clean beta boundary before v1.
