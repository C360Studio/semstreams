# NATS Request vs RequestWithRetry — When to use which

## TL;DR

```
If the call writes state (mutation) AND is idempotent on the responder side:
  → use RequestWithRetry with DefaultRetryConfig

If the call writes state and is NOT idempotent (create-or-fail, claim):
  → one classified attempt per delivery; re-send ONLY on IsNoResponders
  → every other failure is an UNKNOWN outcome — surface it (gh#861)

If the call reads state (query) in STEADY STATE (responder already up):
  → use bare Request / RequestClassified
  → timeout = real signal; surface to the caller

If the call reads state at COLD START / initial reconcile / post-reconnect
(the responder may not be subscribed yet):
  → use RequestReady / RequestReadyClassified
  → short probe timeout + bounded budget
  → "not ready yet" is retried; a hung responder still fails within the budget
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
**steady-state** query is: timeout → return the error → caller
decides whether to retry, fall back, or surface to the user.

## The third bucket: cold-start / readiness-gated reads (gh#420)

The steady-state query rule assumes the responder **exists** — so a
timeout means "hung." That assumption breaks for a read issued at
**cold start**, on a component's **initial reconcile**, or right
after a **NATS reconnect**, where the responder may simply not be
subscribed yet. There, a timeout means "not ready yet," and bare
`Request` degrades badly: whether an absent responder surfaces as a
fast `nats.ErrNoResponders` or a full-timeout hang is **server-config
dependent** (see `request_integration_test.go`), so the read can burn
the entire query timeout before failing — then the caller skips a
pass and waits for a backstop. gh#420 (gated-dag's boot read hanging
the full 30s `query_timeout`) is the canonical case.

`RequestReady` / `RequestReadyClassified` handle this without
reintroducing the query anti-pattern:

- **Short per-attempt probe timeout** + **bounded total budget**. A
  not-yet-subscribed responder fails a probe fast and is retried;
  the moment the responder comes up, the next probe succeeds.
- **A received reply — including a handler-error reply — STOPS the
  loop.** A reply means the responder is up; only a *silent* responder
  (never replies) is retried, and only until the budget is spent. So a
  genuinely hung responder is **bounded by the total budget** (via the
  short per-attempt probe) rather than hanging on one long timeout — the
  short probe is what saves you, not the retry, so keep the probe short.
- Use `IsNoResponders(err)` to distinguish a never-appeared responder
  from a hung one *when the server fast-fails*. But the loop must
  retry on a plain probe **timeout** too — fast-fail is server-config
  dependent (see `request_integration_test.go`), so a no-fast-fail
  server surfaces an absent responder as a timeout, not `ErrNoResponders`.
  Never "optimize" the loop to retry only `IsNoResponders`. **This rule is
  about READS.** Re-reading is free, so tolerate the ambiguity and retry
  wider. For a non-idempotent WRITE the trade inverts — re-sending is not
  free — and the narrowing is required; see the create carve-out below.

**Only the OUTERMOST lifecycle-triggered reader uses `RequestReady*`.**
A steady-state handler that *forwards* to a downstream responder (e.g.
a `graph-query` handler serving an inbound query by calling
`graph.ingest.query.*`) stays `Request*` — even though the very first
request at cold system boot can race the downstream's readiness.
Readiness tolerance belongs to the outermost lifecycle-triggered read
(the component reading on its own `Start()`/reconcile), **not** the
intermediate request-driven hop. Converting a passthrough hop reintroduces
the hung-responder mask for *all* steady-state traffic through it, and
double-counts the budget inside the caller's own timeout. When in doubt:
*is this read triggered by the component's own lifecycle, or by an
inbound request?* Only the former converts.

**Signal a never-appeared responder — don't let it go silent.** A
misconfigured subject (typo, responder never deployed) now burns the
whole budget on every boot/reconcile and then classifies transient — a
lifecycle loop will log-and-skip forever, laundering a config error into
a permanent slow-loop. Callers of `RequestReady*` MUST, on budget
exhaustion, check `IsNoResponders(err)` and emit a **distinct, actionable
signal** (a one-shot Warn naming the subject + a metric where the
component has one) so "responder never appeared" is loud, not silence.

This is a **cold-start / first-read** tool. Once readiness is
established, subsequent reads are steady-state — keep using `Request`
so a later hung responder surfaces immediately.

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

### The carve-out: a CREATE is not idempotent, so it does not get the wide retry (gh#861)

`create_with_triples` is create-**or-fail**. Re-delivering it is not a no-op:
the second delivery answers `entity_already_exists`, and if the first delivery
is still executing, the request has manufactured a conflict **with itself**. The
client cannot tell that apart from a real concurrent birth.

This is not exotic. The caller's per-attempt deadline is typically far shorter
than the responder's: `pkg/lifecycle` uses 5s against graph-ingest's 30s
`DefaultRequestHandlerTimeout`, so the client can give up six times over while
the handler is still working, and `RequestWithRetry` re-sends on **any** non-nil
error including a plain timeout.

So for a **non-idempotent mutation**, retry only the class that proves
non-delivery — `natsclient.IsNoResponders` — and surface everything else as an
unknown outcome:

```go
// NON-IDEMPOTENT MUTATION (create-or-fail): one classified attempt per delivery,
// re-sent ONLY when the server says nothing was subscribed.
respBody, err := c.RequestClassified(ctx, createSubject, body, timeout)
if err != nil && !natsclient.IsNoResponders(err) {
    return nil, err // outcome unknown — the caller reads authoritative state
}
```

Two things this deliberately accepts:

- **The caller gets "I don't know" instead of an answer.** That is the point. The
  alternative shipped for months as a re-read that compared entity content to
  decide "did I write that?", and under concurrency the loser matched the
  winner's write and was told it had succeeded (gh#861). Content two writers can
  produce identically is not an identity.
- **Cold-start protection narrows.** Whether an absent responder fast-fails as
  `ErrNoResponders` or burns the deadline is server-config dependent — measured
  on the repo-pinned `nats:2.14-alpine` it fast-fails in well under a
  millisecond, and `pkg/lifecycle` keeps its ~13s budget for that class. On a
  server that does **not** fast-fail, a create issued before its responder
  subscribes fails honestly instead of converging. An honest failure beats a
  wrong answer; a *correct* answer needs request-scoped idempotency on the
  mutation seam (gh#869), not a wider retry.

The must-exist lanes keep the wide retry, for the reason stated above: an update
carrying `ExpectedRevision` turns a duplicate delivery into a revision mismatch
its caller re-reads, and delete is idempotent at the handler.

## Concrete shape

```go
// MUTATION: retry on transient failures
respData, err := natsClient.RequestWithRetry(
    ctx, mutationSubject, reqData, mutationTimeout,
    natsclient.DefaultRetryConfig(),
)
```

```go
// STEADY-STATE QUERY: no retry, surface timeout
respData, err := natsClient.Request(
    ctx, querySubject, reqData, queryTimeout,
)
```

```go
// COLD-START / RECONCILE READ: short probe + bounded budget
respData, err := natsClient.RequestReadyClassified(
    ctx, querySubject, reqData,
    natsclient.DefaultReadinessProbeTimeout, // per-attempt (2s)
    natsclient.DefaultReadinessBudget,       // total (30s)
)
```

## Current call-site survey (2026-04-28, beta.20 — pre-gh#420)

> This snapshot predates the readiness bucket. It lists "Queries" as
> bare `Request`, but most now use `RequestClassified` (ADR-060). The
> gh#420 sweep re-classifies the lifecycle-triggered reads within it
> into the readiness bucket; treat the categories below as the
> mutation/steady-state split, not the final inventory.

### Mutations (using RequestWithRetry)

- `processor/agentic-loop/graph_writer.go` — trajectory triples
- `processor/rule/triple_mutator.go` — rule-driven add_triple /
  remove_triple actions
- `graph/inference/applier.go` — inference-emitted triples
- `processor/agentic-tools/decide.go` — decide tool's terminal
  triple (coordinator pattern)

### Queries (using bare Request)

- `processor/graph-query/*` — all graph queries
- `graph/query/client.go` — graph-index queries
- `processor/graph-query/pathrag.go` — including the
  `graph.ingest.query.entity` existence check (pure read)
- `processor/graph-clustering/similarity.go` — similarity query
- `gateway/http/http.go`, `gateway/graph-gateway/component.go` —
  gateway query proxies
- `graph/llm/nats_content_fetcher.go` — content fetch

If you find a new call site that doesn't fit a bucket, the default
is to surface it: open a ticket with the call site path, the
subject, and what state (if any) it writes.

**Cold-start reads (gh#420) are a distinct third category.** A query
issued during a component's own `Start()` / initial reconcile /
post-reconnect resubscribe belongs in the readiness bucket
(`RequestReady*`), not the steady-state query bucket — the responder
may not be up yet. The distinguishing test: *is this read triggered by
an incoming request/event (steady state) or by the component's own
lifecycle (cold start)?* The latter uses `RequestReady*`.

## History

The pattern was inconsistent through beta.19. Beta.20 codified the
rule and audited every site. The original incident:
`feedback_approval_required_gap.md`'s sibling memory
`feedback_nats_request_retry_audit.md` has the full story.
