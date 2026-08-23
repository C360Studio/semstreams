# GH-1054 lifecycle drain flake inventory

Baseline: `774c85dcf75bdce242f1f15ee2a5a310991ecf0d`; clean `main` tracking `origin/main`.

## Problem statement

Required CI reports 30-second `Client.Close` drain timeouts in graph-clustering and graph-embedding. This inventory
asks whether transport drain is the first failure or a downstream observation of another failure.

## Surface inventory

### Client and native NATS drain ownership

- Client defaults `drainTimeout` to 30 seconds at `natsclient/client.go:151-171`.
- `WithDrainTimeout` is exported at `natsclient/options.go:191-197`.
- `Client.Close` serializes terminal admission, marks closed, stops transport-owned workers, snapshots the connection
  and timeout, drains, clears connection/credentials, and returns the drain result at `natsclient/client.go:577-620`.
- `drainAndCloseConnection` registers `CLOSED` before `Conn.Drain` and selects `CLOSED`, timeout, or caller
  cancellation at `natsclient/client.go:672-731`; timeout force-closes and returns a transient error at `:713-722`.
- nats.go v1.52.0 `Conn.Drain` starts `drainConnection` asynchronously at `nats.go:6175-6202`.
- Native drain waits for subscriptions, performs a five-second publish Flush, then closes at `nats.go:6155-6173`.
- Native status observation is owned by `StatusChanged`/`RemoveStatusListener` at `nats.go:6504-6538`.
- Native close publishes `CLOSED` at `nats.go:5934-6037`.
- PR #1019 / commit `cc4dfac9` changed Client from starting Drain then immediately closing to awaiting native CLOSED.
- Current truth requires that behavior at `openspec/specs/jetstream-consumer-policy/spec.md:287-308`.

### Test-client and direct-client cleanup spellings

- `TestClient` cleanup gives `Client.Close` and container `Terminate` independent 15-second contexts at
  `natsclient/test_client.go:28-38,295-323`.
- `TestClient.Terminate` memoizes one cleanup result at `natsclient/test_client.go:929-939`.
- `NewTestClient` registers `Terminate` through `t.Cleanup` at `natsclient/test_client.go:852-869`.
- `NewSharedTestClient` creates from the test composition root at `natsclient/test_client.go:846-850`.
- The observed 30-second value therefore identifies direct `Client.Close(context.Background())`, not TestClient's
  15-second bound.
- graph-embedding `newLifecycleNATSClient` starts an in-process server, registers server shutdown, connects Client,
  then registers `Client.Close(context.Background())` at
  `processor/graph-embedding/lifecycle_owner_test.go:56-67`.
- Equivalent direct setup exists at `processor/graph-clustering/lifecycle_owner_test.go:50-61` and
  `processor/graph-query/lifecycle_owner_test.go:49-60`; temporal/spatial belong to the same copied owner-test family.

### First failure and secondary timeout

- graph-embedding installs a callback blocked on `release`, starts Stop, launches Start, then uses a nonblocking
  select that fatals if Start has already returned; `release` closes only afterward at
  `processor/graph-embedding/lifecycle_owner_test.go:183-223`.
- The focused repeated race run's first failure is `processor/graph-embedding/lifecycle_owner_test.go:218`:
  `Start returned before Stop serialized`.
- Fatal exit skips `close(release)` at `:221`; LIFO cleanup invokes Client.Close while the callback remains blocked.
- Client.Close then correctly awaits drain; the blocked callback prevents `CLOSED` until the 30-second bound.
- Production graph-embedding Start necessarily returns: the fixture sets `lifecycleUsed=true` at test `:198`, and
  Start rejects `lifecycleUsed` at `processor/graph-embedding/component.go:623-650`.
- graph-clustering has the same contradiction: fixture `:196`, fatal/select `:212-219`, rejection
  `processor/graph-clustering/component.go:928-955`.
- graph-query has the same contradiction: fixture `:162`, fatal/select `:178-185`, rejection
  `processor/graph-query/component.go:463-490`.
- Same-class census found two additional copies:
  - graph-index-temporal `lifecycle_owner_test.go:152-186`; production rejection `component.go:464-491`.
  - graph-index-spatial `lifecycle_owner_test.go:159-194`; production rejection `component.go:453-480`.
- Exact search `rg -n "Start returned before.*Stop" processor --glob '*_test.go'` returned exactly five owners:
  graph-embedding:218, graph-index-temporal:181, graph-index-spatial:189, graph-query:182, graph-clustering:216.
- All five assert unsupported Start/Stop serialization before normal-path `close(release)`.
- None of those test cases registers terminal cleanup for `release`.

### Current lifecycle authority

- Current authority says concurrent lifecycle calls are not portable guarantees at `component/lifecycle.go:43-52`.
- Accepted #1022 truth says shared tests must not rely on concurrent lifecycle behavior at
  `openspec/changes/align-standard-lifecycle-tests/specs/component-lifecycle/spec.md:25-31`.
- ADR-095 likewise says concurrent Stop/Close and retained result replay are not contracts at
  `docs/adr/095-one-shot-running-lifecycle-with-retained-failed-start-cleanup-authority.md:24-41`.
- The valid fact within the tests is distinct: Stop drains the exact subscription while callback authority and child
  remain live, then cancels, joins, and closes the child. The component-lifecycle delta at `:3-23` and ADR-095 at
  `:26-30` own it.

### Shared helpers and CI

- graph-clustering `TestMain` creates/terminates a shared container at `lifecycle_integration_test.go:18-45`.
- graph-embedding has the same package setup at `lifecycle_integration_test.go:18-39`.
- Their `getSharedNATSClient`/`createTestComponentForLifecycle` definitions have no consumers:
  graph-clustering `lifecycle_integration_test.go:48-77`; graph-embedding `:42-71`.
- The same stale helper shape exists in graph-index-spatial and graph-index-temporal
  `lifecycle_integration_test.go:18-71`.
- Active siblings differ: graph-index calls `StandardLifecycleTests` at `lifecycle_integration_test.go:78`;
  graph-ingest consumes its factory at `:93`; agentic-dispatch at `:83`; agentic-governance at `:75`.
- `rg -l "NewSharedTestClient\(" --glob '*_test.go'` returned 26 files.
- The helper is broad, but the reproduced 30-second path is the direct lifecycle-owner Client, not shared TestMain
  cleanup.
- CI ownership is `.github/workflows/ci.yml:91-106`, which runs `scripts/run-integration-tests.sh`.
- The script runs `go test -race -failfast -tags=integration -timeout=20m -count=1` with no `-p` cap at `:304-311`.
- Historical commit `f2b0581` / PR #518 introduced `-p 2`; commit `804cdbd2` removed that cap via the current script.
- #736 remains open for Docker/package-parallelism oversubscription. It can change scheduling frequency but does not
  explain the reproduced first failure.
- #750 is a distinct, closed, under-margined wall-clock assertion class.
- #1019 deliberately made leaked callbacks visible by awaiting `CLOSED`; reverting it conflicts with current truth.
- #1022 / merged PR #1048 removed portable concurrent lifecycle promises. Its conformance artifact still says #1048
  had not merged, stale relative to current main.

### Release seam

- Any nonzero retained deterministic path blocks tag authorization at
  `openspec/specs/release-candidate-proof/spec.md:7-35`.
- Exact-candidate proof binds full race/integration results at `:114-120`; red or missing gates reject tag
  authorization at `:215-240`.

## Adopter seam inventory

Concrete adopter: an external component/process author calling `Client.Close`.

- They must stop owner-held children and allow admitted callbacks to finish before terminal transport Close; Client
  does not own children (`openspec/specs/jetstream-consumer-policy/spec.md:287-308`).
- If they leave a callback blocked, Close observes reality, waits to its bound, force-closes, and returns an error.
- Discovery today is a typed runtime error plus ERROR log; `Client.Close` GoDoc at `client.go:577` is only one
  sentence.
- semmachina uses `NewSharedTestClient`/`Terminate` at
  `../semmachina/cmd/bellweather-surface-stack/main.go:83-91` and
  `../semmachina/internal/testinfra/harness.go:185-240`.
- semdev, semconnect, semteams, semspec, semdragon, and semmachina also call exported `Client.Close`.
- Exact sibling search for `WithDrainTimeout` across those repositories returned empty.
- External adopters should need to know nothing new for #1054: the reproduced defect is confined to owner tests.
- No exported signature, option, timeout, subject, bucket, schema, or lifecycle promise is absent.
- Consumer-at-birth: no new outward symbol is present or proposed in this inventory.

## Same-class collision table

| Dimension | Existing evidence |
|---|---|
| Semantic class | Terminal lifecycle proof and native transport-drain observation |
| Owners | Component Stop owns exact child ordering; Client.Close owns transport drain; TestClient owns client/container cleanup; Go testing owns fatal/cleanup sequencing; CI owns package scheduling |
| Catalog/status | Component private lifecycle flags and subscription slices; Client.closed; native Conn CLOSED; TestClient.cleanupOnce/cleanupErr |
| Lifecycle | Component child drain precedes cancellation/join; Client awaits CLOSED; TestClient closes then terminates; t.Fatal exits the test goroutine and runs registered cleanup |
| Readers/writers | Components write flags under lifecycleMu; tests construct private state; Client/nats.go own transport status; CI reads package exit and logs |
| Recovery | Client timeout force-closes and returns error; TestClient still attempts termination; the failed owner test has no recovery for unclosed release |

Changing `drainTimeout`, Client.Close, TestClient, or CI scheduling acts on secondary surfaces while five tests retain
the contradictory assertion and can strand callbacks.

## Context audit

- Client retains only private `metricsCancel context.CancelFunc` at `client.go:121`; no `context.Context` is retained
  on touched production structs.
- Test terminal roots are at `test_client.go:310,317,849`.

## Searches closing empty categories

- No payload, config key, subject, bucket, stream, query, schema, or production consumer is implicated.
- Searches covered drain/closed/NewSharedTestClient/WithDrainTimeout/lifecycleUsed/serialization across natsclient,
  component, processor, CI, OpenSpec, ADRs, and named sister repositories.

## Open evidence question

Hosted logs should preserve the first assertion above the later drain timeout. The focused parent reproduction supplied
that exact graph-embedding sequence, while the five-copy census establishes latent sibling exposure.

This is an inventory checkpoint only; it contains no target state, option, recommendation, or artifact delta.
