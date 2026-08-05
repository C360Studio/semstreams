# Reviewed GS-01 recovery contract — revision 35

> **DESIGN REVIEW PASS — OWNER ACCEPTANCE PENDING.** This file preserves the exact reviewed normative stack. Later
> replacement/addendum clauses take precedence over conflicting earlier clauses. Revision 30 is superseded and omitted;
> revision 31 remains normative where revision 32 explicitly retains its CLI/JCS/target-phase corrections.

## Artifact manifest

| Artifact | SHA-256 |
|---|---|
| r27 base | `4f2bdd3eeab714c29da918bf97a664067c5a1f5b8ee53f576189290beae4e670` |
| r28 addendum | `af4a183f7f41b883758a02001f6329abc04de8a4aca5ee0d671ad6be9e701853` |
| r29 addendum | `1ed67ea57fa2ef6fbca9440c9f84e882b6297dd3761d4127d452d14969679348` |
| r31 source replacement | `f2a4400fed5590581697403898e3bc28051b1185c57316434929a49c76b708d2` |
| r32 source replacement | `2391fd27f8afa7b883feaedab83099148617ff96bb28d209f833c8b24111b803` |
| r33 addendum | `8604df1fe927ceedf0367f44c7ed63b74a9a64add1237cfd0dac443fc2c5b1e1` |
| r34 addendum | `3b1e05ef53916f0133cfe42b3d7137dca5b8bb7f84b85fa6c2b68a5a4e08115a` |
| r35 addendum | `311d3360ad50db0cfa9a1baf8f1388105e0d83b4157a4702db26709c6cf83567` |

Reviewer verdict for the exact stack: `DESIGN REVIEW PASS`, `APPROVE`. No blocking or high findings remain.


---

<!-- BEGIN gs01-design-revision27.txt -->

# GS-01 Recovery Contract r27 — Physical Restore, Startup Fence, and Read-Only Inspection

Revisions r13–r26 are superseded by this bounded draft.

Proposed program-gate relocation, requiring owner acceptance:

- GS-01 ends at physically verified restore and exact reader-only CLI inspection.
- Its terminal phase is `complete_readonly`; every normal component remains fenced.
- GS-02 owns source/durable reactivation, restored-guard semantic admission, application-disposition proof, and
  local/cross-host write exclusivity.

GS-01 contains no consumer bind, source release, runtime replay, graph-ingest singleton enforcement, or write
activation.

## 1. Existing surface inventory

### 1.1 Exact authority reads

- Canonical entity: `graph/types.go:24-52`; reader returns `graph.EntityState`.
- Validating decoder: `graph/entity_predicate_contract.go:255-263`; fresh decode boundary.
- Graph query: `graph/query/client.go:281-318`, plus `:347`, `:602`, `:631-674`, `:712`, `:750`, `:849`, `:1021`;
  normal runtime, fenced.
- Graph-ingest query: `processor/graph-ingest/query.go:66-105`; normal runtime, fenced.
- Agentic tools: `processor/agentic-tools/executors/graph_query.go:182-336`; normal runtime, fenced.
- Agent-run: `agentic/agentrun/nats_reader.go:77-136`; normal runtime, fenced.
- Production/e2e composition: `cmd/semstreams/main.go:244`, `cmd/e2e-semstreams/main.go:222`; not built by CLI.
- Gateway: `gateway/graph-gateway/component.go:832-999`, `:1682-1882`; fenced.
- Graph-index: `processor/graph-index/component.go:857-985`; derived runtime, fenced.

The recovery CLI is the only recovery-time exact reader.

### 1.2 Physical state

- `KV_ENTITY_STATES`: whole backing-stream snapshot and raw digest.
- `KV_GRAPH_INGEST_APPLIED_SEQ`: whole snapshot/digest; no semantic validation.
- `KV_ENTITY_SUFFIX_INDEX`: transitional whole snapshot/digest.
- Policy-selected governed `OBJ_*`: whole snapshot/digest.
- Product source streams: provenance only, never restored.
- Existing graph-ingest durables: provenance/diagnostics only; never restored, bound, reset, or changed.

### 1.3 Drain and lifecycle seams

- Shutdown starts at `processor/graph-ingest/component.go:1154`.
- Pool-drain errors are logged and ignored at `processor/graph-ingest/component.go:1182-1186`.
- `pkg/dispatch/keyed_pool.go:351-395` returns hard timeouts; stats are at `:404-421`.
- Server outstanding work is observed at `natsclient/client.go:643-702`.
- Graph-ingest observes it at `processor/graph-ingest/readiness.go:121-145`.

Current `Stop` alone cannot be checkpoint acceptance because drain failure is non-fatal.

### 1.4 Startup gate seams

The recovery gate covers:

- production/e2e `Manager.StartAll`: `cmd/semstreams/main.go:598-610`,
  `cmd/e2e-semstreams/main.go:745-757`;
- initial creation/start: `service/component_manager.go:223-312`, `:364-559`;
- direct creation: `service/component_manager.go:917-990` before factory at `:946`;
- model rebuild/dynamic config: `:693-705`, `:1307-1620`;
- reconcile: `:1622-1845`; and
- restart/create-start: `:1847-2040`.

## 2. Adopter seams

- **Recovery operator:** knows bundle, NATS context, recovery ID, and explicit takeover or begin-new intent. Physical
  restore
  leaves the system fenced. Typed CLI and `AUTHORITY_RECOVERY` report state; the operator predicts no sizes, gaps,
  application disposition, or activation safety.
- **Runtime component author:** knows nothing recovery-specific. Any recovery control prevents allocation/start. GS-02
  defines activation.
- **Recovery reader:** knows entity IDs and typed outcomes. No manager/factory/consumer/writer/subscription/gateway
  starts.
- **Source owner:** GS-01 never authorizes publisher/source restart. GS-02 or an accepted product runbook does.
- **Permission administrator:** grants only §12 subjects. Denied is never absence/empty. No consumer-management or
  source-publish permission is needed.

## 3. Bounded target

GS-01 performs physical quiescence, native checkpoint, raw digest, content-addressed bundling, fenced restore/exact
adoption, physical verification, runtime startup fencing, exact CLI inspection, and `complete_readonly`.

It does not prove every source message applied; interpret AckFloor/delivery/Term/NAK/panic/MaxDeliver/parked state as
success; bind/reactivate consumers; validate guard semantics; release sources; start normal components; enforce
single-writer composition; scan references as a closed world; add graph status; or sign the bundle.

## 4. Exact authority reader

```go
type AuthorityOutcome string

const (
	AuthorityFound AuthorityOutcome = "found"
	AuthorityAbsent AuthorityOutcome = "absent"
	AuthorityPoison AuthorityOutcome = "poison"
	AuthorityUnavailable AuthorityOutcome = "unavailable"
	AuthorityCanceled AuthorityOutcome = "canceled"
	AuthorityInvalid AuthorityOutcome = "invalid"
)

type AuthorityError struct {
	Outcome AuthorityOutcome
	EntityID string
	ObservedRevision uint64
	Retryable bool
	Cause error
}
func (e *AuthorityError) Error() string
func (e *AuthorityError) Unwrap() error

type AuthorityItem struct {
	RequestedID string
	Outcome AuthorityOutcome
	Entity graph.EntityState
	KVRevision uint64
	Err *AuthorityError
}

type EntityReader interface {
	ReadEntities(context.Context, []string) ([]AuthorityItem, error)
}
```

Found returns a fresh validated entity, same-entry KV revision, nil error, and found outcome. Non-found returns zero
entity/revision, nonnil typed error with matching outcome/ID, and observed entry revision when known (especially
poison). `Error()` is a stable outcome/ID summary; `Unwrap()` returns cause. Unavailable/canceled are retryable;
absent/poison/invalid are not.

Top-level error is only whole-request invalidity or prerequisite failure before item work. Later cancellation fills
unfinished positions as canceled. Ordering/cardinality are exact.

For one fetched KV entry, copy its bytes once, allocate a fresh `graph.EntityState` for every requested position, and
call `graph.UnmarshalEntityState` separately. This gives independent supported JSON shapes—triples, nested
`map[string]any`/`[]any`, primitives, ExpiresAt, StorageRef—without reflection, cycles, or shallow fallback.

## 5. Physical quiescence checkpoint

1. Freeze product publishers and every non-graph-ingest writer to captured stores.
2. Keep graph-ingest consuming.
3. Require each relevant consumer `NumPending == 0` and `NumAckPending == 0`.
4. Require local queue depth and in-flight count zero.
5. Hold both observations through a bounded stability interval.
6. Stop intake/component execution.
7. Reobserve all four values as zero.
8. Snapshot while all writers/publishers stay frozen.

This proves only no local/pending writer can mutate captured storage. It does not prove application disposition. Failed,
NAKed, terminated, panicked, MaxDeliver-exhausted, parked, or semantically incomplete messages may coexist with zero.
Their observable evidence is diagnostic and handed fail-closed to GS-02.

A checkpoint-only fail-closed drain seam exposes queue/in-flight, errors on timeout, blocks snapshot on failure, and
propagates failure rather than logging it. It does not redesign scheduling.

## 6. Captured state and object policy

Capture complete backing streams for `KV_ENTITY_STATES`, `KV_GRAPH_INGEST_APPLIED_SEQ`,
`KV_ENTITY_SUFFIX_INDEX`, and every policy-selected configured governed `OBJ_*`. Each gets a native directory, raw
digest, `PhysicalContentTuple`, physical name, and role.

There is no WatchAll, KV semantic inventory, temporary consumer, or guard interpretation.

Object selection is policy-driven. Capture selected stores whole; preserve metadata/chunks/links opaquely; do not
dereference links or add external stores. Dangling/external/unconfigured references remain diagnostics only.

## 7. Tuple separation

`SourceProvenanceTuple` (not restored equality): Name, Created, canonical config digest, FirstSeq, LastSeq, Msgs, Bytes.
Durable provenance is source/name/Created/config digest; dynamic delivery fields are diagnostics and unknown never means
success.

`PhysicalContentTuple` excludes Created and contains complete normalized StreamConfig digest, FirstSeq, LastSeq, Msgs,
Bytes, and only deletion state proven stable across pinned snapshot/restore tests.

## 8. Native snapshot and raw digest

Use pinned `Stream.SnapshotToDirectory` and `Manager.RestoreSnapshotFromDirectory`: fresh directory per stream, no
consumers, expected files only, deterministic directory hash, recorded versions, no custom archive/chunk framing.

Send `{"seq":cursor,"next_by_subj":">"}` to `{P}.STREAM.MSG.GET.<stream>`.

```go
type RawMsgGetResponse struct {
	Type string `json:"type"`
	Error *RawAPIError `json:"error,omitempty"`
	Message *RawStoredMessage `json:"message,omitempty"`
}
type RawAPIError struct {
	Code        int    `json:"code"`
	ErrCode     int    `json:"err_code"`
	Description string `json:"description"`
}
type RawStoredMessage struct {
	Subject string `json:"subject"`
	Seq     uint64 `json:"seq"`
	Time    string `json:"time"`
	Headers []byte `json:"hdrs,omitempty"`
	Data    []byte `json:"data,omitempty"`
}
```

Require type `io.nats.jetstream.api.v1.stream_msg_get_response`. Classify error before message. Only 404/10037 is
no-message. Success requires nonnil message; error+message remains error; nil message without error is malformed.
Stored header/data bytes hash exactly; transport metadata never does.

Refuse `LastSeq == math.MaxUint64`. Empty streams request FirstSeq or 1, require no-message, encode empty, and recheck
tuple. Nonempty traversal starts FirstSeq, encodes each `[cursor,seq)` gap and framed message, advances `seq+1`, ends on
no-message, encodes trailing gap, requires count==Msgs, and rechecks tuple. Movement, malformed envelope, early end,
duplicate/descending sequence, or mismatch refuses.

## 9. Bundle manifest

The bundle is integrity-only and content-addressed. It is not signed. The canonical manifest binds:

- Format version, recovery ID, and manifest content hash.
- NATS account, domain, and API-prefix identity.
- `SourceProvenanceTuple` and durable provenance/diagnostic sidecar reference.
- Exact physical stream names and roles.
- Snapshot-directory, raw stored-message, and `PhysicalContentTuple` digests.
- Governed object-store selection.
- Tool, client, and server versions.

The manifest hash excludes its own field. Transport authenticity and operator identity are outside GS-01.

## 10. Recovery control policy

`AUTHORITY_RECOVERY` is owner-only recovery control, not graph status.

### 10.1 KeyValue policy input

```text
Bucket:       AUTHORITY_RECOVERY
Storage:      File
History:      1
TTL:          0
MaxBytes:     0
Replicas:     1
MaxValueSize: 16384
```

All other `KeyValueConfig` fields are zero or nil. `Replicas: 1` is proposed for offline, target-local recovery whose
durable authority is the external bundle; changing it requires an owner ruling.

### 10.2 Required normalized server readback

```text
Name:                  KV_AUTHORITY_RECOVERY
Subjects:              [$KV.AUTHORITY_RECOVERY.>]
Retention:             Limits
MaxConsumers:          -1
MaxMsgs:               -1
MaxBytes:              -1
MaxAge:                0
MaxMsgsPerSubject:     1
MaxMsgSize:            16384
Storage:               File
Replicas:              1
AllowRollup:           true
DenyDelete:            true
Discard:               DiscardNew
Duplicates:            2m
AllowDirect:           true
Metadata:              {"semstreams.owner":"authority-recovery/gs01"}
```

Also require no republish, mirror, sources, compression, subject transform, subject-delete-marker TTL, placement,
sealed state, or extra metadata. Every other normalized semantic field must hold its pinned false, zero, or nil value.
The policy input's `MaxBytes: 0` intentionally normalizes to server readback `MaxBytes: -1`.

### 10.3 Exact schema value

`_schema` contains exactly these UTF-8 bytes, without whitespace or trailing newline:

```json
{"max_json_value_bytes":12288,"owner":"authority-recovery/gs01","policy_version":2,"schema":"semstreams.authority-recovery","stream":"KV_AUTHORITY_RECOVERY"}
```

All JSON control values are at most 12 KiB so headers plus data remain below the 16 KiB stream maximum.

### 10.4 Keys and records

```text
_schema
v1.claim
v1.stream.<digest>
```

`<digest>` is exactly lowercase 64-character SHA-256 hex over the raw UTF-8 physical stream name. The record repeats
the physical name and the reader verifies the digest.

Claim fields are bounded to `schemaVersion`, `recoveryID`, `bundleDigest`, `phase`, `attemptID`, `epoch`, `createdAt`,
`updatedAt`, and `errorCode`.

Per-stream fields are bounded to `schemaVersion`, `recoveryID`, `attemptID`, `epoch`, `physicalStream`, `role`,
`phase`, `snapshotDigest`, `rawDigest`, `physicalTupleDigest`, `errorCode`, and `updatedAt`.

Claim and stream revisions exist only in KV metadata.

## 11. Attempt fencing and orphan restore rule

### 11.1 Normal ownership

A new recovery create-CASes one claim with a unique attempt ID and epoch. Each per-stream mutation:

1. Reads and validates the current claim.
2. Requires matching recovery ID, attempt ID, and epoch.
3. Reads the stream record and requires the same owner tuple.
4. CASes that record against its own KV revision.

Stale attempts fail the claim or record fence.

### 11.2 Restore issuance

Immediately before sending a restore-start request, CAS the stream record to `phase = restore_issued`. Only an
authoritative terminal result may move it to `verified`, `adopted`, or a terminal failure known to precede server-side
restore execution.

### 11.3 Takeover

Takeover is explicit and owner-authorized, but process-death attestation does not prove a server-side restore stopped.
Before rotating ownership, inspect every stream record:

- A pre-issue record is eligible for takeover.
- A terminal exact `verified` or `adopted` record is eligible for exact reconciliation and rotation.
- Any nonterminal `restore_issued` record refuses the entire takeover with `orphan_restore_unresolved`.

An absent target does not make `restore_issued` retryable: the old server operation may create or mutate it later. A
partial or mismatching target, missing terminal response, or unknown restore state also refuses automatic takeover.
Manual owner action, server restart, or an alternate target is outside automatic GS-01.

Eligible takeover CASes the claim to a new attempt ID and incremented epoch, CAS-rotates every eligible stream record,
does no work until all records are rotated, reconciles terminal exact targets without changing them, and retries only
records proven never to have reached `restore_issued`.

### 11.4 Begin-new

`begin-new` is explicit and never automatic. It refuses any prior nonterminal `restore_issued` record even when the
target appears absent. Otherwise it requires no active attempt, all normal components stopped, publishers and writers
frozen, prior physical targets absent except terminal exact records handled by an owner-approved external removal
procedure, and a validated new bundle. It then CAS-replaces the claim and initializes exactly the new manifest's fixed
digest keys.

## 12. Exact permission matrix

Let `{P}` be exactly `$JS.API`, `$JS.<domain>.API`, or a validated custom prefix.

| Operation | Publish/request permission | Subscribe/reply permission |
|---|---|---|
| Stream info | `{P}.STREAM.INFO.<stream>` | Client inbox |
| Consumer provenance | `{P}.CONSUMER.INFO.<source>.<durable>` | Client inbox |
| Raw message get | `{P}.STREAM.MSG.GET.<stream>` | Client inbox |
| Snapshot request | `{P}.STREAM.SNAPSHOT.<stream>` with client-selected delivery inbox | Request and delivery inboxes |
| Snapshot acknowledgement | `$JS.SNAPSHOT.ACK.<stream>.>` | None |
| Restore request | `{P}.STREAM.RESTORE.<stream>` | Client request inbox |
| Restore chunks | Server-returned `$JS.SNAPSHOT.RESTORE.<stream>.<token>` | Per-chunk client inbox |
| Recovery bucket info | `{P}.STREAM.INFO.KV_AUTHORITY_RECOVERY` | Client inbox |
| Recovery bucket create | `{P}.STREAM.CREATE.KV_AUTHORITY_RECOVERY` | Client inbox |
| Recovery raw record read | `{P}.STREAM.MSG.GET.KV_AUTHORITY_RECOVERY` | Client inbox |
| Recovery schema CAS | `$KV.AUTHORITY_RECOVERY._schema` | Publish acknowledgement |
| Recovery claim CAS | `$KV.AUTHORITY_RECOVERY.v1.claim` | Publish acknowledgement |
| Recovery stream CAS | `$KV.AUTHORITY_RECOVERY.v1.stream.<64hex>` | Publish acknowledgement |
| CLI entity stream info | `{P}.STREAM.INFO.KV_ENTITY_STATES` | Client inbox |
| CLI direct entity get with `AllowDirect` | `{P}.DIRECT.GET.KV_ENTITY_STATES.$KV.ENTITY_STATES.<key>` | Client inbox |
| CLI fallback without direct get | `{P}.STREAM.MSG.GET.KV_ENTITY_STATES` with exact `last_by_subj` | Client inbox |

The CLI reads StreamInfo first. It uses exact direct get only when the observed stream has `AllowDirect=true`;
otherwise it uses the explicit management fallback. GS-01 requires no consumer create, pull, bind, update, or delete
permission.

## 13. RecoveryStartupGate

GS-01 owns a minimal gate:

```go
type RecoveryStartupGate interface {
	CheckBeforeComponentMutation(ctx context.Context) error
}
```

After NATS is available, the check observes `AUTHORITY_RECOVERY`:

| Observation | Result |
|---|---|
| Bucket absent | Existing normal startup policy applies |
| Bucket present in any state | `recovery_startup_fenced` |
| Denied, unavailable, malformed, or indeterminate | Fail closed |

Under GS-01, even `complete_readonly` is fenced. GS-02 may later define an allowed activation phase. Re-read the gate
immediately before every component allocation or start; do not cache an earlier successful dynamic check.

Required placements:

1. Production binary before `Manager.StartAll`.
2. E2E binary before `Manager.StartAll`.
3. `ComponentManager.Initialize` before configured creation.
4. `ComponentManager.Start` before batch preparation.
5. Batch and single-component start defenses.
6. `CreateComponent` before registry factory invocation.
7. Dynamic config create/update.
8. Reconcile.
9. Restart and dependency-driven rebuild.

The recovery CLI bypasses no gate; it simply constructs no `service.Manager` or `ComponentManager`. Tests instrument
factory, initialization, start, dynamic creation, reconcile, and restart paths and require zero allocation or lifecycle
calls whenever the recovery bucket exists.

## 14. Reader-only recovery CLI

After `KV_ENTITY_STATES` is physically verified, the CLI opens only a NATS connection, StreamInfo/read-only access for
`ENTITY_STATES`, and the exact `EntityReader`. It creates no manager, component registry or factory, consumer,
subscription or watch, writer, gateway or index, runtime readiness publisher, or graph-status resource. Inspection
does not advance the claim or authorize activation.

## 15. GS-02 handoff

Subject to owner acceptance, GS-02 receives the verified content-addressed bundle, `complete_readonly`,
`SourceProvenanceTuple`, durable provenance and disposition diagnostics, the restored physical guard stream, and the
unchanged product-owned source and durable.

GS-02 owns:

1. Existing source/durable validation.
2. Failed, NAKed, terminated, panic, MaxDeliver, and parked-message disposition.
3. Restored guard semantic validation.
4. Consumer bind/reactivation.
5. Stale-delivery admission tests.
6. Publisher and source release.
7. Local pre-factory write exclusivity.
8. Cross-host write exclusivity.
9. Transition to a startup phase allowed by `RecoveryStartupGate`.

## 16. Acceptance tests

GS-01 tests prove:

1. Found reads return `graph.EntityState` and exact KV revision.
2. Poison errors carry observed revision; `Error()` and `Unwrap()` behave as specified.
3. Every duplicate output is freshly unmarshaled, including nested maps/slices, `ExpiresAt`, triples, and `StorageRef`.
4. Server pending/ack-pending and local queue/in-flight must all be zero; drain timeout prevents snapshot.
5. No accepted-equals-completed or durable-application claim exists.
6. NAK, `Term`, recovered panic, and MaxDeliver/parked scenarios can reach physical quiescence.
7. Those scenarios permit no write activation and pass fail-closed diagnostics to GS-02.
8. No `WatchAll` or temporary consumer is created.
9. Guard and suffix backing streams round-trip physically; ObjectStore links remain opaque.
10. `SourceProvenanceTuple` and `PhysicalContentTuple` remain distinct; restored `Created` does not fail equality.
11. Full normalized stream configuration participates in the physical digest.
12. Raw-get envelope type is exact and API error is classified before message.
13. Only `404/10037` means no message; success with nil message is malformed.
14. Raw header bytes affect the digest byte-for-byte.
15. Empty, sparse, gap, count-mismatch, and MaxUint overflow cases behave as specified.
16. Recovery policy input normalizes to the exact server readback.
17. Extra metadata or a divergent stream field fails schema validation.
18. Every control JSON value is at most 12 KiB.
19. Stream keys are exact SHA-256 hex digests of physical names.
20. Claim revision exists only in KV metadata; every stream record owns and CASes its revision.
21. Nonterminal `restore_issued` blocks takeover even when target is absent.
22. No process-death attestation permits retry; begin-new refuses unresolved issued records.
23. Snapshot and restore subjects match section 12.
24. CLI selects direct get only after observing `AllowDirect`.
25. Any present recovery bucket fences both binaries.
26. Initial component creation makes zero factory calls while fenced.
27. Batch and single starts make zero `Start` calls while fenced.
28. Dynamic create, restart, rebuild, and reconcile allocate nothing while fenced.
29. Recovery CLI constructs no manager.
30. `complete_readonly` remains fenced.

## 17. r26 finding disposition

| r26 finding | r27 correction |
|---|---|
| Drain implied application completion | Reduced to physical no-writer quiescence |
| Accepted/completed counter equality | Removed |
| Failed/Term/MaxDeliver/parked state | Diagnostics handed fail-closed to GS-02 |
| Takeover could retry absent post-issue target | Prohibited as `orphan_restore_unresolved` |
| Process-death attestation implied safety | Removed; server operation may outlive process |
| No minimal runtime gate | Added GS-01 `RecoveryStartupGate` |
| Gate covered only initial startup | Added initial, batch, single, dynamic, reconcile, rebuild, and restart seams |
| Recovery KV policy/readback conflated | Separated policy input from normalized server configuration |
| Control values could approach stream limit | Limited JSON data to 12 KiB |
| Per-stream key shape underspecified | Fixed to SHA-256 lowercase hex |
| Raw envelope underspecified | Added exact type, error-first classification, and nonnil-message requirement |
| Snapshot/restore permissions incomplete | Added selected inboxes, snapshot ACK, and returned restore subject |
| Reader deep-copy claim too broad | Replaced with fresh validated unmarshal per output position |
| Source and restored physical tuples conflated | Split provenance and physical tuples |

## 18. Owner rulings required

The owner must decide:

1. Whether to accept the GS-01/GS-02 gate relocation.
2. Whether `RecoveryStartupGate` belongs in the component manager package or a lower NATS recovery package.
3. Whether both binary-level and centralized manager checks are required as defense in depth.
4. The GS-02 phase that eventually permits startup.
5. Whether `Replicas: 1` is accepted.
6. The exact normalized `StreamConfig` comparison implementation for the pinned server version.
7. Whether stable deletion representation is admitted after integration proof.
8. The checkpoint hard-drain method shape.
9. The governed object-store selection policy.
10. The custom JetStream API-prefix configuration source.
11. The operator procedure for `orphan_restore_unresolved`.
12. The authentication and audit mechanism for takeover and begin-new.
13. The eventual retirement plan for `KV_ENTITY_SUFFIX_INDEX`.

<!-- END gs01-design-revision27.txt -->

---

<!-- BEGIN gs01-design-revision28-addendum.txt -->

# GS-01 Recovery Contract r28 — Bounded Addendum

This addendum applies to `/private/tmp/gs01-design-revision27.txt`, SHA-256
`4f2bdd3eeab714c29da918bf97a664067c5a1f5b8ee53f576189290beae4e670`.

It replaces r27 sections 1.4, 2, 5.2–5.3, 10.1–10.4, 11.3–11.4, 12–13, and affected tests and rulings.
Unchanged r27 provisions remain normative.

The boundary is unchanged: GS-01 ends at `complete_readonly`, authorizes no writer, consumer, source, publisher, or
normal component activation, and leaves semantic admission and write activation to GS-02.

## 1. Corrected surface inventory

### 1.1 Manager and registry bypass

`component.Registry.CreateComponent` is public at `component/registry.go:185`. A caller can create a component there
and invoke `Start` directly, bypassing both binaries and `ComponentManager`. Manager and binary checks are defense in
depth, not the complete built-in writer guarantee.

### 1.2 Captured-store production writers and mutating accessors

- **Graph-ingest / `KV_ENTITY_STATES`:** writes at `processor/graph-ingest/component.go:2707` and `:2914`;
  mandatory gate is the first operation in `processor/graph-ingest/component.go:1020`.
- **Graph-ingest / `KV_GRAPH_INGEST_APPLIED_SEQ`:** write at `processor/graph-ingest/keyed_ingest.go:298`;
  mandatory gate is the same graph-ingest `Start` gate.
- **Graph-ingest / `KV_ENTITY_SUFFIX_INDEX`:** mutations at `processor/graph-ingest/component.go:3622-3628`,
  `:3645`, and `:3651`; mandatory gate is the same graph-ingest `Start` gate.
- **Object-store component / configured governed store:** dispatch at `storage/objectstore/component.go:910-911`;
  writes in `storage/objectstore/store.go:196`, `:274-280`, `:462`, and `:504`; mandatory gate is the first operation
  in `storage/objectstore/component.go:184`.
- **Agentic-loop / configured `ContentBucket`, default `AGENT_CONTENT`:** creates the store at
  `processor/agentic-loop/component.go:658-665` and writes at `processor/agentic-loop/graph_writer.go:494-517`;
  mandatory gate is the first operation in `processor/agentic-loop/component.go:346`.
- **Graph-embedding / configured store-read ObjectStore:** `processor/graph-embedding/component.go:1150` may create or
  reconcile the store; mandatory gate is the first operation in `processor/graph-embedding/component.go:620`.

Graph-embedding is included because its constructor mutates physical state when the store is absent or divergent. The
production inventory found no other built-in `NewStoreWithConfig` caller or captured-KV writer outside these boundaries.

### 1.3 Unsupported bypasses

The automatic fence does not claim to stop:

- An out-of-repository component writing captured subjects directly.
- A caller using `storage/objectstore.Store` directly without a lifecycle component.
- A process already running before recovery begins.
- A principal retaining direct NATS write permission.
- A custom component that omits the recovery gate.

A governed ObjectStore policy is supported only when every declared production writer is a gated built-in above or an
owner-audited adopter component implementing the same fail-closed `Start` gate. Unknown writers make checkpoint
eligibility `unsupported_writer_inventory`.

## 2. Corrected adopter seams

### 2.1 Built-in component adopter

- Must know: direct registry construction does not bypass recovery fencing for inventoried built-ins.
- If they directly call `Start`: the component checks recovery control before touching NATS storage or subscriptions.
- Where they learn it: lifecycle error `recovery_startup_fenced`.
- What they should not need: manager or binary topology.

### 2.2 Custom writer author

- Must know: a component writing captured storage must call the recovery gate as its first `Start` operation.
- If they do nothing: its store is ineligible for supported GS-01 recovery.
- Where they learn it: captured-store writer registration/configuration validation.
- What they should not assume: manager-level gating covers direct lifecycle calls.

### 2.3 Deployment operator

- Must know: startup gates prevent participating binaries from restarting; they do not prove old processes stopped.
- If they do nothing: recovery creation refuses without stopped-runtime evidence and an external restart lock.
- Where they learn it: recovery preflight results and claim lock provenance.
- What they should not infer: absence of health, logs, or NATS traffic proves process death.

### 2.4 Restricted-credential operator

- Must know: normal runtime credentials now require recovery-control discovery reads.
- If pre-r27 credentials deny the read: startup returns `recovery_gate_permission_migration_required`.
- Where they learn it: credential migration docs, deployment schema, examples, and startup error.
- What they should not do: translate denied recovery discovery into bucket absence.

## 3. Supported fencing guarantee

For inventoried built-in writers, require the gate at three layers:

1. Both binary startup paths.
2. `ComponentManager` initial and dynamic mutation paths.
3. Each captured-store writer or mutating accessor's own `Start`.

The component-local gate runs before bucket or ObjectStore open/create/reconcile, subscription creation, consumer
binding, writer allocation, lifecycle status publication, or any goroutine capable of storage mutation. A nil or
unavailable gate is fail-closed. Constructors build the standard gate from the required NATS dependency; it is not an
optional injection whose absence permits startup.

Direct `Registry.CreateComponent` followed by direct `Start` is therefore fenced for the listed built-ins. This remains
conditional on section 4 for already-running, external, and uninstrumented writers.

## 4. Initial recovery prerequisite and restart lock

### 4.1 Observable stopped-runtime prerequisite

Before creating `AUTHORITY_RECOVERY`, the operator supplies deployment-authoritative evidence that every target runtime
capable of writing captured storage is stopped. The deployment controller, not application silence, supplies:

```text
deploymentProvider
deploymentResource
observedGeneration
desiredReplicaCount
readyReplicaCount
activeProcessCount
observedAt
evidenceDigest
```

Require all three counts to equal zero. For non-replica process managers, an owner-approved equivalent proves the
service unit disabled/stopped and no managed process present. Missing, stale, or unsupported evidence refuses recovery.
NATS connection absence, quiet logs, missing heartbeats, and component health are diagnostic only.

### 4.2 External deployment restart lock

The operator holds a deployment-level exclusive restart lock fencing automated deployment reconciliation, autoscaling,
process supervisors, scheduled restarts, covered manual start workflows, and all target hosts. The external deployment
system owns and enforces it; GS-01 does not pretend an in-process or NATS KV lease can fence external process creation.

Observable lock evidence contains:

```text
provider
resource
lockID
holder
generationOrFenceToken
acquiredAt
verifiedAt
evidenceDigest
```

The CLI verifies the lock before creating `AUTHORITY_RECOVERY`, before each restore-start request, before every phase
transition, and immediately before `complete_readonly`. The claim records bounded lock identity and digest, never
reusable credentials.

The same lock remains held from before recovery bucket creation, through all GS-01 work and `complete_readonly`, until
GS-02 accepts handoff or recovery is abandoned. `complete_readonly` does not release it. Loss or unverifiable ownership
fails closed; the recovery bucket still fences participating binaries, but lock loss is not safe continuation.

## 5. Physical quiescence and disposition diagnostics

The r27 hard drain remains physical only: server pending and ack-pending plus local queue depth and in-flight are all
zero. It proves no inventoried local writer remains able to mutate during snapshot, not application success.

Every source/durable diagnostic uses:

```go
type EvidenceState string

const (
	EvidencePresent EvidenceState = "present"
	EvidenceAbsent  EvidenceState = "absent"
	EvidenceUnknown EvidenceState = "unknown"
)

type EvidenceProvenance struct {
	Operation  string
	Subject    string
	ObservedAt time.Time
	Server     string
	Stream     string
	Consumer   string
	APIError   int
	APIErrCode int
	Source     string
}

type Diagnostic[T any] struct {
	State      EvidenceState
	Value      T
	Provenance EvidenceProvenance
}
```

`present` means authoritatively observed; `absent` means the authoritative source reported absence; `unknown` means
denied, unavailable, unsupported, malformed, timed out, or not observable. No zero value becomes `present`. All states
carry provenance.

Where observable, diagnostics cover pending, ack-pending, delivered and ack floors, redelivery, pause and binding,
termination, NAK/redelivery, panic disposition, MaxDeliver exhaustion, and parked-message evidence. Unsupported evidence
is `unknown`. GS-02 fails closed when required activation evidence is absent or unknown. GS-01 never supplies success by
default.

## 6. Recovery KV metadata correction

`KeyValueConfig.Metadata` explicitly contains:

```json
{"semstreams.owner":"authority-recovery/gs01"}
```

For the accepted zero-feature-level configuration, persisted semantic metadata is exactly:

```json
{"_nats.req.level":"0","semstreams.owner":"authority-recovery/gs01"}
```

The server response may add `_nats.ver` and `_nats.level`. Validation requires the exact owner, required level `"0"`, a
version accepted by the pinned compatibility matrix, and an integer server level supporting required level 0. Only the
response-added `_nats.ver` and `_nats.level` are removed for normalized equality. `_nats.req.level` is retained. Any
other metadata key or divergent value fails. All other r27 normalized `StreamConfig` requirements remain unchanged.

## 7. Prefix resolution and permission correction

Before any JetStream operation, the adapter requires `{P}.INFO` with a client reply inbox. It establishes the selected
account, domain, or custom API context.

Management subjects remain:

```text
{P}.STREAM.INFO.<stream>
{P}.STREAM.MSG.GET.<stream>
{P}.STREAM.SNAPSHOT.<stream>
{P}.STREAM.RESTORE.<stream>
```

Pinned control KV data subjects are:

| Mode | Schema subject example |
|---|---|
| Default | `$KV.AUTHORITY_RECOVERY._schema` |
| Domain | `{P}.$KV.AUTHORITY_RECOVERY._schema` |
| Custom prefix | `{P}.$KV.AUTHORITY_RECOVERY._schema` |

The same rule applies to `v1.claim` and `v1.stream.<digest>`. Domain/custom modes never use the unprefixed default form.

Snapshot flow uses a client-selected delivery inbox and publishes acknowledgements to
`$JS.SNAPSHOT.ACK.<stream>.>`. Restore uses the server-supplied `$JS.SNAPSHOT.RESTORE.<stream>.<token>` and a client
reply inbox for each chunk.

## 8. Normal runtime credential migration

Every normal runtime principal that may start a component now needs:

```text
publish/request: {P}.INFO
publish/request: {P}.STREAM.INFO.KV_AUTHORITY_RECOVERY
subscribe:       its request inboxes
```

Normal runtime credentials receive no recovery write permission. Required migration artifacts are deployment credential
schema; default, domain, and custom-prefix examples; upgrade and startup-error documentation; three-mode permission
tests; and a negative pre-r27 restricted-principal test.

A denied `{P}.INFO` or recovery StreamInfo read returns `recovery_gate_permission_migration_required` when the
connection
matches a known pre-r27 restricted profile. Other denied or indeterminate observations return
`recovery_gate_unavailable`. Neither means bucket absence; both prevent factory creation and `Start`. This
outward-facing
credential change requires an owner-approved rollout before enforcement reaches existing deployments.

## 9. Begin-new record retirement

Before `begin-new`, derive the complete prior record set from the prior content-addressed manifest stream set, current
control-stream `v1.stream.*` subjects, and the claim's stored stream-set digest. Their union must reconcile exactly;
unknown extras are corruption.

Then:

1. CAS the claim to `begin_new_rotating` with a new attempt ID and epoch.
2. Enumerate every prior stream record, including streams omitted by the new manifest.
3. CAS each record to the new owner tuple with `phase = retired`.
4. Create or reinitialize no new record until every prior record is retired.
5. Initialize the new manifest's exact record set only after complete retirement.

Records are not deleted. If an old CAS wins and changes a record to `restore_issued`, retirement CAS fails, the command
re-reads it, returns `orphan_restore_unresolved`, and starts no new work. Any prior nonterminal `restore_issued`
likewise
refuses `begin-new`, regardless of target absence.

## 10. Real maximum-value CAS test

For default, domain, and custom-prefix modes, a pinned integration test:

1. Creates the exact recovery bucket policy.
2. Produces valid canonical JSON of exactly 12,288 bytes.
3. Publishes it through the production CAS adapter and resolved subject.
4. Uses the appropriate expected-last-subject-sequence header.
5. Requires a real server `PubAck` with matching stream and sequence.
6. Reads back and compares all 12,288 bytes.
7. Repeats with update-CAS against the observed revision.
8. Rejects 12,289-byte JSON client-side before publish.
9. Verifies that rejection created no revision.

This is a real pinned-server test of actual headers plus data, not a serialized-size unit test.

## 11. Corrected gate tests

In addition to r27 tests:

1. Direct registry creation plus direct graph-ingest `Start` fences before bucket access.
2. Direct ObjectStore component `Start` fences before store create/open.
3. Direct agentic-loop `Start` fences before content-store creation.
4. Direct graph-embedding `Start` fences before its potentially mutating constructor.
5. Fenced direct starts produce zero captured-store mutation and zero subscription allocation.
6. Unknown captured-store writer inventory refuses checkpoint.
7. Recovery bucket creation refuses without authoritative stopped-runtime evidence and the external restart lock.
8. Quiet logs, absent health, and absent traffic do not satisfy stopped-runtime proof.
9. Lock ownership is reverified before restore issue and `complete_readonly`.
10. Lock loss stops progression but does not permit startup; `complete_readonly` does not release the lock.
11. Stored metadata includes owner and `_nats.req.level:"0"`; only `_nats.ver` and `_nats.level` normalize away.
12. Extra metadata fails validation.
13. `{P}.INFO` is required and domain/custom control writes use `{P}.$KV...`.
14. Pre-r27 restricted runtime credentials fail with the typed migration error.
15. Diagnostics preserve present, absent, and unknown with provenance.
16. GS-02 fixture refuses activation on incomplete required diagnostics.
17. Begin-new retires records omitted by the new manifest.
18. A concurrent old `restore_issued` CAS causes `orphan_restore_unresolved`.
19. Exact 12 KiB create-CAS and update-CAS receive real `PubAck` in all prefix modes.

## 12. Finding disposition

| Reviewer finding | r28 disposition |
|---|---|
| Public registry plus direct `Start` bypass | First-operation gates on every inventoried built-in writer/mutator |
| Captured-store writer inventory missing | Exact production writer and `Start` inventory added |
| Supported guarantee overstated | Limited to gated built-ins and audited adopters; raw/custom writers unsupported |
| Gate treated as proof old processes stopped | Authoritative stopped-runtime prerequisite added |
| No cross-process restart fence | Externally enforced deployment restart lock added |
| Lock duration unclear | Held before bucket creation through GS-02 handoff or abandonment |
| Begin-new omitted prior keys | Complete prior-set reconciliation and CAS retirement required |
| Old CAS may win `restore_issued` | Mandatory `orphan_restore_unresolved` refusal |
| Metadata only in expected readback | Explicit `KeyValueConfig.Metadata` input required |
| Server metadata normalization too broad | Only response-added version/level removed; required level retained |
| JetStream account discovery missing | `{P}.INFO` added |
| KV subjects assumed default prefix | Default `$KV...` split from domain/custom `{P}.$KV...` |
| Runtime credential consequence missing | Explicit migration and typed failure added |
| 12 KiB limit lacked server proof | Pinned PubAck create/update CAS tests in all prefix modes |
| Diagnostics lacked epistemic state | Present/absent/unknown plus provenance and GS-02 fail-closed rule |

## 13. Changed owner rulings

The prior owner-ruling list remains, with these additions or replacements:

1. Accept or reject the built-in writer inventory as the supported GS-01 fence boundary.
2. Define the extension contract for adopter-supplied captured-store writers.
3. Select the authoritative deployment stopped-state provider.
4. Select the external restart-lock provider and operational ownership.
5. Approve the normal runtime credential migration and rollout order.
6. Confirm the pinned adapter's `{P}.$KV...` behavior for supported domain/custom deployments.
7. Approve the accepted server version and API-level compatibility matrix.
8. Decide which disposition diagnostics GS-02 requires and how absent or unknown evidence is resolved.
9. Define the manual procedure for `orphan_restore_unresolved`.
10. Confirm restart-lock release belongs to GS-02 handoff or explicit abandonment, never GS-01 `complete_readonly`.

<!-- END gs01-design-revision28-addendum.txt -->

---

<!-- BEGIN gs01-design-revision29-addendum.txt -->

# GS-01 Recovery Contract r29 — Precedence, Initial Fence, and Source Barrier Addendum

This addendum applies after r27 plus r28 and replaces only the clauses named here. All other r27/r28 text remains
normative. GS-01 still ends at `complete_readonly`, activates nothing, and leaves semantic admission and activation to
GS-02.

## 1. Normative precedence correction

R28 did not replace or delete the recovery-control contract. R27 section 10 remains normative in full, including the
exact bucket/backing-stream policy, `_schema` bytes, key grammar, claim and per-stream records, attempt fencing,
restore,
takeover, begin-new, and startup fencing.

The only r27 section 10 changes are r28's metadata correction and stronger begin-new retirement rule, plus r29's exact
new claim fields, initial lock/evidence ordering, and CAS-safe begin-new clarification. The narrower later clause wins
on conflict; no unmentioned r27 field or invariant disappears.

## 2. Exact bounded claim additions

R27's bounded claim fields remain and add `streamSetDigest` and `restartLock`:

```go
type RecoveryClaimV1 struct {
	SchemaVersion   string              `json:"schema_version"`
	RecoveryID      string              `json:"recovery_id"`
	BundleDigest    string              `json:"bundle_digest"`
	StreamSetDigest string              `json:"stream_set_digest"`
	Phase           RecoveryPhase       `json:"phase"`
	AttemptID       string              `json:"attempt_id"`
	Epoch           uint64              `json:"epoch"`
	RestartLock     RestartLockEvidence `json:"restart_lock"`
	CreatedAt       string              `json:"created_at"`
	UpdatedAt       string              `json:"updated_at"`
	ErrorCode       string              `json:"error_code"`
}
```

Constraints:

- `schema_version` is exactly `gs01-recovery/v1`.
- `recovery_id` is 1–128 ASCII characters matching `[A-Za-z0-9._:-]+`.
- Both digest fields are exactly 64 lowercase SHA-256 hexadecimal characters.
- `phase` is one declared phase and at most 32 ASCII characters.
- `attempt_id` is a canonical lowercase UUID string of exactly 36 characters.
- `epoch` is a nonzero unsigned 64-bit integer.
- Timestamps are UTC RFC3339Nano and at most 35 characters.
- `error_code` is empty or 1–64 characters matching `[a-z0-9_]+`.
- Complete claim JSON is at most 12,288 bytes.

Restart-lock evidence is:

```go
type RestartLockEvidence struct {
	Provider               string `json:"provider"`
	Resource               string `json:"resource"`
	LockID                 string `json:"lock_id"`
	Holder                 string `json:"holder"`
	GenerationOrFenceToken string `json:"generation_or_fence_token"`
	DeploymentGeneration   string `json:"deployment_generation"`
	AcquiredAt             string `json:"acquired_at"`
	VerifiedAt             string `json:"verified_at"`
	EvidenceDigest         string `json:"evidence_digest"`
}
```

`provider` is 1–64 printable ASCII; `resource` and `generation_or_fence_token` are 1–256; `lock_id`, `holder`, and
`deployment_generation` are 1–128; timestamps use the claim rule; `evidence_digest` is exact lowercase SHA-256 hex.
The evidence digest hashes canonical JSON in the field order above, excluding only `evidence_digest`.

`streamSetDigest` hashes canonical JSON of:

```go
type StreamSetDigestItem struct {
	Key            string `json:"key"`
	PhysicalStream string `json:"physical_stream"`
	Role           string `json:"role"`
}
```

Include every manifest physical stream. `key` is `v1.stream.` plus lowercase SHA-256 hex over the raw UTF-8 physical
name. Sort by raw UTF-8 `physical_stream`, then `role`, then `key`. Encode one JSON array without whitespace or trailing
newline and hash those exact bytes. Physical stream and role are each 1–256 printable ASCII characters.

Claim, lock-digest input, and stream-set input use UTF-8, exact struct order, RFC 8785 string/number serialization, no
omitted fields, no insignificant whitespace or trailing newline, and explicit empty `error_code`. `bundleDigest` keeps
the r27 manifest definition. KV revisions remain metadata only.

## 3. Initial target-fence ordering

The one permitted order is:

1. Acquire the external exclusive restart lock.
2. Verify it and record token `T`.
3. While `T` remains held, obtain deployment-authoritative zero-process evidence for generation `G`.
4. Require desired, ready, and active process counts all equal zero.
5. Re-read the controller and require generation `G` remains current.
6. Reverify the same lock instance, holder, and token `T`.
7. Build bounded lock evidence and its digest.
8. Only then create `AUTHORITY_RECOVERY`.
9. Create-CAS the claim containing `T`, `G`, and the evidence digest.
10. Revalidate the same lock identity before every restore issue and phase transition.

Evidence collected before lock acquisition is stale by definition. A changed lock ID, holder, token, generation, or
zero-process count aborts before bucket creation. The startup gate prevents restart after the bucket exists; it does not
prove prior stop. The external lock remains held through `complete_readonly` and GS-02 handoff or abandonment.

Tests interleave token rotation, generation change, and a process count rising between evidence and creation; each must
refuse and leave the bucket absent. Only an unchanged token, holder, generation, and zero counts permit creation. A test
seam rejects any attempt to create before the post-evidence token verification.

## 4. Begin-new CAS ordering

Begin-new uses r28's complete prior record set and never deletes records:

1. Verify the same restart lock and stopped target.
2. CAS the old claim to `begin_new_rotating`, new attempt, and old epoch plus one.
3. Reconcile prior manifest keys, current `v1.stream.*` keys, and keys represented by old `streamSetDigest`.
4. CAS every old record, including omitted keys, to new recovery/attempt/epoch and `phase = retired`.
5. Create or reinitialize no new record until all old records are retired.
6. Compute and store the new exact `streamSetDigest`.
7. Initialize the new manifest's record set.
8. Begin restore only after initialized records and digest reconcile exactly.

On any retirement CAS conflict, re-read the record. A `restore_issued` record without authoritative terminal result
fails `orphan_restore_unresolved`; every other conflict also fails closed pending full reconciliation. Never overwrite
an
old process that won `restore_issued`.

## 5. Captured-store writer-boundary inventory

This inventory is lifecycle-boundary complete for current built-in production components. The grep-derived mutation
sites are evidence, not a promise that future internal call sites remain fixed.

### 5.1 Graph-ingest

- Lifecycle: `Start` at `processor/graph-ingest/component.go:1020`, `Stop` at `:1154`, ingest registration at `:1574`.
- Entity mutations: create `:2700`, put `:2707`, alternate create `:2872`, alternate put `:2914`, update `:2969`,
  delete `:3007`.
- Guard put: `processor/graph-ingest/keyed_ingest.go:298`.
- Suffix puts/deletes: `processor/graph-ingest/component.go:3622`, `:3628`, `:3645`, `:3651`.

### 5.2 Object-store component

- Lifecycle: `Start` at `storage/objectstore/component.go:184`; unsafe current `Stop` at `:305-335`.
- Core API subscription: `:238-250`; core write subscription: `:257-280`; JetStream writer: `:690-731`.
- Native mutations: `storage/objectstore/store.go:196`, `:274`, `:280`, `:358-368`, `:462`, `:504`.
- Component dispatch: `storage/objectstore/component.go:910-911`.

Current `Stop` is not checkpoint-safe: it closes store/cache before intake, does not retain and stop the exact local
JetStream consumer, and has no callback admission barrier or join. Core unsubscribe does not prove an entered handler
exited.

### 5.3 Agentic-loop governed-object writer

- Lifecycle: `Start` at `processor/agentic-loop/component.go:346`; `Stop` at `:508-565`; consume registration at `:892`.
- Store construction: `processor/agentic-loop/component.go:658-665`.
- Content write: `processor/agentic-loop/graph_writer.go:494-517`.

Current stop cancels and stops consumers but exposes no authoritative join for entered delivery, request, or sweeper
handlers before content store close or snapshot.

### 5.4 Graph-embedding distinction

Graph-embedding starts at `processor/graph-embedding/component.go:620`, stops at `:737`, and calls the potentially
mutating ObjectStore constructor at `:1150`. It is a mutating startup accessor, not a runtime writer: no production Put,
StoreContent, or Delete exists under it. Its `Start` stays recovery-gated, but it requires no captured-store writer
drain
while running read-only. Adding a later mutation makes it a barrier participant.

## 6. Source-side checkpoint barrier

Every runtime writer to selected captured storage implements:

```go
type CheckpointBarrier interface {
	QuiesceForCheckpoint(ctx context.Context) (CheckpointEvidence, error)
}
```

The barrier is one-shot; after success, the component cannot resume without later normal lifecycle start outside GS-01.
No snapshot begins until every selected-store writer barrier succeeds.

### 6.1 Common order

1. Product publishers and mutation callers are already frozen.
2. Reach the pre-close zero condition where a durable backlog exists.
3. Atomically close local handler admission.
4. Stop exact owned JetStream consumers.
5. Drain/unsubscribe exact core NATS intake subscriptions.
6. Join every handler admitted before closure.
7. Join every worker, queue, sweeper, or deferred writer.
8. Re-read authoritative zero counters.
9. Close storage handles/caches only after every join succeeds.
10. Mark the barrier terminal and return evidence.

Timeout, unknown applicable counter, missing exact intake handle, or incomplete join fails. Failures propagate;
logging and
continuing is insufficient. Each boundary needs an atomic admission-closed flag, a wait group registered before passing
admission, an atomic handoff that prevents `Add` after closure, retained exact consumer/subscription identities, bounded
joins, and scheduler counters where applicable. This is lifecycle instrumentation, not scheduler redesign.

### 6.2 Graph-ingest barrier

Before closure, require every input consumer pending and ack-pending plus keyed-pool queue and in-flight equal zero.
Then
close ingest/query/direct mutation admission; stop exact ingest consumers; unsubscribe mutation-capable query handlers;
join consumer/mutation callbacks; hard-drain and join the pool; reobserve all counters; and only then close
entity/suffix
caches. No accepted/completed or application-success claim is added.

### 6.3 Object-store component barrier

Before closure, require each JetStream write consumer pending and ack-pending equal zero. Then close API/write
admission;
stop the retained exact consumer; drain/unsubscribe `apiSub` and `writeSub`; join API, core-write, and JetStream
handlers;
reobserve JetStream counters; require core pending messages/bytes zero where exposed and active-handler count zero; then
close Store/cache. The current `Stop` cannot serve as this barrier without correction.

### 6.4 Agentic-loop barrier

For each JetStream input capable of reaching `contentStore`, require pending and ack-pending zero. Close loop/request/
approval admission; stop and join the approval sweeper; stop retained consumers; drain request handlers; join entered
consumer, request, approval, and graph-writer handlers; require content-writer in-flight zero; reobserve server
counters;
then detach and close the content store. Consumer stop without callback join is insufficient.

### 6.5 Direct store users and process-shutdown alternative

A direct `storage/objectstore.Store` user has no component barrier. It is supported only when deployment-authoritative
evidence covers full stop of its owning process before source checkpoint; otherwise fail `unsupported_writer_inventory`.

Full process shutdown may replace component barriers only with authoritative evidence that intake is closed; the exact
NATS client, consumers, and subscriptions stopped; all handlers/workers joined; synchronous publish/ObjectStore acks
completed; and no active process retains captured-store write credentials. Process exit signal alone is insufficient.

## 7. Checkpoint evidence

Each barrier returns bounded evidence containing component instance/type, captured stores, admission-close time,
consumer
identities, consumer pending/ack-pending, core pending messages/bytes, local queue/in-flight, active handlers,
storage-close time, observation time, and evidence digest. Every applicable counter uses r28 `present`, `absent`, or
`unknown` plus provenance. Acceptance requires each applicable zero counter to be `present` and zero; absent or unknown
fails capture. Evidence proves no remaining writer, not semantic disposition.

## 8. Required tests

### 8.1 Control and initial fence

1. Parse r27+r28+r29 and retain exact r27 policy, schema, keys, phases, and fences.
2. Reject missing, overlength, malformed, or noncanonical claim/lock/stream-set fields; pin canonical digest bytes.
3. Refuse when lock follows zero evidence, token or generation changes, or process count rises; create nothing.
4. Permit only the same token, holder, generation, and zero counts; retain the lock after `complete_readonly`.

### 8.2 Writer barriers

1. Blocked graph-ingest create/put/update/delete/guard/suffix mutations prevent snapshot.
2. New graph-ingest callbacks cannot register after admission closes; drain errors propagate; counters reobserve zero.
3. Blocked ObjectStore PutBytes, native Put/Delete, and StoreContent prevent snapshot.
4. ObjectStore API/core/JetStream handlers join; exact consumer stops; subscriptions drain before Store/cache close.
5. Missing consumer identity/counter is unknown and fails.
6. Blocked agentic content writes prevent snapshot; consumer/request/approval/sweeper/writer handlers join.
7. Agentic content store closes only after writer zero; consumer stop without callback join fails.
8. Graph-embedding direct `Start` remains fenced; read-only runtime is not a writer; a test mutation requires a barrier.

### 8.3 Begin-new

1. Retire omitted prior keys before new initialization; never delete records.
2. An old `restore_issued` CAS during retirement fails `orphan_restore_unresolved`.
3. Store new `streamSetDigest` only after complete prior retirement.

## 9. Finding disposition

| Finding | r29 disposition |
|---|---|
| R28 precedence removed r27 section 10 | R27 section 10 explicitly remains normative |
| New claim fields undefined | Exact grammar, bounds, canonical encoding, and digest inputs added |
| Stopped evidence could precede lock | Lock-first, zero evidence, generation/token recheck, then create |
| Begin-new could miss/race old records | Complete union and CAS retirement; orphan restore refuses |
| Source checkpoint lacked joins | Enforceable per-writer barriers added |
| ObjectStore stop/cache ordering unsafe | Declared unsafe and corrected barrier order specified |
| Exact consumer/callback lifecycle absent | Retained identities, admission handoff, and joins required |
| Graph-embedding classification unclear | Mutating startup accessor; runtime read-only |
| Mutation inventory overstated | Boundary-complete label plus grep-derived sites |
| Entity/ObjectStore deletes omitted | Current create/update/delete sites added |

## 10. Changed owner rulings

1. Accept the claim grammar and canonical JSON method.
2. Select the deployment provider supplying restart-lock and current-generation evidence.
3. Define maximum evidence age between observation and final token/generation recheck.
4. Choose component barriers versus authoritative full source-process shutdown.
5. Approve retained consumer handles and admission/join instrumentation in graph-ingest, ObjectStore, and agentic-loop.
6. Decide whether ObjectStore API reads capable of mutation stay shared with write methods or split before checkpoint.
7. Define how direct `storage/objectstore.Store` users register ownership or are excluded.
8. Confirm graph-embedding remains runtime read-only for governed ObjectStores.
9. Retain that GS-02, not GS-01, owns later writer activation.

<!-- END gs01-design-revision29-addendum.txt -->

---

<!-- BEGIN gs01-design-revision31-replacement.txt -->

# GS-01 Recovery Contract r31 — Offline Source Checkpoint Replacement

R31 replaces r30 entirely and replaces r29's in-process source barriers, participant evidence, and source lifecycle
coordination. R27–r29 remain normative for target restore, `AUTHORITY_RECOVERY`, target locking, physical snapshot and
digest
formats, reader-only inspection, `complete_readonly`, and the GS-02 handoff. GS-01 activates nothing.

## 1. Existing surface and command inventory

Captured storage is writable through graph-ingest (`processor/graph-ingest/component.go:1020`), its entity/guard/suffix
paths, ObjectStore (`storage/objectstore/component.go:184`; `storage/objectstore/store.go:196`, `:274`, `:280`, `:368`,
`:462`, `:504`), agentic-loop (`processor/agentic-loop/component.go:346`; `graph_writer.go:494-517`), direct Store
users,
and adopter processes with NATS write permissions. Process-local barriers cannot fence all these without a new lifecycle
subsystem.

The repository has no recovery command. R31 adds one standalone command:

```text
cmd/semstreams-recovery
  checkpoint-source
  restore-target
  inspect-entity

internal/recovery/gs01
```

It imports no manager, component manager, registry, or factory. The NATS server remains running during source
checkpoint.
The command uses Go libraries/direct management requests and never requires the NATS CLI binary.

## 2. Adopter seams

- **Source operator:** inventories every deployment and authenticated NATS principal able to write selected storage. An
  unobservable or unstoppably managed writer makes the environment unsupported.
- **Edge/local operator:** selects a supported supervisor adapter. Manual attestation or a restart convention is
  insufficient.
- **Custom writer author:** registers deployment, physical streams, and authenticated NATS principal in capture policy;
  no component lifecycle interface is required.
- **Runtime component author:** learns nothing new because every source component is stopped before capture.

## 3. Offline source checkpoint boundary

The explicit maintenance window:

1. Externally restart-fence every writer deployment.
2. Stop every SemStreams/source process able to mutate captured storage.
3. Verify exact process and NATS-client termination.
4. Keep NATS servers running.
5. Snapshot backing streams and compute raw digests from the standalone command.
6. Seal the bundle.
7. Leave restart fences held for explicit owner handoff/release.

No source process participates. Delete the proposed source control bucket, phase machine, source-side startup gate,
manager mutex, HTTP endpoint, coordinator, participant registry, component barrier, handler-admission protocol, and
snapshot capability protocol.

There is no `AUTHORITY_SOURCE_CHECKPOINT`. This is safe only when the external maintenance state survives command crash,
prevents fresh writer process creation, and remains held through native completion and bundle seal. Process-scoped locks
are unsupported. A cooperative source KV fence would not stop uninstrumented writers and adds no guarantee beyond the
authority that actually controls restart.

Target restore remains distinct: it retains a separate target lock, `AUTHORITY_RECOVERY`, and the startup fence through
`complete_readonly`.

## 4. Capture policy and writer inventory

```go
type WriterTarget struct {
	WriterID                   string
	Provider                   string
	DeploymentResource         string
	CapturedStreams            []string
	AuthenticatedNATSPrincipals []NATSPrincipal
}

type NATSPrincipal struct {
	Account string
	Subject string
	Kind    string // user or nkey
}
```

Writer IDs are unique. Every physical stream maps to every deployment allowed to write it, and every writer maps to its
authenticated principals. Client connection names are diagnostic only. Direct/custom writers must appear. The policy
binds a reviewed NATS authorization-manifest digest establishing the writer-principal set. If the provider or
authorization model cannot enumerate the applicable writer set, the environment is unsupported. Any active connection
for an unlisted write-capable principal invalidates the checkpoint.

## 5. Source maintenance lock

Source and target lock identities are distinct:

```text
source: scopeKind=source_checkpoint, scopeID=checkpointID
target: scopeKind=target_restore, scopeID=recoveryID
```

They cannot reuse lock IDs or fence tokens. Acquire multiple source locks in canonical `(provider, resource, scopeID)`
order.

```go
type MaintenanceProvider interface {
	AcquireDurableRestartFence(ctx context.Context, scope LockScope) (LockEvidence, error)
	DisableAndStop(ctx context.Context, lock LockEvidence, resource string) (StopEvidence, error)
	VerifyFence(ctx context.Context, lock LockEvidence) (LockEvidence, error)
	VerifyStopped(ctx context.Context, lock LockEvidence, resource string) (StopEvidence, error)
}
```

The fence survives command failure and requires explicit provider release.

Exact order:

1. Validate capture policy.
2. Acquire and reverify every source fence.
3. Under those tokens, disable restart and stop every writer resource.
4. Obtain authoritative stopped evidence.
5. Verify no writer-authenticated NATS connection remains.
6. Reverify fences and deployment generations.
7. Observe stable physical tuples.
8. Snapshot/digest each selected stream.
9. Before and after each native snapshot, reverify fences, stopped resources, writer connections, and NATS servers.
10. Seal the bundle and perform one final complete verification.
11. Return the bundle with locks still held.

Changed token, generation, process count, connection set, or server topology invalidates the attempt. GS-01 never
automatically releases source locks. Before any disable action, partially acquired locks may be released. After disable
begins, locks remain until explicit owner action and partial bundles are unusable.

## 6. Authoritative stopped and NATS evidence

```go
type StopEvidence struct {
	Provider               string
	Resource               string
	FenceToken             string
	DeploymentGeneration   string
	RestartDisabled        bool
	DesiredProcessCount    string
	ActiveProcessCount     string
	ProcessIdentities      []string
	ObservedAt             string
	ProviderEvidenceDigest string
}
```

Accept only restart disabled, desired/active counts `"0"`, and empty process identities. Counts are canonical unsigned
decimal strings. Pre-lock evidence and evidence whose generation later changes are invalid.

The command uses system/admin observation against every expected server in the source JetStream topology, preferring
`$SYS.REQ.SERVER.PING` and `$SYS.REQ.SERVER.PING.CONNZ`. A monitoring API is accepted only with equivalent authenticated
principal/server identity evidence.

```go
type NATSQuiescenceEvidence struct {
	ServerIDs                []string
	ExpectedWriterPrincipals []NATSPrincipal
	ActiveWriterConnections  []WriterConnection
	QueriedAt                string
	ResponseSetDigest        string
}

type WriterConnection struct {
	ServerID      string
	ConnectionID  string
	Account       string
	Principal     string
	PrincipalKind string
}
```

Collect a complete response set, match server-authenticated account and user/NKey identity, and reject any active writer
principal. Repeat before/after each snapshot and before seal. Names, subscription counts, and quiet traffic do not
substitute for authenticated identity. Every expected NATS server must remain responsive.

## 7. Pragmatic provider support

- **Container orchestrators:** require enforceable durable maintenance that blocks controllers, GitOps, autoscaling, and
  ordinary restart; reports stable token/generation; scales all writer workloads to zero; and proves no
  writer-credential
  pod/job/sidecar remains. A plain Lease, annotation, or replicas-zero without enforcement is unsupported.
- **systemd edge:** persistently mask and stop every writer unit; require masked/inactive/dead, PID zero, empty cgroup,
  and disabled dependent timer/socket/path/supervisor units; leave NATS active.
- **launchd edge:** persistently disable and boot out writer services; prove disabled override, absence, and no live
  process; keep NATS loaded.
- **Single-host containers:** one authoritative supervisor durably disables restart/reconciliation, stops writers,
  proves
  no container/process remains, and blocks alternate recreation paths.
- **Manual processes:** PID absence, quiet logs, advisory files, and human promises are unsupported until placed under
  an
  enforceable provider.

## 8. Standalone command and snapshot behavior

`semstreams-recovery checkpoint-source` loads policy, acquires locks, stops/verifies writers, connects directly to NATS,
uses the pinned native snapshot adapter, computes r27 raw digests, and seals a local bundle. It creates no application
runtime and starts nothing.

For every selected stream:

1. Obtain `PhysicalContentTuple` twice across a bounded quiescence interval and require equality.
2. Run native snapshot and raw digest.
3. Match a post-snapshot tuple.
4. Recheck locks, processes, connections, servers, and native completion.

Capture `KV_ENTITY_STATES`, `KV_GRAPH_INGEST_APPLIED_SEQ`, transitional `KV_ENTITY_SUFFIX_INDEX`, and policy-selected
governed `OBJ_*` streams. Do not modify or semantically enumerate them. Physical stability proves bytes only; failed,
NAKed, terminated, panic, MaxDeliver, and parked-message disposition stays with GS-02.

## 9. Bundle evidence

```go
type SourceMaintenanceEvidence struct {
	SchemaVersion                string
	CheckpointID                 string
	CapturePolicyDigest          string
	WriterInventoryDigest        string
	AuthorizationManifestDigest string
	LockEvidence                 []LockEvidence
	StopEvidence                 []StopEvidence
	NATSQuiescenceEvidence       []NATSQuiescenceEvidence
	SnapshotObservations         []SnapshotObservation
	BundleSealedAt               string
	EvidenceDigest               string
}
```

Bounds: 256 writer targets and locks, 1,024 principals, 256 NATS servers, 8 observations per stream, 256 printable-ASCII
characters per identity, and 4 MiB complete evidence JSON. Sort arrays by defined identity tuples. `EvidenceDigest` is
SHA-256 over RFC 8785 JCS with only its own field omitted. Full evidence stays in the content-addressed bundle; nothing
is written to a source control bucket.

## 10. Canonical JSON correction

RFC 8785 JCS is the sole authority. Object keys sort recursively; arrays retain defined semantic order; struct order is
not normative; no whitespace/trailing newline; identity strings are printable ASCII; timestamps are canonical UTC
RFC3339Nano and must parse/reformat identically; digests hash exact JCS UTF-8.

`epoch` is a JSON string matching `^[1-9][0-9]{0,19}$` and parsing as `uint64`; numeric and leading-zero epochs reject.

Exact target claim phases are `initializing`, `restoring`, `verifying`, `begin_new_rotating`, `complete_readonly`,
`failed`, and `ambiguous`. Per-stream phases are `planned`, `restore_issued`, `verified`, `adopted`, `failed_preissue`,
and `retired`. `orphan_restore_unresolved` is an error code. R31 defines no source phases.

Golden tests pin literal JCS bytes and fixed hashes for target claim/lock, source maintenance evidence, writer
inventory,
NATS quiescence evidence, and manifest stream set.

## 11. Required tests

1. No snapshot before every durable source lock is acquired; lock state survives command crash.
2. Every writer is restart-disabled/stopped; missing direct/custom inventory refuses.
3. Active writer principal or incomplete NATS server response set refuses.
4. Fresh restart remains blocked after command crash; NATS servers remain active.
5. Reappearing process/connection, changed token/generation, or disappearing server during snapshot invalidates attempt.
6. Provider adapters reject cooperative/unverifiable maintenance and prove persistent disable semantics.
7. Command imports/constructs no application runtime, calls no `Start`, has no HTTP dependency or NATS CLI dependency,
   and writes no captured source stream.
8. Recursive map reordering yields identical JCS; semantic array reorder changes digest.
9. Large string epoch round-trips; numeric/leading-zero epoch, noncanonical time, and unknown phases reject.
10. Partial bundles are unusable and locks remain held after disable begins.

## 12. Surface deleted versus r30

R31 deletes one source bucket/backing stream, two keys, eight phases, two HTTP routes, the in-process coordinator,
manager inventory adapter/mutex, participant interface/registry, four component barriers, handler admission/join,
snapshot capability, source startup-gate branch, and source credential migration—16 framework/runtime concepts.

It retains one standalone recovery binary, one offline checkpoint package, provider adapters, native snapshot/digest
logic,
and external maintenance/NATS observation evidence.

## 13. Owner rulings

1. Accept offline-only source checkpoint and removal of r30 runtime surfaces.
2. Select initially supported maintenance providers.
3. Approve writer/principal inventory and authorization-manifest source.
4. Approve system-account or monitoring permissions for connection evidence.
5. Define explicit source maintenance release procedure.
6. Set quiescence interval and evidence freshness limits.
7. Confirm target `AUTHORITY_RECOVERY` and `complete_readonly` remain unchanged.
8. Retain GS-02 as the only target activation authority.

<!-- END gs01-design-revision31-replacement.txt -->

---

<!-- BEGIN gs01-design-revision32-replacement.txt -->

# GS-01 Recovery Contract r32 — Closed Single-Node NATS Maintenance Mode

R32 replaces r31's source authorization/completeness mechanism. R27–r29 target restore/adoption and r31 CLI/JCS/phase
corrections remain normative. GS-01 adds no activation, source bucket, runtime coordinator, component, registry entry,
gateway, or NATS CLI dependency.

## 1. Supported topology

`checkpoint-source` supports exactly:

- One stopped normal NATS server and one replacement maintenance process, never concurrent.
- NATS 2.14.x, file-backed JetStream, one source account, and replicas exactly 1.
- No cluster, route, gateway, leaf node, mirror, source, auth callout, JWT/operator resolver, shared/token
  authentication,
  or mTLS-derived identity.
- A provider that fences exact writer resources, NATS supervisor, config paths, binary/argv, listeners, reload/restart
  paths, and physical store root.
- An owner-approved generated closed-template maintenance configuration.

Anything else fails preflight as `source_topology_unsupported`; it is never approximated.

The current repo has no checkpoint command or maintenance config. Local/e2e deployments are single file-store servers
(`docker/compose/e2e.yml:17-28`, `natsclient/test_client.go:236-253`) and NATS is pinned to minor 2.14
(`test/contract/nats_version_contract_test.go:12-35`).

## 2. Adopter seam

The provider-adapter operator identifies the protected deployment and store. The adapter owns provider-native identities
for writer resources, normal NATS service, supervisor, store mount, immutable maintenance config, and restart/reload
paths. The operator does not calculate subjects, stream limits, connection completeness, or timing windows.

Missing provider facts fail before normal NATS stops or a usable bundle exists. Manual promises, PID-file absence, quiet
logs, and advisory locks are unsupported. The CLI observes server, stream, connection, store, config, and supervisor
facts. Source maintenance details do not cross into the target restore API.

## 3. Exact transition

1. Acquire one durable provider lock over the complete `LockScope`; verify its fencing generation.
2. Disable, drain, and stop every declared source writer; prove each restart-disabled and stopped.
3. While normal NATS runs, record server/account/stream inventory and final physical tuples.
4. Disable normal NATS restart and every config reload path; gracefully stop its exact process.
5. Prove process exit, closed client listeners, free store lock, and no process with the store root open.
6. Materialize approved maintenance config; verify bytes, inode, owner, mode, binary/argv, and authorization digests.
7. Start exactly one maintenance NATS process on the same store using a provider-exclusive generation/start primitive.
8. Latch its server ID and verify stable name, version, domain, binary, config, listeners, supervisor, and store
   identity.
9. Observe native store recovery with two identical complete stream-name/StreamInfo passes separated by `$JS.API.INFO`.
   Require the pre-stop set, replicas 1, no mirror/source/cluster/offline/lost state, and equal config/physical tuples.
10. Open exactly the two checkpoint connections in section 4; require two identical exhaustive direct CONNZ sweeps.
11. Snapshot in canonical order. Require accepted config/state equal immediate post-snapshot StreamInfo.
12. Before payload seal, reverify provider/process/store/config and CONNZ, then reread every captured physical tuple and
    require equality with each post-snapshot accepted tuple.
13. Compute the payload seal.
14. While locks remain held, reread every tuple and CONNZ again. Any difference invalidates the candidate.
15. Only then sign and atomically publish final evidence. Locks remain held through publication or candidate discard.

A delayed commit after the pre-seal pass must yield `source_changed_after_seal` and no acceptable bundle.

## 4. Closed maintenance authorization

The generated config contains only two newly minted checkpoint-scoped NKeys:

- `recovery` in the source account: may publish only JetStream account info, stream names/list/info, message/direct get,
  stream snapshot requests, and snapshot acknowledgements; may subscribe only to checkpoint inbox/delivery prefixes.
- `monitor-admin` in the system account: may publish only direct server CONNZ requests and subscribe to its inbox.

Everything else is denied. Neither identity may publish captured stream/KV/ObjectStore subjects, restore/delete/update/
purge/reload APIs, or normal application data. No normal credential appears. The config has no includes, routes,
gateways,
leaf nodes, WebSocket/MQTT listeners, callout, resolver, token, or certificate identity mapping.

Provider fencing blocks SIGHUP, service reload, config replacement, normal restart, and a second maintenance start. A
typed closed template generates both config and authorization manifest; CONNZ verifies runtime identity only and never
infers permissions.

## 5. Direct exhaustive CONNZ

Query the one latched server directly at `$SYS.REQ.SERVER.<server-id>.CONNZ`. Page zero bytes are exactly:

```json
{"auth":true,"limit":256,"offset":0,"sort":"cid","state":0}
```

Later requests change only offset by 256. Require no API error; response server IDs equal the latched ID; offset/limit
match; first-page total remains fixed; returned counts equal `min(256,total-offset)`; pages cover every row; CIDs
strictly
increase without duplicates; and the union contains exactly recovery and monitor-admin. A second sweep must have the
same ordered `(cid,account,principal)` values.

An omitted account normalizes to `$G` only when the manifest declares that connection in the global account. Otherwise
account is required and exact. `authorized_user` is mandatory and equals the configured uppercase public NKey. Canonical
principal is `nkey:<public-nkey>`. JWT, issuer, bearer/token, certificate/client names, and aliases are never identity
inputs; missing/redacted identity rejects.

## 6. Evidence grammar

All fields are required unless nullable. Use RFC 8785 JCS. Potentially large integers are strings matching
`0|[1-9][0-9]{0,19}`. Digests are lowercase 64-hex; timestamps canonical UTC RFC3339Nano; IDs 1–128 printable ASCII;
paths canonical absolute UTF-8 without NUL and at most 4096 bytes; NATS names/subjects 1–1024 bytes and valid to the
pinned parser.

```text
SourceMaintenanceEvidence {
  schema: "semstreams.gs01.source-maintenance-evidence.v1",
  checkpointId,
  lockScope: LockScope,
  lockEvidence: LockEvidence,
  maintenanceConfig: MaintenanceConfigEvidence,
  authorization: AuthorizationManifest,
  topology: ExpectedTopology,
  nativeRecoveryPasses: [RecoveryPass, RecoveryPass],
  connzSweeps: [ConnzSweep, ConnzSweep, ConnzSweep],
  snapshots: [SnapshotObservation]{1..4096},
  payloadSeal: {algorithm:"sha256", digest, sealedAt},
  finalObservedAt
}

LockScope {
  deploymentId,
  writerResources: [ResourceRef]{1..1024},
  normalNATSResource: ResourceRef,
  supervisorResource: ResourceRef,
  normalConfigPath,
  maintenanceConfigPath,
  store: StoreIdentity,
  listeners: [ListenerRef]{1..32},
  reloadRestartPaths: [ResourceRef]{1..64}
}

ResourceRef {kind, id}
ListenerRef {network, address}

LockEvidence {
  provider,
  lockId,
  fencingGeneration: uint-string,
  acquiredAt,
  lastVerifiedAt,
  leaseExpiresAt: timestamp|null,
  stoppedWriters: [ResourceObservation]{same cardinality as writerResources},
  normalServerStopped: ResourceObservation,
  secondServerExcluded: true,
  providerAttestationDigest
}

ResourceObservation {
  resource: ResourceRef,
  generation: uint-string,
  restartDisabled: true,
  activeProcessCount: "0",
  processIds: []
}

StoreIdentity {
  providerVolumeId,
  hostOrNamespaceId,
  canonicalPath,
  filesystemId,
  deviceId: uint-string,
  rootInode: uint-string,
  mountGeneration: uint-string
}

MaintenanceConfigEvidence {
  templateId,
  approvalId,
  path,
  sha256,
  sizeBytes: uint-string,
  uid: uint-string,
  gid: uint-string,
  mode: "0400"|"0440",
  deviceId: uint-string,
  inode: uint-string,
  binaryPath,
  binarySha256,
  argvSha256,
  authorizationManifestSha256
}

AuthorizationManifest {
  sourceAccount,
  systemAccount,
  principals: [Principal]{exactly 2},
  normalCredentialFingerprints: [sha256]{0..1024},
  permissions: [Permission]{1..128},
  defaultDeny: true
}

Principal {role:"recovery"|"monitor-admin", account, kind:"nkey", id:"nkey:<public-nkey>"}
Permission {principalId, operation:"publish"|"subscribe", subject}

ExpectedTopology {
  mode:"offline_single_node",
  servers:[ExpectedNATSServer]{exactly 1}
}

ExpectedNATSServer {
  normalServerId,
  maintenanceServerId,
  serverName,
  version,
  jetstreamDomain,
  clientListener,
  normalProcessGeneration: uint-string,
  maintenanceProcessGeneration: uint-string,
  store: StoreIdentity,
  maintenanceConfigSha256
}

RecoveryPass {
  ordinal:"1"|"2",
  observedAt,
  serverId,
  streams:[RecoveredStream]{1..4096}
}

RecoveredStream {account, stream, configSha256, physical:PhysicalTuple}

PhysicalTuple {
  messages, bytes, firstSeq, firstTime, lastSeq, lastTime,
  consumerCount, numDeleted, deletedSeqDigest, lostBytes, lostSeqDigest
}

SnapshotObservation {
  ordinal: uint-string,
  role:"kv"|"object_store",
  account,
  logicalName,
  physicalStream,
  acceptedConfigSha256,
  acceptedPhysical: PhysicalTuple,
  postSnapshotPhysical: PhysicalTuple,
  preSealPhysical: PhysicalTuple,
  postSealPhysical: PhysicalTuple,
  archiveSha256,
  archiveBytes: uint-string,
  chunkCount: uint-string,
  rawMessageDigest
}

ConnzSweep {
  phase:"pre_snapshot_1"|"pre_snapshot_2"|"post_seal",
  observedAt,
  serverId,
  total: uint-string,
  pages: uint-string,
  connections:[ConnectionEvidence]{exactly 2}
}

ConnectionEvidence {cid:uint-string, account, principalId, role}
```

`firstTime` and `lastTime` are nullable only for zero messages. Lost bytes must be zero with empty-sequence digest;
actual
lost state is unsupported. Both normal and maintenance server IDs are mandatory but need not match.

Before JCS: resources sort `(kind,id)`; listeners `(network,address)`; principals `(account,role,kind,id)`; permissions
`(principalId,operation,subject)`; streams/snapshots `(account,physicalStream)`; connections numeric CID;
deleted/lost sequence inputs numeric before newline-decimal digest. Fixed-phase arrays retain declared order.

Normative primitive golden bytes include the exact CONNZ body and:

```json
{"bytes":"0","consumerCount":"0","deletedSeqDigest":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855","firstSeq":"0","firstTime":null,"lastSeq":"0","lastTime":null,"lostBytes":"0","lostSeqDigest":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855","messages":"0","numDeleted":"0"}
```

A full-envelope byte-for-byte golden fixture is mandatory before implementation acceptance.

## 7. Failure tests

1. Reject normal credentials during maintenance; recovery cannot publish `$KV.>`, `$O.>`, or captured subjects.
2. Reload, normal restart, second server, config/store inode, or supervisor-generation changes invalidate.
3. Missing/extra/offline/lost stream or incomplete native recovery rejects.
4. CONNZ wrong server/directness, changing totals, offsets/page lengths, duplicate/omitted rows, redacted auth, account
   mismatch, unexpected connection, or CID churn rejects.
5. Snapshot accepted/post tuple mismatch rejects.
6. Delayed commit between pre- and post-seal reads invalidates.
7. Locks remain held through final evidence publication or candidate discard.

## 8. Explicit exclusions

Clustered JetStream and any provider unable to prove exact maintenance config and store/supervisor identity are
unsupported. Future cluster support requires peer-wide fencing, split-brain prevention, preserved identities/routes,
quorum-safe maintenance, Raft leader/replica-current/lag-zero evidence, direct CONNZ for every peer, per-peer
attestations,
stable topology through seal, and leader-change-aware acceptance. None is implied here.

## 9. Surface and rulings

The retained design is one standalone CLI with three subcommands, one internal recovery package, one provider adapter,
and one evidence envelope. It adds zero source buckets, coordinators, runtime components, registries, or runtime
subjects.

Owner rulings:

1. Approve offline single-node initial support.
2. Select provider adapters and exact closed config template.
3. Select public-key approval mechanism.
4. Decide whether global-account sources are admitted after store-reopen integration proof.

<!-- END gs01-design-revision32-replacement.txt -->

---

<!-- BEGIN gs01-design-revision33-addendum.txt -->

# GS-01 Recovery Contract r33 — Raw State, Permissions, Observer, and Final Envelope

This addendum changes only four r32 details. All r32 simplifications, exclusions, target inheritance, and zero-runtime-
surface result remain normative.

## 1. Raw physical observations

Retain and digest raw StreamInfo and snapshot envelopes; typed `nats.go` state is never evidence. For every captured
stream request exactly:

```text
subject: {P}.STREAM.INFO.<stream>
body:    {"deleted_details":true}
```

Enforce a raw-envelope bound; classify top-level API error before success fields; strictly parse pinned v2.14 success;
reject duplicate/unknown keys, invalid UTF-8, trailing bytes, malformed values, impossible state, and missing fields;
preserve original bytes and digest; then map raw config/state:

```text
PhysicalTuple {
  configJCSSha256 = SHA256(JCS(raw.config)),
  messages        = raw.state.messages,
  bytes           = raw.state.bytes,
  firstSeq        = raw.state.first_seq,
  firstTime       = raw.state.first_ts,
  lastSeq         = raw.state.last_seq,
  lastTime        = raw.state.last_ts,
  consumerCount   = raw.state.consumer_count,
  numDeleted      = raw.state.num_deleted,
  deletedGapSha256,
  lostBytes       = raw.state.lost.bytes or 0 only when lost is absent,
  lostGapSha256,
  rawEnvelopeSha256
}
```

Zero deleted requires absent/empty `deleted`. Returned details are unique, ascending, in range, and cardinality equals
`num_deleted`; hash inclusive gap lines such as `2-2\n4-6\n`. If detail is absent/incomplete/oversized, the existing
bounded raw `MSG.GET` traversal derives found sequences and deletion gaps. Retain every raw response, require returned
sequence equals requested, and reconcile missing cardinality to `num_deleted`; inability to finish is unsupported.

Preserve `lost` independently. Absent means zero/empty digest. Present `lost.msgs` uses the same gap encoding. Any
nonzero lost bytes/sequence is recorded then rejects `source_stream_actual_loss`.

Snapshot responses follow the same raw discipline. They may reuse adjacent deletion evidence only when `num_deleted`,
first/last sequence, and raw message digest match exactly.

Tests cover no loss, actual loss, sparse deletion, missing/incomplete details, typed-decoder regression,
unknown/duplicate
keys, malformed error/state, oversized envelope, out-of-range details, and count mismatch.

## 2. Exact domain-aware permissions

```text
{P} = "$JS.API"                   for empty observed domain
{P} = "$JS.<observed-domain>.API" otherwise
```

`<S>` ranges only over exact sorted captured physical streams; never substitute a wildcard.

Recovery NKey allows publish only:

```text
{P}.INFO
{P}.STREAM.NAMES
{P}.STREAM.INFO.<S>
{P}.STREAM.MSG.GET.<S>
{P}.STREAM.SNAPSHOT.<S>
$JS.SNAPSHOT.ACK.<S>.>
```

It subscribes only to `_INBOX.GS01.<checkpoint-token>.REC.>` and
`_INBOX.GS01.<checkpoint-token>.SNAP.>`.

Monitor-admin publishes only `$SYS.REQ.SERVER.*.CONNZ` and subscribes only to
`_INBOX.GS01.<checkpoint-token>.MON.>`. Runtime requests still address the exact latched server; wildcard permission is
needed because its ID is created at maintenance start. Remove STREAM.LIST and direct-get permissions. Deny all unlisted
operations.

Snapshot delivery is always under:

```text
_INBOX.GS01.<checkpoint-token>.SNAP.<stream-token>.<nonce>
```

Before snapshotting, use pinned server subject-collision semantics against every persisted stream subject for recovery/
monitor inboxes, delivery prefixes, snapshot ACK, snapshot create/complete advisories, and allowed `{P}` requests. Any
collision fails `checkpoint_subject_persisted_collision`.

The allowlist proves maintenance clients cannot directly publish source data. It cannot constrain server-originated
snapshot chunks/replies/advisories chosen through `deliver_subject`; immutable selection plus collision preflight
protects
that path. Tests pin empty/named-domain matrices, reject one extra permission and all collisions, and prove direct data
publish denial.

## 3. Pre-stop observer

Initial support requires a preprovisioned read-only NKey in normal NATS; no provider-introspection fallback.

Before disruptive locking or writer stop, connect and verify normal-config SHA-256, observer public-key fingerprint, and
permission-manifest SHA-256; observe `{P}`; issue `{P}.INFO`, exhaustive `{P}.STREAM.NAMES`, and exact
`{P}.STREAM.INFO.<S>`; and prove no snapshot, ACK, mutation, purge, restore, or stream-data publish permission.

Observer publish allowlist is the prior three request classes plus `{P}.STREAM.MSG.GET.<S>` only when raw traversal is
required. It subscribes only to its immutable observer inbox and performs no write/snapshot.

After writers stop, the same observer performs authoritative pre-stop raw observations. Evidence adds:

```text
PreStopObserverEvidence {
  normalConfigSha256,
  observerPublicNKeySha256,
  observerPermissionManifestSha256,
  observedAPIPrefix,
  connectionAuthorizedUser,
  preflightObservedAt,
  finalObservationAt
}
```

Absent credentials, fingerprint/permission drift, or raw-observation failure rejects before writer shutdown.

## 4. Four CONNZ phases and final signing

Each phase contains two exhaustive CID-sorted passes with identical ordered connection tuples:

1. `post_native_recovery`.
2. `pre_first_snapshot` after collision/permission preflight.
3. `pre_payload_seal` after snapshots and all pre-seal tuple reads.
4. `post_payload_seal` after payload seal and all post-seal tuple reads.

Exact order:

```text
native recovery
→ CONNZ 1
→ collision/permission preflight
→ CONNZ 2
→ snapshots and immediate raw observations
→ reread every physical tuple
→ CONNZ 3
→ compute payload seal
→ reread every physical tuple again
→ reverify lock/config/server/store
→ CONNZ 4
→ construct and sign final envelope
→ atomically publish valid bundle
```

Payload seal binds only JCS `SnapshotPayloadManifest`: canonical `(account, physicalStream, archiveSha256, archiveBytes,
chunkCount)` rows plus referenced archive bytes. It does not bind evidence.

```text
FinalSignedSourceEnvelope {
  schema: "semstreams.gs01.final-signed-source-envelope.v1",
  sourceMaintenanceEvidenceSha256,
  payloadSeal: {algorithm:"sha256", snapshotPayloadManifestSha256},
  postSealEvidence: {
    physicalTuplesSha256,
    connzPhaseSha256,
    lockEvidenceSha256,
    observedAt
  },
  keyId,
  alg:"Ed25519",
  signature
}
```

Signature is unpadded base64url. Sign RFC 8785 JCS of the complete object with only `signature` omitted; schema, keyId,
and alg remain.

Normative signing vector:

```text
seed hex: 000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f
public key base64url: A6EHv_POEL4dcN0Y50vAmWfk1jCbpQ1fHdyGZBJVMbg
signature base64url: mnqv7YPGo9H7pKo5GbvpoSeW4VIQpwqMsLDj-GNfNhgqlA-QsAfMBAt5YVRpeiKq8xhOEQAgbvH-0qqEYBMMBQ
signature hex: 9a7aafed83c6a3d1fba4aa3919bbe9a12796e15210a70a8cb0b0e3f8635f36182a940f90b007cc040b796154697a22aaf3184e1100206ef1fed2aa8460130c05
```

Unsigned literal JCS bytes:

```json
{"alg":"Ed25519","keyId":"test-ed25519-01","payloadSeal":{"algorithm":"sha256","snapshotPayloadManifestSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"postSealEvidence":{"connzPhaseSha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","lockEvidenceSha256":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","observedAt":"2026-08-05T12:00:00Z","physicalTuplesSha256":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"},"schema":"semstreams.gs01.final-signed-source-envelope.v1","sourceMaintenanceEvidenceSha256":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"}
```

Full signed JCS bytes:

```json
{"alg":"Ed25519","keyId":"test-ed25519-01","payloadSeal":{"algorithm":"sha256","snapshotPayloadManifestSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"postSealEvidence":{"connzPhaseSha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","lockEvidenceSha256":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","observedAt":"2026-08-05T12:00:00Z","physicalTuplesSha256":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"},"schema":"semstreams.gs01.final-signed-source-envelope.v1","signature":"mnqv7YPGo9H7pKo5GbvpoSeW4VIQpwqMsLDj-GNfNhgqlA-QsAfMBAt5YVRpeiKq8xhOEQAgbvH-0qqEYBMMBQ","sourceMaintenanceEvidenceSha256":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"}
```

Tests verify the literal vector and reject signature-present signing input, changed payload/evidence, non-JCS bytes,
missing phase 4, post-seal tuple change, and signing before post-seal verification.

## 5. Finding disposition

- Raw-state loss: exact bounded envelopes, error-first parsing, deletion reconciliation/traversal, preserved lost state.
- Permission gap: exact `{P}`/stream allowlist, immutable delivery prefixes, collision preflight, honest direct-publish
  claim.
- Missing pre-stop authority: preprovisioned read-only NKey and fail-before-stop verification.
- Ambiguous finalization: four CONNZ phases, payload-only seal, post-seal evidence, final signed envelope/vector.

Clustered JetStream and every other r32 exclusion remain unsupported.

<!-- END gs01-design-revision33-addendum.txt -->

---

<!-- BEGIN gs01-design-revision34-addendum.txt -->

# GS-01 Recovery Contract r34 — Bootstrap Census and Owned Snapshot Wire

This corrects five r32+r33 mechanics only. It adds no runtime surface, bucket, subject family, component, coordinator, or
provider API. All single-node boundaries and exclusions remain.

## 1. Exact normalized config/state digest

Never JCS-hash decoded NATS config/state directly. Parse raw v2.14 JSON with number-token preservation. Reject duplicate/
unknown fields; invalid integral spellings; and width/sign overflow. Map each pinned `int`, `int32`, `int64`, `uint64`,
duration/nanosecond, and nested integral field to a canonical decimal JSON string before JCS. Never use `float64`.
Preserve booleans/strings, canonicalize timestamps, and recursively map placement, source, republish, transform, limit,
metadata-bearing nested structures, and state `first_seq`.

`NormalizedStreamConfigV214` mirrors every pinned v2.14 StreamConfig field. `NormalizedStreamStateV214` mirrors messages,
bytes, first sequence/time, last sequence/time, subject counts/details, deleted/lost state, and consumer count. Unmappable
numbers fail `nats_numeric_contract_unsupported`.

Normative distinct vectors:

```text
{"max_bytes":"9007199254740992","name":"S"}
09f2b42e7cdd0c40fb54666948bf745b80d8a6c9cacf5349d680f37ce4401711
{"max_bytes":"9007199254740993","name":"S"}
d5aaa0d482a0d6240168781592d5b753612cbb80fabbced294cff1058e2a1f14
```

Tests preserve this distinction through raw parse/normalization/JCS/digest and reject overflow or generic-number decode.

## 2. Bootstrap collision census before any inbox

Normal and maintenance configs expose pinned NATS 2.14 HTTP monitoring only at provider-controlled loopback:

```text
http_host: 127.0.0.1
http_port: <provider-pinned>
http_base_path: /
```

The port is never published/proxied. Provider proves the socket belongs to the exact NATS process generation. Before any
NATS connection, subscription, inbox, or delivery subject, mint the token, reserve `_INBOX.GS01.<token>.>`, and issue
inside that namespace with redirects/proxies disabled:

```http
GET http://127.0.0.1:<port>/jsz?accounts=true&config=true&consumers=false&direct-consumers=false&leader-only=false&limit=1&offset=0&raft=false&stream-leader-only=false&streams=true
```

Require loopback literal, exact namespace/process, five-second deadline, HTTP 200 JSON, body at most 64 MiB, and one strict
pinned-v2.14 object. Require JetStream enabled, exactly one account row/source account, top-level stream count equals that
row's unpaged `stream_detail`, and every detail has unique matching nonnil config/name. A second account is unsupported.

Normalize/digest all configs with section 1. Check every persisted subject with pinned collision semantics against the
reserved inbox and all r33 API, ACK, delivery, and advisory subjects. Only then may the pre-stop observer connect.

Order:

```text
acquire provider lock
→ normal /jsz census and collision proof
→ observer preflight
→ stop/fence writers and final observations
→ stop normal NATS
→ start maintenance NATS
→ maintenance /jsz census and repeated collision proof
→ create stable recovery and monitor connections
→ native recovery and r33 sequence
```

Maintenance census must equal normal account, stream names, normalized config digests, and subject patterns. No store
internals are inspected. Missing proof fails `bootstrap_subject_census_unavailable`.

Tests reject NATS request before census, second account, missing config, count mismatch, duplicate stream, non-loopback/
external monitor, redirect/proxy, oversized body, restart config drift, and inbox collision.

## 3. Stable tuple and raw evidence

`PhysicalTuple` contains only normalized facts:

```text
configJCSSha256 messages bytes firstSeq firstTime lastSeq lastTime
consumerCount numDeleted deletedGapSha256 lostBytes lostGapSha256
```

Raw transport hashes live outside equality:

```text
RawObservationEvidence {
  phase,
  kind:"jsz"|"stream_info_base"|"stream_info_detail"|"msg_get"|"snapshot_response",
  account,
  stream,
  requestSubject,
  requestBodySha256,
  rawEnvelopeSha256,
  rawEnvelopeBytes,
  physicalTupleSha256
}
```

Signed evidence binds every row and retained bytes. Physical equality ignores transport whitespace/member order and
response timestamps. Tests prove byte-distinct equivalent envelopes share a tuple but not raw hash, and altering either
breaks signed evidence.

## 4. Deletion bounds before materialization

First StreamInfo is always `{P}.STREAM.INFO.<stream>` body `{}`, max response 8 MiB. Parse error-first and inspect
`num_deleted`/`lost` before detail.

```text
maxBaseInfoEnvelope     = 8 MiB
maxDetailedInfoEnvelope = 64 MiB
maxSnapshotResponse     = 8 MiB
maxMsgGetEnvelope       = 64 MiB per sequence
maxDeletedDetailCount   = 100000
maxRawSequenceTraversal = 10000000 sequences
```

Request deletion details only when count <=100000 and `baseBytes + count*22 <= 64 MiB`; hard-cap actual response. Above
either threshold, never request details. Traverse raw MSG.GET from first through last sequence, retaining responses and
deriving missing ranges. Reject when span exceeds ten million, response exceeds cap, returned sequence mismatches,
non-pinned not-found error occurs, or missing count differs: `deletion_proof_unsupported`.

Read lost state from base response first. Preserve and reject any actual lost data. Snapshot responses cap at 8 MiB and
use the same normalized schema. Never truncate.

Tests cover threshold edges, detail skip, oversize, traversal 10,000,000/10,000,001, sparse gaps, mismatch, unexpected
errors, and actual loss.

## 5. Owned source snapshot wire adapter

Source capture does not use JSM `SnapshotToDirectory`. `internal/recovery` owns one pinned v2.14 adapter using the one
already-counted recovery connection for request, delivery, and ACK; no extra CONNZ identity.

For each canonical stream:

1. Subscribe to `_INBOX.GS01.<token>.SNAP.<stream-token>.<nonce>`.
2. Publish exact JCS request to `{P}.STREAM.SNAPSHOT.<S>`:

```json
{"chunk_size":131072,"deliver_subject":"_INBOX.GS01.<checkpoint-token>.SNAP.<stream-token>.<nonce>","jsck":true,"no_consumers":false,"window_size":8388608}
```

3. Retain raw response, cap 8 MiB, classify error first, strictly map pinned type/error/config/state.
4. Open exclusive no-symlink `.partial` beneath locked checkpoint directory.
5. For each full chunk require exact `$JS.SNAPSHOT.ACK.<S>.<nuid>.<size>.<index>`, consecutive index, and declared size;
   write/hash before empty ACK.
6. Final nonempty partial chunk has no reply; write/hash without ACK.
7. Require following zero-length terminal, no reply, NATS status 204. Reject 408/500, missing terminal, order/duplicate/
   reply errors, or post-terminal data.
8. Fsync file, close, atomically rename, fsync directory, and record bytes/hash.

Exact r33 permissions suffice. Collision proof covers delivery, ACK, and snapshot advisories. Restore may retain a helper
only when archive-compatible and target raw acceptance/digest rules remain.

Tests pin exact request, one connection, ACK-after-write, partial/no-ACK, terminal statuses, size/index/reply/order errors,
`.partial` interruption, rename/fsync, byte/hash equality, raw retention, and no extra identity.

## 6. Pinned strict fields

Strict parsing allows only actual pinned v2.14 fields:

- StreamInfo top-level: type, error, total, offset, limit, config, created, state, domain, cluster, mirror, sources,
  alternates, ts.
- Snapshot: type, error, config, state.
- MSG.GET: type, error, message; stored message: subject, seq, hdrs, data, time.
- State: messages, bytes, first_seq/ts, last_seq/ts, num_subjects, subjects, num_deleted, deleted, lost, consumer_count.
- `/jsz`: exact fields reachable from pinned JSInfo, AccountDetail, StreamDetail, JetStreamStats, and StreamConfig under the
  fixed options.

Known omitempty fields may be absent. Future fields reject until NATS pin and normalization schema are reviewed together.

## 7. Finding disposition

- Numeric collapse: width-aware raw parsing and decimal-string normalization.
- Inbox bootstrap paradox: provider-local loopback `/jsz` census before any NATS request, repeated after restart.
- Tuple instability: normalized facts separated from signed raw evidence.
- Deletion allocation risk: base-first exact caps, conditional detail, bounded traversal.
- Hidden helper connection/raw loss: owned one-connection native snapshot adapter.

<!-- END gs01-design-revision34-addendum.txt -->

---

<!-- BEGIN gs01-design-revision35-addendum.txt -->

# GS-01 Recovery Contract r35 — Non-Mutating Loss Proof and Snapshot Flags

This supersedes only conflicting r34 sentences and adds no surface.

## 1. Detailed state is the only loss/deletion proof

Initial `{P}.STREAM.INFO.<stream>` body `{}` returns v2.14 FastState. It supplies counts/bounds but not `lost`:

```text
base response lost absent = unknown
```

Never normalize it to zero. Request `{"deleted_details":true}` to prove both absent/zero `lost` and a complete deletion
list matching `num_deleted`, only when:

```text
num_deleted <= 100000
baseEnvelopeBytes + num_deleted*22 <= 64 MiB
```

If either fails, return `deletion_proof_unsupported` without sending detail. MSG.GET traversal may produce a separately
required message digest but can never prove loss absence. Preserve/validate detailed `lost`; actual loss rejects
`source_stream_actual_loss`. Missing `lost` in the detailed State response is the supported zero-loss representation.

Tests keep base omission unknown, prove zero/actual loss through safe detail, reject above thresholds without detail, and
prove traversal cannot promote unknown to absent.

## 2. Exact snapshot request

```json
{"chunk_size":131072,"deliver_subject":"_INBOX.GS01.<checkpoint-token>.SNAP.<stream-token>.<nonce>","jsck":false,"no_consumers":true,"window_size":8388608}
```

Archives exclude consumers and never request the potentially mutating server checksum scan.

## 3. Exact terminal framing

After nonempty chunks, accept only a reply-less, zero-length terminal whose status is absent or exactly 204. Reject every
other status, any reply, or nonzero payload.

Tests cover a small partial archive followed by a headerless terminal; one or more ACKed full chunks followed by exact
204; and non-204, reply-bearing, or nonempty terminal rejection.

<!-- END gs01-design-revision35-addendum.txt -->
