package natsclient

import (
	"context"
	"sync"
	"time"

	"github.com/c360/semstreams/metric"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
)

// jetstreamMetrics holds Prometheus metrics for JetStream operations.
// Tracks only streams and consumers that are created/accessed through this client.
type jetstreamMetrics struct {
	// Stream state metrics
	streamMessages *prometheus.GaugeVec // Current message count by stream
	streamBytes    *prometheus.GaugeVec // Storage bytes by stream
	streamState    *prometheus.GaugeVec // Stream state (1=active, 0=inactive)

	// Consumer state metrics
	consumerPending     *prometheus.GaugeVec   // Pending messages by consumer
	consumerDelivered   *prometheus.CounterVec // Total delivered by consumer
	consumerAcked       *prometheus.CounterVec // Total acked by consumer
	consumerRedelivered *prometheus.CounterVec // Total redelivered by consumer

	// Operation errors
	errors *prometheus.CounterVec // JetStream operation errors

	// Tracked resources (only what we create/use)
	mu        sync.RWMutex
	streams   map[string]jetstream.Stream   // Streams we've created/accessed
	consumers map[string]jetstream.Consumer // Consumers we've created
}

// newJetStreamMetrics creates and registers JetStream metrics with the provided registry.
func newJetStreamMetrics(registry *metric.MetricsRegistry) (*jetstreamMetrics, error) {
	if registry == nil {
		return nil, nil // Metrics disabled
	}

	m := &jetstreamMetrics{
		// Stream metrics
		streamMessages: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "jetstream",
			Name:      "stream_messages",
			Help:      "Current number of messages in stream",
		}, []string{"stream"}),

		streamBytes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "jetstream",
			Name:      "stream_bytes",
			Help:      "Storage bytes used by stream",
		}, []string{"stream"}),

		streamState: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "jetstream",
			Name:      "stream_state",
			Help:      "Stream state (1=active, 0=inactive)",
		}, []string{"stream"}),

		// Consumer metrics
		consumerPending: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "jetstream",
			Name:      "consumer_pending_messages",
			Help:      "Number of pending messages for consumer",
		}, []string{"stream", "consumer"}),

		consumerDelivered: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "jetstream",
			Name:      "consumer_delivered_total",
			Help:      "Total messages delivered to consumer",
		}, []string{"stream", "consumer"}),

		consumerAcked: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "jetstream",
			Name:      "consumer_acked_total",
			Help:      "Total messages acknowledged by consumer",
		}, []string{"stream", "consumer"}),

		consumerRedelivered: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "jetstream",
			Name:      "consumer_redelivered_total",
			Help:      "Total messages redelivered to consumer",
		}, []string{"stream", "consumer"}),

		// Error counters
		errors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "jetstream",
			Name:      "operation_errors_total",
			Help:      "Total number of JetStream operation errors",
		}, []string{"operation"}),

		streams:   make(map[string]jetstream.Stream),
		consumers: make(map[string]jetstream.Consumer),
	}

	// Register all metrics
	if err := registry.RegisterGaugeVec("jetstream", "stream_messages", m.streamMessages); err != nil {
		return nil, err
	}
	if err := registry.RegisterGaugeVec("jetstream", "stream_bytes", m.streamBytes); err != nil {
		return nil, err
	}
	if err := registry.RegisterGaugeVec("jetstream", "stream_state", m.streamState); err != nil {
		return nil, err
	}
	if err := registry.RegisterGaugeVec("jetstream", "consumer_pending", m.consumerPending); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounterVec("jetstream", "consumer_delivered", m.consumerDelivered); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounterVec("jetstream", "consumer_acked", m.consumerAcked); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounterVec("jetstream", "consumer_redelivered", m.consumerRedelivered); err != nil {
		return nil, err
	}
	if err := registry.RegisterCounterVec("jetstream", "errors", m.errors); err != nil {
		return nil, err
	}

	return m, nil
}

// trackStream adds a stream to the tracking list for metrics collection.
func (m *jetstreamMetrics) trackStream(name string, stream jetstream.Stream) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.streams[name] = stream
	m.streamState.WithLabelValues(name).Set(1) // Mark as active
}

// trackConsumer adds a consumer to the tracking list for metrics collection.
func (m *jetstreamMetrics) trackConsumer(streamName, consumerName string, consumer jetstream.Consumer) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := streamName + ":" + consumerName
	m.consumers[key] = consumer
}

// recordError records a JetStream operation error.
func (m *jetstreamMetrics) recordError(operation string) {
	if m != nil {
		m.errors.WithLabelValues(operation).Inc()
	}
}

// updateStats updates all tracked stream and consumer statistics.
// Called periodically by the background poller. Fails gracefully if stats unavailable.
func (m *jetstreamMetrics) updateStats(ctx context.Context) {
	if m == nil {
		return
	}

	m.mu.RLock()
	streams := make(map[string]jetstream.Stream, len(m.streams))
	consumers := make(map[string]jetstream.Consumer, len(m.consumers))
	for k, v := range m.streams {
		streams[k] = v
	}
	for k, v := range m.consumers {
		consumers[k] = v
	}
	m.mu.RUnlock()

	// Update stream stats
	for name, stream := range streams {
		info, err := stream.Info(ctx)
		if err != nil {
			// Stream might be deleted or unavailable - fail gracefully
			m.streamState.WithLabelValues(name).Set(0)
			continue
		}

		m.streamMessages.WithLabelValues(name).Set(float64(info.State.Msgs))
		m.streamBytes.WithLabelValues(name).Set(float64(info.State.Bytes))
		m.streamState.WithLabelValues(name).Set(1)
	}

	// Update consumer stats
	for _, consumer := range consumers {
		info, err := consumer.Info(ctx)
		if err != nil {
			// Consumer might be deleted or unavailable - fail gracefully
			continue
		}

		streamName := info.Stream
		consumerName := info.Name

		m.consumerPending.WithLabelValues(streamName, consumerName).Set(float64(info.NumPending))
		m.consumerDelivered.WithLabelValues(streamName, consumerName).Add(float64(info.Delivered.Stream))
		m.consumerAcked.WithLabelValues(streamName, consumerName).Add(float64(info.AckFloor.Stream))
		m.consumerRedelivered.WithLabelValues(streamName, consumerName).Add(float64(info.NumRedelivered))
	}
}

// startPoller starts a background goroutine that polls JetStream stats periodically.
// Returns a cancel function to stop the poller.
func (m *jetstreamMetrics) startPoller(ctx context.Context, interval time.Duration) context.CancelFunc {
	if m == nil {
		return func() {} // No-op if metrics disabled
	}

	ctx, cancel := context.WithCancel(ctx)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Update stats, but don't let errors crash the poller
				m.updateStats(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()

	return cancel
}
