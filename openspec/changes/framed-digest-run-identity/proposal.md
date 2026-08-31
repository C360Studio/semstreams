# Change: Framed-digest run identity (Case A)

**DESIGN IN PROGRESS — claim commit.** This proposal is a stub; the architect is materializing the
design from the recorded salvage. Expected to fail OpenSpec validation until the first spec delta
lands (see PR #1179 — a design-phase claim push is expected to fail validation).

Issue: #1192 (milestone `v1.0.0-beta.163`). Split from #1168 by owner ruling 2026-08-30 — Case A,
the run-identity derivation half; #1168 shipped Case B (`ae35f296`, archive
`2026-08-31-federation-identity`).

## Why (from the issue, to be developed)

`Mint` derives the local run instance from the foreign dispatch-root loop UUID alone. Since slice B
(`300e57fe`, #1148) two imported runs from distinct authorities sharing an instance token get a loud
refusal instead of a silent collapse; this change makes them **coexist** by deriving the run
instance via a length-framed digest over the origin's full canonical ID, consolidating on the
existing `alertInstance` primitive (`graph/events.go` `writeFramedString`).

## Design record (salvage — do not rediscover)

- PR #1178 architect revision comment (2026-08-30), Case A blocks: §1.C non-Go lane table,
  §1.D N5/N8, Case A rows of §1.E/§1.I/§1.J/§1.K.
- `docs/proposals/gh1168-federation-identity-{design,inventory,pins}.md` (on main).
- Decisions moved here unruled: O-1 (`RunID` meaning), `ResolveRun` named capability loss,
  `DerivedEntityID` framing/truncation, authority-pair budget 170→168, DEFERRED paragraphs at
  `openspec/specs/graph-ingest/spec.md:934-948`.

## Coordination

Codex is holding for this issue (#1146/#1155 re-adopt the AgentRun records this re-keys); #1154 is
the same class one layer out. Pre-v1 only (ADR-102 d7).
