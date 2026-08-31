# Change: Loop instance tokens are framework-minted UUIDs, enforced at the mint seams

**Supersedes the framed-digest package** (this directory's content at `b0e92253`) under the owner ruling
2026-08-31, transcribed on #1192: "D1: enforce UUID at the mint seams" — NO digest re-key. D2/D3/D4 of the old
docket are OBE; the budget stays 170 effective / 163 declared (ADR-104 unamended); no new exported surface;
Codex's hold reason (#1146/#1155 re-adopting re-keyed records) is gone. Claim: draft PR #1210, branch
`claude/gh1192-framed-digest-run-identity`. Milestone `v1.0.0-beta.163`. ADR draft: ADR-105 (rewritten).
Premises re-pinned at `main@ae35f296` in `inventory.md`.

## Why

Loop instance tokens are the identity plane everything agentic keys on — `AGENT_LOOPS`, the loop-execution graph
entity, the run entity's instance (which stays the loop UUID under this ruling, permanently — #1212). Every mint
path is framework-owned, and all but three spellings already mint full UUIDs (`GenerateLoopID` =
`uuid.NewString()`, `state.go:137-139`; rule-spawned loops carry no LoopID, `actions.go:1713`). The violators mint
32-bit tokens: dispatch's `"loop_" + uuid[:8]` (`http.go:306`, `component.go:884`) and graph-research's
`"rg_" + uuid[:8]` (`executor.go:39,145`). At 32 bits the birthday odds reach ~1% at ~9.3K loops and 50% at ~77K —
and a dispatch collision is SILENT: `CreateLoopWithID` overwrites the existing record and context manager
(`state.go:154`), merging two conversations. No seam refuses a non-UUID pre-filled token, so a client-authored
`reply_to`/`loop_id` mints whatever it likes (`handlers.go:834`), and `agentrun.Mint` will build a run from any
token (`agentrun.go:290`). The #1148 origin-mismatch refusal stays as the loud backstop for a copied token.

## The ruling's premise, corrected (flagged for owner confirmation)

The ruling named ONE violating mint. Enumeration found the same fact at three sites — the second dispatch spelling
is trivially in scope; **graph-research's `rg_` mint extends the ruling's named list** and is included in the
recommendation below because it is the same family (AGENT_LOOPS record, `agentic-loop.agent.execution.<instance>`
entity), the same 32-bit math, and its exclusion would put a carve-out sentence in the spec contract. RULED (owner,
2026-08-31): "q1 - everyone who mints a loop uses uuid" — A1 confirmed, graph-research in scope.

## What changes

- **BREAKING (wire shape, not Go surface):** dispatch loop IDs become full canonical UUIDs (`loop_xxxxxxxx` →
  36-char UUID) at `http.go:306` and `component.go:884`; research loop IDs likewise (`rg_xxxxxxxx` → UUID), and the
  zero-consumer `WithResearchGraphIDGenerator` option is DELETED (round-2 H2: the spec forbids an adopter-facing
  mint knob; its only caller anywhere is this repo's own test, sisters comment-only) — the one exported-surface
  deletion in this change. No other exported signature changes.
- **Refusal at every accepting seam, one validation home:** a new module-internal `internal/looptoken` predicate
  (canonical RFC 4122 text form — 36 bytes, lowercase, hyphenated; form, not version bits). Enforced at:
  - `agentic.TaskMessage.Validate` — a present, non-canonical loop-token field — `loop_id`, `parent_loop_id`,
    `in_reply_to`, or `run_id` — is invalid (round-2 B2: the gh#256 resume anchors are client-set loop tokens,
    and `agentic/loop_execution_entity.go:136-145` stamps them into triples with a silent half-write on
    `agent.run.entity-id`; validating upstream closes the class). This makes the rule engine
    refuse at publish (`actions.go:1885`) and loop intake refuse with the existing loud lane — intake-rejection
    metric + `TerminateDelivery` (`component.go:1174-1179,1300`) — a classified refusal, never a counted skip (#1197).
  - `LoopManager.CreateLoopWithID` (`state.go:142`) — defense in depth for composed binaries; classified invalid.
  - `agentrun.Mint` (`agentrun.go:281`) — refuses a non-UUID firing-loop instance before building the run entity
    ID; the existing spawn-without-run degrade (`actions.go:1920-1929`) applies and stays logged.
  - dispatch intake (`http.go:298`, `component.go:876`) — a non-canonical continuation token gets a typed error
    response naming the field (synchronous on the HTTP path; on the response subject on the channel path), instead
    of "Task submitted" followed by an async TERM the client never sees. The check runs on the RESOLVED token
    after the auto-continue branch and before the mint, so one check covers `reply_to` and auto-continue
    (round-2 M4).
- **BREAKING:** client-supplied non-UUID loop tokens are refused (pre-v1, fresh state — no legacy tokens exist to
  grandfather; ADR-102 d7, no alias, no dual path).
- Doc/prose sweep: `loop_xyz789` examples, `rg_` prose, `predicates.go:61` comment, research-graph README,
  e2e `rg_` prefix detection replaced by a shape-independent discriminator.

## Non-goals

- NO digest re-key; run instance == loop UUID stands. `ResolveRun`, `RunID()`, `TryChainExecutionEntityID`, the
  gh#256 echo contract, `agentic-terminal-events`, and the 170/163 budget are all untouched.
- #1212 (suffix-index loop/run collision) — permanent under this ruling, its own issue, not absorbed.
- #1194 — the import lane inherits the `internal/looptoken` check for the loop-execution family when it lands;
  named here as coordination, not implemented.
- #1174 — RULED (owner, 2026-08-31): "q2 drop it unless we are fixing it" — this scope does not fix it (the
  Mint edit adds a precondition and never touches the mismatch error text, `agentrun.go:316-318`);
  `Closes #1174` dropped from PR #1210. #1174 stays open on its own.
- `related_loops` lineage references — reads of existing loops, not mints. `parent_loop_id` is NOT excluded:
  round-2 H1 measured the write path composing it through the PANICKING `LoopExecutionEntityID`
  (`agentic/loop_execution_entity.go:130`, reached from a NATS consumer callback) — the earlier premise that
  `Try…` fail-closes there was false — so it joins the validated fields instead.
- The `state.go:154` overwrite-on-existing-ID behavior itself (the continuation lane depends on it); out of scope.

## Options considered

- **A0 — do nothing:** keeps a silent 32-bit conversation-merge bug and an unvalidated client mint lane. Rejected.
- **A1 (recommended) — enforce at every framework mint + every accepting seam** (this proposal).
- **A2 (rejected by the Q1 ruling) — dispatch-only, carve research out:** leaves a live 32-bit surface whose own comment concedes the odds,
  and puts a permanent exception clause in the spec contract. Cheaper by one generator line; costs a carve-out.
- **A3 — framed-digest re-key:** the superseded package; ruled out 2026-08-31.

## Adopter seam inventory

Answered as a client/component author outside this repo who has never opened these files.

- **What must they know?** ONE fact: a loop ID is an opaque token you receive and echo — never author. (Before:
  they could author one and it worked, silently joining the 32-bit plane.)
- **What happens if they do nothing?** A client echoing framework-minted IDs: nothing changes but the shape.
  A client authoring tokens: a typed error naming `reply_to` at dispatch (synchronous on HTTP, response-subject
  on the channel path); a stream producer pre-filling `loop_id`, `parent_loop_id`, `in_reply_to`, or `run_id`
  gets a classified intake rejection (metric + TERM) instead of today's silent adoption — or, for the gh#256
  anchors, a silent triple half-write.
- **Where do they find out?** Typed synchronous error response (dispatch) > classified intake rejection metric +
  terminal delivery (stream lane) > migration note. Nothing lands at "log-only" or "nowhere"; the swallowed
  HandleTask error path (`component.go:1188-1191`) is exactly why the check sits in Validate, upstream of it.
- **What SHOULD they have to know?** Nothing — and after this change, nothing: the framework mints, observes, and
  refuses; the only client verb left is echo. Observation over prediction: no caller computes an identity fact.

## Premises (each measured; pins in `inventory.md`)

| # | Premise | Measurement |
|---|---|---|
| P1 | All loop mint paths are framework-owned; rule engine sets no LoopID | `actions.go:1713`, `state.go:137-139` |
| P2 | Exactly three non-UUID mint spellings exist | `http.go:306`, `component.go:884`, `executor.go:145` |
| P3 | A dispatch collision merges conversations silently | `state.go:154` overwrite; no existence check |
| P4 | No accepting seam validates a pre-filled token; the gh#256 anchors are stamped raw with a silent half-write | `handlers.go:834`, `state.go:142`, `agentrun.go:290`, `user_types.go:357`, `loop_execution_entity.go:136-145` |
| P5 | A HandleTask error is ACKed silently; the loud lane is preflight | `component.go:1188-1191` vs `:1174-1179` |
| P6 | No shipped config mints runs or authors loop IDs | `git grep run_scope -- configs/` empty; `02-collect-evidence.json:27` is an ADR-028 reference |
| P7 | No sister authors or shape-parses loop tokens | sister pass, `inventory.md` §Searches |
| P8 | 32-bit birthday: ~1% at ~9.3K, 50% at ~77K; canonical UUID ≥ 2^122 | arithmetic in ADR-105 §Context |

## Capabilities touched

`entity-id-contract` (ADDED ×1), `graph-ingest` (MODIFIED ×1 — Mint's refusal paragraph + its collision scenario).

## Coordination and gates

- Codex may proceed on #1146/#1155 (owner's word on #1192); remaining contact is one refusal inside Mint.
- **Breaking ⇒ e2e gate:** `task e2e:agentic` (dispatch→loop shape) AND `task e2e:research-graph` (rg_ retirement)
  green before the breaking commit lands. If A2 is chosen instead, the research tier drops from the gate.
