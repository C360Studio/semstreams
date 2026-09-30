# Graph-query cleanup design review and acceptance

Review baseline: `714bb0fa69d00032c1be0ecf66e952a67cecea49`.
Coordinator acceptance date: 2026-09-30.
Claim: #1433 / draft PR #1434.

## Exact reviewed artifacts

All paths below are relative to this change. These draft artifacts remain unchanged after promotion.

| Artifact | SHA-256 |
|---|---|
| `design-draft.md` | `665367204f83f89c6d063713c1c9c2e5f61252d014a5012b3bc2bbdaf286f819` |
| `drafts/specs/test-cleanup-policy/spec.md` | `929c90e0a7f5783f289f042e5738798925fa223a773d20604cb36b577b492668` |
| `drafts/tasks.md` | `ab7575f1fd3982ad89e7e45ee9e2d91927cf38164a6a5a7d34dde37d87e40d2d` |

## Independent reviewer verdict

The following complete verdict is preserved verbatim from the independent reviewer:

**DESIGN REVIEW PASS** for the three exact drafts at baseline `714bb0fa69d00032c1be0ecf66e952a67cecea49`. Verified hashes `66536720…`, `929c90e0…`, and `ab7575f1…`. No blocking findings.

The design covers all 39 identities, eight ordinary Stop sites and the short-Start case. It preserves early lexical ownership, accepted Start authority, concrete errors, once-only attempts and substrate ordering. The specification change remains a test-fixture contract; no production or exported surface is added.

Implementation review must enforce these existing design obligations:

- **Stop-task ownership:** only the Stop goroutine accesses mutable owner state until it has joined. Failure cleanup releases callback gates and joins that task before the finalizer runs. A timeout alone cannot authorize concurrent finalization.
- **Independent completion evidence:** retain actual callback completion, captured `runtimeDone`, and the exact view’s stopped observation. Owner flags and finite contexts are insufficient.
- **Small, fast proofs:** reuse narrowly selected child execution only for fatal-exit/reporting behavior; keep other histories in-process and terminal expiry under `synctest`. Split child cases if measured race runtime approaches the five-second top-level ceiling.

The proposed native seams exist and support the stated observations without another container or production hooks. Mutation targets address omitted obligations, and the draft correctly distinguishes pre-change behavioral RED from later mutation sensitivity.

This approves the design for coordinator acceptance, not implementation or merge. No tests ran.

## Coordinator acceptance

After the independent DESIGN REVIEW PASS, the root coordinator explicitly accepted this routine private/test-only
pattern adoption on 2026-09-30 within the user's authorized #1433 continuation. This acceptance authorizes the
reviewed implementation to proceed; it does not infer a new production/exported contract, an additional policy
ruling, implementation results, gate success or merge readiness. The review conditions above remain obligations
for implementation and independent implementation review.

## Narrow promotion record

- Preserved all three exact reviewed drafts and their hashes.
- Created `design.md` from `design-draft.md` with only the title and status changed to record coordinator acceptance
  after independent review on 2026-09-30 and implementation next.
- Copied the reviewed specification delta byte-for-byte to this change's `specs/test-cleanup-policy/spec.md`;
  the current capability specification remains unchanged.
- Promoted the tasks draft to `tasks.md`, updating its title, acceptance introduction and closing record wording.
  Marked draft materialization/review/acceptance and requirement/oracle/PBT/mutation selection completed.
  Preserved the two completed inventory tasks; all source, execution, mutation, guard, gate, implementation-review
  and archive tasks remain unchecked.
- Updated only the proposal's Status section to record accepted design and implementation next, with no results.
- Created this review and acceptance record without changing source, accepted inventories or current specifications.

No tests ran during review or document promotion.
