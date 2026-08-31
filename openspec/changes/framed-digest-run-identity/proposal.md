# Change: Framed-digest run identity (Case A)

**Salvage materialization — the design is recorded, not rediscovered.** Sources: issue #1192 (split from #1168 by
owner ruling 2026-08-30); the PR #1178 architect revision comment (2026-08-30), Case A blocks §1.C/§1.D N5+N8/
§1.E/§1.I/§1.J/§1.K; the pre-cut reviewed package at `4c46fbc5` (its graph-ingest and entity-id-contract deltas);
`docs/proposals/gh1168-federation-identity-{design,inventory,pins}.md` (inventory at `5967394f`, INVENTORY PASS).
Premises re-pinned at `main@ae35f296` in `inventory.md`. ADR draft: ADR-105 (run identity); ADR-104's scope note
explicitly leaves this half undecided. Milestone `v1.0.0-beta.163` (owner ruling on #1192, 2026-08-30).

Adjacent and NOT in this change: the import-lane collision gate → **#1194** (which owns the DEFERRED paragraph at
`openspec/specs/graph-ingest/spec.md:934-948` — the issue body's citation of those lines predates materialization;
the Case A deferral this change lands is the `:1063-1065` sentence, replaced by the spec delta below — and #1194
inherits a SPEC CORRECTION with the paragraph: it instructs "whoever lands #1168 MUST delete it", and #1168 closed
2026-08-31 without the O-4 rejection, so the pointer is stale until #1194 rewrites it); the
`run_scope=new` anchor-append bug → **#1193**; the zero-caller deletions → **#1187**; the environment surface →
**#1186**; the five-type `EntityID()` recompute → **#1154** (its RUN half is absorbed here, the rest sequenced
after).

## Why

Two measured facts on `main@ae35f296` (`inventory.md`):

1. `agentrun.Mint` derives the local run instance from the origin loop's bare instance token alone
   (`agentic/agentrun/agentrun.go:290`), so two imported loops from distinct authorities sharing an instance token
   derive ONE local `org.platform.chain.agent.execution.<instance>`. Since slice B (#1148, `300e57fe`) the second
   mint is REFUSED loudly (`:315-318`) instead of silently receiving the first origin's run — but the two runs
   still cannot coexist, and a refusal of legitimate foreign work is a correctness gap, not a resolution.
2. The framework tells every consumer to recompute the run entity from the bare `RunID`: eight production composer
   sites in this repo and four carrier classes in sisters (census in `inventory.md`). Every one of them predicts a
   value the framework owns.

## What Changes

- **BREAKING:** the run entity's instance becomes the lowercase 64-hex SHA-256 of a length-framed sequence over the
  versioned digest domain `semstreams.agent.run.v1` and the origin loop's FULL canonical entity ID. Two origins can
  never share a run; imported runs from distinct authorities coexist. Run entity IDs change shape:
  `org.platform.chain.agent.execution.<64 hex>`.
- **One home for framed-digest derivation.** `pkg/types` gains the `agent-run` family
  (`{chain, agent, execution, InstanceBytes: 64}`), `AgentRunIdentityFamily()`, and
  `FrameworkIdentityFamily.DerivedEntityID(org, platform, digestDomain, frames...)` — the length-framing is
  byte-identical to the existing `writeFramedString`/`writeRuleTriggerFrame` pair, and the rule-trigger derivation
  consolidates onto it with unchanged digest bytes (pinned by the existing trigger tests). The alert path keeps its
  private writer (its 12-byte raw timestamp frame is outside the string-frame grammar; disposition on #1168 — the framing defect itself is unfiled; task 5.6 files it).
  `DerivedEntityID` truncates the digest to the family's `InstanceBytes` and refuses a family declaring 0 or >64
  (revision finding N5 — `web-observation` declares 16). **New exported `pkg/types` surface — flagged for owner
  design review; drafting is not approval.**
- **BREAKING:** `agentrun.Mint(ctx, mgr, org, platform, originEntityID)` — the `rootLoopID` parameter is deleted;
  the stored-origin comparison stays and its error names neither identity (#1174 absorbed).
- **The run entity ID is carried, never recomputed.** Additive wire fields `RunEntityID`
  (`json:"run_entity_id,omitempty"`) on `TaskMessage`, `LoopEntity`, and `UserMessage`; `TaskMessage.Validate`
  requires both-or-neither; the rule engine sets `task.RunEntityID` from the run `Mint` returned; the loop stamps
  `agent.run.entity-id` verbatim from `Task.RunEntityID`; handlers, loop_wire, tool metadata, and events read the
  carried value. `RunID` keeps naming the root loop's bare identifier and its `AGENT_LOOPS` record (O-1
  recommendation (b) — pending owner ruling), which leaves `agentic-terminal-events` untouched.
- **BREAKING (named capability loss, pending owner ruling):** `agentrun.ResolveRun`, `LoopTripleReader`,
  `nats_reader.go`, `AgentRun.RunID()`, `runIDFromChainEntityID`, `agentic.ChainExecutionEntityID`, and
  `agentic.TryChainExecutionEntityID` are deleted; the milestone subscriber keeps the wire fast path only, so
  `NewMilestoneSubscriber(mgr, logger)` / `NewMilestoneSubscriberWithRunStateReader(runs, logger)` change arity.
  Dispatch can no longer resolve a paused run from a bare `RunID`: `run_entity_id` becomes the echo token of the
  gh#256 resume contract (`agentic/user_types.go:42-51` comment rewritten), and dispatch rejects ONLY a submission
  carrying `RunID` without `RunEntityID`, naming the field.
- **BREAKING:** the agent-run family joins the fixed-suffix table and becomes the longest member (FixedBytes 88),
  so `MaxAuthorityPairBytes()` tightens 170 → **168** and the declared pair bound tightens 163 → **161**
  (mechanical: `config/config.go:818` derives it). `agent.run.origin-entity-id` gains its first readers as the one
  run→loop pointer.
- The loop/run suffix-index collision ends as a query-visible side effect: today the loop
  `…agentic-loop.agent.execution.<uuid>` and its run `…chain.agent.execution.<uuid>` write identical
  `ENTITY_SUFFIX_INDEX` keys (`processor/graph-ingest/component.go:2776-2812` — same instance, same `execution`
  type token), so `graph.query.bySuffix` on a loop UUID is last-write-wins nondeterministic and removing either
  entity deletes the other's index entry; distinct instances end both defects. E2e asserts both resolutions
  (task 5.2; round-1 D6).
- The corpus audit gains `derived_family_composed`: production Go composing positions 3–5 of a derived family
  outside the family table's file is a finding.

## Non-goals

- No knobs, no compatibility shims, no dual paths, no legacy reader: pre-v1 one-time break under ADR-102 d7, fresh
  storage only (owner constraint, 2026-08-31).
- The import-lane admission fact (#1194), the anchor-append bug (#1193), the #1187 deletions, the #1186 surface.
- The graph alert path: `NewAlertEvent` and `alertInstance` are unchanged (disposition on #1168; the alert framing defect files separately — task 5.6).
- Editing sister repositories; impacts land in `docs/operations/migration-beta162-to-beta163.md` (READ-ONLY rule).

## Adopter seam inventory (for what this change adds)

Answered as a component/config author outside this repo who has never opened `agentrun.go`.

- **What must they know?** ONE fact: the run entity ID is a value you receive and carry — `run_entity_id` on the
  wire, `agent.run.entity-id` on the loop, `agent.run.origin-entity-id` on the run — never a value you compute.
  Before this change they had to know the derivation formula, the segment order, AND the instance semantics; the
  deleted builders are the deleted debt. The one residual debt with a name: a client resuming a paused run must
  echo `run_entity_id`, not only `run_id` (gh#256 contract).
- **What happens if they do nothing?** A sister that keeps recomputing composes an identity that names NOTHING —
  and three of the four carrier classes fail SILENTLY (a rule-pack composed subject resolves to a non-existent
  entity; a TS slice returns a 64-hex digest where a loop UUID was expected). This is why the migration note's
  obligation table exists and why dispatch's reject-on-`RunID`-without-`RunEntityID` is loud by design. In-repo,
  the recompute is impossible at compile time (the builders are gone) and the audit catches a reintroduction —
  with ONE measured exception where meaning, not compilation, changes: `read_loop_result`'s `normalizeLoopID`
  (`processor/agentic-tools/loop_result.go:181-186`) strips any full entity ID to its final segment, and its
  parameter doc (`:72`) promises "the full 6-part entity ID also works"; a run entity passed there stops
  resolving the loop and fails not-found naming the wrong cause. The affordance loss is accepted and the doc
  corrected — no new knob (task 5.1; round-1 M2).
- **Where do they find out?** Compile error (deleted symbols, changed arity) > typed validation error (dispatch
  names the missing field) > audit finding > migration note. The silent class lives only OUTSIDE the compiler's
  reach (rule packs, TS) — exactly what the migration note's obligation 12 (`docs/operations/migration-beta162-to-beta163.md:849`) covers, and the residual finding this
  design accepts and records rather than closes (a non-Go carrier has no loud path; #1197 rules this a
  migration-note obligation, never a design gate).
- **What SHOULD they have to know?** Nothing — and after this change, nothing is what remains for the read path:
  observation (carry what the framework handed you) replaces prediction (recompute what you hope it derived).

## Premises (re-pinned; each measurable, measurement in `inventory.md`)

| # | Premise | Measurement |
|---|---|---|
| P1 | Mint derives the instance from the bare token; slice B refuses, does not coexist | `agentrun.go:290,315-318` |
| P2 | `agent.run.origin-entity-id` is written by Mint and read by no production code | `inventory.md` §Searches |
| P3 | `AgentRun.RunID()` has zero production callers; `ResolveRun` has none outside agentrun | `inventory.md` §Searches |
| P4 | Exactly two framed-digest homes exist; trigger frames are byte-identical to the primitive | `graph/events.go:315`, `graph_event_identity.go:44` |
| P5 | `web-observation` declares `InstanceBytes: 16`, so an unconditional 64-hex derivation is silently wrong for it | `framework_identity_families.go:28`, `web_observation_entity.go:234` |
| P6 | Agent-run FixedBytes = 5+5+5+9+64 = 88 > rule-trigger 86, so the budget derives to 168 and the declared bound to 161 | `framework_identity_families.go:65`, `config/config.go:818` |
| P7 | Wire carriage already exists everywhere the value must ride (events, tool metadata, AGENT_LOOPS record fields) — the three ADDED fields complete it | `agentic/events.go`, `tools.go:400`, `state.go:59` |
| P8 | Four sister carrier classes, all on the pre-#1119 order; one (semteams `01b`) has no token that yields a digest and no substitute until #1193 | sister pass, `inventory.md` §Searches |
| P9 | `agentic-terminal-events` walks `AGENT_LOOPS/<RunID>` on the loop plane and is untouched under O-1(b) | its spec `:348-393` |

## Alternatives (recorded; one sentence each — the salvage's A-table)

**A0** do nothing: keeps a loud refusal of legitimate foreign work and thirteen homes of a prediction. **A2**
export the derivation so sisters keep computing it: the owner steer's rejected shape — every site still predicts.
**A3** make `RunID` itself the digest: breaks the loop-plane `AGENT_LOOPS/<RunID>` contract for no identity gain.
**A1 (recommended, the recorded design):** digest over the full origin, one home on the family seam, readers read
the carried value.

## Capabilities touched

`entity-id-contract` (MODIFIED ×1, ADDED ×1), `graph-ingest` (MODIFIED ×1).

## Coordination and gates

- **Codex is holding for this issue** (owner comment, 2026-08-30): #1146/#1155 (PRs #1159/#1156) re-adopt the very
  AgentRun records this change re-keys; #1154 is the same class one layer out. Implementation does not start until
  the sequencing against those PRs is settled on the issue (tasks §2).
- **The four unruled decisions** (O-1, the `ResolveRun` capability loss, `DerivedEntityID` truncation, the budget
  tightening) are framed on #1192 for the owner; implementation waits for the rulings. A judge answer or this
  proposal is never the ruling.
- **Breaking change ⇒ e2e gate:** `task e2e:agentic` green before the breaking commit lands on main (HARD RULE);
  `task e2e:core` re-run because the budget numbers move under every tier's authority fixtures.
