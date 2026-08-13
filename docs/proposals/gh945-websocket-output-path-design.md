# GitHub #945 WebSocket Output Endpoint-Path Complete Design Handoff

Baseline: `633400328864db7fbf16642189bf80ab7d464ac2`

Phase: `pre-owner-design`

Body SHA-256: `f7ad492254c244263f8b5c2d9d8f80766b89ecedbb15b947e1a350733f8df9d0`

Owner acceptance: R1-R8 were approved on 2026-08-13 after independent `DESIGN REVIEW PASS`.

## Complete handoff body

## Accepted inventory

The mandatory inventory is preserved without modification in
`docs/proposals/gh945-websocket-output-path-inventory.md` at the same repository baseline. Its accepted inventory-body
SHA-256 is `cfd1cba336e5cf86cb5f0bb5e770d2768d1b76851b9d01d90f5096c53a57e5d6`; independent review returned
`INVENTORY PASS` after correction.

The complete inventory, collision/ownership table, consumer-at-birth inventory, adopter seam, history, empty searches,
and fixture/E2E census are binding inputs to this design. In particular:

- JSON `output/websocket.Config` has no path while `ConstructorConfig.Path` and `Output.path` already exist.
- `CreateOutput` hard-codes `/ws`.
- `NetworkPort` and `NetworkFacts` contain only protocol, host, and port.
- The private mux registers `Output.path` directly; malformed patterns can panic late.
- Generated `schemas/websocket.v1.json` has no route property.
- The accepted same-class route census includes WebSocket input server path, HTTP/graph/lifecycle gateway routes,
  standalone `MetricsConfig.Path`, and FlowService's fixed `prefix + "status/stream"` WebSocket route.
- `configs/edge-federation.json` omits a route while `configs/cloud-federation.json` dials `/stream`.
- The already-run core E2E excludes the existing federation scenario, so no executed JSON-factory-to-custom-route proof
  exists.

## Binding architectural ruling

Already owner-approved:

> `NetworkPort` remains listener identity: protocol, host, and port. The literal WebSocket route belongs to
> `output/websocket.Config`, defaults to `/ws`, and invalid routes fail configuration validation before mux
> registration.

This ruling is binding. It does not approve the remaining proposed details below.

## Options considered

### Option 0 — Do nothing

Keep `/ws` hard-coded in the JSON factory. This preserves the beta.160 narrowing, leaves `ConstructorConfig.Path`
reachable only to direct Go callers, leaves edge/cloud `/stream` inconsistent, and forces external JSON authors to
discover the hidden route through a failed handshake.

### Option 1 — Add route data to `NetworkPort`

This changes generic listener identity, codec, normalized facts, resource IDs, conflict behavior, discovery, schema,
and every external network-port author. No generic reader currently consumes it, and it mixes HTTP/WebSocket resource
naming with transport binding. It conflicts with the approved ruling.

### Option 2 — Add a component-local full endpoint URL

This duplicates protocol/host/port already owned by `NetworkPort`, requires conflict resolution between two
authorities, and recreates the retired URL-in-`subject` shape under another name.

### Option 3 — Add `Config.Path` and project it into the existing constructor path

This adds one component-specific JSON/schema field plus validation, documentation, fixtures, and tests. It preserves
one listener authority and reopens the already-existing runtime route seam.

## Recommendation

Choose Option 3.

Add `Path string` to `output/websocket.Config` with JSON key `path` and schema default `/ws`. `DefaultConfig` supplies
`/ws`; `CreateOutput` copies the effective value to `ConstructorConfig.Path`. `NetworkPort`, `NetworkFacts`, resource
identity, exclusivity, and discovery remain unchanged.

Use one unexported route validator from both `Config.Validate`/factory construction and `NewOutputFromConfig`, so JSON
and direct Go construction obey identical rules. Validation completes before an `Output` can reach
`setupHTTPServer`.

### Preserved path-pattern contract

The existing exported Go seam accepts every nonempty value and passes it to `http.ServeMux.HandleFunc`. Observed
direct-constructor values are `/ws`, `/test`, `/stream`, `/dashboard/stream`, `/monitor`, and `/events`; the wider
valid-ServeMux-pattern affordance is public even though current tests do not exhaust it. The restoration does not
silently narrow that contract.

The accepted value is a **path-only valid `http.ServeMux` pattern**:

- it is nonempty and begins with `/`, excluding method and host patterns;
- it contains no ASCII whitespace or control character and is not a full URL;
- registration on an otherwise empty scratch `http.ServeMux` succeeds without panic.

Consequently `/`, trailing-slash subtree patterns, percent-escaped segments, and valid Go 1.22 path wildcards remain
accepted. Missing leading slash, method/host patterns, full URLs, and syntactically invalid ServeMux patterns fail.
This preserves the direct-constructor contract while preventing a config value from selecting a method or host
namespace outside the component's route responsibility. It also converts the late `HandleFunc` panic into a typed
construction error. Root `/` intentionally retains ServeMux's catch-all semantics because the output owns a private
mux with one application handler.

Omission means `/ws`. Explicit `path: ""` is invalid and does not silently regain the default. The validator is
documented as following the Go version's `http.ServeMux` path-pattern grammar rather than inventing a second regex
grammar.

### Error timing and aliases

Invalid path configuration returns typed invalid-config context from `CreateOutput`/`NewOutputFromConfig` before
initialization, startup, listener allocation, or mux registration. `setupHTTPServer` may retain a defensive check but
is not the primary validation point.

No legacy alias is introduced:

- Do not accept URL-in-port `subject`; canonical port decoding already rejects it.
- Do not accept `url`, `websocket_path`, or nested network `config.path`.
- Add a targeted rejection for the known stale root `endpoint` key so it no longer boots while doing nothing. Do not
  broaden this issue into a global `SafeUnmarshal` policy change.

No live `UpdateConfig` hook is added. Existing changed-component replacement/restart semantics remain authoritative.

## Schema, documentation, and fixture disposition

- Regenerate `schemas/websocket.v1.json`; expose `path` as string with default `/ws`.
- Update `output/websocket/README.md` and `output/websocket/doc.go` to show component-root `path` and remove
  URL-in-`subject` claims.
- Replace stale `config/example_config.json` `endpoint` with canonical `path: /stream`.
- Add `path: /stream` to `configs/edge-federation.json`; retain the cloud input URL ending in `/stream`. This repairs
  shipped pair consistency but does not claim federation E2E coverage.
- Keep existing direct-constructor `/stream` tests as useful custom-route coverage, while making clear they do not
  prove JSON reachability.
- Give `configs/protocol-flow.json` a deliberately non-default path such as `/e2e-output`.

## TDD and evidence plan

Production-seam tests begin RED before implementation:

1. A literal raw JSON body passed to production `CreateOutput` with `path: /factory-proof` produces an output whose
   effective path is `/factory-proof`. Do not marshal a typed helper; the existing test helper ignores its path
   argument and cannot prove JSON reachability.
2. Omitted path produces `/ws`; explicit empty, missing-leading-slash, method/host, full-URL, whitespace/control, and
   invalid-ServeMux patterns fail during construction. Root, trailing-slash, percent-escaped, and valid wildcard
   patterns remain accepted through both JSON and direct construction.
3. The stale `endpoint` key fails loudly and never acts as an alias.
4. Custom path changes neither normalized network facts nor `NetworkPort.ResourceID`.
5. Generated schema includes the field and default.

Add a race-free ephemeral real HTTP/WebSocket integration proof through the production JSON factory and production
mux. Construct from literal raw JSON with a custom path and an otherwise valid fixed `NetworkPort`, initialize it,
and call the production `setupHTTPServer` to materialize the exact `http.Server.Handler` used by `Start`. Serve that
handler with `httptest.NewServer`, whose kernel-owned `:0` listener provides the selected address without a
close-and-rebind window. Successfully receive HTTP 101 at the custom path and show `/ws` does not upgrade, using real
`gorilla/websocket`. Do not call `handleWebSocket` directly and do not call this a proof of the already-covered
`Output` listener bind; it proves the changed JSON-factory-to-production-mux route seam. Production port validation
and `NetworkPort` semantics remain untouched.

For bounded E2E, extend the already-executed `core-dataflow` scenario with a direct handshake stage against the custom
route declared in `configs/protocol-flow.json`; compose already exposes output port 8082 as host 38082. The stage fails
on non-101 and closes immediately after successful upgrade. This is preferred over expanding the absent federation
task/compose harness: the latter is a separate coverage repair, while core-dataflow already runs under
`task e2e:core`.

Required verification:

```text
go test ./output/websocket
go test -race ./output/websocket
go test -race -tags=integration ./output/websocket
task lint
go vet -tags=integration ./...
go vet -tags=live_llm ./...
go test -race ./...
task test:integration
task build
task schema:generate
git diff --exit-code -- schemas specs/openapi.v3.yaml
go test ./test/contract/...
openspec validate restore-websocket-output-path --strict
task e2e:core
```

## Adopter seam

Specific adopter: a SemSource developer authoring only component JSON.

- **What must they know?** One fact: the desired route, supplied as component-root `path`. Ordinary adopters use a
  literal absolute path; existing advanced Go/JSON callers retain valid path-only ServeMux patterns.
- **What happens if they do nothing?** The server deterministically listens at `/ws`.
- **What happens if they are wrong?** Invalid syntax or the known retired alias fails at boot with component/field
  context; a valid different client route still receives an ordinary failed handshake.
- **Where do they find out?** Generated schema and component docs; correctness is enforced earlier by boot-time typed
  validation.
- **What should they have to know?** Only their desired resource name. They need not know constructor internals, mux
  patterns, port normalization, or listener resource identity.

This is not prediction-shaped configuration: the framework cannot observe which external route the adopter intends.
The correct seam is one explicit component-owned value with a safe default.

## Scoped files

Expected scope:

- `output/websocket/websocket.go`
- focused `output/websocket/*_test.go` files
- `schemas/websocket.v1.json`
- `output/websocket/README.md`
- `output/websocket/doc.go`
- `config/example_config.json`
- `configs/edge-federation.json`
- `configs/protocol-flow.json`
- `test/e2e/scenarios/core_dataflow.go`
- `cmd/e2e/main.go` only if endpoint injection is necessary; avoid a new CLI flag when the core fixture supplies one
  stable endpoint
- `openspec/changes/restore-websocket-output-path/**`

No `component/port_*`, discovery, registry, NATS, payload, query, orchestration, or ADR changes are in scope. No shared
decision skill triggers: this adds no new NATS communication primitive, orchestration, payload type, or query access
pattern.

## OpenSpec target

Create change `restore-websocket-output-path` and add the following to the `websocket-output` capability:

- WebSocket output owns one configurable path-only route pattern.
- Omitted `path` serves `/ws`.
- A raw JSON custom `path` is the registered upgrade route.
- An invalid or non-path ServeMux pattern fails before mux registration.
- Retired endpoint/port aliases are rejected and do not repair configuration.
- Route choice leaves listener port facts and identity unchanged.
- Core E2E handshakes through the custom protocol-flow route.

No delta to `component-runtime-config` or `component-discovery` is required because generic network contract does not
change. No ADR is warranted: this restores component mechanics under the approved listener-versus-route boundary.

## Binding and proposed owner rulings

| Ruling | Status | Target |
|---|---|---|
| R1 — Route ownership | OWNER APPROVED 2026-08-13 | `NetworkPort` remains protocol/host/port listener identity; output `Config.path` owns the literal route. |
| R2 — Default and timing | OWNER APPROVED 2026-08-13 | Omission defaults `/ws`; invalid routes fail before mux registration. |
| R3 — Path-pattern grammar | OWNER APPROVED 2026-08-13 | Preserve every valid path-only ServeMux pattern, including `/`, trailing slash, percent escapes, and valid wildcards; reject empty, non-path, method/host, full-URL, whitespace/control, and invalid ServeMux patterns before mux registration. |
| R4 — Aliases | OWNER APPROVED 2026-08-13 | Add no positive alias; reject known stale root `endpoint` loudly. |
| R5 — Fixture truth | OWNER APPROVED 2026-08-13 | Repair edge/cloud `/stream` consistency now; do not claim the absent federation harness. |
| R6 — Production proof | OWNER APPROVED 2026-08-13 | Require raw-JSON factory to production `setupHTTPServer` mux proof served by `httptest.NewServer`; do not alter port-zero semantics or claim listener-bind coverage. |
| R7 — E2E proof | OWNER APPROVED 2026-08-13 | Extend already-run core-dataflow with a custom output-route handshake. |
| R8 — Specification | OWNER APPROVED 2026-08-13 | Add only the websocket-output capability delta; no ADR or generic-port spec change. |
