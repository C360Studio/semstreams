# Inventory: honor-predicate-datatype (#1267)
base: e5233bd18f4bceed6874389ccaaf1fbb943b0ab7

**Pins refreshed 2026-09-09** at implementation time, from the original base `232b1e7d` to `40b8bcf7` — 76 pins
MOVED (line numbers only) and 16 DRIFT + 2 AMBIGUOUS re-derived by hand, the drifted ones because this change
rewrote the very lines they pin: the validator signature, its two call sites, the `Role` doc comment whose false
consumer claim addendum A4 recorded, `classifyWithExplicitDatatype`, and the call sites and assertions migrated
from a legacy spelling to a constant. Verified 150/150 ok, 0 MOVED, 0 AMBIGUOUS, 0 DRIFT.

**Pins refreshed again 2026-09-10** (task 10.10), base `40b8bcf7` → `e5233bd1`, after the section 10 repairs and a
rebase onto `29187077`. 40 MOVED applied mechanically; 5 DRIFT re-derived by hand — the validator signature moved
to `:423` and lost its pointer, and its two call sites at `:339`/`:402` now pass the value.

Two pins needed more than a line number, and both are worth recording because they are failure modes of pin
refreshing rather than of this change. Written as prose, not as list items: the verifier parses any line beginning
with `-` as a pin and reports UNPARSED when it is not one, so a bulleted note here turns the file's own gate red.
That is exactly what happened on the first attempt at this paragraph.

First, a mechanical MOVED pass can chain. `450` to `452` and `452` to `454` were applied in sorted order, so the pin
the first rewrite had just moved to `452` was caught by the second and pushed on to `454`, leaving two pins on one
line. Only the verifier's re-run found it. Apply MOVED rewrites against the ORIGINAL text, never against the running
result.

Second, `vocabulary/registry.go:554` was never uniquely pinnable. Its text `if meta.IsSymmetric` is a substring of
both `if meta.IsSymmetric && meta.InverseOf != ""` at `:433` and the same statement nested one level deeper at
`:628`, and the verifier matches substrings, so it reported AMBIGUOUS at every base it was checked against.
Re-anchored to the enclosing `func GetInversePredicate` at `:547`, which is unique. A pin whose text cannot be
unique is a pin on the wrong anchor, not a verifier complaint to re-derive on every refresh.

Verified 150/150 ok, 0 MOVED, 0 AMBIGUOUS, 0 DRIFT, 0 UNPARSED — `bash scripts/inventory-verify.sh <this file>`
exit 0.

Scope: `vocabulary.PredicateMetadata.DataType` / `.Units` / `.Range` — declaration, every writer, every reader
(present and absent), the export path that should honor them, the ingest/storage path for the architect's item-7
fork, the guard surface, doc homes, Tier 1 freeze context, the named downstream reader (#1261), and a read-only
sister census. Enumerate-only; no judgment. Every bullet under `## Claimed gap`, `## Spellings of the fact`,
`## Consumers`, and `## Problem shape` is a strict `` `path:line` — `text` `` pin, verified against `base` with
`task inventory:verify -- openspec/changes/honor-predicate-datatype/inventory.md` (0 MOVED, 0 AMBIGUOUS, 0 DRIFT,
0 MALFORMED, 0 UNPARSED — 76 pins, 76 ok). Tables and prose paragraphs (no leading `-`) carry counts,
distributions, and cross-references the pin grammar can't express as a single line; the verifier ignores them by
design (only bulleted lines are checked).

## Claimed gap

Issue #1267's evidence, each line independently re-verified against `base`:

- `vocabulary/predicates.go:463` — `DataType string`
- `vocabulary/predicates.go:475` — `Units string`
- `vocabulary/predicates.go:482` — `Range string`
- `vocabulary/registry.go:131` — `m.DataType = dataType`
- `vocabulary/registry.go:144` — `m.Units = units`
- `vocabulary/registry.go:156` — `m.Range = valueRange`
- `vocabulary/registry.go:318` — `func Register(name string, opts ...Option)`
- `vocabulary/registry.go:423` — `func validatePredicateMetadataLocked(meta PredicateMetadata) error`

Direct read of `validatePredicateMetadataLocked`'s full body (`:382-403`) confirms it checks `InverseOf` parses,
`IsSymmetric`+`InverseOf` mutual exclusion, `IsAlias`/`AliasType`/`AliasPriority` consistency, and inverse-pointer
symmetry — nothing about `DataType`, `Units`, or `Range`. This is a direct read, not an inference from the grep
below.

- `pkg/fusion/fusionvocab/signals.go:47` — `vocabulary.GetPredicateMetadata(predicate); meta != nil`
- `pkg/fusion/fusionvocab/signals.go:48` — `return meta.Weight`

The named contrasting precedent: `Weight` IS read (issue's evidence).

- `vocabulary/export/export.go:209` — `meta := vocabulary.GetPredicateMetadata(predicate)`
- `vocabulary/export/export.go:211` — `return meta.StandardIRI`

The second named contrasting precedent: `StandardIRI` IS read.

- `vocabulary/export/object.go:302` — `func classifyFloat(f float64) classifiedObject`
- `vocabulary/export/object.go:309` — `datatype: xsdDouble,`

`classifyFloat` is reached from `classifyByGoType` for every `float32`/`float64` Go value with no predicate lookup
anywhere in this file (confirmed by the full read under Spellings of the fact below) — the unconditional
`xsd:double` the issue names.

- `message/triple.go:53` — `Object any`

Issue cites `~:52`; the field is actually at `:53` (comment block above it starts at `:49`) — non-material, one
line off.

Sibling bug #1142 is open (`bug`, `area:vocabulary`, `horizon:pre-v1`), title confirmed matching the issue's
characterization (see Adjacent claims). `vocabulary/export` has zero callers anywhere in this repo, production or
test, outside the package itself — re-verified three independent ways (see Spellings of the fact, Export path,
and Searches); no pin is possible for an absence, so this is reported as a search result, not a line citation.

## Spellings of the fact

### Declaration surface (item 1)

- `vocabulary/predicates.go:442` — `type PredicateMetadata struct`
- `vocabulary/predicates.go:445` — `Name string`
- `vocabulary/predicates.go:448` — `Description string`
- `vocabulary/predicates.go:485` — `Domain string`
- `vocabulary/predicates.go:488` — `Category string`
- `vocabulary/predicates.go:495` — `StandardIRI string`
- `vocabulary/predicates.go:499` — `IsAlias bool`
- `vocabulary/predicates.go:504` — `AliasType AliasType`
- `vocabulary/predicates.go:508` — `AliasPriority int`
- `vocabulary/predicates.go:520` — `InverseOf string`
- `vocabulary/predicates.go:528` — `IsSymmetric bool`
- `vocabulary/predicates.go:541` — `RuleOpaque bool`
- `vocabulary/predicates.go:548` — `Role PredicateRole`
- `vocabulary/predicates.go:556` — `Weight float64`

16 fields total (the 3 above plus these 13), confirmed by `gopls workspace_symbol PredicateMetadata` against the
same 16 line numbers independently of the manual read.

- `vocabulary/predicates.go:563` — `type PredicateRole string`
- `vocabulary/predicates.go:568` — `RoleUnspecified PredicateRole = ""`

`PredicateRole` is a Go-typed closed set (7 named constants, `:452-464`) with no runtime validator gating it — see
Problem shape and the Role row under Consumers.

- `vocabulary/registry.go:101` — `type Option func(*PredicateMetadata)`
- `vocabulary/registry.go:104` — `func WithDescription(desc string) Option`
- `vocabulary/registry.go:129` — `func WithDataType(dataType string) Option`
- `vocabulary/registry.go:142` — `func WithUnits(units string) Option`
- `vocabulary/registry.go:154` — `func WithRange(valueRange string) Option`
- `vocabulary/registry.go:168` — `func WithIRI(iri string) Option`
- `vocabulary/registry.go:185` — `func WithAlias(aliasType AliasType, priority int) Option`
- `vocabulary/registry.go:210` — `func WithInverseOf(inversePredicate string) Option`
- `vocabulary/registry.go:227` — `func WithRuleOpaque(opaque bool) Option`
- `vocabulary/registry.go:245` — `func WithSymmetric(symmetric bool) Option`
- `vocabulary/registry.go:265` — `func WithRole(role PredicateRole) Option`
- `vocabulary/registry.go:284` — `func WithWeight(weight float64) Option`
- `vocabulary/registry.go:330` — `meta := predicateRegistry[name]`
- `vocabulary/registry.go:336` — `for _, opt := range opts`
- `vocabulary/registry.go:339` — `if err := validatePredicateMetadataLocked(meta); err != nil`
- `vocabulary/registry.go:384` — `func RegisterPredicate(meta PredicateMetadata)`

`Register` (`:287-312`) seeds `meta` from any existing registration (AMEND semantics, gh#410, doc comment
`:264-277`), applies every option (`:305-307`), then validates (`:308`) before storing (`:312`).
`RegisterPredicate` (`:353-376`) is the direct-struct entry point ("backward compatibility and testing" per its
own doc comment, `:349-352`); same validator call at `:371`.

- `vocabulary/registry.go:464` — `func GetPredicateMetadata(predicate string) *PredicateMetadata`
- `vocabulary/registry.go:479` — `func ListRegisteredPredicates() []string`

Together these ARE the registry's test-time enumeration mechanism — `ListRegisteredPredicates` for names,
`GetPredicateMetadata` for the full struct per name. No separate iterator is missing.

- `vocabulary/registry.go:639` — `func ClearRegistry()`
- `vocabulary/registry.go:654` — `func SnapshotRegistry() func()`

Test isolation helpers, unrelated to DataType.

### Call-site census, in-repo (item 2)

Re-derived independently; **disagreement found, reported as a table, not a correction**:

| Setter | Issue's claim | Raw `git grep -n 'WithX('` non-test | `gopls references` (AST-exact) |
|---|---|---|---|
| `WithDataType(` | 200 | 200 | 195 |
| `WithUnits(` | 12 | 12 | not run |
| `WithRange(` | 11 | 11 | not run |

The raw non-test grep counts (200/12/11) match the issue exactly, but 200 includes the function *declaration*
line itself (`vocabulary/registry.go:112`) plus 4 doc-comment example lines spelling `WithDataType("string")` /
`WithDataType("float64")` inside `//` prose, not code (`vocabulary/registry.go:199,283`; `vocabulary/doc.go:59,129`).
`gopls references vocabulary/registry.go:112:6` returns exactly **195** real, AST-exact call sites — the number a
mechanical guard would actually walk.

DataType literal-value distribution, non-test:

| Spelling | Issue | Re-derived incl. the 4 comment lines (matches issue exactly) | Re-derived, code only (195, gopls-verified) |
|---|---|---|---|
| `string` | 141 | 141 | 140 |
| `float64` | 20 | 20 | 17 |
| `int` | 14 | 14 | 14 |
| `time.Time` | 10 | 10 | 10 |
| `timestamp` | 8 | 8 | 8 |
| `bool` | 4 | 4 | 4 |
| `int64` | 1 | 1 | 1 |
| `entity_ref` | 1 | 1 | 1 |
| **total** | **199** | **199** | **195** |

- `vocabulary/registry_test.go:290` — `WithDataType(DataTypeString),`
- `vocabulary/registry_test.go:460` — `WithDataType(DataTypeString),`
- `vocabulary/registry_test.go:466` — `WithDataType(DataTypeString))`

The complete set of `WithDataType(` test-file call sites (3, all `"string"`). `WithUnits(`/`WithRange(` never
appear in any `_test.go` file (0/0, confirmed by search).

Files holding non-test registrations (`git grep -l`): `examples/processors/document/vocabulary.go`,
`examples/processors/iot_sensor/vocabulary.go`, `examples/processors/weather_station/vocabulary.go`,
`vocabulary/agentic/register.go`, `vocabulary/examples/robotics.go`, `vocabulary/examples/semantic.go`,
`vocabulary/governance/register.go`, `vocabulary/hierarchy.go`, `vocabulary/labels.go` (`WithDataType` present in
all 9); `WithUnits`/`WithRange` additionally appear in `vocabulary/doc.go`/`vocabulary/registry.go`, but only as
doc-comment prose, not registrations.

### Readers — full field-level read/write map (item 3)

Re-derived the "nothing reads them" claim with the issue's exact search
(`git grep -n -w -E '\.(DataType|Units|Range)' -- '*.go' ':!vocabulary/**' ':!*_test.go'`) — reproduced empty
(0 hits). **Search-discipline finding**: that exact pattern is broken in a way that happens to still leave the
underlying claim true — `git grep -E` combined with `-w` and a leading `\.` silently matches nothing even on lines
that plainly contain a match; direct proof:

- `vocabulary/registry.go:131` — `m.DataType = dataType`

`git grep -n -w -E '\.(DataType|Units|Range)' -- vocabulary/registry.go` returns 0 hits against this exact file,
while the same pattern *without* `-w` returns 3 (the three setter writes). `\b` has the identical failure mode
under `git grep -E` (also verified, 0 hits on a pattern that should match). The working substitute is an explicit
trailing character class (`[^A-Za-z0-9_]`), not `-w` or `\b`. Re-run with the corrected pattern across the whole
repo, `vocabulary/**` included: still zero production reads of `.DataType`/`.Units`/`.Range` anywhere beyond the
three setter writes above.

**New finding beyond the issue's evidence**: the issue's Summary says "Nothing reads any of the three, anywhere,"
but its own Evidence grep deliberately excludes `*_test.go`. Test files DO read `.DataType`/`.Range` (never
`.Units`) as equality assertions — 25 lines total, for example:

- `examples/processors/iot_sensor/vocabulary_test.go:16` — `if meta.DataType != vocabulary.DataTypeString`
- `vocabulary/agentic/agentic_test.go:261` — `if meta.DataType != vocabulary.DataTypeFloat`
- `vocabulary/agentic/agentic_test.go:265` — `if meta.Range != "0-1"`
- `vocabulary/hierarchy_test.go:89` — `DataTypeString, meta.DataType,`
- `vocabulary/registry_test.go:297` — `assert.Equal(t, DataTypeString, meta.DataType)`

(Full set of 25 lines recorded under Searches; these prove the metadata round-trips through
`Register`/`GetPredicateMetadata` correctly — they are not production consumers.)

Every OTHER `PredicateMetadata` field, re-derived via `gopls references` plus `meta.Field`-anchored grep (this
avoids the generic-field-name false-positive noise a bare `.Field` sweep produces — `.Name`/`.Description`/`.Role`
alone match hundreds of unrelated structs repo-wide, confirmed and discarded, see Searches):

- `vocabulary/registry.go:385` — `parts, err := ParsePredicate(meta.Name)`
- `vocabulary/registry.go:389` — `if meta.Domain != "" && meta.Domain != parts.Domain`
- `vocabulary/registry.go:393` — `if meta.Category != "" && meta.Category != parts.Category`

`Name`/`Domain`/`Category` are read, but only inside `Register`/`RegisterPredicate`'s own validation — own-package
consistency checks, not an external consumer.

`Description` has **no production reader anywhere** — write-only, the identical shape to `DataType`/`Units`/`Range`,
confirmed only by test-assertion reads (`vocabulary/registry_test.go:296`, `hierarchy_test.go:81`).

- `vocabulary/registry.go:503` — `if meta.IsAlias && meta.AliasType.CanResolveToEntityID()`
- `vocabulary/registry.go:528` — `if meta.IsAlias && meta.AliasType == AliasTypeLabel`
- `vocabulary/registry.go:504` — `aliasPredicates[name] = meta.AliasPriority`

`IsAlias`, `AliasType`, and `AliasPriority` are read production-side by `DiscoverAliasPredicates`
(`:450-463`) and `DiscoverLabelPredicates` (`:475-488`).

- `vocabulary/registry.go:547` — `func GetInversePredicate(predicate string) string`
- `vocabulary/registry.go:559` — `return meta.InverseOf`
- `vocabulary/registry.go:603` — `return meta.IsSymmetric || meta.InverseOf != ""`

`InverseOf`/`IsSymmetric` are read production-side by `GetInversePredicate` (`:501-514`), `IsSymmetricPredicate`
(`:536-545`), `HasInverse` (`:549-558`), and `DiscoverInversePredicates` (`:576-589`).

- `vocabulary/registry.go:574` — `return meta.RuleOpaque`

`RuleOpaque` is read production-side by `IsRuleOpaque` (`:520-529`).

`Role` has **the same write-only shape as `DataType`/`Units`/`Range`** — confirmed no production reader anywhere
(`pkg/fusion/fusionvocab/**` has zero `Role`/`PredicateRole` references, verified by direct search), despite its
doc comment (`vocabulary/registry.go:227`: "Consumers (e.g. the deterministic fusion ranker) read it") asserting
one exists. This was not named in #1267's scope. Only test assertions read it
(`vocabulary/registry_test.go:391,515,520`).

`Weight` and `StandardIRI` readers are pinned under Consumers below, together with `pkg/fusion/rank_signals.go:24`
and `pkg/fusion/fusionvocab/signals.go:45`, which mention `PredicateMetadata.Weight` only in comment prose (not
code-level field access) — recorded to rule out a false-positive match, not as additional reader evidence.

### Export path (item 4)

- `vocabulary/export/object.go:62` — `type classifiedObject struct`
- `vocabulary/export/object.go:86` — `func classifyObject(t message.Triple, opts *options) classifiedObject`
- `vocabulary/export/object.go:92` — `if t.Datatype != ""`
- `vocabulary/export/object.go:93` — `return classifyWithExplicitDatatype(t, opts)`
- `vocabulary/export/object.go:123` — `func classifyWithExplicitDatatype(t message.Triple, opts *options) classifiedObject`
- `vocabulary/export/object.go:262` — `func classifyByGoType(obj any, opts *options) classifiedObject`
- `vocabulary/export/object.go:322` — `func classifyString(s string, opts *options) classifiedObject`
- `vocabulary/export/object.go:323` — `if message.IsValidEntityID(s)`

`classifyObject` consults `t.Datatype` (the **per-triple** field, distinct from `PredicateMetadata.DataType`) and
falls through to Go-type inference otherwise; no call anywhere in `object.go` consults
`vocabulary.GetPredicateMetadata` — the predicate's declared metadata is never looked up during export.
`classifyString` (`:118-130`) is sibling bug #1142's exact code path: an absolute-IRI string is not
entity-ID-shaped, so it always falls to the plain string-literal branch (structural confirmation, not asserting
the bug itself).

- `vocabulary/export/export.go:14` — `type Format int`
- `vocabulary/export/export.go:19` — `Turtle Format = iota`
- `vocabulary/export/export.go:78` — `func Serialize(w io.Writer, triples []message.Triple, format Format, opts ...Option) error`
- `vocabulary/export/export.go:203` — `func resolvePredicateIRI(predicate, base string) string`

`resolvePredicateIRI` (`:203-217`) is the ONLY place in this package that consults the predicate registry at all
(via `GetPredicateMetadata` at `:209`, `StandardIRI` read at `:211`, both pinned under Claimed gap) — it never
reads `DataType`.

`vocabulary/export` has zero callers in this repo, production or test, outside the package itself — confirmed
three independent ways: `git grep -l 'vocabulary/export"'` (0 outside the package dir), `git grep -n
'semstreams/vocabulary/export'` (0), `git grep -n 'vocabulary/export'` outside the package directory (0). stderr
visible throughout; no `2>/dev/null` on any zero-hit claim. `vocabulary/export/` totals 14 files, 2390 lines (7
non-test `.go` totaling 873 lines, 6 `_test.go` totaling 1417 lines, 1 `README.md`).

### Ingest/storage path — architect's item-7 fork (item 5)

- `message/triple.go:53` — `Object any`
- `message/triple.go:93` — `Datatype string`
- `message/triple.go:140` — `func (t Triple) IsRelationship() bool`
- `message/triple.go:145` — `switch t.Datatype`

`Triple.Datatype` (`:86`, doc comment `:82-85`: "optional RDF datatype hint... If omitted, the type is inferred
from the Go type of Object") is a DIFFERENT, per-triple mechanism from `PredicateMetadata.DataType`
(per-registration, set once). `IsRelationship` (`:133-146`) also switches on it. It already works end-to-end for
export (see `object.go:47-48` above); nothing sets it FROM `PredicateMetadata.DataType` at ingest or construction
time today, within the seams this sweep covered (not exhaustively re-swept for every `Triple{...}` literal in the
repo — flagged NOT RUN below).

- `graph/types.go:24` — `type EntityState struct`
- `graph/types.go:27` — `ID string`
- `graph/types.go:31` — `Triples []message.Triple`
- `graph/entity_predicate_contract.go:188` — `func MarshalEntityState(entity *EntityState) ([]byte, error)`
- `graph/entity_predicate_contract.go:192` — `return json.Marshal(entity)`
- `graph/entity_predicate_contract.go:255` — `func UnmarshalEntityState(data []byte, entity *EntityState) error`
- `graph/entity_predicate_contract.go:256` — `if err := json.Unmarshal(data, entity); err != nil`
- `graph/entity_predicate_contract.go:287` — `func UnmarshalEntityStateTrusted(data []byte, entity *EntityState) error`

Plain `encoding/json` throughout — no custom `MarshalJSON`/`UnmarshalJSON` exists on `Triple` anywhere in the
repo (confirmed, 0 hits). `UnmarshalEntityStateTrusted` is the ENTITY_STATES owner's own read-modify-write lane
only (gh#562); every other reader uses the validating `UnmarshalEntityState`.

- `processor/graph-ingest/canonical_mutations.go:276` — `encoded, err := graph.MarshalEntityState(entity)`
- `processor/graph-ingest/canonical_mutations.go:280` — `c.entityBucket.Create(ctx, entity.ID, encoded)`
- `processor/graph-ingest/canonical_mutations.go:344` — `encoded, err := graph.MarshalEntityState(candidate)`

`:280`, inside `handleCanonicalCreate` (`:226-296`), is the exact single-writer KV write call to `ENTITY_STATES`.
`:344` is the equivalent call inside `handleCanonicalReconcile`. Since `Object any` is decoded via plain
`encoding/json` into an untyped `any`, every JSON number in a stored triple becomes `float64` on read — a property
of `encoding/json`'s `any`-unmarshal behavior, not of any repo-owned line, so there is nothing further to pin for
that specific mechanism.

### Guard surface (item 6)

`test/contract/` totals 16 files, 3148 lines. Every `func Test*` name, by file (full enumeration, not sampled):
`catalog_reader_acquisition_contract_test.go` (`TestRetainedGraphReadersUseCatalogAcquisition`);
`component_status_retirement_contract_test.go` (`TestComponentStatusPlaneRemainsRetired`);
`context_ownership_contract_test.go` (`TestProductionStructsRetainNoContext`,
`TestContextFieldDetectorMatrix`); `core_composition_deps_test.go`
(`TestCoreCompositionDependencyClosure`, `TestProductionBinaryExcludesExamplePackages`);
`jetstream_input_identity_contract_test.go`
(`TestShippedJetStreamInputsDeclareBackingStreamAndSubjects`,
`TestShippedAgenticModelConfigDoesNotExposeLegacyStreamName`); `kvcatalog_literal_contract_test.go`
(`TestNoCatalogBucketLiteralsOutsideTheCatalog`); `message_contract_test.go`
(`TestSchemaRegistrationConsistency`, `TestBaseMessageRoundTrip`, `TestPayloadValidation`,
`TestPayloadMarshalJSON`); `nats_version_contract_test.go` (`TestNATSVersionIsConverged`);
`openapi_contract_test.go` (`TestCommittedOpenAPISpecValid`, `TestOpenAPISpecContainsAllComponents`,
`TestOpenAPISpecPaths`, `TestOpenAPISchemaReferences`); `openapi_no_flow_routes_test.go`
(`TestOpenAPIHasNoFlowRoutes`); `published_composition_test.go` (no `func Test*` — helper/fixture file);
`rapid_test_only_dependency_test.go` (`TestRapidStaysIsolatedToTestDependencies`);
`retired_consumer_deletion_config_test.go`
(`TestPublishedSchemasDoNotAdvertiseLifecycleConsumerDeletion`); `schema_contract_test.go`
(`TestCommittedSchemasMatchCode`, `TestCommittedSchemasValidStructure`,
`TestCommittedSchemaJetStreamOutputMaxAckPendingZeroSemantics`, `TestNoOrphanedSchemaFiles`);
`schema_export_test.go` (`TestSchemaExportCarriesDefaultPorts`); `semantic_test_fixture_contract_test.go`
(`TestSemanticTestFixtureHasNoProductionImports`, `TestSemanticTestFixtureAuthorityPackagesRemainCycleFree`,
`TestSemanticTestFixtureImportPolicyRejectsAmbiguity`,
`TestSemanticTestFixtureContractRejectsDirectEntityIDShadowing`); `shared_config_bucket_acquisition_contract_test.go`
(`TestCatalogBucketNamesAreNeverAcquiredDirectly`, `TestCatalogResolvingOwnersUseTheSeam`,
`TestGenericKVWritersConsultTheCatalog`).

- `test/contract/semantic_test_fixture_contract_test.go:22` — `"vocabulary": "vocabulary is the predicate grammar authority",`

The ONLY `vocabulary`-related string anywhere in `test/contract/` — a fixture map entry, unrelated to `DataType`.
Confirmed: no existing contract test references `vocabulary`, `PredicateMetadata`, or `DataType` beyond this one
incidental string. The issue's "no registry guard exists" claim holds.

- `test/contract/kvcatalog_literal_contract_test.go:41` — `func TestNoCatalogBucketLiteralsOutsideTheCatalog(t *testing.T)`
- `test/contract/kvcatalog_literal_contract_test.go:45` — `for _, spec := range graph.KVCatalog()`
- `test/contract/schema_contract_test.go:19` — `func TestCommittedSchemasMatchCode(t *testing.T)`

Shapes pinned here; discussed as reusable precedent under Problem shape.

### Docs and spec homes (item 7)

- `openspec/specs/predicate-contract/spec.md:3` — `## Purpose`
- `openspec/specs/predicate-contract/spec.md:44` — `### Requirement: Every stored graph predicate has one canonical three-segment syntax`
- `openspec/specs/predicate-contract/spec.md:69` — `### Requirement: Vocabulary declaration and namespace authority are explicit and separate from syntax`
- `openspec/specs/predicate-contract/spec.md:101` — `### Requirement: Canonical predicate enforcement is unconditional`

278 lines total, 8 `### Requirement:` headings, ~20 `#### Scenario:` blocks (full heading list recorded under
Searches). **Fully silent** on `DataType`/`datatype`/`Units`/`Range` — zero matches, stderr visible, confirmed by
direct search of the whole file.

`docs/adr/074-canonical-predicate-contract.md` (109 lines) is **fully silent** on the same terms — zero matches,
same search discipline. Nothing to pin from either file for this fact, by definition of "silent."

- `vocabulary/README.md:136` — `WithDataType(dataType string)`
- `vocabulary/README.md:169` — `WithUnits(units string)`
- `vocabulary/README.md:179` — `WithRange(valueRange string)`

662 lines total; all three setters documented with worked examples (`:144,153,162`) and combined usage
(`:78-90,190-192,245,252,259`). **Silent** on any closed set, validation, or export honoring — every example
treats the three as free-form strings.

- `docs/basics/04-vocabulary.md:81` — `WithDataType(vocabulary.DataTypeInt)`
- `docs/basics/04-vocabulary.md:88` — `WithDataType(vocabulary.DataTypeFloat)`

363 lines total; also `:82-83,89` (`WithUnits`/`WithRange` examples), prose on units at `:129,142` (a NAMING
convention, not a `WithUnits` recommendation), a bare `// Range: 0-100` comment at `:308`. Same silence as
README.md on closed-set/validation/export-honoring.

### Tier 1 freeze context (item 8)

- `release/tier1-packages.txt:95` — `github.com/c360studio/semstreams/vocabulary`
- `release/tier1-packages.txt:100` — `github.com/c360studio/semstreams/vocabulary/export`

Both the core package and the export package are Tier 1 frozen, not just `vocabulary` alone.

- `taskfiles/apicompat.yml:4` — `compat:`
- `taskfiles/apicompat.yml:5` — `desc: Report incompatible API changes in the Tier 1 package set (ADR-106 RC-4 instrument)`
- `scripts/api-compat.sh:50` — `list="release/tier1-packages.txt"`
- `scripts/api-compat.sh:60` — `base="$(git tag --list 'v1.0.0-*' --merged HEAD --sort=-version:refname | head -1)"`

Mechanism (header comment `:1-33`, pinned code `:35-199`): `golang.org/x/exp/cmd/apidiff`, pinned at a specific
revision (`:35`). Compares the exported Go API of every package in `release/tier1-packages.txt` between a base and
HEAD. Base defaults to the latest `v1.0.0-*` tag merged into HEAD unless an explicit base is passed as `$1`
(`:56-61`). A package present at base and absent at head is a hard REMOVAL finding (`:132-135`); a package new at
head is a reported, non-failing ADDITION (`:136-139`). Explicitly documented (header comment) as NOT checking
config-schema, subject, entity-ID grammar, or payload-envelope compatibility — separate Tier 1 guards own those.

### Named downstream reader — #1261 (item 9)

- `processor/agentic-tools/executors/graph_query.go:975` — `func validateAuthoritativeEntity(data []byte) error`
- `processor/agentic-tools/executors/graph_query.go:977` — `return graph.UnmarshalEntityState(data, &entity)`
- `processor/agentic-tools/executors/graph_query.go:980` — `func decodeAuthoritativeEntityData(data []byte) (map[string]any, error)`

`decodeAuthoritativeEntityData` (`:496-504`) re-`json.Unmarshal`s the SAME bytes a second time into a bare
`map[string]any` for tool-result shaping, after `validateAuthoritativeEntity` already validated them via the
authoritative decoder. Any triple `Object` numeric value goes through `encoding/json`'s `any`-decode here too,
independent of the ENTITY_STATES round trip. This is the file the issue's scope item 4 points at ("the graph-read
tool result schema… no schema is exposed"); no change made or proposed here. Open draft PR #1262
(`claude/gh1261-graph-read-tools`) is in flight on this exact file — off-limits per this task's Do-not list, not
inspected further.

### Sister census — READ-ONLY (item 10)

`git status --porcelain` snapshots were byte-identical before and after in all five sister repos (semsource,
semspec, semconnect, semboids, semdragon). semspec carried pre-existing, unrelated dirty state (43 lines) both
before and after — untouched by this census. The other four were clean both times. `git grep` only; no `go
list`/`go build` was run in any sister.

**Disagreement, reported as a table, not a correction**: the issue's "8, 10, 3, 2, 1" figure is a
**distinct-file** count (`git grep -l -E 'vocabulary\.With(DataType|Units|Range)\('`), not a call-site count.

| Sister | Distinct files (matches issue exactly) | Raw `vocabulary.WithDataType(` call-site count (a different unit, not what the issue reported) |
|---|---|---|
| semsource | 8 | 142 |
| semspec | 10 | 538 |
| semconnect | 3 | 16 |
| semboids | 2 | 3 |
| semdragon | 1 | 56 |

All matches use the module-qualified `vocabulary.WithDataType(` form — re-verified there is no unqualified/shadowed
`WithDataType(` in any sister (0 hits for `WithDataType(` minus `vocabulary.WithDataType(` in semspec, the largest
gap). The per-call-site volume (755 total `WithDataType(` call sites across the five sisters) is real, not a
false-positive artifact; it is simply a different unit than the issue's file count. Distinct-file lists (all
5): semsource — `source/ast/vocabulary.go`, `source/vocabulary/{config,convention,git,lifecycle,media,
navigational,predicates}.go`; semspec — `vocabulary/{ics,observability,project,semspec,spec,workflow}/predicates.go`,
`vocabulary/observability/tool_recovery.go`, `vocabulary/source/{convention,git,predicates}.go`; semconnect —
`gateway/cs-api/projection_contracts.go`, `parser/sensorml/predicates.go`, `vocabulary/csapi/register.go`;
semboids — `internal/boidgraph/vocabulary.go`, `internal/boidgraph/clustering_spike_integration_test.go`;
semdragon — `domain/vocab.go`.

semconnect's `gateway/cs-api/systems.go` calls `export.Serialize(&buf, state.Triples, export.JSONLD)` at line 746
(import at line 21) — the production RDF emission call the issue names. **Precision note**: `export.Turtle` is
NOT called anywhere in `systems.go` — it appears only in a doc comment at line 615 listing JSON-LD as the
alternative format. The actual `export.Turtle`/`export.SerializeToString` call sites in semconnect (4 total) are
all in test files: `message/oms/roundtrip_test.go:250`, `parser/sensorml/graphable_test.go:85,213,281`. (These are
sister-repo facts; this repo's `inventory:verify` cannot check cross-repo paths, so they are reported here as
prose rather than as pin bullets that would spuriously DRIFT as "file missing.")

## Adjacent claims

- #1267 — this issue (owner-ruled "honor", 2026-09-07).
- #1142 — open, `bug`/`area:vocabulary`/`horizon:pre-v1`, "vocabulary/export: absolute-IRI objects render as
  string literals" — the sibling classifier bug in the same `classifyString` code path this change touches.
- #1261 — open, "agentic-tools/graph_query: the graph-read tools never rejoined the vocabulary registry..." — the
  named downstream reader (scope item 4). Claimed by draft PR #1262 (`claude/gh1261-graph-read-tools`), off-limits.
- #1264 — open, "affordance read... post-v1 candidate" — §4/SHACL comment named in scope item 4 as a future reader
  of typed `DataType`.
- #1260 — open, "docs/concepts: relationships-vs-properties guidance... edge-or-property heuristic" — found via
  the same search, adjacent vocabulary-shape work, not on this field surface.
- #673 — open, "vocabulary: declare the standard dc.terms.modified predicate" — found via search, unrelated to
  DataType specifically.
- `openspec/specs/predicate-contract/spec.md` — current truth for predicate syntax/authority/enforcement; silent
  on datatypes (see Spellings of the fact). A design here is a new capability area within this spec's territory,
  or a new spec, not something the existing 8 Requirements already cover.
- `docs/adr/074-canonical-predicate-contract.md` — silent on datatypes (see Spellings of the fact).
- Open draft PRs (read-only, not inspected beyond title/branch): #1269 `claude/gh1267-honor-predicate-datatype`
  (this task's own claim PR); #1262 `claude/gh1261-graph-read-tools` (touches the item-9 file); #1254
  `claude/gh1205-auth-inventory`; #1159 `codex/gh1146-agentic-loop-restart`; #1156 `codex/gh759-semantic-settlement`;
  #1141 `codex/gh1138-http-page-read`. None of the Codex/other-Claude PRs' branch names or titles suggest overlap
  with `vocabulary/**` or `release/tier1-packages.txt`; not verified by diff (off-limits).

## Consumers

Every field with a confirmed production reader, and its reader:

- `pkg/fusion/fusionvocab/signals.go:47` — `vocabulary.GetPredicateMetadata(predicate); meta != nil`
- `pkg/fusion/fusionvocab/signals.go:48` — `return meta.Weight`
- `vocabulary/export/export.go:209` — `meta := vocabulary.GetPredicateMetadata(predicate)`
- `vocabulary/export/export.go:211` — `return meta.StandardIRI`
- `vocabulary/registry.go:389` — `if meta.Domain != "" && meta.Domain != parts.Domain`
- `vocabulary/registry.go:393` — `if meta.Category != "" && meta.Category != parts.Category`
- `vocabulary/registry.go:503` — `if meta.IsAlias && meta.AliasType.CanResolveToEntityID()`
- `vocabulary/registry.go:528` — `if meta.IsAlias && meta.AliasType == AliasTypeLabel`
- `vocabulary/registry.go:547` — `func GetInversePredicate(predicate string) string`
- `vocabulary/registry.go:559` — `return meta.InverseOf`
- `vocabulary/registry.go:574` — `return meta.RuleOpaque`

`Weight` and `StandardIRI` are the only fields read OUTSIDE `vocabulary/**` in production code. `Name`, `Domain`,
`Category`, `IsAlias`, `AliasType`, `AliasPriority`, `InverseOf`, `IsSymmetric`, and `RuleOpaque` are all read, but
only inside `vocabulary/registry.go` itself (own-package validation and discovery helpers). `DataType`, `Units`,
`Range`, `Description`, and `Role` have **no production reader anywhere** — five fields sharing the exact
write-only shape the issue names for three of them.

## Problem shape

Three existing instances of the same shape, on this surface and adjacent:

- `vocabulary/registry.go:12` — `type AliasType string`
- `vocabulary/registry.go:24` — `AliasTypeIdentity AliasType = "identity"`
- `vocabulary/registry.go:185` — `func WithAlias(aliasType AliasType, priority int) Option`
- `vocabulary/registry.go:452` — `func validAliasType(aliasType AliasType) bool`
- `vocabulary/registry.go:454` — `case AliasTypeIdentity, AliasTypeLabel, AliasTypeAlternate, AliasTypeExternal, AliasTypeCommunication:`

**A closed typed vocabulary validated at Register-time, on the SAME struct, TODAY**: `AliasType` is gated by
`validAliasType` (closed `switch`/`default: false`), invoked from `validatePredicateMetadataLocked` on every
`Register`/`RegisterPredicate` call. `WithAlias` takes the typed constant, not a raw string. This is the exact
option-signature-plus-validator shape scope item 1 describes for `DataType` — it already exists, for a sibling
field, in the same file.

- `message/triple.go:93` — `Datatype string`
- `vocabulary/export/object.go:92` — `if t.Datatype != ""`
- `vocabulary/export/object.go:93` — `return classifyWithExplicitDatatype(t, opts)`

**An explicit per-instance datatype override that already beats Go-type inference, one level down**:
`message.Triple.Datatype` is honored unconditionally by `classifyObject` → `classifyWithExplicitDatatype`,
overriding `classifyByGoType` whenever set. The mechanism the issue wants for `PredicateMetadata.DataType`
(declared type wins over inference) is already live for the per-triple field; it is simply never populated from
the per-predicate declaration.

- `test/contract/kvcatalog_literal_contract_test.go:41` — `func TestNoCatalogBucketLiteralsOutsideTheCatalog(t *testing.T)`
- `test/contract/kvcatalog_literal_contract_test.go:45` — `for _, spec := range graph.KVCatalog()`
- `test/contract/schema_contract_test.go:19` — `func TestCommittedSchemasMatchCode(t *testing.T)`

**A mechanical, repo-wide drift guard over an enumerable registry, already in `test/contract/`**:
`TestNoCatalogBucketLiteralsOutsideTheCatalog` uses `go/parser` to AST-scan every non-test file's string literals
against `graph.KVCatalog()`'s descriptor table, with a 2-file allowlist. `TestCommittedSchemasMatchCode` is a
second shape: load committed artifacts, build a live registry, cross-check. Either is directly reusable for scope
item 3's "every registered predicate carries a `DataType` from the closed set" guard — the registry is already
enumerable via `ListRegisteredPredicates()`/`GetPredicateMetadata()` (pinned above), so no AST scan is even
needed; a plain loop over the live registry suffices, closer to the `schema_contract_test.go` shape than the
`kvcatalog_literal_contract_test.go` shape.

## Searches

- `git branch --show-current && git rev-parse HEAD && git status --porcelain` → branch
  `claude/gh1267-honor-predicate-datatype`, base `232b1e7d4efb530b5170facc87de6698ef7af290`, clean
- `gh issue view 1267 --json body -q .body` → full issue body retrieved
- `Read openspec/project.md` → Purpose + Product Boundary read
- `grep -n "type PredicateMetadata struct" -A 40 vocabulary/predicates.go` → struct start `:351`, fields through `:391`
- `wc -l vocabulary/predicates.go vocabulary/registry.go` → 472, 621
- `sed -n '391,472p' vocabulary/predicates.go` → rest of struct + `PredicateRole`
- `grep -n "^func \|^type " vocabulary/registry.go` → 29 top-level declarations
- `sed -n '101,300p' vocabulary/registry.go` → `Option` type through `Register` start
- `sed -n '300,420p' vocabulary/registry.go` → `Register` body through `validAliasType`
- `sed -n '418,472p' vocabulary/registry.go` → `GetPredicateMetadata`, `ListRegisteredPredicates`, `DiscoverAliasPredicates` start
- `sed -n '590,621p' vocabulary/registry.go` → `ClearRegistry`, `SnapshotRegistry`
- `git grep -c 'WithDataType(' -- '*.go' | wc -l` → 12 files
- `git grep -n 'WithDataType(' -- '*.go' | wc -l` → 203
- `git grep -n 'WithDataType(' -- '*.go' ':!*_test.go' | wc -l` → 200
- `git grep -n 'WithDataType(' -- '*_test.go' | wc -l` → 3
- `git grep -n 'WithUnits(' -- '*.go' | wc -l` → 12; `':!*_test.go'` → 12; `'*_test.go'` → 0
- `git grep -n 'WithRange(' -- '*.go' | wc -l` → 11; `':!*_test.go'` → 11; `'*_test.go'` → 0
- `git grep -h -o 'WithDataType("[^"]*")' -- '*.go' ':!*_test.go' | sort | uniq -c | sort -rn` → 8 spellings,
  141/20/14/10/8/4/1/1
- `git grep -h -o 'WithDataType("[^"]*")' -- '*.go' ':!*_test.go' | wc -l` → 199
- `git grep -n 'WithDataType(' -- '*.go' ':!*_test.go' | grep -v -E 'WithDataType\("[^"]*"\)'` → only the func
  declaration line
- `git grep -l 'WithDataType(' -- '*.go' | sort` → 12 files
- `git grep -l 'WithUnits(' -- '*.go' | sort` → 4 files
- `git grep -l 'WithRange(' -- '*.go' | sort` → 6 files
- `git grep -n 'WithDataType(' -- vocabulary/registry.go` → 3 lines (1 func decl + 2 doc-comment examples)
- `git grep -n -E 'WithDataType\(|WithUnits\(|WithRange\(' -- vocabulary/doc.go` → 6 doc-comment example lines
- `git grep -n 'WithDataType(' -- '*.go' ':!*_test.go' | grep -v -E '^[^:]+:[0-9]+:[[:space:]]*//'` → 196
  (195 real call sites + 1 func decl)
- `git grep -n 'WithDataType(' -- '*_test.go'` → 3 lines, all `"string"`
- `git grep -n -w -E '\.(DataType|Units|Range)' -- '*.go' ':!vocabulary/**' ':!*_test.go'` → 0 (issue's exact
  search, reproduced empty)
- `git grep -n -w -E '\.(DataType|Units|Range)' -- '*.go' ':!vocabulary/**'` → 0
- `git grep -n -w -E '\.(DataType|Units|Range)' -- '*.go' 'vocabulary/**'` → 0 (found to be a `-w`+leading-`\.`
  bug, see below)
- `git grep -n -w -E '\.(DataType|Units|Range)' -- vocabulary/registry.go` → 0 (debug)
- `git grep -n -E '\.(DataType|Units|Range)' -- vocabulary/registry.go` → 3 (writes, without `-w`)
- `git grep -n -E '\.DataType\b' -- '*.go'` / `.Units\b` / `.Range\b` → 0/0/0 (confirms `\b` ALSO silently
  matches nothing under `git grep -E`, a second instance of the documented gotcha)
- `git grep -n -E '\.DataType[^A-Za-z0-9_]' -- '*.go'` → 23 hits, all test-file assertions + 1 write
- `git grep -n -E '\.Units[^A-Za-z0-9_]' -- '*.go'` → 1 (the setter write)
- `git grep -n -E '\.Range[^A-Za-z0-9_]' -- '*.go'` → 5 (2 unrelated `sync.Map.Range`, 2 test assertions, 1 write)
- `git grep -n -E '\.(DataType|Units|Range)[^A-Za-z0-9_]' -- '*.go' ':!vocabulary/**' ':!*_test.go'` → 1 (false
  positive: `.Range(` on `sync.Map`)
- `git grep -n -E '\.(DataType|Units|Range)[^A-Za-z0-9_]' -- '*_test.go'` → 25 (test-time reads; full list:
  `examples/processors/iot_sensor/vocabulary_test.go:16-17`; `vocabulary/agentic/agentic_test.go:261-262,265-266,
  323-324,366-367,406-407,437-438,464-465,514-515,562-563,626-627`; `vocabulary/hierarchy_test.go:89`;
  `vocabulary/registry_test.go:297`)
- `git grep -n -E '\.(DataType|Units|Range)[^A-Za-z0-9_]' -- 'vocabulary/**' ':!*_test.go'` → 3 (the three setter
  writes, confirmed no production reader even in-package)
- `sed -n '40,55p' pkg/fusion/fusionvocab/signals.go` → `Weight` reader pinned, `:47-48`
- `sed -n '200,215p' vocabulary/export/export.go` → `StandardIRI` reader pinned, `:209,211`
- for-loop over `Name/Description/Domain/Category/StandardIRI/IsAlias/AliasType/AliasPriority/InverseOf/
  IsSymmetric/RuleOpaque/Role/Weight` reads outside `vocabulary/**`/tests, unquoted `$f[...]` → zsh subscript
  parse error on every iteration (search-discipline finding, not usable results)
- same for-loop, quoted/`${f}` pattern-build fix → 1020/65/56/59/0/0/0/0/0/0/0/81/2 (too noisy — generic field
  names collide repo-wide; superseded by the `meta.Field`-anchored re-run below)
- `git grep -n 'vocabulary.GetPredicateMetadata(' -- '*.go' ':!vocabulary/**' ':!*_test.go'` → 1
  (`pkg/fusion/fusionvocab/signals.go:47`)
- `git grep -n 'vocabulary.GetPredicateMetadata(' -- '*.go' ':!vocabulary/**'` → 3 (+2 test call sites)
- `git grep -n -E 'GetPredicateMetadata\(|predicateRegistry\[' -- 'vocabulary/**'` → ~55 call sites, full list
  captured
- `sed -n '475,590p' vocabulary/registry.go` → `DiscoverLabelPredicates`, `GetInversePredicate`, `IsRuleOpaque`,
  `IsSymmetricPredicate`, `HasInverse`, `DiscoverInversePredicates` bodies read
- for-loop `meta.{Name,Description,Domain,Category,Role}` reads in `vocabulary/**`, quoted-pattern-build → full
  per-field hit lists (used to build the read/write map)
- `git grep -n -E -- 'meta\.Role[^A-Za-z0-9_]' '*.go' ':!vocabulary/**'` → 0
- `git grep -n -E 'PredicateRole|\.Role\b' -- 'pkg/fusion/**'` → 0 (confirms doc-comment claim about `Role`
  readers is currently false)
- `git grep -n -E '\.Role[^A-Za-z0-9_]' '*.go' | grep -i -E 'predicat|vocabular|fusion'` → only comment/doc
  mentions and test assertions
- `git grep -n -E -- 'meta\.Weight[^A-Za-z0-9_]' '*.go' ':!vocabulary/**'` → 0
- `git grep -n -E -- '\.Weight[^A-Za-z0-9_]' '*.go' ':!vocabulary/**' ':!*_test.go'` → 2, both doc-comment
  mentions of "PredicateMetadata.Weight," not code reads (`pkg/fusion/fusionvocab/signals.go:45`,
  `pkg/fusion/rank_signals.go:24`)
- `ls vocabulary/export/`, `wc -l vocabulary/export/*.go` → 14 files, sizes captured
- `sed -n '1,40p' vocabulary/export/object.go` → classifier top, discovered `Triple.Datatype` distinction
- `grep -n '^func \|^const\|^type \|xsdDouble\|Datatype' vocabulary/export/object.go` → all function/line pins
- `grep -n 'Datatype' message/triple.go` → `Triple.Datatype` field + `IsRelationship` switch found
- `sed -n '40,176p' vocabulary/export/object.go` → full classifier bodies read
- `sed -n '1,20p' message/triple.go`, `sed -n '40,150p' message/triple.go` → `Object`/`Datatype` fields,
  `IsRelationship` body
- `grep -n 'Object any\|Datatype string\|func (t Triple) IsRelationship' message/triple.go` → precise lines 53,
  86, 133
- `gh issue view 1142 --json title,body,state,labels` → confirmed open, labels
- `grep -n 'func IsValidEntityID' -A 15 message/triple.go` → `:153-155`
- `git grep -ln 'ENTITY_STATES' -- '*.go' | grep -v _test.go | sort` → ~85 files (too broad; narrowed next)
- `ls processor/graph-ingest/` → package file listing
- `grep -n 'func \|kv\.\(Put\|Create\|Update\)\|Marshal' processor/graph-ingest/canonical_mutations.go` → found
  `MarshalEntityState` call sites `:276,344`
- `grep -n '^func \|EntityState\b' graph/state_contract.go` → unrelated helpers (`StateContractError`), no
  `EntityState` type here
- `git grep -n 'func MarshalEntityState\|func UnmarshalEntityState\|type EntityState struct' -- '*.go'
  ':!*_test.go'` → 3 declarations pinned
- `sed -n '260,296p' processor/graph-ingest/canonical_mutations.go` → the exact `entityBucket.Create` write call
- `sed -n '180,300p' graph/entity_predicate_contract.go` → Marshal/Unmarshal/UnmarshalTrusted bodies
- `grep -n 'func MarshalEntityState\|json.Marshal(entity)\|func UnmarshalEntityState\|json.Unmarshal(data,
  entity)\|func UnmarshalEntityStateTrusted' graph/entity_predicate_contract.go` → precise lines
- `sed -n '20,45p' graph/types.go` → `EntityState` struct fields
- `git grep -n 'func (t Triple) MarshalJSON\|func (t \*Triple) UnmarshalJSON\|func (t Triple) UnmarshalJSON' --
  '*.go'` → 0 (no custom JSON methods on `Triple`)
- `ls test/contract/` → 16 files; `wc -l test/contract/*.go` → 3148 total
- `git grep -n -i -E 'vocabulary|predicatemetadata|datatype' -- 'test/contract/**'` → 1 (incidental fixture-map
  string)
- for-file-loop `grep -n '^func Test' test/contract/*.go` → full test-function inventory, all 16 files
- `sed -n '1,45p' test/contract/kvcatalog_literal_contract_test.go` → AST-scan shape pinned
- `sed -n '19,40p' test/contract/schema_contract_test.go` → schema cross-check shape pinned
- `ls openspec/specs/ | grep -i predicate`, `find openspec/specs -iname 'spec.md' -path '*predicate*'`, `ls
  docs/adr/ | grep -i '^074'` → `predicate-contract`, `spec.md`, `074-canonical-predicate-contract.md` located
- `grep -n '^## Requirement\|^### Scenario' openspec/specs/predicate-contract/spec.md` → 0/0 (wrong heading
  level, corrected next)
- `wc -l openspec/specs/predicate-contract/spec.md docs/adr/074-canonical-predicate-contract.md
  vocabulary/README.md docs/basics/04-vocabulary.md` → 278/109/662/363
- `grep -n '^#' openspec/specs/predicate-contract/spec.md` → 8 Requirements, ~20 Scenarios, full list
- `grep -ni -E 'datatype|units|range' openspec/specs/predicate-contract/spec.md` → 0
- `grep -ni -E 'datatype|units|range' docs/adr/074-canonical-predicate-contract.md` → 0
- `grep -n -i -E 'datatype|withunits|withrange|units|range' vocabulary/README.md` → 17 lines (per-row sweep)
- `grep -n -i -E 'datatype|withunits|withrange|units|range' docs/basics/04-vocabulary.md` → 8 lines (per-row
  sweep)
- `grep -n 'vocabulary' release/tier1-packages.txt` → `:95` `vocabulary`, `:96-100` 5 subpackages incl.
  `:100` `vocabulary/export`
- `wc -l release/tier1-packages.txt` → 100
- `grep -n -A 5 'api:compat' Taskfile.yml` (miss) → `grep -rn 'api:compat' Taskfile*.yml taskfiles/` →
  `taskfiles/apicompat.yml`
- `sed -n '1,40p' taskfiles/apicompat.yml` → 3 tasks (`compat`, `compat:report`, `compat:test`)
- `wc -l scripts/api-compat.sh` → 199; `sed -n '1,60p' scripts/api-compat.sh` → mechanism + base-resolution logic
  pinned
- `ls processor/agentic-tools/executors/ | grep -i graph` → 5 files
- `grep -n 'func \|ResultHint\|Triples\b' processor/agentic-tools/executors/graph_query.go | head -30` → 15
  function signatures
- `sed -n '491,515p' processor/agentic-tools/executors/graph_query.go` → decode functions pinned
- `which gopls && gopls version` → `/Users/coby/go/bin/gopls`, `v0.20.0`
- `gopls workspace_symbol -matcher=fuzzy PredicateMetadata` → struct + all 16 fields + related functions, lines
  cross-checked against manual reads (exact match)
- `gopls references vocabulary/registry.go:112:6` → 195 real call sites (AST-exact; used to correct the raw-grep
  200/199 counts above)
- for-repo-loop `git -C <sister> status --porcelain` (BEFORE) → semsource/semconnect/semboids/semdragon clean;
  semspec pre-existing unrelated dirty state (43 lines, recorded verbatim)
- for-repo-loop `git -C <sister> grep -n 'WithDataType('/'WithUnits('/'WithRange(' -- '*.go'` (full output,
  persisted to a scratch file due to size) → raw counts 142/0/0 (semsource), 538/0/1 (semspec), 16/1/1
  (semconnect), 3/0/0 (semboids), 56/0/0 (semdragon)
- for-repo-loop compact counts (`wc -l`) → same numbers, confirmed
- for-repo-loop `git -C <sister> grep -n 'vocabulary\.WithDataType('` etc. (qualified form) → identical counts
  (rules out shadowed/local `WithDataType` as the source of the gap)
- `git -C semspec grep -n 'WithDataType(' -- '*.go' | grep -v 'vocabulary\.WithDataType('` → 0 (confirms every
  match IS the qualified/imported form)
- for-repo-loop `git -C <sister> grep -l -E 'vocabulary\.With(DataType|Units|Range)\('` (distinct files) → 8 /
  10 / 3 / 2 / 1 — exact match to the issue's "8/10/3/2/1", resolving the count-unit disagreement
- `git -C semconnect grep -n -E 'export\.(Turtle|JSONLD|Serialize)' -- gateway/cs-api/systems.go` → `:615`
  (comment), `:746` (real call, JSONLD only)
- `git -C semconnect grep -n 'vocabulary/export' -- gateway/cs-api/systems.go` → `:21` import confirmed
- `git -C semconnect grep -n 'export\.Turtle' -- '*.go'` → 4, all in `_test.go` files, none in `systems.go`
- for-repo-loop `git -C <sister> status --porcelain` (AFTER) → byte-identical to BEFORE for all five repos,
  including semspec's pre-existing dirty state — confirmed read-only
- `gh issue list --search "vocabulary datatype" --state open --json number,title --limit 30` → 6 issues:
  #1267, #1264, #1142, #673, #1261, #1260
- `gh issue list --search "PredicateMetadata" --state open --json number,title --limit 30` → 4 issues (subset of
  above)
- `openspec list` → `honor-predicate-datatype  No tasks` (this task's own change dir)
- `gh pr list --state open --json number,title,headRefName --limit 30` → 6 open PRs, listed under Adjacent claims
- `task inventory:verify -- openspec/changes/honor-predicate-datatype/inventory.md` (run twice: once against the
  first draft, which surfaced the pin-grammar violations this file was rewritten to fix; once after the rewrite)

- `grep -rn --include="*.go" -E 'PredicateMetadata' <sister>/` → semconnect 7, semdragon 4, semlink 3, semmachina 3, semsource 26, semspec 33, semteams 4 (incl. 2 stale in-repo worktree copies); 0 in semboids, semembed, seminstruct, semmem, semops, semsage, semdev, semdocs, semstreams-ui  *(addendum)*
- `grep -rn --include="*.go" -E '(meta|metadata|md|m)\.(Role|DataType|Units|Range)[^a-zA-Z_]' <sister>/ | grep -v _test.go` → only `PredicateMetadata`-bearing production hits are semsource `status.go:224` and semmachina `registration.go:128`; every other hit is an unrelated `Role` field on an agentic message/task type  *(addendum)*
- `sed -n '1,25p' semsource/processor/source-manifest/status.go` → import of `github.com/c360studio/semstreams/vocabulary` at line 9  *(addendum)*
- `grep -n -B2 -A12 'type PredicateDescriptor' semsource/processor/source-manifest/*.go` → struct at `payload_predicates.go:27-32`, `DataType` tagged `json:"data_type"`  *(addendum)*
- `git grep -l -E 'PredicateMetadata' -- '*.go' ':!vocabulary/**'` → 4 files, listed in A4  *(addendum)*
- `git grep -n -E '\.Role[^a-zA-Z_]' -- '*.go' ':!vocabulary/**' ':!*_test.go'` → 80+ hits, all unrelated `Role` fields; the wrong instrument for this question, recorded so it is not repeated  *(addendum)*
- `grep -n -E '^func (Register|RegisterPredicate|MustRegister)' vocabulary/*.go` → 2 entry points  *(addendum)*
- `grep -n 'validatePredicateMetadataLocked' vocabulary/*.go` → 1 definition, 2 call sites  *(addendum)*
- `for d in semsource semmachina semconnect semspec semdragon; do git -C $d status --porcelain | wc -l; done` → 0, 16, 0, 42, 0 — semmachina and semspec carry pre-existing unrelated dirty state belonging to their owners; no write was performed by this addendum  *(addendum)*

### NOT RUN

- `gopls references` for `WithUnits`/`WithRange` individually (grep counts for these were low-volume — 12/11 —
  and internally consistent across every search shape tried; AST cross-check judged lower-value than for
  `WithDataType`'s higher, comment-inflated count).
- An exhaustive repo-wide sweep for every writer of `message.Triple{Datatype: ...}` (to confirm no existing code
  derives a triple's per-instance `Datatype` from a predicate's registered `DataType` today) — only confirmed the
  absence of a `PredicateMetadata`-registry-consulting writer within `vocabulary/export/**`; did not sweep
  `processor/**`/`message/**` construction sites for a hand-set `Datatype:` field. This bounds the "architect's
  item-7 fork" pin to what's provable from the export/ingest seam already enumerated, not a full inventory of
  every `Triple` literal in the repo (likely hundreds; out of the 40-call budget this task already exceeded, given
  the brief's 10-item scope).
- Diff-level inspection of open draft PRs #1262/#1254/#1159/#1156/#1141 — titles/branches only, per the Do-not
  list.

## Addendum — orchestrating session, 2026-09-07 (sister READERS + registration seam)

Scope note: the explorer's brief asked for the sister census of who **populates** `WithDataType`/`WithUnits`/
`WithRange`, and for semconnect's RDF emission. It did not ask who **reads** `PredicateMetadata` fields in a
sister. That gap belongs to the brief, not to the census; the four findings below fill it. Every search here was
`grep`/`sed` only — no `go list`/`go build` ran in any sister, so no sister `go.mod` could be rewritten.

Cross-repo facts are written as prose, never as pin bullets, because `inventory:verify` cannot resolve sister
paths and would report them as spurious DRIFT (the convention this file already sets in the item-10 census).

### A1. Premise correction: issue #1267's "no sister reader" claim is FALSE

The issue's Summary states "Nothing reads any of the three, anywhere", and its Evidence line states "none in any
sister (read-only census)". The in-repo half is correct and was independently reproduced earlier in this file. The
sister half is not: semsource reads `.DataType` in production and republishes the value on a wire payload.

In semsource, `processor/source-manifest/status.go` imports `github.com/c360studio/semstreams/vocabulary` at
line 9 — confirmed to be this repo's package, not a same-named local one. Line 219 calls
`vocabulary.GetPredicateMetadata(pred)`; line 224 reads `if meta.DataType != "" { dataType = meta.DataType }`,
with `dataType := "string"` as the default two lines above. The value lands in `PredicateDescriptor` at
`processor/source-manifest/payload_predicates.go` lines 27-32, whose `DataType string` field carries the JSON tag
`data_type` and which is a field of `PredicateSchemaPayload` — a published payload, not an internal read.

Three consequences the design must price. None is detectable by `task api:compat`, because no signature changes —
only emitted values do:

1. Closing the vocabulary and remapping the eight legacy spellings (`time.Time`/`timestamp` collapse, `int`/`int64`
   collapse) changes the `data_type` string semsource emits on its manifest payload. That is a behavioural change
   in a sister's published wire format, and belongs in a SemStreams-owned `docs/operations/migration-*.md` note
   under the repository-ownership boundary; the sister's owner implements it.
2. `Register` rejecting an unknown `DataType` (scope item 1) turns semsource's currently-accepted registrations
   into init-time panics if any spelling falls outside the closed set. The closed set must therefore be derived
   from the measured union of in-repo AND sister spellings, or the rejection must be staged.
3. `PredicateDescriptor` also carries a `Role string` whose comment names an inline closed vocabulary
   ("identity", "content", "location", "relationship", "metric", "metadata"). That is prior art for a closed role
   vocabulary living downstream of this registry, and the natural consumer for A4 below.

### A2. Both registration entry points already share ONE validation home

- `vocabulary/registry.go:318` — `func Register(name string, opts ...Option) {`
- `vocabulary/registry.go:384` — `func RegisterPredicate(meta PredicateMetadata) {`
- `vocabulary/registry.go:339` — `	if err := validatePredicateMetadataLocked(meta); err != nil {`
- `vocabulary/registry.go:402` — `	if err := validatePredicateMetadataLocked(meta); err != nil {`
- `vocabulary/registry.go:423` — `func validatePredicateMetadataLocked(meta PredicateMetadata) error {`

`RegisterPredicate` takes the struct directly, bypassing the `With*` options entirely, and both entry points route
through the same validator. That validator is the seam scope item 1 needs: a closed-set check added there covers
both paths and requires no change to `WithDataType`'s signature, satisfying the owner's constraint exactly.
Enforcing inside `WithDataType` alone would leave the struct-literal path open — and three sisters use that path:
semlink at `internal/projector/contracts.go:105`, `internal/rules/contracts.go:46` and `internal/cop/contracts.go:91`
(all `RegisterPredicate(vocabulary.PredicateMetadata{Name: predicate})`); semmachina at
`internal/vocabulary/registration.go:123`; semteams at `cmd/semteams/vocab/vocab.go:35`. semmachina additionally
writes `metadata.DataType = "string"` as an unconditional default at `internal/vocabulary/registration.go:128`.

### A3. `Register` is amend-not-replace, so a DataType can be inherited from an earlier registration

- `vocabulary/registry.go:330` — `	meta := predicateRegistry[name]`

Options amend rather than replace (gh#410), so a re-`Register` that omits `WithDataType` keeps the previously
declared value. A contract test asserting "every registered predicate carries a DataType from the closed set"
(scope item 3) will therefore pass for predicates that acquired the field from a different, earlier registration
call. Both the guard and the closed-set validation must account for that ordering effect.

### A4. `Role` is a FOURTH write-only field on the same frozen struct, and its doc comment asserts a reader that does not exist

- `vocabulary/registry.go:256` — `// fusion ranker this comment previously named as its consumer reads Weight`
- `vocabulary/registry.go:267` — `		m.Role = role`
- `pkg/fusion/fusionvocab/signals.go:48` — `		return meta.Weight`

The doc comment claims consumers read `PredicateMetadata.Role`; the cited consumer reads `Weight` instead. Only
four files outside `vocabulary/` reference `PredicateMetadata` at all — `examples/processors/iot_sensor/
vocabulary_test.go`, `pkg/fusion/fusionvocab/signals.go`, `pkg/fusion/rank_signals.go`,
`processor/rule/config_validation_test.go` — and none of them reads `.Role`. No sister reads it either. `Role`
sits on the same ADR-106 Tier 1 frozen struct as the three named fields, so scope item 6's "never left write-only"
reasoning applies to it identically, yet #1267's scope does not name it. Whether `Role` joins this pass is an
owner scope question, not a design choice.

### A5. Searches run for this addendum

Recorded with the rest of the file's searches under `## Searches` above, tagged `(addendum)`.

Discipline note, first attempt discarded: the initial sister sweep passed `--include=*.go` unquoted, zsh
glob-expanded it, and every repo reported `refs=0`. A broken sweep and a genuinely empty one are indistinguishable
in the output. The sweep was re-run quoted, with a semstreams positive control returning 100 hits, proving the
pattern matched before any zero was believed.
