# Tasks — establish-authority-read-and-recovery (GS-01)

> **DESIGN REVIEW PASS — OWNER ACCEPTANCE PENDING.** Inventory and revision-35 review are complete. Section 5 is
> active. Do not create a capability delta, implementation task, runtime code, or promotion evidence before explicit
> owner acceptance.

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

## 3. Design after inventory pass

- [x] 3.1 After recorded `INVENTORY PASS`, produce options and costs, including
      doing nothing and extending an existing owner, then state a measured
      recommendation and adopter-seam effects.
- [x] 3.2 Preserve the reviewed inventory without dropping conflicts or unknowns
      while recording every measured design premise and triggered
      decision-skill outcome.

## 4. Independent pre-owner design review

- [x] 4.1 Have an independent reviewer attempt to refute the design against the
      reviewed inventory, current repository, specs, ADRs, and adopter do-nothing
      path.
- [x] 4.2 Resolve every blocking finding and record `DESIGN REVIEW PASS` without
      presenting that verdict as owner approval.

## 5. Owner decision

- [ ] 5.1 Obtain explicit owner acceptance, rejection, or redirection of the
      independently reviewed design.
- [ ] 5.2 Only after explicit acceptance, amend this same GS-01 change with the
      capability spec deltas and TDD implementation tasks. Until then, runtime
      implementation and spec promotion remain prohibited; do not open a second
      baton or implementation change.

## Validation evidence

Revision 13 is revoked and intermediate corrections remain non-normative evidence. The exact r27/r28/r29/r31/r32/r33/
r34/r35 stack is preserved in `reviewed-recovery-contract-r35.md`. Revision 35 received `DESIGN REVIEW PASS` and
`APPROVE`; owner acceptance remains pending.

Current documentation evidence has `git diff --check`; the 120-character Markdown limit outside immutable exact reviewed
artifact bytes; and targeted plus complete strict OpenSpec validation green without a capability delta.
