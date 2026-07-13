## 1. Sharded write paths + key helpers (INCOMING, NAME, CONTEXT)

- [x] 1.1 `incoming_index.go`: key/prefix/parse helpers (raw target prefix; `IsValidEntityID` guards on target+source; reject empty predicate); empty marker; footgun comment (unconditional Put).
- [x] 1.2 `name_index.go`: shard to `hash(name).entity.hex(predicate)` +
  `{name,priority}` value; replace the CAS `UpdateWithRetry`.
- [x] 1.3 `context_index.go`: shard entity-first to
  `entity.hash(context).hex(predicate)` + `{contextValue,...}`; replace the
  non-CAS `Get`+`Put`, reconcile superseded memberships, and enable
  entity-prefix delete cleanup.
- [x] 1.4 Rewrite `updateIncomingIndexBatch` to per-edge Put (no CAS/list merge); keep the single-edge wrapper.

## 2. Reader migration + delete paths + wire-types

- [x] 2.1 INCOMING readers → prefix-scan + reconstruct: `query.go handleQueryIncomingNATS`; `client.go GetIncomingEdges` (bugfix — assert correct edges); clustering `anomaly.go:39/:100`; `component.go:1160 getNeighborsFromBucket` (direction-aware — incoming branch prefix-scans, outgoing stays Get). Use `natsclient.FilteredKeys` for raw `jetstream.KeyValue` holders.
- [x] 2.2 NAME readers → prefix-scan: `name_index.go:156 handleQueryByNameNATS` (reconstruct `{EntityID,Name,Predicate,Priority}` from key+value); keep `:108` `Keys()`-len ready check.
- [x] 2.3 CONTEXT: no production reader — migrate the write only; e2e readers handled in task 5.
- [x] 2.4 Delete path: `DeleteFromIndexes` prefix-scans INCOMING target rows
  (legacy hard-delete semantics) and entity-owned CONTEXT rows.
  Reciprocal/source-owned semantic retraction remains gh#527.
- [x] 2.5 Preserve wire types (`IncomingEntry` etc.) — reconstruct from keys; grep before deleting any type.

## 3. Instrumentation (L2/L3 data gate)

- [x] 3.1 Add the re-index no-op counter in `processEntityUpdateFromData`: compute the index-input projection (relationship pairs, full predicate set, name/context pairs — not raw values), compare to last-indexed, increment `unchanged`/`changed`. Observe only, never skip.

## 4. Tests + LAYER REVIEW CHECKPOINT

- [x] 4.1 Per-index key build/parse round-trip + prefix isolation (nested context not over-matched; sibling entity IDs isolated; malformed ID skipped; empty predicate rejected).
- [x] 4.2 Load test (mirror `predicate_index_load_test.go`; reuse `throughput/synthetic.go` 15-hot-key generator): hub dimension inserts in O(N) writes (assert count); read-back via one prefix scan within timeout. CONTEXT concurrent-writers no-lost-update.
- [x] 4.3 Cutover: old monolithic key inert; rebuild-from-ENTITY_STATES correct. Reader parity (correct edges/members — incl. the `GetIncomingEdges` bugfix). Entity-delete removes the whole `<id>.*` keyset (no phantom on re-add).
- [x] 4.4 NAME production-wire integration test (`graph.index.query.byName`) — closes the e2e gap (breaking-change rule).
- [x] 4.5 **Code review** (semstreams-reviewer) on the sharding + readers + deletes, before the e2e gate.

## 5. Breaking-change e2e gate (both tiers, hard-fail)

- [x] 5.1 Migrate raw e2e readers: incoming `GetIncomingEntries`; CONTEXT `validateContextIndexHierarchy` (read the reconstructed context value, not the literal key), `GetAllContexts`, `GetContextEntries`. Value-scan for CONTEXT (matches the authoritative stored context, no hash replication); prefix-scan reconstruct for INCOMING. Added `nats_shard_reconstruct_test.go` round-trip unit test.
- [x] 5.2 Tighten the incoming AND context e2e assertions to HARD-FAIL (warn-only today): non-structural tiers fail on empty index, container-present-but-reader-empty, and keys-present-but-values-unreadable (drift). Structural keeps the short-run note.
- [x] 5.3 `task e2e:structural` AND `task e2e:semantic` GREEN (`--build`, both exit 0, `validation_errors:0`; sharded readers reconstructed 429/420 CONTEXT keys → 1 distinct context; INCOMING/bidirectional/inverse all green; semantic ran the non-structural HARD-FAIL path on live data).

## 6. Final gates, review, PR

- [x] 6.1 gofmt, `task lint`, `go vet ./...` + `-tags=integration` + `-tags=live_llm`, `go test -race ./...`, `task schema:generate` no-drift — **`task check:push` GREEN (exit 0)**. Two latent gate failures found + fixed: (a) revive `unused-parameter` in `index_hardening_test.go:205` (`c`→`_`); (b) **integration-test migration gap** — `integration_test.go` still did bare-key `incomingBucket.Get`/`contextBucket.Get` (old monolithic format) in KVWatchToIndexFlow / MultipleRelationships / HierarchyEdgeIndexing / delete-verify; migrated to prefix-scan + reconstruct via `readIncomingEntries`/`readContextEntityIDs` helpers (reuse production `incomingEntryFromKey`). A graph-ingest `ConcurrentRaceOneWinner` fail was a testcontainers substrate flake (`port 4222 not found` at setup; passed on retry). Full `task check:push` re-running for the authoritative green.
- [x] 6.2 **Final semstreams-reviewer pass** on the full diff — **APPROVE**. One MEDIUM fixed: strict/soft tier gate keyed off raw `s.config.Variant`, which stays `""` on a flagless `./e2e` run (Execute keeps the auto-detected variant local) → false-fails a legitimately-empty structural index; added `effectiveVariant(result)` fallback to the stamped `result.Metrics["variant"]`. Two minors fixed: `CountIncomingEdges` doc (distinct-source count, not edges); container-incoming async-ordering note.
- [ ] 6.3 `openspec validate graph-index-hardening --strict`; branch + PR + CI (both e2e tiers in the PR body); on merge close gh#474 and note the class-fix + ADR-065 correction.
- [ ] 6.4 File the L2 (change-detection) + L3 (isolation) follow-up issues, each gated on this change's no-op counter / post-merge starvation re-measurement, carrying the corrected designs (B1 full-projection signature, B2 delete-invalidation; L3 rate-limit-not-dedicated-conn + the revert reference).

## 7. Codex P1 review blockers (PR #524, post-freeze correctness)

- [x] 7.1 P1a — hex-encode the predicate key token (INCOMING/NAME/CONTEXT) via shared `graph.EncodePredicateToken`; carry hashed name/context in the value; KV-unsafe-predicate round-trip test.
- [x] 7.2 P1f — re-key CONTEXT to entity-prefix `entityID.hash(context).hex(pred)`; self-reconcile on update (prefix-scan + retract superseded) and self-clean on delete.
- [x] 7.3 P1e — relabel the target-prefix incoming delete as LEGACY HARD-DELETE (source-owned evidence; not for logical retirement); design.md D3 + ADR-065 corrected.
- [x] 7.4 P1b — writes aggregate + return failures; bounded idempotent retry; on ultimate failure mark entity failed + withhold `Ready` via `failedCount`; baseline stored only on success; failure-injection tests.
- [x] 7.5 P1c — sort incoming results by `(FromEntityID, Predicate)` in `handleQueryIncomingNATS` + `GetIncomingEdges`.
- [x] 7.6 P1d — gate incoming/byName on caught-up watermark (sticky), returning `ErrorCodeIndexNotReady` during cutover/cold-replay.
- [x] 7.7 P2b — expose `reindex_events_total{result}` + `write_failures_total` Prometheus metrics; add the ALIAS axis to `computeIndexProjection`.
- [x] 7.8 Re-verified: `task check:push` green (one confirmed graph-ingest contention flake, passes isolated); e2e:structural + e2e:semantic GREEN with the re-changed format (exit 0, validation_errors:0); pushed to PR #524; reply posted to Codex.
- [x] 7.9 P2a byName bounded-read: cap serial hydration and return typed
  `resource_exhausted`; upgrade-debris purge + source-owned retraction remain
  gh#527.

## 8. Codex 3rd-pass review blockers (PR #524, airtight readiness under concurrency)

- [x] 8.1 #1 — split `initialEnumerationComplete` (sentinel; empty-graph exception only) from `indexBootstrapped` (set only when the watermark is caught up), so a non-empty cold replay stays not-ready until workers finish. Test: preloaded non-empty bucket, incoming-fails-while-not-ready invariant.
- [x] 8.2 #5 — PathRAG propagates every availability/protocol/decode failure
  and rejects structurally incomplete success envelopes; only an explicit empty
  relationships array is empty; direction=both fails if either leg fails.
- [x] 8.3 #6 — gate ALIAS + all PREDICATE query handlers on `ensureQueryReady`; propagate predicate-catalog write failures into the entity failure gate.
- [x] 8.4 #3 — coalescer Get-error: mark-failed on a TRANSIENT read error (readiness withheld + repair retries); only a genuine not-found drains the watermark.
- [x] 8.5 #4 — direct readers (query client, clustering) FAIL-CLOSED by default; explicit `allow_ungated_reads` config for standalone/test; fixed the "routes through the handler" doc claim.
- [x] 8.6 #2 — repair routes through the same entity-keyed FIFO dispatcher as
  watcher updates/deletes and reconciles authoritative state at execution;
  ordering correctness is delivered here, not deferred to gh#527.
- [x] 8.7 #7 — spec/design match the delivered key formats, CONTEXT
  reconciliation/delete path, exact watermark completion, bounded repair, and
  retention-only gh#527 scope.
- [ ] 8.8 Re-verify: check:push + e2e:structural + e2e:semantic green; push; reply to Codex (3rd).

## 9. Codex 4th-pass correctness close-out

- [x] 9.1 Replace the generic worker pool with hash-keyed FIFO entity lanes
  shared by updates, deletes, coalesced work, and repair.
- [x] 9.2 Reconcile authoritative `ENTITY_STATES` at execution so stale queued
  work cannot clobber a newer write or resurrect a delete.
- [x] 9.3 Make coalescing revision-aware and complete the watermark at the exact
  detached revision; initialize it before the watcher.
- [x] 9.4 Use a dedicated ENTITY_STATES status handle for concurrent `LastSeq` reads.
- [x] 9.5 Propagate direct-client incoming readiness failures and reject
  structurally invalid PathRAG responses.
- [x] 9.6 Update proposal/design/spec/tasks and ADR current-status notes; strict OpenSpec validation.
- [x] 9.7 Final fourth-pass `task check:push` GREEN on the complete rerun,
  including lint, schema no-drift, contract, race unit, and race integration
  gates. The first run hit one transient Ollama timeout; that test passed in
  isolation, then the full `task check:push` rerun passed. OpenSpec strict
  validation also passed.
- [x] 9.8 `task e2e:structural` PASS: 37/37 validations,
  `validation_errors=0`.
- [ ] 9.9 `task e2e:semantic` could not reach SemStreams: SemEmbed failed while
  downloading `onnx/model.onnx` with HTTP 403. This is an environment/dependency
  failure before the SemStreams semantic tier, not a SemStreams test result;
  rerun remains required.

## 10. Codex 5th-pass OUTGOING replacement correction

- [x] 10.1 Reconcile every present authoritative entity by replacing its complete
  `OUTGOING[entityID]` value, including explicit `[]`; reserve owner-key deletion
  for authoritative `ENTITY_STATES` absence.
- [x] 10.2 Add regression coverage for relationship transition `[A]` to `[]` so
  the stored projection and outgoing query result contain no phantom edge.
- [x] 10.3 Update design/spec/tasks with the bounded live-entity-cardinality
  tradeoff and the authoritative replacement contract.
- [x] 10.4 Focused unit and real-NATS graph-index tests pass under `-race`;
  `task check:push` and strict OpenSpec validation pass; `task e2e:structural`
  passes all 37 validations with `validation_errors=0`.
- [ ] 10.5 Push the correction to PR #524 and verify required GitHub checks.
