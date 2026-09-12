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

It is now a **closed, seven-value pragmatic vocabulary**, **refused at declaration** if you write anything else, and
honored by RDF export.

**Read §2 and §3 before upgrading.** The framework translates nothing. If any declaration in your repository uses a
spelling outside the seven, your binary panics at registration on the version carrying #1267 — this document is the
whole migration, not a supporting note to it.

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

## 2. What you must change

`Register` and `RegisterPredicate` accept the seven canonical values and absence. **Every other spelling is refused
with a panic**, naming the offending value and the accepted vocabulary. The framework does not translate: an earlier
draft of this change normalized sixteen known legacy spellings silently, and the owner ruled against it on
2026-09-09 — a translation table on a frozen package is an undated permanent bridge, and it makes the registry
report a value no author wrote.

So the column that matters is the last one. Anything marked **migrate** is a site that stops your binary booting.

| Declared spelling | Write instead | Action | Family sites measured |
|---|---|---|---|
| `string` | — | none, already canonical | 554 + 34 struct-literal |
| `entity_id` | — | none, already canonical | 126 |
| `int` | — | none, already canonical | 70 |
| `datetime` | — | none, already canonical | 53 |
| `bool` | — | none, already canonical | 21 |
| `json` | — | none, already canonical | 13 |
| `float` | — | none, already canonical | 10 |
| `float64` | `float` | **migrate** | 21 |
| `number` | `float` | **migrate** | 1 + 3 struct-literal |
| `double` | `float` | **migrate** | 0 measured |
| `time.Time` | `datetime` | **migrate** | 10 |
| `timestamp` | `datetime` | **migrate** | 8 |
| `int64` | `int` | **migrate** | 2 |
| `array` | `json` — and read the note below | **migrate** | 20 |
| `entity_ref` | `entity_id` | **migrate** | 1 |
| `reference` | `entity_id` | **migrate** | 1 |
| `boolean` | `bool` | **migrate** | 1 |
| a Go struct name, or anything else | the pragmatic type of the object | **migrate** | 30 (semdragon only) |
| *(absent)* | — | none — absence stays legal | 3 semlink sites |

**`array` is the row worth a moment.** `json` is the honest replacement — the objects are JSON documents in practice
— but it loses "this is a list", and that loss is now yours to accept rather than ours to perform quietly. If your
`array` predicates genuinely need list-ness at the export edge, say so on #1267 before you migrate them; a datatype
invented with no reader would have been worse than the loss, but a reader you can name changes that.

**Absence stays legal.** semlink registers `vocabulary.PredicateMetadata{Name: predicate}` with no datatype at
`internal/cop/contracts.go:91`, `internal/projector/contracts.go:105` and `internal/rules/contracts.go:46`
(`985e97d`). Nothing there needs to change. Seventy-nine SemStreams framework predicates also declare nothing today
(#1277 tracks closing that set).

## 3. What breaks, and where you find out

Ranked honestly — compile error > boot error > typed runtime error > log line > doc > nowhere:

| Situation | Where you find out | What to do |
|---|---|---|
| You do not upgrade | nothing happens | nothing |
| Every spelling you declare is already canonical | nothing happens | nothing — 92% of the family's declarations are in this row |
| Any spelling you declare is not canonical | **boot error** — panic at registration, naming the value and the accepted vocabulary, before any data moves | replace it, per §2 |
| You emit RDF through `vocabulary/export` | **nowhere** — the output changes | read §5 |

The second and third rows are the whole of the ruling. Under the normalizing draft, a retired spelling landed in the
row above with "**nowhere**" as its discovery rank: your value was corrected for you and you were never told, which
is the worst rank on the scale. Refusal moves every affected declaration to **boot error** — the best rank available,
since a compile error is impossible by construction (below). You pay one migration, once, instead of never finding
out.

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

Semsource's owner: two distinct halves, and only one of them is silent.

Your own declarations are 6 `array` sites. Under the refusal rule those **stop your binary booting** until you change
them to `json`, so you will not miss them — and when you do change them, you are the one changing the `data_type`
your manifest publishes, at a moment you chose. That is strictly better than the normalizing draft, where the same
wire value would have changed under you at upgrade with no signal anywhere.

The other half stays silent and is the reason this section exists: the **framework** predicates your binary
registers had their declarations migrated in-repo, so their `data_type` values change on upgrade without any edit on
your side. **17 of those reach a product binary** — 16 in `vocabulary/agentic/register.go` and 1 in
`vocabulary/governance/register.go`. The in-repo migration touched 37 sites in total, but 20 are in
`examples/processors/{iot_sensor,document,weather_station}`, which **no product binary** registers: they are absent
from `go list -deps ./cmd/semstreams`, so they cannot reach your manifest. (The e2e harness binary does pull two of
them in, which is why "nothing registers them" would be too strong.) Nothing in SemStreams can detect a consumer of those.

## 5. RDF export output changes

`vocabulary/export` now classifies an object by a total order: the triple's own `Datatype` hint, then the
predicate's declared `DataType`, then observation of the Go type. Observation is the fallback, never a competing
authority — but a declaration the observed value contradicts is **ignored for that triple**, never applied.

The family's only production RDF emitter is semconnect's CS API gateway
(`gateway/cs-api/systems.go`, `d0d06e0`), which serializes `ENTITY_STATES` triples through this package. SemStreams
itself has zero in-repo callers of `vocabulary/export`.

Before this change the serializer **never consulted `PredicateMetadata.DataType` at all** — `classifyObject` had no
declaration branch. So the output changes are not only the broken cases: every declaration whose rendering differs
from plain observation now emits something different. Six changes, and the two largest are the two that were merely
untyped rather than invalid:

| Before | After | Why |
|---|---|---|
| `"5.0"^^xsd:double` for a predicate declared `int` | `"5"^^xsd:integer` | #1267 — the whole issue |
| bare `"2026-09-09T12:00:00Z"` for a predicate declared `datetime` | `"2026-09-09T12:00:00Z"^^xsd:dateTime` | **highest volume.** `ENTITY_STATES` marshals `time.Time` to an RFC 3339 *string*, so after the authoritative round trip every `datetime` triple took the string branch and emitted an untyped literal (`xsd:string` is the default and is omitted). 53 sister sites — semspec 50, semsource 3 — plus 10 in-repo |
| bare `"{…}"` for a predicate declared `json` | `"{…}"^^rdf:JSON` | same shape: an untyped literal became a typed one. 13 sister sites |
| `"acme.ops.gcs.robotics.drone.002"^^<@id>` | `<…/entities/acme/ops/gcs/robotics/drone/002>` | #1272 — `@id` is a relative reference, not a datatype IRI; a marked object denotes an IRI node |
| `"{…}"^^<rdf:JSON>` | `"{…}"^^rdf:JSON` (Turtle, with the `rdf:` prefix declared) / the full IRI in N-Triples | the prefix was never expanded |
| `"http://schema.org/Thing"` as a literal | `<http://schema.org/Thing>` | #1142 — an absolute-IRI object is a resource; a literal there is invalid RDF for a type-bearing predicate |

Rows 1, 4, 5 and 6 replace output that was wrong or invalid RDF. Rows 2 and 3 replace output that was **valid but
untyped** — no consumer was reading a malformed value, but a consumer that keyed on the absence of a datatype suffix
will now see one. That is the row a semconnect RDF consumer is most likely to notice, and it is why this section
enumerates all six rather than only the defects.

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

Under the refusal rule there is no longer a "changes value silently" column: every non-canonical site is a site that
must be edited, or the binary does not boot.

| Repository | SHA read | Declarations | **Must migrate** | Action |
|---|---|---|---|---|
| **semdragon** | `07f4de9` | 56 | **31** — 30 Go struct-name sites (26 distinct) + 1 `int64` | replace all 31; see below |
| **semspec** | `5a9496ee` | 534 | **16** — `array` 14, `reference` 1, `boolean` 1 | replace all 16 |
| **semsource** | `4093d3c` | 142 | **6** — `array` | replace all 6; **see §4 — its `data_type` wire value changes** |
| **semteams** | `ce22c961` | 34 | **3** — `number`, struct-literal path | replace all 3 |
| **semconnect** | `d0d06e0` | 5 | **1** — `float64` | replace it; **see §5 — its CS API RDF output changes** |
| **semboids** | `8c03cc5` | 3 | **1** — `number` | replace it |
| **semlink** | `985e97d` | 0 | **0** | none — its datatype-free registrations stay legal |
| **semmachina** | `841c45e` | 1 | **0** | none — its `= "string"` default is already canonical |
| **semsage / semmem / semops / semembed** | `4d28b4d` / `b909cbf` / `602c619` / `7ceb528` | 0 | **0** | none — they declare no datatypes |

**Family total: 58 sites across six repositories**, out of 775 declarations measured. 92% of the family is already
canonical and does nothing.

### The walk behind those numbers

Stated because this effort has produced several different counts of the same thing, and the number always depends on
the walk. Re-measured read-only on 2026-09-10 at the SHAs above — the same SHAs the 2026-09-09 pass used, so this is
a reproduction and not a later snapshot. Each repository's `git status --porcelain` was captured before and after and
verified byte-identical; no `go` command was run in any sister.

The walk is all three declaration paths, over `*.go` **including `_test.go`**:

```bash
git grep -h -o -E 'WithDataType\("[^"]*"\)'        -- '*.go'   # functional option
git grep -h -o -E 'DataType:[[:space:]]*"[^"]*"'    -- '*.go'   # struct literal
git grep -h -o -E 'DataType[[:space:]]*=[[:space:]]*"[^"]*"' -- '*.go'   # assignment
```

**This disagrees with the figures quoted in the owner's ruling** (PR #1269 comment 5606966440: 67 sites to migrate,
per-repo `semspec 16 · semteams 12 · semsource 6 · semconnect 1 · semboids 1 · semdragon 1`, plus ~27 semdragon
struct names). Five of the seven rows reproduce exactly. Two do not: **semteams measures 3, not 12**, and semdragon's
struct names measure **30 sites over 26 distinct names**, not ~27. The family total is therefore **58, not 67**.

The ruling's decision is unaffected — it turned on the principle, not the size, and 58 is smaller than the bill the
owner accepted. The numbers here are the measured ones because they are the ones an adopter can reproduce with the
commands above. Flagged rather than silently corrected.

### semdragon: the 31 sites

Thirty of them are in **`domain/vocab.go`** (`07f4de9`), each a Go payload struct name — not a datatype in any sense
the framework can render into RDF, so all are refused at registration. The 31st is a single `int64`, which the
normalizing draft would have translated to `int` and which now needs the one-word edit.

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
# Every spelling you declare today, across all three declaration paths, with
# the canonical seven filtered out. Whatever this prints is your migration.
{ git grep -h -o -E 'WithDataType\("[^"]*"\)'                     -- '*.go' | sed -E 's/.*\("(.*)"\).*/\1/'
  git grep -h -o -E 'DataType:[[:space:]]*"[^"]*"'                 -- '*.go' | sed -E 's/.*"(.*)"/\1/'
  git grep -h -o -E 'DataType[[:space:]]*=[[:space:]]*"[^"]*"'     -- '*.go' | sed -E 's/.*"(.*)"/\1/'
} | grep -vE '^(string|entity_id|int|float|bool|datetime|json)$' | sort | uniq -c | sort -rn

# Empty output means you have nothing to do. Otherwise fix each one per §2 --
# a WithDataType sweep alone misses the struct-literal and assignment paths,
# which is how 34 semteams sites and semmachina's default went unmeasured once
# already.

# Then: your binary boots, or it panics naming the offending value. There is no
# third outcome, and there is no mode in which it boots with a value you did
# not write.
go test ./...
```
