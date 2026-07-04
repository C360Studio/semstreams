package gateddagexec

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func validCfg() Config {
	c := DefaultConfig()
	c.UnitEntityPrefix = "acme.ops.plan.fanout.unit"
	c.DispatchSubject = "gateddag.dispatch.unit"
	return c
}

func TestConfig_DefaultsFillUnsetFields(t *testing.T) {
	c := Config{UnitEntityPrefix: "p", DispatchSubject: "s"}.withDefaults()
	require.Equal(t, FanOutWorkflow, c.FanOutWorkflow)
	require.Equal(t, defaultCompletedPredicate, c.CompletedPredicate)
	require.Equal(t, defaultClaimPredicate, c.ClaimPredicate)
	require.Equal(t, defaultWorkers, c.Workers)
	require.Equal(t, defaultQueueSize, c.QueueSize)
	require.Equal(t, defaultMaxUnits, c.MaxUnits)
	require.Equal(t, FailurePolicyContinueOthers, c.FailurePolicy)
	require.NoError(t, c.Validate())
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		errSub string
	}{
		{"missing prefix", func(c *Config) { c.UnitEntityPrefix = "" }, "unit_entity_prefix is required"},
		{"missing subject", func(c *Config) { c.DispatchSubject = "" }, "dispatch_subject is required"},
		{"empty workflow", func(c *Config) { c.FanOutWorkflow = "" }, "fan_out_workflow"},
		{"zero workers", func(c *Config) { c.Workers = 0 }, "workers must be > 0"},
		{"negative workers", func(c *Config) { c.Workers = -1 }, "workers must be > 0"},
		{"zero queue", func(c *Config) { c.QueueSize = 0 }, "queue_size must be > 0"},
		{"zero max units", func(c *Config) { c.MaxUnits = 0 }, "max_units must be > 0"},
		{"bad backstop", func(c *Config) { c.BackstopInterval = "nope" }, "backstop_interval"},
		{"nonpositive backstop", func(c *Config) { c.BackstopInterval = "0s" }, "backstop_interval"},
		{"bad query timeout", func(c *Config) { c.QueryTimeout = "" }, "query_timeout"},
		{"bad failure policy", func(c *Config) { c.FailurePolicy = "panic" }, "failure_policy"},
		{"predicate collision", func(c *Config) { c.ClaimPredicate = c.CompletedPredicate }, "must be distinct"},
		{"empty predicate", func(c *Config) { c.DirtiedPredicate = "" }, "must not be empty"},
		{"missing dispatch stream", func(c *Config) { c.DispatchStream = "" }, "dispatch_stream is required"},
		{"bad dispatch stream max age", func(c *Config) { c.DispatchStreamMaxAge = "nope" }, "dispatch_stream_max_age"},
		{"nonpositive dispatch stream max age", func(c *Config) { c.DispatchStreamMaxAge = "0s" }, "dispatch_stream_max_age"},
		{"bad stranded after", func(c *Config) { c.StrandedAfter = "nope" }, "stranded_after"},
		{"negative stranded after", func(c *Config) { c.StrandedAfter = "-1s" }, "stranded_after"},
		{"instance id with custom workflow", func(c *Config) {
			c.FanOutInstanceID = "org.plat.gateddag.fanout.instance.x"
			c.FanOutWorkflow = "custom-wf"
		}, "requires the default fan_out_workflow"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validCfg()
			// Bad-failure-policy & empty-predicate paths need the field set
			// post-default, so mutate the already-defaulted config.
			tt.mutate(&c)
			err := c.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errSub)
		})
	}
}

func TestConfig_ValidateHappy(t *testing.T) {
	require.NoError(t, validCfg().Validate())
}

// TestConfig_JSONRoundTrip exercises EVERY operator-reachable field with a
// non-default value through marshal→unmarshal and asserts it survives. A
// mistyped struct tag would silently fall back to the default otherwise
// (operator-configurable-surface discipline). Uses non-default values so a
// dropped field is caught by inequality rather than coinciding with a default.
func TestConfig_JSONRoundTrip(t *testing.T) {
	in := Config{
		FanOutWorkflow:       "custom-fanout",
		UnitEntityPrefix:     "acme.ops.plan.fanout.unit",
		DispatchSubject:      "custom.dispatch",
		CompletedPredicate:   "x.done",
		FailedPredicate:      "x.failed",
		DirtiedPredicate:     "x.reset",
		DependsOnPredicate:   "x.needs",
		ClaimPredicate:       "x.inflight",
		Workers:              7,
		QueueSize:            99,
		BackstopInterval:     "12s",
		QueryTimeout:         "8s",
		MaxUnits:             250,
		FailurePolicy:        FailurePolicyStopOnFirstFailure,
		DispatchStream:       "CUSTOM_DISPATCH",
		DispatchStreamMaxAge: "48h",
		StrandedAfter:        "15m",
	}
	data, err := json.Marshal(in)
	require.NoError(t, err)

	var out Config
	require.NoError(t, json.Unmarshal(data, &out))
	require.Equal(t, in, out, "every operator field must survive a JSON round-trip")
}
