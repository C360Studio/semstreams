# DESIGN CHANGES REQUESTED — 1 BLOCKING / 2 HIGH / 4 MEDIUM / 2 NIT

semstreams-reviewer, **pre-owner design review** (runs only after the recorded INVENTORY PASS in `inventory-pass-2.md`, same session). Read-only. Artifacts: `l4/change/{design.md, proposal.md, tasks.md, specs/agentic-loop/spec.md}`. GitHub read: #1330's last comment (Q1–Q8, "as recommended on all eight") and #1328's scope amendment, nothing else. Neither verdict is owner approval.

Commands, all from `/Users/coby/Code/c360/semstreams-wt/verify-68c14c8e` (HEAD = 68c14c8e…, porcelain empty) except the two marked:
```
gh issue view 1330 --json comments -q '.comments[-1].body' ; gh issue view 1328 --json comments -q '.comments[].body'
grep -n '^### Requirement:' openspec/specs/agentic-loop/spec.md ; sed -n '200,230p;420,450p' openspec/specs/agentic-loop/spec.md   # in /Users/coby/Code/c360/semstreams
scripts/inventory-verify.sh <abs>/l4/change/inventory.md                          # EXIT=0, pins=147 ok=147
sed -n '131,140p' processor/agentic-loop/governance_dispatcher.go                 # VerdictPayload.LoopID
grep -rn 'func.*PublishToStreamWithMsgID' natsclient/ ; git grep -n 'PublishToStreamWithMsgID' origin/main -- natsclient/client.go
sed -n '2583,2600p;2331,2338p;204p' processor/agentic-loop/component.go ; sed -n '598,618p' processor/agentic-loop/state.go
sed -n '486,492p;572,576p;1042,1051p' processor/agentic-loop/settlement_recovery.go ; sed -n '156,160p' agentic/state.go
grep -rn 'approvalDecisionsInapplicable' processor/agentic-loop/*.go | grep -v _test.go
ls test/e2e/harness/processbarrier ; ls test/e2e/scenarios/agentic/ | grep -E 'stage_a_process_replacement|approval_restart'
```

## (a) Rulings — all eight applied as ruled, none reopened, one mechanism beyond them

Q1 → § 3.1 + § 5.1.3 + task 3.4 + spec scenario. Q2 → § 3.2 + task 2.1 + spec `:23-24`. Q3 → § 2 (no separate field) and § 5.3.2 classifies by RequestID *before* consulting the set, as ruled. Q4 → § 3.3 + § 3.5 (retry ordinal parsed from `PublishedRequestID` — the ruling's "durable input"). Q5 → task 2.2; `PublishToStreamWithMsgID` verified at `origin/main:natsclient/client.go:963`; `agent.created` duplicates accepted, nothing built. Q6 → § 5.6 + task 3.6; `VerdictPayload.LoopID` verified present at `governance_dispatcher.go:133`, so the path is implementable. Q7 → § 5.7(a) + § 5.3.1 + task 3.5 + spec scenario. Q8 → `settleAbsentApprovalEvidence` untouched in § 5.5, § 6, task 3.3. Anti-goals: `proposal.md` reproduces all seven #1146 items verbatim, and the design adds no supervisor, event-sourced log, recovery state machine, generic outbox or checkpoint bucket — one optional field plus a write reorder.

`HIGH design.md:144-157, proposal.md:5 — the terminal-payload adoption is a ninth decision attributed to a ruling #1330 does not contain`
- Mechanism: § 5.7(b) has a redelivered terminal **adopt the saved `COMPLETE_` payload, publish it, and write the record to match**, discarding this delivery's derived candidate when they differ. #1330 rules Q7 only as "effect-free ACK with a metric and an audit line, as the cancel lane does; no retry-to-MaxDeliver" — nothing about replacing a terminal payload. § 5.7 is honest about provenance ("coordinator on #1330, applying Q7 and the Q4 identity principle"), but `proposal.md:5` folds it in as "Rulings: #1330 … (Q1–Q8) + the terminal-owner ruling", and § 1's table has no row for it. This is the change's most operator-visible new behaviour: a terminal event whose content differs from what this delivery computed is published anyway.
- Not BLOCKING — it is not a deviation *from* a ruling, and deleting the compare (my round-1 BLOCKING) is right; it is an unratified extension made in the owner's name. Fix: put it to the owner as Q9 on #1330, or relabel it in § 1 and `proposal.md` as an architect recommendation pending owner. Verification: the #1330 comment body.

## (b) Crash windows — closed per lane except the cold path of the one window the change exists for

Task § 5.1: W1/W2/W3 named and closed; "no W4 at birth" is correct because Q1 keeps Put → publish. Response § 5.2 and tool § 5.3: W1–W4 all named, each closed by a durable fact (`PublishedRequestID`, a `PendingToolResults` key, TOOL_CALL_OUTCOMES replay at `TC:710-713`) or an admitted duplicate (re-proposal, `agent.created`) that appears in `proposal.md` § Declared cost. Terminal § 5.7: windows named and mapped to (a)/(b)/(c).

The `readExact` argument itself **holds**: `readRetainedAgentRequest` resolves to `GetLastMsgForSubject` on `agent.request.<loopID>` (`SR:63`, called with a loop id at `SR:517`/`:622`/`:867`); all of a loop's requests share that one subject, so in W4 the last message *is* R(N+1) and the mint-then-adopt comparison is exact and content-free.

`BLOCKING design.md § 5.3 step 3 vs § 5.3 W4, with tasks.md 1.2 — the cold W4 path has no rebuild source and fails closed into an unbounded Retry`
- Mechanism: one read serves two purposes on the same subject. § 5.3 step 3 — "Current: **cold** → rebuild from retained request R and retained response R (`ST:473-521`)". § 5.3 W4 — "`readRetainedAgentRequest` returns **R(N+1)** → adopted". Both are `GetLastMsgForSubject` on `agent.request.<loopID>`, which returns only the newest. In cold W4 (crash after R(N+1)'s PubAck, restart, tool result for R redelivered) the record says R, the input classifies as *current* — correct — and the rebuild then asks for R and receives R(N+1). `tasks.md` 1.2 makes that fail closed: "`restoreLoopFromRequest` (`:349-430`) **refuses** `entity.PublishedRequestID != request.RequestID` when the field is set". The refusal is an error → `loopSettlementDecision` (`SR:1042-1051`, verified: default → `DeliveryDecisionRetry`) → Retry → identical state next delivery → `MaxDeliver`. Cold restart in W4 is precisely what #1146 exists for and what the spec delta's first scenario asserts.
- Why the design misses it: § 5.3's W4 narrative implicitly runs warm (re-store → `AllToolsComplete` → mint → adopt) and never re-enters step 3's cold branch; nothing in § 3.3 or § 5.3 reconciles the two readers of one subject.
- Fix (smallest contract-correct): say that in cold W4 the rebuild source **is** the adopted R(N+1) — its body already carries R's conversation plus the tool messages for R's batch — and scope task 1.2's guard to `request.RequestID ∈ {PublishedRequestID, looprequest.Next(PublishedRequestID)}`; or give `readExact` a RequestID-addressed form so R stays fetchable. Either way § 5.3 must name which request the cold rebuild reads.
- Verification: `sed -n '1042,1051p' settlement_recovery.go` for the Retry default; `SR:63` plus the loop-id call signatures for the subject; `tasks.md` 1.2 for the guard. Refutation attempted and failed — `restoreToolBatch` (`ST:473-521`) takes `request`/`response` as parameters so there is no second source, and the not-observable Retry sites I verified (`SR:489`, `:575`) are revision-based and do not cover this. I did not execute the path.

## (c) Invariants — three are Rapid-checkable, I2 is not

I1 is correctly scoped to existing records ("while the record exists"), with the 24h TTL measured (`internal/loopbucket/acquire.go:20,42-43`) and the expiry consequence declared as a residual — this closes my round-1 MAJOR. I3 and I4 are pure record predicates that check cleanly after each step; I1 checks against the fake stream the property already builds.

`HIGH design.md:64-65 and specs/agentic-loop/spec.md:18-19 — I2's second clause re-imports the message-layout input the change exists to delete`
- Mechanism: I2 reads "…and its conversation effect **is exactly the tool message the request succeeding R carries for it**", and § 4 has the Rapid property "check I1–I4 after every step". To check that clause the property must render `buildToolMessages` and index the successor request's `Messages` — the same computation as `toolResultProvenInLaterRequest` (`SR:687`, `:723`, `:735`) that § 6 deletes, now as an oracle recomputing the expected value with the implementation's own algorithm. That is the test-that-reconstructs finding at property scale, and it makes message decoration (`H:2730-2756`) a restart-safety input again — the exact defect `proposal.md` § Why names.
- Fix: restate I2 as membership only ("every `pending_tool_results[e]` with `request_id == R` names a member of the retained response for R"), which is checkable from the record plus the retained response and is what the classification actually relies on; demote the conversation-effect sentence to design prose as a consequence. The spec delta carries the same clause and needs the same edit.

## (d) Deletion ledger — complete against my independent enumeration; nothing kept decides by layout

§ 6 removes every content-comparison site from my round-1 sweep: `SR:686-740` (`:687`/`:723`/`:735`), `:763-817` (`:793`/`:799`), `:821-852` (`:834`/`:843`), `:517-535`, `:622-665`, `:634-636`, and `component.go:1852-1856` + `:1861-1866` + `:1838-1839`. Every surviving `reflect.DeepEqual` compares **entity** fields, not messages — `ST:389-390`, `:536-537`, `C:1538`, `C:2178`, `ARH:212` — so nothing kept decides by message layout. `Iterations--` (`ST:486-488`) and `requirePreceding` (`:435-444`) go too, and `IncrementTruncationRetry`/`ResetTruncationRetry` are verified at `ST:601-616` as § 6 and task 2.4 claim.

`NIT design.md:171-179 — the survives-list omits validatePendingApprovalEvidence (SR:984-1013)`, which inventory D28 says "reduces to `PendingApproval.RequestID == PublishedRequestID`". `validatePendingApprovalRequest` is listed deleted and `validatePendingApprovalResult` surviving; the third is in neither list and in no task.

## (e) Spec delta

The ADDED requirement's first wrapped line carries SHALL ("The `AGENT_LOOPS` record of a non-terminal loop SHALL carry `published_request_id`…"), satisfying the `--strict` first-line rule. The MODIFIED block restates `main:openspec/specs/agentic-loop/spec.md:201-226` with the heading byte-identical (no `// spec:` citation stranded) and **both** existing scenarios reproduced verbatim — diffed against `sed -n '200,230p'` on main. Seven of the eight new scenarios map to a named task: W4 tool lane → 3.1/4.2 · stale result ACK → 3.1/3.5 · response outruns record → 3.2 · verdict after waiter loss → 3.6 · terminal + unprovable result → 3.5 · terminal adopted by identity → 3.7/4.2 · task at iteration zero → 3.4.

`MEDIUM specs/agentic-loop/spec.md:96-97 — the MODIFIED requirement adds a normative sentence with no scenario and no task.` "`published_request_id` and `pending_tool_results` are settlement facts …; they SHALL NOT be read as an in-flight answer" is new text (absent from main's `:212-213`) and is exactly the adjacent claim inventory § 5 flagged. No scenario exercises it and no task proves it, so the one clause guarding against re-deriving in-flight from the new field is unfalsifiable. Fix: one scenario (a caller asking for in-flight work gets the consumer-bookkeeping answer for a loop whose record names a published request) named by 3.6 or a new task.

## (f) tasks.md

No landing tasks: § 6 holds `task check:push` and `task e2e:agentic` only, both branch-checkable, with the PR-body naming left as an instruction rather than a tickable post-merge assertion — #1230 Option 1 honoured. Every task names a proving test except 5.1, which is docs-only. Mutation evidence (4.3) uses `cp` + checksum, prohibits stash, and its four mutations target the **wiring** (order, CAS form, adoption call, restored compare), not a primitive. The e2e files 4.2/6.2 name exist (`processbarrier.go`, `stage_a_process_replacement.go`, `approval_restart.go`).

`MEDIUM tasks.md 3.5 — the metric pin names the wrong file and understates the work.` It says "new `toolResultsInapplicable` counter beside `approvalDecisionsInapplicable` (`component.go:204`)"; `component.go:204` is a comment inside `consumerInfo`. The symbol lives at `metrics.go:20` (field), `:113` (constructor), `:304` (`RegisterCounter`), `:336` (`DefaultRegisterer`), with the only component-side use at `approval_response_handler.go:204`. A developer following the pin lands in the wrong struct, and "a new counter" is four sites in `metrics.go`, not one. Fix: re-pin.

`MEDIUM design.md § 3.3 — the identity-adoption branch has no else, and § 5.5 skips its window enumeration.` § 3.3 enumerates "RequestID == R' → adopt; absent or == current R → publish" and stops; any other retained value — the case the spec delta itself sends to quarantine — is unhandled and falls to `loopSettlementDecision`'s silent Retry default. § 5.5 answers "Windows: as today" where every other lane names W1–W4; the approval lane's publish-then-CAS is in fact sound, but the obligation the other five meet is waived without saying why.

`NIT design.md:15 — `main:natsclient/client.go:968` is off by five`; the function is at `:963`, and `:968` is a doc-comment line belonging to the asynchronous variant.

## (g) Strongest case against — every item marked, every residual declared

All thirteen § 7 items carry a marking (answered / answered-by-ruling Q2, Q4, Q6, Q7, Q8 / residual). The six residuals — KV growth, audit noise, adoption drift, adopted-terminal drift, divergent duplicate (`SR:799-802`), record lifetime vs stream retention — each appear in `proposal.md` § Declared cost, plus the pre-existing `PendingToolResults × ToolResultMaxBytes` bound L4 explicitly neither widens nor guards. No residual is "satisfied" by a filed issue, and the change opens none.

## Verdict

`DESIGN CHANGES REQUESTED`. Blocking list: (1) the cold W4 rebuild source — `design.md` § 5.3 step 3 vs § 5.3 W4, with `tasks.md` 1.2. The two HIGHs should clear before the owner sees this: one asks the owner to ratify a mechanism already attributed to him, and the other would re-introduce through the property test the defect the change is named for. The design is otherwise faithful to all eight rulings, the deletion ledger is complete, the spec delta's MODIFIED block is correct, and tasks.md is clean of landing work.
