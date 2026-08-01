# Design — durable-task-claims (gh#807)

## Context

See `proposal.md` for motivation. Facts the approach leans on, verified at `c05a11fb`:

- The only task dedupe is `HasActiveLoopForTask` (`processor/agentic-loop/state.go:166-178`):
  an O(n) scan over the in-process `LoopManager.loops` map, filtered by `!IsTerminal()`. The
  manager is 21 in-memory maps; no KV read exists on the dedupe path.
- `AGENT_LOOPS` keys are `<loopID>` and `COMPLETE_<loopID>`; `TaskID` exists only inside the
  serialized `LoopEntity`. Nothing durable maps task→loop.
- Publish order in the spawn path is: spawn-identity graph write (`component.go:1050`) →
  initial `agent.request` publish (`:1083`) → `AGENT_LOOPS` persist (`:1086`). So **a
  KV-persisted loop implies its initial request was already published** — the invariant the
  recovery protocol below relies on.
- `pendingTaskResults` (`component.go:86-90`) already implements resume-after-NAK, but
  process-locally.
- `kv.Create` maps conflict to `natsclient.ErrKVKeyExists` (`natsclient/kv.go:198-216`);
  graph-ingest's strict create (`mutations.go:589`, "exactly one winner") and graphresearch's
  `CreateLoopEntity` are the in-repo claim precedents. gated-dag ADR-070 B1 is the
  claim-before-dispatch + `Nats-Msg-Id` + rollback precedent.
- `inflight.go:154-156` records why a `state=running` loop entry cannot serve as a claim: only
  a handler transitions a loop out of running, so a crashed process leaves a stale running
  entry indistinguishable from live work. The claim must therefore be a separate, write-once
  fact, not derived loop state.
- `RequestID` is already structured `loopID:req:<short>` (`state.go:928-934`) precisely so
  identity survives in-memory map loss.
- `model/wire.ClientConfig.ExtraHeaders` exists but is applied per-endpoint at construction
  (`client.go:38-40`, `:189-195`; mirrored in `responses/client.go`) — it cannot carry a
  per-request idempotency key without a request-path signature change.

## Goals / Non-Goals

**Goals:**

- One durable, atomic authority for "this TaskID has been accepted, as this LoopID, with this
  initial RequestID" that survives process restart, AGENT message eviction, NATS restart, and
  operator purge of the AGENT stream.
- A recovery protocol such that a crash at any point between claim and loop persist is healed
  by JetStream redelivery without minting new identity or re-executing completed work.
- Honest bounds: the residual double-provider-call window is named, measured against the
  duplicates window, and closed fully only by the staged provider idempotency hand-off.

**Non-Goals** (design-level; proposal Non-goals also apply):

- No claim leases, no expiry-triggered re-execution, no fencing tokens — a task claim is
  write-once and permanent for its retention horizon. (ADR-070 rejected lease semantics for
  gated-dag claims; the same reasoning holds here.)
- No change to `LoopManager`'s in-memory model; the claim gates acceptance, it does not
  replace loop state.

## Decisions

### D1 — Claim store: a new KV bucket, `AGENT_TASK_CLAIMS`, key = TaskID, write-once `Create`

Alternatives considered:

- *Prefix keys in `AGENT_LOOPS`* (e.g. `TASK_<taskID>`): rejected. `agentic-tools`'
  `flow_monitor` and `read_loop_result` scan that bucket by prefix conventions today; a third
  key family raises every scanner's filter burden, and claim retention (long) diverges from
  loop retention.
- *Stream-layer dedupe only* (stamp `Nats-Msg-Id`, widen `duplicates`): rejected as the
  authority — bytes retention is exactly the failure mode gh#807 names. Kept as
  defense-in-depth (D5).
- *A graph-entity claim* (gated-dag-style predicate claim): rejected — task acceptance is
  operational state, not a domain fact; the state-ownership table places operational results
  in component-specific KV. The graph CAS surface is gh#689/gh#851's territory.

The claim value is one JSON record: `{task_id, loop_id, request_id, task_hash, claimed_at,
claimant}`. Write-once: no update path exists, deliberately — any mutable field would recreate
the stale-`running` ambiguity `inflight.go` documents. Execution state stays in `AGENT_LOOPS`;
the claim is pure identity.

### D2 — Claim before side effect; the loser adopts the winner's identity

`handleTaskMessage` order becomes: decode → preflight → **mint LoopID + initial RequestID →
`Create` claim** → existing spawn path (graph write → publish → persist). On
`ErrKVKeyExists`:

1. `Get` the claim. Hash mismatch → typed rejection (D4). Hash match:
2. Loop present in `AGENT_LOOPS` (or in-memory) → short-circuit, return the claim's LoopID —
   today's dedupe answer, now durable, and **now also covering terminal loops**: a completed
   task redelivered is acknowledged as complete, not re-executed. This is an intentional
   semantic change; it is the at-most-once acceptance gh#807 asks for.
3. Loop absent everywhere → **resume**: create the loop under the claim's LoopID, publish the
   initial request under the claim's RequestID, persist. This heals the
   crash-between-claim-and-persist window on redelivery, generalizing what
   `pendingTaskResults` does process-locally.

Because persist happens after publish, "loop in KV" implies "request published"; the resume
republication is reached only when the original publish may not have happened. A republication
racing a slow original collapses server-side via D5's `Nats-Msg-Id`. The residual: a crash
after publish, before persist, with redelivery arriving **beyond** the duplicates window,
yields two `agent.request` messages with the *same RequestID*. That window is the named bound
(Risks) and the provider idempotency stage (D6) is its closure.

### D3 — Claim retention: bucket MaxAge, default well beyond every replay horizon

Claims are ~200 bytes; retention is cheap. Default `max_age` for `AGENT_TASK_CLAIMS` is **7
days** vs AGENT's 24h — a claim must outlive every path that can redeliver its task, with
margin. Expiry re-opens the at-most-once window by policy; that is acceptable and documented
because a TaskID redelivered after 7 days is outside every supported replay horizon (AGENT
retention + restart recovery). Per ADR-068 the *live graph* never uses NATS TTL for lifecycle;
this is operational KV (the same class as `OWNER_PRESENCE`'s TTL), not graph state.

### D4 — Same TaskID, different bytes: typed rejection over a canonical hash basis

The claim stores a hash of the task's **work-defining content**: the `TaskMessage` payload
with volatile envelope fields (timestamp, trace metadata) zeroed. The exact zeroed field set
is pinned by the spec delta's scenario and a round-trip test, not prose. Identical canonical
bytes → idempotent accept (D2 paths 2/3). Different canonical bytes under a claimed TaskID →
stable classified rejection (`errs.ErrorInvalid` + a new stable code), because silently
executing different work under an already-claimed identity is the same corruption class the
claim exists to prevent. Producers that intend new work mint a new TaskID (SemMachina's
recovery-generation framing already does exactly this).

### D5 — Stream-layer defense: `Nats-Msg-Id` everywhere, explicit `duplicates` window

All three task publishers stamp `Nats-Msg-Id = TaskID` (via the existing
`PublishToStreamWithMsgID`); the initial `agent.request` publish stamps
`Nats-Msg-Id = <initial RequestID>`. The AGENT stream declaration gains an explicit
`duplicates` window (default candidate: `10m` — covers restart-scale gaps, bounded by
`MaxAge`; the config knob exists at `config/streams.go:43-54` and is currently unset by every
stream in the repo). Correctness does not depend on this layer; the double-publish residual in
D2 shrinks with it.

### D6 — Provider idempotency: staged, separable, keyed by claimed RequestID

The claimed initial RequestID is the durable identity providers with idempotency-key support
should receive. Carrying it requires a per-request header on the wire clients — a request-path
signature change in `model/wire` (both chat and responses clients). Staged as its own task
group so the claim contract ships without blocking on a wire-surface change; until it lands,
gh#807's "universal at-most-once billing remains out of scope" bound stands, now narrowed to
exactly the D2 residual.

## Risks / Trade-offs

- **[Residual double provider call]** crash after publish, before persist, redelivery beyond
  the duplicates window → two `agent.request` with one RequestID. → Mitigation: explicit
  `duplicates` window (D5) shrinks it; provider idempotency (D6) closes it; the claim record
  makes the duplicate *observable* (same RequestID) where today it is silent (fresh IDs).
- **[Claim/loop drift]** claim exists, loop creation fails permanently (e.g. fatal validation
  after claim) → TaskID is claimed but never executes. → Mitigation: claim is written *after*
  preflight validation so validation failures precede claiming; post-claim failures are
  transient-NAK'd and healed by redelivery through D2 path 3. A metric
  (`task_claims_orphaned` gauge derived on resume attempts) makes the remainder visible.
- **[Terminal short-circuit surprises a producer]** a producer relying on re-delivering a
  completed TaskID to re-execute now gets a no-op. → Mitigation: this is the contract gh#807
  requests; changelog entry + the typed short-circuit result names the existing LoopID so the
  producer can read the completion from `COMPLETE_<loopID>`.
- **[Hash-basis drift]** a new volatile envelope field appears and lands in the hash → replay
  of identical work rejected as different-bytes. → Mitigation: the canonical-basis test
  round-trips a task through publish/decode and asserts hash stability; the field set is one
  pinned function.
- **[Bucket proliferation]** one more KV bucket to provision/monitor. → Accepted: retention
  and access patterns genuinely differ from `AGENT_LOOPS`; declared in the same configs.

## Migration Plan

Additive; no breaking API change. New bucket declared in `configs/agentic.json` (and the e2e
config); components create-or-bind it at start like `AGENT_LOOPS`. No feature flag: the claim
is the contract, and an advisory mode would reproduce the observe-only trap this repo has
paid for repeatedly. Rollback = revert; claims left behind are inert (nothing reads them when
the code is reverted, and MaxAge clears them).

Deploys upgrading mid-stream: tasks published before the upgrade carry no claim; their first
post-upgrade redelivery claims normally (D2 path 1 or 3). No backfill needed.

## Open Questions

- Exact default for the claims bucket `max_age` (7d proposed) and the AGENT `duplicates`
  window (10m proposed) — operator-tunable either way; defaults can settle in review.
- Whether the D6 wire-client signature lands as a context value, a per-call options struct, or
  a request-struct field — deferrable; does not change the claim contract or task breakdown
  (D6 is its own group).
