## 0. Review Gate

- [x] 0.1 Adversarially review the generalized watcher implementation from `f3adabb8` against this change, with
      separate concurrency, fail-closed state, resource-bounds, lifecycle, and operator-semantics lenses. Pattern
      validation is reviewed under `entity-id-contract`, not here

## 1. Extract the Existing Implementation

- [x] 1.1 Carry only the watcher-hardening slice into this change: `pkg/cache/coalescing_set*`,
      `processor/rule/entity_evaluation_fence*`, watcher generation/provenance/guard/queue/lifecycle portions of
      `entity_watcher*`, `message_handler.go`, `processor.go`, and `processor/rule/docs/entity-watching.md`, plus their
      focused tests. Keep entity-pattern grammar, schema, and config migration content in `entity-id-contract`
- [x] 1.2 Verify the authoritative WatchAll guard blocks pattern bootstrap and every live revision until validation has
      progressed through that revision; typed poison latches reset-required before evaluation, metrics, transitions,
      or actions, while unexpected transport loss reports degraded and expected closure does neither
- [x] 1.3 Verify dynamic watcher replacement prepares all additions before commit, rolls back prepared transports on
      failure, publishes and retires exact generations under the dispatch gate, and removes authority before Stop so
      Stop failure cannot revive a retired watcher
- [x] 1.4 Verify managed debounce work carries exact watcher generation provenance, stale generations fail before
      current-state fetch, overlapping active watchers fetch/evaluate once per entity, and bootstrap keeps its
      non-coalesced OnRecovery semantics
- [x] 1.5 Verify the per-entity fence serializes fetch, evaluation, delete transition, and cleanup; suppresses same/lower
      revisions and duplicate deletes; removes all queued entity work only after a delete admits; preserves newer
      queued work when a stale overlapping delete is rejected; and uses the documented lock order
- [x] 1.6 Verify active fence entries are never evicted, idle entries obey the 15-minute TTL and 65,536-entry LRU cap,
      and shutdown retires generations, drains the coalescer, releases queued references, clears idle state, and
      reports any remaining active reference

## 2. Proof and Documentation

- [x] 2.1 Run focused `pkg/cache` and `processor/rule` tests with `-race`, including deterministic atomic-bootstrap,
      watcher-replacement, stale-generation, overlapping-watch, delete-ordering, revision, bound, and shutdown cases
- [x] 2.2 Add or retain a real-NATS integration proof covering bootstrap, overlapping patterns, live coalescing,
      deletion during a settling window, dynamic pattern replacement, and clean shutdown without leaked watcher work
- [x] 2.3 Update entity-watching documentation to distinguish authoritative guard, pattern watcher, generation
      authority, coalescing window, evaluation fence, and bounded idle dedupe horizon; do not describe the horizon as
      retention or an operator setting
- [x] 2.4 Run `task lint`, `go test -race ./...`, contract tests, schema drift check, and structural e2e before landing;
      add agentic e2e if rule-pack configuration or agentic rule behavior remains in the extracted diff
- [ ] 2.5 Strict-validate and review this change, then archive only after every task and recorded gate is complete

## Gate Evidence (2026-07-16)

- `go test -race ./pkg/cache ./processor/rule`
- `TestStaleDeleteDoesNotPurgeNewerPendingWatcherWork` pins the revision-fence-before-queue-mutation ordering for
  overlapping watcher recreation.
- `go test -race -tags=integration -run '^TestEntityWatcherHardeningRealNATS$' ./processor/rule`
- `go test -race -tags=integration -p=1 ./pkg/cache ./processor/rule`: watcher and cache suites passed; one unrelated
  `TestIntegration_DenyFlow` NATS-container startup timed out before exposing port 8222, then its exact isolated rerun
  passed
- `task lint`
- `go test -race ./...`
- `go test ./test/contract/...`
- `task entity-id:audit`: 1,132 structured candidates
- `task predicate:audit`: 467 structured candidates
- `task predicate:test-audit`: 1,811 candidates and 123 exact classifications
- `task schema:generate`: no schema or OpenAPI drift
- `task e2e:structural`: 37/37 steps, 617 rule evaluations, 6 rule firings, zero validation errors
- `openspec validate rule-entity-watcher-hardening --strict`
