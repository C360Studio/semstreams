## 1. Landed implementation

- [x] Migrate the inventoried production owners from `internal/lifecyclejoin`.
- [x] Delete `internal/lifecyclejoin` and obsolete retained-result/rejoin tests.
- [x] Return exact native handles from canonical port-backed and internal consume operations.
- [x] Add stateless `NewDurableHandler` and remove `ConsumeDurable`.
- [x] Remove Client child catalogs, replacement/name lifecycle APIs, `StopAllConsumers`, `OutstandingWork`, and
  Close-time child enumeration.
- [x] Preserve independent duplicate claims, consumer policy, Prometheus metrics, OTEL compatibility, graph readiness,
  and agent-loop inflight observation.
- [x] Remove the five deletion fields and published-schema properties without adding a replacement mechanism.
- [x] Preserve current `Subscription.Drain` behavior.
- [x] Add focused API-census, exact-handle, claim-release, durable-handler, schema, service-owner, and race tests.
- [x] Record reviewed implementation evidence for commits `8da1b83a`, `c4fec3d3`, and `2e879304`.

## 2. Archive closeout

- [x] Remove deferred, unrelated, and unimplemented target requirements from proposal, tasks, and deltas.
- [x] Reconcile migration guidance to landed current truth.
- [x] Run the final issue #1011 race, contract, schema, OpenSpec, and relevant E2E verification.
- [x] Record the OpenSpec closeout retrospective and prevention rules required by #1011.
- [x] Obtain independent review and archive the change.
