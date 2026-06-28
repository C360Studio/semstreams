# Spatial-Temporal Graph Queries

How to answer "give me the current-state entities **near this place**, **during this time**, scoped to
**these sources**" using the graph query primitives SemStreams already ships. This is a composition
recipe, not a new engine: SemStreams is not a GIS, routing, or mission-planning system, and this guide
does not make it one.

## When you need this

A product holds source-scoped current-state entities that carry a location and an observed-time, and it
wants to ask "what state applies near this place and time?" without scanning every entity. Examples:
sensor tracks in a region updated in the last minute; advisories valid now within a bounding box; assets
last seen inside a polygon. The shape recurs across products (COP snapshots, area queries, freshness
panels) — this is the canonical way to compose it.

## The primitives (what already exists)

Every piece below is a NATS request/reply subject. None of them is new.

| Concern | Subject | Request | Returns |
|---|---|---|---|
| Identity / type scope | `graph.query.prefix` | `{prefix, limit, cursor}` | Full `EntityState` values, paginated by opaque cursor |
| Space (rectangle) | `graph.query.spatial` → `graph.spatial.query.bounds` | `{north, south, east, west, limit}` | `[{id, type, lat, lon, alt}]` |
| Space (polygon) | `graph.spatial.query.polygon` | `{polygon: <GeoJSON Polygon>, limit}` | `[{id, type, lat, lon, alt}]` |
| Time (range) | `graph.query.temporal` → `graph.temporal.query.range` | `{startTime, endTime, limit}` (RFC3339) | `[{id, type}]` |
| Hydration | `graph.query.batch` | `{ids: [...]}` | `{entities: [EntityState, ...]}` |

Spatial results carry coordinates inline (no follow-up fetch to recover geometry). Temporal results carry
IDs only — hydrate via `graph.query.batch`, never per-ID `graph.query.entity` fan-out.

## The canonical contract: products normalize, the framework does not configure

There is exactly **one** indexed encoding for location and one for time. Products normalize their native
representation into it at ingest. The framework deliberately does **not** offer per-product or per-source
location/time predicate configuration — the moment it does, one index holds three incompatible geo
encodings (WKT here, GeoJSON there, a product predicate elsewhere) and every spatial query becomes a coin
flip across sources. One canonical contract; products project into it.

### Location

The spatial index reads **only** numeric `float64` triples under these predicates
(`processor/graph-index-spatial/component.go:690-708`):

- `geo.location.latitude` (or bare `latitude`)
- `geo.location.longitude` (or bare `longitude`)
- `geo.location.altitude` (or bare `altitude`) — optional

A WKT `POINT` string, a GeoJSON geometry, or a product-specific predicate (e.g. `cop.track.position`) is
**invisible** to the index. To be queryable, an entity must emit the numeric pair. Keep WKT/GeoJSON and
product predicates for rendering and inspection — they are not indexing inputs.

**Coordinate order.** SemStreams' GeoJSON helpers (`graph/geo/geojson`) are RFC 7946: positions are
`[longitude, latitude]`. WKT `POINT(x y)` is also longitude-first. But the spatial index sidesteps order
ambiguity entirely by using **named** numeric predicates — put latitude under `*.latitude` and longitude
under `*.longitude` and order never matters. If you parse WKT or GeoJSON product-side to derive the pair,
that is where you apply the lon-first convention.

### Time

The temporal index keys on the entity's **observation timestamp**, by explicit precedence
(`processor/graph-index-temporal/component.go`, `resolveIndexTimestamp`):

1. `time.observation.recorded` — **event-time** (the latest value when several are present). This is the
   canonical normalized predicate; emit it to make an entity queryable by *when it was observed*.
2. `UpdatedAt` — **processing-time** (last write), used only as a fallback when no observation predicate
   is present.

This is event-time-primary on purpose: every consumer of the temporal index is a "what's *in* this
window" range search, and write-time is a processing artifact (batched, delayed, replayed). Emitting
`time.observation.recorded` is the normalization step — the same discipline as the numeric lat/lon pair
on the spatial side. The `entities_indexed_total{source="observed"|"write_fallback"}` metric exposes how
many entities still fall back, so the fallback is observable rather than silent and can be retired as
producers adopt the predicate.

Re-observation **moves** an entity to its new bucket (the prior bucket is cleaned up), and entity deletion
removes it — so a range query never returns an entity from a window it has since left.

**Out of scope:** a general historical event store (arbitrary-cardinality "every observation ever"
replay). The temporal index holds an entity's *current* observed-time, not its full observation history;
products needing dense historical replay should keep that in their own store.

## The compose pattern

```text
1. Scope by identity/type   →  graph.query.prefix      →  candidate EntityStates (or IDs)
2. Scope by space           →  graph.query.spatial     →  IDs (+coords) in the box/polygon
3. Scope by time            →  graph.query.temporal    →  IDs fresh within the window
4. Intersect the ID sets    →  client-side set intersection
5. Hydrate the survivors    →  graph.query.batch       →  full EntityState values
```

Order the axes by selectivity — run the tightest filter first and carry its ID set forward, so later
axes (and the final `graph.query.batch`) operate on the smallest candidate set. For source-scoped COP
data the prefix or spatial axis is usually tightest.

If a product only needs two of the three axes (e.g. "fresh tracks in this box", no source scope), drop the
unused step. The spatial axis already returns coordinates, so a space-only query needs no hydration to draw
a map — only attribute-rich responses need step 5.

## Truncation and freshness diagnostics

Every axis has its **own** `limit`, and they are independent. The intersection of three independently
capped sets is lossy in a non-obvious way: an entity that would survive all three filters can be dropped
because it fell outside one axis's `limit` page even though it was inside the geometry/window. Guard
against this explicitly:

- Over-fetch per axis relative to the expected intersection size, or page each axis to exhaustion before
  intersecting when correctness matters more than latency.
- Treat any axis whose returned count equals its `limit` as **truncated** and surface that to the caller
  (e.g. a `truncated: {spatial, temporal, prefix}` field) rather than silently returning a partial
  intersection.
- `graph.query.prefix` paginates with an opaque cursor — follow it to completion for authoritative scope;
  a single page is a sample, not the set.

## Shapes: implemented, follow-up, non-goal

**Implemented today:**

- Bounds (rectangular) query via geohash cell enumeration.
- Single-`Polygon` containment via RFC 7946 ray-casting (`graph/geo/geojson` `Polygon.Contains`).

**Not implemented (candidate follow-ups if a product needs them — file an issue):**

- Radius / circle queries (compose client-side from a bounding box + post-filter today).
- Geohash-prefix queries.
- `MultiPolygon` containment (split into single polygons and union client-side today).
- Antimeridian-spanning geometry (split at ±180 product-side first).

**Non-goals (will not be added):**

- Full GIS operations — buffer, union, intersection, reprojection.
- Coordinate reference systems other than WGS84.
- In-framework WKT parsing (products parse WKT and emit the normalized numeric pair).
- Product-specific location/time predicate configuration (see the canonical-contract section).

## GraphQL and NATS are two doors to the same data

The `graph-gateway` GraphQL fields `spatialSearch(north,south,east,west,limit)` and
`temporalSearch(startTime,endTime,limit)` resolve by calling `graph.query.spatial` /
`graph.query.temporal`, which pass through to the index subjects above. Hosted processors that do not go
through GraphQL call the NATS subjects directly and get identical results. There is no separate query
engine behind GraphQL — choose the door that fits the caller.

## What is deliberately not here yet

There is **no single intersection query** that combines identity, space, and time server-side. The recipe
above is three calls plus a client-side intersect plus a batch hydrate. That is correct and cheap for the
sizes COP-style products see today. If this glue proves heavy across products — measured, not assumed — the
right framework addition is a product-neutral `graph.query.spatialTemporal` taking optional prefix/type
scope, optional bounds/polygon, optional time range, and a `hydrate` flag, returning IDs (+coords) or full
`EntityState` with explicit per-axis truncation diagnostics. It is gated behind real usage on purpose:
factor the typed contract once the composition is demonstrably worth a server-side join, not before.

## See also

- [Query Access](11-query-access.md) — GraphQL vs MCP vs NATS-direct decision guide.
- [Governed Semantic State](28-governed-semantic-state.md) — how current-state entities are authored.
- `graph/geo/geojson` — RFC 7946 geometry types, point-in-polygon, bbox.
- `processor/graph-index-spatial`, `processor/graph-index-temporal` — the index processors.
