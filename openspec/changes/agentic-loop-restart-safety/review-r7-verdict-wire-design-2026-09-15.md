# R7 governance wire design review — 2026-09-15

## Scope and verdict

Independent SemStreams reviewer: **DESIGN REVIEW PASS** for the exact bounded wire contract.
Baseline: `c347eff487f50b93bc338d764f43ef5b5ea5e133` plus preserved R6/R7/R8 work.

Reviewed design: `design-r7-verdict-wire-2026-09-15.md`, SHA-256
`91480e9cd53dba53c74f388fdf0ab04e6f1687fa33c3e7493eb10469740524c4`.
Its unchanged inventory appendices retain their independent INVENTORY PASS; see
`review-r7-verdict-wire-inventory-2026-09-15.md`.

This is pre-owner design review, not runtime implementation approval or R7 completion.
The accepted wire boundary and bounded reading rulings remain:

- https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5677581095
- https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5677867478

## Findings and resolution

The first draft (`4f8c9a5a20ee65a5dbe32e35fb1f2b619441e7023ef695d1fd832eb728c09874`) had two HIGH findings:

1. Optional RuleID projection did not define how publish's nested property reached the typed value.
   The architect specified nonempty top-level string, otherwise Properties string, otherwise empty, independently
   for Reason and RuleID. Diagnostic disagreement follows precedence rather than correlation refusal.
   Prose, spec draft and TDD scenarios now agree; normalization does not modify the caller's map.
2. A finite-table rationale did not justify omitting fuzz coverage for arbitrary external strings/maps/JSON.
   The architect replaced it with one native `FuzzGovernanceVerdictBoundary` target, explicit seeds and invariants
   through existing boundaries: refusal without waiter mutation, unchanged Properties and valid wire/direct
   context equivalence. No new API or test framework is introduced.

The second draft (`1a2ade3f2a8562d36c92e2dd7f3371e09408bff58f262d1ccb59d1e12c5e9666`) resolved both substantively.
One remaining propagation sentence was narrowed from all recognized field shapes to correlation/container fields,
expressly deferring optional diagnostics to the defined projection. The reviewer verified that final correction
and returned DESIGN REVIEW PASS at the exact final hash above.

## Owner gate and remaining work

The existing exported method is proposed to become:

```go
HandleVerdict(payload VerdictPayload) (natsclient.DeliveryDecision, error)
```

VerdictPayload and the return contract already exist. No additional exported operation/type is proposed.
The exact method-signature change still requires owner acceptance before implementation.

After acceptance, promote the reviewed additive spec clause and execute its bounded TDD slice. Preserve existing
R6/R7/R8 WIP, the frozen #1156 parent and #1311's separate source-settlement boundary. R7 and R8 stay unchecked.
The wire fix does not prove retained-verdict lookup, originating-proposal matching, replacement safety or full
source settlement. Those guarantees and the normal implementation/release gates remain open.

At this checkpoint, strict OpenSpec validation and `git diff --check` pass. Production source hashes remain
unchanged from inventory intake. No runtime test, implementation commit/push, rebase, archive, merge or closure
was performed in this review slice.

## Subsequent owner acceptance

The owner subsequently answered “Approved” to the exact signature and bounded implementation/TDD request.
Recorded ruling: https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5678920204.
The reviewed design hash remains unchanged. The additive spec clause is promoted and implementation is authorized;
the review-time pending owner gate above is historical, not a current hold. All remaining proof/release gates stand.
