package gateddagexec

import (
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/natsclient"
)

// The dispatch stream is the OPERATOR-REACHABLE side of the bounds requirement:
// dispatch_stream is config JSON passed to EnsureStream, which is why a guard
// living only in the config package's stream provisioner would have left this path
// open.

// dispatchStreamConfigFor mirrors what Start hands to EnsureStream, so a test can
// judge the same configuration the server would get.
func dispatchStreamConfigFor(t *testing.T, cfg Config) jetstream.StreamConfig {
	t.Helper()
	maxAge, err := cfg.dispatchStreamMaxAge()
	require.NoError(t, err)
	discard, err := cfg.dispatchStreamDiscard()
	require.NoError(t, err)
	return jetstream.StreamConfig{
		Name:     cfg.DispatchStream,
		Subjects: []string{cfg.DispatchSubject},
		MaxAge:   maxAge,
		MaxBytes: cfg.DispatchStreamMaxBytes,
		Discard:  discard,
	}
}

// TestDispatchStream_DefaultsSatisfyTheBoundsRequirement pins that an operator who
// configures nothing still gets a declared stream. Before this the defaults
// produced MaxBytes 0, which EnsureStream now refuses — and which the server read
// as unlimited.
func TestDispatchStream_DefaultsSatisfyTheBoundsRequirement(t *testing.T) {
	cfg := Config{
		UnitEntityPrefix: "acme.ops.dag",
		DispatchSubject:  "dag.dispatch",
	}.withDefaults()

	require.NoError(t, natsclient.CheckStreamBounds(
		dispatchStreamConfigFor(t, cfg), "gated-dag Start"))
	assert.Positive(t, cfg.DispatchStreamMaxBytes)
}

// TestDispatchStream_DefaultDiscardRefusesRatherThanDrops is the choice that
// matters most here, and it is the OPPOSITE of the framework default.
//
// A dispatch is a REQUEST to do work. Evicting the oldest at the ceiling silently
// drops a claimed unit's dispatch, which is the precise failure ADR-070 made this
// stream durable to prevent; refusing the newest surfaces as a publish error the
// executor can see.
func TestDispatchStream_DefaultDiscardRefusesRatherThanDrops(t *testing.T) {
	cfg := Config{
		UnitEntityPrefix: "acme.ops.dag",
		DispatchSubject:  "dag.dispatch",
	}.withDefaults()

	discard, err := cfg.dispatchStreamDiscard()
	require.NoError(t, err)
	assert.Equal(t, jetstream.DiscardNew, discard,
		"a work stream must refuse the newest dispatch, not silently strand a claimed unit")
}

func TestDispatchStream_BoundsValidation(t *testing.T) {
	base := func() Config {
		return Config{
			UnitEntityPrefix: "acme.ops.dag",
			DispatchSubject:  "dag.dispatch",
		}.withDefaults()
	}

	t.Run("an omitted max_bytes takes the default rather than unlimited", func(t *testing.T) {
		cfg := base()
		cfg.DispatchStreamMaxBytes = 0
		cfg = cfg.withDefaults()

		require.NoError(t, cfg.Validate())
		assert.Equal(t, defaultDispatchStreamMaxBytes, cfg.DispatchStreamMaxBytes,
			"zero means unlimited to JetStream, and an operator who omitted the field did not ask for that")
	})

	t.Run("a negative max_bytes is rejected, not defaulted", func(t *testing.T) {
		cfg := base()
		cfg.DispatchStreamMaxBytes = -1

		err := cfg.Validate()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "dispatch_stream_max_bytes")
		assert.Contains(t, err.Error(), "unlimited",
			"the operator has to be told what -1 actually means to JetStream")
	})

	t.Run("an unrecognized discard spelling is rejected, not defaulted", func(t *testing.T) {
		cfg := base()
		cfg.DispatchStreamDiscard = "oldest"

		err := cfg.Validate()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "dispatch_stream_discard")
		assert.Contains(t, err.Error(), `"oldest"`)
	})

	t.Run("both spellings are accepted", func(t *testing.T) {
		for spelling, want := range map[string]jetstream.DiscardPolicy{
			"new": jetstream.DiscardNew,
			"old": jetstream.DiscardOld,
		} {
			cfg := base()
			cfg.DispatchStreamDiscard = spelling
			require.NoError(t, cfg.Validate(), "spelling %q", spelling)

			got, err := cfg.dispatchStreamDiscard()
			require.NoError(t, err)
			assert.Equal(t, want, got, "spelling %q", spelling)
		}
	})
}
