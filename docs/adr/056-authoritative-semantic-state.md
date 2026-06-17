# ADR-056: Authoritative Semantic State — The Predicate-Group Ownership Contract for ENTITY_STATES

## Status

**Accepted** — 2026-06-13 (v6, post-merge review thread closed; human sign-off received).
v6 threads the final pre-Accept review (2 P1 + 1 P2): the fourth-path stub-lane
self-contradiction is resolved (the no-birth referential stub is promoted to a first-class,
envelope-bearing framework artifact with a named `ForeignEdgeClaim` owner — Decision 4 lane ii —
reconciled across the framework-surface summary and the ADR-055 narrowing); the write-time
owner-token lease check is given a concrete WIRE CONTRACT (`OwnerToken` field on the graph-ingest
mutation request structs + `ErrorCodeOwnerLeaseStale`, Decision 2); and the stale "sensorml
declares `hosts`/`isHostedBy`" changelog line is corrected to "only `isHostedBy`." No
architectural change — these are contract-precision fixes at the enforcement points. v6 also adds
**two acceptance fixtures** (Consequences §) on complementary axes: **semconnect cs-api** (gateway-write,
Decisions 1–4) and **semteams** (rule/lifecycle, Decision 5 + "rules observe, don't own"). Both are run
against the real consumers' ownership surfaces and described exactly with no Decision re-architecture;
the only thing either forced is prose (a shared-vocabulary-ownership note, and a predicate-group-scoped
correction to "rules observe, don't own" that the semteams run-entity pattern falsified the old wording
of). The gate Coby set for Accept ("if 056 can describe how the consumers register their claims, the
design is real") is passed on both axes.
The architecture is VALIDATED across four adversarial rounds (verdict: 6/6 no hard FAIL);
v5 pins the remaining **implementation contracts** the v4 review (Codex 1 BLOCKING + P1s)
and the verification gate flagged — these are contract-precision fixes, **no architectural
forks**. This ADR names and **enforces** a responsibility the framework has been carrying
implicitly since ADR-049 and patching symptomatically through ADR-055. The architecture is
SETTLED — Decisions 1–5 are unchanged from v3/v4 (v6 ADDS Decision 6 — claims derive from graph
projection contracts — which is additive and constrains the layer ABOVE the substrate, not 1–5);
v5 makes the load-bearing mechanisms
*mechanically buildable* (valid NATS KV keys + a real TTL bucket split; producer identity
threaded to the T2 seam; the hosts-vs-isHostedBy foreign-edge distinction corrected;
the crash-recovery drain made exactly-once-asserting; the Watch-revival made fatal-halt;
the flip-gate predicate sharpened to hatch-empty). No new architectural forks. Every
load-bearing claim cites `file:line` against code read on 2026-06-13. Where this ADR
disagrees with a summary in `CLAUDE.md` or `MEMORY.md`, the cited code wins.

v5 set Proposed with every punch-list item pinned and **no remaining BLOCKING** (see the
v4 → v5 changelog). v6 flips to **Accepted**: the human author signed off conditional on
threading the post-merge review (2 P1 + 1 P2), and that thread is now closed in this revision
(see the v5 → v6 changelog below). No BLOCKING remains.

This ADR **does not supersede** ADR-049 or ADR-055. It is the missing parent. ADR-049's
`Manager` is one *implementation* of the contract; ADR-055 **narrows** to a single
enforcement rule under it (see "How ADR-055 narrows under this parent"). The
cryptographic-provenance follow-up is scoped (not designed) in
[ADR-057](057-cryptographic-provenance.md).

### v6 → v6.1: rule packs are a config-level projection producer (gh#278; additive, no Decision re-architecture)

semteams (the Decision-5 acceptance-fixture consumer) mapped its full rule-pack exposure and
surfaced a producer class Decision 6's derivation chain did not yet reach: the rule engine's triple
actions. All rule triple actions are on the legacy bare-triple lane (`graph.mutation.triple.add` /
`.remove`, `processor/rule/triple_mutator.go:17-18`) and NONE can declare ownership — yet a rule pack
can legitimately OWN a coordination predicate group (the ADR-053 HITL `*_pending` markers,
`autoresearch.best.value`). This amendment NAMES that class and corrects two framings; it adds NO new
model (the `projection.Contract` + `pkg/ownership` substrate already accommodate it — `Contract.Validate`
requires only a logical `Name` + ≥1 group, `MessageType` is optional, `pkg/projection/contract.go`) and
re-architects no Decision:

- **Decision 6a (new subsection)** — a rule pack is a config-level projection producer: it may declare
  `projection.Contract`s beside its rule definitions and `Bind` them at rule-pack load under a stable
  subject-safe owner id `rule-pack.<pack-id>` (the `validOwnerID` charset `[A-Za-z0-9._=-]`,
  `pkg/ownership/glob.go:21` — NOT a colon, NOT a `RuleToken` hash; the owner id is compared and keyed
  as the canonical string). Two packs claiming the same cell collide via the same epoch-CAS overlap
  check as any owner — no parallel registry.
- **Decision 4 flip-gate gets a bare-triple-lane precondition (Fix part 6)** — for a product rule pack
  that stamps a framework-born entity via `triple.add`, the flip signal is
  `mutation_rejections_total{subject="graph.mutation.triple.add", reason="entity_not_found"}` reading
  zero over a bake window + product e2e (anchors born before markers), NOT `foreign_edge_unclaimed_total`
  (a bare `triple.add` never enters the foreign-edge classifier — Lane-independence correction). Note the
  live metric is `mutation_rejections_total` (`processor/graph-ingest/component.go:111-126`); the
  `unregistered_authoritative_write_total` counter named in the upstream feedback does NOT exist today
  (it is Decision-5 enforcement surface, not yet built).
- Two SEPARATE follow-up increments (named here, not designed): the rule-pack contract-binding wiring,
  and a single declared `replace-owned` rule action (`update_with_triples`, scoped to contract-declared
  owned predicates — NOT `cas-transition`; phase stays with the lifecycle `Manager`). `add_triple` is
  unchanged (append-evidence, must-exist after the flip).

Status stays **Accepted**: additive, grounded in code read 2026-06-14, no BLOCKING.

### v5 → v6: post-merge review thread closed (2 P1 + 1 P2; no architectural change)

ADR-056 merged to main (PR #270) at Status Proposed. The pre-Accept review raised three findings,
all at enforcement points; v6 threads them and flips Status to **Accepted**. No Decision changes
shape.

- **P1 — the fourth auto-vivify path contradicted itself across three places.** Decision 4 lane (ii)
  said no-birth targets "keep an EXPLICIT referential-stub lane with a must-exist exemption"; the
  framework-surface summary said `ensureReferencedEntityExists → enqueue, not stub Put`; the ADR-055
  narrowing said "no envelope-less stub." Load-bearing, not cosmetic. **Closed by choosing horn (a):**
  the no-birth referential stub is **promoted to a first-class, envelope-bearing framework artifact**
  with a named owner/claim — `MessageType = core.identity.stub` (domain `core`/category `identity`),
  creating owner recorded as the producer's registered `ForeignEdgeClaim` in a `core.identity.stub_owner`
  triple. Horn (b) (remove the lane) is rejected because sensorml's child has no birth to drain and no
  registered inverse to backfill, so removal would dangle the edge forever. The framework-surface
  summary and the ADR-055 narrowing are reworded to match (both lanes named; "no ENVELOPE-LESS stub"
  is now literally true — the surviving stub carries an envelope).
- **P1 — the write-time owner-token check asserted enforcement but named no wire surface.** Decision 2
  now pins it: `OwnerToken` is a typed field on `Update`/`CreateEntityWithTriplesRequest`
  (`graph/mutation_requests.go:80,38`) — the registry-exempt point-to-point structs that already carry
  `ExpectedRevision`/`TraceID` — NOT a NATS header and NOT a `BaseMessage` envelope. The value is the
  lease handle minted at `RegisterOwner`; graph-ingest rejects a mismatch with a new
  `ErrorCodeOwnerLeaseStale` through the same response path `ErrorCodeRevisionMismatch` uses
  (`pkg/lifecycle/graph_emit.go:134-139`).
- **P2 — stale sensorml changelog line.** The BLOCKING-B changelog said sensorml declares both
  `hosts` and `isHostedBy` as `ForeignEdgeClaim`s; the corrected design (056:872) says only
  `isHostedBy` is the foreign-subject claim (`hosts` carries the parent's own id as Subject). The
  changelog line is corrected to match.
- **Acceptance fixtures added — semconnect cs-api AND semteams (the gate Coby set for Accept).** A new
  "Consequences → Acceptance fixture" subsection runs the design against TWO real consumers on
  complementary axes, both read 2026-06-13.
  - **semconnect (`../semconnect/gateway/cs-api/`) — gateway-write axis (Decisions 1–4).** 11 entity
    types, ~30 `cs-api.*` predicates plus shared `sensorml.*`/`csapi.*`/`sosa:*` vocabulary. 056
    describes the registrations EXACTLY with no new mechanism. Four sharpenings: (1) owned sets are
    dominated by shared vocabulary predicates owned-by-the-writer not the vocab package; (2)
    disjoint-id-space is the cross-producer contract, overlap-reject is the design working; (3)
    `sensorml.component.isHostedBy` is owned AND foreign by subject position, no collision because
    subject-partitioned, no-birth-stub lane because no inverse; (4) two named cs-api migration sites
    (`systems_crd.go:145` blanket replace, `systems_post.go:317` single-subject ingest).
  - **semteams (`../semteams/`) — rule/lifecycle axis (Decision 5 + "rules observe, don't own").**
    Q1 dual-write probe CLEAN (no private `*_STATES` mirror; Decision 5 grounded in a 2nd consumer).
    Q2 found 22 agent-run rules stamping the FOREIGN run entity — correct ADR-053 coordination, but
    it **falsified the ADR's own prose** ("rules cleared only because they stamp their trigger
    entity"). Fixed: *For the architectural identity* now states the invariant is
    PREDICATE-GROUP-scoped — the run entity is **multi-owned** (lifecycle-Manager phase + a disjoint
    rule-marker group), the anti-pattern is REPLACING another owner's group, not writing a foreign
    entity. semteams's marker-group classification (append-evidence vs registered rule-pack owner) is
    its migration question, expressible today.
  - **Both fixtures pass with NO Decision re-architecture** — the only changes either forced are
    prose (the two ownership clarifications). The "if it can describe the consumers, the design is
    real" gate is passed on both the write axis and the rule/lifecycle axis.
- **Decision 6 ADDED — claims derive from graph projection contracts (review concern, 2026-06-13).**
  A reviewer flagged the real risk that ADR-056 could become "…also register your ownership strings
  over here" — a parallel semantic registry that drifts from the flow-based design and rots. Closed
  by naming the three layers as ONE declarative chain (payload-type registry → graph projection
  contract → ownership enforcement) and making it a HARD requirement that ownership claims DERIVE
  from a registered projection contract (declared beside the type/Graphable/resource-projection),
  with manual `RegisterOwner` only as the low-level escape hatch (lifecycle `Manager` dynamic
  patterns; migration scaffolding). `pkg/ownership` is the enforcement substrate ONLY. Additive —
  constrains the layer ABOVE the substrate, changes none of Decisions 1–5.
- **W0 spine implemented + reviewed (`pkg/ownership`).** Increment 1 of the implementation landed
  alongside this revision: the claim types, the single-epoch-key registry with overlap rejection
  (glob × exact-predicate, incl. Owner×ForeignEdge cross-type), stale-owner compaction, and the
  `OwnerOf` lease lookup — pure-logic + testcontainer-integration tested, `task lint` clean. A
  pre-merge go-reviewer pass hardened it: liveness/lease keyed on the canonical owner id (no FNV
  hash on the correctness path), waivers moved to epoch scope with MUTUAL consent (neither owner can
  unilaterally waive into the other's cell), presence rolled back on a fresh owner's failed
  registration, and compaction grace pinned to the OWNER_PRESENCE bucket TTL. Deferred to later
  increments (named in `pkg/ownership/doc.go`): the T2-seam reject + inverse-gate, the PENDING_EDGES
  buffer + crash-recovery flip-gate, the `OwnerToken` wire field + handler lease check, and the
  graph-ingest boot wiring + projection-contract derivation (Decision 6).
- **W0 first consumer landed — the `lifecycle.Manager` embed (Decision 5).** Increment 2: the Manager
  is the first framework consumer of the registry. A pre-implementation architect review reshaped the
  seam around a confirmed boot-ordering hazard — in every binary, `NewManager`/`Register` run BEFORE
  the flow service (and graph-ingest) starts (`cmd/semstreams/main.go:186,193` vs `:220,:225`), so
  creating the ownership buckets in graph-ingest's `initStorage` would make every registration a
  silent no-op. The increment therefore: (1) **`ownership.EnsureBuckets`** creates OWNER_CLAIMS
  (history, no TTL) + OWNER_PRESENCE (bucket TTL = `PresenceTTL`, the whole staleness backstop) and
  is called EAGERLY in the boot path BEFORE `Register`, not in graph-ingest; (2) **`Manager.AttachOwnership(ctx, reg)`**
  embeds the registry and derives each `Workflow` into TWO claims by mode (phase = `cas-transition`;
  audit + writable projected fields = `replace-owned`; read-only/reference/child-link predicates the
  Manager does not author are NOT claimed); (3) the **owner id is the workflow Name** (the workflow
  TYPE is the owner, shared idempotently across processes; presence reflects "≥1 process of this type
  live"); (4) a substrate-owned **`Heartbeater`** ticks liveness over the **app-root ctx** (cancellation
  stops it — no `Close` for sister repos to adopt); (5) the runtime posture is **OBSERVE-ONLY** — a
  cross-owner overlap is LOGGED, not bricked (consistent with Decision 5's observe-only runtime
  enforcement; the substrate still rejects it, the Manager swallows the rejection), while a malformed
  self-claim stays fatal. The hard-fail flip + the `OwnerToken` write-lease + the Watch-revival are the
  deferred write-gating half (an evicted-then-revived owner only churns the epoch until then — no
  dropped writes — which is why observe-only is safe to ship first). Sister repos (semspec/semteams/
  semconnect) opt in by adding the same `EnsureBuckets` + `AttachOwnership` pair; absent it, behaviour
  is exactly pre-ADR-056 (graceful-skip for resourceless/unmigrated deploys). Pure-logic + testcontainer
  tested through `Manager.Register` (the production entry), `task lint` clean; an `e2e:lifecycle` tier
  run gates the merge (Manager boot-surface change).
- **W0 4a landed — the T2-seam reject (OBSERVE-ONLY).** Increment 3: the foreign-routing seam
  (`ingestEntity`, `component.go`) now CLASSIFIES each foreign-subject edge against the registered
  `ForeignEdgeClaim`s and counts the unclaimed ones on `foreign_edge_unclaimed_total{message_type,predicate}`
  with a one-time WARN per `(message_type,predicate)` — **it does NOT change routing** (edges are
  still appended, deprecated-on-arrival; the hard reject + the ADR-055 must-exist flip are 4c).
  graph-ingest is a **reader** of `OWNER_CLAIMS` via a read-only `ownership.ClaimReader` (one epoch
  read per foreign-bearing ingest; graceful-skip to no-classification when the bucket is absent).
  The boot-time **inverse-gate** is wired (`RegisterOwner` runs `CheckInverseGate` over the
  registration's foreign edges using a `vocabulary.InverseResolver` injected at `EnsureBuckets`;
  nil-resolver → skip-with-WARN), and the **FE-claim-only compaction exemption** (an FE claim is not
  a lease — it contests nothing — so it is not liveness-reaped, which otherwise made the metric flap
  every `PresenceTTL`). sensorml's `WithInverseOf` is registered. **Two caveats pinned by a
  pre-implementation architect review and load-bearing for 4c's gate:** (1) **No production producer
  emits foreign edges today** — OMS and StoredMessage (the Graphables that reach the seam in
  `cmd/semstreams`) emit none, and sensorml is not a registered payload nor ingested by any binary.
  So `foreign_edge_unclaimed_total` reads **zero in production BY ABSENCE**, NOT because producers
  migrated — 4c's "hatch-empty over a bake window" gate must not read this zero as "migrated," and
  the seam is exercised by a **registered test-fixture producer**, not sensorml-in-prod (wiring
  sensorml into the framework binary would be the framework-vs-product boundary trap). A real
  SensorML ingest path is separate, larger work. (2) The metric counts edges **CLASSIFIED-unclaimed
  at the seam**, NOT routing failures (it fires before `AddTriples`' all-or-nothing batch), which is
  the semantics 4c's gate must assume.
- **Shared projection-normalization seam landed — 4a RELOCATED, lane-independence corrected (Coby,
  post-4a).** The 4a hook lived only on the fact-arrival `ingestEntity` lane, so its zero metric did
  NOT prove "no consumer" — it proved the observation point missed the **mutation API**, which is how
  the framework's real consumers write (semconnect cs-api `POST /systems` → `create_with_triples`;
  semteams rules → `triple.add`). The "no production producer" caveat above is corrected: it was a
  fact-arrival-only artefact. The fix is one **shared `normalizeProjection`** seam (partition against
  the primary subject + classify foreign edges) called from `ingestEntity`, `create_with_triples`,
  AND `update_with_triples` (bare `triple.add` stays a direct ownership-checked write to its subject,
  not a foreign-edge producer, until the request grows origin context). A foreign edge sent via the
  mutation API is now partitioned off the primary (no longer misfiled onto `Entity.ID`), classified,
  and routed onto its own subject — for every current caller `foreign` is empty (single-subject
  batches), so this is a behavioural no-op today and the foundation semconnect builds on when it drops
  `singleSubject` (`systems_post.go:317`, a migration guard for the previously-missing framework
  support, NOT the desired architecture). See the "Lane-independence correction" note under Decision 4.
- **W0 4b fourth-path fold landed — envelope-bearing referential stub (re-evaluated WITH the consumer
  correction).** `ensureReferencedEntityExists`'s envelope-less stub `Put` is promoted to a
  first-class envelope-bearing artifact: `MessageType = core.identity.stub.v1` + a
  `core.identity.stub_owner` triple (the reachable SOURCE `MessageType`; the ADR's "producer's
  `ForeignEdgeClaim` `MessageType`" was unreachable at this seam — corrected above). The stub stays
  PROFILE-LESS, so `MergeEntity`'s true-birth detection (profile-absence keyed) is intact — directly
  unit-tested (`TestReferentialStub_RealMergeStillDetectsTrueBirth`, the architect's flagged risk).
  This is the lane-ii fold ONLY; the `PENDING_EDGES`/lane-i buffer is still deferred, and the
  re-evaluation against the semconnect-consumer correction CONFIRMS that's right: cs-api's single
  `POST /systems` references the no-birth child via the parent's own `hosts` edge, so the stub
  materializes it BEFORE `routeForeignEdges` lands the foreign `isHostedBy` — no future-birth drain
  (lane-i) is triggered. This fold is what makes cs-api's no-birth children survive the eventual
  must-exist flip (4c). **E2E coverage gap (tracked, gated to 4c):** no e2e tier drives a production
  Graphable through the fourth path today (OMS relationship objects are IRIs, not entity IDs; mission
  uses literal-string objects), so the stub-shape change is unit + integration covered but not yet
  e2e-exercised end-to-end. Closing it (a relationship-target-bearing e2e fixture) is a prerequisite
  for the 4c must-exist flip, NOT for this additive fold — the fold keeps auto-vivify working, so it
  does not change observable behaviour for any current path.
- **W0 4c-pre-2 landed — `routeForeignEdges` per-edge must-exist policy (stub-materialize-or-loud-drop,
  not bare `AddTriples`).** The routing seam now does its OWN target-existence check
  (`foreignTargetExists`) and, for an ABSENT target, branches on the covering `ForeignEdgeClaim`'s
  `EdgeMode` (a new `ClaimReader.ForeignEdgeMode` reader reusing `epoch.foreignEdgeClaimFor`):
  `EdgeNoBirthStub` materializes the 4b stub then appends; `EdgeStrict` loud-drops (the new
  `foreign_edge_dropped_total{message_type,predicate,reason=strict_absent_target}` metric — kept
  DISTINCT from `foreign_edge_unclaimed_total` so a Strict drop can't corrupt the flip-gate's
  hatch-empty signal); `EdgeConditional`/`EdgeBackfill` are DEFERRED (the `PENDING_EDGES` buffer is a
  later increment) and route-with-warn + `reason=conditional_deferred` in the interim; an UNCLAIMED
  edge stays routed deprecated-on-arrival so the hatch can still drain to zero. A PRESENT target
  appends regardless of mode; `claimReader == nil` graceful-skips to the legacy bare append; a read
  blip on either the existence check or the mode lookup fails OPEN (route-toward-append, never
  Strict-drop on a transient error). The own existence check is what makes routing correct BOTH before
  and after the eventual flip removes `AddTriples`' auto-vivify else-branch. **Lane-asymmetry note
  (corrects the 4b "stub materializes BEFORE `routeForeignEdges`" claim):** that upstream stub
  (`ensureRelationshipTargetsExist` walking the parent's own `hosts` edge) runs only on the
  fact-arrival and `create_with_triples` lanes; `update_with_triples` does NOT call it, so on the
  update lane `routeForeignEdges`'s own `ensureReferencedEntityExists` call for `EdgeNoBirthStub` is
  load-bearing, not redundant. Additive / observe-leaning: only `EdgeStrict`+absent drops, and no live
  producer registers `EdgeStrict` today (sensorml's `isHostedBy` is `EdgeNoBirthStub`). Does NOT by
  itself enable the flip — that remains gated on the `PENDING_EDGES` increment (if any Conditional
  producer ever ships), the bare-triple-lane precondition (Fix part 6), the hatch-empty bake, and the
  e2e fourth-path fixture. *(Source-line anchors elsewhere in this Decision-4 section predate W0 and
  have drifted; trust symbol names over line numbers when reading the code.)*

### v4 → v5: implementation contracts pinned (1 BLOCKING + P1s; no architectural forks)

The four-round adversarial validation settled the ARCHITECTURE; v5 closes the remaining
**contract-precision** holes so each load-bearing mechanism is mechanically buildable. No
Decision changes shape — these are how-it-binds fixes.

- **BLOCKING — the registry keys used SLASHES, which are INVALID NATS KV keys.** v4 wrote
  `OWNER_CLAIMS/_registry` and `OWNER_CLAIMS/heartbeat/<owner_id>`. NATS KV keys permit only
  alphanumerics + `-` `_` `=` `.` — **no `/`** (and no `>`/`*`, the wildcard tokens); the
  rest of the codebase keys with dots (`entity.sensor.`, `natsclient/kv.go:357`; `0.>`,
  `:380`). **Closed (Decision 2 rewritten):** the epoch key is the bare `_registry`; presence
  keys are dot-segmented `heartbeat.<owner_token>` where `<owner_token> = governance.RuleToken(owner_id)`
  (`governance/verdict.go:114-119` — a 64-bit FNV-1a hex, already the framework's
  subject/key-safe encoding for free-form ids that may contain `.`/space/`*`/`>`; canonical
  `owner_id` travels in the epoch value for display). **TTL resolved concretely:** `natsclient.KVStore`
  exposes **no per-key TTL** — `Create`/`Put` take only `(ctx, key, value)` (`natsclient/kv.go:95,112`),
  no `KeyTTL`/`LimitMarkerTTL` passthrough. So a per-key TTL is not available without a wrapper
  extension; rather than extend the wrapper, **presence keys move to a SEPARATE bucket
  `OWNER_PRESENCE` with bucket-level TTL** (the epoch lives alone in `OWNER_CLAIMS`, no TTL — a
  bucket TTL on `OWNER_CLAIMS` would also age out the durable `_registry` epoch). Clean, no
  `natsclient` change. (See Decision 2 "Bucket + key layout".)
- **P1 — producer identity must REACH the T2 seam or the reject can't fire.** v4's
  unclaimed-foreign-edge reject keys on the producer, but `ingestEntity` (`component.go:908`)
  receives only `*graph.EntityState` — `extractEntityFromMessage` (`:963-1019`) has already
  reduced payload identity to `entity.MessageType = msg.Type()` (`:992`) + StorageRef + profile,
  dropping the concrete Graphable type and the rest of `BaseMessage`. **Closed:** producer
  identity for the gate **IS the registered `MessageType`** (the payload-registry key already
  on `EntityState`), and a `ForeignEdgeClaim` is keyed to that `MessageType`. It is already
  threaded — no new plumbing — because `partitionTriplesBySubject`/`AddTriples` run inside
  `ingestEntity`, which holds `entity.MessageType`. The reject keys
  `foreign_edge_unclaimed_total{message_type,predicate}`.
- **P1 — fourth-path semantics made precise (three sub-fixes).** (a) **hosts vs isHostedBy
  corrected:** only `isHostedBy` (`Subject=childID`, `graphable.go:124`) is a foreign edge / a
  `ForeignEdgeClaim`; `hosts` (`Subject=a.entityID`, the parent's OWN id, `graphable.go:123` +
  `EntityID()` `:37`) is the source entity's own triple — **NOT** a foreign edge, **NOT** a
  `ForeignEdgeClaim`. Every "declare hosts/isHostedBy as ForeignEdgeClaims" is corrected to "declare
  `isHostedBy`." (b) **Source-edge visibility ruled:** `ensureRelationshipTargetsExist`
  (`component.go:1311`/`:1411`) runs AFTER the source entity is written (`MergeEntity`/`createEntity`),
  so the source's relationship edge is already visible; only the TARGET node is deferred. Graph
  traversal degrades to "edge present, target node absent," never a withheld edge. (c) **Fold
  preference INVERTED:** enqueue-into-`PENDING_EDGES` only when the target has a FUTURE owner-birth
  to drain against. sensorml children are NEVER independently published (one `EntityID()` in the
  package, `Asset` returns the parent id, `graphable.go:37`; `ChildIDFn` optional, `:166-175`) → no
  birth → enqueue would dangle forever. For no-birth targets the DEFAULT is an explicit
  referential-stub lane with a must-exist EXEMPTION (the stub is load-bearing), not a grudging fallback.
- **P1/P2 — crash re-drain idempotency is a HARD exactly-one test.** `AddTriples` appends BLINDLY
  (`component.go:1798`: `entity.Triples = append(entity.Triples, group...)`, no (s,p,o) de-dupe),
  so re-applying a pending edge after crash-before-drain would DUPLICATE it — the v4 "benign
  double-apply, idempotent on (subject,predicate,object)" claim is FALSE against this path. **Closed:**
  the drain MUST use a dedicated **de-dupe merge (replace-by-(subject,predicate,object))**, not blind
  `AddTriples`; and the flip-gate crash-recovery test asserts **EXACTLY ONE** edge after
  enqueue → crash-before-drain → restart (the literal gate), not merely "present."
- **Watch-revival = FATAL HALT + the double-WRITE window closed.** The post-registration Watch
  overlap re-check on a compacted-then-revived owner **terminates the revived process** (fatal halt),
  not just logs. AND: to close the bounded false-eviction window where two owners could briefly
  double-WRITE the predicate group (not just double-register), authoritative writes carry a
  **write-time owner-token verified against the epoch** (a lightweight lease check) so a
  stale-evicted owner's writes fail at the write seam, not only at the next registration.
- **Flip-gate predicate = hatch EMPTY.** ADR-055 closing-move part-1 gate changes from "the
  unclaimed-foreign-edge reject is wired" to "**zero `foreign_edge_unclaimed_total` over a bake
  window**" — the flip cannot land while ANY live foreign-edge producer (e.g. sensorml) is still on
  the deprecated escape hatch.
- **Nits — stale version markers purged** ("v3 fixes both" / "v3 makes" in Decision 4 → version-neutral).

### v3 → v4: two BLOCKING *implementation* holes closed + three MEDIUMs

The v3 three-lens final gate confirmed the ARCHITECTURE is settled but found that BOTH
v3 BLOCKING fixes had residual **implementation-correctness** holes — the mechanism
each described did not actually catch the case it claimed to. v4 closes both with real
mechanisms (no new architectural fork) and folds three MEDIUMs.

- **BLOCKING-A — v3's per-owner-id key cannot catch CROSS-OWNER overlap.** v3 keyed
  `OWNER_CLAIMS` as one record per `owner_id`, with CAS-on-`Create` as the race-breaker.
  But CAS only conflicts on the **same key**: two *different* owners
  (`execution-manager` + `requirement-executor`) booting together write *different*
  keys, both `Create`s succeed, and their overlap is never compared — the exact
  cross-process collision the fix exists to catch slips through. CAS-per-owner-key only
  serializes a *same-owner re-register*. **Closed (Decision 2 rewritten):** the
  overlap-arbitrated state lives in a **single epoch key** (the bare `_registry` key in the
  `OWNER_CLAIMS` bucket — v5 corrects v4's invalid slash-key)
  holding the UNION of every registered owner's claims, written under
  `UpdateWithRetry` CAS (`natsclient/kv.go:172`). Registration is
  **read-epoch → check-overlap → merge-own-claims → CAS-write-at-read-revision →
  retry-on-mismatch-against-the-now-larger-epoch.** A concurrent registrant advances
  the epoch; the loser re-reads an epoch that now *contains the winner's claims* and
  re-runs overlap — so there is a **total order across ALL registrants of ANY claim**
  and the cross-process overlap IS detected. Per-owner liveness rides lightweight
  presence keys (`heartbeat.<owner_token>` in the SEPARATE `OWNER_PRESENCE` bucket — v5), and stale entries are **compacted out
  of the epoch during the next registrant's own CAS write** (no third-party `kv.Delete`, which is not CAS-safe —
  `natsclient/kv.go:314`); native **NATS KV bucket-level TTL on the `OWNER_PRESENCE` bucket** is
  the server-enforced liveness backstop (the arbitrated epoch key itself is compaction-maintained,
  not TTL'd), and a **post-registration Watch** re-triggers overlap if a compacted-then-revived
  owner reappears.
- **BLOCKING-B — v3's inverse-gate binds at the WRONG seam; it never sees sensorml's
  edge.** v3's gate was a check on `ForeignEdgeClaim` *registration* — but the T2-regroup
  foreign-routing path (`ingestEntity`/`partitionTriplesBySubject`,
  `component.go:917-955,1031-1040`) routes ANY foreign-subject triple from ANY Graphable
  with **NO claim lookup** (`AddTriples`, `component.go:950`). sensorml — the FLAGSHIP
  motivating case — emits its `isHostedBy` child-subject edge
  (`parser/sensorml/graphable.go:122-125`) and **registers no claim**, so v3's
  registration-gate is bypassed entirely. **Closed (Decision 4 rewritten):** the
  **T2-regroup foreign-routing path becomes the ENFORCEMENT seam** — a Graphable that
  emits a foreign-subject triple MUST have declared that predicate as a
  `ForeignEdgeClaim` (validated at boot the same way payload registration is); a
  foreign-subject triple with no registered claim is the reject (or a flagged,
  deprecated-on-arrival migration escape hatch). sensorml declares ONLY `isHostedBy` as a
  `ForeignEdgeClaim` — the child-subject edge; `hosts` carries the parent's OWN id as Subject
  (`Subject == entityID`), so it files as `own` and is NOT a foreign edge (corrected in
  Decision 4, 056:872). v4 also (1) **enumerates `ensureReferencedEntityExists`
  (`component.go:1424-1525`) as a FOURTH auto-vivify path** that survives ADR-055's flip
  (the flip deletes only the `AddTriple`/`AddTriples` branches, `:1691-1698`/`:1790-1796`)
  and rules on its fate; (2) **strikes the physically-impossible "single-revision-atomic
  drain with birth"** (ENTITY_STATES + PENDING_EDGES are two buckets, no cross-bucket
  txn) and replaces it with **delete-after-apply + boot-time re-drain sweep**, made a
  HARD/TESTABLE gate via a counting crash-recovery test; and (3) states **Window B**
  (born-without-edges) honestly and routes edge-presence-sensitive consumers to Strict
  mode.

**Three MEDIUMs folded:** (1) a **cross-type collision check** — an `OwnerClaim` in
replace/CAS mode whose predicate is ALSO a `ForeignEdgeClaim` over an overlapping
entity-ID pattern FAILS registration (the 2-type split closed FE×FE false-positives but
lost the Owner×FE true-positive); (2) the **inverse-predicate count is corrected** to
"the 6 hierarchy predicates (`vocabulary/hierarchy.go:25-64`) AND the 2 delegation
predicates (`vocabulary/agentic/register.go:162,168`) — none of which is sensorml's
`hosts`/`isHostedBy` pair"; (3) **waiver expiry** is a next-registration-boundary
review-obligation forcing function (boot/redeploy), NOT a runtime kill of an in-flight
writer — distinct from the heartbeat/TTL *claim*-staleness machinery, which IS runtime.

### v2 → v3: the two BLOCKING holes closed, and the Codex folds

The v2 four-lens re-review found **two BLOCKING holes**, one in each net-new crux
mechanism. Both forks are now RESOLVED by the product owner and implemented in the
Decision (not re-litigated, not parked):

- **BLOCKING-A (Decision 2): the overlap check was per-process in-memory, so it could
  NOT see another process's claim.** The motivating cross-process collision
  (semspec's `execution_store.go:496-502`, two binaries) would **not** be caught, and
  the `CoordinationWaiver` was an unverified in-process annotation. **Closed by making
  owner claims live in a shared NATS KV bucket (`OWNER_CLAIMS`).** Every process reads
  ALL registered claims from the bucket at startup, so a cross-process overlap *is* in
  the bucket and *is* detected. The registration race (two processes claiming
  overlapping cells at once) is resolved by CAS. **(v4 NOTE: v3's specific key scheme —
  one record per `owner_id`, CAS-on-`Create` — was found broken for the CROSS-owner case
  by the v3 gate; CAS only conflicts on the SAME key, so two different owners write two
  different keys and never compare. v4 supersedes it with the single epoch key — see the
  v3→v4 entry above and the rewritten Decision 2.)** Stale claims (crash/redeploy) are
  bounded by claim-record staleness (heartbeat + TTL). This is the KV-twofer applied to
  ownership: **the claim write IS the registration event AND the audit history.**
- **BLOCKING-B (Decision 4): "Backfill is the durability backstop" was FALSE.** Backfill
  re-derives the inverse edge via `GetInversePredicate` (`vocabulary/registry.go:368-381`),
  but sensorml's `PredHosts`/`PredIsHostedBy` (`parser/sensorml/predicates.go:96-97`)
  are registered `WithIRI` only — **no `WithInverseOf`** — so `GetInversePredicate`
  returns `""` and the sweep reconstructs **nothing**. With an in-memory pending buffer,
  an OOM restart silently loses the edge with no metric. **Closed by three real
  mechanisms:** (1) the pending-edge buffer is **KV-backed** (`PENDING_EDGES` bucket,
  keyed by target EntityID, drained on the target's birth — survives restart); (2) a
  **registration-time inverse-gate** — a foreign-edge predicate may use **Conditional**
  mode ONLY if it has a registered `InverseOf`; otherwise registration FAILS and the
  predicate must declare **Strict** (fail-loud, producer guarantees origin-first); and
  (3) sensorml's `PredHosts`/`PredIsHostedBy` get `WithInverseOf` wired (change
  described, not implemented) so the edge becomes genuinely Backfill-recoverable.

Folded **Codex refinements**: (P1) the foreign-subject edge is a distinct
`ForeignEdgeClaim` (relationship-producer claim), separate from the owned-state
`OwnerClaim`, so the registry does not conflate two different things; (P1) the
`CoordinationWaiver` is now **structured** (`owner`, `with`, `predicates`, `reason`,
`expiry`/`review_by`, `ref`) so a "temporary" cross-process overlap carries an expiry
and a review obligation and cannot silently become permanent — this also resolves the
deferred waiver-audit OQ; (P2) Decision 5 sharpened with the explicit invariant *"a
product bucket may be authoritative ONLY for data that is NOT also authoritative state
in `ENTITY_STATES`"*; (P2) **cryptographic provenance is RESERVED, not implemented** —
owner registration is a **provenance + write-semantics** contract (an authorization MODEL whose
runtime enforcement is **observe-only on the current tag** — overlaps and unclaimed foreign edges
are metered/logged, not rejected; the hard reject + owner-token write lease are later increments,
so this is NOT yet an enforced authorization gate), and it is NOT cryptographic proof of authorship
(today's envelope is unauthenticated — `message/base_message.go:158-180`, a SHA256 of type+payload
with no signing); the signing follow-up is scoped in ADR-057. (Don't overclaim security: per the
semconnect review, label this provenance + write-semantics discipline, not authorization-enforced.)

Folded **re-review HIGHs**: predicate sets are **exact-string enumeration only — no
prefix/namespace/glob on predicates** (wildcards apply to the entity-ID pattern only);
the `coordinator.decision.*` existence-proof fires only **after** the Wave-1 mode
reclassification (today both writers go through `writeBatch`/append, which is
append-evidence and exempt); Decision 5 leads with a **build-time CI lint over call
sites** as the enforcement and recasts the startup audit as a runtime observe-only
rejection-metric; and the **append-evidence ↔ replace-owned migration-window hazard**
(a predicate flipping mode while old binaries run) is named and mitigated.

Folded **code-accuracy fixes**: hierarchy inverse-edge citations corrected to
`graph/inference/hierarchy.go:313,368` (not `processor/graph-ingest/`); and the
`edgesFailed` metric covers the **sibling**-inverse drop (`hierarchy.go:319`) but NOT
the **container**-inverse drop (`hierarchy.go:368-374` is Warn-only, no metric) — the
container inverse edge is *even less observable*, which strengthens the "silent drop is
a correctness bug" argument. The v2 child-link claim (`056:376-378`) is **corrected**:
`ChildSpec.LinkPredicate` is NOT a racing foreign edge — the parent writes it on the
parent subject (`workflow.go:120-140`: `LinkPredicate` carries the child's EntityID as
the *Object* on the *parent*), which already exists; it is removed from the foreign-edge
enumeration.

### v1 → v2: what the adversarial review changed

The v1 draft led with "exactly one semantic owner per **entity**," then admitted in its
own "biggest risk" section that hybrids make the real unit "owner per *predicate set*"
and **parked that as an open risk.** That was the central flaw: the headline claim was
false on the flagship control-plane entity (the loop-execution entity has ~8 distinct
writer sources — below), and the design's load-bearing unit was filed under "Open
Questions." v2 closes that gap. **The ownership unit is the predicate GROUP throughout
the Decision.** The five things v1 left open — ownership granularity, overlap detection,
schema-backed reconcile, foreign-edge birth, and product-bucket enforcement — are
**Decisions here, not Open Questions.** Open Questions now hold only genuinely-deferrable
items.

## Context

### The reframe, in one sentence

`ENTITY_STATES` (`graph/constants.go:6`) began as a **fact materialization surface** and
has quietly become — for a growing class of entities — the framework's **authoritative
semantic state store**. The framework never named the second responsibility out loud, so
every consumer that needs it hand-rolls it, and the framework keeps patching the symptoms
one producer at a time.

### Two contracts on one bucket (the load-bearing fact)

There are two distinct write contracts on `ENTITY_STATES`, with opposite merge semantics.

**Contract 1 — Fact arrival (append).** A registered payload implements `graph.Graphable`
(`graph/graphable.go:54-60`: `EntityID()` + `Triples()`), is published as a `BaseMessage`,
flows through a JetStream stream, and graph-ingest's consumer extracts it
(`extractEntityFromMessage`, `component.go:963-1020`, stamping `MessageType` provenance at
`component.go:992`) and merges it. On an existing entity the merge is a **blind append**:

```go
// processor/graph-ingest/component.go:1270
existing.Triples = append(existing.Triples, entity.Triples...)
```

Facts accumulate. Two arrivals of the same predicate coexist. Correct for the data model
it was built for — observations, telemetry, evidence — where every arrival is a new fact
and history is the point (the KV-twofer: `docs/concepts/02-kv-twofer.md`).

**Contract 2 — Authoritative state (replace + CAS).** ADR-049's lifecycle `Manager`
(`pkg/lifecycle/manager.go`) writes the *same bucket* with completely different semantics:
it declares a predicate set (the `Workflow.Schema` struct, `workflow.go:61-65`), writes
through `update_with_triples`, does **replace-by-predicate** not append (`manager.go:492-506`
builds `RemoveTriples` for every predicate it writes), and does **CAS-on-condition** via
`ExpectedRevision` (`manager.go:505`). The merge it relies on is `graph.MergeTriples`
(`graph/helpers.go:101-134`) — **replace-by-(subject,predicate)**, the opposite of
`MergeEntity`'s append.

ADR-049's own `manager.go:484-491` documents why append is a *correctness bug* for state:

> Replace, don't append: every field Transition writes (phase, audit, mutator-changed
> scalars) is single-valued... Without this, transitions ACCUMULATE phase triples;
> extractTripleScalar reads last-match (so the Manager stays correct) but the rule engine's
> GetFieldValue reads first-match → it sees the stale initial phase and phase guards never
> re-fire.

The difference between the two contracts is the difference between a fact and a state
transition. **ADR-049 named the mechanism (`Manager`); it never named the responsibility
(authoritative state) — nor enforced who may discharge it.**

### Granularity: the unit is the predicate group, not the entity (proven on the code)

The v1 "one owner per entity" claim is **false on the flagship control-plane entity.** The
loop-execution entity `{org}.{platform}.agent.agentic-loop.execution.{loopID}` is written by
**~8 distinct sources** plus two framework writers, none of which owns the whole entity:

| Writer source | Predicate group it writes | Anchor |
|---|---|---|
| agentic-loop graph_writer | spawn identity, completion/failure/cancellation outcome, lineage | `graph_writer.go:269-323,474-496,561-577` |
| synthetic-decide | `coordinator.decision.*` (no-terminal-tool fallback) | `graph_writer.go:191-199` |
| coordinator decide tool | `coordinator.decision.*` (the real decision) | `decide.go:362-370` |
| write_todos tool | `agent.todo.*` (REPLACE — RemoveByPredicate + AddTriplesBatch) | `write_todos.go:235-247` |
| scratchpad tool | scratchpad predicates | `scratchpad.go:217` |
| ops emit_diagnosis | diagnosis back-links | `emit_diagnosis.go:199` |
| web-search / http-request | web-observation back-links | `websearch.go:259`, `httprequest.go:270` |
| rule-engine run-anchor | `agent.run.*` anchor | `actions.go:1212` |
| lifecycle Manager (when loop is a Participant) | phase + audit | `manager.go:492-506` |

Two facts kill "owner per entity":

1. A **non-owner** component already does owned-state REPLACE on this entity:
   `write_todos.go:235-247` clears every `agent.todo.*` predicate (`RemoveByPredicate` loop)
   then writes the new set (`AddTriplesBatch`). It owns the *todo predicate group* on an
   entity it does not otherwise own. This is the predicate-group unit, in production, today.

2. Two **different components** write the **same predicates** on the **same entity** with
   **no framework arbitration** — the `coordinator.decision.*` collision (worked existence-
   proof below).

mission and the github-pr-workflow example confirm the same shape: the control-plane class
is dominated by **hybrids** — some predicate groups are owned single-valued state, some
accumulate as facts, on one entity. The ADR-055 per-predicate matrix
(`graphable-fix-plan.md:91-118`) already classifies *per predicate* for exactly this reason.
**So the contract is predicate-group-granular or it is wrong.**

### The worked existence-proof: the `coordinator.decision.*` collision

Two different components write the **same two predicates** onto the **same loop-execution
entity**, with no framework arbitration:

- The **decide tool** (`processor/agentic-tools/decide.go:362-370`) writes
  `agvocab.CoordinatorNextAction` and `agvocab.CoordinatorDecisionReason` onto
  `loopEntityID`, `Source = decideToolSource`.
- **synthetic-decide** (`processor/agentic-loop/graph_writer.go:191-199`) writes the **same**
  `agvocab.CoordinatorNextAction` and `agvocab.CoordinatorDecisionReason` onto the **same**
  `loopEntityID`, `Source = syntheticDecideSource`.

These are two components, both legitimately claiming the `coordinator.decision.*` predicate
group on the same entity pattern. Today nothing detects this; both writes land and the
later-arriving merge silently wins (or both coexist as appended duplicates on the fact path).
This is the *exact* failure the ownership contract must catch at **registration time**, not
at runtime. **Decision 2 below specifies how it is resolved** (a coordination contract: one
owning component, a distinct synthetic predicate, or a declared overlap waiver) and the
registration FAILS otherwise. This is the migration that demonstrates overlap-rejection
working.

The same defect class is **already live in semspec, cross-process**:
`execution_store.go:496-502` documents two separate processes (`execution-manager` and
`requirement-executor`) writing overlapping predicates (Type, Slug, Phase, TraceID,
NodeCount, ErrorReason) to the same hashed subject, coexisting *only* because each
`UpsertEntityIfChanged` "only removes its own OwnedPredicates." That is CQRS-by-accident
with no arbiter — the symptom this ADR's registration check exists to make unrepresentable.

### ADR-055 is patching symptoms around the unnamed responsibility

The ADR-055 audit (`docs/proposals/graphable-bypass-audit.md`) found 21 confirmed call
sites where the graph-ingest *mutation API* is used as a **producer API** — conjuring
first-class entities with no backing `Graphable` type and often no semantic envelope. The
leak mechanism is auto-vivify: `AddTriple` (`component.go:1669-1714`) and `AddTriples`
(`component.go:1733-1826`) both synthesize a bare entity from a triple subject with **no
`MessageType`, version 0, no envelope**:

```go
// processor/graph-ingest/component.go:1691-1698 (AddTriple)
} else {
    entity = graph.EntityState{ID: triple.Subject, Version: 0, UpdatedAt: time.Now()}
}
```

(Same shape at `component.go:1790-1796` for `AddTriples`. The `1411-1423`/`1511-1521` line
numbers in older `CLAUDE.md`/`MEMORY.md` notes are **stale** for the `AddTriple`/`AddTriples`
auto-vivify — re-anchor those on 1691-1698 and 1790-1796. But the `1424-1525` range is NOT
nothing: it is `ensureReferencedEntityExists`, the **fourth** auto-vivify path — an
envelope-less stub `Put` that ADR-055's two-branch flip does NOT touch. Decision 4 enumerates
and rules on it.)

ADR-055 already corrected its own framing from "everything must be Graphable" to the right
invariant (`graphable-fix-plan.md:21-36`): *no write may create an entity without a semantic
envelope.* But ADR-055 is still framed around **write lanes** (a mechanism). The four lanes
are four answers to one question this ADR names: *who owns this entity's predicate groups,
and is this write a fact, a birth, a transition, or evidence?*

### The live proof the gap is real: semspec's dual-write

semspec composes `pkg/lifecycle` **zero times** in non-test code and hand-rolls every
state-ownership concern. It runs a **3-layer manager pattern**: an in-memory cache, a
product-owned authoritative bucket `PLAN_STATES` (`plan_store.go:20-37`), AND a *mirror
projection* into `ENTITY_STATES`. `planStore.save` (`plan_store.go:267-301`) writes all
three in order — the textbook dual-write. To stop the projection corrupting on every save
(graph-ingest's append accumulates duplicates), semspec reinvented the missing primitive:
`UpsertEntityIfChanged` (`workflow/graphutil/triple.go:235-270`) plus a hand-maintained
`OwnedPredicates` list per writer. Its rationale comment names the gap exactly
(`triple.go:565-603`):

> WHY this exists: semstreams beta.90 changed graph.ingest.entity's handler from
> CreateEntity (full-replace Put) to MergeEntity, which does a raw append... the rule engine
> reads first-match while lifecycle reads last-match, so the divergence is a silent
> correctness bug.

That is the *same* corruption ADR-049's `manager.go:484-491` documents — discovered and
fixed independently in two projects because the framework never shipped the
replace-owned-predicates primitive as surface. There are **7 production `OwnedPredicates`
declarations** (`plan_store.go:449`, `execution_store.go:464` & `:540` (in
`processor/execution-manager/`), `plan_requirement.go:249`, `plan_decision.go:82`,
`plan_capability.go:96`, `plan_scenario.go:96`).

### Why naming the responsibility (not the mechanism) is the fix

`Graphable`, the mutation API, and `Manager` are mechanisms. Centering the design on any of
them makes every new producer a fresh special case (the ADR-055 per-producer matrix and the
semspec per-component `*_store.go` files). The thing all of them are *for* is one
responsibility — and the responsibility's unit is the **predicate group**:

> For a given entity, each owned **predicate group** has exactly one writer responsible for
> its current value, written with replace-owned / preserve-foreign semantics, observed by
> rules and indexes, recovered correctly on restart. The responsibility is real whether the
> predicate group arrives as a fact, is born by a manager, or transitions under CAS.

## Decision

The contract is **predicate-group-granular end to end.** The headline unit is the claim
`(entity pattern, predicate set, write mode, owner id)` — never "this component owns this
entity."

### Decision 1 — Ownership unit = the predicate GROUP (the claim), in TWO claim types

> **A state-ownership claim is a tuple `(entity-ID pattern, predicate set, write mode, owner
> id)`.** It asserts: "for entities matching this pattern, this owner is responsible for the
> current value of exactly these predicates, written in this mode." An entity may carry
> several claims from several owners (it is a hybrid); it may also carry un-claimed
> predicates (facts/evidence) that no owner reconciles.

`write mode` ∈ {`replace-owned` (single-valued, re-emitted), `cas-transition`
(`ExpectedRevision`, phase/RMW), `append-evidence` (multi-valued, must-exist, no owner)}.
This is the ADR-055 per-predicate matrix (`graphable-fix-plan.md:97-105`) promoted to the
ownership primitive. The lifecycle `Workflow.Schema` (`workflow.go:61-65`) is *already* a
claim of this shape: its tagged predicate set + `EntityIDPattern` + replace/CAS mode is one
owner's claim over one predicate group. The contract generalizes that one claim shape to
non-state-machine owners.

**The registry holds TWO distinct claim types (Codex P1 — do not conflate them):**

- **`OwnerClaim` — owned current state.** The subject of every write is an entity *this
  owner owns the predicate group on*. Governs `replace-owned` and `cas-transition` writes
  to `ENTITY_STATES`. This is the claim Decisions 2/3/5 arbitrate. The loop-graph_writer's
  identity/outcome/lineage group and the `write_todos` tool's `agent.todo.*` group are
  `OwnerClaim`s.
- **`ForeignEdgeClaim` — a relationship-producer claim.** A foreign-subject edge is NOT
  "owned current state": its Subject is a *different* entity than the one the producer is
  ingesting/transitioning (the inverse hierarchy edge targets the sibling/container; the
  T2-regroup edge targets the foreign entity). The producer is asserting a *relationship
  onto another owner's entity*, not owning that entity's state. This claim carries an
  `edge-mode` (Conditional / Backfill / Strict — Decision 4) and a *single edge predicate*,
  and it is governed by Decision 4's inverse-gate, NOT by Decision 2's overlap check. Keeping
  it a separate type stops the registry from treating a cross-subject relationship write as
  an owned-state claim (which it is not) and stops two foreign-edge producers from being
  flagged as "overlapping owners" when they are legitimately both adding edges.

A producer may hold both: an `OwnerClaim` over the entity it owns AND a `ForeignEdgeClaim`
for an edge it writes onto someone else's entity. The two are arbitrated by different rules.

**This replaces "one owner per entity" everywhere.** An entity has owners *of predicate
groups*. The loop-execution entity has: the loop-graph_writer owning the
identity/outcome/lineage group (replace), the write_todos tool owning the `agent.todo.*`
group (replace, `write_todos.go:235-247`), a `coordinator.decision.*` owner (resolved in
Decision 2), the lifecycle Manager owning phase+audit when the loop is a Participant (CAS),
and un-claimed evidence predicates (back-links from web/ops tools — append, no owner).

### Decision 2 — Cross-process overlap rejection via a SINGLE-EPOCH-KEY claim registry (load-bearing)

> **Owner claims live in a SINGLE epoch key (the bare `_registry` key in the `OWNER_CLAIMS`
> bucket) holding the UNION
> of every registered owner's claims, advanced under `UpdateWithRetry` CAS. At every
> process startup, the registrant reads the epoch, computes the overlap of its OWN
> candidate claims against EVERY other owner's claims already in the epoch, and if two
> claims select an overlapping `(entity-ID pattern, predicate)` cell in `replace-owned` or
> `cas-transition` mode, registration FAILS** with `ErrOwnershipOverlap`, naming both
> owners, the overlapping pattern, and the overlapping predicates. No silent coexistence —
> across the process boundary, not just within one process, because every registrant of
> ANY claim serializes through ONE key.

**Why a single epoch key, not a key-per-owner (this is BLOCKING-A's fix — and why v3's
per-owner key was broken).** The collision the fix exists to catch is two *different*
owners — semspec's `execution-manager` and `requirement-executor`
(`execution_store.go:496-502`), two separate binaries — booting together and claiming
overlapping predicates. v3 keyed `OWNER_CLAIMS` as one record per `owner_id` and relied on
CAS-on-`Create` (`natsclient/kv.go:235,240`) as the race-breaker. **That does not work for
the cross-owner case:** CAS conflicts only on the SAME key, so two different owners writing
two different `owner_id` keys both `Create` successfully and their claims are NEVER
compared against each other — the overlap is undetected. CAS-per-owner-key serializes only
a *same-owner re-register* (a redeploy of one binary), not two distinct owners. The fix is
to put the **overlap-arbitrated state in ONE epoch key** so that EVERY registrant — of any
owner — passes through the same CAS-serialized read-modify-write and is compared against
the full union. This is still the KV-twofer applied to ownership: the epoch write IS the
registration event AND the audit history (`docs/concepts/02-kv-twofer.md`); per-owner
*identity* and liveness still live in per-owner entries *inside* the epoch value.

**Bucket + key layout (BLOCKING fix — valid NATS KV keys + a real TTL split).** NATS KV keys
permit only alphanumerics + `-` `_` `=` `.` — **no `/`**, and **no `>`/`*`** (the subject-wildcard
tokens). v4's `OWNER_CLAIMS/_registry` and `OWNER_CLAIMS/heartbeat/<owner_id>` were therefore
**invalid keys** (and `owner_id` may itself contain `.`/space/`*`/`>`). The whole layout uses
valid keys and a two-bucket TTL split, because `natsclient.KVStore` exposes **no per-key TTL** —
`Create`/`Put` take only `(ctx, key, value)` (`natsclient/kv.go:95,112`), with no
`KeyTTL`/`LimitMarkerTTL` passthrough to NATS's `KeyValuePutOpt`. Extending the wrapper for a
per-key TTL is one option; the simpler, zero-wrapper-change option is chosen — **presence keys
live in their own bucket with a bucket-level TTL, the epoch lives alone in a non-TTL bucket** (a
bucket TTL on the epoch bucket would also age out the durable `_registry` epoch, which must never
silently vanish between deploys):

- **Bucket `OWNER_CLAIMS` (no TTL):** the durable claim registry. Framework-owned, created at
  graph-ingest boot like `ENTITY_STATES`.
- **The one arbitrated key:** `_registry` (a bare, valid key — no slash) — a single epoch record
  holding the UNION of every owner's claims plus every active `CoordinationWaiver`. **This is the
  key the overlap check reads and the registration CAS-writes.** All cross-owner overlap is decided
  here, so two different owners cannot land on different keys and dodge comparison.
- **Value (`RegistryEpoch`):** `{ epoch_revision (the KV revision, implicit), owners:
  map[owner_id]OwnerEntry, waivers: [CoordinationWaiver] }`, where
  `OwnerEntry = { owner_id, owner_token, claims: [{pattern, predicates[], mode}], process_instance_id,
  registered_at, heartbeat_at, ttl_hint }`. The canonical free-form `owner_id` lives HERE in the
  value (for display/query); `owner_token = governance.RuleToken(owner_id)`
  (`governance/verdict.go:114-119` — a deterministic 64-bit FNV-1a hex, the framework's existing
  subject/key-safe encoding for free-form ids that can contain the token separator `.`, spaces, or
  the wildcards `*`/`>`) is the subject-safe handle used in the presence-key name and in the
  write-time lease check (below). `predicates` is an **exact-string list** (see below).
  `process_instance_id` distinguishes a fresh process from a stale prior incarnation of the same
  `owner_id`; `heartbeat_at` is the per-owner liveness signal (see staleness, below). The whole
  union being one value is bounded — claims are boot-time and small (tens-to-low-hundreds across a
  deployment), well under the 1MB `MaxValueSize` (`natsclient/kv.go:39`); if a deployment ever
  outgrew that, the staleness compaction (below) bounds it and a sharded-epoch-by-pattern-prefix
  variant is the deferred optimization (OQ).

  > **Implementation note (PR-1, 2026-06-17) — the shipped lease-token format supersedes the
  > `owner_token` + `process_instance_id` sketch above.** The OwnerToken write-lease (workstream #2)
  > shipped its wire field as a single string `"<owner>#<incarnation>"`, NOT the `RuleToken(owner_id)`
  > FNV hash plus a separate `process_instance_id`. Rationale: (1) the shipped substrate already keys
  > and compares owners by their **raw canonical identity** (`Registry.OwnerOf` returns the exact
  > `owner_id` — "no hash"; `pkg/ownership/registry.go`), so a hash handle would be a lossy second
  > encoding of an id the code already treats as exact; (2) the per-process incarnation is a
  > `crypto/rand` boot nonce stored on each `OwnerClaim` at `RegisterOwner` time and folded into the
  > token itself (`<owner>#<incarnation>`) rather than carried as a sibling `process_instance_id`
  > field — one wire value, one comparison. The two-state wire contract is: **empty token = unowned /
  > legacy writer, the lease check skips it; `"<owner>#<incarnation>"` = compare against the live
  > owner+incarnation** (the comparison/reject is a later increment). This note governs the lease
  > TOKEN only; the presence-key naming (`heartbeat.<...>`) and the key-safety question for free-form
  > owner ids are a SEPARATE, pre-existing concern, unchanged by PR-1.
- **Bucket `OWNER_PRESENCE` (bucket-level TTL):** the per-owner liveness keys, SEPARATE from the
  epoch precisely so the TTL applies to presence WITHOUT endangering the durable epoch. Each owner
  bumps a valid, dot-segmented presence key `heartbeat.<owner_token>` on an interval (a plain `Put`,
  last-writer-wins — heartbeat is not arbitrated state, only a freshness timestamp). The bucket's
  TTL is the **server-enforced** liveness floor: a live owner re-bumps within the TTL; a dead
  owner's presence key **ages out automatically**, so "is this owner live?" has a server floor
  independent of whether any registrant ever runs compaction. The epoch's `heartbeat_at` is
  refreshed from a registrant's own presence write on each CAS pass; keeping the high-frequency
  heartbeat OFF the arbitrated epoch key means heartbeats do not churn the epoch revision and force
  spurious overlap-recheck retries on every other registrant.

**The startup registration sequence (read-check-merge-CAS-retry — `Manager.Register` writes
through this).** `Manager.Register` and every non-state-machine `RegisterOwner` call funnel
into one epoch-CAS routine, built on `UpdateWithRetry` (`natsclient/kv.go:172`), whose
callback runs the check-merge atomically against the just-read epoch value:

1. **Read** the `_registry` epoch value (in `OWNER_CLAIMS`) at its current revision (the
   `UpdateWithRetry` callback receives `current []byte`; a missing key is the empty epoch,
   revision 0).
2. **Compact** stale entries out of the read epoch (any `OwnerEntry` whose `heartbeat_at` is
   older than `ttl_hint + grace` AND whose `process_instance_id` differs from a live presence
   key — see staleness, below) and drop any **expired** waivers. Compaction happens *inside*
   the callback so it rides the same CAS write — no separate `kv.Delete` (which is not
   CAS-safe, `natsclient/kv.go:314`, and would race a revived owner's heartbeat).
3. **Check overlap** of THIS owner's candidate claims against the union of every OTHER
   (non-stale) owner's claims in the compacted epoch (owning modes only), honoring any
   matching unexpired waiver. **Overlap, no waiver →** the callback returns a
   *non-retryable* `ErrOwnershipOverlap` (boot fails, naming both owners + the exact cells).
   **Cross-type overlap (Owner×FE) →** same failure (MEDIUM fix, below).
4. **Merge** this owner's `OwnerEntry` (its claims + fresh `heartbeat_at` + new
   `process_instance_id`) into the compacted epoch's `owners` map and return the new epoch
   value.
5. **CAS-write** at the read revision. `UpdateWithRetry` does this as a `kv.Update` at the
   read revision (or `kv.Create` rev-0 for the first-ever registrant) and **retries on a
   revision mismatch** (`IsKVConflictError`, `natsclient/kv.go:240,262`).

**Why the retry catches the concurrent-boot cross-owner overlap (the BLOCKING-A walk).** Two
fresh processes — `execution-manager` (E) and `requirement-executor` (R) — boot at the same
instant and both read epoch revision *r* (neither sees the other yet). Both compute "no
overlap" against *r* and both attempt the CAS write at *r*. **Exactly one wins** (say E):
its `kv.Update` lands, the epoch advances to *r+1* now containing E's claims. R's CAS write
at *r* **fails with a revision mismatch** — `UpdateWithRetry` re-invokes the callback, which
**re-reads the now-larger epoch *r+1* that CONTAINS E's claims** and re-runs step 3 against
it. Now R's claims ARE compared against E's, the overlapping `(pattern, predicate)` cell is
found, and R fails boot with `ErrOwnershipOverlap`. The cross-owner overlap v3's per-owner
key silently admitted is now **detected by construction**, because every registrant of any
owner serializes through the one epoch key and re-checks against whatever the winner merged.
(A same-owner redeploy is the degenerate case: it overwrites its own `OwnerEntry` in the
epoch under the same CAS, with a new `process_instance_id`, and does not overlap itself.)

**Stale-claim lifecycle (crash / redeploy — a dead process's claim must not block a
restart — reconciled with the single epoch key).** A claim is bound by **heartbeat-staleness
+ TTL**, evicted via **compaction on the next registrant's CAS write**, never via an
unconditional third-party delete:

- The live process **heartbeats** its `heartbeat.<owner_token>` presence key (in the
  `OWNER_PRESENCE` bucket) on an
  interval. An `OwnerEntry` whose `heartbeat_at` (and matching presence key) has not advanced
  for longer than `ttl_hint + grace`, AND whose `process_instance_id` differs from any live
  presence, is **stale**.
- Stale entries are **compacted OUT of the epoch by the next registrant during its CAS write**
  (step 2 above) — riding the same atomic read-modify-write, so reaping never needs a separate
  `kv.Delete` (not CAS-safe — `natsclient/kv.go:314` — and would race a revived owner's
  heartbeat). A crashed `requirement-executor`'s old entry is compacted by the restarted one
  (different `process_instance_id`), so the dead claim does not block the live boot.
- **NATS KV bucket-level TTL is the hard backstop — applied to the `OWNER_PRESENCE` bucket, not
  the epoch bucket.** Because `natsclient.KVStore` has no per-key TTL (`natsclient/kv.go:95,112`),
  the TTL is a property of the SEPARATE `OWNER_PRESENCE` bucket holding the `heartbeat.<owner_token>`
  presence keys: a live owner re-bumps its presence within the TTL, a dead owner's presence key
  **ages out automatically**, so "is this owner live?" has a server-enforced floor independent of
  whether any registrant ever runs compaction. (The arbitrated `_registry` epoch key lives in the
  NON-TTL `OWNER_CLAIMS` bucket — it is the durable registry and must not silently vanish between
  deploys; it is kept current by compaction-on-CAS, and its stale *entries* are reaped using the
  `OWNER_PRESENCE` TTL as the liveness oracle. If a presence key has aged out and the entry is older
  than `ttl_hint + grace` with a different `process_instance_id`, the entry is stale.) Heartbeat
  compaction is the *fast, portable* reaper; the bucket TTL is the *server-guaranteed* liveness
  floor it consults. The two-bucket split is what lets the TTL backstop exist at all without a
  `natsclient` wrapper change.
- **`ttl_hint ≥ 3×max(boot_time, gc_pause_budget)` (named assumption).** The staleness window
  must exceed the longest benign no-heartbeat gap — a slow boot or a long GC pause — by a
  comfortable margin, or a *live* owner mid-pause is falsely evicted. Three-times the larger of
  the two budgets is the floor; the concrete value is implementation tuning (OQ).
- **Post-registration Watch closes the false-eviction window (safety) — and a re-detected overlap
  is a FATAL HALT, not a log.** After it registers, every owner **Watches** the `_registry` epoch
  key. If a slow/paused owner's entry was compacted by another registrant during the owner's pause
  and the owner then revives and resumes heartbeating, the Watch fires on the next epoch change and
  the revived owner **re-runs the overlap check** — so a heartbeat false-eviction cannot silently
  reintroduce an uncompared overlap (a second writer could have claimed the freed cell in the
  interim; the revived owner now sees it). **The revived owner's re-check is FAIL-LOUD-AND-HALT: on
  a re-detected overlap it terminates the revived process** (a fatal halt, the same `ErrOwnershipOverlap`
  outcome as a boot-time overlap), NOT merely a logged warning — a process that has lost its claim to
  a competitor must stop writing the contested predicate group, not keep running beside it. A
  `owner_claims_stale_evicted_total{owner}` metric fires on every compaction, so a flapping owner is
  observable, not silently de-registered.
- **Write-time owner-lease check closes the double-WRITE window (safety, not just double-register).**
  The Watch re-check above fires *eventually* (bounded by Watch latency); during
  `[entry compacted, owner revives + Watch halt]` a freed cell could be re-claimed by a second owner
  AND the stale-evicted (still-running, not-yet-halted) owner could keep WRITING the predicate group
  — a brief DOUBLE-WRITE, not just a double-registration. To close the write window rather than only
  bound it, **every authoritative (`replace-owned`/`cas-transition`) write carries the writer's
  `owner_token` and graph-ingest verifies it against the current epoch's owner of that
  `(pattern, predicate)` cell** before applying the reconcile: a stale-evicted owner whose cell has
  been re-claimed **fails the write at the write seam** (a cheap epoch read, cache-friendly), not
  only at its next registration or Watch fire. This is the chosen close — a lightweight write-time
  lease verified against the epoch — over merely declaring the window an accepted availability cost.
- **Where the `owner_token` lives — the WIRE CONTRACT, not prose (P1 close).** The token is a
  new typed field `OwnerToken string` on the graph-ingest mutation request structs —
  `UpdateEntityWithTriplesRequest` and `CreateEntityWithTriplesRequest`
  (`graph/mutation_requests.go:80,38`), the SAME point-to-point request/reply structs that already
  carry `ExpectedRevision`, `IndexingProfile`, and `TraceID`. It is **NOT a NATS header and NOT a
  `BaseMessage` envelope field**: graph mutation request/reply is intentionally registry-exempt
  (`graph/mutation_requests.go:7-10`) because the subject already selects the handler, so the
  authorization context rides as a typed request field exactly like the CAS condition
  (`ExpectedRevision`) does — no envelope, no header indirection, one struct the handler already
  deserializes. The token's VALUE is the lease handle minted at `RegisterOwner` time (the owner's
  entry in the `OWNER_CLAIMS` epoch for that `(pattern, predicate)` cell). graph-ingest's
  `update_with_triples` / `create_with_triples` handler reads the current epoch, looks up the live
  owner of the target cell, and rejects with a new `ErrorCodeOwnerLeaseStale` when the request's
  `OwnerToken` does not match — surfaced through the **same response-classification path**
  `ErrorCodeRevisionMismatch` already uses (`pkg/lifecycle/graph_emit.go:134-139`), so the lifecycle
  `Manager` (which already emits through these exact subjects — `graph_emit.go:12-15`) gets the
  check for free. Append-evidence (`triple.add`/`add_batch`) and unowned writes carry no token and
  skip the lookup — only `replace-owned` / `cas-transition` writes (Decision 1's two owning modes)
  are gated. This is the enforcement point the reviewer flagged: the lease check is now a wire field
  with a named owner, a named handler seam, and a named error code, not a sentence.
- **The availability-vs-safety tradeoff, stated.** Compaction prioritizes *availability* (a
  crashed owner's claim must not block a restart forever) at the cost of a bounded *registration*
  window: during `[entry compacted, owner revives + Watch halt]` a freed cell could be re-claimed
  and briefly double-OWNED in the registry. The window is bounded by the Watch latency, the Watch
  re-check halts one of the two owners loudly, AND the write-time lease check above prevents the
  stale owner from actually double-WRITING the predicate group in the interim. We accept a brief,
  observable, self-correcting double-CLAIM (closed at the write seam, halted at the Watch) over a
  permanent boot-block on a dead owner's stale claim — the same liveness-over-strict-safety call
  NATS KV TTLs make, now with the write path closed rather than merely bounded.

**Predicate sets are EXACT-STRING enumeration only (re-review HIGH).** `predicates` is a list
of full predicate strings; the overlap check is **exact-string set intersection** over those
predicates. There is **no prefix / namespace / glob on predicates** — wildcards apply to the
**entity-ID pattern only** (the 6-part glob). A claim cannot register `coordinator.decision.*`
as a predicate namespace; it enumerates `{CoordinatorNextAction, CoordinatorDecisionReason}`
explicitly. Without this rule the *first* namespace-registration attempt would silently
false-negative (a glob predicate would fail to intersect a sibling glob, or over-match), and
the check would ship feeling-like-coverage while catching nothing — the exact failure class
MEMORY.md warns about. Exact-string is verifiable and total.

**Corollary — aliases are read-compatibility, NOT equal write/ownership keys (semconnect review).**
Because ownership claims AND indexes are exact-predicate driven, two predicates that mean the same
thing semantically (e.g. `rdf.type` ↔ `sensorml.process.type`) are NOT interchangeable here. A read
path may resolve both, but a producer MUST write the canonical framework constant — an alias is a
*read*-compatibility shim, never an equal write contract or an equal ownership key. Owning `rdf.type`
does not own `sensorml.process.type`; writing the alias bypasses the claim that names the constant.

**Append-evidence is exempt.** Multi-valued evidence predicates have no single owner by
design; many writers may append (web/ops back-links). The overlap check covers only the two
*owning* modes (`replace-owned`, `cas-transition`) where a second replace/CAS writer silently
clobbers.

**The only legal overlap is an explicit, STRUCTURED coordination contract (Codex P1).** A
v2 waiver was a bare `{with, reason}` that could bless ANY overlap PERMANENTLY. v3 requires:

> ```text
> CoordinationWaiver {
>   owner:      <owner_id>            // who is asking for the overlap
>   with:       <other_owner_id>      // the owner it overlaps
>   predicates: [<exact predicate>…]  // EXACTLY which cells are waived (not "all")
>   reason:     <string>              // why the overlap is safe (external serialization)
>   expiry:     <RFC3339> | review_by:<date>  // REQUIRED — a waiver cannot be permanent
>   ref:        <issue/ADR id>        // the tracked obligation to remove it
> }
> ```

The registry permits an overlap ONLY if BOTH overlapping owners carry a matching, **unexpired**
waiver naming each other and the exact overlapping predicates. Waivers live in the epoch's
`waivers` list (the `_registry` key in `OWNER_CLAIMS`) alongside the claims, so they are queryable
and operator-visible by construction (this **resolves the deferred waiver-audit OQ** — folded into
Decision 2, not deferred).

**Waiver expiry is a registration-boundary forcing function, NOT a runtime kill (MEDIUM fix).**
An expired waiver fails the overlap check **at the next registration boundary** (boot or
redeploy of either overlapping owner) — at which point the now-uncovered overlap re-becomes
`ErrOwnershipOverlap` and the deploy fails until the operator renews the waiver or removes the
overlap. Expiry does **not** runtime-kill an already-running, in-flight overlapping writer
mid-stream — there is no live "your waiver just lapsed, halt your writes" interrupt. This is
deliberate: expiry is a *review obligation* that bites on the next deploy (you cannot ship a
new binary while carrying a stale, unreviewed overlap), so a "temporary" cross-process overlap
(semspec's legit case) cannot silently become permanent across the deploy lifecycle. **This is
distinct from the heartbeat/TTL machinery above**, which IS runtime: claim-staleness compaction
reaps *dead owners'* claims continuously; waiver-expiry reviews *live owners'* deliberate
overlaps at deploy time. Two different clocks, two different jobs.

**Cross-type collision: an `OwnerClaim` may not silently strip a `ForeignEdgeClaim` (MEDIUM
fix).** The Decision-1 two-type split (OwnerClaim vs ForeignEdgeClaim) closed the FE×FE
false-positive — two foreign-edge producers legitimately adding edges to one entity are no
longer flagged as overlapping owners. But it opened an Owner×FE **true-positive** gap: an
`OwnerClaim` in `replace-owned` or `cas-transition` mode reconciles its *whole owned predicate
group* (Decision 3 schema-derived removal), so if that owned set includes a predicate `P` that
some OTHER owner has registered as a `ForeignEdgeClaim` over an overlapping entity-ID pattern,
the owner's reconcile would **silently strip the foreign edge** on its next write (the foreign
predicate looks like a "dropped owned predicate" to the remove-set). **Fix:** the overlap check
ALSO intersects each candidate `OwnerClaim`'s predicate set (in owning modes) against every
registered `ForeignEdgeClaim`'s edge predicate over an overlapping entity-ID pattern; a hit
**FAILS registration** (or requires a structured waiver naming the Owner and the FE owner). So
the registry now catches both FE×FE (correctly *allows*) and Owner×FE (correctly *rejects*) —
the two-type split no longer loses the true-positive it was never meant to.

**The `coordinator.decision.*` collision — resolved, AND when the check actually fires
(re-review HIGH).** Today decide-tool (`decide.go:362-370`) and synthetic-decide
(`graph_writer.go:191-199`) both write `{CoordinatorNextAction, CoordinatorDecisionReason}`
onto the loop-execution entity. **Crucially, both currently go through the batch/append path
(`AddTriplesBatch`-style, `write_todos.go`'s `RemoveByPredicate`+batch shape is the replace
exemplar; the decision writes are append today) — i.e. `append-evidence`, which is EXEMPT.**
So Decision 2's check does NOT fire on today's code: it fires only **after the ADR-055 Wave-1
mode reclassification** flips these two writes to `replace-owned`. No one should expect the
overlap to be caught on current `main`; it is caught post-migration, which is precisely when
the silent-clobber hazard becomes real. The three legal resolutions, in preference order:

- **(a) One owning component.** Pull both writes behind a single `coordinator-decision`
  owner that decides whether the value is real or synthetic — the cleanest, since the two
  are mutually exclusive per loop (synthetic fires *only* when no terminal tool ran). One
  `OwnerClaim`, no overlap.
- **(b) Distinct predicate for the synthetic path.** synthetic-decide writes
  `coordinator.decision.synthetic_next_action` (it already stamps a
  `CoordinatorDecisionSynthetic="true"` discriminator, `graph_writer.go:208-215`); rules
  that must treat them uniformly read both. Two disjoint claims, no overlap.
- **(c) A structured `CoordinationWaiver`** between the two owners, justified by the per-loop
  mutual exclusion, with an `expiry` and a `ref`. Permitted, recorded, but the weakest
  because the serialization is a runtime invariant the registry cannot verify — the review
  should prefer (a).

The recommended resolution is **(a)** (one owner) for semstreams; the migration that lands it
(after the Wave-1 reclassification) is the demonstration that overlap-rejection works.
semspec's cross-process collision (`execution_store.go:496-502`) is the same problem one
process boundary out — there it needs a structured `CoordinationWaiver` (the two owners are
separate binaries that cannot be merged), making the waiver mechanism load-bearing for the
real cross-process case, not a loophole. And because both owners now register through the SAME
epoch key (`_registry` in `OWNER_CLAIMS`), the overlap is actually *seen* — each registrant is
compared against the other's claims via the read-check-merge-CAS-retry above — and the waiver
actually *gates* it, which neither the v2 in-memory map nor the v3 per-owner key could do.

### Decision 3 — Reconcile is SCHEMA-BACKED (registered owner schemas, not wire-level slices)

> **An owner declares its owned predicate set ONCE, at registration, as a schema. The
> framework derives the reconcile remove-set from the registered schema — the owner does not
> resend its `OwnedPredicates` on every write.** Wire-level `OwnedPredicates` on the update
> request is permitted only as a **transitional escape hatch** for owners not yet registered;
> it is SemSpec's emergency shape, not the framework shape, and it carries the delete-on-omit
> hazard (Decision 3a).

Mechanism:

- The registered claim (Decision 1) *is* the schema: `(pattern, predicate set, mode)`. At
  reconcile time the framework knows the owner's full owned predicate set from the registry,
  so an `update_with_triples` from a registered owner reconciles the **whole owned group**
  (replace what changed, **remove what the owner dropped**, leave foreign predicates
  untouched) without the caller enumerating the remove-set. This is exactly what
  `Manager.Transition` does today via the schema-derived delta (`manager.go:481-506`,
  remove-set built from `reg.meta`), generalized to non-state-machine owners.
- The lifecycle `Workflow.Schema` (`workflow.go:61-65`, reflected at `manager.go:120`) is the
  reference implementation of a registered owner schema. A non-state-machine owner registers
  the same shape minus the Transitions table (Decision 5).
- **Transitional escape hatch:** until an owner is registered, it may pass an explicit
  `OwnedPredicates []string` on `UpdateEntityWithTriplesRequest` (semspec's
  `triple.go:248-256` shape lifted to the framework). This is the migration on-ramp, marked
  deprecated-on-arrival; the registry-backed path is the destination. Carrying it lets
  semspec's 7 hand-rolled sites migrate one at a time without a flag day.

**Why schema-backed over wire-level is the framework shape.** Wire-level `OwnedPredicates`
on every request is flexible and error-prone: it is re-derived at every call site (7 sites in
semspec, each a place to forget a predicate), it cannot be checked for overlap (the registry
never sees it), and a partial send silently shrinks the owned set (Decision 3a). The
registered schema is declared once, is the input to Decision 2's overlap check, and makes the
remove-set a framework computation rather than a caller obligation.

#### Decision 3a — `OwnedPredicates` removal is delete-on-omit; nil = no removal

The owned-set removal is the mirror of `preserveStoredEntityMetadata`'s preserve-when-zero
(`mutations.go:548-561`) and carries the symmetric hazard: a partial send (a buggy or
partially-deployed owner that omits a predicate it actually owns) would **strip** that
predicate. Guard:

> **`OwnedPredicates == nil` means "do no owned-set removal" (today's behavior — only
> AddTriples/RemoveTriples apply). The set-difference removal fires ONLY on a non-nil
> `OwnedPredicates`.** A registered owner's schema is non-nil by construction, so registered
> reconcile always removes dropped predicates; an unregistered caller that sends nil keeps
> the safe append/explicit-remove behavior.

This makes partial-deploy safe: an old owner binary that doesn't know about the owned-set
field sends nil and cannot strip. It also bounds the blast radius of the escape hatch — a
wire-level caller must *opt in* to removal by sending a non-nil set.

### Decision 4 — Foreign-subject edges: gate-at-the-T2-seam + KV-backed pending buffer, and it GATES the ADR-055 flip

> **A foreign-subject edge — a triple whose Subject is a DIFFERENT entity than the one being
> ingested/transitioned (inverse hierarchy edges, T2-regroup cross-entity edges) — is a
> legitimate cross-entity write (a `ForeignEdgeClaim`, Decision 1) that may RACE the target's
> birth. It does NOT ride the owner-reconcile contract (it is not the writer's owned state),
> and it must NOT silently vanish under must-exist. The framework enforces this at the
> SHARED projection-normalization seam every graph-ingest write path passes through before the
> `ENTITY_STATES` mutation — fact-arrival Graphable ingestion AND the mutation API
> (`create_with_triples`/`update_with_triples`) — NOT at claim registration (which a no-claim
> producer bypasses) and NOT on the fact-arrival lane alone (which the real gateway consumers,
> writing via the mutation API, bypass): a foreign-subject triple with no registered
> `ForeignEdgeClaim` is rejected at the seam, on whichever lane it arrives. The edge survives a
> restart (KV-backed `PENDING_EDGES` buffer) and is reconstructable (the inverse-gate). ADR-055's
> closing-move flip (delete auto-vivify) is GATED on this contract shipping.**

> **Lane-independence correction (post-4a, the cited code wins — 056:34).** An earlier draft called
> the fact-arrival `ingestEntity`/T2-regroup path "the ONE seam every foreign edge already flows
> through." That is FALSE: it is the one seam every *Graphable* foreign edge flows through, but the
> framework's real consumers are GATEWAYS (semconnect cs-api `POST /systems`, semteams rules) that
> write through the **mutation API** (`graph.mutation.entity.create_with_triples` /
> `update_with_triples` / `triple.add`), which never reached `ingestEntity` and so were classified by
> nothing. A foreign edge sent via `create_with_triples` was silently MISfiled onto the request's
> primary `Entity.ID` (cs-api's `singleSubject()` guard, `systems_post.go:317`, is a *migration
> guard* for this missing support, not the desired architecture). The mutation API is a graph WRITE
> API, not a bypass; it must participate in the same projection/ownership/foreign-edge enforcement.
> The canonical seam is therefore **not `ingestEntity`** — it is the **shared projection-
> normalization step** (`normalizeProjection`, `processor/graph-ingest/component.go`) that
> partitions a projected triple set against its primary subject and classifies the foreign-subject
> edges, called from EVERY write path that accepts projected triples (fact-arrival ingestEntity,
> `create_with_triples`, `update_with_triples`; bare `triple.add` is a direct write to
> `Triple.Subject`, ownership-checked per (subject,predicate), and is NOT treated as a foreign-edge
> producer unless the request grows explicit origin/projection context). 4a's observe-only
> classification reruns inside this shared seam (lane-independent). Once mutation-lane normalization
> exists, cs-api drops `singleSubject` for registered foreign-edge projections — its blocked
> SensorML-hierarchy round-trip is the immediate consumer, making the "no producer" reading a
> fact-arrival-only artefact, not a real absence of consumers.**

The problem must-exist creates: today these edges auto-vivify the target if it is absent. Two
live sources:

- **T2-regroup foreign-routing** (`ingestEntity`, `component.go:949-955`): when a Graphable
  carries triples whose Subject names another entity, graph-ingest routes them via
  `AddTriples` (append-by-subject) onto the foreign entity — **best-effort, Warn-only on
  failure.** Under must-exist, if the foreign entity isn't born yet, the edge is dropped. The
  flagship case is sensorml: the parser emits BOTH directions at parse time
  (`parser/sensorml/graphable.go:122-125`) — `PredHosts` on the parent subject AND
  `PredIsHostedBy` on the **child** subject — and the child-subject `isHostedBy` edge is the
  foreign edge T2-regroup routes (its Subject `childID` ≠ the ingested parent `a.entityID`).
- **HierarchyInference inverse/sibling edges** (`graph/inference/hierarchy.go:313`
  sibling-inverse, `:368` container-inverse — note: `graph/inference/`, NOT
  `processor/graph-ingest/`): the inverse edge targets a *different* subject (siblingID /
  containerID) than the entity being ingested, written via the in-process `AddTriple`. The
  **sibling**-inverse drop is Warn + `edgesFailed` metric (`hierarchy.go:314-319`), but the
  **container**-inverse drop is **Warn-only, with NO metric** (`hierarchy.go:368-374`) — the
  container inverse edge is *even less observable* than the sibling one. ADR-055 #267 cleared
  these as "graceful degradation." **This ADR reframes that clearance: a silent inverse-edge
  DROP is a correctness question (the graph loses a traversal edge), not a free degradation —
  and the least-observable of the two (container) has no metric at all, which strengthens the
  case that this is a correctness bug, not a degradation knob.**
  - **4c-pre-3 update (code-traced):** the container-inverse drop now increments `edgesFailed`
    for parity with the sibling drop (`hierarchy.go`). The trace also clarified its NATURE: the
    container is materialised by `ensureContainerExists` *before* the inverse write, so an absent
    target is never the cause — this drop is a CAS/storage failure, NOT a must-exist/absent-target
    drop, and the closing-move flip does not touch this in-process `AddTriple` path. The
    observability gap is closed; the "correctness drop" framing applies to the foreign-edge lane
    (the `PENDING_EDGES`/Conditional story), not to this container-inverse counter.

**The enforcement seam is the SHARED projection-normalization step, not claim-registration
(BLOCKING-B fix part 1 — the core bypass) and not one lane (the lane-independence correction
above).** v3 placed the inverse-gate on `ForeignEdgeClaim` REGISTRATION. But registration is not on
the path a raw foreign triple takes: every graph-ingest write path that accepts projected triples
calls `normalizeProjection` (`processor/graph-ingest/component.go`), which classifies **any** triple
whose `Subject != primaryID` as foreign by pure string comparison, then `routeForeignEdges` routes
the whole foreign batch via `AddTriples` — **with no claim lookup gating it.** A producer that never
registers a `ForeignEdgeClaim` flows straight through. sensorml is exactly this on the fact-arrival
lane: it emits `{Subject: childID, Predicate: PredIsHostedBy, Object: parentID}`
(`parser/sensorml/graphable.go:122-125`) as part of the *parent's* Graphable, registers **no** claim,
and its `isHostedBy` edge is routed at the seam — so v3's registration-time gate **never sees it.**
And cs-api emits the same edge on the MUTATION lane (`create_with_triples`), which v3's gate — and
4a's fact-arrival-only hook — also never saw. The flagship motivating edge bypassed the flagship fix,
on every lane.

**Fix: make the SHARED projection-normalization step THE enforcement seam — `normalizeProjection`,
called from `ingestEntity`, `create_with_triples`, AND `update_with_triples` (bare `triple.add`
stays a direct ownership-checked write to its subject).** That step is exactly where a foreign edge
is *recognized as foreign* on any lane, so it is exactly where the claim contract must bind:

> **A producer that emits a foreign-subject triple MUST have declared that predicate as a
> `ForeignEdgeClaim`, validated at boot the same way payload registration is. At the shared
> projection-normalization seam — on whichever lane the write arrives (fact-arrival Graphable
> ingestion OR the mutation API) — a foreign-subject triple whose `(message_type, predicate)` has
> NO registered `ForeignEdgeClaim` covering the producer is the REJECT** — counted on a
> `foreign_edge_unclaimed_total{message_type,predicate}` metric, dropped-loud, never silently
> routed.

**Producer identity at the seam IS the registered `MessageType` (P1 — the reject must be able to
fire).** The reject keys on the producer, but the seam only has what reached it. `ingestEntity`
(`component.go:908`) receives a bare `*graph.EntityState`; `extractEntityFromMessage`
(`component.go:963-1019`) has already reduced payload identity to `entity.MessageType = msg.Type()`
(`component.go:992`) plus StorageRef + IndexingProfile — the concrete Graphable Go type and the
rest of `BaseMessage` are GONE by the time `partitionTriplesBySubject` runs. So "producer identity"
for the gate is defined as the **registered `MessageType`** (the payload-registry key, already a
field on `EntityState`), and a `ForeignEdgeClaim` is **keyed to that `MessageType`** (the same
discriminator payload registration already uses). This identity is **already carried into the seam**
— `partitionTriplesBySubject`/`AddTriples` run *inside* `ingestEntity`, which holds
`entity.MessageType` — so no new plumbing is needed; the reject reads `entity.MessageType` directly
and looks up a `ForeignEdgeClaim` registered under it. Without this definition the reject has
nothing to key on and cannot fire; with it, the reject and its `foreign_edge_unclaimed_total{message_type,predicate}`
metric are mechanically realizable at the seam.

The reject has a transitional escape hatch mirroring Decision 3's shape: until a producer
registers its foreign-edge predicates, an **unclaimed foreign triple is routed under a
`deprecated-on-arrival` flag** (the same metric fires, plus a one-time WARN per
`(producer,predicate)`) so the migration is observable and bounded, not a flag-day. Once a
producer's predicates are claimed, the unclaimed path becomes the hard reject. Boot-time
validation (payload-registration-style) means a producer that declares a `ForeignEdgeClaim`
gets it checked at startup (the inverse-gate below); a producer that emits foreign triples
without a claim trips the runtime metric until it migrates.

**sensorml declares `isHostedBy` as a `ForeignEdgeClaim` — NOT `hosts` (P1 correction).** Only the
child-subject edge is foreign. At parse time sensorml emits BOTH directions
(`parser/sensorml/graphable.go:122-125`): `{Subject: a.entityID, Predicate: PredHosts, Object: childID}`
(`:123`) and `{Subject: childID, Predicate: PredIsHostedBy, Object: a.entityID}` (`:124`).
`a.entityID` is the parent's OWN id (`Asset.EntityID()` `:37`), so **`hosts` is the source entity's
own triple — `Subject == entityID`, it stays on the primary, `partitionTriplesBySubject`
(`component.go:1033`) files it as `own`, it is NOT a foreign edge and NOT a `ForeignEdgeClaim`.**
`isHostedBy` has `Subject = childID != a.entityID`, so it is the ONLY foreign-subject edge here and
the ONLY edge T2-regroup routes; it is the single `ForeignEdgeClaim` sensorml registers (with an
`edge-mode` — see the no-birth subclass ruling in the fourth-path section, which makes the sensorml
child a referential-stub-lane target, not a Conditional pending-edge target, because the child has
no independent birth). This is described, not implemented; the point is the seam now *requires* the
declaration for the one genuinely foreign edge, and does NOT mis-declare the parent's own `hosts`
triple as foreign.

**Why v2's "Backfill is the durability backstop" was FALSE (BLOCKING-B's root).** Backfill
(mode 2 below) re-derives the inverse edge from the forward edge via `GetInversePredicate`
(`vocabulary/registry.go:368-381`). But the inverse is reconstructable **only if the
predicate has a registered inverse.** The flagship foreign edge — sensorml's
`PredHosts`/`PredIsHostedBy` — is registered `WithIRI` ONLY, with **no `WithInverseOf`**
(`parser/sensorml/predicates.go:96-97`). A complete registry sweep confirms only **eight**
predicates carry an inverse: the 6 hierarchy predicates (`vocabulary/hierarchy.go:25,32,41,48,57,64`
— `HierarchyTypeSibling` at `:71` is `WithSymmetric` instead, its own inverse) AND the 2
delegation predicates (`DelegationFrom`/`DelegationTo`, `vocabulary/agentic/register.go:162,168`)
— **none of which is sensorml's `hosts`/`isHostedBy` pair.** So
`GetInversePredicate("sensorml.system.hosts")` returns
`""` and the sweep reconstructs **nothing** — even though the constant's own doc-comment
*claims* `PredIsHostedBy` "is the inverse of PredHosts" (`parser/sensorml/predicates.go:29-30`,
a comment the registration never honored). With an in-memory pending buffer, an OOM restart
between enqueue and drain loses the edge with **no metric and no backstop**. Both legs are
broken; this ADR fixes both (KV-backed pending buffer + the registration-time inverse-gate).

**The edge-birth contract — three modes, owner declares which the edge predicate uses.**

1. **Conditional (default for inference-derived inverse edges).** The edge write succeeds iff
   the target exists; if absent, the edge is **enqueued** against the target's birth, not
   dropped. **The pending-edge buffer is KV-backed (BLOCKING-B fix part 2):** a `PENDING_EDGES`
   bucket keyed by **target EntityID**, value = the list of pending foreign triples awaiting
   that target. When graph-ingest later births the target (Fact-arrival or Entity-create), it
   **drains the target's key via delete-after-apply** (apply the edges to the now-born target,
   confirm durable, THEN delete the pending key — NOT a single cross-bucket revision, which is
   impossible; see the atomicity strike below). The KV backing means an OOM restart between
   enqueue and drain does NOT lose the edge — it is durable in `PENDING_EDGES` until drained,
   and the boot-time re-drain sweep re-applies any key whose target already exists. Bounded
   (max pending per target, max buffer age) with a `pending_edges_dropped_total{reason}` metric
   when a bound is hit — observable, never silent.
2. **Backfill (a real backstop ONLY when the inverse is registered — BLOCKING-B fix part 3).**
   The edge is written conditionally now; a periodic reconcile sweep re-derives missing inverse
   edges from forward edges via `GetInversePredicate`. This is a genuine recovery path **only
   if the predicate has a registered `InverseOf`** — otherwise the sweep reconstructs nothing
   (the false-backstop that broke v2). The **registration-time inverse-gate** enforces this:
   *a `ForeignEdgeClaim` may declare `Conditional` mode ONLY if its edge predicate has a
   registered `InverseOf` (`GetInversePredicate` != "" or the predicate is symmetric); if it
   does not, registration FAILS and the predicate must declare `Strict`.* So "Conditional"
   now *structurally implies* "Backfill is a real backstop." No predicate can sit in the
   silent-drop gap.
3. **Strict (for edges whose target MUST pre-exist by causal ordering).** The edge is
   must-exist and fails loudly if the target is absent — used where the producer guarantees
   origin-first ordering (e.g. the example-fan-out parent-counter stamp, ADR-055 §3
   subject-override case, where the parent is spawned before the child completes). A
   foreign-edge predicate with no registered inverse and no KV-durable producer guarantee
   lands HERE by the gate, not in a silent Conditional drop.

**BLOCKING-B fix part 4 — wire `WithInverseOf` for sensorml (landed in W0 4a).**
`parser/sensorml/predicates.go:96-97` registered `PredHosts`/`PredIsHostedBy` with `WithIRI`
only. The change: add `WithInverseOf(PredIsHostedBy)` to the `PredHosts` registration and
`WithInverseOf(PredHosts)` to the `PredIsHostedBy` registration (mirroring the 6 hierarchy
predicates at `vocabulary/hierarchy.go:25-64` and the 2 delegation predicates at
`vocabulary/agentic/register.go:162,168`, the only existing inverse-bearing predicates). Then
`GetInversePredicate("sensorml.system.hosts") == "sensorml.component.isHostedBy"` and vice
versa, so sensorml's foreign `isHostedBy` edge becomes genuinely Backfill-recoverable.

**The registered inverse makes `Conditional` LEGAL for this edge — it does NOT make it the
edge's mode (contradiction resolved).** An earlier draft said the edge "passes the inverse-gate
*as Conditional*," which read as a mode recommendation and contradicted the fourth-path ruling
above (the sensorml child has no independent birth, so its correct mode is **`NoBirthStub`** —
lane ii — which `requiresInverse() == false` and passes the gate trivially, with or without the
inverse). The `WithInverseOf` change is about gate *eligibility* (Conditional/Backfill become
permissible should the edge ever be re-classified) and about the **Backfill recoverability
floor**, not about declaring the no-birth child Conditional. (It also honors the doc-comment at
`predicates.go:29-30` that already *asserted* the inverse the registration omitted; sensorml
emits both directions explicitly at parse time, `graphable.go:122-125`, so the registered inverse
is the correct semantic backstop if the explicit child-subject write ever races.)

**Fix part 5 — the sharpened flip-gate predicate (hatch-empty, not merely wired).** ADR-055's
closing move (delete both auto-vivify branches, flip `triple.add`/`add_batch` to must-exist) must
not land until *"the Conditional path exists"* — and this ADR makes that predicate **precise and
verifiable**:

> **"The Conditional path exists"** ≡ ALL of:
> 1. the pending-edge buffer is **KV-durable** (`PENDING_EDGES`, survives restart);
> 2. **every** `Conditional` foreign-edge predicate has a **registered inverse** (enforced by
>    the inverse-gate, so this is true by construction at boot or boot fails);
> 3. the drain is **crash-safe by idempotent re-run** via a **de-dupe merge**
>    (replace-by-(subject,predicate,object), NOT blind `AddTriples` — see the exactly-once gate
>    below) plus delete-after-apply ordering and a boot-time re-drain sweep; and
> 4. a **counting crash-recovery test** is green that asserts **EXACTLY ONE** edge after
>    enqueue → crash before drain → restart (not merely "present" — see the exactly-once gate
>    below); and the **escape hatch is EMPTY** — `foreign_edge_unclaimed_total` reads **zero over
>    a bake window**, i.e. every live foreign-edge producer (sensorml included) has migrated off
>    the deprecated unclaimed-routing hatch onto a registered `ForeignEdgeClaim`. This is the GATE
>    — a passing exactly-one test AND a drained hatch, not "the reject is wired" and not a
>    "stated and accepted" sentence.
>
> If any of (1)–(4) is not met, the ADR-055 flip does NOT land. Hard precondition on
> ADR-055's closing-move ordering, added here. **The part-4 change from "the reject is wired" to
> "`foreign_edge_unclaimed_total` is zero over a bake window" is deliberate: a wired-but-still-firing
> reject means a producer is still on the hatch, and flipping must-exist then would hard-break that
> producer's foreign edges. The gate is hatch-EMPTY, not hatch-EXISTS.**

**Fix part 6 — the bare-triple-lane (product rule-pack) flip precondition (gh#278, semteams).** The
hatch-empty gate above covers the FOREIGN-EDGE lane (the T2-seam classifier). It does NOT cover the
OTHER producer whose writes the must-exist flip touches: product rule packs whose triple actions write
bare `graph.mutation.triple.add` onto framework-born entities (loop-execution, plan-loop). A bare
`triple.add` never enters the foreign-edge classifier (Lane-independence correction, 056:34), so
`foreign_edge_unclaimed_total` is the WRONG signal for it — that counter reads zero by structure while
the flip would still hard-break a rule pack whose target anchor is not yet born-first. The precondition
for THIS lane is:

> For every product rule pack that stamps a framework-born entity via `triple.add`:
> 1. the target anchor's OWN birth lane is migrated to a born-first path (e.g. loop-execution via
>    4c-pre-1's `create_with_triples` birth), AND
> 2. `mutation_rejections_total{subject="graph.mutation.triple.add", reason="entity_not_found"}`
>    reads **zero over a bake window** (the existing metric, `processor/graph-ingest/component.go:111-126`
>    — `reason` is the `MutationResponse` ErrorCode; post-flip a `triple.add` to an absent entity
>    surfaces as `reason=entity_not_found`), AND
> 3. a targeted product e2e proves every marker's anchor entity EXISTS before the marker write fires.
>
> NOT `unregistered_authoritative_write_total` (a Decision-5 enforcement counter that does not exist
> today); the live signal is `mutation_rejections_total{reason=entity_not_found}`.

The flip is declared safe only when BOTH the foreign-edge gate (parts 1–4) AND this bare-triple-lane
precondition hold — never from the framework producers' view alone.

**Why "single-revision-atomic drain with birth" is STRUCK (BLOCKING-B fix, the atomicity
strike).** v3's gate clause (3) required the pending edges to apply *in the same revision that
births the target*. That is **physically impossible**: `ENTITY_STATES` and `PENDING_EDGES` are
two separate KV buckets, and NATS JetStream KV has **no cross-bucket transaction**. The birth
write is one `UpdateWithRetry` callback on the target's key in `ENTITY_STATES`
(`component.go:1246-1288`) — it returns ONE value for ONE key; it cannot also, atomically,
delete a key in `PENDING_EDGES`. Any design that claims "single-revision-atomic with birth"
across the two buckets is wrong on the storage model. The disjunct is removed.

**Replaced with crash-safe-by-idempotent-re-run (no cross-bucket txn needed):**

- **Delete-after-apply ordering, applied via a DE-DUPE MERGE — not blind `AddTriples` (P1/P2 — the
  idempotency the crash-safety argument rests on must be REAL).** On the target's birth, graph-ingest
  (1) reads the target's `PENDING_EDGES` key, (2) applies the pending foreign edges to their subjects
  via a **de-dupe merge that replaces-by-(subject,predicate,object)** (idempotent: re-applying an
  already-present edge is a no-op), (3) confirms those applies are durable, and ONLY THEN (4) deletes
  the `PENDING_EDGES` key. **The drain MUST NOT use `AddTriples` for this.** `AddTriples`
  (`component.go:1733`) appends **blindly** — `entity.Triples = append(entity.Triples, group...)`
  (`component.go:1798`), no (subject,predicate,object) de-dupe — so a crash-before-(4) followed by a
  re-drain through `AddTriples` would write the SAME edge TWICE, duplicating it. The v4 phrasing
  ("benign double-apply ... idempotent on (subject,predicate,object)") was FALSE against the actual
  `AddTriples` path; the fix is to make the drain genuinely idempotent by construction with the
  de-dupe merge, so the boot-sweep re-run is a true no-op rather than a duplicate. The only crash
  outcomes are then: crash before (4) → the pending key survives and the edges are re-applied on the
  next drain (the boot sweep, below) — **a true no-op** (the (s,p,o) edge already present is not
  re-appended); crash after (4) → the edges are durably applied and the key is gone — done. There is
  **no "consumed-but-lost" outcome** and **no "applied-twice" outcome.** Lost-edge is unacceptable;
  duplicate-edge is now also unrepresentable (not merely "acceptable").
- **The exactly-one crash-recovery gate (the literal flip-gate test).** The flip-gate's part-4
  counting crash-recovery test asserts **EXACTLY ONE** edge after enqueue → crash-before-drain →
  restart, NOT merely "the edge is present." This is the assertion that catches the blind-`AddTriples`
  duplicate: a drain that re-appended on re-run would show TWO edges and FAIL the gate; only a drain
  that replaces-by-(s,p,o) shows exactly one and passes. The exactly-one count IS the gate — it is the
  reason the de-dupe merge is mandatory and not an optimization.
- **Boot-time `PENDING_EDGES` re-drain sweep.** On graph-ingest start, before serving, it lists
  `PENDING_EDGES` and, for every pending key whose **target already exists** in `ENTITY_STATES`,
  re-applies and deletes (delete-after-apply again). This recovers any edge whose target was
  born but whose drain crashed mid-flight, and any edge enqueued against a target that was born
  concurrently (Window B, below). The sweep is the idempotent-re-run that the crash-safety
  argument rests on.
- **Backfill is the inverse-derivable floor.** For Conditional predicates (which, by the
  inverse-gate, all have a registered inverse), the periodic Backfill sweep
  (`GetInversePredicate`, `vocabulary/registry.go:368-381`) re-derives any still-missing inverse
  edge from its forward edge — a third, slowest recovery layer beneath drain-on-birth and the
  boot sweep.

**Window B — born-without-edges, stated honestly.** Because the two buckets cannot be written
atomically, there is a real window: between the target's **birth revision** and the **drain
revision** (drain-on-birth or boot-sweep), a reader that Gets the target sees it WITHOUT its
pending foreign edges. The window is bounded by drain latency (drain-on-birth fires in the same
ingest handler immediately after the birth merge, so it is short on the happy path; the boot
sweep bounds the crash path) and is fully recovered by Backfill as the floor. **The edge is
never lost — but for a bounded window it may be not-yet-visible.** Consequence for consumers:

- A consumer that branches on **edge PRESENCE** (a rule firing on "does target T have an
  inbound `isHostedBy`?") MUST NOT use Conditional — it must require the producer to use
  **Strict** mode (origin-first ordering, must-exist), so the edge is present at the target's
  first observable revision or the write fails loud. Conditional is for edges where eventual
  presence is correct and a bounded born-without-edge window is acceptable (graph traversal
  completeness, not a real-time branch condition).
- This is the same availability-vs-safety call as Decision 2's staleness: Conditional buys
  birth-race tolerance at the cost of a bounded visibility window; Strict buys immediate
  presence at the cost of requiring the producer to guarantee ordering. The mode is the
  consumer-sensitivity knob, declared per `ForeignEdgeClaim`.

**The FOURTH auto-vivify path: `ensureReferencedEntityExists` (BLOCKING-B fix, the
fourth-path ruling).** ADR-055's closing move is framed as "delete BOTH auto-vivify branches"
— but it names only `AddTriple` (`component.go:1691-1698`) and `AddTriples`
(`component.go:1790-1796`). There is a **fourth ownerless-birth path that survives that flip**:
`ensureReferencedEntityExists` (`component.go:1424-1525`) runs **unconditionally at the tail of
BOTH `MergeEntity` (`component.go:1311`) AND `createEntity` (`component.go:1411`)**. It walks the
entity's `IsRelationship()` triples and, for every referenced target that does not yet exist,
**`Put`s a stub** (`component.go:1488-1519`) — `{core.identity.stub:true,
core.identity.referenced_by:<src>}`, Version 1, no `MessageType`, **no semantic envelope**, via
plain `Put` (last-writer-wins, not even CAS). This is a fourth conjure-an-ownerless-entity path,
and deleting the two `AddTriple`/`AddTriples` branches does **nothing** to it — every relationship
triple that flows through the *Fact-arrival* birth lane still auto-vivifies its targets here.
ADR-055's "no ownerless births" closing move is **incomplete** until it accounts for this path.

> **Ruling — the fold preference INVERTS on whether the target has a future owner-birth (P1
> correction). Two subclasses, two defaults:**
>
> **(i) Target HAS a future owner-birth → fold into the `PENDING_EDGES` buffer (enqueue, don't
> `Put`).** When the referenced target will later be born by its OWN envelope-bearing producer
> (a loop-execution entity, a mission, a plan — anything independently published), the stub `Put`
> is strictly worse than the pending-edge buffer: it conjures an envelope-less, ownerless
> `core.identity.stub` that the target's real birth then has to reconcile against. Replace the
> stub `Put` with: enqueue the relationship edge against the target's EntityID in `PENDING_EDGES`
> (Conditional semantics, de-dupe-merge drain). When the target is born by its real producer, the
> drain applies the edge — born ONCE, by its owner, with a proper envelope, never as an ownerless
> stub.
>
> **(ii) Target has NO independent producer → keep the EXPLICIT referential-stub lane with a
> must-exist EXEMPTION (this is the DEFAULT for the no-birth subclass, not a grudging fallback).**
> Some referenced targets are NEVER independently published and so have **no birth to drain
> against** — enqueuing their edge into `PENDING_EDGES` would dangle the reference **forever** (a
> traversal regression strictly worse than today's stub). sensorml children are exactly this: the
> package defines only ONE `EntityID()` (`parser/sensorml/graphable.go:37`, `Asset` returns the
> PARENT id); a child id comes only from the optional `ChildIDFn` (`:166-175`) and is never the
> subject of its own published Graphable. For this subclass the stub IS load-bearing — it is the
> only thing that ever materializes the child node — so it stays, but it is **promoted from the
> envelope-less ownerless `Put` it is today (`component.go:1488-1519`: Version 1, no `MessageType`,
> no envelope, last-writer-wins) to a FIRST-CLASS, ENVELOPE-BEARING framework artifact with a named
> owner/claim.** This resolves the reviewer's first horn — we choose horn (a) (first-class stub),
> not horn (b) (remove the lane), because removal would dangle sensorml's child edge forever
> (no birth to drain, no registered inverse to backfill). Concretely, the referential-stub creator
> (`ensureReferencedEntityExists`) stamps the stub `EntityState` with a framework semantic envelope
> — `MessageType` from the framework stub family (`{Domain: core, Category: identity.stub, Version:
> v1}`, Key `core.identity.stub.v1`), the same discriminator shape the payload registry uses — and
> records the referencing producer in a `core.identity.stub_owner` triple alongside the existing
> `core.identity.referenced_by`. **Implementation precision (4b fold landed — the cited code wins,
> 056:34):** the original prose said `stub_owner` is "the producer's registered `ForeignEdgeClaim`
> `MessageType`," but that is NOT reachable at this seam — `ensureReferencedEntityExists` holds only
> the target id + the SOURCE `EntityState`'s `MessageType` (the entity that referenced it), never a
> `ForeignEdgeClaim`. So `stub_owner` records the reachable SOURCE `MessageType` (`referencedByType.Key()`);
> an untyped source (a gateway that does not stamp `Entity.MessageType`, e.g. cs-api today) attributes
> to the framework referential producer (`graph-ingest-referential-integrity`). The stub also stays
> **profile-less** so a real producer's later merge is still detected as the entity's true birth
> (`reconcileIndexingProfile` keys on profile-absence). The stub is therefore **NOT "an envelope-less
> ownerless stub that silently survives the flip"**; it is a NAMED framework birth lane (the
> referential-integrity producer) carrying a valid envelope, eligible for the **must-exist EXEMPTION**
> in ADR-055's flip text. The exemption is bounded to
> no-birth targets whose producer registered that `edge-mode` claim — declared, not a blanket
> carve-out: a foreign edge whose producer registered NO no-birth claim still trips the
> unclaimed-foreign reject (Decision 4 BLOCKING-B) and never reaches the stub lane.

How the seam decides which subclass a target is in: a referenced target whose `(message_type)`
maps to a registered birth-producer (it WILL be born) takes lane (i); a target with no registered
independent producer (sensorml children, and any future parse-time-only sub-entity) takes lane
(ii). The classification is the same registry the foreign-edge reject already consults, so it adds
no new lookup surface. **Either way, ADR-055's "delete both auto-vivify branches" closing move is
amended in 056's prose to cover the fourth path** — fold-into-pending for birth-bearing targets
(lane i), explicit-stub-with-exemption for no-birth targets (lane ii) — so the flip cannot ship
leaving a live UNACCOUNTED ownerless-birth path standing, and cannot ship dangling a no-birth
child's edge forever.

**Correction — the lifecycle child-link is NOT a foreign edge (v2 056:376-378 was wrong).**
v2 listed `ChildSpec.LinkPredicate` (`workflow.go:120-140`) as a Conditional foreign edge.
It is not: `LinkPredicate` carries the **child's EntityID as the Object** on the **parent
subject** (`workflow.go:120-140`) — `Manager.Children` enumerates triples *matching this
predicate on the parent*. The parent is the entity the Manager is writing/transitioning; it
already exists. There is no foreign subject and no birth race — the write lands on the
parent's own owned state (an `OwnerClaim` predicate, or a reference). It is **removed from the
foreign-edge enumeration.** (A child-link does the opposite of a racing foreign edge: it
points *from* an existing owner *to* a child by ID; the child's own birth carries the child's
state, not the parent's link.)

**Gate, restated.** ADR-055's closing move must not land until the four-part predicate above
holds — the **escape hatch is EMPTY** (`foreign_edge_unclaimed_total` reads ZERO over a bake
window, i.e. every live foreign-edge producer has migrated off the deprecated hatch onto a
registered `ForeignEdgeClaim` keyed by its `MessageType`; this is part 1, sharpened from "the
reject is wired" — a wired-but-still-firing reject means a producer is still on the hatch and the
flip would hard-break it), the buffer is KV-durable (part 2), every Conditional predicate has a
registered inverse (part 3), and the **counting crash-recovery test is green asserting EXACTLY ONE
edge** (via the de-dupe-merge drain, NOT blind `AddTriples` — `component.go:1798`) with
delete-after-apply + boot-sweep (part 4) — AND the **fourth auto-vivify path
(`ensureReferencedEntityExists`) is covered** (folded into the pending-edge buffer for birth-bearing
targets, or explicit-stub-with-must-exist-exemption for no-birth targets like sensorml children).
Otherwise the T2-regroup and HierarchyInference inverse edges silently lose graph structure on
every birth race, OR the flip ships leaving a live ownerless-birth path standing, OR it ships while
a producer is still on the hatch. Hard precondition on ADR-055's closing-move ordering, added here.

### Decision 5 — Product buckets: a framework owner registry, or honestly unenforced

> **A product-local bucket may be (a) a read cache of `ENTITY_STATES`, (b) a JetStream-style
> command/work queue (Facts-vs-Requests), or (c) a framework-derived projection — but NOT an
> independent authoritative store the product hand-mirrors into `ENTITY_STATES`. This is
> enforced by the OWNER REGISTRY: state that rules must observe is registered as a claim and
> lives in `ENTITY_STATES` as its home. Any predicate group an owner reconciles into
> `ENTITY_STATES` MUST be a registered claim; an unregistered owning write is the
> escape-hatch path (Decision 3) and is flagged.**

**The sharpened invariant (Codex P2):** *a product bucket may be authoritative ONLY for data
that is NOT also authoritative state in `ENTITY_STATES`.* A product is free to own a bucket of
data that has no `ENTITY_STATES` representation at all (a private index, a work queue, a
product-only cache key). What it may NOT do is keep a *second* authoritative copy of a
predicate group that is ALSO authoritative in `ENTITY_STATES` and reconcile both — that is the
dual-write this ADR exists to retire. This keeps the CQRS boundary from softening into "a
registered mirror is good enough": a registered mirror is still a mirror.

What the registry can and cannot enforce — stated honestly, **enforcement-first**:

- **Build-time CI lint over call sites (the primary enforcement).** `update_with_triples` is
  ALWAYS replace-mode by construction (`graph/mutation_requests.go:81-90`: `AddTriples` is
  replace-by-(subject,predicate), documented as upsert-not-append). So a CI lint can
  enumerate every `update_with_triples` / `UpdateEntityWithTriplesRequest` call site and flag
  any whose writer is not a registered owner of the predicates it sends — *at build time,
  before the binary ships.* This is feasible precisely because the lane's mode is fixed: the
  lint does not need to reason about runtime values, only call sites. This is the same
  call-site-lint discipline as the ADR-055 audit (`graphable-bypass-audit.md`).
- **Runtime observe-only rejection-metric (the backstop, NOT the same as the lint).** At
  runtime, an `update_with_triples` whose predicates are not covered by any registered claim
  in `OWNER_CLAIMS` emits an `unregistered_authoritative_write_total{owner,predicate}` metric
  — observe-only, does not reject the write (so a partial migration does not brick). This is
  the ADR-053/ADR-055 rejection-metric discipline: make the violation observable, not (yet)
  impossible. **The lint is the gate; the metric is the runtime canary** — do not conflate
  them (a green lint with a noisy metric means a writer slipped past the build-time check, e.g.
  via reflection or a code path the lint cannot see).
- **Registration/startup (Decisions 2 & 3).** Two owners cannot both claim the same predicate
  group (Decision 2, now cross-process via `OWNER_CLAIMS`). An owner's reconcile remove-set is
  schema-derived, not hand-rolled (Decision 3). These are real framework checks.
- **NOT enforced (named honestly):** the framework **cannot** stop a product from keeping a
  second bucket and writing it. `PLAN_STATES` (`plan_store.go:20-37`) is a separate
  jetstream.KeyValue the framework does not arbitrate. Per the P2 invariant, that bucket is
  legitimate ONLY for data not also authoritative in `ENTITY_STATES`; the framework cannot
  *force* that, but it CAN make the *mirror write* into `ENTITY_STATES` go through a registered
  claim (or trip the lint + metric), so the dual-write's `ENTITY_STATES` leg is owned and
  arbitrated. The product's private bucket stays the product's call — but it can no longer be a
  *second authoritative source into `ENTITY_STATES`* without registering as the owner (and then
  it cannot also be mirrored by a second unregistered writer, because that second writer fails
  Decision 2).

So Decision 5 is **partly a framework contract (the `ENTITY_STATES` leg is lint-gated +
registered or flagged) and partly honest policy (the product's own bucket is not the
framework's to forbid, but may not be a second authoritative copy of `ENTITY_STATES` state).**
The bucket-ownership rubric (ADR-049 §, `feedback_bucket_ownership_rubric`) remains the design
guidance; the registry + lint make the *authoritative-write* half enforceable.

#### The Manager embed-vs-parallel decision (how the registry is built)

The owner registry must accept **non-state-machine owners.** Today `Manager.Register`
(`manager.go:116-145`) is keyed by workflow name and `workflow.validate()` REQUIRES a
Transitions table, a PhasePredicate, and a Schema (`workflow.go:166-193`). A non-state-machine
owner (a model-endpoint config re-asserted per boot; a content-addressed web-observation
vertex) has no phases and cannot register without faking a transition graph.

**Decision: extract a base `OwnerRegistry` and have `lifecycle.Manager` EMBED it — not a
parallel type.** Rationale:

- A **parallel** registry (a second map keyed by MessageType, living beside `Manager`) would
  re-split the exact thing this ADR unifies: overlap detection (Decision 2) must see *all*
  claims — lifecycle and non-lifecycle — in one place, or two owners in different registries
  could claim the same predicate group undetected. A parallel registry reintroduces the
  cross-registry blind spot that is semspec's cross-process collision in framework form.
- The **base `OwnerRegistry`** holds claims `(pattern, predicate set, mode, owner id)`, **merges
  them into the single `_registry` epoch key (in `OWNER_CLAIMS`) under CAS (Decision 2),** and runs the
  cross-process overlap check over that epoch's union. Because lifecycle and non-lifecycle owners
  register *through the same base into the same epoch key*, the overlap check sees both — the
  embed (not parallel) decision is what guarantees the single shared claim view that BLOCKING-A's
  cross-process fix depends on (and why a per-owner key, which would let two owners write two keys
  and dodge comparison, is wrong).
  `Manager` registration becomes: build the `Workflow` claim (its Schema predicate set,
  replace/CAS modes, EntityIDPattern), register it *through the base*. The Transitions table,
  History, operator API, and phase projection are `Manager`-specific layers on top of a
  registered claim — they stay in `Manager`, they do not move into the base.
- A non-state-machine owner registers a claim through the **same base** with no Transitions
  table: `RegisterOwner(OwnerClaim{pattern, ownedPredicates, mode, ownerID})`. The reconcile
  uses Decision 3's schema-derived remove-set; there is no phase graph to validate.

So `lifecycle.Manager` becomes "owner registration (base) + transition table + projection +
operator API." The base is "owner registration + overlap check + schema-derived reconcile."
This is deliberate primitive *completion* (`feedback_reactive_patches_vs_engine_completion`),
not a parallel path. **New framework surface flagged for the final review:**

- **Three KV buckets:** `OWNER_CLAIMS` (the shared cross-process claim registry, Decision 2 —
  ONE arbitrated epoch key `_registry` holding the union of all claims + waivers,
  CAS-advanced via `UpdateWithRetry`; no bucket TTL); the SEPARATE `OWNER_PRESENCE` bucket
  (lightweight per-owner `heartbeat.<owner_token>` presence keys whose BUCKET-level NATS KV TTL is
  the server-enforced liveness backstop — separate bucket because `natsclient.KVStore` has no
  per-key TTL, `natsclient/kv.go:95,112`); and `PENDING_EDGES` (the KV-durable
  Conditional foreign-edge buffer, Decision 4 — keyed by target EntityID, drained
  delete-after-apply with a boot-time re-drain sweep). Both framework-owned, created at
  graph-ingest boot.
- **Two claim types + the cross-type check:** `OwnerClaim` (owned current state, Decision
  1/2/3) and `ForeignEdgeClaim` (relationship-producer, Decision 1/4) — distinct so the
  registry does not conflate cross-subject relationship writes with owned-state claims — PLUS
  the **Owner×FE cross-type collision check** (Decision 2 MEDIUM) that an OwnerClaim's owned
  predicate may not silently strip a registered ForeignEdgeClaim's edge.
- **The structured `CoordinationWaiver`** (`owner`, `with`, `predicates`, `reason`,
  `expiry`/`review_by`, `ref`) — predicate-scoped, audit-stored in the epoch; expiry enforced
  at the next registration boundary (review-obligation forcing function), distinct from runtime
  claim-staleness.
- **The epoch-CAS registration routine** (`UpdateWithRetry` on the `_registry` key in `OWNER_CLAIMS`):
  read-epoch → compact-stale → check-overlap (incl. cross-type) → merge-own-claims →
  CAS-write-at-read-revision → retry-on-mismatch-against-the-now-larger-epoch; plus the
  **post-registration epoch Watch** that re-checks overlap if a compacted-then-revived owner
  reappears — **a re-detected overlap there is a FATAL HALT of the revived process (v5), not a log**;
  plus the **write-time owner-token lease check** (v5; wire surface pinned v6) verifying the writer
  against the epoch's owner of the `(pattern, predicate)` cell to close the double-WRITE window — the
  token rides as the `OwnerToken` field on `Update`/`CreateEntityWithTriplesRequest` (NOT a header or
  `BaseMessage` envelope; the structs are registry-exempt), rejected with `ErrorCodeOwnerLeaseStale`
  through graph-ingest's existing response-classification path (Decision 2, "Where the `owner_token`
  lives").
- **The T2-seam foreign-edge enforcement** (Decision 4 BLOCKING-B): the unclaimed-foreign-edge
  reject at `ingestEntity`/`partitionTriplesBySubject` (`component.go:917-955,1031-1040`),
  boot-time `ForeignEdgeClaim` validation including the **inverse-gate** (`Conditional` requires
  a registered `InverseOf` — `vocabulary/registry.go:368-381` — else `Strict`), the
  delete-after-apply drain + boot re-drain sweep, and the **fourth-path fold**
  (`ensureReferencedEntityExists`): enqueue into `PENDING_EDGES` for birth-bearing targets (lane i),
  OR materialize via the framework's envelope-bearing referential-stub lane — named owner = the
  producer's registered `ForeignEdgeClaim`, `MessageType = core.identity.stub` — for no-birth
  targets like sensorml children (lane ii). Never an anonymous, envelope-less, ownerless `Put`.
- **The base `OwnerRegistry` type** and the `RegisterOwner` entrypoint; the overlap algorithm
  (glob-vs-glob *entity-ID-pattern* intersection × exact-string *predicate* intersection,
  over the epoch union).
- **The counting crash-recovery test** that is the flip-gate (enqueue → crash → restart →
  assert edge present) — a test, not a sentence.

These are additive but they are genuinely new spine, not "rename an existing field." Target
all of them in the final review.

### Decision 6 — Claims DERIVE from graph projection contracts (one declarative chain, not a parallel registry)

> **Ownership claims MUST be derived from a registered graph projection contract wherever the
> owner is driven by a payload type / Graphable. A hand-maintained list of ownership strings,
> registered in a boot block divorced from the projection that emits the facts, is the
> anti-pattern this decision forbids. `pkg/ownership` is the distributed-ENFORCEMENT substrate
> only — never a second user-facing model.**

The concern this closes (raised in review, 2026-06-13): if ADR-056 lands as "…and also register
your ownership strings over here," we will have built a **parallel semantic registry** that drifts
from the flow-based design and rots — the registry and the projection saying different things,
maintained in different places, by hand. That is exactly the failure mode the framework's existing
registries were designed to avoid.

The fix is to see that these are **three layers of ONE declarative chain**, not three registries:

| Layer | Question it answers | Where it lives today |
|---|---|---|
| **Payload type registration** | "When I see `agentic.task.v1`, what Go type do I decode?" — in-process type dispatch | `payloadregistry/registry.go` (point-to-point graph mutation request/reply is intentionally exempt, `graph/mutation_requests.go:7`) |
| **Graph projection contract** | "What graph facts does this type emit, and in what write mode is each predicate group?" | THE MISSING LAYER — today `Graphable.Triples()` (`graph/messagemanager/processor.go:259`) is treated as the whole truth; it says *what* triples but not whether they are append-evidence, owned current state, CAS-transition state, or foreign edges |
| **Ownership enforcement** | "Who may author/reconcile those facts at runtime, and is anyone else already there?" | `pkg/ownership` (this ADR) — substrate only |

The missing middle layer is the **graph projection contract**, declared ONCE beside the type /
Graphable / gateway-resource projection code, co-locating:

- the **entity-ID pattern** the projection writes,
- the **emitted predicates** grouped by **write mode** (replace-owned / cas-transition /
  append-evidence — Decision 1),
- the **foreign-edge claims** it produces (Decision 4),
- and the **indexing profile** (ADR-054 — already a per-type declaration, so the projection
  contract subsumes a thing that already exists rather than inventing a new surface).

The runtime chain is then **derivation, not duplication**:

```text
payload type registration
  └─ optionally declares a graph projection contract
       (entity pattern · predicates × write mode · foreign-edge claims · indexing profile)
component / gateway boot
  └─ binds each projection to an OWNER ID and DERIVES the OwnerClaim/ForeignEdgeClaim set,
     registering it with pkg/ownership.RegisterOwner
graph-ingest
  └─ enforces the registered claims at the write boundary (overlap reject, lease check, T2 seam)
```

**Manual `RegisterOwner` is the low-level ESCAPE HATCH, not the default.** Two legitimate
escape-hatch users: (1) owners whose entity-ID pattern is not derivable from a single payload type
(the lifecycle `Manager`'s `Workflow.Schema` — already claim-shaped, Decision 1 §500-503 — registers
directly), and (2) migration scaffolding before a producer has declared its projection contract
(the Decision-3 / Decision-4 deprecated-on-arrival hatch). Everything else derives.

**Why this is now a HARD requirement, not a nicety.** The semconnect acceptance fixture is the
proof and the cautionary tale at once: cs-api's System claim is six `sensorml.*` vocabulary
predicates over `…csapi.system.*` (Consequences → Acceptance fixture). If those live as a
hand-edited slice in a boot function, the day someone adds a predicate to the System triple-builder
(`systems_post.go`) and forgets the slice, the projection emits a fact the registry doesn't know is
owned — silent drift. Derived from a projection contract declared *beside the builder*, the claim
and the emission cannot disagree. semconnect should declare "the CS-API System projection owns these
predicates on this pattern; the Deployment projection owns these; SensorML emits this foreign
edge" next to the resource projection code — never as a parallel hand-maintained registry.

This decision does **not** change `pkg/ownership` (the W0 spine is correctly substrate-only); it
constrains the **layer above** it. The projection-contract type and the boot-time derivation are a
named deliverable of the enforcement-wiring increment, and graph-ingest enforces what derivation
registers. The spine's `RegisterOwner` is the seam both the derivation and the escape hatch call.

#### Decision 6a — Rule packs are a config-level projection producer (gh#278, semteams)

Decision 6's derivation chain enters via **payload-type registration** ("a Graphable optionally
declares a projection contract"). There is a THIRD producer shape that is neither a payload-typed
Graphable nor a gateway resource: the **rule engine's triple actions**. semteams mapped its full
rule-pack exposure and found the gap precisely:

- **Every** rule triple action is on the legacy **bare-triple lane** and **none can declare
  ownership** (subject consts `processor/rule/triple_mutator.go:17-18`; the executor switch + the
  non-atomic `update_triple` = remove-then-add at `processor/rule/actions.go:848`):

  | Rule action | Path | Graph-ingest write semantics |
  |---|---|---|
  | `add_triple` | `triple.add` | append-evidence (auto-vivifies today; must-exist after the flip) |
  | `remove_triple` | `triple.remove` | CAS-destructive (removes all triples for the predicate) |
  | `update_triple` | `triple.remove` then `triple.add` | **non-atomic** remove-plus-add, two revisions, no `ExpectedRevision` |

- So a rule pack that legitimately OWNS a coordination predicate group has **no place in Decision 6's
  derivation model and no atomic owned-write lane**. The clearable-coordination class lives on the
  non-atomic `update_triple` today (a reader between the two revisions sees no value; two writers race
  with no conflict detection — the ADR-055 §4 smell).

A rule pack is a **permanent config-level producer class**, not a payload type and not a gateway
resource. This decision NAMES it inside Decision 6's chain. It adds **no new model**: the
`projection.Contract` type already carries a logical `Name` (not only a payload `MessageType`) and an
owner bound at `Bind` time, and `Contract.Validate` requires only `Name` + at least one predicate group
— `MessageType` is optional (`pkg/projection/contract.go`). What is missing is the **wiring**.

**The two write-classes (semteams's classification, confirmed).** Rule-written predicate groups split
by actual lifecycle, and only the second class is in scope:

1. **Write-once append-evidence** — set once, never cleared; multi-writer-safe; genuinely unowned
   (Decision 1 already exempts these, no change). E.g. `agent.run.outcome` (terminal scalar),
   `agent.run.handoff`, `*.completed` markers. These keep using `add_triple` (append-evidence).
   They are NOT exempt from the flip, though: a write-once marker stamped on a **framework-born
   anchor** (loop-execution, plan-loop) is still flip-exposed via that ANCHOR's birth lane (the
   marker write hard-fails if the anchor is not born-first) — see Decision 4, Fix part 6,
   precondition (1). The append semantics are unchanged; the exposure is the anchor's existence.
2. **Clearable coordination / current-state** — set then removed, or replaced; a SINGLE writer pack;
   presence/value is current-state, not accumulating evidence. E.g. the ADR-053 HITL
   `*_pending`/`*_resumed` markers (set then cleared on reply), `autoresearch.iteration.pending`,
   `autoresearch.best.value` (a running max). These are **owned coordination state** managed today with
   non-atomic remove / remove-plus-add and zero ownership registration.

**The decision (three parts; the model is unchanged, the wiring + one action are new):**

- **Rule packs declare and `Bind` `projection.Contract`s at rule-pack load**, under a stable
  subject-safe owner id `rule-pack.<pack-id>` — `<pack-id>` constrained to `validOwnerID`'s charset
  `[A-Za-z0-9._=-]` (`pkg/ownership/glob.go:21`), so the separator is a **dot, not a colon**
  (`rule-pack:<pack>` is not a valid NATS KV key), and the owner id is the **canonical string, not a
  `RuleToken` hash**. Two packs claiming the same cell then collide via `ownership.RegisterOwner`
  against the live epoch exactly like any other owner — **no parallel registry, no drift**. (Wiring is
  a follow-up increment: rule-pack config declares contracts beside the rule definitions and binds them
  via `projection.Bind(ctx, reg, "rule-pack.<pack-id>", contracts...)` at load.)
- **One declared `replace-owned` rule action** emits `update_with_triples` (atomic
  replace-by-(subject,predicate)), valid **only** for predicates a bound contract declares the pack
  owns. It writes under the bound owner identity and carries the `OwnerToken` once that write-lease
  field lands (deferred — `pkg/ownership/doc.go`). The clearable-coordination class migrates onto this
  action; `add_triple` is unchanged for the write-once class.
- **Scope red line — NOT a state-machine substrate.** The action is `replace-owned`, **not**
  `cas-transition`: a rule has no real target revision to condition on, and phase transitions stay with
  the lifecycle `Manager` via `lifecycle_transition`. Rules must not become a parallel state-machine
  runtime — consistent with the retired `processor/reactive/` and the no-DSL / no-state-machine-runtime
  rule (CLAUDE.md). The generator/action COMPILES to ownership claims + an owned-write; it does not
  introduce a new executor.

**Object-token discipline for rule-written edges (gh#278 Finding D).** A rule that stamps an object
which is an entity REFERENCE must use `$entity.id` (the full 6-part entity ID,
`processor/rule/execution_context.go:179`), **not** `$entity.instance` (the bare instance segment —
the 6th part only; `processor/rule/entity_substitution.go:55-56`).
The two are distinct tokens: `$entity.id` is the whole address, `$entity.org`/`…`/`$entity.instance` are
the individual parts, and `$entity.triple.<predicate>` resolves to a triple object on the entity. Mixing
them — one marker stamping a full ref, a sibling stamping the bare segment — forces consumers to
normalize and has broken run-resume in practice. Rule authors emitting entity references should default
to `$entity.id`.

**Sequencing.** This subsection NAMES the producer class + the conventions + the flip-gate signal (Fix
part 6, Decision 4). The **contract-binding wiring** and the **`replace-owned` action** are two separate,
independently-reviewed follow-up increments — not designed here.

### The CQRS boundary, made explicit

The framework owns deterministic projection; products do not hand-roll mirrors.

| Role | Primitive | Owner | Enforced? |
|---|---|---|---|
| **Command / write model** (one owner mutates a predicate group) | `update_with_triples` (schema-backed replace-owned/CAS) | the predicate group's registered owner | YES — Decision 2 overlap + Decision 3 schema |
| **Read projections** (graph queries, gateway views, dashboards, files) | derived from `ENTITY_STATES`, read-only | framework / read-side consumers | n/a (read-only) |
| **Events / work items** (tasks, requests) | JetStream streams or product command buckets | producers; consumed via ack | n/a (different substrate) |
| **Product private bucket** (PLAN_STATES) | product KV | product | NO (honest) — but its `ENTITY_STATES` mirror leg IS (Decision 5) |

**Ownership arbitration is NOT a domain fact — but provenance CAN be (semconnect review).** Sharpened
two-part rule: (1) *Do not* encode ownership ARBITRATION as domain triples — who-may-write lives in the
`OWNER_CLAIMS` substrate, never as facts on the entity. (2) *Do* allow explicit provenance triples when
they describe entity **materialization, source, audit, or lineage**. `core.identity.stub_owner` (the 4b
fold) and `core.identity.referenced_by` are provenance-on-entity — facts about *how a node came to exist*
— NOT ownership claims and NOT enforcement rules. The distinction matters for graph readers: CS API clients
will see provenance-like facts and must not read them as authorization or write ownership.

## The atomicity guarantee (the KV-twofer is preserved)

Replace-owned does **not** break "the write IS the event." `RemoveTriples` + `AddTriples`
happen inside ONE `UpdateWithRetry` callback: the callback reads the current value, computes
the reconciled triple slice in memory, and returns ONE new value, written as exactly ONE
`kv.bucket.Create` (rev-0) or ONE `kv.Update` (CAS) per successful attempt
(`natsclient/kv.go:235,257`). **One KV write = one new revision = one Watch event.** A rule
watching the entity sees a single atomic transition from old owned-set to new owned-set, never
an intermediate "predicates removed but not yet re-added" state. The KV-twofer holds
unchanged; replace-owned is a single-revision delta, the same as today's `Manager.Transition`.

## The missing primitive, named

The primitive every consumer hand-rolls — semspec's `UpsertEntityIfChanged`
(`triple.go:235-270`), lifecycle's `Manager` (`manager.go`), the mutation handlers
(`mutations.go`) — is the **Owned-State Reconcile**:

> For an entity and a registered owner's predicate group, reconcile `ENTITY_STATES` so the
> owned predicates match the owner's current values (replace what changed, remove what the
> owner dropped, leave foreign predicates untouched), optionally under a CAS condition.

`pkg/lifecycle.Manager` already IS this primitive **for entities whose owned set is a Go
struct AND that have a phase graph.** Walk it: declared owned set = `Workflow.Schema`
(`workflow.go:61-65`); replace-owned = `RemoveTriples` per predicate (`manager.go:492-506`);
preserve-foreign = graph-ingest leaves undeclared triples alone (`projection.go:38-45`); CAS =
`ExpectedRevision` (`manager.go:505`); the rationale matches semspec's independently
(`manager.go:484-491` ≈ `triple.go:565-603`). **It is ~75% the reconcile primitive. The
missing 25% is (a) non-state-machine owner registration (Decision 5 base) and (b) schema-driven
owned-set REMOVAL for the dropped-predicate case** (Decision 3 / 3a — semspec's set→empty fix
lifted to the framework). Everything else exists.

## Envelope fields sit OUTSIDE the predicate-group contract

`MessageType`, `Version`, and `StorageRef` are **fields on `EntityState`, not predicates.**
They merge latest-wins / preserve-when-nil (`MergeEntity` at `component.go:1271-1274`;
`preserveStoredEntityMetadata` at `mutations.go:548-561`), independent of any owner's predicate
claim. The ownership contract governs *triples*; envelope fields are governed separately:

- **Owner = the birth lane.** `MessageType`/`Version` are stamped at birth (Fact-arrival
  `component.go:992`, Entity-create) and preserved-when-zero thereafter — no predicate owner
  reconciles them. This is correct: the envelope is the entity's provenance, not an owned
  state value.
- **`StorageRef` is an envelope field, preserved-when-nil**, and `mutations.go:540-547`
  documents the gh#260 gap: there is **no deliberate-clear escape hatch** — sending nil means
  "preserve," so a content-GC that needs to clear a dangling `StorageRef` cannot. This ADR does
  not resolve gh#260; it **names that StorageRef is an envelope field outside the predicate
  contract**, so the eventual fix (a `ClearFields` list or pointer-optional, per
  `mutations.go:545-546`) is an envelope-field concern, not an owned-predicate concern. The
  owner of envelope fields is the birth lane; a future "clear StorageRef on GC" capability is a
  birth-lane / mutation-handler escape hatch, not an owner reconcile.

Naming this prevents a reviewer (or implementer) from trying to model `StorageRef` as an owned
predicate and inheriting the wrong merge semantics.

## Restart across an owner-schema migration

An owner version that **drops a predicate from its owned set** will, on its next reconcile,
strip the prior version's value for that predicate (Decision 3's set-difference removal fires
because the dropped predicate is no longer in the new schema's owned set). **This is intended
cleanup, not data loss — but only when the drop is deliberate.** The contract:

- A predicate removed from an owner's schema is, by definition, no longer owned state the owner
  maintains. Stripping its stale value on next reconcile is the correct convergence (the same
  logic as semspec's set→empty fix, `triple.go:248-256`).
- The hazard is an **accidental** drop (a refactor typo) silently deleting live state. Guard:
  schema changes to an owned set are a reviewable diff (the schema is declared, not computed),
  and the startup audit (Decision 5) can emit an `owned_predicate_dropped{owner,predicate}`
  signal on a schema that drops a predicate present in stored data — observable, so a deliberate
  drop is confirmed and an accidental one is caught. Partial-deploy safety holds via Decision 3a
  (an old binary sends nil and strips nothing).

### The append-evidence ↔ replace-owned migration-window hazard

A SECOND migration hazard, distinct from the dropped-predicate case: **a predicate flipping
write mode (`append-evidence` → `replace-owned`) while old binaries are still running.** This
is exactly the `coordinator.decision.*` situation (Decision 2): the overlap check fires only
after the Wave-1 reclassification, and during the rollout window some processes write the
predicate as append (old) and some as replace (new). For that window, the replace-mode writer
strips siblings the append-mode writer is still adding, and the append-mode writer re-grows a
set the replace writer thinks it owns — a split-brain on one predicate group.

Mitigation (coordinated flip, not a free-running rollout):

- The flip is a **declared, single-direction change** to the owner's registered claim mode;
  it is a reviewable diff like the schema change above.
- The same `owned_predicate_dropped{owner,predicate}` audit signal (Decision 5) fires when a
  freshly-`replace`-mode writer strips a predicate that stored data shows was being appended —
  so the migration-window split-brain is **observable**, not silent.
- The operational rule: **flip the mode in one coordinated deploy of all writers of that
  predicate group**, not a long mixed-mode window. Because Decision 2's overlap check now reads
  the SHARED `OWNER_CLAIMS` bucket, the moment all writers register the new `replace` mode the
  cross-process overlap (if any) is caught at boot — so the flip cannot half-land into a
  silently-coexisting state across processes. The window is bounded by the deploy, and its tail
  is observable via the audit signal.

## Cryptographic provenance — RESERVE the seam, do NOT implement here (Codex P2)

Owner registration (Decision 2) + semantic envelopes (ADR-055) are an
**AUTHORIZATION / provenance contract**: they assert *"this owner is ALLOWED to mutate this
predicate group"* and *"this write carries a declared type/domain/category/version."* They are
**NOT cryptographic proof of authorship** — they do not prove *"this owner really authored this
envelope"* or that the bytes were not forged or replayed. Those are adjacent properties, not
identical ones, and this ADR is careful not to conflate them.

Today's envelope is **unauthenticated.** `BaseMessage.Hash()` (`message/base_message.go:158-180`)
is a SHA256 over message-type + payload — an integrity/content digest, **no signing key, no
authentication**: anyone who can publish to the bus can mint an envelope with any `owner_id` and
any `MessageType`. The `OWNER_CLAIMS` registry says *which* owner is permitted to write a
predicate group; it does not (today) verify that a write *claiming* to be from that owner
*actually is*. In a single-trust-domain deployment that is acceptable; the registry's value is
catching honest wiring collisions, not adversaries.

**ADR-056 leaves room for, but does NOT design:** signed owner claims (a claim record carrying
a signature) and signed message/mutation envelopes (verified at graph-ingest before a
fact-arrival or owner-reconcile write is accepted). **Out of scope here:** key management,
canonical-byte serialization for signing, verification policy (fail-closed vs observe), and
replay handling. Pulling any of that into 056 would bloat the spine and couple it to a key
infrastructure decision that does not exist yet. The follow-up is scoped — not designed — in
**[ADR-057: Cryptographic Provenance](057-cryptographic-provenance.md)** (Status: Proposed,
scope-only). 056's contract stands on its own as an authorization contract; 057 is the optional
authenticity layer that can ride the same envelope + registry seams later.

## How ADR-055 narrows under this parent (NARROWER, not weaker)

ADR-055 becomes the **small enforcement ADR UNDER 056** — its scope shrinks to one rule, and
that rule gets *more* teeth, not less:

> **ADR-055 (narrowed): No write may create an entity without a semantic envelope. The two
> `triple.add`/`add_batch` auto-vivify branches (`component.go:1691-1698`, `1790-1796`) are
> deleted (`triple.add`/`add_batch` become must-exist), AND the FOURTH auto-vivify path
> `ensureReferencedEntityExists` (`component.go:1424-1525`) is covered — folded into the
> Decision-4 pending-edge buffer (enqueue) for birth-bearing targets, OR materialized via the
> framework's envelope-bearing referential-stub lane (named owner = the producer's registered
> `ForeignEdgeClaim`, `MessageType = core.identity.stub`) for no-birth targets like sensorml
> children. No ownerless births; no auto-vivify PRODUCER path; no ENVELOPE-LESS stub — the only
> stub that survives is the framework's own referential-stub artifact, which now carries a semantic
> envelope and a named creating claim (Decision 4 lane ii), not the anonymous envelope-less `Put`
> the flip deletes.**

What moves OUT of ADR-055 and INTO 056 (this ADR), making 055 narrower:

- **The four-lane taxonomy and per-predicate matrix** are now *consequences* of Decision 1
  (the claim's `write mode` IS the lane). ADR-055 need not own the taxonomy; it owns the
  enforcement of the one birth rule.
- **The reconcile primitive** (owned-set removal) is 056's Decision 3, not an ADR-055
  appendix.
- **The foreign-edge / must-exist interaction** is 056's Decision 4, which now **gates**
  ADR-055's closing-move flip: the unclaimed-foreign-edge reject binds at the T2-regroup seam
  (not at registration, which sensorml bypasses), the pending-edge buffer is KV-durable and
  crash-safe by delete-after-apply + boot-sweep (not a fictional cross-bucket atomic), and the
  **fourth auto-vivify path is folded in**. ADR-055's #267 "graceful degradation" clearance is
  superseded by Decision 4's Conditional edge-birth (deferred-apply, never silent drop).

What ADR-055 KEEPS (its sharpened core): the envelope-on-birth rule, the must-exist flip, the
governance verdict-stream (§3a), the T1/T2/StorageRef transport guards, the rejection metric.
**055 is not weakened — it is pure enforcement of "no ownerless births," gated on 056's
edge-birth contract.** Its closing move stays its closing move; it now has one more
precondition (Decision 4).

## How the other ADRs fold in

### ADR-049 (lifecycle harness) — one *instance* of the contract

ADR-049's `Manager` is the owned-state reconcile specialized for state machines. Nothing in
ADR-049 changes behaviorally; this ADR re-describes `Manager` as registering one claim
(Decision 1) through the base `OwnerRegistry` (Decision 5), with the phase graph / History /
operator API as `Manager`-specific layers. The ADR-055 fix-plan reached the same conclusion
(`graphable-fix-plan.md:149-155`: *"ADR-049 is the exemplar of the state pattern, not a
violator"*).

### ADR-054 (indexing eligibility) — the envelope rider

ADR-054's `IndexingProfile` rides the *same envelope* a birth lane carries
(`extractEntityFromMessage` stamps it at `component.go:1015-1017`). It is an envelope-field
concern (above), not an owned-predicate concern. No conflict.

## Consequences

### For graph-ingest

- The two merge contracts get **named and enforced**, not just lived: `MergeEntity` append
  (`component.go:1270`) is the *fact* contract; schema-backed `MergeTriples` replace
  (`helpers.go:101-134`) is the *owned-state* contract, gated by the owner registry.
- Net-new: the base `OwnerRegistry` + the cross-process overlap check over the single
  `_registry` epoch key in `OWNER_CLAIMS` (read-check-merge-CAS-retry, Decision 2); the schema-derived
  owned-set removal (Decision 3, nil-guarded per 3a); the **T2-seam foreign-edge reject +
  inverse-gate** and the **KV-backed delete-after-apply** Conditional pending-edge buffer
  (`PENDING_EDGES`, Decision 4). All additive, but all genuinely new spine — flagged for the
  final review.
- All FOUR auto-vivify paths are accounted for by ADR-055's closing move, now gated on Decision
  4: the two `AddTriple`/`AddTriples` branches (`component.go:1691-1698`, `1790-1796`) are
  deleted, and the fourth path `ensureReferencedEntityExists` (`component.go:1424-1525`) is folded
  into the pending-edge buffer (enqueue) for birth-bearing targets, or routed through the
  envelope-bearing referential-stub lane for no-birth targets (Decision 4 lane ii) — never an
  anonymous, envelope-less `Put`.

### For lifecycle (`pkg/lifecycle`)

- `Manager` embeds the base `OwnerRegistry` (Decision 5). Registration routes its `Workflow`
  claim through the base; the overlap check now sees lifecycle and non-lifecycle claims
  together. No transition/projection behavior change.

### For products (semspec, semteams, semconnect)

- **The migration value is PROSPECTIVE, and the ADR says so plainly.** This does NOT shrink the
  ADR-055 producer migration — the 21 producers still need per-producer edits to move onto
  enveloped birth lanes. What it buys: semteams (which inherits whatever substrate exists) does
  **not** build a *third* hand-rolled `UpsertEntityIfChanged`/`OwnedPredicates` pattern, and
  semspec's 7 sites + `PLAN_STATES` mirror gain a framework destination to migrate *toward* (via
  the Decision-3 escape hatch, one site at a time, then registry-backed). The cost is real and
  per-producer; the value is stopping the next reinvention.

### Acceptance fixture — semconnect cs-api (the design, made concrete)

> **The gate (Coby, 2026-06-13): if 056 cannot describe EXACTLY how semconnect registers claims
> for its entity types, the ADR is still too abstract.** This subsection runs that gate against the
> real code (`../semconnect/gateway/cs-api/`, read 2026-06-13). It is the worked example, not an
> abstraction — every claim cites `file:line`.

semconnect's cs-api gateway writes **11 entity types** into `ENTITY_STATES` through
`graph.mutation.entity.create_with_triples` / `update_with_triples` (`systems_post.go:27-30`).
Under 056 each becomes one `OwnerClaim` keyed to its entity-ID glob (the per-type prefix from
`config.go:180-190`, e.g. `c360.semconnect.systems.csapi.system.*`) enumerating its full predicate
set. Representative registrations (the rest follow the same shape):

| Entity (glob) | `OwnerClaim` predicate set (exact strings) | Write mode | Foreign-subject edge → `ForeignEdgeClaim` |
|---|---|---|---|
| System `…csapi.system.*` | `sensorml.process.{type,uid,label,description,position}`, `sensorml.component.isHostedBy` (own→parent) | replace-owned | YES (SensorML path only — see below) |
| Deployment `…csapi.deployment.*` | `cs-api.deployment.{parent,deployedSystems}` + `sensorml.process.{type,uid,label,description,position}` | replace-owned | none (object-only refs) |
| Datastream `…csapi.datastream.*` | `cs-api.datastream.{phenomenonTime,resultTime}` + `csapi.{ProducedBy,HasResultSchema}` + `sensorml.process.{label,description}` + `sosa:observedProperty` | replace-owned | none (object-only refs) |
| SchemaArtifact `…csapi.schema.*` | `sensorml.process.type` (=`SWESchemaDocument`) | replace-owned (cs-api-created target) | none |

This worked example surfaces four things the abstract design only implied — all **resolved by the
existing mechanism**, which is exactly why the gate is passed and not failed:

1. **A consumer's owned set is DOMINATED by shared vocabulary predicates, not its own namespace.**
   cs-api's System claim is six `sensorml.*` predicates and ZERO `cs-api.*` ones; the owner is the
   **writing component (cs-api), not the `parser/sensorml` package** whose constants it reuses
   (`systems_post.go:209-237`). The design already supports this because a claim is
   `(entity-ID glob × EXACT predicate string)` (Decision 1/2) — cs-api owns `sensorml.process.label`
   *only on `…csapi.system.*`*; an input-component SensorML-ingest path owning the same predicate on
   a **disjoint** id-glob does NOT collide. **This must be stated or a reader concludes shared
   predicates are unownable.** (Stated here.)

2. **Disjoint-id-space is the cross-producer contract, and overlap rejection is the design WORKING.**
   If a non-cs-api SensorML-ingest path were ever configured to mint entity IDs under
   `…csapi.system.*` AND own `sensorml.process.label` there, registration is rejected at boot
   (Decision 2) — the correct outcome, because two producers replace-owning the same cell is the
   dual-write 056 exists to retire. cs-api owns its id-space exclusively; that is its half of the
   contract.

3. **The SensorML child edge is the flagship `ForeignEdgeClaim` — and `isHostedBy` is owned AND
   foreign, disambiguated by SUBJECT position.** On the GeoJSON-Feature path cs-api emits
   `{Subject: <own System>, isHostedBy, Object: parent}` — an OWN edge to a foreign object
   (`systems_post.go:237`), governed by the System `OwnerClaim`, its object a fourth-path reference
   target. On the SensorML path `buildSystemTriplesFromSensorML` calls `asset.Triples()`
   (`systems_post.go:151`), which for a `PhysicalSystem` with `Components` emits
   `{Subject: childID, isHostedBy, Object: <System>}` — a FOREIGN-subject edge
   (`parser/sensorml/graphable.go:124`). The same predicate is therefore in BOTH cs-api's
   `OwnerClaim` (own→parent) and a `ForeignEdgeClaim` (child→System); they do **not** trip the
   Owner×FE collision check (Decision 2 MEDIUM) because `partitionTriplesBySubject` files them on
   **different subject entities** — replace-owned on the System never touches the child. The
   `ForeignEdgeClaim` is the **no-birth referential-stub lane** (Decision 4 lane ii): the child has
   no independent birth (sensorml defines one `EntityID()`, the parent — `graphable.go:37`) and
   `sensorml.component.isHostedBy` carries **no registered inverse** (`predicates.go:97`,
   `WithIRI` only), so neither the pending-edge drain nor backfill can reconstruct it — the
   envelope-bearing stub is load-bearing.

4. **Two concrete cs-api migration sites, named.** (a) The blanket replace — `replaceEntityTriples`
   sets `RemoveTriples: uniquePredicates(current.Triples)` (`systems_crd.go:145`; same shape in the
   PATCH paths), which today erases EVERY predicate on the entity including lifecycle/provenance/
   inferred/foreign facts from other owners. Under 056 it becomes replace-**owned**: `RemoveTriples`
   = the System claim's declared set (Decision 3). (b) The single-subject assumption — `ingestTriples`
   runs `singleSubject(triples)` (`systems_post.go:317`) and **errors** on the multi-subject set a
   rich SensorML hierarchy produces, so cs-api cannot round-trip embedded `Components` at all today;
   the `ForeignEdgeClaim` + the shared projection-normalization seam (Decision 4, now lane-independent
   — it covers cs-api's `create_with_triples` lane, not just fact-arrival) is the path that makes it
   work. **Migration rider (open question, named for the fixture):** dropping `singleSubject` is
   necessary but not sufficient — the seam keys the foreign-edge claim lookup + the metric on
   `req.Entity.MessageType`, and cs-api's `createEntityWithTriples` builds the request with a
   MessageType-less `&graph.EntityState{ID, Triples}` (`systems_post.go:321`). A zero MessageType is
   metered as `message_type="_invalid"` and can only match a `Producer:""` (any-producer) claim, never
   cs-api's exact-producer `ForeignEdgeClaim`. So the cs-api migration MUST also STAMP a non-zero
   `Entity.MessageType` (the cs-api System producer type it registers its claim under) on its mutation
   requests, or its foreign edges read as unclaimed forever. This belongs in the semconnect migration
   fixture explicitly.

**Verdict: the gate is PASSED.** 056 describes semconnect's registrations exactly — one `OwnerClaim`
per entity-type id-glob, the shared-vocabulary-owned-by-writer rule, one `ForeignEdgeClaim` for the
SensorML child edge in no-birth-stub mode, and two named replace/ingest migration sites — without a
single new mechanism beyond Decisions 1–4. The cs-api migration is real, per-site, and breaking-ish
(boot-time claim failures and write-time lease failures replace silent corruption); it needs tests
and a compatibility window, tracked as the first 056 consumer-migration.

#### Second fixture — semteams (the rule/lifecycle axis semconnect didn't exercise)

semconnect grounds the **gateway-write** axis (Decisions 1–4). semteams — the heaviest rule/lifecycle
consumer — grounds the axis 056 made its strongest *unverified* claims about: Decision 5 (product
buckets ≠ a second authoritative source) and "rules observe, don't own." A bounded two-question probe
(`../semteams/`, read 2026-06-13):

- **Q1 — dual-write / private-authoritative bucket: CLEAN.** semteams creates **no** state bucket of
  its own. Every KV bucket it touches is a framework read-cache / work-queue / config (`PERSONAS`,
  `FLOW_TEMPLATES`, `AGENT_LOOPS`, `RULES`, `semstreams_config`); there is **no `*_STATES`-style
  authoritative-mirror** like semspec's `PLAN_STATES`. All entity state flows through graph-ingest.
  Decision 5 is now grounded in a **second independent consumer**, not just asserted.

- **Q2 — foreign-subject rule writes: FOUND (22 agent-run rules), and they are CORRECT — but they
  falsified the ADR's prose, now fixed above.** The rules stamp coordination markers
  (`agent.run.outcome`, `agent.run.handoff`, `agent.run.clarification_pending`, the per-pack
  `*.completed`/`*.task_failed`) onto the **run entity** while firing on a different loop, then a
  sibling rule consumes the marker and triggers the lifecycle Manager's `lifecycle_transition`
  (`configs/rules/agent-run/04-*.json`). Phase moves **only** through the Manager; the markers are a
  **disjoint predicate group**. This is the **multi-owned-by-predicate-group** pattern — load-bearing
  ADR-053 coordination, not the anti-pattern — and it forced the "rules observe, don't own"
  correction in *For the architectural identity* above (the old "cleared only because they stamp
  their trigger entity" rationale would have wrongly flagged 22 production rules).

- **semteams's migration question** (its half of the contract, for the semteams team — recorded here
  for ground truth, not actioned): the run-marker predicate group needs a 056 classification —
  **append-evidence exemption** (the natural fit: write-once trigger conditions, and two
  mutually-exclusive rules can write the same marker) **or** a registered rule-pack owner. Either is
  expressible today (Decision 2's append-evidence exemption already exists); nothing new is required.

**Both fixtures pass with no Decision re-architecture.** semconnect exercised replace-owned +
foreign-edge + the fourth-path stub; semteams exercised Decision 5 + multi-owner-per-entity + the
rules-don't-own boundary. The only design-doc change either forced is **prose** (the shared-vocabulary
ownership note, and the predicate-group-scoped correction to "rules observe, don't own"). The design
center — authoritative semantic state, owned per `(entity × predicate-group)` — held against both.

### For the architectural identity

KV-twofer preserved (single-revision replace, atomicity section). Facts-vs-Requests preserved.
State ownership exclusive — **sharpened from "only graph-ingest writes" to "exactly one
registered owner per predicate group, overlap rejected at registration."**

**"Rules observe, don't own" is PREDICATE-GROUP-scoped, not entity-scoped or
trigger-entity-scoped (semteams correction).** An earlier draft cleared the rule-engine
derived-fact stamps (`actions.go:600/684/821/1353`) *"only because they stamp the rule's own
trigger entity"* (`graphable-bypass-audit.md:240-244`). The semteams acceptance probe (below)
falsifies that rationale as a general principle: **22 production agent-run rules deliberately
stamp a FOREIGN entity** — the run entity, via `$entity.triple.agent.run.entity_id`, while firing
on a coordinator/reviewer/execute loop (`configs/rules/agent-run/05-*.json`). That is not the
anti-pattern; it is the load-bearing ADR-053 coordination pattern. The correct invariant is
therefore: a rule may write a predicate group on **any** entity, including one it does not own,
**provided** (a) that group is registered to the rule pack as an owner OR is append-evidence
(multi-writer, Decision 2 exemption), and (b) it is **disjoint** from every other owner's group on
that entity. The actual anti-pattern is narrower and sharper: **a rule REPLACING another owner's
owned group** — e.g. writing `agent.run.phase` directly instead of requesting the transition
through the lifecycle Manager's `lifecycle_transition` action. semteams obeys exactly this line:
phase moves *only* through the Manager (the registered owner), while the rule markers
(`agent.run.outcome`, `agent.run.clarification_pending`, …) are a **disjoint coordination group**
on the same entity, consumed by a sibling rule that then triggers the owner's transition. The run
entity is thus **MULTI-OWNED by predicate group** (lifecycle-Manager phase + rule-pack marker
group), which the predicate-group ownership model already expresses — "single ownership per
entity" was never the rule; "single ownership per `(entity × predicate-group)`" always was.

## Open Questions (genuinely deferrable only — none of Decisions 1–5 or the BLOCKING fixes are here)

Both v3 BLOCKING holes AND both v4 implementation holes are CLOSED IN THE DECISION, not parked
here: the cross-owner overlap detection is **decided via the single epoch key** (read-check-
merge-CAS-retry, Decision 2); the pending-edge buffer durability is **decided KV-backed with
delete-after-apply + boot re-drain** (`PENDING_EDGES`, Decision 4); the inverse-gate is **decided
to bind at the T2-regroup seam**, not at registration (Decision 4); the fourth auto-vivify path
is **decided folded into the pending-edge buffer** (Decision 4); the `CoordinationWaiver` surface
is **decided** (structured, expiry-bound at the registration boundary, stored in the epoch —
Decision 2). What remains is genuinely deferrable optimization / tuning / downstream-timing /
sequencing:

1. **Overlap-check intersection cost + epoch-key sharding.** Entity-ID glob-vs-glob ×
   exact-string predicate intersection over the epoch union is O(claims²) per registrant at boot;
   registration is a boot-time, bounded-N operation, so this is almost certainly fine. If a
   deployment ever registers thousands of claims, two deferred optimizations apply: an
   interval-tree / trie index for the intersection, and **sharding the single epoch key by
   entity-ID-pattern prefix** (one epoch key per shard, overlap-arbitrated within a shard; only
   warranted if the union approaches `MaxValueSize`). Both deferrable — correctness (total order
   across registrants of any claim) is decided; these are scale optimizations.
2. **`PENDING_EDGES` / `OWNER_CLAIMS` retention + heartbeat tuning.** The *contract* is decided
   (KV-durable buffer drained delete-after-apply + boot sweep; per-owner heartbeat + epoch
   compaction + per-owner-presence-key NATS KV TTL backstop; `ttl_hint ≥ 3×max(boot_time, gc_pause_budget)`).
   The concrete heartbeat interval, the exact `ttl_hint`/grace values, the per-target pending
   bound, and the post-registration Watch debounce are implementation-tuning calls, deferrable.
   (Correctness — never-silent-drop, never-block-forever, false-eviction-recovered-by-Watch —
   does not depend on the specific numbers, only on `ttl_hint` honoring the stated 3× floor.)
3. **Envelope-field clear (gh#260) timing.** This ADR names `StorageRef` as an envelope field
   needing a deliberate-clear path for content-GC; *when* that lands (with the first GC consumer)
   is gh#260's call, deferrable.
4. **Cryptographic-provenance scope (ADR-057).** Whether/when the optional signed-claim +
   signed-envelope authenticity layer is built rides on a trust-domain requirement that does not
   exist today; 056 reserves the seam, ADR-057 scopes it, neither blocks Accept.
5. **Migration ordering vs ADR-055 waves.** Decision 4 adds a precondition to ADR-055's
   closing-move flip (the four-part "Conditional path exists" predicate + the green counting
   crash-recovery test + fourth-path coverage must hold first). The exact wave interleaving
   (does the base `OwnerRegistry` + `OWNER_CLAIMS`/`PENDING_EDGES` land in a 056 Wave 0 before
   ADR-055 Wave 1 pilots, or alongside?) is a sequencing call for the implementation plan, not a
   design decision. Naming the responsibility does not reorder the producer migrations.
6. **Explicit NON-GOAL — semantic dedupe (semconnect review).** Ownership arbitrates *writes* over
   `(entity-ID pattern, predicate)` cells; it does NOT reconcile two distinct entity IDs that denote
   the same real-world object. Flagship case: a no-birth child gets a deterministic child ID under
   its parent's instance token (Decision 4 lane-ii stub), but a later client that posts that same
   physical object as a *standalone* entity under a DIFFERENT id/UID creates a SECOND graph entity
   for one real thing — and governance will NOT merge them. Id-canonicalization / `sameAs`
   reconciliation is a separate concern, out of scope here. This ADR guarantees only that a
   referenced id resolves to a node and that writes to a given `(id, predicate)` cell are arbitrated
   — not that two ids for one object are unified. (Listed as a non-goal so it is labeled, not implied
   solved.)

> Confirmed: **none of Decisions 1–5 (ownership granularity, overlap rejection, schema-backed
> reconcile, foreign-edge birth, product-bucket enforcement), neither v3 BLOCKING fix, NOR either
> v4 implementation hole (the single epoch key catching cross-owner overlap via read-check-merge-
> CAS-retry + compaction/TTL/Watch; the T2-seam inverse-gate + delete-after-apply/boot-sweep +
> the fourth-path fold + the testable crash-recovery gate) appears in Open Questions.** They are
> all in the Decision. Open Questions hold only intersection-cost/epoch-sharding optimization,
> retention/heartbeat tuning, downstream gh#260 timing, the reserved-and-deferred ADR-057
> authenticity layer, and ADR-055 wave sequencing — all genuinely deferrable. No bar requirement
> and no BLOCKING/implementation fix is parked here.

## Migration sketch (NOT a wave plan)

A *sketch* of direction, not a sequenced plan (that follows acceptance + review):

1. **Name the two merge contracts in code (zero behavior change)** at their seams
   (`MergeEntity` append `component.go:1270` vs `MergeTriples` replace `helpers.go:101`). Stops the
   next independent rediscovery of the corruption.
2. **Build the base `OwnerRegistry` over the shared `OWNER_CLAIMS` bucket** (Decision 2/5):
   `OwnerClaim` + `ForeignEdgeClaim`, `RegisterOwner`, the epoch-CAS overlap check
   (read-epoch → compact → check → merge → CAS-write → retry-on-mismatch, plus the
   post-registration Watch + heartbeat/TTL staleness + Owner×FE cross-type check), and the
   structured `CoordinationWaiver`. Have `lifecycle.Manager` embed it.
3. **Add schema-derived owned-set removal** to `update_with_triples` (Decision 3 / 3a, nil-guarded),
   with the wire-level escape hatch for unregistered owners.
4. **Build the T2-seam foreign-edge enforcement** (Decision 4): the unclaimed-foreign-edge
   reject at `ingestEntity`/`partitionTriplesBySubject` (`component.go:917-955,1031-1040`),
   the boot-time `ForeignEdgeClaim` validation + inverse-gate, the KV-backed `PENDING_EDGES`
   buffer with **delete-after-apply + boot re-drain sweep**, the Backfill floor, the
   **fourth-path fold** (`ensureReferencedEntityExists` → enqueue), and **wire `WithInverseOf`
   for sensorml's `PredHosts`/`PredIsHostedBy`** (`parser/sensorml/predicates.go:96-97`). The
   GATE on ADR-055's closing move is the green **counting crash-recovery test**.
5. **Resolve the `coordinator.decision.*` collision** (Decision 2 resolution (a)) — AFTER the
   Wave-1 mode reclassification flips those writes to `replace-owned` (the check is exempt-on-
   append until then), as the existence proof that overlap-rejection works.
6. **Let ADR-055 enforce its narrowed rule** (must-exist flip — both `AddTriple`/`AddTriples`
   branches deleted AND the fourth path covered) as its closing move — gated on Decision 4.
7. **Migrate one product consumer** (semspec's `planStore`) off the hand-rolled
   `UpsertEntityIfChanged`/3-layer mirror onto the framework reconcile (escape hatch → registry),
   as the prospective-value existence proof.
