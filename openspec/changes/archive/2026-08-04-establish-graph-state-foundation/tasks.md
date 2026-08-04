# Tasks — establish-graph-state-foundation (GS-00)

## 1. Bind program control

- [x] 1.1 Create the only living ADR-090 program record with one Next Action,
      ordered increments, WIP 1, stop/go gates, resume protocol, and append-only
      log.
- [x] 1.2 Bind ADR-090 to the frozen inventory and canonical program without
      turning the ADR into a tracker.
- [x] 1.3 Freeze the accepted decision record, record #894/#895 complete, and
      remove its stale next-slice language.
- [x] 1.4 Scope the pre-v1 baton's exclusivity claim to its own program.
- [x] 1.5 Suspend and freeze `semantic-tier-split` and
      `discovery-under-stream-shapes` in their own proposal/task records without
      rewriting historical task truth.

## 2. Harden evidence and scheduling boundaries

- [x] 2.1 Pin the inventory to `c6ef4541`, record its review status, and link the
      canonical program.
- [x] 2.2 Distinguish repo-local evidence from frozen holdout observations; do
      not require a downstream census for each increment.
- [x] 2.3 Record issue and baton evidence as defect classes, never task order.
- [x] 2.4 Freeze all ten holdouts until the Foundation tag-candidate gate records
      `PASS` and the coordinated migration window begins.

## 3. Draft the foundation contracts

- [x] 3.1 Draft typed authoritative reads carrying current value and per-entity
      revision.
- [x] 3.2 Draft two-axis mutation results: honest commit knowledge and optional
      current authority observation, separate from projection visibility.
- [x] 3.3 Draft role/lifecycle declarations and allowed role-specific
      variation.
- [x] 3.4 Record the graph-index, embedding, and clustering proof matrix and the
      evidence trigger for any future shared runtime.
- [x] 3.5 Bind offline-first and tier fallback behavior.
- [x] 3.6 Bind the complexity budget, 31-document concept baseline, and ratchet.
- [x] 3.7 Bind an objective Foundation tag-candidate gate whose recorded `PASS`
      is required before candidate release or holdout migration.
- [x] 3.8 Record the two graph-write seams and reject a general typed reactive
      subscription absent named-adopter and surface-reduction evidence.
- [x] 3.9 Record the 14-descriptor disposition and GS-01 through GS-14 sequence.
- [x] 3.10 Give GS-11 the deterministic E2E harness, GS-12 GraphQL/read-front,
      GS-13 concept consolidation, and GS-14 the tag-candidate gate.
- [x] 3.11 Schedule `pkg/projection` authority-writer rename/migration and old
      namespace removal as a GS-02 entry/exit requirement.
- [x] 3.12 Bind authority restore, `COMPONENT_STATUS` disposition,
      single-active/active-active proof, scoped rebuild, and GS-10 inference
      ownership gates to their increments and the final tag gate.
- [x] 3.13 Sequence role families one owner at a time from GS-01 through GS-10,
      including reactive/cache census and effectful inference conformance.
- [x] 3.14 Record the owner-approved read supersession: required conformant
      GraphQL; retire/internalize the aggregate embedded client.

## 4. Correct canonical guidance

- [x] 4.1 Correct direct MCP guidance and ledger the runtime placeholder for
      removal or implementation in GS-12.
- [x] 4.2 Replace universal eventual-consistency language with source-specific
      authority/view contracts.
- [x] 4.3 State that `ENTITY_STATES` has history 1 and cannot reconstruct past
      authority.
- [x] 4.4 Correct directly linked query/KV concept pages only where they conflict
      with ADR-090; defer broad concept consolidation.

## 5. Validation and review

- [x] 5.1 `git diff --check` passes and every changed Markdown line is at most
      120 characters.
- [x] 5.2 `openspec validate establish-graph-state-foundation --strict` passes.
- [x] 5.3 SemStreams architecture review APPROVED with no findings and confirmed
      no runtime, receipt, request lookup, idempotency, or compatibility surface
      entered GS-00.
- [x] 5.3a Map every ADR-090 decision and all eight accepted owner rulings to
      exact GS-00 file:line evidence without claiming runtime conformance.
- [x] 5.4 Owner deferred the exact tag/window to GS-14 and `ANOMALY_INDEX`
      survival to GS-10; the GraphQL/embedded-client choice is resolved.
- [x] 5.5 Program Next Action and append-only log are updated after review.

## Archive rule

GS-00 contains no runtime spec delta and archives after architecture acceptance.
Each bounded GS-01+ change creates, implements, promotes, and archives its own
capability delta before the next increment starts.
