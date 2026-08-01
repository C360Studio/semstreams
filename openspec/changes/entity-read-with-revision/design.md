# Design — entity-read-with-revision (gh#851)

## Context

See `proposal.md`. Verified at `c05a11fb`: the handler already holds the revision it discards
(`query.go:87` does the `Get`; `:101` uses `entry.Revision` for poison bookkeeping; `:104`
returns `entry.Value` alone). The CAS write path, its typed sentinel, and receipt revisions all
exist with `pkg/lifecycle.Manager` as their only caller (`manager.go:732, 972, 1103`).
`MutationReceipt.KVRevision` is already populated on every commit path.

Constraint from the parallel gh#810 rework: `SubscribeForRequests` subjects are becoming a
declared, capture-checked registry. Growing the request/reply surface mid-flight would collide;
this change therefore adds **no new subject**.

## Goals / Non-Goals

**Goals:** the public read half of the CAS contract; the minimal client-side condition
pass-through that makes the existing typed conflict reachable; wire-level proof of the full
loop.

**Non-Goals:** claim semantics, owner-token requirements, ambiguity read-back (gh#689's
change); batch revisions; any change to mutation-side behavior (the server CAS branch is
untouched).

## Decisions

### D1 — Opt-in flag on the existing subject, not a new subject or unconditional envelope

The entity query request gains `include_revision: bool`. When false/absent the reply is the
bare `EntityState` bytes — byte-identical behavior for every existing caller. When true, the
reply is a versioned envelope `{entity, revision}`.

Alternatives: a parallel subject (`…query.entityVersioned`) — rejected: grows the request/reply
surface the gh#810 registry work is fencing, and splits one lane's contract across two
subjects. Unconditional envelope — rejected: breaks every bare-shape consumer in one move
(lockstep cost with no compensating need; gh#851 explicitly permits a parallel/versioned read).

Version skew: an old server ignores the unknown request field and replies bare. The client
MUST treat a bare reply to a revision-requesting call as "revision unavailable" (typed,
distinguishable), never as revision 0 — a zero would be accepted by nothing (the CAS branch
requires `ExpectedRevision > 0`), but a fabricated revision would be worse than an honest
absence.

### D2 — The revision is the entry's, from the same read, no re-fetch

The envelope's `revision` MUST be `entry.Revision()` from the same KV `Get` that produced the
returned bytes — the contract gh#851 states ("identify the exact ENTITY_STATES entry whose
bytes produced the returned entity"). Not-found, stub, and poison behavior are unchanged: the
revision rides the success path only; every existing error contract stays byte-for-byte.

### D3 — Client surface: additive variant + optional condition, no interface break

- `ReadAuthoritative` keeps its signature; a revision-bearing variant returns
  `(*graph.EntityState, uint64, error)`. The `AuthoritativeReader` capability interface is
  extended additively (a second narrow interface, so existing fakes keep compiling — the
  narrow-method-set bridging lesson).
- `ReplaceOwnedMutation.ExpectedRevision uint64` (additive, zero = unconditioned, preserving
  today's behavior exactly). The client passes it through at request build. A non-zero value
  routes the server to its existing CAS branch; mismatch surfaces as the already-mapped
  `MutationRevisionConflict`, whose receipt carries commit-state `not-committed` — the retry
  loop is: refetch (new revision) → rebuild desired state → replace again.

### D4 — Proof drives the production wire

The acceptance test subscribes nothing in-process: it round-trips
`graph.ingest.query.entity` (with and without the flag) and
`graph.mutation.entity.update_with_triples` over NATS against a real graph-ingest component,
runs two competing conditional writers from one read revision, and asserts exactly one
committed success, one typed `MutationRevisionConflict`, and a successful refetch-retry.
Handler-seam tests (the existing `cas_integration_test.go` pattern) do not discharge this —
tests must drive the production wire.

## Risks / Trade-offs

- **[Envelope/bare discrimination]** the versioned reply is a new shape on an old lane. →
  The reply shape is keyed to the request flag (the caller knows what it asked for);
  discrimination-by-key-set is not needed. Decoder tolerates bare replies (D1 skew rule).
- **[Revision as an attractive nuisance]** callers may cache revisions across long gaps and
  see conflicts. → Documented contract: a revision is a *retry token*, not state; the example
  shows the refetch loop, and the typed conflict is the designed signal, not an error to
  eliminate.
- **[gh#810 rebase]** the lane's callers pass through `RequestClassified`, whose error surface
  gains ack-rejection when gh#810 lands. → No merge conflict (different files); rebase and
  re-run integration.

## Migration Plan

Additive on both wire and client; no flag day. Old client + new server: unchanged. New client
+ old server: bare reply → typed "revision unavailable". Rollback = revert; no data shape
persisted.

## Open Questions

_None that change specs, approach, or tasks._ (Batch revisions and the claim primitive are
recorded as Non-goals with owners, not open questions.)
