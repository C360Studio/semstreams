# Foundation B port-language control artifacts

This control plane freezes the migration population accepted for Foundation B at tracked baseline
`61022ae1b4da0309e93ce49ec00c9c64679d09d8`. It does not implement the new port grammar and it does not rewrite a
checked-in configuration.

## Authority

The trusted migration sources are:

- `foundation-b-port-language-worklist.tsv` — every current shipped configuration port row and every executable Go
  `component.PortDefinition` construction;
- `foundation-b-port-language-dispositions.tsv` — the reviewed decision for every non-mechanical configuration row.

The worklist is path based and deterministically ordered by `record_id`. Configuration identities use repository path
plus RFC 6901 JSON pointer and include the compact current row plus its SHA256. Go identities use repository path plus
the resolved composite literal's line and column and include its enclosing function or method, exact source fragment,
and SHA256. Duplicate JSON rows are separate work items because their paths and pointers differ.

The worklist schema is:

```text
record_id record_type path pointer enclosing lane ordinal name current_kind current_data classification source_line source_column source_sha256
```

The disposition schema is:

```text
record_id path pointer action target_lane target_kind target_data reason
```

Both files are TSV. Their leading `#` records name the schema, baseline, authority, and frozen counts. There is no
generation command: the worklist and dispositions are immutable owner-accepted input. Changing either requires an
explicitly reviewed replacement, not regeneration from current code.

## Reproduced population

The checked-in population is:

- 24 configuration documents;
- 522 configuration rows: 448 mechanical and 74 adjudicated;
- 74 dispositions: 57 `kv`, nine top-level `kv-read`, and eight `http`;
- two reviewed graph-query `ENTITY_STATES` rows deleted;
- seven reviewed agentic-tools `ENTITY_STATES` rows moved to ordinary `kv-read` inputs;
- 124 executable Go `PortDefinition` literals across 34 production files and 41 enclosing functions or methods.

The earlier “45 Go defaults” premise was rejected. It cannot be reproduced from the accepted baseline. The corrected
census uses `golang.org/x/tools/go/packages` with tests disabled and
`NeedName|NeedFiles|NeedCompiledGoFiles|NeedSyntax|NeedTypes|NeedTypesInfo`. A composite literal is included only when
its resolved named type is `github.com/c360studio/semstreams/component.PortDefinition`. The reflection sentinel at
`component/schema_tags.go:705` is explicitly excluded. This produces 124 literals, 34 files, and 41 enclosing sources;
no invented grouping is used to reach 45.

## Cutover status

The cutover retains the two TSV files as the immutable historical migration record. The target test accounts for all
646 ledger identities, requires production decoding and resolution for all 520 surviving configuration rows, accounts
explicitly for the two approved graph-query deletions, and verifies the 124 frozen Go constructions plus eleven approved
checkpoint additions. The full-repository AST census, rather than the frozen path list, proves that no additional
production `PortDefinition` construction is hidden outside this population.

The eleven additions are deliberately recorded here rather than added to the immutable baseline:

| File | Name | Kind | Direction | Resource | Reason |
|---|---|---|---|---|---|
| `processor/graph-clustering/component.go` | `entity_states` | `KVReadPort` | input | `ENTITY_STATES` | truthful existing read |
| `processor/graph-clustering/component.go` | `outgoing_index` | `KVReadPort` | input | `OUTGOING_INDEX` | truthful existing read |
| `processor/graph-clustering/component.go` | `incoming_index` | `KVReadPort` | input | `INCOMING_INDEX` | truthful existing read |
| `processor/agentic-tools/config.go` | `entity_states` | `KVReadPort` | input | `ENTITY_STATES` | truthful existing read |
| `processor/agentic-tools/config.go` | `agent_loops` | `KVReadPort` | input | `AGENT_LOOPS` | truthful existing read |
| `input/http/http.go` | `http_schedule` | `TimerPort` | input | configured polling interval | cadence sibling required by `HTTPClientPort` |
| `input/http/http.go` | `http_source` | `HTTPClientPort` | input | configured method and URL | constructor-owned external source |
| `processor/gated-dag/component.go` | `dispatch` | `JetStreamPort` | output | configured stream and subject | truthful durable dispatch path |
| `processor/gated-dag/component.go` | `graph_mutations` | `NATSRequestPort` | output | canonical graph-mutation family and interface | truthful request path |
| `input/file/file.go` | `file_source` | `FilePort` | input | configured path | constructor-time replacement of a renderer literal |
| `storage/objectstore/component.go` | `store-provide` | `StoreProvidePort` | output | configured instance | constructor-time replacement of a renderer literal |

The last two were invisible to the original `PortDefinition` census because they were direct runtime `Port` literals.
They add no knobs: each is derived from existing owner configuration, resolved once during construction, stored, and
returned unchanged. The runtime-completeness guard now rejects runtime `Port` literals in every shipped renderer.

Owner review also admitted two narrowly scoped facts projections required to remove downstream concrete assertions:
immutable `NetworkFacts` through `PortFacts.Network()` (`Protocol`, `Host`, and `Port`) and
`PortFacts.StoreReadBucket()`. Classification and validation remain owned by the canonical binding table; consumers
only read normalized facts and cannot reinterpret concrete port configs.

The `http_schedule`/`http_source` pair is derived from the same validated interval used by the runtime ticker.
`http_source.trigger_port` names that sibling, so discovery cannot advertise an HTTP polling dependency without its
actual cadence.

The owner explicitly accepted the checkpoint-4 shape-gate addition `PatternRead InteractionPattern = "read"`.
Exact/list-only `KVReadPort` now projects `PatternRead`, while `KVWatchPort` alone projects `PatternWatch`. Flowgraph
still connects writers to both dependency kinds by canonical bucket identity, but exact readers are no longer reported
as watchers or replay consumers. Registry and manager reporting expose the same canonical `read` value; it is not an
alias for watch and does not imply notifications or replay.

The owner also accepted gated-DAG as the sole specialized physical provisioner for its dispatch stream. Its canonical
`JetStreamPort` declaration is discovery/flow truth for kind, stream, subject, storage, and work-queue retention. The
component-local provisioner additionally owns byte-exact `MaxBytes`, discard-new behavior, `MaxAge`, and deduplication
because the generic GiB/day declaration cannot represent those exact policies. This is an explicit checkpoint-4
binding deviation, not authority for another consumer to infer or provision those settings.

The mandatory post-B component-authorship inventory must revisit pre-existing external-boundary omissions such as
`output/httppost` having no outbound HTTP declaration. It is recorded here so renderer closure is not misread as proof
that every older component already has a complete authorship contract.

That inventory must also revisit `gateway/graph-gateway`. In its normal shipped mode the gateway registers routes on
ServiceManager's shared HTTP mux, but its current input declaration advertises an exclusive `NetworkPort` listener
derived from `bind_address`; its single `queries` output also does not enumerate every NATS request family the gateway
emits. Correcting either surface is an intentional adopter-visible break and is deferred pending an explicit owner
ruling. Checkpoint 4 does not treat those existing declarations as accurate component-authorship precedent.

The one-shot live census, production rewriter, and CLI were retired after that target gate passed. They are not an
ongoing framework subsystem. Test-local helpers retain the ledger-to-target accounting and the proof that mechanical
configuration rewriting changed only ledger-owned `ports` objects.
