# ADR-107: Semantic-Web Vocabulary Lives at the Export Edge

## Status

**Accepted (2026-09-09)** — owner ruling in session, recorded verbatim on #1267. Ruled alongside six scoped
decisions (Q2–Q7) for the `honor-predicate-datatype` change; this ADR records only the boundary rule, which is
standing and wider than that change. The mechanics of the datatype vocabulary live in the `predicate-contract`
capability spec, not here.

## Context

SemStreams builds knowledge graphs and can serialize them as RDF — Turtle and JSON-LD — through
`vocabulary/export`. That capability makes semantic-web vocabulary *available* everywhere, and the pull is to let it
drift inward: to name a core field with an XSD type, a JSON-LD keyword, or an RDF datatype IRI because those names
are precise and already written down by a standards body.

The pull was made concrete by #1267. `PredicateMetadata.DataType` had been declared but never read, and the design
to honor it proposed a closed vocabulary spelled with XSD local names plus JSON-LD's `@id` — on the reasoning that
every value would then already be a spelling the repository used, and the per-predicate declaration and the
per-triple datatype hint would share one value space.

The measurement said otherwise. Across the family — 17 c360 Go repositories scanned, six with any hit, 942
declarations — the spellings declaring authors actually write are pragmatic:

| spelling | uses | | spelling | uses |
|---|---|---|---|---|
| `string` | 554 (+34 struct-literal) | | `float64` | 21 |
| `entity_id` | 126 | | `bool` | 21 |
| `int` | 70 | | `array` | 20 |
| `datetime` | 53 | | `json` | 13 |

Measured read-only on 2026-09-09 and re-measured at implementation; the ruling's own restatement of this table
transcribed the `string` row as 680 and the `number` row as 13 against a measured 1 (4 with struct literals). Neither
difference touches the decision — every one of those spellings is pragmatic, and all of them map — but the numbers
here are the measured ones.

`integer`, `dateTime`, `@id` and `rdf:JSON` are written **zero times** in the declaration surface, and `double`
zero times as well; `boolean` is written **once** (semspec `vocabulary/observability/predicates.go:178`). Every
one of them otherwise appears only inside `vocabulary/export`, which is the boundary. The point the measurement
carries is not that semantic-web names are literally absent — it is that they are vanishingly rare next to the
pragmatic spellings, so canonicalizing on them would impose a vocabulary almost no declaring author uses, and would
have changed the emitted value of roughly 270 predicates on a wire payload a sister republishes — a break `task
api:compat` cannot detect, because no signature moves — and would have obliged every sister author to learn XSD in
order to say "this is an int".

## Decision

**Semantic-web vocabulary belongs at the export edge, where interoperability is the job. It does not belong in the
core.** The core carries pragmatic triples with RDF\*-like statement metadata.

Concretely:

- A declaration surface an adopter writes against takes **pragmatic, neutral spellings**. It never requires an
  author to know XSD, JSON-LD, RDF, SHACL, or OWL.
- The mapping from those values to RDF datatype IRIs, JSON-LD keywords, or shape languages lives **with the
  serializer**, and nowhere else. The registry is not required to know it.
- Statement-level metadata on `message.Triple` — `Datatype`, `Source`, `Timestamp`, `Confidence` — is the RDF\*-like
  shape this decision endorses. The prohibition is on semantic-web *vocabulary migrating inward from the edge*, not
  on per-statement metadata, and not on the existing per-triple markers.
- Before canonicalizing any vocabulary, **measure what the family already writes.** A canon that differs from the
  measured usage is a migration bill, and the bill is paid by adopters who are not in the review.

## Consequences

The `honor-predicate-datatype` change is the first application, not the subject: its closed set is `string`,
`entity_id`, `int`, `float`, `bool`, `datetime`, `json`, and the XSD/JSON-LD mapping table sits in
`vocabulary/export`. Two further decisions on that change fall out of this rule rather than being argued separately —
the declared datatype is honored at **export time** rather than coerced at ingest, and `Units`/`Range` stay
documentation-only rather than pulling QUDT-style unit identifiers into the core for a field with one sister call
site.

An import cycle that would otherwise have constrained the design dissolves: `vocabulary` cannot import `message`
(`message` → `payloadregistry` → `vocabulary` already exists), but under this rule only `vocabulary/export` needs
`message.EntityReferenceDatatype`, and it may import `message` freely.

The cost is one mapping table at the boundary and two value spaces instead of one — a declaration spelling and a
per-triple marker that are deliberately not the same string. That is the price of keeping the adopter-facing surface
free of a vocabulary the adopter never asked for, and it is paid in a single function that already existed.

This rule does not restrict what SemStreams can *export*. RDF, JSON-LD, and future shape-language output remain
first-class at the boundary; #219 (SHACL at the export boundary) is unaffected in scope, only in where its
vocabulary may live.

## Related

ADR-074 (canonical predicate contract) · ADR-106 (two-tier surface freeze — `vocabulary` is Tier 1, which is why a
write-only field had to be decided before the RC clock) · #1267 (the ruling) · #1272 (the `^^<@id>` export defect the
boundary rule's first application closes) · #1142 · #219 · #1264
