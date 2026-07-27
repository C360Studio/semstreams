# Operating the Projection Mutation Client

`pkg/projection` provides the contract-bound write side of
[ADR-056](../adr/056-authoritative-semantic-state.md). Use it when a Go component owns a declared predicate group
and must create an entity, reconcile owned current state, append evidence, or verify authoritative state.

The client is not a general graph administration API. It derives authority from `projection.Contract`, keeps the
owner token private, and uses graph-ingest for mutations and authoritative read-back.

## Mandatory graph-ingest lease gate

Do not enable an owner-bound mutation client unless **every** graph-ingest instance serving its mutation subjects
sets `enforce_owner_lease=true`. A mixed fleet is unsafe because a request routed to a non-enforcing instance
bypasses the owner-token fence.

Before rollout, record evidence that:

1. every serving instance enables enforcement and has its ownership claim reader wired;
2. the owner heartbeat is live before the writer starts; and
3. owner-lease mismatch metrics remain zero throughout a bounded observation window.

Treat missing evidence as a fail-closed deployment blocker. Semdragon
[#313](https://github.com/C360Studio/semstreams/issues/313) must not adopt the client until this gate passes for its
complete serving fleet.

## Bind at the composition root

Create the ownership registry and one process-lifetime heartbeater before starting components. Aggregate every
static built-in contract for each stable owner, then bind that complete owner set once after NATS and ownership
storage are ready but before the owner can write:

The illustrative predicates below are assumed to be registered canonical predicates.

```go
widgetContracts := []projection.Contract{{
    Name:          "example.widget.v1",
    MessageType:   "example.widget.v1",
    EntityPattern: "acme.ops.example.system.widget.*",
    BirthPredicates: []string{
        "example.widget.name",
    },
    Groups: []projection.PredicateGroup{
        {
            Name: "runtime",
            Mode: ownership.ModeReplaceOwned,
            Predicates: []string{
                "example.widget.status",
                "example.widget.mode",
            },
        },
        {
            Name: "position",
            Mode: ownership.ModeReplaceOwned,
            Predicates: []string{
                "example.widget.latitude",
                "example.widget.longitude",
            },
        },
        {
            Name:       "observations",
            Mode:       ownership.ModeAppendEvidence,
            Predicates: []string{"example.widget.observation"},
        },
    },
}}

registry, err := ownership.EnsureBuckets(appCtx, nats, logger, nil)
if err != nil {
    return fmt.Errorf("ensure ownership storage: %w", err)
}

heartbeater := registry.NewHeartbeater(0)
go heartbeater.Run(appCtx)

mutations, err := projection.BindMutationClient(appCtx, projection.MutationClientConfig{
    NATS:        nats,
    Registry:    registry,
    Heartbeater: heartbeater,
    Owner:       "widget-projector",
    Contracts:   widgetContracts,
    Timeout:     2 * time.Second,
    Retry:       natsclient.DefaultRetryConfig(),
})
if err != nil {
    return fmt.Errorf("bind widget mutations: %w", err)
}
```

`appCtx` must be cancelled during process shutdown. The caller owns the heartbeater lifecycle; the mutation client
does not start, stop, or replace it. Reuse the composition-root heartbeater for all static owners in that process.

Owner registration is a Registry-wide invariant that applies to every binding entry point. The first successful
registration consumes the owner identity for that Registry lifetime. Direct `Registry.RegisterOwner`,
`projection.Bind`, `projection.BindAndHeartbeat`, and `BindMutationClient` all reject a concurrent or later
same-owner attempt with `ownership.ErrOwnerAlreadyBound` before owner-presence heartbeat, ownership-claim mutation,
or heartbeater enrollment. This includes an identical second registration; matching contracts do not make it
idempotent.

Collect the complete contract set intended for a registered owner before the first registration. In particular,
aggregate all static built-in contracts for one owner and pass them to one `BindAndHeartbeat` or
`BindMutationClient` call. Do not bind partial contract sets per component or add contracts incrementally. A failed
first registration releases the in-progress identity guard, so its configuration can be corrected and retried.
After a successful registration, contract correction or owner revival requires a newly constructed Registry and
incarnation.

An owning contract is any supplied contract with a `replace-owned` or `cas-transition` group. Such a collection
requires a non-nil heartbeater. A collection containing only `append-evidence` groups may bind with a nil
heartbeater. Without `BirthPredicates`, that client is limited to append and authoritative read-back: create and
replace fail validation before transport.

`PredicateGroup.Name` gives a replacement group its stable selector. A non-empty name is case-sensitive and must be
one NATS subject token without `.`, whitespace, `*`, or `>`. Names must be unique within the contract. Names are
optional only for backward compatibility; new contracts with more than one replacement group should name every
group.

`Contract.BirthPredicates` declares primary-subject facts that this client accepts only during entity creation.
Each must be a registered canonical exact predicate, cannot be duplicated, and cannot also appear in any mutable or
append group. Birth predicates derive no ownership or foreign-edge claim, never enter a replacement removal set,
and do not authorize append.

A contract containing only `BirthPredicates` is valid. It binds without a heartbeater, skips ownership
registration, retains a zero owner token, and can perform tokenless creation. Birth predicates never create a
heartbeat or token requirement. A contract that also declares an owning group still requires a heartbeater, and a
create request containing a predicate from that owning group carries the bound token.

Because a birth-only contract derives neither an ownership claim nor a foreign-edge claim, its no-claim client does
not call `RegisterOwner` and does not consume the owner registration identity in a supplied Registry.

Create-only does not mean graph-enforced immutable. Graph-ingest does not lease or protect birth predicates after
creation, and a nonconforming writer using another accepted mutation lane can change or remove them. A product that
requires write-once facts needs a separately enforced ownership or storage contract.

Do not pass the concrete client throughout the application when a component needs less authority. Depend on the
narrow interface that matches the component's role:

```go
type writerDependencies struct {
    creator  projection.EntityCreator
    replacer projection.OwnedReplacer
    appender projection.EvidenceAppender
    reader   projection.AuthoritativeReader
}

deps := writerDependencies{
    creator:  mutations,
    replacer: mutations,
    appender: mutations,
    reader:   mutations,
}
```

An append-only component should receive only `projection.EvidenceAppender`; a verifier should receive only
`projection.AuthoritativeReader`.

## Create an entity with primary-subject facts

`CreateWithTriples` atomically creates one entity and its declared initial facts. Every triple must use the new
entity ID as its subject. Outbound relationships are valid when the relationship predicate is declared by the
contract and the new entity remains the triple subject.

```go
timestamp := time.Now().UTC()
receipt, err := creator.CreateWithTriples(ctx, projection.CreateMutation{
    Contract: "example.widget.v1",
    Entity: &graph.EntityState{
        ID: "acme.ops.example.system.widget.001",
        MessageType: message.Type{
            Domain: "example", Category: "widget", Version: "v1",
        },
    },
    Triples: []message.Triple{
        {
            Subject:   "acme.ops.example.system.widget.001",
            Predicate: "example.widget.name",
            Object:    "Widget 001",
        },
        {
            Subject:   "acme.ops.example.system.widget.001",
            Predicate: "example.widget.status",
            Object:    "ready",
        },
    },
    Metadata: projection.MutationMetadata{
        RequestID: requestID,
        TraceID:   traceID,
        Source:    "widget-projector",
        Timestamp: timestamp,
    },
})
```

In this example, `example.widget.name` is accepted only during creation by this client because it is in
`BirthPredicates`; the graph does not enforce it as immutable.
`example.widget.status` is an owning birth fact because it is in the named `runtime` replacement group. The contract
therefore requires the composition-root heartbeater, and this create request carries the bound owner token.

Create predicates must be declared in `BirthPredicates`, a `replace-owned` group, or a `cas-transition` group.
An append-only contract with no birth predicates cannot create an entity. An append-only contract that also
declares birth predicates may create only those initial facts, without an owner token.

`CreateMutation.Triples` is the only birth-fact source. `Entity.Triples` must be empty. The client rejects a
populated `Entity.Triples` without sending a mutation or read request, does not merge the two fields, and does not
modify caller input.

Cross-subject triples are also rejected before transport, even if they match a declared `ForeignEdgeClaim`.
Foreign-subject edges remain on the existing reconciliation path because the create handler cannot promise atomic
creation and verification across subjects.

Create and append require a non-empty request ID and source. Build metadata once per logical mutation. The client
copies triples and fills only missing source, timestamp, and request-ID context fields; conflicting values are
invalid.

## Reconcile the complete owned group

`ReplaceOwned` is full-set reconciliation for exactly one selected predicate group, not a contract-wide replace and
not a patch. Set `ReplaceOwnedMutation.Group` to the exact name of one `replace-owned` group. Desired triples may
contain only predicates from that selected group:

```go
receipt, err := replacer.ReplaceOwned(ctx, projection.ReplaceOwnedMutation{
    Contract: "example.widget.v1",
    Group:    "runtime",
    EntityID: "acme.ops.example.system.widget.001",
    Desired: []message.Triple{{
        Subject:   "acme.ops.example.system.widget.001",
        Predicate: "example.widget.status",
        Object:    "ready",
    }},
    Metadata: projection.MutationMetadata{
        RequestID: requestID,
        TraceID:   traceID,
        Source:    "widget-projector",
        Timestamp: timestamp,
    },
})
```

The selected `runtime` group also declares `example.widget.mode`, so omitting mode from `Desired` deletes the stored
mode. The sibling `position` group is preserved, as are `BirthPredicates`, foreign predicates, and append-only
predicates. Authoritative verification also examines only the selected group.

An empty `Group` selector is accepted only when the contract has exactly one `replace-owned` group. That sole group
may be named or unnamed. If the contract has no replacement group or more than one, omission is invalid. An unnamed
group cannot be selected by name, and naming an append or CAS group is invalid.

Selected-group delete-on-omit is why callers must construct the complete desired state for that group. Never call
`ReplaceOwned` with one changed field unless every other predicate in the selected group should be removed.

The client may retry replacement with the same owner token and schema-derived removal set because replacement is
idempotent at the selected predicate-group boundary. Callers cannot supply an arbitrary removal list or lifecycle
expected revision; lifecycle CAS remains owned by `pkg/lifecycle`.

## Append evidence with duplicate resistance

`AppendEvidence` accepts only triples in the named contract's `append-evidence` groups, all for one existing entity:

```go
receipt, err := appender.AppendEvidence(ctx, projection.AppendEvidenceMutation{
    Contract: "example.widget.v1",
    EntityID: "acme.ops.example.system.widget.001",
    Evidence: []message.Triple{{
        Subject:   "acme.ops.example.system.widget.001",
        Predicate: "example.widget.observation",
        Object:    "calibration passed",
    }},
    Metadata: projection.MutationMetadata{
        RequestID: requestID,
        TraceID:   traceID,
        Source:    "widget-calibrator",
        Timestamp: timestamp,
    },
})
```

Append is not blindly idempotent. The client makes a classified single attempt. After an ambiguous result, it reads
authoritative state and searches for the exact tuple:

```text
(subject, predicate, object, datatype, source, context=request-id)
```

If the tuple exists, the client returns verified success without issuing another append. If authoritative state
does not contain it, the client may retry within its configured budget using identical provenance. That read does
not prove absence: the original timed-out request can commit after read-back, followed by the retry, and append the
same evidence twice. The client is duplicate-resistant, not exactly-once.

Never wrap append or another mutation call in a generic outer retry loop. Deployments requiring strict no-retry
behavior must set `Retry.MaxRetries=0` until graph-ingest provides the server-side idempotency primitive tracked by
[#697](https://github.com/C360Studio/semstreams/issues/697).

`no responders` is different from a timeout: it proves that no serving handler accepted that attempt, so the client
may retry it within its configured budget. A timeout or lost response is ambiguous and must not be reclassified as
no responders.

## Handle commit state before error kind

Every mutation returns a `projection.MutationReceipt`, including commit-aware error paths:

- `CommitNotCommitted`: the client can prove the mutation did not commit. Inspect the typed error and correct the
  cause; do not apply a generic retry policy.
- `CommitUnknown`: transport was ambiguous and authoritative verification was unavailable. Do not retry. Reconcile
  through authoritative state or escalate for operator recovery.
- `CommitCommitted`: the mutation committed, but may not be authoritatively verified. Do not retry. Reconcile or
  alert if verification matters.
- `CommitVerified`: authoritative state matches the operation's verification contract. Continue.

Always inspect `*projection.MutationError` with `errors.As`; do not parse its text:

```go
if err != nil {
    var mutationErr *projection.MutationError
    if !errors.As(err, &mutationErr) {
        return err
    }

    switch mutationErr.Kind {
    case projection.MutationStaleOwnerToken:
        // Stop this owner incarnation and return control to the supervisor.
        return err
    case projection.MutationCommitUnknown,
        projection.MutationCommittedUnverified:
        // Never retry this mutation. Reconcile or escalate.
        return err
    default:
        return err
    }
}
```

`MutationError` unwraps its cause. Existing inspections remain valid when callers need lower-level detail:

```go
var classified *errs.ClassifiedError
if errors.As(err, &classified) {
    // classified.Code and classified.Class retain the graph/NATS classification.
}

if errors.Is(err, errs.ErrRevisionMismatch) {
    // Existing sentinel handling remains available.
}
```

A response with `Degraded == true` means the handler committed the mutation but its own read-back failed. The client
never retries that response. It performs authoritative read-back itself: successful verification returns
`CommitVerified`; failed verification returns `CommitCommitted` with
`projection.MutationCommittedUnverified`. In both cases, callers must not retry.

Context cancellation or deadline stops further retry and read-back. For create and append, a timeout is ambiguous;
it does not prove the server failed to commit.

## Recover from a stale owner token

`projection.MutationStaleOwnerToken` with `CommitNotCommitted` is terminal for that client instance. The client is
immutable and intentionally does not refresh its token or call `BindMutationClient` internally.

Recovery belongs to the composition root:

1. Quiesce the affected writer and stop accepting new work.
2. Cancel and drain the old owner incarnation.
3. Discard the old Registry; neither resigning nor changing contracts permits another successful registration
   against it.
4. Construct a replacement Registry, which mints a new incarnation, and bind the owner's complete contract set
   before starting the writer.

Do not rebind inside a request handler or register the same owner a second time against one Registry. The global
guard rejects that attempt with `ErrOwnerAlreadyBound`; creating a new Registry makes the incarnation change
explicit at the composition root.

## Semdragon replacement path

Semdragon adoption remains gated by issue #313 and the mandatory lease-enforcement evidence above. After that gate,
it adopts this client without adding another product-local mutation framework. The replacement order is:

1. In `cmd/semdragons/main.go`, initialize ownership storage, run one application-lifetime heartbeater, aggregate all
   approved static contracts by owner, and bind each complete owner set once before starting its components.
2. Declare contracts beside the projections owned by `processor/agentprogression`, `processor/agentstore`,
   `processor/guildformation`, `processor/partycoord`, `processor/bossbattle`, and `questdag`. Use the exact owners,
   entity patterns, named predicate groups, and `BirthPredicates` in
   `docs/migrations/semstreams-beta158/writer-owner-matrix.md`.
3. Inject only the required narrow interfaces. Entity birth paths receive `EntityCreator`; mutable current-state
   owners receive `OwnedReplacer` and select one exact group per call; evidence producers receive
   `EvidenceAppender`; exact verification paths receive `AuthoritativeReader`.
4. Replace the write side of `graphclient.go`: `EmitEntity`, `EmitEntityUpdate`, `EmitEntityCAS`, and
   `PutEntityState` must no longer write shared `ENTITY_STATES` state for migrated owners. Keep its reads, prefix
   queries, and watches until a separate read-side migration removes them.
5. In `questdag/unit.go`, replace `createUnitEntity` with `EntityCreator`, leaving `Entity.Triples` empty and sending
   initial facts declared in `BirthPredicates` and owning facts through `CreateMutation.Triples`. Retain product
   validation in `natsUnitCompletionWriter`, but replace its `updateUnitCompletion` transport with
   `OwnedReplacer` selecting the exact completion group. Replace exact-entity verification in `readUnitEntity` with
   `AuthoritativeReader`; the execution prefix query remains a separate read concern.
6. Migrate `cmd/semdragons/seed_e2e.go` from complete-Agent `GraphClient.EmitEntity` writes to authoritative create
   plus the `agentprogression` owner boundary. Startup code must not borrow or construct an owner token.
7. Route every cross-owner effect through the target owner or lifecycle API. Bossbattle, agentstore, redteam,
   guildformation, and partycoord must not use another owner's bound client to replace a full Quest, Agent, Guild,
   Party, review, or DAG entity.

Store-item, inventory-entry, active-effect, guild, party, battle, membership, assignment, and DAG-unit births use
`CreateWithTriples` only after their product contracts, `BirthPredicates`, and deterministic IDs are approved. Each
mutable group then uses a distinct named `ReplaceOwned` selection; append-only observations use `AppendEvidence`.

This SemStreams change does not edit Semdragon. Downstream adoption remains tracked by
[SemStreams issue #313](https://github.com/C360Studio/semstreams/issues/313) and Semdragon's own migration work.
PR #696 is a later internal-adoption change, not part of this public API PR.

## Structured values remain separate

The mutation client does not define a representation for repeated structured values. It also does not choose
between structured literals and first-class child entities.

[Issue #683](https://github.com/C360Studio/semstreams/issues/683) remains responsible for structured-literal
encoding, or, if it selects child entities, their IDs, predicates, parent links, ordering, cardinality, query
behavior, and lifecycle. The mutation client can create and reconcile those child entities after that model exists;
it does not supply the model.

Until #683 is resolved, do not treat an undocumented `map[string]any` value or dynamic predicate segments as a
framework contract.

## Related material

- [ADR-056: Authoritative Semantic State](../adr/056-authoritative-semantic-state.md)
- [Governed Semantic State](../concepts/28-governed-semantic-state.md)
- [Issue #313: reusable Go owned-write helper](https://github.com/C360Studio/semstreams/issues/313)
- [Issue #683: repeated structured values](https://github.com/C360Studio/semstreams/issues/683)
- [Issue #697: server-side mutation idempotency](https://github.com/C360Studio/semstreams/issues/697)
