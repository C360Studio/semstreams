## 1. Sharded write paths + key helpers (INCOMING, NAME, CONTEXT)

- [x] 1.1 `incoming_index.go`: key/prefix/parse helpers; raw target prefix; `IsValidEntityID` guards on target and
  source; empty-predicate rejection; empty marker; unconditional-Put footgun comment.
- [x] 1.2 `name_index.go`: shard to `hash(name).entity.hex(predicate)` +
  `{name,priority}` value; replace the CAS `UpdateWithRetry`.
- [x] 1.3 `context_index.go`: shard entity-first to
  `entity.hash(context).hex(predicate)` + `{contextValue,...}`; replace the
  non-CAS `Get`+`Put`, reconcile superseded memberships, and enable
  entity-prefix delete cleanup.
- [x] 1.4 Rewrite `updateIncomingIndexBatch` to per-edge Put (no CAS/list merge); keep the single-edge wrapper.

## 2. Reader migration + delete paths + wire-types

- [x] 2.1 INCOMING readers use prefix-scan and reconstruction: `query.go handleQueryIncomingNATS`;
  `client.go GetIncomingEdges`; clustering `anomaly.go:39/:100`; and direction-aware
  `component.go:1160 getNeighborsFromBucket`. Raw `jetstream.KeyValue` holders use
  `natsclient.FilteredKeys`; OUTGOING remains a direct Get.
- [x] 2.2 NAME readers use prefix-scan in `name_index.go:156 handleQueryByNameNATS`, reconstructing
  `{EntityID,Name,Predicate,Priority}` from key and value; keep the `:108` `Keys()` length readiness check.
- [x] 2.3 CONTEXT: no production reader — migrate the write only; e2e readers handled in task 5.
- [x] 2.4 Delete path: `DeleteFromIndexes` prefix-scans INCOMING target rows
  (legacy hard-delete semantics) and entity-owned CONTEXT rows.
  Reciprocal/source-owned semantic retraction remains gh#527.
- [x] 2.5 Preserve wire types (`IncomingEntry` etc.) — reconstruct from keys; grep before deleting any type.

## 3. Instrumentation (L2/L3 data gate)

- [x] 3.1 Add the re-index no-op counter in `processEntityUpdateFromData`: compute the index-input projection
  (relationship pairs, full predicate set, and name/context pairs, not raw values), compare it with the last-indexed
  projection, and increment `unchanged` or `changed`. Observe only; never skip.

## 4. Tests + LAYER REVIEW CHECKPOINT

- [x] 4.1 Per-index key build/parse round-trip and prefix isolation: nested context is not over-matched, sibling entity
  IDs remain isolated, malformed IDs are skipped, and empty predicates are rejected.
- [x] 4.2 Load test mirrors `predicate_index_load_test.go` and reuses the `throughput/synthetic.go` 15-hot-key
  generator: hub insertion is O(N) writes, read-back is one bounded prefix scan, and concurrent CONTEXT writers lose
  no updates.
- [x] 4.3 Cutover: old monolithic keys are inert and rebuild from ENTITY_STATES is correct. Reader parity includes the
  `GetIncomingEdges` fix. Entity delete removes the complete `<id>.*` keyset with no phantom on re-add.
- [x] 4.4 NAME production-wire integration test (`graph.index.query.byName`) closes the breaking-change e2e gap.
- [x] 4.5 **Code review** (semstreams-reviewer) on the sharding + readers + deletes, before the e2e gate.

## 5. Breaking-change e2e gate (both tiers, hard-fail)

- [x] 5.1 Migrate raw e2e readers: incoming `GetIncomingEntries`; CONTEXT `validateContextIndexHierarchy`,
  `GetAllContexts`, and `GetContextEntries`. CONTEXT scans values for authoritative context; INCOMING prefix-scans and
  reconstructs. Add `nats_shard_reconstruct_test.go` round-trip coverage.
- [x] 5.2 Make incoming and context e2e assertions hard-fail. Non-structural tiers fail on an empty index, a present
  container with an empty reader, or present keys with unreadable values. Structural retains its short-run note.
- [x] 5.3 `task e2e:structural` and `task e2e:semantic` green with `--build`, exit 0, and
  `validation_errors:0`; sharded readers reconstructed 429/420 CONTEXT keys into one context, and semantic exercised
  the non-structural hard-fail path on live data.

## 6. Final gates, review, PR

- [x] 6.1 `task check:push` green: formatting, lint, vet variants, race tests, and schema generation passed. Fixed a
  revive unused parameter and an integration migration gap where old bare-key INCOMING/CONTEXT reads remained. A
  testcontainers `port 4222 not found` setup flake passed on retry before the authoritative full green run.
- [x] 6.2 Final semstreams-reviewer pass approved. Fixed the strict/soft tier gate by falling back from an empty
  `s.config.Variant` to `result.Metrics["variant"]`; corrected the distinct-source count documentation and incoming
  asynchronous-ordering note.
- [x] 6.3 Strict validation passed; PR #524 merged as `af3cc844` with required checks green, closed gh#474, and
  recorded the class fix plus ADR-065 correction.
- [x] 6.4 Filed L2 change-detection follow-up gh#525 and L3 isolation follow-up gh#526 with the corrected gates and
  designs.

## 7. Codex P1 review blockers (PR #524, post-freeze correctness)

- [x] 7.1 P1a — hex-encode the predicate key token (INCOMING/NAME/CONTEXT) via shared
  `graph.EncodePredicateToken`; carry hashed name/context in the value; prove codec-only round-trip without treating
  encoding as graph-write acceptance.
- [x] 7.2 P1f — re-key CONTEXT to `entityID.hash(context).hex(pred)`; reconcile superseded rows on update and clean
  the entity prefix on delete.
- [x] 7.3 P1e — label target-prefix INCOMING deletion as legacy hard-delete, not logical retirement; correct D3 and
  ADR-065.
- [x] 7.4 P1b — aggregate write failures, retry idempotently within bounds, mark ultimate failure, withhold readiness,
  and store the no-op baseline only on success; cover with failure injection.
- [x] 7.5 P1c — sort incoming results by `(FromEntityID, Predicate)` in the handler and client.
- [x] 7.6 P1d — gate incoming and byName on the caught-up watermark; return `ErrorCodeIndexNotReady` during cutover
  and cold replay.
- [x] 7.7 P2b — expose re-index and write-failure metrics; add ALIAS to `computeIndexProjection`.
- [x] 7.8 Re-verified `task check:push`, structural, and semantic green with exit 0 and zero validation errors; pushed
  to PR #524 and posted the review reply. One graph-ingest contention flake passed in isolation.
- [x] 7.9 P2a byName bounded-read: cap serial hydration and return typed
  `resource_exhausted`; upgrade-debris purge + source-owned retraction remain
  gh#527.

## 8. Codex 3rd-pass review blockers (PR #524, airtight readiness under concurrency)

- [x] 8.1 #1 — split `initialEnumerationComplete` from `indexBootstrapped`, so non-empty cold replay stays not-ready
  until workers finish; test a preloaded bucket and the incoming-fails-while-not-ready invariant.
- [x] 8.2 #5 — PathRAG propagates every availability/protocol/decode failure
  and rejects structurally incomplete success envelopes; only an explicit empty
  relationships array is empty; direction=both fails if either leg fails.
- [x] 8.3 #6 — gate ALIAS and all PREDICATE query handlers on `ensureQueryReady`; propagate predicate-catalog write
  failures into the entity failure gate.
- [x] 8.4 #3 — mark transient coalescer Get errors failed so readiness and repair engage; only genuine not-found
  drains the watermark.
- [x] 8.5 #4 — make direct query and clustering readers fail closed by default; retain explicit standalone/test
  `allow_ungated_reads`; correct the handler-routing documentation.
- [x] 8.6 #2 — repair routes through the same entity-keyed FIFO dispatcher as
  watcher updates/deletes and reconciles authoritative state at execution;
  ordering correctness is delivered here, not deferred to gh#527.
- [x] 8.7 #7 — spec/design match the delivered key formats, CONTEXT
  reconciliation/delete path, exact watermark completion, bounded repair, and
  retention-only gh#527 scope.
- [x] 8.8 Pushed the third-pass corrections and posted the review reply with `task check:push` and structural evidence.
  This task does not claim a third-pass semantic result; PR #532's later current-stack semantic 46/46 with zero
  validation errors supplies the superseding semantic coverage recorded in task 9.9.

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
- [x] 9.9 The original fourth-pass semantic attempt produced no SemStreams result because SemEmbed's
  `onnx/model.onnx` download returned HTTP 403; it is not recorded as passing. Later PR #532 validation supplied
  current-stack coverage: structural 37/37, statistical 41/41, and semantic 46/46, all with zero validation errors.

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
- [x] 10.5 The OUTGOING correction `d65acbae` is included in merged PR #524; required GitHub checks were green.
