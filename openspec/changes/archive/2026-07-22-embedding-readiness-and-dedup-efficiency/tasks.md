## 1. Shared readiness projection (D1)

- [x] 1.1 Add `FailedCount uint64` to `IndexStatusInputs` (`graph/index_status.go`)
- [x] 1.2 Project `FailedCount > 0 → degraded` BEFORE the "ready wins" branch in `ComputeIndexStatus`; leave `Ready` coverage-accurate and `Stuck` handling intact
- [x] 1.3 Unit test: `FailedCount>0` with `Indexed>=Target` → `State=degraded` while `Ready=true`; `FailedCount=0` path byte-identical to today (graph-index parity)
- [x] 1.4 Confirm graph-index's `ComputeIndexStatus` call compiles unchanged with the widened struct (new field defaults `0`, no behavior change) — grep the call sites

## 2. Terminal outcome plumbing (D3)

- [x] 2.1 Define `TerminalOutcome` (Generated | Failed | Skipped | Deleted); widen the worker terminal callback to carry `(entityID, sourceRevision, outcome, reason)` (`worker.go:155,410`)
- [x] 2.2 `completeEmbedding` advances the watermark for ALL outcomes (unchanged — deadlock avoidance) and routes the failed-map update by outcome
- [x] 2.3 Regression guard: a `failed` / no-text terminal still advances the watermark (a permanently-failing or telemetry-only entity does not stall readiness)

## 3. Current-failed tracking + bootstrap seed (D2)

- [x] 3.1 Add the mutex-guarded `failed map[entityID]{reason, at}` to the component; `FailedCount = len(failed)`
- [x] 3.2 Bootstrap scan of `EMBEDDING_INDEX` for `Status==failed` to seed the map at Start (precedent `storage.go:665`) — implemented as `Storage.ScanFailed` via the WatchAll snapshot (streamed, not Get-per-key) so it stays O(1)-round-trip for large adopters
- [x] 3.3 Pass `FailedCount: len(failed)` into `IndexStatusInputs` in `computeEmbeddingStatus`
- [x] 3.4 Integration test: dependency-down cold start → `degraded` with `FailedCount>0`; on recovery `FailedCount` drops to 0 and `State` returns to `ready` (`TestIntegration_EmbeddingReadiness_DependencyDownDegradesThenRecovers`, real NATS + controllable HTTP embedder)

## 4. Reason classification (D5)

- [x] 4.1 Add `Reason string` (`omitempty`) to `Record` (`storage.go:70`); `SaveFailed` takes + stores the reason
- [x] 4.2 Add `classifyEmbedErr(err) string` and classify at each `markFailed` site (`worker.go:459,539,556,562,672`) per the design table; default `embedder_error`
- [x] 4.3 Unit test: each `markFailed` path yields the expected bounded reason; an unrecognized embedder error → `embedder_error`

## 5. Failed-record reprocessing on re-delivery (D4)

- [x] 5.1 Widen `worker.go:441` to process `StatusPending` OR `StatusFailed` — GATED to the initial WatchAll snapshot only (restart re-delivery); live `StatusFailed` self-writes are skipped to prevent a hot re-embed loop (see NOTE below — design D4 did not cover the self-write re-delivery loop)
- [x] 5.2 Verify the `SaveGenerated` revision-CAS (`storage.go:378`) tolerates the equal-revision retry; confirm no self-loop — snapshot-gating makes reprocessing re-delivery driven only (a live failed re-write lands after the snapshot sentinel → skipped)
- [x] 5.3 Integration test: a re-delivered failed record (new revision) re-embeds to `generated`, clears from the failed-map (covered by 3.4's recovery arm). NOTE: the restart-snapshot re-embed arm is implemented + unit-covered by the `ScanFailed` seed test, but not separately integration-tested (restart recovery is also carried by hop-1 re-SavePending, which every existing restart test exercises)

## 6. Observability — envelope (L2) + metrics (L1) (D6, D7)

- [x] 6.1 Add `failed_count`, `failed_reasons` (bounded map), `first_failure_at` (all `omitempty`) to `IndexStatusResponse` (`graph/index_status.go`)
- [x] 6.2 Populate them in `computeEmbeddingStatus` from the failed-map (reason histogram, min timestamp); graph-index leaves them zero → omitted
- [x] 6.3 Add the `failed` gauge + `{reason}`-labeled failures counter (`metrics.go`, per-registry register-or-get, no process-global); raw `ErrorMsg` never a label
- [x] 6.4 JSON round-trip test: envelope carries the fields when present, omits them when zero; metrics reflect the map on transition

## 7. L3 failure-detail surfaces (D8)

- [x] 7.1 Confirm fusion / graph-query relay the GRAPH_STATUS envelope failure detail through their existing status surfaces (they already hold the watch — no new production endpoint); test the relayed aggregate — fields added to `fusion.IndexStatus` + `readinessEnvelope()`; the gate-projection round-trip enforces lockstep; graph-query decodes `graph.IndexStatusResponse` directly (transparent)
- [x] 7.2 Add an opt-in `Status==failed` filter to the message-logger `EMBEDDING_INDEX` read (`service/message_logger_http.go`); message-logger stays OFF by default
- [x] 7.3 JSON-round-trip test for the debug enumerate response shape (operator-reachable fields)

## 8. Concurrent dedup — process-local singleflight (D10, #630)

- [x] 8.1 Wrap the embedder `Generate` (`worker.go:~556`) in a process-local `singleflight.Group` keyed by the dedup key; each worker still performs its own `SaveGenerated` (empty key falls through to a direct call — cannot collapse distinct texts)
- [x] 8.2 Concurrency test: K workers + byte-identical content + a counting fake embedder → exactly one `Generate`, all K stored

## 9. Cross-lane dedup regression (D9, #627 close)

- [x] 9.1 Test: byte-identical over-cap content via the inline lane vs a `StorageRef` lane derives an identical dedup key / identical embedded bytes (locks the inc-1 `truncateAtWord` unification) (`TestOverCapContentDedupsAcrossLanes`) — no production change (#627 verify-only)

## 10. Wire, docs, gates (BREAKING)

- [x] 10.1 Grep in-repo consumers to confirm none gates on `Ready` past the canonical `State` gate (the `Ready:true, State:degraded` envelope is intentional) — only non-test `.Ready` reads are metric-reporting (coverage gauges) and graph-index's own producer latch; `EvaluateReadinessGate` reads `State`
- [x] 10.2 Adopter note in `docs/operations/embedding-readiness-degraded-change.md` documenting the BREAKING readiness change (embedding reports `degraded` under failures; how to read `failed_count`/`failed_reasons`; recovery; rollback)
- [x] 10.3 `task schema:generate` + `git diff schemas/ specs/` clean (envelope + `Record.Reason` additions) — regenerated; the only diff is the message-logger `status` query param, committed to the working tree
- [x] 10.4 `go test -race ./...`, `go vet` (plain + `-tags=integration` + `-tags=live_llm`), `task lint`, contract tests, and tagged integration on touched packages — all green
- [x] 10.5 BREAKING e2e gate satisfied: `task e2e:semantic` GREEN (`validation_errors:0`, 7/7 known-answer, `embedding_failed_total:0`, `data_loss:0`, `semembed_health:200`) — proves the change does not break the happy path. The semembed-down → `degraded` + failure-detail behavior is fully covered by the integration test `TestIntegration_EmbeddingReadiness_DependencyDownDegradesThenRecovers` (real NATS + `graph.embedding.query.status` handler + a real HTTP embedder returning 503; asserts degraded/FailedCount/reason-histogram/first_failure_at + recovery). Owner-decided (2026-07-22): a bespoke no-fallback degraded e2e config variant is NOT needed — integration coverage is genuine and non-tautological; no follow-up filed.
- [x] 10.6 `openspec validate --strict` passes. (#627 GitHub issue closure left to the owner's review/merge.)
