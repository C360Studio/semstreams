# Tasks

## Contract and implementation

- [x] Inventory all declaration, carrier, consumer, schema, documentation, and observability surfaces.
- [x] Add canonical minimum/direction metadata and runtime validation.
- [x] Forward the field in ordinary managed consumers and bypass consumers.
- [x] Retain and identify component-owned agentic policies; reject nonzero declarations.
- [x] Require bounded context on port-backed consumption APIs and add the explicit non-port path.
- [x] Remove the ambiguous legacy stream consumer.
- [x] Add canonical metric registration and requested/effective/availability lifecycle.
- [x] Add direct OTEL observation with opaque cleanup.
- [x] Make generated schemas consume canonical port metadata.
- [x] Update package, operational, concept, example, and ADR documentation.

## Verification

- [x] Add canonical constraint, classifier, registry identity, metric lifecycle, schema, and call-site census tests.
- [x] Run focused unit tests with race detection.
- [x] Compile the real-NATS integration-tag suite.
- [x] Run the real-NATS positive, zero, unlimited, and in-place update integration tests with race detection.
      Evidence: `go test -tags=integration -race -count=1 -timeout=20m ./natsclient` passed in 76.655s.
- [x] Run `task lint`.
- [x] Run `go test -race ./...`.
- [x] Run `task schema:generate` and verify no generation drift remains.
- [x] Run `go test ./test/contract/...`.
- [x] Run relevant E2E tiers before merge because the exported API break is tagged BREAKING.
      Evidence: `task e2e:core` passed 3/3 and `task e2e:agentic` passed its scenario.
- [x] Obtain SemStreams reviewer approval.
      Evidence: final independent SemStreams implementation re-review returned `APPROVED` with no remaining findings.
