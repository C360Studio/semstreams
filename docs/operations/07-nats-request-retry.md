# NATS Request vs RequestWithRetry — When to use which

## TL;DR

```
If the call writes state (mutation):
  → use RequestWithRetry with DefaultRetryConfig
  → MUST be idempotent on the responder side

If the call reads state (query):
  → use bare Request
  → timeout = real signal; surface to the caller
```

This rule is permanent. Don't make ad-hoc choices per call site.

## Why the distinction matters

`natsClient.RequestWithRetry(..., DefaultRetryConfig())` retries 3
times with 100ms → 2s exponential backoff on **any error** —
including "no responders" (subscriber not yet subscribed) and
timeout (responder hung, slow, or genuinely broken).

For a **mutation** that's idempotent at the data level (writing the
same triple twice → same graph state), retry-on-any-error is
exactly the right policy. The dominant failure mode is "graph
responder isn't subscribed yet" during startup races, restarts, or
NATS reconnects. Without retry, the mutation silently fails, the
caller logs-and-swallows, downstream readers see incomplete state,
and nobody notices until a graph audit reveals the gap.

For a **query**, retry-on-any-error is wrong. A genuinely hung
responder turns a 5s timeout into ~12.7s of wallclock retry storm.
That masks "responder is broken" as latency, breaks alerting, and
delays user-visible error surfacing. The right behavior for a
query is: timeout → return the error → caller decides whether to
retry, fall back, or surface to the user.

## Idempotency rule (mutation responders)

Any responder that handles a request through `RequestWithRetry`
**must** be safe receiving the same request twice. The retry path
will re-deliver to the responder if the first attempt's response
gets lost (e.g., subscriber crash between processing and reply).

For graph-mutation responders this is structural: the graph is a
set of triples, so adding `(s, p, o)` twice = same state, removing
already-removed = no-op success. New mutation responders should
preserve this property — if a responder needs to count requests or
log every receive, add a request-ID dedup cache before relying on
the retry path.

## Concrete shape

```go
// MUTATION: retry on transient failures
respData, err := natsClient.RequestWithRetry(
    ctx, mutationSubject, reqData, mutationTimeout,
    natsclient.DefaultRetryConfig(),
)
```

```go
// QUERY: no retry, surface timeout
respData, err := natsClient.Request(
    ctx, querySubject, reqData, queryTimeout,
)
```

## Current call-site survey (2026-04-28, beta.20)

### Mutations (using RequestWithRetry)

- `processor/agentic-loop/graph_writer.go` — trajectory triples
- `processor/rule/triple_mutator.go` — rule-driven add_triple /
  remove_triple actions
- `graph/inference/applier.go` — inference-emitted triples
- `processor/agentic-tools/decide.go` — decide tool's terminal
  triple (coordinator pattern)
- `examples/github-pr-workflow/component.go` — example mutation

### Queries (using bare Request)

- `processor/graph-query/*` — all graph queries
- `graph/query/client.go` — graph-index queries
- `processor/graph-query/pathrag.go` — including the
  `graph.ingest.query.entity` existence check (pure read)
- `processor/graph-clustering/similarity.go` — similarity query
- `gateway/http/http.go`, `gateway/graph-gateway/component.go` —
  gateway query proxies
- `graph/llm/nats_content_fetcher.go` — content fetch

If you find a new call site that doesn't fit either bucket, the
default is to surface it: open a ticket with the call site path,
the subject, and what state (if any) it writes.

## History

The pattern was inconsistent through beta.19. Beta.20 codified the
rule and audited every site. The original incident:
`feedback_approval_required_gap.md`'s sibling memory
`feedback_nats_request_retry_audit.md` has the full story.
