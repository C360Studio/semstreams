# Design — contract-bound-claim (gh#689)

## Context

See `proposal.md`. Verified at `c05a11fb`: `claim.go` builds
`UpdateEntityWithTriplesRequest{Entity, RemoveTriples: [predicate], AddTriples: [claim
triple]}` with revision and token empty, object = unitID (claimant-anonymous), retries via
`RequestWithRetryClassified`, discards the response's committed `KVRevision`, and `Unclaim`
is the same write minus the add — unconditional. The executor treats every claim error as
not-committed and re-selects; an ambiguous-but-committed claim is later overwritten by the
next claim (`executor.go:400-450`). `contractsRequireHeartbeat` already forces a heartbeating
owner bind for any contract carrying `ModeCASTransition` predicates
(`mutation_client.go:106-131`), so the token plumbing for a claim capability exists; only the
method is missing. `MutationReceipt` already carries `KVRevision` and the four-state
`CommitState`.

## Goals / Non-Goals

**Goals:** the reusable claim primitive with committed/not-committed/unknown honesty;
conditional unclaim; gated-dag off raw wire requests; ADR-056's named missing primitive
closed for the CAS case.

**Non-Goals:** lease/expiry (ADR-070's rejection stands); other raw-write lanes (gh#688/690);
any server-side change — the server CAS branch, sentinel, and receipts are used as shipped.

## Decisions

### D1 — The capability is claim-shaped, not generic CAS-update-shaped

New narrow interface (name candidate: `ClaimTransitioner`) with two methods: claim and
conditional-unclaim over a `ModeCASTransition` group's predicate. A generic "conditional
replace anything" method was rejected: `ReplaceOwned` + `ExpectedRevision` (gh#851) already
covers general conditional replacement; what gated-dag and active-active consumers need is
the *transition* discipline — read → condition → transition → verify — packaged so its
ambiguity handling cannot be skipped. The method resolves the claim predicate through the
bound contract exactly as `ReplaceOwned` resolves groups (contract-bound, gh#689's first
ask), and the owner token rides the existing bound-token path (second ask).

### D2 — Claim value carries the claimant; unclaim is CAS on value + revision

The claim triple's object becomes the claimant identity (the bound owner id — stable across
the claim's lifetime, unlike incarnation). Claim: read-with-revision → refuse locally if a
*different* claimant's claim is resident (typed already-claimed outcome, no wire write) →
conditional update expecting the read revision, adding the claim triple. Unclaim: read
-with-revision → verify the resident claim object is **this claimant** → conditional update
expecting that revision, removing the predicate. A newer/different claim therefore fails the
value check or the revision check — one worker cannot clear another's claim (gh#689's
conditional-unclaim ask) — and unclaim-of-absent is a typed no-op success (idempotent
rollback replay).

### D3 — Unknown outcomes resolve by read-back, never by guessing

On a transport-ambiguous claim (timeout/lost response), the primitive performs the
authoritative read: claim triple present with own claimant ⇒ **committed** (return the
receipt with the read revision); absent or another claimant ⇒ **not-committed**. Only when
the read itself fails does the caller receive `unknown` — and the receipt says so via the
existing `CommitState`, with retry guidance typed, not prose. This is the "lost response must
not leave the executor guessing whether dispatch may proceed" requirement; the executor's
current conflation of ambiguous-with-failed retires with it.

### D4 — Mutual exclusion rests on revision-CAS; the token is fencing, stated honestly

Two claimers racing from one read revision: the server's single-pass CAS commits exactly one;
the loser gets the typed revision conflict. This holds **today**, with lease enforcement
default-off — the token adds cross-incarnation fencing (stale-owner typed error) that
strengthens as the fail-closed lease rollout completes, and the spec words the guarantee
accordingly so the contract does not overclaim during rollout. A stale token under
enforcement fails with the public typed error (gh#689's acceptance), pinned by a test gated
on the enforcement flag.

### D5 — gated-dag migration keeps ADR-070's shape, discharges the upgrade point

`natsClaimer` is replaced by the primitive behind the existing `claimer` interface, so the
executor's claim-before-dispatch and publish-failure rollback (ADR-070 B1) are untouched in
shape; rollback becomes conditional unclaim, and the ambiguous-claim path now read-backs
before re-selection instead of overwriting. `doc.go` invariant #1 is amended: single-flight
remains the *performance* model; correctness no longer depends on one-instance deployment.
The `CAS-UPGRADE POINT` comment is removed in the same diff that fulfills it — a stale
prediction left standing would misdirect the next reader.

## Risks / Trade-offs

- **[Two wire round-trips per claim]** (read + conditional write) where today there is one.
  → Accepted: a dispatch claim brackets an LLM-scale unit of work; correctness over one RTT.
  The read's revision is reused for the unclaim path when rollback follows immediately.
- **[Claimant identity in graph state]** the claim object changes from unitID to claimant.
  → Migration note: resident old-shape claims (object = unitID) are cleared by the stranded
  -unit detector exactly as today; the executor treats them as foreign (unclaimable by value
  check) and lets that detector reap them — no bespoke migration write.
- **[Cross-change coupling]** deltas modify requirements gh#851's change also touches. →
  Sequenced landing (#851 first); this change's delta text is written against post-#851
  wording and task 1.1 re-verifies before archive.

## Migration Plan

Lands after `entity-read-with-revision`. Additive client surface; gated-dag behavior change
is internal to its executor. Old-shape resident claims reaped by the existing stranded-unit
detector (no data migration). Rollback = revert; the primitive has no persistent state of its
own.

## Open Questions

- Interface naming (`ClaimTransitioner` vs folding into a widened CAS capability) — cosmetic,
  settles in review.
