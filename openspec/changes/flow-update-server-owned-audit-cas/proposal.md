# Proposal — flow-update-server-owned-audit-cas

Slice A of the accepted flow-CRUD design (`docs/proposals/gh1008-1010-flow-crud-design.md`, owner-accepted
2026-08-23; rulings 2, 3, and 9). Closes #1009.

## Why

`flowstore.Manager.Update` marshals the caller's `*Flow` wholesale after re-stamping only `Version`, `UpdatedAt`,
and `LastModified` (`flowstore/manager.go:132-145`). It reads the stored record (`:120`) but uses it only for the
version comparison (`:126`), so the stored `CreatedAt` is never restored: an omitted `created_at` persists Go's zero
`time.Time`, and a supplied one is stored verbatim. The semstreams-ui editor save path sends no timestamps, so every
save destroys provenance silently (#1009).

The same function promises optimistic concurrency (`flowstore/flow.go:18-19`, `flowstore/doc.go:8-10`, ADR-096
`:18-19`, OpenAPI `service/flow_service.go:225`) but persists with `KVStore.Put`, which is last-writer-wins
(`natsclient/kv.go:184-198`). Two Managers over the same `semstreams_flows` bucket — FlowService owns one
(`service/flow_service.go:75`), the FlowExecutor composition owns another (`cmd/semstreams/main.go:245,711`) — can
both pass the in-memory version check and both write. The revision fence already exists
(`natsclient.KVStore.Update`, `natsclient/kv.go:222`; `ErrKVRevisionMismatch`, `:652`) and is unused here.

The HTTP layer recognises a conflict by the substring `"conflict"` in the error text (`service/flow_service.go:314`),
the message-parsing shape `openspec/specs/nats-kv-keys/spec.md` forbids for classified failures.

## What Changes

- **`Manager.Update` owns the audit timestamps.** The candidate written to KV is a copy of the request with
  `CreatedAt` restored from the stored record, `Version` set to stored version + 1, and one server `now` assigned to
  both `UpdatedAt` and `LastModified`. `CreatedBy` remains caller-preserved (ruling: not expanded to timestamp
  ownership).
- **The request version is a precondition, not a value.** It must equal the stored version; it is never stored as
  sent.
- **Persist is revision-fenced.** Update reads the stored value and its KV revision, and writes with
  `KVStore.Update(ctx, key, bytes, observedRevision)`. Under concurrent Update through any number of Managers, exactly
  one writer commits (ruling 2, CAS repair inside #1009).
- **One typed conflict.** Logical version mismatch and `natsclient.ErrKVRevisionMismatch` both surface as the existing
  ADR-060 optimistic-concurrency sentinel: a classified `ErrorInvalid` error carrying code `revision_mismatch`, so
  `errors.Is(err, errs.ErrRevisionMismatch)` is true (`pkg/errs/errs.go:386-390`). No new exported sentinel;
  `graph-ingest` already translates the same KV failure to the same code
  (`processor/graph-ingest/canonical_mutations.go:308-311`).
- **Copy-on-write, success-only caller mutation (ruling 3).** The caller's `*Flow` is untouched on every failure
  path and is assigned the committed record only after the fenced write succeeds.
- **`PUT /flows/{id}` projects the typed conflict as 409 by classification**, replacing the `"conflict"` substring
  match. Body shape (`{"error": ...}`) is unchanged; exact public message text is Slice C's.
- **Request-schema separation (ruling 9).** `FlowCreateRequest` (requires `name`, `nodes`, `connections`; optional
  `id`, `description`, `created_by`; no `version`, no timestamps) and `FlowUpdateRequest` (requires `id`, `version`,
  `name`, `nodes`, `connections`; optional `description`, `created_by`; no timestamps) become the POST and PUT request
  bodies in the generated OpenAPI. `Flow` remains the response schema and the validate-draft request schema. Legacy
  full-`Flow` bodies keep decoding: unknown fields are ignored, Create ignores version/timestamps as it already does,
  Update uses `version` only as the precondition and ignores timestamps.

### Consumers

- semstreams-ui: editor `saveFlow` (timestamp-free body) and `flowApi.updateFlow` (full-`Flow` body) — both keep
  working; the editor E2E `flow-crud.spec.ts` "Flow timestamps update correctly on save" is the sister-owned tracking
  signal.
- FlowExecutor `update_flow` (`processor/agentic-tools/executors/flows.go:150-160`): unchanged code; its
  full-`Flow` argument is a legacy body whose timestamps are now ignored.
- Generated OpenAPI clients gain the two request schemas.

## Non-goals

- #1010 current-state List (Slice B), #1008 invalid-handling vocabulary, exact HTTP error messages, must-exist DELETE,
  404 for a missing Update target (Slice C), and the six Get projections (Slice D). Slice A leaves every non-conflict
  Update failure with its current classification and status.
- No ADR: Slice A conforms to ADR-096's existing CAS promise. Weakening CAS would need a superseding ADR.
- No NATS migration, bucket change, or stored-record shape change. Records already persisted with a zero `created_at`
  are not repaired (pre-v1 fresh-state policy).
- No runtime rejection of `"nodes": null` / `"connections": null`; the schemas declare the arrays required and typed,
  runtime non-null enforcement is Slice C's structural validation.
- No change to the validate-draft request schema or the `Flow` response schema.
- No exported test knob on `Manager`; the two-Manager proof pauses through an unexported package-private seam.
- No new exported surface on `natsclient`, `graph`, `message`, or `pkg/*`.

## Impact

- **Affected spec:** `flow-authoring` (ADDED requirements; no existing requirement text changes).
- **Affected code:** `flowstore/manager.go` (Update), `flowstore/manager_integration_test.go`,
  `service/flow_service.go` (request types, decode, 409 classification, OpenAPI request refs/types),
  `service/flow_surface_test.go`, `service/flow_service_test.go`, `specs/openapi.v3.yaml` (regenerated).
- **Not breaking.** Additive schemas; legacy bodies decode; the `Manager.Update` signature is unchanged. No BREAKING
  commit, so no e2e tier is mandated by the hard rule; the concurrency proof is a real-NATS integration test.
- **Rollback boundary:** one PR. Reverting restores Get/compare/Put and removes the request types and their schema
  rows on regeneration; stored bytes are the same `Flow` JSON either way, so no data step is needed. Slices B, C, D
  do not depend on this revert.
