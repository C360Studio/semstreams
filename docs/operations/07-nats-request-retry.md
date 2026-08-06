# NATS Request and Retry Policy

Request/reply failure is not one category. Choose the call shape from the operation's commit semantics and lifecycle
context, not from whether the subject happens to be called a query or mutation.

## Decision table

| Call class | API | Retry policy |
|---|---|---|
| Canonical graph mutation | `RequestClassified` through the graph mutation client | Exactly one transport attempt |
| Steady-state query | `RequestClassified` | Exactly one attempt; surface timeout |
| Cold-start/readiness probe | `RequestReadyClassified` | Short probes within one bounded readiness budget |
| Explicitly idempotent non-graph request | `RequestWithRetryClassified` | Contract-authorized redelivery only |

Do not select `RequestWithRetryClassified` merely because a call writes state. Data-level convergence does not prove
that replaying an ambiguous request is safe or that the second response describes the first attempt.

## Canonical graph mutations

Create, reconcile, append, and delete always use one `RequestClassified` transport attempt. The typed graph client
preserves what the transport can prove:

- A definite validation, not-found, exists, or revision-mismatch response means no write occurred.
- `unavailable` means NATS reported no responder, so the request was not delivered.
- `deadline` means the context was already done before send, so the request was not delivered.
- `commit_unknown` covers a post-send timeout or disconnect and a malformed or semantically invalid success reply.
  The request may have committed, but no valid reply proves its result.
- Matching state found later proves only current state; it does not prove which request authored it.

The framework never automatically retries a graph mutation. A component may choose a new attempt after a definite
non-commit. For example, after `revision_mismatch` it can exact-read, recompute the complete desired state, and issue
one new reconcile according to its domain policy. It must not automatically retry `commit_unknown`.

## Steady-state queries

Use `RequestClassified` and surface timeout. Retrying a hung responder turns a useful failure signal into a longer
latency spike and multiplies load on an unhealthy dependency.

```go
data, err := natsClient.RequestClassified(ctx, querySubject, request, queryTimeout)
```

The caller decides whether to fall back, retry at a higher application boundary, or report the failure.

## Cold-start and readiness-gated reads

A component's initial reconcile or post-reconnect read may run before its responder subscribes. Use
`RequestReadyClassified` only at that outer lifecycle boundary:

```go
data, err := natsClient.RequestReadyClassified(
    ctx,
    querySubject,
    request,
    natsclient.DefaultReadinessProbeTimeout,
    natsclient.DefaultReadinessBudget,
)
```

The helper uses short probes within a bounded total budget. A received reply, including a classified handler error,
stops the loop. It retries silence because absence may surface as either no-responders or timeout depending on server
configuration. On budget exhaustion, callers should emit an actionable signal naming the dependency.

Only the outer lifecycle-triggered reader uses this helper. A request handler forwarding to another responder remains
single-attempt; nesting readiness loops hides steady-state failures and double-counts caller budgets.

## Explicitly idempotent non-graph requests

`RequestWithRetryClassified` remains available for a non-graph operation whose own contract explicitly authorizes
redelivery after transport ambiguity. The responder must be safe if the first attempt committed and only its reply was
lost. This is a per-operation decision, not a package default.

Before using it, document:

1. the stable logical request identity;
2. why duplicate delivery cannot duplicate externally visible effects;
3. which errors are retried and the bounded budget; and
4. how operators distinguish exhaustion from a handler rejection.

If those facts are not explicit, use `RequestClassified` and let the component own the next action.

## Classified replies

Use classified request APIs for new code. Plain `Request` and `RequestWithHeaders` do not inspect response error
headers and can misdecode an error body as success. `RequestClassified`, `RequestReadyClassified`, and the explicitly
authorized retry variant all preserve handler class/code plus transport errors.
