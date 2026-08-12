# Tasks

- [x] Add failing unit and real-NATS coverage for the exact circuit-neutral capacity refusal set and false cases
      (`natsclient/stream_capacity_circuit_test.go:13-85`,
      `natsclient/stream_capacity_circuit_integration_test.go:14-146`).
- [x] Apply one private classifier/accounting helper at all three publish-failure accounting seams
      (`natsclient/client.go:309-334,1035-1053`; `natsclient/stream.go:650-655`).
- [x] Preserve caller-visible sync, acknowledged, async-future, and batch error behavior
      (`openspec/changes/stream-capacity-rejection-is-circuit-neutral/design.md`).
- [x] Record the per-ruling conformance table and adopter seam inventory
      (`openspec/changes/stream-capacity-rejection-is-circuit-neutral/design.md`).
- [ ] Obtain SemStreams reviewer approval and run the required integration gates before archive.
