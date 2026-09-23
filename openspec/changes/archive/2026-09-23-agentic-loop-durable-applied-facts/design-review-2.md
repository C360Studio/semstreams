# DESIGN CHANGES REQUESTED — 1 BLOCKING / 1 HIGH / 1 MEDIUM / 1 NIT (round-1: all 9 closed)

semstreams-reviewer, pre-owner design review, pass 2, read-only. All commands from `/Users/coby/Code/c360/semstreams-wt/verify-68c14c8e` (HEAD = 68c14c8e…, `git status --porcelain` = 0):
```
scripts/inventory-verify.sh <abs>/l4/change/inventory.md ; gh issue view 1330 --json comments -q '.comments[-2:][].body'
sed -n '215,280p' processor/agentic-loop/approval_response_handler.go        # the reject/approve publish→Update flow
sed -n '435,472p' processor/agentic-loop/state.go ; sed -n '2730,2756p' processor/agentic-loop/handlers.go
sed -n '31,40p' processor/agentic-loop/execution_identity.go ; grep -n 'ToolResult) Validate' agentic/tools.go + body
grep -n '^| D16 \|^| D26 \|PublishToStreamWithMsgID' <abs>/l4/change/inventory.md
```

## 1. Round-1 BLOCKING — CLOSED for every lane step 0 covers; task-lane exemption sound

§ 3.6 makes the rebuild source "always that newest retained request, never 'the request for R'", and adopts it into the record under `Update(revision)` before classifying. Walking cold W4 on the tool lane end to end: § 5.3 step 1 reads entity+revision (`C:2163`) → step 0 (task 2.5, wired at `SR:517`/`:622`/`:867`) reads `SR:63`, parses R(N+1) > R, Updates the record → § 5.3 step 2 classifies the redelivered result for R as **older** → ACK, nothing published, **no rebuild attempted**. `restoreLoopFromRequest` (`ST:349-430`) is therefore never entered with a mismatch, and on the paths that do enter it the record was just written to match, so task 1.2's guard is satisfied by construction rather than by refusal. `loopSettlementDecision`'s default Retry (`SR:1042-1051`) is unreachable on this path. Task 4.2 now RESTARTS the process via `processbarrier` rather than using a fresh handler, and 4.3(e) mutation-checks the wiring ("skip step 0 → the restarted W4 case retries to `MaxDeliver`") — a wiring mutation, not a primitive one. Closed.

**Task-lane exemption holds.** § 5.1 step 3 fires only on `Iterations == 0` AND empty `PendingToolResults`; step 4 ACKs everything else. Both of step 4's conditions genuinely mean applied: `Iterations > 0` requires `AllToolsComplete` → `handleToolsComplete` (`H:2410-2414`, `:2566`), and a non-empty applied set requires R1's response to have been handled and its tools dispatched. Birth is Put → publish (Q1), so the lane has no W4 — only W2 (record written, R1 unpublished), which step 3 answers, and W3, which step 3 answers with a republish whose duplicate is absorbed by Q5's MsgId window or L2 reuse. No window is left open by omitting step 0 here. One observation, not a finding: I1 makes that republish provably redundant when `published_request_id` is already set, which is precisely why Q5 is the mitigation — Q1 ruled the unconditional republish, so it stands.

## 2. Round-1 HIGH ×2 — both CLOSED

- **Terminal adoption.** I read #1330's last two comments: the coordinator-applied decision, then the owner's verbatim "confirmed". § 1 now carries a "Terminal outcome adoption" row citing the owner ruling with the same standing as Q1–Q8, and § 5.7's heading and § 7 item 11 match. No longer self-certified.
- **I2.** Restated as membership only — "names an ExecutionID derived from a tool call of the retained response for R … membership only", with the conversation-effect sentence demoted to prose and "recovery never checks it, and neither does the property below". Task 4.1 states "(I2 as membership, never rendering)". Spec delta `:18-19` matches. The rendering oracle is gone.

## 3. Round-1 MEDIUM ×4 and NIT ×2 — all CLOSED

MODIFIED requirement's new SHALL NOT now has a scenario (`spec.md:122-128`) and a proving task (3.8, which also records that no `// spec:` citation exists today). Metric pin corrected to the four `metrics.go` sites (`:20`, `:113`, `:304`, `:336`) — matches my grep exactly. § 3.3 gained the else branch (older/beyond/unparseable → Quarantine via `errs.WrapFatal`), with task 2.3 testing it. § 5.5 now enumerates W1/W2/W3 — but see the BLOCKING for its W4 claim. `natsclient/client.go` re-pinned to `:963`. `validatePendingApprovalEvidence` (`SR:984-1013`) added to the survives list and to task 3.3.

## 4. Inventory — EXIT 0, and both text defects fixed

`pins=158 ok=158 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0`, `changed since base (68c14c8e..HEAD): (none)`, `EXIT=0`. D16 now ends "**ruled Q6** (#1330, 2026-09-18): L4 owns a minimal ACK path with the tool lane's classification"; D26 ends "**ruled Q7** (#1330, 2026-09-18): effect-free ACK with a metric and an audit line, no retry-to-`MaxDeliver`". The § 6 search line is now `git grep -n 'PublishToStreamWithMsgID' origin/main -- natsclient   # hits client.go:946,963,968,1056 (receiver is (m *Client), decl at :963)` — a search that hits, with the receiver trap recorded.

## 5. New in round 2

`BLOCKING design.md § 5.5 (windows) with § 3.6 — "W4 does not arise: this lane mints no request" is false, and step 0 on the approval-response lane writes a record that violates I4`
- Mechanism: `handleApprovalResponseMessage` routes a rejection through `handleRejectedApproval` → `HandleToolResult` (`ARH:130-149`), which on the batch's last result reaches `handleToolsComplete` and **mints R(N+1)** (`H:2654`) into `result.PublishedMessages`. The non-terminal branch I read at `ARH:255-278` then does `publishResults` (`:269`) and only afterwards the single `loopsBucket.Update(..., revision)` (`:275`) that both clears the gate and records the advance. A crash between them leaves R(N+1) retained with the record still `awaiting_approval` and `PendingApproval.RequestID = R` — a genuine W4 on this lane. § 5's preamble runs step 0 on "every lane but task", so a cold redelivery of that approval response adopts R(N+1) first and writes `PublishedRequestID = R(N+1)` while `PendingApproval.RequestID` stays R. That breaks **I4** in the durable record, written by recovery itself; § 5.5 step 2 then "Require[s] I4" against the record it just corrupted, and task 4.1 checks I4 after every step, so the property would fail on the approval lane if the generator reaches it.
- § 3.6 lists exactly three fields the adopt writes — `PublishedRequestID`, `Iterations`, `PendingToolResults` — and says nothing about `PendingApproval` or `State`.
- Fix (either): scope step 0 out of the approval-response lane, which already classifies by gate identity at `ARH:182-215`; or make the adopt clear `PendingApproval` and set `State` whenever the adopted request is newer than `PendingApproval.RequestID`, since the advance the request proves is exactly the gate's resolution. Correct § 5.5's W4 sentence either way.
- Verification: `sed -n '215,280p' approval_response_handler.go` (publish `:269` before Update `:275`, one result covering both reject and approve); `ARH:130-149` for the synthetic result's path into `HandleToolResult`. Refutation attempted: the approve branch alone would not mint a request, but the reject/timeout branch demonstrably can, and the sweeper (`AS:156`) publishes auto-rejects onto this same lane. I did not execute the path.

`HIGH design.md § 3.6, tasks.md 2.5, specs/agentic-loop/spec.md:46-48 — the adopted applied set is derived from the request's message array, contradicting the change's own I2`
- Mechanism: step 0 writes `PendingToolResults` "keyed by the ExecutionIDs whose tool messages **the request** carries", and the new spec scenario repeats it: "`pending_tool_results` keyed by the execution IDs `R(N+1)` carries tool messages for". `deriveToolExecutionID(requestID, callID, ordinal)` (`execution_identity.go:31-40`) needs a callID and an ordinal; the only place to get them out of a request is its `Messages`, filtering role `tool` for `ToolCallID` (`buildToolMessages`, `H:2743-2749`) and taking the ordinal from position. That makes conversation layout a recovery input again — the defect `proposal.md` § Why names — and it contradicts I2 and `spec.md:18-19`, which say the ExecutionID is "derived from a tool call of the **retained response** for R".
- Fix: say response, not request. `readRetainedAgentResponse` survives (§ 6), its `Message.ToolCalls` already carry `ID` and `CallOrdinal` stamped by `stampToolExecutionCorrelation`, and that source is pure identity. Update § 3.6, task 2.5, and the spec scenario together.

`MEDIUM design.md § 3.6 vs state.go:441-443 — identity-only synthetic entries can Fatal a surviving validator`
- `validatedToolBatchResults` survives (task 3.1 deletes only `requirePreceding`) and fatals on `(stored.Name != call.Name && call.Name != "")` at `ST:441-443` → `loopSettlementDecision` → **Quarantine**, not Retry. § 3.6's entries hold "identity fields only (`RequestID`, `CallID`, `ExecutionID`, `CallOrdinal`)" — no `Name`. Task 2.5's test asserts only that the record passes `agentic/state.go:96-108`; `ToolResult.Validate` (`agentic/tools.go:646-651`) requires just `CallID`, so that assertion cannot catch this. Either carry `Name` from the source tool call, or state why the synthetic entries are never validated against a batch and add that case to 2.5's test. (A synthetic entry also cannot represent an approval-required gate status, since it has no `Error` for `IsApprovalRequired`.)

`NIT design.md § 7 — item 14 is inserted between 12 and 13.`

## Verdict

`DESIGN CHANGES REQUESTED`. Blocking list: (1) § 5.5's W4 claim and step 0's I4 violation on the approval-response lane. Every round-1 finding is closed — the cold-read BLOCKING properly, by a mechanism that removes the second reader rather than papering over it, and the owner ruling is now genuinely the owner's. The two new items are both consequences of round 2's step 0: it is the right fix, applied one lane too widely and sourced from the wrong retained message. Not owner approval.
