# OpenSpec Change Discipline

OpenSpec records a reviewable contract change. It is not the program backlog,
an execution journal, or a container for every related cleanup discovered while
implementing a change.

## Choose the right durable record

- Use an OpenSpec change when behavior or an adopter-visible contract will change.
- Use a GitHub issue for sequencing, investigation, proof-only work, release
  coordination, or a defect whose target behavior is not yet decided.
- Use an ADR for a durable architectural decision with meaningful alternatives
  and consequences.
- Use tests and migration documentation as evidence for behavior that has landed.

An issue may coordinate several OpenSpec changes. An OpenSpec task list must not
become the coordinator for an open-ended program.

## Keep changes reviewable

A change should describe one coherent behavioral outcome. Split it when work
crosses independently reviewable owner families, capabilities, or adopter
contracts. Related work is not automatically the same change.

Tasks are a short completion checklist. Do not append investigation logs,
historical wave plans, speculative proofs, or unrelated defects. A deferred item
leaves the change and receives its own issue; it does not remain as an unchecked
task that prevents truthful completion.

## Reconcile scope immediately

When implementation or owner direction changes the target:

1. Compare the proposal, design, tasks, and deltas with the code that actually
   landed.
2. Remove unimplemented claims from the change. Do not promote aspiration into
   baseline current truth.
3. Move still-justified follow-up work to bounded issues. Open a new OpenSpec
   change later only when that issue has an approved behavioral delta.
4. Record supersession on historical execution artifacts that would otherwise
   appear authoritative.
5. Validate, review, and archive the reconciled change promptly.

Strict CLI validation proves document shape, not repository truth. Archive review
must cite implementation evidence for every promoted requirement.

## Completion gate

Before archive:

- every promoted requirement describes current behavior;
- every task remaining in scope is complete;
- excluded work has no implied completion claim;
- migration and observability documentation matches the supported surface;
- required tests and independent review are green.

If those statements are not all true, the change is not ready to archive.
