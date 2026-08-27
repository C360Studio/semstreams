# gh#1095 — entity-ID segment semantics: options, contract, break wave, owner items

**Baseline:** `origin/main` `5cc0c7fb`; committed as draft PR #1099 (`b6b4b024`). **Status: ACCEPTED** — owner ruling
2026-08-26, applied in this revision. Row ids (S1, K3, W2, H1, …) refer to the inventory.

## Checkpoint (revision 5)

- **Owner ruling 2026-08-26 (comment on #1095): O-1 through O-11, O-13, O-14 accepted as recommended; **O-12 overridden to option (a) — foreign-authority imports are read-only mirrors**; the permanent hierarchy-inference skip for all foreign-authority entities explicitly accepted, on every lane.** Revision 5 applies it: ADR-102 → Accepted; O-12(a) in §C.3, §B.4, §F, the graph-ingest
  delta, tasks, and conformance; the #1096 imported-firing-entity path redesigned as a local linkage (§C.3).
- Inventory: `docs/proposals/gh1095-entity-id-segment-semantics-inventory.md` revision 5, SHA-256
  `1667c3266dc6022b3d0ffc9325e835516bd9fec61727610095ecafb878fe60c8` (r4 `eae13661…`, r3 `b6e1e9e2…`, r2 `ba2131bc…`, r1 `0e511e91…`).
- Inventory review: independent blind pass on r1 → **INVENTORY PASS WITH DIVERGENCES**; D1–D5 corrected and
  R-A–R-D added in r2.
- Design review round 1 (Fable, adversarial, at `3226c220`) → **REQUEST CHANGES**. Dispositions in this revision:
  **B-1 fixed** (authority applies to the candidate subject only; `@id` clause deleted from §C.3 and the graph-ingest
  delta; scenario aligned). **B-2 fixed** (owner item O-12 mirror-vs-annotate with options; contract "hierarchy
  inference skips foreign-authority entities"; ADR-102 consequence names the run anchor's fate). **F-1 fixed**
  (§C.6 authority-pair bound at config load; ADR-076 d2 amended; O-14). **F-2 fixed** (system-value grounding in §B;
  O-11 served level; gh606 amendment list in §D; ADR-102 amends both ADR-099 level phrases). **F-3 fixed** (five
  example builders and the `iot_sensor` reader in the inventory and tasks 5.1/5.2). **F-4 fixed** (two new audit
  surfaces named, §D and tasks 5.6). **F-5 fixed** (`NewAlertEvent` signature change and rule-processor plumbing
  named). **F-6** owned by the coordinator (PR body). **F-7 fixed** (one landing PR; union tier list; crud-tools and
  lifecycle added; P6 covered by statistical/semantic). **F-8 fixed** (reference import lane moves to
  `configs/graph-backend.json`). **F-9 fixed** (`ValidateEntityIDAuthority(candidate, org, platform string,
  importLane bool)`). **F-10 fixed** (§C.3 states it; O-13). **F-11 fixed** (O-9 removed; reserved set conditional
  on the `gateddag` re-slot; ADR-102 d2 conditional on O-2). **F-12 fixed** (every scenario names a test; forced
  omissions M8–M14; conformance row for constraint 3). Notes folded: honest O-B delta; `hierarchy.*.member`
  misnaming; forward-looking sentence removed from the graph-clustering delta; `tiered_structural.go` variable
  names in the rewrite list; ADR-102 d5 mechanics moved to the spec; fallback priced under O-7; #1096 is live today.
- Design review round 2 (narrow re-review at `84cb6e3a`) → **APPROVE WITH CHANGES**. Dispositions in this revision:
  **F1 fixed** ("must-exist" replaced by "no stub is created and an absent object is permitted" in the ADR, §C.3, the
  graph-ingest delta, tasks 6.1, and conformance D5). **F2 fixed** (#1096 fixture is a peer's own loop execution,
  `foreign.dep9.agentic-loop.agent.execution.<uuid>`). **F3 fixed** (`hierarchy.*` removed from O-12(c); the skip
  stands regardless of O-12). **F4 fixed** (O-12(c) narrowed to the closed run-anchor pair; the delegated-namespace
  variant kept as costed option (d)). **F5 fixed** (O-10 pointers; ADR references). **F6 fixed** (`gateddag`
  conditional in the §C table and §C.2). **N7 folded** — with a correction: `config` imports neither `graph` nor
  `agentic` (`grep` over `config/*.go` → 0), so the family table lives in `pkg/types`, which both `config` and
  `processor/rule` may import. **N8, N9, N10, N11, N12, N14 folded.** N13 coordinator-owned (PR body).
- Scenario accounting (N10): 28 scenarios across the five deltas; 22 name a verifying test or a recorded gate; the
  6 unnamed are MODIFIED carry-overs (4 byte-identical, 2 in `agentic-lessons` with pre-r3 wording edits).
- Owner-item renumbering after removing O-9: review letters O-12/O-13/O-14/O-15 are **O-11/O-12/O-13/O-14** here;
  former O-10/O-11 are **O-9/O-10**.
- Design review rounds 1 and 2 are complete and the owner has ruled; this design, ADR-102 (Accepted), and the spec
  deltas are the accepted target state for the `entity-id-segment-semantics` change.

## B. Reorder options

Constraints from the ruling: arity six is fixed; reordering is allowed; the design must keep every second-order
consumer in §1 of the inventory expressible.

### Why `instance` stays last under every option

1. It is the only unbounded-cardinality position (UUIDs, 64-hex digests, hash-bounded slugs). NATS wildcard
   filters are prefix-shaped (`prefix.>`; `natsclient.KeysByPrefix` = `prefix + ">"`, ADR-065 `:157-161`), so every
   grouping token must precede the leaf for a prefix scan to be selective.
2. `ENTITY_SUFFIX_INDEX` keys the last two positions (K3); `LoopIDFromExecutionEntityID`/`runIDFromChainEntityID`
   read `parts[5]` (W3); `agent.complete.<loopID>` and `user.response.$entity.instance` put the leaf on a subject
   (S1–S4); `COMPLETE_<loopID>` keys AGENT_LOOPS (S4).
3. ADR-076 puts the arbitrary-author-string digest in the instance so no author string enters a grouping position.

### O-A — keep `org.platform.domain.system.type.instance`

| Question | Answer |
|---|---|
| Prefix lengths | 2 = deployment; 3 = deployment+taxonomy; 4 = +source; 5 = +type |
| Deployment / source / taxonomy as prefixes | deployment: yes (2); taxonomy: yes (3); **source: no** — "everything this deployment holds from source S" is the wildcard filter `org.platform.*.S.>`, legal for KV filters but not for `graph.query.prefix`, `MatchesAnyIDPrefix`, lesson `id:` keys, embedding `Scope`, or an ADR-099 level |
| ADR-099 cut points | unchanged: 0 = 4 (source-within-taxonomy), 1 = 3 (taxonomy), 2 = 2 (deployment); structurally distinct: yes. Note the level the ruling calls "system" is source×taxonomy; one semsource repo splits across up to six level-0 groups (git/golang/config/web/media/svelte) |
| Hierarchy containers | unchanged padding (H1–H3) |
| Wildcard/prefix consequences | none |
| KV scan locality | `org.platform.` scans are federation-friendly (all entities of one peer); a source scan needs a filter, not a prefix |
| Subject tokens | unchanged |
| Blast radius | every builder still changes for ruling 2/5 (platform value, semsource re-slot) → fresh state anyway; no literal reorders; 29 docs name the order and stay correct |

### O-B — `org.platform.system.domain.type.instance` (recommended)

| Question | Answer |
|---|---|
| Prefix lengths | 2 = deployment; 3 = deployment+source (**the federation triple**, `pkg/types/entity_id.go:83-86`'s declared grouping made true on the wire); 4 = +taxonomy; 5 = +type |
| Deployment / source / taxonomy | deployment: 2; source: 3; taxonomy-within-source: 4; **taxonomy across sources** becomes the filter `org.platform.*.D.>` (legal KV filter and `MatchEntityIDPattern` pattern), or a `tag:` lesson scope |
| ADR-099 cut points (recomputed) | 0 = 4 parts = `{org, platform, source, domain}` — the SAME set partition as O-A's level 0 (only the ID string reorders); 1 = 3 parts = **source** (new meaning: one repo/feed/world = one community — the partition semsource's field measurement found useful, ADR-099 `:15-17`); 2 = 2 parts = deployment. Levels remain structurally distinct (arity-distinct) |
| Hierarchy containers | the 3-part container becomes a source container; `hierarchy.domain.member` misnames it → see §B.4 |
| Wildcard/prefix consequences | 5 in-tree Go declaration patterns and 3 config literals rewrite (W5–W6); all-wildcard patterns untouched; `id:` scope keys with 3 segments now scope to a source; a config-authored `$entity.id` subject reorders its tokens (S3) |
| KV scan locality | `org.platform.` unchanged; `org.platform.system.` is a new selective scan (all of one source) — the unit an import lane, a per-source retention decision (ADR-068), and semmem's "source authority" need |
| Subject tokens | unchanged |
| Blast radius | in-tree: `pkg/types` (P1–P3), 9 builders, 10 index-position readers (W3) including the LPA provider and summarizer (C3), 5 Go declaration patterns (W5: `agentrun.go:100`, `builtinprojection/contracts.go:26,56`, `gated-dag/participant.go:17`, `mission/state.go:28`), 3 config literals, 2 e2e literal assertions (W8), 29 docs naming the order plus `docs/concepts/18-rule-driven-artifacts.md` (whole-ID subject examples, S3), `semantictest` builder, `entityPartNames`; two values that leave the graph: the GraphQL `EntityTypeSummary.type` value (P6) and the vocabulary export IRI path (P5, owner item O-10); sisters: every builder literal that names domain before system (semboids 3, semdev 3, semdragon 4, semops 1, semconnect config, semsage constants, semteams 1 literal, semspec 2) — all files that ruling 2/5 already opens |

### O-C — `org.system.platform.domain.type.instance` (considered, rejected)

Puts source before deployment so `org.system.` groups one source across deployments. Rejected: the 2-part prefix
is no longer the deployment authority, which breaks the boundary rule (positions 1–2 = authority) and ADR-032's
`org` → `org.platform` tenancy ladder; and a source name is producer-chosen, so cross-deployment grouping by it is
exactly the unverified collision the ruling refuses.

### O-D — do nothing (lexical-only) — rejected by ruling 1.

### Recommendation

**O-B.** Grounding sentence: *the federation triple (org, platform, source) is the unit every cross-deployment
operation scopes on — import, scope keys, per-source retention, the measured useful partition — and a scope the
framework must express as a prefix has to be a prefix on the wire; under the current order "everything this
deployment holds from source S" is not a prefix at all, and the pre-v1 clean break is the only window in which the
order can change.*

The system-value grounding (r3, inventory C4): the gh#606 ruling found that LPA over same-system edges reduces to
the system filter. "Same system" in that code is `getSystem` = `parts[3]` alone — the system VALUE (semsource's
repo), whose member set under today's order is `org.platform.*.<repo>`: a wildcard filter, not a prefix at any
ADR-099 level. Under O-B that set is exactly the three-position prefix, level 1. O-A cannot serve the partition the
ruling measured as useful without leaving the prefix language.

The honest delta over O-A (r3): O-B changes two published values that O-A does not — the vocabulary export IRI
path (P5, O-10) and the GraphQL `EntityTypeSummary.type` value (P6) — re-opens gh606 Q8 (which level is served and
summarised, O-11), renames or retires the `hierarchy.*.member` predicates (H7), and rewrites every position literal
in five example builders, one silent example reader (W9), five Go declaration patterns, three config literals, four
e2e sites (W8, W10), and 29 documents. It also introduces a byte-bound obligation on the authority pair that O-A
shares the moment `semstreams.framework` leaves the alert/trigger families (P7). The cost is one-time and lands in
files ruling 2/5 already opens; the benefit — source as a prefix — is permanent and unobtainable later.

ADR-099 consequence: level 0 partition is identical as a set; level 1 changes from "taxonomy" to "source"; ADR-102
amends both of ADR-099's level phrases ("level 0 = system (4 parts)" becomes source×taxonomy; "1 = domain (3)"
becomes source). The full gh606 amendment list is in §D.

### B.4 Hierarchy containers without arity padding

Fixed arity cannot express a 3/4/5-part group as a first-class identity without either (a) padding with reserved
literal tokens, or (b) a digest family in the ADR-076 shape (`org.platform.<component>.graph.container.<sha256(prefix)>`)
with the prefix carried as a property. The framework already holds the group as a pure function of the ID
(ADR-099), so containers are a second spelling of the same fact (H5) with two latent defects (H2, H3), and under O-B
both membership predicates misname their container (H7: `hierarchy.system.member` on source+taxonomy,
`hierarchy.domain.member` on the source).

A third defect is order-independent (H6, B-2): containers and inverse sibling edges are minted from the ingested
entity's own prefix through in-process direct persistence, so for an imported entity the framework mints under
foreign authority — which ruling 2 forbids by construction. The contract, whichever option below is chosen:
**hierarchy inference skips foreign-authority entities** (no container birth, no membership triple, no inverse
sibling edge for an entity whose `org.platform` is not the deployment's). Accepted by ruling on every lane; it
holds until containers leave the tree.

Options: **(1) retire containers** (type/system/domain container entities and `hierarchy.*.member` edges) inside the
gh606 change, keeping only sibling edges if the structural tier still needs them; **(2) keep padding, declare the
tokens** (`group`, `container`, `level` reserved in position 6 by contract; the audit rejects any producer instance
equal to them; the byte overflow becomes a coded rejection at mint; the two predicates renamed to
`hierarchy.taxonomy.member` / `hierarchy.source.member`); **(3) digest family** (fixes H2/H3, keeps two homes).
Ruled (O-6): (1), landed by gh606 in the same wave; (2) only as the priced fallback if gh606 slips a tag (§D).

## C. The segment-semantics contract (draft, O-B order)

| Pos | Name | Meaning | Owner / value source | Registered? |
|---|---|---|---|---|
| 1 | `org` | organization namespace; the tenancy root (ADR-032) | operator config `platform.org` (`config/config.go:225-238`, lowercased at load) | operator-declared |
| 2 | `platform` | **minting deployment authority** — the composition root that produced the entity | `platform.id` (see C.4) via `deps.Platform`; never from a payload, a constant, or a firing entity | operator-declared; unique within org by declaration |
| 3 | `system` | **source** — the subsystem, feed, repo, world, board, API, or framework component that produced the entity | the producer; stable per source; MUST NOT be the product name (product = provenance: `Triple.Source`, envelope `source`) | producer-chosen, unregistered |
| 4 | `domain` | **delegated taxonomy** — the subject-matter category | a registered delegation per producer (C.2); framework reserves `agent`, `ops`, `graph`; the gated-DAG family re-slots under `agent` (O-9) | registered at the composition root |
| 5 | `type` | entity type within the domain (`EntityType{Domain,Type}`, `vocabulary.EntityTypeIRI`) | same delegation as `domain` | registered with its domain |
| 6 | `instance` | leaf identifier; families: UUID, conventional name, content hash | producer | never registered; high cardinality; last |

### C.2 Delegated authority for `domain` — does the predicate pattern transfer?

Donor: `vocabulary.PredicateAuthority` (`vocabulary/namespace_authority.go:28-124`): explicit
`NamespaceDelegation{Producer, Namespace}`; registered names pass for all producers; unregistered need a matching
delegation; producer identity comes from the trusted integration boundary, never from `Triple.Source`
(`:26-27`); enforced at declaration surfaces, with runtime persistence staying syntax-only (`:40-46`). One production
consumer: `agentic/tools.go:369-382`.

It transfers with one substitution — the unit is `domain` or `domain.type` instead of `domain` or
`domain.category`:

- `pkg/types.EntityDomainDelegation{Producer, Domain, Type}` (empty `Type` = domain-wide) and
  `EntityDomainAuthority.Authorize(producer, domain, entityType) error`.
- Framework-reserved set declared in-tree (`agent`, `ops`, `graph`; the gated-DAG family re-slots under `agent`,
  O-9); every framework builder in §1.14
  authorizes at construction (boot-time builders) or returns the coded error (Try* builders).
- A product registers its delegations where it installs its payload registry today (`RegisterPayloads`, the
  composition root — the same trusted boundary the predicate authority uses). Registration shape:
  `RegisterEntityDomains(authority, EntityDomainDelegation{Producer: "semsource", Domain: "git"}, …)`.
- Enforcement point: declaration time (builders, `EntityIDPattern` declarations, projection contracts,
  `lifecycle.Workflow`), NOT the graph-ingest hot path — the wire carries no producer identity (F3), so a runtime
  check would have nothing to authorize against. This mirrors the donor exactly.
- Consumer at birth: the nine framework builders and the two projection-contract `EntityPattern` declarations —
  `internal/builtinprojection/contracts.go:26,56` when this was written; PR #1109 deleted that package and moved both
  onto the payload registrations (`agentic/loop_execution_entity.go:224`, `agentic/agent_lesson_entity.go:399`).

What it does not do: it cannot stop a product from choosing a colliding domain with another product in the same
deployment; registration makes the collision visible at boot (two producers delegating the same domain is a
composition rejection, the ADR-076 d4 "duplicate PackID" shape). Ruled (O-3): `system` is not registered — `system` values are runtime-derived (repo slugs, world namespaces) and unbounded; registering
them would make every new repo a config change. The audit checks that a builder's system position is not a product
name literal.

### C.3 Boundary enforcement for `org.platform`

- **Where:** graph-ingest's structural gate (`graph-ingest/spec.md:232-278`; `component.go:1888`) on every lane —
  Graphable fact arrival, `graph.mutation.>`, direct persistence — before any KV I/O. The pure marshal seam stays
  syntax-only (it has no dependency).
- **What (r3, B-1):** positions 1–2 of every final candidate **subject** identity MUST equal the deployment's
  `org`/`platform` unless the arrival lane is a declared import lane. `@id` objects are NOT authority-checked: they
  keep structural validation; no stub is created and an absent object is permitted (`graph-ingest/spec.md:776-780`; no auto-vivify,
  `openspec/project.md:92-93`), so a local run entity may cite its imported parent and a curated lesson may cite an
  imported loop — the federation purpose. On an import lane a subject whose positions 1–2 EQUAL the local authority
  is rejected (a peer cannot mint as this deployment); foreign subjects are accepted unchanged (never rewritten,
  ADR-076 d6).
- **An import is a read-only mirror (ruled, O-12 option (a)).** No local lane mutates a foreign subject: the gate
  rejects any non-import-lane mutation whose subject is an already-persisted foreign-authority entity with
  `foreign_authority`. Local facts about an import — the run linkage, semmem curation status, any product
  annotation — live on a local overlay or a local entity that references the import through `@id` (B-1).
- **#1096 on an imported firing entity (binding):** the run mint under `deps.Platform` stands. The run-anchor pair
  (`agvocab.LoopRun` = `agent.loop.run`, `agvocab.LoopRunEntityID` = `agent.run.entity-id`, `actions.go:1710-1712`)
  is written only when the firing loop carries the deployment's own authority. For an imported firing loop the rule
  action detects the foreign authority BEFORE `stampRun` (`pkg/types.ValidateEntityIDAuthority(entityID, org,
  platform, false) != nil`), skips both anchor writes deliberately — no mutation request targets the foreign subject,
  not even a rejected one — and records the skip as `rule_run_anchor_skipped_total{reason="foreign_authority"}` with
  an Info log naming the rule and the lane; it is a counted skip, never a rejection. The linkage moves to the LOCAL
  run entity: `agent.run.origin-entity-id` (`@id`; `agvocab.RunOriginEntityID`, declared beside `LoopRunEntityID` in `vocabulary/agentic/predicates.go:502`) as a birth predicate of the local run entity (`AgentRun.OriginEntityID`, lifecycle tag `predicate=agent.run.origin-entity-id`, set by `agentrun.Mint` at creation), set for every run, local or imported origin, so the run→loop pointer has one home that
  never depends on writing the loop. The run entity today carries only `agent.run.phase` and
  `agent.run.parent-entity-id` (`agentrun.go:114-124`; ADR-053) — the parent RUN, not the originating loop — so no
  existing predicate fits and the new one is required. Walk behaviour: from the local side, run →
  `agent.run.origin-entity-id` → the mirrored loop (a read; the mirror is local state) → its mirrored
  `agent.loop.parent` chain, so ancestry resolves; child loops the task spawns are local and carry `agent.loop.run`
  through `task.RunID`, so descendants resolve; a chained rule that reads `$entity.triple.agent.run.entity-id` off
  the imported firing loop finds nothing by design and must trigger on the local run entity or its local children.
  **#1096 is complete only when this path is implemented and tested** (tasks 2.6, 6.3; omissions M6a–M6c).
- **Hierarchy inference skips foreign-authority entities** (§B.4) — the only satisfiable reading of ruling 2 for
  containers; accepted by ruling on every lane. The
  authority pair reaches `inference.NewHierarchyInference` (which carries none today, `graph/inference/hierarchy.go:109-114`)
  through `HierarchyConfig` from the `deps.Platform` read graph-ingest adds.
- **Import lane declaration:** a boolean on the JetStream input port (`"import": true`) — an operator statement of
  trust, not a predicted framework value. Provenance recorded = the port name and the envelope `source` string.
  Nothing is authenticated (F3–F4); the contract says so. The reference declaration lives in
  `configs/graph-backend.json`, which composes graph-ingest. (This sentence originally contrasted it against
  `cloud-federation.json`/`edge-federation.json`; PR #1130 (#1129) deleted both. Re-measured 2026-08-27: 12 of the 14
  shipped `configs/*.json` compose graph-ingest — the exceptions are `gemini-example.json` and `prompts.json`.)
  (T6).
- **Signature (r3, F-9):** `pkg/types.ValidateEntityIDAuthority(candidate, org, platform string, importLane bool)
  error` — strings, because `types.PlatformMeta` lives in the root `types` package (P9).
- **Rejection:** new coded error in `pkg/types`: `ErrorCodeEntityIDAuthorityInvalid = "entity_id_authority_invalid"`,
  reasons `EntityIDReasonForeignAuthority = "foreign_authority"` (non-import lane) and
  `EntityIDReasonLocalAuthorityClaimed = "local_authority_claimed"` (import lane), detail `segment_index` (0 or 1)
  and `lane` — no identity bytes. Metered once at the boundary as
  `mutation_rejections{reason="authority_foreign"}` / `{reason="authority_claimed"}`, mirroring
  `structural_invalid` (`graph-ingest/spec.md:257-264`).
- **Exported-surface changes this forces (r3, F-5):** `graph.NewAlertEvent` (`graph/events.go:171-175`) and
  `ruleTriggerEntityID` gain `org, platform` parameters (no sister caller, P8); the rule processor gains a
  `platform` field plumbed from `deps.Platform` at construction — new plumbing, since it holds none today (R3).
  New exported surface on `graph/` and `pkg/types` requires owner design review per the architect contract.
- **#1096** folds in: `actions.go:1575-1583` mints from the new `platform` field; the firing entity stays the origin
  reference on the local run. #1096 is live today for any semsource-fed deployment (a rule firing on an imported
  entity mints under `acme.semsource`).
- **Imported lessons are invisible to the framework's own reader (r3, F-10):** `processor/agentic-loop/handlers.go:721`
  scans only the local `AgentLessonRecordPrefix`; ruled (O-13): an imported lesson does not apply locally by
  default — a loop opts in by naming the imported source in its scope.
- **What "provenance" can mean today:** the declared lane + envelope `source` + `Triple.Source`. Typed origin is a
  separate proposal (ADR-057 withdrawn); semmem's derivation-chain needs are product-side.

### C.4 Instance families and the collision matrix at a federation boundary

| Family | Examples | Collision across two deployments sharing `org.platform` | After the authority rule | Mitigation |
|---|---|---|---|---|
| UUID v4 | loop/chain executions, diagnoses | negligible | — | none needed |
| content hash | lessons (UUIDv5 over content incl. loop IDs), web observations (sha256-16 url), ADR-076 digests | iff identical content; loop IDs inside lesson content make cross-deployment lesson collisions vanish once platforms differ; web observations of the same URL collide by design | a foreign `org.platform` never collides with local | for imports: first-write-wins drops the second authorship — the import lane MUST reject a candidate whose ID already exists locally under a different `Triple.Source` (owner item O-4) |
| conventional name | model endpoints (`claude-sonnet`), semsource slugs, boid ids | likely | only within one authority | framework name-instanced families are deployment-scoped and MUST NOT be exported; products namespace by `system` |

### C.5 `platform.id` / `instance_id` precedence

Today `instance_id` silently wins (`config/config.go:772-778`; copied into semdev, semteams, semboids, semmem,
`cmd/*/main.go`). Two fields, one identity. Ruled (O-2): option (b); ADR-102 d2 names `platform.id`. Options: (a) keep both, reject at load when both are set and differ;
(b) one field. Recommendation: **(b)** — `platform.id` is the authority (already required and validated,
`:240-241`); `platform.instance_id` is removed from identity and its presence fails config load with replacement
guidance (`removedConfigFields` precedent, `processor/graph-clustering/component.go:239-269`). Cost: every shipped
config's minted platform value changes (fresh state anyway); every sister's `extractPlatformMeta` drops two lines.
Ruled (O-2): (b).

### C.6 The authority pair is bounded at configuration load (r3, F-1)

ADR-076 d2 fixed alert and trigger identities at 103 and 105 bytes on a 20-byte authority. Under ruling 2 the
authority is `org.platform` from config, which `config.Validate` does not bound (P7). Prefer observation: the
framework knows every fixed-suffix family it mints — the family table and its longest fixed suffix live in `pkg/types`
beside the identity contract, because `config` imports neither `graph` nor `processor/rule` and the trigger prefix is
unexported there (`graph_event_identity.go:14`); `graph/events.go:20` and `ruleTriggerEntityID` build from that
constant, so the number is never hand-copied — and config load derives the budget — `256 − 86` (the trigger
family, `rules.graph.trigger.` + 64 hex + two separators, the longest fixed suffix) = **170 bytes for `len(org) +
len(platform)`** — and rejects a configuration that exceeds it, naming the family that binds. No operator predicts
a byte count; the alert/trigger constructors keep their fail-closed validation as the second layer. ADR-102 amends
d2 accordingly. Ruled (O-14): the bound lives at config load; the number falls out of the family table.

## D. The break wave (beta.163), batched with #1093

Per `entity-id-contract:319-350`: fresh state, no migration, no alias, no dual contract. Per
`docs/operations/29-entity-id-contract-clean-cutover.md:17-30`: one breaking tag carries every coupled change.

**Landing shape (r3, F-7): slices A and B are ONE landing PR** (draft PR #1099, `Closes #1095`, `Closes #1096`),
because the contract (A) and the boundary that enforces it (B) are one system unit and the tier list is the union.

| Order | Item | BREAKING? | Covering tiers before landing |
|---|---|---|---|
| 1 | #1093 flow retirement (independent; smaller tree for the sweep) | yes | `task e2e:core`, `e2e:crud-tools`, `e2e:agentic` (per #1093) |
| 2 | #1095 — slice A (`pkg/types` order + names + prefix levels + domain authority + coded rejection + authority-pair bound; builder/pattern/config/doc/example sweep; audit extension + CI wiring) and slice B (boundary gate on the subject identity + import-lane port flag + hierarchy foreign-skip + #1096), one PR | yes | union: `e2e:core`, `e2e:structural` (hierarchy, rules), `e2e:statistical` and `e2e:semantic` (the only tiers asserting `EntityTypeSummary`, `tiered.go:350`), `e2e:agentic`, `e2e:lessons`, `e2e:lifecycle` (mission minted from wire authority, `mission/command.go:59-66`; lifecycle Manager on the mutation lane), `e2e:ops`, `e2e:crud-tools` (CRUD tools over `graph.mutation.>`), `e2e:research-graph` (seed builder `scenario.go:201-203`). Excluded with reason: `slow-consumer`, `throughput`, `openai-responses`, `deep-research` carry no position literal (`configs/rules/deep-research/*` use `*.*.*.*.*.*`) |
| 3 | gh606 / ADR-099 on the new cut points, container retirement (O-6), level 1 served by default and summaries gated there (O-11) | yes | `e2e:statistical`, `e2e:semantic` (ADR-099) |
| 4 | Sister re-slots (post-publication; communicate, do not modify) | — | each sister's own gates on fresh storage |

gh606 amendment list (r3, F-2) — the design must be **restated**, not annotated, before `/opsx:new
gh606-derived-communities`: `docs/proposals/gh606-derived-communities-design.md:24-25` (P4 "production reads level 0
only" — ruled O-11: level 1 is served), `:29` (P6 symbol names after tasks 3.2: `SourcePrefix`/`TaxonomyPrefix`/
`DeploymentPrefix`), `:65-71` (level table: 0 = `TaxonomyPrefix` 4 parts = source×taxonomy, 1 = `SourcePrefix` 3
parts = source, 2 = `DeploymentPrefix`), `:76-79` ("Level 0 = system" is false under O-B — the measured system
filter is level 1), `:90` (group-by calls), `:126` (record example), `:271` (GraphQL `level` docs), `:334-335`
(Q8 re-ruled: summaries gate to level 1). `docs/adr/099:25-27` is amended by ADR-102, not edited.

Sequencing window (r2, C3): between rows 2 and 3 the LPA provider (`graph/clustering/entityid_provider.go:231-236`,
live via `processor/graph-clustering/component.go:1331`) and the summarizer's domain grouping
(`graph/clustering/summarizer.go:719-731`) would otherwise compute on the wrong position with no test. Both are in
slice A's W3 rewrite explicitly (tasks 5.2) — they read `System` and `Domain` by named field until gh606 deletes
them — AND the tag holds until row 3 lands (O-7). Both, not either.

e2e rewrite list (r2/r3, W8, W10): `test/e2e/scenarios/ops/scenario.go:604` and `:712` (position literals),
`test/e2e/scenarios/tiered_structural.go:428-434` (variables named `domain`/`system` in the old order — rename with
the rewrite so the sweep is not misled), `test/e2e/scenarios/research-graph/scenario.go:201-203` (seed literal),
`cmd/e2e-semstreams/mission/command.go:59-66,324-328` (mission minted from wire `org`/`platform` — must carry the
deployment authority once the gate exists), `test/e2e/scenarios/tiered.go:350` (asserts the `EntityTypeSummary`
shape; its value expectations follow P6). `test/e2e/client/nats.go:965-974` is arity-only and stays. A literal
mismatch in these tiers before the rewrite is the expected shape, not a regression.

Values that leave the graph (r2, P5–P6): every consumer of `graphSummary` (`entity_types[].type`) and of the
vocabulary export sees the new token order; neither is re-minted by fresh state. The PR body names both.

Per-sister migration list (values → after):

| Sister | Change |
|---|---|
| semsource | `PlatformSemsource` constant → `deps.Platform.Platform`; order swap in `entityid.Build` call sites; register domains `web, media, config, git, golang, svelte`; `MaxOrgLen` arithmetic re-checked against the 170-byte pair bound (§C.6); `handler/entity_state_test.go` fixtures |
| semmachina | per-world composed `platform.id` is already the authority (one composition root per world); drop the `"semmachina-"` prefix or keep it — it is the operator's `id`; order swap |
| semboids | delete the `"semboids"` fallback literal; order swap in two builders; register `sim` |
| semdev | order swap (`forge.intake`, `repo.standards`, `agent.chain.execution` prefix); register `forge`, `repo`; drop `instance_id` precedence |
| semdragon | replace `Org "default"`/`Platform "local"` defaults with config; order swap (`game.<board>`, `web.agent.doc`); register `game`, `web` — `web` collides with semsource's `web` (owner item O-5) |
| semteams | one literal (`attestation_runner.go:124`); drop precedence; e2e configs |
| semops | `Platform: "edge"` literal → config; order swap (`cop.fusion`); register `cop` |
| semconnect | `semconnect` platform → config; `SystemEventIDPrefix` shape; register `systems` |
| semspec | order swap in `agentgraph/entities.go:54-60`; 10k fixtures importing semsource IDs re-generated after semsource re-slots |
| semsage | `OrgDefault`/`PlatformDefault` constants → config; `_` placeholder fixture |
| semmem | 5-part fixtures → six-part; PR #2 negative cases (`federation-mvp.md:46-55`) become the import-lane scenarios; imported lessons opt-in by scope (O-13); curation status on an imported lesson lives on a local overlay entity (O-12) |

Enforcement: `cmd/entity-id-audit` extended (slice A) with two segment rules over production Go and configs —
`authority_literal` (a literal, non-`*`, non-template value in positions 1–2 of a builder, pattern, or prefix
constant outside tests) and `domain_unregistered` (a literal position-4 value not in the registered set) — over two
NEW audit surfaces the tool lacks today (T5): `go-format-prefix` (a `fmt.Sprintf` format string whose dot-separated
tokens are read as positions with `%s` as a template position) and `go-dotted-constant` (a string constant of two or
more dotted tokens ending in `.`, e.g. `graph/events.go:20`, `processor/rule/graph_event_identity.go:14`). Plus the
30 existing findings classified, and `task entity-id:audit` added to the CI lint job.

Candidate-proof rows (per `release-candidate-proof`): tiers above green at the exact SHA; `task entity-id:audit`
green; cold start on fresh storage with readiness fail-closed through replay; `openspec validate --all --strict`.

Tag consequence and the priced fallback (r3, O-7): one tag is enough **only if** row 3 lands inside it.
Fallback if gh606 slips: tag rows 1–2 as beta.163 with the padding contract (B.4 option 2: reserved tokens declared,
the two predicates renamed, foreign-skip in force) — that costs one vocabulary rename that gh606 then deletes, one
extra audit rule, and a second fresh-state break at the gh606 tag: every sister provisions storage twice and every
downstream announcement is written twice. Ruled (O-7): one tag holding rows 1–3; the fallback stays on record only.

## F. Owner items — RULED 2026-08-26

All items were ruled by the owner on #1095 (2026-08-26): O-1 through O-11, O-13, and O-14 accepted as recommended
below; **O-12 overridden to option (a)**; the hierarchy-inference skip explicitly accepted. The table is kept as the
record of what was put to the owner.

| # | Decision | Evidence | Recommendation / ruling |
|---|---|---|---|
| O-1 | Order: O-A or O-B | §B; inventory W2–W6, C1–C4 | O-B |
| O-2 | `platform.id` vs `instance_id` (ADR-102 d2 is conditional on this) | C.5 | one field: `id`; `instance_id` removed with load-time guidance |
| O-3 | Are `system` values registered? | C.2 | no; audit rejects product-name literals |
| O-4 | Import-lane semantics for an ID that already exists locally under a different source | C.4 | reject (`local_authority_claimed` sibling reason `exists_foreign_source`) |
| O-5 | Cross-product domain collisions (`web`: semsource vs semdragon) | inventory §1.14 | registration makes it a boot-time composition rejection in a deployment running both; each product owns its delegation |
| O-6 | Hierarchy containers: retire with gh606, or declare padding tokens and rename the two predicates | B.4, H7 | retire with gh606; padding contract only as the priced fallback |
| O-7 | Tag split | §D (fallback priced) | one tag holding rows 1–3 |
| O-8 | ADR-076 d1 supersession (framework namespace → deployment authority for alerts/triggers) | PF-7, P8 | supersede d1; amend d2 (O-14); keep d3–d6 |
| O-9 | `gateddag` family re-slot (`gateddag.fanout.instance` → `gated-dag.agent.fanout`); the framework-reserved domain set is `{agent, ops, graph}` plus `gateddag` only if this is declined | inventory §1.14 | re-slot in slice A; reserved set `{agent, ops, graph}` |
| O-10 | Export IRI path order: `vocabulary/export/export.go:123-126` publishes `<base>/entities/{org}/{platform}/{domain}/{system}/{type}/{instance}` outside the graph; fresh state does not re-mint published artifacts | inventory P5 | follow the canonical order (one home for position order); announce it as a published-artifact break in the PR body; pinning a private exporter order would create a second interpreter of the shared type |
| O-11 (review O-12) | Which ADR-099 level is served by default after the reorder, and which level LLM summarisation gates to (re-rules gh606 Q8) | inventory C4: the ruling's "same system" is the system value = level 1 under O-B; gh606 P4/Q8 serve level 0 = source×taxonomy | serve level 1 (source) by default and gate summaries to level 1; level 0 stays available by request — the served partition should be the one the ruling measured |
| O-12 (review O-13) | May a local lane mutate (annotate) a foreign-authority entity, or is an import a read-only mirror? Live paths: run anchor (R3), inverse sibling edges (H6), semmem curation status on an imported lesson | C.3; `graph/helpers.go:98-107` | **RULED (a): read-only mirror** — no local lane mutates a foreign subject; local facts about an import live on a local overlay or a local entity referencing it; the run linkage moves to the local run entity (§C.3). Options (c) closed framework run-anchor pair and (d) delegated local namespaces were the recommendation and its costed variant; both considered and rejected by ruling |
| O-13 (review O-14) | Do imported lessons apply to local loops by default? | inventory L5: `handlers.go:721` scans the local prefix only | no by default; a loop's scope may name an imported source (`id:<peer-org.peer-platform.src>`) to opt in — applicability stays a declared scope, not an ambient effect |
| O-14 (review O-15) | The authority-pair byte bound: where it is enforced and that ADR-076 d2 is amended | §C.6, inventory P7 | enforce at config load, budget derived from the longest fixed-suffix framework family (170 bytes for `org`+`platform` today); constructors keep fail-closed validation as the second layer |
