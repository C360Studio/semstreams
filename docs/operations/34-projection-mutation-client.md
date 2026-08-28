# Operating the Projection Mutation Client

`pkg/projection` is a small, concurrency-safe client that validates a component's local graph shape before using the
canonical graph request/reply interface. It has no registry, owner, lease, heartbeat, token, or compatibility mode.

## Declare a local contract

Use named groups so every mutation says whether it reconciles complete current state or appends exact evidence:

```go
widgetContract := projection.Contract{
    Name:          "example.widget.v1",
    MessageType:   "example.widget.v1",
    EntityPattern: "acme.ops.example.system.widget.*",
    BirthPredicates: []string{
        "example.widget.name",
    },
    Groups: []projection.PredicateGroup{
        {
            Name:       "runtime",
            Mode:       projection.ModeReconcile,
            Predicates: []string{"example.widget.status", "example.widget.mode"},
        },
        {
            Name:       "observations",
            Mode:       projection.ModeAppend,
            Predicates: []string{"example.widget.observation"},
        },
    },
}
```

All predicates must be declared canonical predicates. Group names are case-sensitive subject-safe tokens. One
predicate may appear only once within a contract. Birth predicates are create-time validation, not a graph-enforced
write-once rule.

Construct the client at the composition root after NATS is available:

```go
mutations, err := projection.NewMutationClient(projection.MutationClientConfig{
    NATS:      natsClient,
    Contracts: []projection.Contract{widgetContract},
    Timeout:   2 * time.Second,
})
if err != nil {
    return fmt.Errorf("configure widget graph mutations: %w", err)
}
```

Give components the narrow interface they need: `EntityCreator`, `PredicateReconciler`, `TripleAppender`,
`EntityDeleter`, or `AuthoritativeReader`. Do not pass raw NATS subjects or graph KV handles.

### Framework-owned built-in writers

Local contracts remain the generic product rule. When SemStreams exposes an owned
purpose-scoped contract snapshot for a first-party built-in writer, include that
snapshot in the composition-root client instead of copying the framework's own
declaration. For example, `agentictools.LessonProjectionContract()`
(`processor/agentic-tools/lesson_promotion.go:53`) returns an independent
canonical lesson contract snapshot, delegating to `agentic.LessonContract()`
(`agentic/agent_lesson_entity.go:396`). The framework declaration lives on the
payload registration (`agentic/payload_registry.go:53`), not in a projection
package — PR #1109 deleted `internal/builtinprojection`.

The snapshot removes a private predicate-set prediction while preserving one local
client and narrow capability injection. It does not make local contracts globally
authoritative or change graph-ingest enforcement.

## Create

`Create` births one absent entity. `CreateMutation.Triples` is the sole initial-fact source; `Entity.Triples` must be
empty. Every triple must use the new entity ID as its subject and a predicate declared by the contract.

```go
receipt, err := creator.Create(ctx, projection.CreateMutation{
    Contract: "example.widget.v1",
    Entity: &graph.EntityState{
        ID:          "acme.ops.example.system.widget.001",
        MessageType: message.Type{Domain: "example", Category: "widget", Version: "v1"},
    },
    Triples: []message.Triple{
        {
            Subject:   "acme.ops.example.system.widget.001",
            Predicate: "example.widget.name",
            Object:    "Widget 001",
        },
    },
    Metadata: projection.MutationMetadata{
        RequestID: requestID,
        Source:    "widget-projector",
        Timestamp: time.Now().UTC(),
    },
})
```

An existing entity returns a classified conflict. The framework does not read it back and convert that conflict into
success; the component decides whether an existing entity is acceptable.

## Reconcile

`Reconcile` exact-reads the entity once, then replaces the complete selected group using that same-entry KV revision.
Desired triples may contain only predicates in the named `reconcile` group.

```go
receipt, err := reconciler.Reconcile(ctx, projection.ReconcileMutation{
    Contract: "example.widget.v1",
    Group:    "runtime",
    EntityID: "acme.ops.example.system.widget.001",
    Desired: []message.Triple{
        {
            Subject:   "acme.ops.example.system.widget.001",
            Predicate: "example.widget.status",
            Object:    "ready",
        },
    },
    Metadata: metadata,
})
```

Omitting `example.widget.mode` removes it because it belongs to `runtime`; unrelated predicates remain unchanged. The
client does not retry revision mismatch. If product semantics allow a retry, the component exact-reads, recomputes the
complete desired group, and calls again with stable logical-request provenance.

Reconcile treats each triple's persisted annotations as desired state too: `Timestamp`, `Confidence`, and `ExpiresAt`
participate in equality alongside the append-tuple fields. To receive `unchanged`, preserve those annotations from the
exact read for facts whose desired state has not changed. Do not regenerate a timestamp (for example with `time.Now()`)
unless advancing that annotation is the intended mutation; a new timestamp correctly commits a new authority revision
and notifies watchers.

## Append

`Append` adds exact canonical tuples to one existing entity through an `append` group. Duplicate tuples are an
`unchanged` success and do not advance the KV revision.

```go
receipt, err := appender.Append(ctx, projection.AppendMutation{
    Contract: "example.widget.v1",
    Group:    "observations",
    EntityID: "acme.ops.example.system.widget.001",
    Triples:  observations,
    Metadata: metadata,
})
```

The lower-level append wire operation supports several subjects and returns one explicit result per subject. There is
no cross-subject transaction. Retry only subjects selected by component policy; never resend applied subjects merely
because another subject failed.

## Delete and exact read

Delete requires a revision obtained from an exact read:

```go
exact, err := reader.ReadAuthoritative(ctx, entityID)
if err != nil {
    return err
}

receipt, err := deleter.Delete(ctx, projection.DeleteMutation{
    EntityID:         entityID,
    ExpectedRevision: exact.KVRevision,
    Metadata:         metadata,
})
```

The delete receipt reports the matched expected revision; it does not invent a delete-marker revision.

## Handle classified outcomes

Errors returned by the client preserve the operation, kind, server code/class, and commit state in
`projection.MutationError`.

- Invalid input, not-found, entity-exists, and revision mismatch are definite non-commits.
- No responder is `unavailable`, a definite non-commit.
- A context already done before send is `deadline`, also a definite non-commit.
- A post-send timeout or disconnect, malformed reply, or semantically invalid success reply is
  `MutationCommitUnknown` with `CommitUnknown`.
- Successful create, reconcile, append, and delete receipts use `CommitVerified`.

Never automatically retry `CommitUnknown`. A later matching read proves only current state, not that the ambiguous
request authored it.

## Missing relationships and operations

The mutation client permits a valid relationship whose object entity is absent. The source edge commits normally;
later dereference reports the missing target. There is no stub or repair queue.

No recovery service accompanies this client. Operators maintain NATS backups using deployment-appropriate practices;
components handle their own request retry and convergence policy.

See [Graph Mutation Contracts](../concepts/28-governed-semantic-state.md).
