# Todo Model Ruling

The independently reviewed inventory is
`todo-model-blocker-inventory.md` at SHA-256
`a6f82fd2839f027d3b387469cb611e59f828076aa16c69677e8e5f0176645b0f`.

## Options considered

1. Keep five unkeyed triples per item. Rejected because canonical set deduplication shears records and the reader
   remains order-dependent.
2. Store one rule-opaque record triple per todo. Selected because it gives every item an explicit record boundary while
   preserving one complete reconcile request and existing graph set semantics.
3. Store the entire ordered list in one snapshot triple. Rejected because every edit rewrites one potentially large
   value, removes item-level set behavior, and makes malformed data all-or-nothing by construction.

## Binding decisions

- Use one `agent.todo.record` triple per item. Its object is deterministic JSON containing `id`, `content`, `status`,
  `position`, and `updated_at` with the current logical types and validation.
- Remove all five legacy todo predicates and positional decoding. Do not add aliases, compatibility paths, or deprecated
  constants.
- Mark the record predicate rule-opaque. Remove advertised raw field matching. Add no derived todo counters or status
  observations until a concrete consumer requires them.
- Any malformed record invalidates the complete list. `TodoReader` returns an error and no partial list. Missing loop
  entities and entities with no record triples remain an empty list.
- Keep exported `TodoState` and `TodoReader` with their existing logical fields and signatures. They remain the
  abstraction that shields adopters from storage encoding.
- Remove `triple_count` from tool result metadata. Keep logical `todo_count`; do not rename or redefine the storage-shaped
  count.
- Preserve one contract-bound reconcile request, exact reads, eventual consistency, and component-owned retry policy.
- Add no bucket, stream, service, retry loop, ownership primitive, or child-entity model.
