# Proposal — contract-bound-claim (gh#689)

## Why

`processor/gated-dag/claim.go` marshals raw `graph.mutation.entity.update_with_triples` wire
requests with a locally re-declared subject constant, leaves `ExpectedRevision` and
`OwnerToken` deliberately empty, performs an **unconditional** `Unclaim` that can clear
another worker's claim, and has no ambiguity recovery — a timed-out claim that actually
committed is indistinguishable from a failed one (`claim.go:44-110`;
`executor.go:400-450`). Mutual exclusion rests entirely on single-flight + one-instance
deployment (ADR-046 invariant #1), which is narrower than the reusable gated-DAG primitive
should promise near v1.

The framework has meanwhile grown every ingredient except the primitive itself:
`ownership.ModeCASTransition` is declarable on a contract but **no client method exists for
it**; the server CAS branch, the typed `revision_mismatch` sentinel, receipts carrying the
committed `KVRevision`, and the four-state `CommitState` taxonomy all ship today; and ADR-056
names the missing piece explicitly — Owned-State Reconcile "optionally under a CAS condition"
(`056:1801-1819`). The `entity-read-with-revision` change (gh#851) supplies the last
ingredient, the public revision-bearing read. `claim.go`'s own `CAS-UPGRADE POINT` comment is
the recorded design intent this change discharges.

## What Changes

- **A CAS-scoped claim capability in `pkg/projection`** — the client method for
  `ModeCASTransition`: claim a predicate bound to a declared contract group, transported with
  the bound owner token, conditioned on the authoritative revision from the revision-bearing
  read. The receipt distinguishes not-committed, committed, and unknown; **unknown is
  resolved by authoritative read-back** (did my claim value land?) so a lost response never
  leaves the caller guessing whether dispatch may proceed.
- **Claimant identity in the claim value**: the claim triple's object carries the claimant,
  making **conditional unclaim** possible — an unclaim verifies, under the same CAS
  condition, that the claim it clears is its own; one worker cannot clear another's claim.
- **`processor/gated-dag` migrates onto the primitive**: `claim.go` no longer marshals graph
  mutation wire requests or owns mutation subject constants (gh#689's acceptance);
  ADR-046 invariant #1's single-flight assumption relaxes from load-bearing to
  defense-in-depth, and the `CAS-UPGRADE POINT` comment retires.
- **Contract and concurrency semantics specified**: two concurrent claimers of one
  unit/revision cannot both receive committed success; a stale owner token fails with the
  public typed error; lost-response behavior is pinned by tests.

## Capabilities

### New Capabilities

_None — this completes the projection client and hardens gated-dag's existing contract._

### Modified Capabilities

- `projection-mutation-client`: the capability set gains the CAS claim interface; claim,
  ambiguity-resolution, and conditional-unclaim requirements added.
- `gated-dag-dispatch`: the claim path is contract-bound and CAS-conditioned; rollback
  (unclaim) becomes conditional; ambiguous claim outcomes are resolved by read-back before
  re-selection.

## Impact

- `pkg/projection` — new capability interface + methods; `processor/gated-dag` — `claim.go`
  rewrite onto the client, `doc.go` invariant amendment, executor ambiguity handling.
- Depends on `entity-read-with-revision` (gh#851) landing first; its spec deltas touch the
  same `projection-mutation-client` requirements, so this change's deltas are written against
  the post-#851 text and MUST be rebased if #851's wording shifts before archive.
- Owner-lease enforcement remains default-off during rollout: revision-CAS alone provides the
  mutual exclusion promised here; the owner token adds cross-incarnation fencing that
  strengthens as the fail-closed lease rollout completes (already tracked by the spec's
  rollout requirement). Stated honestly in the design.
- Consumers: **gated-dag** (in-repo), **SemMachina** (active-active ladder advancement rides
  the same primitive per gh#851's "reusable CAS surface" question), **SemDragon** (#313-era
  ownership rollout).

## Non-goals

- **Lease/expiry semantics.** ADR-070 explicitly rejected timer-based claim re-dispatch; a
  claim clears by conditional unclaim or by the stranded-unit detector, never by TTL.
- **Migrating other raw-write lanes** (rules, `graph_writer`, research, `agentrun`) — those
  remain tracked by gh#688/gh#690 per the spec's PR #696 boundary requirement.
- **The revision-bearing read itself** — gh#851's change.
