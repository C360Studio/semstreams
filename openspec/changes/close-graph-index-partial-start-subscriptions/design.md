# Design: Prove and correct graph-index partial-Start subscription ownership

Status: owner accepted on 2026-08-22.

The accepted design is `docs/proposals/gh989-graph-index-failed-start-design.md`, SHA-256
`d8574c69664734ead58e7c81b31787cfb6a880a180440cfef2fce1207d6008bc`. Its verbatim accepted inventory prefix is
SHA-256 `00de531276a2e13e5509bae8168d83794b36947c2a0211829481ad6cbe1f2b1f`.

## Decision

Apply only the accepted minimal owner-local correction plus protocol proof.

### Parent-aware failed-Start rollback

In `processor/graph-index/component.go`:

- remove the graph-index-local failed-Start cleanup timeout;
- preserve the exact Start parent before deriving the runtime context;
- invoke `lifecyclecleanup.RollbackFailedStart(parent, rollback)` from the deferred failed-Start path; and
- preserve synchronous cleanup, joined Start/cleanup errors, retained authority on incomplete rollback, and handle
  clearing only after complete rollback.

The rollback continues to drain every acquired query subscription while callback authority is live, then cancel, then
join `runDone`, coalescer completion, and dispatcher/pool completion. Persistent catalog buckets are not deleted and
cleanup is never detached.

### Private acquisition seam

Add only this package-private field to graph-index Component:

```go
subscribeForRequests func(
    context.Context,
    string,
    func(context.Context, []byte) ([]byte, error),
) (*natsclient.Subscription, error)
```

`setupQueryHandlers` uses the field when nonnil and otherwise calls `c.natsClient.SubscribeForRequests`. The factory
and configuration do not initialize or expose it. The eight existing subjects and handlers remain in their current
byte-for-byte order.

### Fixed causal boundary

The proof fails acquisition 2, `graph.index.query.incoming`, after acquisition 1,
`graph.index.query.outgoing`, succeeds.

One focused graph-index test file proves:

1. an admitted outgoing callback completes under live Start authority before failed Start returns;
2. a callback that exceeds the canonical five-second rollback budget leaves the exact outgoing subscription and
   cleanup authority retained, rejects another Start, and is later cleaned by `Stop(callerCtx)`;
3. after successful rollback, a direct same-instance Start acquires the canonical eight subjects once, serves exactly
   one outgoing response, and leaves no responder after Stop; and
4. focused tests are race-clean and use channel synchronization rather than arbitrary sleeps.

Generic native Drain/rejoin semantics remain owned by existing `natsclient.Subscription` tests. Existing manager
coverage remains sufficient for manager retained-cleanup authority; no service test or mechanism is added.

## Boundaries

Issue #986, implemented by PR #997 / commit `81178583`, remains the boot-only/fail-closed composition boundary.
PR #999 / commit `c84a9de7` remains the source of the existing failed-Start ownership mechanics. This change fills only
the graph-index proof gap and parent-context-policy divergence.

The implementation exports nothing and requires no adopter knowledge. It introduces no communication path,
orchestration, payload, remote query operation, persona, prompt, model call, or runtime agent.

## Verification evidence

Final repository verification is green:

- `task lint` and `go test -race ./...` passed.
- `task schema:generate` passed with zero tracked drift under `schemas/` or `specs/`.
- `git diff --check` and strict OpenSpec validation passed.
- The first full `task test:integration` run had one unrelated timing-ceiling failure in the existing
  `processor/agentic-tools TestIntegration_ToolConcurrentExecution` assertion (`955.9ms < 800ms`) while graph-index
  passed. The exact isolated test then passed, and the complete canonical integration suite passed on rerun.

Independent SemStreams review returned `FINAL APPROVE` with no findings. The reviewer independently reran the focused
graph-index causal tests, repository race suite, lint, diff check, and strict OpenSpec validation.

This evidence closes only the accepted graph-index partial-Start subscription boundary. It makes no adjacent
lifecycle, manager, sibling-owner, adopter, LLM, persona, prompt, model-call, or runtime-agent completion claim.

Landing: PR #1040 (`43dbf6fb`, 2026-08-22). Archived in PR #1071; spec sync: graph-index requirement appended.
