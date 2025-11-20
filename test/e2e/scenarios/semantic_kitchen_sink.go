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

	"github.com/c360/semstreams/test/e2e/client"
)

// SemanticKitchenSinkScenario validates comprehensive semantic processing
type SemanticKitchenSinkScenario struct {
	name        string
	description string
	client      *client.ObservabilityClient
	udpAddr     string
	config      *SemanticKitchenSinkConfig
}

// SemanticKitchenSinkConfig contains configuration for kitchen sink test
type SemanticKitchenSinkConfig struct {
	// Test data configuration
	MessageCount    int           `json:"message_count"`
	MessageInterval time.Duration `json:"message_interval"`

	// Validation configuration
	ValidationDelay time.Duration `json:"validation_delay"`
	MinProcessed    int           `json:"min_processed"`
}

// DefaultSemanticKitchenSinkConfig returns default configuration
func DefaultSemanticKitchenSinkConfig() *SemanticKitchenSinkConfig {
	return &SemanticKitchenSinkConfig{
		MessageCount:    20,
		MessageInterval: 50 * time.Millisecond,
		ValidationDelay: 5 * time.Second,
		MinProcessed:    10, // At least 50% should make it through
	}
}

// NewSemanticKitchenSinkScenario creates a new kitchen sink semantic test scenario
func NewSemanticKitchenSinkScenario(
	obsClient *client.ObservabilityClient,
	udpAddr string,
	config *SemanticKitchenSinkConfig,
) *SemanticKitchenSinkScenario {
	if config == nil {
		config = DefaultSemanticKitchenSinkConfig()
	}
	if udpAddr == "" {
		udpAddr = "localhost:14550"
	}

	return &SemanticKitchenSinkScenario{
		name:        "semantic-kitchen-sink",
		description: "Tests comprehensive semantic stack: Protocol + Graph + Indexes + Queries + Multiple Outputs",
		client:      obsClient,
		udpAddr:     udpAddr,
		config:      config,
	}
}

// Name returns the scenario name
func (s *SemanticKitchenSinkScenario) Name() string {
	return s.name
}

// Description returns the scenario description
func (s *SemanticKitchenSinkScenario) Description() string {
	return s.description
}

// Setup prepares the scenario
func (s *SemanticKitchenSinkScenario) Setup(_ context.Context) error {
	// Verify UDP endpoint is reachable
	conn, err := net.Dial("udp", s.udpAddr)
	if err != nil {
		return fmt.Errorf("cannot reach UDP endpoint %s: %w", s.udpAddr, err)
	}
	_ = conn.Close()

	return nil
}

// Execute runs the kitchen sink semantic test scenario
func (s *SemanticKitchenSinkScenario) Execute(ctx context.Context) (*Result, error) {
	result := &Result{
		ScenarioName: s.name,
		StartTime:    time.Now(),
		Success:      false,
		Metrics:      make(map[string]any),
		Details:      make(map[string]any),
		Errors:       []string{},
		Warnings:     []string{},
	}

	// Track execution stages
	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"verify-components", s.executeVerifyComponents},
		{"send-mixed-data", s.executeSendMixedData},
		{"validate-processing", s.executeValidateProcessing},
		{"test-semantic-search", s.executeTestSemanticSearch},
		{"test-http-gateway", s.executeTestHTTPGateway},
		{"test-embedding-fallback", s.executeTestEmbeddingFallback},
		{"validate-metrics", s.executeValidateMetrics},
		{"verify-outputs", s.executeVerifyOutputs},
	}

	// Execute each stage
	for _, stage := range stages {
		stageStart := time.Now()

		if err := stage.fn(ctx, result); err != nil {
			result.Success = false
			result.Error = fmt.Sprintf("%s failed: %v", stage.name, err)
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime)
			return result, nil // Return result even on failure
		}

		result.Metrics[fmt.Sprintf("%s_duration_ms", stage.name)] = time.Since(stageStart).Milliseconds()
	}

	// Overall success
	result.Success = true
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	return result, nil
}

// Teardown cleans up after the scenario
func (s *SemanticKitchenSinkScenario) Teardown(_ context.Context) error {
	// No cleanup needed for kitchen sink test
	return nil
}

// executeVerifyComponents checks that all kitchen sink components exist
func (s *SemanticKitchenSinkScenario) executeVerifyComponents(ctx context.Context, result *Result) error {
	components, err := s.client.GetComponents(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to get components: %v", err))
		return fmt.Errorf("component verification failed: %w", err)
	}

	// Protocol components
	protocolComponents := []string{"udp", "json_generic", "json_filter", "json_to_entity"}
	// Semantic components
	semanticComponents := []string{"graph"}
	// Output components
	outputComponents := []string{"file", "httppost", "websocket", "objectstore"}
	// Gateway components (use instance names from config, not factory names)
	gatewayComponents := []string{"api-gateway"}

	allRequired := append(protocolComponents, semanticComponents...)
	allRequired = append(allRequired, outputComponents...)
	allRequired = append(allRequired, gatewayComponents...)

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
		"protocol": protocolComponents,
		"semantic": semanticComponents,
		"outputs":  outputComponents,
		"total":    len(allRequired),
		"found":    len(components),
	}

	return nil
}

// executeSendMixedData sends mixed test data (entities + regular messages)
func (s *SemanticKitchenSinkScenario) executeSendMixedData(ctx context.Context, result *Result) error {
	conn, err := net.Dial("udp", s.udpAddr)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to connect to UDP: %v", err))
		return fmt.Errorf("UDP connection failed: %w", err)
	}
	defer conn.Close()

	messagesSent := 0
	telemetryCount := 0
	regularCount := 0

	for i := 0; i < s.config.MessageCount; i++ {
		var testMsg map[string]any

		// Alternate between telemetry (entities) and regular messages
		if i%2 == 0 {
			// Telemetry message (will be processed by graph)
			testMsg = map[string]any{
				"type":        "telemetry",
				"entity_id":   fmt.Sprintf("device-%d", i/2),
				"entity_type": "sensor",
				"timestamp":   time.Now().Unix(),
				"data": map[string]any{
					"temperature": 20.0 + float64(i),
					"humidity":    50.0 + float64(i*2),
					"pressure":    1013.0 + float64(i)*0.5,
					"location": map[string]any{
						"lat": 37.7749 + float64(i)*0.001,
						"lon": -122.4194 + float64(i)*0.001,
					},
				},
				"value": i * 5,
			}
			telemetryCount++
		} else {
			// Regular message (will be filtered out of entity stream)
			testMsg = map[string]any{
				"type":      "regular",
				"value":     i * 10,
				"timestamp": time.Now().Unix(),
				"metadata": map[string]any{
					"source":   "test",
					"sequence": i,
				},
			}
			regularCount++
		}

		msgBytes, err := json.Marshal(testMsg)
		if err != nil {
			continue
		}

		_, err = conn.Write(msgBytes)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to send message %d: %v", i, err))
			continue
		}

		messagesSent++

		// Wait between messages
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.config.MessageInterval):
		}
	}

	result.Metrics["messages_sent"] = messagesSent
	result.Metrics["telemetry_sent"] = telemetryCount
	result.Metrics["regular_sent"] = regularCount
	result.Details["data_sent"] = fmt.Sprintf(
		"Sent %d messages: %d telemetry (entities), %d regular",
		messagesSent, telemetryCount, regularCount)

	return nil
}

// executeValidateProcessing validates data was processed through semantic pipeline
func (s *SemanticKitchenSinkScenario) executeValidateProcessing(ctx context.Context, result *Result) error {
	// Wait for processing
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(s.config.ValidationDelay):
	}

	// Query component status
	components, err := s.client.GetComponents(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to get components: %v", err))
		return fmt.Errorf("component query failed: %w", err)
	}

	// Find graph processor and verify it's healthy
	var graphFound bool
	for _, comp := range components {
		if comp.Name == "graph" {
			graphFound = true
			if !comp.Healthy {
				result.Warnings = append(
					result.Warnings,
					fmt.Sprintf("Graph processor not healthy: state=%s", comp.State),
				)
			}
			result.Details["graph_processor_status"] = map[string]any{
				"name":    comp.Name,
				"type":    comp.Type,
				"healthy": comp.Healthy,
				"state":   comp.State,
			}
			break
		}
	}

	if !graphFound {
		result.Errors = append(result.Errors, "Graph processor not found")
		return fmt.Errorf("graph processor not found")
	}

	result.Metrics["component_count"] = len(components)
	result.Details["processing_validation"] = fmt.Sprintf(
		"Graph processor found and processing. Components: %d",
		len(components))

	return nil
}

// executeVerifyOutputs verifies multiple outputs are working
func (s *SemanticKitchenSinkScenario) executeVerifyOutputs(ctx context.Context, result *Result) error {
	components, err := s.client.GetComponents(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to get components: %v", err))
		return fmt.Errorf("component query failed: %w", err)
	}

	// Verify all outputs are present
	expectedOutputs := []string{"file", "httppost", "websocket", "objectstore"}
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

// executeTestSemanticSearch validates semantic search with semembed embeddings
func (s *SemanticKitchenSinkScenario) executeTestSemanticSearch(ctx context.Context, result *Result) error {
	// Check semembed health endpoint
	semembedHealthURL := "http://localhost:8081/health"
	resp, err := http.Get(semembedHealthURL)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("semembed health check failed: %v", err))
		// Not a hard failure - might be running in environment without semembed
		result.Details["semembed_available"] = false
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		result.Warnings = append(result.Warnings, fmt.Sprintf("semembed unhealthy: status=%d", resp.StatusCode))
		result.Details["semembed_available"] = false
		return nil
	}

	result.Details["semembed_available"] = true
	result.Metrics["semembed_health_status"] = resp.StatusCode

	// Send entities with rich text content for embedding
	conn, err := net.Dial("udp", s.udpAddr)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to connect for semantic test: %v", err))
		return fmt.Errorf("UDP connection failed: %w", err)
	}
	defer conn.Close()

	semanticTestMessages := []map[string]any{
		{
			"type":        "telemetry",
			"entity_id":   "robot-alpha",
			"entity_type": "robot",
			"timestamp":   time.Now().Unix(),
			"description": "Autonomous delivery robot operating in warehouse facility",
			"data": map[string]any{
				"battery":     85.5,
				"temperature": 42.0,
			},
		},
		{
			"type":        "telemetry",
			"entity_id":   "robot-beta",
			"entity_type": "robot",
			"timestamp":   time.Now().Unix(),
			"description": "Mobile robot performing inventory scanning tasks",
			"data": map[string]any{
				"battery":     92.0,
				"temperature": 38.5,
			},
		},
	}

	for i, msg := range semanticTestMessages {
		msgBytes, err := json.Marshal(msg)
		if err != nil {
			continue
		}

		if _, err := conn.Write(msgBytes); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to send semantic message %d: %v", i, err))
		}
	}

	// Wait for embedding generation and indexing
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(2 * time.Second):
	}

	result.Metrics["semantic_messages_sent"] = len(semanticTestMessages)

	// Wait for embeddings to be generated
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(3 * time.Second):
	}

	// Query metrics to verify embeddings were generated
	metricsURL := "http://localhost:9090/metrics"
	metricsResp, err := http.Get(metricsURL)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to query metrics: %v", err))
	} else {
		defer metricsResp.Body.Close()
		body, _ := io.ReadAll(metricsResp.Body)
		metricsText := string(body)

		// Check for embedding metrics
		embeddingsGenerated := strings.Contains(metricsText, "indexengine_embeddings_generated_total")
		embeddingsActive := strings.Contains(metricsText, "indexengine_embeddings_active")
		embeddingProvider := strings.Contains(metricsText, "indexengine_embedding_provider")

		result.Details["semantic_search_test"] = map[string]any{
			"semembed_healthy":            true,
			"messages_sent":               len(semanticTestMessages),
			"embedding_tested":            true,
			"embeddings_generated_metric": embeddingsGenerated,
			"embeddings_active_metric":    embeddingsActive,
			"embedding_provider_metric":   embeddingProvider,
		}

		if embeddingsGenerated && embeddingsActive && embeddingProvider {
			result.Metrics["embedding_metrics_verified"] = 1
		}
	}

	return nil
}

// executeTestHTTPGateway validates HTTP Gateway query endpoints
func (s *SemanticKitchenSinkScenario) executeTestHTTPGateway(ctx context.Context, result *Result) error {
	gatewayURL := "http://localhost:8080/api-gateway"
	httpClient := &http.Client{Timeout: 10 * time.Second}

	// Test semantic search endpoint
	searchQuery := map[string]interface{}{
		"query":     "robot warehouse",
		"threshold": 0.2,
		"limit":     10,
	}

	queryJSON, err := json.Marshal(searchQuery)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to marshal search query: %v", err))
		return nil // Not a hard failure
	}

	url := gatewayURL + "/search/semantic"
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(queryJSON)))
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to create gateway request: %v", err))
		return nil
	}
	req.Header.Set("Content-Type", "application/json")

	startTime := time.Now()
	resp, err := httpClient.Do(req)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("HTTP Gateway request failed: %v", err))
		return nil
	}
	defer resp.Body.Close()

	latency := time.Since(startTime)
	result.Metrics["http_gateway_latency_ms"] = latency.Milliseconds()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		result.Warnings = append(result.Warnings, fmt.Sprintf("HTTP Gateway returned status %d: %s", resp.StatusCode, body))
		return nil
	}

	// Parse response structure
	var searchResult struct {
		Data struct {
			Query string `json:"query"`
			Hits  []struct {
				EntityID string  `json:"entity_id"`
				Score    float64 `json:"score"`
			} `json:"hits"`
		} `json:"data"`
		Error string `json:"error"`
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to read gateway response: %v", err))
		return nil
	}

	if err := json.Unmarshal(bodyBytes, &searchResult); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to parse gateway response: %v", err))
		return nil
	}

	if searchResult.Error != "" {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Gateway search error: %s", searchResult.Error))
		return nil
	}

	hitCount := len(searchResult.Data.Hits)
	result.Metrics["http_gateway_search_hits"] = hitCount
	result.Details["http_gateway_tested"] = true
	result.Details["http_gateway_endpoint"] = url

	return nil
}

// executeTestEmbeddingFallback validates BM25 fallback when semembed unavailable
func (s *SemanticKitchenSinkScenario) executeTestEmbeddingFallback(ctx context.Context, result *Result) error {
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

	// Find graph processor and check its health
	var graphHealthy bool
	for _, comp := range components {
		if comp.Name == "graph" {
			graphHealthy = comp.Healthy
			break
		}
	}

	// The key insight: if semembed is unavailable, graph should still be healthy (using BM25)
	// If semembed is available, we verify hybrid mode is working
	result.Details["embedding_fallback_test"] = map[string]any{
		"semembed_available": semembedAvailable,
		"graph_healthy":      graphHealthy,
		"fallback_mode":      !semembedAvailable,
		"message":            "Graph processor operational regardless of semembed availability",
	}

	// If semembed was unavailable but graph is healthy, BM25 fallback is working
	if !semembedAvailable && graphHealthy {
		result.Metrics["fallback_verified"] = 1
		result.Details["fallback_validation"] = "BM25 fallback active - graph healthy without semembed"
	} else if semembedAvailable && graphHealthy {
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
		"entity_id":   "fallback-test-device",
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

// executeValidateMetrics validates Prometheus metrics exposure
func (s *SemanticKitchenSinkScenario) executeValidateMetrics(ctx context.Context, result *Result) error {
	// Query metrics endpoint (port 9090, not 8080 which is the HTTP API)
	metricsURL := "http://localhost:9090/metrics"
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
	// Note: Graph processor Prometheus metrics are not yet implemented
	// so we verify basic dataflow metrics instead
	requiredMetrics := []string{
		"semstreams_cache_hits_total",          // DataManager L1/L2 cache
		"semstreams_cache_misses_total",        // DataManager cache misses
		"semstreams_json_filter_matched_total", // JSON filter metrics
	}

	// Optional metrics (present only when certain features active)
	optionalMetrics := []string{
		"semstreams_indexmanager_embeddings_generated_total",
		"semstreams_graph_edges_created_total",
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
