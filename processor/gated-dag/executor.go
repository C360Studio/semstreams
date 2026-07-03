package gateddagexec

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/dispatch"
	"github.com/c360studio/semstreams/pkg/gateddag"
	"github.com/c360studio/semstreams/pkg/lifecycle"
)

// entityStatesBucket is the KV bucket the unit-completion watch (GH #363) reads.
const entityStatesBucket = "ENTITY_STATES"

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
	nc      *natsclient.Client // unit-completion watch (#363) + FanOut instance ops (#364)
	reader  graphReader
	claimer claimer
	pub     publisher
	stall   stallPublisher // edge-triggered stall event (#365.3); nil when no stall_subject
	metrics *metrics

	backstop time.Duration

	// lastStalled edge-triggers the stall event (#365.3): emit only on the
	// 0→non-zero transition. Touched only inside reEvaluate (under evalMu).
	lastStalled bool
	// completed guards the FanOut auto-complete (#364) so the instance is
	// transitioned to completed at most once. Touched only inside reEvaluate.
	completed bool
	// inflight tracks units submitted-but-not-yet-durably-claimed. The durable
	// claim commits in the worker (claimThenDispatch) AFTER submit returns and
	// evalMu releases, so between submit and claim-commit a fast re-eval (the
	// #363 unit watch nudges sub-millisecond) would re-select the unit and
	// double-dispatch. This in-memory set closes that window; the durable claim
	// covers it once committed, and entries clear when the unit goes terminal or
	// is reset (dirtied). Touched only inside reEvaluate (under evalMu).
	inflight map[string]bool

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

	// Unit-completion watch (#363): a raw KV watch over ENTITY_STATES filtered to
	// the unit prefix nudges re-eval the moment a completion/failure/reset marker
	// lands, instead of waiting for the backstop tick. Best-effort — if it can't
	// start, the backstop stays the correctness floor. NOT a lifecycle Watch:
	// unit entities don't carry the FanOut phase predicate, and the lifecycle
	// Watch above remains the restart-recovery driver (invariant #3). The
	// executor's own claim writes also fire this watch — harmless: the trigger is
	// coalesced (cap 1) + single-flight, and re-eval converges (dispatches only
	// new ready-unclaimed units).
	if e.nc != nil {
		if err := e.startUnitWatch(runCtx); err != nil {
			e.log.Warn("gated-dag: unit-completion watch unavailable; dispatch latency falls back to the backstop",
				slog.String("error", err.Error()))
		}
	}

	// FanOut instance lifecycle (#364): when configured, create the instance in
	// `dispatching` so it's operator-visible from boot; the eval loop drives it to
	// `completed` on whole-set terminality. Idempotent — tolerate already-exists.
	if e.cfg.FanOutInstanceID != "" {
		inst := &FanOut{EntityIDField: e.cfg.FanOutInstanceID, PhaseField: PhaseDispatching}
		if err := e.mgr.Create(runCtx, inst); err != nil && !errors.Is(err, lifecycle.ErrAlreadyExists) {
			e.log.Warn("gated-dag: FanOut instance create failed; instance lifecycle not owned this boot",
				slog.String("instance", e.cfg.FanOutInstanceID), slog.String("error", err.Error()))
		}
	}

	// Eval goroutine: single consumer of trigger + backstop ticker ⇒ passes are
	// serialized (single-flight). Fire one initial pass so a cold bucket still
	// reconciles its unit set on boot.
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		ticker := time.NewTicker(e.backstop)
		defer ticker.Stop()
		e.reEvaluate(runCtx, false) // initial reconcile (not a backstop tick)
		for {
			select {
			case <-runCtx.Done():
				return
			case <-e.trigger:
				e.reEvaluate(runCtx, false)
			case <-ticker.C:
				e.reEvaluate(runCtx, true)
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
// fromBackstop marks a periodic-tick pass: the stall EVENT (#365.3) is emitted
// only on a backstop tick so a transient stall during consumer seeding (a
// dependent written before its prerequisite) does not spuriously alert — the
// gauge + WARN log still update every pass.
func (e *executor) reEvaluate(ctx context.Context, fromBackstop bool) {
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
	if e.metrics != nil {
		// gh#429: referential stubs excluded from the unit set this eval. A
		// sustained nonzero value is a stuck-stub signal (a referenced unit whose
		// real content never landed on a re-stamping lane), not a silent wedge.
		e.metrics.stubsSkipped.Set(float64(view.stubsSkipped))
	}
	decisions := gateddag.Evaluate(view.unitIDs, view.dependsOn, view.markers)

	// In-flight bookkeeping: drop units that have gone terminal, been reset
	// (dirtied), or vanished from the set — their durable claim (or absence) now
	// governs, so the in-memory hint is no longer needed and a reset must be free
	// to re-dispatch.
	if e.inflight == nil {
		e.inflight = make(map[string]bool)
	}
	present := make(map[string]bool, len(view.unitIDs))
	for _, id := range view.unitIDs {
		present[id] = true
	}
	for id := range e.inflight {
		if !present[id] || view.markers.Completed[id] || view.markers.Failed[id] || view.markers.Dirtied[id] {
			delete(e.inflight, id)
		}
	}

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
	// Edge-triggered stall event (#365.3), backstop-only: publish on the
	// 0→non-zero transition as seen at a periodic tick (the settled state), so a
	// transient stall mid-seeding does not alert and a persistent stall emits
	// once. lastStalled tracks the backstop's view.
	if fromBackstop {
		if len(stalled) > 0 && !e.lastStalled && e.stall != nil {
			if perr := e.stall.PublishStall(ctx, stalled); perr != nil {
				e.log.Warn("gated-dag: stall event publish failed", slog.String("error", perr.Error()))
			}
		}
		e.lastStalled = len(stalled) > 0
	}

	stopAll := e.cfg.FailurePolicy == FailurePolicyStopOnFirstFailure && anyFailed(view.markers)

	for _, d := range decisions {
		if !d.Dispatchable {
			continue
		}
		// In-flight dedup (#6): skip a unit already carrying the durable claim OR
		// already submitted this run (the in-memory hint that closes the
		// submit→claim-commit window). V4 robustness: a dirtied unit overrides
		// both — a reset that cleared the terminal marker but not the claim (or
		// that races the in-memory hint) still re-dispatches.
		if (view.claimed[d.UnitID] || e.inflight[d.UnitID]) && !view.markers.Dirtied[d.UnitID] {
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
			continue
		}
		// Submitted: mark in-flight until the durable claim commits (worker) and
		// the unit goes terminal/reset (cleared above on a later pass).
		e.inflight[d.UnitID] = true
	}

	// FanOut auto-complete (#364): when every unit is Done (terminal-success),
	// drive the instance dispatching→completed exactly once. An empty unit set is
	// NOT complete (vacuous all-Done) — require ≥1 unit.
	if e.cfg.FanOutInstanceID != "" && !e.completed && allUnitsDone(decisions) {
		e.maybeCompleteFanOut(ctx)
	}
}

// allUnitsDone reports whether the fan-out is terminal-and-successful: at least
// one unit and every unit derived Done. Failed/blocked sets are not "done" (that
// is a stall — surfaced, consumer-driven failure), so this is success-only.
func allUnitsDone(decisions []gateddag.Decision) bool {
	if len(decisions) == 0 {
		return false
	}
	for _, d := range decisions {
		if d.Status != gateddag.StatusDone {
			return false
		}
	}
	return true
}

// maybeCompleteFanOut transitions the FanOut instance dispatching→completed.
// Guarded against re-firing: the completion write nudges the FanOut lifecycle
// Watch → another re-eval, so we only transition an instance still in
// `dispatching` and latch e.completed afterward. Uses an explicit Transition to
// PhaseCompleted (NOT Manager.Complete, which is ambiguous when both terminal
// phases are reachable from `dispatching`).
func (e *executor) maybeCompleteFanOut(ctx context.Context) {
	inst, err := e.mgr.Get(ctx, e.cfg.FanOutWorkflow, e.cfg.FanOutInstanceID)
	if err != nil {
		e.log.Warn("gated-dag: FanOut instance read failed; cannot complete",
			slog.String("instance", e.cfg.FanOutInstanceID), slog.String("error", err.Error()))
		return
	}
	if inst.Phase() != PhaseDispatching {
		e.completed = true // already terminal (or consumer-driven elsewhere); stop checking
		return
	}
	if err := e.mgr.Transition(ctx, e.cfg.FanOutWorkflow, e.cfg.FanOutInstanceID,
		PhaseCompleted, lifecycle.TransitionSourceComponent, "all units complete"); err != nil {
		e.log.Warn("gated-dag: FanOut complete transition failed",
			slog.String("instance", e.cfg.FanOutInstanceID), slog.String("error", err.Error()))
		return
	}
	e.completed = true
	e.log.Info("gated-dag: fan-out complete", slog.String("instance", e.cfg.FanOutInstanceID))
}

// startUnitWatch opens a raw KV watch over ENTITY_STATES filtered to the unit
// prefix and nudges the trigger on every unit write (#363). The goroutine owns
// the watcher's lifetime via ctx.
func (e *executor) startUnitWatch(ctx context.Context) error {
	bucket, err := e.nc.GetKeyValueBucket(ctx, entityStatesBucket)
	if err != nil {
		return err
	}
	watcher, err := e.nc.NewKVStore(bucket).Watch(ctx, e.cfg.UnitEntityPrefix+".>")
	if err != nil {
		return err
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer func() { _ = watcher.Stop() }()
		for {
			select {
			case <-ctx.Done():
				return
			case entry, ok := <-watcher.Updates():
				if !ok {
					return
				}
				if entry == nil {
					continue // end-of-initial-values sentinel (NATS KV bootstrap)
				}
				e.nudge()
			}
		}
	}()
	return nil
}

// claimThenDispatch is the bounded worker body: commit the durable claim BEFORE
// publishing the dispatch reference (invariant #2). A claim error returns
// without publishing (the unit is retried next eval). A publish error AFTER the
// claim is surfaced but the claim is NOT rolled back — rolling it back would
// re-open the double-run window; recovery is reset-driven.
func (e *executor) claimThenDispatch(ctx context.Context, unitID string) error {
	if err := e.claimer.Claim(ctx, unitID); err != nil {
		// No durable claim was written, so the in-memory in-flight hint must be
		// cleared or the unit (and its whole downstream subtree) wedges forever:
		// it stays Ready+unclaimed, but inflight=true would skip it on every pass
		// and the clear-on-terminal/dirtied path never fires for a non-terminal
		// unit. Clearing it re-selects the unit next eval (genuine retry). evalMu
		// serializes with reEvaluate (this runs in the dispatch worker, never
		// under a held evalMu, so there is no re-entrancy).
		e.evalMu.Lock()
		delete(e.inflight, unitID)
		e.evalMu.Unlock()
		if e.metrics != nil {
			e.metrics.claimErr.Inc()
		}
		e.log.Warn("gated-dag: claim failed; in-flight hint cleared, unit re-selected next eval",
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
