# INVENTORY FAIL — 1 BLOCKING / 4 MAJOR / 3 MINOR

semstreams-reviewer, **inventory review** mode, read-only. Target `scratchpad/l4/inventory.md` @ base `68c14c8e`. Every command run from `/Users/coby/Code/c360/semstreams-wt/verify-68c14c8e` (`git rev-parse HEAD` = 68c14c8e…, `git status --porcelain` empty). No worktree/branch/sister/GitHub mutation. Bar applied: step 1 grammar-only (MET) · no (b)/(c) completeness miss (**(c) MISSED** — the fail) · no step-3 BLOCKING (met).

## 1. Pins — exit 1, all nine non-OK lines are grammar, zero stale pins

`scripts/inventory-verify.sh <abs path>/inventory.md ; echo "EXIT=$?"` → `pins=129 ok=122 moved=0 ambiguous=0 drift=3 malformed=4 unparsed=2` · `changed since base (68c14c8e..HEAD): (none)` · `EXIT=1`
```
MALFORMED - `openspec/specs/agentic-loop/spec.md:212-213` (main): "A restart-surviving answer SHALL NOT be sou
MALFORMED - `openspec/specs/agentic-tools/spec.md:452-537` (main): completed-outcome replay is the existing au
MALFORMED - `openspec/specs/agentic-loop/spec.md:430-446` (main): terminal redelivery creates another terminal
MALFORMED - `docs/concepts/17-approval-flow.md:65-68` (main): "Restart-safe. `LoopEntity.PendingApproval` live
UNPARSED - No active `openspec/changes/` entry touches agentic-loop (`ls openspec/changes` → only `archive`).
UNPARSED - ADRs: none mention #1146/#759/settlement (`git grep -l -i -E '1146|#759|restart-safe|settlement' o
DRIFT agentic/state.go:52
DRIFT agentic/state.go:54
DRIFT agentic/state.go:74
```
- **3 DRIFT = backtick substitution.** `sed -n '52p;54p;74p' agentic/state.go` returns the pinned text exactly, with Go struct tags in backticks; the inventory transcribed them as `'json:"iterations"'`, so the fixed-string compare misses. Content at all three pins is correct.
- **6 MALFORMED/UNPARSED = § 5's heading.** `scripts/inventory-verify.sh:43` enters lenient mode only on the literal prefix `## Adjacent claims`; the file's heading is `## 5. Adjacent claims on the territory`, so § 5 parses strict and its `(main):`-scoped line *ranges* and prose bullets are rejected. None is a 68c14c8e pin.
- **MINOR-1**: both are one-line fixes (backticks; rename the heading) and would take the script to exit 0. Do not re-pin — re-pinning a landed base destroys pre-change evidence.

## 2. Completeness — independent enumeration vs the 43 rows / 7 Put sites

Four `grep -rn --include='*.go' -E <pattern> processor/agentic-loop processor/agentic-dispatch/task_recovery.go processor/agentic-model/provider_settlement.go | grep -v '_test.go'`, patterns verbatim:
```
(a) GetLastMsgForSubject|readRetained[A-Za-z]*\(|ReadRetained[A-Za-z]*\(|ReadAgent[A-Za-z]*\(|ReadGovernanceVerdict|readRetainedGovernanceVerdict
(b) loopsBucket\.(Put|Update|Create|Delete)\(|persistLoopState|persistApprovalGate|persistHandlerResult|persistTerminalOutcome|UpdateLoop\(|\.Put\(ctx|\.Update\(ctx|\.Create\(ctx
(c) reflect\.DeepEqual|buildToolMessages|\.Messages\[|Messages\[[a-z]|== result\.Content|== response\.Content|strings\.EqualFold
(d) DeliveryDecision(Ack|Retry|Quarantine|Term)|\) \(.*DeliveryDecision   [over settlement_recovery.go approval_response_handler.go delivery_owner.go task_recovery.go provider_settlement.go]
```
**(a) COMPLETE.** Helpers SR:29-31,38,44,50,63,197,273,325; call sites SR:402→D4, 517→D11, 577→D20, 622→D21, 867/881→D32, 149/211→D14/D15; TR:52/90/165→D42; PS:37/89 + `agentic-model/component.go:616`→D43. All rowed.

**(b) COMPLETE.** Exactly six primitive loops-bucket writes — `component.go:1532` Create, `:1927` Update, `:1960` COMPLETE_ Create, `:2319` Update, `:2411` Put, `approval_response_handler.go:275` Update — all six in § 2. The four `persistHandlerResult` callers § 2 does not name (`:1596`, `:1708`, `:2215`, `:2564`) each pass `result.State.IsTerminal()`, so they take the terminal branch at `component.go:1783-1792` and land on the `:1927` Update that row 6 covers as "all terminal lanes". `handlers.go:2478`, `approval_sweeper.go:81`, `settlement_recovery.go:961`, `component.go:2536` are `LoopManager.UpdateLoop` (`state.go:524`) — process map only, correctly excluded. `trajectory_recorder.go:217`, `trajectory_evidence.go:70` write other stores.

**(c) ONE MISS.**

`BLOCKING processor/agentic-loop/component.go:1852-1866 — terminal applied-proof by message-content equality, unrowed and absent from the design's delete/survive ledger`
- Mechanism: on a redelivered terminal, `selectTerminalOutcome` (`:1959-1975`) finds `COMPLETE_<loopID>` already present, `Get`s it, and `persistTerminalOutcome` decides this delivery's disposition by **payload content** — `marker.Result != saved.Result || prepared.Result != saved.Result || !reflect.DeepEqual(prepared.Decision, saved.Decision)` (`:1854-1855`), and for failures `marker.Error != saved.Error || prepared.Reason != saved.Reason || prepared.Error != saved.Error` (`:1863-1865`). Mismatch → `DeliveryDecisionRetry`, "lacks this delivery's compatible applied proof": retry-forever on an unproven terminal. Identical shape to D26 (`SR:843`), which the inventory verdicts "the content-equality proof is GONE" and design § 4 deletes.
- Consequence: § 4 deletes one terminal content proof and leaves its twin on the single terminal owner D41 declares "stays", so the design's claim that terminal redelivery settles from `State` alone is not true of the code it keeps — and the "retries unproven terminal results forever" owner-Q raised for D26 (`SR:851`) applies here with no row asking it.
- Fix: one row for `C:1846-1870`, classified, folded into D26's owner question or told why it diverges.
- Verification: `sed -n '1833,1870p;1940,1975p' processor/agentic-loop/component.go`. Refutation attempted — D41 cites `C:1833-1845`, `C:1917-1927`, `C:1959-1964`; `:1852-1866` is in none, no § 7 pin lands there, and § 4 names neither line.

Every other (c) hit is rowed: SR:687/697/723/735→D22 · SR:793/799→D27 · SR:834/843→D26 · SR:948→D33 · SR:505→D10 · ARH:212→D30 · C:1538→D7 · ST:389-390→D5 · H:2614→normal path (H:2612 pinned).

**(d) one unrowed cluster.** ARH:155-280→D29-D36 · SR:857-916→D32/D34 · SR:921-981→D33 · SR:1045-1049 `loopSettlementDecision` and `delivery_owner.go:15-19` both kept by § 4. Plus:

`MAJOR processor/agentic-loop/component.go:2162-2190 — four tool-lane recovery dispositions have no row`
- Mechanism: when `observedRevision == 0` the handler re-reads authority and returns `Retry` "loop not yet observable" (`:2168`); `Quarantine` "tool process authority conflicts" on TaskID/Role/Model divergence (`:2175-2176`); `Retry` "tool authority for loop %q changed" on terminal or `PendingApproval` DeepEqual drift (`:2178-2180`); `Retry` "tool execution %q does not own the current gate" on a six-field `PendingApproval` identity compare (`:2185-2189`). § 1 rows none of them; `C:2163`/`C:2178` are pinned in § 7 but cited only in § 2's prose about the discarded revision.
- Consequence: the last check is the loop's current gate-ownership rule, and design § 2 changes exactly the revision all four consume (`Put` → `Update(observedRevision)`). Unrowed means unclassified means the design cannot say whether it survives.
- Fix: a row per disposition (or one for the cluster) with its facts-read column. Verification: `sed -n '2160,2205p' processor/agentic-loop/component.go`; grep of § 1 for `2163`/`2178`.

## 3. Classification spot-check — 12 rows read at their pins, no BLOCKING

D2/D3 `SR:384-400` (task) HOLDS — TaskID/Role/Model + `State.IsTerminal()` are entity fields · D4 `SR:402-422` (task) HOLDS **conditional on L2**: the `:414-421` rebuild and design § 3.1's `PublishedRequestID != R1` test both need R1 re-derivable, which § 0 row 1 measures as false today · D11 `SR:517-535` (model response) HOLDS, open Q stated on the row · D21 `SR:622-632` (tool result) HOLDS — role/model already on the record `AG:46-93` · D26 `SR:821-852` HOLDS as written, but see the BLOCKING twin · D28 `SR:743-758` HOLDS · D32 `SR:857-916` (approval response) HOLDS — `:859-862` is pure identity; the retained reads at `:867`/`:881` stay as rebuild sources and their absence routes to D33, as the row says · D37 `ARH:130-149` (approval reject) HOLDS — the synthetic result carries `RequestID: pending.RequestID` (`:139`), which I4 pins equal to `PublishedRequestID` · D16 `GD:547-564` (governance verdict) HOLDS — `lookupWaiter` miss → `Retry`; NEED is the honest verdict · D38 `AS:69-86` (startup/timer) HOLDS — reads `State` + `PendingApproval.Timeout` only; `:81` is in-process.

`MAJOR (§ 1 row D22) — "≠ PublishedRequestID ⇒ older batch ⇒ applied" asserts an ordering the ID grammar does not carry`
- Mechanism: `GenerateRequestID` = `<loopID>:req:<uuid.NewString()>` (`ST:1364-1365`); a UUID suffix is unordered, so `result.RequestID != PublishedRequestID` cannot be split into older-vs-unknown from the two IDs. D11 states this for the same compare on the response lane ("stale-vs-conflict needs an ordered ID grammar (open Q)"); D22 states the conclusion flatly — and D22 is the row authorizing deletion of `toolResultProvenInLaterRequest`, the inventory's own named "hazard". The design is more careful than the row (§ 3.3 step 2 routes unknown to Q4), so the row is where a settled reading would be inherited.
- Not BLOCKING under the stated bar: the missing fact is an ordering, not message content, and the fallback is Retry, not a content compare. Fix: carry D11's caveat and open Q onto D22.
- Verification: `sed -n '622,672p' settlement_recovery.go`; `ST:1364-1365`. Refutation attempted and the row's *other* premise survives: `H:2410-2414` gates `handleToolsComplete` — hence `IncrementIteration` (`H:2566`) and the mint (`H:2654`) — behind `AllToolsComplete`, so a *known-older* RequestID does imply applied.

`MINOR-2 SR:799-802` — § 4 deletes `approvalRequiredResultSuperseded` wholesale, which also deletes the `!reflect.DeepEqual(stored, result)` → Fatal "retained gate status conflicts" quarantine. A divergent duplicate on an already-stored ExecutionID would then ACK as applied instead of quarantining. D26 gets an explicit "Semantics change … owner Q"; this one gets none. (D27's AF classification itself still holds: design § 3.4 reduces the `:793` whole-`ToolResult` DeepEqual to key presence in `PendingToolResults`.)

## 4. Adopter seam — partly confirmed; the sister-readers row is materially incomplete

Read-only `git -C <sister> grep -n` only; no `go list`, no writes. `ls /Users/coby/Code/c360/` then swept semsage, semspec, semsource, semconnect, semboids, semmem, semmachina, semspec-ui-bmad, semspec-ui-run-visibility, semstreams-ui, semteams with `git -C <s> grep -n -E 'AGENT_LOOPS|agentic\.LoopEntity|LoopEntity' -- '*.go' '*.ts' '*.svelte'`, `git -C <s> grep -n -E "':req:'|\":req:\"|:req:" -- '*.go' '*.ts'`, and an unfiltered `git -C <s> grep -n 'AGENT_LOOPS' -- '*.go'`.

**Confirmed:** semsage `processor/ui-api/component.go:2,50,160,217` + `types.go:2`; semspec `cmd/semspec/watch_live.go:196,241` and `pkg/health/capture.go:30,84`; semmachina `internal/resume/pending.go:88` (+ `doc.go:80`). semsource/semconnect/semboids/semmem: zero hits — the inventory's silence on them is correct.

**Refuted — nothing parses the `:req:` suffix as a UUID.** The only sister parsing the subject is semspec `pkg/health/agent_response_walk.go:58,119`, and `extractLoopIDFromSubject` splits on the *first* colon because "*loop_id* is a UUID v4 (no colons)". The request-id suffix shape is unconstrained (semteams' fixture uses `agent.response.loop_x:req:abc`). Sharpen § 4 finding 4: the binding constraint is a colon-free loop-id half plus the literal `:req:` separator; L2 may shape the suffix freely.

`MAJOR (§ 3 "Readers (sisters)" and § 4 Surface A) — names 4 consumers, misses at least 7, including two KV-Watch control paths and an operator config key`
- Missed, all semspec unless noted: `processor/execution-bridge/completion.go:22,25,39-54` and `review_completion.go:28-51` **Watch** AGENT_LOOPS for terminal loops and translate them into `exec_produced` / `review_verdict_signal`; `processor/lesson-decomposer/component.go:901-924` and `processor/qa-reviewer/component.go:302-323` watch it for completions; `processor/recovery-consumer/backstop.go:40-45,167-247` + `config.go:32-35,70` scan it for liveness/orphan detection and expose `loops_bucket` as an operator config key defaulting to `AGENT_LOOPS`; `pkg/health/orchestrate.go:14,73,92` + `detector_repeattoolfailure.go:230` parse the key space, knowing it holds both `<uuid>` entries and `COMPLETE_` markers; semteams `cmd/semteams/main.go:492` and `approvalpause/doc.go:40` reimplement against `LoopEntity.State`.
- Consequence: the row reads as read-and-mirror UIs; in fact two sister control planes gate on this record and one treats entry presence as liveness. Fix: extend the § 3 Readers row and § 4 Surface A with the watch and liveness consumers, pinned. Verification: the per-sister unfiltered `AGENT_LOOPS` grep above.

`MAJOR (§ 3 "Lifecycle": "no expiry on the entity (retention is the bucket's, unmeasured here)") vs design I1`
- semspec `processor/recovery-consumer/backstop.go:247` asserts "AGENT_LOOPS is bounded (24h TTL, active…)" as its cost basis. Design I1 couples the KV record's lifetime to the retained request's; if a TTL'd record outlives or is outlived by that request, I1 breaks by expiry alone, no crash involved. D33 is the only place retention is observed (`SR:938-940`, `stream.Info().Config`) and it observes the *stream*, never the bucket. I did not measure the bucket's `KeyValueConfig` — outside the named packages; stated UNVERIFIED, not asserted. Fix: measure the AGENT_LOOPS TTL at this commit into § 3 Lifecycle before I1 enters a spec delta.

`MINOR-3 (§ 4 Surface A item 2, "unchanged behaviour. No silent loss.")` — semsage `processor/ui-api/sse.go:15` "watches the AGENT_LOOPS KV bucket and emits **one SSE event per KV change**", and `http.go:72,137,297` re-serve the decoded record. The additive-field claim holds, but the do-nothing path is not only a decode question: design § 2's `Put` → `Update(revision)` introduces CAS-failure/Retry re-handling, and write cadence is adopter-visible as SSE cadence. Surface A should answer cadence too.

## Verdict

`INVENTORY FAIL` — **1 BLOCKING, 4 MAJOR, 3 MINOR**. Not a rejection of the method: 122/129 pins verify, categories (a) and (b) are complete, and twelve classifications hold at the pin. The fail is the category-(c) miss at `component.go:1852-1866` plus the unrowed tool-lane dispositions at `component.go:2162-2190` — both recovery decisions the design's delete/survive ledger therefore never accounts for. Add those rows, carry D11's ordering caveat onto D22, extend the sister-readers row and the bucket-TTL measurement, and this reaches INVENTORY PASS without re-deriving anything else.

Per the reviewer contract I do not review or suggest a target state while the inventory is unpassed; nothing above is approval of `design-draft.md`.
