# Issue #1029 projection-contract drift inventory

Inventory only. This artifact contains no target state, options, recommendation, design, artifact delta, or
implementation tasks.

## Checkpoint

- SemStreams baseline: `7b6ff1e1a2718b4dd3087904748c296cb73215d2`.
- `HEAD`, `main`, and `origin/main` resolved to that commit when this inventory was taken.
- `git status --short` was empty.
- No active SemStreams OpenSpec change existed outside `openspec/changes/archive/`.
- `gh pr list --state open` returned `[]`.
- Issue #1029 was open on 2026-08-22.
- Read-only adopter baselines:
  - semdev: `f54a9432c5bc30713deff30a1da507bb8aa109f5`
  - semteams: `adcc07a43e865a1e65631073880e6c2833294a79`

## Problem statement

The measured question is whether a caller-supplied local `projection.Contract` can classify an official built-in
birth predicate as part of a reconcile group, pass local validation, send that caller-defined predicate list to
graph-ingest, remove the stored birth fact, and receive verified success.

The answer on this baseline is yes, with one qualification: a predicate cannot remain in both `BirthPredicates` and a
group because local validation rejects overlap. The silent path requires the caller's snapshot to move or omit the
predicate from `BirthPredicates` while placing it in a reconcile group, or to declare only a reconcile group as rule
configuration does.

## Surface inventory

### Claimed gap

#### Caller-side contract behavior

`projection.Contract` is caller-local intent rather than global authority:

- `pkg/projection/contract.go:16-25` defines `reconcile` and `append`.
- `pkg/projection/contract.go:27-42` defines named groups and `BirthPredicates`.
- `pkg/projection/contract.go:34-35` says contracts validate caller intent and do not reserve predicates.
- `openspec/specs/projection-mutation-client/spec.md:9-21` requires copied local contracts and permits overlap between
  different clients.
- `docs/concepts/28-governed-semantic-state.md:29-40` repeats that contracts are local schemas and grant no
  ownership.

Local validation prevents one predicate from appearing twice in one caller contract:

- `pkg/projection/contract.go:62-96` shares `seenPredicates` across groups and `BirthPredicates`.
- Adding `agent.lesson.category` to a group while leaving it in `BirthPredicates` fails construction.
- Moving it from the birth list into the group, or using a contract with no birth list, passes if the predicate is
  registered and canonical.

Client construction validates and snapshots only supplied local contracts:

- `pkg/projection/mutation_client.go:36-68` validates the supplied set and builds the client.
- `pkg/projection/mutation_client.go:84-107` builds `allowed` and group indexes from that set.
- `pkg/projection/mutation_client.go:110-118` copies the source slices.
- No comparison with `internal/builtinprojection.Contracts()` exists.

A wrongly grouped predicate remains allowed at create because `binding.allowed` is the union of birth and group
predicates (`pkg/projection/mutation_client.go:96-104,153-161`). Reconcile validates desired triples against the
caller's selected group, exact-reads, then sends the complete caller group:

- `pkg/projection/mutation_client.go:176-199`, especially `:191-195`.
- `canonicalizeGroupMutation` checks mode, pattern, subjects, and group membership, but no other contract:
  `pkg/projection/mutation_client.go:262-305`.

#### Wire and graph-ingest behavior

The wire request contains no contract identity or birth/mutable classification. It contains entity ID, expected
revision, raw `Predicates`, `Desired`, trace ID, and request ID (`graph/mutation_requests.go:25-34`).

Graph-ingest validates structural predicate syntax, subject equality, desired membership in the supplied list, and
the complete structural entity contract (`processor/graph-ingest/canonical_mutations.go:492-519`). It does not know
`projection.Contract`, `BirthPredicates`, built-in contract names, or group names.

After the CAS read, graph-ingest compares selected current predicates with desired, removes every current triple whose
predicate is in the request list, appends desired, commits, and reports `MutationApplied`:

- handler flow: `processor/graph-ingest/canonical_mutations.go:260-324`
- selection comparison: `processor/graph-ingest/canonical_mutations.go:535-560`
- destructive set replacement: `processor/graph-ingest/canonical_mutations.go:610-618`
- successful response: `processor/graph-ingest/canonical_mutations.go:320-324`

The wire client accepts `applied` and `unchanged` as success (`internal/graphmutation/client.go:101-114`). The public
client returns `CommitVerified` without the server outcome in its receipt (`pkg/projection/mutation_client.go:199`;
`pkg/projection/mutation_types.go:79-84`). An unintended structurally valid deletion is therefore not rejected or
classified as ambiguous.

#### Concrete lesson trace

The official built-in lesson contract contains 11 birth predicates, not the issue's claimed ten: category, polarity,
severity, created-at, summary, detail, injection-form, evidence, applies-to, observed-role, and
action.executed-by. Its reconcile group contains status, superseded-by, and retired-at.

- Declaration: `internal/builtinprojection/contracts.go:48-73`.
- Exact-set/no-overlap proof: `internal/builtinprojection/contracts_test.go:44-70`.
- Tests label the lanes explicitly: `internal/builtinprojection/contracts_test.go:11-12,61-70,114-124`.

`LessonCurator` always names the built-in contract and lifecycle group through internal constants, but receives a
caller-constructed reconciler (`processor/agentic-tools/lesson_promotion.go:44-56,156-175`). Promotion desired state
contains only `status=active` (`processor/agentic-tools/lesson_promotion.go:102-106`). A local group containing status
and category, with category absent from the local birth list, selects both current predicates and replaces them with
one desired triple. Category is removed and the response is verified.

#### Exact identity impact

Four stored fields determine lesson identity: category, sorted applies-to, summary, and sorted evidence
(`processor/agentic-tools/emit_lesson.go:495-501,642-674`). If category is removed:

- the entity remains readable by its unchanged key;
- deriving the key from the original content still produces that key;
- strict-create conflict verification reads the entity, finds no category, and rejects identical re-emission as an
  identity collision (`processor/agentic-tools/emit_lesson.go:224-244,247-313`).

Issue #1029's lookup claim is therefore too broad: category loss does not stop exact-key lookup. It destroys the
stored identity basis and causes a later identical re-emission to fail identity verification.

#### Searches closing missing protection

No server-side projection catalog or classification input was found:

```text
rg -n '(builtinprojection|pkg/projection|BirthPredicates|birth_predicates|ProjectionContract|projection_contract|contract_name)' \
  processor/graph-ingest graph/mutation_requests.go
```

No existing exported built-in lesson contract/factory surface was found:

```text
rg -n '(NewNATSLessonCurator|NewBuiltin.*Lesson|Lesson.*Contract\(|Builtin.*Contract\(|Export.*Contract|Public.*Contract)' \
  --glob '!openspec/changes/archive/**' --glob '!docs/proposals/**' .
```

No lesson-specific guard in graph-ingest or `pkg/projection` was found:

```text
rg -n '(agent\.lesson\.category|LessonCategory|LessonRecordContractName|lesson-lifecycle)' \
  processor/graph-ingest pkg/projection
```

### Every current spelling of contract authority and predicate classification

| Spelling/home | Current fact |
|---|---|
| `projection.Contract.BirthPredicates` | Generic caller-local create classification: `pkg/projection/contract.go:34-43`. |
| `projection.PredicateGroup` | Generic mutable complete-set/append classification: `pkg/projection/contract.go:16-32`. |
| `internal/builtinprojection.Contracts()` | Framework declaration for built-in loop and lesson writers; returns fresh copies: `internal/builtinprojection/contracts.go:1-75`; independence proof `contracts_test.go:135-146`. |
| Internal lesson classification | Exact 11 birth plus three lifecycle predicates: `internal/builtinprojection/contracts.go:48-73`. |
| Rule `projection_contracts` | Operator/config-authored local copy or action-derived minimal groups: `processor/rule/config.go:73-78`; `processor/rule/projection_derivation.go:26-49,151-229`. Birth predicates are explicit-only. |
| Rule target index | Copies local contracts and resolves exact targets: `processor/rule/projection_targets.go:20-63,83-132`. |
| Reference lesson rule pack | A second local spelling: `configs/rules/lessons/lesson-lifecycle-rulepack.json:35-50`; it has no `birth_predicates`. |
| Reference README | Lists ten birth predicates, omitting official `agent.action.executed-by`: `configs/rules/lessons/README.md:9-28`. |
| Vocabulary registry | Declares names, types, visibility, and descriptions, not projection membership: `vocabulary/agentic/register.go:66-130`. |
| Vocabulary comments | Lifecycle predicates are replace/single-valued; created-at is immutable and excluded from reconcile: `vocabulary/agentic/predicates.go:832-838,881-908`. |
| Lesson birth builder | Stamps official birth predicates and born mutable status: `processor/agentic-tools/emit_lesson.go:676-743`. |
| Identity subset | Category, applies-to, summary, and evidence determine UUIDv5: `processor/agentic-tools/emit_lesson.go:495-500,642-674`. |
| Graph-ingest | Physical `ENTITY_STATES` authority, with structural validation only here: `openspec/project.md:89-99`; `processor/graph-ingest/canonical_mutations.go:492-519`. |
| Mutation request | Caller-selected predicate list is the only selector reaching graph-ingest: `graph/mutation_requests.go:25-34`. |
| Go operations guide | Tells adopters to declare local membership and says birth predicates are not graph-enforced write-once facts: `docs/operations/34-projection-mutation-client.md:6-48`. |
| Agent-memory guide | Tells applications to construct clients from copied local contracts: `docs/concepts/32-agent-memory.md:299-324`. |
| Projection spec | Normatively requires copied local contracts: `openspec/specs/projection-mutation-client/spec.md:9-32`. |

The repository uses authority in two distinct senses: `internal/builtinprojection.Contracts()` is source declaration
for built-ins, while graph-ingest/`ENTITY_STATES` is current-state authority. Runtime current-state authority does not
receive or enforce the source declaration's birth/group classification.

### Adjacent claims

#### Current specs

- `openspec/specs/projection-mutation-client/spec.md`: copied contracts `:9-32`; complete-group deletion `:60-66`;
  exact authority and commit classification `:109-138`.
- `openspec/specs/graph-ingest/spec.md`: sole writer, four mutation verbs, selected predicate replacement, typed
  outcomes, and complete-candidate structural validation. The unintended deletion remains structurally valid.
- `openspec/specs/agentic-lessons/spec.md`: identity `:38-62`; gated reconcile lifecycle `:113-133`; category
  semantics `:135-149`.
- `openspec/specs/rule-projection-mutations/spec.md`: copied snapshots `:9-32`; explicit birth metadata `:34-55`;
  exact local target and one reconcile attempt `:69-97`.

#### ADRs

- ADR-091 makes graph-ingest the physical writer, applications responsible for domain appropriateness, removes
  semantic ownership, establishes typed CAS mutations, and uses local projection shape.
- ADR-080 defines content-derived lesson identity and gated lifecycle; products invoke or wrap the curator.
- ADR-074 separates syntax, vocabulary declaration, mutation intent, and encoding.

The gap is divergence between local spellings of mutation intent, not syntax failure, CAS error, or owner overlap.

#### Current issues and changes

- #818 overlaps immutable-birth enforcement but proposes broader graph-ingest policy that intersects ADR-091.
- #982 confirms that complete lifecycle-group omission-as-deletion is intended behavior.
- #582 tracks a future operator wrapper and confirms current direct Go construction.
- #979 records semdev as a direct lesson consumer and its copy burden.
- #980 and #981 are adjacent lesson construction/injection issues.
- #1027 corrected stale references to a removed per-binary builder; baseline includes that correction.
- #693, #694, #695, and #688 remain contract-adoption territory with pre-ADR-091 terminology.
- SemStreams had no active OpenSpec change or open PR at inventory time.
- semdev has active `standards-via-lessons` work whose design explicitly mirrors the internal contract; production
  code and tests are present.

### Present consumers

#### SemStreams

- Both binaries build a mutation client from `builtinprojection.Contracts()`:
  `cmd/semstreams/main.go:219-225`, `cmd/e2e-semstreams/main.go:152-158`.
- Production registers built-in tools with it: `cmd/semstreams/main.go:238-252`.
- `write_todos` is the current production consumer: `processor/agentic-tools/executors/register_write_todos.go:13-35`.
- `emit_lesson` birth uses typed graph create, not the projection client:
  `processor/agentic-tools/executors/register_emit_lesson.go:12-32`, `emit_lesson.go:187-214`.
- Only the E2E binary constructs `LessonCurator`: `cmd/e2e-semstreams/main.go:159-164`; consumer
  `test/e2e/harness/lessoncuration/handler.go:37`.
- No `cmd/semstreams` production curator construction was found.

The reference lesson rule pack is intentionally deferred and not a working runtime consumer. It passes shallow config
validation, then load fails because selectors are missing (`processor/rule/lesson_lifecycle_config_test.go:16-50`).

#### semdev

semdev hand-mirrors the official contract:

- internal-package reason and exact copy: `semdev/internal/graphown/contracts.go:428-469`
- shared client set/construction: `semdev/internal/graphown/contracts.go:420-425`,
  `semdev/internal/graphown/construction.go:33-69`
- narrow lesson surfaces and production curator: `semdev/internal/graphown/construction.go:92-125`,
  `semdev/internal/standards/wiring.go:69-87`
- literal local conformance pin: `semdev/test/conformance/standards_contracts_test.go:79-131`

The current mirror matches all 11 official birth and all three lifecycle predicates. Its pin compares local literals
with local expectations; it cannot import and compare upstream automatically.

#### semteams

semteams mirrors both built-ins and documents the internal-package rationale:
`semteams/cmd/semteams/main.go:919-927,957-966,997-1023`. No lesson-curator operation was found, so semteams is a
current contract carrier but not a proven lesson-curation consumer.

A read-only search across available `/Users/coby/Code/c360/sem*` repositories for `agentic.lesson-record`,
`lesson-lifecycle`, `LessonRecordMirror`, `NewLessonCurator`, and `builtinprojection.Contracts` found only semdev and
semteams outside SemStreams. This does not prove absence in modules not checked out locally.

## Same-class collision table

Semantic class: classification of a projection predicate as create-time birth data, complete-set mutable state, or
append evidence, and use of that classification to bound a mutation.

| Dimension | Current evidence |
|---|---|
| Semantic class | `BirthPredicates` versus reconcile/append groups: `pkg/projection/contract.go:16-43`. |
| Owners | `internal/builtinprojection` authors built-ins; each client and rule owns an independent copy; graph-ingest owns physical mutation; vocabulary owns canonical names. No owner enforces equality. |
| Catalogs | Built-in function, caller slices, rule config, generated schema, vocabulary registry. No server-side projection catalog. |
| Status | Construction/preflight can fail locally; graph-ingest exposes structural rejection metrics. No status reports cross-copy grouping drift; destructive valid reconcile reports `applied`. |
| Lifecycle | Built-ins return fresh slices; clients copy once; rule snapshots replace after preflight. No runtime persistence, watch, expiry, or reconciliation between definitions. |
| Ownership | ADR-091 permits overlapping local contracts; graph-ingest remains physical writer: `openspec/project.md:89-99`. |
| Readers | Client, rule target index, composition, built-in tools, curator, E2E, semdev, semteams, docs, and tests. |
| Writers | Framework maintainers, Go adopters, rule/config authors, semdev, and semteams. |
| Recovery | CAS prevents stale overwrite, not wrong selected sets. `ENTITY_STATES` history 1 is current authority, not recovery history. No repair path for misclassification was found. |

## Adopter seam inventory

### Semdev standards-via-lessons developer

#### What they must know

1. `LessonCurator` emits the exact contract name `agentic.lesson-record`.
2. It emits the exact group name `lesson-lifecycle`.
3. Their client must contain those exact names before curator construction.
4. Entity pattern and optional message type must match lesson records.
5. The lifecycle group must contain exactly status, superseded-by, and retired-at.
6. The 11 official birth predicates must not move into that group.
7. Reconcile replaces the complete listed group; omitted selected predicates are deleted.
8. The upstream declaration is in a Go `internal` package they cannot import.
9. Exported vocabulary constants prevent predicate spelling drift but not contract/group/grouping drift.

This exceeds two correctness facts and is an adopter-seam finding.

#### What happens if they do nothing

- Missing contract/group currently reaches first-use typed errors because `NewLessonCurator` validates neither.
- The correct mirror works.
- Moving a birth predicate into the lifecycle group removes it on the first transition that omits it and returns
  verified success.
- Duplicating it in both lanes fails client construction.
- Missing a future lifecycle predicate fails locally when the curator begins emitting it.
- Category loss causes later identical store birth to fail identity conflict verification.

#### Where they find out

The compiler cannot expose the official internal contract. Construction catches local structural errors, not drift.
Missing names surface at first use. Valid wrong grouping surfaces only through changed graph data, downstream behavior,
or adopter assertions. The current sources are a non-importable file and prose; the README omits one birth predicate.
The honest rank for grouping drift is documentation/source/manual review, and a valid wrong grouping is silent success.

#### What they should have to know

They should know the product operation and domain preconditions. They should not have to predict and preserve the
framework's private contract snapshot or destructive complete-group set.

#### Observation versus prediction

The adopter predicts a framework-owned declaration before client construction even though the framework already has
that fact in `internal/builtinprojection.Contracts()`.

### External rule/config author

They must know exact selector names, group membership, that the complete group becomes the removal set, that birth
metadata is explicit-only, and that no built-in comparison occurs. Missing selectors and local mismatches fail at
load; a valid group that includes category can pass and delete it. They should know the intended transition and
trigger, not reconstruct a hidden complete removal set.

### Semteams product-shell maintainer

Semteams pays the same copy bill without a proven curator consumer. Duplicate mistakes fail wiring; a missing contract
may remain latent; a valid wrong group can alter the removal set silently. Its comment directs manual upstream diffing.

## Search and measurement log

```text
git rev-parse HEAD
git rev-parse main
git rev-parse origin/main
git status --short
find openspec/changes -mindepth 1 -maxdepth 1 -type d ! -name archive
gh pr list --repo C360Studio/semstreams --state open
rg -n '(BirthPredicates|birth_predicates|birth predicates|PredicateGroup|predicate group|lesson-lifecycle|LessonLifecycleGroupName|LessonRecordContractName|agentic\.lesson-record|builtinprojection\.Contracts|copied local projection contracts|projection contracts|projection\.Contract|ModeReconcile)' .
rg -n '(NewLessonCurator|\.Promote\(|\.Retire\(|\.Supersede\(|LessonRecordContractName|LessonLifecycleGroupName|builtinprojection\.Contracts\(\)|NewMutationClient\()' .
for each checked-out sister sem* repo:
  rg -l '(agentic\.lesson-record|lesson-lifecycle|LessonRecordMirror|NewLessonCurator|builtinprojection\.Contracts)'
```

## Open evidence questions

- No evidence establishes that deployed `ENTITY_STATES` data already lost a birth predicate through this path.
- The local sister census cannot establish whether un-checked-out modules carry other copies.
- #818 overlaps immutable-birth territory but asks for broader graph-ingest policy with authorization implications; no
  ruling relates it to #1029.
- The reference rule pack is intentionally deferred and invalid at load; it is documentation/config territory, not
  runtime evidence of loss.
- No current status, metric, or health surface distinguishes intended complete-group deletion from valid drift.

## Reviewed semantic-producer correction

This section supersedes the earlier current-spellings, adjacent-claims, present-consumer, collision-table, and search-log
sections wherever they describe complete-set replacement as projection-client-only. The original lesson trace and its
corrected identity impact remain valid.

### Complete-set predicate replacement census

The baseline has six production constructors of `graph.ReconcilePredicatesRequest`, plus one equivalent Graphable
replacement lane:

| Producer | Selector authority and behavior |
|---|---|
| `pkg/projection.MutationClient` | A copied local contract supplies the named group verbatim. The client exact-reads and sends `group.Predicates` (`pkg/projection/mutation_client.go:176-199`). This fans out to built-in tools, `LessonCurator`, and rule `reconcile_predicates` actions. |
| Lifecycle attach-on-existing | `Manager.Create` derives the selector from predicates in the initial lifecycle delta (`pkg/lifecycle/manager.go:395-431,454-481`) and emits through the raw typed client (`pkg/lifecycle/graph_emit.go:17-45`). |
| Lifecycle transition | `TransitionWith` reconstructs phase, retained transition records, configured audit fields, and changed scalar fields (`pkg/lifecycle/manager.go:552-617`). The selector is the delta predicates plus the fixed transition-record family (`pkg/lifecycle/manager.go:619-631`; `pkg/lifecycle/transition_records.go:20-34`). |
| Lifecycle operator patch | `UpdateFromOperator` derives additions and removals from the submitted patch and selects their union (`pkg/lifecycle/projection.go:223-245`; `pkg/lifecycle/manager.go:719-751`). A nil patch value selects a predicate with no desired replacement. |
| Gated-DAG claim | `natsClaimer` exact-reads and reconciles the one configured `ClaimPredicate`; claim supplies one desired triple and unclaim none (`processor/gated-dag/claim.go:21-62`). Config defaults and pairwise validation are at `processor/gated-dag/config.go:13-20,86-93,230-250,410-432`. |
| Raw rule remove/update | `tripleMutator.RemoveTriple` exact-reads and reconciles one runtime predicate to an empty desired set (`processor/rule/triple_mutator.go:67-90`). `remove_triple` calls it directly; `update_triple` removes then appends separately (`processor/rule/actions.go:802-840,955-990`). |

`service.ConfigureRulePackMutations` is an additional contract-authority construction seam rather than a seventh wire
constructor: it validates copied per-pack contracts and creates an independent client per contract-bearing rule
processor (`service/rule_pack_bind.go:47-70,74-120`). Rule actions resolve through a separate copied target index
(`processor/rule/projection_targets.go:20-63,83-132`) before the client resolves the contract/group again.

The equivalent Graphable lane does not construct `ReconcilePredicatesRequest`:

- A producer supplies `Triples()` (`graph/graphable.go:53-60`).
- For every incoming subject/predicate pair, `graph.MergeTriples` removes the complete previous object set and retains
  the incoming set (`graph/helpers.go:98-108`).
- `MergeEntity` applies this to every existing Graphable entity
  (`processor/graph-ingest/component.go:1900-1918,2006-2044`).
- An absent predicate is preserved; a present partial multi-valued set drops omitted objects. This is normative in
  `openspec/specs/graph-ingest/spec.md:26-38,54-65,81-95`.
- `entity.indexing.profile` is the explicit create-time immutable exception
  (`processor/graph-ingest/component.go:2025-2044`).

Graph-ingest therefore receives complete-set selectors from copied groups, lifecycle operation deltas, component
config, runtime rule predicates, and Graphable arrival content. None carries the built-in projection contract's
birth/group classification.

### Complete current spellings of selector authority

| Spelling/home | Current fact |
|---|---|
| Projection birth/groups | Caller-local create/reconcile/append classification: `pkg/projection/contract.go:16-43`. |
| Built-in projection declaration | Framework source for loop and lesson writers: `internal/builtinprojection/contracts.go:19-75`. |
| Rule contracts and targets | Config-authored/derived groups plus another immutable target copy: `processor/rule/config.go:73-78`; `projection_derivation.go:180-229`; `projection_targets.go:20-63,83-132`. |
| Rule-pack composition | Another copied snapshot and client per enabled processor: `service/rule_pack_bind.go:47-70,106-120`. |
| Lifecycle phase declarations | `Workflow.PhasePredicate` (`pkg/lifecycle/workflow.go:24-67`) and a second phase spelling in the schema tag (`pkg/lifecycle/tags.go:134-175,219-254`). `Manager.Register` parses both but does not compare them (`pkg/lifecycle/manager.go:111-140`). |
| Lifecycle audit/scalar selectors | Audit predicates and changed reflected scalar fields may enter transition selectors: `pkg/lifecycle/workflow.go:76-113`; `pkg/lifecycle/manager.go:604-617,653-704`. |
| Lifecycle operator authority | Enforcement derives from `lifecycle:"operator_writable"` schema metadata: `pkg/lifecycle/tags.go:219-276`; `pkg/lifecycle/projection.go:223-245`. |
| `Workflow.OperatorWritablePredicates` | Separate exported slice validated/logged but not used by the patch-authority path: `pkg/lifecycle/workflow.go:69-74,198-203`; `pkg/lifecycle/manager.go:159-165`; advertised predicates derive from schema tags at `pkg/lifecycle/manager_query.go:619-627`. |
| Lifecycle single-vs-many guard | Phase, audit, and scalar fields are classified single-valued and disjoint from child-link/reference predicates: `pkg/lifecycle/workflow.go:253-319`. |
| Transition record family | Five fixed predicates always enter transition selectors: `pkg/lifecycle/transition_records.go:20-34,93-110`; `pkg/lifecycle/manager.go:627-630`. |
| Gated-DAG markers | Config distinguishes completed, failed, dirtied, dependency, and claim predicates and requires local pairwise disjointness: `processor/gated-dag/config.go:86-93,410-432`. |
| Raw rule predicate | Remove/update use a runtime predicate as a one-cell complete set: `processor/rule/actions.go:802-840,955-990`; `processor/rule/triple_mutator.go:67-90`. |
| Graphable arrival | Predicates present in `Triples()` implicitly select complete object sets; there is no central Graphable group catalog: `graph/graphable.go:53-60`; `graph/helpers.go:98-108`. |
| Canonical wire request | `Predicates` is the only selector graph-ingest receives: `graph/mutation_requests.go:25-34`. |
| Graph-ingest | Validates selector structure and final entity shape, then applies the selector: `processor/graph-ingest/canonical_mutations.go:492-519,610-618`. |

“Contract” therefore names projection contracts, lifecycle workflow/schema declarations, gated-DAG config, rule
configuration, and implicit Graphable arrival sets. Graph-ingest owns physical state but receives none of those source
declarations' semantic class labels.

### Adjacent same-class claims

`openspec/specs/lifecycle/spec.md` is a second normative complete-set producer:

- Registration validates local shape without claiming predicates or rejecting cross-component overlap (`:10-21`).
- Attach-on-existing uses revision-fenced `entity.reconcile` (`:23-45`).
- Transitions reconstruct and reconcile the complete lifecycle group after definite revision conflict (`:77-108`).
- The same reconcile replaces phase while retaining the bounded 64-occurrence window (`:89-115`).

Closed #234 records a prior silent-deletion class: a lifecycle phase/audit/scalar predicate colliding with a
child-link/reference predicate made transition reconciliation delete cardinality-many triples. The implemented local
guard runs during registration (`pkg/lifecycle/manager.go:137-140`), rejects single-vs-many and child-vs-reference
collisions (`pkg/lifecycle/workflow.go:253-319`), and has a regression matrix at
`pkg/lifecycle/manager_test.go:1088-1144`. It does not compare lifecycle declarations with projection contracts,
gated-DAG config, rules, or Graphable producers.

Closed #177 established preservation of predicates absent from a Graphable arrival. Closed #466 established
predicate-level full-set replacement for predicates present in an arrival. Current code and the graph-ingest spec
encode #466 (`graph/helpers.go:98-108`; `processor/graph-ingest/component.go:2018-2035`;
`openspec/specs/graph-ingest/spec.md:26-95`).

ADR-049 remains lifecycle declaration context but is partially superseded by ADR-091. ADR-070 retains gated-DAG claim
state. ADR-091 leaves domain appropriateness with producers and physical mutation with graph-ingest. ADR-055 and
ADR-056 are superseded historical evidence, not current birth catalogs. Open #818 spans all mutation and Graphable
lanes and is broader than #1029. Open #982 confirms intended omission-as-deletion inside the correctly classified
lesson lifecycle group.

### Additional present consumers

- `cmd/semstreams` constructs the lifecycle manager beside the built-in projection client
  (`cmd/semstreams/main.go:219-225`).
- Lifecycle gateway patches call `UpdateFromOperator`; transitions call `Transition`
  (`gateway/lifecycle-gateway/handlers.go:350-368,410-437`).
- Rule `lifecycle_transition` calls `TransitionWith`, including schema-authorized scalar changes
  (`processor/rule/actions_lifecycle.go:55-85`).
- Gated-DAG registers its workflow, creates/transitions a FanOut instance, and separately reconciles its claim marker
  (`processor/gated-dag/component.go:119-133,265`; `processor/gated-dag/executor.go:157-165,350-371`;
  `processor/gated-dag/claim.go:36-62`).
- Graph-enabled rule processors construct the raw mutator (`processor/rule/processor.go:683-697`); configured remove
  and update actions call its one-predicate reconcile.
- Every accepted Graphable payload is an implicit complete-set producer for predicates returned by `Triples()`
  (`processor/graph-ingest/component.go:1714-1734,1900-1918,2018-2035`).

These consumers cannot supply built-in projection birth/group classification to graph-ingest.

### Replacement same-class collision table

Semantic class: selecting predicate cells on an existing entity that become exactly equal to a desired set, including
empty desired state, plus local classifications deciding which cells may enter that selector.

| Dimension | Current evidence |
|---|---|
| Semantic class | Contract group replacement, lifecycle delta replacement, one-predicate marker removal, and Graphable per-predicate full-set replacement. |
| Owners | Projection clients; lifecycle workflow/schema registrations; gated-DAG config; rule contracts/actions; Graphable implementations; graph-ingest as physical writer. No owner enforces equality between declarations. |
| Catalogs | Built-in projection function, caller/rule contracts, lifecycle workflow and tags, fixed transition predicates, gated-DAG config, and vocabulary. Graphable producers have no central catalog. |
| Status | Local construction/registration catches local invalidity; workflow discovery exposes some metadata; graph-ingest returns mutation outcomes. No status reports cross-producer overlap or copy drift. |
| Lifecycle | Projection/rule clients snapshot at composition; lifecycle at registration; gated-DAG at construction/start; raw rule predicates per execution; Graphable selectors per arrival. |
| Ownership | Projection and lifecycle specs permit cross-component overlap; ADR-091 removed semantic claims. CAS detects revision conflict, not semantic overlap. |
| Readers | Projection/rule resolution, lifecycle gateway/rules, gated-DAG, graph-ingest, tests, adopters, and graph readers. |
| Writers | Six production wire-request constructors, their callers, and every Graphable arrival. |
| Recovery | Projection sends once; lifecycle owns bounded rebuild loops; gated-DAG retries indirectly; raw remove has no internal retry; Graphable retries CAS/merge. None repairs a semantically wrong selector after successful commit. |

### Additional adopter seams

- A lifecycle author must align two phase spellings, understand tag-derived operator authority, dynamic scalar
  selectors, and single-vs-many disjointness. Registration catches local collisions, not cross-component overlap.
- A gated-DAG config author must keep the claim predicate distinct from other local markers. The default works and
  local collision is a boot error; unrelated producer overlap is not detected.
- A raw rule author supplies the selector directly. `remove_triple` removes the complete predicate object set;
  `update_triple` is separate remove then append. Neither is compared with projection birth groups or lifecycle.
- A Graphable author must know that a present predicate replaces its complete prior object set while absence preserves
  it. The interface does not represent that fact; it is documented in graph-ingest and `MergeTriples` prose.

### Replacement semantic search log

```text
rg -n --glob '*.go' 'ReconcilePredicatesRequest\s*\{' .
```

Production results were exactly:

```text
pkg/projection/mutation_client.go:191
pkg/lifecycle/manager.go:427
pkg/lifecycle/manager.go:627
pkg/lifecycle/manager.go:747
processor/gated-dag/claim.go:55
processor/rule/triple_mutator.go:80
```

All other hits were tests. The following searches found no seventh production producer and closed equivalent lanes,
selector helpers, client construction, and Graphable replacement:

```text
rg -n --glob '*.go' 'ReconcilePredicates' --glob '!**/*_test.go' .
rg -n --glob '*.go' 'Predicates:\s*(uniqueStrings|predicatesOf|append)|predicatesOf\(|uniqueStrings\(' .
rg -n --glob '*.go' 'Desired:\s*nil|Predicates:\s*\[\]string' .
rg -n --glob '*.go' 'MergeTriples\(' --glob '!**/*_test.go' .
rg -n --glob '*.go' 'graphmutation\.NewClient|NewMutationClient\(|SetPredicateReconciler|PredicateReconciler' .
rg -n 'OperatorWritablePredicates' pkg/lifecycle --glob '*.go'
rg -n --glob '*.go' --glob '!**/*_test.go' \
  'func \([^)]*\) Triples\(\) \[\]message\.Triple|func \([^)]*\) Triples\(\) \[\]Triple' .
```

### Conclusions after correction

1. Complete-set selection is not projection-client-only: six production wire constructors and one Graphable lane
   exist.
2. The #1029 lesson trace remains valid: a drifted lesson copy can delete `agent.lesson.category` and receive verified
   success.
3. No producer supplies graph-ingest with projection birth/group classification.
4. Lifecycle already guards one local silent-deletion collision class from #234, but not cross-producer overlap.
5. A graph-ingest-wide immutable-birth policy would intersect lifecycle, gated-DAG, raw rule mutation, Graphable
   replacement, and the additional lanes named by #818.
6. No global runtime equality owner for projection birth/group classification was found.
