# R3 evidence — GraphQL executor: library evaluation for the missing ruling

> **OWNER-SIDE REVIEW EVIDENCE — NOT A DESIGN EDIT.** Supports the added ruling requested in the post-GS-01
> design review ("library vs hand-written must be an explicit ruling"). Baseline
> `d1570ef81b23096021af0d7bf3321b4c08c7e54b`. Produced 2026-08-06. Library versions/maintenance reflect
> knowledge through early 2026 and must be re-pinned at adoption time.

## Requirements the executor must satisfy (from the design's proof gates)

- Real parsing and execution; schema validation; selection-set projection; conformant introspection;
  conformant error shape; variables, aliases, fragments (§6.3, §17 read-contract gate).
- Error `extensions` carrying operation-specific fields (`extensions.aliasCoveredThroughRevision`, §7.4).
- Query-only root; 20 canonical operations (§6.4); no mutation/subscription surface.
- Net-negative production code **after generated artifacts are excluded** (§17 complexity gate).
- Repo facts at baseline: **no existing GraphQL dependency in `go.mod`**; Go 1.26.3; the current facade is
  ~2,000 lines of hand-written substring routing in `gateway/graph-gateway/component.go` — the thing being
  deleted, and the cautionary tale for hand-writing "conformant" anything.

## Candidates

### 1. `99designs/gqlgen` (executor + codegen; parsing/validation via `vektah/gqlparser/v2`) — RECOMMENDED

- **Conformance**: full current-spec parse/validate/execute, variables, fragments, aliases, introspection,
  errors with path/locations/extensions. The proof-gate list is substantially the library's own contract;
  our tests pin behavior rather than implement it.
- **Fit with the design**: schema-first SDL — the 20-operation catalog becomes literally one `.graphql` file,
  which is the cleanest possible external projection of "the catalog is internal typed declaration data."
  Codegen emits typed resolver interfaces; resolvers are thin NATS-composite calls.
- **Selection projection**: the executor marshals only requested fields from resolver-returned structs.
  Providers still return full objects over NATS; projection happens at the gateway edge — satisfies the gate
  as written.
- **Error extensions**: first-class (`gqlerror.Error.Extensions`, custom error presenter).
- **Budget**: generated executor/models are excluded from the net-negative gate by the design's own wording.
  The repo already has a generate-and-check-diff discipline (`task schema:generate` + CI clean-diff gate);
  gqlgen slots into that workflow with zero new process.
- **Costs**: a codegen build step and tool-version pin; `go.sum` grows (gqlparser plus a handful of small
  runtime deps; websocket transport can simply not be imported — query-only needs POST/GET only).
- **Maintenance**: de-facto standard, active.

### 2. `graph-gophers/graphql-go` — fallback if the owner rejects codegen

- SDL parsed at runtime; resolvers are reflection-bound methods; schema/resolver mismatch fails at boot
  (acceptable — boot-time failure is the house preference over runtime drift).
- Small dependency footprint, no codegen step. Conformance good for this schema's shape; error extensions
  supported but less ergonomic; slower maintenance cadence; reflection binding is less type-safe than
  generated interfaces.

### 3. `graphql-go/graphql` — REJECT

Runtime schema-in-Go (no SDL), verbose, sporadic maintenance, weakest introspection/error ergonomics.

### 4. `gqlparser` + hand-rolled execution — REJECT explicitly

Parser/validator only; execution and introspection would be hand-written — that is the facade-v2 trap this
ruling exists to prevent, and the recovery arc's owned-wire-adapter lesson in miniature.

## Drafted ruling text

> The external graph-read API is implemented on an established conformant GraphQL library, primary
> `99designs/gqlgen`: schema-first SDL as the canonical operation catalog's external projection; generated
> executor and models excluded from the net-negative gate; gqlgen tool and `gqlparser` versions pinned and
> regenerated under the existing schema-generation CI clean-diff gate. Only the Query root exists. Error
> extensions carry operation-specific currency fields. Hand-writing a GraphQL parser, validator, executor, or
> introspection surface requires a separate owner ruling with evidence that an established library cannot
> satisfy a named proof gate. `graph-gophers/graphql-go` is the approved fallback if codegen is rejected.

## Recommended first implementation slice (the probe, analog to native-snapshot-probe)

Before committing the full migration: one operation (`entity`) end-to-end through gqlgen against the real
graph-ingest provider — SDL, generated resolver, NATS composite, error-extensions path, introspection on.
Success criteria: the read-contract proof gate's conformance tests pass for that one operation and the
generated-code exclusion keeps the slice net-negative against the facade lines it replaces. If the probe
fails a named gate, the fallback library runs the same probe before any hand-written line is considered.
