# gh#1100 — Single type authority: design

Baseline `origin/main` `c3a17741` (2026-08-26). Companion inventory: `docs/proposals/gh1100-type-authority-inventory.md`
(awaiting independent `INVENTORY PASS`; the design is conditional on it). ADR draft:
`docs/adr/103-payload-registry-is-the-single-type-authority.md`. Target state: `openspec/changes/single-type-authority/`.
Status: **draft for pre-owner design review; not approved.**

## 1. The decision (as the owner stated it, 2026-08-26)

- Inventory: `docs/proposals/gh1100-type-authority-inventory.md`, SHA-256
  `286ce1d0bf83878d9a8f2623a993b843e5de9873152cbb43ed767358a09ea194`. Review state: independent inventory pass PENDING; this design was drafted in the same pass at the
  caller's direction and is conditional on that pass.

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
- Mutation lane only: the fact lane already rejects at decode (`message/base_message.go:301-306`); `reconcile`/`append`/`delete`
  carry no type (`graph/mutation_requests.go:17-70`).
- A loud log names the key (a type key is not identity bytes).
- The factory (`component.go:646`) rejects a nil `deps.PayloadRegistry` at construction — today a nil registry surfaces at the
  first message (`message/decoder.go:39-44`); after ADR-103 it would also silently make every create fail, so it must be a boot error.
- `pkg/projection/mutation_client.go:322-327` is unchanged: the client does not predict registration; ingest observes it.

## 7. The six framework types as registered Graphable payloads

Serialization rule for all six: the wire form is the struct's fields; **every triple object is a field**; `EntityID()` derives
from the identity fields through the existing builder; `Triples()` is the ONE builder (moved from the writer package beside the
type); `Triple.Timestamp` is stamped at `Triples()` time (arrival on decode — the same as every Graphable today); `Source` is a
constant beside the type. `MarshalJSON` wraps in `BaseMessage` with the alias idiom; `Schema()` returns the key; `Validate()`
checks identity fields. Factory: `func() any { return &T{} }`.

| Key | Struct (package `agentic` unless noted) | Identity → `EntityID()` | `Triples()` moved from | Floor | Contract on the registration |
|---|---|---|---|---|---|
| `agentic.loop_execution.v1` | `LoopExecutionEntity` (exists, `loop_execution_entity.go:68-73`: Org, Platform, LoopID, Task) — add JSON tags, `Schema`, `Validate`, `MarshalJSON` | `LoopExecutionEntityID` | already beside the type (`:91-151`) | `control` (ADR-054 §7 "run entities") | `LoopExecutionContract()` — the literal at `internal/builtinprojection/contracts.go:23-46`, moved to `agentic`; group `todos` |
| `agentic.agent_lesson.v1` | `AgentLessonEntity{Org, Platform, ID, Category, Polarity, Severity, Status, CreatedAt time.Time, Summary, Detail, InjectionForm, Evidence []string, AppliesTo []string, ObservedRole, ExecutedBy}` | `AgentLessonEntityID` | `emit_lesson.go:693-741` (`buildEmitLessonTriples`); `emit_lesson.go:518` constructs the entity and calls `Triples()`; source `ops-emit-lesson` (`:34`) | `content` (issue consequence 1; ADR-054 §7) | `LessonContract()` — `contracts.go:52-80` moved to `agentic`; `LessonProjectionContract()` (`lesson_promotion.go:52`) returns its copy |
| `agentic.ops_diagnosis.v1` | `OpsDiagnosisEntity{Org, Platform, ID, Finding, Recommendation, Confidence float64, Evidence []string, ObservedRole, Severity, ExecutedBy}` | `OpsDiagnosisEntityID` | `emit_diagnosis.go:249-291`; source `ops-emit-diagnosis` (`:26`) | `content` (prose finding + recommendation for human review, ADR-027; O-3) | new `agentic.ops-diagnosis` birth contract: BirthPredicates = `OpsDiagnosisFinding, …Recommendation, …Confidence, …Evidence, …ObservedRole, …Severity, ActionExecutedBy` (`vocabulary/agentic/predicates.go:756-790,253`) |
| `agentic.model_endpoint.v1` | `ModelEndpointEntity{Org, Platform, Name, Provider, Model, URL, SupportsTools bool, MaxTokens int, InputPricePer1MTokens, OutputPricePer1MTokens float64, RequestsPerMinute int}` — plain fields, no `model` import into `agentic` | `ModelEndpointEntityID` | `graph_writer.go:511-548`; source `agentic-loop` (`:24`, equals `loopExecutionSource`) | `control` (config-derived, low cardinality) | new `agentic.model-endpoint` birth contract: `ModelProvider, ModelName, ModelSupportsTools, ModelMaxTokens, ModelInputPrice, ModelOutputPrice, ModelEndpointURL, ModelRateLimit` (`predicates.go:352-387`) |
| `agentic.web_observation.v1` | `WebObservationEntity{Org, Platform, CanonicalURL, Title, Snippet, Text, SourceQuery, ObservedAt, FetchedAt, ObservedBy, FetchedBy, ContentType, StatusCode int, Truncated bool}` — one struct for both tools | `TryWebObservationEntityID` (returns "" on error; ingest rejects an empty ID `component.go:1723`) | the two builders in `executors/httprequest.go` and `websearch.go` (one home) | `content` (issue consequence 1) | new `agentic.web-observation`: BirthPredicates = `WebURL`; group `observation` (append) = the rest (`predicates.go:1034-1115`) — matches `publishWebObservation`'s create-then-append (`web_emit.go:55-73`) |
| `lifecycle.harness.v1` | `lifecycle.HarnessEntity{ID string; Facts []message.Triple}` — a verbatim carrier (the harness's triples come from the registered workflow schema, `manager.go:399`), package `pkg/lifecycle`, `RegisterPayloads` added to `payloadbuiltins.Register` (`pkg/lifecycle` does not import `payloadbuiltins` — measured) | the field | verbatim | `control` (ADR-054 §7 "harness") | none — per-workflow contracts stay with `Manager.Register` (`lifecycle/spec.md:10`) |

`internal/builtinprojection` is retired; its four constants move to `agentic` (`LoopExecutionContractName`, `TodoGroupName`,
`LessonRecordContractName`, `LessonLifecycleGroupName`); `service.WireGraphRuntime` is called with `reg.Contracts()...` at both
composition roots (`cmd/semstreams/main.go:221`, `cmd/e2e-semstreams/main.go:154`) — the registry is the one table.

The three new contracts get a consumer at birth: `TestRegisteredContractMatchesTriples` (one per type) derives the predicate
set from a fully populated entity's `Triples()` and asserts equality with the registered contract's birth ∪ group predicates —
this is what stops the contract drifting from the builder, the drift class #1100 is about. Whether to mint them here or in #818
is owner item O-4; the design mints them because the conformance test is their consumer today and #818 then has nothing to invent.

## 8. Tests that change shape

- The four `_Distinct` tests are deleted. Their job is done by `Register`'s duplicate rejection (`registry.go:121-128`) exercised
  by `payloadbuiltins/register_test.go:10-13` on the full builtin set: a colliding category fails that test and the boot.
- One-table test `payloadbuiltins/single_type_authority_test.go` `TestPayloadRegistryIsTheSingleTypeAuthority`: builds the
  builtin registry; for each of the six keys asserts registered, non-empty floor, and (five) a contract whose `MessageType`
  equals the key; asserts `reg.Contracts()` names are unique and equal the retired `builtinprojection` set plus the three new;
  asserts every registration's profile is empty or valid. The other two tables are gone at compile time (`indexingProfileDefaults`,
  `internal/builtinprojection`).
- `processor/graph-ingest/indexing_profile_registry_test.go` re-targets `IndexingProfileFor` on a registry built from
  `agentic`, `research`, and `agenticdispatch` `RegisterPayloads`, keeping every one of its 22 expectations (values preserved).
- Test registries: `payloadregistry.RegisterTestType(tb, reg, key string)` (beside `NewForTest`, `payloadregistry/testing.go`)
  registers a schema-less stub factory so `test.fixture.v1`/`test.widget.v1` pass the gate in unit tests. Measured:
  `go list -deps ./payloadbuiltins | grep -c processor/graph-ingest` → 0, so `package graphingest` tests may build their registry
  from `payloadbuiltins.Register` plus the stub helper; `newTestDependencies` (`processor/graph-ingest/metrics_test.go:147-156`)
  gains a `PayloadRegistry`. 13 test files construct `CreateEntityRequest{` (inventory §2.2).
- e2e: `cmd/e2e-semstreams/fixtures.RegisterPayloads(reg)` registers the six test keys as verbatim carriers with floor `control`
  from `buildPayloadRegistry` (`main.go:358-378`); the ops seed's `agentic.loop-completed.1` (`ops/scenario.go:462`) becomes the
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
`RegisterTestType`); `internal/builtinprojection` was never importable by sisters.

Covering tiers (each exercises a mutation-lane birth the gate now guards): `e2e:agentic` (`loop_execution`, `model_endpoint`),
`e2e:lessons` (`agent_lesson`, `test.fixture`), `e2e:ops` (`ops_diagnosis`), `e2e:research-graph` (`loop_execution` via
llmwrap, `research.e2e_search_seed`), `e2e:lifecycle` (`lifecycle.harness`), `e2e:crud-tools` (`e2e.probe`), `e2e:core`
(`test.fixture` roundtrip), `e2e:structural` (three `e2e.*`). Minimum green before the BREAKING commit lands: `e2e:agentic` and
`e2e:lessons`; the full union runs in tasks §7. `web_observation` has no tier (inventory §9.1) — coverage gap filed (O-10);
its gate is the integration test `TestWebObservationBirthIsRegistered`.

Sister migration (communicate-only, carried in the PR body as a published layer): "register every type you stamp on
`entity.create` with `IndexingProfile` and, where you hold a birth contract, `Contracts`; a create with an unregistered type
returns `message_type_unregistered` with the key in `detail.message_type`; `projection.Contract` literals keep compiling."

## 12. Sequencing

- **#1095 / PR #1099.** Textual overlap at `handleCanonicalCreate` (its authority gate and this type gate both insert after
  `:207`) and additive requirements on `graph-ingest` and `agentic-lessons` with distinct names (no header collision). Its
  slice B import lane decodes through the registry: importing a **lesson** needs `agentic.agent_lesson.v1`'s factory from this
  change; the lane itself does not. Recommend this change lands first, or #1099's lesson-import scenario names it (O-7).
- **#1093.** Both edit `cmd/semstreams/main.go`; merge order free.
- **#818.** Becomes implementable on `Registration.Contracts` without a parallel table; out of scope here.
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
`TestWebObservationBirthIsRegistered`. `processor/agentic-tools`: `TestEmitLessonBuildsEntityTriples` (equality with the former builder's output).

## 14. Forced omissions (one per new registration path, check, or builder)

Delete `agentic.RegisterPayloads`'s lesson row → `TestPayloadRegistryIsTheSingleTypeAuthority` and `e2e:lessons`; delete
`lifecycle.RegisterPayloads` from `payloadbuiltins.Register` → `TestPayloadRegistryIsTheSingleTypeAuthority` and `e2e:lifecycle`;
delete the registry lookup at the create seam → `TestCreateRejectsUnregisteredMessageType`; delete `IndexingProfile:` on the
lesson registration → `TestFloorComesFromRegistration`; delete one predicate line in `AgentLessonEntity.Triples()` →
`TestRegisteredContractMatchesTriples` and `TestEmitLessonBuildsEntityTriples`; delete the nil-registry guard in the factory →
`TestFactoryRejectsNilPayloadRegistry`; delete the e2e fixtures registration → `e2e:core` and `e2e:structural`.

## 15. Owner items

- **O-1** Accept ADR-103 as worded (§1); flip Status.
- **O-2** New exported leaf `pkg/projection/contract` with aliases in `pkg/projection` (§4) — the contract rule requires owner
  review of new `pkg/*` surface.
- **O-3** Floors: lesson `content`, web observation `content`, ops diagnosis `content`, loop execution `control`, model endpoint
  `control`, harness `control`. Confirm ops diagnosis.
- **O-4** Mint the three new birth contracts here (with the `Triples()`-conformance test as consumer) or defer to #818.
- **O-5** A registered type without a floor: meter (recommended) or reject at `Register`.
- **O-6** Milestone: no `beta.163` milestone exists (only `v1.0.0-beta.162`, 1 open); #1100 carries none. Wave membership and the
  BREAKING tag discipline (CLAUDE.md "Breaking changes — E2E required").
- **O-7** Order relative to PR #1099 (lesson import needs this factory).
- **O-8** `.agents/skills/new-payload/SKILL.md:51-73,129-134` and `CLAUDE.md:420-422` teach a retired idiom; technical writer
  rewrites them as the ONE checklist (Registration with floor and contracts).
- **O-9** `test/e2e/scenarios/ops/scenario.go:459-470` seeds `ENTITY_STATES` by direct `PutKV` with a mis-spelled key — fix the
  key here; the direct write is a separate hygiene issue.
- **O-10** `web_observation` births have no e2e tier — file the coverage gap.
- **O-11** semteams reproduces framework contract literals (`cmd/semteams/main.go:971,998`) against `agentic-lessons/spec.md:193-206` —
  communicate-only.
- **O-12** semmem's local tree is pre-rename; the federation MVP that motivated #1100 is not in any local tree — where is the
  sister's finding recorded?
- **O-13** `Contract.IndexingProfile` retained; when both it and the type's floor are set they must agree (validated at
  `Register`). Retire later or keep?

## 16. PREMISE FAILED (claims in the issue or comments that do not survive measurement)

- **P1** "five mutation-only stamps" — six in-tree: `lifecycle.harness.v1` (`pkg/lifecycle/manager.go:24-28,400-407`), plus
  seven test-only keys and one mis-spelled direct-KV seed (inventory §2.2).
- **P2** "ADR-076 … families are registered — compare" — they are entity-ID families, not types; `graph/events.go` mints no
  `message.Type`; `graph.events.entity.create` has no consumer.
- **P3** The registry's "unknown types … falling back to GenericPayload" (`payloadregistry/registry.go:135-137`) is a stale
  comment: `message/base_message.go:301-306` rejects. The fact lane already enforces the ruling; this change extends it.
- **P4** "Three of five have no birth contract" — count holds; the two that have one hold it in an **internal** package, and
  two sisters re-type the literals (`semteams/cmd/semteams/main.go:971,998`, `semdev/internal/graphown/contracts.go:444`).
- **P5** "`emit_lesson.go:236` … the only place the stamp does semantic work" — also `pkg/projection/mutation_client.go:322-327`
  and semdragon `questdag/unit.go:599`; no query-side reader exists.
- **P6** "Independent of the #1095 rulings … ahead of slice B" — PR #1099 lands slices A and B in one PR; its lesson-import path
  depends on this change's factory.
- **P7** "keyed by string so the generic layer would not import the domain packages" — the layer already imports the registry
  (`component.go:692`); no new import is needed for the floor to move.
- **P8** "a test walks all three tables" — after the change there is one; the test asserts the other two are gone.
- **P9** `pkg/projection/contract.go:12-14` duplicates the profile vocabulary (`vocabulary/predicates.go:325-332`) — a fourth
  spelling the issue did not list.

## 17. Decision skills applied

`new-payload` — applied to all six types (§7); its checklist is itself stale (O-8). `kv-or-stream`, `entity-or-bucket`,
`orchestration-check`, `query-pattern` — not triggered: no new durable state, communication path, orchestration, or query access.
Context ownership: graph-ingest retains a registry, not a context; no new goroutines or lifecycle records.
