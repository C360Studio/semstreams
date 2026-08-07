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

The cutover retains the two TSV files as the immutable historical migration record. The owner-approved graph-gateway
amendment does not rewrite either TSV. The target test accounts for all 646 frozen ledger identities, requires
production decoding and resolution for 512 surviving frozen configuration rows, and records ten approved deletions:
the two graph-query rows already retired plus the eight graph-gateway listener inputs retired by the amendment. Sixteen
new graph-gateway output rows bring the actual canonical configuration population to 528.

The production AST census now requires 136 `PortDefinition` identities. The amendment replaces the five frozen
graph-gateway Go identities with six canonical constructions: one contract and one default declaration for each of the
three required query families. Together with the eleven earlier checkpoint additions below, the accounting is
`124 - 5 + 6 + 11 = 136`. The full-repository AST census, rather than the frozen path list, proves that no additional
production construction is hidden outside this population.

### Owner-approved graph-gateway amendment

Graph-gateway owns no composition input in shared-mux mode. Its input declaration set is empty, and startup rejects
any configured input. `bind_address` remains only the standalone development/test server setting; it is not a
`NetworkPort` composition claim.

The output contract is exactly three required `nats-request` ports:

| Name | Subject family |
|---|---|
| `graph_queries` | `graph.query.*` |
| `graph_index_queries` | `graph.index.query.*` |
| `agentic_queries` | `agentic.query.*` |

Startup rejects the legacy `queries` name and every missing, duplicate, extra, optional, wrong-kind, or malformed
family declaration. There is no auto-fill, alias, or compatibility shim. Valid configured family overrides remain the
runtime routing authority after canonical port resolution.

The eight amended shipped configurations are `configs/e2e-structural.json`, `configs/hello-world.json`,
`configs/protocol-flow.json`, `configs/semantic-8b.json`, `configs/semantic-frontier.json`, `configs/semantic.json`,
`configs/statistical.json`, and `configs/structural.json`. External configurations that retain an input, use `queries`,
or omit any required output fail startup until migrated. Full release validation, including the relevant breaking-change
E2E tier, remains checkpoint 5 and is not discharged by the Foundation B target guard.

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

The post-B component-authorship inventory no longer carries a graph-gateway deferral. The owner-approved amendment
above is the binding disposition for that surface; checkpoint 5 still owns full release and E2E evidence.

The one-shot live census, production rewriter, and CLI were retired after that target gate passed. They are not an
ongoing framework subsystem. Test-local helpers retain the ledger-to-target accounting and the proof that mechanical
configuration rewriting changed only ledger-owned `ports` objects.
