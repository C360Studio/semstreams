# WebSocket output endpoint-path restoration design

## Accepted evidence and scope

- Accepted inventory: `docs/proposals/gh945-websocket-output-path-inventory.md`, body SHA-256
  `cfd1cba336e5cf86cb5f0bb5e770d2768d1b76851b9d01d90f5096c53a57e5d6`.
- Accepted complete design: `docs/proposals/gh945-websocket-output-path-design.md`; its final body hash is recorded in
  that artifact.
- Independent inventory review returned `INVENTORY PASS` and independent pre-owner design review returned
  `DESIGN REVIEW PASS`.
- Owner accepted R1-R8 on 2026-08-13.

`NetworkPort` remains the sole listener-binding owner. WebSocket output extends its existing component-local route
owner: `Config.path` defaults to `/ws`, the factory projects it into `ConstructorConfig.Path`, and the runtime's
private mux registers that effective path. No generic port surface changes.

## Adopter seam

The adopter is a developer outside this repository who can author only component JSON.

- **What must they know?** One fact: their desired route, supplied as component-root `path`. Ordinary users provide a
  literal absolute path; existing advanced callers retain valid path-only ServeMux patterns.
- **What happens if they do nothing?** The output serves `/ws`.
- **Where do they find out?** The generated component schema and corrected output documentation. Invalid syntax and
  the known stale `endpoint` spelling fail at construction with field context.
- **What should they have to know?** Nothing about constructor internals, mux registration, normalized port facts, or
  listener identity.

The intended external resource name cannot be observed or inferred by the framework; one component-owned value is
the minimal honest seam.

## Behavior

Omitted `path` retains `/ws`. Explicit empty path is invalid. An accepted value must be nonempty, begin with `/`,
contain no ASCII whitespace/control character, and be a valid Go `http.ServeMux` pattern when registered on an empty
scratch mux. This preserves root, trailing-slash, percent-escaped, and valid wildcard patterns while rejecting
non-path method/host patterns, full URLs, and syntactically invalid patterns. The same private validator applies to
JSON and direct Go construction before any live mux registration.

The known stale root `endpoint` key is rejected explicitly. No positive alias is decoded. Existing strict nested
port decoding continues to reject network `config.path` and legacy flat URL subjects.

The race-free changed-seam proof passes literal raw JSON through `CreateOutput`, materializes the production
`setupHTTPServer` handler, and serves that handler through `httptest.NewServer`. This proves the JSON-factory-to-live
WebSocket route without changing port-zero semantics or claiming new listener-bind coverage.

`configs/protocol-flow.json` selects a non-default route. The already-executed core-dataflow scenario performs a
direct upgrade and closes immediately, making `task e2e:core` falsifiable for the configured route. The shipped edge
fixture selects `/stream` to match its cloud input. The absent federation task/compose harness remains unclaimed.

## Binding-ruling conformance

No deviation was taken.
Paths named without a directory in R1-R4 and R6 are relative to `output/websocket/`.

| Ruling | Exact implementation evidence |
|---|---|
| R1 — Route ownership | `websocket.go:42-46,409-416`; `path_test.go:111-127` |
| R2 — Default and timing | `websocket.go:72-115,346-349,1661-1714`; `path_test.go:12-25,62-97` |
| R3 — Path-pattern grammar | `websocket.go:430-449,681-688`; `path_test.go:28-97` |
| R4 — Aliases | `websocket.go:1665-1672,1717-1725`; `path_test.go:99-109` |
| R5 — Fixture truth | `configs/edge-federation.json:112-118`; `configs/cloud-federation.json:83-86` |
| R6 — Production proof | `path_integration_test.go:21-78` |
| R7 — E2E proof | `configs/protocol-flow.json:273-279`; `test/e2e/scenarios/core_dataflow.go:15,112-124,149-162` |
| R8 — Specification | `openspec/changes/restore-websocket-output-path/specs/websocket-output/spec.md:1-65` |

R1's production references show the component-root field stored separately from unchanged output ports; its test
proves different paths retain equal network facts and resource IDs. R2's references show both `/ws` defaults,
pre-projection direct validation, JSON projection, omission coverage, and construction-time rejection.

R3's production references implement the shared scratch-mux validator and defensive live-mux check. Its tests cover
root, trailing slash, percent escape, wildcard, and invalid/non-path forms through both construction paths. R4 rejects
only root `endpoint`; its test also proves unrelated unknown-root handling was not broadened. No positive alias or
network-port change exists in the diff.

R5 repairs the shipped `/stream` pair without adding or claiming a federation harness. R6 drives literal raw JSON
through `CreateOutput`, `Initialize`, production `setupHTTPServer`, and its actual handler under
`httptest.NewServer`; it proves HTTP 101 at the custom route and 404 at `/ws` without changing or claiming output
listener/port-zero behavior. Lines 40-71 synchronize on the client registry's explicit zero-to-one transition before
closing the connection and its one-to-zero transition before `Wait`, so the proof cannot race a handler's `Add`.
Cleanup at lines 40-56 skips `Wait` unless registration was observed and uses bounded condition-based
`assert.Eventually` to repeat the zero observation after assertion failures; it contains no literal sleep polling.

R7 selects `/e2e-output` and makes the already-run scenario upgrade and immediately close that exact route. The E2E
result below records its non-skipped stage. R8 is the only capability delta; no ADR, generic-port, discovery,
registry, or generic runtime specification changed.
