# Tasks: Durable MaxDeliver occurrence visibility

## 1. Inventory and design

- [x] 1.1 Inventory current provisioning, ObjectStore consumer, advisory, metrics, logging, and boot-order owners.
- [x] 1.2 Apply `kv-or-stream`: choose a retained JetStream occurrence ledger because restart resumes unacknowledged
  work and replicas share one logical delivery.
- [x] 1.3 Record adopter seam, options, retention sizing, ACL, completeness, cluster, and duplicate-emission semantics.

## 2. TDD implementation

- [x] 2.1 Observe RED unit/boot tests for missing typed decoder, disposition, declarations, and two-binary wiring.
- [x] 2.2 Implement fixed stream provisioning and reconciliation without an adopter config surface.
- [x] 2.3 Implement strict typed decoding, bounded-label metrics, structured ERROR logging, ACK-after-emission,
  telemetry-failure NAK, poison decoder telemetry+ACK, and unlimited observer delivery.
- [x] 2.4 Wire `cmd/semstreams` and `cmd/e2e-semstreams` after capture provisioning and before `Manager.StartAll`.

## 3. Real-NATS and assembled proof

- [x] 3.1 Prove on real NATS 2.12.4 that an occurrence emitted before observer bind is retained and delivered later.
- [x] 3.2 Prove telemetry failure redelivers, two observer processes share one logical delivery, the fresh stream is
  bounded DiscardOld, and the observer durable is unlimited.
- [x] 3.3 Prove test-side sealing makes a held ObjectStore handle fail deterministically without a production knob.
- [x] 3.4 Run the assembled core E2E: shipped ObjectStore raw lane, test-administered MaxDeliver=1, sealed backing stream,
  retained typed event whose `stream_seq` equals the marker publish PubAck sequence, and Prometheus occurrence signal.
- [x] 3.5 Prove restrictive authorization with disposable NATS config files: a sufficient scoped
  stream/consumer/inbox/ACK grant set succeeds without a direct advisory subscription; missing stream API or inbox
  fails boot, missing STREAM.UPDATE fails drift reconciliation, and missing consumer-create fails binding. Assert the
  handled advisory advances the fixed durable ACK floor and does not redeliver. Prove the intentional R=1 declaration
  on a disposable three-node JetStream cluster: all nodes have two routes and current metadata, one leader reports all
  three peers, clients on different nodes share the fixed durable, retain one advisory, and handle it once. No
  replicated-storage availability claim is made for R=1.

## 4. Verification and review

- [x] 4.1 Focused unit and race tests: `go test` and `go test -race` across `config`, `internal/maxdelivery`, and both
  binary packages passed on the final implementation.
- [x] 4.2 Focused real-NATS integration tests: `go test -race -tags=integration ./config ./internal/maxdelivery`
  passed, including restrictive auth and a disposable three-node cluster.
- [x] 4.3 Final gates passed: `task lint`; `go test -race ./...`; `task schema:generate` with no `schemas/` or
  `specs/` drift; `go test ./test/contract/...`; `go mod tidy -diff`; strict OpenSpec validation (43/43); and
  `task e2e:core` (3/3, exact marker PubAck/advisory sequence equality, clean teardown).
- [x] 4.4 Record current-session independent `semstreams-reviewer` approval with no findings. Focused race proof
  passed for `config` (1.552s), `internal/maxdelivery` (1.402s), and scenario helpers (1.404s). Sequential real-NATS
  proof passed for `config` (28.088s) and `internal/maxdelivery` (9.644s), including restrictive authorization and
  disposable three-node coverage. PR #948's GitHub review collection remains empty; this is session-local evidence,
  not retroactive GitHub approval.

## Binding ruling conformance table

| Binding ruling | Implementation evidence or deviation |
|---|---|
| Durable occurrence ledger; no current-count inference/redrive/API/retry-policy change | `internal/maxdelivery/observer.go:24-35`; scope deviation: none |
| Capture exact server MAX_DELIVERIES subject before components consume | `config/streams.go:184-196`, `cmd/semstreams/main.go:121-137`, `cmd/e2e-semstreams/main.go:128-139`; deviation: capture is one declaration in the central provisioning pass, not a second provisioner |
| File/Limits/DiscardOld/168h/64MiB/bounded/fixed replicas | `config/streams.go:184-196`, `config/streams.go:278-292`; deviation: none |
| One fixed durable across replicas; observer MaxDeliver unlimited | `internal/maxdelivery/observer.go:24-35`, `internal/maxdelivery/observer.go:251-260`; deviation: none |
| Metric + structured ERROR before ACK; report failure redelivers | `internal/maxdelivery/observer.go:127-178`, `internal/maxdelivery/observer.go:191-215`; settlement errors are separately counted/logged at `internal/maxdelivery/observer.go:139-163`; deviation: none |
| Wrong/malformed/required-field poison emits decoder telemetry then ACK | `internal/maxdelivery/observer.go:38-103`, `internal/maxdelivery/observer.go:181-200`; deviation: settlement failure is additionally made visible rather than discarded |
| Bounded labels domain/stream/consumer; ID/sequence only in log | `internal/maxdelivery/observer.go:127-132`, `internal/maxdelivery/observer.go:166-177`; deviation: none |
| No false availability gauge; readiness/data-plane occurrence behavior unchanged | `internal/maxdelivery/observer.go:125-144`, `internal/maxdelivery/observer.go:192-235`; deviation: no gauge or readiness mutation exists in this slice |
| ACL, retention/completeness, duplicates, and replica semantics documented | `design.md` adopter seam, retention, acknowledgement, ACL, and cluster sections; deviation: none |
| Central fixed ownership and pre-I/O config collision rejection | `config/stream_bounds.go:218-256`, `config/stream_bounds.go:298-303`, `config/streams_test.go:14-53`; deviation: none |
| Both binaries wired; real NATS and assembled E2E proof | `cmd/semstreams/main.go:121-137`, `cmd/semstreams/main.go:346-352`, `cmd/e2e-semstreams/main.go:128-139`, `cmd/e2e-semstreams/main.go:525-537`, `internal/maxdelivery/observer_integration_test.go:75-196`, `internal/maxdelivery/runtime_integration_test.go:33-150`, `test/e2e/scenarios/core_dataflow.go:116-120`, `test/e2e/scenarios/core_objectstore_raw.go:75-183`; deviation: none |
