package readiness

import (
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// ProducerNames carries the two naming vocabularies this repo already uses for the
// same component.
//
// A struct rather than two string parameters: they differ only in punctuation
// ("graph-index" vs "graph_index"), so positional arguments are a silent-swap footgun
// whose failure mode is renaming EVERY metric the producer emits — with no error
// raised anywhere and dashboards simply going dark. That is the same reasoning
// graph.IndexStatusInputs carries, and the same reason this package refuses to DERIVE
// one spelling from the other: guessing wrong fails silently, so the caller states
// both.
type ProducerNames struct {
	// Service is the metrics-registry key, hyphenated: "graph-index".
	Service string
	// Subsystem is the Prometheus subsystem, underscored: "graph_index".
	Subsystem string
}

// Gauges is the readiness envelope's Prometheus projection, owned ONCE for every
// producer.
//
// It exists because the duplication it replaces did not merely permit a bug, it
// produced one twice: graph-index and graph-embedding each hand-rolled this set, and
// both independently omitted bootstrap_complete — the field the KV envelope treats as
// load-bearing and EvaluateReadinessGate consumes. A second producer shape (backlog)
// would have made four copies with the same drift mode.
//
// Set is the single projection site, so a field added to IndexStatusResponse cannot be
// wired in three producers and forgotten in a fourth.
type Gauges struct {
	names ProducerNames

	readiness         prometheus.Gauge
	lag               prometheus.Gauge
	bootstrapComplete prometheus.Gauge
	state             *prometheus.GaugeVec
	publishFailures   prometheus.Counter

	// Revision gauges are OPT-IN (WithRevisionGauges). The graph-index-readiness
	// spec states a backlog producer SHALL NOT be required to expose them and SHALL
	// NOT synthesize a value, because a fabricated revision is worse than an absent
	// one. Nil for backlog producers, and Set skips them.
	indexedRevision prometheus.Gauge
	targetRevision  prometheus.Gauge
}

// GaugeOption configures the set at construction.
type GaugeOption func(*Gauges)

// WithRevisionGauges adds indexed_revision and target_revision. Only revision-lag
// producers (graph-index, graph-embedding) may pass it: their Lag is in ENTITY_STATES
// revisions and the two fields are meaningful. A backlog producer must not.
func WithRevisionGauges() GaugeOption {
	return func(g *Gauges) {
		g.indexedRevision = prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Subsystem: g.names.Subsystem,
			Name:      "indexed_revision",
			Help:      "Low-water-of-pending watermark: every delivered ENTITY_STATES revision at or below this has been applied (ADR-066)",
		})
		g.targetRevision = prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Subsystem: g.names.Subsystem,
			Name:      "target_revision",
			Help:      "ENTITY_STATES stream LastSeq the producer must catch up to (ADR-066)",
		})
	}
}

// metricNamespace is the shared Prometheus namespace. Held as a constant so the
// name-pinning test and the constructors cannot disagree about it.
const metricNamespace = "semstreams"

// NewGauges builds the readiness gauge set for one producer.
//
// The emitted metric NAMES are an external contract — dashboards and operator alerts
// consume them, and the graph-index-readiness spec explicitly protects graph-index's
// published output. They are therefore reproduced here exactly as the two hand-rolled
// sets emitted them; MetricNames pins that, and a test compares it against the live
// registry.
func NewGauges(names ProducerNames, opts ...GaugeOption) *Gauges {
	g := &Gauges{
		names: names,
		readiness: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Subsystem: names.Subsystem,
			Name:      "readiness",
			Help:      "1 when the readiness envelope is Ready (producer caught up), else 0 (ADR-066)",
		}),
		lag: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Subsystem: names.Subsystem,
			Name:      "lag",
			Help:      "Outstanding work in the producer's OWN unit — ENTITY_STATES revisions for a revision-lag producer, messages for a backlog producer; 0 = caught up",
		}),
		bootstrapComplete: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Subsystem: names.Subsystem,
			Name:      "bootstrap_complete",
			Help:      "1 when the producer has finished its INITIAL build in this process lifetime, else 0. Distinguishes an index mid-format-cutover from one merely behind (gh#474, ADR-084)",
		}),
		state: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Subsystem: names.Subsystem,
			Name:      "readiness_state",
			Help:      "Readiness state one-hot (building|ready|degraded|reset_required): current state=1, others=0, so catching-up is distinguishable from broken",
		}, []string{"state"}),
		publishFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricNamespace,
			Subsystem: names.Subsystem,
			Name:      "status_publish_failures_total",
			Help:      "Readiness heartbeat writes to the GRAPH_STATUS KV key that failed (ADR-083): consumers go status_unknown and fail closed while this rises",
		}),
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// MetricNames returns the registry metric names this set emits, in registration order.
// Exported so a producer's test can pin its external contract without reaching into
// unexported fields.
func (g *Gauges) MetricNames() []string {
	names := []string{"readiness", "lag", "bootstrap_complete"}
	if g.indexedRevision != nil {
		names = append(names, "indexed_revision", "target_revision")
	}
	return append(names, "readiness_state", "status_publish_failures_total")
}

// Register wires every gauge into the metrics registry, falling back to the default
// Prometheus registerer when the registry is nil — the fallback each producer
// previously hand-rolled for tests.
func (g *Gauges) Register(registry *metric.MetricsRegistry) {
	if registry == nil {
		for _, c := range g.collectors() {
			_ = prometheus.DefaultRegisterer.Register(c)
		}
		return
	}
	_ = registry.RegisterGauge(g.names.Service, "readiness", g.readiness)
	_ = registry.RegisterGauge(g.names.Service, "lag", g.lag)
	_ = registry.RegisterGauge(g.names.Service, "bootstrap_complete", g.bootstrapComplete)
	if g.indexedRevision != nil {
		_ = registry.RegisterGauge(g.names.Service, "indexed_revision", g.indexedRevision)
		_ = registry.RegisterGauge(g.names.Service, "target_revision", g.targetRevision)
	}
	_ = registry.RegisterGaugeVec(g.names.Service, "readiness_state", g.state)
	_ = registry.RegisterCounter(g.names.Service, "status_publish_failures_total", g.publishFailures)
}

func (g *Gauges) collectors() []prometheus.Collector {
	out := []prometheus.Collector{g.readiness, g.lag, g.bootstrapComplete}
	if g.indexedRevision != nil {
		out = append(out, g.indexedRevision, g.targetRevision)
	}
	return append(out, g.state, g.publishFailures)
}

// Set projects one readiness envelope onto the gauges.
//
// THE SINGLE PROJECTION SITE. Every producer funnels through here, so adding a field
// to IndexStatusResponse means adding it once rather than remembering four places —
// which is exactly the omission that left bootstrap_complete unexposed on two
// independently-written producers.
//
// The state one-hot iterates graph.AllIndexStates rather than the states this producer
// happens to emit, so a new readiness state renders as an explicit 0 instead of a
// silently absent series.
func (g *Gauges) Set(resp graph.IndexStatusResponse) {
	g.readiness.Set(boolGauge(resp.Ready))
	g.lag.Set(float64(resp.Lag))
	g.bootstrapComplete.Set(boolGauge(resp.BootstrapComplete))

	// Skipped, not zeroed, for a backlog producer: publishing 0 would assert a
	// revision the producer does not have.
	if g.indexedRevision != nil {
		g.indexedRevision.Set(float64(resp.IndexedRevision))
		g.targetRevision.Set(float64(resp.TargetRevision))
	}

	for _, s := range graph.AllIndexStates {
		g.state.WithLabelValues(s).Set(boolGauge(s == resp.State))
	}
}

// RecordPublishFailure counts one failed GRAPH_STATUS heartbeat write (ADR-083).
func (g *Gauges) RecordPublishFailure() {
	g.publishFailures.Inc()
}

func boolGauge(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
