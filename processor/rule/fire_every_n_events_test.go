package rule

import (
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShouldFireAction verifies the shouldFireAction helper directly.
// Modulo convention: the counter is incremented before the check, so the
// first fire occurs on match number N (not match number N-1). For N=2 that
// means matches 2, 4, 6, … For N=10 that means matches 10, 20, 30, …
func TestShouldFireAction(t *testing.T) {
	t.Parallel()

	t.Run("N=0 fires every event (fast-path, counter not touched)", func(t *testing.T) {
		t.Parallel()
		var c atomic.Int64
		for i := 0; i < 5; i++ {
			assert.True(t, shouldFireAction(0, &c), "N=0 must always fire (event %d)", i+1)
		}
		// Counter should not have been incremented for N=0
		assert.Equal(t, int64(0), c.Load(), "N=0 must not touch the counter")
	})

	t.Run("N=1 fires every event (fast-path, counter not touched)", func(t *testing.T) {
		t.Parallel()
		var c atomic.Int64
		for i := 0; i < 5; i++ {
			assert.True(t, shouldFireAction(1, &c), "N=1 must always fire (event %d)", i+1)
		}
		assert.Equal(t, int64(0), c.Load(), "N=1 must not touch the counter")
	})

	t.Run("N=2 fires on 2nd, 4th, 6th matching events", func(t *testing.T) {
		t.Parallel()
		var c atomic.Int64
		results := make([]bool, 6)
		for i := range results {
			results[i] = shouldFireAction(2, &c)
		}
		// Fired on 2nd and 4th and 6th (1-indexed)
		assert.False(t, results[0], "event 1: no fire")
		assert.True(t, results[1], "event 2: fire")
		assert.False(t, results[2], "event 3: no fire")
		assert.True(t, results[3], "event 4: fire")
		assert.False(t, results[4], "event 5: no fire")
		assert.True(t, results[5], "event 6: fire")
	})

	t.Run("N=10 fires on 10th, 20th, 30th matching events", func(t *testing.T) {
		t.Parallel()
		var c atomic.Int64
		fires := 0
		for i := 0; i < 30; i++ {
			if shouldFireAction(10, &c) {
				fires++
			}
		}
		assert.Equal(t, 3, fires, "30 events with N=10 must produce exactly 3 fires")
		assert.False(t, shouldFireAction(10, &c), "event 31 must not fire")
	})
}

// TestFireEveryNEvents_RulesDoNotShareCounters verifies that two rules with
// different FireEveryNEvents values each maintain independent counters.
func TestFireEveryNEvents_RulesDoNotShareCounters(t *testing.T) {
	t.Parallel()

	var counterA, counterB atomic.Int64

	// Rule A: fire every 3 events
	// Rule B: fire every 2 events
	type result struct{ aFired, bFired bool }
	results := make([]result, 6)
	for i := range results {
		results[i] = result{
			aFired: shouldFireAction(3, &counterA),
			bFired: shouldFireAction(2, &counterB),
		}
	}

	// Rule A fires on event 3 and 6
	assert.False(t, results[0].aFired, "A event 1")
	assert.False(t, results[1].aFired, "A event 2")
	assert.True(t, results[2].aFired, "A event 3: fire")
	assert.False(t, results[3].aFired, "A event 4")
	assert.False(t, results[4].aFired, "A event 5")
	assert.True(t, results[5].aFired, "A event 6: fire")

	// Rule B fires on event 2, 4, 6
	assert.False(t, results[0].bFired, "B event 1")
	assert.True(t, results[1].bFired, "B event 2: fire")
	assert.False(t, results[2].bFired, "B event 3")
	assert.True(t, results[3].bFired, "B event 4: fire")
	assert.False(t, results[4].bFired, "B event 5")
	assert.True(t, results[5].bFired, "B event 6: fire")
}

// TestValidateDefinition_FireEveryNEvents verifies that ValidateDefinition
// rejects negative values and accepts zero, one, and positive values.
func TestValidateDefinition_FireEveryNEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		n           int
		expectError bool
	}{
		{"negative rejects", -1, true},
		{"negative large rejects", -100, true},
		{"zero accepts (every event)", 0, false},
		{"one accepts (every event)", 1, false},
		{"two accepts", 2, false},
		{"ten accepts", 10, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			def := Definition{
				ID:               "test-rule",
				FireEveryNEvents: tt.n,
			}
			err := ValidateDefinition(def)
			if tt.expectError {
				require.Error(t, err, "expected validation error for N=%d", tt.n)
			} else {
				require.NoError(t, err, "unexpected validation error for N=%d", tt.n)
			}
		})
	}
}

// TestFireEveryNEvents_CounterResetOnRuleUpdate verifies the hot-reload
// semantics: when a rule definition is replaced via applyRuleChanges, the
// match counter resets so the new policy starts with a fresh rhythm.
//
// Scenario: rule with N=5. Fire 3 events (no action yet — need 5). Replace
// the rule definition. Fire 5 more events. Expect exactly 1 fire at the
// fresh counter's 5th event, NOT 2 fires (which would imply the old counter
// state of 3 survived the update and carried forward to fire at count 8→N=5).
func TestFireEveryNEvents_CounterResetOnRuleUpdate(t *testing.T) {
	t.Parallel()

	// Build a minimal Processor without NATS for unit-test purposes.
	// We exercise applyRuleChanges directly, so we only need the fields it touches.
	cfg := DefaultConfig()
	rp := &Processor{
		natsClient:      nil,
		logger:          slog.Default(),
		rules:           make(map[string]Rule),
		ruleDefinitions: make(map[string]Definition),
		ruleConfigs:     make(map[string]map[string]any),
		matchCounters:   make(map[string]*atomic.Int64),
		config:          &cfg,
	}

	const ruleID = "sampling-rule"

	// Manually install the rule and its initial counter (simulating loadRules).
	rp.mu.Lock()
	rp.rules[ruleID] = &alwaysMatchTestRule{name: ruleID}
	rp.ruleDefinitions[ruleID] = Definition{
		ID:               ruleID,
		Type:             "test_rule",
		Name:             ruleID,
		Enabled:          true,
		FireEveryNEvents: 5,
	}
	initialCounter := &atomic.Int64{}
	rp.matchCounters[ruleID] = initialCounter
	rp.mu.Unlock()

	// Drive 3 matches through the counter directly (simulating 3 incoming events
	// that matched but did not fire, because 3 < N=5).
	for i := 0; i < 3; i++ {
		shouldFireAction(5, initialCounter)
	}
	assert.Equal(t, int64(3), initialCounter.Load(), "pre-update counter should be 3")

	// Simulate hot-reload: applyRuleChanges replaces the rule and resets its counter.
	ruleMap := map[string]any{
		"type":                "test_rule", // registered in test_rule_factory.go init()
		"name":                ruleID,
		"enabled":             true,
		"fire_every_n_events": float64(5), // JSON numbers decode as float64
	}
	rp.mu.Lock()
	err := rp.applyRuleChanges(map[string]any{ruleID: ruleMap})
	rp.mu.Unlock()
	require.NoError(t, err, "applyRuleChanges must succeed")

	// Retrieve the freshly reset counter.
	rp.mu.RLock()
	newCounter := rp.matchCounters[ruleID]
	rp.mu.RUnlock()
	require.NotNil(t, newCounter, "counter must exist after applyRuleChanges")
	assert.Equal(t, int64(0), newCounter.Load(), "counter must be zero after rule update")
	assert.NotSame(t, initialCounter, newCounter, "counter must be a new pointer after rule update")

	// Drive 5 more events. Only the 5th should fire.
	fires := 0
	for i := 0; i < 5; i++ {
		if shouldFireAction(5, newCounter) {
			fires++
		}
	}
	assert.Equal(t, 1, fires, "exactly 1 fire after reset (on the 5th fresh match)")
}

// alwaysMatchTestRule is a minimal Rule that always reports a match but
// returns no events, used in unit tests that exercise Processor internals
// without NATS.
type alwaysMatchTestRule struct {
	name string
}

func (r *alwaysMatchTestRule) Name() string                      { return r.name }
func (r *alwaysMatchTestRule) Subscribe() []string               { return []string{">"} }
func (r *alwaysMatchTestRule) Evaluate(_ []message.Message) bool { return true }
func (r *alwaysMatchTestRule) ExecuteEvents(_ []message.Message) ([]Event, error) {
	return nil, nil
}
