package http

import (
	"fmt"

	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// metrics holds prometheus counters for the http input component.
type metrics struct {
	requestsTotal     prometheus.Counter
	requestsFailed    prometheus.Counter
	requestsRetried   prometheus.Counter
	messagesParsed    prometheus.Counter
	messagesPublished prometheus.Counter
	parseErrors       prometheus.Counter
}

// newMetrics constructs and registers the input/http counters
// under the per-instance service name. Returns nil when the
// registry is nil — caller must nil-check.
func newMetrics(registry *metric.MetricsRegistry, name string) *metrics {
	if registry == nil {
		return nil
	}
	m := &metrics{
		requestsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "http_input",
			Name:      "requests_total",
			Help:      "Total HTTP requests issued (across retries).",
		}),
		requestsFailed: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "http_input",
			Name:      "requests_failed_total",
			Help:      "Total HTTP requests that ultimately failed after retries.",
		}),
		requestsRetried: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "http_input",
			Name:      "requests_retried_total",
			Help:      "Total retry attempts beyond the first request.",
		}),
		messagesParsed: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "http_input",
			Name:      "messages_parsed_total",
			Help:      "Total messages decoded from response bodies.",
		}),
		messagesPublished: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "http_input",
			Name:      "messages_published_total",
			Help:      "Total messages published to NATS.",
		}),
		parseErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "http_input",
			Name:      "parse_errors_total",
			Help:      "Total decode errors on response bodies.",
		}),
	}
	service := fmt.Sprintf("http_input_%s", name)
	registry.RegisterCounter(service, "requests", m.requestsTotal)
	registry.RegisterCounter(service, "requests_failed", m.requestsFailed)
	registry.RegisterCounter(service, "requests_retried", m.requestsRetried)
	registry.RegisterCounter(service, "messages_parsed", m.messagesParsed)
	registry.RegisterCounter(service, "messages_published", m.messagesPublished)
	registry.RegisterCounter(service, "parse_errors", m.parseErrors)
	return m
}
