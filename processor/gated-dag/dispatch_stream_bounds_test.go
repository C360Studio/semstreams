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

// TestDispatchStream_RetentionMakesTheCeilingMeanBacklog covers the finding that
// the size ceiling introduced.
//
// Under "limits" retention a dispatch stream retains SUCCESSFULLY PROCESSED
// dispatches for the full MaxAge, so a finite MaxBytes is reached by acked history
// rather than by backlog. Paired with discard "new" — which is the right choice for
// a work stream in every other respect — that refuses all new dispatch on a
// perfectly healthy system, drained by nothing but time. At a few hundred bytes per
// envelope the default 256 MiB is roughly six dispatches a second for a day.
func TestDispatchStream_RetentionMakesTheCeilingMeanBacklog(t *testing.T) {
	cfg := Config{
		UnitEntityPrefix: "acme.ops.dag",
		DispatchSubject:  "dag.dispatch",
	}.withDefaults()

	retention, err := cfg.dispatchStreamRetention()
	require.NoError(t, err)
	assert.Equal(t, jetstream.WorkQueuePolicy, retention,
		"a dispatch is a request: deleting it on ack is what makes the byte ceiling mean backlog")

	// And the pair is coherent by default.
	require.NoError(t, cfg.Validate())
}

func TestDispatchStream_RetentionValidation(t *testing.T) {
	base := func() Config {
		return Config{
			UnitEntityPrefix: "acme.ops.dag",
			DispatchSubject:  "dag.dispatch",
		}.withDefaults()
	}

	// The combination that stops a healthy system is UNDECLARABLE, not documented.
	// The symptom — all dispatch refused while the consumer sits idle and healthy —
	// points nowhere near the cause, so an operator must not be able to configure it.
	t.Run("limits retention with discard new is refused", func(t *testing.T) {
		cfg := base()
		cfg.DispatchStreamRetention = "limits"
		cfg.DispatchStreamDiscard = "new"

		err := cfg.Validate()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "dispatch_stream_retention")
		assert.Contains(t, err.Error(), "processed history",
			"the operator has to be told WHY, or they will read it as an arbitrary restriction")
		assert.Contains(t, err.Error(), `"old"`, "and be given the escape")
	})

	t.Run("limits retention with discard old is allowed", func(t *testing.T) {
		cfg := base()
		cfg.DispatchStreamRetention = "limits"
		cfg.DispatchStreamDiscard = "old"

		require.NoError(t, cfg.Validate(),
			"several independent consumers of one dispatch subject need limits retention")
	})

	t.Run("an unrecognized retention spelling is rejected, not defaulted", func(t *testing.T) {
		cfg := base()
		cfg.DispatchStreamRetention = "work_queue" // the natsclient spelling, not this one

		err := cfg.Validate()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "dispatch_stream_retention")
		assert.Contains(t, err.Error(), `"work_queue"`)
	})

	// "interest" is deliberately absent: it deletes once every SUBSCRIBED consumer
	// acks, which for an adopter-wired consumer makes durability depend on who
	// happened to be listening — the opposite of what ADR-070 made this stream for.
	t.Run("interest retention is not offered", func(t *testing.T) {
		cfg := base()
		cfg.DispatchStreamRetention = "interest"

		require.Error(t, cfg.Validate())
	})
}
