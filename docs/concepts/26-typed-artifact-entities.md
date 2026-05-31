# Typed Artifact Entities

When a domain has structured payloads that are *constitutive of an
entity's identity but inconveniently large for triples* — schemas,
source documents, reference data — model them as first-class artifact
entities, not as inline triple objects. The artifact entity carries
its own 6-part EntityID and a singular `StorageRef` pointing to the
payload in NATS ObjectStore. Parent resources relate to the artifact
via vocabulary predicates.

This is the pattern crystallized while resolving gh#171 — semconnect
needed a place for SensorML source documents and SWE Common schemas
that wouldn't push object-shaped payloads into graph triples. The
substrate already supported it; this doc names the pattern so the next
sister-repo doesn't re-derive it.

## When to reach for it

| Use a typed artifact entity when... | Use inline triples / a single StorageRef when... |
|---|---|
| The payload is **reusable** across N parent resources (one schema, many datastreams referencing it) | The payload is **constitutive of exactly one entity** — never reused |
| The payload has **independent lifecycle** — versioned, deprecated, retired separately from the parent | Lifecycle is locked 1:1 with the parent |
| You want **graph-native discoverability** — "give me all schemas," "what datastreams use schema X" should be one-hop queries | Discoverability across siblings isn't a need |
| The payload is **large or structured** (XML, JSON-LD, binary) — putting it in a triple Object would be a footgun | The payload is a small scalar or short string |

The classic counter-shape is a multimedia document where the entity
IS the bundle (one video + one thumbnail belong together, no reuse,
no independent lifecycle) — that's a `BinaryStorable` bundled
under the entity's singular StorageRef, not separate artifact
entities.

## The shape

```text
Parent entity (Datastream, System, ControlStream, ...)
  rdf:type                csapi:Datastream
  csapi:producedBy        system:<systemEntityID>
  csapi:hasResultSchema   artifact:<schemaEntityID>     ← typed-artifact link
  csapi:hasSource         artifact:<sensormlEntityID>   ← typed-artifact link

artifact:<schemaEntityID>
  rdf:type                csapi:SWESchemaDocument
  StorageRef              { Instance: "csapi-artifacts",
                            Key: "swe/schemas/temp-celsius-v1.json",
                            ContentType: "application/swe+json" }

artifact:<sensormlEntityID>
  rdf:type                csapi:SensorMLDocument
  StorageRef              { Instance: "csapi-artifacts",
                            Key: "sml/systems/weather-station-42.xml",
                            ContentType: "application/sml+xml" }
```

EntityID convention for artifacts: the `instance` segment of the
6-part ID encodes identity in a content-addressable way — schema name
+ version, or content hash. e.g.
`acme.shared.csapi.gateway.swe-schema.temp-celsius-v1`.

## Why not just put a `StorageRefs map[string]*StorageReference` on the parent?

Considered and rejected during gh#171 triage. The `StorageRefs` map
form bundles content that would naturally be discoverable graph
entities; the role-keyed map is opaque to graph queries. Schemas
can't be referenced by N datastreams without duplication. It
introduces two ways to express "this entity has stored content"
(singular `StorageRef` + new `StorageRefs` map) with no
conflict-resolution semantics. And it bypasses the bucket-ownership
rubric: the default architectural answer is "live as a graph entity";
artifacts ARE entities, and their CONTENT belongs in ObjectStore via
the existing per-entity singular `StorageRef`. The framework already
cleanly splits those — no new primitive needed.

## How sister repos consume it

Three steps on the producing side:

1. **Generate the artifact's EntityID** (content-hashed or
   schema-name + version, in the `instance` segment of the 6-part ID).
2. **Emit the artifact entity** through the standard graph-ingest
   wire — `Graphable` with the artifact's class triple
   (`rdf:type csapi:SWESchemaDocument`) and a `ContentStorable`
   implementation so the framework stamps the StorageRef during
   ingest.
3. **Emit the parent entity** with the relationship triple
   (`csapi:hasResultSchema artifact:<schemaEntityID>`) and any other
   facts the parent owns.

On the consuming side: resolve the relationship triple → fetch the
artifact entity → read the artifact's `StorageRef` → fetch content via
the NATS ObjectStore API. Same API the framework already exposes for
every other storage-ref-bearing entity.

## Cross-references

- `vocabulary/csapi/` — `HasSource`, `HasResultSchema`,
  `HasCommandSchema` predicates; `SensorMLDocument`,
  `SWESchemaDocument` class IRIs.
- [Rule-Driven Artifacts](18-rule-driven-artifacts.md) — the sister
  pattern for *external-facing* rendered artifacts (markdown, CSV,
  HTTP payloads). Different shape; complementary.
- ADR-049 § bucket-ownership rubric — the architectural principle
  underwriting this pattern's "live as a graph entity" default.
- gh#171 — the upstream-asks issue that crystallized the pattern
  alternatives (Patterns 1, 2, 3) and selected Pattern 2.
