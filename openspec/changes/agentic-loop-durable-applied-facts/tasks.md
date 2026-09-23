# Tasks — agentic-loop-durable-applied-facts (#1330, L4a)

> **Re-pinned at `b7ce8727`; amended 2026-09-22 to the owner's rulings.** `scripts/inventory-verify.sh inventory.md`
> at the re-pin → `pins=293 ok=293 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0`. Rulings applied: #1330
> [issuecomment-5773604066](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5773604066) over the
> reconciliation docket and its simplicity re-read
> ([issuecomment-5773199445](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5773199445)), the
> re-read governing where the two differ; `design.md` § 1 carries the bullets. **Scope is L4a.** The approval lane,
> the verdict after waiter loss, the single terminal owner and route-ambiguity metering are **L4b = #1362** and are
> listed under "Moved to L4b (#1362)" at the end of this file — moved, not dropped. The approval lane's and
> sweeper's carrier reorder and OQ8's gate-order decision moved there too (coordinator scoping under OQ7/OQ8,
> `design.md` § 1): L4a's writer becomes CAS `Update` for every caller, but the ORDER flip reaches only the task,
> model-response and tool-result lanes. Delta text those tasks take over is held verbatim at the end of this file. Standing simplicity rule (owner,
> 2026-09-22): keep complexity as low as possible; an edge case that a doc sentence or a plain "not supported" can
> carry does not earn a code branch. From here `scripts/inventory-verify.sh` is expected to go RED as the
> implementation lands — pins are pre-change evidence and a landed change is never re-pinned.

> File:line pins are at `b7ce8727` through the substitutions in `tasks-pins.md`, using the `inventory.md`
> abbreviations (ST = `processor/agentic-loop/state.go`, C = `…/component.go`, H = `…/handlers.go`, and so on); a
> `68c14c8e` pin appears only where the text names the predecessor site it replaces.
> **Two pin classes (checkpoint-4 review, MEDIUM-3).** That baseline holds for a pin in a task's DESCRIPTION: it is
> pre-change evidence at `b7ce8727` and is never re-pinned. A pin inside a **Landed** or **Amended** block is a
> different thing — it names what this branch built, so it is regenerated at the branch head, currently `2e245243`,
> with every number produced by `sed -n "${n}p"` rather than transcribed. Which class a pin belongs to is decided by
> the block it sits in, not by how it is spelled. Dependency order; each task names
> its file and the proving test. No landing tasks (claim PR, CI, merge, archive) — PR checklist, owner ruling #1230
> Option 1. Prerequisites #1327–#1329 merged; #1328 shipped the RequestID grammar (`ST:1339-1347`).

## 1. Field and invariant surface

- [x] 1.0 New `processor/agentic-loop/internal/looprequest`: `Parse(id) (ID, error)` over `<loopID>:req:<i>:<r>` (loopID may itself contain colons; parse from the right, require all four parts, reject a missing retry part), `Next(prev, retry bool) string` (retry → same iteration, retry+1; else iteration+1, retry 0), `Compare(a, b)` on `(iteration, retry)`. `Next` must reproduce `ST:1345-1347` (`iteration = entity.Iterations + 1`; `%s:req:%d:%d`), so birth mints `…:req:1:0` from `Iterations = 0`. `Parse` becomes the one reader of the grammar, replacing the two prefix-only readers on `main`: `ST:1360-1361` (`ExtractLoopIDFromRequest`) and `LP:50` (`loopIDFromStructuredID`). L2 (PR #1335) shipped no parser because no reader existed yet; this change is the reader. Rapid property: `Parse(format(x)) == x`; `Compare` is a total order consistent with `Next`.
      **Landed.** `looprequest.go` — `ID{LoopID,Iteration,Retry}`, `Parse` (right-hand read; canonical ordinals
      only — `007`, `+1` and `iteration 0` are refused), `ID.String`, `Next(prev, retry)`, `Compare(a, b)`. The task
      line above says `Parse(id) (ID, error)` because that is what shipped and what the rest of the change calls;
      the design wrote it as a three-value tuple, and the three parts travel together at every call site, so they
      get a named value rather than three positional returns (developer contract, exported-surface rule). Corrected
      here 2026-09-22 so the task text and the code agree. `Parse` did NOT replace the
      two prefix-only readers (`ST:1360`, `LP:50`): both are on the Tier 1 surface or its call graph and replacing
      them is section 3's lane work, not a grammar change — recorded so the substitution is not lost.
      Tests: `looprequest_test.go` (13 refusal cases + 6 accept cases — the half a round-trip property cannot see),
      `looprequest_prop_test.go` `TestPropParseRoundTripsEveryMintedName` and
      `TestPropCompareIsATotalOrderConsistentWithNext` (boundary-hugging generators: iteration at 1, retry at 0,
      loop IDs that contain colons and that END in `:req`), `FuzzParseNeverPanicsAndRoundTrips` (15 seeds across
      both grammar classes; 2,316,130 execs / 21s clean, `new interesting: 134`).
      Mutant (`cp` backup + md5 + printed `[applied]`, restored and checksum-verified): delete the
      `looprequest.Parse`/`Next` read from `GenerateRequestID` (`ST:1385-1390`) → seven tests red, among them
      `TestRetryOrdinalSurvivesARebuiltLoopManager` and `TestRequestIDCarriesTheTruncationRetryOrdinal`.
- [x] 1.1 `agentic/state.go`: add `PublishedRequestID string \`json:"published_request_id,omitempty"\`` beside `AG:57`
      with the I1 doc comment; `Validate` (`AG:136`) unchanged. Test: `agentic/state_test.go` JSON round-trip;
      `TransitionTo` (`AG:171`) keeps it.
      **Landed** at `agentic/state.go:58-71`; `Validate` untouched. Tests are in a NEW file,
      `agentic/published_request_id_test.go`, not `state_test.go`: that file carries a "Builder must make these
      tests pass without modification" banner. `TestPublishedRequestIDSurvivesTheDurableRoundTrip`,
      `TestPublishedRequestIDIsOmittedWhenUnset` (both directions of additivity — no key when unset, empty when a
      pre-field record decodes), `TestTransitionToKeepsTheOutstandingRequest`,
      `TestValidateIgnoresTheOutstandingRequest`.
- [x] 1.2 `processor/agentic-loop/state.go`: `SetPublishedRequest(loopID, requestID)`; new `restoreLoopFromRequest`
      beside `attachContinuation` (`ST:298`) — no rebuild path exists on `main` — fed the record step 0 (2.5) already
      adopted plus the newest retained request; it rebuilds the ContextManager, the routing maps (`ST:79`
      `outstandingRequests`, `ST:887-888`) and the tool batch (`restoreToolBatch`, membership against the retained
      response only), so it has no `PublishedRequestID` mismatch to refuse. Test: `state_test.go` — set /
      restore-after-adoption / restore-equal.
      **Landed.** `SetPublishedRequest` is at `state.go:1082-1095`, refusing a loop the manager does not hold rather
      than ignoring it, and has three production callers (the mint sites of 2.4). The rebuild landed in two
      commits: `6c91a1cc` for the two manager primitives, `c09cc282` for the reader, the component seam and the
      call sites — sequenced that way because the shape of the ContextManager rebuild is decided by its call sites,
      never ahead of them.

      Five parts, as built:

      1. `LoopManager.restoreLoopFromRequest(record, request)` (`state.go:363`) — seats the loop, its
         ContextManager and its routing maps from the record plus the retained request. The conversation replays
         into ONE region: `system` → `RegionSystemPrompt`, everything else → `RegionRecentHistory` in the retained
         order, then `RepairToolPairs()`. It also restores `cachedTools` / `cachedToolChoice` /
         `cachedResponseFormat` / `cachedRequestTimeout` off the request — an addition to the task text, because
         without them a rebuilt loop's NEXT request advertises no tools at all. The loop is marked outstanding on
         its request (TrackRequest's shape), which `restoreToolBatch` settles when a response for it is in hand.
      2. `LoopManager.restoreToolBatch(loopID, response, applied, inFlight)` (`state.go:466`) — re-derives every
         execution identity from the retained response with `stampToolExecutionCorrelation`, adds the assistant
         turn the batch belongs to, seats names/arguments/ordinals for all of them, seats routes for the unapplied
         ones only, and queues the unapplied minus `inFlight`. **Divergence from the task text:** a fourth
         parameter, `inFlight`. Dispatch is serial, so the call whose result is arriving must not go back on the
         queue; folding it into `applied` would have meant inventing a fake ToolResult value for it.
         `pendingTools` is deliberately left empty — the queue is what says how much of the batch is left, and
         `HandleToolResult` dispatches from it before it ever asks `AllToolsComplete`.
      3. `loopEvidenceReader.ReadRetainedResponse` (`loop_evidence.go:56`) with `responseAddress`
         (`loop_evidence.go:124`) and `readRetainedAgentResponse` (`loop_evidence.go:216`). The address resolves
         from the agent.response **INPUT** port — `requestAddress`'s mirror — so the recovery read and the live
         subscription resolve the same subject after a config change. Both reads share one `newestOn`.
      4. `Component.restoreLoopFromEvidence(ctx, loopID, record, inFlightExecutionID)` (`loop_evidence.go:577`),
         called from both cold arms on `requestOrderCurrent` only (`component.go:1905`, `component.go:2659`), which
         then fall through to the ordinary warm apply (`component.go:1749`, `component.go:2454`) — no second apply
         path for a recovered loop. It ends with `rememberLoopRevision`. **Divergence from the task text:** the
         retained-RESPONSE read fires on the tool lane only. On the response lane, applying the response is what
         creates the batch; pre-seating one would add the assistant turn to the conversation twice.
         `requestOrderUnnamed` and `requestOrderAhead` still retry: neither names a request to rebuild from.
      5. Tests. Unit: `loop_rebuild_test.go` drives the two manager primitives
         (`TestARebuiltLoopIsTheRecordPlusItsRetainedRequest`, four arms — full rebuild, orphan assistant repaired
         away, mismatched request refused, held loop not rebuilt over — and
         `TestARestoredToolBatchKnowsWhatIsLeftToRun`) and both component seams
         (`TestAColdResponseRebuildsTheLoopItAnswers`, `TestAColdToolResultRebuildsTheBatchItBelongsTo`).

      **The W2 halves of 4.2** (moved here 2026-09-22, checkpoint-2 review) are REWRITTEN, not deleted, from
      "retries, writes nothing" to "rebuilds and applies":
      `tool_result_redelivery_integration_test.go` § "W2: the result was applied in memory and the record never
      learned it" and `identity_adoption_integration_test.go` § "W2: the dispatch landed and the record never
      learned it". Both now assert Ack, the replacement HOLDING the loop, and the record moving past the revision
      the crash left — the invariant that survives either way is that the input is never acknowledged away and the
      record never moves on a delivery nobody applied. The tool-lane case gained `retainModelResponse`, which
      publishes the model response to `agent.response.<requestID>` exactly as agentic-model does: a test that calls
      `HandleModelResponse` directly skips the delivery that would have put the durable fact there, and the rebuild
      needs it.
      Recorded residual, not a defect: the record says which executions are APPLIED, never which were DISPATCHED,
      so the rebuild re-dispatches a sibling the predecessor had already sent. Both cases assert the re-dispatch and
      name why it is a replay rather than a duplicate execution — the execution identity is derived, not minted
      (L2), and agentic-tools keys `TOOL_CALL_OUTCOMES` by it.

      **Mutation evidence** (`cp` backup + `md5 -q`, `[applied]` printed between mutating and testing, restore
      verified by checksum, `git status --porcelain` clean after each):
      - (i) `restoreLoopFromEvidence` stops remembering the record's revision. `loop_evidence.go`
        `ec65ad839dff6d229aa583cdbf818ad8` → `b3024025358db0a5c4363037e9b32c3f` → restored
        `ec65ad839dff6d229aa583cdbf818ad8`. RED: `TestAColdResponseRebuildsTheLoopItAnswers` ("the rebuilt holder
        could not write the record it had just read"), and both real-NATS W2 cases ("a response/result the rebuilt
        loop applied is settled, not owed to a process that will never exist").
      - (ii) `restoreLoopFromRequest` stops calling `RepairToolPairs()`. `state.go`
        `ceac885addaa74f5d8dcd8fbfc74b2cc` → `d3474b75b5ef734e1875020d92b8f501` → restored
        `ceac885addaa74f5d8dcd8fbfc74b2cc`. RED:
        `TestARebuiltLoopIsTheRecordPlusItsRetainedRequest/an assistant turn whose results never arrived is
        repaired away`.
      - (iii) `restoreToolBatch` restores the batch without skipping the applied executions. `state.go`
        `ceac885addaa74f5d8dcd8fbfc74b2cc` → `e5cc0d199aa81fde8f60d803c686006a` → restored
        `ceac885addaa74f5d8dcd8fbfc74b2cc`. RED: `TestARestoredToolBatchKnowsWhatIsLeftToRun` and
        `TestAColdToolResultRebuildsTheBatchItBelongsTo` ("the sibling that never ran must be dispatched next; an
        applied one must not be re-run").

      `go test -race -count=3 -tags=integration -p 2` over the two rewritten W2 files plus
      `task_redelivery_integration_test.go`: `ok … 5.242s`.

      **Amended 2026-09-22 (checkpoint-3 review, BLOCKING-2): a retained request is not the conversation.**
      All three mint sites wrap the conversation in `prependIterationContext` (`handlers.go:326`), so the retained
      `AgentRequest` opens with an `[Iteration Budget]` line and, when the loop has a working list, a
      `[Working list …]` block — both Role `system`. The rebuild seated them into `RegionSystemPrompt`, pinning ONE
      iteration's framing at the top of the loop's system prompt for the rest of its life while every later request
      prepended a fresh one; the reviewer read `[Iteration Budget] Iteration 1 of 20 (5% used).` back off the
      rebuilt context on the real-broker W2 path. `isIterationPrefixMessage` (`handlers.go`, beside the function it
      inverts) is the filter, and `restoreLoopFromRequest` drops the LEADING run only — a user may type either
      string, and a message further in is the conversation. The two opening literals are now constants both builders
      and the predicate read, so the recogniser cannot drift from the writer.
      Two claims corrected with it: the doc comment no longer asserts the retained body IS `GetContext()`'s order
      without qualification, and it names the second residual this exposed — system messages are re-seated together
      at the front, so a request that interleaved one does not get that interleaving back. The migration note says
      both in adopter terms.
      Tests, fixtures built by the PRODUCTION prefixer rather than by hand: `loop_rebuild_test.go` "the per-iteration
      prefix belongs to the request, not to the conversation" (drives `handler.prependIterationContext` with a real
      todo list, asserts the rebuilt `GetContext()` equals the un-prefixed conversation) and "a message that only
      looks like the prefix is still the conversation" (the leading-only rule). On the real broker the W2 tool-lane
      case asserts the rebuilt conversation opens on the loop's own task and that no message carries either prefix.
      Mutant (iv): delete the leading-prefix skip in `restoreLoopFromRequest`. `state.go`
      `eaba19f1844225c316baeed4e7dff350` → `c00431a5f7ccb0e9ed6d5570104a24b1` → restored
      `eaba19f1844225c316baeed4e7dff350`; RED on the unit arm (exit 1) AND on the real-NATS W2 arm (exit 1), green
      on restore, `git status --porcelain` empty after.

      **The rebuild's `startTrajectory` degrade branch is GONE (checkpoint-4 review NIT).** Checkpoint 3 added a
      log-and-count degrade there and recorded that no test asserted it because
      `trajectoryManager.startTrajectory` (`trajectory.go:24-32`) returns `nil` unconditionally. That reasoning
      admits the branch was unreachable, and an unreachable degrade branch is dead code pretending to be a guard:
      it advertises a failure mode the function cannot produce, and the contract's own rule is that a declared
      degrade carries a test that a mutation kills. Deleted, along with its
      `recovery_degradations_total{site="rebuilt_trajectory_aggregate"}` site and its clause in the metric's Help.
      The call is now `_, _ = c.handler.trajectoryManager.startTrajectory(loopID)` with the reason written at the
      site. `recovery_degradations_total{site="tool_result_classification"}` — the reachable one, with a test and a
      mutant (task 3.1) — is untouched and is now the metric's only site.

      **Migration note.** `docs/operations/migration-beta162-to-beta163.md` § "A rebuilt loop's conversation is one
      region" records the one visible consequence of the one-region replay: compaction attribution does not survive
      a process replacement. No message is lost and none moves, but per-region sizes reset at the replacement. The
      owner ruled the one-region shape stands with no objection
      ([issuecomment-5776942078](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5776942078)).

## 2. Carrier: order, CAS, identity adoption

- [x] 2.1 `processor/agentic-loop/component.go`: `persistLoopState` (`C:2468`, write `C:2483`) takes `revision` and uses
      `loopsBucket.Update` (`KV:231`) — the writer changes for EVERY caller. The ORDER flip is lane-scoped
      (coordinator scoping under OQ7/OQ8, `design.md` § 1): `persistHandlerResult` (`C:1923`) publishes (`C:1959`)
      before it writes (`C:1947`) for a non-terminal result on the model-response and tool-result lanes only; the
      approval lane's call site (`ARH:205`) keeps today's write → publish, and the sweeper keeps its own
      publish-then-`Put` pair (`AS:100-101`) — both move in #1362. **Superseded in part by 8.2:** a result that
      CREATES an approval gate keeps write → publish too, and it arrives on the tool-result lane, so the carve-out is
      in the carrier rather than at a call site. Birth (`C:1496` publish → `C:1499` `Put` with the
      error ignored today) becomes `Put` → publish, and a birth write that errors returns Retry — the first of
      #1345's five task-intake branches, converted by necessity; the other four stay #1345's. On
      `ErrKVRevisionMismatch` (`KV:238`) return Retry and release the loop's process state (`ST:574`, `C:1964`).
      `persistLoopState`'s other callers ride the same `Update`: `C:1425` (deferred continuation marker) and `AS:101`
      (sweeper). Test: `persist_handler_result_test.go` — publish observed before `Update` on the tool-result lane;
      write observed before publish on the approval lane's call site; CAS conflict → `DeliveryDecisionRetry`; birth
      order write-then-publish.
      **Landed.** The writer is `persistLoopState` (`C:2698`): CAS `Update` against a per-loop revision the process
      retains (`loopRevisions`, `C:73`; `rememberLoopRevision` `C:2624`, `observedLoopRevision` `C:2636`,
      `forgetLoopRevision` `C:2647`, released with the loop at `trajectory_handler_wiring.go:69`). **Deviation from
      the task's wording, recorded:** `persistLoopState` keeps its `(ctx, loopID)` signature and reads the retained
      revision itself instead of taking one. Four callers would otherwise have to carry a fact the component already
      holds, which the developer contract's "am I asking a caller to predict something the framework could observe"
      refuses; the invariant is unchanged, because the retained revision IS the revision this process observed. A
      caller with no observation fails CLOSED (`C:2712`, Fatal) rather than writing blind or updating against
      revision 0, which NATS reads as "must not exist".
      The ORDER is a parameter, `carrierOrder` (`C:1980`), passed at every call site:
      `publishThenWrite` on the model-response (`C:1650`) and tool-result (`C:2244`) lanes for a NON-terminal result
      (`publishThenPersistResultState`, `C:2085`); `writeThenPublish` on the approval lane (`ARH:208`), on the
      failed-terminal tool result (`C:2295`), and for every terminal result on every lane. The sweeper's own
      publish-then-`Put` pair is untouched in order and only rides the new writer (`AS:103`, which now names its
      write failure instead of discarding it — a timer has no delivery to retry).
      Birth is `Put` → publish at `C:1535`, its error returns Retry; the birth publish error stays discarded, which
      is #1345's remaining branch.
      **A second recorded deviation:** the transient test in the carrier is `errors.Is(err,
      natsclient.ErrKVRevisionMismatch)`, NOT `errs.IsTransient`. The latter substring-matches error TEXT for
      "unavailable"/"timeout" (`pkg/errs/errs.go:177-187`), which handed a commit-unknown KV failure a Retry it had
      not earned — caught by `TestResponseAndToolResultPersistenceFailureCannotAck` going from Quarantine to Retry.
      Tests: `loop_carrier_test.go` — `TestCarrierOrderDecidesWhatAFailedPublishLeavesBehind` (both orders, through
      a real but unconnected client so the publish genuinely fails: publish-first leaves NO record, write-first
      commits it), `TestApprovalLaneKeepsWriteThenPublish` (the CALL SITE, through
      `handleApprovalResponseMessage`), `TestCarrierCompareAndSwapLossRetriesAndReleasesTheLoop` (a foreign write
      moves the record → `ErrKVRevisionMismatch`, the loop is gone from memory AND its revision is forgotten).
      Birth order: `carried_continuation_durability_test.go`
      `TestQuarantinedCarryWritesNoRecordAndLeavesTheTurnUncarried` asserts `bucket.written()` is EMPTY after a
      failed publish, which is the order assertion on the response lane's real path.
      Mutants: delete the `publishThenWrite` dispatch (`C:2021-2023`) →
      `TestCarrierOrderDecidesWhatAFailedPublishLeavesBehind` +
      `TestQuarantinedCarryWritesNoRecordAndLeavesTheTurnUncarried` red; replace the CAS `Update` (`C:2724`) with a
      blind `Put` → `TestCarrierCompareAndSwapLossRetriesAndReleasesTheLoop` red.
      **Defect found by CI on the pushed head `cdd2c18b` and fixed inside this change** (run 35719155514,
      `TestIntegration_CancelMidExecution_NoOrphanToolCalls`: the loop stayed `exploring` where the test required
      `cancelled`). The new CAS writer read the revision it compares against as a step SEPARATE from the write, so
      two lanes of ONE process — the cancel signal and the tool lane's advance — interleaved between the read and the
      `Update`, and the loser's compare-and-swap was refused by its own process. A refusal is indistinguishable from
      another process owning the loop, so the cancel path released the loop and reported unknown durability. Root
      cause, not a flake: render → observe revision → `Update` is one critical section and had no lock. Fixed by
      serializing that whole sequence per component (`loopRecordMu`, `C:99`; taken at `C:2811` in `createLoopState`
      and `C:2857` in `persistLoopState`) rather than by retrying a lost CAS — a blind retry would rewrite the record
      from an entity read before the write that beat it, which is the lost-update the CAS exists to refuse.
      Reproduction with
      `go test -race -count=100 -cpu 2,4 -tags=integration -run TestIntegration_CancelMidExecution_NoOrphanToolCalls ./processor/agentic-loop/`:
      **74 of 200 runs red at `cdd2c18b`; 0 of 200 after the fix** (`ok … 298.063s`, exit 0). The same command at the
      default GOMAXPROCS was 0 of 25 — the low CPU counts are what expose the interleaving.
      Test: `loop_record_writer_test.go` `TestTwoLanesWritingOneLoopDoNotRefuseEachOther` — a barrier bucket holds the
      first writer inside its `Update` and announces every entry, so the interleaving is forced by explicit
      synchronization; a second writer reaching the bucket at all fails the test.
- [x] 2.2 `C:2318-2333` `publishResults`: messages on the `agent.request` subject publish via `PublishToStreamWithMsgID`
      with `Nats-Msg-Id = RequestID`. **Shipped by L2 (#1328, PR #1335)** — recorded here because tasks record work when
      it happens. Verified at `b7ce8727`:
      `git grep -n PublishToStreamWithMsgID -- 'processor/agentic-loop/*.go'` →
      `processor/agentic-loop/component.go:2328:		if err := c.natsClient.PublishToStreamWithMsgID(ctx, msg.Subject, msg.Data, msg.MsgID); err != nil {`;
      the three mint sites it carries, `git grep -n 'MsgID:' -- 'processor/agentic-loop/*.go'` →
      `processor/agentic-loop/handlers.go:1174:				MsgID:   request.RequestID,`,
      `processor/agentic-loop/handlers.go:2194:		MsgID:   request.RequestID,`,
      `processor/agentic-loop/handlers.go:2958:		MsgID:   request.RequestID,`.
      Test: `publication_semantics_integration_test.go` (exists). L4a verifies the window collapse only.
- [x] 2.3 `component.go`: new `readRetainedAgentRequest` (identity only; the newest message on `agent.request.<loopID>`,
      pattern `PS:21`/`PS:28`/`PS:37` `GetLastMsgForSubject`, `TR:51`) behind a new evidence-reader interface with two
      reads (request, response), called before `C:2318` for the messages minted at `H:1122`, `H:2168`, `H:2927`; plus a
      revision-returning entity read (`LP:75` discards `entry.Revision()` today). Adopt on exact `RequestID` match,
      publish on absent/current, quarantine anything else (design § 3.3). Test: unit through the evidence-reader seam —
      retained == next → no publish; == current → publish; absent → publish; other → Quarantine.
      **Landed** in a new file, `processor/agentic-loop/loop_evidence.go`: `loopEvidenceReader` (`:32`),
      `natsLoopEvidenceReader` over `GetLastMsgForSubject`, `requestAddress` (`:65`, resolved from the same output
      port the publish side uses), `readRetainedAgentRequest` (`:97`, identity only — the body is decoded for its
      RequestID and nothing else), and the decision `adoptRetainedRequest` (`:218`) called from `publishResults`
      before every minted request (`C:2470`; `msg.MsgID != ""` is exactly the three mint sites, measured).
      The revision-returning entity read is `readLoopRecord` (`:159`) returning `loopRecord{entity, revision,
      presence}` (`:147`); `classifyMissingLoop` keeps its signature and delegates (`loop_presence.go:65`).
      **Recorded simplification (standing simplicity rule, owner 2026-09-22):** the interface carries ONE read, not
      two. The retained-RESPONSE read's only consumer is `restoreToolBatch`, which lands with the cold rebuild; an
      interface method nothing calls is the surface the contract refuses to add. **Recorded simplification #2:** the
      design separates "retained == the record's current request" (publish) from "retained older than it"
      (quarantine); both are "older than the request being minted" here and both publish. Reaching the second
      requires I1 to be broken already — the current request is retained by definition while the record exists — and
      publishing the request the loop needs beats refusing the loop.
      Test: `loop_carrier_test.go` `TestMintedRequestAdoptsAnAlreadyRetainedIdentity`, six arms through the
      evidence-reader seam — already retained (adopt, publish nothing), previous request, retry of the previous,
      nothing retained (publish), a LATER request (Quarantine), another loop's request (Quarantine).
      **The ritual found that test blind to its own wiring**, and a second pin exists because of it: deleting the
      whole identity check from `publishResults` (`C:2469-2477`) left it green, because it drives
      `adoptRetainedRequest` directly. `TestPublishingAMintedRequestConsultsTheRetainedIdentity` drives
      `publishResults` with a client that cannot publish — adoption is then the only thing that can make it return
      nil — and goes red on that mutant; its never-retained arm shows the nil is adoption's, not the path's.
      Two corrections from the internal review (2026-09-22). `readRetainedAgentRequest` with neither a NATS client
      nor an injected reader now returns a transient error rather than `found = false`: both callers read a false
      `found` as "this request has not gone out, publish it", so absence was standing in for unknown. And
      `readLoopRecord` no longer retains the revision it read on the component — every caller of it is asking about
      a loop this process does not hold, the per-loop entry is released only with a loop's own transient state, and
      so one entry accumulated per loop this process was ever asked about; the revision travels in the returned
      `loopRecord` to the one caller that writes with it (`adoptNewerRetainedRequest`), which does not retain its
      committed revision either. Test: `TestAColdReadRetainsNoRevisionForALoopItDoesNotHold`, three arms (a loop
      another process holds, a settled loop, and the adoption that follows a cold read).
- [x] 2.4 `handlers.go`: set the field at the three minting sites (`H:1122` in `buildTaskRequest`, `H:2168` in
      `emitRetryRequest`, `H:2927` in `publishIterationRequest`); mint via `looprequest.Next(PublishedRequestID)`
      (task 1.0); retry ordinal from the parsed field; delete `IncrementTruncationRetry` (`ST:466`) and
      `ResetTruncationRetry` (`ST:477`) with their callers `H:2037`, `H:1389`, `H:1409` and the map cleared at
      `ST:588`. Test: `handlers_test.go` — grammar holds; retry ordinal survives a rebuilt LoopManager.
      **Landed.** The field is set at `H:1138` (`buildTaskRequest`), `H:2190`
      (`emitRetryRequest`) and `H:2956` (`publishIterationRequest`), each beside the `TrackRequest` the site already
      made. `GenerateRequestID` (`ST:1375`) now mints through `looprequest`: iteration stays `Iterations + 1`, and
      the retry ordinal is read back out of `PublishedRequestID` — a mint at the iteration the field already names
      is the retry and takes `Next(published, true)`. The truncation budget moved with it:
      `handleLengthTruncation`'s gate is `publishedRetryOrdinal(loopID) + 1` (`ST:1403`), one spelling of one fact.
      Five caller sites removed, not the three the design enumerated — `H:2037` (`Increment`), `H:1389`/`H:1409`
      (forward-progress resets) AND `H:2054`/`H:2072`, two further `ResetTruncationRetry` calls inside
      `handleLengthTruncation`'s own failure arms that the design's enumeration missed.
      `IncrementTruncationRetry` and `ResetTruncationRetry` shipped one commit marked `Deprecated:` with no
      production caller, on the reading that removing an exported method from a **Tier 1 package** (ADR-106,
      `release/tier1-packages.txt:75`) was the owner's call. **Both are DELETED as of 2026-09-22**, under the
      coordinator's ruling on the checkpoint-1 review applying the standing no-deprecation rule: a helper whose last
      caller is gone goes with it. `truncationRetryAttempts` (the map, its two constructor inits and its clear in
      `DeleteLoop`) went with them — with both methods gone nothing read or wrote it. `task api:compat:report` adds
      exactly `(*LoopManager).IncrementTruncationRetry: removed` and `(*LoopManager).ResetTruncationRetry: removed`
      to the three incompatible entries `processor/agentic-loop` already carried, and the package total is
      unchanged at 15. Migration note: `docs/operations/migration-beta162-to-beta163.md` § "The loop record names its
      outstanding request, and the truncation-retry helpers are removed (#1330, restart safety L4a)", which records
      the measured sister result — a `grep -rn` over every `sem*` repository finds the two names only in frozen
      `.txt` copies of this repository's own source under `semdev/openspec/changes/.../evidence/`, and a control
      grep over the same tree set reaches sister Go sources.
      The one test caller, `populatedLoop`'s `IncrementTruncationRetry` line in `terminal_release_test.go`, is
      dropped with the map it populated: the durable ordinal is not a per-loop manager map, so that assertion has no
      subject to move to. `perLoopMapCount` loses the same entry and the fixture floor drops 13 → 12, keeping the
      slack that refuses a fixture which skips a map.
      Tests: `execution_identity_test.go` `TestRequestIDCarriesTheTruncationRetryOrdinal` (rewritten onto
      `SetPublishedRequest`, the production seam) and the new `TestRetryOrdinalSurvivesARebuiltLoopManager` (a
      SECOND manager holding only the durable record mints `:3:2`, the name a process-local counter could not);
      `published_request_identity_test.go` `TestEveryMintedRequestIsNamedOnTheLoopRecord` (all three mint sites, in
      one run, through `HandleTask` → truncation retry → tool batch); `truncation_branch_test.go`
      `TestHandleLengthTruncation_BudgetRenewsOnTheNextIteration` replaces
      `TestHandleLengthTruncation_ResetAfterForwardProgress`, which encoded the counter's semantics (a tool-call
      response mid-iteration renewed the budget); the durable rule is one self-heal per ITERATION, and the new test
      drives a real tool batch to the advance.
      Mutant: delete the `SetPublishedRequest` call from `publishIterationRequest` (`H:2956-2958`) →
      `TestEveryMintedRequestIsNamedOnTheLoopRecord`, `TestMintedRequestIDsAreInjectiveAcrossTheHandlerPath` and
      `TestHandleLengthTruncation_BudgetRenewsOnTheNextIteration` red.
      **Superseded in part by 7.3** (owner Codex round, finding 3; #1330 Q1, 2026-09-23): "the three minting sites"
      is no longer where the field is set. Birth keeps its mint-site call; the two ITERATION sites lost theirs to
      the carrier, which stamps after the request PubAcks. Everything else on this row — the grammar, the durable
      retry ordinal, the deletions, the migration note — stands.
- [x] 2.5 Step 0 for every cold read but the task lane (design § 3.6): read the newest retained request (task 2.3's
      reader), order it against `PublishedRequestID` with `looprequest.Parse`/`Compare` (task 1.0); newer →
      `Update(revision)` the record before classifying — field, `Iterations = parsed iteration − 1` (`ST:1345`: a
      record's request carries `Iterations + 1`), `PendingToolResults = nil` (the shape the advance itself leaves on
      `main`, `H:2870` → `ST:1121`), and `PendingApproval = nil` + `State = running` when a gate was pending; no entry
      is synthesized. Older/unparseable → Fatal → Quarantine. Wire at the cold arms `C:1700`
      (`settleResponseWithoutLoop`) and `C:2292` (`settleToolResultWithoutLoop`); NOT the task lane (Q1) and NOT cancel
      (`C:2593-2612` classifies by `State` only). Test: unit via the evidence-reader seam — equal / newer-adopt (record
      passes `Validate`, `AG:136`; applied set empty; I4 holds) / newer-adopt over an `awaiting_approval` record (gate
      cleared, `State = running`) / older / unparseable.
      **Landed** as `adoptNewerRetainedRequest` (`loop_evidence.go:271`), wired at both cold arms:
      `settleResponseWithoutLoop` (`C:1775`) and `settleToolResultWithoutLoop` (`C:2446`), each BEFORE the arm
      returns. The task lane and cancel are untouched. The gate clear is `LoopEntity.ResolveApproval` (`AG:246`) —
      the existing owner of that transition — which restores `StateBeforeApproval`; there is no `running` state in
      the vocabulary, and `StateBeforeApproval` is what "running" names. The adopted record is `Validate`d before it
      is written, and the write is `Update(record.revision)`, so a record that moved under the read is a Retry, not
      a silent overwrite.
      L4a note: the classification that follows step 0 is tasks 3.1/3.2, so today the arm still returns its existing
      not-held error after adopting. The adopt is idempotent — the redelivery reads the record it just wrote and
      compares equal.
      **Cost, corrected 2026-09-22** (the checkpoint's first reading said a cold redelivery pays a KV write per
      redelivery; it does not): the FIRST cold read that finds a newer retained request pays one `Update`. Every
      redelivery after it re-reads the record it just wrote, `looprequest.Compare` returns 0 and
      `adoptNewerRetainedRequest` returns at `loop_evidence.go:312` before building anything — one KV write in
      total, then one record read plus one `GetLastMsgForSubject` per redelivery. That read pair is the standing
      cost of a lane whose classification is still tasks 3.1/3.2's, and it ends when they land.
      Test: `loop_carrier_test.go` `TestColdReadAdoptsTheNewestRetainedRequestFirst`, seven arms — newer-adopt
      (`published_request_id`, `iterations = parsed − 1`, applied set emptied, `Validate` green), newer-adopt over an
      `awaiting_approval` record (gate nil, state restored), current (nothing written), nothing retained (nothing
      written), record naming a request the stream never retained (Quarantine), unparseable retained (Quarantine),
      unparseable record field (Quarantine).
      **Same blind spot, same remedy:** deleting the `adoptNewerRetainedRequest` call from BOTH cold arms
      (`C:1775`, `C:2446`) left all seven arms green. `TestColdSettlementArmsAdoptBeforeTheyRefuseTheDelivery`
      drives `handleResponseMessage` and `handleToolResultMessage` through `deliverylane.Consume` and reads the
      record afterwards; both its arms go red on that mutant.
      **Amended 2026-09-22 (checkpoint-3 review, HIGH-1): step 0 adopts only into a record that NAMES a request.**
      `orderAgainstPublished` gives an empty `PublishedRequestID` the pre-#1330 answer on purpose —
      `requestOrderUnnamed`, retry to the loop's holder, settle nothing — and step 0 contradicted it: with the field
      empty the ordering guard was skipped entirely and the newest retained request was written onto the record
      together with `PendingToolResults = nil`, so the evidence the holder was about to apply was drained by the one
      process that could not apply it, on a delivery it then retried anyway. `adoptNewerRetainedRequest` now returns
      as soon as it reads an unnamed record: no stream read, no write. The seven arms above are unaffected — each
      seeds a record that names a request.
      Test: `loop_carrier_test.go` `TestStep0LeavesAnUnnamedRecordExactlyAsItFoundIt` — an unnamed record with a
      non-empty applied set and a newer request retained, driven through the real cold tool lane, asserting Retry
      with revision, applied set, request name and iteration count all unchanged.
      Mutant (v): make the early-return condition constant false in `adoptNewerRetainedRequest`. `loop_evidence.go`
      `9ac5734981c08360d67bb522e9a6f554` → `08695b3a2e35e77f08e61b859d278294` → restored
      `9ac5734981c08360d67bb522e9a6f554`; RED at "step 0 adopted into a record the classification refuses to order
      against" (exit 1), green on restore (exit 0), `git status --porcelain` empty after.
      One fixture moved with the rule: `TestAColdAdoptAndAWarmWriteOfOneLoopDoNotRefuseEachOther` seeded an UNNAMED
      record, so under the new arm the adopt it is about never reaches the bucket. Its loop now names a request,
      which is the state every loop past its birth is in.
- [x] 2.6 Birth by `Create` and CAS-loss release (docket OQ3, owner ruling 2026-09-22): birth writes the record with
      `loopsBucket.Create` (`KV:211`) so a second consumer's birth is refused with `ErrKVKeyExists` (`KV:218`) and
      takes the cold fork; a CAS loss anywhere on the carrier releases the loop's process state (`ST:574` `DeleteLoop`,
      `C:1964` `releaseLoopTransientState`) before it returns Retry. Test: `persist_handler_result_test.go` — a second
      birth for the same loop ID is refused and forks cold; after a CAS loss the loop holds no in-memory state and the
      redelivery reads the winning record.
      **Landed.** `createLoopState` (`C:2663`) writes birth with `Create` and seeds the loop's revision; the refusal
      is `natsclient.ErrKVKeyExists` and birth answers it by releasing the loop it just built in memory and
      returning Retry (`C:1535-1547`). `natsclient.IsKVConflictError` is the shared classifier at both sites — the
      component holds a raw `jetstream.KeyValue`, not a `natsclient.KVStore`, so `KV:211`/`KV:231` are the pattern
      and not the call; the conflict class is unambiguous by call site (Create ⇒ key exists, Update ⇒ revision
      moved). Recorded, because it is the one place the design's pins do not resolve to the call this makes.
      CAS-loss release: `persistLoopState` (`C:2721-2727`) calls `releaseLoopTransientState` before returning, which
      takes `DeleteLoop` (`ST:581`) and `forgetLoopRevision` with it.
      Birth's publish error is returned too, not discarded (internal review, 2026-09-22): the record written a line
      earlier names R1, and I1 says that while the record exists the stream retains that request, so acknowledging a
      birth whose publish did not land leaves the record naming a request nothing retains — the state § 3.6's I1 arm
      answers with Quarantine on every later cold read. It returns instead, and the task lane classifies it (an
      ordinary publish error is Retry).
      **In-scope gap, recorded:** "takes the cold fork" is task 3.4's; until it lands, a refused birth AND a birth
      whose publish returned both Retry instead of forking, bounded by the lane's MaxDeliver — a redelivery meets
      birth's own `Create`, is refused with `ErrKVKeyExists` and Retries again. Task 3.4 is what republishes R1 from
      the record, so it is a merge precondition for this change, not a later slice.
      Tests: `loop_carrier_test.go` `TestBirthRefusesASecondCreateForTheSameLoop` and
      `TestCarrierCompareAndSwapLossRetriesAndReleasesTheLoop` (the loop is gone from memory and its revision with
      it); the I1 pair at birth — either the stream retains the request the record names, or the delivery was not
      ACKed — is `TestBirthWhosePublishFailsIsNotAcknowledged` (unit: nothing retained, unconnected client, the
      lane answers Retry and the message is never ACKed) plus
      `TestIntegrationBirthAcknowledgesOnlyWhatTheStreamRetains` (real broker: the request is retained under the
      name the record carries and the delivery ACKs). Neither arm alone separates "acknowledges what was published"
      from "acknowledges regardless".
      Mutants: `Create` → `Put` in `createLoopState` (`C:2674`) → `TestBirthRefusesASecondCreateForTheSameLoop` red.
      Deleting the `createLoopState` CALL at birth (`C:1535-1547`) left the package green — the third pin the ritual
      bought: the birth test drives `handleTaskMessage` through the real task lane with an unpublishable client, so
      a record in the bucket can only have been written before the publish that did not land, and it goes red there.
      Re-discarding the birth publish error (`_ = c.publishResults(...)`) → `TestBirthWhosePublishFailsIsNotAcknowledged` red.
## 3. Lane classification (3.9 is L4b's — see "Moved to L4b")

- [x] 3.1 Tool-result classification: nothing to delete on `main`; build it at component entry `C:2195` (ahead of
      `H:2546` and `H:2580`) and in `C:2292`'s live arm — cold → 2.5 first; classify `result.RequestID` against
      `PublishedRequestID` (older → ACK; newer → Retry; unknown → Quarantine). Test: write
      `tool_result_recovery_test.go` (absent on `main`).
      **Landed.** One reading of the grammar for all four sites that ask: `orderAgainstPublished`
      (`processor/agentic-loop/loop_classification.go:57`) orders an incoming request name against the one the record
      names — applied / current / ahead / foreign / unnamed — so warm and cold, tool lane and response lane, cannot
      drift into four readings of one grammar. The tool lane's warm arm is `classifyRedeliveredToolResult` (`:174`),
      called at component entry (`C:2440`) BEFORE `HandleToolResult`, which stores the result and acts on `StopLoop`
      ahead of its own terminal guard: a result the loop has moved past has to be settled there or not at all. The cold
      arm classifies after step 0 (`C:2573`). Older → Ack with a counted drop; ahead of the record → Retry, not yet
      observable; not this loop's grammar → Quarantine.
      **Deviation, recorded:** the warm arm classifies against the loop's IN-MEMORY entity rather than re-reading the
      record. That entity is exactly what the carrier marshals into `AGENT_LOOPS`, and this process holds the loop, so
      a read would return what the process already has at the price of a round trip on every delivery. The cold arms
      read the record, because there the entity is what is missing.
      Tests: `tool_result_recovery_test.go` — `TestRedeliveredToolResultIsClassifiedBeforeTheHandlerTouchesIt` (older,
      not-yet-named, foreign, current) and `TestColdToolResultIsClassifiedAgainstTheAdoptedRecord` (superseded by the
      adoption, foreign). The real broker is 4.2.
      Mutant (`cp` backup + md5 + printed `[applied]`, restored and checksum-verified): delete the whole classification
      call at `C:2439-2447` → `TestRedeliveredToolResultIsClassifiedBeforeTheHandlerTouchesIt` (older, ahead) and
      `TestTerminalLoopAcknowledgesAToolResultWithoutEffect` (warm) red.
      **Declared degrade, metered (checkpoint-3 NIT-2).** When `GetLoop` fails between the routing lookup and this
      check — the loop was released mid-delivery — the classification is SKIPPED and the result goes to the handler
      anyway. That is safe (the handler answers the race as it did before the check existed) but it is a degrade,
      and a degrade is a declared event: the Warn line is now joined by
      `recovery_degradations_total{site="tool_result_classification"}`, because a log line is not something an
      operator can alert on. The counter drops no work and changes no delivery decision, and its Help says so.
      Test: `TestASkippedClassificationSaysSoInTheLog` asserts the line AND a delta of exactly one on that site.
      Mutant (vi): delete the counter call, keep the log. `component.go`
      `77064b5e84396c23a6fdf20a389a712e` → `ffb00040435af2668767b11d3bbfb627` → restored
      `77064b5e84396c23a6fdf20a389a712e`; RED at "a log line is not something an operator can alert on; a declared
      degrade carries both" (exit 1), green on restore (exit 0), `git status --porcelain` empty after.
- [x] 3.2 Response lane: cold → 2.5 first at `C:1698-1700`'s live arm; the warm superseded-response guard at `H:1253`
      (`CurrentRequest`, process-local and empty after replacement — L2's residual `ST:955-961`) compares against the
      record's `PublishedRequestID`; newer → Retry. Test: write `settlement_recovery_test.go` (response-lane cases).
      **Landed.** The warm superseded-response guard (`H:1274`) compares against the record's `PublishedRequestID`
      through the same `orderAgainstPublished`, and the process-local map it used to read is DELETED, not bypassed:
      `LoopManager.CurrentRequest` and the `currentRequests` map are gone from `state.go`, so the two spellings of "the
      loop's current request" cannot drift apart again. A response the record does not yet name is no longer a drop —
      the guard returns `errRequestNotYetObservable` (`H:1227`), which `handleResponseMessage` turns into a Retry
      instead of a loop failure (`C:1756`). The cold arm is step 0 then classify (`C:1855`, `C:1863`).
      **No deviation.** All four sites now give a FOREIGN request name the same disposition — Quarantine — as the
      delta requires: the warm guard returns `errResponseForeign` (`handlers.go`) and `handleResponseMessage` wraps it
      fatal, matching `classifyRedeliveredToolResult` and both cold arms. The warm APPLIED arm returns
      `errResponseSuperseded` and the component acknowledges it in place: an empty `HandlerResult` was still a result,
      and it flowed into `persistHandlerResult` → `persistLoopState`, whose compare-and-swap moved the record's
      revision for a delivery that changed nothing. The early return matches the tool lane's `if !apply { return nil }`.
      Tests: `settlement_recovery_test.go` — `TestAReplacementClassifiesAResponseAgainstTheRecordNotItsOwnMints` (four
      warm arms: superseded, not-yet-named, current, and the foreign name that is refused rather than dropped — the
      superseded arm also asserts the record's revision does not move) and `TestColdResponseIsClassifiedAgainstTheAdoptedRecord`;
      `superseded_response_test.go` still pins the metric and now pins the empty write list; `export_test.go`'s `CurrentRequestForTest` now reads the record's field, so no fixture can pass
      against a source production no longer has. The real broker is 4.2.
      Mutant: replace the guard's ordering with a constant `requestOrderCurrent` (`H:1274`) →
      `TestAReplacementClassifiesAResponseAgainstTheRecordNotItsOwnMints` (superseded, not-yet-named) and
      `TestSupersededResponseDoesNotSettleATimedOutLoop` red.
      **The quarantine found a reconstruction one package over** (CI run
      [35733719359](https://github.com/C360Studio/semstreams/actions/runs/35733719359), Test job).
      `processor/agentic-dispatch/terminal_loop_seam_test.go` births a REAL loop through `HandleTask` and then fed
      `HandleModelResponse` a response named `"seam-req"` — a value production never mints. It passed only because the
      old collapsed arm dropped-and-acked a foreign name; with the disposition corrected it quarantines, exactly as
      ruled. Fixed in the TEST: `publishedRequestID` reads the `Nats-Msg-Id` off the `agent.request.<loopID>` message
      the loop's own birth published, so the answer names the question the loop actually asked. Class swept before the
      fix: `git grep -nE 'RequestID:[[:space:]]*"[a-z-]+"' -- 'processor/**/*_test.go' 'agentic/**/*_test.go' 'test/**/*_test.go'`
      returns **55** hits, and `HandleModelResponse` has exactly ONE call site outside `processor/agentic-loop` — this
      one. (`8dc81425`'s commit body records this sweep with `\s` and a count of 56; `git grep -E` is POSIX ERE, where
      `\s` matches nothing and the command returns 0 — the POSIX class above is the one that was actually run, and 55
      is its count. The squash body must carry the corrected form, since a commit body is what reaches `main`.) The in-package literals (`delivery_owner_test.go:285`, `tool_result_handler_failure_test.go:193`) sit on
      loops built with `CreateLoop`, whose record names no request, so they meet `requestOrderUnnamed` and are fixtures
      rather than reconstructions. The 14 in-package files that both birth a loop and answer it are green under
      `-race`.
- [x] 3.4 Task lane: a cold fork before `C:1396` (`HandleTask`) — read the record by `task.LoopID`; absent → birth;
      present at iteration 0 → rebuild R1 through `buildTaskRequest` (`H:1120`) and republish it unconditionally with
      the MsgId, no retained read (Q1); present and advanced → ACK. Test: `recovery_test.go` (exists, extend) — cold
      redelivery at iteration 0 and after advance.
      **Landed.** `classifyRedeliveredTask` (`loop_classification.go:117`) is the cold fork, called before `HandleTask`
      (`C:1436`): no record → birth; a record at iteration 0 → `taskRepublishFirstRequest`; advanced or terminal →
      `taskApplied`, an Ack with an audit line naming the iteration and state; an unreadable record → a transient
      error, never a birth, because birthing on a failed read is a second loop under a name that may already have one.
      The republish arm rebuilds R1 through the ordinary birth path and takes the record's own revision as its own
      (`rememberLoopRevision`, `C:1573`), so the replacement becomes the holder without writing the record again.
      **Deviation from the task line, RATIFIED by the owner
      ([issuecomment-5776942078](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5776942078),
      2026-09-22, verbatim "1330 agree with recommendation"):** the rebuilt R1 goes out through `publishResults`,
      which consults `adoptRetainedRequest` first, so an R1 the stream ALREADY retains is adopted rather than
      published a second time. The task line says "publish it unconditionally with the MsgId, no retained read (Q1)".
      Q1's point is that a birth must not be blocked behind a retained read, and it is not — adoption is a no-op when
      nothing is retained, which is the state the ruling describes. Where the two rules meet, publishing a second copy
      of a request the stream holds under the same name is the thing 2.3 exists to prevent. Q1 is amended to the
      adopt-not-republish reading; `design.md` § Conformance carries the row.
      Test: `recovery_test.go` `TestTaskRedeliveredToAProcessWithNoMemoryOfItsLoop` — six arms: iteration zero
      republishes R1; the replacement becomes the holder at the record's revision; an advanced loop Acks without
      effect; a settled loop Acks without re-birthing; no record at all is an ordinary birth; an unreadable record
      never births a second loop.
      Mutant: replace the classify call at `C:1436` with a constant `taskBirth` → four of the six arms red.
- [x] 3.5 Terminal + unproven result (Q7): effect-free ACK with a `WarnContext` audit line; two new reason values on
      the existing `tool_results_dropped_total` (`older_request`, `terminal_unproven`; metric name `M:169`, recorder
      `M:527`, the reason values enumerated in its doc comment `M:514-526`, existing use `C:2300`) rather than a new
      counter; the warm check is inserted before `HandleToolResult` at
      `C:2195`, because `H:2546` and `H:2580` precede the lane's only terminal guard at `H:2652`. Test: write
      `terminal_tool_recovery_test.go`; both reason values asserted.
      **Landed.** Two reason values on the existing counter and no new metric: `older_request` and `terminal_unproven`
      join `stale_execution` on `tool_results_dropped_total` (Help at `M:170`, the recorder's doc comment enumerating
      all three at `M:514-527`). Each drop carries a `WarnContext` audit line naming the loop, the execution and both
      request names (`loop_classification.go:181-204`, `C:2574-2580`). Q7 is unconditional: a terminal loop can apply
      nothing.
      **Deviation, recorded:** the terminal arm does not check `PendingToolResults` membership first. Membership would
      only distinguish "already applied" from "never applied" for a loop that can no longer apply either, and Q7 rules
      both to the same effect-free Ack — the branch's two sides would do the same thing.
      Test: `terminal_tool_recovery_test.go` `TestTerminalLoopAcknowledgesAToolResultWithoutEffect`, warm
      (`terminal_unproven`) and cold (`stale_execution`), each asserting the Ack, the counted reason and an unwritten
      record; `older_request` is asserted warm and cold in `tool_result_recovery_test.go` and on a real broker in both
      W4 cases of 4.2.
- [x] 3.8 In-flight answer (the MODIFIED requirement's new SHALL NOT): a test citing that requirement (`// spec:` line;
      `git grep 'acknowledgement floor' -- '*_test.go'` is empty — add it beside the in-flight query) with a case where
      the record names `published_request_id` and a non-empty `pending_tool_results` while the process is gone and the
      answer is still consumer bookkeeping; `git grep -n 'PublishedRequestID\|published_request_id' -- ':!processor/agentic-loop' ':!agentic'`
      returns only docs/spec.
      **Landed.** `inflight_test.go` `TestInFlight_ALoudLoopRecordIsNotAnInFlightAnswer` carries
      `// spec: agentic-loop / In-flight state MUST NOT be derived from the acknowledgement floor` and puts the record
      in the loudest state it can reach — `published_request_id` set, `pending_tool_results` non-empty, no process
      holding the loop — and asserts the in-flight answer is still consumer bookkeeping. Its bucket fails the test if
      the in-flight path reads the record AT ALL (`readCountingLoopBucket`), which is the assertion that survives a
      refactor of what the record happens to contain.
      `git grep -n 'PublishedRequestID\|published_request_id' -- ':!processor/agentic-loop' ':!agentic'` returns only
      `docs/operations/migration-beta162-to-beta163.md` and this change's own `openspec/` files: the field has no
      reader outside the two packages that own it. `task spec:properties` → `spec-properties: 312/312 citations
      resolve.`
- [x] 3.10 Component-entry classification for a duplicate terminal tool result (docket OQ5, owner ruling 2026-09-22):
      the check sits at `C:2195`, and `TransitionTo`'s same-state `nil` (`AG:181`) stays untouched — it is a legitimate
      no-op for other callers. Test: a duplicate `StopLoop` result delivered to a loop whose record is `complete`
      publishes nothing and moves no record timestamp (the record is not written at all), in
      `terminal_tool_recovery_test.go`.
      **Landed** inside 3.1's component-entry check: a duplicate `StopLoop` result meets the terminal arm at `C:2440`
      before `HandleToolResult` can act on it, and `LoopEntity.TransitionTo`'s same-state `nil` (`AG:181`) is left
      alone — it is a legitimate no-op for the other callers of a state machine this lane does not own.
      Test: `terminal_tool_recovery_test.go`, warm arm — a duplicate `StopLoop` result for a loop whose record is
      `complete` Acks, counts `terminal_unproven`, writes NO record (`bucket.written()` is empty after a reset, so not
      even a timestamp moves) and publishes nothing (the fixture's client cannot publish, so any publication would have
      failed the delivery). The cold arm proves the same for a loop no process holds.

## 4. Property and window evidence

- [x] 4.1 New `processor/agentic-loop/applied_facts_property_test.go`: Rapid state machine over an in-memory KV and a
      fake evidence reader; actions deliver / crash-at-{W1,W2,W3,W4} / redeliver / replace-process on the tool and
      response lanes; checks I1–I4 after every step (I2 as membership, never rendering) and "no duplicate request
      published unless absent from the fake stream". The approval-response lane's three shapes use named examples and
      land with L4b (#1362).
      **Landed, and NARROWED in round 2 — see 8.5.** `applied_facts_property_test.go`
      `TestPropAppliedFactsHoldAcrossEveryCrashWindow`: a Rapid state machine over the recording bucket and the
      evidence-reader seam with five actions — advance the loop (its crash point is a drawn bool, so "the record write
      never landed" is an ordinary draw, with or without a tool batch), replay the mint of the current request, replace
      the process, redeliver a tool result to a cold process, redeliver a model response to a cold process — plus an
      unnamed invariant action that runs after EVERY step. What that action GENERATES, and all this task claims, is
      **I1, I3 in its derived form (`iterations == the named request's iteration − 1`), and "no request is published
      twice while the stream holds it"**. The decisions under test are production's: `adoptRetainedRequest`,
      `adoptNewerRetainedRequest`, `persistLoopState` and both cold lane arms run unchanged, and the model supplies
      only retention and crash points. 300 checks under `-race` in 11.09s.
      **I2 and I4 are NOT generated, and the two checks that claimed them were DELETED** (owner ruling 2026-09-23,
      docket question 2 — NARROW): neither could fire. The advance action drains the applied set and writes the
      advanced record with it empty, so the durable set between actions was always empty and the I2 loop never reached
      an assertion; no action creates an approval gate, so the I4 branch was never true. The `toolCalls` ledger the
      deleted I2 check was the only reader of went with them. Both invariants are carried by NAMED examples instead,
      listed in the test's own doc comment: I2 by `TestToolResultRedeliveredToAReplacementProcess`'s "W2" arm,
      `TestAReplayedAppliedToolResultDoesNotQuarantineItsLane`,
      `TestATaskRedeliveredOverAProgressedFirstBatchIsNotRepublished`, `TestARestoredToolBatchKnowsWhatIsLeftToRun`
      and `TestAColdToolResultRebuildsTheBatchItBelongsTo`; I4 by the step-0 adopt's two arms —
      `TestColdReadAdoptsTheNewestRetainedRequestFirst`'s "a pending approval gate is cleared in the same write"
      (`loop_carrier_test.go`), which pins the DURABLE record, and `TestAnAdoptedRequestClearsTheGateItAdvancedPast`
      (`loop_record_writer_test.go`), added in round 2, which pins the record the adopt RETURNS to the classification
      running next.
      **Premise correction, measured (see 8.5):** the docket recommended the new example on the ground that the
      step-0 gate clear was untested in L4a. It was not untested — the first arm above already covered it, and the
      finding-5 mutant reds BOTH. The docket's search enumerated a fixed file list that excluded
      `loop_carrier_test.go`. The example was still landed as ruled; what it adds is the returned record and a gate
      built by the production constructor, and whether it is worth keeping beside the existing arm is the owner's
      call, not a developer's.
      The approval lane's three shapes stay with L4b (#1362).
      Mutant (c) in 4.3 is what proves the property is not self-satisfying.
- [x] 4.2 Real-NATS W4 on the tool lane — the W4 case RESTARTS the process via `test/e2e/harness/processbarrier`
      (replacement, not a fresh handler) and asserts the adoption-first write — plus W2 and W4 (truncation retry) on the
      response lane; assertions read `AGENT_LOOPS` (`published_request_id`, `iterations`, `state`) and count messages
      per subject, never bodies. Files: new `tool_result_redelivery_integration_test.go`, new
      `identity_adoption_integration_test.go`. The approval-lane reject W4 and the terminal lane's
      crash-after-publish-before-`Update` are L4b's.
      **Landed** as two in-package files under the `integration` tag: `tool_result_redelivery_integration_test.go`
      (`TestToolResultRedeliveredToAReplacementProcess`) and `identity_adoption_integration_test.go`
      (`TestModelResponseRedeliveredToAReplacementProcess`), W2 and W4 on each lane.
      Every residue is built by dying where a process really dies. A real loop is born (`createLoopState` →
      `publishResults`), a real turn is taken through the handler and the real carrier, and only then does the
      component's loops bucket refuse its `Update` (`crashedBeforeRecordUpdate` — `Update` only; every read stays the
      real bucket). The publish that runs first has genuinely PubAck'd and the record write genuinely never landed, so
      no part of the durable state is written by hand. Assertions read `AGENT_LOOPS` fields (`published_request_id`,
      `iterations`, `state`, `pending_tool_results`, and the record's revision) and per-subject message counts from
      `stream.Info(WithSubjectFilter)`; no message body is compared.
      W4 on the tool lane: the record ends at `<loop>:req:2:0` with `iterations = 1` and an empty applied set, the
      delivery Acks as `older_request`, and `agent.request.<loopID>` still holds exactly two messages — the adopting
      process publishes nothing. W4 on the response lane: the truncation retry `<loop>:req:1:1` is adopted with
      `iterations` still 0 (a within-iteration retry advances nothing) and the response for `:req:1:0` Acks as
      `superseded_request`. **The W2 halves of both files are NOT this task's** — they pin a provisional settlement
      that the cold rebuild changes, so they moved under task 1.2 (split out 2026-09-22 at the checkpoint-2 review:
      a ticked task must not carry a provisional assertion).
      The task lane's own real-broker case lands here too: `task_redelivery_integration_test.go`
      (`TestTaskRedeliveredToAReplacementLeavesOneFirstRequest`) births a loop through the real task lane, hands the
      same bytes to a replacement, and asserts the delivery Acks while `agent.request.<loopID>` still holds exactly
      one message under `Nats-Msg-Id = R1`, with the record's revision unmoved. It exists because the unit arm of 3.4
      asserts only what the delivery does not do, and the `taskBirth` mutant satisfies that too; on a real stream it
      does not (re-run recorded on 3.4).
      **Deviation, recorded:** the process replacement is a second `Component` with its own `MessageHandler` over the
      same bucket and stream, not an OS process restarted through `test/e2e/harness/processbarrier`. That barrier is
      the agentic E2E tier's tool executor for holding a real app across a docker restart; inside a Go integration test
      there is no second OS process to restart, and everything the cold arms recover from — the routing maps, the
      context managers, the minted-request map — is process-local state a new `Component` genuinely does not have. The
      OS-process replacement is task 6.2's `task e2e:agentic` stage.
      `go test -race -tags=integration -count=3` over both tests: `ok … 4.198s`.
- [x] 4.3 Mutation evidence (`cp` backup + checksum, never stash): (a) restore Put-before-publish in 2.1 → W4 in 4.2
      fails; (b) restore plain `Put` → the CAS case in 2.1 fails; (c) skip adoption in 2.3 → the 4.1 property fails;
      (e) skip step 0 in 2.5 → the restarted W4 case in 4.2 retries to `MaxDeliver`. (d) and (f) are L4b's.
      **Landed.** Every mutant was taken on a committed tree: `cp` backup into the scratch directory, `md5 -q` before,
      a printed `[applied]` line between the edit and the test run, then restore, checksum match and an empty
      `git status --porcelain`. Baselines: `component.go` `d018dd58870c81f8a06e3f922505bc83`, `loop_evidence.go`
      `a34674f97e5206c77a8ebb4bafbd650a`, `handlers.go` `5a6990797e92deb80dad88fbd3aee75d`.
      (a) Delete the `publishThenWrite` dispatch in `persistHandlerResult` (`C:2122-2124`), restoring write-then-publish
      on every lane → all four cases of 4.2 red, the W4 arms at "the self-heal re-asks the same iteration under the
      next retry ordinal" and its tool-lane twin: a record write that fails FIRST means nothing is ever published, so
      the crash window cannot even form.
      (b) Replace the CAS `Update` in `persistLoopState` (`C:2881`) with a blind `Put` →
      `TestCarrierCompareAndSwapLossRetriesAndReleasesTheLoop` red ("Expected error with `kv: revision mismatch
      (concurrent update)` in chain but got nil"), and both 4.2 tests red as well — the crash double overrides `Update`
      only, so a writer that stops calling it can no longer be interrupted where a process really dies.
      (c) Neuter the adoption decision: `adoptRetainedRequest` (`loop_evidence.go:230`) returns `false, nil` →
      `TestPropAppliedFactsHoldAcrossEveryCrashWindow` red after 0 tests, `request "…:req:1:0" was published 2 times
      while the stream held it`. Its WIRING is pinned separately, because the property drives the decision directly:
      deleting the identity check from `publishResults` (`C:2609-2617`) leaves the property GREEN and turns
      `TestPublishingAMintedRequestConsultsTheRetainedIdentity` red — the second pin task 2.3's own ritual added for
      exactly this blindness, re-verified here.
      (e) Delete step 0 from both cold arms (`C:1855` and `C:2565`, each replaced by `adopted := record`) → both W4
      cases of 4.2 red with Ack (1) expected and Retry (2) observed, while the W2 cases stay green, which is the right
      asymmetry: they have nothing to adopt. Without step 0 the restarted process retries a delivery nobody will ever
      apply, to `MaxDeliver`.
      (d) and (f) are L4b's.
      Three further wiring mutants, outside 4.3's list, pin the lane classification itself and are recorded on
      tasks 3.1, 3.2 and 3.4.

## 5. Docs and spec

- [x] 5.1 Correct `docs/concepts/17-approval-flow.md:65` and `processor/agentic-loop/doc.go` restart claims to state the
      I1–I4 contract and its prerequisites (#1327–#1329).
      **Landed.** Four sites, three of them claims that were FALSE rather than merely incomplete:
      - `doc.go:224` said "replacement preserves retained deadlines". Replaced by a `# Recovery across a process
        replacement` section (`processor/agentic-loop/doc.go:226-253`) stating I1–I4, the ordering rule
        (older → effect-free ACK, newer → retry, foreign → quarantine, current → rebuild from the record plus the
        retained request), the one-region replay, and what a replacement does NOT recover.
      - `docs/concepts/17-approval-flow.md:65-69` claimed "the new process picks up the same `awaiting_approval` loop
        and waits on the same response subject" — that is #1362's cold approval branch, not L4a's. The bullet now
        separates the durable pending state from the process-local timer and names #1362.
      - `approval_sweeper.go:40-46` carried the same class one path over from the task's pins: "Restart-safe:
        … a restored loop's deadline is computed correctly on the first sweep after process restart" reads as a
        guarantee the sweeper cannot give, because it snapshots `m.loops` and nothing reads the bucket at startup.
        Rewritten to say memory-only, and to say what a rebuilt loop does get.
      - `docs/concepts/13-agentic-systems.md:216-223` gained the durable one-region fact beside the compaction
        list, agreeing with the operator version in `docs/operations/migration-beta162-to-beta163.md`
        § "A rebuilt loop's conversation is one region" (no "nothing moves" claim on either side).
      Class sweep for the same defect elsewhere. **The first sweep was VOID** (checkpoint-4 review MEDIUM-4): it was
      recorded as `git grep -n -i 'restart|replacement|survives|persist'`, which is POSIX BRE — `|` is a literal
      there, so the alternation matched nothing and the file-set query `git grep -rln
      "PendingApproval|awaiting_approval|approval_timeout" -- 'docs/**'` returned **0 files**. The `-E` form returns
      **13 files**. Re-run with `-E`, three hits in the class, all now resolved:
      - `processor/agentic-loop/approval_sweeper.go:40-46` — FIXED above.
      - `docs/concepts/27-frontier-harness-mapping.md:117` — "The loop is a Lifecycle Participant (ADR-049):
        **current-state restart hydration**, …". FALSE, and the same claim class: there is no hydration anywhere in
        `agentic-loop`, and the package does not import `pkg/lifecycle` at all (`git grep -n 'pkg/lifecycle' --
        processor/agentic-loop/` returns two comment lines and no import). CORRECTED to say durable state in
        `AGENT_LOOPS` plus on-demand recovery when a delivery names the loop, and that nothing scans the bucket at
        boot.
      - `docs/operations/migration-beta24-to-beta25.md:155-162` ("An expired loop in KV at restart will auto-reject
        within `approvalSweepInterval` of the new process booting"). SHIPPED release note for a past release, left
        as written; the beta162→beta163 note's #1330 section names and supersedes it (task 5.3).
      One further hit read and left as CORRECT: `docs/operations/migration-beta19.md:76-77` ("a process restart
      mid-approval still remembers what's pending") — the record does remember; it is the timer that does not, which
      is the distinction 5.3's migration section draws.
- [x] 5.2 Apply the `specs/agentic-loop/spec.md` and `specs/agentic-dispatch/spec.md` deltas;
      `openspec validate agentic-loop-durable-applied-facts --strict` green; `task spec:properties` resolves the `// spec:` citation from 4.1 against the ADDED requirement.
      **Landed.** Both deltas are in the tree: `specs/agentic-loop/spec.md` (one ADDED requirement with seven
      scenarios, one MODIFIED requirement restated in full) and `specs/agentic-dispatch/spec.md` (the OQ4
      requirement, landed `0e327a51`).
      `openspec validate agentic-loop-durable-applied-facts --strict` → `Change 'agentic-loop-durable-applied-facts'
      is valid`, exit 0.
      `task spec:properties` → `323/323 citations resolve.`, exit 0, when the deltas landed. The denominator moves
      by exactly the citations the later tasks' test files add, because the script counts TRACKED test files only:
      **324/324** with task 5.3's, **325/325** with task 5.4's (checkpoint 4's head `2999e9f6`), **326/326** with
      task 5.5's arm (this round). Each re-measured, never extrapolated. Fifteen files carry
      `// spec: agentic-loop / The loop record names its outstanding request`, including 4.1's
      `applied_facts_property_test.go:102`, and every one resolves against the ADDED requirement.
- [x] 5.3 No approval-deadline hydration (docket OQ2, owner ruling 2026-09-22): the delta scenario "a replaced process
      re-arms no approval deadline; the loop stays `awaiting_approval` until answered or cancelled" plus the same
      sentence as a line in `docs/operations/migration-beta162-to-beta163.md` under a `#1330` section. Test: the zero
      is a measured delta, not a bare absence. The same test first arms a deadline in-process — a real
      `awaiting_approval` record whose deadline the live component's snapshot (`AS:69`
      `SnapshotExpiredApprovals`) reports — then starts a REPLACEMENT component over the same KV and asserts the
      replacement's snapshot is empty, the record is still `awaiting_approval`, and its revision is unchanged.
      **Landed.** Test `TestAReplacementReArmsNoApprovalDeadline`
      (`processor/agentic-loop/approval_deadline_hydration_integration_test.go:41`, `//go:build integration`, real
      NATS). It drives the REAL gate — born loop, model response dispatching one tool, a tool result whose error
      carries `agentic.ApprovalRequiredPrefix` — so `RequestedAt` and `Timeout` are a real pending approval's and
      the record is written by the real carrier. The zero is a measured delta: one instrument
      (`SnapshotExpiredApprovals`, `state.go:634`), one instant, two processes over the same bucket — 1 candidate on
      the process that gated the loop, 0 on the replacement. The instant is passed in rather than waited for, so
      there is no sleep and no backdated fixture and the configured 12h wait stays production's.
      Migration section: `docs/operations/migration-beta162-to-beta163.md:1766` § "A replaced process re-arms no
      approval deadline", which also supersedes the beta.25 note's false "Restart safety" paragraph by name.
      Delta scenario: `specs/agentic-loop/spec.md:67-72`. Conformance row OQ2 updated (`design.md` § 9).
      Mutant (`cp` backup + `md5 -q`, `[applied]` printed between mutating and testing, restore verified by
      checksum): hydrate the replacement — `initializeKVBuckets` lists `AGENT_LOOPS` after acquiring it and seats
      every non-terminal record into the LoopManager, which is exactly the startup pass OQ2 refused. `component.go`
      `77064b5e84396c23a6fdf20a389a712e` → `2333f89a51f9782b80eb857035968c8b` → restored
      `77064b5e84396c23a6fdf20a389a712e`, `git status --porcelain` empty after. RED at
      `approval_deadline_hydration_integration_test.go:66` — "a replacement re-armed a deadline its predecessor
      held", the snapshot returning the gated candidate `{… call-gated … 12h0m0s}`.
      **Finding, escalated not applied (contract § 0.2).** The requirement's free-text sentence
      (`specs/agentic-loop/spec.md:36-38`, verbatim OQ2 ruling text) says a replaced process SHALL re-arm no
      approval deadline, without qualification. Measured false in this tree: task 1.2's rebuild seats the record
      wholesale (`state.go:387-388`), including `State = awaiting_approval` and `PendingApproval`, so a replacement
      that takes a redelivered tool result or model response for a gated loop DOES hold that deadline again — at
      the record's own `RequestedAt + Timeout`. Probe, run and discarded: predecessor snapshot 1, replacement at
      start 0, replacement after redelivering the gated result 1, record still `awaiting_approval` at revision 4.
      The delta SCENARIO is scoped to startup and is exactly true; only the free-text sentence over-reaches. It is
      ruling text, so it is not edited here — the migration note and the doc comments state the narrow truth, and
      the narrowing is owed to the owner.
      **Ratified by the owner 2026-09-22** (#1330 issuecomment-5781101792, "otherwise as recommended"): the
      sentence now reads "at startup" with the rebuild clause (`specs/agentic-loop/spec.md:36-39`); design § 4 and
      the § 9 OQ2 row (now CONFORMS), `proposal.md`, and the migration section's first paragraph carry the same scope.
- [x] 5.4 `tasks_submitted_total` is at-least-once under redelivery (docket OQ4, owner ruling 2026-09-22): the new
      `specs/agentic-dispatch/spec.md` delta states it, plus one line in
      `docs/operations/migration-beta162-to-beta163.md` under the same `#1330` section; no arm change. Test: a
      dispatch-side unit test carrying `// spec: agentic-dispatch / The task submission counter is at-least-once
      under redelivery` and asserting the delta's scenario — a replayed task submission increments the counter again
      while reusing the retained LoopID and publishing no second task
      (`processor/agentic-dispatch/metrics.go:112`, `recordTaskSubmitted` `:321`, increment `:322`).
      **Landed.** Test `TestTaskSubmissionCounterIsAtLeastOnceUnderRedelivery`
      (`processor/agentic-dispatch/task_submission_counter_integration_test.go:41`). Migration section:
      `docs/operations/migration-beta162-to-beta163.md:1790` § "`tasks_submitted_total` is at-least-once, and stays
      that way", with the two reading rules an operator needs. No arm changed: `recordTaskSubmitted` is still called
      unconditionally after the publication on both lanes (`component.go:1168`, `http.go:428`). Conformance row OQ4
      updated (`design.md` § 9).
      **Overlap measured, not assumed.** The second increment is ALREADY observed on this head, inside
      `TestIntegrationPublishedTaskWithFailedResponseQuarantines`
      (`task_submission_settlement_integration_test.go:134`, "the redelivery counted one submission twice"). That
      test's GIVEN is a quarantine with no USER stream, and it cites a different requirement — the counter's
      semantics must not be contingent on that failure being present. The new test's GIVEN is the ordinary path:
      both streams exist, both deliveries acknowledge, nothing fails. It adds what the existing one does not carry —
      the OQ4 citation, the clean-ack precondition, and the derived-identity assertion against
      `stableDispatchTaskID`.
      **Integration, not unit, and why.** Both publications on this path go through the NATS client, so a component
      built without a live one fails the task publication and never reaches the counter. The only way to make it a
      unit test was a production publish seam whose sole consumer is a test, which the "before adding anything new"
      check refuses.
      Mutant (`cp` backup + `md5 -q`, `[applied]` printed between mutating and testing, restore verified by
      checksum): arm the counter to exactly-once — gate `c.metrics.recordTaskSubmitted()` at `component.go:1168`
      behind `if !found`, so a recovered submission does not count. `component.go`
      `bf2dc93c82632dc9e9ba8ded5f56dfe3` → `0949490c12ce7182b610091b55f91de4` → restored
      `bf2dc93c82632dc9e9ba8ded5f56dfe3`, `git status --porcelain` empty after.
      RED at `task_submission_counter_integration_test.go:78` — expected 2, actual 1.

## 6. Verification (before the push, every time)

- [x] 5.5 **A rebuilt loop keeps its record's deadline** (checkpoint-4 review HIGH-1; a residual recorded, not a
      ruling). `TimeoutAt` is written at birth and lives on the record, so the cold rebuild seats it with everything
      else and the warm apply's `IsTimedOut` fails the loop on the FIRST delivery whenever the replacement gap
      outran `timeout`. Three published layers said a current-naming delivery "rebuilds rather than refuses"
      without that qualification.
      **Documented, behaviour unchanged.** Refreshing the deadline on rebuild, or excluding downtime from it, would
      let a loop outlive the budget its caller set — an owner ruling, not a recovery decision.
      Landed: one sentence each in `processor/agentic-loop/doc.go` § Recovery across a process replacement and
      `docs/operations/migration-beta162-to-beta163.md` § "A rebuilt loop's conversation is one region" (with the
      operator action: size `timeout` above the replacement window); delta scenario "A rebuilt loop keeps its
      record's deadline"; conformance row in `design.md` § 9.
      Test: a third arm of `TestToolResultRedeliveredToAReplacementProcess`,
      "the replacement gap outran the loop's deadline: rebuilt, then failed". It builds the same W2 residue under a
      short loop deadline, waits past the RECORD's own `TimeoutAt` (never a duration the test picked, and never by
      rewriting the record), then delivers to the replacement.
      **Measured correction to the review's expectation.** The delivery is `Ack`, not a refusal: the loop settles
      terminally, its failure is durable, and nothing is owed. That is the sharp edge — the shape is INVISIBLE to a
      consumer-settlement or health check and shows up only on `agent.failed.<loopID>`, which is what the arm
      asserts on. `GetLoop` is likewise not the witness that the rebuild happened, because the terminal transition
      releases the loop; the failure event is, since only a loop this process HOLDS can produce one.
      Mutant (`cp` backup + `md5 -q`, `[applied]` printed between mutating and testing, restore verified by
      checksum): `HandleToolResult` stops consulting `IsTimedOut` on the way in,
      so a rebuilt loop's inherited deadline is never read. `handlers.go` `6c1eaf6d5e0a616a1b0a0626017dad17` →
      `6a4df6f4ef8326fe250eaba97a9f7179` → restored `6c1eaf6d5e0a616a1b0a0626017dad17`, `git status --porcelain`
      empty after. RED at `tool_result_redelivery_integration_test.go:452` — zero messages on
      `agent.failed.<loopID>` where one is required.
      Coordinator re-run 2026-09-22 after the disk reclaim, at `453a0f5c`, same mutant as `if false && …` on
      `handlers.go:2610` (enclosing func `HandleToolResult`): `6c1eaf6d5e0a616a1b0a0626017dad17` →
      `45f73fb14c6f945afbeafbdd41b4cc3f` → restored `6c1eaf6d5e0a616a1b0a0626017dad17`, porcelain empty. RED at
      `:452`, `Not equal` on the `agent.failed` count, 1.09s.

- [x] 6.1 `task check:push` (schema drift expected empty — `LoopEntity` is in no schema); `go run ./cmd/entity-id-audit .`
      green.
      **Ticked 2026-09-22 — see the resolution at the end of this entry. History first: `task check:push` was RED on this host, exit 201, 631s wall.** One package failed, and it failed
      on disk:
      ```
      --- FAIL: TestReleaseArtifactsReportInjectedVersions/production (19.50s)
          release_smoke_test.go:44: build release-smoke artifact: exit status 1
              /usr/local/go/pkg/tool/darwin_arm64/link: running dsymutil failed: exit status 1
              LLVM ERROR: IO failure on output stream: No space left on device
      FAIL	github.com/c360studio/semstreams/test/release	21.594s
      ```
      Everything else in the run is green: 307 packages `ok`, exactly one `FAIL` line, and every phase BEFORE the
      integration suite passed — `build`, `lint` (vet + fmt + revive + fixed-port guard + raw-Request guard),
      `go vet -tags=integration`, `go vet -tags=live_llm`, `schema:generate` + `schema:check-changes`
      (`git diff --exit-code schemas/ specs/openapi.v3.yaml` clean, as expected: `LoopEntity` is in no schema),
      `go test ./test/contract/...`, and `go test -race ./...`.
      **Not the substrate flake #1363.** `service` is `ok` in both phases of this run (`(cached)` in the race phase,
      `24.706s` in integration); `TestMetricsForwarder_TickerInterval` did not fire.
      **Cause localized, not guessed.** The same package passes ALONE: `go test -tags=integration -count=1 -run
      TestReleaseArtifactsReportInjectedVersions ./test/release/` → `ok … 26.399s`, exit 0, taken at the same disk
      level (`1.3Gi` available before and after). So the build is sound and the link is sound; what the full
      parallel suite ran out of is peak temp headroom. This is NOT recorded as green — one isolated pass does not
      make `check:push` green, and the gate stays RED until it is run on a host with room.

      **Re-run at the checkpoint-4 fix round's head (`0f964eef`): RED again, and further from green.** Exit 201.
      Every phase before the race suite passed once more — `build`, `lint`, `go vet -tags=integration`,
      `go vet -tags=live_llm`, `schema:generate` + `schema:check-changes` (clean), `go test ./test/contract/...`.
      `go test -race ./...` then died in the toolchain, not in a test: **112 `[build failed]` packages, 50 `ok`,
      and ZERO `--- FAIL` lines** — not one assertion failed.
      ```
      github.com/c360studio/semstreams/processor/agentic-detonator: mkdir /var/folders/.../T/go-build.../b862/: no space left on device
      compile: writing output: write $WORK/b794/_pkg_.a: no space left on device
      /usr/local/go/pkg/tool/darwin_arm64/link: running clang failed: exit status 1
      ld: write() failed, errno=28 (No space left on device)
      ```
      Not rerun, per the round's standing instruction. The host went from `1.3Gi` free at checkpoint 4 to **`192Mi`**
      (`df -h /System/Volumes/Data` → `460Gi size, 424Gi used, 100%`), because each package-scoped gate this round ran
      consumed more of the little that was left. The package-scoped evidence that DOES exist at this head, each exit 0:
      `task lint`; `go test -race -count=1 ./processor/agentic-loop/...` (5 packages `ok`);
      `./processor/agentic-dispatch/... ./test/contract/...` (`ok`, `ok … 19.015s`); `task spec:properties`
      → `326/326 citations resolve.`; `openspec validate --all --strict` → `56 passed, 0 failed`;
      `task api:compat:report` → exit 0 with the `processor/agentic-loop` block unchanged from the one recorded above
      (this round altered no exported surface).
      `./test/e2e/...` is the one place the disk changes the READING of a run: it reported two `[build failed]`
      packages on one pass and a different two on the next, and both pairs pass alone. A failing package set that
      moves between runs is the host, not the tree.
      **CI at `34d70975` (run 35755159080): RED on one finding the package-scoped gates could not see.**
      `test/testinfra` `TestInfrastructurePolicyGuard` reported
      `integration-time-sleep|processor/agentic-loop/tool_result_redelivery_integration_test.go|waitPastLoopDeadline|time.Sleep(remaining)|1`:
      the 5.5 arm's deadline wait was a bare `time.Sleep` in an integration file, the exact shape the guard ratchets
      out, and `test/testinfra` sits in the race suite the linker killed locally. Fixed by the coordinator: the helper
      is now `require.Eventually` on `time.Now().After(deadline + margin)` with a 5s bound — the package's established
      wait form — and the bound carries the "deadline inside this arm's budget" premise the deleted `require.Less` held.
      `go test -count=1 -run 'TestInfrastructurePolicyGuard$' ./test/testinfra/` → `ok`. `go vet -tags=integration
      ./processor/agentic-loop/` could not compile on this host (`write $WORK/b001/_pkg_.a: no space left on device`),
      so the arm's compile and run at the fixed head are CI's to prove.
      **The host.** `df -h /System/Volumes/Data` → `460Gi size, 423Gi used, 1.3Gi available, 100%`. Where it is:
      `/Users/coby/Library/Caches/go-build` 71G, `~/Library/Containers/com.docker.docker/Data` 97G (of which
      `docker system df` reports 86.48GB build cache, 9.93GB reclaimable without touching any image),
      `/Users/coby/go/pkg/mod` 14G. Reclaiming the 9.93GB of dead Docker build cache is the smallest sufficient
      action and unblocks task 6.2's tier run as well. Not done here: `.claude/skills/e2e-doctor/SKILL.md` makes
      host-wide pruning the owner's call, and this is the owner's laptop.
      `go run ./cmd/entity-id-audit .` → `entity ID audit passed: 1332 structured candidates across 1 roots`,
      exit 0.
      `task api:compat:report` → exit 0. The `processor/agentic-loop` block is exactly the shape the design's
      no-deprecation row predicts — five incompatible lines, the two truncation removals this change makes plus the
      three earlier layers left:
      ```
      --- github.com/c360studio/semstreams/processor/agentic-loop
          Incompatible changes:
          - (*LoopManager).IncrementTruncationRetry: removed
          - (*LoopManager).ResetTruncationRetry: removed
          - (*LoopManager).ResolveApprovalIfPending: changed from func(string, string) (…PendingApprovalState, bool, error) to func(string, string, string) (…PendingApprovalState, bool, error)
          - Config.LoopsBucket: removed
          - GovernanceDispatcher.HandleVerdict: changed from func(string, string, []byte) to func(string, string, VerdictPayload) (…DeliveryDecision, error)
      ```
      **Resolved 2026-09-22 (coordinator), after the owner-authorized host reclaim** (Docker images + build cache
      100.6 GB and the Go build cache 72 GB pruned; 168 GiB free after): `task check:push` on this tree at `dafdd799`
      plus the 6.2 record, `pgrep -fl e2e.test` empty before the run: **exit 0**, 316 `ok` lines, zero `--- FAIL`
      lines. `go run ./cmd/entity-id-audit .` exit 0. Every earlier red in this entry was the host, as the moving
      `[build failed]` set already said. Log: coordinator scratchpad `l4a/checkpush-dafdd799-plus-6.2.log`.
- [x] 6.2 `task e2e:agentic` with the process-replacement stage (`stage_a_process_replacement.go`,
      `process_replacement_test.go`) named in the PR body with exit codes; BREAKING for the recovery contract, so this
      tier is the gate (`docs/contributing/02-e2e-tests.md` § Breaking Changes). The approval-after-restart stage is
      L4b's.
      **Landed: tier GREEN at `dafdd799`, no-rebuild mutant RED (coordinator, 2026-09-22, after the host reclaim).**
      What the stage was missing: its three checks (completed-outcome replay, tool quarantine, dispatch quarantine)
      are all SETTLEMENT checks. None of them holds a LOOP across the replacement, which is the one claim task 4.2's
      in-process `Component` pair cannot make (recorded deviation, 4.2 above). Added
      `verifyMidFlightLoopAcrossReplacement` (`test/e2e/scenarios/agentic/stage_a_process_replacement.go:824`),
      wired as the stage's fourth check (`:69`).
      Shape, built only from knobs the tier already has — the existing `composeProcessController`, `PauseConsumer`
      (the dispatch check's own knob), `waitForStreamSubject`/`streamSubjectCount`/`waitForConsumerSettled`, and the
      `AGENT_LOOPS` bucket the approval walk already reads. No new harness package, no Dockerfile target, no env var:
      pause `agentic-model`'s request consumer, inject the tier's ordinary task, let the loop publish R1 and write
      the record naming it, kill the process, resume the consumer while nothing is running, start the replacement.
      The answer to the retained request then arrives at a process with no memory of the loop.
      Assertions, all on durable state: the record's revision MOVED, it names `<loopID>:req:2:0` (so the loop
      advanced an iteration rather than being rewritten), `iterations >= 1`, exactly TWO messages on
      `agent.request.<loopID>` (the retained first and one next), the terminal is on `agent.complete.<loopID>`, the
      response lane settled, and `agentic-loop` is still healthy.
      **THREE refusal shapes, all distinguished** (checkpoint-4 review HIGH-2): (1) quarantine → the health
      assertion; (2) retry to MaxDeliver → the settlement assertion, which now requires the ack floor to have
      PASSED this delivery (`responseBaseline.AckFloor.Consumer + 1`) — `wantAckFloor = 0` was vacuous, satisfied by
      any consumer that had ever acked anything, including one retrying this delivery to death; (3) **rebuilt, then
      failed on the inherited deadline** (task 5.5) → the wait is on `agent.complete`, never on "a terminal", and
      the failure path names `agent.failed` explicitly when it is the one that landed. Shape 3 is the subtle one:
      it ACKNOWLEDGES the delivery and leaves the component healthy, so shapes 1 and 2 both read clean.
      **The tier raced its own loop timeout** (HIGH-2). `configs/agentic.json` had `agentic-loop.timeout = "30s"`
      against a replacement window of kill + `up --wait` + health settle, 10-25s on a warm host and more on a cold
      one — so the stage was likely red on its own tier for a reason unrelated to disk, failing after 90s with a
      message pointing at the recovery. Raised to `"180s"`; JSON carries no comments, so the reason is recorded in
      `taskfiles/e2e/agentic.yml` beside the tier description. The stage also asserts the PREMISE now: it reads the
      record's `TimeoutAt` after the replacement and fails loudly naming the budget and the file to change, rather
      than timing out on a message that was never going to come.
      **Why the run is blocked, measured 2026-09-22.** `task e2e:agentic` → exit 201, 31s wall. The SemStreams
      container exits 1 during boot, before any stage runs:
      `Boot phase failed … boot_stage="stream-provisioning" error="ensure streams: create stream LOGS: create
      stream: nats: API error: code=500 err_code=10047 description=insufficient storage resources available"`.
      Not the change: this is the host's Docker disk. `/jsz` on the tier's own NATS reports
      `config.max_storage = 1085048832` (NATS auto-sizes it from free disk) against
      `reserved_storage = 1061158912` already taken by the five file streams created before LOGS; LOGS asks for
      100 MiB (`config/streams.go:115`) and 1061158912 + 104857600 > 1085048832. `docker run --rm alpine df -h /`
      → `102.1G size, 95.5G used, 1.3G available, 99%`; `docker system df` → Build Cache 86.48GB (9.93GB
      reclaimable), Images 15.88GB (14.28GB reclaimable).
      **Not remedied here, deliberately.** `.claude/skills/e2e-doctor/SKILL.md` § "Reclaim only the identified run":
      "Host-wide builder/image pruning is not a routine preflight step … removing another project's image requires
      its owner's explicit authorization." Only the agentic stack this session started was torn down
      (`docker compose -f docker/compose/agentic.yml down -v`; `docker compose ls` and `docker ps` both empty
      after). The owner's one-command remedy is `docker builder prune -f` (9.93GB reclaimable, images untouched),
      after which this tier run is owed.
      **Mutant OWED, not run.** The intended record — short-circuit `restoreLoopFromEvidence` to a Retry and watch
      this stage go red — needs a tier run, so it is UNVERIFIED rather than recorded. It lands with the run above.
      Note for whoever runs it: `task e2e:agentic` depends on `e2e:clean`, which tears down every compose stack on
      the host (`Taskfile` → `e2e:clean` lines visible at the head of the run log). Nothing was running on this
      host, so nothing was lost; the "never run `e2e:clean`" rule cannot be honoured while running this tier.
      **Tier run at `dafdd799`** (Docker images + build cache and the Go build cache pruned first, image built cold):
      `pgrep -fl e2e.test` printed nothing, `docker compose ls` listed no stacks; `task e2e:agentic` exit 0,
      `Scenario completed successfully duration=2m11.212145125s`, `assertions_run=15`, no `level=ERROR` line;
      `verify-stage-a-process-replacement_duration_ms:85043`, `midflight_record_revision_delta:3`,
      `midflight_requests_published:2`, `replacement_user_responses:1`. Log: coordinator scratchpad
      `l4a/tier-dafdd799.log`.
      **Mutant** (`cp` backup + `md5 -q`, `[applied]` printed between mutating and testing, `go vet` gate before
      the tier, restore verified by checksum): `restoreLoopFromEvidence` returns Transient unconditionally, so the
      replacement never rebuilds and the redelivered response retries as it did before L4a. `loop_evidence.go`
      `d30e4845f7345c7e09981861d65cb8d9` → `2468a3feb0b2336a4d38da55beaf4171` → restored
      `d30e4845f7345c7e09981861d65cb8d9`, porcelain empty. Tier exit 201, `level=ERROR msg="Scenario completed with
      failure" error="verify-stage-a-process-replacement failed: mid-flight loop: replacement did not carry the
      mid-flight loop to a terminal: subject agent.complete.<loopID> was not stored within 1m30s"` — the stage's own
      assertion, not a build or boot failure. A first attempt at this mutant did NOT compile (`errs.WrapTransient`
      takes four arguments) and its tier exit 201 was a build failure — vacuous, discarded, and the reason the
      `go vet` gate now precedes the tier in the ritual. Log: `l4a/tier-dafdd799-mutant-no-rebuild.log`.

## 7. Owner Codex round (2026-09-22)

The owner's round on PR #1361 at `952d7eacf0c9f559d541c4d7515a883a873fd9f4`
([issuecomment-5783708903](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5783708903)) requested
changes on findings 1-6 and raised four more. **All ten are answered here.** Eight landed first, at `3f76e16b`;
findings 3 and 4 were held on owner rulings and landed after them, once the owner ruled on 2026-09-23
([issuecomment-5790258247](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5790258247), verbatim
"As recommended on all eight") — Q1 puts the stamp-after-PubAck reorder in L4a, Q2/Q3 make the deferred turn a
documented limitation plus a two-line clear-on-rebuild, and Q8 rides Q2's sentence with the task prompt. The durable
turn and task-prompt fields are #1365.

Every code fix carries a regression proven red WITHOUT the fix through the `cp` backup + `md5 -q` ritual, with
`[applied]` printed between mutating and testing and the restore verified by checksum (`git status --porcelain`
empty after each). `go vet` precedes every tier run, so a non-compiling mutant cannot pass as a red tier.

- [x] 7.6 **Finding 6 — `Iterations == 0` alone is not an untouched birth.** A loop advances its iteration only when a
      whole tool batch is in, so the ENTIRE first batch runs at zero while its applied set fills. The republish arm
      read the ordinal alone, seated a fresh loop with no batch over a record that carries one, and the sibling result
      then had no execution to route to: the rebuild was refused over the seat and the delivery retried to MaxDeliver.
      The delta scenario already required the other half ("an empty `pending_tool_results`"); the code dropped it.
      Landed: `loop_classification.go:157`, commit `732d0d0e`.
      Test: `task_redelivery_integration_test.go`, `TestATaskRedeliveredOverAProgressedFirstBatchIsNotRepublished` —
      a real birth, a real two-call batch and a real apply of the first result build the residue, then the ORIGINAL
      task is redelivered to a replacement.
      Mutant: the `PendingToolResults` clause removed. `loop_classification.go`
      `84b2db9831de008fd1b5927ee24c09a0` → `95628e902ca939667a7d5b4a76f55634` → restored
      `84b2db9831de008fd1b5927ee24c09a0`. RED at `task_redelivery_integration_test.go:187` — "An error is expected
      but got nil. the redelivery seated a fresh loop over a record that carries a running batch; the sibling result
      now has no execution to route to and the rebuild is refused over the seat".

- [x] 7.1 **Finding 1 — a rebuilt loop dispatched without its task enforcement metadata.** `dispatchToolCall` stamps
      ADR-067's `DispatchEnforcedMetadataKeys` (read-only filesystem policy, scratch exemptions, decide action
      allowlist) from the loop's cached task metadata, written once at birth. The rebuild restored the four
      request-side caches and not that one, and neither consumer fails closed on an absent key: the bash executor
      reads `""` as the workspace-write default and decide permits any action with no allowlist. A recovered
      read-only task was silently writable.
      Landed: `state.go:445` — a defensive copy of the RECORD's `Metadata` (the request never carried it), commit
      `754dca63`.
      Test: `rebuild_enforcement_metadata_integration_test.go`,
      `TestARebuiltLoopDispatchesWithItsTaskEnforcementMetadata` — assertions read the EMITTED `ToolCall` off the
      stream, never the cache, because the executors enforce against the call. Both recovery entries into dispatch
      are exercised: the cold response lane, and the cold tool lane whose apply releases a queued sibling. The
      fixture's key set is checked against `agentic.DispatchEnforcedMetadataKeys` itself, so a key added to the
      contract joins the test instead of escaping it.
      Mutant: the restoring lines deleted. `state.go` `662227b8be405f4d66240159fc857694` →
      `c2b542a6486068971376cf2cb0fbc40b` → restored `662227b8be405f4d66240159fc857694`. RED in BOTH arms at
      `rebuild_enforcement_metadata_integration_test.go:73` — "the rebuilt loop dispatched a read-only task's call
      with no filesystem policy; the bash executor reads an absent policy as permissive".

- [x] 7.2 **Finding 2 — a refused birth write left a warm loop and the retry ACKed without R1.** A non-conflict
      `Create` failure returned transient without releasing the loop birth had just built. The redelivery found it
      warm, so the cold classification never ran, `HandleTask` answered with its task-id dedup, and the lane
      acknowledged a task that had never issued a request: no record, nothing retained, nobody owed it. The
      key-exists arm above and the publish-failure arm below both already release.
      Landed: `component.go:1601`, commit `680ccbff`.
      Test: `task_redelivery_integration_test.go`, `TestABirthWhoseRecordWriteFailedIsFinishedByItsRetry` — a real
      store whose first `Create` is refused, then healed, with the redelivery going to the SAME process (a
      replacement would have met the cold fork regardless). The mechanism is asserted without stopping the run, so
      the held loop and the unfinished birth show up as one fact.
      Mutant: the release call deleted from the non-conflict arm. `component.go`
      `bd617f4be47c6bb46f55bd268c067f6a` → `871b7978c2c4ee44dda9ee19a9bea5ea` → restored
      `bd617f4be47c6bb46f55bd268c067f6a`. RED at `task_redelivery_integration_test.go:289` (the warm loop is still
      held) and `:303` — "the acknowledged birth must have put its first request on the stream", expected `0x1`,
      actual `0x0`.

- [x] 7.5 **Finding 5 — a replay of an applied tool result quarantined the lane.** A lost ACK is ordinary
      at-least-once delivery, and ordering cannot settle it: the batch belongs to the request the record still names.
      The cold arm therefore rebuilt, `restoreToolBatch` deliberately leaves applied executions unrouted, the lane
      found no route and returned Fatal, and a routine redelivery Terminated the tool lane with the batch's
      unfinished sibling stranded behind a seated loop.
      Landed: the membership check BEFORE any rebuild, `component.go:2683`, counting a third reason value
      `already_applied` at `:2688` — enumerated in the metric's Help (`metrics.go:173`) and the recorder's doc
      comment (`:538`) beside the other three — with a `WarnContext` naming the loop and the execution. The terminal
      arm stays membership-free, as owner ruling Q7 requires. Commit `58317c26`.
      Test: `tool_result_redelivery_integration_test.go`,
      `TestAReplayedAppliedToolResultDoesNotQuarantineItsLane` — the applied result is replayed to a replacement and
      must Ack with the counted reason and an untouched record, after which the sibling completes the batch and mints
      the loop's second request.
      Mutant: the membership check deleted from the `requestOrderCurrent` arm. `component.go`
      `fa89de94644214b792ca70b3f304956f` → `49f37f530e5ce0713ca783d6b1bc674b` → restored
      `fa89de94644214b792ca70b3f304956f`. RED at the decision assertion — expected `0x1` (Ack), actual `0x4`
      (Quarantine; the parenthetical read "Terminate" until 8.3 re-read the enum —
      `natsclient/delivery_settlement.go:19-29` is Invalid 0, Ack 1, Retry 2, Terminate 3, Quarantine 4, and the
      finding's own prose says quarantine) — "a lost ACK is ordinary at-least-once delivery; terminating it
      quarantines the tool lane".

- [x] 7.8 **Finding 8 — the active deltas contradicted the Q1 amendment and their own test.** Documentation only; no
      accepted runtime behaviour changed. The loop scenario is now "adopts or publishes the first request"; `design.md`
      § 1's Q1 row, § 3.6 and § 5.1 step 3 no longer say "unconditionally / no evidence read" and cite
      [issuecomment-5776942078](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5776942078); the
      dispatch delta says no second LOGICAL task is minted and the retained task is republished under the same
      `TaskID`, matching `task_submission_counter_integration_test.go:86`.
      The delta also gained the two behaviours this round's regressions assert and it did not state — the task
      redelivered over a progressed first batch, and the replay of an already-applied tool result — with the replay
      clause added to the requirement prose beside the ordering rules, since ordering is exactly what cannot decide
      it. `openspec validate --all --strict` stays 56/56. Commit `66ac92f2`.

- [x] 7.9 **Finding 9 — the migration guide recommended actions that cannot settle a cold parked loop.** A cold
      `ApprovalResponse` finds no pending approval, is stale-dropped, and is acknowledged without touching
      `AGENT_LOOPS` (`approval_response_handler.go` `ResolveApprovalIfPending` → `staleDrop` → Ack); a cold `cancel`
      against a live record returns `ErrLoopNotFound` through `settleUncancellableLoop`, whose stale arm does not
      apply to a live record, so it retries to `MaxDeliver`. The note now says both actions need the loop present in
      process memory, that a cold parked loop is NOT settleable in beta.163, and that the cold approval branch is
      #1362 — with the one action that does help (size `timeout` above the replacement window).
      `docs/operations/migration-beta162-to-beta163.md:1794`, commit `0c2700c2`.

- [x] 7.7 **Finding 7 — prefix stripping also removes a configured system prompt that looks like framing.**
      DOCUMENTED under the standing simplicity rule rather than hardened: the reach is one system message, in the
      framing slot, on the cold path only, and the honest adopter fact is cheaper than a peel that guesses intent.
      `[Iteration Budget]` and `[Working list` are named as RESERVED prefixes in `doc.go:247` § Recovery across a
      process replacement and in the migration note's framing paragraph (`:1766`). No code change; the existing
      lookalike arm of `loop_rebuild_test.go` already covers a message further into the conversation. Commit
      `1f1cd870`.

- [x] 7.10 **Finding 10 — the E2E stage could kill before the task's ACK and pass warm.** Stage A observed the first
      request and the record and then killed; both are observable before the task's own acknowledgement, so the task
      redelivery could reach the replacement first, rebuild the loop from the TASK, and let the model response run
      WARM — the cold-response reconstruction the check exists for never running, and the recorded no-rebuild mutant
      going green on that schedule.
      Landed: the kill now waits on this delivery's settlement on agentic-loop's `agent.task` consumer against the
      floor observed BEFORE the task was published (a floor of zero is vacuous), while the model consumer is still
      paused, so the arranged window is unchanged. The consumer is resolved off the server's consumer list by its
      filter subject and exactly one match is required, rather than guessing the framework's naming pattern — a
      guessed name resolving elsewhere would make the wait pass vacuously. Handle setup moved into
      `openMidFlightHandles` to stay inside the function-length budget.
      `test/e2e/scenarios/agentic/stage_a_process_replacement.go:980` and `:773`, commit `3f76e16b`.
      **Tier run at `3f76e16b`:** `pgrep -fl e2e.test` printed nothing and `docker compose ls` listed no stacks
      before the run; `task e2e:agentic` exit 0, `Scenario completed successfully duration=2m11.032901791s`,
      `assertions_run=15`, no `level=ERROR` line; `verify-stage-a-process-replacement_duration_ms:84971`,
      `midflight_record_revision_delta:3`, `midflight_requests_published:2`. Log: coordinator scratchpad
      `l4a/tier-3f76e16b.log`. (`task e2e:agentic` depends on `e2e:clean`, which tears down every compose stack on
      the host; nothing was running, so nothing was lost.)
      **No-rebuild mutant re-run at the same commit** (`go vet ./processor/agentic-loop/` and
      `go vet -tags=e2e ./test/e2e/...` both exit 0 before the tier, so the mutant is a compiling one):
      `restoreLoopFromEvidence`'s whole body replaced by an unconditional `errs.WrapTransient`, so the replacement
      never rebuilds. `loop_evidence.go` `d30e4845f7345c7e09981861d65cb8d9` → `179176f0fafefa22de2893d76e9ff9b2` →
      restored `d30e4845f7345c7e09981861d65cb8d9`, porcelain empty. Tier exit 201,
      `level=ERROR msg="Scenario completed with failure" error="verify-stage-a-process-replacement failed:
      mid-flight loop: replacement did not carry the mid-flight loop to a terminal: subject agent.complete.<loopID>
      was not stored within 1m30s"` — the stage's own assertion, now under a forced schedule rather than an observed
      one. Log: `l4a/tier-3f76e16b-mutant-no-rebuild.log`.

- [x] 7.3 **Finding 3 — a sibling lane can commit `PublishedRequestID = R2` before R2 is published.** Ruled into L4a
      by the owner on 2026-09-23 ([issuecomment-5790258247](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5790258247),
      "as recommended on all eight", Q1): the MEMORY stamp moves from the mint to the carrier, `TrackRequest` stays
      at the mint, birth untouched.
      The name was set on the SHARED entity the moment the request was built, so every other writer of that loop
      could commit it — a deferred continuation's record write on the task lane, a tool lane's compare-and-swap —
      while the stream still retained only R1. `MaxAckPending=1` is per consumer and `loopRecordMu` serializes the
      record WRITERS, not the handler mutations that precede them, so nothing ordered the two.
      Landed: `component.go:3029` (`stampPublishedRequest`, under `loopRecordMu`) with its selector
      `component.go:2982` (`mintedRequestID`, reading the identity off `PublishedMessage.MsgID` — the field
      `publishResults` already reads — rather than a second spelling on `HandlerResult`); call sites
      `component.go:2255` (after `publishResults` PubAcks, before `persistResultState`) and `component.go:2202` (the
      write-first branch, whose order is unchanged); the two iteration mint sites in `handlers.go`
      (`publishIterationRequest`, `emitRetryRequest`) no longer call `SetPublishedRequest`; birth still does at
      `handlers.go:1168`. Commit `99cef493`.
      **Hole class enumerated before claiming the guard:** every `persistHandlerResult` call site was resolved, not
      just the publish-first ones. `approval_response_handler.go:208` settles with `writeThenPublish`, and the
      approval-REJECTION path reaches `publishIterationRequest` through `handleRejectedApproval` →
      `HandleToolResult` → `handleToolsComplete`; a carrier that stamped only on the publish-first order would have
      left that lane's record naming the previous request forever, and every response to the new one refused as
      not-yet-named. The stamp is therefore at the carrier for both orders.
      Test: `loop_record_writer_test.go`, `TestARecordNeverNamesARequestBeforeItsPubAck` — the carrier is held
      inside its own publication, on the evidence read `publishResults` runs before it sends a minted request, and
      the SIBLING lane commits the loop's record while it waits. Releasing the gate makes the request retained,
      which is what a PubAck does; the record must then name it, and both lanes' writes must have committed.
      Mutant: `stampPublishedRequest` moved BEFORE `publishResults`, today's order. `component.go`
      `dcff6f256a9fa0877971c12af7f336bf` → `610600dc0352af455bb0c4cfb89fbc79` → restored
      `dcff6f256a9fa0877971c12af7f336bf`, `go vet` 0 with the mutant applied, porcelain empty after. RED at
      `loop_record_writer_test.go:322` — "Not equal: expected `<loop>:req:1:0`, actual `<loop>:req:2:0` … a sibling
      lane committed a record naming a request whose PubAck has not landed: KV now names a request the stream does
      not retain, which is the state I1 declares impossible and which every later cold read answers with
      Quarantine".
      Twelve handler-only fixtures drove more than one model turn with no carrier to advance the record's name and
      went red on `errRequestNotYetObservable`; each now takes the one carrier step through `CarrierStampForTest`,
      which selects the request with the PRODUCTION selector.
      Claim sweep (the claim, not the line): `agentic/state.go:68` ("set at birth and by every request-minting
      transition") and `state.go:1116` (`SetPublishedRequest`'s "called at each of the three mint sites") were both
      FALSE and are amended in the same commit; `design.md` § 3 item 4 is amended with the ruling cited; `tasks.md`
      2.4 carries a "superseded in part by 7.3" line. `doc.go`'s I1 text, the delta's I1 bullet and the migration
      note's I1 paragraph all state the INVARIANT, never where the field is set, and are true as written — the
      reorder is what makes them true by construction.

- [x] 7.4 **Finding 4 — an acknowledged deferred turn is lost across a replacement.** Ruled a DOCUMENTED LIMITATION
      for beta.163 plus the two-line clear-on-rebuild (owner, 2026-09-23, Q2; Q3 read with it — #1146's acceptance
      for L3's continuation marker promises the MARKER survives a replacement, not the turn's text; Q8 rides the
      same sentence). The durable-turn and durable-task-prompt fields are an exported-surface addition to a Tier 1
      package and are their own issue, see #1365.
      `PendingContinuation` is a marker: the turn's TEXT went into the predecessor's context manager and
      `PendingContinuationRequestID` is empty precisely because no request carried it. Seated wholesale,
      `HasPendingContinuation` was true on a loop with nothing new to say, and the next completion spent an
      iteration re-asking the model with a context that had gained nothing before settling anyway.
      Landed: `state.go:409` — clear plus a `WarnContext` naming the loop, its published request and its iteration;
      `restoreLoopFromRequest` takes `ctx` for it. A non-empty carrier is left alone: that turn is inside a retained
      request and the replay carries it. The rebuild writes no record of its own (checked: `restoreLoopFromEvidence`
      only remembers the revision), so the cleared marker becomes durable on the carrier's next write like every
      other rebuilt field. Commit `1709421e`.
      Docs, one sentence family at three homes: `doc.go:257` § "Recovery across a process replacement";
      `docs/operations/migration-beta162-to-beta163.md:1777`, beside the "deliberately NOT replayed" framing; and
      the delta, as scenario `specs/agentic-loop/spec.md:132` plus a normative sentence at `:39-42`. **Q8 rides it**
      in all three: a rebuilt loop publishes `LoopCompletedEvent.Prompt` and `LoopFailedEvent.Prompt` EMPTY and
      `recoverEmptyContext` falls back to its placeholder, with the durable field filed as #1365.
      Test: `loop_rebuild_test.go`, `TestARebuiltLoopDoesNotReAskForATurnItCannotRecover` — the rebuilt marker is
      cleared, the completion settles without minting, and (the owner's anti-gaming condition on a documented
      limitation) the SAME arm asserts the empty `Prompt` on the rebuilt loop's completion event. The three are
      `assert` rather than `require` so one run reports every consequence.
      Mutant: the clear and its warning removed from `restoreLoopFromRequest`. `state.go`
      `b6903aadeaf28e927ab871a52aca7d1f` → `10d2437953d1183fca848619e4fbce3e` → restored
      `b6903aadeaf28e927ab871a52aca7d1f`, `go vet` 0 with the mutant applied, porcelain empty after. RED at
      `loop_rebuild_test.go:436` — "the rebuilt loop still claims a deferred turn whose text died with the
      predecessor; the next completion will spend an iteration re-asking the model with nothing new" — and, in the
      same run, `:446` "Should be empty, but was [4b7d2e91-…:req:4:0] … the rebuilt loop minted another request to
      re-ask a turn it does not have", `:448` "with nothing carryable deferred the completion must settle the loop",
      `:450` "a settling completion builds its terminal record". The phantom iteration is the `:req:4:0` in that
      output.

### 7.11 Gates for the round, exit codes verbatim

Re-run at `1709421e`, the GATED pair's last content commit, plus this records commit, which changes markdown only.
(The first eight findings were gated at `3f76e16b` with the same table and the same results; findings 3 and 4 changed
the carrier and the rebuild, so every row was re-run rather than carried forward.) Logs in the coordinator scratchpad
`l4a/` unless named otherwise.

| Gate | Exit | Result |
|---|---|---|
| `task lint` | 0 | vet, fmt, pinned revive, fixed-port guard, raw-Request guard |
| `go test -race -count=1 ./processor/agentic-loop/... ./processor/agentic-dispatch/... ./test/contract/... ./test/e2e/scenarios/agentic/...` | 0 | 8 packages ok, no race |
| `go test -race -count=1 -tags=integration -p 2 ./processor/agentic-loop/` | 0 | the package's own real-NATS arms, run after each of the two fixes |
| `openspec validate --all --strict` | 0 | 56 passed, 0 failed (56 items) |
| `task spec:properties` | 0 | 332/332 citations resolve — 330 before this round, +2, exactly the two new regressions |
| `git diff --check b7ce8727` | 0 | no whitespace defect |
| `task api:compat:report` | 0 | compared 62, clean 47, incompatible 15, removed 0, added 0 — IDENTICAL totals to the runs at `952d7eac` and `3f76e16b`, and the `processor/agentic-loop` block diffs clean against the owner-round log. This round adds no exported surface: the stamp and its selector are unexported and `CarrierStampForTest` is in `export_test.go` |
| `task e2e:agentic` | 0 | `assertions_run=15`, no `level=ERROR`; log `l4a/tier-1709421e.log` |
| `task e2e:agentic` with the no-rebuild mutant | 201 | `level=ERROR msg="Scenario completed with failure" error="verify-stage-a-process-replacement failed: mid-flight loop: replacement did not carry the mid-flight loop to a terminal: subject agent.complete.9bd9ef51-6d75-4e77-ae76-f8904cae0551 was not stored within 1m30s"`, `assertions_run=9`. `restoreLoopFromEvidence`'s whole body replaced with an unconditional `errs.WrapTransient`; `go vet ./processor/agentic-loop/` and `go vet -tags=e2e ./test/e2e/...` both 0 with it applied. `loop_evidence.go` `e56941d1783bc386c74de88742e946a6` → `bf1526e35d6db96b218fc39d2605aca1` → restored `e56941d1783bc386c74de88742e946a6`, porcelain empty. Log `l4a/tier-1709421e-mutant-no-rebuild.log` |
| `task check:push` | 0 | build, lint, tagged vet, schema drift, contract, race unit, then integration through the canonical runner and its host lock. Log `l4a/checkpush-gated-pair.log` |

`pgrep -fl e2e.test` and `docker compose ls` were both empty before each tier run and after the mutant restore.

## 8. Owner Codex round 2 (2026-09-23)

The owner's second round on PR #1361 at `c8c1f1bc`
([issuecomment-5790425046](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5790425046)) requested
changes on findings 1-4 and raised a fifth. **Three landed here: 8.3, 8.4 and 8.2, in that order.** Findings 1 and 5
are GATED on owner rulings and are untouched — 8.1 and 8.5 below name the question each is waiting on.

Every code fix carries a regression proven red WITHOUT it through the `cp` backup + `md5 -q` ritual, with `[applied]`
printed between mutating and testing, `go vet` exit 0 with the mutant applied so a non-compiling mutant cannot pass as
a red, and the restore verified by checksum (`git status --porcelain` empty after each). Each fix was also observed
red before it was written, against the unfixed tree.

- [x] 8.3 **Finding 3 — a first-iteration truncation retry still read as an untouched birth.** A length-truncated
      first response self-heals by re-asking the SAME iteration under the next retry ordinal: it publishes `:req:1:1`,
      deliberately leaves `Iterations` at zero, and no tool has run, so the applied set is empty. The record is
      byte-for-byte the shape the republish arm called an untouched birth. Redelivering the original task to a process
      not holding the loop therefore minted `:req:1:0` under a NEWER retained request, and `adoptRetainedRequest`
      refused the backward name as Fatal — a routine at-least-once redelivery quarantining the `agent.task` lane,
      which runs at `MaxAckPending` 1, for every task queued behind it.
      Landed: `loop_classification.go:175` — the republish arm now also requires the record to NAME the loop's first
      request (`looprequest.ID{L, 1, 0}`, minted at `:174`), which is exactly what the delta's GIVEN at
      `specs/agentic-loop/spec.md:117` has always said and the code did not read. The applied arm's audit line gains
      `published_request_id` (`component.go:1450`) so an operator can see which of the three facts settled a task
      still at iteration zero. Commit `122606ed`.
      Claim sweep (the claim, not the line): `design.md` § 5.1 steps 3 and 4, the § 1 Q1 row and § 3 item 6's
      task-lane sentence each named `Iterations == 0` and the applied set as the whole test and are amended; the two
      `taskDisposition` doc comments and the republish arm's comment are amended with them. The delta scenario is
      unchanged — it already stated the condition, which is what made this a code/delta divergence rather than a
      design gap.
      Test: `task_redelivery_integration_test.go:340`,
      `TestATaskRedeliveredAfterItsFirstIterationRetriedIsNotRepublished` — a real birth, a real length-truncated
      response through the real carrier, the ORIGINAL task redelivered to a replacement, and a last arm proving the
      loop is not stranded: the retained `:req:1:1` is answered on the response lane, cold, and settles it.
      Mutant: the name clause removed from the republish arm. `loop_classification.go`
      `98e2e3458984e82f19bf7d909833bb9a` → `48a281b07ca355312c04b59ea42e13c1` → restored
      `98e2e3458984e82f19bf7d909833bb9a`, `go vet` 0 with the mutant applied, porcelain empty after. RED at
      `task_redelivery_integration_test.go:388` — expected `0x1` (Ack), actual `0x4` (Quarantine) —
      "republishing over a retried first iteration mints :req:1:0 under a retained :req:1:1, which the cold adopt
      refuses as Fatal — quarantining the task lane over a valid redelivery".

- [x] 8.4 **Finding 4 — cold R1 reconstruction refreshed the original deadline.** The cold response and tool arms
      rebuild through `restoreLoopFromRequest`, which seats the record wholesale and inherits its timing. The task
      lane's R1 arm does not: it runs the ORDINARY `HandleTask`, whose `configureLoopMetadata` calls `SetTimeout`
      (`state.go:1356`), which stamps `StartedAt = now` and `TimeoutAt = now + budget` on the entity it just built.
      Only the durable revision was restored afterwards, so an expired record whose task redelivered before its
      response resumed on a full fresh budget — a loop outliving the budget its caller set. The owner ruled that out
      explicitly ([issuecomment-5781101792](https://github.com/C360Studio/semstreams/issues/1330#issuecomment-5781101792)),
      and a deviation from a binding ruling is blocking at any label.
      Landed: `component.go:1608` overlays the record's `StartedAt`/`TimeoutAt` onto the rebuilt loop through
      `restoreRecordedLoopDeadline` (`component.go:1678`), using the existing `GetLoop`/`UpdateLoop` seam. A record
      with no deadline overlays zero onto zero, which is what the wholesale seat gives. Neither call can fail for a
      loop `HandleTask` just built in this process; if one does, the delivery is released and returned transient
      rather than republished on a deadline the ruling forbids — the shape the two birth arms below it already take.
      Commit `73370641`.
      **Class sweep, re-run at this head:** the only writers of either field in production code are `SetTimeout`,
      reached only from `configureLoopMetadata`, reached from `HandleTask` on three paths — birth (intended), this
      arm (fixed), and a WARM continuation (`handlers.go:932` runs for `continuation == true`).
      `adoptNewerRetainedRequest`, `attachContinuation` and `agentic.NewLoopEntity` touch neither.
      **Three residuals RECORDED, none built and none filed** (owner placement, not a developer's): (a) a warm
      continuation refreshes a live loop's deadline — pre-existing on `main`, not this change's; (b) the R1 arm
      republishes `agent.created` on every redelivery, and that event carries no `Nats-Msg-Id`, so the server cannot
      collapse it — stated for adopters at `docs/operations/migration-beta162-to-beta163.md:1850`, beside the
      `tasks_submitted_total` at-least-once rule; (c) the R1 arm drops a `PendingContinuation` marker without the
      warning the ruled clear-on-rebuild gives, because it builds a fresh entity rather than seating the record —
      the turn is lost either way and the ruled limitation sentence already covers the loss.
      The delta gains the task-lane scenario at `specs/agentic-loop/spec.md:78`: the existing one at `:69` is scoped
      by its WHEN to an input naming the REQUEST the record names, and a task names the loop.
      Test: `task_redelivery_integration_test.go:446`, `TestAColdR1ReconstructionKeepsTheRecordsDeadline` — an
      expired R1 record, a cold task replay, then the R1 response. The consequence chain is `assert` rather than
      `require` so one run reports both the refreshed deadline and the completion it produces.
      Mutant: the overlay call deleted from the R1 arm. `component.go` `7aa4db3c87cc5c42e8b0a0f6bba94ec6` →
      `e364d6db202abae5891610bdebc4422e` → restored `7aa4db3c87cc5c42e8b0a0f6bba94ec6`, `go vet` 0 with the mutant
      applied, porcelain empty after. RED in six places in one run: `:491` "a rebuild is not a reprieve: the rebuilt
      loop carries the record's deadline, not a fresh budget" (expected `07:39:07.212342`, actual `07:39:08.28446`),
      `:493` the same for `StartedAt`, `:511` no `agent.failed` (expected `0x1`, actual `0x0`), `:520` "a loop handed
      a fresh budget would have completed instead" (`agent.complete` expected `0x0`, actual `0x1`), `:524` record
      state `"complete"` where `"failed"` was required, and `:525` the record's own `TimeoutAt`.

- [x] 8.2 **Finding 2 — approval-gate creation took the publication order deferred to L4b.** An `awaiting_approval`
      result is not terminal, so the carrier's non-terminal branch took the tool-result lane's `publishThenWrite`:
      the `ApprovalPendingEvent` was published before the gate was written. A crash between the two leaves a human an
      approval request with no durable gate behind it, and the replacement's approval-response handler stale-drops
      the answer to a gate it cannot find and acknowledges it — an admitted human decision silently lost, on the one
      lane whose whole purpose is a human decision. `design.md` § 5.4 and moved task 2.7 already kept the gate on
      write → publish in L4a; § 5.4's "there is no W4 here" was false as shipped.
      Landed: one clause at `component.go:2241` (`gated` at `:2237`), in the CARRIER rather than at the call site,
      because the `carrierOrder` contract is where the reader looks for which result takes which order and the
      tool-result lane asks for publish-first for every other result it produces. Commit `c0a31dff`.
      **Hole class enumerated:** `checkApprovalGate` (`handlers.go:2784`) is the only producer of an
      `awaiting_approval` `HandlerResult` in production code and is reachable only from `HandleToolResult`; the
      response lane cannot produce one. Every other result class of both lanes was re-read against design § 5.3 and
      § 5.7 and conforms.
      **Interaction with 7.3's stamp reorder, verified at this head and stated in the carrier's doc
      (`component.go:2222-2232`):** a gate result mints no request — its only publication is the
      `ApprovalPendingEvent`, built by `gateForApproval` with no `MsgID` — so `mintedRequestID` returns `""` for it
      and `stampPublishedRequest` is a no-op on this path under either order. Nothing is lost by keeping the old
      order here.
      Claim sweep: the `carrierOrder` const doc (`component.go:2184-2195`), the delta's carve-out sentence
      (`specs/agentic-loop/spec.md:28-30`) and `design.md` § 9's carrier-reorder scoping row each enumerated who
      keeps write-first and are amended with the gate. Task 2.1's own wording is superseded in part by this entry.
      Test: `loop_carrier_test.go:148`, `TestAnApprovalGateIsWrittenBeforeItsEventIsPublished` — the gating tool
      result runs through the real `handleToolResultMessage`, so what is pinned is the order the CALL SITE and the
      carrier produce together, not the carrier alone. `TestApprovalLaneKeepsWriteThenPublish` (the approval
      RESPONSE) and 7.3's `TestARecordNeverNamesARequestBeforeItsPubAck` both stay green.
      Mutant: the `!gated` clause dropped from the order selection. `component.go`
      `de593e4f1ea19b7a33c33c917a24c9a6` → `6230b5873f1c29414f7b343b1b09ba81` → restored
      `de593e4f1ea19b7a33c33c917a24c9a6`, `go vet` 0 with the mutant applied, porcelain empty after. RED at
      `loop_carrier_test.go:197` — expected `[<loopID>]`, actual `[]` — "the gate must be durable before its
      ApprovalPendingEvent is visible: a crash between the two leaves a human an approval request whose answer the
      replacement stale-drops".

- [ ] 8.1 **Finding 1 — a new cold continuation is acknowledged as an already-applied task. GATED, not touched.**
      `classifyRedeliveredTask` receives only the loop ID, so a task T2 naming a loop whose record belongs to T1 is
      answered from the record alone: advanced, it Acks as applied and T2's prompt is never applied; at iteration
      zero with an empty set it takes the republish arm and seats loop L in memory with T2's prompt as its
      conversation. The settlement is an owner question, not a developer's — refuse and acknowledge (the shape the
      warm `ErrLoopBusy` refusal already takes), or restore and attach — and the coordinator's docket carries the
      recommendation and the rejected alternatives. It is therefore left for the ruling on #1330, and the
      continuation-limitation paragraph in `doc.go` § Recovery, the migration note and the delta is deliberately
      left with room for its sentence beside the one 7.4 landed.

- [ ] 8.5 **Finding 5 — the generated I2/I4 coverage claimed in 4.1 is vacuous. GATED, not touched.** The property's
      advance action drains the applied set and overwrites the record before the invariant runs, its crash arm skips
      the earlier applied-set write, no action creates a `PendingApproval`, and the cold-tool action supplies no
      execution identity — so the I2 and I4 assertions never inspect populated state. Whether to NARROW the claim
      (delete the two checks that cannot fire, cite the named examples, and land one I4 example at the step-0 seam)
      or to BUILD the generated application step is an owner question on #1330. Task 4.1's claim stands as written
      until it is answered, and this entry is the record that it is not yet evidence.

### 8.6 Gates for the round, exit codes verbatim

Run at `c0a31dff`, the round's last code commit, plus this records commit, which changes markdown only. Logs in the
coordinator scratchpad `l4a/`.

| Gate | Exit | Result |
|---|---|---|
| `task lint` | 0 | vet, fmt, pinned revive, fixed-port guard, raw-Request guard |
| `go test -race -count=1 ./processor/agentic-loop/... ./processor/agentic-dispatch/... ./test/contract/... ./test/e2e/scenarios/agentic/...` | 0 | 8 packages ok, no race |
| `go test -race -count=1 -tags=integration -p 2 ./processor/agentic-loop/` | 0 | the package's own real-NATS arms |
| `openspec validate --all --strict` | 0 | 56 passed, 0 failed (56 items) |
| `task spec:properties` | 0 | 335/335 citations resolve — 332 before this round, +3, exactly the three new regressions |
| `git diff --check b7ce8727` | 0 | no whitespace defect |
| `task api:compat:report` | 0 | compared 62, clean 47, incompatible 15, removed 0, added 0 — the whole report is BYTE-IDENTICAL to the gated pair's log (`md5` `8b3388e949bd1eeac9b67d02ebfb924c` both), `processor/agentic-loop` block included. This round adds no exported surface: `restoreRecordedLoopDeadline` and `gated` are unexported |
| `task e2e:agentic` | 0 | `Scenario completed successfully duration=2m10.873115875s`, `assertions_run=15`, zero `level=ERROR` lines; `verify-stage-a-process-replacement_duration_ms:84886`, `midflight_record_revision_delta:3`, `midflight_requests_published:2`. Log `l4a/tier-c0a31dff.log` |
| `task e2e:agentic` with the no-rebuild mutant | 201 | `level=ERROR msg="Scenario completed with failure" error="verify-stage-a-process-replacement failed: mid-flight loop: replacement did not carry the mid-flight loop to a terminal: subject agent.complete.0306c3d1-fd65-40bc-9361-7d9437e71b09 was not stored within 1m30s"`, `assertions_run=9` — the stage's own assertion. `restoreLoopFromEvidence`'s whole body replaced with an unconditional `errs.WrapTransient`; `go vet ./processor/agentic-loop/` and `go vet -tags=e2e ./test/e2e/...` both 0 with it applied. `loop_evidence.go` `e56941d1783bc386c74de88742e946a6` → `b0ea9ec0814f7573ae49267952773d13` → restored `e56941d1783bc386c74de88742e946a6`, porcelain empty. Log `l4a/tier-c0a31dff-mutant-no-rebuild.log` |
| `task check:push` | 0 | build, lint, tagged vet, schema drift, contract, race unit, then integration through the canonical runner and its host lock. Log `l4a/checkpush-round2.log` |

`pgrep -fl e2e.test` and `docker compose ls` were both empty before each tier run and after the mutant restore.

## Moved to L4b (#1362)

Plain bullets, deliberately not checkboxes: these are #1362's tasks, listed so the reader sees exactly what left this
change and where it went. Owner ruling 2026-09-22, OQ7, plus the coordinator's scoping of the carrier reorder under
OQ7/OQ8 (`design.md` § 1); the split line is `reconciliation.md` § G.

- 3.3 Approval lane — the cold KV branch at `ARH:58` on `ErrLoopNotFound` (today `staleDrop` `ARH:78` → Ack
  `ARH:194-199`), I4 plus key presence against the record it just adopted, `republishPendingApproval` built inside it,
  and the warm re-echo at `H:2674`. Tests: `approval_timeout_recovery_test.go`, `approval_restore_order_test.go`.
- OQ1 `continuation_unavailable` — the ruled outcome for confirmed-absent approval evidence (#1146 acceptance line;
  owner ruling 2026-09-13), one branch, built with the approval lane.
- 3.6 Governance verdict after waiter loss (Q6) — `C:2734` → `settleVerdictWithoutWaiter` (`C:2759`) reads the record
  and classifies (Ack older/consumed/terminal with a reason on `M:393`; Retry current-unseen). Test:
  `verdict_wire_test.go`.
- 3.7 One terminal owner — three `Put` paths (`C:1974`, `C:1752` → `:1809` → `:1831`, `C:2631` → `:2668` → `:2683`)
  become marker `Create` (`KV:211`/`KV:218`) → stamps → publish → entity `Update` (`KV:231`), clearing
  `PendingApproval` on the terminal transition (L3's deferred item). Test: `terminal_owner_test.go` (a)/(b)/(c).
- 3.9 Route-ambiguity metering (owner ruling 2026-09-21; docket OQ6) — meter `loop_route_ambiguous` on
  `loop_admission_refusals_total` inside `activeLoop` (`processor/agentic-dispatch/http_activity.go:334`,
  `metrics.go:180`) with one resolver-seam reason value; retire the two post-refusal absence assertions in
  `command_target_resolution_test.go` (`:335`, `:349`), keep the 409 assertion at `:346-348`. Never opened here.
- 2.1 (approval lane and sweeper order) — flip the approval lane's call site (`ARH:205`) and the sweeper's own
  publish-then-`Put` pair (`AS:100-101`, D39) to publish → CAS `Update`, so the auto-reject takes the same order and
  the same CAS as an operator rejection. It moves with the lane because the reject-minted W4 the flip opens is closed
  only by the approval lane's cold branch (3.3 above). L4a changes the writer for these callers, never the order.
- 2.7 Gate order, conditional (docket OQ8) — write the test that an approval answer arriving before its gate is
  durable is Retried by the approval-response lane's cold KV branch. If it can be written, the approval gate takes
  the uniform publish → `Update` order; if it cannot, the accepted write → publish branch for the gate stands and
  #1362's PR body records which shipped and why. Test: `persist_handler_result_test.go` (gate branch) plus the lane
  case named above.
- 4.2 (approval and terminal cases) — the approval-response lane's reject W4 (crash between the carrier's publish and
  its `Update` for the approval result, restart, redeliver → ACK inapplicable, R(N+1) counted once, record `running` at
  R(N+1) with no gate) and the terminal lane's crash-after-publish-before-`Update`, in a new
  `terminal_tool_redelivery_integration_test.go`.
- 4.3 (d) and (f) — restore `Put` for the marker in 3.7 (drop the `Create` read-back) → the terminal case in 4.2 fails;
  drop the gate clear from 2.5 → the approval-lane W4 case in 4.2 fails on I4 and re-runs the rejection.
- 6.2 (approval stage) — the `task e2e:agentic` approval-after-restart stage (`approval_restart.go`,
  `approval_restart_test.go`, both absent on `main`), which asserts the answer is applied after replacement, not that a
  deadline fires.

## Delta text carried to L4b (#1362)

Not tasks. This is the delta text that was written for L4a's `agentic-loop` delta and moved out of it because it
states #1362's behaviour (tasks 3.3, 3.6, 3.7 above). It is held here verbatim so #1362 seeds its own delta from it
and nothing is lost. The header clause it replaces — "L4b is #1362, which adds no delta of its own beyond what is
stated here" — is deliberately NOT carried: #1362 carries its own delta.

```markdown
<!-- from the ADDED requirement's opening sentence: the two lanes L4b classifies -->
... and every redelivered model response, tool result, approval response, and governance verdict SHALL be classified
against that field and against `pending_tool_results` rather than against retained conversation content.

<!-- from the same requirement, after the compare-and-swap sentence -->
A redelivered terminal input SHALL adopt the loop's durable terminal by loop ID and terminal kind.
Recovery SHALL never compare rendered messages, result content, or terminal content to decide whether an input was applied.

#### Scenario: A cold replacement adopts past a rejection-minted request and acknowledges the stale approval response

- **GIVEN** a loop `awaiting_approval` at `R` whose gate was rejected, where the rejection minted and published `R(N+1)`
  and the process crashed before the record was updated
- **WHEN** the approval response is redelivered to a replacement process with no memory of the loop
- **THEN** the replacement writes the record to `R(N+1)` with the gate cleared and `state = running` under
  compare-and-swap, classifies the approval response as inapplicable, acknowledges it, and publishes nothing

#### Scenario: A governance verdict redelivered after its waiter is gone

- **GIVEN** a loop that restarted after proposing execution `e` under request `R` and later applied `e`'s result
- **WHEN** the verdict for `e` is redelivered and no waiter exists
- **THEN** the loop reads its record, finds `e` in `pending_tool_results` or `R` older than
  `published_request_id`, and acknowledges the verdict without dispatching it

#### Scenario: A redelivered terminal adopts the published terminal by identity

- **GIVEN** a loop whose durable terminal (`COMPLETE_<loopID>`) exists and whose record is not yet terminal, because the
  process crashed after publishing the terminal event and before the record update
- **WHEN** the input that produced the terminal is redelivered and this delivery derives a terminal whose content differs
- **THEN** the loop adopts the durable terminal by loop ID and terminal kind, publishes it, writes the record terminal under
  compare-and-swap, acknowledges, and logs the content difference at the audit line without retrying or quarantining
```

L4a's own delta keeps the remainder, with the compare-and-swap sentence scoped to the model-response and tool-result
lanes and loop birth, and the approval lane and approval-timeout sweeper named as keeping their present order
until #1362.
