# gh#1192 — framed-digest run identity: premises, re-pinned at materialization

Salvage re-pin of the recorded design (PR #1178 revision comment Case A blocks;
`docs/proposals/gh1168-federation-identity-inventory.md` at `5967394f`, INVENTORY PASS; pre-cut package
`4c46fbc5`). Owner ruling A (2026-08-30, #1180) admits starting from the recorded inventory; every pin below was
re-measured at `base:`. The full seam inventory and sister census live in `proposal.md` §Premises and §Adopter seam.

base: ae35f296d6660f1d5987d53f4f4b2c8dde1caa9d

## Design premises

- `agentic/agentrun/agentrun.go:290` — `	entityID, err := agentic.TryChainExecutionEntityID(org, platform, rootLoopID)`
- `agentic/agentrun/agentrun.go:315` — `			if run.OriginEntityID != originEntityID {`
- `agentic/agentrun/agentrun.go:146` — `	OriginEntityID string `json:"origin_entity_id,omitempty" lifecycle:"predicate=agent.run.origin-entity-id"``
- `agentic/agentrun/agentrun.go:176` — `	return runIDFromChainEntityID(r.EntityIDField)`
- `agentic/agentrun/agentrun.go:373` — `func ResolveRun(ctx context.Context, runs RunStateReader, reader LoopTripleReader, org, platform, loopID string) (*AgentRun, error) {`
- `graph/events.go:297` — `func alertInstance(sourceEntityID, alertType string, metadata EventMetadata) string {`
- `graph/events.go:315` — `func writeFramedString(destination byteWriter, value string) {`
- `processor/rule/graph_event_identity.go:12` — `const ruleTriggerDigestDomain = "semstreams.graph.rule-trigger.v1"`
- `processor/rule/graph_event_identity.go:44` — `func writeRuleTriggerFrame(destination ruleTriggerWriter, value string) {`
- `vocabulary/agentic/predicates.go:518` — `	RunOriginEntityID = "agent.run.origin-entity-id"`
- `pkg/types/framework_identity_families.go:28` — `	{Name: "web-observation", System: "web", Domain: "agent", Type: "observation", InstanceBytes: 16},`
- `pkg/types/framework_identity_families.go:65` — `	return MaxEntityIDBytes - LongestFrameworkIdentityFamily().FixedBytes()`
- `config/config.go:818` — `	return semtypes.MaxAuthorityPairBytes() - mintedSuffixBytes`
- `agentic/user_types.go:51` — `	RunID string `json:"run_id,omitempty"``
- `agentic/user_types.go:284` — `	RunID    string `json:"run_id,omitempty"``
- `agentic/state.go:59` — `	RunID string `json:"run_id,omitempty"``
- `agentic/entity_ids.go:138` — `func TryChainExecutionEntityID(org, platform, chainID string) (string, error) {`
- `agentic/loop_execution_entity.go:138` — `		if runEntityID, err := TryChainExecutionEntityID(e.Org, e.Platform, e.Task.RunID); err == nil {`
- `agentic/tools.go:400` — `const MetadataKeyRunEntityID = "agent.run_entity_id"`
- `processor/agentic-loop/handlers.go:571` — `	id, err := agentic.TryChainExecutionEntityID(h.platform.Org, h.platform.Platform, runID)`
- `processor/agentic-dispatch/loop_wire.go:76` — `		if id, err := agentic.TryChainExecutionEntityID(org, platform, e.RunID); err == nil {`
- `processor/rule/actions.go:684` — `	if runEntityID, idErr := agentic.TryChainExecutionEntityID(org, platform, firingLoopID); idErr == nil {`
- `processor/rule/actions.go:1918` — `		if _, mintErr := agentrun.Mint(ctx, e.lifecycle,`
- `agentic/web_observation_entity.go:234` — `	sum := sha256.Sum256([]byte(canonicalURL))`
- `test/compat/semteams/agentrun_terminal_compat_test.go:44` — `			LoopID: "loop-success", TaskID: "task-success", RunEntityID: agentic.ChainExecutionEntityID("semteams", "test", "missing-success"),`
- `openspec/specs/graph-ingest/spec.md:1065` — `stored run is this caller's. Making the identity collision-free is out of scope here and is #1168.`
- `openspec/specs/entity-id-contract/spec.md:517` — `family — `256 − 86 = 170` bytes while the rule trigger family (`rules.graph.trigger.` + 64 hex + two separators) is`

## Adjacent claims

- #1148 / `300e57fe` (slice B, merged 2026-08-29) — the SILENT half of the issue's bug is closed on main: `Mint`
  refuses an empty origin and an origin mismatch. This change makes distinct origins COEXIST instead of refusing.
- #1194 — the import-lane collision gate owns `openspec/specs/graph-ingest/spec.md:934-948` (the O-4 DEFERRED
  paragraph). The issue body's citation of those lines for Case A is pre-materialization drift; the Case A deferral
  is at `:1063-1065` and lands via this change's MODIFIED block.
- #1193 — `run_scope=new` appends rather than replaces the run anchor; semteams' `01b` literal-subject rule is its
  workaround and dies with this change (migration note obligation; no substitute until #1193 lands).
- #1187 — the three zero-caller deletions (`FederationMeta` family, `DeploymentPrefix`, `MinimalConfig`). NOT
  `ResolveRun`: its deletion is this change's named capability loss.
- #1186 — environment surface; #1188 — bucket namespacing; neither touched here.
- #1154 — the five agentic types recompute `EntityID()` from wire `Org`/`Platform`; the RUN half (the
  `loop_execution_entity.go:138` derivation) is absorbed here, the rest stays #1154, sequenced after.
- #1146/#1155 (Codex, PRs #1159/#1156) — restart safety re-adopts the very AgentRun records this change re-keys;
  Codex is holding for #1192 (owner comment, 2026-08-30). Sequencing before implementation is a task gate.
- ADR-104 (Accepted) — scope note routes the run-identity half to #1192, undecided there; ADR-102 d2/d5/d7;
  ADR-053 D1/D8 (amended by this change's ADR draft); ADR-076 d2 (bounded, not fixed-length).

## Searches

- `git grep -n "TryChainExecutionEntityID|ChainExecutionEntityID" -- '*.go'` (non-test) → the 8 production
  composer sites pinned above plus the builders and the compat test; this IS the in-repo derivation census.
- `git grep -n "RunOriginEntityID|agent.run.origin-entity-id" -- '*.go'` (non-test) → writers and comments only
  (agentrun lifecycle tag, actions.go log naming, vocabulary registration). ZERO production readers — the issue's
  "written by Mint, read by nobody" claim HOLDS at base.
- `git grep -n "\.RunID()" -- '*.go'` → test files only. `AgentRun.RunID()` has zero production callers.
- `git grep -n "ResolveRun" -- '*.go'` (non-test) → agentrun.go (decl + internal slow path at :634),
  nats_reader.go, one doc comment in terminal_settlement.go:335. No external production caller.
- `git grep -n "run_id|agent\.run|chain\.agent\.execution" openspec/specs/` outside graph-ingest → only
  `agentic-terminal-events` (`AGENT_LOOPS/<RunID>` origin walk, :348-393) — untouched under O-1(b), which is why
  option A3 was rejected.
- `git grep -n "writeFramedString|FramedString|writeRuleTriggerFrame"` → exactly two framed-digest homes
  (graph/events.go alert, processor/rule trigger) + web-observation's unframed sha256-16. Same-class collision
  set for the proposed `DerivedEntityID` home; consolidation target is trigger (byte-identical frames), alert
  stays private (12-byte raw timestamp frame; O-4 disposition, filed defect).
- Sister pass (read-only, one bounded pass, 2026-08-31): semteams `configs/rules/agent-run/
  01b-handoff-marker-redispatch.json:34` (literal composed subject, OLD segment order), `ui/src/lib/stores/
  runStatus.svelte.ts:52,171` + `ui/src/lib/utils/runHealth.ts:136,151-154` (`RUN_INFIX` slicing the bare id),
  `semdev/internal/intake/admission/resolver.go:116,352,357` (prefix composed as a string) — all confirmed, all
  still on pre-#1119 `agent.chain.execution` order. Sizes the migration note; gates nothing (#1197 rule).
