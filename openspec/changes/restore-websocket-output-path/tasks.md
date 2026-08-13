# Tasks: Restore WebSocket output endpoint path

## 1. Accepted inventory and design

- [x] 1.1 Materialize the complete path-owner, history, schema, consumer, fixture, E2E, and adopter-seam inventory.
- [x] 1.2 Obtain independent `INVENTORY PASS` and `DESIGN REVIEW PASS` after correcting both review findings.
- [x] 1.3 Record owner acceptance of R1-R8 on 2026-08-13.
- [x] 1.4 Materialize the accepted proposal, design, capability delta, and conservative task truth.

## 2. TDD implementation

- [x] 2.1 Add raw-JSON factory tests for custom/default paths, preserved valid path patterns, invalid/non-path values,
  loud stale-`endpoint` rejection, and unchanged `NetworkPort` facts/resource identity.
- [x] 2.2 Observe focused RED against the pre-change factory and record the exact failing assertions.
- [x] 2.3 Add the component-local path field, shared pre-mux validator, factory projection, and no compatibility
  aliases.
- [x] 2.4 Pass focused unit and unit-race GREEN.

## 3. Production handler and product E2E proof

- [x] 3.1 Add race-free real WebSocket proof through raw JSON, production factory, production mux, and
  `httptest.NewServer`; do not claim listener-bind or port-zero coverage.
- [x] 3.2 Observe integration RED before implementation and GREEN afterward.
- [x] 3.3 Repair edge/cloud `/stream`, select a non-default protocol-flow path, and add its direct upgrade stage to the
  already-run core-dataflow scenario.
- [x] 3.4 Run `task e2e:core` and record exact non-skipped handshake evidence; keep the absent federation harness
  unclaimed.

## 4. Schema, documentation, verification, and review

- [x] 4.1 Regenerate the WebSocket schema and correct README/package/example configuration truth.
- [x] 4.2 Record exact per-ruling `file:line` conformance with no unapproved deviation.
- [x] 4.3 Run focused unit/integration/race, lint, integration/live-LLM vet, repository race, CI-equivalent integration,
  build, schema no-drift, contract, strict OpenSpec, and relevant E2E gates.
- [x] 4.4 Obtain independent SemStreams implementation review approval.

## Implementation evidence

### TDD RED

- Pre-production focused unit command:

  ```bash
  go test ./output/websocket -run \
    'Test(CreateOutputPathFromRawJSON|CreateOutputAcceptsValidPathOnlyServeMuxPatterns|'\
  'NewOutputFromConfigAcceptsValidPathOnlyServeMuxPatterns|CreateOutputRejectsInvalidPathFromRawJSON|'\
  'NewOutputFromConfigRejectsInvalidPath|CreateOutputRejectsRetiredRootEndpointOnly|'\
  'CreateOutputPathDoesNotChangeListenerIdentity)$'
  ```

  It failed because raw `{"path":"/factory-proof"}` produced `/ws`, invalid JSON/direct paths returned no error,
  and root `endpoint` returned no error.
- Pre-production integration command:
  `go test -tags=integration ./output/websocket -run '^TestCreateOutputCustomPathServesProductionMux$'`.
  It failed at the custom-route dial with `websocket: bad handshake`.

### GREEN and release gates

- The two focused commands above passed after implementation.
- `go test ./test/e2e/scenarios ./output/websocket` — passed.
- `go test -race ./output/websocket` — passed.
- `go test -race -tags=integration ./output/websocket` — passed, including real NATS integration cases.
- Reviewer race correction:
  `go test -race -tags=integration ./output/websocket -run '^TestCreateOutputCustomPathServesProductionMux$' -count=100`
  — passed 100/100 after proving the registry's `0 -> 1 -> 0` transition before `Wait`.
- The full `go test -race -tags=integration ./output/websocket` package passed again after that correction.
- The focused path/config unit command passed again after that correction.
- PR #957 CI correction replaced cleanup's manual sleep polling with condition-based `assert.Eventually`, retaining
  both registration/removal observations and both guards on `Wait`. After correction:
  - `go test ./test/testinfra -run '^TestInfrastructurePolicyGuard$' -count=1` — passed.
  - The focused integration-race proof passed 100/100 again.
  - The full WebSocket integration-race package passed in 18.130s.
  - `go test ./output/websocket ./test/e2e/scenarios` — passed.
- `task lint` — passed (`go vet`, `go fmt`, revive, fixed-port guard, request guard).
- `go vet -tags=integration ./...` — passed.
- `go vet -tags=live_llm ./...` — passed without live model calls.
- `go test -race ./...` — passed.
- `task test:integration` — passed with integration tag and race detector.
- `task build` — passed; built `bin/semstreams`.
- `task schema:generate` — passed twice. Before/after second-run SHA-256 values were unchanged:
  `schemas/websocket.v1.json` `912b4bd34288c3973fa0836f9371c0b68633877907a739ccce2ad240164568eb` and
  `specs/openapi.v3.yaml` `11fd3e435f986c716c280718784595c410dd30d568a275a8e39d50938c577879`.
- `go test ./test/contract/...` — passed.
- `openspec validate restore-websocket-output-path --strict` — passed.
- `task e2e:core` — passed 3/3 (`core-health`, `core-dataflow`, `core-graph-roundtrip`). The dataflow result included
  non-skipped `verify-websocket-output-route_duration_ms:1`; teardown removed the app, NATS container, network, and
  volume.
- Independent SemStreams implementation review — `REVIEW PASS` after the reviewer identified and the developer
  corrected a test-only `WaitGroup.Add`/`Wait` ordering hole. The reviewer independently passed the corrected focused
  integration-race proof at `-count=100`, the full WebSocket integration-race package, unit/scenario regressions,
  `git diff --check`, and strict OpenSpec validation.
