# ADR-042: OASF Taxonomy Adoption via `vocabulary/oasf` Sub-Package

## Status

**Proposed — 2026-05-14.** Tag scope: TBD. Resolves the PR-C blocker
recorded in `project_pr70_deferred_review_findings.md` (`OASFSkill.ID`
string vs AGNTCY proto `Skill.id` uint32 schema mismatch).

Forcing function: PR #70 (gRPC directory backend) landed the wire path
but exposed that our `oasf-generator` emits records that don't validate
against AGNTCY's typed OASF v1 proto schema. Without resolution, the
PR-C wire-up would publish records the hosted hub rejects at decode
time. The deeper question — how does SemStreams relate to OASF's
published skills taxonomy — has been deferred since PR #65 added the
generator. This ADR settles it.

## Context

### What OASF actually defines

OASF (Open Agentic Schema Framework, governed by AGNTCY) defines skills
as a **hierarchical numeric taxonomy** with two paired fields per skill:

| Field | Type | Example | Purpose |
|---|---|---|---|
| `id` | `uint32` | `10101` | Class identifier in the taxonomy hierarchy |
| `name` | `string` | `natural_language_processing/natural_language_understanding/contextual_comprehension` | Hierarchical path identifier matching the numeric class |

IDs encode hierarchy by digit count:

- 1-digit category (`1` = NLP, `5` = Coding)
- 3-digit subcategory (`101` = Natural Language Understanding, `502` = Coding Skills)
- 5-digit specific skill (`10101` = Contextual Comprehension, `50201` = Text to Code)

The taxonomy is published at `schema.oasf.outshift.com` with a browseable
class registry. OASF v1 also supports an extension mechanism for skills
outside the published taxonomy (the proto reserves space for it; details
in [Extensions strategy](#extensions-strategy) below).

### What SemStreams emits today

`processor/oasf-generator/mapper.go` translates triples to OASF records.
The current field mapping:

| SemStreams predicate | OASF field (current) | OASF spec expectation |
|---|---|---|
| `agent.capability.expression` (e.g., `"software-design"`) | `skills[].id` | **wrong slot**; this is a string slug, not a taxonomy class ID |
| `agent.capability.name` (e.g., `"Software Design"`) | `skills[].name` | **wrong format**; OASF wants hierarchical paths like `engineering/software_design` |

`OASFSkill.ID` is typed `string` in `processor/oasf-generator/oasf_types.go`,
which decoded fine against our own `MockDirectory` (HTTP backend) and
own contract test golden — but fails AGNTCY's typed proto decode the
moment we try to publish to a conformant directory:

```text
failed to unmarshal JSON to Go struct field Skill.skills.id of type uint32
```

### Why the gap exists

Our internal capability model treats `Expression` as a free-form
semantic slug — useful for downstream graph reasoning (rule matching,
BM25, embeddings) where the string content carries meaning. OASF's
taxonomy treats skills as numeric class membership — useful for cross-
agent discovery and standards conformance. These are not the same
abstraction; the generator's current "expression → id" mapping
collapses them and gets neither right.

### Existing pattern: the `vocabulary/` package

SemStreams already uses a pragmatic-semantic-web pattern for standards
adoption. The `vocabulary/` root package (see
`vocabulary/standards.go`) declares constants for established W3C and
semantic-web vocabularies — OWL, SKOS, RDF Schema, Dublin Core,
Schema.org, PROV-O, FOAF, SSN/SOSA. Sub-packages (`vocabulary/agentic`,
`vocabulary/bfo`, `vocabulary/cco`) hold richer per-vocabulary surfaces.
Each follows the same shape:

- Dotted-notation predicate constants for internal NATS-friendly use
- IRI/identifier constants for external export and standards mapping
- A `Register()` helper that links the two via the predicate registry

This pattern explicitly serves the sponsor's "standards at work"
mandate: adopted vocabularies are visible in the code, exported in
JSON-LD/Turtle, and traceable to the W3C or IETF spec they implement.

The OASF taxonomy is the natural next entry.

## Decision

**Add a `vocabulary/oasf/` sub-package that mirrors the established
pattern, and refactor `processor/oasf-generator` to source skill `id` +
`name` from it. Custom skills outside the published taxonomy use a
documented extension ID range.**

### `vocabulary/oasf/` package shape

```text
vocabulary/oasf/
├── doc.go                   # package documentation; sponsor-facing
├── categories.go            # 1-digit category constants + names
├── subcategories.go         # 3-digit subcategory constants + names
├── skills.go                # 5-digit skill class constants + names
├── extensions.go            # extension-ID scheme (see below)
├── lookup.go                # ID ↔ name helpers; expression → class resolver
├── register.go              # registers with vocabulary registry
└── *_test.go                # golden test against published taxonomy snapshot
```

Constant shape mirrors `vocabulary/agentic` and `vocabulary/cco`:

```go
package oasf

// Categories (1-digit class IDs).
const (
    // ClassNLP is the top-level category for natural language processing skills.
    // OASF id: 1, name: natural_language_processing
    ClassNLP uint32 = 1

    // ClassCodingSkills is the top-level category for code-related skills.
    // OASF id: 5, name: coding_skills
    ClassCodingSkills uint32 = 5

    // ClassToolUse is the top-level category for tool-use skills.
    // OASF id: 14, name: tool_use
    ClassToolUse uint32 = 14
    // ...etc
)

// Specific skills (5-digit class IDs).
const (
    // SkillContextualComprehension is the OASF class for understanding text in context.
    // Hierarchy: natural_language_processing/natural_language_understanding/contextual_comprehension
    SkillContextualComprehension uint32 = 10101

    // SkillTextToCode is the OASF class for generating code from natural language.
    // Hierarchy: coding_skills/code_generation/text_to_code
    SkillTextToCode uint32 = 50201

    // SkillToolUsePlanning is the OASF class for planning tool invocations.
    // Hierarchy: tool_use/planning
    SkillToolUsePlanning uint32 = 1403
    // ...10 total at MVP; selection methodology in "Decisions on initial scope" below
)

// Name resolves a class ID to its OASF hierarchical name.
// Returns the empty string for unknown IDs.
func Name(id uint32) string { /* ... */ }

// ID resolves a hierarchical name to its OASF class ID.
// Returns 0 (not a valid OASF ID) for unknown names.
func ID(name string) uint32 { /* ... */ }
```

Initial coverage: the 5–10 specific skills our internal capability
expressions actually map onto today (planning, tool use, code analysis,
documentation). Coverage grows organically as new capabilities surface,
keyed off the published taxonomy.

### Extensions strategy

OASF supports custom skill classes outside the published taxonomy via
its extension mechanism. We treat any skill we cannot resolve to a
published class as an extension and assign it a synthetic ID from a
reserved local-extension range:

```go
package oasf

// ExtensionBase is the ID range reserved for SemStreams custom skill
// classes. The published OASF taxonomy uses IDs in low ranges (1–4 digit
// categories, 3–5 digit subcategories and skills); we use 7-digit IDs
// >= 9_000_000 to avoid collision with future taxonomy growth.
//
// Extension records also carry the `extensions` field with provenance
// (source = "semstreams", taxonomy = "internal"), so consumers can
// distinguish extension classes from canonical OASF classes.
const ExtensionBase uint32 = 9_000_000
```

The hierarchical `name` for extension skills follows the OASF convention
but lives under a `semstreams/` prefix so it's visibly extension-scoped:
`semstreams/robotics/drone_telemetry`, `semstreams/ops/anomaly_review`.

### Generator refactor

`processor/oasf-generator/mapper.go` changes:

1. **Field types**: `OASFSkill.ID` becomes `uint32`. (BREAKING for the
   existing contract test golden and any direct consumer of the type;
   none in `cmd/`, `processor/`, or `gateway/` per grep.)
2. **Field mapping**: `agent.capability.expression` resolves through
   `oasf.ID(...)` to populate `Skill.id`. The hierarchical name comes
   from `oasf.Name(id)`. If the expression doesn't match any published
   class, the mapper assigns an extension ID and a `semstreams/`-prefixed
   name.
3. **Contract test golden**: `processor/oasf-generator/testdata/oasf_record_golden.json`
   regenerates with numeric IDs and hierarchical names.
4. **Operator override** (optional, lands in same PR or follow-up): a
   triple predicate `agent.capability.oasf_class` lets operators pin a
   specific OASF class ID per capability, bypassing the lookup. Useful
   when the published taxonomy adds a class our internal expression
   doesn't auto-match.

### Wire-format consequence

After the refactor, records emitted by `oasf-generator` validate
against `corev1.UnmarshalRecord` (the typed proto decoder we tried to
adopt during PR #70 review and reverted). The `oasfToProtoRecord`
helper in `output/directory-bridge/grpc_backend.go` can be migrated
back to using `corev1.UnmarshalRecord` for early-fail validation, as
the original go-reviewer finding suggested.

PR-C wire-up (config + OIDC + factory selector) is unblocked.

## What this is NOT

To stay out of semweb hell, this ADR explicitly does **not**:

- Adopt OWL inferencing, RDF Schema reasoning, or any RDF semantics
  beyond what the existing `vocabulary/` package already does (constants
  + export, no reasoner).
- Require operators to author taxonomy mappings in RDF, Turtle, or
  JSON-LD. Configuration is plain JSON/Go.
- Introduce SPARQL or any query layer over OASF taxonomy.
- Make OASF the only emission format. The `output/directory-bridge`
  `HTTPBackend` keeps emitting our internal shape for the SemStreams
  mock and any privately-hosted HTTP directory; only the
  AGNTCY-targeted `GRPCBackend` requires taxonomy conformance. (Future
  ADR can decide whether to make the HTTP shape match too.)
- Lock SemStreams into OASF v1 specifically. The `vocabulary/oasf/`
  package can grow `v1alpha1.go`, `v2.go` etc. as the standard evolves;
  the `schema_version` discriminator in the record already routes
  decoders accordingly.

## Alternatives considered

### A. Translator layer in `output/directory-bridge` (no internal change)

Keep `OASFSkill.ID` as `string` internally; add a translator in the
gRPC backend that maps to AGNTCY's typed schema only at the wire
boundary. Localized change, no impact on the generator or downstream
consumers of our types.

**Rejected** because it implicitly says "OASF is the export format, our
internal model is something else" — exactly the framing the sponsor's
"standards at work" mandate argues against. A standards-positive
architecture treats the standard's types as our types where the
standard owns the domain.

### B. Pre-flight log-warn only

Try `corev1.UnmarshalRecord` at marshal time, log a warning if it
fails, send the malformed record anyway. Trivial scope; doesn't fix
the underlying rejection at the hub.

**Rejected** because it's not standards-conformant — it's
standards-observable. The hub still rejects every record.

### C. String IDs upstreamed to OASF

File an issue/PR with AGNTCY asking the proto Skill schema to accept
`oneof { uint32 id; string id; }`. Maximal "standards participation"
story.

**Rejected (for now)** because the published spec is unambiguous: IDs
are taxonomy class numbers, not arbitrary strings. The hierarchical
`name` field already serves the string-identifier use case. Adopting
the spec as-written is more aligned with sponsor expectations than
campaigning to change it. We can revisit if our adoption reveals a
real expressivity gap.

## Consequences

### Positive

- **Standards-conformance is real, not nominal.** Records emitted by
  `oasf-generator` validate against the published OASF v1 schema.
- **Sponsor demonstration story is strong.** The
  `vocabulary/oasf/` package is a visible code artifact that says "we
  adopt and surface the OASF taxonomy as first-class vocabulary,"
  matching the existing W3C SAC-CG, BFO, and CCO adoptions.
- **PR-C unblocked.** The schema mismatch in
  `project_pr70_deferred_review_findings.md` finding #1-revert
  resolves.
- **No semweb hell.** Constraints in [What this is NOT](#what-this-is-not)
  keep the scope bounded to constants + lookup helpers + JSON-friendly
  config. No reasoners, no SPARQL, no triple-store-as-database.

### Negative / costs

- **BREAKING** for the `processor/oasf-generator` contract test
  golden and for any external consumer of `OASFSkill.ID` as a string.
  Grep confirms zero callers outside the generator + directory-bridge,
  both of which we control.
- **Curation cost.** The `vocabulary/oasf/` package needs ongoing
  maintenance as the published taxonomy evolves. Mitigation: snapshot
  test against `schema.oasf.outshift.com` so drift is visible.
- **Expression → class resolver is imperfect.** Some internal
  capability expressions map cleanly (`code-review` → SkillCodeReview
  if it exists in taxonomy); others don't and fall to extensions.
  Operators can override via the `agent.capability.oasf_class`
  predicate where they need precise control.

### Risks

- **Taxonomy version drift.** If AGNTCY publishes a v2 taxonomy with
  reorganized IDs, our snapshot lags. Snapshot test + the
  `schema_version` field on records (already populated as "1.0.0") give
  us version isolation; the registry pattern supports multiple
  versions side-by-side.
- **Sister-project ripple.** semspec and semteams do not import
  `processor/oasf-generator` directly (confirmed via grep), so the
  `string → uint32` field type change does not propagate. They use the
  `vocabulary/agentic` package for capability *predicates*, which is
  not affected by this ADR.

## Implementation plan (concrete)

This ADR proposes the design; the implementation lands in a small
chunked PR series:

1. **PR-O1 (this ADR + scaffold)** — Add `vocabulary/oasf/` package
   with category + subcategory constants, the extension scheme, and
   the `Name`/`ID` lookup helpers. Initial skill class coverage limited
   to the 5–10 our generator actually emits today. Snapshot test
   against the published taxonomy.

2. **PR-O2 (generator refactor)** — Switch `OASFSkill.ID` to `uint32`.
   Wire `mapper.go` through `oasf.ID(...)`/`oasf.Name(...)`.
   Regenerate the contract test golden. Update
   `processor/oasf-generator/oasf_types.go` and any callers (limited
   blast radius per ADR analysis).

3. **PR-O3 (gRPC backend uses typed decode)** — Migrate
   `output/directory-bridge/grpc_backend.go::oasfToProtoRecord` back
   to `corev1.UnmarshalRecord` (the path go-reviewer originally
   recommended in PR #70, reverted pending this work).

4. **PR-C (deferred, unblocked by O1–O3)** — Config selector,
   OIDC token provider, factory wire-up.

5. **PR-D (deferred, after PR-C)** — Opt-in hosted-sandbox e2e stage.

## Related

- ADR-XXX-W3C-SAC-CG adoption (whichever ADR introduced
  `vocabulary/agentic`) — same pattern, same sponsor motivation.
- PR #70 (gRPC backend) — surfaces the gap.
- `project_pr70_deferred_review_findings.md` — finding #1-revert
  closes when PR-O2 lands.
- `feedback_polymorphic_config_needs_json_roundtrip_test` — the
  `agent.capability.oasf_class` override predicate is operator-reachable
  and needs the JSON-round-trip test treatment.

## Decisions on initial scope

### Initial skill class coverage: 10 for MVP

PR-O1 ships with **exactly 10 OASF skill class constants**, chosen by
this methodology (executed during PR-O1, not pre-committed in this
ADR):

1. Grep all `agent.capability.expression` values across `configs/*.json`,
   `processor/oasf-generator/*_test.go`, `test/e2e/scenarios/agentic/`,
   and any persona/role configs in sister projects (semspec, semteams)
   that ship capability triples.
2. Bucket by frequency.
3. Pick the top 10 distinct internal expressions.
4. For each, resolve to the closest published OASF class via the
   schema server at `schema.oasf.outshift.com`. Document the mapping
   inline in the constant's godoc.
5. If fewer than 10 internal expressions exist (we're MVP-scale), pad
   the package with the foundational categories an agent framework
   inevitably uses — `coding_skills`, `tool_use_planning`,
   `natural_language_understanding`, `data_analysis` — so the
   constant set is useful out of the gate.

The number 10 is the MVP floor, not a cap. The package grows
organically as new capabilities are introduced; each addition is a
two-line `const SkillXxx uint32 = NNN` change.

### Operator override predicate: `agent.capability.oasf_class`

Lives under our existing `agent.capability.*` namespace, with `oasf_`
as the field-level prefix that names the external vocabulary being
referenced. This matches the established pattern: SemStreams owns the
`agent.*` namespace and our pragmatic dotted-notation internals;
external-vocabulary references appear as named fields under our
concepts rather than as triples in someone else's namespace.

Rationale (this is the "sniff test" SemStreams-as-framework playing
nicely with external refs):

- ✅ Reads naturally — "this capability has an OASF class of 10101"
- ✅ Discoverable — anyone scanning the `agent.capability.*` constants
  finds the OASF override sitting alongside `name`, `description`,
  `expression`, `confidence`, `permission`
- ✅ Multi-standard ready — if we later add SAC-CG capability class
  refs or other taxonomies, the same shape works:
  `agent.capability.sac_cg_class`, etc.
- ❌ Rejected: top-level `oasf.skill.class` predicate — would imply
  the triple speaks OASF natively, blurring the "we reference, we don't
  pretend to be the standard" boundary

### HTTP backend conformance

`HTTPBackend` automatically inherits the new numeric-ID shape since
both backends share `processor/oasf-generator`. No special handling
required; downstream mock + private HTTP directory consumers are
internal-only and we can update them in lockstep. Tracked as a
non-issue rather than an open question.
