package rule

import (
	"encoding/json"
	"testing"
	"time"
)

// The startup-wait budget is operator-reachable, so it owes a JSON round-trip
// over the real Config (no shadow struct). It also owes a floor check: gh#610
// made the rule processor wait for ENTITY_STATES instead of creating it, and
// exhausting that wait latches the graph-state guard degraded for the process
// lifetime. A budget that silently collapses to resource's smaller default is
// the failure this pins.
func TestStartupWaitConfigRoundTrip(t *testing.T) {
	const operatorJSON = `{
		"startup_attempts": 45,
		"startup_interval_ms": 250
	}`

	var cfg Config
	if err := json.Unmarshal([]byte(operatorJSON), &cfg); err != nil {
		t.Fatalf("decode operator config: %v", err)
	}
	if cfg.StartupAttempts != 45 {
		t.Errorf("StartupAttempts = %d, want 45", cfg.StartupAttempts)
	}
	if cfg.StartupInterval != 250 {
		t.Errorf("StartupInterval = %d, want 250", cfg.StartupInterval)
	}

	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("re-encode config: %v", err)
	}
	var restored Config
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatalf("decode re-encoded config: %v", err)
	}
	if restored.StartupAttempts != cfg.StartupAttempts {
		t.Errorf("StartupAttempts did not survive round-trip: %d != %d",
			restored.StartupAttempts, cfg.StartupAttempts)
	}
	if restored.StartupInterval != cfg.StartupInterval {
		t.Errorf("StartupInterval did not survive round-trip: %d != %d",
			restored.StartupInterval, cfg.StartupInterval)
	}
}

// The default must match every sibling ENTITY_STATES reader (graph-index,
// -spatial, -temporal, graph-embedding, graph-clustering all use 30 x 500ms).
// The rule processor waiting a third as long as its siblings for the same
// bucket is how a slow cold boot permanently disables rule evaluation.
func TestStartupWaitDefaultsMatchSiblingReaders(t *testing.T) {
	cfg := defaultConfig()
	if cfg.StartupAttempts != 30 {
		t.Errorf("default StartupAttempts = %d, want 30 (sibling ENTITY_STATES readers)", cfg.StartupAttempts)
	}
	if cfg.StartupInterval != 500 {
		t.Errorf("default StartupInterval = %d, want 500ms (sibling ENTITY_STATES readers)", cfg.StartupInterval)
	}
}

// A Config built without going through defaultConfig (tests, partial operator
// JSON) must still floor to the sibling budget rather than inheriting
// resource.DefaultConfig's 10 attempts.
func TestStartupWaitZeroValueFloorsToSiblingBudget(t *testing.T) {
	var cfg Config // zero value: StartupAttempts == 0

	attempts, interval := cfg.startupWaitBudget()

	if attempts != 30 {
		t.Errorf("zero-value config floored to %d attempts, want 30", attempts)
	}
	if interval != 500*time.Millisecond {
		t.Errorf("zero-value config floored to %v, want 500ms", interval)
	}

	// A negative value is a broken config, not a request for zero patience.
	negative := Config{StartupAttempts: -1, StartupInterval: -1}
	if attempts, interval = negative.startupWaitBudget(); attempts != 30 || interval != 500*time.Millisecond {
		t.Errorf("negative config gave %d attempts / %v, want 30 / 500ms", attempts, interval)
	}

	// An explicit operator value must survive the floor untouched.
	explicit := Config{StartupAttempts: 5, StartupInterval: 100}
	if attempts, interval = explicit.startupWaitBudget(); attempts != 5 || interval != 100*time.Millisecond {
		t.Errorf("explicit config was overridden: %d attempts / %v, want 5 / 100ms", attempts, interval)
	}
}
