package config

import (
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// observedStream is a live JetStream configuration carrying plenty the framework
// declaration says nothing about, so a reconcile that resets any of it shows up.
func observedStream() jetstream.StreamConfig {
	return jetstream.StreamConfig{
		Name:              "OBSERVED",
		Subjects:          []string{"observed.>"},
		Storage:           jetstream.MemoryStorage,
		Retention:         jetstream.LimitsPolicy,
		MaxAge:            24 * time.Hour,
		MaxBytes:          4 * 1024 * 1024,
		Discard:           jetstream.DiscardOld,
		Replicas:          3,
		Duplicates:        3 * time.Minute,
		Description:       "set by whoever created this stream",
		MaxMsgs:           9999,
		MaxMsgsPerSubject: 7,
		MaxMsgSize:        4096,
		AllowDirect:       true,
		DenyDelete:        true,
		Metadata:          map[string]string{"operator": "keep-me"},
	}
}

// declaredFor builds the configuration buildStreamConfig would produce, so the
// unit tests exercise the same declaration -> jetstream.StreamConfig path
// production uses rather than a hand-written struct that could disagree with it.
func declaredFor(t *testing.T, decl streamDeclaration) jetstream.StreamConfig {
	t.Helper()
	cfg, err := buildStreamConfig(decl, nil)
	require.NoError(t, err)
	return cfg
}

func observedDeclaration() streamDeclaration {
	return streamDeclaration{
		name:   "OBSERVED",
		source: "test",
		cfg: StreamConfig{
			Subjects: []string{"observed.>"},
			Storage:  "memory",
			MaxAge:   "24h",
			MaxBytes: 4 * 1024 * 1024,
			Discard:  StreamDiscardOld,
		},
	}
}

// ---------------------------------------------------------------------------
// 5.5 — editable drift is reconciled, and only what the declaration governs
// ---------------------------------------------------------------------------

// TestReconcileStreamConfig_RepairsEachGovernedFieldAlone pins the per-field
// trigger. Every case diverges in exactly ONE field, so none of them can pass
// because a sibling field triggered the update.
func TestReconcileStreamConfig_RepairsEachGovernedFieldAlone(t *testing.T) {
	tests := []struct {
		name      string
		declare   func(*StreamConfig)
		wantField string
		wantDrift string
		assert    func(*testing.T, jetstream.StreamConfig)
	}{
		{
			name:      "max_age",
			declare:   func(c *StreamConfig) { c.MaxAge = "48h" },
			wantField: "max_age",
			wantDrift: "max_age: observed=24h0m0s declared=48h0m0s",
			assert: func(t *testing.T, u jetstream.StreamConfig) {
				t.Helper()
				assert.Equal(t, 48*time.Hour, u.MaxAge)
			},
		},
		{
			name:      "max_bytes",
			declare:   func(c *StreamConfig) { c.MaxBytes = 8 * 1024 * 1024 },
			wantField: "max_bytes",
			wantDrift: "max_bytes: observed=4194304 declared=8388608",
			assert: func(t *testing.T, u jetstream.StreamConfig) {
				t.Helper()
				assert.Equal(t, int64(8*1024*1024), u.MaxBytes)
			},
		},
		{
			name:      "discard",
			declare:   func(c *StreamConfig) { c.Discard = StreamDiscardNew },
			wantField: "discard",
			wantDrift: "discard: observed=old declared=new",
			assert: func(t *testing.T, u jetstream.StreamConfig) {
				t.Helper()
				assert.Equal(t, jetstream.DiscardNew, u.Discard)
			},
		},
		{
			name:      "subjects",
			declare:   func(c *StreamConfig) { c.Subjects = []string{"observed.>", "extra.>"} },
			wantField: "subjects",
			wantDrift: "subjects: observed=observed.> declared=observed.>,extra.>",
			assert: func(t *testing.T, u jetstream.StreamConfig) {
				t.Helper()
				assert.Equal(t, []string{"observed.>", "extra.>"}, u.Subjects)
			},
		},
		{
			name:      "duplicates",
			declare:   func(c *StreamConfig) { c.Duplicates = "5m" },
			wantField: "duplicates",
			wantDrift: "duplicates: observed=3m0s declared=5m0s",
			assert: func(t *testing.T, u jetstream.StreamConfig) {
				t.Helper()
				assert.Equal(t, 5*time.Minute, u.Duplicates)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decl := observedDeclaration()
			tt.declare(&decl.cfg)

			update, drifts := reconcileStreamConfig(decl, observedStream(), declaredFor(t, decl), nil)

			require.Len(t, drifts, 1, "exactly one field diverges: %v", driftLabels(drifts))
			assert.Equal(t, tt.wantField, drifts[0].field)
			assert.Equal(t, tt.wantDrift, drifts[0].String(),
				"the drift must carry BOTH the observed and the declared value")
			tt.assert(t, update)

			// Nothing the declaration does not govern may move.
			assert.Equal(t, "set by whoever created this stream", update.Description)
			assert.Equal(t, int64(9999), update.MaxMsgs)
			assert.Equal(t, int64(7), update.MaxMsgsPerSubject)
			assert.Equal(t, int32(4096), update.MaxMsgSize)
			assert.True(t, update.AllowDirect)
			assert.True(t, update.DenyDelete)
			assert.Equal(t, map[string]string{"operator": "keep-me"}, update.Metadata)
			assert.Equal(t, 3, update.Replicas,
				"replicas is never restamped: a max_age repair must not scale an operator's stream down")
			if tt.wantField != "duplicates" {
				assert.Equal(t, 3*time.Minute, update.Duplicates,
					"an undeclared duplicate window is preserved, not reset to the server default")
			}
		})
	}
}

// TestReconcileStreamConfig_NoDriftWhenLiveMatchesDeclaration is the idempotence
// half: a matching stream produces no drift, so no UpdateStream is issued and
// the log line means something when it appears.
func TestReconcileStreamConfig_NoDriftWhenLiveMatchesDeclaration(t *testing.T) {
	decl := observedDeclaration()
	_, drifts := reconcileStreamConfig(decl, observedStream(), declaredFor(t, decl), nil)
	assert.Empty(t, drifts, "a stream matching its declaration must not be re-stamped")
}

// TestReconcileStreamConfig_UnlimitedSpellingsAreNotDrift is the perpetual-drift
// guard: NATS reports an absent byte limit as -1 while a declaration spells
// unlimited as 0, so a raw comparison would re-stamp every archival and bridged
// stream on every boot forever.
func TestReconcileStreamConfig_UnlimitedSpellingsAreNotDrift(t *testing.T) {
	observed := observedStream()
	observed.MaxAge = 0
	observed.MaxBytes = -1
	observed.Discard = jetstream.DiscardNew

	decl := streamDeclaration{
		name:      "OBSERVED",
		source:    `archival_streams["OBSERVED"]`,
		cfg:       StreamConfig{Subjects: []string{"observed.>"}, Storage: "memory"},
		exemption: exemptArchival,
	}

	_, drifts := reconcileStreamConfig(decl, observed, declaredFor(t, decl), nil)
	assert.Empty(t, drifts, "-1 and 0 are the same absent byte limit: %v", driftLabels(drifts))
}

// TestReconcileStreamConfig_ClampsPreservedDuplicateWindowUnderRepairedMaxAge
// covers a failure mode that only becomes reachable once max_age is a repair
// trigger: the repaired age can land below a PRESERVED duplicate window, and the
// server rejects rather than clamps such a window, so the repair would abort
// boot instead of fixing anything.
func TestReconcileStreamConfig_ClampsPreservedDuplicateWindowUnderRepairedMaxAge(t *testing.T) {
	decl := observedDeclaration()
	decl.cfg.MaxAge = "1m" // below the observed 3m duplicate window

	update, drifts := reconcileStreamConfig(decl, observedStream(), declaredFor(t, decl), nil)

	require.Len(t, drifts, 1)
	assert.Equal(t, "max_age", drifts[0].field)
	assert.Equal(t, time.Minute, update.MaxAge)
	assert.Equal(t, time.Minute, update.Duplicates,
		"a preserved window above the repaired max_age must clamp, not be sent and rejected")
}

// ---------------------------------------------------------------------------
// Governance — reconciling a field the declaration does not govern is restamping
// ---------------------------------------------------------------------------

// TestBoundsGovernedBy covers the three exemption classes. The migration-override
// row is the load-bearing one: resolveBounds falls back to DiscardOld for a
// policy the operator never wrote, and stamping that fallback onto a legacy
// stream would have the bridge change the behavior it exists to preserve.
func TestBoundsGovernedBy(t *testing.T) {
	tests := []struct {
		name string
		decl streamDeclaration
		want governedBounds
	}{
		{
			name: "an ordinary stream governs all three because it must declare all three",
			decl: streamDeclaration{cfg: StreamConfig{MaxAge: "24h", MaxBytes: 1024, Discard: StreamDiscardOld}},
			want: governedBounds{maxAge: true, maxBytes: true, discard: true},
		},
		{
			name: "archival governs all three: permanence IS the declaration",
			decl: streamDeclaration{exemption: exemptArchival},
			want: governedBounds{maxAge: true, maxBytes: true, discard: true},
		},
		{
			name: "a bridge that declares nothing governs nothing",
			decl: streamDeclaration{exemption: exemptMigrationOverride},
			want: governedBounds{},
		},
		{
			name: "a bridge governs exactly what it wrote",
			decl: streamDeclaration{
				exemption: exemptMigrationOverride,
				cfg:       StreamConfig{MaxBytes: 1024},
			},
			want: governedBounds{maxBytes: true},
		},
		{
			name: "an unrecognized discard spelling is not a declaration",
			decl: streamDeclaration{
				exemption: exemptMigrationOverride,
				cfg:       StreamConfig{Discard: "oldest"},
			},
			want: governedBounds{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, boundsGovernedBy(tt.decl))
		})
	}
}

// TestReconcileStreamConfig_BridgeLeavesUndeclaredBoundsAlone is the behavior
// TestBoundsGovernedBy's override rows exist to protect, through the reconciler
// itself: a legacy stream under a bridge keeps every bound its operator did not
// re-declare.
func TestReconcileStreamConfig_BridgeLeavesUndeclaredBoundsAlone(t *testing.T) {
	observed := observedStream()
	observed.MaxAge = 168 * time.Hour
	observed.MaxBytes = 5 * 1024 * 1024
	observed.Discard = jetstream.DiscardNew

	decl := streamDeclaration{
		name:      "OBSERVED",
		source:    `stream_migration_overrides["OBSERVED"]`,
		cfg:       StreamConfig{Subjects: []string{"observed.>"}, Storage: "memory"},
		exemption: exemptMigrationOverride,
	}

	update, drifts := reconcileStreamConfig(decl, observed, declaredFor(t, decl), nil)

	assert.Empty(t, drifts, "a bridge declares no bounds, so it drifts from none: %v", driftLabels(drifts))
	assert.Equal(t, 168*time.Hour, update.MaxAge)
	assert.Equal(t, int64(5*1024*1024), update.MaxBytes)
	assert.Equal(t, jetstream.DiscardNew, update.Discard)
}

// ---------------------------------------------------------------------------
// 5.6 — drift the provisioner will not change in place fails readiness
// ---------------------------------------------------------------------------

// TestNonEditableDrift reports both configurations for a governed divergence and
// stays silent for a field the declaration never named. The undeclared rows are
// the ones that keep this from being a boot-breaking false alarm: an omitted
// `storage` resolves to file at CREATION but governs nothing on a stream someone
// deliberately created in memory.
func TestNonEditableDrift(t *testing.T) {
	tests := []struct {
		name     string
		declared StreamConfig
		observed func(*jetstream.StreamConfig)
		want     []string
	}{
		{
			name:     "declared storage tier diverges",
			declared: StreamConfig{Storage: "file"},
			observed: func(c *jetstream.StreamConfig) { c.Storage = jetstream.MemoryStorage },
			want:     []string{"storage: observed=memory declared=file"},
		},
		{
			name:     "declared retention policy diverges",
			declared: StreamConfig{Retention: "interest"},
			observed: func(c *jetstream.StreamConfig) { c.Retention = jetstream.LimitsPolicy },
			want:     []string{"retention: observed=limits declared=interest"},
		},
		{
			name:     "workqueue divergence is reported too",
			declared: StreamConfig{Retention: "workqueue"},
			observed: func(c *jetstream.StreamConfig) { c.Retention = jetstream.LimitsPolicy },
			want:     []string{"retention: observed=limits declared=workqueue"},
		},
		{
			name:     "both diverge and both are reported",
			declared: StreamConfig{Storage: "memory", Retention: "interest"},
			observed: func(c *jetstream.StreamConfig) {
				c.Storage = jetstream.FileStorage
				c.Retention = jetstream.LimitsPolicy
			},
			want: []string{
				"storage: observed=file declared=memory",
				"retention: observed=limits declared=interest",
			},
		},
		{
			name:     "an undeclared storage tier governs nothing",
			declared: StreamConfig{},
			observed: func(c *jetstream.StreamConfig) { c.Storage = jetstream.MemoryStorage },
			want:     nil,
		},
		{
			name:     "an undeclared retention policy governs nothing",
			declared: StreamConfig{},
			observed: func(c *jetstream.StreamConfig) { c.Retention = jetstream.InterestPolicy },
			want:     nil,
		},
		{
			name:     "an unrecognized spelling is not a declaration",
			declared: StreamConfig{Storage: "in-memory", Retention: "workqueues"},
			observed: func(c *jetstream.StreamConfig) {
				c.Storage = jetstream.MemoryStorage
				c.Retention = jetstream.InterestPolicy
			},
			want: nil,
		},
		{
			name:     "agreement is not drift",
			declared: StreamConfig{Storage: "memory", Retention: "limits"},
			observed: func(_ *jetstream.StreamConfig) {},
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observed := observedStream()
			tt.observed(&observed)

			got := nonEditableDrift(streamDeclaration{name: "OBSERVED", cfg: tt.declared}, observed)
			if tt.want == nil {
				assert.Empty(t, got, "no governed field diverges: %v", driftLabels(got))
				return
			}
			assert.Equal(t, tt.want, driftLabels(got))
		})
	}
}

// TestNonEditableDriftRemedy_StatesWhatTheServerActuallyRefuses keeps the
// diagnostic honest about the two different reasons a field is in this set. The
// server refuses a storage change outright; it would ACCEPT limits <-> interest,
// and the framework declines anyway because that switch starts deleting acked
// messages. An operator told "the server won't let you" about a change the
// server would allow goes looking for the wrong fix.
func TestNonEditableDriftRemedy_StatesWhatTheServerActuallyRefuses(t *testing.T) {
	for _, want := range []string{"storage-type change", "workqueue", "interest", "acknowledged messages"} {
		assert.Contains(t, nonEditableDriftRemedy, want)
	}
}

// TestDeclaredStorageAndRetention_ResolveTheSameWayCreationDoes guards the
// refactor that made buildStreamConfig share these helpers: an absent or
// unrecognized spelling must still resolve to the historical creation default,
// or this slice would silently change what new streams are created with.
func TestDeclaredStorageAndRetention_ResolveTheSameWayCreationDoes(t *testing.T) {
	for _, spelling := range []string{"", "nonsense", "FILE"} {
		storage, governed := declaredStorage(StreamConfig{Storage: spelling})
		assert.Equal(t, jetstream.FileStorage, storage, "storage %q", spelling)
		assert.False(t, governed, "storage %q must not govern an existing stream", spelling)

		retention, governed := declaredRetention(StreamConfig{Retention: spelling})
		assert.Equal(t, jetstream.LimitsPolicy, retention, "retention %q", spelling)
		assert.False(t, governed, "retention %q must not govern an existing stream", spelling)
	}
}
