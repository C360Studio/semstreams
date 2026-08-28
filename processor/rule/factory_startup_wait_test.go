package rule

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
)

// CreateRuleProcessor overlays operator fields onto defaultConfig() one field at
// a time, so a new config field is inert until it is added to that list. The
// Config-level round-trip test cannot catch that — it decodes straight into a
// Config and never crosses the factory, which is the production wire.
//
// This is the test that was missing when gh#610's startup knobs shipped
// accepted, validated, and published in the schema while the processor kept the
// hard-coded budget.
func TestFactoryAppliesStartupWaitBudget(t *testing.T) {
	tests := []struct {
		name             string
		rawConfig        string
		wantAttempts     int
		wantInterval     time.Duration
		wantIntervalDesc string
	}{
		{
			name:             "operator values reach the processor",
			rawConfig:        `{"pack_id":"test","startup_attempts":45,"startup_interval_ms":250}`,
			wantAttempts:     45,
			wantInterval:     250 * time.Millisecond,
			wantIntervalDesc: "250ms",
		},
		{
			name:             "omitted knobs keep the sibling-reader default",
			rawConfig:        `{"pack_id":"test"}`,
			wantAttempts:     defaultStartupAttempts,
			wantInterval:     defaultStartupInterval * time.Millisecond,
			wantIntervalDesc: "500ms",
		},
		{
			// A partial config must not let the omitted half clobber the default
			// back to zero — the field-by-field overlay makes that the natural bug.
			name:             "one knob set does not zero the other",
			rawConfig:        `{"pack_id":"test","startup_attempts":45}`,
			wantAttempts:     45,
			wantInterval:     defaultStartupInterval * time.Millisecond,
			wantIntervalDesc: "500ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := component.Dependencies{
				NATSClient: &natsclient.Client{},
				// The factory refuses an absent deployment authority (ADR-102 d2).
				Platform: component.PlatformMeta{Org: "acme", Platform: "ops"},
			}

			discoverable, err := CreateRuleProcessor(json.RawMessage(tt.rawConfig), deps)
			if err != nil {
				t.Fatalf("CreateRuleProcessor: %v", err)
			}

			rp, ok := discoverable.(*Processor)
			if !ok {
				t.Fatalf("CreateRuleProcessor returned %T, want *Processor", discoverable)
			}

			// Assert the EFFECTIVE budget the watcher will use, not the raw
			// field: getEntityStatesBucket reads it through startupWaitBudget.
			attempts, interval := rp.config.startupWaitBudget()
			if attempts != tt.wantAttempts {
				t.Errorf("effective StartupAttempts = %d, want %d", attempts, tt.wantAttempts)
			}
			if interval != tt.wantInterval {
				t.Errorf("effective StartupInterval = %v, want %s", interval, tt.wantIntervalDesc)
			}
		})
	}
}
