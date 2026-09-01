# ADR-105: A Loop Instance Token Is a Framework-Minted v4 UUID, Enforced at the Mint Seams

## Status

**Accepted (2026-09-01)** — owner rulings on #1192: comments `5481478395`, `5481998272`, `5494522256`, and the
2026-09-01 ruling comment closing the Codex review of PR #1210 (form-vs-provenance narrowed, seam census narrowed
to four, ADR accepted). Scope ruled 2026-08-31 ("everyone who mints a loop uses uuid" — graph-research included,
A1); implements "enforce UUID at the mint seams"; supersedes the unmerged framed-digest ADR-105 draft (PR #1210
history, `b0e92253`). ADR-104's budget figures (170 effective / 163 declared) stand unamended. Mechanics live in
`entity-id-contract` and `graph-ingest`.

## Carve-out: a loop token is NOT an authorization token

**A loop instance token confers control of its loop to any holder, and this ADR does not change that.** Enforcement
here is canonical FORM, not provenance — `internal/looptoken.Valid` cannot tell a framework mint from a fresh UUID
a client authored, and a client that supplies one is accepted. More importantly, provenance is the wrong axis:
a second party echoing another user's framework-minted token *verbatim* honors "echo, never author" to the letter
and still takes over the loop's tracker entry, redirects its completion routing, and overwrites its in-flight
context. Perfect mint-provenance would close none of that; the missing control is authorization at the seam that
ATTACHES to a loop, filed as **#1227**.

Multi-user IS a supported pre-v1 configuration — `Permissions.SubmitTask` is a per-user list that accepts `"*"`.
Therefore: **multi-tenant deployments MUST NOT rely on loop tokens for isolation until #1227 lands.** The token's
only protection today is UUID unguessability (2^122), which is a mitigation, not a contract — the token is
returned to clients on every response and keys the AGENT_LOOPS record, the loop-execution entity, and the run
instance, so it appears on surfaces a second party may legitimately read. Seams outside the four enforced ones
(`UserSignal`, `ApprovalResponse`, uncensused control requests) accept non-canonical tokens today: **#1228**.

## Context

The loop instance token is the identity plane the agentic substrate keys on: the `AGENT_LOOPS` record, the
loop-execution graph entity, and — permanently, under this ruling — the run entity's instance. Every mint path is
framework-owned. Two mint spellings truncated a v4 UUID to 8 hex characters (`loop_` + uuid[:8] in dispatch,
`rg_` + uuid[:8] in graph-research): 32 bits of entropy, where the birthday bound reaches ~1% collision
probability at ~9,300 loops and 50% at ~77,000. A dispatch collision was SILENT — `CreateLoopWithID` overwrites
the colliding loop's record and context manager, merging two conversations. A full canonical v4 UUID carries 122
random bits (~5.3 × 10^36 values); at any plausible loop volume the collision probability is not worth a design.
These numbers are recorded here so collision odds are never re-litigated.

Cross-authority: two imported loops sharing an instance token collapse to one local run ID; #1148 made the second
mint a loud refusal. The framed-digest design would have re-keyed the run entity so such runs coexist; the owner
ruled instead that the collision only exists for non-UUID tokens, and the framework mints every token.

**Annotation (2026-09-01, Codex review B1).** That last premise — "the framework mints every token" — is the
contract asked of adopters, NOT a property this design enforces: enforcement is FORM only, so a client-authored
canonical UUID is accepted. The no-re-key rationale does not rest on it. What the rationale actually rests on is
collision math over the 122-bit space plus a loud backstop for a token this deployment did not mint, and that
backstop is enforced: `agentrun.Mint` compares the STORED `agent.run.origin-entity-id` against the requested one
on the already-exists path and refuses a mismatch with a classified error (`agentic/agentrun/agentrun.go:332`,
the #1148 check).

## Decision

1. **A loop instance token is a framework-minted v4 UUID**, carried in canonical RFC 4122 text form (36 bytes,
   lowercase, hyphenated). No component, config, client, tool, or injected generator authors one — stated as the
   contract asked of adopters, which the framework does not verify; enforcement is form only (see the carve-out).
2. **Enforcement lives at the mint seams**, not in a registry or family-table mechanism: task validation
   (`TaskMessage.Validate` — every loop-token field the task carries: `loop_id`, `parent_loop_id`,
   `in_reply_to`, `run_id` — enforced at rule-engine publish and loop intake), `LoopManager.CreateLoopWithID`,
   `agentrun.Mint`, and dispatch's continuation intake (synchronous on the HTTP path, via the response subject on
   the channel path, validating the resolved token). The research pipeline's injectable generator option is
   deleted rather than validated. One
   module-internal predicate (`internal/looptoken`); zero adopter-facing surface. Seams validate form, not
   version bits; minting is v4. The import lane (#1194) inherits the same check for the loop-execution family.
3. **No digest re-key.** The run entity's instance remains the root loop's UUID; `ResolveRun`, `RunID`, the
   gh#256 echo contract, and the authority-pair budget are untouched. The #1148 origin-mismatch refusal remains
   the loud backstop for a copied token.

## Consequences

- BREAKING, in the beta.163 wave: dispatch loop IDs change shape (`loop_xxxxxxxx` → full UUID), research loop IDs
  likewise (`rg_xxxxxxxx` → full UUID), and a client-supplied non-UUID loop token (`reply_to`, `loop_id`,
  `parent_loop_id`, `in_reply_to`, `run_id`) is refused — a typed error at dispatch (synchronous on HTTP,
  response-subject on the channel path), a classified terminated delivery at stream intake.
  `graphresearch.WithResearchGraphIDGenerator` (zero production consumers) is deleted. Pre-v1 fresh state
  (ADR-102 d7): no alias, no dual format, no legacy reader.
- Sisters are unaffected in code: the bounded 2026-08-31 pass found zero sites authoring loop tokens or branching
  on their shape — the contract for adopters is "echo, never author."
- The loop/run `ENTITY_SUFFIX_INDEX` collision (#1212) becomes permanent (run instance == loop UUID by design)
  and is that issue's own work.
- The `rg_`/`loop_` operator-glanceability affordance is retired; the loop's role field and predicates carry that
  distinction.

## Alternatives rejected

- Framed-digest re-key of run identity (superseded by the ruling: re-keys a plane to fix a token defect).
- The family-table / exported-derivation mechanism (new exported surface for a grammar one predicate states).
- Dispatch-only enforcement with a research carve-out (leaves a live 32-bit surface and an exception clause in
  the contract).
- Do nothing (keeps a silent conversation-merge bug and an unvalidated client mint lane).

## Cross-repo contract

An adopter conforms when it treats every loop token as an opaque value it received from the framework — echoed in
`reply_to`, tool calls, and queries verbatim — and authors none. Anything composing or truncating a loop token is
nonconforming.
