# keyed-dispatch

> New capability seeded by gh#480 (ADR-072): the keyed-ordered dispatch primitive in
> `pkg/dispatch`, a sibling to `BoundedDispatcher` (ADR-048). Distilled from the design;
> other dispatch behavior is seeded when a change first touches it.

## ADDED Requirements

### Requirement: A keyed dispatch pool MUST process same-key work serially and different-key work in parallel

The framework MUST provide a bounded, key-partitioned dispatch primitive that routes
each work item to one of N lanes by a caller-supplied key, such that all items sharing
a key are processed serially in submit order on a single lane, while items with
different keys are processed concurrently across lanes. This is distinct from
`pkg/worker.Pool` / `BoundedDispatcher`, which distribute work across workers with no
key affinity or ordering guarantee. The primitive MUST bound memory via per-lane queues
and MUST drain in-flight work on graceful stop.

Lane assignment MUST be a pure function of the key (same key → same lane for the pool's
lifetime), and the caller MUST supply the key function and the per-item process
function. Submission MUST offer a blocking form that applies backpressure when the
target lane is full.

#### Scenario: same-key items are ordered

- **GIVEN** a keyed pool and two items with the same key, item A submitted before B
- **WHEN** both are dispatched
- **THEN** A's process function completes before B's begins

#### Scenario: different-key items run concurrently

- **GIVEN** a keyed pool with more than one lane and items with distinct keys
- **WHEN** they are submitted
- **THEN** their process functions may run concurrently on different lanes

#### Scenario: full lane applies backpressure

- **GIVEN** a keyed pool whose target lane queue is full
- **WHEN** the caller uses the blocking submit
- **THEN** the submit blocks until the lane has capacity

### Requirement: A keyed dispatch pool MUST expose queue-wait and processing metrics

The keyed dispatch primitive MUST expose Prometheus metrics for queue-wait latency
(time between submit and process start), processing duration, current queue depth,
in-flight concurrency, and submitted/completed/dropped counts, each labelled by pool
name. These MUST allow a consumer to measure achieved concurrency and to separate time
spent waiting for a lane from time spent processing, without the consumer adding its
own probes.

#### Scenario: queue wait is observable independent of processing

- **GIVEN** a keyed pool under load
- **WHEN** metrics are scraped
- **THEN** the queue-wait histogram and the processing-duration histogram are reported
      separately
