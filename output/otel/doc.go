// Package otel exports SemStreams agent telemetry as OTLP/HTTP JSON spans and
// metrics. It is an explicit optional framework adapter under ADR-075, not
// part of the core component composition.
//
// The component consumes agent lifecycle and tool-result events, builds
// correlated spans and metrics, and periodically posts accepted batches to
// the configured collector's /v1/traces and /v1/metrics endpoints. Collector
// responses outside the 2xx range are export failures.
//
// # Configuration
//
// The proven transport is OTLP/HTTP only:
//
//	{
//	  "endpoint": "http://localhost:4318",
//	  "protocol": "http",
//	  "service_name": "semstreams",
//	  "service_version": "1.0.0",
//	  "export_traces": true,
//	  "export_metrics": true,
//	  "batch_timeout": "5s",
//	  "export_timeout": "30s",
//	  "sampling_rate": 1.0
//	}
//
// Unknown fields fail component construction. OTLP logs, configurable
// resource attributes, item-count batch limits, and a separate insecure
// switch are not implemented. Select HTTP or HTTPS with the endpoint URL.
//
// # Trace correlation
//
// Trace IDs are deterministically derived from loop IDs and span IDs from
// span keys, preserving correlation across the agent execution hierarchy.
//
// See https://opentelemetry.io/docs/specs/ for the OTLP specification.
package otel
