// storage_observability_metrics.go is the Prometheus surface over the
// PUBLISHED storage report (storage-observability, task 4.1).
//
// It is a CONSUMER of the report bucket, never a second producer: every value
// here arrives through natsclient.StorageReportObserver, carrying rows the
// collector already derived. Growth, projection and pressure need inputs only a
// collection holds — the retained observation series and the account limits —
// so a metrics surface that recomputed any of them would eventually disagree
// with the report an operator reads next to it.
//
// # The cardinality decision
//
// Resource count is unbounded in principle, and an account can carry hundreds
// of streams, so the split is deliberate:
//
//   - NUMERIC facts are per-resource, labelled by resource, owner, kind and
//     tier. These are what an operator acts on, and acting requires a name:
//     "which stream is filling" has no useful aggregate answer.
//   - CATEGORICAL states are AGGREGATED into fixed-cardinality counts per tier.
//     A state carried as a per-resource label value multiplies series by the
//     state count for no alerting gain, and — worse — leaves a stale series
//     behind on every transition, because Prometheus keeps the old label tuple
//     until something deletes it. `count by (state)` is the question operators
//     actually ask of a categorical axis.
//   - The one place a state IS a label is the ACCOUNT tier enum, where the
//     label set is closed and tiny (three tiers) and EVERY combination is
//     emitted on every rebuild. Fully emitting a closed enum has no stale-series
//     problem, and it is what makes "unbounded" and "not applicable" visible as
//     explicit values rather than as absent series nobody can alert on.
//
// Per-resource pressure is therefore a NUMERIC severity (0..3), which is
// label-stable across transitions and alertable as `>= 2`.
//
// # Absence is a measurement
//
// A gauge is published only when the underlying value exists. An unmeasured
// growth rate publishes NO series rather than zero, because zero means
// "measured, and not growing" — a different fact that licenses a different
// decision. Every optional series is explicitly retracted when its value goes
// away, so a resource that loses its bound stops reporting a limit instead of
// reporting the last one forever.
//
// Nothing here rejects, throttles, or degrades anything.

package service

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
)

// resourceLabels are the per-resource label names, in the order every
// DeleteLabelValues call must use.
var resourceLabels = []string{"resource", "owner", "kind", "tier"}

// metricTiers is the closed tier enum. Fully emitted on every rebuild.
var metricTiers = []natsclient.StorageTier{
	natsclient.TierFile, natsclient.TierMemory, natsclient.TierUnknown,
}

var metricCapacityStates = []natsclient.CapacityState{
	natsclient.CapacityBounded, natsclient.CapacityUnbounded, natsclient.CapacityUnknown,
}

// pressureStateNotEvaluated is the aggregate bucket for a resource carrying no
// pressure state at all. It is a real, counted value rather than an omission:
// an unbounded resource has no pressure state, and a surface that only counted
// states would make exactly those resources invisible.
const pressureStateNotEvaluated = "not-evaluated"

var metricPressureStates = []string{
	string(natsclient.PressureNormal),
	string(natsclient.PressureWarning),
	string(natsclient.PressureHigh),
	string(natsclient.PressureCritical),
	pressureStateNotEvaluated,
}

var metricLimitStates = []natsclient.CapacityState{
	natsclient.CapacityBounded, natsclient.CapacityUnbounded, natsclient.CapacityUnknown,
}

var metricOvercommitmentStates = []natsclient.OvercommitmentState{
	natsclient.OvercommitmentWithin,
	natsclient.OvercommitmentOver,
	natsclient.OvercommitmentNotApplicable,
}

// trackedResource is what the surface remembers about a resource so it can
// retract exactly the series it published for it.
//
// The label tuple is retained rather than recomputed because it can CHANGE
// under a stable resource — a bucket added to the descriptor catalog gains an
// owner between two collections — and deleting a series requires the tuple it
// was created with.
type trackedResource struct {
	labels   []string
	capacity natsclient.CapacityState
	pressure string
}

// storageObservabilityMetrics publishes the report as Prometheus series.
// Safe for concurrent use; the observer callbacks arrive from the consumer's
// watch goroutine while a scrape reads the registry.
type storageObservabilityMetrics struct {
	mu sync.Mutex

	// Per-resource numeric facts.
	used            *prometheus.GaugeVec
	limit           *prometheus.GaugeVec
	headroomBytes   *prometheus.GaugeVec
	headroomRatio   *prometheus.GaugeVec
	growth          *prometheus.GaugeVec
	timeToThreshold *prometheus.GaugeVec
	pressure        *prometheus.GaugeVec

	// Fixed-cardinality categorical aggregates.
	resourcesByCapacity *prometheus.GaugeVec
	resourcesByPressure *prometheus.GaugeVec

	// Account-level, per storage tier.
	accountLimit          *prometheus.GaugeVec
	accountUsed           *prometheus.GaugeVec
	accountDeclared       *prometheus.GaugeVec
	accountLimitState     *prometheus.GaugeVec
	accountOvercommitment *prometheus.GaugeVec

	// reportCollected carries the COLLECTION time of the freshest published
	// row, as a unix timestamp, so an operator can alert on
	// `time() - gauge > horizon`. It exists because a stopped collector is
	// otherwise indistinguishable from a calm account: Prometheus stamps
	// SCRAPE time, not data time, so every other series here keeps looking
	// fresh forever while the data behind it ages. timestamp() cannot
	// substitute for the same reason.
	//
	// The source is deliberately the row's CollectedAt and NOT the snapshot's
	// UpdatedAt: UpdatedAt advances whenever the consumer applies a change,
	// including a watch reconnect replaying rows it already held, which would
	// refresh the gauge without any new collection having happened — masking
	// exactly the failure this gauge exists to expose.
	reportCollected prometheus.Gauge
	lastCollected   time.Time

	tracked      map[string]trackedResource
	accountTiers map[natsclient.StorageTier]struct{}
}

// observeCollectedAt advances the freshness gauge monotonically. Monotonic
// because a watch delivers rows in no guaranteed order and a replay may hand
// back an older row after a newer one; letting the gauge move backwards would
// manufacture staleness that never happened.
// A zero timestamp needs no separate guard: the zero time is never After
// anything, including the zero lastCollected, so an unstamped row is rejected
// by the same comparison. Adding an IsZero arm here would be a dead defense
// that no mutation could kill.
func (m *storageObservabilityMetrics) observeCollectedAt(collectedAt time.Time) {
	if !collectedAt.After(m.lastCollected) {
		return
	}
	m.lastCollected = collectedAt
	m.reportCollected.Set(float64(collectedAt.UnixNano()) / float64(time.Second))
}

func storageGauge(name, help string, labels []string) *prometheus.GaugeVec {
	return prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "semstreams",
		Subsystem: "storage",
		Name:      name,
		Help:      help,
	}, labels)
}

func newStorageObservabilityMetrics() *storageObservabilityMetrics {
	tierLabel := []string{"tier"}
	tierStateLabel := []string{"tier", "state"}

	return &storageObservabilityMetrics{
		used: storageGauge("resource_used_bytes",
			"Bytes currently stored by one account storage resource. Absent when usage could not be read.",
			resourceLabels),
		limit: storageGauge("resource_limit_bytes",
			"Configured byte bound of one resource. Absent for an unbounded or unreadable resource.",
			resourceLabels),
		headroomBytes: storageGauge("resource_headroom_bytes",
			"Bytes between current usage and the configured bound. Negative when already over it.",
			resourceLabels),
		headroomRatio: storageGauge("resource_headroom_ratio",
			"Free fraction of the configured bound (0..1).",
			resourceLabels),
		growth: storageGauge("resource_growth_bytes_per_second",
			"Observed rate of change across successive published observations. "+
				"Absent when fewer than two observations exist; zero means measured and not growing.",
			resourceLabels),
		timeToThreshold: storageGauge("resource_time_to_threshold_seconds",
			"Projected seconds until usage reaches the critical-headroom level at the observed rate. "+
				"Absent when unbounded, unmeasured, or not growing.",
			resourceLabels),
		pressure: storageGauge("resource_pressure",
			"Derived storage pressure (0=normal, 1=warning, 2=high, 3=critical). "+
				"REPORT-ONLY: nothing is rejected, throttled, or degraded because of it. "+
				"Absent for a resource with unbounded or unknown capacity, which carries no pressure state.",
			resourceLabels),

		resourcesByCapacity: storageGauge("resources",
			"Count of account storage resources by tier and capacity state (bounded, unbounded, unknown).",
			[]string{"tier", "capacity_state"}),
		resourcesByPressure: storageGauge("pressure_resources",
			"Count of account storage resources by tier and pressure state, "+
				"including the not-evaluated resources that carry no state at all.",
			[]string{"tier", "pressure_state"}),

		accountLimit: storageGauge("account_limit_bytes",
			"JetStream account byte limit for one storage tier. "+
				"Absent when the tier is unbounded or its limit could not be read; "+
				"read account_limit_state to tell those apart.",
			tierLabel),
		accountUsed: storageGauge("account_used_bytes",
			"Account bytes stored in one storage tier, as the server reports it.",
			tierLabel),
		accountDeclared: storageGauge("account_declared_bytes",
			"Sum of the configured bounds declared by BOUNDED resources in one storage tier. "+
				"A floor rather than a total when the tier holds unbounded resources. "+
				"Tiers are never summed together: memory and file have separate account limits.",
			tierLabel),
		accountLimitState: storageGauge("account_limit_state",
			"Whether a tier's account limit is bounded, unbounded, or unknown (1 for the current state). "+
				"Every state is emitted so unbounded is explicit rather than an absent series.",
			tierStateLabel),
		accountOvercommitment: storageGauge("account_overcommitment",
			"Whether a tier's declared bounds are within-limit, over-committed, or not-applicable "+
				"(1 for the current state). An unbounded or unreadable account limit reports "+
				"not-applicable, NEVER within-limit.",
			tierStateLabel),

		reportCollected: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "storage",
			Name:      "report_collected_timestamp_seconds",
			Help: "Unix timestamp of the freshest published report row's COLLECTION time. " +
				"Alert on `time() - this > horizon` to catch a collector that stopped: every other " +
				"series here is stamped with SCRAPE time, so a stopped collector otherwise looks " +
				"identical to a calm account. Absent until the first row is read.",
		}),

		tracked:      make(map[string]trackedResource, 32),
		accountTiers: make(map[natsclient.StorageTier]struct{}, 3),
	}
}

// register publishes the collectors. It is idempotent: the constructor
// registers through the injected registry so the series reach the scraped
// registry regardless of whether anything calls the Service interface's
// RegisterMetrics, and both paths may run against the same registrar.
func (m *storageObservabilityMetrics) register(registrar metric.MetricsRegistrar) error {
	for name, vec := range map[string]*prometheus.GaugeVec{
		"resource_used_bytes":                m.used,
		"resource_limit_bytes":               m.limit,
		"resource_headroom_bytes":            m.headroomBytes,
		"resource_headroom_ratio":            m.headroomRatio,
		"resource_growth_bytes_per_second":   m.growth,
		"resource_time_to_threshold_seconds": m.timeToThreshold,
		"resource_pressure":                  m.pressure,
		"resources":                          m.resourcesByCapacity,
		"pressure_resources":                 m.resourcesByPressure,
		"account_limit_bytes":                m.accountLimit,
		"account_used_bytes":                 m.accountUsed,
		"account_declared_bytes":             m.accountDeclared,
		"account_limit_state":                m.accountLimitState,
		"account_overcommitment":             m.accountOvercommitment,
	} {
		if err := registrar.RegisterGaugeVec(StorageObservabilityServiceName, name, vec); err != nil {
			return err
		}
	}
	return registrar.RegisterGauge(StorageObservabilityServiceName,
		"report_collected_timestamp_seconds", m.reportCollected)
}

// ObserveResource publishes one row's series.
func (m *storageObservabilityMetrics) ObserveResource(row natsclient.ResourceReport) {
	resource := row.Resource
	labels := resourceLabelValues(resource)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.observeCollectedAt(row.CollectedAt)

	// A label tuple that changed identifies DIFFERENT series. Retract the old
	// ones first, or a bucket that gained a catalog owner reports under both
	// forever.
	previous, held := m.tracked[resource.Name]
	if held && !sameLabels(previous.labels, labels) {
		m.deleteResourceSeries(previous.labels)
	}

	m.setOrDelete(m.used, labels, resource.Bytes.Usage)
	m.setOrDelete(m.limit, labels, resource.Bytes.Limit)
	m.setOrDeletePtr(m.headroomBytes, labels, row.Projection.HeadroomBytes)
	m.setOrDeleteFloatPtr(m.headroomRatio, labels, row.Projection.HeadroomFraction)
	m.setOrDeleteRate(m.growth, labels, row.Growth.Rate)

	if row.Projection.TimeToThreshold != nil {
		m.timeToThreshold.WithLabelValues(labels...).Set(row.Projection.TimeToThreshold.Seconds())
	} else {
		m.timeToThreshold.DeleteLabelValues(labels...)
	}

	pressureState := pressureStateNotEvaluated
	if row.Pressure.Evaluated {
		pressureState = string(row.Pressure.State)
		m.pressure.WithLabelValues(labels...).Set(float64(pressureSeverityValue(row.Pressure.State)))
	} else {
		// No band has an input, so there is no state. Publishing `normal` here
		// would assert a verdict the model never produced.
		m.pressure.DeleteLabelValues(labels...)
	}

	current := trackedResource{
		labels:   labels,
		capacity: resource.Bytes.State,
		pressure: pressureState,
	}
	changed := !held || previous.capacity != current.capacity ||
		previous.pressure != current.pressure || !sameLabels(previous.labels, current.labels)
	m.tracked[resource.Name] = current

	// The categorical aggregate is rebuilt only when a CATEGORY moved. Usage
	// and rates change every collection while states rarely do, so the steady
	// state costs nothing and a transition costs one pass over the tracked set.
	if changed {
		m.rebuildAggregatesLocked()
	}
}

// ForgetResource retracts every series a reclaimed resource published. A stream
// that no longer exists must stop reporting, or its last pressure reading pages
// someone forever.
func (m *storageObservabilityMetrics) ForgetResource(row natsclient.ResourceReport) {
	m.mu.Lock()
	defer m.mu.Unlock()

	tracked, held := m.tracked[row.Resource.Name]
	labels := resourceLabelValues(row.Resource)
	if held {
		labels = tracked.labels
		delete(m.tracked, row.Resource.Name)
	}
	m.deleteResourceSeries(labels)
	m.rebuildAggregatesLocked()
}

// ObserveAccount publishes the per-tier account comparison.
func (m *storageObservabilityMetrics) ObserveAccount(report natsclient.AccountReport) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.observeCollectedAt(report.CollectedAt)

	seen := make(map[natsclient.StorageTier]struct{}, len(report.Tiers))
	for _, comparison := range report.Tiers {
		tier := string(comparison.Tier)
		seen[comparison.Tier] = struct{}{}

		m.setOrDelete(m.accountLimit, []string{tier}, comparison.Limit.Limit)
		m.setOrDelete(m.accountUsed, []string{tier}, comparison.Limit.Usage)
		m.accountDeclared.WithLabelValues(tier).Set(float64(comparison.DeclaredBytes))

		// Closed enums, emitted in FULL. That is what makes "unbounded" and
		// "not applicable" visible as values an operator can alert on rather
		// than as the absence of a series.
		for _, state := range metricLimitStates {
			m.accountLimitState.WithLabelValues(tier, string(state)).Set(boolGauge(comparison.Limit.State == state))
		}
		for _, state := range metricOvercommitmentStates {
			m.accountOvercommitment.WithLabelValues(tier, string(state)).Set(boolGauge(comparison.State == state))
		}
	}

	// A tier the report no longer carries — the unknown tier, once every
	// resource is describable again — stops reporting rather than freezing.
	for tier := range m.accountTiers {
		if _, still := seen[tier]; still {
			continue
		}
		m.deleteAccountTierSeries(string(tier))
	}
	m.accountTiers = seen
}

// deleteResourceSeries retracts every per-resource series for one label tuple.
func (m *storageObservabilityMetrics) deleteResourceSeries(labels []string) {
	for _, vec := range []*prometheus.GaugeVec{
		m.used, m.limit, m.headroomBytes, m.headroomRatio,
		m.growth, m.timeToThreshold, m.pressure,
	} {
		vec.DeleteLabelValues(labels...)
	}
}

func (m *storageObservabilityMetrics) deleteAccountTierSeries(tier string) {
	m.accountLimit.DeleteLabelValues(tier)
	m.accountUsed.DeleteLabelValues(tier)
	m.accountDeclared.DeleteLabelValues(tier)
	for _, state := range metricLimitStates {
		m.accountLimitState.DeleteLabelValues(tier, string(state))
	}
	for _, state := range metricOvercommitmentStates {
		m.accountOvercommitment.DeleteLabelValues(tier, string(state))
	}
}

// rebuildAggregatesLocked re-emits the fixed-cardinality categorical counts.
// Every combination is written, including the zeros: a state that emptied must
// report zero rather than leave its last count standing.
func (m *storageObservabilityMetrics) rebuildAggregatesLocked() {
	capacityCounts := make(map[natsclient.StorageTier]map[natsclient.CapacityState]int, len(metricTiers))
	pressureCounts := make(map[natsclient.StorageTier]map[string]int, len(metricTiers))
	for _, tier := range metricTiers {
		capacityCounts[tier] = make(map[natsclient.CapacityState]int, len(metricCapacityStates))
		pressureCounts[tier] = make(map[string]int, len(metricPressureStates))
	}

	for _, tracked := range m.tracked {
		tier := natsclient.StorageTier(tracked.labels[3])
		if _, known := capacityCounts[tier]; !known {
			tier = natsclient.TierUnknown
		}
		capacityCounts[tier][tracked.capacity]++
		pressureCounts[tier][tracked.pressure]++
	}

	for _, tier := range metricTiers {
		for _, state := range metricCapacityStates {
			m.resourcesByCapacity.WithLabelValues(string(tier), string(state)).
				Set(float64(capacityCounts[tier][state]))
		}
		for _, state := range metricPressureStates {
			m.resourcesByPressure.WithLabelValues(string(tier), state).
				Set(float64(pressureCounts[tier][state]))
		}
	}
}

// setOrDelete publishes a value that may be absent, retracting the series when
// it is. Absence is the honest encoding: a zero would read as a measurement.
func (m *storageObservabilityMetrics) setOrDelete(
	vec *prometheus.GaugeVec, labels []string, read func() (int64, bool),
) {
	if value, ok := read(); ok {
		vec.WithLabelValues(labels...).Set(float64(value))
		return
	}
	vec.DeleteLabelValues(labels...)
}

func (m *storageObservabilityMetrics) setOrDeletePtr(
	vec *prometheus.GaugeVec, labels []string, value *int64,
) {
	if value != nil {
		vec.WithLabelValues(labels...).Set(float64(*value))
		return
	}
	vec.DeleteLabelValues(labels...)
}

func (m *storageObservabilityMetrics) setOrDeleteFloatPtr(
	vec *prometheus.GaugeVec, labels []string, value *float64,
) {
	if value != nil {
		vec.WithLabelValues(labels...).Set(*value)
		return
	}
	vec.DeleteLabelValues(labels...)
}

// setOrDelete for a float64 read accessor (Growth.Rate).
func (m *storageObservabilityMetrics) setOrDeleteRate(
	vec *prometheus.GaugeVec, labels []string, read func() (float64, bool),
) {
	if value, ok := read(); ok {
		vec.WithLabelValues(labels...).Set(value)
		return
	}
	vec.DeleteLabelValues(labels...)
}

// resourceLabelValues renders one resource's label tuple, in resourceLabels
// order.
//
// The owner label is NEVER blank. An empty label value cannot be grouped on and
// cannot say which of two different facts it means, so a resource with no
// declared owner reports its ATTRIBUTION state instead: "unattributed" is a
// bucket that escaped the descriptor catalog and is a finding, while
// "not-applicable" is a resource kind for which ownership is not a question.
func resourceLabelValues(resource natsclient.StorageResource) []string {
	owner := resource.Owner
	if resource.Attribution != natsclient.AttributionAttributed || owner == "" {
		owner = string(resource.Attribution)
	}
	if owner == "" {
		owner = string(natsclient.AttributionNotApplicable)
	}
	return []string{resource.Name, owner, string(resource.Kind), string(resource.Tier)}
}

func sameLabels(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// pressureSeverityValue is the numeric encoding of a pressure state, chosen so
// an alert reads `>= 2` rather than matching a label value that changes.
func pressureSeverityValue(state natsclient.PressureState) int {
	switch state {
	case natsclient.PressureWarning:
		return 1
	case natsclient.PressureHigh:
		return 2
	case natsclient.PressureCritical:
		return 3
	default:
		return 0
	}
}

func boolGauge(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
