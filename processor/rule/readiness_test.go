package rule

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/graph"
)

// newBootstrapRecord builds an authoritative watcher record at a generation, with its
// replay either finished or in flight.
func newBootstrapRecord(generation uint64, complete bool, replayed uint64) managedEntityWatcher {
	progress := &entityBootstrapProgress{}
	progress.complete.Store(complete)
	progress.replayed.Store(replayed)
	return managedEntityWatcher{generation: generation, bootstrap: progress}
}

// TestEntityBootstrapState_ZeroPatternsIsVacuouslyComplete covers task 4.5. A rule
// processor with no entity-watch patterns holds no ENTITY_STATES watcher at all, so
// there is authoritatively nothing to replay. Reporting never-bootstrapped here would
// defer every consumer forever on a correctly-configured deployment.
func TestEntityBootstrapState_ZeroPatternsIsVacuouslyComplete(t *testing.T) {
	rp := &Processor{}

	complete, scope := rp.entityBootstrapState()
	if !complete {
		t.Error("zero watchers must be vacuously complete")
	}
	if scope != 0 {
		t.Errorf("scope = %d, want 0", scope)
	}
}

// TestEntityBootstrapState_ConjunctionOverGenerations pins that EVERY authoritative
// watcher must have finished — one lagging pattern holds the whole producer back.
func TestEntityBootstrapState_ConjunctionOverGenerations(t *testing.T) {
	rp := &Processor{
		entityDispatchRecords: map[string]managedEntityWatcher{
			"ENTITY_STATES:a.>": newBootstrapRecord(1, true, 10),
			"ENTITY_STATES:b.>": newBootstrapRecord(2, false, 3),
		},
	}

	complete, scope := rp.entityBootstrapState()
	if complete {
		t.Error("reported complete while one watcher was still replaying")
	}
	if scope != 13 {
		t.Errorf("scope = %d, want 13 (summed across generations)", scope)
	}

	rp.entityDispatchRecords["ENTITY_STATES:b.>"] = newBootstrapRecord(2, true, 3)
	if complete, _ = rp.entityBootstrapState(); !complete {
		t.Error("expected complete once every watcher finished")
	}
}

// TestEntityBootstrapState_NewGenerationReopensBootstrap is the reason this diverges
// from graph-index's process-lifetime latch, and is gh#732's bug in a new costume if
// it regresses. The watcher set is runtime-mutable via a component-config PUT, and a
// recreated watcher RE-RUNS its replay because the watch carries no UpdatesOnly.
func TestEntityBootstrapState_NewGenerationReopensBootstrap(t *testing.T) {
	rp := &Processor{
		entityDispatchRecords: map[string]managedEntityWatcher{
			"ENTITY_STATES:a.>": newBootstrapRecord(1, true, 10),
		},
	}

	if complete, _ := rp.entityBootstrapState(); !complete {
		t.Fatal("precondition: generation 1 finished")
	}

	// Operator re-adds the pattern: a new generation supersedes the old record and
	// its replay starts over.
	rp.entityDispatchRecords["ENTITY_STATES:a.>"] = newBootstrapRecord(2, false, 0)

	complete, _ := rp.entityBootstrapState()
	if complete {
		t.Error("a new generation mid-replay must reopen bootstrap; a process-lifetime " +
			"latch would report bootstrapped here while replay was in flight")
	}
}

// TestEntityBootstrapState_RecordWithoutProgressFailsClosed guards the nil case. A
// record that cannot vouch for its own replay must not be counted as done.
func TestEntityBootstrapState_RecordWithoutProgressFailsClosed(t *testing.T) {
	rp := &Processor{
		entityDispatchRecords: map[string]managedEntityWatcher{
			"ENTITY_STATES:a.>": {generation: 1}, // no progress record
		},
	}

	if complete, _ := rp.entityBootstrapState(); complete {
		t.Error("a record without progress tracking must not read as complete")
	}
}

// TestComputeReadinessStatus_StateFromExistingLatches pins task 4.4: State comes from
// the two EXISTING sticky latches, and no new states are invented. reset_required
// outranks degraded because a reset-required processor has stopped evaluating rules,
// not merely become unhealthy.
func TestComputeReadinessStatus_StateFromExistingLatches(t *testing.T) {
	t.Run("healthy and bootstrapped is ready", func(t *testing.T) {
		rp := &Processor{}
		got := rp.computeReadinessStatus()
		if got.State != graph.IndexStateReady || !got.Ready {
			t.Errorf("got state=%q ready=%v, want ready/true", got.State, got.Ready)
		}
	})

	t.Run("degraded latch reports degraded and never ready", func(t *testing.T) {
		rp := &Processor{}
		rp.graphStateGuardDegraded.Store(true)
		got := rp.computeReadinessStatus()
		if got.State != graph.IndexStateDegraded {
			t.Errorf("state = %q, want degraded", got.State)
		}
		if got.Ready {
			t.Error("a degraded producer must not report Ready")
		}
	})

	t.Run("reset-required outranks degraded", func(t *testing.T) {
		rp := &Processor{}
		rp.graphStateGuardDegraded.Store(true)
		rp.graphStateResetRequired.Store(true)
		if got := rp.computeReadinessStatus(); got.State != graph.IndexStateResetRequired {
			t.Errorf("state = %q, want reset_required", got.State)
		}
	})

	t.Run("mid-replay is building, not ready", func(t *testing.T) {
		rp := &Processor{
			entityDispatchRecords: map[string]managedEntityWatcher{
				"ENTITY_STATES:a.>": newBootstrapRecord(1, false, 2),
			},
		}
		got := rp.computeReadinessStatus()
		if got.State != graph.IndexStateBuilding {
			t.Errorf("state = %q, want building", got.State)
		}
		if got.Ready || got.BootstrapComplete {
			t.Error("mid-replay must report neither Ready nor BootstrapComplete")
		}
	})
}

// TestComputeReadinessStatus_GateAgreesWithState makes sure the envelope this producer
// publishes actually drives the shared gate the way the states imply — the producer
// and the gate must not disagree about the same envelope.
func TestComputeReadinessStatus_GateAgreesWithState(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(*Processor)
		wantProceed bool
	}{
		{"ready", func(*Processor) {}, true},
		{"degraded", func(rp *Processor) { rp.graphStateGuardDegraded.Store(true) }, false},
		{"reset required", func(rp *Processor) { rp.graphStateResetRequired.Store(true) }, false},
		{"mid-replay", func(rp *Processor) {
			rp.entityDispatchRecords = map[string]managedEntityWatcher{
				"ENTITY_STATES:a.>": newBootstrapRecord(1, false, 1),
			}
		}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rp := &Processor{}
			tt.setup(rp)
			status := rp.computeReadinessStatus()
			proceed, reason := graph.EvaluateReadinessGate(
				graph.StatusReading{Status: status, Fresh: true})
			if proceed != tt.wantProceed {
				t.Errorf("gate proceed = %v (reason %v), want %v", proceed, reason, tt.wantProceed)
			}
		})
	}
}

// TestHealth_ContradictsNeitherLatch is honesty fix 7.1. Health used to latch true once
// watchers were established and never lower again, so it disagreed with the KV envelope
// the moment either sticky latch tripped.
func TestHealth_ContradictsNeitherLatch(t *testing.T) {
	t.Run("degraded lane is not healthy", func(t *testing.T) {
		rp := &Processor{}
		rp.health.Healthy = true
		rp.graphStateGuardDegraded.Store(true)
		if rp.Health().Healthy {
			t.Error("Health reported healthy with the entity-watch lane degraded")
		}
	})

	t.Run("reset required is not healthy", func(t *testing.T) {
		rp := &Processor{}
		rp.health.Healthy = true
		rp.graphStateResetRequired.Store(true)
		if rp.Health().Healthy {
			t.Error("Health reported healthy while rule evaluation was halted")
		}
	})

	t.Run("neither latch leaves health untouched", func(t *testing.T) {
		rp := &Processor{}
		rp.health.Healthy = true
		if !rp.Health().Healthy {
			t.Error("Health must stay true when neither latch has tripped")
		}
	})
}

// TestEntityBootstrapProgress_CountsReplayThroughTheRealHandler closes a gap the
// reviewer found by mutation: deleting the production replay counter
// (`progress.replayed.Add(1)` in handleEntityUpdatesForKey) left `go test
// ./processor/rule/` GREEN. Every unit test hand-built entityDispatchRecords, so no
// unit test ever ran the increment.
//
// My FIRST attempt at this test was also mutation-green — it registered a watcher and
// then called progress.replayed.Add(1) itself, exercising the plumbing while never
// touching the mutated line. This drives the REAL handler with a real watcher channel:
// two values, then the nil sentinel that ends replay, then a post-bootstrap value that
// must NOT be counted as replay.
func TestEntityBootstrapProgress_CountsReplayThroughTheRealHandler(t *testing.T) {
	processor := newAtomicBootstrapProcessor(t)
	watcher := newAtomicBootstrapWatcher()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const key = "ENTITY_STATES:readiness.>"
	processor.entityDispatchGate.Lock()
	processor.mu.Lock()
	if processor.entityWatcherMap == nil {
		processor.entityWatcherMap = map[string]jetstream.KeyWatcher{}
	}
	watcherCtx, generation := processor.registerEntityWatcherLocked(ctx, key, watcher)
	processor.mu.Unlock()
	processor.entityDispatchGate.Unlock()

	go processor.handleEntityUpdatesForKey(watcherCtx, watcher, key, generation)

	// Two values BEFORE the sentinel: this generation's replay.
	watcher.updates <- validRuleEntityEntry(t, "acme.ops.rule.gcs.mission.one", 1)
	watcher.updates <- validRuleEntityEntry(t, "acme.ops.rule.gcs.mission.two", 2)
	watcher.updates <- nil // end-of-replay sentinel
	// One value AFTER: live traffic, must not inflate the initial-build size.
	watcher.updates <- validRuleEntityEntry(t, "acme.ops.rule.gcs.mission.three", 3)

	waitForRuleWatcher(t, func() bool {
		complete, _ := processor.entityBootstrapState()
		return complete
	})

	complete, scope := processor.entityBootstrapState()
	if !complete {
		t.Fatal("sentinel observed: expected bootstrap complete")
	}
	if scope != 2 {
		t.Errorf("scope = %d, want 2 — values replayed BEFORE the sentinel only; "+
			"post-bootstrap traffic must not enlarge the initial build", scope)
	}

	// A superseded generation must not resolve, so a retired watcher cannot keep
	// writing into the authoritative record.
	if stale := processor.entityBootstrapProgressFor(key, generation+1); stale != nil {
		t.Error("a non-authoritative generation resolved a progress record")
	}
}
