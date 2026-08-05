# Tasks — authority reads and graph-ingest safety (GS-01)

> **REVISION 35 REJECTED — REVISION 36 INVENTORY ACTIVE.** Do not create a replacement design, capability delta,
> implementation task, runtime code, or promotion evidence until revision-36 inventory receives independent pass and
> the later design/owner gates complete.

## 0. Establish the corrected baton

**Baton evidence:** proposed `GRAPH_INGEST_ACTIVE` duplicated existing `GRAPH_STATUS` and graph-ingest semantic
territory. The proposed NATS CLI requirement was withdrawn, prior GS-01 acceptance was revoked/not granted, and this
design-only OpenSpec change became the durable baton with updated architect, reviewer, and canonical program controls.

## 1. Fresh inventory-only handoff

- [x] 1.1 Re-run a repository-first, problem-only inventory at a recorded commit.
      Treat every prompt, briefing, issue, prior design, and named mechanism as a
      hypothesis; enumerate independently before comparison.
- [x] 1.2 Enumerate claimed gaps, all current spellings and owners, adjacent
      claims, present consumers, adopter do-nothing paths, and exact searches for
      empty categories.
- [x] 1.3 For every candidate durable, communication, or runtime-coordination
      primitive, complete the same-class collision table across semantic class,
      owners, catalogs, status, lifecycle, ownership, readers, writers, and
      recovery.
- [x] 1.4 Stop after the inventory-only deliverable. Add no options,
      recommendation, target state, artifact delta, or implementation task.

## 2. Independent inventory review

- [x] 2.1 Have an independent SemStreams reviewer enumerate the repository before
      reading the inventory conclusions, then try to refute the claimed gaps and
      completeness.
- [x] 2.2 Record `INVENTORY PASS` only when the collision inventory and all
      required categories are complete. Any omitted same-class owner is
      `BLOCKING` and returns work to section 1.

## 3. Re-audit scope after revision-35 rejection

- [x] 3.0 Record owner rejection of revision 35 and preserve it as correction evidence.
- [x] 3.0a Record the binding boundary: no SemStreams operational recovery tooling; NATS clusters remain supported;
      edge/offline backup checkpoints are documented operator responsibility.
- [x] 3.1 Materialize revision-36 inventory-only scope loss, current owners, adopter costs, collisions, and searches.
- [x] 3.2 Obtain independent `INVENTORY PASS` on the exact revision-36 inventory.

## 4. Design after revision-36 inventory pass

- [ ] 4.1 After recorded `INVENTORY PASS`, produce options and costs, including
      doing nothing and extending an existing owner, then state a measured
      recommendation and adopter-seam effects.
- [ ] 4.2 Preserve the reviewed inventory without dropping conflicts or unknowns
      while recording every measured design premise and triggered
      decision-skill outcome.

## 5. Independent pre-owner design review

- [ ] 5.1 Have an independent reviewer attempt to refute the design against the
      reviewed inventory, current repository, specs, ADRs, and adopter do-nothing
      path.
- [ ] 5.2 Resolve every blocking finding and record `DESIGN REVIEW PASS` without
      presenting that verdict as owner approval.

## 6. Owner decision

- [ ] 6.1 Obtain explicit owner acceptance, rejection, or redirection of the
      independently reviewed design.
- [ ] 6.2 Only after explicit acceptance, amend this same GS-01 change with the
      capability spec deltas and TDD implementation tasks. Until then, runtime
      implementation and spec promotion remain prohibited; do not open a second
      baton or implementation change.

## Validation evidence

Revision 35 received `DESIGN REVIEW PASS` but was rejected by owner redirection on 2026-08-05. Its exact stack remains
in `reviewed-recovery-contract-r35.md` as correction evidence. `scope-audit-r36.md` is the active inventory-only
handoff.

Current documentation evidence has `git diff --check`; the 120-character Markdown limit outside immutable exact reviewed
artifact bytes; and targeted plus complete strict OpenSpec validation green without a capability delta.
