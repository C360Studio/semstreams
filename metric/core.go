package metric

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics contains all platform-level metrics (not domain-specific)
type Metrics struct {
	// Service metrics
	ServiceStatus      *prometheus.GaugeVec
	MessagesReceived   *prometheus.CounterVec
	MessagesProcessed  *prometheus.CounterVec
	MessagesPublished  *prometheus.CounterVec
	ProcessingDuration *prometheus.HistogramVec
	ErrorsTotal        *prometheus.CounterVec
	HealthCheckStatus  *prometheus.GaugeVec

	// NATS metrics
	NATSConnected      prometheus.Gauge
	NATSRTT            prometheus.Gauge
	NATSReconnects     prometheus.Counter
	NATSCircuitBreaker prometheus.Gauge
}

// NewMetrics creates a new Metrics instance with all platform metrics
func NewMetrics() *Metrics {
	return &Metrics{
		// Service metrics
		ServiceStatus: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "service",
				Name:      "status",
				Help:      "Service status (0=stopped, 1=starting, 2=running, 3=stopping, 4=failed)",
			},
			[]string{"service"},
		),

		MessagesReceived: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "messages",
				Name:      "received_total",
				Help:      "Total number of messages received",
			},
			[]string{"service", "type"},
		),

		MessagesProcessed: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "messages",
				Name:      "processed_total",
				Help:      "Total number of messages processed",
			},
			[]string{"service", "type", "status"},
		),

		MessagesPublished: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "messages",
				Name:      "published_total",
				Help:      "Total number of messages published",
			},
			[]string{"service", "subject"},
		),

		ProcessingDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "semstreams",
				Subsystem: "processing",
				Name:      "duration_seconds",
				Help:      "Message processing duration in seconds",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"service", "operation"},
		),

		ErrorsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "errors",
				Name:      "total",
				Help:      "Total number of errors",
			},
			[]string{"service", "type"},
		),

		HealthCheckStatus: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "health",
				Name:      "status",
				Help:      "Health check status (0=unhealthy, 1=healthy)",
			},
			[]string{"service"},
		),

		// NATS metrics
		NATSConnected: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "nats",
				Name:      "connected",
				Help:      "NATS connection status (0=disconnected, 1=connected)",
			},
		),

		NATSRTT: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "nats",
				Name:      "rtt_milliseconds",
				Help:      "NATS round-trip time in milliseconds",
			},
		),

		NATSReconnects: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "nats",
				Name:      "reconnects_total",
				Help:      "Total number of NATS reconnections",
			},
		),

		NATSCircuitBreaker: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "nats",
				Name:      "circuit_breaker",
				Help:      "NATS circuit breaker status (0=closed, 1=open, 2=half-open)",
			},
		),
	}
}

// RecordServiceStatus updates service status metric
func (c *Metrics) RecordServiceStatus(service string, status int) {
	c.ServiceStatus.WithLabelValues(service).Set(float64(status))
}

// RecordMessageReceived increments received message counter
func (c *Metrics) RecordMessageReceived(service, messageType string) {
	c.MessagesReceived.WithLabelValues(service, messageType).Inc()
}

// RecordMessageProcessed increments processed message counter
func (c *Metrics) RecordMessageProcessed(service, messageType, status string) {
	c.MessagesProcessed.WithLabelValues(service, messageType, status).Inc()
}

// RecordMessagePublished increments published message counter
func (c *Metrics) RecordMessagePublished(service, subject string) {
	c.MessagesPublished.WithLabelValues(service, subject).Inc()
}

// RecordProcessingDuration records processing time
func (c *Metrics) RecordProcessingDuration(service, operation string, duration time.Duration) {
	c.ProcessingDuration.WithLabelValues(service, operation).Observe(duration.Seconds())
}

// RecordError increments error counter
func (c *Metrics) RecordError(service, errorType string) {
	c.ErrorsTotal.WithLabelValues(service, errorType).Inc()
}

// RecordHealthStatus updates health check status
func (c *Metrics) RecordHealthStatus(service string, healthy bool) {
	value := 0.0
	if healthy {
		value = 1.0
	}
	c.HealthCheckStatus.WithLabelValues(service).Set(value)
}

// RecordNATSStatus updates NATS connection status
func (c *Metrics) RecordNATSStatus(connected bool) {
	value := 0.0
	if connected {
		value = 1.0
	}
	c.NATSConnected.Set(value)
}

// RecordNATSRTT updates NATS round-trip time
func (c *Metrics) RecordNATSRTT(rtt time.Duration) {
	c.NATSRTT.Set(float64(rtt.Milliseconds()))
}

// RecordNATSReconnect increments reconnection counter
func (c *Metrics) RecordNATSReconnect() {
	c.NATSReconnects.Inc()
}

// RecordCircuitBreakerState updates circuit breaker status
func (c *Metrics) RecordCircuitBreakerState(state int) {
	c.NATSCircuitBreaker.Set(float64(state))
}
