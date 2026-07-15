# ADR-044: OGC Connected Systems — Framework Primitives vs Sister-Repo Boundary

## Status

**Partially superseded by ADR-075 — 2026-07-15.** Generic HTTP and GeoJSON primitives remain framework-owned. OMS,
SensorML, SWE Common, CS API, and associated vocabulary move to SemConnect-owned composition.

**Proposed — 2026-05-14.** Tag scope: no breaking change. Doc-only ADR;
implementation phased and deferred. Independent of PR #77 (ADR-043
detonation corpus) and PR #76 (ADR-042 OASF scaffold) — different
files, different concerns.

Forcing function: sponsor evaluating semstreams as the substrate for
several adjacent sensor-related projects, with one concrete target
being a server implementing the
[OGC API – Connected Systems standard](https://www.ogc.org/standards/ogc-api-connected-systems/)
(v1.0 Parts 1 + 2 published; Parts 3–5 in draft). This ADR resolves
the architectural question of **what work belongs in semstreams vs. a
new sister repository** without committing to an implementation
schedule or a final sister-repo name.

## Context

### The opportunity

CS API v1.0 [defines RESTful interfaces](https://docs.ogc.org/DRAFTS/23-001r0.html)
bridging static metadata about observing systems (sensors, actuators,
platforms, robots, drones) with dynamic time-series data
(observations, commands, events). Built on
SOSA/SSN ontology, SensorML, OMS (Observations, Measurements, Samples),
SWE Common Data Model, and GeoJSON. RESTful HTTP + planned pub/sub
bindings ([Part 3](https://docs.ogc.org/DRAFTS/23-002r0.html), draft).

The data model and protocol shape align with semstreams to a degree
that is non-coincidental:

| CS API concept | semstreams primitive | Existing? |
|---|---|---|
| `System` (observing thing) | 6-part Entity ID's `system` segment (`org.platform.domain.system.type.instance`) | Yes — the ID format has a "system" position from inception. |
| `Deployment`, `Procedure`, `Sampler` | `Graphable` entities with typed triples | Yes — natural fit. |
| `Observation` (timestamped result with metadata) | `BaseMessage` + `Graphable.Triples()` | Yes — `graph-ingest` already does this. |
| `DataStream of Observations` (facts) | NATS KV Watch — state-as-events | Yes — per CLAUDE.md Facts/Requests split. |
| `ControlStream of Commands` (requests) | NATS JetStream Stream | Yes — per CLAUDE.md Facts/Requests split. |
| GeoJSON geometry | `lat/lon` triples + `graph-index-spatial` geohash index | Partial — points only; lines/polygons not covered. |
| SOSA/SSN ontology IRIs | `vocabulary/standards.go:465-471` already ships `SsnHasDeployment`, `SosaObserves`, `SosaHasSimpleResult`; `vocabulary/export/prefix.go:21-22` registers `sosa:`/`ssn:` prefixes | Partial — ad-hoc constants, not yet a sub-package. |

The vocabulary README already names "OGC compliance (GeoSPARQL,
SSN/SOSA)" as an existing design target. This is not retrofit; the
framework was shaped with SOSA-aligned semantics in mind.

### Multi-consumer reality

The sponsor's adjacent sensor-related projects represent **multiple
concurrent downstream consumers** for the same set of primitives:

- SOSA/SSN-aligned metadata predicates
- GeoJSON parsing for non-point geometries
- HTTP-based REST ingestion of external APIs
- SensorML JSON parsing (CS API v1.0-pinned)
- OMS observation document encoding

A single-consumer assumption ("only the CS API server needs this")
does not hold. Decisions made for one consumer should not lock
the others out.

### The "backport later" anti-pattern

A natural-sounding alternative is "start everything in the sister
repo; lift the cross-cutting bits into the framework later when a
second consumer appears." This pattern produces the opposite outcome
in practice: code born in a domain-specific repo accumulates
domain-specific dependencies, naming conventions, and integration
points. The eventual lift requires (a) re-justifying framework-fit
under non-original requirements, (b) cleaning up domain-leak in
imports and interfaces, and (c) breaking or duplicating the original
consumer during the migration. At meaningful frequency, the lift is
proposed, scoped, deprioritized, and never executed; the framework
carries the dependency externally instead.

The mitigation is to identify the framework-shaped work **at
inception**, ship it framework-side from the start, and accept the
discipline of co-shipping in-repo smoke tests so no dead code
accumulates while sister consumers come online.

### Why this work is scopeable now without enumerated use cases

Each candidate framework primitive is shaped by a **published
standard**, not by user requirements:

- SOSA/SSN — W3C TR (2017, stable)
- SWE Common — OGC 08-094r1 v2.1 + JSON encoding update bundled with CS API v1.0
- OMS — OGC 20-082r4 v3.0
- GeoJSON — IETF RFC 7946 (2016, stable)
- HTTP — RFC stack
- SensorML — OGC 12-000r2 + JSON encoding bundled with CS API v1.0

When the shape is dictated by a standard, scoping does not require
use-case enumeration — the spec defines the surface. Use cases come
in for the *server* side (which conformance classes to claim, which
optional resources to expose, which examples to ship), which is why
that work stays sister-side.

## Decision

### High-level split

```
┌──────────────────────────── semstreams (framework) ──────────────────────────┐
│                                                                              │
│  vocabulary/sosa     vocabulary/swe      vocabulary/oms                      │
│       │                  │                   │                               │
│       └──────────────────┴───────────────────┘                               │
│                          │                                                   │
│                          ▼                                                   │
│              parser/sensorml      message/oms                                │
│                                       │                                      │
│       graph/geo/geojson  ◀────────────┘                                      │
│              │                                                               │
│              ▼                                                               │
│       graph-index-spatial   (extended for non-point geometries)              │
│                                                                              │
│       input/http     (generic REST polling + optional WebSocket sub)         │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
                                       ▲
                                       │  Go module dep
                                       │
┌──────────────────────── semconnect (sister, working name) ───────────────────┐
│                                                                              │
│       gateway/cs-api/                                                        │
│           • RESTful Systems / Deployments / Procedures endpoints (Part 1)    │
│           • Datastreams / ControlStreams endpoints (Part 2)                  │
│           • OpenAPI conformance suite execution                              │
│                                                                              │
│       cmd/cs-api-server/    (reference deployment binary)                    │
│                                                                              │
│       examples/   (drone fleet, sensor network, robotic platform configs)    │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

### Framework-side work (semstreams)

All of the following ship in this repository, each bundled with
in-repo smoke tests so no dead code lingers between framework merge
and first sister-repo consumer.

**Tier A — Standards adoption (vocabulary sub-packages):**

| Package | Standard | Shape | Notes |
|---|---|---|---|
| `vocabulary/sosa` | W3C SOSA (2017) | IRI constants + dotted predicates + `Register()` helper | Promotes the ad-hoc SOSA constants currently in `vocabulary/standards.go` to a sub-package. Mirrors `vocabulary/oasf` pattern from ADR-042. |
| `vocabulary/swe` | OGC SWE Common Data Model v2.1 + JSON encoding | IRI constants for Quantity/Category/Time/Count/Boolean/Text fields | Domain typing for observation results. |
| `vocabulary/oms` | OGC OMS v3.0 | IRI constants for Observation/ObservableProperty/Procedure/FeatureOfInterest/Result | Standardized observation vocabulary used by Part 2 encoding. |

In-repo proof: contract test per package showing IRI ↔ dotted-name
roundtrip, RDF/Turtle export with new prefixes registered in
`vocabulary/export/prefix.go`.

**Tier B — Cross-cutting primitives:**

| Package | Standard | Shape | Notes |
|---|---|---|---|
| `graph/geo/geojson` | IETF RFC 7946 | Geometry types (Point, LineString, Polygon, MultiPoint, MultiLineString, MultiPolygon, Feature, FeatureCollection) + JSON marshal/unmarshal + WGS84 normalization | Hardens `graph-index-spatial` beyond point-only. Required for CS API and for every non-trivial geospatial pipeline. |
| `input/http` | RFC HTTP | REST polling input component with: configurable schedule, retry/backoff, header/bearer/basic auth, optional WebSocket subscription, response → payload decoder | Generic — value beyond sensors. Closes the current gap (no HTTP input today; only UDP, WebSocket, File, GitHub-webhook, CLI, a2a, slim). |

In-repo proofs: `graph-index-spatial` extended with a polygon-containment
fixture test; `input/http` smoke test against `httptest.Server` decoding
to a registered payload type.

**Tier C — Sensor-shaped framework primitives:**

| Package | Standard | Shape | Notes |
|---|---|---|---|
| `parser/sensorml` | OGC 12-000r2 + JSON encoding bundled with CS API v1.0 | SensorML JSON parser/emitter; produces `Graphable` entities with SOSA/SSN-aligned triples | Heavyweight schema (~500 classes); consider auto-generating Go types from upstream schema. Pin to CS API v1.0 version. |
| `message/oms` | OGC OMS v3.0 | Bidirectional mapper: semstreams `BaseMessage` ↔ OMS Observation document | Required by sister-repo CS API encoder; also valuable for any sensor pipeline producing OMS-shaped output. Depends on `vocabulary/oms`, `graph/geo/geojson`. |

In-repo proofs: roundtrip test against canonical SensorML JSON fixture
from OGC test corpus; `message/oms` roundtrip
`BaseMessage` → OMS JSON → `BaseMessage` preserving triples.

### Sister-repo work (working name: `semconnect`)

The following lives in a new sibling repository that depends on
`semstreams` as a Go module, following the established pattern of
`semspec`, `semteams`, `semembed`:

- `gateway/cs-api/` — RESTful endpoints exposing the framework's entity/observation/command primitives as Systems / Deployments / Procedures / DataStreams / ControlStreams. Implements the [CS API Part 1](https://docs.ogc.org/DRAFTS/23-001r0.html) and [Part 2](https://docs.ogc.org/DRAFTS/23-002r0.html) HTTP surface.
- Conformance test harness running against the official OGC suite.
- `cmd/cs-api-server/` reference deployment binary composing framework primitives into a turnkey CS-API-compliant server.
- Domain examples (drone fleet, sensor network, robotic platform configs).
- Container + Helm + deployment docs.

Name is provisional. Naming options considered: `semconnect`,
`semsystems`, `semsense`, `semogc`, `semobserve`. Final choice
deferred to sister-repo launch. `semconnect` is the working
placeholder because it reads naturally with adjacent OGC API
standards (SensorThings, EDR) without renaming if scope broadens.

The sister repo gets its own ADR (call it `ADR-S001` in the sister)
when launched. This ADR does not pre-commit the sister's structure
beyond "uses semstreams as a Go module, follows the established
sem-prefixed pattern."

### Why this is the right split

1. **Standards-shaped primitives don't need use-case enumeration.** Vocab/geo/parser/encoder packages are constrained by published specs, not user stories. Scoping is precise from day one.
2. **Multi-consumer from inception.** Sponsor's adjacent sensor projects ensure the framework primitives have downstream demand before sister-repo launch. The "complete system" PR-scope discipline holds via in-repo smoke tests.
3. **Sister repo stays thin.** `semconnect` becomes the *deployment* layer — spec-specific endpoints, conformance proofs, reference binary, domain configs — with no responsibility for general-purpose geo/HTTP/SOSA primitives. Operationally healthy.
4. **Standards drift absorbed downstream.** CS API Parts 3-5 are still in draft. When pub/sub bindings or sampling features finalize, the churn is contained to `semconnect` (gateway changes) rather than affecting framework consumers.
5. **Anti-pattern avoided.** Backport-later doesn't happen at meaningful frequency. Framework-side from inception is the only way to keep these primitives clean and aligned.

### Dependency order for implementation

Phases below are independently shippable and each completes a system.
Order respects dependency direction:

| Phase | Scope | Depends on |
|---|---|---|
| 1 | **This ADR (doc-only).** | — |
| 2 | **Tier A vocab packages** — `vocabulary/sosa`, `vocabulary/swe`, `vocabulary/oms`. Constants, IRI helpers, prefix registration, contract tests. | — |
| 3 | **`graph/geo/geojson`** — types, parser, emitter, normalization. Extend `graph-index-spatial` for non-point geometries. | — |
| 4 | **`input/http`** — REST polling input component with smoke test. | — |
| 5 | **`parser/sensorml`** — SensorML JSON parser/emitter. Roundtrip test against OGC canonical fixture. Pinned to CS API v1.0 schema version. | Phase 2 (`vocabulary/sosa`) |
| 6 | **`message/oms`** — OMS observation document mapper, bidirectional with `BaseMessage`. | Phase 2 (`vocabulary/oms`), Phase 3 (`graph/geo/geojson`) |
| 7 | **`semconnect` repo launched separately.** Sister-repo ADR-S001 captures gateway design, conformance approach, deployment shape. Out of scope for this ADR. | Phases 2–6 |

Each framework phase is self-contained — a sister-repo consumer is
not required for any framework phase to land cleanly, because the
in-repo smoke tests provide the proximate use that satisfies the
PR-scope discipline.

## Consequences

### Positive

- Multiple downstream sensor projects gain coherent framework support without per-project duplication.
- The framework's value proposition strengthens: "Go-native knowledge-graph substrate with SOSA/SSN/OMS-aligned ingestion and OGC-shaped output." Defensible positioning beyond the OGC use case.
- The AGNTCY-plus-OGC narrative becomes a single coherent technical claim: one knowledge-graph substrate serving both AGNTCY-compliant agent-skill directory (ADR-042) and OGC-compliant physical-system directory. Neither OpenSensorHub nor a pure-agent framework can tell this story.
- `vocabulary/sosa` formalizes constants that already exist ad-hoc in `vocabulary/standards.go` — net code reduction at the call sites once the sub-package lands.
- Each framework primitive is independently useful — `input/http` for any REST ingestion, `graph/geo/geojson` for any geospatial work, the vocabulary sub-packages for any standards-compliant semantic export.

### Negative

- Framework gains seven new packages (or six if `parser/sensorml` and `message/oms` are combined; not recommended). Maintenance surface grows.
- `parser/sensorml` is heavyweight — SensorML JSON schema is large and evolving. Locking to CS API v1.0 schema version is mandatory; chasing draft schema changes will be discipline-intensive.
- `input/http` is a perennial scope-creep risk. Must stay narrow: RESTful polling + optional WebSocket subscription, no fancy auth flows, no rate-limiting (use the rule engine for that), no streaming uploads.
- Standards version pinning becomes a framework-release concern. SOSA/SSN is stable but SWE Common, OMS, and SensorML JSON encodings have all moved with CS API v1.0; future CS API parts may move them again.
- The decision to ship framework-side before any sister consumer exists carries a small risk: if sponsor adjacent projects don't materialize as expected, several packages carry only smoke-test users. Mitigation: each phase's smoke test is independently valuable as a framework-coverage test, so the cost of unused-by-sister is bounded to the maintenance overhead of code already proven to work.

### Risks and open questions

- **SensorML schema generation strategy.** Two options: hand-write Go types matching the spec, or auto-generate from the upstream JSON schema. Auto-generation is more maintainable as the spec moves but adds a build-time dependency. Decision deferred to Phase 5.
- **Multi-consumer assumption.** This ADR's case for framework-side inclusion relies on sponsor's adjacent sensor projects being real consumers. If consumer count drops back to one before any framework phase ships, the calculus weakens — revisit before Phase 5/6 if the consumer roster materially changes.
- **SOSA vs SSN namespace tension.** Both vocabularies define overlapping concepts (`sosa:Observation` and `ssn:Observation`). Per W3C 2017 TR, SOSA is the lightweight subset; full SSN is the extended vocabulary. Default to SOSA; document explicit deviation if SSN-only concepts surface.
- **Conformance test ownership.** OGC conformance test runner is a real artifact (Team Engine, ETS). Whether to vendor it in `semconnect` or fetch on CI is a sister-repo decision deferred to ADR-S001.
- **CS API Part 3 pub/sub binding.** Once finalized, may affect whether `semconnect` uses native NATS pub/sub or proxies via MQTT/WebSocket for spec compliance. The framework provides the substrate; the binding choice is a sister-side decision.

## Alternatives considered

### All-in-semstreams (CS API server as `examples/cs-api/`)

Rejected. Frameworks attract examples; examples grow into products; products pull in domain-specific deps; the framework repo bloats with non-framework concerns. The established `sem*` pattern was designed precisely to avoid this. No `examples/` directory currently houses a major application in semstreams.

### All-in-semconnect (lift to framework only on second-consumer demand)

Rejected per the "backport later" anti-pattern discussion above. Code born in a domain repo accumulates domain dependencies and the lift rarely executes. With multiple downstream consumers already in flight, the framework-side decision is correct on first principles regardless of the anti-pattern.

### Hybrid: framework primitives only, sister-repo for spec-specific server

**Chosen.** Matches `semspec`, `semteams`, `semembed` precedent. Framework absorbs the standards-shaped general-utility work; sister absorbs spec-specific deployment work.

### Wait for second consumer before any framework work

Rejected. Multiple sensor projects already in flight; standards-shaped primitives don't require use-case validation; deferring forfeits the strategic positioning value of being early on a v1.0 standard.

### Vendor OpenSensorHub (Java) and skip framework work

Rejected as out-of-scope but worth noting: OSH is the dominant open-source CS API server, but in Java. Vendoring would mean operating a Java service alongside Go infrastructure, gaining nothing semstreams couldn't provide natively. The Go-server differentiator is part of the strategic case.

## References

### Internal

- ADR-016: Agentic governance layer (governance topology this work composes with)
- ADR-028: Rule-skeleton + coordinator + ops architecture
- ADR-039: Tool-call governance — rule-driven
- ADR-042: OASF taxonomy adoption (pattern this ADR mirrors for SOSA/SWE/OMS sub-packages)
- ADR-043: Prompt-injection defense via detonation corpus (parallel doc-only ADR; orthogonal scope)
- [`docs/operations/21-adr044-framework-primitives-reference.md`](../operations/21-adr044-framework-primitives-reference.md) — reference for Phase 2-6 primitives + CS API endpoint composition (Phase 7 deliverable)
- `vocabulary/standards.go:465-471` (existing ad-hoc SOSA constants that Phase 2 formalizes)
- `vocabulary/export/prefix.go:21-22` (existing SOSA/SSN prefix registrations)
- `processor/graph-index-spatial/` (point-only spatial index that Phase 3 extends)

### External

- [OGC API Connected Systems standard](https://www.ogc.org/standards/ogc-api-connected-systems/)
- [OGC announcement of CS API v1.0](https://www.ogc.org/announcement/ogc-announces-publication-of-ogc-api-connected-systems-and-updates-to-supporting-standards/)
- [CS API Part 1 (23-001) — Feature Resources](https://docs.ogc.org/DRAFTS/23-001r0.html)
- [CS API Part 2 (23-002) — Dynamic Data](https://docs.ogc.org/DRAFTS/23-002r0.html)
- [CS API SWG GitHub](https://github.com/opengeospatial/ogcapi-connected-systems)
- [OGC API portal — Connected Systems](https://ogcapi.ogc.org/connectedsystems/)
- [W3C SOSA/SSN Recommendation (2017)](https://www.w3.org/TR/vocab-ssn/)
- [IETF RFC 7946 — GeoJSON](https://datatracker.ietf.org/doc/html/rfc7946)
- [OpenSensorHub (OSH)](https://www.opensensorhub.org/) — reference Java implementation
- [GeoRobotix](https://georobotix.com/opensensorhub/) — commercial SaaS OSH fork
- [planetlabs/go-ogc](https://github.com/planetlabs/go-ogc) — Go OGC client utilities
- [FOSS4G NA 2024 — Open Source Implementations of CS API](https://talks.osgeo.org/foss4g-na-2024/talk/KHCLXW/)
