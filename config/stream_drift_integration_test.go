//go:build integration

package config

import (
	"bytes"
	"context"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/natsclient"
)

// driftTestManager builds a StreamsManager over a real JetStream-enabled NATS
// and CAPTURES its log output, because "the repair is logged with both observed
// and declared values" is part of the drift requirement rather than a debugging
// nicety: an operator whose stream was silently re-stamped at boot has no other
// record that it happened.
func driftTestManager(t *testing.T) (*StreamsManager, jetstream.JetStream, context.Context, *bytes.Buffer) {
	t.Helper()
	client := natsclient.NewTestClient(t, natsclient.WithJetStream())

	logs := &bytes.Buffer{}
	sm := NewStreamsManager(client.Client,
		slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	js, err := client.Client.JetStream()
	require.NoError(t, err)
	return sm, js, ctx, logs
}

// baselineStreamConfig is an existing stream that AGREES with baselineDeclaration
// on every field the declaration governs, and additionally carries configuration
// the declaration says nothing about. A drift subtest mutates exactly ONE
// governed field, so a repair cannot be credited to a sibling field having
// triggered the update — which is precisely how the pre-existing reconciler
// (subjects + duplicate window only) appeared to apply retention.
func baselineStreamConfig(name string) jetstream.StreamConfig {
	return jetstream.StreamConfig{
		Name:      name,
		Subjects:  []string{strings.ToLower(name) + ".>"},
		Storage:   jetstream.MemoryStorage,
		Retention: jetstream.LimitsPolicy,
		MaxAge:    24 * time.Hour,
		MaxBytes:  4 * 1024 * 1024,
		Discard:   jetstream.DiscardOld,
		Replicas:  1,

		// Everything below is UNGOVERNED: no StreamConfig field declares it, so a
		// reconcile that resets any of it has restamped another owner's choices.
		Duplicates:        3 * time.Minute,
		Description:       "operator note the framework never declared",
		MaxMsgs:           9999,
		MaxMsgsPerSubject: 7,
		MaxMsgSize:        4096,
		AllowDirect:       true,
		DenyDelete:        true,
		Metadata:          map[string]string{"operator": "keep-me"},
	}
}

// baselineDeclaration is the framework declaration matching baselineStreamConfig.
// It declares no duplicate window on purpose: the existing 3m window is an
// ungoverned field that must survive every repair.
func baselineDeclaration(name string) streamDeclaration {
	return streamDeclaration{
		name:   name,
		source: "test",
		cfg: StreamConfig{
			Subjects: []string{strings.ToLower(name) + ".>"},
			Storage:  "memory",
			MaxAge:   "24h",
			MaxBytes: 4 * 1024 * 1024,
			Discard:  StreamDiscardOld,
		},
	}
}

// assertUngovernedIntact fails when a repair touched configuration the
// declaration does not govern.
func assertUngovernedIntact(t *testing.T, live jetstream.StreamConfig) {
	t.Helper()
	assert.Equal(t, 3*time.Minute, live.Duplicates, "an undeclared duplicate window must survive a repair")
	assert.Equal(t, "operator note the framework never declared", live.Description)
	assert.Equal(t, int64(9999), live.MaxMsgs)
	assert.Equal(t, int64(7), live.MaxMsgsPerSubject)
	assert.Equal(t, int32(4096), live.MaxMsgSize)
	assert.True(t, live.AllowDirect, "AllowDirect must survive a repair")
	assert.True(t, live.DenyDelete, "DenyDelete must survive a repair")
	assert.Equal(t, "keep-me", live.Metadata["operator"], "operator metadata must survive a repair")
	assert.Equal(t, 1, live.Replicas)
}

// TestCreateStream_ReconcilesEachEditableFieldIndependently is the acceptance
// criterion for editable retention drift. Each subtest diverges in EXACTLY ONE
// governed field, so none of them can pass because a sibling field triggered the
// update: against the pre-existing reconciler every one of these is a no-op.
func TestCreateStream_ReconcilesEachEditableFieldIndependently(t *testing.T) {
	tests := []struct {
		name       string
		stream     string
		mutate     func(*jetstream.StreamConfig)
		assertLive func(*testing.T, jetstream.StreamConfig)
		wantLog    []string
	}{
		{
			name:   "max_age drift alone is repaired",
			stream: "DRIFTAGE",
			mutate: func(c *jetstream.StreamConfig) { c.MaxAge = 48 * time.Hour },
			assertLive: func(t *testing.T, live jetstream.StreamConfig) {
				t.Helper()
				assert.Equal(t, 24*time.Hour, live.MaxAge, "declared max_age must win")
				assert.Equal(t, int64(4*1024*1024), live.MaxBytes)
				assert.Equal(t, jetstream.DiscardOld, live.Discard)
			},
			wantLog: []string{"max_age", "observed=48h0m0s", "declared=24h0m0s"},
		},
		{
			name:   "max_bytes drift alone is repaired",
			stream: "DRIFTBYTES",
			mutate: func(c *jetstream.StreamConfig) { c.MaxBytes = 8 * 1024 * 1024 },
			assertLive: func(t *testing.T, live jetstream.StreamConfig) {
				t.Helper()
				assert.Equal(t, int64(4*1024*1024), live.MaxBytes, "declared max_bytes must win")
				assert.Equal(t, 24*time.Hour, live.MaxAge)
				assert.Equal(t, jetstream.DiscardOld, live.Discard)
			},
			wantLog: []string{"max_bytes", "observed=8388608", "declared=4194304"},
		},
		{
			name:   "discard drift alone is repaired",
			stream: "DRIFTDISCARD",
			mutate: func(c *jetstream.StreamConfig) { c.Discard = jetstream.DiscardNew },
			assertLive: func(t *testing.T, live jetstream.StreamConfig) {
				t.Helper()
				assert.Equal(t, jetstream.DiscardOld, live.Discard, "declared discard policy must win")
				assert.Equal(t, 24*time.Hour, live.MaxAge)
				assert.Equal(t, int64(4*1024*1024), live.MaxBytes)
			},
			wantLog: []string{"discard", "observed=new", "declared=old"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm, js, ctx, logs := driftTestManager(t)

			existing := baselineStreamConfig(tt.stream)
			tt.mutate(&existing)
			_, err := js.CreateStream(ctx, existing)
			require.NoError(t, err)

			require.NoError(t, sm.createStream(ctx, baselineDeclaration(tt.stream)))

			live := liveStreamConfig(t, js, ctx, tt.stream)
			tt.assertLive(t, live)
			assertUngovernedIntact(t, live)

			for _, want := range tt.wantLog {
				assert.Contains(t, logs.String(), want,
					"the repair must be logged with both observed and declared values")
			}
		})
	}
}

// TestCreateStream_NoDriftIssuesNoUpdate pins the other half of the reconciler:
// a boot against a stream that already matches its declaration must not log a
// repair, so "Reconciling stream configuration drift" in a log means something.
func TestCreateStream_NoDriftIssuesNoUpdate(t *testing.T) {
	sm, js, ctx, logs := driftTestManager(t)

	_, err := js.CreateStream(ctx, baselineStreamConfig("DRIFTCLEAN"))
	require.NoError(t, err)

	require.NoError(t, sm.createStream(ctx, baselineDeclaration("DRIFTCLEAN")))

	assert.NotContains(t, logs.String(), "Reconciling stream configuration drift",
		"a stream that matches its declaration must not be re-stamped every boot")
	assertUngovernedIntact(t, liveStreamConfig(t, js, ctx, "DRIFTCLEAN"))
}

// TestCreateStream_NonEditableDriftFailsReadiness is the fail-closed half of the
// requirement. JetStream refuses a storage-type change outright, and the
// framework refuses to flip retention for the operator even where the server
// would allow it, so both must fail readiness naming observed AND declared
// rather than being silently accepted as they are today.
func TestCreateStream_NonEditableDriftFailsReadiness(t *testing.T) {
	tests := []struct {
		name     string
		stream   string
		mutate   func(*jetstream.StreamConfig)
		declare  func(*StreamConfig)
		wantMsg  []string
		liveWant func(*testing.T, jetstream.StreamConfig)
	}{
		{
			name:    "storage tier drift",
			stream:  "DRIFTSTORAGE",
			mutate:  func(c *jetstream.StreamConfig) { c.Storage = jetstream.MemoryStorage },
			declare: func(c *StreamConfig) { c.Storage = "file" },
			wantMsg: []string{"storage", "observed=memory", "declared=file"},
			liveWant: func(t *testing.T, live jetstream.StreamConfig) {
				t.Helper()
				assert.Equal(t, jetstream.MemoryStorage, live.Storage,
					"a failed readiness check must not have changed the stream")
			},
		},
		{
			name:    "retention policy drift",
			stream:  "DRIFTRETENTION",
			mutate:  func(c *jetstream.StreamConfig) { c.Retention = jetstream.LimitsPolicy },
			declare: func(c *StreamConfig) { c.Retention = "interest" },
			wantMsg: []string{"retention", "observed=limits", "declared=interest"},
			liveWant: func(t *testing.T, live jetstream.StreamConfig) {
				t.Helper()
				assert.Equal(t, jetstream.LimitsPolicy, live.Retention,
					"the framework must not flip a live stream's delivery semantics for the operator")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm, js, ctx, _ := driftTestManager(t)

			existing := baselineStreamConfig(tt.stream)
			tt.mutate(&existing)
			_, err := js.CreateStream(ctx, existing)
			require.NoError(t, err)

			decl := baselineDeclaration(tt.stream)
			tt.declare(&decl.cfg)

			err = sm.createStream(ctx, decl)
			require.Error(t, err, "non-editable drift must not be silently accepted")
			require.ErrorIs(t, err, ErrStreamNonEditableDrift)
			for _, want := range tt.wantMsg {
				assert.Contains(t, err.Error(), want,
					"the diagnostic must report BOTH the observed and the declared configuration")
			}
			assert.Contains(t, err.Error(), tt.stream, "the diagnostic must name the stream")

			live := liveStreamConfig(t, js, ctx, tt.stream)
			tt.liveWant(t, live)
			assertUngovernedIntact(t, live)
		})
	}
}

// TestEnsureStreams_NonEditableDriftFailsBoot proves the failure reaches the
// readiness gate an operator actually sees, not just the internal seam.
func TestEnsureStreams_NonEditableDriftFailsBoot(t *testing.T) {
	sm, js, ctx, _ := driftTestManager(t)

	existing := baselineStreamConfig("BOOTDRIFT")
	existing.Storage = jetstream.MemoryStorage
	_, err := js.CreateStream(ctx, existing)
	require.NoError(t, err)

	cfg := &Config{
		Version:  "1.0.0",
		Platform: PlatformConfig{Org: "acme", ID: "test"},
		Streams: StreamConfigs{
			"BOOTDRIFT": {
				Subjects: []string{"bootdrift.>"}, Storage: "file",
				MaxAge: "24h", MaxBytes: 4 * 1024 * 1024, Discard: StreamDiscardOld,
			},
		},
	}

	require.ErrorIs(t, sm.EnsureStreams(ctx, cfg), ErrStreamNonEditableDrift,
		"boot must fail on drift the provisioner cannot repair")
}

// TestCreateStream_MigrationOverrideDoesNotRestampUndeclaredBounds keeps drift
// handling coherent with slice 5a's bridge semantics: an override admits a
// legacy stream WITHOUT declaring its bounds, and resolveBounds falls back to
// DiscardOld for a policy the operator never wrote. That fallback is a framework
// default, not a declaration — stamping it onto the legacy stream would be the
// bridge changing the very behavior it exists to preserve.
func TestCreateStream_MigrationOverrideDoesNotRestampUndeclaredBounds(t *testing.T) {
	sm, js, ctx, logs := driftTestManager(t)

	_, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     "BRIDGED",
		Subjects: []string{"bridged.>"},
		Storage:  jetstream.MemoryStorage,
		MaxAge:   168 * time.Hour,
		MaxBytes: 5 * 1024 * 1024,
		Discard:  jetstream.DiscardNew,
	})
	require.NoError(t, err)

	require.NoError(t, sm.createStream(ctx, streamDeclaration{
		name:      "BRIDGED",
		source:    `stream_migration_overrides["BRIDGED"]`,
		cfg:       StreamConfig{Subjects: []string{"bridged.>"}, Storage: "memory"},
		exemption: exemptMigrationOverride,
	}))

	live := liveStreamConfig(t, js, ctx, "BRIDGED")
	assert.Equal(t, 168*time.Hour, live.MaxAge, "a bridge must not restamp an undeclared max_age")
	assert.Equal(t, int64(5*1024*1024), live.MaxBytes, "a bridge must not restamp an undeclared max_bytes")
	assert.Equal(t, jetstream.DiscardNew, live.Discard,
		"a bridge must not restamp an undeclared discard policy with the framework fallback")
	assert.NotContains(t, logs.String(), "Reconciling stream configuration drift")
}

// TestCreateStream_MigrationOverrideReconcilesWhatItDeclares is the other side:
// a bridge that DOES declare a bound still owns it, so the declared field is
// repaired while the undeclared ones are left exactly as they were.
func TestCreateStream_MigrationOverrideReconcilesWhatItDeclares(t *testing.T) {
	sm, js, ctx, _ := driftTestManager(t)

	_, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     "BRIDGEDPARTIAL",
		Subjects: []string{"bridgedpartial.>"},
		Storage:  jetstream.MemoryStorage,
		MaxAge:   168 * time.Hour,
		MaxBytes: 5 * 1024 * 1024,
		Discard:  jetstream.DiscardNew,
	})
	require.NoError(t, err)

	require.NoError(t, sm.createStream(ctx, streamDeclaration{
		name:   "BRIDGEDPARTIAL",
		source: `stream_migration_overrides["BRIDGEDPARTIAL"]`,
		cfg: StreamConfig{
			Subjects: []string{"bridgedpartial.>"},
			Storage:  "memory",
			MaxBytes: 2 * 1024 * 1024,
		},
		exemption: exemptMigrationOverride,
	}))

	live := liveStreamConfig(t, js, ctx, "BRIDGEDPARTIAL")
	assert.Equal(t, int64(2*1024*1024), live.MaxBytes, "a declared bound is still governed under a bridge")
	assert.Equal(t, 168*time.Hour, live.MaxAge, "an undeclared bound stays exactly as it was")
	assert.Equal(t, jetstream.DiscardNew, live.Discard)
}

// TestCreateStream_ContestedStreamReportsEveryRepairWithBothValues is the
// two-declarer signal. A provisioner sees its own declaration and the live
// configuration; it never sees another process's declaration, so two components
// declaring the same stream with different limits is locally indistinguishable
// from ordinary drift — and reconciliation turns "first boot wins" into "last
// reconcile wins", flapping the stream forever. The framework cannot resolve
// that locally. What it CAN do is make it visible, and the visible signature is
// a repair that fires on EVERY boot with an ALTERNATING observed value. That
// signature only exists if every occurrence is reported with both values, so any
// "already logged this once" suppression would destroy the only available
// diagnosis.
func TestCreateStream_ContestedStreamReportsEveryRepairWithBothValues(t *testing.T) {
	sm, js, ctx, logs := driftTestManager(t)

	_, err := js.CreateStream(ctx, baselineStreamConfig("CONTESTED"))
	require.NoError(t, err)

	alpha := baselineDeclaration("CONTESTED")
	alpha.source = `component "alpha" port "out"`
	alpha.cfg.MaxAge = "12h"

	beta := baselineDeclaration("CONTESTED")
	beta.source = `component "beta" port "out"`
	beta.cfg.MaxAge = "36h"

	// Three boots alternating between the two declarers. The observed value moves
	// with whoever booted last — that is what contested looks like from here.
	boots := []struct {
		decl      streamDeclaration
		wantDrift string
	}{
		{alpha, "max_age: observed=24h0m0s declared=12h0m0s"},
		{beta, "max_age: observed=12h0m0s declared=36h0m0s"},
		{alpha, "max_age: observed=36h0m0s declared=12h0m0s"},
	}

	for i, boot := range boots {
		logs.Reset()
		require.NoError(t, sm.createStream(ctx, boot.decl))

		assert.Contains(t, logs.String(), "Reconciling stream configuration drift", "boot %d", i)
		assert.Contains(t, logs.String(), boot.wantDrift,
			"boot %d must report BOTH values; suppressing repeats would erase the contested signal", i)
		// Matched as slog renders it. A production source is built with %q
		// (`component "alpha" port "out"`, stream_bounds.go), so the handler quotes
		// the attribute and escapes the inner quotes — the raw string never appears
		// verbatim. Asserting the rendered attribute keeps this a test of "the log
		// names the declarer" rather than of the fixture's spelling.
		assert.Contains(t, logs.String(), "source="+strconv.Quote(boot.decl.source),
			"boot %d must name the declarer, so two sources are distinguishable in one log", i)
	}
}

// TestCreateStream_BackingStreamIsRefusedBeforeReconciliation pins the property
// that makes widening the reconciler safe AT ALL, against a REAL backing stream
// that really diverges from the declaration handed in. The existing guard tests
// prove refusal precedes the JetStream context fetch using a client that has
// none; this one proves the refusal dominates RECONCILIATION specifically — the
// KV bucket's backing stream exists, its live configuration differs from the
// declaration in every governed field, and none of it is touched.
func TestCreateStream_BackingStreamIsRefusedBeforeReconciliation(t *testing.T) {
	sm, js, ctx, logs := driftTestManager(t)

	const bucket = "DRIFTGUARD"
	_, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: bucket, History: 5})
	require.NoError(t, err)

	backingStream := natsclient.KVStreamPrefix + bucket
	before := liveStreamConfig(t, js, ctx, backingStream)

	// A declaration that would stamp reachability-blind age eviction onto the
	// bucket — precisely the one-typo hazard, and precisely what the widened
	// reconciler would now be capable of writing to an existing stream.
	err = sm.createStream(ctx, streamDeclaration{
		name:   backingStream,
		source: `config.streams["` + backingStream + `"]`,
		cfg: StreamConfig{
			Subjects: []string{"$KV." + bucket + ".>"},
			Storage:  "file",
			MaxAge:   "1h",
			MaxBytes: 1024,
			Discard:  StreamDiscardOld,
		},
	})

	require.ErrorIs(t, err, ErrBackingStreamNotProvisionable,
		"the prefix refusal must fire, not the drift reconciler")
	assert.NotErrorIs(t, err, ErrStreamNonEditableDrift,
		"the guard must dominate: drift is never even evaluated for a backing stream")

	after := liveStreamConfig(t, js, ctx, backingStream)
	assert.Equal(t, before.MaxAge, after.MaxAge, "no age eviction may be stamped onto a KV backing stream")
	assert.Equal(t, before.MaxBytes, after.MaxBytes)
	assert.Equal(t, before.Discard, after.Discard)
	assert.Equal(t, before.Subjects, after.Subjects)
	assert.NotContains(t, logs.String(), "Reconciling stream configuration drift",
		"reconciliation must not have run at all")
}

// TestCreateStream_ArchivalDriftRepairsToUnlimited keeps drift handling coherent
// with slice 5a's archival semantics: permanence IS the declaration, so an
// existing stream that still evicts is drift, and repairing it can only ever
// REMOVE a limit — the safe direction.
func TestCreateStream_ArchivalDriftRepairsToUnlimited(t *testing.T) {
	sm, js, ctx, logs := driftTestManager(t)

	_, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     "ARCHIVEDRIFT",
		Subjects: []string{"archivedrift.>"},
		Storage:  jetstream.MemoryStorage,
		MaxAge:   24 * time.Hour,
		MaxBytes: 1024 * 1024,
		Discard:  jetstream.DiscardOld,
	})
	require.NoError(t, err)

	require.NoError(t, sm.createStream(ctx, streamDeclaration{
		name:      "ARCHIVEDRIFT",
		source:    `archival_streams["ARCHIVEDRIFT"]`,
		cfg:       StreamConfig{Subjects: []string{"archivedrift.>"}, Storage: "memory"},
		exemption: exemptArchival,
	}))

	live := liveStreamConfig(t, js, ctx, "ARCHIVEDRIFT")
	assert.Zero(t, live.MaxAge, "nothing may ever age out of an archive")
	assert.Equal(t, int64(-1), live.MaxBytes, "nothing may ever be evicted from an archive for size")
	assert.Equal(t, jetstream.DiscardNew, live.Discard)
	assert.Contains(t, logs.String(), "Reconciling stream configuration drift")

	// And a second boot is a no-op: NATS reports the absent byte limit as -1
	// while the declaration carries 0, so a comparison that did not normalize
	// them would re-stamp this stream on every boot forever.
	logs.Reset()
	require.NoError(t, sm.createStream(ctx, streamDeclaration{
		name:      "ARCHIVEDRIFT",
		source:    `archival_streams["ARCHIVEDRIFT"]`,
		cfg:       StreamConfig{Subjects: []string{"archivedrift.>"}, Storage: "memory"},
		exemption: exemptArchival,
	}))
	assert.NotContains(t, logs.String(), "Reconciling stream configuration drift",
		"unlimited declared as 0 and reported as -1 must not read as perpetual drift")
}
