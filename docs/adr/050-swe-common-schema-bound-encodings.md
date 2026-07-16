# ADR-050: SWE Common Schema-Bound Encodings as Framework Primitive

## Status

**Superseded by ADR-075 — 2026-07-15.** The schema-bound encoding contract remains useful, but its package and
conformance lifecycle are SemConnect-owned rather than SemStreams framework substrate.

**Accepted** — 2026-05-29 with the next tag bundle (Phase 1 of
ADR-050 lands as an additive framework primitive; tag will be cut
once any sibling work in flight has cleared). Closes
[#116](https://github.com/C360Studio/semstreams/issues/116) and
the corresponding upstream-ask
`semstreams-swe-schema-bound-encodings.md` in semconnect's
`docs/upstream-asks/` directory.

## Context

### What semconnect Stage 27 shipped

semconnect Stage 27 added CS API observation-read with three SWE
Common media types negotiable on the `GET /datastreams/{id}/observations`
endpoint: `application/swe+json`, `application/swe+csv`, and
`application/swe+binary`. Every response is tagged
`X-CS-SWE-Subset: observation-values` because the framework did not
yet expose schema-bound SWE Common record encodings; the gateway
projects each observation down to `{time, result}` and serializes
that two-column shape per the requested format.

That value-only projection is enough to round-trip primitive
sensor readings (a Quantity per observation), but it loses every
piece of structural information SWE Common is designed to carry —
field labels, units of measure, multi-field records, controlled-
vocabulary tokens, nil reasons, command-side parity. The
`X-CS-SWE-Subset` header is the explicit "we know this is not
SWE Common conformance" hedge: semconnect cannot claim the SWE
Common JSON / SWE Common Text / SWE Common Binary conformance
classes from CS API Part 2 §A.7.x with this subset.

### What every CS API consumer eventually needs

Both sides of the CS API contract need schema-bound encoders:

- **Observation read** (`GET /observations`) — a multi-field
  record per observation result, with the schema advertised on
  the datastream and the values streamed in the negotiated media
  type. The producer guarantees structural fidelity (UoMs survive,
  Time stays ISO 8601, Category tokens stay scoped to their code
  list, nil readings are tagged with the schema's `nilValue`).
- **Command payload** (`POST /controlstreams` once CS API Part 2
  command posting lands) — same encoding pipeline run in reverse:
  the consumer decodes the posted bytes against the controlstream's
  declared schema, validates structural conformance, and dispatches
  the typed command payload.

These two paths need the same model. Letting the gateway own one
shape and a future command processor own a different one
guarantees the SemSpec lifecycle case study repeats: parallel
hand-rolled conventions that have to be re-converged at v2 cost.

## Decision

Add a new framework package `pkg/swecommon` providing:

1. **Sealed component model.** A `DataComponent` interface with
   one concrete type per SWE Common scalar (`Quantity`, `Count`,
   `Time`, `Boolean`, `Text`, `Category`) plus the composite
   `DataRecord`. The interface is sealed by an unexported method
   so adding a new kind requires changing this package — every
   encoder dispatch table fails to compile until the new kind is
   handled.
2. **Schema marshal / unmarshal** in OGC SWE Common JSON Encoding
   (22-022) shape. Operators advertise a datastream's schema by
   serving this metadata document; consumers parse it back to a
   typed `*DataRecord` and drive the encoders.
3. **Three schema-bound encoder/decoder pairs:**
   - `application/swe+json` — JSON array of objects, one per row,
     field names as keys, RFC 3339 for Time.
   - `application/swe+csv` — SWE TextEncoding with configurable
     token + block separators (default comma + newline), decimal
     separator pinned to `.`, optional header row.
   - `application/swe+binary` — packed primitives with a
     per-record nil bitmap, big-endian by default, length-prefixed
     UTF-8 for variable-width fields.
4. **Media-type constants.** `MediaSWEJSON`, `MediaSWECSV`,
   `MediaSWEBinary` so consumers reference one declaration site.

### Why the sealed-interface shape

The same SWE-Common-XML-parser-evolution issue every other
implementation has hit: someone adds a new component kind
(`Vector`, `DataArray`, `DataChoice`) and the encoders silently
fall back to a generic shape that loses the new type's invariants.
A sealed interface with kind-exhaustive switches in each encoder
forces the maintainer to handle the new kind everywhere or fail
the build — the same pattern that has kept the orchestration
layer's `OperatorKind` table consistent through three rule-engine
expansions.

### Why the Phase 1 scope cut

Phase 1 covers everything CS API consumers need to drop
`X-CS-SWE-Subset: observation-values`:

- Three encoders + decoders end-to-end
- Scalar components + DataRecord
- UoMs, nil values, time, typed quantities, categories
- Round-trip tests covering each shape

Deferred (tracked in [#167](https://github.com/C360Studio/semstreams/issues/167), not blocking the conformance claim):

- DataArray (homogeneous element record) — useful for waveform /
  spectrum results, not in CS API Phase 1 observation use cases.
- DataChoice (discriminated union) — rare in CS API contexts.
- Vector (axis-frame-bound values) — specialized to geometry
  observations.
- Per-component constraints (`allowedValues`, ranges) — validation
  layer; producers can validate independently for now.
- Multi-reason NilValues block — Phase 1 supports a single
  `nilValue` stand-in token per field, which covers the "no
  reading" case CS API streams hit.
- Nested DataRecord values — the framework MVP is one flat record
  per row, matching every CS API observation/command body.
- SWE XML encoding — CS API does not require it.

### Why `pkg/swecommon` (not `message/swecommon`, not `encoding/swecommon`)

The package follows the `pkg/lifecycle` / `pkg/dispatch` /
`pkg/errs` shape: self-contained framework substrate that
multiple consumers import. It is not a payload type (no
`Schema()` / `Validate()` / `Payload` interface — it does not flow
through JetStream envelopes), so `message/` would mis-label it.
It is not the only `encoding/`-shaped subpackage we will ever
need but the `pkg/` location is consistent with every other
framework substrate package, and consumers grep for `pkg/` when
they want to find "framework-level shared primitives."

## Consequences

### Positive

- **semconnect drops `X-CS-SWE-Subset: observation-values`.** The
  gateway parses each datastream's advertised SWE schema, hands
  the schema + the unwrapped observation results to
  `swecommon.Encode{JSON,Text,Binary}`, and serves the response.
  CS API Part 2 SWE Common conformance classes become claimable.
- **One encoding contract for read and command paths.** When CS
  API Part 2 command posting lands, the same package's decoders
  validate the posted bytes against the controlstream schema.
- **Future framework consumers get the contract for free.** Other
  CS API frontends, MCP tools that surface observation
  collections, internal processors emitting SWE-shaped fixtures —
  all import the same package and dispatch through the same
  encoders.

### Negative

- **One more framework package to maintain.** Mitigated by the
  sealed-interface design — the scope of each kind is small and
  changes localize to one switch table per encoder.
- **Phase 1 deferrals (DataArray, DataChoice, Vector, etc.) leave
  some SWE Common shapes unencoded.** Operators with waveform or
  multi-element observation needs would have to extend the
  package before they can claim full SWE Common conformance.
  Reasonable cost: every CS API consumer using the framework
  today produces flat record observations.

### Migration path for semconnect

1. semconnect imports `github.com/c360studio/semstreams/pkg/swecommon`.
2. The datastream resource gains a `resultSchema` field carrying
   the marshaled SWE schema document. Stage 27 already accepts
   the field as opaque JSON; the new behavior is to parse it
   through `swecommon.UnmarshalSchema` at datastream-create time
   and reject bad shapes.
3. `gateway/cs-api/observations_get.go`'s three subset writers
   (`writeObservationsSWEJSON` / `writeObservationsSWECSV` /
   `writeObservationsSWEBinary`) replace their value-only
   projection with `swecommon.Encode{JSON,Text,Binary}` calls.
4. `X-CS-SWE-Subset: observation-values` header writes are
   removed. The conformance claim is filed.

The migration is the next semconnect stage, gated on this tag
landing on a published semstreams release.

## References

- OGC SWE Common Data Model 2.0 (08-094r1)
- OGC SWE Common JSON Encoding (22-022)
- OGC CS API Part 2 §A.7 (SWE Common conformance classes)
- [#116](https://github.com/C360Studio/semstreams/issues/116)
- semconnect upstream-ask
  `docs/upstream-asks/semstreams-swe-schema-bound-encodings.md`
- [ADR-044](044-ogc-connected-systems-framework-split.md) —
  framework / sister-repo split that established `pkg/swecommon`
  as the right home for this primitive.
