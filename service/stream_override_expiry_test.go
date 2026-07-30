package service

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/metric"
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
