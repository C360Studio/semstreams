# Tasks — establish-authority-read-and-recovery (GS-01)

> **UNAPPROVED — DESIGN-ONLY.** Inventory and revision-13 design passed their independent reviews. Only section 5
> owner decision is executable next. Do not create a spec delta, implementation task, runtime code, or promotion
> evidence before explicit owner acceptance.

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

The reviewed artifact is `/private/tmp/gs01-design-revision13.txt`, SHA-256
`24f99453d108d4f8dd3b9b9879e7a0083a9ed6adc2eaf74bd3b5f3e124ff2103`, at clean checkpoint
`52dc5e3031131dda0a3a55c4de252b2df9d3d8fc`. Independent review recorded `DESIGN REVIEW PASS` with no findings.

Current documentation evidence has `git diff --check`, the 120-character Markdown limit, targeted strict OpenSpec
validation, and complete strict OpenSpec validation green without a capability delta.
