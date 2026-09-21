## Design docket — one owner question (2026-09-19)

Design package drafted (architect) and independently passed (reviewer: inventory 247/247 pins, 0 BLOCKING; design 0 BLOCKING / 2 HIGH / 6 MEDIUM, all being applied in round 2). Home recommended and adopted as a design call: `internal/deliverylane`, a framework-internal package like `internal/lifecyclecleanup` and `internal/maxdelivery`, importable by `agentic/agentrun`, invisible to sisters, outside both ADR-106 tiers; export is recorded as a future gate on semdev's migration, not taken now. Measured (round 2, at the L3 head `c58c65bd`): 550 lines in five files replaced by 256 (package 228 plus per-component residue), 47% of what it deletes. The residual R1 the pass surfaced is filed as #1342.

**Q2 — amend the ruled #1249 design?** The #1249 docket ruled 2026-09-18 ("as recommended on all five") accepted design-draft-4 § 2.6, which copies the agentic-loop latch verbatim (`delivery_owner.go:29-63`, `:74-86`) into `MilestoneSubscriber` as a sixth spelling. The 2026-09-19 ruling that created this issue names L4 ("so L4 does not need to do it all over"). The two rulings point opposite ways for #1249 and only the owner reconciles them.

- **Recommended:** amend #1249 § 2.6 to consume `deliverylane.Admission` / `Binding` / `Observe` and drop its hand-rolled drained flags; #1249 implementation then sequences after #1341 like L4. Nothing else in the #1249 ruling changes (`DeliveryFatal()`, `RegisterMetrics`, the matrix, Q1–Q6 all stand).
- Alternative: leave § 2.6 as ruled; the sixth copy lands with #1249 and #1341 deletes it afterwards, costing one extra rewrite and one more review round.

A one-word ruling ("amend" / "as ruled") is enough; the design is written assuming the amendment and marks that paragraph pending.
