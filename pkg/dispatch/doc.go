// Package dispatch provides two bounded-concurrency substrate
// primitives:
//
//   - BoundedDispatcher — unordered bounded-concurrency parallel worker
//     pool with optional KV-twofer-aware completion handling (ADR-048).
//   - KeyedPool — keyed-ORDERED bounded concurrency: same-key work is
//     serialized on one lane, different keys run in parallel (ADR-072).
//
// Pick by whether per-key ordering matters. If any two work items that
// share a key must be processed in submit order (e.g. graph-ingest,
// whose arrival-order merge would corrupt on out-of-order same-entity
// updates), use KeyedPool. Otherwise use BoundedDispatcher.
//
// # What this is
//
// BoundedDispatcher is the framework-provided primitive for the
// "rules sequence, components parallelize" architecture (CLAUDE.md
// Orchestration Boundaries section). It's what components compose
// internally when they need to do parallel work over a known list
// of items — drone fleet weather-monitor walking all active
// missions, scenario-orchestrator dispatching ready requirements
// under DAG gating, manufacturing batch's per-widget station
// processing, semspec scenario-orchestrator's bounded-concurrency
// dispatch pattern.
//
// BoundedDispatcher is NOT:
//   - A workflow engine (no DAG semantics, no branching, no
//     lifecycle — see pkg/lifecycle for those)
//   - A rule-engine extension (rules don't gain new fan-out
//     primitives; for_each at the rule layer is the at-the-rule-
//     layer fan-out)
//   - A replacement for pkg/worker.Pool — it WRAPS it. New uses
//     prefer BoundedDispatcher (higher-level, KV-twofer aware);
//     existing pkg/worker.Pool consumers stay as-is.
//
// # Use when
//
//   - A component does internal parallel work over a list of items
//   - Bounded concurrency is required (caller picks the worker count)
//   - Optionally: each work item completes async and the dispatcher
//     should fire OnComplete when KV signals match
//
// # Do NOT use for
//
//   - At-the-rule-layer fan-out (use rule engine's for_each instead)
//   - Sequential per-item processing (use a plain loop)
//   - Unbounded concurrency (use a bare goroutine pool)
//
// # Example usage (no completion watcher)
//
//	d, err := dispatch.New(ctx, dispatch.Config[*Requirement]{
//	    Workers:   c.MaxConcurrent,
//	    QueueSize: 256,
//	    Process:   c.processRequirement,
//	}, dispatch.Deps{
//	    NATSClient: c.natsClient,
//	    Logger:     c.logger,
//	})
//	if err != nil {
//	    return fmt.Errorf("dispatch new: %w", err)
//	}
//	defer func() {
//	    if err := d.Stop(context.Background()); err != nil {
//	        c.logger.Warn("dispatch stop", slog.String("error", err.Error()))
//	    }
//	}()
//
//	for _, req := range filterReady(...) {
//	    if err := d.Submit(req); err != nil {
//	        // ErrQueueFull on overflow; caller chooses
//	        // retry/drop/backpressure-propagate.
//	    }
//	}
//
// # Example usage (with KV-twofer completion watcher)
//
//	d, err := dispatch.New(ctx, dispatch.Config[*Requirement]{
//	    Workers:                   c.MaxConcurrent,
//	    QueueSize:                 256,
//	    Process:                   c.processRequirement,
//	    CompletionKVBucket:        "EXECUTION_STATES",
//	    CompletionKeyForWorkItem:  func(r *Requirement) string {
//	        return "req." + r.Slug + "." + r.ID
//	    },
//	    OnComplete: c.onRequirementComplete,
//	}, deps)
//
// In the completion-watcher mode, the dispatcher subscribes to the
// configured KV bucket BEFORE accepting any Submit. Each Submit
// registers a tracking entry keyed by CompletionKeyForWorkItem(work)
// before enqueuing to the underlying pool, so a completion-signal
// write that arrives between Submit and Process can't slip past the
// watcher.
//
// # Shutdown
//
// Stop attempts the underlying worker.Pool first, then cancels and joins the
// completion watcher. It returns pool timeout and caller-context causes rather
// than reporting an unobserved join as clean. A failed Stop is terminal and
// must not be retried; a successfully completed repeated Stop remains nil.
// Callers waiting on KV-triggered OnComplete callbacks should ensure those
// complete before Stop, typically by canceling the caller's own runtime context
// and using a separate live bounded shutdown context for Stop.
//
// # KeyedPool — keyed-ordered concurrency (ADR-072)
//
// KeyedPool partitions work into N lanes by a caller-supplied key:
// lane = fnv1a(KeyOf(work)) % Lanes. Each lane is one goroutine
// draining a bounded queue in order, so items sharing a key process
// serially in submit order while distinct keys run concurrently. Its
// Process receives the assigned lane index, so a composer can shard
// per-lane state (e.g. an applied-sequence guard) without locking. A
// panic in Process is recovered — the lane survives and the optional
// OnPanic disposition fires (so a composer can Nak the message).
//
//	pool, err := dispatch.NewKeyedPool(ctx, dispatch.KeyedConfig[ingestWork]{
//	    Lanes:      c.config.IngestLanes,
//	    QueueDepth: 256,
//	    Name:       "graph_ingest",
//	    KeyOf:      func(w ingestWork) string { return w.entity.ID },
//	    Process:    c.processIngest,   // (ctx, lane, work) error
//	    OnPanic:    func(w ingestWork, _ any) { _ = w.msg.Nak() },
//	}, dispatch.KeyedDeps{MetricsRegistry: deps.MetricsRegistry, Logger: c.logger})
//
// SubmitBlocking applies backpressure (blocks on a full lane);
// non-blocking Submit returns ErrLaneFull. On shutdown, cancel the
// submit context BEFORE Stop so a producer parked in SubmitBlocking
// unblocks (ADR-072 M3), then Stop drains the lanes.
//
// # See also
//
//   - pkg/worker — the underlying worker pool (Pool[T])
//   - pkg/lifecycle — the workflow-shaped substrate that often
//     pairs with BoundedDispatcher (component-internal fan-out
//     over a Lifecycle workflow's instances)
//   - ADR-048 — the BoundedDispatcher decision
//   - ADR-072 — the KeyedPool decision (keyed-concurrent entity ingest)
package dispatch
