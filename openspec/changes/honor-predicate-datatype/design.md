# Design: honor the registry's declared predicate DataType (#1267)

base: `b57a6912` · inventory: `openspec/changes/honor-predicate-datatype/inventory.md` (150 pins, `task
inventory:verify` green at 150/150 against base `232b1e7d`, re-run 2026-09-07 — `changed since base: (none)`).

Owner ruling (2026-09-07, verbatim on #1267): *"agree - we need to honor them. and now is the time to establish best
practices with our vocab/datatype."* Decision A: **honor**. The seven-item scope on the issue is the owner's framing
of what "honor" means; each line below is answered, not re-litigated.

**Status: DRAFT, pre-review.** Not approved. Per `.agents/contracts/semstreams-architect.md` §Required workflow this
design was written before an `INVENTORY PASS` was recorded on PR #1269, at the launching session's direction; the
gate is still owed and the design is contingent on it. See "Process note" at the end.

---

## Owner questions (answer these first; the rest of the design assumes the recommendation)

**Q1 — The closed set is RDF-shaped, not Go-shaped.** The field is documented today as "the expected **Go type** for
the object value" (`vocabulary/predicates.go:359`, `vocabulary/registry.go:111`). This design retires that axis: the
seven canonical values are XSD/RDF local names (`string`, `integer`, `double`, `boolean`, `dateTime`) plus the
repository's own two non-XSD markers (`@id`, `rdf:JSON`). Go type names (`float64`, `time.Time`, and semdragon's 26
struct names) become legacy spellings that normalize or are refused. *Recommend: adopt.* Rationale in §2; this is the
single decision the whole design rests on.

**Q2 — The migration bill is bigger than #1267 measured, and it is a sister boot break.** The issue counted eight
in-repo spellings. The family-wide union is **42 distinct spellings across 950+ call sites** (measured today,
read-only; §1.2). Sixteen map cleanly. **Twenty-six do not** — all semdragon, all Go payload struct names
(`PeerReviewPayload`, `GuildRankPayload`, … `Lesson`), 30 call sites, all in the single file `domain/vocab.go`. Under this design those become an
init-time panic when semdragon upgrades. *Recommend: proceed and accept it* — pre-v1, sisters pin their semstreams
version, the refusal is loud and at boot, and the migration is one file. But it is the owner's call whether a sister
boot break lands now or is staged behind a warn-only phase.

**Q3 — Item 7 fork: export-time only.** *Recommend: export-time only in this change; the ingest-time population of
`Triple.Datatype` is a different change about fusion edge semantics, filed separately.* Reasoning and the two
measurements that decide it are in §5. This is the answer the issue asked the architect to give, not pre-decide.

**Q4 — Item 6, `Units`/`Range`.** Family-wide adoption measured today: **zero** `WithUnits` in any sister, **one**
`WithRange` in any sister (semspec, `"0-100"`); 12 and 11 in-repo. Three options in §7. *Recommend: declare them
documentation-only in the spec and in their doc comments — a smaller promise the framework actually keeps — and file
"honor Units/Range" against #1264 so the decision has a home.* The issue's words are "otherwise decided explicitly";
this is that decision, and the spec delta's third requirement is contingent on this ruling. **If the owner rules
otherwise, strike that requirement — do not adapt it.**

**Q5 — `Role` scope (inventory addendum A4).** A fourth write-only field on the same frozen struct, whose doc comment
(`vocabulary/registry.go:227`) asserts a consumer that reads a different field. #1267's scope does not name it. §8
frames three options. *Recommend: the one-line doc-comment correction inside this change (it is a false statement on a
line this change already touches), and the `Role` closed-set validator as a separate owner decision.* Explicitly NOT
folded in silently either way.

**Q6 — ADR-074 or a new ADR.** Item 5 says "ADR-074 gains the datatype rule as a decision". The repo's own policy
(`CLAUDE.md`, `docs/adr/README.md`) is that existing ADRs stay as history and mechanics live in the capability spec.
This IS a cross-repo contract (it changes what value semsource republishes on a wire payload), so it earns an ADR.
*Recommend: a new one-page ADR-107 cross-referencing ADR-074, with the mechanics in the `predicate-contract` delta —
not an edit to ADR-074.* Named because it deviates from the issue's literal wording.

**Q7 — #1142 rides.** *Recommend: yes, with `Closes #1142` on the PR.* Reasoning in §6. Flagged because it puts two
issues on one PR.

---

## 1. Premises, each with its measurement

Every claim this design rests on, stated as something that was measured. Inventory pins are cited by their file:line;
new measurements taken today are given with the command.

### 1.1 What the inventory already established (not re-derived)

- The three fields exist and are written: `vocabulary/predicates.go:360,363,366`; `vocabulary/registry.go:114,122,130`.
- No in-repo production reader of any of them (inventory § Spellings of the fact, re-derived with a corrected
  pattern — `-w` and `\b` silently match nothing under `git grep -E`).
- `validatePredicateMetadataLocked` (`vocabulary/registry.go:381`) is reached from BOTH entry points —
  `Register` at `:308`, `RegisterPredicate` at `:371` (addendum A2). One seam covers both; no `WithDataType`
  signature change is needed, which is exactly the owner's constraint.
- `Register` amends rather than replaces (`vocabulary/registry.go:299`, gh#410) — a re-registration can inherit a
  datatype it never declared (addendum A3).
- `AliasType` is in-package prior art: a closed typed vocabulary validated at the same seam (`registry.go:12,24,406,408`).
- `message.Triple.Datatype` already beats Go-type inference in export (`vocabulary/export/object.go:47-48`).
- `vocabulary` and `vocabulary/export` are both Tier 1 frozen (`release/tier1-packages.txt:95,100`).
- semsource reads `.DataType` in production and republishes it as `data_type` on a wire payload (addendum A1) — the
  issue's "no sister reader" premise is FALSE, and this is the central adopter-seam fact of the change.

### 1.2 New measurements taken for this design (read-only; sister `git status --porcelain` byte-identical before and after)

**M1 — the family-wide spelling union is 42, not 8.** `for r in <15 repos>; do git -C $r grep -h -o -E
'WithDataType\("[^"]*"\)' -- '*.go'; done | sed -E 's/.*\("(.*)"\).*/\1/' | sort | uniq -c | sort -rn` →
`string` 554, `entity_id` 126, `int` 70, `datetime` 53, `float64` 21, `bool` 21, `array` 20, `json` 13, `time.Time`
10, `float` 10, `timestamp` 8, `int64` 2, `entity_ref` 1, `reference` 1, `number` 1, `boolean` 1, **plus 26 Go payload
struct names in semdragon** (`PeerReviewPayload` 3, `GuildRankPayload` 2, `GuildMemberPayload` 2, and 23 more at 1 each
— **30 call sites, 26 distinct names, all in `domain/vocab.go`**). Distinct count family-wide: **42**.

Units, stated because the two columns of the mapping table in §2.3 differ: the **in-repo** column is the
gopls-exact non-test code call-site count (195 total, inventory § Call-site census); the **family** column is raw
literal occurrences across all fifteen repos including `_test.go` files and the four SemStreams doc-comment example
lines (`git -C semstreams grep -h -o -E 'WithDataType\("[^"]*"\)' -- '*.go'` → 202; `':!*_test.go'` → 199).
The two columns are therefore not subtractable for the `string` and `float64` rows; every other row agrees.

Three of those spellings are new information the issue did not have: `entity_id` at 126 uses versus the in-repo
`entity_ref` at 1 (the minority spelling is ours); `array`/`json` at 33 uses, which have **no XSD scalar mapping at
all**; and semdragon's 26 Go type names, which are not datatypes in any sense the framework can honor.

**M2 — struct-literal writers exist and were missed by a `WithDataType` sweep.** `git grep -n -E
'(DataType:[ ]*"|DataType[ ]*=[ ]*")' -- '*.go'` per repo → semteams `cmd/semteams/vocab/vocab.go` 34 sites
(`string` 31, `number` 3), semmachina `internal/vocabulary/registration.go:128` (`= "string"` default). Both route
through `RegisterPredicate` → the same validator, so §3's seam covers them. *(POSIX ERE: the first attempt used `\s`
and returned a false zero — the documented `git grep -E` trap, re-run and corrected.)*

**M3 — three sisters register with NO datatype.** semlink `internal/cop/contracts.go:91`,
`internal/projector/contracts.go:105`, `internal/rules/contracts.go:46`, all
`RegisterPredicate(vocabulary.PredicateMetadata{Name: predicate})`. **Empty must therefore stay legal** or three
sisters panic at boot for no benefit.

**M4 — `Units`/`Range` have essentially no family adoption.** `git grep -h -o -E 'With(Units|Range)\("[^"]*"\)'`
across all five populating sisters → **zero** `WithUnits`, **one** `WithRange` (semspec, `"0-100"`). Against 755+
`WithDataType` sites. The two fields are not the same problem at the same scale.

**M5 — a real import cycle forbids sharing a Go constant.** `payloadregistry/registry.go:31` imports `vocabulary`
(used at `:144`, `vocabulary.IsValidIndexingProfile`), and `message/base_message.go:10` imports `payloadregistry`.
So `vocabulary` → `message` → `payloadregistry` → `vocabulary` is a cycle. The core `vocabulary` package cannot import
`message` to reuse `message.EntityReferenceDatatype`; only `vocabulary/export` can (it does, `object.go:9`). The
two-constants-plus-contract-check shape in §2 is **forced by this measurement, not chosen**.

**M6 — the `@id` export defect (larger than #1142, and not in the inventory).**
`vocabulary/export/object.go:57-63`, `classifyWithExplicitDatatype`, returns `kind: objectLiteral`
**unconditionally**, and `expandDatatypePrefix` (`:166-176`) returns `"@id"` unchanged because it knows only `xsd:`
and `://`. So a triple carrying `message.EntityReferenceDatatype` serializes as a literal with a non-IRI datatype:
`"acme.ops.gcs.robotics.drone.001"^^@id`. That is invalid RDF. It is reachable production state, not a hypothetical:
`graph/entity_predicate_contract.go:168` validates `@id` triples at the authoritative persistence seam,
`pkg/fusion/engine_graph.go:319` projects them as edges, and semconnect serializes ENTITY_STATES triples through this
package (`gateway/cs-api/systems.go:746`). `git grep -n '@id' -- vocabulary/export/` returns only JSON-LD document
keys and zero handling of the marker; there is no test for it.

**M7 — the same trap one prefix over.** `vocabulary/agentic/predicates.go:962` defines
`TodoRecordJSONDatatype = "rdf:JSON"`, written onto real triples at `processor/agentic-tools/write_todos.go:361`.
`expandDatatypePrefix` does not know `rdf:` either, so it emits `^^rdf:JSON` — also not an IRI.

**M8 — the in-package prior art for the exact constant shape.** `vocabulary/predicates.go:304-319` declares
`IndexingProfileContent`/`Control`/`Signal`/`Trace` as **untyped string constants**, with the closed-set checker
`IsValidIndexingProfile` at `:325-332`, consumed from another package's registration seam
(`payloadregistry/registry.go:144`). This is closer to the target shape than `AliasType`, which uses a named type.

---

## 2. The design

### 2.1 The reframe: the declaration names what observation cannot recover

The field has been write-only for its whole life, and the reason is legible in its own doc comment: it asks the
declaring author to predict **the Go type of the object value**. That is a prediction of a fact the framework already
holds — `classifyByGoType` (`vocabulary/export/object.go:66-95`) observes the Go type directly at serialization time —
and it is a prediction the author *cannot get right*, because the value they declared as `int` comes back out of
`ENTITY_STATES` as `float64` no matter what they wrote. Forty-two spellings, twenty-six of them Go struct names, is
the measured cost of asking for a prediction nobody can make.

So the closed vocabulary carries **only what observation cannot recover**:

| The fact | Can the serializer observe it? | In the closed set? |
|---|---|---|
| This string is an entity reference | No — a 6-part ID and a plain string are both `string` | **Yes** (`@id`) |
| This number is semantically an integer | No — the JSON round trip erased it to `float64` | **Yes** (`integer`) |
| This string holds a structured document | No — it is a `string` | **Yes** (`rdf:JSON`) |
| This value is a float / bool / timestamp / plain string | Yes, from the Go type | Yes, but as a *confirmation*, never a requirement |
| This value is a `PeerReviewPayload` | No, and RDF has no rendering for it | **No** — refused |

The Go-type column of the rendering table below is documentation. It is never a spelling anyone types again.

### 2.2 The closed set — seven untyped string constants

Untyped, in `vocabulary/predicates.go` beside `IndexingProfile*` (M8). **Not** a named type: `WithDataType` keeps its
`string` parameter by the owner's constraint, and `PredicateMetadata.DataType` is a `string` field written directly by
semteams and semmachina (M2), so a named type would break both the option call and the struct literal.

Every value is a spelling **this repository already uses for that exact fact** — nothing is invented:

| Constant | Value | Where the value already exists | Go values it describes | XSD / RDF IRI | JSON Schema |
|---|---|---|---|---|---|
| `DataTypeString` | `string` | `xsdString`, `export/object.go:14` | `string` | `xsd:string` (omitted in output) | `{"type":"string"}` |
| `DataTypeInteger` | `integer` | `xsdInteger`, `:15` | `int`, `int8..64`, `uint*`, and a `float64` with integral value after the JSON round trip | `xsd:integer` | `{"type":"integer"}` |
| `DataTypeDouble` | `double` | `xsdDouble`, `:16` | `float32`, `float64` | `xsd:double` | `{"type":"number"}` |
| `DataTypeBoolean` | `boolean` | `xsdBoolean`, `:17` | `bool` | `xsd:boolean` | `{"type":"boolean"}` |
| `DataTypeDateTime` | `dateTime` | `xsdDateTime`, `:18` | `time.Time`, RFC 3339 `string` | `xsd:dateTime` | `{"type":"string","format":"date-time"}` |
| `DataTypeEntityRef` | `@id` | `message.EntityReferenceDatatype`, `message/triple.go:15` | canonical 6-part entity-ID `string` | **resource (an IRI), not a literal** | `{"type":"string","format":"semstreams-entity-id"}` |
| `DataTypeJSON` | `rdf:JSON` | `agentic.TodoRecordJSONDatatype`, `vocabulary/agentic/predicates.go:962` | `string` holding a JSON document | `rdf:JSON` (`…22-rdf-syntax-ns#JSON`) | `{"type":"string","contentMediaType":"application/json"}` |

Deliberately absent: `decimal`, `float`, `date`, `duration`, `anyURI`. Each would be a new exported symbol with **zero
consumers at birth**, which the architect contract's category 4 rules out. #1264's SHACL sketch mentions `decimal`;
#1264 is a post-v1 candidate and supplies a motive, not a consumer.

`@id` shares its value with `message.EntityReferenceDatatype` **exactly**, so the per-predicate declaration and the
per-triple hint are one value space rather than two that must be translated. M5 makes a shared Go constant impossible,
so a `test/contract/` equality check enforces it — the `TestNATSVersionIsConverged` shape
(`test/contract/nats_version_contract_test.go`), which exists for precisely this "two homes, one asserted value"
situation.

### 2.3 The legacy mapping — normalize once at declaration, never at read

| Legacy spelling | in-repo sites | family sites | → canonical |
|---|---|---|---|
| `string` | 140 | 554 (+31 struct-literal) | `string` |
| `int` | 14 | 70 | `integer` |
| `int64` | 1 | 2 | `integer` |
| `float64` | 17 | 21 | `double` |
| `float` | 0 | 10 | `double` |
| `number` | 0 | 1 (+3 struct-literal) | `double` |
| `bool` | 4 | 21 | `boolean` |
| `boolean` | 0 | 1 | `boolean` |
| `time.Time` | 10 | 10 | `dateTime` |
| `timestamp` | 8 | 8 | `dateTime` |
| `datetime` | 0 | 53 | `dateTime` |
| `entity_ref` | 1 | 1 | `@id` |
| `entity_id` | 0 | 126 | `@id` |
| `reference` | 0 | 1 | `@id` |
| `json` | 0 | 13 | `rdf:JSON` |
| `array` | 0 | 20 | `rdf:JSON` |
| 26 Go payload struct names (semdragon `domain/vocab.go`) | 0 | 30 | **REFUSED** |
| *(absent)* | — | 3 semlink sites | *(stays absent — legal)* |

`array` → `rdf:JSON` loses "it is a list". That is honest: RDF has no scalar list datatype, the objects are JSON
documents in practice, and inventing an `array` datatype with no consumer is the category-4 mistake. Named as a lossy
mapping so it is not discovered later.

**Why this is not the alias table `predicate-contract` already forbids.** Requirement *Canonical predicate enforcement
is unconditional* (`openspec/specs/predicate-contract/spec.md:102-108`) prohibits "a permissive runtime mode,
compatibility alias, deprecated predicate table, dual read/write path, or configuration escape hatch". That
prohibition is about **predicate names**, and it is about **runtime**. This mapping is a declaration-time normalizer:
the registry stores only the canonical value, nothing persists a legacy spelling, and no reader ever observes one. The
spec delta states that explicitly so the distinction is a contract rather than a reading. It is the same shape as
`expandDatatypePrefix` (`export/object.go:166-176`), which normalizes at a boundary and stores nothing.

### 2.4 Where enforcement lives

`validatePredicateMetadataLocked` (`vocabulary/registry.go:381`), signature changed to take `*PredicateMetadata` so it
can normalize in place. It is private, so the change costs nothing externally; both entry points already call it
(`:308`, `:371`); and that is what makes the struct-literal path — semteams' 34 sites, semmachina's default, semlink's
three empty registrations — covered by the same rule as the option path. Enforcing inside `WithDataType` would leave
that path open.

**Refusal is a panic**, matching everything else this validator does (`Register` panics at `:309-311`,
`RegisterPredicate` at `:372-374`). The message names the offending value AND the accepted set, because the adopter
who wrote `"PeerReviewPayload"` needs to be told what to write instead, not that they were wrong.

**Empty is accepted** (M3).

### 2.5 Invariants (the only admissible source for the property harness)

Each cites the spec delta requirement that makes it true. A property authored later by reading the implementation
reconstructs it and proves nothing.

| # | Invariant | Spec home |
|---|---|---|
| I1 | For every registered predicate, `DataType ∈ closedSet ∪ {""}` | *A declared predicate datatype comes from one closed RDF-shaped vocabulary* |
| I2 | `canonical` is idempotent: `canonical(canonical(x)) = canonical(x)` — this is what makes amend-registration (A3) safe | same, scenario *an amending re-registration keeps its inherited canonical value* |
| I3 | `canonical` is total on `closedSet ∪ legacyMap` and errors elsewhere; no third outcome | same, scenario *an unrecognized spelling is refused at declaration* |
| I4 | `GetPredicateMetadata(p).DataType` never returns a legacy spelling, for any `p`, at any time | same, scenario *a recognized legacy spelling normalizes once at declaration* |
| I5 | Export classification is a total order: per-triple datatype ≻ declared datatype ≻ Go-type observation ≻ invalid | *RDF export honors the declared datatype and never fabricates a value* |
| I6 | The emitted lexical form always round-trips to the observed `Object`; a declaration the observed value contradicts is ignored for that triple | same, scenario *a declaration the value contradicts is ignored, not applied* |
| I7 | Any object carrying `@id`, from either scope, whose value is a canonical entity ID emits as an IRI, never as a literal | same, scenario *an entity reference serializes as a resource* |
| I8 | An absent declaration leaves export output byte-identical to today | same, scenario *an undeclared predicate serializes by observation* |

---

## 3. Options considered

**Option A — do nothing.** The field stays write-only and freezes that way at 1.0 under ADR-106. Cost: a free-form
string on a Tier 1 frozen struct can never be tightened later, and 42 spellings keep diverging. The owner ruled this
out; recorded because the contract requires the do-nothing option be priced.

**Option B — remove the three fields.** `apidiff` REMOVAL findings on Tier 1 `vocabulary`, 950+ call sites deleted
across seven repos, and it destroys real information (`entity_id` at 126 sites is a fact nothing else records).
Rejected: the owner ruled honor, and this is the more expensive option anyway.

**Option C — document a convention, validate nothing.** Zero enforcement, so drift resumes immediately; the issue's
scope item 3 explicitly asks for "best practice enforced, not documented". Rejected.

**Option D — closed set, hard reject, no legacy mapping.** Every non-canonical spelling panics. Bill: 41 of 42
spellings, ~950 call sites across seven repos, all at once. Rejected: it converts a correctness improvement into a
family-wide flag day for no additional correctness — 16 of the 42 spellings have an unambiguous canonical form, and
refusing them teaches the adopter nothing the mapping cannot.

**Option E (recommended) — closed set, normalize the 16 known spellings at declaration, refuse the rest.** Bill: 26
spellings in one file of one sister, plus the semsource wire-value change (§4). Everything else keeps compiling and
booting, and the value it reads back is canonical.

**Option F — extend the existing surface instead: reuse `message.Triple.Datatype` and delete
`PredicateMetadata.DataType` entirely.** Genuinely attractive — one home for one fact, which the contract prefers.
Rejected on measurement: the per-triple field answers "what is THIS value", the per-predicate field answers "what is
this PREDICATE always"; #1261's tool schema and #1264's affordance shapes need the second without holding any triple,
and M5's import cycle means the packages cannot even share a constant. Option E keeps two scopes over **one value
space**, which is the achievable form of "one home".

---

## 4. The adopter seam inventory (mandatory second deliverable)

Answered for a specific person: **the semspec vocabulary author.** 538 `WithDataType` call sites across ten files
(`vocabulary/{ics,observability,project,semspec,spec,workflow}/predicates.go`, `vocabulary/observability/
tool_recovery.go`, `vocabulary/source/{convention,git,predicates}.go`), ten distinct spellings, the largest bill in
the family. They have never opened `vocabulary/registry.go`.

### 4.1 What must they know?

**One thing: pick one of these seven constants.** That is the whole debt on the write path. No ordering, no wiring, no
threshold, no "call X before Y" — registration is still one call with one option, and `Register`'s amend semantics are
unchanged. Under the contract's two-item threshold.

The second thing they must *unlearn* is not a debt they carry, because the compiler and the mapping carry it for
them: `float64`, `float`, and `number` were three spellings of one fact, and they now cannot be spelled three ways.

### 4.2 What happens if they do nothing?

Three different paths, and they must be told apart honestly:

1. **They do not upgrade.** Nothing. Sisters pin their semstreams version in `go.mod`; this change reaches them when
   they choose. There is no instantaneous family break.
2. **They upgrade and their spelling is one of the 16 mapped ones** — which is all ten of semspec's spellings, all
   eight of semsource's, both of semconnect's, both of semboids' and semteams' — **it keeps compiling and booting**,
   and `GetPredicateMetadata(p).DataType` starts returning the canonical spelling. This is the **silent** path, and it
   is where the real bill lands (§4.3).
3. **They upgrade and their spelling is one of the 26 unmapped ones** — semdragon only — **init-time panic**, naming
   the value and the accepted set, before any data moves. Loud, at boot, one file.

### 4.3 Where do they find out?

Ranked honestly (compile error > boot error > typed runtime error > log line > doc > nowhere):

| Path | Rank | Assessment |
|---|---|---|
| Unmapped spelling | **boot error** | The best rank available. A compile error is impossible *by the owner's constraint* — `WithDataType` keeps its `string` parameter so 755 sister call sites survive — so registration-time refusal is the earliest surface that exists. |
| Mapped spelling, in-repo effect | **nowhere** | Deliberate: making normalization loud would panic 729 currently-correct call sites. The effect on the declaring author is nil, so silence is correct *for them*. |
| Mapped spelling, **semsource's wire consumer** | **doc** | **This is the finding.** |

**The finding, stated as a finding.** semsource reads `.DataType` at `processor/source-manifest/status.go:219,224` and
republishes it as `data_type` on `PredicateDescriptor` → `PredicateSchemaPayload`
(`payload_predicates.go:27-32`) — a published wire format. After this change, a semsource deployment emits
`"double"` where it emitted `"float64"`, and `"@id"` where it emitted `"entity_id"` (126 predicates). The person who
breaks is **a consumer of semsource's manifest payload — two repos away, who never called a SemStreams API, is in no
review here, and has no seam at which to find out.** `task api:compat` is structurally blind to it: no signature
moves, only emitted values do.

That residual is why `docs/operations/migration-predicate-datatype.md` is a deliverable of this change rather than an
afterthought, and why it must state the value-level before/after table and not just the constant list. Under the
repository ownership boundary, semsource's owner implements; SemStreams owes them the exact table.

### 4.4 What SHOULD they have to know?

**"Pick one of these constants."** Nothing else.

The design achieves that for the write path. The gap between 4.1 and 4.4 is **zero for the declaring author and
nonzero for a consumer two hops downstream**, and naming that asymmetry is the point of running this inventory: a
design that only checked the declaring author would have scored itself perfect and shipped a silent wire change.

### 4.5 Prefer observation to prediction

*Is this asking the caller to predict something the framework could observe?*

**Today: yes, and that is the whole defect.** `WithDataType("float64")` asks an author to predict the Go type of a
value the serializer observes directly (`classifyByGoType`, `export/object.go:66-95`), and which they cannot predict
correctly anyway — the authoritative store's `encoding/json` round trip rewrites every number to `float64`
(`message/triple.go:53` `Object any`; `graph/entity_predicate_contract.go:192,256`; no custom `Triple` JSON codec
exists, 0 hits). The prediction was doomed at the seam, and 42 spellings including 26 Go struct names are the
measured receipt.

**After: no.** The closed set names only the three facts observation cannot recover (§2.1). Where the declaration and
the observation disagree, **observation wins** (I6) — the framework absorbs the error instead of emitting a value the
author predicted wrong. The knob that gets deleted is the Go-type axis itself: `float64` / `float` / `number` /
`time.Time` / `PeerReviewPayload` were all attempts to answer "what Go type is this", and after this change nobody
answers that question again.

---

## 5. Item 7 answered: export-time only

The issue hands the architect the fork: **ingest-time coercion** (store a declared-integer as an integer at write
time, under graph-ingest's single-writer authority) versus **export-time only**. Two measurements decide it.

**P1 — value coercion cannot survive the store. It is not risky; it is a no-op.** `Triple.Object` is `any`
(`message/triple.go:53`). `MarshalEntityState`/`UnmarshalEntityState` are plain `encoding/json`
(`graph/entity_predicate_contract.go:192,256`), and no custom `MarshalJSON`/`UnmarshalJSON` exists on `Triple`
anywhere (inventory § Ingest/storage path, 0 hits). `encoding/json` decodes every JSON number into `float64` when the
destination is `any`. So writing `int(5)` into `ENTITY_STATES` yields `float64(5)` on the very next read, whatever
ingest coerced. Getting `int` to survive would require a custom codec on `message.Triple` using `json.Number` — a new
decode path on a Tier 1 frozen type, affecting every reader in the repo. That is a different change with a different
justification, and it is not what "honor the declaration" means.

**P2 — the only thing that survives the store is the datatype STRING, and that string is contract-bearing at ingest,
not a hint.** `Triple.Datatype` round-trips fine as JSON. But three production paths read it:
`graph/entity_predicate_contract.go:168` (an `@id` triple whose object is not a canonical entity ID is a typed
`EntityStateContractError`, **refused at `MarshalEntityState` — the authoritative persistence seam**),
`pkg/fusion/engine_graph.go:319` (`edgeTarget` projects an `@id` triple as a directed **edge**, and its doc comment at
`:307-313` says it deliberately does NOT use `IsRelationship` because value-shape classification is "the exact
behavior this facet must never exhibit"), and `message/triple.go:139` (`IsRelationship`).

So populating `Triple.Datatype` from the registry at ingest would:

- convert every predicate declared `entity_ref`/`entity_id` into a **fusion edge** — 127 such declarations family-wide
  (M1) — changing ADR-062 deterministic-fusion output for shipped consumers, silently;
- turn any triple whose object under such a predicate is not a canonical entity ID into a **new refusal class at the
  single-writer seam** — state that is accepted today becomes state that cannot be written;
- change the stored bytes of every triple in the graph.

**P3 — export-time honoring obtains the issue's entire stated benefit with none of that.** `"5"^^xsd:integer` instead
of `"5.0"^^xsd:double` needs only the classifier to consult the registry; the observed `float64(5)` is unchanged, the
stored bytes are unchanged, fusion is unchanged, and no refusal class is created.

**Call: export-time only.** The ingest-time population of `Triple.Datatype` from the declaration is filed as a
separate change whose real subject is fusion edge semantics and a new `ENTITY_STATES` refusal class — not a datatype
detail. **This change makes that one cheaper**, because afterwards the declared value and `Triple.Datatype` are the
same value space (`@id` is literally the same string), so the later change is a copy rather than a translation.

---

## 6. #1142: rides

- **Mechanism** (confirmed against the code): #1142's defect is `classifyString` (`export/object.go:118-130`) —
  `message.IsValidEntityID(s)` is false for anything carrying a scheme, so an absolute IRI falls to the literal branch
  by construction, producing invalid RDF for `rdf:type`.
- **This change rewrites the same decision in the same two functions.** `classifyObject` gets the registry branch, and
  `classifyWithExplicitDatatype` gets the resource branch that M6 requires for `@id`. #1142's fix is one more `case`
  in the branch this change is already adding.
- **Separating them costs more than combining them**: the resource branch gets written twice and the same 30 lines get
  reviewed twice, and in between there is a release where `@id` exports as a resource and an absolute IRI does not —
  two answers to one question, which is exactly the "more than one home" defect the contract warns about.
- **Identical blast radius**: same package, same Tier 1 freeze, same zero in-repo callers, same single sister
  consumer.
- **Cost of riding**: one scenario, one fixture. **Call: rides, with `Closes #1142`** (Q7 — flagged because it puts
  two issues on one PR; the owner may prefer `Refs`).

Note that M6 is the *larger* sibling of #1142 and was not previously filed: the framework's **own** entity-reference
marker, validated at its own persistence seam, exports as invalid RDF today. If the owner splits #1142 off, M6 must
still ride here, because it is the direct consequence of adding `@id` to the closed set.

---

## 7. Item 6 answered: `Units` / `Range`

M4 is the fact the issue did not have: **zero sister `WithUnits`, one sister `WithRange`.** 23 in-repo sites against
755+ for `DataType`. These are not the same problem at the same scale, and treating them as one would ratchet the
change up for no measured demand.

- **7A — honor now.** Needs a units vocabulary (QUDT is large and external) and a range grammar — the doc comment
  alone advertises three incompatible grammars: `"0-100"`, `"-90 to 90"`, `"positive"` (`registry.go:127-128`). Two
  new closed vocabularies with **zero consumers at birth**. This is a design pass ratcheting complexity up against a
  standing rule, and it freezes a grammar chosen with no reader to constrain it — the same mistake being corrected one
  field over.
- **7B (recommended) — declare them documentation-only.** State it in the spec and rewrite both doc comments; no
  validation, no reader, no removal. This satisfies "never left write-only" in the only honest sense available before
  a consumer exists: the field's contract becomes a smaller promise that the framework actually keeps, recorded so it
  cannot silently drift back to implied-honored. File "honor Units/Range" against #1264 so the decision has a home.
- **7C — remove them.** `apidiff` REMOVAL on Tier 1, 24 call sites across the family, and it deletes information
  adopters actually wrote (`percent`, `celsius`, `0-100`) that #1264's SHACL sketch would want. Pre-v1 permits it; the
  evidence does not require it.

The asymmetry, stated plainly: `DataType` gets honored because a consumer exists **today** (export, and #1261 next).
`Units`/`Range` have none. Same field family, different evidence, different answer. **Owner ruling required (Q4)** —
the spec delta's third requirement is written as 7B and must be struck, not adapted, if the owner rules otherwise.

---

## 8. `Role` (inventory addendum A4) — an owner scope question, not a design choice

The fact: `Role` is a **fourth** write-only field on the same ADR-106 frozen struct, and its doc comment
(`vocabulary/registry.go:227`) states "Consumers (e.g. the deterministic fusion ranker) read it" while the named
consumer reads `Weight` (`pkg/fusion/fusionvocab/signals.go:48`). No in-repo reader, no sister reader (addendum A4).
The owner's "never left write-only" logic applies to it identically; #1267's scope does not name it.

One difference matters: `PredicateRole` is **already** a Go named type with seven constants
(`vocabulary/predicates.go:447-464`), so `WithRole` already takes the typed constant. The only hole is the
struct-literal path, which no sister uses for `Role` (measured: 0).

- **8i (recommended, inside this change)** — correct the false doc comment at `:227`. One line, on a line this change
  already touches, and leaving a documented-but-false consumer claim in place while fixing three fields beside it is
  not defensible.
- **8ii (separate owner decision)** — add `validPredicateRole` beside `validAliasType` in the same validator (~8
  lines, the same shape), closing the frozen struct's last open grammar before RC.
- **8iii — rejected on the contract's own terms**: wiring a reader for `Role` introduces an exported consumer with
  **zero consumers at birth**.

---

## 9. Collision table (semantic class: "the declared type of a triple object")

| Dimension | Evidence |
|---|---|
| Semantic class | What type a triple's object is — the fact this vocabulary would own |
| Owners | `vocabulary.PredicateMetadata.DataType` (`predicates.go:360`, per-predicate declaration); `message.Triple.Datatype` (`triple.go:86`, per-triple hint); `vocabulary/export.classifyByGoType` (`object.go:66`, per-value observation) |
| Catalogs | The in-process predicate registry map (`registry.go:299`). No generated datatype catalog exists: `git grep -ln 'xsd' -- schemas/` → 0 hits, stderr visible |
| Status | Empty. The field has no operator-visible state, no metric, no health signal — `git grep -n -E '\.DataType[^A-Za-z0-9_]'` returns only the setter write and test assertions (inventory § Searches) |
| Lifecycle | `Register` amends (`registry.go:299`, gh#410); `ClearRegistry`/`SnapshotRegistry` (`:593,608`) are test isolation only. Registration happens at `init()` and at the `vocabulary/builtins.Register()` composition root |
| Ownership | One in-process registry guarded by `registryMu` (`registry.go:301`); no partitioning, no lease, no active/active question — the registry is rebuilt from code at every boot |
| Readers | `Triple.Datatype`: `graph/entity_predicate_contract.go:168`, `pkg/fusion/engine_graph.go:319`, `message/triple.go:139`, `vocabulary/export/object.go:47`. `PredicateMetadata.DataType`: **semsource `processor/source-manifest/status.go:219,224` only** — nothing in this repo |
| Writers | `WithDataType` 195 in-repo (gopls-exact) + 755 family; struct-literal/assignment: semteams 34, semmachina 1; semlink 3 sites writing none |
| Recovery | None applicable — no persisted registry state, nothing to replay or reconcile |

**The collision this reports**: two homes already exist for one fact — declaration (per-predicate) and instance
(per-triple) — plus a third observer. The contract says more than one home is a defect to consolidate toward ONE
primitive. This design's answer is that the two scopes are not consolidated away (Option F, rejected on measurement)
but are made **one value space**: `@id` means the same thing in both, enforced by a contract check because M5's import
cycle forbids a shared constant. Observation becomes strictly the fallback, never a competing authority (I5).

---

## 10. Problem shape and the adoption sweep

**Shape**: *a closed vocabulary validated at a declaration seam, with recognized legacy spellings normalized once at
the boundary and never persisted.*

Both halves have in-repo prior art, so this change **establishes no new pattern and owes no adoption sweep**:

- Closed set + untyped string constants + exported checker, consumed at another package's registration seam:
  `IndexingProfile*` / `IsValidIndexingProfile` (`vocabulary/predicates.go:304-332`, consumed at
  `payloadregistry/registry.go:144`). This is the shape §2.2 adopts.
- Closed set validated inside `validatePredicateMetadataLocked` on this exact struct: `validAliasType`
  (`vocabulary/registry.go:406-412`, called from `:394`). This is the seam §2.4 adopts.
- Normalize-at-a-boundary, store nothing: `expandDatatypePrefix` (`vocabulary/export/object.go:166-176`). This is the
  shape §2.3 adopts.
- Two homes, one asserted value, enforced by a contract test: `TestNATSVersionIsConverged`
  (`test/contract/nats_version_contract_test.go`). This is the shape §2.2's cycle workaround adopts.
- Mechanical drift guard walking an enumerable registry: `TestCommittedSchemasMatchCode`
  (`test/contract/schema_contract_test.go:19`). This is the shape the completeness guard adopts — no AST scan needed,
  since `ListRegisteredPredicates()`/`GetPredicateMetadata()` already enumerate.

## 11. Decision skills

| Skill | Triggered? | Outcome |
|---|---|---|
| `kv-or-stream` | No | No new communication path. Nothing is published, watched, or streamed; the registry is in-process and rebuilt from code at boot. |
| `orchestration-check` | No | No multi-step behavior, no trigger, no component boundary. Validation is one synchronous call inside an existing function. |
| `new-payload` | No | No new message type. `PredicateSchemaPayload` is semsource's, not ours; this change alters a value it carries, never its shape. |
| `query-pattern` | No | No new query access. #1261 will expose a tool schema that reads this registry, but it owns that surface; this change adds none. |
| `entity-or-bucket` | No | No new durable state. Nothing new is written to KV or the graph. |

## 12. Guard split (item 3)

Two different jobs, deliberately not one test:

- **Registration validation enforces the SET** — every declared value is canonical or refused. It cannot enforce
  completeness, because empty must stay legal (M3).
- **The `test/contract/` guard enforces COMPLETENESS for framework-owned vocabulary** — `ClearRegistry()`,
  `builtins.Register()` (`vocabulary/builtins/register.go`), walk `ListRegisteredPredicates()`, assert every
  framework-declared predicate carries a datatype from the closed set. It must be shown capable of failing, per the
  precedent the `predicate-contract` spec already sets for registry audits (`spec.md:187-192`).

A3's amend semantics are why the guard alone is not sufficient evidence: a predicate can pass it by inheriting a
datatype from an earlier registration call it never declared. The validator is what makes the inherited value
trustworthy, since normalization is idempotent (I2).

---

## Process note

`.agents/contracts/semstreams-architect.md` §Required workflow puts an independent `INVENTORY PASS` between the
inventory and any target state. This design was written before that gate at the launching session's direction, starting
from the explorer's inventory under owner ruling A (2026-08-30, #1180). It is therefore a **draft contingent on the
gate**, not an accepted design, and §1.2's eight new measurements (M1–M8) are additions to the inventory that the
gate has not seen — M1, M5, and M6 in particular change what the change costs and what it must fix. If the inventory
review rejects any of them, this design goes back to §1.
