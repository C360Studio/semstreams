# gh#1192 — loop-token UUID enforcement: premises, re-pinned at re-materialization

Successor to the framed-digest inventory (this directory's previous content at `b0e92253`), re-derived for the
2026-08-31 owner ruling on #1192: "D1: enforce UUID at the mint seams" — no digest re-key. Every pin re-measured
at `base:`; what each pin means lives in §Adjacent claims (the verifier's strict grammar takes pure pins only).
The adopter seam inventory lives in `proposal.md`.

base: ae35f296d6660f1d5987d53f4f4b2c8dde1caa9d

## Design premises

- `processor/agentic-dispatch/http.go:306` — `		loopID = "loop_" + uuid.New().String()[:8]`
- `processor/agentic-dispatch/component.go:884` — `		loopID = "loop_" + uuid.New().String()[:8]`
- `frameworkcapabilities/graphresearch/executor.go:39` — `const loopIDPrefix = "rg_"`
- `frameworkcapabilities/graphresearch/executor.go:145` — `			return loopIDPrefix + id[:8]`
- `frameworkcapabilities/graphresearch/executor.go:251` — `	loopID := e.newLoopID()`
- `frameworkcapabilities/graphresearch/executor.go:104` — `func WithResearchGraphIDGenerator(gen func() string) ResearchGraphOption {`
- `processor/agentic-loop/state.go:137` — `func (m *LoopManager) GenerateLoopID() string {`
- `processor/agentic-loop/state.go:130` — `	loopID := m.GenerateLoopID()`
- `processor/agentic-loop/state.go:142` — `func (m *LoopManager) CreateLoopWithID(loopID, taskID, role, model string, maxIterations ...int) (string, error) {`
- `processor/agentic-loop/state.go:154` — `	m.loops[loopID] = &entity`
- `processor/agentic-loop/handlers.go:834` — `		loopID, err = h.loopManager.CreateLoopWithID(task.LoopID, task.TaskID, task.Role, task.Model, effectiveMaxIterations)`
- `processor/agentic-loop/component.go:1314` — `	if task.LoopID == "" {`
- `processor/agentic-loop/component.go:1300` — `	if err := task.Validate(); err != nil {`
- `agentic/user_types.go:357` — `func (t TaskMessage) Validate() error {`
- `agentic/user_types.go:37` — `	ReplyTo          string            `json:"reply_to,omitempty"`           // loop_id if continuing`
- `processor/agentic-dispatch/http.go:298` — `	if msg.ReplyTo != "" {`
- `processor/agentic-dispatch/component.go:876` — `	if msg.ReplyTo != "" {`
- `processor/rule/actions.go:1713` — `	task := agentic.TaskMessage{`
- `processor/rule/actions.go:1885` — `	if err := task.Validate(); err != nil {`
- `agentic/agentrun/agentrun.go:281` — `func Mint(`
- `processor/rule/actions.go:1918` — `		if _, mintErr := agentrun.Mint(ctx, e.lifecycle,`
- `configs/rules/deep-research/02-collect-evidence.json:27` — `        "loop_id": "$entity.id",`
- `processor/agentic-tools/loop_result.go:181` — `func normalizeLoopID(loopID string) string {`

## Adjacent claims

- The claimed gap is THREE non-UUID mint spellings, not the ruling's one: dispatch `http.go:306` and its channel-path
  twin `component.go:884` (both `"loop_" + uuid[:8]`, 32 bits), and graph-research `executor.go:39,145`
  (`"rg_" + id[:8]`; its `:140-142` comment concedes the odds; its KV `ErrKVKeyExists` catch at `:324` does not
  cover the loop-execution graph entity or the suffix index, #1212's class). `executor.go:104`
  (`WithResearchGraphIDGenerator`) is an exported generator knob — an injected generator bypasses any mint-side
  fix, so validation must observe the OUTPUT at the use site (`:251`).
- Compliant mints: `GenerateLoopID` (`state.go:137`, `:138` = `uuid.NewString()`); the reserve path fills empty
  LoopID via the same call (`component.go:1314-1315`); the rule engine sets NO LoopID at all (`actions.go:1713`,
  fields `:1714-1721`), so rule-spawned loops always get a fresh framework UUID.
- The local collision mechanism is SILENT: `CreateLoopWithID` (`state.go:142`) has no shape or existence check and
  `state.go:154` overwrites the colliding loop's record and context manager — two conversations merge.
- The loud lane vs the swallowed lane: loop intake validates via `task.Validate()` (`component.go:1300`) and
  rejects with metric + `natsclient.TerminateDelivery` (`component.go:1174-1179`); a `CreateLoopWithID` error
  surfacing later in `HandleTask` is logged and ACKed — `component.go:1188-1191` returns nil, no metric. This is
  why the refusal's one home is `TaskMessage.Validate` (`user_types.go:357`, no LoopID check today), enforced
  producer-side by the rule engine (`actions.go:1885`) and consumer-side at intake, with `CreateLoopWithID` as
  defense in depth.
- Client-supplied tokens: `ReplyTo` (`user_types.go:37`) becomes `task.LoopID` unvalidated via `http.go:298` /
  `component.go:876`.
- `agentrun.Mint` (`agentrun.go:281`): `:287-289` empty-origin refusal; `:290` builds the run ID from
  `rootLoopID`; `:315-318` origin-mismatch refusal (#1148 backstop — STAYS). Its only production caller is
  `run_scope=new` (`actions.go:1918-1919`); a Mint failure degrades to spawn-without-run, logged Error
  (`:1920-1929`) — the new refusal inherits that declared degrade.
- `configs/rules/deep-research/02-collect-evidence.json:27` is an update_kv REFERENCE payload (ADR-028), not a
  mint; no config surface authors loop IDs.
- `processor/agentic-tools/loop_result.go:181` (`normalizeLoopID`) is a shape-agnostic reader; its `:72` "bare
  UUID" description becomes MORE true under enforcement; untouched.
- #1148 / `300e57fe` — origin-mismatch refusal stays as backstop; the ruling confirms it.
- #1212 — the loop/run `ENTITY_SUFFIX_INDEX` collision is PERMANENT under this ruling (run instance == loop
  UUID); adjacent, not absorbed.
- #1194 — the import lane inherits the token check for the loop-execution family when it lands; coordination only.
- #1174 — Mint's mismatch error text (`agentrun.go:316-318`) is NOT touched by this scope; recommendation: drop
  `Closes #1174` (see proposal).
- #1146/#1155 (Codex) — recorded hold reason gone (nothing re-keyed); remaining contact = one added refusal
  inside Mint.
- ADR-104 (Accepted) — budget figures 170/163 stand unamended; `openspec/specs/entity-id-contract/spec.md:517`
  unchanged.
- `openspec/specs/graph-ingest/spec.md:1059-1065` — owns Mint's refusal behavior today; its closing sentence
  points at closed #1168 (stale) — MODIFIED here.
- `openspec/specs/agentic-terminal-events` — `AGENT_LOOPS/<RunID>` walk unaffected (RunID stays the root loop
  UUID).
- Superseded framed-digest package (`b0e92253`, this directory + `docs/adr/105-*.md` draft) — replaced by this
  change; design record survives in PR #1210 history.

## The consumer at birth

No new exported symbol, port, subject, bucket, or config field. The one new code home, `internal/looptoken`
(module-internal, invisible to adopters), is consumed at birth by five seams: `agentic.TaskMessage.Validate`,
`LoopManager.CreateLoopWithID`, `agentrun.Mint`, graph-research's generator-output check, dispatch's `reply_to`
check. Same-class collision table: not triggered — no new durable, communication, or runtime-coordination
primitive is proposed (a validation predicate owns no state, no channel, no coordination).

## Searches

- `git grep -n 'GenerateLoopID|CreateLoopWithID' -- '*.go'` (non-test) → the pinned sites; no other creation path.
- `git grep -n '"loop_"' -- '*.go'` (non-test) → exactly the two dispatch mints. `git grep -n '"rg_'` →
  executor.go:39, predicates.go:61 comment, e2e scenario.go:448.
- `git grep -nE '\.LoopID = ' -- '*.go'` (non-test) → the pinned mints + echoes (tool results copy
  `call.LoopID`; payload decode).
- `git grep -n 'uuid.New' -- processor/agentic-dispatch processor/agentic-loop agentic/` (non-test) → loop-token
  truncations only at the pinned mints; `state.go:951,959` truncations are request/tool-call SUB-IDs
  (`loopID:req:<short>`), not loop identities — out of scope.
- `git grep -n 'run_scope' -- configs/` → EMPTY: no shipped config mints runs, so the Mint refusal regresses
  nothing in-tree.
- Loop-token shape reliance in-tree: `test/e2e/scenarios/research-graph/scenario.go:448` (`HasPrefix(k, "rg_")`),
  `test/e2e/scenarios/agentic/scenario_test.go:74,86,114` (`LoopID: "loop-1"` fixtures), docs prose
  (`docs/advanced/08-agentic-components.md:458,515,528` `loop_xyz789`; `configs/rules/research-graph/README.md:57`;
  `agentic/research/predicates.go:61`). No production branch on shape.
- Sister pass (read-only, one bounded pass, 2026-08-31, `/Users/coby/Code/c360/{semteams,semsource,semdev}`,
  grep only): ZERO sites author loop IDs or branch on the `loop_`/`rg_` shape. semteams echoes framework-minted
  values (`ui/src/lib/services/agentApi.ts:293` `in_reply_to`; `cmd/semteams/chainpause/decision_handler.go:154`
  validates an echoed `failed_loop_id` via `TryLoopExecutionEntityID` — shape-agnostic); shape appears only in
  comments/fixtures (`ui/src/lib/stores/taskRefs.svelte.ts:3`; `ui/src/lib/services/messageLoggerApi.ts:60`;
  `configs/personas/fragments/researcher-research-synthesize/00-identity.md:40`). semsource: zero hits
  (near-matches were `org_1` literals). semdev: reads `loop_id` opaquely from tool calls (`internal/tools/*`).
  Sizes the migration note at "no action required"; gates nothing.
