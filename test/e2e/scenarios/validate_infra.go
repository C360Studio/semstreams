// Package scenarios provides E2E test scenarios for SemStreams semantic processing
package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/c360studio/semstreams/test/e2e/client"
)

// Infrastructure validation functions for tiered E2E tests

// executeVerifyComponents checks that all tiered test components exist
func (s *TieredScenario) executeVerifyComponents(ctx context.Context, result *Result) error {
	components, err := s.client.GetComponents(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to get components: %v", err))
		return fmt.Errorf("component verification failed: %w", err)
	}

	var allRequired []string

	// Every tier exercises the rule processor. Requiring the instance here is
	// load-bearing: a config-validation failure used to omit the component while
	// the remainder of the semantic scenario still reported success.
	if s.config.Variant == "structural" {
		// Components for structural testing with rule processor
		// Graph components are now modular: graph-ingest, graph-index, graph-gateway
		// Note: "rule" is the instance name (config key), "rule-processor" is the factory name
		allRequired = []string{"udp", "iot_sensor", "rule", "graph-ingest", "graph-index", "graph-gateway", "file"}
	} else {
		// Full components for statistical/semantic tiers
		// Input components
		inputComponents := []string{"udp"}
		// Domain processors (document_processor, iot_sensor handle domain-specific data)
		domainProcessors := []string{"document_processor", "iot_sensor"}
		// Graph components (modular: ingest, index, gateway + optional embedding/clustering)
		graphComponents := []string{"graph-ingest", "graph-index", "graph-gateway"}
		// Output/storage components
		outputComponents := []string{"file", "objectstore"}

		allRequired = append(inputComponents, domainProcessors...)
		allRequired = append(allRequired, graphComponents...)
		allRequired = append(allRequired, outputComponents...)
		allRequired = append(allRequired, "rule")
	}

	foundComponents := make(map[string]bool)
	for _, comp := range components {
		foundComponents[comp.Name] = true
	}

	missingComponents := []string{}
	for _, required := range allRequired {
		if !foundComponents[required] {
			missingComponents = append(missingComponents, required)
		}
	}

	if len(missingComponents) > 0 {
		result.Errors = append(result.Errors,
			fmt.Sprintf("Missing components: %v", missingComponents))
		return fmt.Errorf("missing components: %v", missingComponents)
	}

	result.Details["component_breakdown"] = map[string]any{
		"variant":  s.config.Variant,
		"required": allRequired,
		"total":    len(allRequired),
		"found":    len(components),
	}

	return nil
}

// executeSendMixedData captures baseline metrics before data processing.
// Test data is loaded at container startup via file_input components
// (configs/structural.json defines file_sensors, file_documents, etc.)
// which read from testdata/semantic/*.jsonl files.
func (s *TieredScenario) executeSendMixedData(ctx context.Context, result *Result) error {
	// Capture baseline BEFORE data is processed
	// This allows executeValidateProcessing to wait for the delta
	baseline, err := s.metrics.CaptureBaseline(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Could not capture pre-send baseline: %v", err))
	} else {
		s.preSendBaseline = baseline
	}

	result.Details["data_source"] = "file_input components (testdata/semantic/*.jsonl)"
	result.Metrics["messages_sent"] = 0 // Data loaded via file_input, not UDP

	return nil
}

// executeValidateProcessing validates data was processed through semantic pipeline
// using event-driven metric waits instead of fixed delays
func (s *TieredScenario) executeValidateProcessing(ctx context.Context, result *Result) error {
	// Test data (sensors.jsonl etc.) is loaded at container startup, so processing
	// may already be complete. Check current state first before waiting.
	currentValue, err := s.metrics.SumMetricsByName(ctx, "semstreams_datamanager_entities_updated_total")
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Could not fetch current metrics: %v", err))
	}

	// If we have sufficient entities already processed, skip waiting
	// (test data is pre-loaded, so baseline delta approach doesn't apply)
	if currentValue >= float64(s.config.MinProcessed) {
		result.Details["processing_already_complete"] = true
		result.Metrics["entities_processed_at_validation"] = currentValue
	} else if s.preSendBaseline == nil {
		result.Warnings = append(result.Warnings, "No pre-send baseline available, using default wait")
		// Use event-driven wait with component health check instead of hardcoded sleep
		if err := s.client.WaitForAllComponentsHealthy(ctx, 10*time.Second); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Components health wait: %v", err))
		}
	} else {
		// Wait for processing using event-driven metric polling
		waitOpts := client.WaitOpts{
			Timeout:      s.config.ValidationTimeout,
			PollInterval: s.config.PollInterval,
			Comparator:   ">=",
		}

		// Get baseline value for entity updates (captured BEFORE sending data)
		baselineUpdates := s.preSendBaseline.Metrics["semstreams_datamanager_entities_updated_total"]
		expectedUpdates := baselineUpdates + float64(s.config.MinProcessed)

		if err := s.metrics.WaitForMetric(ctx, "semstreams_datamanager_entities_updated_total", expectedUpdates, waitOpts); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Processing wait: %v (may still be processing)", err))
			// Don't fail - processing may complete by later stages
		}
	}

	// Use FlowTracer to capture flow snapshot for stage validation
	flowSnapshot, err := s.tracer.CaptureFlowSnapshot(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to capture flow snapshot: %v", err))
	} else {
		result.Details["flow_snapshot_captured"] = true
		result.Metrics["flow_snapshot_message_count"] = flowSnapshot.MessageCount
	}

	// Query component status
	components, err := s.client.GetComponents(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to get components: %v", err))
		return fmt.Errorf("component query failed: %w", err)
	}

	// Find graph components (modular architecture) and verify they're healthy
	// Required components: graph-ingest, graph-index, graph-gateway
	graphComponents := map[string]bool{
		"graph-ingest":  false,
		"graph-index":   false,
		"graph-gateway": false,
	}
	graphStatus := make(map[string]map[string]any)

	for _, comp := range components {
		if _, isGraphComp := graphComponents[comp.Name]; isGraphComp {
			graphComponents[comp.Name] = true
			if !comp.Healthy {
				result.Warnings = append(
					result.Warnings,
					fmt.Sprintf("Graph component %s not healthy: state=%s", comp.Name, comp.State),
				)
			}
			graphStatus[comp.Name] = map[string]any{
				"name":      comp.Name,
				"component": comp.Component,
				"type":      comp.Type,
				"healthy":   comp.Healthy,
				"state":     comp.State,
			}
		}
	}

	// Check all required graph components are present
	var missingGraph []string
	for name, found := range graphComponents {
		if !found {
			missingGraph = append(missingGraph, name)
		}
	}

	if len(missingGraph) > 0 {
		result.Errors = append(result.Errors, fmt.Sprintf("Graph components not found: %v", missingGraph))
		return fmt.Errorf("graph components not found: %v", missingGraph)
	}

	result.Details["graph_processor_status"] = graphStatus

	result.Metrics["component_count"] = len(components)
	result.Details["processing_validation"] = fmt.Sprintf(
		"Graph processor found and processing. Components: %d",
		len(components))

	return nil
}

// executeVerifyOutputs verifies multiple outputs are working
func (s *TieredScenario) executeVerifyOutputs(ctx context.Context, result *Result) error {
	components, err := s.client.GetComponents(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to get components: %v", err))
		return fmt.Errorf("component query failed: %w", err)
	}

	// Verify all outputs are present
	expectedOutputs := []string{"file", "objectstore"}
	foundOutputs := make(map[string]bool)

	for _, comp := range components {
		for _, expected := range expectedOutputs {
			if comp.Name == expected {
				foundOutputs[expected] = true
			}
		}
	}

	missingOutputs := []string{}
	for _, expected := range expectedOutputs {
		if !foundOutputs[expected] {
			missingOutputs = append(missingOutputs, expected)
		}
	}

	if len(missingOutputs) > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Missing outputs: %v", missingOutputs))
	}

	result.Metrics["outputs_found"] = len(foundOutputs)
	result.Metrics["outputs_expected"] = len(expectedOutputs)
	result.Details["output_validation"] = map[string]any{
		"expected": expectedOutputs,
		"found":    foundOutputs,
		"missing":  missingOutputs,
	}

	return nil
}

// checkSemembedHealth checks the semembed health endpoint.
func (s *TieredScenario) checkSemembedHealth(result *Result) bool {
	semembedHealthURL := "http://localhost:38081/health"
	resp, err := http.Get(semembedHealthURL)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("semembed health check failed: %v", err))
		result.Details["semembed_available"] = false
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		result.Warnings = append(result.Warnings, fmt.Sprintf("semembed unhealthy: status=%d", resp.StatusCode))
		result.Details["semembed_available"] = false
		return false
	}

	result.Details["semembed_available"] = true
	result.Metrics["semembed_health_status"] = resp.StatusCode
	return true
}

// executeWaitForEmbeddings waits for embeddings to be generated based on embedding provider.
// This stage runs before any search stages to ensure vectorCache is populated.
//
// Key fix: Wait for TOTAL embeddings (entityCount), not baseline + entityCount.
// The baseline approach was flawed because baseline is captured AFTER data is sent,
// so if 50 embeddings already exist, waiting for baseline(50) + entityCount(74) = 124
// would timeout since only 74 embeddings will ever exist.
func (s *TieredScenario) executeWaitForEmbeddings(ctx context.Context, result *Result) error {
	variant := s.detectVariantAndProvider(result)

	switch variant.embeddingProvider {
	case "disabled", "":
		// Structural tier - no embeddings to wait for
		result.Details["embedding_wait"] = "skipped (embeddings disabled)"
		return nil

	case "bm25", "http":
		// For HTTP, verify semembed health first
		if variant.embeddingProvider == "http" && !s.checkSemembedHealth(result) {
			result.Warnings = append(result.Warnings, "semembed unavailable, HTTP embeddings may not be generated")
			return nil
		}

		entityCount := s.getEntityCountForEmbeddings(result)

		waitOpts := client.WaitOpts{
			Timeout:      s.config.ValidationTimeout,
			PollInterval: s.config.PollInterval,
			Comparator:   ">=",
		}

		// Wait for TOTAL embeddings generated (not baseline + delta)
		startWait := time.Now()
		if err := s.metrics.WaitForMetric(ctx,
			"semstreams_graph_embedding_embeddings_generated_total",
			float64(entityCount),
			waitOpts); err != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Embedding wait: %v", err))
		}

		// Wait for entity count to stabilize in NATS KV.
		// This is more reliable than the previous 500ms sleep because the embedding
		// metric increments BEFORE the entity is persisted to KV. Polling actual
		// entity count ensures all entities are written before proceeding.
		stabilization := s.waitForEntityCountStabilization(ctx, entityCount)

		result.Details["embedding_wait"] = map[string]any{
			"provider":             variant.embeddingProvider,
			"entity_count":         entityCount,
			"wait_duration":        time.Since(startWait).String(),
			"entity_stabilization": stabilization.Stabilized,
			"final_entity_count":   stabilization.FinalCount,
			"used_sse":             stabilization.UsedSSE,
		}

	default:
		// Unknown provider - still wait for entity stabilization as a safety measure
		// This handles cases where the embedding provider metric isn't available yet
		// but embeddings are expected (e.g., semantic tier during startup)
		result.Warnings = append(result.Warnings, fmt.Sprintf("Unknown embedding provider: %s, waiting for entity stabilization", variant.embeddingProvider))

		entityCount := s.getEntityCountForEmbeddings(result)
		stabilization := s.waitForEntityCountStabilization(ctx, entityCount)

		result.Details["embedding_wait"] = map[string]any{
			"provider":             variant.embeddingProvider,
			"entity_count":         entityCount,
			"entity_stabilization": stabilization.Stabilized,
			"final_entity_count":   stabilization.FinalCount,
			"fallback_mode":        true,
			"used_sse":             stabilization.UsedSSE,
		}
	}

	return nil
}

// getEntityCountForEmbeddings returns the expected number of entities for embedding wait.
func (s *TieredScenario) getEntityCountForEmbeddings(result *Result) int {
	if count, ok := result.Metrics["entity_count"].(int); ok {
		return count
	}
	// Fallback based on variant
	variant := s.detectVariantAndProvider(result)
	if variant.variant == "structural" {
		return 7 // Structural test data
	}
	return 74 // Kitchen sink dataset
}

// executeTestHTTPGateway validates GraphQL Gateway query endpoints
func (s *TieredScenario) executeTestHTTPGateway(ctx context.Context, result *Result) error {
	graphqlURL := s.config.GraphQLURL
	httpClient := &http.Client{Timeout: 10 * time.Second}

	// Test globalSearch via GraphQL endpoint
	graphqlQuery := map[string]any{
		"query": `query($query: String!, $level: Int, $maxCommunities: Int) {
			globalSearch(query: $query, level: $level, maxCommunities: $maxCommunities) {
				entities { id type }
				count
			}
		}`,
		"variables": map[string]any{
			"query":          "robot warehouse",
			"level":          0,
			"maxCommunities": 10,
		},
	}

	queryJSON, err := json.Marshal(graphqlQuery)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to marshal GraphQL query: %v", err))
		return nil // Not a hard failure
	}

	req, err := http.NewRequestWithContext(ctx, "POST", graphqlURL, strings.NewReader(string(queryJSON)))
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to create gateway request: %v", err))
		return nil
	}
	req.Header.Set("Content-Type", "application/json")

	startTime := time.Now()
	resp, err := httpClient.Do(req)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("GraphQL Gateway request failed: %v", err))
		return nil
	}
	defer resp.Body.Close()

	latency := time.Since(startTime)
	result.Metrics["graphql_gateway_latency_ms"] = latency.Milliseconds()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		result.Warnings = append(result.Warnings, fmt.Sprintf("GraphQL Gateway returned status %d: %s", resp.StatusCode, body))
		return nil
	}

	// Parse GraphQL response structure
	var gqlResp struct {
		Data struct {
			GlobalSearch struct {
				Entities []struct {
					ID   string `json:"id"`
					Type string `json:"type"`
				} `json:"entities"`
				Count int `json:"count"`
			} `json:"globalSearch"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to read gateway response: %v", err))
		return nil
	}

	if err := json.Unmarshal(bodyBytes, &gqlResp); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to parse gateway response: %v", err))
		return nil
	}

	if len(gqlResp.Errors) > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("GraphQL search error: %s", gqlResp.Errors[0].Message))
		return nil
	}

	hitCount := len(gqlResp.Data.GlobalSearch.Entities)
	result.Metrics["graphql_gateway_search_hits"] = hitCount
	result.Details["graphql_gateway_tested"] = true
	result.Details["graphql_gateway_endpoint"] = graphqlURL

	return nil
}

// executeTestEmbeddingFallback validates BM25 fallback when semembed unavailable
func (s *TieredScenario) executeTestEmbeddingFallback(ctx context.Context, result *Result) error {
	// Check if semembed was available during semantic search test
	semembedAvailable, ok := result.Details["semembed_available"].(bool)
	if !ok {
		semembedAvailable = false
	}

	// Query component status to verify graph processor configuration
	components, err := s.client.GetComponents(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to get components for fallback test: %v", err))
		return fmt.Errorf("component query failed: %w", err)
	}

	// Find graph-embedding component and check its health (for fallback validation)
	var graphEmbeddingHealthy bool
	for _, comp := range components {
		if comp.Name == "graph-embedding" {
			graphEmbeddingHealthy = comp.Healthy
			break
		}
	}

	// The key insight: if semembed is unavailable, graph-embedding should still be healthy (using BM25)
	// If semembed is available, we verify hybrid mode is working
	result.Details["embedding_fallback_test"] = map[string]any{
		"semembed_available":      semembedAvailable,
		"graph_embedding_healthy": graphEmbeddingHealthy,
		"fallback_mode":           !semembedAvailable,
		"message":                 "Graph embedding operational regardless of semembed availability",
	}

	// If semembed was unavailable but graph-embedding is healthy, BM25 fallback is working
	if !semembedAvailable && graphEmbeddingHealthy {
		result.Metrics["fallback_verified"] = 1
		result.Details["fallback_validation"] = "BM25 fallback active - graph-embedding healthy without semembed"
	} else if semembedAvailable && graphEmbeddingHealthy {
		result.Metrics["hybrid_mode_verified"] = 1
		result.Details["fallback_validation"] = "Hybrid mode active - semembed + BM25 available"
	}

	// Send test message to verify search works in current mode
	conn, err := net.Dial("udp", s.udpAddr)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to connect for fallback test: %v", err))
		return nil // Don't fail the whole test
	}
	defer conn.Close()

	fallbackMsg := map[string]any{
		"type":        "telemetry",
		"entity_id":   "test.e2e.semantic.fallback.sensor.device",
		"entity_type": "sensor",
		"timestamp":   time.Now().Unix(),
		"description": "Testing search fallback mechanism with lexical matching",
		"data": map[string]any{
			"value": 123,
		},
	}

	msgBytes, err := json.Marshal(fallbackMsg)
	if err == nil {
		_, _ = conn.Write(msgBytes)
		result.Metrics["fallback_test_messages_sent"] = 1
	}

	return nil
}

// executeValidateRules validates that rules are being evaluated and triggered
// using MetricsClient for consistent metric access
func (s *TieredScenario) executeValidateRules(ctx context.Context, result *Result) error {
	// Capture baseline metrics
	baselineMetrics, err := s.metrics.ExtractRuleMetrics(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to capture baseline rule metrics: %v", err))
		baselineMetrics = &client.RuleMetrics{}
	}

	// Check for reactive workflow metrics presence
	ruleMetricsPresent, foundCount := s.checkReactiveMetricsPresence(ctx)

	// Send test messages
	sentCount := s.sendRuleTestMessages(result)

	// Wait for rule evaluations if needed
	s.waitForRuleEvaluations(ctx, baselineMetrics, sentCount, result)

	// Get final metrics
	finalMetrics, err := s.metrics.ExtractRuleMetrics(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to get final rule metrics: %v", err))
		return nil
	}

	// Record validation results
	s.recordRuleValidationResults(result, baselineMetrics, finalMetrics, ruleMetricsPresent, foundCount, sentCount)
	if foundCount < 2 {
		return fmt.Errorf("rule engine metrics missing: found %d of 3", foundCount)
	}
	if finalMetrics.Evaluations <= 0 {
		return fmt.Errorf("rule engine performed no evaluations")
	}

	return nil
}

// checkReactiveMetricsPresence checks for rule engine metrics and returns presence map and count.
func (s *TieredScenario) checkReactiveMetricsPresence(ctx context.Context) (map[string]bool, int) {
	metricsRaw, err := s.metrics.FetchRaw(ctx)
	metricNames := []string{
		"semstreams_rule_evaluations_total",
		"semstreams_rule_triggers_total",
		"semstreams_rule_events_published_total",
	}

	presence := make(map[string]bool, len(metricNames))
	count := 0
	for _, name := range metricNames {
		found := err == nil && strings.Contains(metricsRaw, name)
		presence[name] = found
		if found {
			count++
		}
	}
	return presence, count
}

// sendRuleTestMessages sends test messages via UDP and returns the count sent.
func (s *TieredScenario) sendRuleTestMessages(result *Result) int {
	conn, err := net.Dial("udp", s.udpAddr)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to connect for rule test: %v", err))
		return 0
	}
	defer conn.Close()

	messages := []map[string]any{
		{"type": "telemetry", "entity_id": "test.e2e.rules.validation.sensor.battery", "battery": map[string]any{"level": 15.0}, "timestamp": time.Now().Unix()},
		{"type": "telemetry", "entity_id": "test.e2e.rules.validation.sensor.temperature", "data": map[string]any{"temperature": 55.0}, "timestamp": time.Now().Unix()},
	}

	sentCount := 0
	for _, msg := range messages {
		if msgBytes, err := json.Marshal(msg); err == nil {
			if _, err := conn.Write(msgBytes); err == nil {
				sentCount++
			}
		}
	}
	result.Metrics["rule_test_messages_sent"] = sentCount
	return sentCount
}

// waitForRuleEvaluations waits for rule evaluations if baseline count is low.
func (s *TieredScenario) waitForRuleEvaluations(ctx context.Context, baseline *client.RuleMetrics, sentCount int, result *Result) {
	if baseline.Evaluations >= 100 {
		result.Details["rules_already_evaluated"] = true
		return
	}

	waitOpts := client.WaitOpts{
		Timeout:      s.config.ValidationTimeout,
		PollInterval: s.config.PollInterval,
		Comparator:   ">=",
	}
	expected := baseline.Evaluations + float64(sentCount)
	if err := s.metrics.WaitForMetric(ctx, "semstreams_rule_evaluations_total", expected, waitOpts); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Rule evaluation wait: %v", err))
	}
}

// recordRuleValidationResults records all rule validation metrics and details.
func (s *TieredScenario) recordRuleValidationResults(result *Result, baseline, final *client.RuleMetrics, metricsPresent map[string]bool, foundCount, sentCount int) {
	firingsDelta := int(final.Firings - baseline.Firings)
	evaluatedDelta := int(final.Evaluations - baseline.Evaluations)
	actionsDelta := int(final.ActionsDispatched - baseline.ActionsDispatched)

	// Record metrics
	result.Metrics["rules_firings_count"] = int(final.Firings)
	result.Metrics["rules_evaluated_count"] = int(final.Evaluations)
	result.Metrics["rules_firings_delta"] = firingsDelta
	result.Metrics["rules_evaluated_delta"] = evaluatedDelta
	result.Metrics["rule_metrics_found"] = foundCount
	result.Metrics["actions_dispatched"] = int(final.ActionsDispatched)
	result.Metrics["executions_created"] = int(final.ExecutionsCreated)
	result.Metrics["executions_completed"] = int(final.ExecutionsCompleted)

	if final.Firings < 1 {
		result.Warnings = append(result.Warnings, "No rules fired - check workflow configuration and test data")
	}

	validationPassed := foundCount >= 2 && final.Evaluations > 0
	if validationPassed {
		result.Metrics["rules_validation_passed"] = 1
	}

	result.Details["rule_validation"] = map[string]any{
		"metrics_present":      metricsPresent,
		"metrics_found":        foundCount,
		"firings_before":       int(baseline.Firings),
		"firings_after":        int(final.Firings),
		"firings_delta":        firingsDelta,
		"evaluated_before":     int(baseline.Evaluations),
		"evaluated_after":      int(final.Evaluations),
		"evaluated_delta":      evaluatedDelta,
		"actions_dispatched":   int(final.ActionsDispatched),
		"actions_delta":        actionsDelta,
		"executions_created":   int(final.ExecutionsCreated),
		"executions_completed": int(final.ExecutionsCompleted),
		"test_messages_sent":   sentCount,
		"validation_passed":    validationPassed,
		"message": fmt.Sprintf("Rule engine: %d firings, %d evaluations (delta: +%d firings, +%d evaluations), %d actions dispatched",
			int(final.Firings), int(final.Evaluations), firingsDelta, evaluatedDelta, int(final.ActionsDispatched)),
	}
}

// executeValidateMetrics validates Prometheus metrics exposure
func (s *TieredScenario) executeValidateMetrics(_ context.Context, result *Result) error {
	// Query metrics endpoint (port 9090, not 8080 which is the HTTP API)
	metricsURL := s.config.MetricsURL + "/metrics"
	resp, err := http.Get(metricsURL)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to query metrics endpoint: %v", err))
		return fmt.Errorf("metrics endpoint unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		result.Errors = append(result.Errors, fmt.Sprintf("Metrics endpoint returned status %d", resp.StatusCode))
		return fmt.Errorf("metrics endpoint returned status %d", resp.StatusCode)
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to read metrics response: %v", err))
		return fmt.Errorf("failed to read metrics: %w", err)
	}

	metricsText := string(body)

	// Define key metrics to validate (presence only, not values)
	// Metrics list curated from processor/graph-index/metrics.go, pkg/cache/metrics.go,
	// and processor/json_filter/metrics.go - updated 2026-01-10
	requiredMetrics := []string{
		"semstreams_graph_index_events_processed_total", // graph-index events successfully processed
		"semstreams_graph_index_updates_total",          // Per-index update counts
		"semstreams_cache_hits_total",                   // DataManager L1/L2 cache hits
		"semstreams_cache_misses_total",                 // DataManager cache misses
	}

	// Optional metrics (present only when certain features active)
	optionalMetrics := []string{
		"semstreams_graph_index_events_total",                   // Total events received
		"semstreams_graph_index_events_failed_total",            // Processing failures
		"semstreams_graph_embedding_embeddings_generated_total", // Embedding generation count
		"semstreams_json_filter_matched_total",                  // JSON filter matched messages
		"semstreams_json_filter_dropped_total",                  // JSON filter dropped messages
		"semstreams_graph_embedding_pending",                    // Current pending embeddings
		"semstreams_graph_embedding_dedup_hits_total",           // Embeddings deduplicated (reused)
		"semstreams_graph_embedding_errors_total",               // Failed embedding generations
	}

	foundRequired := make(map[string]bool)
	foundOptional := make(map[string]bool)
	missingRequired := []string{}

	// Check required metrics
	for _, metric := range requiredMetrics {
		if strings.Contains(metricsText, metric) {
			foundRequired[metric] = true
		} else {
			missingRequired = append(missingRequired, metric)
		}
	}

	// Check optional metrics (don't fail if missing)
	for _, metric := range optionalMetrics {
		if strings.Contains(metricsText, metric) {
			foundOptional[metric] = true
		}
	}

	// Fail if required metrics are missing
	if len(missingRequired) > 0 {
		result.Errors = append(result.Errors, fmt.Sprintf("Missing required metrics: %v", missingRequired))
		return fmt.Errorf("missing required metrics: %v", missingRequired)
	}

	// Record results
	result.Metrics["required_metrics_found"] = len(foundRequired)
	result.Metrics["required_metrics_expected"] = len(requiredMetrics)
	result.Metrics["optional_metrics_found"] = len(foundOptional)
	result.Metrics["optional_metrics_expected"] = len(optionalMetrics)

	result.Details["metrics_validation"] = map[string]any{
		"endpoint":         metricsURL,
		"required_found":   foundRequired,
		"optional_found":   foundOptional,
		"missing_required": missingRequired,
		"message":          fmt.Sprintf("Found %d/%d required metrics, %d/%d optional metrics", len(foundRequired), len(requiredMetrics), len(foundOptional), len(optionalMetrics)),
	}

	return nil
}

// executeWaitForRuleStabilization waits for rule evaluation count to stabilize.
// This is needed because rules are evaluated asynchronously via KV watch,
// and the semantic tier's slower embedding generation means rule evaluations
// may still be in progress when validate-rules runs.
func (s *TieredScenario) executeWaitForRuleStabilization(ctx context.Context, result *Result) error {
	startTime := time.Now()

	// Get initial evaluation count
	initialMetrics, err := s.metrics.ExtractRuleMetrics(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to get initial rule metrics: %v", err))
		return nil
	}

	// Poll until evaluation count stabilizes (no change for 2 consecutive polls)
	lastCount := initialMetrics.Evaluations
	stableCount := 0
	requiredStablePolls := 2 // Must be stable for 2 consecutive polls

	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()

	timeout := time.After(s.config.ValidationTimeout)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			result.Details["rule_stabilization"] = map[string]any{
				"stabilized":    false,
				"final_count":   lastCount,
				"wait_duration": time.Since(startTime).String(),
			}
			return nil
		case <-ticker.C:
			currentMetrics, err := s.metrics.ExtractRuleMetrics(ctx)
			if err != nil {
				continue
			}

			if currentMetrics.Evaluations == lastCount {
				stableCount++
				if stableCount >= requiredStablePolls {
					result.Details["rule_stabilization"] = map[string]any{
						"stabilized":         true,
						"final_count":        currentMetrics.Evaluations,
						"wait_duration":      time.Since(startTime).String(),
						"firings":            currentMetrics.Firings,
						"actions_dispatched": currentMetrics.ActionsDispatched,
					}
					return nil
				}
			} else {
				stableCount = 0
				lastCount = currentMetrics.Evaluations
			}
		}
	}
}
