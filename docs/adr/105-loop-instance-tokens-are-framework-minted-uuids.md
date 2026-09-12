# ADR-105: A Loop Instance Token Is Minted at Its Framework Birth Seam as a v4 UUID

## Status

**Accepted (2026-09-01)** — owner rulings on #1192: comments `5481478395`, `5481998272`, `5494522256`, and the
2026-09-01 ruling comment closing the Codex review of PR #1210 (form-vs-provenance narrowed, seam census narrowed
to four, ADR accepted). Scope ruled 2026-08-31 ("everyone who mints a loop uses uuid" — graph-research included,
A1); implements "enforce UUID at the mint seams"; supersedes the unmerged framed-digest ADR-105 draft (PR #1210
history, `b0e92253`). ADR-104's budget figures (170 effective / 163 declared) stand unamended. Mechanics live in
`entity-id-contract` and `graph-ingest`.

**Amended (2026-09-07)** — #1146 comment `5575482141` corrects task-birth ownership after restart review proved that
an empty retained TaskMessage lets agentic-loop mint a second identity after replacement. A direct TaskMessage
producer is now the framework birth seam; it mints before validation and marshal. The amendment adds no exported
helper, constructor, configurable generator, provenance enforcement, or recovery state.

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
loop-execution graph entity, and — permanently, under this ruling — the run entity's instance. Every birth mint seam
is framework-owned. For durable TaskMessage work, the producer is that seam; a component that directly constructs and
publishes the payload participates in framework task production. Two mint spellings truncated a v4 UUID to 8 hex
characters (`loop_` + uuid[:8] in dispatch,
`rg_` + uuid[:8] in graph-research): 32 bits of entropy, where the birthday bound reaches ~1% collision
probability at ~9,300 loops and 50% at ~77,000. A dispatch collision was SILENT — `CreateLoopWithID` overwrites
the colliding loop's record and context manager, merging two conversations. A full canonical v4 UUID carries 122
random bits (~5.3 × 10^36 values); at any plausible loop volume the collision probability is not worth a design.
These numbers are recorded here so collision odds are never re-litigated.

Cross-authority: two imported loops sharing an instance token collapse to one local run ID; #1148 made the second
mint a loud refusal. The framed-digest design would have re-keyed the run entity so such runs coexist; the owner
ruled instead that the collision only exists for non-UUID tokens, and the framework mints every token.

**Annotation (amended 2026-09-07).** The premise is now precise: each framework birth seam mints the token, including a
direct TaskMessage producer before durable publication. This remains a contract asked of producers, NOT a property
the design enforces: enforcement is FORM only, so a client-authored
canonical UUID is accepted. The no-re-key rationale does not rest on it. What the rationale actually rests on is
collision math over the 122-bit space plus a loud backstop for a token this deployment did not mint, and that
backstop is enforced: `agentrun.Mint` compares the STORED `agent.run.origin-entity-id` against the requested one
on the already-exists path and refuses a mismatch with a classified error (`agentic/agentrun/agentrun.go:332`,
the #1148 check).

## Decision

1. **A loop instance token is minted at its framework birth seam as a v4 UUID**, carried in canonical RFC 4122 text
   form (36 bytes, lowercase, hyphenated). For newly published TaskMessage work, its producer is that seam and mints
   locally before validation and marshal; a continuation producer echoes the admitted existing token. End-user input,
   config, and tool-call arguments do not choose a birth token. A direct component producer participates in the
   framework seam. This is the contract asked of producers and is not provenance the framework can verify.
2. **Enforcement lives at accepting seams**, not in a registry or family-table mechanism: `TaskMessage.Validate`
   requires LoopID and validates every loop-token field the task carries (`loop_id`, `parent_loop_id`, `in_reply_to`,
   `run_id`); rule publish, loop intake, and direct HandleTask boundaries classify failure before side effects;
   `LoopManager.CreateLoopWithID`,
   `agentrun.Mint`, and dispatch's continuation intake (synchronous on the HTTP path, via the response subject on
   the channel path, validating the resolved token). The research pipeline's injectable generator option is
   deleted rather than validated. One
   module-internal predicate (`internal/looptoken`); zero adopter-facing surface. Seams validate form, not
   version bits; minting is v4. The import lane (#1194) inherits the same check for the loop-execution family.
   Task intake never repairs absence through minting, derivation, scan, map, ledger, bucket, or another owner.
   Producer-local `uuid.NewString()` is the task-birth seam; no exported helper or constructor wraps it.
3. **No digest re-key.** The run entity's instance remains the root loop's UUID; `ResolveRun`, `RunID`, the
   gh#256 echo contract, and the authority-pair budget are untouched. The #1148 origin-mismatch refusal remains
   the loud backstop for a copied token.

## Consequences

- BREAKING, in the beta.163 wave: every TaskMessage requires nonempty canonical LoopID before publication; dispatch
  loop IDs change shape (`loop_xxxxxxxx` → full UUID), research loop IDs likewise (`rg_xxxxxxxx` → full UUID), and a
  client-supplied non-UUID loop token (`reply_to`, `loop_id`,
  `parent_loop_id`, `in_reply_to`, `run_id`) is refused — a typed error at dispatch (synchronous on HTTP,
  response-subject on the channel path), a classified terminated delivery at stream intake.
  `graphresearch.WithResearchGraphIDGenerator` (zero production consumers) is deleted. Pre-v1 fresh state
  (ADR-102 d7): no alias, no dual format, no legacy reader.
- Direct sister producers are affected: the 2026-09-07 sweep found eight constructors omitting LoopID and five using
  prefixed NUID tokens. They migrate in their owning repositories using the SemStreams migration note. Rule-JSON
  adopters require no sister code change because the in-repo rule producer owns that birth seam.
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

An adopter directly producing new TaskMessage work conforms when it mints one canonical v4 UUID before validation and
marshal and retains the same serialized bytes when retrying that publication. A continuation producer echoes the
admitted existing LoopID. Clients that do not produce TaskMessage still treat loop tokens as opaque values received
from the framework. Anything composing, truncating, or asking the consumer to repair a missing token is nonconforming.
