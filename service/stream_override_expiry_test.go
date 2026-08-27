package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
)

func expiryReporter(t *testing.T, cfg *config.Config) (*streamOverrideExpiryReporter, *strings.Builder, *metric.MetricsRegistry) {
	t.Helper()
	logs := &strings.Builder{}
	r := newStreamOverrideExpiryReporter(
		func() *config.Config { return cfg },
		slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	)
	registry := metric.NewMetricsRegistry()
	require.NoError(t, r.register(registry))
	return r, logs, registry
}

func overrideConfig(expires string) *config.Config {
	return &config.Config{
		StreamMigrationOverrides: config.StreamMigrationOverrides{
			"LEGACY": {Owner: "team-legacy", Expires: expires, Reason: "sizing study"},
		},
	}
}

// TestOverrideExpiry_CrossesTheDeadlineWithoutRestart is the property that made
// this reporter exist: the same process, the same configuration, evaluated either
// side of the deadline. Boot-time evaluation cannot see this transition at all.
func TestOverrideExpiry_CrossesTheDeadlineWithoutRestart(t *testing.T) {
	r, logs, registry := expiryReporter(t, overrideConfig("2026-09-30"))
	labels := map[string]string{"stream": "LEGACY", "owner": "team-legacy"}

	r.evaluate(time.Date(2026, 9, 30, 23, 59, 59, 0, time.UTC))
	assert.Equal(t, 0.0, requireGauge(t, registry, "semstreams_streams_migration_override_expired", labels),
		"an open bridge reports zero — the series must EXIST before it matters, or the alert cannot be tested")
	assert.NotContains(t, logs.String(), "EXPIRED")

	logs.Reset()
	r.evaluate(time.Date(2026, 10, 1, 4, 0, 0, 0, time.UTC))

	assert.Equal(t, 1.0, requireGauge(t, registry, "semstreams_streams_migration_override_expired", labels),
		"one process, one config, no restart: the same evaluation now reports the lapse")
	logged := logs.String()
	assert.Contains(t, logged, "EXPIRED")
	assert.Contains(t, logged, "LEGACY")
	assert.Contains(t, logged, "team-legacy", "the remedy needs an addressee")
	assert.Contains(t, logged, "next boot will refuse to start",
		"the operator must be told where enforcement actually lands")
	assert.Contains(t, logged, "archival_streams", "and be given the escape if permanence is the contract")
}

// TestOverrideExpiry_ReportsOnEveryTick keeps the signal alive. A lapse that
// scrolled past once at 03:00 is not a signal, and the gauge is what an alert reads
// — but the log is what someone greps at 09:00.
func TestOverrideExpiry_ReportsOnEveryTick(t *testing.T) {
	r, logs, _ := expiryReporter(t, overrideConfig("2026-09-30"))

	for i := range 3 {
		logs.Reset()
		r.evaluate(time.Date(2026, 10, 1, 4+i, 0, 0, 0, time.UTC))
		assert.Contains(t, logs.String(), "EXPIRED", "tick %d must report", i)
	}
}

// TestOverrideExpiry_ClearsWhenTheBridgeIsRenewed is the other half of a latching
// gauge. An operator may extend or remove an override without restarting, and a
// reporter that kept paging for a problem already fixed is worse than one that said
// nothing.
func TestOverrideExpiry_ClearsWhenTheBridgeIsRenewed(t *testing.T) {
	cfg := overrideConfig("2026-09-30")
	r, _, registry := expiryReporter(t, cfg)
	labels := map[string]string{"stream": "LEGACY", "owner": "team-legacy"}
	now := time.Date(2026, 10, 1, 4, 0, 0, 0, time.UTC)

	r.evaluate(now)
	require.Equal(t, 1.0, requireGauge(t, registry, "semstreams_streams_migration_override_expired", labels))

	// The operator extends it, live.
	cfg.StreamMigrationOverrides["LEGACY"] = config.StreamMigrationOverride{
		Owner: "team-legacy", Expires: "2027-03-01", Reason: "sizing study",
	}
	r.evaluate(now)

	assert.Equal(t, 0.0, requireGauge(t, registry, "semstreams_streams_migration_override_expired", labels),
		"a renewed bridge must stop reporting without a restart")
}

// TestOverrideExpiry_RemovedOverrideStopsReporting covers the series going away
// entirely rather than reporting a stream nobody declares any more.
func TestOverrideExpiry_RemovedOverrideStopsReporting(t *testing.T) {
	cfg := overrideConfig("2026-09-30")
	r, _, registry := expiryReporter(t, cfg)
	labels := map[string]string{"stream": "LEGACY", "owner": "team-legacy"}
	now := time.Date(2026, 10, 1, 4, 0, 0, 0, time.UTC)

	r.evaluate(now)
	require.Equal(t, 1.0, requireGauge(t, registry, "semstreams_streams_migration_override_expired", labels))

	cfg.StreamMigrationOverrides = nil
	r.evaluate(now)

	_, ok := gaugeValue(t, registry, "semstreams_streams_migration_override_expired", labels)
	assert.False(t, ok, "an override the operator deleted must not keep a series standing")
}

// --- rehome guard (ADR-100 D5, #1093) ---------------------------------------

// composeComponentManagerWithOverride drives the PRODUCTION composition path —
// embedded NATS, a real config.Manager over a boot config carrying a migration
// override, and NewComponentManager — because the property under test is the
// wiring, not the reporter. A helper-only assertion would keep passing after
// the wiring was dropped, which is the exact failure the rehome must not have.
func composeComponentManagerWithOverride(
	t *testing.T, overrides config.StreamMigrationOverrides, logger *slog.Logger,
) (Service, *metric.MetricsRegistry) {
	t.Helper()

	server, err := natsserver.NewServer(&natsserver.Options{
		Port: -1, NoLog: true, NoSigs: true, JetStream: true, StoreDir: t.TempDir(),
	})
	require.NoError(t, err)
	server.Start()
	t.Cleanup(func() {
		server.Shutdown()
		server.WaitForShutdown()
	})
	if !server.ReadyForConnections(10 * time.Second) {
		t.Fatal("embedded NATS server not ready")
	}
	client, err := natsclient.NewClient(server.ClientURL())
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	t.Cleanup(func() { _ = client.Close(context.WithoutCancel(ctx)) })

	bootConfig := &config.Config{StreamMigrationOverrides: overrides}
	configManager, err := config.NewConfigManager(bootConfig, client, logger)
	require.NoError(t, err)

	registry := metric.NewMetricsRegistry()
	manager, err := NewComponentManager(json.RawMessage(`{}`), &Dependencies{
		NATSClient:      client,
		MetricsRegistry: registry,
		Logger:          logger,
		Manager:         configManager,
	})
	require.NoError(t, err)
	return manager, registry
}

// TestStreamOverrideExpiryReporterRegistersWithoutFlowService is the guard for
// the one production concern that must not die with the flow-builder service.
// The reporter was hosted ONLY by FlowService (`flow_service.go:560-585` before
// removal); ADR-100 D5 deletes that service, so the metric moves to the
// component-manager — the one service the framework treats as mandatory
// (`service_manager.go` mandatoryServices; a configuration that disables it is
// refused with MandatoryServiceDisabledError). Hosting it on an optional
// service would hand the operator a fact to predict: "also enable X or your
// bridge lapses silently."
//
// It asserts the metric is registered against the registry the /metrics
// endpoint scrapes, not merely that a RegisterMetrics method exists: nothing in
// the framework calls Service.RegisterMetrics (see storage_observability.go),
// so a metric reachable only through it is a phantom.
func TestStreamOverrideExpiryReporterRegistersWithoutFlowService(t *testing.T) {
	logs := &strings.Builder{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	_, registry := composeComponentManagerWithOverride(t, config.StreamMigrationOverrides{
		"LAPSED": {Owner: "team-legacy", Expires: "2020-01-01", Reason: "sizing study"},
		"OPEN":   {Owner: "team-current", Expires: "2999-01-01", Reason: "sizing study"},
	}, logger)

	const metricName = "semstreams_streams_migration_override_expired"
	assert.Equal(t, 1.0,
		requireGauge(t, registry, metricName, map[string]string{"stream": "LAPSED", "owner": "team-legacy"}),
		"a lapsed bridge must still report after the flow-builder service is gone")
	assert.Equal(t, 0.0,
		requireGauge(t, registry, metricName, map[string]string{"stream": "OPEN", "owner": "team-current"}),
		"an open bridge reports zero so the alert series exists before it matters")
	assert.Contains(t, logs.String(), "EXPIRED",
		"the WARN half of the report must survive the rehome too")
}

// syncBuffer is a log sink safe for the reporter goroutine to write while the
// test reads. Without it this test would report a data race rather than the
// behaviour it is about.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

// TestComponentManagerStartRunsOverrideExpiryReporter covers the second half of
// the rehome: registration alone leaves a series frozen at its boot value, and
// the whole point of the reporter is that it crosses the deadline WITHOUT a
// restart. Start must put the loop on the manager's runtime context, and Stop
// must join it (supervise waits for it before closing supervisorDone).
func TestComponentManagerStartRunsOverrideExpiryReporter(t *testing.T) {
	sink := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(sink, &slog.HandlerOptions{Level: slog.LevelDebug}))

	manager, _ := composeComponentManagerWithOverride(t, config.StreamMigrationOverrides{
		"LAPSED": {Owner: "team-legacy", Expires: "2020-01-01", Reason: "sizing study"},
	}, logger)

	// Discard the composition-time evaluation so the next WARN can only have
	// come from the loop Start launched.
	sink.Reset()

	runtimeCtx, cancelRuntime := context.WithCancel(t.Context())
	defer cancelRuntime()
	require.NoError(t, manager.Start(runtimeCtx))

	require.Eventually(t, func() bool {
		return strings.Contains(sink.String(), "EXPIRED")
	}, 5*time.Second, 10*time.Millisecond,
		"Start must run the override-expiry loop; nothing re-evaluated after composition")

	stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(runtimeCtx), 30*time.Second)
	defer cancelStop()
	require.NoError(t, manager.Stop(stopCtx))
}
