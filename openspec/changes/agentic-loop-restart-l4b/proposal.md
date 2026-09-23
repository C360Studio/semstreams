# Change: agentic-loop restart safety L4b — approval-lane cold branch, verdict after waiter loss, one terminal owner, route-ambiguity metering

> Change id `agentic-loop-restart-l4b`. It claims #1362 (draft PR #1366) and closes #1155. Base: `c4a79fd5`.
>
> **Accepted design.** No new design pass is owed (owner, #1362 issuecomment-5798319271). The design is
> `openspec/changes/archive/2026-09-23-agentic-loop-durable-applied-facts/design.md`: § 5.4 (the warm re-echo only),
> § 5.5 (D35), § 5.6 (D16, D39) and § 5.7 (D41/P6).
>
> **Rulings applied, none reopened:**
> - #1330 Q1–Q12
> - the 2026-09-18 terminal-adoption ruling on #1330
> - the body and comments of #1362, including the spec-text rulings OQ-A–OQ-F in issuecomment-5799118983
>
> `tasks.md` carries the pins, refreshed to `c4a79fd5`.

## Why

L4a (#1330, PR #1361) did two things. The loop record now names its outstanding request. Redelivered tasks, model
responses and tool results are now classified against that record. The owner split the work on 2026-09-22 (OQ7) and
left four gaps to #1362:

- **Approval answers are lost across a replacement.** An approval answer delivered to a replacement process takes this
  path: `approval_response_handler.go:58` → `:78` → `:194-199`. It is stale-dropped and acknowledged, so the human
  decision is lost. That leaves the #1146 acceptance line unmet: "approval recovery uses existing loop KV plus exact
  retained request/response evidence; confirmed missing evidence durably fails `continuation_unavailable`".
- **Applied verdicts are retried.** When a governance verdict has no waiter and the record is live, the verdict is
  retried (`component.go:3611-3618`). This happens even when the record shows the execution already applied.
- **A redelivered terminal has nothing to adopt.** Three terminal paths each `Put` `COMPLETE_<loopID>`
  (`component.go:2950`, `:2981`, `:3005`). None of them creates the marker once.
- **Route ambiguity is unmetered.** `loop_route_ambiguous` is not metered on the delivery lane (owner ruling
  2026-09-21, docket OQ6).

#1155 has one stage left, the approval-after-restart tier stage. It is task 6.1 of this change.

## What changes

- **Approval lane.**
  - It gains the cold KV branch and `continuation_unavailable`.
  - It publishes before it writes.
  - It gets a warm re-echo of a pending gate.
  - The approval-timeout sweeper moves onto the carrier. L4a already gave the sweeper publish → CAS, so only its home
    changes.
- **Verdict lane.** A waiterless verdict is classified against the record.
- **One terminal owner.** The order becomes marker `Create` → graph stamps → publish → entity `Update`. An existing
  marker is adopted by loop ID and terminal kind. `PendingApproval` is cleared on the terminal transition. The entity
  writes are already CAS `Update`s since L4a; what changes is the marker's create-once and the order.
- **Metering.** `activeLoop` meters `loop_route_ambiguous` on `loop_admission_refusals_total`.
- **Gate order (docket OQ8).** A test settles it, and the PR body records which branch shipped. Under design § 5.5
  step 1, the expected outcome is write → publish for gates (#1362 issuecomment-5799118983, OQ-A).

## Impact

- **Specs.** `agentic-loop` MODIFIED "The loop record names its outstanding request". `agentic-dispatch` has no delta,
  because task 5.1 changes no stated requirement. Its route-ambiguity text at `:506` and `:610-621` is about refusing,
  not metering.
- **Adopters.**
  - The entity's terminal `state` now lands after the terminal event. `COMPLETE_<loopID>` precedes the event on all
    three paths.
  - `continuation_unavailable` is a new failure reason.
  - Both go in a section of `docs/operations/migration-beta162-to-beta163.md`.
  - This is BREAKING for watchers, so `task e2e:agentic` must be green before merge.

## Riders (placed 2026-09-23; not tasks of this change)

Each rider lands inside this PR only under the 7-day / ~100-file breaker. Otherwise it lands directly after this PR.

- #1345, only with a docket whose rows put the document-or-not-supported sentence first.
- #1342: one `onRefused` per silent lane, plus the spec scenario's test.
- #1238: the three carrier refusals made tier-falsifiable.

Links: #1362, #1155, #1146 (epic), #1330 (L4a), #1342, #1238, #1345.
