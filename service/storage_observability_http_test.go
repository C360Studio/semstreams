package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/c360studio/semstreams/natsclient"
)

// --- fakes -------------------------------------------------------------------

// fakeReportEntry is one KV entry as a watch delivers it.
type fakeReportEntry struct {
	key   string
	value []byte
	op    jetstream.KeyValueOp
}

func (e fakeReportEntry) Bucket() string                  { return "STORAGE_REPORT_FAKE" }
func (e fakeReportEntry) Key() string                     { return e.key }
func (e fakeReportEntry) Value() []byte                   { return e.value }
func (e fakeReportEntry) Revision() uint64                { return 1 }
func (e fakeReportEntry) Created() time.Time              { return time.Now() }
func (e fakeReportEntry) Delta() uint64                   { return 0 }
func (e fakeReportEntry) Operation() jetstream.KeyValueOp { return e.op }

// fakeReportWatcher reproduces one KV watch, including the single nil entry
// nats.go sends once every current value has been delivered.
type fakeReportWatcher struct {
	updates chan jetstream.KeyValueEntry
}

func (w *fakeReportWatcher) Updates() <-chan jetstream.KeyValueEntry { return w.updates }
func (w *fakeReportWatcher) Stop() error                             { return nil }

func (w *fakeReportWatcher) put(key string, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	w.updates <- fakeReportEntry{key: key, value: encoded, op: jetstream.KeyValuePut}
}

func (w *fakeReportWatcher) synced() { w.updates <- nil }

// fakeReportWatchStore hands the consumer its watcher.
type fakeReportWatchStore struct {
	mu      sync.Mutex
	watcher *fakeReportWatcher
}

func (s *fakeReportWatchStore) WatchAll(
	_ context.Context, _ ...jetstream.WatchOpt,
) (jetstream.KeyWatcher, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.watcher == nil {
		s.watcher = &fakeReportWatcher{updates: make(chan jetstream.KeyValueEntry, 32)}
	}
	return s.watcher, nil
}

func (s *fakeReportWatchStore) established() *fakeReportWatcher {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.watcher
}

// --- helpers -----------------------------------------------------------------

// unknownRow is a resource the server declined to describe: no tier, no
// capacity on either axis, and therefore no usage series at all.
func unknownRow(name string) natsclient.ResourceReport {
	row := resourceRow(name, natsclient.TierUnknown)
	row.Resource.Bytes = natsclient.UnknownCapacity()
	row.Resource.Messages = natsclient.UnknownCapacity()
	row.Projection = natsclient.Projection{
		HeadroomUnavailable:        natsclient.ProjectionUnavailableUnknownCapacity,
		TimeToThresholdUnavailable: natsclient.ProjectionUnavailableUnknownCapacity,
	}
	row.Pressure = natsclient.Pressure{Unavailable: natsclient.PressureUnavailableUnknownCapacity}
	return row
}

func httpSurfaceOver(snapshot natsclient.StorageReportSnapshot) *storageReportHTTPSurface {
	return &storageReportHTTPSurface{snapshotOf: staticSnapshot(snapshot)}
}

// getReport drives the REGISTERED ROUTE rather than the handler function, so the
// prefix shape is exercised too.
func getReport(t *testing.T, surface *storageReportHTTPSurface) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	surface.register("/storage-observability", mux)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/storage-observability/report", nil))
	return recorder
}

func decodeReport(t *testing.T, recorder *httptest.ResponseRecorder) StorageReportResponse {
	t.Helper()
	var response StorageReportResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

// --- the response shape (task 4.4) -------------------------------------------

// TestStorageReportHTTP_ResponseRoundTrips pins the wire shape. An operator
// surface whose JSON drifts silently is one nobody can build a CLI or a
// dashboard against, and this is the response an operator reads next to the
// metrics.
func TestStorageReportHTTP_ResponseRoundTrips(t *testing.T) {
	collected := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	updated := collected.Add(time.Second)

	bounded := withPressure(resourceRow("LOGS", natsclient.TierFile), natsclient.PressureHigh)
	bounded.CollectedAt = collected
	unbounded := unboundedRow("EVENTS", natsclient.TierMemory)
	unbounded.CollectedAt = collected

	surface := httpSurfaceOver(natsclient.StorageReportSnapshot{
		Resources:    []natsclient.ResourceReport{unbounded, bounded},
		Account:      natsclient.AccountReport{ProducedBy: "unit-test", CollectedAt: collected},
		AccountKnown: true,
		Synced:       true,
		UpdatedAt:    updated,
		PressureCounts: map[natsclient.PressureState]int{
			natsclient.PressureHigh: 1,
		},
		NotEvaluated:  1,
		WorstPressure: natsclient.PressureHigh,
	})

	recorder := getReport(t, surface)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))

	// Decoded as a generic document first: these assertions are about the JSON
	// an operator's tooling sees, not about Go field names.
	var document map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &document))

	assert.Equal(t, true, document["report_only"],
		"the response states the report-only guarantee on its face")
	assert.Equal(t, true, document["synced"])
	assert.Equal(t, updated.Format(time.RFC3339Nano), document["updated_at"])

	summary, ok := document["summary"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(2), summary["resources"])
	assert.Equal(t, float64(1), summary["not_evaluated"])
	assert.Equal(t, "high", summary["worst_pressure"])
	pressure, ok := summary["pressure"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(1), pressure["high"])

	resources, ok := document["resources"].([]any)
	require.True(t, ok)
	require.Len(t, resources, 2)

	// The rows are the PUBLISHED rows verbatim, so the HTTP shape cannot drift
	// away from the bucket's.
	response := decodeReport(t, recorder)
	assert.Equal(t, []natsclient.ResourceReport{unbounded, bounded}, response.Resources)
	require.NotNil(t, response.Account)
	assert.Equal(t, "unit-test", response.Account.ProducedBy)
}

// TestStorageReportHTTP_UnknownAccountIsAbsentNotZero keeps an unread account
// from publishing a zero-valued comparison that reads as "no tiers, nothing
// over-committed" — the manufactured-confidence class this capability exists to
// remove.
func TestStorageReportHTTP_UnknownAccountIsAbsentNotZero(t *testing.T) {
	surface := httpSurfaceOver(natsclient.StorageReportSnapshot{Synced: true})

	recorder := getReport(t, surface)
	require.Equal(t, http.StatusOK, recorder.Code)

	var document map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &document))
	_, present := document["account"]
	assert.False(t, present, "an unread account publishes NO comparison, never an empty one")

	assert.Nil(t, decodeReport(t, recorder).Account)
}

// TestStorageReportHTTP_UnreadReportSaysSo distinguishes "not read yet" from
// "the account holds nothing". An empty resource list under synced=true is a
// measurement; under synced=false it is an admission.
func TestStorageReportHTTP_UnreadReportSaysSo(t *testing.T) {
	recorder := getReport(t, httpSurfaceOver(natsclient.StorageReportSnapshot{}))
	require.Equal(t, http.StatusOK, recorder.Code,
		"an unread report is still a 200: this surface fails no gate")

	response := decodeReport(t, recorder)
	assert.False(t, response.Synced)
	assert.Empty(t, response.Resources)

	var document map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &document))
	_, present := document["updated_at"]
	assert.False(t, present, "nothing was ever applied, so no update time is claimed")
	assert.NotNil(t, document["resources"], "the list is empty, never null")
}

// --- unbounded and not-evaluated visibility (task 4.7) -----------------------

// TestStorageReportHTTP_NotEvaluatedRowsStayVisible is task 4.7 on the HTTP
// surface. An unbounded resource carries NO pressure state, so any surface that
// filtered on `state != normal` would make exactly the unbounded resources
// invisible — the opposite of what 4.7 exists to do.
func TestStorageReportHTTP_NotEvaluatedRowsStayVisible(t *testing.T) {
	normal := withPressure(resourceRow("QUIET", natsclient.TierFile), natsclient.PressureNormal)
	unbounded := unboundedRow("UNBOUNDED", natsclient.TierFile)
	unknown := unknownRow("OFFLINE")

	surface := httpSurfaceOver(natsclient.StorageReportSnapshot{
		Resources:      []natsclient.ResourceReport{normal, unbounded, unknown},
		Synced:         true,
		PressureCounts: map[natsclient.PressureState]int{natsclient.PressureNormal: 1},
		NotEvaluated:   2,
	})

	response := decodeReport(t, getReport(t, surface))

	names := make(map[string]natsclient.ResourceReport, len(response.Resources))
	for _, row := range response.Resources {
		names[row.Resource.Name] = row
	}
	require.Contains(t, names, "UNBOUNDED", "an unbounded resource must be named, not filtered out")
	require.Contains(t, names, "OFFLINE", "a resource with unknown capacity must be named too")
	assert.Equal(t, 2, response.Summary.NotEvaluated,
		"the rows carrying no state at all are counted rather than folded into normal")

	// Never represented as having headroom.
	for _, name := range []string{"UNBOUNDED", "OFFLINE"} {
		row := names[name]
		assert.False(t, row.Pressure.Evaluated, "%s carries no pressure state", name)
		assert.Nil(t, row.Projection.HeadroomBytes, "%s must not report headroom bytes", name)
		assert.Nil(t, row.Projection.HeadroomFraction, "%s must not report a headroom fraction", name)
		assert.Nil(t, row.Projection.TimeToThreshold, "%s must not project exhaustion", name)
		assert.NotEmpty(t, row.Projection.HeadroomUnavailable, "%s says WHY it has no headroom", name)
	}

	// And the two are still distinguishable from each other, on the wire.
	assert.Equal(t, natsclient.CapacityUnbounded, names["UNBOUNDED"].Resource.Bytes.State)
	assert.Equal(t, natsclient.CapacityUnknown, names["OFFLINE"].Resource.Bytes.State)

	// The whole reason this route takes no filter parameter.
	assert.Len(t, response.Resources, 3, "every published row is served; the route does not filter")
}

// TestStorageReportHTTP_WorstPressureIsEmptyWhenNothingIsEvaluated keeps the
// summary from asserting `normal` over an account whose rows all declined to
// evaluate.
func TestStorageReportHTTP_WorstPressureIsEmptyWhenNothingIsEvaluated(t *testing.T) {
	surface := httpSurfaceOver(natsclient.StorageReportSnapshot{
		Resources:    []natsclient.ResourceReport{unboundedRow("UNBOUNDED", natsclient.TierFile)},
		Synced:       true,
		NotEvaluated: 1,
	})

	var document map[string]any
	require.NoError(t, json.Unmarshal(getReport(t, surface).Body.Bytes(), &document))

	summary, ok := document["summary"].(map[string]any)
	require.True(t, ok)
	_, present := summary["worst_pressure"]
	assert.False(t, present, "no row was evaluated, so no verdict is published")
	assert.Equal(t, float64(1), summary["not_evaluated"])
}

// --- report-only (the hard constraint) ---------------------------------------

// TestStorageReportHTTP_CriticalPressureFailsNothing is the report-only
// guarantee expressed where it would break first. A 5xx on critical pressure
// would turn an observability route into a readiness signal for anything that
// polls it.
func TestStorageReportHTTP_CriticalPressureFailsNothing(t *testing.T) {
	surface := httpSurfaceOver(natsclient.StorageReportSnapshot{
		Resources: []natsclient.ResourceReport{
			withPressure(resourceRow("FULL", natsclient.TierFile), natsclient.PressureCritical),
		},
		Synced:         true,
		PressureCounts: map[natsclient.PressureState]int{natsclient.PressureCritical: 1},
		WorstPressure:  natsclient.PressureCritical,
	})

	recorder := getReport(t, surface)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, natsclient.PressureCritical, decodeReport(t, recorder).Summary.WorstPressure)
}

// TestStorageReportHTTP_RejectsNonGET keeps the route read-only.
func TestStorageReportHTTP_RejectsNonGET(t *testing.T) {
	mux := http.NewServeMux()
	httpSurfaceOver(natsclient.StorageReportSnapshot{}).register("/storage-observability", mux)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder,
		httptest.NewRequest(http.MethodPost, "/storage-observability/report", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
}

// TestStorageReportHTTP_PrefixWithOrWithoutTrailingSlash follows the
// registration shape the other services use, where the prefix may or may not
// carry one.
func TestStorageReportHTTP_PrefixWithOrWithoutTrailingSlash(t *testing.T) {
	for _, prefix := range []string{"/storage-observability", "/storage-observability/"} {
		mux := http.NewServeMux()
		httpSurfaceOver(natsclient.StorageReportSnapshot{Synced: true}).register(prefix, mux)

		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder,
			httptest.NewRequest(http.MethodGet, "/storage-observability/report", nil))
		assert.Equal(t, http.StatusOK, recorder.Code, "prefix %q", prefix)
	}
}

// --- the anti-divergence property (task 4.4) ---------------------------------

// TestStorageReportHTTP_CannotDivergeFromTheMetricsSurface is the load-bearing
// test of this slice, and its SHAPE is the argument: ONE consumer, fed by ONE
// watch, fans out to BOTH surfaces. Neither recomputes anything — the HTTP
// route projects the published rows and the metrics surface publishes them as
// series — so the two cannot disagree about an account.
//
// A test that hand-built a snapshot for HTTP and separately fed rows to the
// metrics would prove only that two fixtures matched.
func TestStorageReportHTTP_CannotDivergeFromTheMetricsSurface(t *testing.T) {
	metrics, registry := newTestStorageMetrics(t)

	store := &fakeReportWatchStore{}
	consumer, err := natsclient.NewStorageReportConsumer(store,
		natsclient.StorageReportConsumerConfig{
			Observer:     metrics,
			RetryBackoff: time.Millisecond,
		})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		consumer.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("the consumer did not stop when its context ended")
		}
	})

	// One publication, four resources spanning every state that matters.
	rows := []natsclient.ResourceReport{
		withPressure(resourceRow("CRITICAL", natsclient.TierFile), natsclient.PressureCritical),
		withPressure(resourceRow("NORMAL", natsclient.TierFile), natsclient.PressureNormal),
		unboundedRow("UNBOUNDED", natsclient.TierMemory),
		unknownRow("OFFLINE"),
	}

	require.Eventually(t, func() bool { return store.established() != nil },
		3*time.Second, time.Millisecond, "the watch is established")

	watcher := store.established()
	for _, row := range rows {
		watcher.put(row.Resource.Name, row)
	}
	watcher.synced()

	require.Eventually(t, func() bool {
		snapshot := consumer.Snapshot()
		return snapshot.Synced && len(snapshot.Resources) == len(rows)
	}, 3*time.Second, time.Millisecond, "every published row reaches the consumer")

	surface := &storageReportHTTPSurface{snapshotOf: consumer.Snapshot}
	response := decodeReport(t, getReport(t, surface))

	require.Len(t, response.Resources, len(rows))
	for _, row := range response.Resources {
		name := row.Resource.Name
		labels := map[string]string{"resource": name}

		// Pressure. The metrics surface encodes it as a NUMERIC severity and
		// publishes no series at all for a row carrying no state; the HTTP
		// surface carries the state itself. The two must agree about which rows
		// have a verdict, and about what it is.
		severity, published := gaugeValue(t, registry, "semstreams_storage_resource_pressure", labels)
		assert.Equal(t, row.Pressure.Evaluated, published,
			"%s: a pressure series exists exactly when the row was evaluated", name)
		if row.Pressure.Evaluated {
			assert.InDelta(t, float64(pressureSeverityValue(row.Pressure.State)), severity, 1e-9,
				"%s: both surfaces report the same pressure", name)
		}

		// Usage. Absent on both surfaces for an unreadable resource.
		used, hasUsed := gaugeValue(t, registry, "semstreams_storage_resource_used_bytes", labels)
		rowUsed, rowHasUsed := row.Resource.Bytes.Usage()
		assert.Equal(t, rowHasUsed, hasUsed, "%s: usage is present on both surfaces or neither", name)
		if rowHasUsed {
			assert.InDelta(t, float64(rowUsed), used, 1e-9, "%s: both surfaces report the same usage", name)
		}

		// Limit. Absent for unbounded AND unknown, which is what keeps an
		// unbounded resource from reading as having headroom on either surface.
		limit, hasLimit := gaugeValue(t, registry, "semstreams_storage_resource_limit_bytes", labels)
		rowLimit, rowHasLimit := row.Resource.Bytes.Limit()
		assert.Equal(t, rowHasLimit, hasLimit, "%s: a limit is present on both surfaces or neither", name)
		if rowHasLimit {
			assert.InDelta(t, float64(rowLimit), limit, 1e-9, "%s: both surfaces report the same bound", name)
		}
	}

	// The aggregate the alert rule reads must equal the summary the HTTP route
	// serves, or an operator paging on one would be reading a different account
	// from the one the route describes.
	notEvaluated := 0.0
	for _, tier := range []string{"file", "memory", "unknown"} {
		value, _ := gaugeValue(t, registry, "semstreams_storage_pressure_resources",
			map[string]string{"tier": tier, "pressure_state": pressureStateNotEvaluated})
		notEvaluated += value
	}
	assert.InDelta(t, float64(response.Summary.NotEvaluated), notEvaluated, 1e-9,
		"both surfaces count the same not-evaluated rows")
	assert.Equal(t, 2, response.Summary.NotEvaluated)
}

// --- the example alert rule (tasks 4.2, 4.7, 4.8) ----------------------------

// storageAlertRuleFile is the operator-facing example rule set. It lives beside
// the Prometheus scrape config the compose stack already mounts.
const storageAlertRuleFile = "../configs/prometheus/rules/storage-pressure.yml"

type promRule struct {
	Alert       string            `yaml:"alert"`
	Expr        string            `yaml:"expr"`
	For         string            `yaml:"for"`
	Labels      map[string]string `yaml:"labels"`
	Annotations map[string]string `yaml:"annotations"`
}

type promRuleFile struct {
	Groups []struct {
		Name  string     `yaml:"name"`
		Rules []promRule `yaml:"rules"`
	} `yaml:"groups"`
}

// loadStorageAlertRules parses the example rule set and flattens it.
func loadStorageAlertRules(t *testing.T) []promRule {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(storageAlertRuleFile))
	require.NoError(t, err, "the example alert rule must exist: a gauge with nothing "+
		"downstream is the phantom-signal class this program has deleted thirteen instances of")

	var parsed promRuleFile
	require.NoError(t, yaml.Unmarshal(raw, &parsed), "the rule file must be valid YAML")
	require.NotEmpty(t, parsed.Groups)

	var rules []promRule
	for _, group := range parsed.Groups {
		rules = append(rules, group.Rules...)
	}
	require.NotEmpty(t, rules)
	return rules
}

// TestStorageAlertRules_ReferenceOnlyPublishedMetrics is what makes the example
// rule a real CONSUMER rather than a second phantom. A rule naming a metric this
// binary never publishes is silently never satisfied, which looks exactly like a
// calm account.
func TestStorageAlertRules_ReferenceOnlyPublishedMetrics(t *testing.T) {
	metrics, registry := newTestStorageMetrics(t)

	// Every shape the surface can publish, so the closed-enum aggregates and the
	// per-resource series are all present in the registry. The critical row also
	// carries a measured rate and a projection, because those series exist only
	// once a resource has two observations and is actually growing.
	metrics.ObserveResource(withGrowth(
		withPressure(resourceRow("CRITICAL", natsclient.TierFile), natsclient.PressureCritical),
		1024, 30*time.Minute))
	metrics.ObserveResource(unboundedRow("UNBOUNDED", natsclient.TierFile))
	metrics.ObserveResource(unknownRow("OFFLINE"))
	// The file tier carries its OWN growth, projection and pressure as well as the
	// over-commitment verdict. Those series exist only for a bounded tier with a
	// measured rate, and they are what every unbounded resource's state is
	// inherited from — so a fixture without them would let a rule reading them
	// look covered while publishing nothing.
	tierRate := 4096.0
	tierHeadroom := int64(1 << 20)
	tierToThreshold := 90 * time.Minute
	metrics.ObserveAccount(natsclient.AccountReport{
		Tiers: []natsclient.TierComparison{
			{
				Tier:  natsclient.TierFile,
				Limit: natsclient.NewCapacity(1<<30, 1<<20, true),
				State: natsclient.OvercommitmentOver,
				Growth: natsclient.Growth{
					State:          natsclient.GrowthKnown,
					BytesPerSecond: &tierRate,
					ObservedOver:   30 * time.Minute,
				},
				Projection: natsclient.Projection{
					HeadroomBytes:   &tierHeadroom,
					TimeToThreshold: &tierToThreshold,
				},
				Pressure: natsclient.Pressure{
					Evaluated:        true,
					State:            natsclient.PressureHigh,
					RaisedBy:         natsclient.PressureInputHeadroom,
					EvaluatedAgainst: natsclient.PressureBasisAccountTier,
				},
			},
			{
				Tier:  natsclient.TierMemory,
				Limit: natsclient.NewCapacity(0, 0, true),
				State: natsclient.OvercommitmentNotApplicable,
			},
		},
	})

	families, err := registry.PrometheusRegistry().Gather()
	require.NoError(t, err)
	published := make(map[string]struct{}, len(families))
	for _, family := range families {
		published[family.GetName()] = struct{}{}
	}

	metricName := regexp.MustCompile(`semstreams_storage_[a-z_]+`)
	referenced := 0
	for _, rule := range loadStorageAlertRules(t) {
		for _, name := range metricName.FindAllString(rule.Expr, -1) {
			referenced++
			assert.Contains(t, published, name,
				"alert %s reads %s, which this binary never publishes", rule.Alert, name)
		}
	}
	assert.Greater(t, referenced, 0, "the rule set must actually read the storage metrics")
}

// TestStorageAlertRules_KeyNothingOnRowDisappearance is task 4.8. Reclamation is
// eventually consistent under concurrent producers, so a row may transiently
// vanish and return; an alert keyed on its absence would fire on a timing skew
// and be unsound as a signal.
func TestStorageAlertRules_KeyNothingOnRowDisappearance(t *testing.T) {
	for _, rule := range loadStorageAlertRules(t) {
		assert.NotContains(t, rule.Expr, "absent(",
			"alert %s keys on absence; reclamation is eventually consistent and a row "+
				"may transiently vanish and return", rule.Alert)
		assert.NotContains(t, rule.Expr, "absent_over_time(",
			"alert %s keys on absence over a window, which a transiently reclaimed row "+
				"would satisfy", rule.Alert)
	}
}

// TestStorageAlertRules_MakeUnboundedResourcesVisible is task 4.7 on the
// alerting surface. An unbounded resource has no bound to have headroom against,
// so a rule set built only on the per-resource headroom and projection series
// would make exactly those resources invisible. It is named by its usage series
// and counted in the not-evaluated aggregate instead.
//
// Since task 5.9 such a resource DOES carry a pressure severity, inherited from
// its storage tier — but the per-resource pressure alerts deliberately scope
// themselves to bounded resources, so this rule remains the thing that names an
// unbounded resource, and the tier alerts are what page for its state.
func TestStorageAlertRules_MakeUnboundedResourcesVisible(t *testing.T) {
	var namesUnbounded, countsNotEvaluated bool
	for _, rule := range loadStorageAlertRules(t) {
		expr := rule.Expr
		if strings.Contains(expr, "semstreams_storage_resource_used_bytes") &&
			strings.Contains(expr, "unless") &&
			strings.Contains(expr, "semstreams_storage_resource_limit_bytes") {
			namesUnbounded = true
		}
		if strings.Contains(expr, pressureStateNotEvaluated) ||
			strings.Contains(expr, `capacity_state="unknown"`) {
			countsNotEvaluated = true
		}
	}
	assert.True(t, namesUnbounded,
		"a rule must NAME the resources carrying no bound; they have no pressure series to alert on")
	assert.True(t, countsNotEvaluated,
		"a rule must surface the rows carrying no pressure state at all")
}

// TestStorageAlertRules_AreAnnotatedAndReportOnly keeps the example from
// implying enforcement. Nothing in this capability rejects, throttles, or
// degrades because of pressure, and an alert that told an operator otherwise
// would be worse than no alert.
func TestStorageAlertRules_AreAnnotatedAndReportOnly(t *testing.T) {
	reportOnly := 0
	for _, rule := range loadStorageAlertRules(t) {
		require.NotEmpty(t, rule.Alert)
		require.NotEmpty(t, rule.Annotations["summary"], "alert %s needs a summary", rule.Alert)
		require.NotEmpty(t, rule.Annotations["description"],
			"alert %s needs a description an operator can act on", rule.Alert)
		require.NotEmpty(t, rule.For,
			"alert %s needs a `for` window; the report is republished every collection "+
				"and a zero-duration alert would flap on one skewed sample", rule.Alert)
		if strings.Contains(rule.Annotations["description"], "report-only") {
			reportOnly++
		}
	}
	assert.Greater(t, reportOnly, 0,
		"at least one alert must state that pressure rejects nothing")
}
