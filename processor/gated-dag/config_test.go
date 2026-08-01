package gateddagexec

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validCfg() Config {
	c := DefaultConfig()
	c.UnitEntityPrefix = "acme.ops.plan.fanout.unit"
	c.DispatchSubject = "gateddag.dispatch.unit"
	return c
}

func TestConfig_DefaultsFillUnsetFields(t *testing.T) {
	c := Config{UnitEntityPrefix: "acme", DispatchSubject: "s"}.withDefaults()
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
		{"wildcard prefix", func(c *Config) { c.UnitEntityPrefix = "acme.*" }, "unit_entity_prefix"},
		{"seven-part prefix", func(c *Config) { c.UnitEntityPrefix = "a.b.c.d.e.f.g" }, "unit_entity_prefix"},
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
		{"empty predicate", func(c *Config) { c.DirtiedPredicate = "" }, "must not be empty"},                                    // predicate-audit:invalid {"kind":"stored-predicate","value":"","reason":"empty"}
		{"noncanonical predicate", func(c *Config) { c.DirtiedPredicate = "gateddag.unit.dirtied_marker" }, "segment_character"}, // predicate-audit:invalid {"kind":"stored-predicate","value":"gateddag.unit.dirtied_marker","reason":"segment_character"}
		{"missing dispatch stream", func(c *Config) { c.DispatchStream = "" }, "dispatch_stream is required"},
		{"bad dispatch stream max age", func(c *Config) { c.DispatchStreamMaxAge = "nope" }, "dispatch_stream_max_age"},
		{"nonpositive dispatch stream max age", func(c *Config) { c.DispatchStreamMaxAge = "0s" }, "dispatch_stream_max_age"},
		{"bad dedupe window", func(c *Config) { c.DispatchDedupeWindow = "nope" }, "dispatch_dedupe_window"},
		{"dedupe window below backstop", func(c *Config) { c.DispatchDedupeWindow = "5s"; c.BackstopInterval = "30s" }, "must be >= backstop_interval"},
		{"bad stranded after", func(c *Config) { c.StrandedAfter = "nope" }, "stranded_after"},
		{"negative stranded after", func(c *Config) { c.StrandedAfter = "-1s" }, "stranded_after"},
		{"instance id with custom workflow", func(c *Config) {
			c.FanOutInstanceID = "org.plat.gateddag.fanout.instance.x"
			c.FanOutWorkflow = "custom-wf"
		}, "requires the default fan_out_workflow"},
		{"noncanonical instance id", func(c *Config) {
			c.FanOutInstanceID = "fanout-1"
		}, "fan_out_instance_id"},
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

func TestConfig_ValidateRejectsPredicateCollision(t *testing.T) {
	cfg := validCfg()
	cfg.ClaimPredicate = semantictest.Predicate(t, "gateddag", "unit", "completed")
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be distinct")
}

func TestConfig_ValidateHappy(t *testing.T) {
	cfg := validCfg()
	cfg.FanOutInstanceID = ""
	require.NoError(t, cfg.Validate(), "empty fan_out_instance_id is the explicit no-lifecycle sentinel")
}

func TestConfig_EntityIDByteBoundaries(t *testing.T) {
	t.Parallel()

	prefix256 := strings.Repeat("a", semtypes.MaxEntityIDBytes)
	// Pattern-conformant prefix: Validate now also matches FanOutInstanceID
	// against FanOutEntityIDPattern, and this test's subject is byte
	// boundaries, not pattern conformance — a generic a.b.c.d.e. prefix would
	// fail for the wrong reason.
	const fanOutPrefix = "a.b.gateddag.fanout.instance."
	id256 := fanOutPrefix + strings.Repeat("x", semtypes.MaxEntityIDBytes-len(fanOutPrefix))
	require.Len(t, prefix256, semtypes.MaxEntityIDBytes)
	require.Len(t, id256, semtypes.MaxEntityIDBytes)

	tests := []struct {
		name     string
		mutate   func(*Config)
		wantCode string
	}{
		{"256 byte prefix", func(c *Config) { c.UnitEntityPrefix = prefix256 }, ""},
		{"257 byte prefix", func(c *Config) { c.UnitEntityPrefix = prefix256 + "a" }, semtypes.ErrorCodeEntityIDPrefixInvalid},
		{"256 byte instance ID", func(c *Config) { c.FanOutInstanceID = id256 }, ""},
		{"257 byte instance ID", func(c *Config) { c.FanOutInstanceID = id256 + "x" }, semtypes.ErrorCodeEntityIDInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := validCfg()
			tt.mutate(&cfg)
			err := cfg.Validate()
			if tt.wantCode == "" {
				require.NoError(t, err)
				return
			}
			var classified *errs.ClassifiedError
			require.Error(t, err)
			require.True(t, errors.As(err, &classified), "entity contract error type must survive config context")
			assert.Equal(t, tt.wantCode, classified.Code)
		})
	}
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
		CompletedPredicate:   semantictest.Predicate(t, "test", "gateddag", "done"),
		FailedPredicate:      semantictest.Predicate(t, "test", "gateddag", "failed"),
		DirtiedPredicate:     semantictest.Predicate(t, "test", "gateddag", "reset"),
		DependsOnPredicate:   semantictest.Predicate(t, "test", "gateddag", "needs"),
		ClaimPredicate:       semantictest.Predicate(t, "test", "gateddag", "inflight"),
		Workers:              7,
		QueueSize:            99,
		BackstopInterval:     "12s",
		QueryTimeout:         "8s",
		MaxUnits:             250,
		FailurePolicy:        FailurePolicyStopOnFirstFailure,
		DispatchStream:       "CUSTOM_DISPATCH",
		DispatchStreamMaxAge: "48h",
		DispatchDedupeWindow: "3m",
		StrandedAfter:        "15m",
	}
	data, err := json.Marshal(in)
	require.NoError(t, err)

	var out Config
	require.NoError(t, json.Unmarshal(data, &out))
	require.Equal(t, in, out, "every operator field must survive a JSON round-trip")
}
