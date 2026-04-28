# Migration Guide: beta.19 → beta.20

## Summary

Beta.20 is a **stability/correctness tag**. No API breakage, no
behavior changes on existing code paths. The release closes a
silent-data-loss pattern that was present across five graph-mutation
call sites: when graph-gateway's NATS subscription wasn't yet
propagated (startup races, restarts, reconnects), bare
`natsClient.Request` calls returned "no responders" and the mutation
silently failed.

The fix switches all production graph mutations to
`natsClient.RequestWithRetry(..., DefaultRetryConfig())` — the
helper that was already in use for trajectory triples in beta.19.

## What changes

Five production call sites switch from `Request` →
`RequestWithRetry`:

- `processor/rule/triple_mutator.go` — rule-driven `add_triple` /
  `remove_triple` actions
- `graph/inference/applier.go` — inference-emitted triples
- `processor/agentic-tools/decide.go` — decide tool's terminal
  triple (coordinator pattern)
- `examples/github-pr-workflow/component.go` — example mutation

Behaviour change: a transient "no responders" or other recoverable
NATS error is now retried 3 times with 100ms → 2s exponential
backoff before the call surfaces an error. Worst-case wallclock for
a permanently-broken responder grows from `MutationTimeout` to
roughly `MutationTimeout + 100ms + 200ms + 400ms`. For a 5s
timeout that's ~5.7s instead of 5s.

If you depended on the previous fast-fail behaviour for any reason
(e.g., a chaos test deliberately probing graph-gateway downtime),
factor the retry latency into your assertions.

## What you should do

For most deployments: nothing. Pull beta.20 and the silent failures
go away.

If you have **custom graph-mutation responders** outside the
framework's set, this is the moment to verify they're idempotent —
the retry path may re-deliver the same request when the first
attempt's response gets lost. Triple mutations against a KV-backed
graph are structurally idempotent; if your responder tracks per-
request side effects (logs, counters) those need a request-ID dedup
cache to converge correctly under retries.

If you have **custom NATS request call sites** outside the
framework, consult the new decision framework at
[`docs/operations/07-nats-request-retry.md`](07-nats-request-retry.md)
and switch any mutations to `RequestWithRetry`.

## What didn't change

- Query call sites stay bare (`Request` without retry). The
  decision framework explains why — retry-on-any-error masks hung
  responders as latency, which is the wrong tradeoff for queries.
- Approval flow, payload registry, all beta.19 surfaces — no
  changes.
- API surface — no removals, no renames.

## Verification

After upgrading:

- `go build ./...` succeeds.
- `go test -race ./...` passes.
- `task lint` reports 0 revive warnings.
- If your deployment had visible "rules occasionally don't write
  their triples" bugs, they should disappear. The silent-failure
  pattern was the most likely cause.

## Related

- [docs/operations/07-nats-request-retry.md](07-nats-request-retry.md)
  — the decision framework codifying when to use `Request` vs
  `RequestWithRetry`.
- [migration-beta19.md](migration-beta19.md) — the previous
  migration (approval-flow loop wiring).
