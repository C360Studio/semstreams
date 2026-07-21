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

// TestConfig_MaxStaleness_JSONRoundTrip proves the operator-surface field survives a
// marshal/unmarshal through the production Config type — no shadow struct (house
// operator-surface discipline) — and that ApplyDefaults parses it into the duration
// the gate actually reads. The absent case must decode to the strict default 0, which
// is the exact Ready gate.
func TestConfig_MaxStaleness_JSONRoundTrip(t *testing.T) {
	original := Config{MaxStalenessStr: "3s"}
	data, err := json.Marshal(original)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"max_staleness":"3s"`, "field must serialize under its json tag")

	var decoded Config
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "3s", decoded.MaxStalenessStr, "tolerance must survive the round trip")

	// The parsed duration is what the gate reads; the string alone proves nothing.
	decoded.ApplyDefaults()
	assert.Equal(t, 3*time.Second, decoded.MaxStaleness(), "ApplyDefaults must parse the duration the gate consumes")

	// Absent field decodes to the strict, contract-preserving default.
	var fromEmpty Config
	require.NoError(t, json.Unmarshal([]byte(`{}`), &fromEmpty))
	fromEmpty.ApplyDefaults()
	assert.Empty(t, fromEmpty.MaxStalenessStr, "absent tolerance must stay empty")
	assert.Equal(t, time.Duration(0), fromEmpty.MaxStaleness(), "absent tolerance must default to 0 (exact Ready gate)")
}

// TestConfig_Validate_MaxStaleness covers the guards: a typo must not silently
// tighten the gate to exact (ApplyDefaults leaves an unparseable value at 0, so
// Validate is the only thing between the operator and a silent behavior change), and a
// pathological value must not silently ungate the bootstrap/cutover defer.
func TestConfig_Validate_MaxStaleness(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr string
	}{
		{"absent is valid (exact gate)", "", ""},
		{"explicit zero is valid", "0s", ""},
		{"modest tolerance is valid", "3s", ""},
		{"sub-second tolerance is valid", "1500ms", ""},
		{"exactly the ceiling is valid", maxStalenessCeiling.String(), ""},
		{"past the ceiling is rejected", (maxStalenessCeiling + time.Second).String(), "exceeds the sane maximum"},
		{"negative is rejected", "-1s", "negative"},
		{"a bare number is not a duration", "3000", "is not a duration"},
		{"gibberish is rejected", "soon", "is not a duration"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{Ports: basePorts(), MaxStalenessStr: tt.value}
			cfg.ApplyDefaults()

			err := cfg.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "max_staleness", "error must name the offending field")
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestCreateGraphClustering_RejectsRemovedIndexLagTolerance is the BREAKING-change
// contract. encoding/json would silently DROP the withdrawn key and hand the operator
// the strict exact gate with no warning — the exact silent behavior change ADR-083
// exists to prevent. The load must fail, and the message must name the replacement.
func TestCreateGraphClustering_RejectsRemovedIndexLagTolerance(t *testing.T) {
	// The factory only stores the client; a zero value gets past its nil-dependency
	// guard and reaches config decoding, which is what this test is about.
	deps := component.Dependencies{NATSClient: &natsclient.Client{}}

	withRemovedField := []byte(`{
		"ports": {
			"inputs":  [{"name":"entity_watch","type":"kv-watch","subject":"ENTITY_STATES"}],
			"outputs": [{"name":"communities","type":"kv-write","subject":"COMMUNITY_INDEX"}]
		},
		"index_lag_tolerance": 250
	}`)
	_, err := CreateGraphClustering(withRemovedField, deps)
	require.Error(t, err, "a removed field must fail the load, not be silently dropped")
	assert.Contains(t, err.Error(), "index_lag_tolerance", "the error must name the field the operator wrote")
	assert.Contains(t, err.Error(), "max_staleness", "the error must name the replacement, not just complain")

	// The same config with the replacement loads cleanly and reaches the gate, so the
	// probe rejects the field rather than the shape.
	migrated := []byte(`{
		"ports": {
			"inputs":  [{"name":"entity_watch","type":"kv-watch","subject":"ENTITY_STATES"}],
			"outputs": [{"name":"communities","type":"kv-write","subject":"COMMUNITY_INDEX"}]
		},
		"max_staleness": "3s"
	}`)
	comp, err := CreateGraphClustering(migrated, deps)
	require.NoError(t, err)
	assert.Equal(t, 3*time.Second, comp.(*Component).config.MaxStaleness(),
		"the replacement field must reach the gate through the production factory")
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

// TestObserveDetectionRun proves clustering-under-staleness is operator-visible, not
// Debug-only (#579 lesson): the gauge carries the view age of the last run and an INFO
// log fires with the value when the run proceeded on bounded-stale topology.
func TestObserveDetectionRun(t *testing.T) {
	c, buf := newLoggedComponent(t, Config{Ports: basePorts(), MaxStalenessStr: "3s"})

	// A run under bounded staleness: gauge set to the view age AND an INFO log.
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
	assert.Contains(t, logged, `"level":"INFO"`, "clustering-under-staleness must not be confined to debug")
	assert.Contains(t, logged, `"staleness_ms":1500`, "the log must carry the view age")
	assert.Contains(t, logged, `"max_staleness":3000000000`, "the log must carry the configured tolerance")

	// An exactly-caught-up run resets the gauge to 0 and emits no INFO log.
	buf.Reset()
	c.observeDetectionRun(gateDecision{
		proceed: true,
		reading: readiness.Reading{Known: true, Fresh: true, Status: graph.IndexStatusResponse{Ready: true, State: graph.IndexStateReady}},
	})
	assert.Equal(t, float64(0), testutil.ToFloat64(c.metrics.stalenessAtDetection),
		"gauge must reflect the latest run (0 = caught up)")
	assert.False(t, strings.Contains(buf.String(), `"level":"INFO"`),
		"a caught-up run must not emit the bounded-staleness INFO log")
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

// TestRecordDefer_IsEvidence is the gh#590 regression guard. The old defer line was a
// bare constant, so a transport failure and a genuinely-behind index logged
// identically and the investigation cost three comment cycles. Every defer must now
// carry the full reading and increment its typed counter.
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
			name:   "an over-stale view names the age against the bound",
			reason: graph.DeferOverStaleness,
			reading: readiness.Reading{
				Known: true, Fresh: true,
				Status: graph.IndexStatusResponse{State: graph.IndexStateBuilding, TargetRevision: 500, Lag: 400, StalenessMs: 12000},
			},
			want: []string{`"reason":"over_staleness"`, `"staleness_ms":12000`, `"max_staleness":3000000000`},
		},
		{
			// The gh#474 cutover window, now wire-observable (ADR-084 D2). It replaces
			// the former `empty` reason: TargetRevision==0 was a proxy that was wrong
			// in both directions — false mid-cutover, and true for the
			// authoritatively-empty graph it then wrongly deferred.
			name:    "an unbootstrapped index is not confused with caught up",
			reason:  graph.DeferBootstrapIncomplete,
			reading: readiness.Reading{Known: true, Fresh: true, Status: graph.IndexStatusResponse{State: graph.IndexStateBuilding}},
			want:    []string{`"reason":"bootstrap_incomplete"`, `"lag":0`},
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
			c, buf := newLoggedComponent(t, Config{Ports: basePorts(), MaxStalenessStr: "3s"})
			before := deferCount(t, c, tt.reason)

			c.recordDefer(gateDecision{reason: tt.reason, reading: tt.reading})

			logged := buf.String()
			for _, want := range tt.want {
				assert.Contains(t, logged, want, "the defer line must be diagnosable from ONE grep")
			}
			assert.Equal(t, before+1, deferCount(t, c, tt.reason), "every defer must be counted under its reason")
		})
	}
}

// TestRecordDefer_TransitionsAreVisible: a defer must be visible without raising the
// log level (the first of a reason, and any change of reason), while a steady defer
// must not flood one line per tick. defer_total stays the rate signal either way.
func TestRecordDefer_TransitionsAreVisible(t *testing.T) {
	c, buf := newLoggedComponent(t, Config{Ports: basePorts(), MaxStalenessStr: "3s"})
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
// verifiable status) and never a received not-ready. The generous tolerance proves
// tolerance is not evaluated against unknown state.
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
				Ports: basePorts(), MaxStalenessStr: "10m", AllowUngatedReads: tt.allowUngate,
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
