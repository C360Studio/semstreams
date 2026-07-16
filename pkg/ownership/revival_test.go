// Package ownership — unit tests for the ADR-056 PR-4 Watch-revival quiesce
// detection logic. These exercise the pure decision + the in-memory
// detect-and-latch path (decideRevival, epoch.incarnationOfOwner,
// markQuiesced/IsQuiesced, processEpochUpdate) WITHOUT NATS — the full
// WatchRevival goroutine over a real epoch watcher is covered in
// revival_integration_test.go.
package ownership

import (
	"testing"

	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecideRevival(t *testing.T) {
	t.Parallel()
	const mine = "aaaa111122223333"
	tests := []struct {
		name    string
		liveInc string
		present bool
		want    revivalDecision
	}{
		{"absent → warn-only", "", false, revivalAbsentWarn},
		{"present, same incarnation → none", mine, true, revivalNone},
		{"present, different incarnation → quiesce", "bbbb444455556666", true, revivalQuiesce},
		{"present, empty (legacy/pre-fence) incarnation → none", "", true, revivalNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, decideRevival(mine, tt.liveInc, tt.present))
		})
	}
}

func TestEpoch_IncarnationOfOwner(t *testing.T) {
	t.Parallel()
	ep := newEpoch()
	ep.Owners["owns"] = ownerEntry{Claims: []OwnerClaim{{
		Owner: "owns", Pattern: "a.b.c.d.e.*", Predicates: []string{semantictest.Predicate(t, "test", "value", "p")},
		Mode: ModeReplaceOwned, Incarnation: "inc-owns",
	}}}
	ep.Owners["appends"] = ownerEntry{Claims: []OwnerClaim{{
		Owner: "appends", Pattern: "a.b.c.d.e.*", Predicates: []string{semantictest.Predicate(t, "test", "value", "p")},
		Mode: ModeAppendEvidence, Incarnation: "inc-appends",
	}}}
	ep.Owners["fe-only"] = ownerEntry{ForeignEdges: []ForeignEdgeClaim{{
		Owner: "fe-only", Predicate: semantictest.Predicate(t, "test", "edge", "rel"), Mode: EdgeStrict,
	}}}

	t.Run("owning claim returns incarnation, present", func(t *testing.T) {
		inc, present := ep.incarnationOfOwner("owns")
		assert.True(t, present)
		assert.Equal(t, "inc-owns", inc)
	})
	t.Run("append-evidence-only owner is not present (no write-fence)", func(t *testing.T) {
		_, present := ep.incarnationOfOwner("appends")
		assert.False(t, present)
	})
	t.Run("foreign-edge-only owner is not present", func(t *testing.T) {
		_, present := ep.incarnationOfOwner("fe-only")
		assert.False(t, present)
	})
	t.Run("unknown owner is not present", func(t *testing.T) {
		_, present := ep.incarnationOfOwner("nope")
		assert.False(t, present)
	})
}

func TestRegistry_MarkQuiesced_LatchesAndIsIdempotent(t *testing.T) {
	t.Parallel()
	reg := newNoopRegistry(t)
	assert.False(t, reg.IsQuiesced("o"), "owner is not quiesced initially")
	assert.True(t, reg.markQuiesced("o"), "first markQuiesced is the 0→1 transition")
	assert.True(t, reg.IsQuiesced("o"), "owner is quiesced after markQuiesced")
	assert.False(t, reg.markQuiesced("o"), "second markQuiesced is a no-op (already latched)")
	assert.True(t, reg.IsQuiesced("o"), "owner stays quiesced (latching)")
}

// TestRegistry_ProcessEpochUpdate drives the in-memory detection path: build an
// epoch value, register our owner, and assert processEpochUpdate quiesces only
// on supersession by a different incarnation — never on our own incarnation, a
// benign bump, or absence.
func TestRegistry_ProcessEpochUpdate(t *testing.T) {
	t.Parallel()
	counter := getOwnerRevivalQuiesceMetric(nil)

	encodeEpochWith := func(t *testing.T, owner, incarnation string) []byte {
		t.Helper()
		ep := newEpoch()
		ep.Version = 7
		ep.Owners[owner] = ownerEntry{Claims: []OwnerClaim{{
			Owner: owner, Pattern: "a.b.c.d.e.*", Predicates: []string{"test.value.p"},
			Mode: ModeReplaceOwned, Incarnation: incarnation,
		}}}
		b, err := ep.encode()
		require.NoError(t, err)
		return b
	}

	t.Run("supersession by different incarnation → quiesce", func(t *testing.T) {
		t.Parallel()
		reg := newNoopRegistry(t)
		reg.registered["mine"] = struct{}{}
		reg.processEpochUpdate(encodeEpochWith(t, "mine", "RIVAL-incarnation"), counter)
		assert.True(t, reg.IsQuiesced("mine"), "a different live incarnation must quiesce our owner")
	})

	t.Run("our own incarnation → not quiesced", func(t *testing.T) {
		t.Parallel()
		reg := newNoopRegistry(t)
		reg.registered["mine"] = struct{}{}
		reg.processEpochUpdate(encodeEpochWith(t, "mine", reg.incarnation), counter)
		assert.False(t, reg.IsQuiesced("mine"), "our own incarnation must never quiesce")
	})

	t.Run("benign bump of an unrelated owner → not quiesced", func(t *testing.T) {
		t.Parallel()
		reg := newNoopRegistry(t)
		reg.registered["mine"] = struct{}{}
		reg.processEpochUpdate(encodeEpochWith(t, "someone-else", "their-incarnation"), counter)
		assert.False(t, reg.IsQuiesced("mine"), "an epoch bump that does not touch our owner must not quiesce")
	})

	t.Run("our owner absent → not quiesced (warn-only)", func(t *testing.T) {
		t.Parallel()
		reg := newNoopRegistry(t)
		reg.registered["mine"] = struct{}{}
		reg.processEpochUpdate(encodeEpochWith(t, "other", "other-inc"), counter)
		assert.False(t, reg.IsQuiesced("mine"), "absence (no rival) must not quiesce — warn-only")
	})

	t.Run("post-update signal fires for tests", func(t *testing.T) {
		t.Parallel()
		reg := newNoopRegistry(t)
		updates := make(chan struct{}, 1)
		reg.setRevivalUpdates(updates)
		reg.processEpochUpdate(encodeEpochWith(t, "x", "y"), counter)
		select {
		case <-updates:
		default:
			t.Fatal("processEpochUpdate must fire the revivalUpdates signal")
		}
	})
}
