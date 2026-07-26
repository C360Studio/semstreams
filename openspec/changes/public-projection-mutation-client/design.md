# Design: Public Projection Mutation Client

## Context

ADR-056 defines projection contracts, owner-token fencing, and three write modes:
`replace-owned`, `cas-transition`, and `append-evidence`. `projection.BindAndHeartbeat` exposes the read-side
ownership boundary, and graph-ingest exposes public mutation request/response types. The missing layer is a typed
client that combines those primitives without making consumers understand rule execution or raw NATS behavior.

The existing rule `TripleMutator.ReplaceOwned` is insufficient as a framework API:

- it is coupled to rule internals;
- it does not cover authoritative entity birth or append evidence;
- it cannot represent whether an ambiguous or degraded response committed;
- it does not provide authoritative read-back;
- downstream consumers still have to implement ownership and retry policy.

The live append handler adds triples to entity storage. A transport retry can therefore duplicate evidence. The
client must not apply one generic retry helper to all mutation modes.

## Decision

### 1. Put the boundary in `pkg/projection`

The mutation client belongs beside `projection.Contract`, `Bind`, and `BindAndHeartbeat`. It is a projection
capability, not a rule capability or a general graph administration client.

The initial public surface is:

```go
type PredicateGroup struct {
	Name       string              `json:"name,omitempty"`
	Mode       ownership.WriteMode `json:"mode"`
	Predicates []string            `json:"predicates"`
}

type Contract struct {
	Name            string           `json:"name"`
	MessageType     string           `json:"message_type,omitempty"`
	EntityPattern   string           `json:"entity_pattern"`
	Groups          []PredicateGroup `json:"groups,omitempty"`
	BirthPredicates []string         `json:"birth_predicates,omitempty"`
	ForeignEdges    []ForeignEdge    `json:"foreign_edges,omitempty"`
	IndexingProfile string           `json:"indexing_profile,omitempty"`
}

type MutationClientConfig struct {
	NATS        *natsclient.Client
	Registry    *ownership.Registry
	Heartbeater *ownership.Heartbeater
	Owner       string
	Contracts   []Contract
	Timeout     time.Duration
	Retry       natsclient.RetryConfig
}

func BindMutationClient(
	ctx context.Context,
	cfg MutationClientConfig,
) (*MutationClient, error)

type MutationMetadata struct {
	RequestID string
	TraceID   string
	Source    string
	Timestamp time.Time
}

type CreateMutation struct {
	Contract string
	Entity   *graph.EntityState
	Triples  []message.Triple
	Metadata MutationMetadata
}

type ReplaceOwnedMutation struct {
	Contract string
	Group    string
	EntityID string
	Desired  []message.Triple
	Metadata MutationMetadata
}

type AppendEvidenceMutation struct {
	Contract string
	EntityID string
	Evidence []message.Triple
	Metadata MutationMetadata
}

func (c *MutationClient) CreateWithTriples(
	ctx context.Context,
	req CreateMutation,
) (MutationReceipt, error)

func (c *MutationClient) ReplaceOwned(
	ctx context.Context,
	req ReplaceOwnedMutation,
) (MutationReceipt, error)

func (c *MutationClient) AppendEvidence(
	ctx context.Context,
	req AppendEvidenceMutation,
) (MutationReceipt, error)

func (c *MutationClient) ReadAuthoritative(
	ctx context.Context,
	entityID string,
) (*graph.EntityState, error)
```

`MutationClient` also implements four narrow public interfaces:

```go
type EntityCreator interface {
	CreateWithTriples(context.Context, CreateMutation) (MutationReceipt, error)
}

type OwnedReplacer interface {
	ReplaceOwned(context.Context, ReplaceOwnedMutation) (MutationReceipt, error)
}

type EvidenceAppender interface {
	AppendEvidence(context.Context, AppendEvidenceMutation) (MutationReceipt, error)
}

type AuthoritativeReader interface {
	ReadAuthoritative(context.Context, string) (*graph.EntityState, error)
}
```

These signatures are the implementation contract. Naming may change before implementation only through an
architect-reviewed OpenSpec amendment.

### 2. Register each owner once per Registry across every entry point

`BindMutationClient` validates and indexes all supplied contracts before registration or heartbeat side effects. If
any contract derives an owning claim from a `replace-owned` or `cas-transition` group, a non-nil heartbeater is
required. The constructor then calls `BindAndHeartbeat` and stores the opaque owner token. The constructed client is
immutable and safe for concurrent use.

- The one-registration rule belongs to `ownership.Registry` and applies beyond `MutationClient`. Direct
  `RegisterOwner`, `projection.Bind`, `BindAndHeartbeat`, and `BindMutationClient` all converge on the same guard.
- The composition root must collect every contract intended for a registered owner before its first registration.
  All static built-in contracts for the same owner are aggregated and passed to one `BindAndHeartbeat` call rather
  than registered incrementally.
- The first successful registration consumes that owner identity for the Registry lifetime. A concurrent or later
  same-owner attempt returns `ErrOwnerAlreadyBound` before the owner-presence heartbeat, ownership-claim KV
  mutation, or heartbeater enrollment. Identical, overlapping, and disjoint second registrations are all rejected.
- A failed first registration releases its in-progress identity guard, so a corrected first registration may be
  attempted against the same Registry. After a successful registration, correction or revival requires a new
  `Registry`, which provides a new incarnation; resigning or changing contracts does not permit same-Registry
  registration.
- `BindMutationClient` preserves `ErrOwnerAlreadyBound` for `errors.Is` and classifies it as a not-committed mutation
  conflict.
- The caller owns the heartbeater lifecycle and cancels it through the supplied context/composition root.
- Create requests containing owning facts and all replace-owned requests carry the bound token.
- Append-evidence requests do not carry an owner token.
- A collection containing only `append-evidence` groups may bind with a nil heartbeater. That client is limited to
  append and read-back; create and replace fail validation before transport or registry mutation.
- A birth-only contract has no ownership claim. It may bind and create without a heartbeater or owner token.
- When the complete contract set derives no claim or foreign edge, binding skips ownership registration and retains
  a zero token. Because `RegisterOwner` is not called, a birth-only/no-claim client does not consume the owner's
  one-registration identity.
- `BirthPredicates` never cause heartbeat or token requirements. A contract that also contains a `replace-owned` or
  `cas-transition` group still requires liveness because that group derives an owning claim.
- A stale-token response is terminal for that client instance. The client never silently rebinds. Recovery replaces
  the Registry and owner incarnation at the composition root; it does not bind the same owner a second time against
  the old Registry.
- The token is not exposed as a string or accepted per request.

### 3. Enforce the declared contract before transport

Every mutation names one contract. The client validates entity identity, message type, predicate groups, birth
predicates, subject, and indexing profile before publishing.

`PredicateGroup.Name` is optional for backward compatibility. When present, it is a stable, case-sensitive, single
NATS subject token: no `.`, whitespace, `*`, or `>`. Names are unique across all groups in one contract.

For `ReplaceOwned`, a non-empty `Group` selects exactly the named `replace-owned` group. An empty selector is
accepted only when the contract has exactly one `replace-owned` group. It is invalid when the contract has none or
more than one. An unnamed group can be selected only by the single-group omission rule; existing unnamed contracts
remain valid but cannot participate in selective replacement.

The client derives the removal set from every predicate in only the selected group. `Desired` may contain only
triples in that group. Omitted predicates in that group are removed, while sibling groups, foreign predicates,
birth predicates, and append-only predicates are preserved. A multi-predicate group is one atomic reconciliation
boundary: omission intentionally clears a predicate in that group. Callers cannot submit an arbitrary remove list.

For `AppendEvidence`, every triple must belong to an `append-evidence` group and target the requested entity as its
subject. The first version is deliberately single-entity; it does not claim cross-entity atomicity.

`Contract.BirthPredicates` is an optional list of create-only facts. Each value is a registered canonical exact
predicate. Duplicates are invalid, and a birth predicate cannot also appear in any write-mode predicate group
(`replace-owned`, `cas-transition`, or `append-evidence`). A matching `ForeignEdge` is not an overlap because it
targets a different subject lane.

`BirthPredicates` are validation metadata only. They derive no `OwnerClaim` or `ForeignEdgeClaim`, are never added
to a replacement removal set, and cannot authorize append. A contract with birth predicates but no groups or
foreign edges is valid.

Create-only is a client authorization rule, not a graph write-once invariant. Graph-ingest does not lease, fence, or
otherwise protect a birth predicate after creation. A nonconforming writer using another mutation lane can change
or remove it. Consumers that require immutable facts need a separately enforced ownership or storage contract.

For `CreateWithTriples`, every supplied triple must use the primary entity ID as its subject and be declared either
in `BirthPredicates` or in a `replace-owned` or `cas-transition` group. An `append-evidence` group alone does not
authorize entity creation. Primary-subject outbound relationship triples remain valid when their predicate is in
one of those create-authorized lanes. Owned primary-subject facts are written atomically with the entity.

`CreateMutation.Triples` is the sole source of birth facts. The client rejects a non-empty
`CreateMutation.Entity.Triples` before mutation transport side effects. It sends no mutation RPC or authoritative
read-back, leaves caller input unchanged, and never merges the two fields or applies a precedence rule.

The client rejects every cross-subject triple before transport, including a triple that could match a
`ForeignEdgeClaim`. The current graph-ingest create handler commits the primary entity before routing
foreign-subject edges best-effort, so it cannot provide the atomicity or verification this API promises.
Foreign-subject writes remain on the existing reconciliation path.

The API does not expose lifecycle expected revision. `cas-transition` remains owned by `pkg/lifecycle`.

### 4. Make provenance stable before the first request

`MutationMetadata.RequestID` and `MutationMetadata.Source` are required for create and append, and are accepted for
replace. The client works on copies and fills only unset triple metadata:

- `Source` from mutation metadata;
- `Timestamp` from mutation metadata;
- `Context` from the stable request ID.

If a triple supplies a conflicting non-zero value, validation fails. Every retry and read-back comparison uses the
same canonical values. `TraceID` remains correlation metadata and is not an idempotency key.

This is operational provenance, not cryptographic authorization or non-repudiation.

### 5. Use operation-specific retry and read-back rules

The client uses the existing graph subjects and classified response envelopes. It never interprets transport
silence as success.

#### Create with triples

Creation sends one atomic `CreateEntityWithTriplesRequest` containing only `CreateMutation.Triples`. A birth-only
creation carries an empty owner token; creation containing an owned-group predicate carries the bound token. On
`entity_already_exists` or an ambiguous transport outcome, the client performs authoritative read-back. It reports
verified success only when the stored entity has the requested identity/message type and every requested
primary-subject birth fact matches as a complete canonical `message.Triple`. Equality covers every field,
including `Confidence` and `ExpiresAt`. Framework-injected facts may be ignored, but a divergent requested
predicate is a conflict.

`no responders` proves that no serving handler accepted that attempt, so the client may retry it within the
configured budget. A timeout or lost response is ambiguous. If read-back finds the entity, equality can verify the
commit. If read-back does not find it, that observation does not prove the original request cannot commit late; a
retry can race the late commit. The operation remains duplicate-resistant, not exactly-once.

#### Replace owned

Replacement is idempotent at the selected contract predicate-group boundary. The client may use classified bounded
retry with the same request, owner token, and derived removal set. Authoritative replacement verification compares
the complete canonical `message.Triple` set for the selected group, including `Confidence` and `ExpiresAt`, and
proves that omitted facts in that group are absent. It ignores sibling groups. A revision mismatch is still
classified if returned, but this API does not offer caller-driven CAS.

#### Append evidence

Append performs a classified single attempt rather than unconditional transport retry. After an ambiguous result,
the client reads the entity and searches for the exact canonical evidence tuple:

`(subject, predicate, object, datatype, source, context=request-id)`.

This six-field tuple is intentionally narrower than create and replace equality. Append ambiguity checks do not add
timestamp, confidence, expiration, or another `message.Triple` field to the idempotency key.

If the tuple is present, the operation is verified. If it is absent, the client may retry within its configured
budget using identical values, but it cannot prove the original request will not commit after the read. The original
can commit late and the retry can then append the same evidence again. This narrows the ambiguity window but does
not prevent duplicates.

Callers must not wrap any mutation operation in a generic outer retry. A deployment that requires strict no-retry
append behavior sets `Retry.MaxRetries=0` until graph-ingest implements the server-side idempotency primitive tracked
by [#697](https://github.com/C360Studio/semstreams/issues/697). A `no responders` result remains distinct: it proves
that attempt did not reach a serving handler and may be retried by the client according to its configured budget.

#### Degraded responses

A mutation response with `degraded=true` means the mutation committed but handler-side read-back failed. The client
never retries it. It performs its own authoritative read-back:

- verification returns a committed and verified receipt;
- failed verification returns a committed receipt plus `committed-unverified`;
- the caller is explicitly told not to retry.

### 6. Expose commit state and typed errors

```go
type CommitState string

const (
	CommitNotCommitted CommitState = "not-committed"
	CommitUnknown      CommitState = "unknown"
	CommitCommitted    CommitState = "committed"
	CommitVerified     CommitState = "verified"
)

type MutationReceipt struct {
	Entity     *graph.EntityState
	KVRevision uint64
	Commit     CommitState
	Degraded   bool
}

type MutationErrorKind string

const (
	MutationInvalid             MutationErrorKind = "invalid"
	MutationNotFound            MutationErrorKind = "not-found"
	MutationConflict            MutationErrorKind = "conflict"
	MutationRevisionConflict    MutationErrorKind = "revision-conflict"
	MutationStaleOwnerToken     MutationErrorKind = "stale-owner-token"
	MutationUnavailable         MutationErrorKind = "unavailable"
	MutationCommitUnknown       MutationErrorKind = "commit-unknown"
	MutationCommittedUnverified MutationErrorKind = "committed-unverified"
	MutationInternal            MutationErrorKind = "internal"
)

type MutationError struct {
	Operation MutationOperation
	Kind      MutationErrorKind
	Code      string
	Class     errs.ErrorClass
	Commit    CommitState
	Detail    map[string]any
	Err       error
}
```

`MutationError` implements `Error` and `Unwrap`. Existing `errors.As` checks for `*errs.ClassifiedError` and
`errors.Is` checks for sentinel causes continue to work.

The required mappings are:

| Wire or transport result | Mutation kind | Commit state | Retry rule |
| --- | --- | --- | --- |
| local validation, `invalid_request`, `structural_invalid` | invalid | not committed | never |
| `entity_not_found` | not found | not committed | never |
| divergent `entity_already_exists` | conflict | not committed | never |
| `revision_mismatch` | revision conflict | not committed | never |
| `owner_lease_stale` | stale owner token | not committed | new Registry/incarnation; no old-Registry rebind |
| NATS `no responders` | unavailable | not committed | bounded client retry |
| transient handler error | unavailable | not committed | existing classified policy |
| timeout/lost response with failed verification | commit unknown | unknown | no outer retry; internal risk per mode |
| degraded response with failed verification | committed unverified | committed | never |
| `graph_state_reset_required` or fatal/internal invariant | internal | not committed | never |

Context cancellation or deadline always terminates further retry and read-back. A request timeout is an ambiguous
transport outcome for create and append, not proof that the server did not commit.

### 7. Preserve wire compatibility

The client uses the existing:

- `graph.mutation.entity.create_with_triples`;
- `graph.mutation.entity.update_with_triples`;
- `graph.mutation.triple.add_batch`;
- `graph.ingest.query.entity`.

It serializes the current graph request and response types without a wrapper envelope or `BaseMessage`. Subject
constants may be centralized internally, but values and JSON shapes must not change. `PredicateGroup.Name`,
`Contract.BirthPredicates`, and `ReplaceOwnedMutation.Group` are local contract/client inputs and are not serialized
onto graph mutation requests. No graph-ingest handler, persisted schema, or compatibility migration is required.

### 8. Make owner-lease enforcement a rollout prerequisite

Every graph-ingest instance serving mutation subjects for an owner-bound client must run with
`enforce_owner_lease=true`. A mixed serving fleet is unsafe because a request routed to a non-enforcing instance can
bypass the token fence.

Before enabling an owner-bound client, operators must prove that every serving instance has the claim reader
configured, the owner heartbeat is live, and owner-lease mismatch metrics remain zero during a bounded rollout
window. Configuration review alone is insufficient. Semdragon issue
[#313](https://github.com/C360Studio/semstreams/issues/313) remains gated on this evidence.

### 9. Keep issue #683 as a model-layer dependency

If issue #683 selects canonical child entities for repeated structured values, it may depend on this client for
atomic child creation, owned replacement, and read-back. Issue #683 must still define child IDs, predicates, parent
links, ordering/cardinality, query behavior, and lifecycle. If it selects structured literals instead, this client
still provides group replacement but does not define the encoding.

## Alternatives Considered

### Export only rule `TripleMutator.ReplaceOwned`

Rejected. It preserves rule coupling and leaves most duplicated downstream orchestration unsolved.

### Add a general raw graph client

Rejected. A raw client cannot derive owned removal sets or enforce projection contracts, and would recreate the
unsafe caller-controlled boundary ADR-056 removed.

### Retry all mutations with `RequestWithRetryClassified`

Rejected. Blind append is not idempotent, and create transport timeouts require read-back before retry.

### Automatically refresh a stale token

Rejected. Owner incarnation changes are a composition-root event and must not be hidden inside one request.

## Migration

1. Extend contract validation with optional group names and create-only birth predicates.
2. Add request, error, receipt, and group-selection types with unit tests.
3. Add the classified RPC/read-back adapter and authoritative create path.
4. Add selected-group owned replacement.
5. Add append evidence with lost-response verification tests.
6. Prove lease enforcement, claim-reader wiring, heartbeat liveness, and zero mismatch metrics on every serving
   graph-ingest instance.
7. Keep PR #696 and Semdragon #313 as later adoptions after the public contract and rollout gate are approved.

Existing APIs remain in place throughout. Removal of duplicated internal helpers requires separate evidence that no
caller depends on their old surface.

## Test Strategy

- Table-driven validation tests for heartbeat requirements, group names/selectors, birth predicates, birth-fact
  source, entity pattern, predicate mode, metadata, and selected-group remove-set derivation.
- Fake responder tests for every error-code mapping, context cancellation, retry budget, degraded success, and
  ambiguous transport.
- NATS/graph-ingest integration tests proving wire token propagation, stale-token fencing, primary-subject atomic
  create, cross-subject rejection, conflict/read-back, owned delete-on-omit, foreign-predicate preservation, and
  authoritative read-back.
- Lost-response append tests proving duplicate resistance and the documented late-commit double-apply boundary.
- Registry and bind tests proving exactly one successful same-owner registration across direct `RegisterOwner`,
  `Bind`, `BindAndHeartbeat`, and `BindMutationClient`; identical and concurrent second attempts fail before
  heartbeat or claim mutation, while a failed first attempt releases the identity.
- Composition-root tests proving static built-in contracts for one owner are validated, aggregated, and bound once.
- Deployment tests proving every serving graph-ingest instance enforces owner leases before mutation traffic.
- Concurrency and race tests for one immutable client used by multiple goroutines.
- Compatibility tests that compare subjects and serialized request/response shapes with existing graph types.

## Non-Goals

- Lifecycle CAS transitions or state-machine orchestration.
- Entity deletion or cross-entity atomic mutation.
- Cross-subject creation and foreign-edge pending/reconciliation policy.
- Structured-value encoding or issue #683's domain model.
- Domain adapters or Semdragon source changes.
- Cryptographic authorization, signing, or provenance attestation.
- Graph-enforced write-once semantics for birth predicates; they derive no claim and nonconforming writers remain
  able to mutate them.
- Changes to graph handlers, NATS wire shapes, or persisted storage.
- A singleton/global client or a replacement for all graph query APIs.
