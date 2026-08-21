# Design: Direct one-shot ownership

## Landed decisions

### Owner-local Go lifecycle

The code that starts runtime work owns its private cancellation, exact native handle, and completion signal. Shared
generation, operation, retained-result, rejoin, and rollback state from `internal/lifecyclejoin` is removed. Completed
repeated Stop is a no-op; no contract promises concurrent Stop result sharing.

### Native consumer ownership

Canonical port-backed and internal consume operations return the exact `jetstream.ConsumeContext`. The caller retains
that handle and owns its shutdown. Client does not catalog, rediscover, stop, delete, or wait for component-owned
consumers and subscriptions.

### Stateless durable handling

`NewDurableHandler` validates heartbeat policy and composes the existing `ConsumeWithHeartbeat` settlement behavior.
It retains no context, handle, worker, identity, or lifecycle authority.

### Observation remains separate

Consumer-policy Prometheus metrics, graph readiness, agent-loop inflight observation, and the optional OTEL adapter
remain observation surfaces. They do not become lifecycle owners. Health and structured `slog` behavior are preserved.

### Durable topology is not lifecycle cleanup

Normal Stop and Client Close do not delete durable consumers. The five deletion configuration fields and the Client
name-routed deletion operations are removed without a replacement production cleanup mechanism.

## Explicitly separate debt

This change does not redesign Client Connect/Close, callbacks, reconnect timers, async publication settlement,
`Subscription.Drain`, Registry, Flow, Fusion, classifiers, or logging delivery. It does not require raw NATS-root
narrowing or claim controlled/dirty process-restart proof. Those require separate bounded issues when current evidence
justifies a contract change.

## Verification

The landed work is recorded in the historical recovery ledger. Archive closeout relies on current code, focused API
census and race tests, contract guards, schema no-drift, relevant E2E evidence, and independent review—not on the
superseded broader gates retained in that ledger for forensic history.
