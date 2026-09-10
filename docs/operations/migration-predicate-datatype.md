# Migration — the closed predicate `DataType` vocabulary (#1267)

SemStreams-owned record of what each downstream product must do when it picks up the SemStreams version carrying
#1267. Sister repositories are **read-only** to SemStreams agents (owner ruling on #1100, 2026-08-26): no issues,
comments, or edits are made there. Every obligation is recorded here; each sister's owner implements and validates it
in their own repository, at their own pace — sisters pin their `semstreams` version in `go.mod`, so nothing breaks
until they choose to upgrade.

Every sister figure below was measured read-only on 2026-09-09 with `git grep`, at the SHAs named in
[Per-repository bill](#per-repository-bill). Each repository's `git status --porcelain` was captured before and
after and verified byte-identical.

## What changed

`vocabulary.PredicateMetadata.DataType` was a free-form string that **nothing read** and that asked the declaring
author to predict the *Go type* of the object value — a prediction nobody can make correctly, because a value read
back from `ENTITY_STATES` has been through `encoding/json` into an `any` and is `float64` whatever was written.
Forty-two spellings across the family, twenty-six of them Go struct names, were the measured receipt.

It is now a **closed, seven-value pragmatic vocabulary**, normalized at declaration and honored by RDF export.

## 1. The closed set

Declare one of these seven constants, or declare nothing.

| Constant | Value | The declaration asserts |
|---|---|---|
| `vocabulary.DataTypeString` | `string` | plain text |
| `vocabulary.DataTypeEntityID` | `entity_id` | the object is a canonical 6-part entity ID, not text that resembles one |
| `vocabulary.DataTypeInt` | `int` | the number is semantically whole, whatever the JSON round trip left in the Go value |
| `vocabulary.DataTypeFloat` | `float` | a real number |
| `vocabulary.DataTypeBool` | `bool` | a truth value |
| `vocabulary.DataTypeDateTime` | `datetime` | an instant, whether carried as `time.Time` or an RFC 3339 string |
| `vocabulary.DataTypeJSON` | `json` | the string holds a structured document |

They are **untyped string constants**, not a named type. `WithDataType` keeps its `string` parameter and
`PredicateMetadata.DataType` stays a `string` field, so every existing call site and struct literal still compiles.
Tightening is by validation, never by signature: `task api:compat` reports the seven constants as ADDITIONs and zero
REMOVALs.

No semantic-web name appears in this list. `xsd:integer`, `@id` and `rdf:JSON` name the same facts correctly and live
at the export boundary, where interoperability is the job — see
[ADR-107](../adr/107-semantic-web-vocabulary-lives-at-the-export-edge.md). A declaring author never has to know XSD.

## 2. The mapping — what your stored value becomes

`Register` and `RegisterPredicate` normalize a recognized legacy spelling to its canonical value at declaration
time. Nothing persists a legacy spelling and no reader ever observes one. The rightmost column is the whole bill.

| Declared spelling | → canonical | Value changes? | Family sites measured |
|---|---|---|---|
| `string` | `string` | no | 554 + 34 struct-literal |
| `entity_id` | `entity_id` | no | 126 |
| `int` | `int` | no | 70 |
| `datetime` | `datetime` | no | 53 |
| `bool` | `bool` | no | 21 |
| `json` | `json` | no | 13 |
| `float` | `float` | no | 10 |
| `float64` | `float` | **yes** | 21 |
| `number` | `float` | **yes** | 1 + 3 struct-literal |
| `double` | `float` | **yes** | 0 (carried for authors migrating from an XSD-shaped draft) |
| `time.Time` | `datetime` | **yes** | 10 |
| `timestamp` | `datetime` | **yes** | 8 |
| `int64` | `int` | **yes** | 2 |
| `array` | `json` | **yes**, and lossy — it loses "this is a list" | 20 |
| `entity_ref` | `entity_id` | **yes** | 1 |
| `reference` | `entity_id` | **yes** | 1 |
| `boolean` | `bool` | **yes** | 1 |
| *anything else* | **REFUSED — panic at registration** | — | 30 (semdragon only) |
| *(absent)* | *(stays absent — legal)* | no | 3 semlink sites |

`array → json` is named as lossy here so it is not discovered later: the objects are JSON documents in practice, and
inventing an `array` datatype with no reader would be a new value nothing consumes.

**Absence stays legal.** semlink registers `vocabulary.PredicateMetadata{Name: predicate}` with no datatype at
`internal/cop/contracts.go:91`, `internal/projector/contracts.go:105` and `internal/rules/contracts.go:46`
(`985e97d`). Nothing there needs to change. Eighty-one SemStreams framework predicates also declare nothing today.

## 3. What breaks, and where you find out

Ranked honestly — compile error > boot error > typed runtime error > log line > doc > nowhere:

| Situation | Where you find out | What to do |
|---|---|---|
| You do not upgrade | nothing happens | nothing |
| Your spelling is one of the 17 mapped ones | **nowhere** on the write path | nothing to compile; see §4 if you republish the value |
| Your spelling is anything else | **boot error** — panic at registration, naming the value and the accepted vocabulary, before any data moves | replace it with a constant |
| You emit RDF through `vocabulary/export` | **nowhere** — the output changes | read §5 |

A compile error is impossible by construction: `WithDataType` keeps its `string` parameter so 755 sister call sites
survive, which makes registration-time refusal the earliest surface that exists.

## 4. The wire change — for semsource's manifest consumers

**This is the finding of this migration, and it is the one nothing in SemStreams can detect.**

semsource reads `.DataType` in production at `processor/source-manifest/status.go:224-225` and republishes it as
`data_type` on `PredicateDescriptor` → `PredicateSchemaPayload`
(`processor/source-manifest/payload_predicates.go:30`), at `4093d3c`. That is a **published wire format**.

After the upgrade, a semsource deployment emits the canonical value where it emitted the legacy one, for every
predicate registered in its binary — its own and every framework predicate the binary registers. Concretely:
`"float"` where it emitted `"float64"`, `"datetime"` where it emitted `"time.Time"` or `"timestamp"`, `"int"` where
it emitted `"int64"`, `"json"` where it emitted `"array"`, `"entity_id"` where it emitted `"entity_ref"`.

The person who breaks is **a consumer of semsource's manifest payload** — two repositories away, who never called a
SemStreams API, who is in no review, and who has no seam at which to find out. `task api:compat` is structurally
blind to it: no signature moves, only emitted values do. That is why this document exists and why it carries the
value-level table rather than only the constant list.

Semsource's owner: the predicates semsource itself declares change on `array` only (6 sites). The larger part of the
delta is the framework predicates its binary registers — 37 in-repo SemStreams call sites change value.

## 5. RDF export output changes

`vocabulary/export` now classifies an object by a total order: the triple's own `Datatype` hint, then the
predicate's declared `DataType`, then observation of the Go type. Observation is the fallback, never a competing
authority — but a declaration the observed value contradicts is **ignored for that triple**, never applied.

The family's only production RDF emitter is semconnect's CS API gateway
(`gateway/cs-api/systems.go`, `d0d06e0`), which serializes `ENTITY_STATES` triples through this package. SemStreams
itself has zero in-repo callers of `vocabulary/export`. Four output changes, each replacing output that was wrong or
invalid RDF:

| Before | After | Why |
|---|---|---|
| `"5.0"^^xsd:double` for a predicate declared `int` | `"5"^^xsd:integer` | #1267 — the whole issue |
| `"acme.ops.gcs.robotics.drone.002"^^<@id>` | `<…/entities/acme/ops/gcs/robotics/drone/002>` | #1272 — `@id` is a relative reference, not a datatype IRI; a marked object denotes an IRI node |
| `"{…}"^^<rdf:JSON>` | `"{…}"^^rdf:JSON` (Turtle, with the `rdf:` prefix declared) / the full IRI in N-Triples | the prefix was never expanded |
| `"http://schema.org/Thing"` as a literal | `<http://schema.org/Thing>` | #1142 — an absolute-IRI object is a resource; a literal there is invalid RDF for a type-bearing predicate |

Two more consequences worth stating because they are silent:

- a predicate declared `entity_id` now emits an IRI node for a canonical entity-ID object, identical to what the
  per-triple `@id` marker produces. The two spellings converge at the export boundary, by design;
- a predicate declared `string` renders **exactly as today**. `xsd:string` is the default and is omitted from
  output, so the declaration confirms observation rather than overriding it. An entity-ID-shaped or absolute-IRI
  object under a `string` declaration is still emitted as a resource.

Only `xsd:` and `rdf:` prefixes are expanded in a per-triple `Datatype`; an absolute IRI passes through as written.
Any other prefix — `geo:point`, say — still reaches the output unexpanded, as a relative reference that is not valid
RDF. The `message.Triple.Datatype` doc comment used to offer `geo:point` as an example and no longer does; it states
the rule instead.

Nothing at ingest changed. `message.Triple.Datatype` is not populated from the registry, no stored bytes change, and
ADR-062 deterministic-fusion edge projection (`pkg/fusion/engine_graph.go`) is untouched.

## 6. `Units` and `Range` are documentation

`PredicateMetadata.Units` and `.Range` are now stated to be free-form human-readable documentation. No framework
path validates, normalizes, interprets, or honors either, and no closed vocabulary is invented for them ahead of a
consumer. Nothing to change; the contract is now the smaller promise the framework actually keeps. Honoring them is
tracked on #1264.

`PredicateMetadata.Role` keeps its behavior; only its doc comment was corrected — it claimed the deterministic
fusion ranker reads `Role`, and the ranker reads `Weight`.

## Per-repository bill

| Repository | SHA read | Sites that change value | Sites REFUSED | Action |
|---|---|---|---|---|
| **semdragon** | `07f4de9` | 1 (`int64`) | **30**, in `domain/vocab.go` | replace 26 Go struct-name spellings; see below |
| **semspec** | `5a9496ee` | 16 (`array` 14, `reference` 1, `boolean` 1) | 0 | none required; values change on read |
| **semsource** | `4093d3c` | 6 (`array`) | 0 | none required; **see §4 — its `data_type` wire value changes** |
| **semteams** | `ce22c961` | 3 (`number`, struct-literal path) | 0 | none required |
| **semconnect** | `d0d06e0` | 1 (`float64`) | 0 | none required; **see §5 — its CS API RDF output changes** |
| **semboids** | `8c03cc5` | 1 (`number`) | 0 | none required |
| **semlink** | `985e97d` | 0 | 0 | none — its datatype-free registrations stay legal |
| **semmachina** | — | its `= "string"` default is already canonical | 0 | none required |

### semdragon: the 26 unmappable spellings

All 30 sites are in **`domain/vocab.go`** (`07f4de9`). Each is a Go payload struct name, which is not a datatype in
any sense the framework can render into RDF, so none of them maps and all of them are refused at registration.

`ApprovalRequest` · `ApprovalResponse` · `AutonomyEvaluatedPayload` · `AutonomyIdlePayload` · `ClaimIntentPayload` ·
`EscalationPayload` · `ExecutionCompletedPayload` · `ExecutionFailedPayload` · `ExecutionStartedPayload` ·
`GuildAutoJoinedPayload` · `GuildCreateIntentPayload` · `GuildCreatedPayload` · `GuildIntentPayload` ·
`GuildMemberPayload` (2 sites) · `GuildRankPayload` (2 sites) · `GuildSuggestedPayload` · `InterventionPayload` ·
`Lesson` · `MentorBonusPayload` · `PeerReviewPayload` (3 sites) · `SessionEndPayload` · `SessionStartPayload` ·
`ShopIntentPayload` · `SkillLevelUpPayload` · `SkillProgressionPayload` · `UseIntentPayload`

**What to write instead.** The object under each of those predicates is a serialized document, so
`vocabulary.DataTypeJSON` is the honest declaration for almost all of them. Where the object is in fact a reference
to another entity, `vocabulary.DataTypeEntityID` is the one that buys something — export emits an IRI node for it
rather than a string literal. Where the object is plain text, `vocabulary.DataTypeString`. Declaring nothing is also
legal and is a valid interim step: it keeps the boot green and costs only the export improvement.

## Verifying the upgrade in your repository

```bash
# Every spelling you declare today, and whether it survives.
git grep -h -o -E 'WithDataType\("[^"]*"\)' -- '*.go' | sed -E 's/.*\("(.*)"\).*/\1/' | sort | uniq -c | sort -rn
git grep -n -E 'DataType:[ ]*"' -- '*.go'   # the struct-literal path a WithDataType sweep misses

# Then: your binary boots, or it panics naming the offending value. There is no third outcome.
go test ./...
```
