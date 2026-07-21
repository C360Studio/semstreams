package graphclustering

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/readiness"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// basePorts is the minimum valid port shape; every config here needs it to validate.
func basePorts() *component.PortConfig {
	return &component.PortConfig{
		Inputs:  []component.PortDefinition{{Name: "entity_watch", Type: "kv-watch", Subject: graph.BucketEntityStates}},
		Outputs: []component.PortDefinition{{Name: "communities", Type: "kv-write", Subject: graph.BucketCommunityIndex}},
	}
}

// TestCreateGraphClustering_RejectsRemovedGateKnobs is the BREAKING-change contract for
// BOTH withdrawn readiness knobs. encoding/json silently DROPS a key with no matching
// struct field, so an operator upgrading with either one in hand would see no error and
// a quietly different gate — the precise silent behavior change this line of work exists
// to prevent, twice over:
//
//   - index_lag_tolerance (revisions) was withdrawn by ADR-083 for max_staleness;
//   - max_staleness (wall time) is withdrawn now, with NO replacement field, because
//     readiness gates on index health alone and a healthy index serves however far
//     behind it is.
//
// The second is the more dangerous silent drop of the two: leaving it in place would
// read to an operator as "my tolerance is being honored" while in fact every run
// proceeds regardless. The load must fail, and the message must say where the answer
// went.
func TestCreateGraphClustering_RejectsRemovedGateKnobs(t *testing.T) {
	// The factory only stores the client; a zero value gets past its nil-dependency
	// guard and reaches config decoding, which is what this test is about.
	deps := component.Dependencies{NATSClient: &natsclient.Client{}}

	withKnob := func(key, value string) []byte {
		return []byte(`{
			"ports": {
				"inputs":  [{"name":"entity_watch","type":"kv-watch","subject":"ENTITY_STATES"}],
				"outputs": [{"name":"communities","type":"kv-write","subject":"COMMUNITY_INDEX"}]
			},
			"` + key + `": ` + value + `
		}`)
	}

	tests := []struct {
		name  string
		key   string
		value string
		// wantSubstrings are the load-bearing halves of the guidance: the field the
		// operator wrote, and where the answer moved.
		wantSubstrings []string
	}{
		{
			name: "index_lag_tolerance", key: "index_lag_tolerance", value: "250",
			wantSubstrings: []string{"index_lag_tolerance", "staleness_at_detection_ms", "HEALTH"},
		},
		{
			name: "max_staleness", key: "max_staleness", value: `"15s"`,
			wantSubstrings: []string{"max_staleness", "staleness_at_detection_ms", "HEALTH"},
		},
		{
			// The zero value an operator might reach for to "turn it off" is still a
			// present key, so it must fail too — silently accepting it would teach that
			// the field is still read.
			name: "max_staleness explicitly zeroed", key: "max_staleness", value: `"0s"`,
			wantSubstrings: []string{"max_staleness"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CreateGraphClustering(withKnob(tt.key, tt.value), deps)
			require.Error(t, err, "a removed field must fail the load, not be silently dropped")
			for _, want := range tt.wantSubstrings {
				assert.Contains(t, err.Error(), want,
					"the startup failure must name the field and where the answer moved")
			}
		})
	}

	// The same config with NEITHER knob loads cleanly and reaches the gate, so the probe
	// rejects the fields rather than the shape.
	clean := []byte(`{
		"ports": {
			"inputs":  [{"name":"entity_watch","type":"kv-watch","subject":"ENTITY_STATES"}],
			"outputs": [{"name":"communities","type":"kv-write","subject":"COMMUNITY_INDEX"}]
		}
	}`)
	_, err := CreateGraphClustering(clean, deps)
	require.NoError(t, err, "a config carrying no withdrawn knob must load")
}

// TestConfig_NoStalenessKnobOnTheOperatorSurface is the serialization half of the
// deletion. A removed field that still MARSHALS would keep appearing in generated
// schemas, example configs, and operator round-trips — teaching the knob back into
// existence from the very surface that is supposed to have lost it.
func TestConfig_NoStalenessKnobOnTheOperatorSurface(t *testing.T) {
	cfg := Config{Ports: basePorts()}
	cfg.ApplyDefaults()

	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "max_staleness",
		"the withdrawn knob must not reappear on the marshalled operator surface")
	assert.NotContains(t, string(data), "index_lag_tolerance")

	// And a config that validates without it is the ONLY shape: nothing about the gate
	// is configurable any more, so a minimal valid config must simply pass.
	require.NoError(t, cfg.Validate())
}

// newLoggedComponent builds a component whose log output the caller can inspect.
// slog.SetDefault is never touched (and no test here is parallel), so the capture
// stays local to this component instance.
func newLoggedComponent(t *testing.T, cfg Config) (*Component, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cfg.ApplyDefaults()
	return &Component{logger: logger, metrics: getMetrics(nil), config: cfg}, &buf
}

// TestStaleViewProceedsAndIsStamped is the load-bearing pin of the "report, don't gate"
// contract, and the direct inverse of the retired over_staleness rows: a healthy index
// at an ARBITRARY view age clears the gate, and the run it clears stamps that age on
// staleness_at_detection_ms instead of withholding a partition.
//
// Both halves matter. The proceed alone would be a silent loosening; the gauge is what
// keeps an always-admitted stale view from becoming an invisible one, which is the whole
// reason the metric outlived the tolerance it was built for (#579).
func TestStaleViewProceedsAndIsStamped(t *testing.T) {
	// Ages spanning "just behind" to "absurd" — including values no operator could have
	// legally configured a tolerance for (the withdrawn ceiling was one hour).
	ages := []uint64{1, 1_500, 60_000, 3_600_000, 86_400_000}

	for _, ms := range ages {
		status := graph.IndexStatusResponse{
			State: graph.IndexStateBuilding, BootstrapComplete: true,
			IndexedRevision: 400, TargetRevision: 500, Lag: 100, StalenessMs: ms,
		}

		proceed, reason := graph.EvaluateReadinessGate(
			graph.StatusReading{Status: status, Fresh: true})
		require.True(t, proceed, "a healthy index %dms behind deferred as %q", ms, reason)

		c, buf := newLoggedComponent(t, Config{Ports: basePorts()})
		c.observeDetectionRun(gateDecision{
			proceed: true,
			reading: readiness.Reading{Known: true, Fresh: true, Status: status},
		})

		assert.Equal(t, float64(ms), testutil.ToFloat64(c.metrics.stalenessAtDetection),
			"the run must stamp the view age it proceeded at")
		assert.Contains(t, buf.String(), `"level":"INFO"`,
			"a run on a lagging view must be visible without raising the log level")
	}
}

// TestObserveDetectionRun proves clustering-on-a-lagging-view is operator-visible, not
// Debug-only (#579 lesson), and that a caught-up run resets the gauge rather than
// leaving the last non-zero value standing.
func TestObserveDetectionRun(t *testing.T) {
	c, buf := newLoggedComponent(t, Config{Ports: basePorts()})

	c.observeDetectionRun(gateDecision{
		proceed: true,
		reading: readiness.Reading{
			Known: true, Fresh: true,
			Status: graph.IndexStatusResponse{State: graph.IndexStateBuilding, TargetRevision: 500, Lag: 40, StalenessMs: 1500},
		},
	})
	assert.Equal(t, float64(1500), testutil.ToFloat64(c.metrics.stalenessAtDetection),
		"gauge must record the view age the run proceeded at")
	logged := buf.String()
	assert.Contains(t, logged, `"level":"INFO"`, "clustering on a lagging view must not be confined to debug")
	assert.Contains(t, logged, `"staleness_ms":1500`, "the log must carry the view age")
	assert.Contains(t, logged, `"index_lag":40`, "the log must carry the revision lag alongside it")
	assert.NotContains(t, logged, "max_staleness",
		"no tolerance exists to log; carrying one would imply the age was checked against something")

	// An exactly-caught-up run resets the gauge to 0 and emits no INFO log.
	buf.Reset()
	c.observeDetectionRun(gateDecision{
		proceed: true,
		reading: readiness.Reading{Known: true, Fresh: true, Status: graph.IndexStatusResponse{Ready: true, State: graph.IndexStateReady}},
	})
	assert.Equal(t, float64(0), testutil.ToFloat64(c.metrics.stalenessAtDetection),
		"gauge must reflect the latest run (0 = caught up)")
	assert.False(t, strings.Contains(buf.String(), `"level":"INFO"`),
		"a caught-up run must not emit the lagging-view INFO log")
}

// TestObserveDetectionRun_UngatedRunRecordsNoStaleness: an allow_ungated_reads run
// verified nothing about the view. Recording 0 would publish "verified caught up",
// which is precisely the false confidence this work exists to remove.
func TestObserveDetectionRun_UngatedRunRecordsNoStaleness(t *testing.T) {
	c, _ := newLoggedComponent(t, Config{Ports: basePorts(), AllowUngatedReads: true})
	c.metrics.setStalenessAtDetection(4242) // a prior verified run

	c.observeDetectionRun(gateDecision{proceed: true, ungated: true})

	assert.Equal(t, float64(4242), testutil.ToFloat64(c.metrics.stalenessAtDetection),
		"an ungated run must leave the gauge alone rather than claim a verified 0")
}

// TestRecordDefer_IsEvidence is the gh#590 regression guard, now covering the FOUR
// surviving defer reasons and only those. The old defer line was a bare constant, so a
// transport failure and a genuinely-broken index logged identically and the
// investigation cost three comment cycles. Every defer must carry the full reading and
// increment its typed counter.
//
// Every row here is an index-HEALTH fault. That is the point: an operator reading any of
// these four knows something is wrong with the index or its status feed, and none is
// answered by tuning a tolerance — over_staleness and staleness_unknown, the two that
// were, no longer exist.
func TestRecordDefer_IsEvidence(t *testing.T) {
	feedErr := errors.New("nats: bucket not found")

	tests := []struct {
		name    string
		reason  graph.DeferReason
		reading readiness.Reading
		want    []string
	}{
		{
			name:   "a dead status feed names the transport, not index state",
			reason: graph.DeferStatusUnknown,
			reading: readiness.Reading{
				Known: true, Age: 17 * time.Second,
				// The last-known envelope said READY: without status_known/status_age
				// and the reason, this line would read as a healthy index.
				Status: graph.IndexStatusResponse{Ready: true, State: graph.IndexStateReady},
				Err:    feedErr,
			},
			want: []string{
				`"reason":"status_unknown"`,
				`"status_known":true`,
				`"status_age":17000000000`,
				`"status_error":"nats: bucket not found"`,
			},
		},
		{
			name:   "a broken index names the state and lag",
			reason: graph.DeferHardStop,
			reading: readiness.Reading{
				Known: true, Fresh: true,
				Status: graph.IndexStatusResponse{
					State: graph.IndexStateDegraded, TargetRevision: 500, Lag: 90, StalenessMs: 9000,
				},
			},
			want: []string{`"reason":"hard_stop"`, `"state":"degraded"`, `"lag":90`, `"staleness_ms":9000`},
		},
		{
			// The gh#474 cutover window, wire-observable (ADR-084 D2) and now the ONLY
			// "not yet" the gate enforces. It replaced the former `empty` reason, whose
			// TargetRevision==0 proxy was wrong in both directions — false mid-cutover,
			// and true for the authoritatively-empty graph it then wrongly deferred.
			name:    "an unbootstrapped index is not confused with caught up",
			reason:  graph.DeferBootstrapIncomplete,
			reading: readiness.Reading{Known: true, Fresh: true, Status: graph.IndexStatusResponse{State: graph.IndexStateBuilding}},
			want:    []string{`"reason":"bootstrap_incomplete"`, `"lag":0`},
		},
		{
			// Version skew: the producer is talking, but saying something this consumer
			// cannot interpret. The operator action is a version check.
			name:   "an uninterpretable envelope names the state it could not read",
			reason: graph.DeferUnrecognizedState,
			reading: readiness.Reading{
				Known: true, Fresh: true,
				Status: graph.IndexStatusResponse{State: "rebuilding", TargetRevision: 500},
			},
			want: []string{`"reason":"unrecognized_state"`, `"state":"rebuilding"`},
		},
		{
			name:    "a producer never seen reports status_known false",
			reason:  graph.DeferStatusUnknown,
			reading: readiness.Reading{},
			want:    []string{`"reason":"status_unknown"`, `"status_known":false`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, buf := newLoggedComponent(t, Config{Ports: basePorts()})
			before := deferCount(t, c, tt.reason)

			c.recordDefer(gateDecision{reason: tt.reason, reading: tt.reading})

			logged := buf.String()
			for _, want := range tt.want {
				assert.Contains(t, logged, want, "the defer line must be diagnosable from ONE grep")
			}
			assert.NotContains(t, logged, "max_staleness",
				"a defer line must not carry a tolerance that no longer exists")
			assert.Equal(t, before+1, deferCount(t, c, tt.reason), "every defer must be counted under its reason")
		})
	}
}

// TestDeferMetricCoversEverySurvivingReason closes the silent-drop gap: countDefer
// enumerates a CLOSED label set and drops anything outside it rather than leak
// cardinality, so a reason the gate can emit but the metric does not know is a defer
// that increments nothing. That is the invisible-defer shape gh#590 cost three cycles
// on, reintroduced by omission.
func TestDeferMetricCoversEverySurvivingReason(t *testing.T) {
	c, _ := newLoggedComponent(t, Config{Ports: basePorts()})

	// Driven from the canonical list rather than a local copy: a reason added to
	// graph.AllDeferReasons must be countable HERE without anyone remembering to extend
	// this table. countDefer drops anything outside deferReasons, so the failure this
	// guards is silent by construction.
	require.NotEmpty(t, graph.AllDeferReasons, "canonical defer-reason set is empty")
	for _, reason := range graph.AllDeferReasons {
		before := deferCount(t, c, reason)
		c.metrics.countDefer(reason)
		assert.Equal(t, before+1, deferCount(t, c, reason),
			"reason %q is emitted by the gate but not counted — it must be in deferReasons", reason)
	}

	// And the label set carries no retired reasons, which would scrape forever at zero
	// and read as "this never happens" rather than "this cannot happen".
	for _, retired := range []string{"over_staleness", "staleness_unknown"} {
		for _, live := range deferReasons {
			assert.NotEqual(t, retired, string(live),
				"retired reason %q must not remain in the metric label set", retired)
		}
	}
}

// TestRecordDefer_TransitionsAreVisible: a defer must be visible without raising the
// log level (the first of a reason, and any change of reason), while a steady defer
// must not flood one line per tick. defer_total stays the rate signal either way.
func TestRecordDefer_TransitionsAreVisible(t *testing.T) {
	c, buf := newLoggedComponent(t, Config{Ports: basePorts()})
	unknown := gateDecision{reason: graph.DeferStatusUnknown}
	hardStop := gateDecision{reason: graph.DeferHardStop}

	c.recordDefer(unknown)
	assert.Contains(t, buf.String(), `"level":"WARN"`, "the first defer must be visible at default log level")

	buf.Reset()
	c.recordDefer(unknown)
	assert.NotContains(t, buf.String(), `"level":"WARN"`, "a steady defer must not flood a WARN per tick")
	assert.Contains(t, buf.String(), `"level":"DEBUG"`, "but it must still be recorded")

	buf.Reset()
	c.recordDefer(hardStop)
	assert.Contains(t, buf.String(), `"level":"WARN"`, "a CHANGE of reason is a new fact and must be visible")

	// Recovery closes the timeline: the other half of the gh#590 investigation was
	// "when did it start working again".
	buf.Reset()
	c.observeDetectionRun(gateDecision{proceed: true})
	assert.Contains(t, buf.String(), "readiness gate cleared", "recovery from a defer must be visible")

	buf.Reset()
	c.observeDetectionRun(gateDecision{proceed: true})
	assert.NotContains(t, buf.String(), "readiness gate cleared", "a steady healthy run must not log a recovery per tick")
}

// TestEvaluateReadiness_UnknownStatusFailsClosed covers the posture with no readiness
// feed at all (no bucket, no producer, watcher unbound): fail closed, attributed to
// the TRANSPORT (status_unknown), never to index state — and the allow_ungated_reads
// escape keeps its pre-ADR-083 semantics, which covered exactly this case (no
// verifiable status) and never a received not-ready.
func TestEvaluateReadiness_UnknownStatusFailsClosed(t *testing.T) {
	tests := []struct {
		name        string
		allowUngate bool
		wantProceed bool
		wantUngated bool
		wantReason  graph.DeferReason
	}{
		{"fail closed by default", false, false, false, graph.DeferStatusUnknown},
		{"allow_ungated_reads proceeds, marked ungated", true, true, true, graph.DeferNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newLoggedComponent(t, Config{
				Ports: basePorts(), AllowUngatedReads: tt.allowUngate,
			})

			got := c.evaluateReadiness()

			assert.Equal(t, tt.wantProceed, got.proceed)
			assert.Equal(t, tt.wantReason, got.reason)
			assert.Equal(t, tt.wantUngated, got.ungated,
				"an ungated proceed must be marked so it cannot claim a verified staleness")
			assert.False(t, got.reading.Known, "nothing may be fabricated when no envelope was ever received")
		})
	}
}

// deferCount reads one reason's counter. The metrics registry is a process-wide
// singleton, so assertions must be deltas rather than absolute values.
func deferCount(t *testing.T, c *Component, reason graph.DeferReason) float64 {
	t.Helper()
	counter, err := c.metrics.deferTotal.GetMetricWithLabelValues(string(reason))
	require.NoError(t, err)
	return testutil.ToFloat64(counter)
}
