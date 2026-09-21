# INVENTORY PASS — round 2 — 0 BLOCKING / 0 MAJOR / 1 MINOR (round-1: 2 MAJOR + 3 MINOR, all closed)

semstreams-reviewer, **inventory review** mode, pass 2, read-only. Target `scratchpad/latch/inventory.md`
(sha256 `eee8baef…`) @ base `20fe8d09`. All commands from `/Users/coby/Code/c360/semstreams-wt/verify-20fe8d09`
(HEAD = `20fe8d09db61…`, `git status --porcelain` = 0 at start and finish). No repo, worktree, branch, sister or
GitHub mutation; no `git checkout`/`restore`/`stash`/`clean`/`reset`; no `go test`.

## 1. Pins — EXIT 0, 247 → 268

```
bash scripts/inventory-verify.sh <abs>/latch/inventory.md ; echo "EXIT=$?"
changed since base (20fe8d09..HEAD):  (none)
pins=268 ok=268 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0
EXIT=0
```

Twenty-one new pins, all clean. The note at the end of the rebase block explains why a file full of `c58c65bd`
facts still verifies against `20fe8d09` — "Facts about `c58c65bd` appear in prose and tables only: the verify script
reads this tree at `20fe8d09`" — which is the right way to carry a two-base measurement through a single-base
checker.

## 2. Round-1 MAJOR-1 — CLOSED by mechanism

The drain-once census now pins **all eleven** planes individually, matching my round-1 enumeration exactly,
including the two under `examples/` that are easy to drop:

```
json_filter:117 · json_generic:101 · json_map:123 · output/file:140 · output/httppost:140 · output/websocket:195
graph-ingest:649 · rule/processor:581 · storage/objectstore:91 · examples/document:144 · examples/iot_sensor:143
```

More important than the count, the **reason** is now recorded and measured rather than asserted: `:169-171` states
the class boundary I used to refute my own round-1 BLOCKING — "the five agentic bindings need drain-once because the
observer goroutine is a second drainer" — and the Measurements table at `:517` carries the evidence
(`for f in <eleven>; do grep -cE '\.Drain\(\)' $f; done` → `1`, eleven times). Design § 10 R4 carries the same
finding forward as a recorded residual with the adoption condition named. That is the establishing-side adoption
enumeration my contract asks for, done properly: one line per plane, pinned, with no migration required of any of
them.

## 3. Round-1 MAJOR-2 — closed in METHOD; the output is one row short (new MINOR-A)

The census is now generated rather than hand-listed, and the method is strictly better than mine: adding
`streamConsumerBinding{` and `admission.refuse` to the pattern surfaced the five `lifecycle_causal_test.go` files
that my round-1 pattern missed entirely — and those five are exactly the **do-nothing path** for MEDIUM-3's
never-nil `Done()`, so finding them changed the design rather than just the count. `:355` says so explicitly
("The reviewer's 31/11 is the subset without `streamConsumerBinding` and `admission.refuse`"), which is the honest
way to record a reviewer being out-measured.

My independent re-run of the widened pattern returns **36 test functions across 16 files, 4 `//go:build
integration`** — see MINOR-A for where the artifact disagrees with itself.

## 4. Round-1 MINOR-1..3 — all CLOSED by mechanism

- **MINOR-1**: `openspec/project.md` is now a verified pin (`:483`), not a paraphrase. The rule reads "two or more
  independent products" — the phrasing that made my round-1 grep miss it.
- **MINOR-2**: `:97-99` now reads "Governance's copy is narrower **in the file only**: `consumeAdmittedDelivery` is
  genuinely absent, but its drain and its panic wrapper exist and live in `component.go:728` and `component.go:349`
  — the issue's 'no `drain` / run-wrapper' is a wording defect, not a fact." It names the issue's error instead of
  echoing it.
- **MINOR-3**: the rebase note now distinguishes the two L3 heads by measurement (`f4fd4369` not a descendant, exit
  1; `c58c65bd` a descendant, exit 0, five files byte-identical), and carries the consequence I asked for —
  "static lane constructions drop 10 → 8 (dispatch 5 → 3), settlement-only constructions 5 → 3, refusal declarers
  stay 3, silent lanes 7 → 5 … a re-MEASURE for dispatch, not only a line shift". I re-derived every one of those
  numbers independently and they are correct.

## 5. Finding

`MINOR-A inventory.md:355 and :518 — the test census disagrees with itself four ways, and the generated list drops one file`
- Mechanism: `:355` prose says "**35** test functions across **16** files"; the pin list beneath it holds **35**
  pins across **15** files; the Measurements row at `:518` says "**36** tests / 16 files"; `design.md` § 8 and
  `tasks.md` 2.6 both say **36 / 16**. Measured truth is **36 / 16**. The file with no pin is
  `processor/agentic-dispatch/terminal_settlement_integration_test.go` — named in the `:355` prose as one of the four
  integration-tagged files and named again in `tasks.md`'s gate line, but absent from the list. Its touching test is
  `TestIntegrationProductionCallbackUnknownPublishQuarantinesExactLane`, reaching the surface at `:285`
  (`completeClosed := c.consumers[1].handle.Closed()`).
- Why it is worth a line: that exact pin was **present in the round-1 inventory** (`:327`) and the mechanical
  regeneration lost it. A generated census is supposed to make that impossible, so the regeneration's filter needs
  the check, not just its output.
- Consequence is bounded: `tasks.md` 2.6 tells the developer to "regenerate the list at the re-derived base", and
  HIGH-2's `go vet -tags=integration` compiles that file on every dispatch commit, so the miss cannot survive to
  merge. That is why this is MINOR and not MAJOR.
- Fix: reconcile to 36/16 in all four places and add the dropped row; while there, `:355` says "the seven symbols"
  where § 1 enumerates six.
- Verification: `git grep -lE '<widened pattern>' 20fe8d09 -- 'processor/agentic-*/*_test.go'` with awk attribution
  → 36/16/4; `awk` over the `:355` block counting `^- \`processor` bullets → 35 across 15 files;
  `grep -n 'terminal_settlement_integration' inventory.md` → prose only, no pin.

## 6. Verdict

**INVENTORY PASS.** Both round-1 MAJORs and all three MINORs are closed by a checkable mechanism, not by wording;
MAJOR-1's closure is measured, and MAJOR-2's method change found a class of consumers the round-1 review had
missed. MINOR-A is a one-row reconciliation with a bounded consequence and does not gate the design.
