package graphingest

import (
	"testing"
	"time"
)

// TestLatchBootstrap_ScopeCapturedOnce pins BootstrapScope as the size of the INITIAL
// catch-up. A later burst must not retroactively enlarge the "initial build" — scope
// answers "how big was the build I latched against", and a moving value would make
// `complete && scope == 0` unreadable.
//
// This exercises the FALLBACK capture (every bind-time read failed). The normal path
// captures at bind via recordBindBacklog; see the integration test for that.
func TestLatchBootstrap_ScopeCapturedOnce(t *testing.T) {
	c := withTestRegistry(t, &Component{})

	scope, complete := c.latchBootstrap(900, false)
	if scope != 900 {
		t.Errorf("first read: scope = %d, want 900", scope)
	}
	if complete {
		t.Error("first read: complete with 900 outstanding")
	}

	// A larger later backlog must not move it.
	scope, _ = c.latchBootstrap(5000, false)
	if scope != 900 {
		t.Errorf("after a burst: scope = %d, want 900 (captured once)", scope)
	}

	scope, complete = c.latchBootstrap(0, false)
	if scope != 900 {
		t.Errorf("after draining: scope = %d, want 900", scope)
	}
	if !complete {
		t.Error("expected complete once the backlog drained and no sweep was started")
	}
}

// TestLatchBootstrap_ZeroScopeIsAuthoritativelyNothingToDo covers the distinction the
// field exists for (gh#732): a producer that started with an empty backlog reports
// complete with scope 0, which a consumer reads as "there was nothing to do" rather
// than "I might be early".
func TestLatchBootstrap_ZeroScopeIsAuthoritativelyNothingToDo(t *testing.T) {
	c := withTestRegistry(t, &Component{})

	scope, complete := c.latchBootstrap(0, false)
	if scope != 0 {
		t.Errorf("scope = %d, want 0", scope)
	}
	if !complete {
		t.Error("an empty initial backlog is immediately complete")
	}
}

// TestLatchBootstrap_ObservationFailureNeverLatches is the fail-closed guard. A zero
// read off a FAILED observation is the absence of a measurement, not a measurement of
// absence; letting it latch would permanently mark bootstrap complete off a backend
// fault.
func TestLatchBootstrap_ObservationFailureNeverLatches(t *testing.T) {
	c := withTestRegistry(t, &Component{})

	scope, complete := c.latchBootstrap(0, true)
	if complete {
		t.Error("a failed observation reported bootstrap complete")
	}
	if scope != 0 {
		t.Errorf("scope = %d, want 0 — nothing was measured", scope)
	}

	// The failed read must not have consumed the once-only scope capture either:
	// the first SUCCESSFUL read is what defines the initial build.
	scope, _ = c.latchBootstrap(700, false)
	if scope != 700 {
		t.Errorf("scope = %d, want 700 — the failed read must not have captured scope", scope)
	}
}

// TestLatchBootstrap_DrainedIsALatchNotALiveRead pins the deliberate asymmetry with
// Ready. Ready carries "caught up right now" and must flicker with load; bootstrap
// must NOT, or every consumer would defer during ordinary operation.
func TestLatchBootstrap_DrainedIsALatchNotALiveRead(t *testing.T) {
	c := withTestRegistry(t, &Component{})

	c.latchBootstrap(10, false)
	if _, complete := c.latchBootstrap(0, false); !complete {
		t.Fatal("expected complete after draining to zero")
	}

	// New work arrives. Ready will be false (the projection computes that from
	// Outstanding), but bootstrap stays complete.
	if _, complete := c.latchBootstrap(50, false); !complete {
		t.Error("bootstrap un-latched under new load; it must stay complete")
	}
}

// TestLatchBootstrap_RequiresTheEntityStateSweep pins the conjunction: the boot
// ENTITY_STATES sweep draining is an INDEPENDENT fact from the stream backlog, and
// both must hold.
func TestLatchBootstrap_RequiresTheEntityStateSweep(t *testing.T) {
	c := withTestRegistry(t, &Component{})
	c.entityBootstrapStarted.Store(true) // a sweep is in progress

	if _, complete := c.latchBootstrap(0, false); complete {
		t.Error("reported complete with the entity-state sweep still running")
	}

	c.entityBootstrapComplete.Store(true)
	if _, complete := c.latchBootstrap(0, false); !complete {
		t.Error("expected complete once both the sweep and the backlog finished")
	}
}

// TestBoundConsumers_DedupedAndDeterministic keeps the backlog sum from
// double-counting a consumer across a rebind, and keeps a degraded verdict naming the
// same consumer run to run.
func TestBoundConsumers_DedupedAndDeterministic(t *testing.T) {
	c := withTestRegistry(t, &Component{})
	c.registerBoundConsumer("SENSOR", "graph-ingest-sensor-all")
	c.registerBoundConsumer("EVENTS", "graph-ingest-events-all")
	c.registerBoundConsumer("SENSOR", "graph-ingest-sensor-all") // rebind

	got := c.boundConsumerSnapshot()
	if len(got) != 2 {
		t.Fatalf("got %d consumers, want 2 (a rebind must not double-count)", len(got))
	}
	if got[0].stream != "EVENTS" || got[1].stream != "SENSOR" {
		t.Errorf("snapshot not in deterministic order: %+v", got)
	}
}

// TestOldestOutstandingAt_PresenceEncoding pins the "0 means no information" contract
// at the producer. A caught-up producer reports no staleness however old its last
// applied message was, and a producer that has applied nothing yet reports absent
// rather than fabricating a fresh view.
func TestOldestOutstandingAt_PresenceEncoding(t *testing.T) {
	applied := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)

	t.Run("caught up reports no staleness", func(t *testing.T) {
		c := withTestRegistry(t, &Component{})
		c.recordApplied(applied)
		if got := c.oldestOutstandingAt(0); !got.IsZero() {
			t.Errorf("got %v, want zero — caught up has no staleness to report", got)
		}
	})

	t.Run("backlog ages from the last applied message", func(t *testing.T) {
		c := withTestRegistry(t, &Component{})
		c.recordApplied(applied)
		if got := c.oldestOutstandingAt(5); !got.Equal(applied) {
			t.Errorf("got %v, want %v", got, applied)
		}
	})

	t.Run("nothing applied yet reports absent, not fresh", func(t *testing.T) {
		c := withTestRegistry(t, &Component{})
		if got := c.oldestOutstandingAt(5); !got.IsZero() {
			t.Errorf("got %v, want zero — an unknown age must not read as 0ms fresh", got)
		}
	})

	t.Run("a zero delivery timestamp is not recorded", func(t *testing.T) {
		c := withTestRegistry(t, &Component{})
		c.recordApplied(time.Time{})
		if got := c.oldestOutstandingAt(5); !got.IsZero() {
			t.Errorf("got %v, want zero", got)
		}
	})
}

// TestLastSyncedRFC3339 pins the operator field's encoding, including the empty
// string before anything has been applied.
func TestLastSyncedRFC3339(t *testing.T) {
	c := withTestRegistry(t, &Component{})
	if got := c.lastSyncedRFC3339(); got != "" {
		t.Errorf("got %q, want empty before anything is applied", got)
	}

	c.recordApplied(time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC))
	if got := c.lastSyncedRFC3339(); got != "2026-07-30T12:00:00Z" {
		t.Errorf("got %q, want 2026-07-30T12:00:00Z", got)
	}
}

// TestLatchBootstrap_PartialBindCaptureIsDiscarded is the Codex-found regression.
//
// The bug: one global bootBacklogKnown flag. With two bound consumers, a SUCCESSFUL
// bind read for A marked the aggregate captured, so a FAILED read for B did not
// "leave the capture unclaimed" — the tick fallback could never fire, and B's initial
// backlog was omitted forever. If A measured zero and B held work, the producer would
// publish `bootstrap_complete && bootstrap_scope == 0`: the contract's "authoritatively
// nothing to replay", asserted over a real initial build.
func TestLatchBootstrap_PartialBindCaptureIsDiscarded(t *testing.T) {
	c := withTestRegistry(t, &Component{})

	// Consumer A binds and reads 0. Consumer B's read FAILS.
	c.bootBacklog.Add(0)
	c.bootBacklogPartial.Store(true)

	// First whole observation sees both consumers' real work.
	scope, complete := c.latchBootstrap(700, false)
	if scope != 700 {
		t.Errorf("scope = %d, want 700 — a partial bind capture must be replaced by "+
			"the first whole observation, not kept", scope)
	}
	if complete {
		t.Error("700 outstanding is not complete")
	}

	// And it is still captured once: a later burst must not move it.
	if scope, _ = c.latchBootstrap(9000, false); scope != 700 {
		t.Errorf("scope = %d, want 700 (captured once)", scope)
	}
}

// TestLatchBootstrap_CompleteBindCaptureIsKept is the other half: when every bind read
// succeeded, the accumulated total is authoritative and the tick must NOT overwrite it
// with a later, smaller observation (the backlog is draining by then).
func TestLatchBootstrap_CompleteBindCaptureIsKept(t *testing.T) {
	c := withTestRegistry(t, &Component{})

	// Two consumers bound cleanly, 900 total at bind.
	c.bootBacklog.Add(400)
	c.bootBacklog.Add(500)

	// By the first tick, 850 have drained.
	scope, _ := c.latchBootstrap(50, false)
	if scope != 900 {
		t.Errorf("scope = %d, want 900 — a clean bind capture is the initial build; "+
			"the tick must not replace it with a drained observation", scope)
	}
}

// TestLatchBootstrap_NoBindReadRanTakesTheFirstObservation covers the deployment where
// the tick beats every bind read (or no consumer is bound yet).
func TestLatchBootstrap_NoBindReadRanTakesTheFirstObservation(t *testing.T) {
	c := withTestRegistry(t, &Component{})
	if scope, _ := c.latchBootstrap(120, false); scope != 120 {
		t.Errorf("scope = %d, want 120", scope)
	}
}
