# INVENTORY PASS — 0 BLOCKING / 0 MAJOR / 2 MINOR

semstreams-reviewer, **inventory review** mode, read-only, second pass over `scratchpad/l4/change/inventory.md`
(317 lines, 49 rows). All commands from `/Users/coby/Code/c360/semstreams-wt/verify-68c14c8e` (`git rev-parse HEAD`
= 68c14c8e…, `git status --porcelain` = 0 entries). No mutation anywhere.

## 1. Script — exit 0, every pin holds

`scripts/inventory-verify.sh <abs>/l4/change/inventory.md ; echo "EXIT=$?"`
→ `changed since base (68c14c8e..HEAD): (none)` · `pins=147 ok=147 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0` · `EXIT=0`

Both round-1 grammar defects are fixed: the three `agentic/state.go` struct-tag pins now carry real backticks
(:289-291 of the file), and the § 5 heading is now `## Adjacent claims on the territory (§ 5)`, which the script's
`'## Adjacent claims'*` case at `scripts/inventory-verify.sh:43` does **not** match either — but the six § 5 bullets
no longer parse as pins because the heading line is itself the `## ` reset, and the four `(main):` entries now read
as lenient-mode-exempt. Net: 0 unparsed, and no pin was weakened to get there (147 > 129; the 18 new pins are the
ones the new rows needed).

## 2. Round-1 findings, each re-checked

- **BLOCKING `component.go:1852-1866` — CLOSED.** Now **D49** at `C:1846-1870`, pinned at `:1846`, `:1852`, `:1854`,
  `:1855`, `:1856`, `:1861`, `:1863`, `:1865` (all `ok`). Verdict reads "**DELETE the compare**; … ADOPT it by
  identity (loop ID + terminal kind)", with the saved payload replacing the candidate at `:1857-1858`/`:1867-1869`
  and content differences "logged at the audit line, never a disposition". That is the verdict the coordinator
  describes. It also correctly notes the "retained published terminal" at `68c14c8e` is the `COMPLETE_` **KV
  marker**, not a stream read — a distinction the design then carries (`design.md` § 5.7(b)).
- **D44 split — CONFIRMED.** `C:1833-1843` with sub-pins `:1838-1839` (revision/terminal Retry) and `:1841-1842`
  (TaskID Quarantine); pins `1838`, `1839`, `1841` all `ok`. Q7 applied: terminal-at-revision → effect-free ACK,
  revision conflict alone → Retry, TaskID conflict → Quarantine.
- **MAJOR `component.go:2162-2190` — CLOSED, all four dispositions rowed.** D45 `:2162-2170` (not observable →
  Retry), D46 `:2171-2177` (TaskID/Role/Model → Quarantine), D47 `:2178-2181` (terminal or gate drift), D48
  `:2182-2190` (six-field gate-ownership compare). Pins `2163`, `2168`, `2175`, `2178`, `2185`, `2189` all `ok`.
  D48 is correctly classified as an **identity** compare that survives, not content.
- **MAJOR D22 ordering — CLOSED.** D11 and D22 now both read "**conditional on ruling Q4**" and both name the
  grammar `<loopID>:req:<iteration>:<retry>` (#1328), both state that at `68c14c8e` IDs are UUID-suffixed
  (`ST:1364-1365`) and the classification is "not decidable" without Q4, and D22 adds "an unparseable ID
  quarantines, never falls to a content compare". That is the caveat I asked for, stated on both rows.
- **MINOR-2 `SR:799-802` — CLOSED.** Now carried in D22's verdict as an explicit "**Semantics change** … declared
  residual", with pins at `:799` and `:802`, and surfaced in `proposal.md` § Declared cost.
- **MAJOR sister readers — CLOSED, seven present and then some.** The Readers row now names, by category:
  Control planes (Watch) — `execution-bridge/completion.go:22,25,39,54`, `review_completion.go:28,51`,
  `lesson-decomposer/component.go:901,924`, `qa-reviewer/component.go:302,323`; Liveness —
  `recovery-consumer/backstop.go:40,45,167,247` + `config.go:32,35,70`; Key-space parsers —
  `pkg/health/orchestrate.go:14,73,92`, `detector_repeattoolfailure.go:230`; Mirrors — semsage ui-api, semteams
  `main.go:492` / `approvalpause/doc.go:40`, semspec `watch_live.go`, `capture.go`, semmachina. Plus a config
  coupling I had not found (`semspec/configs/e2e-*.json` `loops_bucket`), scoped to L3 #1329.
- **MAJOR TTL — CLOSED and measured.** § 0 adds a premise row and § 3 Lifecycle pins
  `internal/loopbucket/acquire.go:20` (`CreateKeyValue … History: 10, TTL: 24 * time.Hour`) and `:42-43` (startup
  refusal of any other policy). Both pins `ok`. The conclusion drawn — "an expired record is a gone loop", I1
  scoped to existing records, post-expiry redelivery takes the existing not-observable Retry — is sound and I
  independently confirmed the Retry sites (`sed -n '486,492p;572,576p' settlement_recovery.go` → `"loop %q is not
  yet observable"` at `:489` and `:575`).
- **MINOR-3 SSE cadence — CLOSED.** Surface A item 2 now answers cadence explicitly.
- **`:req:` UUID — CLOSED.** Surface A item 4 records the refutation and the sharpened constraint (colon-free
  loop-id half + literal `:req:` separator; suffix free).

## 3. Remaining findings

`MINOR (§ 1 rows D16, D26) — two verdict cells still say "owner Q" after the owner ruled them.` D16's cell ends
"could answer it — **owner Q**" and D26's ends "Semantics change … — **owner Q**", but #1330's last comment rules
**Q6** (verdict after waiter loss: "L4 owns a minimal ACK path with the tool lane's classification") and **Q7**
(terminal + unproven: "effect-free ACK with a metric and an audit line … no retry-to-MaxDeliver"). D44, D47 and D49
were updated to cite the rulings; D16 and D26 were not. `design.md` § 5.6/§ 5.7 and `tasks.md` 3.5/3.6 do apply
both rulings correctly, so this is a stale claim in a published layer, not a design defect. Fix: replace both
cells' "owner Q" with the Q6 / Q7 citation.

`MINOR (§ 6 command list, line 149) — a recorded search used the wrong receiver and could not have found its
target.` The line reads `git grep -n 'func (c \*Client) PublishToStreamWithMsgID\|Nats-Msg-Id' origin/main --
natsclient`; the actual declaration is `func (m *Client) PublishToStreamWithMsgID(...)` at
`origin/main:natsclient/client.go:963`. Only the `Nats-Msg-Id` alternate could match, so the first half is a
zero-hit-by-construction search left in the evidence record. No conclusion in the inventory rests on it (the
"neither side stamps a MsgID" premise at § 0 is carried by the `processor/rule/publisher.go` grep, line 156),
and the design correctly assumes the function exists — but a recorded search that cannot hit is the shape that
later reads as proven absence. Fix: correct the pattern or drop the line.

## Verdict

`INVENTORY PASS` — 0 BLOCKING, 0 MAJOR, 2 MINOR. Every round-1 finding is closed at the pins claimed, the script
is green at 147/147 against the stated base, and the two MINORs are text hygiene in the inventory, not gaps in
what the design may rely on. Design review proceeds (see `design-review.md`); this verdict is not owner approval.
