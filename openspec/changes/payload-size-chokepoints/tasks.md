# Tasks — payload-size-chokepoints (gh#857)

> **This change runs under the 2026-08-02 conformance rules** (`.agents/contracts/`,
> developer workflow rule 0): the recorded rulings are the gh#857 owner-constraint comments
> (including the knob-taxonomy correction) and design.md D1–D7. Task 6.1 is the conformance
> table; a deviation from any of them escalates for re-ruling — it does not execute.

**Amend a task line when the work HAPPENS, not only when it succeeds.** A deliberate
not-done gets `[~]`, its reasoning, AND propagation into the spec delta. Run
`task openspec:queue` before archiving.

## 1. Classification and the shared guard

- [x] 1.1 **DONE with a recorded DEVIATION from D2's letter** (2026-08-02): the mapping
      lives at the natsclient boundary (`classifyMaxPayload`, `natsclient/payload_size.go`)
      applied on the KV lanes' and `Publish`'s raw-error returns — NOT as an arm inside
      `errs.Classify`, because `pkg/errs` is dependency-free and must not import nats.
      Spec requirement ("classifies as permanent by any path") is met; wrapped-form path
      covered (`%w` chains preserve the sentinel; `TestCheckPayloadSize` asserts
      `errs.IsInvalid` through the wrap). **Deviation row on the 6.1 table; owner sign-off
      pending.** Original text: map inside `errs.Classify`.
- [x] 1.2 DONE (`natsclient/payload_size.go`: `checkPayloadSize`, `serverPayloadLimit`
      live-read with 1MB fallback). AMENDED from original: no fake-conn unit seam exists on
      `nats.Conn`; derivation-not-hardcoding is proven by (a) fallback + override tests
      (`payload_size_test.go`) and (b) the offload integration test reading the REAL
      server's advertised limit via `ServerPayloadLimit()` on live NATS
      (`result_offload_integration_test.go`). Compiled-in value exists only as the
      unexported no-conn fallback.
- [x] 1.3 DONE: KV `Put`/`Create`/`Update` guarded (`natsclient/kv.go`),
      `UpdateWithRetry*` hardcoded 1MB replaced by `effectiveValueLimit()` (override > live
      > default; `DefaultKVOptions.MaxValueSize` now 0 = derive), publish surface
      enumerated from exported methods and guarded: `Publish`, `PublishToStream`,
      `PublishToStreamWithMsgID`, `PublishToStreamAsync(+WithMsgID)` (shared funnels),
      `PublishBatchToStream` (pre-checks every message before enqueuing any),
      `RequestWithHeaders` (request payloads are publishes too). Guard runs BEFORE
      conn/circuit gates: permanent outranks transient.
- [x] 1.4 DONE: per-seam subtests (`TestSeamGuards_RefuseOversizedBeforeIO`, 10 seams +
      KV lanes over nil bucket = guard-before-I/O ordering proof). Deleting one seam's
      guard call fails exactly that seam's subtest by construction (zero-value client:
      without the guard the seam returns ErrNotConnected/panics instead of
      ErrPayloadTooLarge).

## 2. The respond seam

- [x] 2.1 DONE: `SubscribeForRequests` respond path answers oversized replies with the
      ADR-060 classified error via `RespondError` (`natsclient/request.go`); objectstore's
      raw responder uses new exported `Client.CheckReplySize` + its error Response shape
      (`storage/objectstore/component.go` respond()). **`CheckReplySize` +
      `ServerPayloadLimit` are new exported surface — rows on the 6.1 table, sign-off
      pending.** Covered by seam subtests; a live caller-side timeout-vs-typed-error wire
      test rides the integration tier (the offload integration test exercises the reply
      path under real NATS).
- [~] 2.2 **CANCELED — the premise expired between audit and implementation** (2026-08-02):
      `maxPrefixResponseBytes` is no longer dead — current main enforces it as a byte
      budget with trim-until-fits + `maxPrefixResponseBytesOverride` test hook and a
      regression test (`processor/graph-ingest/query.go`,
      `query_prefix_regression_test.go`). Nothing to delete; the respond-seam guard is now
      the BACKSTOP behind a real per-handler budget. Proposal amended to match.
- [x] 2.3 DONE, amended location: no changelog FILE exists in-tree — the sister-facing
      behavior-change notice (timeout→typed too-large; remedy: narrow/page/offload) rides
      the PR body, gh#857, and the release-tag notes per repo convention. Post-review
      addition: the notice MUST name the offload window — results in (½·limit, limit]
      previously arrived inline in `COMPLETE_*` values and now arrive ref-bearing; sisters
      with their own COMPLETE_ decoders must check themselves (in-tree census is 3.3).

## 3. Agentic: COMPLETE_ values loud and ref-bearing (D4)

- [x] 3.1 Route the four completion/failure/cancellation writes through the guarded KV
      lane (retire the raw `jetstream.KeyValue` handle), return errors, bounded retry on
      transient, typed result-not-durable loop state on permanent. Failing tests per write
      site; the void-return shape is the mutation target (a test must detect a dropped
      return, not a non-nil stub).
      DONE 2026-08-02: raw handle retired (`processor/agentic-loop/component.go:58`
      loopsKV interface, `component.go:665` NewKVStore wrap; `http.go:161-182` migrated);
      all four persist* return errors with bounded retry (`component.go` writeLoopKV +
      loopKVWriteRetry, MaxAttempts=3) and `markResultNotDurable` sets + persists
      `LoopEntity.ResultNotDurable/-Reason` (`agentic/state.go`,
      `processor/agentic-loop/state.go` MarkResultNotDurable). Per-site tests in
      `processor/agentic-loop/persist_durability_test.go`; mutation checks run (swallowed
      error → 2 tests FAIL; verified via cp-backup, checksum-restored). Mid-loop write
      failure deliberately does NOT mark result-not-durable (false-claim guard;
      TestPersistLoopState_MidLoopFailure_DoesNotClaimResultLoss).
- [x] 3.2 Offload results above the derived threshold to `AGENT_CONTENT` with
      `{storage_ref, preview, size}` in the KV value (additive fields; payload-registry
      round-trip test per house rule).
      DONE 2026-08-02: threshold = ½ live `ServerPayloadLimit()`
      (`component.go` resultOffloadThreshold; NEW exported `natsclient.ServerPayloadLimit`
      `natsclient/payload_size.go` — new framework export, flagged for reviewer sign-off
      on the conformance table); additive fields on `LoopCompletedEvent`
      (`agentic/events.go` json `storage_ref`/`preview`/`size`) with production-decoder
      round-trip test (`agentic/loop_result_entity_test.go`); carrier entity
      `agentic.LoopResultEntity` + `TryLoopResultEntityID`
      (`agentic/loop_result_entity.go`, `agentic/entity_ids.go`). Offload mutates the
      shared event BEFORE the entity write and RE-MARSHALS the queued agent.complete
      publish (`component.go` offloadCompletionResult + rebuildCompletionMessage) so KV,
      entity, and stream lanes carry one shape — mutation check: dropped offload call →
      end-to-end test FAILS. Loop entity mirrors {result_ref, result_size, preview}
      (`processor/agentic-loop/state.go` ApplyResultOffload).
- [x] 3.3 `read_loop_result` resolves refs transparently under its existing
      `max_bytes`/`offset` contract; enumerate ALL readers of `COMPLETE_*` values from the
      owning components and cover each.
      DONE 2026-08-02. Hydration: `processor/agentic-tools/loop_result.go`
      (LoopContentFetcher + hydrateResult; pages over HYDRATED content, fail-closed on
      absent fetcher/role/field; tests in `loop_result_test.go`); wired in
      `executors/register_read_loop_result.go` + `executors/register.go` (new
      ToolDependencies.ContentBucket, default AGENT_CONTENT). READER CENSUS (grepped
      COMPLETE_/AGENT_LOOPS/LoopCompletedEvent tree-wide):
      (1) read_loop_result — ref-aware (above). (2) flow_monitor_executor — metadata-only
      aggregation, never reads Result; unaffected by additive fields; pinned by
      TestDecodeTerminalEvent_OffloadedCompletion. (3) agentic-dispatch
      (loopFromCompletion → activity SSE + loops listing) — surfaces preview WITH
      `result_truncated` + `result_size` markers (`loop_wire.go`; Loop OpenAPI schema is
      reflected, regen committed); tests in `loop_wire_test.go`. (4)
      research-graph-synthesize — WRITER only (guarded KVStore lane since group 1;
      `adapters.go` PutLoopCompletion), decodes no COMPLETE_ values; unaffected. (5)
      agentic-loop http.go — SKIPS COMPLETE_ keys, decodes LoopEntity (additive fields);
      migrated to the guarded lane. (6) output/otel span_collector — consumes
      LoopCompletedEvent from the agent.complete STREAM, reads metadata fields only
      (never Result); unaffected. (7) frameworkcapabilities/graphresearch — writer via
      guarded KVStore; no COMPLETE_ decode. (8) e2e scenarios (ops: key existence only;
      research-graph: reads the research-graph-written envelope) — unaffected. (9)
      processor/rule/entity_substitution.go + vocabulary — doc comments only.
- [~] 3.4 Integration test: oversized completion → offloaded → read back whole via paging;
      crash between offload and KV write → redelivery converges (no dangling ref
      presented as complete).
      FIRST HALF DONE 2026-08-02:
      `processor/agentic-loop/result_offload_integration_test.go` drives the production
      path (task → agent.request → agent.response, 720KB result) against real
      NATS/ObjectStore: ref-bearing COMPLETE_, slim entity, exact-byte reassembly via
      read_loop_result paging; PASS under `-tags=integration -race`. SECOND HALF (crash
      injection between offload and KV write) NOT BUILT — no crash-injection harness at
      the persist seam exists. Convergence argument recorded instead: the ref becomes
      visible ONLY via the KV write that follows it, so a crash before the write leaves
      COMPLETE_ ABSENT (honest absence, never a dangling ref presented as complete);
      redelivery re-runs the offload to a fresh timestamped content key
      (`storage/objectstore/store.go:671` generateContentKey) and the orphaned object is
      unreferenced by construction. The crash-injection test remains open for the
      follow-up or reviewer ruling.

## 4. Agentic: request-lane bound (D5)

- [x] 4.1 Interim loudness first: with the D1 guard live, an over-limit `agent.request`
      publish fails the loop with a typed reason naming size and limit — never a retry
      loop. Failing test drives a loop to the limit with the fake-connection limit set low.
      DONE 2026-08-02: `processor/agentic-loop/component.go` publishResults detects
      `errors.Is(err, natsclient.ErrPayloadTooLarge)` on a NON-terminal result and routes
      through failLoopForOversizedPublish → handleLoopFailure (reason
      `payload_too_large`; error carries the guard's size+limit facts; COMPLETE_ failure
      event written for watchers). No transient-retry path can capture it: publishResults
      is the only request-publish site (grep: initial via handleTaskMessage →
      persistHandlerResult → publishResults; continuation via handlers.go
      result.PublishedMessages, same sink) and the guard refuses BEFORE the circuit
      breaker/connection gates. Test drives the PRODUCTION seam with a conn-less
      `natsclient.NewClient` + 2MB payload
      (persist_durability_test.go TestPublishResults_OversizedPublish_FailsLoopTerminally
      + already-terminal boundary test); mutation check: branch removed → test FAILS
      (cp-backup, checksum-restored).
- [~] 4.2 Hydration: loop-side builder offloads bulky historical message content above the
      derived threshold to `AGENT_CONTENT` refs; `agentic-model` hydrates refs to
      identical full text before the provider call. Byte-identical hydration is the
      assertion: fixture proves the provider-bound body with and without offload is equal.
      DEFERRED 2026-08-02 (design D5 anticipated this ordering: "If hydration slips, the
      guard still ships"). Reasoning: hydration is a full slice of its own — an additive
      `content_ref` on `agentic.ChatMessage`, an offload seam in the loop's request
      builder that must not disturb the in-memory context, a NEW content-store dependency
      and hydration failure-mode surface in `processor/agentic-model`, plus the
      byte-identical fixture — and shipping it half-wired was explicitly ruled out. The
      interim bound (4.1) is live and loud. Propagated into the spec delta:
      `specs/agentic-loop/spec.md` now ships the loud-terminal requirement and marks the
      hydration requirement + "Deep loop crosses the wire limit" scenario DEFERRED with
      the follow-up obligation recorded.
- [x] 4.3 Re-document `tool_result_max_bytes` as an ingestion bound (D6) — schema
      description + docs; assert nothing represents it as wire defense. `task
      schema:generate` + diff clean.
      DONE 2026-08-02: `processor/agentic-loop/config.go:54-61` (doc comment + schema
      description: ingestion bound, NOT wire defense, 0=unlimited safe because the seam
      guard backstops). Swept every mention (`grep -rn tool_result_max_bytes` across
      go/md/json/yaml): only code + generated schema + this change's own docs; no doc
      represents it as wire defense. `task schema:generate` run; regenerated
      `schemas/agentic-loop.v1.json` + `specs/openapi.v3.yaml` in the working tree.
- [x] 4.4 Knob taxonomy sweep: classify every size-adjacent knob in the tree by which
      limit it defends (ingestion/resource policy stays; wire defense dissolves into the
      seams); record the classification table in the design doc; retire only proven
      wire-defense knobs.
      DONE 2026-08-02: table appended to design.md ("Knob taxonomy (task 4.4 sweep)").
      Outcome: exactly one wire-defense knob existed (`KVOptions.MaxValueSize`), already
      dissolved by group 1 (derive-by-default, explicit override retained); every other
      knob classified ingestion or resource policy and stays; nothing met the
      wire-defense-AND-unused retirement bar.

## 5. Governance stream ruling (D7)

- [ ] 5.1 Present D7's options to the owner; record the ruling here verbatim before
      implementing. Recommendation on file: DiscardOld + fill-ratio metric/Warn now,
      archival exemption recorded as ADR-068-lane follow-up.
- [ ] 5.2 Implement per the ruling (metric + threshold Warn if (a)); test that the metric
      moves as the stream fills.

## 6. Gates

- [x] 6.1 **Conformance table** (filled 2026-08-02; reviewer verifies per contract; owner
      sign-off needed on the DEVIATION and NEW-SURFACE rows):

      | Ruling | Status | Evidence |
      |---|---|---|
      | D1 one derived-limit guard, ≤5 chokepoints, Invalid refusal | CONFORMS | `natsclient/payload_size.go` (`checkPayloadSize`, `serverPayloadLimit`); wired: `kv.go` Put/Create/Update/UpdateWithRetry (`effectiveValueLimit`), `client.go` Publish + both stream funnels + batch pre-check, `request.go` RequestWithHeaders |
      | D2 oversize classifies permanent | **DEVIATION — sign-off pending** | mechanism at natsclient boundary (`classifyMaxPayload`) — `pkg/errs` stays dependency-free. Post-review (6.6): residue classification now covers EVERY send lane — both stream funnels, async enqueue, `Request`/`RequestWithHeaders`/`RequestWithRetry`/`RequestReady` loops (which also stop retrying and stop counting breaker failures on oversize), `ReplyWithHeaders` — so "wherever it surfaces" holds without the earlier overclaim |
      | D3 typed too-large reply, never timeout | CONFORMS | `request.go` respond path via `RespondError`; objectstore respond via `CheckReplySize` |
      | D4 COMPLETE_ loud + ref-bearing, reader census | CONFORMS (3.4 crash-injection half `[~]`) | `processor/agentic-loop/component.go` persist*/offload, `agentic/loop_result_entity.go`, `processor/agentic-tools/loop_result.go` hydration; census in 3.3 |
      | D5 request lane: loud interim, hydration behavior-neutral | CONFORMS-with-deferral (sanctioned by D5's own slip clause) | 4.1 shipped (`failLoopForOversizedPublish`); 4.2 hydration `[~]` DEFERRED, propagated into the agentic-loop delta |
      | D6 knob taxonomy: ingestion stays, wire dissolves | CONFORMS | `config.go:54` re-doc; taxonomy table appended to design.md; sole wire-defense knob (KVOptions.MaxValueSize default) dissolved to derive |
      | D7 governance-stream ruling | OPEN — owner ruling pending (task 5) | recommendation on file in design.md |
      | Owner C1: no component wire-size knowledge/knobs | CONFORMS | no new knobs; thresholds derived (`resultOffloadThreshold` = ½ live limit) |
      | Owner C2: paved path is default | CONFORMS for results; request lane pending 4.2 | offload automatic above threshold |
      | Owner C3: agentic lanes first | CONFORMS | groups 3–4 shipped in this slice |
      | Knob correction (ingestion vs wire) | CONFORMS | 4.3 + 4.4 |
      | NEW EXPORTED SURFACE — **sign-off pending** | listed (review-verified complete) | `natsclient.ServerPayloadLimit`, `natsclient.CheckReplySize`, `natsclient.ErrPayloadTooLarge`, `agentic.LoopResultEntity` + `TryLoopResultEntityID`, `agentictools.LoopContentFetcher`, `NewReadLoopResultExecutor` signature widened (compile-breaking for out-of-tree callers; all in-repo callers updated), `ToolDependencies.ContentBucket`, additive fields on `LoopCompletedEvent`/`LoopEntity`/dispatch `Loop`. Review excess resolved: `LoopManager.ApplyResultOffload`/`MarkResultNotDurable` UNEXPORTED (zero out-of-package consumers) |
- [x] 6.2 DONE 2026-08-02: `gofmt` clean, `task lint` clean (revive+vet+fmt+port guard),
      `go vet` clean plain and `-tags=integration` on all touched trees.
- [x] 6.3 DONE with an honest bound: full `go test -race ./...` GREEN (one failure found
      and fixed during the run: line-pinned entity-id-audit annotation shifted by the
      dispatch wire fields — repinned, green). Integration: 108 packages GREEN in a full
      `-p 2` run that was interrupted before completing the matrix (shared-Docker
      contention environment); `natsclient` full integration re-run GREEN standalone after
      updating `kv_error_integration_test.go` to assert the classified contract
      (sentinel + Invalid class) instead of the retired prose; `agentic-loop` full
      integration GREEN incl. the new offload test. **CI is the arbiter for the complete
      integration matrix** — recorded, not asserted.
- [x] 6.4 DONE: `task schema:generate` run; `git diff schemas/ specs/` shows only the
      intended regen (agentic-loop schema description, OpenAPI additive fields);
      `go test ./test/contract/...` GREEN.
- [x] 6.5 DONE 2026-08-02: `task e2e:agentic` GREEN — real verdict, full metric set
      (trajectory steps 6, tool executions 1, governance verdicts 1, loop triples 19)
      through the modified completion path. `task e2e:core` GREEN — `passed=2 failed=0`,
      TASK_RC=0 captured directly (not a pipeline tail). Both tiers post-#845, i.e. tiers
      that CAN fail.
- [x] 6.6 DONE 2026-08-02: `semstreams-reviewer` full-diff pass, CHANGES REQUESTED, all
      findings fixed same-session: BLOCKING-1 four unguarded request/reply lanes
      (`Request`, readiness probe, retry lane — which retried the impossible write and
      poisoned the breaker — `ReplyWithHeaders`) now guarded + residue-classified;
      BLOCKING-2 header-residue `ErrMaxPayload` on both stream funnels +
      `RequestWithHeaders` now classified permanent and excluded from breaker counting;
      HIGH taxonomy-row/Impact pre-correction claims re-synced (the correction-propagation
      class, caught on our own change); HIGH D3-branch-untested → 
      `TestIntegration_OversizedReplyAnswersTyped` drives real NATS: typed Invalid error
      at the caller in 0.64s vs the 10s timeout pathology; MEDIUM surface excess →
      two LoopManager methods unexported, signature change added to the table; NIT kv.go
      comment fixed. Reviewer's clean list (guard-vs-breaker ordering, gh#810
      no-conflict, offload atomicity, hydration fail-closed, retry classification,
      additive-fields round-trip) recorded in the review output.
- [ ] 6.7 Owner-run Codex round; arm `--auto` only AFTER it closes.
- [ ] 6.8 Owner CONFIRM-CLOSE on gh#857; gh#855's deferred CONFIRM-CLOSE unblocks when the
      clustering-relevant chokepoints (1.x, 2.x) land — note it there.
- [ ] 6.9 Archive: `payload-bounds` Purpose ships in the delta; confirm `agentic-loop`'s
      Purpose does not regress.
