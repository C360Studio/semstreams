# gh#1100 — Single type authority: design

Baseline `origin/main` `c3a17741` (2026-08-26); revision 3 re-premised at `7e7ea76e`. Companion inventory: `docs/proposals/gh1100-type-authority-inventory.md`
(awaiting independent `INVENTORY PASS`; the design is conditional on it). ADR draft:
`docs/adr/103-payload-registry-is-the-single-type-authority.md`. Target state: `openspec/changes/single-type-authority/`.
Status: **ACCEPTED** — owner ruling on #1100, 2026-08-26 (recorded on the issue): O-1…O-18 as recommended, with three
overrides (O-6, O-11/O-12, O-17) and explicit acceptances; revision 5 applies them (§18, owner ruling).

## 1. The decision (as the owner stated it, 2026-08-26)

- Inventory: `docs/proposals/gh1100-type-authority-inventory.md`, SHA-256
  `59eb4ac2f7a089751ea9363ef69c10f6e7bdcbd6bd1d3bf282314c9adc82a517` (revision 5). Review state: **INVENTORY PASS WITH DIVERGENCES** (blind, Fable, 2026-08-26);
  D1–D3 corrected, L1–L5 added in revision 2. **Pre-owner design review round 1 (Fable, adversarial, 2026-08-26): REQUEST
  CHANGES** — revision 3 folds B-1 (→ O-16), F-1…F-6, N-1…N-8 and the nits; every item has a disposition in §18. Re-premised
  against `origin/main` `7e7ea76e` (#1099 and #1104 merged; milestone `v1.0.0-beta.163` holds #1100). **Narrow re-review round 2
  (2026-08-26): APPROVE WITH CHANGES** — B-1 mechanically closed; revision 4 folds F1–F7 and the notes (§18, round 2). **Owner ruling 2026-08-26:** O-1…O-18 as
  recommended; overrides O-6 (complete eight-tier gate + the web-observation integration test), O-11/O-12 (sisters read-only;
  obligations in `docs/operations/migration-beta162-to-beta163.md`), O-17 (fill the stamp from the contract, now); explicit
  acceptances O-16 (a) with the factory check, O-14, O-15, O-18, and #1095's permanent hierarchy foreign-authority skip — **r5
  applies** (§18, owner ruling).

> The payload registry is the single type authority; a projection contract and an indexing-profile floor are attributes
> registered WITH the type, not parallel tables; `EntityState.MessageType` is therefore always a registered key, and ingest
> rejects a stamp the registry does not know.

Everything below is mechanics that follow from that sentence. The only places the design adds a judgement are marked as
owner items (§15).

## 2. Options considered

| # | Shape | Cost | Verdict |
|---|---|---|---|
| A | Attributes on `payloadregistry.Registration` (`IndexingProfile`, `Contracts`); the contract data types move to a leaf package so the registry can hold them without an import cycle; ingest reads floor and registration from the registry it already holds | one new leaf package under `pkg/` (owner review required); 2 additive fields; the floor table and `internal/builtinprojection` deleted | **recommended** — the only shape in which the owner's "registered WITH the type" is literally true and the generic layer's "no domain imports" reason (`indexing_profile_registry.go:16-22`) is preserved by construction |
| B | Keep three tables; add a boot-time cross-check `ValidateContracts(reg, contracts)` and a floor-key check | small; no leaf split | rejected by the ruling ("not parallel tables"); also keeps three places to edit per type |
| C | `Registration.ContractName string` referencing a contract held elsewhere | tiny | rejected — a linkage by naming coincidence (`.agents/contracts/semstreams-architect.md:156-158`) |
| D | Retire `Contract.MessageType` and key every contract from the registry only | removes one spelling | rejected for this wave — five sisters set the field (§11); nothing in the ruling needs it; see owner item O-13 |
| E | Do nothing | — | the issue's consequences stand; #1095 slice B cannot import a lesson |

Premise for A (**L1**): `pkg/projection.MutationClient.Create` has zero production callers in-tree (only e2e
`graph_roundtrip.go:105`, `lessons/scenario.go:388`); the six framework stampers call `internal/graphmutation/client.go:89`
directly, so the client-side check at `mutation_client.go:322-326` protects only sisters using `CreateMutation` (semmachina,
semdev) and e2e. An ingest-side gate is the **only** check that covers the framework's own writers; a client-side-only design
would leave every in-tree birth unchecked.

Extending an existing surface (A) rather than adding a channel beside it: the registry already exists per binary
(`component/dependencies.go:74`), graph-ingest already holds it (`component.go:692`), and every one of the 22 floor keys is
already a registration (inventory §0).

## 3. `Registration` shape

```go
// payloadregistry/registry.go
type Registration struct {
    Factory     Factory        `json:"-"`
    Builder     Builder        `json:"-"`
    Domain      string         `json:"domain"`
    Category    string         `json:"category"`
    Version     string         `json:"version"`
    Description string         `json:"description"`
    Example     map[string]any `json:"example"`

    // ADR-103 — attributes registered with the type.

    // IndexingProfile is the ADR-054 channel-(c) floor graph-ingest stamps on an
    // entity born with this type when the producer declares none. Empty means
    // the type declares no floor: ingest applies control and meters the gap.
    IndexingProfile string `json:"indexing_profile,omitempty"`

    // Contracts are the projection contracts bound to this type. Each names
    // this registration's key (an empty MessageType is filled at Register);
    // a contract naming another key is a registration error.
    Contracts []contract.Contract `json:"-"`
}
```

`Register` (`registry.go:78-132`) gains, after the schema-consistency check: `IndexingProfile` must be empty or pass
`vocabulary.IsValidIndexingProfile`; for each contract, `MessageType` is filled with `reg.MessageType()` when empty and rejected
when different; contract names are unique within the registration; each contract passes the leaf's shape validation
(name, pattern, groups, birth predicates, profile — the current `Validate` minus predicate declaration, which stays at mutation-client
construction, `projection-mutation-client/spec.md:23-31`, so registration order relative to vocabulary is not an obligation).
`GetRegistration`/`List`/`ListByDomain` copy the new fields (deep copy of `Contracts`, matching the snapshot rule in
`agentic-lessons/spec.md:193-206`). New accessors: `IndexingProfileFor(key string) (profile string, registered bool)` and
`Contracts() []contract.Contract` (fresh copies, sorted by key then name).

Registered-without-a-floor is **metered, not rejected** (owner item O-5): rejecting would put an obligation on every existing
registration in every sister (~40) for a value ADR-054 already treats as a declaration of intent.

## 4. The leaf split — `pkg/projection/contract`

Measured: `payloadregistry` deps = `pkg/retry pkg/errs pkg/types` (leaf); `pkg/projection` deps include `graph`, `message`,
`natsclient`, `internal/graphmutation`; `message` imports `payloadregistry` — so `payloadregistry` cannot import `pkg/projection`.
But `pkg/projection/contract.go` imports only `pkg/types` and `vocabulary` (leaf: `pkg/platform`); `errors.go` holds the
`ErrInvalidContract` sentinel. Move `Contract`, `PredicateGroup`, `WriteMode`, `ModeReconcile`, `ModeAppend`, `ErrInvalidContract`,
`Validate`, `ValidateContracts`, `validateGroupName` into `pkg/projection/contract` (package `contract`); `pkg/projection` keeps
type aliases and re-exported constants/vars so every sister literal (`projection.Contract{…}`, `projection.ModeReconcile`)
compiles unchanged. Delete the leaf's private `validIndexingProfiles` (`contract.go:12-14`) in favour of
`vocabulary.IsValidIndexingProfile` — one home for the profile vocabulary. New exported package under `pkg/*` → owner design
review before implementation (contract rule; O-2).

## 5. The floor moves onto the type

`indexingProfileDefaults` (`indexing_profile_registry.go:30-65`) is deleted. Its 22 values become `IndexingProfile:` on the
registrations that already exist: `agentic/payload_registry.go:20-36` (16 keys, values verbatim from the table),
`processor/agentic-dispatch/payload_registry.go:42` (`signal`), `agentic/research/register.go:16-58` (6). `indexingProfileFloorFor`
becomes `c.payloadRegistry.IndexingProfileFor(mt.Key())`; graph-ingest retains `deps.PayloadRegistry` as a field beside the
decoder (a registry, not a context). Meaning of `indexing_profile_default_total{message_type}` after: "a registered type that
declares no floor received an entity with no producer declaration" — the label now points at a `Registration` literal. The
help string and `docs/adr/054` are not amended (history); the `graph-ingest` spec delta carries the new meaning.

**Floors are per-binary because registrations are (D1).** The six `research.*` floors exist only in a binary that selects
graph research (`cmd/semstreams/main.go:766-770`; `agentic/research/register.go:10-14`). This is intended, not a loss: a
binary that does not register a type can neither decode it on the fact lane nor birth it on the mutation lane (§6), so a
floor for it would be dead. The table it replaces was global text that described types some binaries never see. Owner item O-14.

**The `unknown` label (B-1).** Hierarchy containers are born with an empty type (`graph/inference/hierarchy.go:427-437`) and
meter as `indexing_profile_default_total{message_type="unknown"}` (`component.go:1876-1880`) on every deployment with
`enable_hierarchy: true`. Under O-16 option (a) that label stops firing for the framework's own writer and the metric's new
meaning holds without exception; under (b) the label persists and the spec delta must say the framework's containers are
what it names.

## 6. The ingest check

Seam: `processor/graph-ingest/canonical_mutations.go:207`, immediately after `IsValid` and before any clone or profile work:

```go
key := request.Entity.MessageType.Key()
if _, ok := c.payloadRegistry.GetRegistration(key); !ok {
    c.mutationRejections.WithLabelValues(subject, graph.ErrorCodeMessageTypeUnregistered).Inc() // via the existing reject path
    return nil, rejectInvalidDetail(graph.ErrorCodeMessageTypeUnregistered,
        map[string]any{"message_type": key},
        errors.New("entity message_type is not registered in this deployment's payload registry"))
}
```

- New closed code `graph.ErrorCodeMessageTypeUnregistered = "message_type_unregistered"` (`graph/mutation_responses.go:10-52`;
  class `errs.ErrorInvalid` — the caller registers the type, it does not retry).
- Mutation lane only: the fact lane already rejects at decode (`message/base_message.go:301-307`); `reconcile`/`append`/`delete`
  carry no type (`graph/mutation_requests.go:17-41`).
- A loud log names the key (a type key is not identity bytes).
- The factory (`component.go:646`) rejects a nil `deps.PayloadRegistry` at construction — today a nil registry surfaces at the
  first message (`message/decoder.go:39-44`); after ADR-103 it would also silently make every create fail, so it must be a boot error.
- `pkg/projection/mutation_client.go:322-327` is unchanged: the client does not predict registration; ingest observes it.
- **Two create paths, one gate (B-1).** Births reach `ENTITY_STATES` by two disjoint paths: the RPC lane above
  (`canonical_mutations.go:199-243`) and the in-process lane `Component.CreateEntity` (`component.go:1893-1896`) →
  `createEntityWithReceipt` (`:2081`; `ValidateEntityStateContract :2093`, `reconcileIndexingProfile :2121`, `entityBucket.Create
  :2132`), whose only external caller is the hierarchy container birth (`graph/inference/hierarchy.go:440`, via the adapter
  `component.go:451-456`) — with an **empty** `MessageType` (`:427-437`). A gate on the RPC handler alone leaves the framework's
  own writer unchecked and the owner's clause false at archive. The gate is therefore ONE helper,
  `(c *Component) requireRegisteredMessageType(entity *graph.EntityState) error`, called at `canonical_mutations.go:207` (RPC
  lane: wrapped as the coded rejection above) and at the top of `createEntityWithReceipt` before `ValidateEntityStateContract`
  (in-process lane: the **same** `*errs.ClassifiedError` — class invalid, code `message_type_unregistered`, detail
  `message_type` — returned to the caller; **not metered**, because `mutation_rejections_total` is labelled by RPC subject and an
  in-process birth has none; the observable is the caller's existing WARN: `hierarchy.go:440-451` returns the error without
  logging, and both graph-ingest callers WARN and continue without the container, `component.go:1971`, `:2108`). Every birth
  passes the same check. Births and mutations reach `ENTITY_STATES` through **six** `entityBucket` writers (`git grep -nE 'entityBucket\.[A-Z]\w*\('`, non-test — the earlier `Create|Put|Update` filter could not match `UpdateWithRetry`): `canonical_mutations.go:243` (RPC create), `:306` (RPC reconcile, must-exist), `component.go:1985` (`MergeEntity`, **birth-capable** through the `len(current)==0` branch `:1993-2000`), `:2132` (in-process create), `:2311` and `:2495` (`AddTriple`/batch, must-exist); plus `:2174` `DeleteAtRevision`. **Four are birth-capable, and one of those is decode-gated:** `MergeEntity`'s sole caller is `ingestEntity` `:1633`, reached only through `c.decoder.Decode` `:1599` → `extractEntityFromMessage` `:1704` (`MessageType: msg.Type()` `:1732`), so a fact-lane birth carries a registered key by construction (ADR-103 d3) and `:1985` needs no helper. The two births that need the helper are `canonical_mutations.go:243` and `component.go:2132`. What the container then carries is owner item **O-16**:
  - **(a) — recommended.** Stamp containers with a registered framework type `graph.hierarchy_container.v1`: a verbatim
    carrier `graph/inference.ContainerEntity{ID string; Facts []message.Triple}` with `HierarchyContainerMessageType()`, floor
    `control` (ADR-054 §7 machinery), `inference.RegisterPayloads` wired into `payloadbuiltins.Register` (import direction
    measured: `graph/inference` reaches neither `payloadbuiltins` nor `graph-ingest`, and `payloadbuiltins` does not reach
    `graph/inference`); one line at `hierarchy.go:428`. Grounding: the owner's clause admits no exception; the delta's metric
    meaning needs the framework's own writer to stop emitting `unknown`; containers are ruled to retire with gh606 (#1095 O-6,
    its design `:143,365`) so the type retires with them — one registration to delete — whereas an exception written into an
    ADR outlives the code it excused; cost ≈ 20 lines. Covering tiers: `e2e:structural` (`configs/e2e-structural.json:480`)
    and `e2e:agentic` (`configs/agentic.json:182`), both with hierarchy on. **Two costs of (a), stated (F7):** *(1) the do-nothing
    path.* A hierarchy-on graph-ingest whose registry lacks `graph.hierarchy_container.v1` — any registry not built by
    `payloadbuiltins.Register`, i.e. every sister composition root that builds its own — would have every Graphable birth's
    container refused, WARNed, and skipped (`component.go:1971`, `:2108`): hierarchy edges silently absent, query-visible. The
    component holds both facts at construction (`EnableHierarchy` and the registry), so the guard is a **factory error**, not a
    per-arrival WARN: construction fails naming `graph.hierarchy_container.v1` when hierarchy is on and the registry lacks it
    (`TestFactoryRejectsHierarchyWithoutContainerType`; forced omission (l)). *(2) dependency weight.* `payloadbuiltins →
    graph/inference` is not a cycle (measured 0/0) but drags `graph/inference`'s closure into every `payloadbuiltins` importer;
    the packages not already reached are: graph/inference graph/llm graph/structural.
  - **(b).** Carve the exception explicitly in ADR-103 d3 and the `graph-state-contract` delta ("framework-internal container
    births carry no stamp; the `unknown` label names them") and exempt the empty type on the in-process lane for the hierarchy
    caller only. Cheaper by a type, but the invariant is false at archive, the in-process helper grows a caller-specific branch,
    and the exception must be remembered when gh606 lands.
- **The contract-bound client fills the stamp (O-17, ruled DO NOW).** `pkg/projection.MutationClient.Create` (`mutation_client.go:133`)
  fills an empty `entity.MessageType` from `binding.contract.MessageType` before `validateEntity` (`:146` → `:308-327`) and
  before the request is built (`:164`); the existing equality check (`:325-326`) becomes the conflict branch — a non-empty
  stamp that differs from the contract is rejected with a classified invalid error naming both keys; a contract with no
  `MessageType` and an entity with no stamp stays rejected (`:322-323`). Adopter-seam consequence: a product using
  `CreateMutation` may omit the stamp entirely — the contract it already bound is the only spelling of the type. Tests
  `TestCreateFillsMessageTypeFromContract`, `TestCreateRejectsConflictingMessageType`; forced omission (n).
- **Nil registry at the seam is fail-closed (L2).** `c.payloadRegistry == nil` at the create seam → `rejectInternal`
  (`mutation_runtime.go:206-208`, code `internal`) with an ERROR log naming the missing dependency; never a pass-through
  (fail-open would be a zero standing in for UNKNOWN). 23 `&Component{` literals in six ingest test files bypass the factory
  (inventory §2.4); a fixture helper sets the registry on each and lands in the same change (tasks 5.4). Owner item O-15.
- **The gate is create-only and touches no read or merge path (L3).** The Graphable merge branch (`component.go:2036`)
  assigns the arrival's decoded — therefore registered — type and consults nothing; the boot sweep, codec, exact reads, and
  must-exist operations never validate the type (§10). A running deployment holding the six stamps needs no migration, and
  this is not a pre-v1 storage cutover under the fresh-state rules.

## 7. The six framework types as registered Graphable payloads

Serialization rule for all six: the wire form is the struct's fields; **every triple object is a field**; `EntityID()` derives
from the identity fields through the existing builder; `Triples()` is the ONE builder (moved from the writer package beside the
type) and is **byte-identical to today's builder for that type** — `Source`, `Confidence`, the predicate set, and the
conditional-emission rules are reproduced exactly (reviewer-verified builder-by-builder for loop execution, lesson, model
endpoint, and diagnosis; the web observation's two-builder shape is below); the only regenerated value is `Triple.Timestamp`
(stamped at `Triples()` time — arrival on decode, the same as every Graphable today). Two facts the byte-identity rule pins:
the ops diagnosis stamps `Confidence: args.Confidence` on **every** triple (`emit_diagnosis.go:259-265`), not `1.0`, so
`OpsDiagnosisEntity.Triples()` does the same; and the web observation has two sources. `MarshalJSON` wraps in `BaseMessage`
with the alias idiom; `Schema()` returns the key; `Validate()` checks identity fields. Factory: `func() any { return &T{} }`.

**Contract relation (F-1).** For every contract registered with a type, birth(C) ⊆ predicates(`Triples()` of a fully populated
entity) ⊆ birth(C) ∪ groups(C). Equality is unsatisfiable and was wrong in r2: `LoopExecutionEntity.Triples()`
(`loop_execution_entity.go:91-151`) never emits `TodoRecord` (the `todos` group), and the lesson builder
(`emit_lesson.go:693-741`) never emits `LessonSupersededBy`/`LessonRetiredAt` (the lifecycle group) but does emit
`LessonStatus` at birth, which sits in the group. `TestRegisteredContractMatchesTriples` asserts the two inclusions; the drift
scenario it keeps is "a **birth** predicate removed from the builder but not from the contract is caught" (a group predicate
absent at birth is admitted by design).

| Key | Struct (package `agentic` unless noted) | Identity → `EntityID()` | `Triples()` moved from | Floor | Contract on the registration |
|---|---|---|---|---|---|
| `agentic.loop_execution.v1` | `LoopExecutionEntity` (exists, `loop_execution_entity.go:68-73`: Org, Platform, LoopID, Task) — add JSON tags, `Schema`, `Validate`, `MarshalJSON` | `LoopExecutionEntityID` | already beside the type (`:91-151`) | `control` (ADR-054 §7 "run entities") | `LoopExecutionContract()` — the literal at `internal/builtinprojection/contracts.go:23-46`, moved to `agentic`; group `todos` |
| `agentic.agent_lesson.v1` | `AgentLessonEntity{Org, Platform, ID, Category, Polarity, Severity, Status, CreatedAt time.Time, Summary, Detail, InjectionForm, Evidence []string, AppliesTo []string, ObservedRole, ExecutedBy}` | `AgentLessonEntityID` | `emit_lesson.go:693-741` (`buildEmitLessonTriples`); `emit_lesson.go:518` constructs the entity and calls `Triples()`; source `ops-emit-lesson` (`:34`) | `content` (issue consequence 1; ADR-054 §7) | `LessonContract()` — `contracts.go:52-80` moved to `agentic`; `LessonProjectionContract()` (`lesson_promotion.go:52`) returns its copy |
| `agentic.ops_diagnosis.v1` | `OpsDiagnosisEntity{Org, Platform, ID, Finding, Recommendation, Confidence float64, Evidence []string, ObservedRole, Severity, ExecutedBy}` | `OpsDiagnosisEntityID` | `emit_diagnosis.go:249-291`; source `ops-emit-diagnosis` (`:26`); `Confidence` = the field on every triple | `content` (prose finding + recommendation for human review, ADR-027; O-3) | **O-4** — none (reviewer, recommended in r3) / `agentic.ops-diagnosis` birth contract over `OpsDiagnosisFinding, …Recommendation, …Confidence, …Evidence, …ObservedRole, …Severity, ActionExecutedBy` (`predicates.go:756-790,253`) (r2) |
| `agentic.model_endpoint.v1` | `ModelEndpointEntity{Org, Platform, Name, Provider, Model, URL, SupportsTools bool, MaxTokens int, InputPricePer1MTokens, OutputPricePer1MTokens float64, RequestsPerMinute int}` — plain fields, no `model` import into `agentic` | `ModelEndpointEntityID` | `graph_writer.go:511-548`; source `agentic-loop` (`:24`, equals `loopExecutionSource`) | `control` (config-derived, low cardinality) | **O-4** — none (reviewer, recommended in r3) / `agentic.model-endpoint` over `ModelProvider, ModelName, ModelSupportsTools, ModelMaxTokens, ModelInputPrice, ModelOutputPrice, ModelEndpointURL, ModelRateLimit` (`predicates.go:352-387`) (r2) |
| `agentic.web_observation.v1` | `WebObservationEntity{Org, Platform, CanonicalURL, Tool WebObservationTool, LoopEntityID; FetchedAt, ContentType string, StatusCode int, Text string, Truncated bool; Title, Snippet, SourceQuery, ObservedAt string}` — one struct, a **`Tool` discriminator** (`http_request` \| `web_search`) selects the source constant and the emitted set (F-2) | `TryWebObservationEntityID` (returns "" on error; ingest rejects an empty ID `component.go:1723`) | `Tool == http_request`: source `agent-http-request` (`httprequest.go:28`), always emits `WebURL, WebFetchedAt, WebFetchedBy, WebContentType, WebStatusCode, WebText, WebTruncated` — zero values included, byte-identical to `:257-266`; `Tool == web_search`: source `agent-web-search` (`websearch.go:31`), always emits `WebURL, WebTitle, WebSnippet, WebSourceQuery, WebObservedAt, WebObservedBy` (`:255-262`). Omission rule per field: **none** — each tool's set is unconditional and the other tool's fields are ignored; `Validate()` requires a known `Tool` | `content` (issue consequence 1) | **O-4** — none (reviewer, recommended in r3: `web_emit.go:69` appends through `internal/graphmutation`, so an append group would be consulted by nothing) / birth `WebURL` + append group `observation` (r2) |
| `lifecycle.harness.v1` | `lifecycle.HarnessEntity{ID string; Facts []message.Triple}` — a verbatim carrier (the harness's triples come from the registered workflow schema, `manager.go:399`), package `pkg/lifecycle`, `RegisterPayloads` added to `payloadbuiltins.Register` (`pkg/lifecycle` does not import `payloadbuiltins` — measured). Registering a carrier makes the **fact-lane merge path reachable** for lifecycle entities (same class as `storage.stored.v1`, N-6): a marshalled harness entity arriving on a Graphable input merges by predicate replacement like any other Graphable | the field | verbatim | `control` (ADR-054 §7 "harness") | none — per-workflow contracts stay with `Manager.Register` (`lifecycle/spec.md:10`) |
| `graph.hierarchy_container.v1` (**O-16 (a)**) | `inference.ContainerEntity{ID string; Facts []message.Triple}` — verbatim carrier in `graph/inference`; `HierarchyContainerMessageType()`; `inference.RegisterPayloads` in `payloadbuiltins` | the field | verbatim (`hierarchy.go:429-437`) | `control` | none |

`internal/builtinprojection` is retired; its four constants move to `agentic` (`LoopExecutionContractName`, `TodoGroupName`,
`LessonRecordContractName`, `LessonLifecycleGroupName`); `service.WireGraphRuntime` is called with `reg.Contracts()...` at both
composition roots (`cmd/semstreams/main.go:221`, `cmd/e2e-semstreams/main.go:154`) — the registry is the one table.

**O-4 — the three contracts that do not exist today (F-5).** Two positions for the owner. *r2 (architect):* mint
`ops-diagnosis`, `model-endpoint`, `web-observation` birth contracts now, with `TestRegisteredContractMatchesTriples` as their
consumer, so #818 has nothing to invent. *Reviewer, and the design's recommendation in r3:* **defer to #818** — the only consumer
would be the conformance test; none of the three writers goes through a contract-bound client (all call
`internal/graphmutation` directly, L1), and `web_emit.go:69` appends through it, so an observation append group is consulted by
nothing — phantom surface under this contract's own consumer-at-birth rule (`.agents/contracts/semstreams-architect.md:64-66`).
Under the recommendation `Registration.Contracts` is populated only where a contract exists today (loop execution, lesson), the
conformance test runs on those two, and the payload-registry delta makes the clause conditional on O-4. Unruled = defer.

## 8. Tests that change shape

- The four `_Distinct` tests are deleted. Their job is done by `Register`'s duplicate rejection (`registry.go:121-128`) exercised
  by `payloadbuiltins/register_test.go:10-13` on the full builtin set: a colliding category fails that test and the boot.
- One-table test `payloadbuiltins/single_type_authority_test.go` `TestPayloadRegistryIsTheSingleTypeAuthority`: builds the
  builtin registry; for each of the six keys (seven under O-16 (a)) asserts registered, non-empty floor, and — for loop
  execution and lesson, plus the three others only if O-4 = mint — a contract whose `MessageType` equals the key; asserts `reg.Contracts()` names are unique and equal the retired `builtinprojection` set (plus the three new only under O-4 =
  mint);
  asserts every registration's profile is empty or valid. The other two tables are gone at compile time (`indexingProfileDefaults`,
  `internal/builtinprojection`).
- `processor/graph-ingest/indexing_profile_registry_test.go` re-targets `IndexingProfileFor` on a registry built from
  `agentic`, `research`, and `agenticdispatch` `RegisterPayloads`, keeping every one of its 22 expectations (values preserved).
- Test registries: `payloadregistry.RegisterTestType(tb, reg, key string)` (beside `NewForTest`, `payloadregistry/testing.go`)
  registers a schema-less stub factory so `test.fixture.v1`/`test.widget.v1` pass the gate in unit tests. Measured:
  `go list -deps ./payloadbuiltins | grep -c processor/graph-ingest` → 0, so `package graphingest` tests may build their registry
  from `payloadbuiltins.Register` plus the stub helper; `newTestDependencies` (`processor/graph-ingest/metrics_test.go:147-156`)
  gains a `PayloadRegistry`. 13 test files construct `CreateEntityRequest{` (inventory §2.2).
- e2e (**L4**): `cmd/e2e-semstreams/fixtures.RegisterPayloads(reg)` registers the six e2e keys as verbatim carriers with floor
  `control` from `buildPayloadRegistry` (`main.go:358-378`) — without it every scenario that stamps them through the real wire is
  rejected (tier map §11); the ops direct-`PutKV` seed is unaffected; the ops seed's `agentic.loop-completed.1` (`ops/scenario.go:462`) becomes the
  registered `agentic.loop_completed.v1` (a mis-spelled key today; the direct `PutKV` remains — O-9).

## 9. ADR-076 families

They are not types. `graph/events.go` mints no `message.Type`; the alert/trigger families are entity-ID prefixes
(`events.go:19-20`, `processor/rule/graph_event_identity.go:12-15`), written by must-exist reconcile/append that carry no stamp.
Nothing joins the registry from ADR-076. (PREMISE P2.)

## 10. Day one for a deployment holding entities with unknown stamps

Measured: no reader consults the registry for a stored entity — the boot sweep uses the canonical codec only
(`component.go:1264-1290`), `ValidateEntityStateContract` checks ID/subjects/references (`entity_predicate_contract.go:134-175`),
graph-query/gateway never read the field, and `reconcile`/`append`/`delete` carry no type. So: reads unaffected; must-exist
mutations unaffected; a fact-lane re-arrival overwrites the stamp with its registered type (`component.go:2036`); only a new
`entity.create` carrying an unknown stamp is rejected. No migration; the pre-v1 fresh-state policy applies regardless. The
`graph-state-contract` delta pins that the codec and sweep never consult the registry, so retiring a type later cannot poison a graph.

## 11. BREAKING assessment

**BREAKING.** After this lands, an `entity.create` whose `message_type` is not registered in the receiving binary returns
`message_type_unregistered`. In-tree that is nobody once §7 and §8 land. Sisters (inventory §6): semmachina must register 4
types, semdev 2, semconnect 11, before adopting the tag; semsource and semteams stamp only registered or framework types;
semdragon (beta.135) and semmem (pre-rename module) are off the current wire. Exported surface grows only additively
(`Registration` fields, `pkg/projection/contract` + aliases, one error code, six payload types, `lifecycle.RegisterPayloads`,
`RegisterTestType`); `internal/builtinprojection` was never importable by sisters. **New import edge (L5):**
`payloadregistry → pkg/projection/contract → {pkg/types, vocabulary → pkg/platform}`; neither `payloadregistry` nor `message`
imports `vocabulary` today (measured 0/0), and `message` inherits the edge through `payloadregistry`. The package comment at
`payloadregistry/registry.go:1-16` ("imports only stdlib + pkg/errs + pkg/types") is rewritten to name it (tasks 3.2).
semmachina and semdev also birth `lifecycle.harness.v1` from inside their binaries (inventory §6, D2); registering it in
`payloadbuiltins.Register` covers them because both call it.

**Tag gate (O-6, owner override): the complete union.** `task e2e:agentic`, `task e2e:lessons`, `task e2e:structural`, `task e2e:ops`, `task e2e:research-graph`, `task e2e:lifecycle`, `task e2e:crud-tools`, `task e2e:core` — all eight green, each as a provenance-complete row (exact command, runner identity, UTC start/end) in the candidate-proof record per `openspec/specs/release-candidate-proof/spec.md`; and, until the web-observation tier exists (O-10), `go test -race -tags=integration -count=1 -run TestWebObservationBirthIsRegistered ./processor/agentic-tools/executors/` recorded as a row of its own. Per tier, what it exercises (the e2e-only keys in
parentheses are registered by `cmd/e2e-semstreams/fixtures`, tasks 6.1): `e2e:agentic` (`loop_execution`, `model_endpoint`), `e2e:lessons` (`agent_lesson`,
`test.fixture.v1`), `e2e:ops` (`ops_diagnosis`; the direct-KV seed is unaffected), `e2e:research-graph` (`loop_execution` via
llmwrap, `research.e2e_search_seed.v1`), `e2e:lifecycle` (`lifecycle.harness`), `e2e:crud-tools` (`e2e.probe.v1`), `e2e:core`
(`test.fixture.v1` roundtrip), `e2e:structural` (`e2e.eventtime.v1`, `e2e.canonical_create_contract.v1`, `e2e.relationship_contract.v1`;
also hierarchy containers, `configs/e2e-structural.json:480` — the covering tier for O-16 either way). **N-1:** `e2e:agentic`
asserts the loop execution entity (`test/e2e/scenarios/agentic/scenario.go:786-800`) but records a missing model endpoint only
as a **warning** (`:838-848`); tasks 6.4 promotes it to a failure so the tier covers `model_endpoint` for real. There is no
"minimum" subset: the BREAKING commit lands only behind all eight rows and the integration-test row. Minimum green before the BREAKING commit lands: `e2e:agentic` and
`e2e:lessons`; the full union runs in tasks §7. `web_observation` has no tier (inventory §9.1) — coverage gap filed (O-10);
its gate is the integration test `TestWebObservationBirthIsRegistered`.

Sister obligations (O-11/O-12, owner override: sister repositories stay **read-only** — no issues, comments, or edits) are
recorded in the SemStreams-owned `docs/operations/migration-beta162-to-beta163.md` (part of this package; one `##` per
landing so #1095's re-slot and reorder append their own), linked from `proposal.md` and the PR body: per sister the types
stamped at the pinned SHA, day-one breakage, the exact `RegisterPayloads` obligation with a template copied from
`storage/objectstore/stored_message.go:88-103` (semconnect: the host composition root, since `cmd/cs-api-server` holds no
registry), floor and contract to declare, and the decoder round-trip verification; semmem's finding is there marked for
downstream-owner validation.

## 12. Sequencing

- **#1095 (PR #1099, MERGED at `7e7ea76e` as a design package; ADR-102 Accepted; change 0/51 open).** Its change carries
  **no lesson-import scenario** — the lesson-factory dependency is semmem's federation MVP, not #1099's. The real overlap is
  with #1095's *implementation*: its tasks 5.1 (`:210-218`, the builder files — `agent_lesson_entity.go:68,92`,
  `web_observation_entity.go:79`, `ops_diagnosis_entity.go:56`) and 5.3 (`:223-229`, declaration patterns —
  `internal/builtinprojection/contracts.go:26,56`, which this change deletes, and the lesson prefix `:85-93`) edit the same five
  `agentic/*_entity.go` files; ADR-102's order rewrites every entity pattern (its inventory W5). **This change lands FIRST in the wave; #1095 slice A rebases onto it.** The moved contracts carry the
  **current** patterns verbatim — `*.*.agent.agentic-loop.execution.*` and `*.*.agent.lesson.record.*` — now in
  `agentic/loop_execution_entity.go` and `agentic/agent_lesson_entity.go`; #1095's **5.3** pointer re-targets to
  `agentic.LoopExecutionContract().EntityPattern` and `agentic.LessonContract().EntityPattern` (and its inventory line-13 line
  numbers shift once this change adds structs and `Triples()` to those files) and rewrites them under the new order (loop execution → `*.*.agentic-loop.agent.execution.*`, per its graph-ingest scenario
  `acme.dep1.agentic-loop.agent.execution.<uuid>`; lesson, diagnosis, and observation forms follow ADR-102 §1 positions 3–4
  `<component>.<reserved-domain>` — #1095 mints those literals, this design does not). In the five files the two changes touch
  disjoint functions (this: structs, `Triples()`, contracts, registrations; #1095: `EntityID` builders and prefixes), so the
  overlap is a rebase, not a design conflict. The `handleCanonicalCreate` seam is also shared (its authority gate inserts after
  `:207` too); both gates are pre-clone, order-independent.
- **#1093.** Both edit `cmd/semstreams/main.go`; merge order free.
- **#818.** Becomes implementable on `Registration.Contracts` without a parallel table; out of scope here; under O-4 = defer it
  also mints the three contracts.
- **#1104 (MERGED).** The skill, `docs/concepts/15`, `CLAUDE.md`, and `AGENTS.md` already teach `RegisterPayloads`; this change
  only adds floor, contracts, and `RegisterTestType` to that checklist (tasks 6.3).
- **#1092 / PR #1101.** `component.Registration` is a different registry; no overlap.

## 13. Named tests (RED at baseline unless stated)

`payloadregistry`: `TestRegisterRejectsInvalidIndexingProfile`, `TestRegisterFillsAndChecksContractMessageType`,
`TestGetRegistrationCopiesAttributes`, `TestContractsReturnsIndependentSortedCopies`, `TestIndexingProfileFor`. `pkg/projection/contract`:
`TestContractValidateUsesVocabularyProfiles`; `pkg/projection`: existing `contract_test.go` compiles against the aliases (GREEN at baseline,
documents the alias). `agentic`: `TestAgentLessonEntity_RoundTrip`, `TestOpsDiagnosisEntity_RoundTrip`, `TestModelEndpointEntity_RoundTrip`,
`TestWebObservationEntity_RoundTrip`, `TestLoopExecutionEntity_RoundTrip` (production decoder round-trip), `TestRegisteredContractMatchesTriples`
(table over the five). `pkg/lifecycle`: `TestHarnessEntity_RoundTrip`. `payloadbuiltins`: `TestPayloadRegistryIsTheSingleTypeAuthority`.
`processor/graph-ingest` (`-tags=integration`): `TestCreateRejectsUnregisteredMessageType` (decode the reply into a fresh value;
code `message_type_unregistered`, `detail.message_type` = key; no `ENTITY_STATES` key; `mutation_rejections_total{reason}` +1),
`TestCreateAcceptsRegisteredMessageType`, `TestFloorComesFromRegistration`, `TestFactoryRejectsNilPayloadRegistry` (unit),
`TestCreateSeamRejectsWhenRegistryMissing` (unit; a `&Component{}` literal without a registry answers `internal`, no panic),
`TestInProcessCreateRejectsUnregisteredType` (unit; `Component.CreateEntity` with an unregistered type returns an invalid error,
no write), `TestHierarchyContainerBirthCarriesRegisteredType` (`-tags=integration`, `enable_hierarchy: true`; the container is
created with `message_type` `graph.hierarchy_container.v1` and `indexing_profile_default_total{message_type="unknown"}` does not
increment — under O-16 (b) the same test asserts the empty stamp and the `unknown` label instead), `agentic`:
`TestWebObservationEntityMatchesToolBuilders` (per tool, byte-identity with the former `httprequest.go`/`websearch.go` sets
including source and zero-valued triples), `TestModelEndpointEntityMatchesBuilder` (golden from `graph_writer.go:511-548`: the
five zero-gates `:529-542` and the `bool`/`int`/`float64` objects — a dropped `if ep.MaxTokens > 0` fails it),
`TestOpsDiagnosisEntityMatchesBuilder` (golden from `emit_diagnosis.go:249-291`: the full set, the `fmt.Sprintf("%g")` confidence
object `:262`, and `Confidence` on every triple), `TestFactoryRejectsHierarchyWithoutContainerType` (unit; `EnableHierarchy`
with a registry lacking `graph.hierarchy_container.v1` does not construct), `pkg/projection`: `TestCreateFillsMessageTypeFromContract`
(empty stamp → the request carries the contract's key; MUST fail at baseline: `validateEntity :322-323` rejects the empty stamp),
`TestCreateRejectsConflictingMessageType` (GREEN at baseline — the existing `:325-326` check, now the conflict branch),
`TestWebObservationBirthIsRegistered`. `processor/agentic-tools`: `TestEmitLessonBuildsEntityTriples` (equality with the former builder's output).

## 14. Forced omissions (one per new registration path, check, or builder)

Delete `agentic.RegisterPayloads`'s lesson row → `TestPayloadRegistryIsTheSingleTypeAuthority` and `e2e:lessons`; delete
`lifecycle.RegisterPayloads` from `payloadbuiltins.Register` → `TestPayloadRegistryIsTheSingleTypeAuthority` and `e2e:lifecycle`;
delete the registry lookup at the create seam → `TestCreateRejectsUnregisteredMessageType`; delete `IndexingProfile:` on the
lesson registration → `TestFloorComesFromRegistration`; delete one predicate line in `AgentLessonEntity.Triples()` →
`TestRegisteredContractMatchesTriples` and `TestEmitLessonBuildsEntityTriples`; delete the nil-registry guard in the factory →
`TestFactoryRejectsNilPayloadRegistry`; delete the nil-registry guard at the seam → `TestCreateSeamRejectsWhenRegistryMissing`
(panic or pass-through); delete the e2e fixtures registration → `e2e:core` and `e2e:structural`; remove the
`payloadReg.Contracts()...` spread from `service.WireGraphRuntime` at a composition root (N-3, the wiring, not the primitive)
→ boot MUST fail with `projection: invalid contract: no contracts` (`contract.go:102-104`) and `e2e:core` MUST fail; delete the
helper call on the in-process lane → `TestInProcessCreateRejectsUnregisteredType`; delete `inference.RegisterPayloads` from
`payloadbuiltins.Register` → `TestHierarchyContainerBirthCarriesRegisteredType`; delete the hierarchy factory check →
`TestFactoryRejectsHierarchyWithoutContainerType`; delete one `if ep.X > 0` gate in `ModelEndpointEntity.Triples()` →
`TestModelEndpointEntityMatchesBuilder`; drop the fill in `MutationClient.Create` → `TestCreateFillsMessageTypeFromContract`.

## 15. Owner items — RULED 2026-08-26 (O-1…O-18 as recommended unless marked OVERRIDE)

- **O-1** Accept ADR-103 as worded (§1); flip Status.
- **O-2** Every new export, for owner design review (contract rule; F-6): `pkg/projection/contract` (package: `Contract`,
  `PredicateGroup`, `WriteMode`, `ModeReconcile`, `ModeAppend`, `ErrInvalidContract`, `Validate`, `ValidateShape`,
  `ValidateContracts`) with aliases in `pkg/projection`; `payloadregistry.Registration.{IndexingProfile, Contracts}`,
  `(*Registry).IndexingProfileFor`, `(*Registry).Contracts`, `RegisterTestType`; `graph.ErrorCodeMessageTypeUnregistered`;
  `pkg/lifecycle.{HarnessEntity, HarnessMessageType, RegisterPayloads}`; `agentic.{AgentLessonEntity, OpsDiagnosisEntity,
  ModelEndpointEntity, WebObservationEntity, WebObservationTool, WebObservationToolHTTPRequest, WebObservationToolWebSearch,
  LoopExecutionContract, LessonContract, LoopExecutionContractName, TodoGroupName, LessonRecordContractName,
  LessonLifecycleGroupName}` (the `Tool` constants are exported because `processor/agentic-tools/executors` sets them; the four
  source constants — `ops-emit-lesson`, `ops-emit-diagnosis`, `agent-http-request`, `agent-web-search` — move into `agentic`
  **unexported** beside their types, selected inside `Triples()`); under O-16 (a)
  `graph/inference.{ContainerEntity, HierarchyContainerMessageType, RegisterPayloads}`; under O-4 = mint
  `agentic.{OpsDiagnosisContract, ModelEndpointContract, WebObservationContract}`.
- **O-3** Floors: lesson `content`, web observation `content`, ops diagnosis `content`, loop execution `control`, model endpoint
  `control`, harness `control`. Confirm ops diagnosis.
- **O-4** The three birth contracts that do not exist today: architect r2 = mint here; reviewer and r3 = **defer to #818**
  (§7); unruled = defer.
- **O-5** A registered type without a floor: meter (recommended) or reject at `Register`.
- **O-6** **OVERRIDE.** The tag gate is the complete §7.3 union — `task e2e:agentic`, `task e2e:lessons`, `task e2e:structural`, `task e2e:ops`, `task e2e:research-graph`, `task e2e:lifecycle`, `task e2e:crud-tools`, `task e2e:core` — all eight green, each as a provenance-complete row (exact command, runner identity, UTC start/end) in the candidate-proof record per `openspec/specs/release-candidate-proof/spec.md`; and, until the web-observation tier exists (O-10), `go test -race -tags=integration -count=1 -run TestWebObservationBirthIsRegistered ./processor/agentic-tools/executors/` recorded as a row of its own (§11, tasks 7.3, conformance).
- **O-7** Wave order: this change lands first; #1095 slice A rebases its 5.1 and **5.3** onto the moved contracts (§12). Confirm.
- **O-8** (premise closed by #1104) The rewritten checklist knows no floor, contract, or `RegisterTestType`; tasks 6.3 adds
  them to `.agents/skills/new-payload/SKILL.md` and `docs/concepts/15-payload-registry.md` (the checklist block byte-identical
  between the two; `.claude/skills/new-payload/SKILL.md` is a thin adapter and is untouched).
- **O-9** `test/e2e/scenarios/ops/scenario.go:459-470` seeds `ENTITY_STATES` by direct `PutKV` with a mis-spelled key — fix the
  key here; the direct write is a separate hygiene issue.
- **O-10** `web_observation` births have no e2e tier — file the coverage gap.
- **O-11 / O-12** **OVERRIDE.** Sister repositories remain read-only — no sister issues or comments, ever. Every sister impact
  and migration instruction (semmachina, semdev, semconnect, semteams, semmem; semsource and semdragon as not affected) is
  recorded in `docs/operations/migration-beta162-to-beta163.md`; semmem's finding is there marked "for downstream-owner
  validation".
- **O-13** `Contract.IndexingProfile` retained; when both it and the type's floor are set they must agree (validated at
  `Register`). Retire later or keep?
- **O-14** Floors are per-binary because registrations are (§5, D1): the six `research.*` floors exist only where graph research
  is selected. **Explicitly accepted.**
- **O-15** Nil registry at the create seam is fail-closed (`internal`, ERROR log) with the test-fixture sweep in the same
  change (§6, L2). **Explicitly accepted.**
- **O-16** Hierarchy containers (B-1): **(a) recommended** — stamp `graph.hierarchy_container.v1`, registered, floor `control`,
  transitional until gh606 retires containers, **with the factory check** (hierarchy on + registry lacking the type = construction
  error) and the dependency cost named in §6; **(b)** carve the exception in ADR-103 d3 and the `graph-state-contract` delta.
  **Explicitly accepted: (a) with the factory check.**
- **O-17** **OVERRIDE — DO NOW, in this change.** `MutationClient.Create` fills an empty `entity.MessageType` from the bound
  contract; a non-empty conflicting stamp is rejected (the existing equality check becomes the conflict branch). §6, the
  `projection-mutation-client` delta, tasks 3.4, omission (n), conformance O-17.
- **O-18** (N-8) ADR-102's `RegisterEntityDomains` (#1095 design `:174`) is a second boot-time registration surface keyed by
  entity-ID domain — a different namespace, no contradiction with ADR-103. **Explicitly accepted: two distinct registration
  acts** (payload types; entity-ID domains). Also accepted from #1095: the permanent hierarchy-inference skip for
  foreign-authority entities.

## 16. PREMISE FAILED (claims in the issue or comments that do not survive measurement)

- **P1** "five mutation-only stamps" — six in-tree: `lifecycle.harness.v1` (`pkg/lifecycle/manager.go:24-28,400-407`), plus
  seven test-only keys and one mis-spelled direct-KV seed (inventory §2.2).
- **P2** "ADR-076 … families are registered — compare" — they are entity-ID families, not types; `graph/events.go` mints no
  `message.Type`; `graph.events.entity.create` has no consumer.
- **P3** The registry's "unknown types … falling back to GenericPayload" (`payloadregistry/registry.go:134-137`) is a stale
  comment: `message/base_message.go:301-307` rejects. The fact lane already enforces the ruling; this change extends it.
- **P4** "Three of five have no birth contract" — count holds; the two that have one hold it in an **internal** package, and
  two sisters re-declare the structure with the framework's key builders (`semteams/cmd/semteams/main.go:971,998`,
  `semdev/internal/graphown/contracts.go:444`).
- **P5** "`emit_lesson.go:236` … the only place the stamp does semantic work" — also `pkg/projection/mutation_client.go:322-327`
  and semdragon `questdag/unit.go:599`; no query-side reader exists.
- **P6** "Prerequisite for #1095 slice B (the import lane)" — corrected twice: PR #1099 merged as a design package with **no**
  lesson-import scenario; the lesson-factory dependency belongs to semmem's federation MVP. The real relation to #1095 is the
  implementation overlap in §12 (this lands first; slice A rebases).
- **P7** "keyed by string so the generic layer would not import the domain packages" — the layer already imports the registry
  (`component.go:692`); no new import is needed for the floor to move.
- **P8** "a test walks all three tables" — after the change there is one; the test asserts the other two are gone.
- **P9** `pkg/projection/contract.go:12-14` duplicates the profile vocabulary (`vocabulary/predicates.go:325-332`) — a fourth
  spelling the issue did not list.
- **P10** (reviewer B-1) The issue's "graph-ingest checks a stamp only syntactically … and persists it" is true of the RPC lane
  only; graph-ingest's own in-process lane (`Component.CreateEntity`, hierarchy containers) persists an **empty** stamp with no
  check at all (`graph/inference/hierarchy.go:427-440`, `component.go:1893-1896,2081-2132`).
- **P11** (reviewer F-1) r2's own claim "birth ∪ group predicates equal the predicate set `Triples()` emits" was unsatisfiable
  for loop execution and lesson; the relation is the two inclusions in §7.

## 17. Decision skills applied

`new-payload` — applied to all six types (§7); its checklist is itself stale (O-8). `kv-or-stream`, `entity-or-bucket`,
`orchestration-check`, `query-pattern` — not triggered: no new durable state, communication path, orchestration, or query access.
Context ownership: graph-ingest retains a registry, not a context; no new goroutines or lifecycle records.

## 18. Review dispositions — pre-owner design review round 1 (Fable, adversarial, 2026-08-26)

| Item | Disposition in revision 3 |
|---|---|
| B-1 third writer lane (hierarchy containers, empty type; RPC and in-process create paths disjoint) | **Accepted, measured** (inventory §2.2/§2.4). Gate becomes one helper on both create paths (§6); O-16 added with (a) recommended and grounded, (b) documented; `e2e:structural`/`e2e:agentic` named; every other in-process writer enumerated (three bucket writers, one `CreateEntity` caller) |
| F-1 contract relation unsatisfiable | **Accepted.** Relation restated as birth ⊆ Triples(full) ⊆ birth ∪ groups (§7, delta, test); drift scenario kept; P11 |
| F-2 web observation shape / diagnosis confidence | **Accepted.** One struct with a `Tool` discriminator selecting source and set; per-field omission rule = none; diagnosis `Confidence: args.Confidence`; byte-identity rule stated for all builders (§7) |
| F-3 stale premises vs `7e7ea76e` | **Accepted.** O-6, O-7, O-8, P6, §12 rewritten; overlap with #1095's implementation and the rebase order stated; patterns the moved contracts carry stated; #1104 folded (tasks 6.3); AGENTS.md Land bullet `:68-73` |
| F-4 no scenario names its test | **Accepted.** Every scenario in the six deltas now ends with the verifying test (existing names where the behaviour is unchanged) |
| F-5 three new contracts required before O-4 is ruled | **Accepted.** Delta clause conditional on O-4; both recommendations presented; r3 recommends defer (§7, O-4) |
| F-6 unrouted exports | **Accepted.** O-2 enumerates every new export |
| N-1 model endpoint only a warning in `e2e:agentic` | **Accepted.** Tasks 6.4 promotes to a failure; §11 |
| N-2 semconnect has no registry | **Accepted.** Inventory §6 and tasks 7.5 reworded: export `RegisterPayloads` from `gateway/cs-api`, host calls it |
| N-3 forced omission for the composition-root wiring | **Accepted.** §14 and tasks 7.1 (i): boot fails `no contracts` |
| N-4 builtinprojection consumers are 9 files | **Accepted.** Tasks 4.7 lists all seven consumer files (plus the package's two) |
| N-5 client-side stamp check is prediction-shaped | **Accepted as O-17** (candidate; not implemented here) |
| N-6 lifecycle carrier makes the merge path reachable | **Accepted.** §7 row and the lifecycle delta say so |
| N-7 the truly new edge is `vocabulary` itself | **Accepted.** Tasks 3.2: the package comment names `vocabulary` (five `init()`s, a global predicate registry); `message` already imports `pkg/platform` |
| N-8 ADR-102's `RegisterEntityDomains` | **Accepted as O-18** (one line for the owner) |
| Nits: two D1 rows; "O-1…O-13"; "once it is CLOSED" | **Accepted.** Conformance second row renamed DV1; tasks header O-1…O-18; wording: the RED task is flagged while OPEN by design |

### Round 2 — narrow re-review (2026-08-26): APPROVE WITH CHANGES

| Item | Disposition in revision 4 |
|---|---|
| F1 writer enumeration was a broken-filter claim (`UpdateWithRetry` unmatched) | **Accepted, re-measured.** Six writers, four birth-capable, one decode-gated — stated in §6, inventory §2.4, tasks premises, with why `:1985` needs no helper |
| F2 #1095 pointer is in its 5.3, not 5.1 | **Accepted.** §12 and O-7 say 5.1 and 5.3; the re-target is to `LoopExecutionContract().EntityPattern` / `LessonContract().EntityPattern`; line-13 shift noted |
| F3 §8 still said "plus the three new" | **Accepted.** Made O-4-conditional |
| F4 byte-identity clause had no scenario; model endpoint and diagnosis had no golden test | **Accepted.** Scenario added in the payload-registry delta; `TestModelEndpointEntityMatchesBuilder` and `TestOpsDiagnosisEntityMatchesBuilder` (full set, `%g` object) added; forced omission for a dropped zero-gate |
| F5 two readings of the gate MUST (RPC vs in-process) | **Accepted.** Code/detail/metric/log scoped to the RPC lane; in-process outcome stated: same classified error to the caller, not metered, caller's WARN is the observable; requirement header renamed "A birth MUST…" |
| F6 grep named at tasks 7.2 was absent | **Accepted.** Added to 7.2 |
| F7 O-16 (a) do-nothing path unstated; caller description wrong | **Accepted.** `hierarchy.go:440-451` returns without logging; callers WARN and continue (`component.go:1971`, `:2108`); the guard is a factory error (task 4.8, `TestFactoryRejectsHierarchyWithoutContainerType`, omission (l)); named as an (a) cost with the dependency weight |
| Notes: ADR chatter; "six keys"; O-2 web constants; (a) closure weight; F-1 over-claim; omission lettering | **Accepted.** ADR reduced to decision + consequences (review state and wave order live in §12/§18); "six (seven under O-16 (a))"; `Tool` constants exported, sources unexported; closure delta named; "a **birth** predicate"; N-3 is (i) |

### Owner ruling (2026-08-26, recorded on #1100) — r5 applies

| Ruling | Applied in revision 5 |
|---|---|
| O-1…O-18 as recommended (unless overridden below) | §15 marked RULED; ADR-103 Status → Accepted with the compact ruling list |
| OVERRIDE O-6 — the tag gate is the complete §7.3 union (eight tiers) plus `TestWebObservationBirthIsRegistered` until O-10's tier exists | tasks 7.3 rewritten (no "minimum"); §11 tier table; conformance O-6; candidate-proof rows named |
| OVERRIDE O-11/O-12 — sisters read-only; obligations recorded in a SemStreams-owned migration doc | `docs/operations/migration-beta162-to-beta163.md` written (one `##` per landing); linked from `proposal.md` and the PR body; every "notice"/"communicate" instruction replaced (tasks 7.5, §11, inventory §6, proposal) |
| OVERRIDE O-17 — fill an empty stamp from the bound contract, now | §6 mechanics; `projection-mutation-client` delta requirement + two scenarios; tasks 2.2/3.4; omission (n); conformance O-17; adopter-seam consequence stated |
| Explicitly accepted: O-16 (a) with the factory check; O-14 per-binary floors; O-15 fail-closed nil registry; O-18 two registration acts; #1095's permanent hierarchy foreign-authority skip | marked in §15; no text change beyond marking |
