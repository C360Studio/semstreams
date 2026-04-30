package rule

import (
	"testing"

	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func newCronMetricsForTest(t *testing.T) *cronMetrics {
	t.Helper()
	// Per-test registry guarantees isolation: getCronMetrics caches by
	// pointer, so a fresh registry yields fresh collectors with no
	// accumulated counts from sibling tests.
	return getCronMetrics(metric.NewMetricsRegistry())
}

func TestGetCronMetrics_NilRegistryReturnsSingleton(t *testing.T) {
	t.Parallel()
	a := getCronMetrics(nil)
	b := getCronMetrics(nil)
	if a != b {
		t.Errorf("getCronMetrics(nil) returned distinct singletons; want shared instance")
	}
}

func TestGetCronMetrics_PerRegistryIsolation(t *testing.T) {
	t.Parallel()
	r1 := metric.NewMetricsRegistry()
	r2 := metric.NewMetricsRegistry()
	m1 := getCronMetrics(r1)
	m2 := getCronMetrics(r2)
	if m1 == m2 {
		t.Error("distinct registries returned shared metrics; want per-registry isolation")
	}
}

func TestCronMetrics_RecordFireSuccessAndError(t *testing.T) {
	t.Parallel()
	m := newCronMetricsForTest(t)

	m.recordFire("rule-A", cronFireStatusSuccess, 0.05)
	m.recordFire("rule-A", cronFireStatusError, 0.10)
	m.recordFire("rule-B", cronFireStatusSuccess, 0.02)

	if got := testutil.ToFloat64(m.firesTotal.WithLabelValues("rule-A", cronFireStatusSuccess)); got != 1 {
		t.Errorf("rule-A success = %f, want 1", got)
	}
	if got := testutil.ToFloat64(m.firesTotal.WithLabelValues("rule-A", cronFireStatusError)); got != 1 {
		t.Errorf("rule-A error = %f, want 1", got)
	}
	if got := testutil.ToFloat64(m.firesTotal.WithLabelValues("rule-B", cronFireStatusSuccess)); got != 1 {
		t.Errorf("rule-B success = %f, want 1", got)
	}

	// Histogram observed for both success and error (= actual dispatches).
	if got := testutil.CollectAndCount(m.fireDurationSeconds); got == 0 {
		t.Errorf("fire duration histogram has no observations after recordFire(success/error)")
	}
}

// Skipped statuses (cooldown_skipped, inflight_skipped) and the panic
// backstop status all skip the histogram observation. Skipped fires
// never dispatched; panic duration represents partial work + recovery
// overhead and would distort success/error percentiles. Panics are
// paged on via the counter, not the histogram. This test locks the
// chokepoint contract documented on recordFire.
func TestCronMetrics_RecordFireSkippedStatusesOmitDuration(t *testing.T) {
	t.Parallel()
	m := newCronMetricsForTest(t)

	// Seed one valid observation so we can detect any extra additions
	// from the skipped/panic calls below.
	m.recordFire("rule", cronFireStatusSuccess, 0.05)
	baseline := testutil.CollectAndCount(m.fireDurationSeconds)

	m.recordFire("rule", cronFireStatusCooldownSkipped, 0.001)
	m.recordFire("rule", cronFireStatusInflightSkipped, 0.001)
	m.recordFire("rule", cronFireStatusPanic, 0.001)

	// Counters must increment for each call.
	for _, status := range []string{cronFireStatusCooldownSkipped, cronFireStatusInflightSkipped, cronFireStatusPanic} {
		if got := testutil.ToFloat64(m.firesTotal.WithLabelValues("rule", status)); got != 1 {
			t.Errorf("fires{status=%s} = %f, want 1", status, got)
		}
	}
	// Histogram MUST NOT have grown past the baseline — none of the
	// three statuses observe duration.
	if got := testutil.CollectAndCount(m.fireDurationSeconds); got != baseline {
		t.Errorf("fire_duration histogram count = %d, want %d (skipped/panic statuses must not observe duration)", got, baseline)
	}
}

func TestCronMetrics_RegisteredGaugeFlips(t *testing.T) {
	t.Parallel()
	m := newCronMetricsForTest(t)

	m.recordRuleRegistered()
	m.recordRuleRegistered()
	if got := testutil.ToFloat64(m.registered); got != 2 {
		t.Errorf("registered = %f, want 2", got)
	}
	m.recordRuleDeregistered()
	if got := testutil.ToFloat64(m.registered); got != 1 {
		t.Errorf("registered after deregister = %f, want 1", got)
	}
}

func TestCronMetrics_MissedFiresAdditive(t *testing.T) {
	t.Parallel()
	m := newCronMetricsForTest(t)

	m.recordMissedFire("rule", 5)
	m.recordMissedFire("rule", 3)
	if got := testutil.ToFloat64(m.missedFiresTotal.WithLabelValues("rule")); got != 8 {
		t.Errorf("missed fires = %f, want 8", got)
	}

	// Zero / negative counts are no-ops.
	m.recordMissedFire("rule", 0)
	m.recordMissedFire("rule", -1)
	if got := testutil.ToFloat64(m.missedFiresTotal.WithLabelValues("rule")); got != 8 {
		t.Errorf("missed fires after zero/negative = %f, want 8 (unchanged)", got)
	}
}

func TestCronMetrics_MissedFireCappedCounter(t *testing.T) {
	t.Parallel()
	m := newCronMetricsForTest(t)
	m.recordMissedFireCapped("rule")
	if got := testutil.ToFloat64(m.missedFiresCappedTotal.WithLabelValues("rule")); got != 1 {
		t.Errorf("capped = %f, want 1", got)
	}
}

func TestCronMetrics_NextFireGaugeSetAndClear(t *testing.T) {
	t.Parallel()
	m := newCronMetricsForTest(t)

	m.recordNextFire("rule", 1735689600) // 2025-01-01T00:00:00Z
	if got := testutil.ToFloat64(m.nextFireTimestampSecs.WithLabelValues("rule")); got != 1735689600 {
		t.Errorf("next_fire = %f, want 1735689600", got)
	}

	m.clearNextFire("rule")
	// After delete, ToFloat64 on the WithLabelValues handle re-creates
	// the gauge at 0; instead, count vector entries via CollectAndCount.
	if got := testutil.CollectAndCount(m.nextFireTimestampSecs); got != 0 {
		t.Errorf("next_fire vector count after clear = %d, want 0", got)
	}
}

func TestCronMetrics_SchedulerRunningGauge(t *testing.T) {
	t.Parallel()
	m := newCronMetricsForTest(t)

	m.recordSchedulerRunning(true)
	if got := testutil.ToFloat64(m.schedulerRunning); got != 1 {
		t.Errorf("running=true gauge = %f, want 1", got)
	}
	m.recordSchedulerRunning(false)
	if got := testutil.ToFloat64(m.schedulerRunning); got != 0 {
		t.Errorf("running=false gauge = %f, want 0", got)
	}
}

// All recordX methods must be safe on a nil receiver. Production paths
// initialize cronMetrics from a registry; degraded mode (nil registry +
// production singleton race in tests) historically left some fields nil.
// This test guards against any future field that forgets the nil guard.
func TestCronMetrics_NilReceiverSafe(t *testing.T) {
	t.Parallel()
	var m *cronMetrics
	m.recordFire("r", cronFireStatusSuccess, 0.1)
	m.recordFire("r", cronFireStatusCooldownSkipped, 0)
	m.recordRuleRegistered()
	m.recordRuleDeregistered()
	m.recordMissedFire("r", 5)
	m.recordMissedFireCapped("r")
	m.recordNextFire("r", 0)
	m.clearNextFire("r")
	m.recordSchedulerRunning(true)
	// Reaching here without panic is the assertion.
}
