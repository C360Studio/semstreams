package gateddagexec

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/c360studio/semstreams/pkg/dispatch"
	"github.com/c360studio/semstreams/pkg/gateddag"
	"github.com/c360studio/semstreams/pkg/lifecycle"
)

// dispatchJob is the bounded-dispatcher work item: one dispatchable unit.
type dispatchJob struct {
	unitID string
}

// executor runs the gated-DAG eval loop. It is constructed by the Component
// wrapper; its collaborators (reader/claimer/pub) are interfaces so the loop is
// unit-testable without NATS.
type executor struct {
	cfg     Config
	log     *slog.Logger
	mgr     *lifecycle.Manager
	reader  graphReader
	claimer claimer
	pub     publisher
	metrics *metrics

	backstop time.Duration

	disp *dispatch.BoundedDispatcher[dispatchJob]
	// submit enqueues a dispatchable unit. Defaults to disp.Submit; unit tests
	// override it with a recorder so reEvaluate's selection is testable without a
	// live dispatcher.
	submit func(dispatchJob) error

	// evalMu enforces single-flight: exactly one reEvaluate pass runs at a time
	// per instance (ADR-046 invariant #1). The single eval goroutine already
	// serializes passes; the mutex makes the contract explicit and testable.
	evalMu sync.Mutex
	// trigger coalesces a burst of watch events into <=1 queued pass (cap 1).
	trigger chan struct{}

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// start wires the dispatcher, the watch pump, and the eval goroutine. The
// lifecycle Watch bootstrap-replays current FanOut instances on subscribe (the
// restart-recovery substrate); the periodic backstop is the correctness floor
// that picks up unit-completion-driven state and surfaces stalls even if a watch
// event is missed (invariants #3, #4). Each pass reads the authoritative unit
// set, so any trigger converges on the latest state.
func (e *executor) start(ctx context.Context) error {
	backstop, err := e.cfg.backstopInterval()
	if err != nil {
		return err
	}
	e.backstop = backstop
	e.trigger = make(chan struct{}, 1)

	runCtx, cancel := context.WithCancel(ctx)
	e.cancel = cancel

	// Concurrency leg only: NO CompletionKVBucket (its completion-watcher
	// suppresses bootstrap replay; re-eval/recovery ride the lifecycle Watch
	// below — invariant #3). Process commits the claim then publishes.
	disp, err := dispatch.New[dispatchJob](runCtx, dispatch.Config[dispatchJob]{
		Workers:   e.cfg.Workers,
		QueueSize: e.cfg.QueueSize,
		Process: func(ctx context.Context, job dispatchJob) error {
			return e.claimThenDispatch(ctx, job.unitID)
		},
	}, dispatch.Deps{Logger: e.log})
	if err != nil {
		cancel()
		return err
	}
	e.disp = disp
	if e.submit == nil {
		e.submit = func(j dispatchJob) error { return e.disp.Submit(j) }
	}

	// Watch pump: lifecycle Watch over the FanOut workflow. Bootstrap replay =
	// restart recovery; live FanOut-instance writes nudge a re-eval. Each
	// delivery coalesces into the trigger channel.
	ch, err := e.mgr.Watch(runCtx, e.cfg.FanOutWorkflow)
	if err != nil {
		_ = e.disp.Stop(runCtx)
		cancel()
		return err
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		for {
			select {
			case <-runCtx.Done():
				return
			case _, ok := <-ch:
				if !ok {
					return
				}
				e.nudge()
			}
		}
	}()

	// Eval goroutine: single consumer of trigger + backstop ticker ⇒ passes are
	// serialized (single-flight). Fire one initial pass so a cold bucket still
	// reconciles its unit set on boot.
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		ticker := time.NewTicker(e.backstop)
		defer ticker.Stop()
		e.reEvaluate(runCtx) // initial reconcile
		for {
			select {
			case <-runCtx.Done():
				return
			case <-e.trigger:
				e.reEvaluate(runCtx)
			case <-ticker.C:
				e.reEvaluate(runCtx)
			}
		}
	}()

	return nil
}

// nudge requests a re-eval, coalescing with any already-pending request.
func (e *executor) nudge() {
	select {
	case e.trigger <- struct{}{}:
	default: // a pass is already queued; it will read the latest state
	}
}

// stop cancels the run context and waits for goroutines + the dispatcher.
func (e *executor) stop(timeout time.Duration) {
	if e.cancel != nil {
		e.cancel()
	}
	if e.disp != nil {
		stopCtx, c := context.WithTimeout(context.Background(), timeout)
		_ = e.disp.Stop(stopCtx)
		c()
	}
	e.wg.Wait()
}

// reEvaluate is one authoritative pass. Single-flight via evalMu (invariant #1).
func (e *executor) reEvaluate(ctx context.Context) {
	e.evalMu.Lock()
	defer e.evalMu.Unlock()

	if e.metrics != nil {
		e.metrics.evals.Inc()
	}

	states, err := e.reader.ReadUnitSet(ctx)
	if err != nil {
		// Transient read failure: skip this pass; the backstop retries. Never
		// panic the loop.
		e.log.Warn("gated-dag: authoritative read failed; skipping pass", slog.String("error", err.Error()))
		return
	}

	view := extractGraph(states, e.cfg)
	decisions := gateddag.Evaluate(view.unitIDs, view.dependsOn, view.markers)

	// Stall surfacing (#8): pure observability, computed BEFORE dispatch so a
	// stalled plan alerts even when nothing dispatches. Subtract in-flight
	// (claimed, non-terminal) units so running work is not mistaken for a stall.
	stalled := stallAfterInflight(gateddag.Stalled(view.unitIDs, view.dependsOn, view.markers), view)
	if e.metrics != nil {
		e.metrics.stall.Set(float64(len(stalled)))
	}
	if len(stalled) > 0 {
		e.log.Warn("gated-dag: stalled — held-ready units with no forward progress (depends_on cycle or all-blocked-behind-failure); recovery is reset-driven",
			slog.Any("units", stalled))
	}

	stopAll := e.cfg.FailurePolicy == FailurePolicyStopOnFirstFailure && anyFailed(view.markers)

	for _, d := range decisions {
		if !d.Dispatchable {
			continue
		}
		// In-flight dedup (#6): a unit already carrying the claim is skipped.
		// V4 robustness: a dirtied unit's stale claim is treated as absent so a
		// reset that cleared the terminal marker but not the claim still
		// re-dispatches (dirtied overrides claim).
		if view.claimed[d.UnitID] && !view.markers.Dirtied[d.UnitID] {
			continue
		}
		if stopAll {
			continue // stop_on_first_failure: hold all new dispatch
		}
		if err := e.submit(dispatchJob{unitID: d.UnitID}); err != nil {
			// Queue full: drop this pass's submit; the next eval re-selects it
			// (it carries no claim yet, so it is not deduped away).
			e.log.Warn("gated-dag: dispatch queue full; will retry next eval",
				slog.String("unit", d.UnitID), slog.String("error", err.Error()))
		}
	}
}

// claimThenDispatch is the bounded worker body: commit the durable claim BEFORE
// publishing the dispatch reference (invariant #2). A claim error returns
// without publishing (the unit is retried next eval). A publish error AFTER the
// claim is surfaced but the claim is NOT rolled back — rolling it back would
// re-open the double-run window; recovery is reset-driven.
func (e *executor) claimThenDispatch(ctx context.Context, unitID string) error {
	if err := e.claimer.Claim(ctx, unitID); err != nil {
		e.log.Warn("gated-dag: claim failed; not dispatching (retried next eval)",
			slog.String("unit", unitID), slog.String("error", err.Error()))
		return err
	}
	if e.metrics != nil {
		e.metrics.claimed.Inc()
	}
	if err := e.pub.Dispatch(ctx, unitID); err != nil {
		if e.metrics != nil {
			e.metrics.dispatchErr.Inc()
		}
		e.log.Error("gated-dag: dispatch publish failed AFTER claim committed; unit may be stranded until reset",
			slog.String("unit", unitID), slog.String("error", err.Error()))
		return err
	}
	if e.metrics != nil {
		e.metrics.dispatched.Inc()
	}
	e.log.Debug("gated-dag: dispatched unit", slog.String("unit", unitID))
	return nil
}

// stallAfterInflight suppresses a stall alert while genuine work is in flight: a
// unit that is claimed but not yet terminal (and not dirtied) is running, so the
// plan is progressing even if the brain reports held-ready units (S1). Returns
// the original stalled set only when nothing is in flight.
func stallAfterInflight(stalled []string, view graphView) []string {
	if len(stalled) == 0 {
		return nil
	}
	for id := range view.claimed {
		if view.markers.Dirtied[id] {
			continue // reset in progress, not in-flight
		}
		if !view.markers.Completed[id] && !view.markers.Failed[id] {
			return nil // a claimed, non-terminal unit is running ⇒ not stalled
		}
	}
	return stalled
}

// anyFailed reports whether any unit carries the failure marker.
func anyFailed(m gateddag.MarkerSet) bool {
	return len(m.Failed) > 0
}
