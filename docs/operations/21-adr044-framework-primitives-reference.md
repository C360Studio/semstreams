# ADR-044 Framework Primitives — Reference

Reference for every framework-side primitive shipped under
[ADR-044](../adr/044-ogc-connected-systems-framework-split.md)
Phases 2-6. This document is the authoritative pointer the
sister repository (`semconnect`, working name) links back to
when scaffolding its OGC API Connected Systems server.

## Reading order

1. [ADR-044](../adr/044-ogc-connected-systems-framework-split.md) — the architecture and why.
2. This reference — what landed, where it lives, how it composes.
3. The sister-repo `README` and `docs/000-getting-started.md` (in `semconnect`) — how to consume.

## Where the boundary lives

ADR-044 splits CS API work into **framework-shaped primitives**
(this repo) and **deployment-shaped concerns** (the sister repo).
Framework primitives are constrained by published standards
(W3C SOSA, OGC SWE / OMS / SensorML, IETF RFC 7946). Deployment
concerns are constrained by use case (which conformance classes
to claim, which optional resources to expose, which examples to
ship).

The split is settled. Phase 7 of ADR-044 is the sister-repo
launch; this document is the reference it leans on.

## Phase 2 — Vocabulary sub-packages

| Package | What it provides | Use it for |
|---|---|---|
| [`vocabulary/sosa`](../../vocabulary/sosa) | W3C SOSA + SSN IRI constants (Sensor / Observation / Platform / Procedure / Sample / Result classes; observes / madeObservation / hasFeatureOfInterest / hosts / isHostedBy / usedProcedure / resultTime / phenomenonTime / hasResult / hasSimpleResult predicates; SSN System / Deployment classes; SSN hasDeployment / hasSubSystem / hasInput / hasOutput / hasProperty predicates) | RDF / Turtle / JSON-LD output, predicate registry mappings via `WithIRI(sosa.Observes)` etc. |
| [`vocabulary/swe`](../../vocabulary/swe) | OGC SWE Common v2.1 IRI constants (Quantity / Category / Time / Count / Boolean / Text / QuantityRange / DataRecord / DataArray / DataChoice / Vector types; label / definition / uom / value / nilValue / referenceFrame predicates) | Typing observation results, declaring SWE-shape fields |
| [`vocabulary/oms`](../../vocabulary/oms) | OGC OMS v3.0 IRI constants (Observation / ObservableProperty / Procedure / FeatureOfInterest / Result classes; resultTime / phenomenonTime / hasResult / hasFeatureOfInterest / observedProperty / usedProcedure predicates) | OMS-shape observation export |

Each package registers its prefix with [`vocabulary/export`](../../vocabulary/export)
at `init()` — importing the package is sufficient. Idempotent;
collision with a foreign namespace errors loud.

**Convention at a call site:**

```go
import (
    "github.com/c360studio/semstreams/vocabulary"
    "github.com/c360studio/semstreams/vocabulary/sosa"
)

// Register an app-level dotted predicate that resolves to a SOSA IRI:
vocabulary.Register("my.sensor.observes", vocabulary.WithIRI(sosa.Observes))

// Triples then carry the dotted name; vocabulary/export compacts to sosa:observes.
triples := []message.Triple{
    {Subject: entityID, Predicate: "my.sensor.observes", Object: someProperty},
}
```

## Phase 3 — GeoJSON + spatial extension

| Package | What it provides |
|---|---|
| [`graph/geo/geojson`](../../graph/geo/geojson) | RFC 7946 Point / MultiPoint / LineString / MultiLineString / Polygon / MultiPolygon / GeometryCollection / Feature / FeatureCollection types, polymorphic `UnmarshalGeometry` / `UnmarshalFeature` / `UnmarshalFeatureCollection`, WGS84 `Normalize`, `Polygon.Contains` ray-cast, `ComputeBBox` |
| `processor/graph-index-spatial` (extended) | New NATS subject `graph.spatial.query.polygon` accepts a GeoJSON Polygon envelope and returns entities whose indexed point falls inside the polygon (honoring holes) |

**Coordinate order.** RFC 7946 mandates `[longitude, latitude]`.
Use `geojson.NewPosition(lon, lat)` / `geojson.Position.Lon()` /
`Position.Lat()` to keep the order explicit at call sites.

**Containment query envelope:**

```json
{
    "polygon": {
        "type": "Polygon",
        "coordinates": [[ [0,0], [10,0], [10,10], [0,10], [0,0] ]]
    },
    "limit": 100
}
```

## Phase 4 — HTTP input

[`input/http`](../../input/http) — generic REST polling input
component.

| Capability | Notes |
|---|---|
| GET / POST with optional body | Method allow-list; POST body literal |
| Configurable poll interval (≥100ms floor) | Per-attempt timeout; retries get a fresh clock |
| Bearer / Basic / no-auth | `Authorization` header set after custom-headers iteration so framework auth wins over operator misconfig |
| Exponential backoff retry on network + 5xx | No retry on 4xx (client misconfig is not transient) |
| Decoder modes `json` or `jsonl` | Each decoded record wraps in a `core.json.v1` `BaseMessage` envelope before publish |
| 32 MiB response body cap | Oversized responses drain to preserve HTTP/1.1 keep-alive |
| Context cancellation cuts retry backoff | `select` arm |

Register as a normal component:

```go
import (
    httpinput "github.com/c360studio/semstreams/input/http"
)

if err := httpinput.Register(componentRegistry); err != nil { ... }
```

Config and port wiring follow the existing `input/{file,websocket}` shape.

## Phase 5 — SensorML parser + Graphable bridge

[`parser/sensorml`](../../parser/sensorml) — Go parser, emitter,
and Graphable adapter for the OGC SensorML JSON encoding bundled
with CS API v1.0.

**Coverage** (the four CS-API-critical types from a ~500-class spec):

- `PhysicalSystem` — composite hardware with `Components` + `Connections`
- `PhysicalComponent` — leaf hardware unit with `Method` reference
- `SimpleProcess` — leaf algorithmic / procedural unit
- `AggregateProcess` — composite process with children

**Graphable bridge** lives on [`sensorml.Asset`](../../parser/sensorml/graphable.go),
which pairs an operator-supplied 6-part SemStreams entity ID
with a parsed Process. Optional `ChildIDFn` resolves SensorML
document-local ids to 6-part ids for cross-system references.

```go
import (
    "github.com/c360studio/semstreams/parser/sensorml"
)

process, err := sensorml.UnmarshalProcess(data)
asset := sensorml.NewAsset("acme.ops.robotics.gcs.drone.001", process)
asset.ChildIDFn = func(localID string) string {
    return "acme.ops.robotics.gcs.drone.001/" + localID
}

// asset.EntityID() → "acme.ops.robotics.gcs.drone.001"
// asset.Triples() → SOSA / SSN-aligned triples
```

Importing the package registers its dotted predicates against
SOSA / SSN / DC / SKOS IRIs via `vocabulary.Register` at
`init()`. RDF / Turtle export through `vocabulary/export`
emits compacted `sosa:` / `ssn:` forms automatically.

## Phase 6 — OMS Observation payload

[`message/oms`](../../message/oms) — bidirectional mapper
between SemStreams `BaseMessage` envelopes and OGC OMS v3.0
Observation JSON documents.

| Capability | Notes |
|---|---|
| `Observation` struct implementing `message.Payload` | Schema type `ogc.oms.v3` — registered via `payloadbuiltins.Register` so every production binary picks it up automatically |
| Required-field validation | procedure, observedProperty, resultTime enforced at marshal-time |
| Polymorphic `FeatureOfInterest` | URI reference OR inline GeoJSON Feature (via `graph/geo/geojson`) |
| Graphable triples | rdf:type sosa:Observation + usedProcedure / observedProperty / hasFeatureOfInterest / resultTime / phenomenonTime / hasSimpleResult |
| Production round-trip exercised in tests | `payloadbuiltins.NewTestDecoder` round-trip proves the wire is decodable through the registry |

**OMS-natural JSON shape:**

```json
{
    "type": "Observation",
    "id": "acme.ops.robotics.gcs.drone.001/obs/12345",
    "procedure": "http://example.org/procedures/voltmeter",
    "observedProperty": "http://example.org/properties/battery-voltage",
    "featureOfInterest": "http://example.org/features/battery-pack-001",
    "phenomenonTime": "2026-05-15T14:30:00Z",
    "resultTime": "2026-05-15T14:30:00.250Z",
    "result": 12.4
}
```

`Observation.MarshalJSON` emits this shape; a `BaseMessage`
wrapping the payload places that JSON in its `payload` field, so
the same bytes flow through internal NATS publishes via
`message.NewDecoder` without re-encoding.

## How the primitives compose for a CS API server

The sister repo `semconnect` (working name) builds a CS API v1.0
server. Each CS API endpoint maps to a framework primitive
sequence:

```
CS API request →  input/http or HTTP framework gateway
              →   parser/sensorml (for system / component / procedure descriptions)
              →   message/oms (for observation publishes)
              →   message.BaseMessage envelope on NATS subject
              →   graph-ingest builds entity state
              →   graph/geo/geojson + graph-index-spatial for spatial queries
              →   vocabulary/export for RDF / Turtle responses
```

**Concrete endpoint mappings:**

| CS API path | Framework primitives |
|---|---|
| `GET /systems` | `graph-query` (lists `ssn:System` entities); response via `vocabulary/export` JSON-LD |
| `GET /systems/{id}` | `graph-query` (entity + relationships); SensorML JSON via `parser/sensorml` round-trip |
| `POST /systems/{id}/observations` | `parser/sensorml` validates the incoming SensorML; `message/oms` wraps the resulting Observation; publish to `cs-api.observations` JetStream subject |
| `GET /datastreams/{id}/observations` | KV watch on entity-keyed subject; serialize each `Observation` payload via `message/oms` marshal |
| `GET /areas?bbox=...` | `graph.spatial.query.bounds` (existing) — point-only |
| `GET /areas?polygon=...` | `graph.spatial.query.polygon` (Phase 3) — point-in-polygon |

The CS API server's job is endpoint routing, content negotiation,
auth, and conformance-class declaration. The data shape work is
done by the framework primitives.

## Phase 7 follow-up — SWE Common schema-bound encodings

Added by [ADR-050](../adr/050-swe-common-schema-bound-encodings.md). Closes [#116](https://github.com/C360Studio/semstreams/issues/116) — the semconnect Stage 27 upstream-ask carrying the `X-CS-SWE-Subset: observation-values` workaround.

| Primitive | What it provides | Use it for |
|-----------|------------------|------------|
| [`pkg/swecommon.DataComponent`](../../pkg/swecommon/component.go) | Sealed supertype for SWE Common data components | Type-switching across kinds in encoder dispatch |
| [`pkg/swecommon.DataRecord`](../../pkg/swecommon/schema.go) | Composite record of named typed fields | Result-schema + command-payload model |
| [`pkg/swecommon.Quantity / Count / Time / Boolean / Text / Category`](../../pkg/swecommon/component.go) | Scalar components with UoM / codeSpace / referenceFrame | Field-level typed values |
| [`pkg/swecommon.MarshalSchema / UnmarshalSchema`](../../pkg/swecommon/schema.go) | Round-trip OGC SWE Common JSON Encoding (22-022) schema document | Advertising a datastream's result schema |
| [`pkg/swecommon.EncodeJSON / DecodeJSON`](../../pkg/swecommon/json.go) | Schema-bound `application/swe+json` | Observation-collection responses + command JSON payloads |
| [`pkg/swecommon.EncodeText / DecodeText`](../../pkg/swecommon/text.go) | Schema-bound `application/swe+csv` (configurable separators) | CSV-shaped observation streams |
| [`pkg/swecommon.EncodeBinary / DecodeBinary`](../../pkg/swecommon/binary.go) | Schema-bound `application/swe+binary` (packed primitives + nil bitmap) | Binary observation streams + command payloads |
| [`pkg/swecommon.MediaSWEJSON / MediaSWECSV / MediaSWEBinary`](../../pkg/swecommon/media_types.go) | CS API media-type strings | Content negotiation in CS API gateways |

ADR-050 covers the scope cuts (DataArray / DataChoice / Vector /
constraints / nested records / XML encoding all deferred) and the
semconnect migration path.

## Scope cut — what's deferred to follow-up tags

ADR-044 explicitly defers these to future phases / ADRs (post-Phase 6):

- **SWE Common 3.0** — Phase 2 packages pin to v2.1 (the CS API v1.0 bundle). v3.0 lands as a sibling `vocabulary/swe3` rather than rebinding the existing constants.
- **OMS typed results** — Quantity / Category / TimeSeries result envelopes for `message/oms`. Phase 6 ships simple `any` results only; typed envelopes land together so the discriminator pattern picks once.
- **OMS time intervals** — Phase 6 ships ISO 8601 instants. `{begin, end}` interval shape deferred.
- **OMS ResultQuality, Parameter, ValidTime, RelatedObservation** — deferred per Phase 6 reviewer note. `ResultQuality` is the most likely first ask from a real CS API consumer.
- **SensorML Mode / ModeChoice / Algorithm / Configuration / DeployedSystem** — Phase 5 ships PhysicalSystem / PhysicalComponent / SimpleProcess / AggregateProcess. Mode + Algorithm are the next two most-asked-for.
- **SensorML auto-generated types from upstream JSON schema** — Phase 5 hand-writes the load-bearing subset. Auto-gen lands when coverage broadens beyond MVP.
- **HTTP input — SSE / streaming HTTP / WebSocket subscription** — Phase 4 ships REST polling only. SSE is the most likely follow-up.
- **HTTP input — dedicated `network.http.v1` payload type** — Phase 4 publishes via `core.json.v1`. A dedicated payload carrying `{url, status, latency_ms, decoded}` lands when a downstream consumer asks.
- **GeoJSON polygon-shaped entity indexing** — Phase 3 ships polygon-as-query against point-shaped entities. Polygon-shaped entities require a schema + lifecycle change deferred to a future phase.

Each is independently shippable as an additive tag. None of them block CS API v1.0 critical-path implementation in the sister repo.

## Pre-existing primitives the sister repo also leans on

These existed before ADR-044 and the sister repo uses them as-is:

- [`message.BaseMessage`](../../message/base_message.go) — wire envelope with payload registry
- [`message.NewDecoder`](../../message/decoder.go) — registry-aware unmarshaling
- [`graph.Graphable`](../../graph/graphable.go) — entity self-declaration via triples
- [`processor/graph-ingest`](../../processor/graph-ingest) — triple → entity-state KV
- [`processor/graph-query`](../../processor/graph-query) — entity / relationship queries
- [`vocabulary/export`](../../vocabulary/export) — RDF / Turtle / N-Triples / JSON-LD emit
- [`natsclient`](../../natsclient) — NATS connection management
- [`component`](../../component) — component lifecycle, ports, schema generation

## References

### Internal

- [ADR-044](../adr/044-ogc-connected-systems-framework-split.md) — the framework / sister-repo split decision
- [ADR-042](../adr/042-oasf-taxonomy-adoption.md) — vocabulary sub-package pattern this work mirrors

### External

- [OGC API Connected Systems standard](https://www.ogc.org/standards/ogc-api-connected-systems/)
- [CS API Part 1 (23-001)](https://docs.ogc.org/DRAFTS/23-001r0.html) — Feature Resources
- [CS API Part 2 (23-002)](https://docs.ogc.org/DRAFTS/23-002r0.html) — Dynamic Data
- [W3C SOSA/SSN Recommendation (2017)](https://www.w3.org/TR/vocab-ssn/)
- [IETF RFC 7946 — GeoJSON](https://datatracker.ietf.org/doc/html/rfc7946)
- [OGC SensorML (OGC 12-000r2)](https://www.ogc.org/standard/sensorml/)
- [OGC OMS v3.0 (20-082r4)](https://docs.ogc.org/as/20-082r4/20-082r4.html)
