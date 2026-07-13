## 1. Sharded write paths + key helpers (INCOMING, NAME, CONTEXT)

- [x] 1.1 `incoming_index.go`: key/prefix/parse helpers (raw target prefix; `IsValidEntityID` guards on target+source; reject empty predicate); empty marker; footgun comment (unconditional Put).
- [x] 1.2 `name_index.go`: shard to `hash(name).entity.predicate` + `{name,priority}` value; replace the CAS `UpdateWithRetry`.
- [x] 1.3 `context_index.go`: shard to `hash(context).entity.predicate` + `{contextValue,...}` value; replace the non-CAS `Get`+`Put` (fixes the lost-update race AND the raw-key collision).
- [x] 1.4 Rewrite `updateIncomingIndexBatch` to per-edge Put (no CAS/list merge); keep the single-edge wrapper.

## 2. Reader migration + delete paths + wire-types

- [x] 2.1 INCOMING readers → prefix-scan + reconstruct: `query.go handleQueryIncomingNATS`; `client.go GetIncomingEdges` (bugfix — assert correct edges); clustering `anomaly.go:39/:100`; `component.go:1160 getNeighborsFromBucket` (direction-aware — incoming branch prefix-scans, outgoing stays Get). Use `natsclient.FilteredKeys` for raw `jetstream.KeyValue` holders.
- [x] 2.2 NAME readers → prefix-scan: `name_index.go:156 handleQueryByNameNATS` (reconstruct `{EntityID,Name,Predicate,Priority}` from key+value); keep `:108` `Keys()`-len ready check.
- [x] 2.3 CONTEXT: no production reader — migrate the write only; e2e readers handled in task 5.
- [x] 2.4 Delete path: `DeleteFromIndexes` incoming branch → `KeysByPrefix(entityID+".")`+delete-each (entity is the prefix). Confirm NAME/CONTEXT have no delete path today (no regression; reciprocal cleanup stays gh#433).
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
