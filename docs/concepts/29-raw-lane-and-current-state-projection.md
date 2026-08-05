# Raw Facts and Current-State Projection

High-rate sources often need two different representations:

1. immutable or retained raw observations for analysis; and
2. a compact graph entity representing the latest useful state.

These representations have different retention and mutation semantics. A raw observation store is not the graph
authority, and `ENTITY_STATES` is not a general event archive.

## Keep raw history where it belongs

Use the storage primitive that matches the source and replay requirement. JetStream streams preserve ordered work;
KV buckets expose current state plus watch/history behavior; ObjectStore holds large content. Retention, limits, and
rebuild policy belong to the component that owns that physical store.

Do not encode a raw feed as unbounded triples on one graph entity. That makes entity size and query cost grow with
traffic and obscures which facts are current.

## Project current state explicitly

A component may project selected raw facts into `ENTITY_STATES` through the canonical graph mutation port:

- use `entity.create` to birth a missing current-state entity;
- use `entity.reconcile` to replace a complete selected-predicate set after an exact read;
- use `triple.append` only for evidence whose exact tuples should accumulate; and
- use `entity.delete` only with an observed revision.

`reconcile` is usually the correct choice for status, position, health, or other bounded current state. Omitting a
predicate from the desired set removes that predicate only when it belongs to the selected reconcile group. Predicates
outside the group are untouched.

## Handle conflicts at the source boundary

The projection client performs one request per call. A no-responder `unavailable` result or a context `deadline`
observed before send is a definite non-commit. If reconcile returns a definite revision mismatch, the component can
read the new state, recompute, and retry according to product semantics. It must not automatically retry
`commit_unknown`: a post-send timeout or disconnect, malformed reply, or invalid success reply cannot prove that the
first request did not commit.

There are no owner leases or priority writers. When several components are allowed to update one current-state
entity, their domain policy must define how they converge. If the policy is unclear, use separate entities or separate
predicates so each projection remains understandable.

## Preserve eventual relationships

Current state may reference an entity that has not arrived yet. Keep the relationship and surface the missing target
at read time. Do not manufacture a placeholder target. A later real birth makes subsequent dereference succeed.

## Observe outcomes

Use the bounded graph mutation outcome metric by operation and outcome. Repeated revision mismatch is evidence of
writer contention; `entity_not_found` is evidence that the component attempted a must-exist operation before birth;
`commit_unknown` is a post-send result ambiguity the component must resolve. None requires global graph shutdown.

See [Graph Mutation Contracts](28-governed-semantic-state.md) for the full foundation.
