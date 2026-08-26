# gh#1095 — entity-ID segment semantics: options, contract, break wave, owner items

**Baseline:** `origin/main` `5cc0c7fb`; committed as draft PR #1099 (`b6b4b024`). Row ids (S1, K3, W2, H1, …)
refer to the inventory.

## Checkpoint (revision 2)

- Inventory: `docs/proposals/gh1095-entity-id-segment-semantics-inventory.md` revision 2, SHA-256
  `ba2131bc2f743019728f10202ba149391d3254771ca3843ae94df3e7267dd216` (revision 1 was `0e511e9169b0952ab40cfb6f7dc4135c67e2678076dca6650c1a91cc18360b8f`).
- Review state: the independent blind inventory pass on revision 1 returned **INVENTORY PASS WITH DIVERGENCES**;
  D1–D5 are corrected and R-A–R-D added in this revision (marked `(r2)` in the inventory; this design's O-B rows,
  §D, and §F carry the corresponding changes).
- This design, ADR-102, and the spec deltas have **NOT** had a pre-owner design review; that review runs after this
  revision lands. Binding rulings stay with the owner.

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
| Blast radius | in-tree: `pkg/types` (P1–P3), 9 builders, 10 index-position readers (W3) including the LPA provider and summarizer (C3), 5 Go declaration patterns (W5: `agentrun.go:100`, `builtinprojection/contracts.go:26,56`, `gated-dag/participant.go:17`, `mission/state.go:28`), 3 config literals, 2 e2e literal assertions (W8), 29 docs naming the order plus `docs/concepts/18-rule-driven-artifacts.md` (whole-ID subject examples, S3), `semantictest` builder, `entityPartNames`; two values that leave the graph: the GraphQL `EntityTypeSummary.type` value (P6) and the vocabulary export IRI path (P5, owner item O-11); sisters: every builder literal that names domain before system (semboids 3, semdev 3, semdragon 4, semops 1, semconnect config, semsage constants, semteams 1 literal, semspec 2) — all files that ruling 2/5 already opens |

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
order can change.* The marginal cost over O-A is literal edits in files the ruling already opens; the marginal
benefit is permanent.

ADR-099 consequence: level 0 partition is identical as a set; level 1 changes from "taxonomy" to "source";
gh606's design table (`:65-71`) and §3.1 text must be restated before its change is created; nothing in ADR-099's
decision text needs supersession beyond the phrase "1 = domain (3)".

### B.4 Hierarchy containers without arity padding

Fixed arity cannot express a 3/4/5-part group as a first-class identity without either (a) padding with reserved
literal tokens, or (b) a digest family in the ADR-076 shape (`org.platform.<component>.graph.container.<sha256(prefix)>`)
with the prefix carried as a property. The framework already holds the group as a pure function of the ID
(ADR-099), so containers are a second spelling of the same fact (H5) with two latent defects (H2, H3).

Options: **(1) retire containers** (type/system/domain container entities and `hierarchy.*.member` edges) inside the
gh606 change, keeping only sibling edges if the structural tier still needs them; **(2) keep padding, declare the
tokens** (`group`, `container`, `level` reserved in position 6 by contract; the audit rejects any producer instance
equal to them; the byte overflow becomes a coded rejection at mint); **(3) digest family** (fixes H2/H3, keeps two
homes). Recommendation: (1), landed by gh606 in the same wave; (2) as the interim contract if gh606 slips a tag.
Owner item O-6.

## C. The segment-semantics contract (draft, O-B order)

| Pos | Name | Meaning | Owner / value source | Registered? |
|---|---|---|---|---|
| 1 | `org` | organization namespace; the tenancy root (ADR-032) | operator config `platform.org` (`config/config.go:225-238`, lowercased at load) | operator-declared |
| 2 | `platform` | **minting deployment authority** — the composition root that produced the entity | `platform.id` (see C.4) via `deps.Platform`; never from a payload, a constant, or a firing entity | operator-declared; unique within org by declaration |
| 3 | `system` | **source** — the subsystem, feed, repo, world, board, API, or framework component that produced the entity | the producer; stable per source; MUST NOT be the product name (product = provenance: `Triple.Source`, envelope `source`) | producer-chosen, unregistered |
| 4 | `domain` | **delegated taxonomy** — the subject-matter category | a registered delegation per producer (C.2); framework reserves `agent`, `ops`, `gateddag`, `graph` | registered at the composition root |
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
- Framework-reserved set declared in-tree (`agent`, `ops`, `gateddag`, `graph`); every framework builder in §1.14
  authorizes at construction (boot-time builders) or returns the coded error (Try* builders).
- A product registers its delegations where it installs its payload registry today (`RegisterPayloads`, the
  composition root — the same trusted boundary the predicate authority uses). Registration shape:
  `RegisterEntityDomains(authority, EntityDomainDelegation{Producer: "semsource", Domain: "git"}, …)`.
- Enforcement point: declaration time (builders, `EntityIDPattern` declarations, projection contracts,
  `lifecycle.Workflow`), NOT the graph-ingest hot path — the wire carries no producer identity (F3), so a runtime
  check would have nothing to authorize against. This mirrors the donor exactly.
- Consumer at birth: the nine framework builders and `internal/builtinprojection/contracts.go:26,56`.

What it does not do: it cannot stop a product from choosing a colliding domain with another product in the same
deployment; registration makes the collision visible at boot (two producers delegating the same domain is a
composition rejection, the ADR-076 d4 "duplicate PackID" shape). Owner item O-3: is `system` also registered?
Recommendation: no — `system` values are runtime-derived (repo slugs, world namespaces) and unbounded; registering
them would make every new repo a config change. The audit checks that a builder's system position is not a product
name literal.

### C.3 Boundary enforcement for `org.platform`

- **Where:** graph-ingest's structural gate (`graph-ingest/spec.md:232-278`; `component.go:1888`) on every lane —
  Graphable fact arrival, `graph.mutation.>`, direct persistence — before any KV I/O. The pure marshal seam stays
  syntax-only (it has no dependency).
- **What:** positions 1–2 of every final candidate ID and every `@id` object MUST equal `deps.Platform.{Org,Platform}`
  unless the arrival lane is a declared import lane. On an import lane a candidate whose positions 1–2 EQUAL the local
  authority is rejected (a peer cannot mint as this deployment); foreign pairs are accepted unchanged (never
  rewritten, ADR-076 d6).
- **Import lane declaration:** a boolean on the JetStream input port (`"import": true`) — an operator statement of
  trust, not a predicted framework value. Provenance recorded = the port name and the envelope `source` string. Nothing
  is authenticated (F3–F4); the contract says so.
- **Rejection:** new coded error in `pkg/types`: `ErrorCodeEntityIDAuthorityInvalid = "entity_id_authority_invalid"`,
  reasons `EntityIDReasonForeignAuthority = "foreign_authority"` (non-import lane) and
  `EntityIDReasonLocalAuthorityClaimed = "local_authority_claimed"` (import lane), detail `segment_index` (0 or 1)
  and `lane` — no identity bytes. Metered once at the boundary as
  `mutation_rejections{reason="authority_foreign"}` / `{reason="authority_claimed"}`, mirroring
  `structural_invalid` (`graph-ingest/spec.md:257-264`).
- **#1096** folds in: `actions.go:1575-1583` mints from `deps.Platform`; under the gate the old behaviour would be a
  loud rejection, so the fix lands with or before the gate.
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
`cmd/*/main.go`). Two fields, one identity. Options: (a) keep both, reject at load when both are set and differ;
(b) one field. Recommendation: **(b)** — `platform.id` is the authority (already required and validated,
`:240-241`); `platform.instance_id` is removed from identity and its presence fails config load with replacement
guidance (`removedConfigFields` precedent, `processor/graph-clustering/component.go:239-269`). Cost: every shipped
config's minted platform value changes (fresh state anyway); every sister's `extractPlatformMeta` drops two lines.
Owner item O-2.

## D. The break wave (beta.163), batched with #1093

Per `entity-id-contract:319-350`: fresh state, no migration, no alias, no dual contract. Per
`docs/operations/29-entity-id-contract-clean-cutover.md:17-30`: one breaking tag carries every coupled change.

| Order | Item | BREAKING? | Covering tiers before landing |
|---|---|---|---|
| 1 | #1093 flow retirement (independent; smaller tree for the sweep) | yes | `task e2e:core`, `e2e:crud-tools`, `e2e:agentic` (per #1093) |
| 2 | #1095 slice A — `pkg/types` order + names + prefix levels + domain authority + coded rejection; builder/pattern/config/doc sweep; audit extension + CI wiring | yes | `e2e:core`, `e2e:structural` (hierarchy, rules), `e2e:agentic`, `e2e:lessons`, `e2e:lifecycle` |
| 3 | #1095 slice B — boundary authority gate + import-lane port flag + #1096 | yes | `e2e:core`, `e2e:agentic`, `e2e:lessons`, `e2e:ops` |
| 4 | gh606 / ADR-099 on the new cut points (+ container retirement) | yes | `e2e:statistical`, `e2e:semantic` (ADR-099) |
| 5 | Sister re-slots (post-publication; communicate, do not modify) | — | each sister's own gates on fresh storage |

Sequencing window (r2, C3): between rows 2 and 4 the LPA provider (`graph/clustering/entityid_provider.go:231-236`,
live via `processor/graph-clustering/component.go:1331`) and the summarizer's domain grouping
(`graph/clustering/summarizer.go:719-731`) would otherwise compute on the wrong position with no test. Both are in
slice A's W3 rewrite explicitly (tasks 5.2) — they read `System` and `Domain` by named field until gh606 deletes
them — AND the tag holds until row 4 lands (O-7). Both, not either.

e2e rewrite list (r2, W8): `test/e2e/scenarios/ops/scenario.go:604` and `:712` pin positions 3–5 by literal and are
rewritten in slice A (tasks 5.3); `test/e2e/client/nats.go:965-974` is arity-only and stays. The `e2e:ops` and
`e2e:lessons` tiers cannot pass slice A without that rewrite, and a literal mismatch there is the expected shape,
not a regression.

Values that leave the graph (r2, P5–P6): every consumer of `graphSummary` (`entity_types[].type`) and of the
vocabulary export sees the new token order; neither is re-minted by fresh state. The PR body names both.

Per-sister migration list (values → after):

| Sister | Change |
|---|---|
| semsource | `PlatformSemsource` constant → `deps.Platform.Platform`; order swap in `entityid.Build` call sites; register domains `web, media, config, git, golang, svelte`; `MaxOrgLen` arithmetic re-checked; `handler/entity_state_test.go` fixtures |
| semmachina | per-world composed `platform.id` is already the authority (one composition root per world); drop the `"semmachina-"` prefix or keep it — it is the operator's `id`; order swap |
| semboids | delete the `"semboids"` fallback literal; order swap in two builders; register `sim` |
| semdev | order swap (`forge.intake`, `repo.standards`, `agent.chain.execution` prefix); register `forge`, `repo`; drop `instance_id` precedence |
| semdragon | replace `Org "default"`/`Platform "local"` defaults with config; order swap (`game.<board>`, `web.agent.doc`); register `game`, `web` — `web` collides with semsource's `web` (owner item O-5) |
| semteams | one literal (`attestation_runner.go:124`); drop precedence; e2e configs |
| semops | `Platform: "edge"` literal → config; order swap (`cop.fusion`); register `cop` |
| semconnect | `semconnect` platform → config; `SystemEventIDPrefix` shape; register `systems` |
| semspec | order swap in `agentgraph/entities.go:54-60`; 10k fixtures importing semsource IDs re-generated after semsource re-slots |
| semsage | `OrgDefault`/`PlatformDefault` constants → config; `_` placeholder fixture |
| semmem | 5-part fixtures → six-part; PR #2 negative cases (`federation-mvp.md:46-55`) become the import-lane scenarios |

Enforcement: `cmd/entity-id-audit` extended (slice A) with two segment rules over production Go and configs —
`authority_literal` (a literal, non-`*`, non-template value in positions 1–2 of a builder or pattern outside tests)
and `domain_unregistered` (a literal position-4 value not in the registered set) — plus the 30 existing findings
classified, and `task entity-id:audit` added to the CI lint job.

Candidate-proof rows (per `release-candidate-proof`): tiers above green at the exact SHA; `task entity-id:audit`
green; cold start on fresh storage with readiness fail-closed through replay; `openspec validate --all --strict`.

Tag consequence: one tag is enough **only if** row 4 lands inside it; otherwise beta.163 and the gh606 tag are two
fresh-state breaks and every sister provisions twice. Recommendation: hold the tag until row 4 lands (owner item
O-7).

## F. Owner items

| # | Decision | Evidence | Recommendation |
|---|---|---|---|
| O-1 | Order: O-A or O-B | §B; inventory W2–W6, C1–C2 | O-B |
| O-2 | `platform.id` vs `instance_id` | C.5 | one field: `id`; `instance_id` removed with load-time guidance |
| O-3 | Are `system` values registered? | C.2 | no; audit rejects product-name literals |
| O-4 | Import-lane semantics for an ID that already exists locally under a different source | C.4 | reject (`local_authority_claimed` sibling reason `exists_foreign_source`) |
| O-5 | Cross-product domain collisions (`web`: semsource vs semdragon) | inventory §1.14 | registration makes it a boot-time composition rejection in a deployment running both; each product owns its delegation |
| O-6 | Hierarchy containers: retire with gh606, or declare padding tokens | B.4 | retire with gh606; declare as interim |
| O-7 | Tag split | §D | one tag holding rows 1–4 |
| O-8 | ADR-076 d1 supersession (framework namespace → deployment authority for alerts/triggers) | PF-7 | supersede d1; keep d2–d6 |
| O-9 | Enforcement strictness on non-import lanes: reject (recommended) vs metric-only | C.3; `entity-id-contract:202-203` forbids permissive modes | reject |
| O-10 | `gateddag` family re-slot (`gateddag.fanout.instance` → `gated-dag.agent.fanout`) | inventory §1.14 | yes, in slice A |
| O-11 (r2) | Export IRI path order: `vocabulary/export/export.go:123-126` publishes `<base>/entities/{org}/{platform}/{domain}/{system}/{type}/{instance}` outside the graph; fresh state does not re-mint published artifacts | inventory P5 | follow the canonical order (one home for position order); announce it as a published-artifact break in the PR body; pinning a private exporter order would create a second interpreter of the shared type |
