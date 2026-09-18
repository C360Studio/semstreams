# Change: durable loop authority and port-owned loop bucket

## Why

Dispatch kept a second model of loop state in process memory. `LoopTracker` (583 lines) was populated by consuming
`agent.created` and `agent.approval_pending`, and every loop read — `/loops`, `/activity`, `/debug/state`,
auto-continue, the approval gate, command ownership — asked that cache rather than the KV record agentic-loop
actually owns. A replaced process has an empty cache and cannot ask for those notifications again: they were
acknowledged by the process that died. The cache therefore reported "no such loop" for loops that plainly existed,
and a tracker that lagged or diverged could answer a question the durable record would have refused.

The second defect is a bucket named twice. agentic-loop took its loop-state bucket from `Config.LoopsBucket`
(`loops_bucket`) *and* declared a `loops` KV-write output port. Two declarations of one fact drift, and the config
key silently won — so a deployment could point its port at one bucket and write to another.

## What changes

- **Dispatch reads authority, never memory.** `LoopTracker`, its constructors, `Component.LoopTracker()` and the
  `agent.created` / `agent.approval_pending` consumers are deleted. One caught-up graph view over `AGENT_LOOPS`
  serves listing, activity, debug and auto-continue; explicit LoopID operations exact-read the record. An
  unavailable view answers 503 rather than a false empty list.
- **The approval gate reads the record at decision time.** CallID comes from validated `PendingApproval` state on
  every decision. Unreadable or incoherent authority refuses as unavailable; a record no longer awaiting approval
  refuses as conflict; nothing dispatch does mutates the record, so a failed publish stays retryable.
- **The `loops` output port owns bucket selection.** `Config.LoopsBucket` and `loops_bucket` are removed, and any
  supplied `loops_bucket` fails configuration admission naming the retired key and its replacement — including
  when its value equals the port's bucket.
- **The bucket's policy is observed before work.** Startup acquires the bucket and refuses one whose observed
  policy is not History 10 / TTL 24h / MaxAge 24h / MaxBytes <= 0. No reconciliation, no repair.
- **Approval waits are bounded.** `approval_timeout` defaults to 12h and may not exceed it; empty, malformed, zero,
  negative and longer values fail admission instead of being clamped.

## Impact

- Affected capabilities: `agentic-dispatch`, `agentic-loop`.
- Affected code: `processor/agentic-dispatch/loop_tracker.go` (deleted), `http.go`, `http_activity.go`,
  `loop_admission.go`, `loop_info.go`, `loop_wire.go`, `terminal_settlement.go`, `component.go`,
  `processor/agentic-loop/internal/loopbucket/acquire.go`, `processor/agentic-loop/config.go`, `component.go`,
  `frameworkcapabilities/graphresearch/register.go`, nine shipped `configs/**` fixtures,
  `schemas/agentic-{loop,dispatch}.v1.json`, `specs/openapi.v3.yaml`, `pkg/graphview/view.go`.
- **BREAKING, twice.** `LoopTracker` and `CommandContext.LoopTracker` are removed with no alias;
  `graphview.View.Restart()` is removed. `loops_bucket` on agentic-loop is refused. An `approval_timeout` above 12h
  no longer starts. A loop bucket at another policy is refused at startup. Migration:
  `docs/operations/migration-beta162-to-beta163.md`.

## Non-goals

- **No recovery logic.** Redelivery reconciliation from durable applied facts is L4 (#1330).
- **No new bucket, subject, metric family or recovery service.** `semstreams_router_active_loops` is removed with
  no authoritative replacement count; `/loops` is the answer while its view is ready.
- **No repair of a non-conforming bucket.** Refusal is the whole behaviour; a retained deployment that needs an
  upgrade gets its own reviewed plan.
