# Tasks — payload-size-chokepoints (gh#857)

> **This change runs under the 2026-08-02 conformance rules** (`.agents/contracts/`,
> developer workflow rule 0): the recorded rulings are the gh#857 owner-constraint comments
> (including the knob-taxonomy correction) and design.md D1–D7. Task 6.1 is the conformance
> table; a deviation from any of them escalates for re-ruling — it does not execute.

**Amend a task line when the work HAPPENS, not only when it succeeds.** A deliberate
not-done gets `[~]`, its reasoning, AND propagation into the spec delta. Run
`task openspec:queue` before archiving.

## 1. Classification and the shared guard

- [ ] 1.1 `errs.Classify`: map `nats.ErrMaxPayload` → `ErrorInvalid` (D2). Failing test
      first; include the wrapped-form path (`errors.Is` through `fmt.Errorf %w`).
- [ ] 1.2 Implement `checkPayloadSize` in `natsclient` (D1): limit from `Conn.MaxPayload()`
      at call time, refusal `WrapInvalid` with bytes/limit/target/remedy. Unit tests with a
      fake connection advertising a non-default limit prove derivation, not hardcoding.
- [ ] 1.3 Wire the guard into `KVStore.Put`/`Create`/`Update`, replace `UpdateWithRetry*`'s
      hardcoded `MaxValueSize` with the derived limit, and wire `Publish`/
      `PublishToStream`/`PublishToStreamAsync*`. Enumerate the publish surface from
      `natsclient`'s exported methods, not from this list.
- [ ] 1.4 Mutation-check the WIRING per seam: remove the guard call at ONE seam and confirm
      that seam's test FAILS (per-seam, so a missed seam cannot hide behind the others).

## 2. The respond seam

- [ ] 2.1 On oversized reply in `SubscribeForRequests`' respond path (and the objectstore
      raw responder's), send the ADR-060 classified error reply naming size and limit
      (D3). Failing test: caller through `RequestClassified` receives the typed permanent
      error within normal latency; no timeout path.
- [ ] 2.2 Delete the dead `maxPrefixResponseBytes` constant
      (`processor/graph-ingest/query.go:222`) — the respond guard is the mechanism; a
      declared-but-unread budget is a false comfort.
- [ ] 2.3 Sister-facing changelog entry: timeout→typed-error behavior change, with the
      remedy taxonomy (narrow/page/offload).

## 3. Agentic: COMPLETE_ values loud and ref-bearing (D4)

- [ ] 3.1 Route the four completion/failure/cancellation writes through the guarded KV
      lane (retire the raw `jetstream.KeyValue` handle), return errors, bounded retry on
      transient, typed result-not-durable loop state on permanent. Failing tests per write
      site; the void-return shape is the mutation target (a test must detect a dropped
      return, not a non-nil stub).
- [ ] 3.2 Offload results above the derived threshold to `AGENT_CONTENT` with
      `{storage_ref, preview, size}` in the KV value (additive fields; payload-registry
      round-trip test per house rule).
- [ ] 3.3 `read_loop_result` resolves refs transparently under its existing
      `max_bytes`/`offset` contract; enumerate ALL readers of `COMPLETE_*` values from the
      owning components (`read_loop_result`, `flow_monitor`, research-graph adapters — and
      grep for others; the list here is a hint, not the census) and cover each.
- [ ] 3.4 Integration test: oversized completion → offloaded → read back whole via paging;
      crash between offload and KV write → redelivery converges (no dangling ref
      presented as complete).

## 4. Agentic: request-lane bound (D5)

- [ ] 4.1 Interim loudness first: with the D1 guard live, an over-limit `agent.request`
      publish fails the loop with a typed reason naming size and limit — never a retry
      loop. Failing test drives a loop to the limit with the fake-connection limit set low.
- [ ] 4.2 Hydration: loop-side builder offloads bulky historical message content above the
      derived threshold to `AGENT_CONTENT` refs; `agentic-model` hydrates refs to
      identical full text before the provider call. Byte-identical hydration is the
      assertion: fixture proves the provider-bound body with and without offload is equal.
- [ ] 4.3 Re-document `tool_result_max_bytes` as an ingestion bound (D6) — schema
      description + docs; assert nothing represents it as wire defense. `task
      schema:generate` + diff clean.
- [ ] 4.4 Knob taxonomy sweep: classify every size-adjacent knob in the tree by which
      limit it defends (ingestion/resource policy stays; wire defense dissolves into the
      seams); record the classification table in the design doc; retire only proven
      wire-defense knobs.

## 5. Governance stream ruling (D7)

- [ ] 5.1 Present D7's options to the owner; record the ruling here verbatim before
      implementing. Recommendation on file: DiscardOld + fill-ratio metric/Warn now,
      archival exemption recorded as ADR-068-lane follow-up.
- [ ] 5.2 Implement per the ruling (metric + threshold Warn if (a)); test that the metric
      moves as the stream fills.

## 6. Gates

- [ ] 6.1 **Conformance table**: D1–D7 + the two gh#857 owner-constraint comments →
      `file:line` or DEVIATION row with owner sign-off. Reviewer verifies the table per
      the reviewer contract.
- [ ] 6.2 `gofmt`, `task lint`, `go vet ./...` plain + `-tags=integration`.
- [ ] 6.3 BOTH suites: `go test -race ./...` AND
      `go test -race -tags=integration -p 2 -count=1 ./...`; grep `^FAIL`.
- [ ] 6.4 `task schema:generate` + `git diff schemas/ specs/` clean;
      `go test ./test/contract/...`.
- [ ] 6.5 `task e2e:agentic` (the tier on the flagship touched path) + `task e2e:core`.
      Confirm the tier can fail before trusting green.
- [ ] 6.6 `semstreams-reviewer` pass on the full diff, including the conformance table.
- [ ] 6.7 Owner-run Codex round; arm `--auto` only AFTER it closes.
- [ ] 6.8 Owner CONFIRM-CLOSE on gh#857; gh#855's deferred CONFIRM-CLOSE unblocks when the
      clustering-relevant chokepoints (1.x, 2.x) land — note it there.
- [ ] 6.9 Archive: `payload-bounds` Purpose ships in the delta; confirm `agentic-loop`'s
      Purpose does not regress.
