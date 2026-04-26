# Endpoint Health and Circuit Breaking

`agentic-model` runs a per-endpoint circuit breaker by default. When an
endpoint starts failing, the breaker opens and the fallback chain skips
it; once a cooldown elapses, a single probe tests recovery and either
closes the breaker or reopens it. No configuration is required to get
this behavior — every flow boots with sensible defaults.

This guide covers what the breaker actually does, how to observe it,
and how to override it (custom policy, shared state across processes,
or fully disable).

## What's Tracked

For each endpoint named in the model registry, the breaker keeps a
sliding window of the most recent results (default: 20). Each result
is one of:

| Outcome | Counted as |
|---|---|
| Success after any internal retry | success |
| Final failure (after retry budget) | failure, classified by kind: timeout / rate_limit / network / server_error / unknown |

When the window has at least `MinRequests` observations (default: 5)
and the failure ratio exceeds `ErrorRateThreshold` (default: 0.5), the
breaker transitions from Closed to Open.

## State Machine

```text
Closed   --error rate > threshold (and window ≥ MinRequests)--> Open
Open     --cooldown elapsed------------------------------>      HalfOpen
HalfOpen --probe succeeds-------------------------------->      Closed
HalfOpen --probe fails----------------------------------->      Open  (cooldown restarts)
```

- **Closed**: requests flow normally. The window keeps sliding; when
  failures push the rate above threshold, it opens.
- **Open**: agentic-model's fallback chain skips this endpoint. The
  breaker stays Open for `Cooldown` (default: 30s).
- **HalfOpen**: the first request after cooldown is sent through as a
  probe. If it succeeds, the breaker closes; if it fails, the cooldown
  restarts.

The transitions are lazy — the cooldown→half-open promotion happens
the first time `IsHealthy` is queried after the cooldown elapses, not
on a background timer.

## Fallback Chain Behavior

When a request comes in for capability `fast`:

1. agentic-model walks the capability's preferred + fallback chain
   in order. **Endpoints with an Open breaker are skipped.**
2. If `req.Model` was a direct endpoint name (not a capability), that
   endpoint is health-gated too. If it's Open, the resolution falls
   through to the default.
3. The default (`model_registry.defaults.model`) is **never** health-
   gated. It's the last guaranteed responder; refusing to serve when
   nothing else is available would just queue dead air for the user.
4. Pathological case: the chain is unhealthy AND no default is
   configured. The breaker is ignored on a final retry of the chain
   so the request still has a chance, and the result is recorded so
   the breaker stays accurate.

This matches the answer to the design question "what should happen
when the preferred endpoint is open" — silent fall-through to the next
chain entry. Callers don't have to handle a special "endpoint
unhealthy" error.

## Observability

### Prometheus metric

A gauge tracks the current state per endpoint:

```text
semstreams_agentic_model_endpoint_health_state{endpoint="claude-sonnet",state="closed"} 1
semstreams_agentic_model_endpoint_health_state{endpoint="claude-sonnet",state="open"} 0
semstreams_agentic_model_endpoint_health_state{endpoint="claude-sonnet",state="half_open"} 0
```

For each endpoint, the matching state label is set to 1 and the others
to 0, so dashboards can sum across `state` and rely on the result
equaling 1 (= "we have one tracked status for this endpoint"). Useful
PromQL:

```promql
# Endpoints currently Open
semstreams_agentic_model_endpoint_health_state{state="open"} == 1

# Time-series of state transitions
changes(semstreams_agentic_model_endpoint_health_state[5m]) > 0
```

### Logs

Skipped endpoints log at INFO:

```text
level=INFO msg="skipping unhealthy endpoint in fallback chain" endpoint=claude-sonnet status=open
level=INFO msg="requested endpoint unhealthy; falling through to default" endpoint=claude-sonnet status=open
```

The "all chain endpoints unhealthy; attempting anyway" path logs at
WARN — this is the pathological last-ditch case worth alerting on.

## Tuning

The default `BreakerConfig`:

```go
WindowSize:         20
MinRequests:        5
ErrorRateThreshold: 0.5
Cooldown:           30 * time.Second
```

Defaults aim at typical LLM workloads: tens of requests per minute,
latency dominated by upstream. To tune, construct a custom breaker and
inject it via the option below.

## Customization

agentic-model accepts a custom `model.HealthPolicy` via
`WithHealthPolicy`. Three common cases:

### Tune the default breaker

```go
import (
    "github.com/c360studio/semstreams/model"
    agenticmodel "github.com/c360studio/semstreams/processor/agentic-model"
)

policy := model.NewRollingWindowBreaker(model.BreakerConfig{
    WindowSize:         50,
    MinRequests:        10,
    ErrorRateThreshold: 0.3,
    Cooldown:           60 * time.Second,
})

comp, err := agenticmodel.NewComponentWithOptions(rawConfig, deps,
    agenticmodel.WithHealthPolicy(policy))
```

Most factory paths use `NewComponent`, which wires the default policy.
`NewComponentWithOptions` is the option-aware constructor.

### Disable circuit breaking entirely

```go
comp, err := agenticmodel.NewComponentWithOptions(rawConfig, deps,
    agenticmodel.WithHealthPolicy(model.NewAlwaysHealthyPolicy()))
```

`NewAlwaysHealthyPolicy` reports every endpoint as healthy and ignores
all `RecordResult` calls. Use this in tests or in deployments where
upstream rate limiting / fallback is handled by something else.

### Share state across processes

If multiple processes need to share circuit-breaker state (e.g., a
team of dispatchers behind a load balancer that should agree on which
endpoint is Open), implement `model.HealthPolicy` against your shared
store:

```go
type sharedPolicy struct{ /* NATS KV bucket, Redis client, etc. */ }

func (p *sharedPolicy) IsHealthy(endpoint string) bool                 { /* read */ }
func (p *sharedPolicy) EndpointStatus(endpoint string) model.EndpointStatus { /* read */ }
func (p *sharedPolicy) EndpointStats(endpoint string) model.HealthStats     { /* read */ }
func (p *sharedPolicy) RecordResult(endpoint string, r model.Result)        { /* write */ }
```

Inject via `WithHealthPolicy(yourSharedPolicy)`. Each process records
results to the shared store and reads health state from it — they
converge on the same view.

## External Consumers Inspecting Health

Code outside the agentic-model component can consult breaker state in
two ways:

### Through the component

`Component.HealthPolicy()` returns the active policy. Pass it through
to anything that needs to consult endpoint state:

```go
policy := comp.(*agenticmodel.Component).HealthPolicy()
if !policy.IsHealthy("claude-sonnet") {
    // skip dispatch, alert, fall over, etc.
}
```

### Through a HealthAwareRegistry

For consumers that already pass `model.RegistryReader` around through
DI, wrap registry + policy into a single value:

```go
aware := model.ComposeHealth(registry, policy)
// aware satisfies both RegistryReader AND HealthPolicy
endpoint := aware.GetEndpoint("claude-sonnet")
if !aware.IsHealthy("claude-sonnet") {
    // ...
}
```

`ComposeHealth(r, p)` panics if `r` is nil. If `p` is nil, the
always-healthy policy is substituted so health methods don't have to
nil-guard their callers.

## When To Reach For This vs. Built-In Provider Logic

The breaker is a per-process supplement to provider-side rate limiting
and retry, not a replacement. Specifically:

- **Per-endpoint 429 handling** is already done by `agentic-model` via
  `RateLimitDelay` + retry budget (`MaxRateLimitRetries`). Only
  *exhausted* 429s reach the breaker — transient ones don't count.
- **5xx retry** is also already done up to `MaxAttempts`. Same rule:
  only final failures reach the breaker.
- **Concurrency caps** live in `EndpointThrottle` (per-endpoint
  semaphore + token bucket). Throttle and breaker are independent —
  the throttle prevents overload, the breaker reacts to it.

Reach for `WithHealthPolicy` when:

- You want different tuning than the defaults.
- You're operating multiple agentic-model processes that should agree
  on endpoint health.
- You want to fully disable circuit breaking (e.g., during chaos
  testing where every error matters).

## Related

- [Agentic Component Patterns](../advanced/08-agentic-components.md) —
  rate limits, retry config, throttle config in `model_registry`
- [Model Registry Runtime Updates](05-model-registry-runtime-updates.md) —
  how the registry itself stays fresh under KV-driven changes
- [ADR-024 Layered LLM Timeouts](../adr/024-layered-llm-timeouts.md) —
  per-endpoint and per-capability timeout precedence
