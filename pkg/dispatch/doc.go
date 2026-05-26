// Package dispatch provides the BoundedDispatcher substrate primitive
// — a bounded-concurrency parallel worker pool with optional
// KV-twofer-aware completion handling.
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
//	defer d.Stop(context.Background())
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
// Stop drains the underlying worker.Pool first (blocks until
// in-flight Process calls complete), then stops the completion
// watcher. Callers waiting on KV-triggered OnComplete callbacks
// should ensure those complete before Stop — typically by canceling
// the caller's own context and letting Process see the cancellation.
//
// # See also
//
//   - pkg/worker — the underlying worker pool (Pool[T])
//   - pkg/lifecycle — the workflow-shaped substrate that often
//     pairs with BoundedDispatcher (component-internal fan-out
//     over a Lifecycle workflow's instances)
//   - ADR-048 — the canonical decision
package dispatch
