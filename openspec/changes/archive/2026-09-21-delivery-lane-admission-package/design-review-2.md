# DESIGN REVIEW PASS — round 2 — 0 BLOCKING / 0 HIGH / 2 MEDIUM / 2 NIT (round-1: all 13 closed, none by wording)

semstreams-reviewer, **pre-owner design review**, pass 2, read-only, after the recorded round-2 `INVENTORY PASS`
(`latch/inventory-pass-2.md`). Target `design.md` sha256 `5bddec30…`, `inventory.md` `eee8baef…` — both match the
handoff. All commands from `/Users/coby/Code/c360/semstreams-wt/verify-20fe8d09` (HEAD = `20fe8d09db61…`,
`git status --porcelain` = 0 at start and finish). No repo mutation, no `go test` (host rule, #1340).

PASS is not owner approval. **Q2 remains the gate**: the design is written assuming an amendment the owner has not
ruled, and one artefact still asserts that amendment as fact (MEDIUM-A).

Gates: `scripts/inventory-verify.sh` pins=268 ok=268 EXIT=0 · `gofmt -l` on the round-2 draft clean, `gofmt -e`
parses · `go build ./...` exit 0 · `go vet ./processor/agentic-...` exit 0.

## 1. Round-1 findings — every one closed by mechanism

I checked each against code or an artefact, not against the § 14 changelog. **None was closed by wording alone.**

| # | Closed by | Evidence I ran |
|---|---|---|
| HIGH-1 | `L3:` pins throughout; § 2 row 4 "1 at `L3:`"; § 4 `−9`; § 9 states the measurement | Printed all fifteen claimed dispatch `L3:` lines from `git show c58c65bd:…component.go` — `:569`, `:576`, `:610`, `:614`, `:651`, `:655`, `:584-585`, `:625-626`, `:666-667`, `:497-498`, `:505` — **every one matches** the quoted text. `tasks.md` 2.4 now says "three lanes at `L3:`", `proposal.md` says "dispatch ×3". |
| HIGH-2 | `tasks.md` gate line: `task lint && go test -race ./… && go vet -tags=integration ./…`, with all four tagged files named inline | Re-derived the four files by `head -3 … \| grep 'go:build integration'` over the census: dispatch `terminal_settlement_integration_test.go`, governance `delivery_settlement_integration_test.go`, loop `delivery_settlement_integration_test.go` and `partial_publish_settlement_integration_test.go`. Exact match. |
| MEDIUM-1 | `runWork` unexported | Read the draft: declared `:144`, called once at `:134` inside `Settle`. No external caller anywhere in § 5's rewrite table, § 6's #1249 account, or `tasks.md`. |
| MEDIUM-2 | `Observe` panics on nil `react` (`:214-216`); the nil branch in the fatal arm is gone (`:223` calls `react(result)` unguarded) | Checked house precedent before accepting a panic: `pkg/logging/nats_handler.go:45`, `pkg/worker/pool.go:73`, `model/health.go:190`, `processor/agentic-dispatch/factory.go:15` all panic on a nil required dependency, and `revive.toml` carries no panic/deep-exit rule. Idiomatic here. |
| MEDIUM-3 | `noObserver` package var (`:162-166`); `NewBinding` seeds `done` (`:181`); `Done()` doc says "never nil" | See § 2 — this one needed real reasoning, and it holds. |
| MEDIUM-4 | I5 carves the subject out, I9 conditions it; the spec delta carries the same sentence at `:28-29` | Read both. I5: "reads no payload; the only metadata it reads is the subject, and only when a declarer exists." I9: "when absent, nothing on the delivery is read." Both are now provable against the draft's `refuse` (`:85-94`), and `tasks.md` 1.2 was widened from I1–I8 to **I1–I9**. |
| MEDIUM-5 | No import path in any SHALL | `grep -nE 'internal/\|deliverylane\|pkg/' specs/jetstream-consumer-policy/spec.md` → **exit 1, zero hits**. The delta says "one shared package"; the path lives in `design.md` § 5 and the doc comment, so option C needs no second spec delta — which is what makes § 3's "directory move plus the walked path" claim true. |
| MEDIUM-6 | Census regenerated mechanically, 31/11 → 36/16 | Closed in method; the output is one row short — inventory MINOR-A, carried here as NIT-C. |
| MEDIUM-7 | R1 → **#1342**, genuinely filed | `gh issue view 1342`: OPEN, milestone **v1.0.0-beta.163**, labels `bug` / `area:agentic` / `horizon:pre-v1` / `class:advertised-absent`, GraphQL `blockedByIssues.totalCount` = 1. Its body carries a per-lane table at `c58c65bd` — 8 lanes, 3 declare, 5 silent — that matches my own round-1 measurement row for row. Artefact-backed, not a wording claim. |
| NIT-1 | § 2 row 2 now claims the tightening ("Strictly narrower than today, and closer to spec `:603`") and names the three tools test sites that take the changed path | Read. |
| NIT-2 | `Settle` doc: "a nil here is the caller's contract violation"; `Consume` doc: "A nil msg is tolerated because the typed helper quarantines it" | Verified the `Consume` half is **true**, not assumed: `natsclient/delivery_settlement.go:327-332` returns `quarantined: true, ownerStopNeeded: true` for a nil msg, so `Latch` closes the lane. The asymmetry is now justified rather than accidental. |
| NIT-3 | `tasks.md` 3.1 scoped to the latch with the eleven-`drainIssued` carve-out stated; the spec SHALL narrowed to "No migrated binding SHALL declare its own admission latch" | Read both. Narrowing the SHALL to match the guard is the right direction — the drain-once half is still covered by the one-home scenario's "retains the shared binding it constructed after acquisition". |
| Q1 / Q3 / Q4 | Recorded as decisions in § 13 | Q1 `internal/deliverylane` with the export gate named; Q3 → #1342; Q4 accepted with MEDIUM-5 applied. |

## 2. New code read fresh — the closed-channel seed

The coordinator asked me to reason about *a binding constructed, never started, then drained*. Walked:

`b := NewBinding(h)` → `done = noObserver` (closed at package init). `b.Drain()` → `drainOnce.Do(h.Drain)`, drains
once. `<-b.Done()` → returns immediately. Correct: there is no observer, so joining must not block.

**The guarantee is structural, not documentary, and that is what makes MEDIUM-3 closed rather than merely
described.** All three `Binding` fields are unexported, so no package outside `deliverylane` can write a composite
literal with a handle — `NewBinding` is the only way to obtain a usable binding, and it always seeds `done`. The one
residual construction, a zero-value `deliverylane.Binding{}`, has a nil handle, so `Drain()` and `Closed()` panic on
a nil interface call long before anything could block on `Done()`. There is no silent path.

**Dropping the five Stop guards is behaviour-preserving for the `lifecycle_causal_test.go` bindings.** I checked the
five literals rather than taking § 5's word: dispatch `:149`, governance `:136`, loop `:204`, model `:138`, tools
`:187`, all `consumers: []streamConsumerBinding{{handle: h}}`, all inside
`TestLifecycleRunningDeadlineIsTerminalNoReplay`. Today those have `observerDone == nil`, so the guarded Stop
**skips** the join; after the change they have `done == noObserver`, so the unguarded `<-Done()` **returns at once**.
Same observable behaviour, one fewer branch.

A second, unclaimed gain worth recording: those literals also have `drainOnce == nil` today, which is why every
current `drain()` carries a lazy `if b.drainOnce == nil { b.drainOnce = &sync.Once{} }` repair — a repair that is
itself racy in principle (two drainers could each install a different `Once` and drain twice). It is unreachable
today because only literal-built bindings have a nil `Once` and only Stop drains those. The value `sync.Once` in
`*Binding` removes the possibility structurally; § 2's "makes that structural" undersells it.

The residual ordering caveat is stated honestly in both the design (§ 5 "Done() safety caveat") and the doc comment
(`:206-207`): a Stop that ran before `Observe` would see an already-closed `Done()` and not join the observer that
starts afterwards. That is **identical to today's nil behaviour**, and unreachable at all five sites because
`Observe` runs before the binding is published under `lifecycleMu` (dispatch `:584-587`, loop `:1112-1117`, same
pattern in the other three). Not a regression; correctly not designed around.

**`react` at every present call site, including governance.** Yes for all five. Governance's "narrower shape" is
only the absence of `consumeAdmittedDelivery`; its observer already has a body
(`logger.Error("Governance delivery ownership lost", "port", portName, …)`), so it supplies a closure, and
`setupConsumer(ctx, port, handler)` takes `port` as a **parameter** (`component.go:454`), so the capture has no loop-
variable hazard. Model (`:317`) and tools (`consumerSetup`, `:362`) likewise. Dispatch and model pass a
`reactDeliveryFatal` method value; tools and loop pass closures. None can be nil.

## 3. Spec delta — re-diffed fresh, all three blocks

```
diff <(sed -n '600,625p' openspec/specs/jetstream-consumer-policy/spec.md)                          <(sed -n '11,41p'  <delta>)
diff <(sed -n '37,58p'  openspec/changes/settle-after-durable-effect/specs/jetstream-.../spec.md)   <(sed -n '53,75p'  <delta>)
diff <(sed -n '5,33p'   openspec/changes/settle-after-durable-effect/specs/jetstream-.../spec.md)   <(sed -n '77,106p' <delta>)
```

- **Block 1** (live spec, untouched by L1): both existing scenarios restated **verbatim**; the only additions are one
  prose paragraph and one new scenario. The trailing diff chunk is my `sed` range spilling into the next
  requirement's heading, not a dropped scenario.
- **Block 2** (restates L1's MODIFIED): **exactly one changed line**, the `**AND**` clause. Both scenarios restated.
- **Block 3** (restates L1's ADDED): **exactly one changed line**, the same clause. All three scenarios restated.

So the coordinator's claim holds as stated, and the OpenSpec rule that a MODIFIED block restates every scenario is
satisfied in all three. No `### Requirement:` heading is removed or reworded, so no `// spec:` citation is stranded.
Intent is preserved and sharpened: the round-1 table of five things the original clause forbade still maps
one-for-one, and the new prose now spells "no lifecycle authority" out as "no Stop, no restart, no reconstruction,
no registry of lanes", which closes the round-1 observation that "restart" survived only by inference.

## 4. Size — re-measured at round 2

228 total / 139 code / 89 comment-blank — matches § 4 exactly. Residue 228 +17 +25 +5 −9 −10 = **256**, and "266 if
the guards are kept" checks. I verified the `−10` rather than accepting it: dispatch `:505` and tools `:688` use a
bare `<-done` (3 lines → 1); model `:557`, governance `:650` and loop `:785` wrap a `select` on `ctx.Done()`
(7 lines → 5, dropping only the `if`/`}`). Five × −2 = −10, and § 5's parenthetical "(loop/model/governance keep
their `select` on `ctx.Done()`)" is exactly right. 256 is 47% of the 550 in-file bound and 43% of 594 all-in.

## 5. Findings

`MEDIUM-A draft deliverylane.go:11 (= design.md:97, landed verbatim by task 1.1) — the package doc asserts agentrun as a PRESENT consumer, which depends on the unruled Q2`
- Mechanism: the doc comment reads "its present consumers are the five agentic components and agentrun (#1249)".
  If Q2 is ruled "as ruled" rather than "amend", #1249 lands its own sixth copy and agentrun is **not** a consumer —
  the sentence ships false in the source tree. `tasks.md:5` claims the ruling "affects only #1249's consumption, not
  these tasks", and task 1.1 says the file lands **verbatim**, so this is the one place the claim is not true.
  Everywhere else the dependency is correctly fenced: § 6 is headed "pending owner ruling on #1341 Q2",
  `proposal.md:45` says "pending owner ruling", and `tasks.md:73` puts #1249's scope out of scope. I swept for
  others (`grep -nE 'Q2|1249'` across design, tasks, proposal, spec delta and the draft) and this is the only leak.
- Fix: "its consumers are the five agentic components, and `agentic/agentrun` once #1249 adopts it" — or drop the
  parenthetical. One line, no design change.
- Verification: the sweep above; § 6's own closing sentence, "Without the Q2 ruling the sixth copy lands regardless
  of what #1341 builds", which is the design contradicting the doc comment two hundred lines apart.

`MEDIUM-B #1341 docket comment (GitHub) — the owner will rule Q2 off round-1 numbers`
- Mechanism: the docket comment on #1341 says "Measured: 550 lines in five files replaced by **≈247** (package
  **≈209** plus per-component residue)". Round 2 is **256** and **228**. The comment is the artefact the owner reads
  — `design.md` is not on the issue — so the correction has not propagated to the published layer. My contract calls
  a surviving pre-correction claim in a published layer a finding, not hygiene.
- Fix: edit the comment to 228 / 256 and to "36 tests in 16 files"; the Q2 question itself is well-posed and needs no
  change. While there: the comment says the reviewer found "2 HIGH, 6 MEDIUM, all being applied in round 2" — true,
  and now verifiable as closed, so a one-line round-2 addendum would let the owner rule without reading the package.
- Verification: `gh issue view 1341 --json comments`; `design.md` § 4; `proposal.md:59`.

`NIT-C design.md § 8 / inventory.md:355 — the census number is stated three ways and the list is one row short`
- 36/16 in `design.md` § 8, `tasks.md` 2.6 and the inventory Measurements row; **35**/16 in the inventory prose at
  `:355`; **35 pins across 15 files** in the list itself. Truth is 36/16 (my independent awk run). The unpinned file
  is `agentic-dispatch/terminal_settlement_integration_test.go`
  (`TestIntegrationProductionCallbackUnknownPublishQuarantinesExactLane`, reaching the surface at `:285`) — a pin the
  **round-1 inventory had** and the regeneration dropped. Consequence is bounded by HIGH-2's `go vet
  -tags=integration`, which compiles that file on every dispatch commit. Detail in `inventory-pass-2.md` MINOR-A.

`NIT-D design.md § 7 I5 — cites "this change's delta scenario" for text that lives in the requirement prose`
- The subject carve-out is at delta `:28-29`, inside the requirement body, not inside a `#### Scenario:`. Normative
  either way in OpenSpec, but task 1.2 asks the developer to resolve I5 against its cited home, and the citation
  points at the wrong kind of clause. Say "this change's delta, requirement prose".

## 6. Verdict

**DESIGN REVIEW PASS.** No blocking, no high. All thirteen round-1 items are closed by a mechanism I checked in
code, in an artefact, or in a command — **none by wording**; MEDIUM-6 is the only one whose closure was a method
change rather than a fix, and that method change found the five `lifecycle_causal_test.go` consumers the round-1
review had missed, which improved the design (MEDIUM-3's do-nothing path) rather than just the count.

Before landing: MEDIUM-A (one doc-comment line), MEDIUM-B (the docket comment the owner rules from), NIT-C, NIT-D.
None needs another design pass.

**The one thing still owed is the owner's:** Q2. The design, the proposal and § 6 all fence it correctly as pending,
and nothing in `tasks.md` § 1–4 depends on the answer — a ruling either way leaves this change's shape unchanged and
only decides whether #1249 consumes the package or lands a sixth copy for #1341 to delete afterwards.
