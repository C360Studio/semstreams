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
explicitly for the two approved graph-query deletions, and verifies the 124 frozen Go constructions plus the five
approved read declarations.

The one-shot live census, production rewriter, and CLI were retired after that target gate passed. They are not an
ongoing framework subsystem. Test-local helpers retain the ledger-to-target accounting and the proof that mechanical
configuration rewriting changed only ledger-owned `ports` objects.
