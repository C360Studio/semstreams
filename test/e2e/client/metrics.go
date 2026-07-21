// Package client provides HTTP clients for SemStreams E2E tests
package client

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/c360studio/semstreams/test/e2e/config"
)

// MetricsClient fetches and parses Prometheus metrics from SemStreams
type MetricsClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewMetricsClient creates a new client for Prometheus metrics endpoints
func NewMetricsClient(baseURL string) *MetricsClient {
	return &MetricsClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: config.DefaultTestConfig.Timeout,
		},
	}
}

// Metric represents a single parsed Prometheus metric
type Metric struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels,omitempty"`
	Value  float64           `json:"value"`
}

// MetricsSnapshot represents a collection of metrics at a point in time
type MetricsSnapshot struct {
	Timestamp time.Time         `json:"timestamp"`
	Metrics   map[string]Metric `json:"metrics"`
	Raw       string            `json:"raw,omitempty"`
}

// MetricsReport is a structured summary of key metrics for comparison
type MetricsReport struct {
	Timestamp    time.Time           `json:"timestamp"`
	Duration     time.Duration       `json:"duration_ns"`
	DurationText string              `json:"duration"`
	Counters     map[string]float64  `json:"counters"`
	Gauges       map[string]float64  `json:"gauges"`
	Histograms   map[string]float64  `json:"histograms,omitempty"`
	Categories   map[string]Category `json:"categories"`
}

// Category groups related metrics
type Category struct {
	Name    string             `json:"name"`
	Metrics map[string]float64 `json:"metrics"`
}

// WaitOpts configures metric waiting behavior
type WaitOpts struct {
	Timeout      time.Duration // Max wait time (default 30s)
	PollInterval time.Duration // Check frequency (default 100ms)
	Comparator   string        // ">=", "==", ">", "<", "<=" (default ">=")
}

// DefaultWaitOpts returns sensible defaults for metric waiting
func DefaultWaitOpts() WaitOpts {
	return WaitOpts{
		Timeout:      30 * time.Second,
		PollInterval: 100 * time.Millisecond,
		Comparator:   ">=",
	}
}

// MetricsBaseline captures metrics at a point in time for comparison
type MetricsBaseline struct {
	Timestamp time.Time          `json:"timestamp"`
	Metrics   map[string]float64 `json:"metrics"`
}

// MetricsDiff represents the difference between two metric snapshots
type MetricsDiff struct {
	BaselineTime time.Time          `json:"baseline_time"`
	CurrentTime  time.Time          `json:"current_time"`
	Duration     time.Duration      `json:"duration"`
	Deltas       map[string]float64 `json:"deltas"`       // current - baseline
	RatePerSec   map[string]float64 `json:"rate_per_sec"` // delta / duration.Seconds()
}

// FetchRaw retrieves raw metrics text from the Prometheus endpoint
func (c *MetricsClient) FetchRaw(ctx context.Context) (string, error) {
	url := c.baseURL + "/metrics"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	return string(body), nil
}

// FetchSnapshot retrieves and parses all metrics into a snapshot
func (c *MetricsClient) FetchSnapshot(ctx context.Context) (*MetricsSnapshot, error) {
	raw, err := c.FetchRaw(ctx)
	if err != nil {
		return nil, err
	}

	snapshot := &MetricsSnapshot{
		Timestamp: time.Now(),
		Metrics:   make(map[string]Metric),
		Raw:       raw,
	}

	// Parse metrics line by line
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := scanner.Text()

		// Skip comments and empty lines
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}

		metric, err := parseLine(line)
		if err != nil {
			continue // Skip unparseable lines
		}

		// Use full metric key (name + labels) for uniqueness
		key := metric.Name
		if len(metric.Labels) > 0 {
			labelStr := formatLabels(metric.Labels)
			key = fmt.Sprintf("%s{%s}", metric.Name, labelStr)
		}
		snapshot.Metrics[key] = metric
	}

	return snapshot, nil
}

// FetchReport retrieves metrics and organizes them into a categorized report
func (c *MetricsClient) FetchReport(ctx context.Context, duration time.Duration) (*MetricsReport, error) {
	snapshot, err := c.FetchSnapshot(ctx)
	if err != nil {
		return nil, err
	}

	report := &MetricsReport{
		Timestamp:    snapshot.Timestamp,
		Duration:     duration,
		DurationText: duration.String(),
		Counters:     make(map[string]float64),
		Gauges:       make(map[string]float64),
		Histograms:   make(map[string]float64),
		Categories:   make(map[string]Category),
	}

	// Initialize categories
	categories := map[string]*Category{
		"indexmanager": {Name: "Index Manager", Metrics: make(map[string]float64)},
		"cache":        {Name: "Cache", Metrics: make(map[string]float64)},
		"filter":       {Name: "JSON Filter", Metrics: make(map[string]float64)},
		"embeddings":   {Name: "Embeddings", Metrics: make(map[string]float64)},
		"rules":        {Name: "Rules", Metrics: make(map[string]float64)},
		"graph":        {Name: "Graph", Metrics: make(map[string]float64)},
		"http":         {Name: "HTTP Gateway", Metrics: make(map[string]float64)},
	}

	// Categorize metrics
	for key, metric := range snapshot.Metrics {
		// Classify into counter/gauge based on name patterns
		if strings.HasSuffix(metric.Name, "_total") ||
			strings.HasSuffix(metric.Name, "_count") {
			report.Counters[key] = metric.Value
		} else if strings.Contains(metric.Name, "_bucket") ||
			strings.HasSuffix(metric.Name, "_sum") {
			report.Histograms[key] = metric.Value
		} else {
			report.Gauges[key] = metric.Value
		}

		// Categorize by prefix
		switch {
		case strings.HasPrefix(metric.Name, "indexmanager_"):
			if strings.Contains(metric.Name, "embedding") {
				categories["embeddings"].Metrics[metric.Name] = metric.Value
			} else {
				categories["indexmanager"].Metrics[metric.Name] = metric.Value
			}
		case strings.HasPrefix(metric.Name, "semstreams_cache_"):
			categories["cache"].Metrics[metric.Name] = metric.Value
		case strings.HasPrefix(metric.Name, "semstreams_json_filter_"):
			categories["filter"].Metrics[metric.Name] = metric.Value
		case strings.HasPrefix(metric.Name, "rule_"):
			categories["rules"].Metrics[metric.Name] = metric.Value
		case strings.HasPrefix(metric.Name, "graph_"):
			categories["graph"].Metrics[metric.Name] = metric.Value
		case strings.HasPrefix(metric.Name, "http_"):
			categories["http"].Metrics[metric.Name] = metric.Value
		}
	}

	// Convert to non-pointer map
	for name, cat := range categories {
		if len(cat.Metrics) > 0 {
			report.Categories[name] = *cat
		}
	}

	return report, nil
}

// GetMetricValue retrieves a specific metric value by name
func (c *MetricsClient) GetMetricValue(ctx context.Context, metricName string) (float64, error) {
	snapshot, err := c.FetchSnapshot(ctx)
	if err != nil {
		return 0, err
	}

	for _, metric := range snapshot.Metrics {
		if metric.Name == metricName {
			return metric.Value, nil
		}
	}

	return 0, fmt.Errorf("metric not found: %s", metricName)
}

// GetMetricsByPrefix retrieves all metrics matching a prefix
func (c *MetricsClient) GetMetricsByPrefix(ctx context.Context, prefix string) (map[string]float64, error) {
	snapshot, err := c.FetchSnapshot(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]float64)
	for key, metric := range snapshot.Metrics {
		if strings.HasPrefix(metric.Name, prefix) {
			result[key] = metric.Value
		}
	}

	return result, nil
}

// Health checks if the metrics endpoint is reachable
func (c *MetricsClient) Health(ctx context.Context) error {
	url := c.baseURL + "/metrics"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("metrics endpoint unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("metrics endpoint returned status %d", resp.StatusCode)
	}

	return nil
}

// parseLine parses a single Prometheus metric line
// Format: metric_name{label="value",...} value
var metricLineRegex = regexp.MustCompile(`^([a-zA-Z_:][a-zA-Z0-9_:]*)(\{([^}]*)\})?\s+([0-9eE.+-]+|NaN|Inf|-Inf)`)

func parseLine(line string) (Metric, error) {
	matches := metricLineRegex.FindStringSubmatch(line)
	if matches == nil {
		return Metric{}, fmt.Errorf("invalid metric line: %s", line)
	}

	metric := Metric{
		Name:   matches[1],
		Labels: make(map[string]string),
	}

	// Parse labels if present
	if matches[3] != "" {
		metric.Labels = parseLabels(matches[3])
	}

	// Parse value
	value, err := strconv.ParseFloat(matches[4], 64)
	if err != nil {
		return Metric{}, fmt.Errorf("parsing value: %w", err)
	}

	// Validate numeric sanity - log warning but don't fail
	// Some metrics legitimately have NaN/Inf values (e.g., division by zero)
	if math.IsNaN(value) || math.IsInf(value, 0) {
		slog.Debug("metric has special float value",
			"name", metric.Name,
			"raw_value", matches[4],
			"is_nan", math.IsNaN(value),
			"is_inf", math.IsInf(value, 0))
	}

	metric.Value = value

	return metric, nil
}

// parseLabels parses the label portion of a metric line
// Format: label1="value1",label2="value2"
func parseLabels(labelStr string) map[string]string {
	labels := make(map[string]string)
	if labelStr == "" {
		return labels
	}

	// Simple parser for label="value" pairs
	parts := strings.Split(labelStr, ",")
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			key := strings.TrimSpace(kv[0])
			value := strings.Trim(strings.TrimSpace(kv[1]), "\"")
			labels[key] = value
		}
	}

	return labels
}

// formatLabels formats labels back to Prometheus format
func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}

	parts := make([]string, 0, len(labels))
	for k, v := range labels {
		parts = append(parts, fmt.Sprintf("%s=\"%s\"", k, v))
	}
	return strings.Join(parts, ",")
}

// SumMetricsByName sums all metrics with the given name (across all label combinations)
func (c *MetricsClient) SumMetricsByName(ctx context.Context, metricName string) (float64, error) {
	snapshot, err := c.FetchSnapshot(ctx)
	if err != nil {
		return 0, err
	}

	var sum float64
	found := false
	for _, metric := range snapshot.Metrics {
		if metric.Name == metricName {
			sum += metric.Value
			found = true
		}
	}

	if !found {
		return 0, fmt.Errorf("metric not found: %s", metricName)
	}

	return sum, nil
}

// SubsystemMetric is the result of summing one metric within a Prometheus
// subsystem whose *presence* is itself evidence.
//
// E2E tiers deploy different component sets — the structural tier runs neither
// graph-embedding nor graph-clustering — so those series are legitimately absent
// from its scrape. A gate over such a metric must separate three states that a
// bare (float64, error) pair collapses into an indistinguishable 0:
//
//	SubsystemPresent=false             the component is not deployed, so zero is
//	                                   PROVEN rather than assumed.
//	SubsystemPresent=true, Found=false the component IS deployed but exports no
//	                                   such name — metric-name drift, returned
//	                                   as an error.
//	Found=true                         Sum is a real reading.
//
// The middle state is gh#615: e2e gates polled metric names that no production
// code ever exported, discarded the resulting "metric not found" into _, read 0,
// and therefore passed unconditionally for their entire existence.
type SubsystemMetric struct {
	// Name is the fully qualified metric name that was requested.
	Name string `json:"name"`

	// Subsystem is the name prefix owned by the producing component.
	Subsystem string `json:"subsystem"`

	// Sum is the total across all label combinations. Meaningful only when Found.
	Sum float64 `json:"sum"`

	// Found reports whether at least one series carried Name.
	Found bool `json:"found"`

	// SubsystemPresent reports whether any series carried Subsystem, i.e.
	// whether the producing component is deployed and scraped at all.
	SubsystemPresent bool `json:"subsystem_present"`

	// SeriesInSubsystem is how many series carried Subsystem, for diagnostics.
	SeriesInSubsystem int `json:"series_in_subsystem"`
}

// SumMetricInSubsystem sums metricName across labels while also reporting whether
// the owning subsystem was scraped at all.
//
// It returns an error when the subsystem is present but metricName is not: a
// deployed component that does not export the name being gated on means the gate
// is measuring nothing, which is strictly worse than a failing gate because it
// looks like a pass. Absence of the whole subsystem is NOT an error — it is the
// expected reading for a tier that does not deploy the component.
func (c *MetricsClient) SumMetricInSubsystem(
	ctx context.Context,
	subsystem, metricName string,
) (SubsystemMetric, error) {
	if !strings.HasPrefix(metricName, subsystem) {
		return SubsystemMetric{}, fmt.Errorf(
			"metric %q does not belong to subsystem %q: the caller's prefix and name disagree",
			metricName, subsystem)
	}

	snapshot, err := c.FetchSnapshot(ctx)
	if err != nil {
		return SubsystemMetric{}, fmt.Errorf("fetching metrics snapshot: %w", err)
	}

	return SumMetricInSnapshot(snapshot, subsystem, metricName)
}

// SumMetricInSnapshot is SumMetricInSubsystem against an already-fetched
// snapshot, for callers that must read several counters as of the SAME scrape.
//
// Counters that are arithmetically related to one another (dedup_hits_total and
// embeddings_generated_total, where the former is a subset of the latter) cannot
// be read from independent scrapes without admitting skew: the pipeline can
// increment between the two HTTP calls and produce a difference that is negative
// or otherwise impossible. Deriving one from the other is only sound within a
// single snapshot.
func SumMetricInSnapshot(
	snapshot *MetricsSnapshot,
	subsystem, metricName string,
) (SubsystemMetric, error) {
	if snapshot == nil {
		return SubsystemMetric{}, fmt.Errorf("nil metrics snapshot for %q", metricName)
	}
	if !strings.HasPrefix(metricName, subsystem) {
		return SubsystemMetric{}, fmt.Errorf(
			"metric %q does not belong to subsystem %q: the caller's prefix and name disagree",
			metricName, subsystem)
	}

	out := SubsystemMetric{Name: metricName, Subsystem: subsystem}
	for _, metric := range snapshot.Metrics {
		if strings.HasPrefix(metric.Name, subsystem) {
			out.SubsystemPresent = true
			out.SeriesInSubsystem++
		}
		if metric.Name == metricName {
			out.Sum += metric.Value
			out.Found = true
		}
	}

	if out.SubsystemPresent && !out.Found {
		return out, fmt.Errorf(
			"metric %q is not exported even though subsystem %q is scraped (%d other series present): "+
				"the metric name is misspelled or was renamed in production code",
			metricName, subsystem, out.SeriesInSubsystem)
	}

	return out, nil
}

// CaptureBaseline takes a snapshot of all counter metrics for later comparison
func (c *MetricsClient) CaptureBaseline(ctx context.Context) (*MetricsBaseline, error) {
	snapshot, err := c.FetchSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("capturing baseline: %w", err)
	}

	baseline := &MetricsBaseline{
		Timestamp: snapshot.Timestamp,
		Metrics:   make(map[string]float64),
	}

	// Aggregate metrics by name (sum across labels)
	aggregated := make(map[string]float64)
	for _, metric := range snapshot.Metrics {
		aggregated[metric.Name] += metric.Value
	}

	baseline.Metrics = aggregated
	return baseline, nil
}

// CompareToBaseline compares current metrics to a baseline and returns deltas
func (c *MetricsClient) CompareToBaseline(ctx context.Context, baseline *MetricsBaseline) (*MetricsDiff, error) {
	if baseline == nil {
		return nil, fmt.Errorf("baseline cannot be nil")
	}

	snapshot, err := c.FetchSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching current metrics: %w", err)
	}

	// Aggregate current metrics by name
	current := make(map[string]float64)
	for _, metric := range snapshot.Metrics {
		current[metric.Name] += metric.Value
	}

	diff := &MetricsDiff{
		BaselineTime: baseline.Timestamp,
		CurrentTime:  snapshot.Timestamp,
		Duration:     snapshot.Timestamp.Sub(baseline.Timestamp),
		Deltas:       make(map[string]float64),
		RatePerSec:   make(map[string]float64),
	}

	// Calculate deltas for all metrics in baseline
	durationSec := diff.Duration.Seconds()
	for name, baselineValue := range baseline.Metrics {
		currentValue := current[name]
		delta := currentValue - baselineValue
		diff.Deltas[name] = delta

		if durationSec > 0 {
			diff.RatePerSec[name] = delta / durationSec
		}
	}

	// Also include any new metrics not in baseline
	for name, currentValue := range current {
		if _, exists := baseline.Metrics[name]; !exists {
			diff.Deltas[name] = currentValue
			if durationSec > 0 {
				diff.RatePerSec[name] = currentValue / durationSec
			}
		}
	}

	return diff, nil
}

// WaitForMetric polls until a metric reaches the expected value or timeout
func (c *MetricsClient) WaitForMetric(ctx context.Context, metricName string, expected float64, opts WaitOpts) error {
	// Apply defaults
	if opts.Timeout == 0 {
		opts.Timeout = DefaultWaitOpts().Timeout
	}
	if opts.PollInterval == 0 {
		opts.PollInterval = DefaultWaitOpts().PollInterval
	}
	if opts.Comparator == "" {
		opts.Comparator = DefaultWaitOpts().Comparator
	}

	deadline := time.Now().Add(opts.Timeout)
	ticker := time.NewTicker(opts.PollInterval)
	defer ticker.Stop()

	var lastValue float64
	var lastErr error

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for %s: %w", metricName, ctx.Err())
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for %s %s %.2f (last value: %.2f, last error: %v)",
					metricName, opts.Comparator, expected, lastValue, lastErr)
			}

			value, err := c.SumMetricsByName(ctx, metricName)
			if err != nil {
				lastErr = err
				continue // Metric may not exist yet, keep trying
			}

			lastValue = value
			lastErr = nil

			if compareValue(value, expected, opts.Comparator) {
				return nil // Success!
			}
		}
	}
}

// WaitForMetricDelta waits for a metric to increase by at least delta from baseline
func (c *MetricsClient) WaitForMetricDelta(ctx context.Context, metricName string, baseline, delta float64, opts WaitOpts) error {
	expected := baseline + delta
	return c.WaitForMetric(ctx, metricName, expected, opts)
}

// WaitForMetricChange waits for any change in a metric from its baseline value
func (c *MetricsClient) WaitForMetricChange(ctx context.Context, metricName string, baseline float64, opts WaitOpts) error {
	// Apply defaults
	if opts.Timeout == 0 {
		opts.Timeout = DefaultWaitOpts().Timeout
	}
	if opts.PollInterval == 0 {
		opts.PollInterval = DefaultWaitOpts().PollInterval
	}

	deadline := time.Now().Add(opts.Timeout)
	ticker := time.NewTicker(opts.PollInterval)
	defer ticker.Stop()

	var lastValue float64
	var lastErr error

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for %s to change: %w", metricName, ctx.Err())
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for %s to change from %.2f (last value: %.2f, last error: %v)",
					metricName, baseline, lastValue, lastErr)
			}

			value, err := c.SumMetricsByName(ctx, metricName)
			if err != nil {
				lastErr = err
				continue
			}

			lastValue = value
			lastErr = nil

			if value != baseline {
				return nil // Any change is success
			}
		}
	}
}

// compareValue compares actual to expected using the given comparator
func compareValue(actual, expected float64, comparator string) bool {
	switch comparator {
	case ">=":
		return actual >= expected
	case ">":
		return actual > expected
	case "==":
		return actual == expected
	case "<=":
		return actual <= expected
	case "<":
		return actual < expected
	default:
		return actual >= expected // Default to >=
	}
}

// GetMetricByLabels retrieves metrics matching the given name and optional label filters
func (c *MetricsClient) GetMetricByLabels(ctx context.Context, metricName string, labelFilters map[string]string) ([]Metric, error) {
	snapshot, err := c.FetchSnapshot(ctx)
	if err != nil {
		return nil, err
	}

	var results []Metric
	for _, metric := range snapshot.Metrics {
		if metric.Name != metricName {
			continue
		}

		// Check label filters if provided
		if labelFilters != nil {
			matches := true
			for key, value := range labelFilters {
				if metric.Labels[key] != value {
					matches = false
					break
				}
			}
			if !matches {
				continue
			}
		}

		results = append(results, metric)
	}

	return results, nil
}

// RuleMetrics holds rule engine metrics for E2E tests.
// These map to the semstreams_rule_* Prometheus metrics from the rule processor.
type RuleMetrics struct {
	// Rule evaluation metrics
	Evaluations float64 `json:"evaluations"` // Total rule evaluations
	Firings     float64 `json:"firings"`     // Rules that fired (conditions met)

	// Action metrics
	ActionsDispatched float64 `json:"actions_dispatched"` // Total actions dispatched

	// Execution lifecycle metrics (legacy — kept for backwards compat, always 0)
	ExecutionsCreated   float64 `json:"executions_created"`
	ExecutionsCompleted float64 `json:"executions_completed"`
	ExecutionsFailed    float64 `json:"executions_failed"`

	// Callback metrics (legacy — kept for backwards compat, always 0)
	CallbacksReceived float64 `json:"callbacks_received"`
}

// ExtractRuleMetrics gets all rule engine metrics in a single call.
// This enables consistent rule validation across E2E scenarios.
func (c *MetricsClient) ExtractRuleMetrics(ctx context.Context) (*RuleMetrics, error) {
	metrics := &RuleMetrics{}

	// Rule processor metrics (semstreams_rule_*)
	evaluations, err := c.SumMetricsByName(ctx, "semstreams_rule_evaluations_total")
	if err == nil {
		metrics.Evaluations = evaluations
	}

	firings, err := c.SumMetricsByName(ctx, "semstreams_rule_triggers_total")
	if err == nil {
		metrics.Firings = firings
	}

	actions, err := c.SumMetricsByName(ctx, "semstreams_rule_events_published_total")
	if err == nil {
		metrics.ActionsDispatched = actions
	}

	return metrics, nil
}
