package service

// Runtime metrics proxy endpoint for Flow Builder UI.
//
// This file implements the GET /flowbuilder/flows/{id}/runtime/metrics endpoint
// which provides component-level metrics (throughput, error rates, queue depth)
// with graceful degradation across three tiers:
//
//   Tier 1: Query Prometheus HTTP API for computed rates
//   Tier 2: Parse raw /metrics endpoint for counter values
//   Tier 3: Return health status only (metrics unavailable)
//
// The endpoint is designed to support the Runtime Visualization Panel in the UI
// with sub-500ms response times and 2-second polling intervals.
//
// Configuration:
//   - PrometheusURL: Base URL for Prometheus (default: http://localhost:9090)
//   - FallbackToRaw: Enable tier 2 fallback (default: true)
//
// Response format includes:
//   - timestamp: Request timestamp
//   - prometheus_available: Whether Prometheus queries succeeded
//   - components: Array of component metrics with computed rates or raw counters

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/c360/semstreams/errors"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// RuntimeMetricsResponse represents the JSON response for runtime metrics
type RuntimeMetricsResponse struct {
	Timestamp           time.Time         `json:"timestamp"`
	PrometheusAvailable bool              `json:"prometheus_available"`
	Components          []ComponentMetric `json:"components"`
}

// ComponentMetric represents metrics for a single component
type ComponentMetric struct {
	Name        string             `json:"name"`
	Type        string             `json:"type"`
	Status      string             `json:"status"`
	Throughput  *float64           `json:"throughput"`   // msgs/sec, null if unavailable
	ErrorRate   *float64           `json:"error_rate"`   // errors/sec, null if unavailable
	QueueDepth  *float64           `json:"queue_depth"`  // current queue depth, null if unavailable
	RawCounters *map[string]uint64 `json:"raw_counters"` // only present when Prometheus unavailable
}

// componentTypeToMetricPrefix maps component types to their Prometheus metric prefixes
var componentTypeToMetricPrefix = map[string]string{
	"input":     "input",
	"processor": "processor",
	"output":    "output",
	"storage":   "storage",
	"gateway":   "gateway",
}

// componentNameRegex defines valid characters for component names in PromQL queries
// Only alphanumeric, underscore, and hyphen are allowed to prevent injection
var componentNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// sanitizeComponentName ensures component names are safe for use in PromQL queries
// by replacing any invalid characters with underscores to prevent PromQL injection attacks.
// Valid characters are: alphanumeric, underscore, hyphen
func sanitizeComponentName(name string) string {
	if componentNameRegex.MatchString(name) {
		return name
	}
	// Replace invalid characters with underscore
	return regexp.MustCompile(`[^a-zA-Z0-9_-]`).ReplaceAllString(name, "_")
}

// handleRuntimeMetrics handles GET /flows/{id}/runtime/metrics
// Implements three-tier fallback:
// 1. Query Prometheus API for computed rates
// 2. Parse raw metrics endpoint for counters
// 3. Return health status only
func (fs *FlowService) handleRuntimeMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	flowID := r.PathValue("id")

	// Get the flow definition to know what components to query
	flow, err := fs.flowStore.Get(ctx, flowID)
	if err != nil {
		fs.logger.Error("Failed to get flow for metrics", "flow_id", flowID, "error", err)
		// Return structured error response with prometheus_available flag
		errorResponse := RuntimeMetricsResponse{
			Timestamp:           time.Now(),
			PrometheusAvailable: false,
			Components:          []ComponentMetric{},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(errorResponse)
		return
	}

	// Build component list from flow nodes
	components := make([]componentInfo, 0, len(flow.Nodes))
	for _, node := range flow.Nodes {
		components = append(components, componentInfo{
			Name: node.Name,
			Type: node.Type,
		})
	}

	// Try tier 1: Prometheus API
	response, err := fs.queryPrometheusMetrics(ctx, components)
	if err == nil {
		// Success! Return computed rates
		response.Timestamp = time.Now()
		response.PrometheusAvailable = true
		fs.writeJSON(w, response)
		return
	}

	fs.logger.Warn("Prometheus API unavailable, trying raw metrics fallback", "error", err)

	// Try tier 2: Raw metrics endpoint
	if fs.config.FallbackToRaw {
		response, err := fs.parseRawMetrics(ctx, components)
		if err == nil {
			response.Timestamp = time.Now()
			response.PrometheusAvailable = false
			fs.writeJSON(w, response)
			return
		}

		fs.logger.Warn("Raw metrics unavailable, falling back to health only", "error", err)
	}

	// Tier 3: Health status only
	response = fs.buildHealthOnlyResponse(components)
	response.Timestamp = time.Now()
	response.PrometheusAvailable = false
	fs.writeJSON(w, response)
}

// componentInfo holds basic component information
type componentInfo struct {
	Name string
	Type string
}

// queryPrometheusMetrics queries Prometheus HTTP API for computed metrics
func (fs *FlowService) queryPrometheusMetrics(ctx context.Context, components []componentInfo) (*RuntimeMetricsResponse, error) {
	// Create Prometheus API client with timeout
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	client, err := api.NewClient(api.Config{
		Address: fs.config.PrometheusURL,
	})
	if err != nil {
		return nil, errors.WrapTransient(err, "FlowService", "queryPrometheusMetrics", "create prometheus client")
	}

	v1api := v1.NewAPI(client)

	// Query metrics for each component
	componentMetrics := make([]ComponentMetric, 0, len(components))

	for _, comp := range components {
		metric := ComponentMetric{
			Name:   comp.Name,
			Type:   comp.Type,
			Status: "unknown", // Will be updated if we add health status integration
		}

		// Get metric prefix based on component type
		metricPrefix, ok := componentTypeToMetricPrefix[comp.Type]
		if !ok {
			// Unknown type, try to infer from factory name
			metricPrefix = inferMetricPrefix(comp.Type)
		}

		// Sanitize component name to prevent PromQL injection
		safeName := sanitizeComponentName(comp.Name)

		// Query throughput: rate(semstreams_{type}_messages_published_total{component="name"}[1m])
		throughputQuery := fmt.Sprintf(
			`rate(semstreams_%s_messages_published_total{component="%s"}[1m])`,
			metricPrefix, safeName,
		)

		if value, err := fs.queryPrometheusSingle(ctx, v1api, throughputQuery); err == nil {
			metric.Throughput = &value
		}

		// Query error rate: rate(semstreams_{type}_messages_dropped_total{component="name"}[1m])
		errorQuery := fmt.Sprintf(
			`rate(semstreams_%s_messages_dropped_total{component="%s"}[1m])`,
			metricPrefix, safeName,
		)

		if value, err := fs.queryPrometheusSingle(ctx, v1api, errorQuery); err == nil {
			metric.ErrorRate = &value
		}

		// Query queue depth: semstreams_{type}_queue_depth{component="name"}
		queueQuery := fmt.Sprintf(
			`semstreams_%s_queue_depth{component="%s"}`,
			metricPrefix, safeName,
		)

		if value, err := fs.queryPrometheusSingle(ctx, v1api, queueQuery); err == nil {
			metric.QueueDepth = &value
		}

		componentMetrics = append(componentMetrics, metric)
	}

	return &RuntimeMetricsResponse{
		Components: componentMetrics,
	}, nil
}

// queryPrometheusSingle executes a single PromQL query and returns the scalar result
func (fs *FlowService) queryPrometheusSingle(ctx context.Context, api v1.API, query string) (float64, error) {
	result, warnings, err := api.Query(ctx, query, time.Now())
	if err != nil {
		return 0, errors.WrapTransient(err, "FlowService", "queryPrometheusSingle", "execute prometheus query")
	}

	if len(warnings) > 0 {
		fs.logger.Debug("Prometheus query warnings", "query", query, "warnings", warnings)
	}

	// Parse result
	switch result.Type() {
	case model.ValVector:
		vector := result.(model.Vector)
		if len(vector) > 0 {
			return float64(vector[0].Value), nil
		}
		return 0, errors.WrapTransient(fmt.Errorf("no data returned for query"), "FlowService", "queryPrometheusSingle", "parse query result")

	case model.ValScalar:
		scalar := result.(*model.Scalar)
		return float64(scalar.Value), nil

	default:
		return 0, errors.WrapInvalid(fmt.Errorf("unexpected result type: %s", result.Type()), "FlowService", "queryPrometheusSingle", "parse query result")
	}
}

// parseRawMetrics parses raw Prometheus metrics from /metrics endpoint
func (fs *FlowService) parseRawMetrics(ctx context.Context, components []componentInfo) (*RuntimeMetricsResponse, error) {
	// Construct raw metrics URL (typically same host, different path)
	metricsURL := strings.TrimSuffix(fs.config.PrometheusURL, "/") + "/metrics"

	// Create request with timeout
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metricsURL, nil)
	if err != nil {
		return nil, errors.WrapInvalid(err, "FlowService", "parseRawMetrics", "create http request")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.WrapTransient(err, "FlowService", "parseRawMetrics", "fetch raw metrics")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.WrapTransient(
			fmt.Errorf("unexpected status code: %d", resp.StatusCode),
			"FlowService", "parseRawMetrics", "check http status")
	}

	// Parse Prometheus text format
	parser := expfmt.TextParser{}
	metricFamilies, err := parser.TextToMetricFamilies(resp.Body)
	if err != nil {
		return nil, errors.WrapTransient(err, "FlowService", "parseRawMetrics", "parse prometheus text format")
	}

	// Extract counters for each component
	componentMetrics := make([]ComponentMetric, 0, len(components))

	for _, comp := range components {
		metric := ComponentMetric{
			Name:   comp.Name,
			Type:   comp.Type,
			Status: "unknown",
		}

		// Sanitize component name before using in metric extraction
		safeName := sanitizeComponentName(comp.Name)

		// Extract raw counters for this component
		counters := fs.extractComponentCounters(metricFamilies, safeName)
		if len(counters) > 0 {
			metric.RawCounters = &counters
		}

		componentMetrics = append(componentMetrics, metric)
	}

	return &RuntimeMetricsResponse{
		Components: componentMetrics,
	}, nil
}

// extractComponentCounters extracts raw counter values for a component from metric families
func (fs *FlowService) extractComponentCounters(families map[string]*dto.MetricFamily, componentName string) map[string]uint64 {
	counters := make(map[string]uint64)

	// Look for metrics with component label matching our component name
	for _, family := range families {
		// Only interested in counters
		if family.GetType() != dto.MetricType_COUNTER {
			continue
		}

		// Check each metric in the family
		for _, m := range family.GetMetric() {
			// Look for component label
			for _, label := range m.GetLabel() {
				if label.GetName() == "component" && label.GetValue() == componentName {
					// Extract counter value
					if counter := m.GetCounter(); counter != nil {
						// Use the metric family name as the key
						metricName := family.GetName()
						counters[metricName] = uint64(counter.GetValue())
					}
					break
				}
			}
		}
	}

	return counters
}

// buildHealthOnlyResponse builds a response with only health status
func (fs *FlowService) buildHealthOnlyResponse(components []componentInfo) *RuntimeMetricsResponse {
	componentMetrics := make([]ComponentMetric, 0, len(components))

	for _, comp := range components {
		componentMetrics = append(componentMetrics, ComponentMetric{
			Name:   comp.Name,
			Type:   comp.Type,
			Status: "unknown", // TODO: integrate with component manager health status
		})
	}

	return &RuntimeMetricsResponse{
		Components: componentMetrics,
	}
}

// inferMetricPrefix attempts to infer the metric prefix from a factory name
// E.g., "udp" -> "input", "graph-processor" -> "processor", "file-output" -> "output"
func inferMetricPrefix(factoryName string) string {
	lower := strings.ToLower(factoryName)

	// Specific input types
	if lower == "udp" || lower == "websocket" || lower == "tcp" || lower == "mqtt" {
		return "input"
	}

	// Common patterns
	if strings.Contains(lower, "input") || strings.Contains(lower, "source") {
		return "input"
	}
	if strings.Contains(lower, "processor") || strings.Contains(lower, "transform") {
		return "processor"
	}
	if strings.Contains(lower, "output") || strings.Contains(lower, "sink") {
		return "output"
	}
	if strings.Contains(lower, "storage") || strings.Contains(lower, "store") {
		return "storage"
	}
	if strings.Contains(lower, "gateway") {
		return "gateway"
	}

	// Default to the factory name itself
	return lower
}

// healthOnlyLog logs when falling back to health-only mode
func (fs *FlowService) healthOnlyLog(logger *slog.Logger, components []componentInfo) {
	logger.Info("Returning health-only metrics response",
		"component_count", len(components),
		"prometheus_available", false)
}
