// Package scenarios provides E2E test scenarios for SemStreams semantic processing
package scenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
)

// Structural variant validation functions for rules-only testing

// executeValidateZeroEmbeddings validates that NO embeddings were generated (structural tier constraint)
func (s *TieredScenario) executeValidateZeroEmbeddings(ctx context.Context, result *Result) error {
	embeddingCount, _ := s.metrics.SumMetricsByName(ctx, "semstreams_graph_embedding_embeddings_generated_total")

	result.Metrics["embeddings_generated"] = int(embeddingCount)

	constraintMet := int(embeddingCount) <= s.config.ExpectedEmbeddings

	result.Details["zero_embeddings_validation"] = map[string]any{
		"embeddings_generated": int(embeddingCount),
		"expected":             s.config.ExpectedEmbeddings,
		"constraint_met":       constraintMet,
		"message":              fmt.Sprintf("Embeddings: %d (expected %d for structural tier)", int(embeddingCount), s.config.ExpectedEmbeddings),
	}

	// Structural tier constraint: embeddings MUST be zero (or within expected limit)
	if !constraintMet {
		return fmt.Errorf("structural tier constraint violated: embeddings=%d (expected max %d)",
			int(embeddingCount), s.config.ExpectedEmbeddings)
	}

	return nil
}

// executeValidateZeroClusters validates that NO clustering occurred (structural tier constraint)
func (s *TieredScenario) executeValidateZeroClusters(ctx context.Context, result *Result) error {
	clusteringCount, _ := s.metrics.SumMetricsByName(ctx, "semstreams_clustering_runs_total")

	result.Metrics["clustering_runs"] = int(clusteringCount)

	constraintMet := int(clusteringCount) <= s.config.ExpectedClusters

	result.Details["zero_clusters_validation"] = map[string]any{
		"clustering_runs": int(clusteringCount),
		"expected":        s.config.ExpectedClusters,
		"constraint_met":  constraintMet,
		"message":         fmt.Sprintf("Clustering runs: %d (expected %d for structural tier)", int(clusteringCount), s.config.ExpectedClusters),
	}

	// Structural tier constraint: clustering MUST NOT occur
	if !constraintMet {
		return fmt.Errorf("structural tier constraint violated: clustering_runs=%d (expected max %d)",
			int(clusteringCount), s.config.ExpectedClusters)
	}

	return nil
}

// executeValidateRuleTransitions validates reactive workflow rule firings and actions (structural tier)
func (s *TieredScenario) executeValidateRuleTransitions(ctx context.Context, result *Result) error {
	// Get reactive workflow metrics using MetricsClient
	ruleMetrics, err := s.metrics.ExtractRuleMetrics(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to extract rule metrics: %v", err))
		return nil
	}

	firings := int(ruleMetrics.Firings)
	actionsDispatched := int(ruleMetrics.ActionsDispatched)
	evaluations := int(ruleMetrics.Evaluations)

	result.Metrics["rule_firings"] = firings
	result.Metrics["actions_dispatched"] = actionsDispatched
	result.Metrics["rule_evaluations"] = evaluations

	// Validate minimum rule activity
	violations := []string{}
	if firings < s.config.MinRuleFirings {
		violations = append(violations,
			fmt.Sprintf("Rule firings: %d < %d (expected)", firings, s.config.MinRuleFirings))
	}
	if actionsDispatched < s.config.MinActionsDispatched {
		violations = append(violations,
			fmt.Sprintf("Actions dispatched: %d < %d (expected)", actionsDispatched, s.config.MinActionsDispatched))
	}

	result.Details["rule_transitions_validation"] = map[string]any{
		"rule_firings":       firings,
		"actions_dispatched": actionsDispatched,
		"evaluations":        evaluations,
		"min_firings":        s.config.MinRuleFirings,
		"min_actions":        s.config.MinActionsDispatched,
		"violations":         violations,
		"validation_passed":  len(violations) == 0,
		"reactive_behavior":  firings > 0 || actionsDispatched > 0,
		"message":            fmt.Sprintf("Reactive workflow: %d firings, %d actions dispatched, %d evaluations", firings, actionsDispatched, evaluations),
	}

	if len(violations) > 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Reactive workflow validation issues: %v", violations))
	}

	return nil
}

// executeValidateEntityTriples validates that sensor entities have the expected triples
// This helps diagnose rule trigger issues by showing exactly what triples are in ENTITY_STATES
func (s *TieredScenario) executeValidateEntityTriples(ctx context.Context, result *Result) error {
	// Get a sample temperature sensor entity
	sampleEntityID := "c360.logistics.environmental.sensor.temperature.temp-sensor-001"

	entity, err := s.natsClient.GetEntity(ctx, sampleEntityID)
	if err != nil {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Failed to get sample entity %s: %v", sampleEntityID, err))
		return nil
	}

	if entity == nil {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Sample entity %s not found in ENTITY_STATES", sampleEntityID))
		return nil
	}

	// Extract and categorize triples
	tripleDetails := make([]map[string]any, 0, len(entity.Triples))
	hasFahrenheit := false
	hasZone := false
	var fahrenheitValue any
	var zoneValue any

	for _, triple := range entity.Triples {
		tripleDetails = append(tripleDetails, map[string]any{
			"predicate":   triple.Predicate,
			"object":      triple.Object,
			"object_type": fmt.Sprintf("%T", triple.Object),
		})

		if triple.Predicate == "sensor.measurement.fahrenheit" {
			hasFahrenheit = true
			fahrenheitValue = triple.Object
		}
		if triple.Predicate == "geo.location.zone" {
			hasZone = true
			zoneValue = triple.Object
		}
	}

	// Check if triples match rule conditions
	ruleConditionsMet := false
	if hasFahrenheit && hasZone {
		if temp, ok := fahrenheitValue.(float64); ok && temp >= 40.0 {
			if zone, ok := zoneValue.(string); ok && strings.Contains(zone, "cold-storage") {
				ruleConditionsMet = true
			}
		}
	}

	result.Metrics["entity_triple_count"] = len(entity.Triples)
	result.Metrics["entity_has_fahrenheit"] = 0
	result.Metrics["entity_has_zone"] = 0
	if hasFahrenheit {
		result.Metrics["entity_has_fahrenheit"] = 1
	}
	if hasZone {
		result.Metrics["entity_has_zone"] = 1
	}

	result.Details["entity_triples_validation"] = map[string]any{
		"entity_id":                sampleEntityID,
		"triple_count":             len(entity.Triples),
		"has_fahrenheit":           hasFahrenheit,
		"has_zone":                 hasZone,
		"fahrenheit_value":         fahrenheitValue,
		"zone_value":               zoneValue,
		"rule_conditions_met":      ruleConditionsMet,
		"triples":                  tripleDetails,
		"expected_fahrenheit_pred": "sensor.measurement.fahrenheit",
		"expected_zone_pred":       "geo.location.zone",
		"message": fmt.Sprintf(
			"Entity %s: %d triples, fahrenheit=%v (has=%v), zone=%v (has=%v), conditions_met=%v",
			sampleEntityID, len(entity.Triples),
			fahrenheitValue, hasFahrenheit,
			zoneValue, hasZone,
			ruleConditionsMet,
		),
	}

	// Log warning if expected triples are missing
	if !hasFahrenheit {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("MISSING sensor.measurement.fahrenheit in entity %s - rules cannot evaluate temperature", sampleEntityID))
	}
	if !hasZone {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("MISSING geo.location.zone in entity %s - rules cannot evaluate zone", sampleEntityID))
	}

	// Always print triple details to stdout for debugging
	fmt.Printf("[ENTITY TRIPLES DEBUG] Entity: %s\n", sampleEntityID)
	fmt.Printf("[ENTITY TRIPLES DEBUG] Triple count: %d\n", len(entity.Triples))
	fmt.Printf("[ENTITY TRIPLES DEBUG] Fahrenheit value: %v (type: %T)\n", fahrenheitValue, fahrenheitValue)
	fmt.Printf("[ENTITY TRIPLES DEBUG] Zone value: %v (type: %T)\n", zoneValue, zoneValue)
	fmt.Printf("[ENTITY TRIPLES DEBUG] Rule conditions met: %v\n", ruleConditionsMet)
	for i, t := range entity.Triples {
		fmt.Printf("[ENTITY TRIPLES DEBUG] Triple[%d]: pred=%s, obj=%v (type=%T)\n", i, t.Predicate, t.Object, t.Object)
	}

	// Also check humidity entity to debug why humidity rule doesn't trigger
	humidEntityID := "c360.logistics.environmental.sensor.humidity.humid-sensor-001"
	humidEntity, humidErr := s.natsClient.GetEntity(ctx, humidEntityID)
	if humidErr != nil {
		fmt.Printf("[HUMIDITY DEBUG] Failed to get entity %s: %v\n", humidEntityID, humidErr)
	} else if humidEntity == nil {
		fmt.Printf("[HUMIDITY DEBUG] Entity %s NOT FOUND in ENTITY_STATES\n", humidEntityID)
	} else {
		fmt.Printf("[HUMIDITY DEBUG] Entity: %s\n", humidEntityID)
		fmt.Printf("[HUMIDITY DEBUG] Triple count: %d\n", len(humidEntity.Triples))
		var percentValue any
		var typeValue any
		for i, t := range humidEntity.Triples {
			fmt.Printf("[HUMIDITY DEBUG] Triple[%d]: pred=%s, obj=%v (type=%T)\n", i, t.Predicate, t.Object, t.Object)
			if t.Predicate == "sensor.measurement.percent" {
				percentValue = t.Object
			}
			if t.Predicate == "sensor.classification.type" {
				typeValue = t.Object
			}
		}
		// Check if rule conditions would be met
		conditionsMet := false
		if pct, ok := percentValue.(float64); ok && pct >= 50.0 {
			if typ, ok := typeValue.(string); ok && typ == "humidity" {
				conditionsMet = true
			}
		}
		fmt.Printf("[HUMIDITY DEBUG] percent value: %v, type value: %v, conditions met: %v\n", percentValue, typeValue, conditionsMet)
	}

	return nil
}

// executeValidateReferentialStub validates ADR-056 Decision-4's fourth path: when
// a write carries a RELATIONSHIP triple whose Object is a valid 6-part entity ID
// that no producer independently creates, graph-ingest materialises an
// envelope-bearing referential-integrity stub for that target so the reference
// always resolves to a node (load-bearing for traversal, and what makes the
// must-exist flip safe). This is the e2e coverage gap ADR-056 tracked as "gated to
// 4c": the only relationship-emitting production processor (IoT sensor) also
// emits its zone target, so nothing else exercises this path. Structural tier — no ML.
//
// The fixture drives the create_with_triples mutation lane (the cs-api lane that
// runs ensureRelationshipTargetsExist) directly, with a target under a dedicated
// e2e prefix that no processor emits, so the stub is the ONLY thing that can create it.
func (s *TieredScenario) executeValidateReferentialStub(ctx context.Context, result *Result) error {
	const (
		referrerID    = "c360.platform.e2e.referential.referrer.001"
		danglingID    = "c360.platform.e2e.referential.target.001"
		relationPred  = "test.e2e.references"
		createSubject = "graph.mutation.entity.create_with_triples"
	)

	// 1. Create the referrer carrying a relationship triple to the dangling target.
	req := graph.CreateEntityWithTriplesRequest{
		Entity: &graph.EntityState{
			ID:          referrerID,
			MessageType: message.Type{Domain: "e2e", Category: "referential", Version: "v1"},
		},
		Triples: []message.Triple{
			{Subject: referrerID, Predicate: relationPred, Object: danglingID, Confidence: 1.0},
		},
		RequestID: "e2e-referential-stub",
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal create_with_triples request: %w", err)
	}

	// ADR-060: RequestClassified surfaces handler failures via err; a hard
	// failure no longer returns an in-body Success=false.
	if _, err := s.natsClient.RequestClassified(ctx, createSubject, reqData, 10*time.Second); err != nil {
		return fmt.Errorf("create_with_triples request failed: %w", err)
	}

	// 2. Poll for the referential stub on the dangling target. ensureRelationshipTargetsExist
	//    blocks (wg.Wait) before the mutation reply, so this typically resolves on the first
	//    read; the bounded poll absorbs any KV/cache visibility lag.
	stub, getErr := s.natsClient.GetEntity(ctx, danglingID)
	for attempt := 0; (getErr != nil || stub == nil) && attempt < 20; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
		stub, getErr = s.natsClient.GetEntity(ctx, danglingID)
	}
	if stub == nil {
		return fmt.Errorf("referential stub %s was not created within timeout — fourth path (ensureReferencedEntityExists) did not fire", danglingID)
	}

	// 3. Assert the envelope-bearing stub markers (ADR-056 4b): the core.identity.stub
	//    marker and core.identity.referenced_by = the referrer that named it.
	hasMarker, hasReferencedBy, hasStubOwner := false, false, false
	var referencedByValue, stubOwnerValue any
	for _, t := range stub.Triples {
		switch t.Predicate {
		case "core.identity.stub":
			hasMarker = true
		case "core.identity.referenced_by":
			hasReferencedBy = true
			referencedByValue = t.Object
		case "core.identity.stub_owner":
			hasStubOwner = true
			stubOwnerValue = t.Object
		}
	}

	result.Metrics["referential_stub_triples"] = len(stub.Triples)
	result.Details["referential_stub_validation"] = map[string]any{
		"referrer_id":         referrerID,
		"dangling_target_id":  danglingID,
		"stub_created":        true,
		"has_stub_marker":     hasMarker,
		"has_referenced_by":   hasReferencedBy,
		"has_stub_owner":      hasStubOwner,
		"referenced_by_value": referencedByValue,
		"stub_owner_value":    stubOwnerValue,
		"stub_version":        stub.Version,
		"message":             fmt.Sprintf("Fourth path created referential stub %s (marker=%v, referenced_by=%v, owner=%v)", danglingID, hasMarker, referencedByValue, stubOwnerValue),
	}

	if !hasMarker || !hasReferencedBy || !hasStubOwner {
		return fmt.Errorf("referential stub %s missing required markers: core.identity.stub=%v, core.identity.referenced_by=%v, core.identity.stub_owner=%v",
			danglingID, hasMarker, hasReferencedBy, hasStubOwner)
	}
	if got := fmt.Sprintf("%v", referencedByValue); got != referrerID {
		return fmt.Errorf("referential stub %s core.identity.referenced_by = %q, want %q", danglingID, got, referrerID)
	}
	// Envelope-bearing stub must record a non-empty owner — the ADR-055
	// "no ownerless births" property the must-exist flip relies on.
	if owner, ok := stubOwnerValue.(string); !ok || owner == "" {
		return fmt.Errorf("referential stub %s core.identity.stub_owner is empty — an envelope-bearing stub must record an owner", danglingID)
	}

	result.Metrics["referential_stub_valid"] = 1
	return nil
}

// pathRAGResponse represents the parsed GraphQL response for PathRAG queries
type pathRAGResponse struct {
	Data struct {
		PathSearch struct {
			Entities  []pathRAGEntity `json:"entities"`
			Paths     [][]pathRAGStep `json:"paths"` // Each path is a sequence of steps
			Truncated bool            `json:"truncated"`
		} `json:"pathSearch"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type pathRAGEntity struct {
	ID    string  `json:"id"`
	Type  string  `json:"type"`
	Score float64 `json:"score"`
}

type pathRAGStep struct {
	From      string `json:"from"`
	Predicate string `json:"predicate"`
	To        string `json:"to"`
}

// executeTestPathRAGSensor validates PathRAG traversal using a sensor entity.
// Sensor entities demonstrate EntityID sibling inference (structured IoT data).
// PathRAG is a Tier 0 capability that runs on ALL tiers.
func (s *TieredScenario) executeTestPathRAGSensor(ctx context.Context, result *Result) error {
	startEntity := s.getPathRAGSensorEntity()
	gatewayURL := s.config.GraphQLURL

	resp, latency, err := s.sendPathRAGRequest(ctx, startEntity, gatewayURL)
	if err != nil {
		result.Details["pathrag_sensor_test"] = map[string]any{
			"start_entity": startEntity, "error": err.Error(), "gateway_url": gatewayURL,
		}
		return err
	}

	result.Metrics["pathrag_sensor_latency_ms"] = latency.Milliseconds()
	return s.validatePathRAGResultNamed(resp, startEntity, latency, result, "pathrag_sensor_test")
}

// executeTestPathRAGDocument validates PathRAG traversal using a document entity.
// Document entities demonstrate text-based similarity (statistical/semantic enhancements).
// PathRAG is a Tier 0 capability that runs on ALL tiers.
func (s *TieredScenario) executeTestPathRAGDocument(ctx context.Context, result *Result) error {
	startEntity := s.getPathRAGDocumentEntity()
	gatewayURL := s.config.GraphQLURL

	resp, latency, err := s.sendPathRAGRequest(ctx, startEntity, gatewayURL)
	if err != nil {
		result.Details["pathrag_document_test"] = map[string]any{
			"start_entity": startEntity, "error": err.Error(), "gateway_url": gatewayURL,
		}
		return err
	}

	result.Metrics["pathrag_document_latency_ms"] = latency.Milliseconds()
	return s.validatePathRAGResultNamed(resp, startEntity, latency, result, "pathrag_document_test")
}

// getPathRAGSensorEntity returns a sensor entity for PathRAG testing.
// All tiers now use testdata/semantic/sensors.jsonl which contains temperature sensors.
// Sensor entities demonstrate EntityID sibling inference (structural IoT data).
func (s *TieredScenario) getPathRAGSensorEntity() string {
	// All tiers use testdata/semantic/sensors.jsonl
	// Entity IDs follow format: {org}.{platform}.environmental.sensor.{type}.{device_id}
	// From sensors.jsonl: device_id=temp-sensor-001, type=temperature
	// Config: org_id=c360, platform=logistics
	return "c360.logistics.environmental.sensor.temperature.temp-sensor-001"
}

// getPathRAGDocumentEntity returns a document entity for PathRAG testing.
// All tiers use testdata/semantic/maintenance.jsonl which contains maintenance records.
// Document entities demonstrate text-based similarity (statistical/semantic enhancements).
func (s *TieredScenario) getPathRAGDocumentEntity() string {
	// All tiers use testdata/semantic/maintenance.jsonl
	// Use maintenance entity which has 15+ siblings with same type prefix
	// This allows sibling inference to find related entities
	return "c360.logistics.maintenance.work.completed.maint-001"
}

// sendPathRAGRequest sends the PathRAG GraphQL query and returns the parsed response
// Uses includeSiblings=true to leverage EntityID hierarchy for sibling detection
func (s *TieredScenario) sendPathRAGRequest(ctx context.Context, startEntity, gatewayURL string) (*pathRAGResponse, time.Duration, error) {
	graphqlQuery := map[string]any{
		"query": `query($startEntity: ID!, $maxDepth: Int, $maxNodes: Int, $includeSiblings: Boolean) {
			pathSearch(startEntity: $startEntity, maxDepth: $maxDepth, maxNodes: $maxNodes, includeSiblings: $includeSiblings) {
				entities { id type score } paths { from predicate to } truncated
			}}`,
		"variables": map[string]any{"startEntity": startEntity, "maxDepth": 2, "maxNodes": 10, "includeSiblings": true},
	}

	queryJSON, err := json.Marshal(graphqlQuery)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to marshal PathRAG query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", gatewayURL, bytes.NewReader(queryJSON))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create PathRAG request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 10 * time.Second}
	start := time.Now()
	resp, err := httpClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		return nil, latency, fmt.Errorf("PathRAG request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, latency, fmt.Errorf("PathRAG returned status %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, latency, fmt.Errorf("failed to read PathRAG response: %w", err)
	}

	var graphqlResp pathRAGResponse
	if err := json.Unmarshal(bodyBytes, &graphqlResp); err != nil {
		return nil, latency, fmt.Errorf("failed to parse PathRAG response: %w", err)
	}

	if len(graphqlResp.Errors) > 0 {
		return nil, latency, fmt.Errorf("PathRAG GraphQL error: %s", graphqlResp.Errors[0].Message)
	}

	return &graphqlResp, latency, nil
}

// validatePathRAGResult validates the PathRAG response and records results (backward compatible)
func (s *TieredScenario) validatePathRAGResult(resp *pathRAGResponse, startEntity string, latency time.Duration, result *Result) error {
	return s.validatePathRAGResultNamed(resp, startEntity, latency, result, "pathrag_test")
}

// validatePathRAGResultNamed validates the PathRAG response and records results with a custom test name
func (s *TieredScenario) validatePathRAGResultNamed(resp *pathRAGResponse, startEntity string, latency time.Duration, result *Result, testName string) error {
	ps := resp.Data.PathSearch
	entityCount := len(ps.Entities)
	// Count total paths (each path is a sequence of steps)
	pathCount := len(ps.Paths)

	// Use test-specific metric names
	metricsPrefix := testName[:len(testName)-5] // Remove "_test" suffix
	result.Metrics[metricsPrefix+"_entities_found"] = entityCount
	result.Metrics[metricsPrefix+"_paths_found"] = pathCount

	if entityCount == 0 {
		result.Details[testName] = map[string]any{
			"start_entity": startEntity, "entities_found": 0, "message": "No entities returned",
		}
		return fmt.Errorf("PathRAG returned no entities for start entity %s", startEntity)
	}

	// Verify scores decrease with depth (decay factor working)
	// This is a hard failure - with controlled input, decay scoring should be deterministic
	scoresValid := true
	var decayViolation string
	prevScore := 2.0
	entityIDs := make([]string, 0, len(ps.Entities))
	entityScores := make([]float64, 0, len(ps.Entities))
	for i, e := range ps.Entities {
		entityIDs = append(entityIDs, e.ID)
		entityScores = append(entityScores, e.Score)
		if i > 0 && e.Score > prevScore {
			scoresValid = false
			decayViolation = fmt.Sprintf("entity %s has score %.3f > previous %.3f", e.ID, e.Score, prevScore)
		}
		prevScore = e.Score
	}

	result.Details[testName] = map[string]any{
		"start_entity": startEntity, "entities_found": entityCount, "paths_found": pathCount,
		"truncated": ps.Truncated, "entity_ids": entityIDs, "entity_scores": entityScores,
		"scores_valid": scoresValid, "latency_ms": latency.Milliseconds(),
		"message": fmt.Sprintf("PathRAG traversal successful: found %d entities via %d paths", entityCount, pathCount),
	}

	// Hard failure on decay scoring violation - input is controlled, results should be deterministic
	if !scoresValid {
		return fmt.Errorf("PathRAG decay scoring violated: %s", decayViolation)
	}

	return nil
}

// executeTestEntityIDHierarchy validates the EntityID hierarchy GraphQL queries.
// This tests that the 6-part EntityID structure can be navigated via GraphQL.
// EntityID hierarchy is a Tier 0 capability that runs on ALL tiers.
func (s *TieredScenario) executeTestEntityIDHierarchy(ctx context.Context, result *Result) error {
	gatewayURL := s.config.GraphQLURL
	httpClient := &http.Client{Timeout: 10 * time.Second}

	// Test 1: Get hierarchy stats from root
	hierarchyQuery := map[string]any{
		"query": `query($prefix: String) {
			entityIdHierarchy(prefix: $prefix) {
				prefix totalEntities children { prefix name count }
			}}`,
		"variables": map[string]any{"prefix": ""},
	}

	queryJSON, err := json.Marshal(hierarchyQuery)
	if err != nil {
		return fmt.Errorf("failed to marshal hierarchy query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", gatewayURL, bytes.NewReader(queryJSON))
	if err != nil {
		return fmt.Errorf("failed to create hierarchy request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := httpClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		return fmt.Errorf("hierarchy request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("hierarchy returned status %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read hierarchy response: %w", err)
	}

	var hierarchyResp struct {
		Data struct {
			EntityIDHierarchy struct {
				Prefix        string `json:"prefix"`
				TotalEntities int    `json:"totalEntities"`
				Children      []struct {
					Prefix string `json:"prefix"`
					Name   string `json:"name"`
					Count  int    `json:"count"`
				} `json:"children"`
			} `json:"entityIdHierarchy"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(bodyBytes, &hierarchyResp); err != nil {
		return fmt.Errorf("failed to parse hierarchy response: %w", err)
	}

	if len(hierarchyResp.Errors) > 0 {
		return fmt.Errorf("hierarchy GraphQL error: %s", hierarchyResp.Errors[0].Message)
	}

	hierarchy := hierarchyResp.Data.EntityIDHierarchy

	result.Metrics["hierarchy_total_entities"] = hierarchy.TotalEntities
	result.Metrics["hierarchy_children_count"] = len(hierarchy.Children)
	result.Metrics["hierarchy_latency_ms"] = latency.Milliseconds()

	// Validate we found entities
	if hierarchy.TotalEntities == 0 {
		result.Details["entityid_hierarchy_test"] = map[string]any{
			"prefix":         "",
			"total_entities": 0,
			"error":          "No entities found in hierarchy",
		}
		return fmt.Errorf("entityIdHierarchy returned 0 entities")
	}

	// Validate we have at least one child level (org level should have platforms)
	if len(hierarchy.Children) == 0 {
		result.Details["entityid_hierarchy_test"] = map[string]any{
			"prefix":         "",
			"total_entities": hierarchy.TotalEntities,
			"error":          "No children found at root level",
		}
		return fmt.Errorf("entityIdHierarchy returned no children at root level")
	}

	// Collect child info for logging
	childInfo := make([]map[string]any, len(hierarchy.Children))
	for i, child := range hierarchy.Children {
		childInfo[i] = map[string]any{
			"prefix": child.Prefix,
			"name":   child.Name,
			"count":  child.Count,
		}
	}

	result.Details["entityid_hierarchy_test"] = map[string]any{
		"prefix":         "",
		"total_entities": hierarchy.TotalEntities,
		"children":       childInfo,
		"latency_ms":     latency.Milliseconds(),
		"message":        fmt.Sprintf("Hierarchy query successful: %d entities across %d org-level children", hierarchy.TotalEntities, len(hierarchy.Children)),
	}

	return nil
}

// executeTestEntitiesByPrefix validates the entitiesByPrefix GraphQL query.
// This tests that entities can be queried by EntityID prefix.
// EntityID prefix query is a Tier 0 capability that runs on ALL tiers.
func (s *TieredScenario) executeTestEntitiesByPrefix(ctx context.Context, result *Result) error {
	gatewayURL := s.config.GraphQLURL
	httpClient := &http.Client{Timeout: 10 * time.Second}

	// Test: Query entities by prefix (all temperature sensors)
	// entitiesByPrefix returns [Entity] - an array of full entity objects
	prefix := "c360.logistics.environmental.sensor.temperature"
	prefixQuery := map[string]any{
		"query": `query($prefix: String!, $limit: Int) {
			entitiesByPrefix(prefix: $prefix, limit: $limit) {
				id
			}}`,
		"variables": map[string]any{"prefix": prefix, "limit": 100},
	}

	queryJSON, err := json.Marshal(prefixQuery)
	if err != nil {
		return fmt.Errorf("failed to marshal prefix query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", gatewayURL, bytes.NewReader(queryJSON))
	if err != nil {
		return fmt.Errorf("failed to create prefix request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := httpClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		return fmt.Errorf("prefix request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("prefix query returned status %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read prefix response: %w", err)
	}

	var prefixResp struct {
		Data struct {
			EntitiesByPrefix []struct {
				ID string `json:"id"`
			} `json:"entitiesByPrefix"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(bodyBytes, &prefixResp); err != nil {
		return fmt.Errorf("failed to parse prefix response: %w", err)
	}

	if len(prefixResp.Errors) > 0 {
		return fmt.Errorf("prefix query GraphQL error: %s", prefixResp.Errors[0].Message)
	}

	entities := prefixResp.Data.EntitiesByPrefix
	totalCount := len(entities)

	result.Metrics["prefix_query_total_count"] = totalCount
	result.Metrics["prefix_query_returned"] = totalCount
	result.Metrics["prefix_query_latency_ms"] = latency.Milliseconds()

	// We expect at least 1 temperature sensor from the test data
	if totalCount == 0 {
		result.Details["entities_by_prefix_test"] = map[string]any{
			"prefix":      prefix,
			"total_count": 0,
			"error":       "No entities found for temperature sensor prefix",
		}
		return fmt.Errorf("entitiesByPrefix returned 0 entities for prefix %s", prefix)
	}

	// Verify all returned entity IDs match the prefix
	for _, entity := range entities {
		if !strings.HasPrefix(entity.ID, prefix) {
			result.Details["entities_by_prefix_test"] = map[string]any{
				"prefix":    prefix,
				"entity_id": entity.ID,
				"error":     "Entity ID does not match prefix",
			}
			return fmt.Errorf("entity %s does not match prefix %s", entity.ID, prefix)
		}
	}

	result.Details["entities_by_prefix_test"] = map[string]any{
		"prefix":      prefix,
		"total_count": totalCount,
		"returned":    totalCount,
		"truncated":   false, // Array response doesn't indicate truncation
		"latency_ms":  latency.Milliseconds(),
		"message":     fmt.Sprintf("Prefix query successful: found %d temperature sensors", totalCount),
	}

	return nil
}

// executeTestSpatialQuery validates spatial index queries via GraphQL.
// Tests that entities can be found using bounding box search.
// Spatial query is a Tier 0 capability that runs on ALL tiers.
func (s *TieredScenario) executeTestSpatialQuery(ctx context.Context, result *Result) error {
	gatewayURL := s.config.GraphQLURL
	httpClient := &http.Client{Timeout: 10 * time.Second}

	// Test data is in SF Bay Area: ~37.77, -122.42
	// Create bounding box that should include all test sensors
	spatialQuery := map[string]any{
		"query": `query($north: Float!, $south: Float!, $east: Float!, $west: Float!, $limit: Int) {
			spatialSearch(north: $north, south: $south, east: $east, west: $west, limit: $limit) {
				id type
			}}`,
		"variables": map[string]any{
			"north": 37.78,
			"south": 37.77,
			"east":  -122.41,
			"west":  -122.43,
			"limit": 100,
		},
	}

	queryJSON, err := json.Marshal(spatialQuery)
	if err != nil {
		return fmt.Errorf("failed to marshal spatial query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", gatewayURL, bytes.NewReader(queryJSON))
	if err != nil {
		return fmt.Errorf("failed to create spatial request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := httpClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		return fmt.Errorf("spatial request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("spatial query returned status %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read spatial response: %w", err)
	}

	var spatialResp struct {
		Data struct {
			SpatialSearch []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"spatialSearch"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(bodyBytes, &spatialResp); err != nil {
		return fmt.Errorf("failed to parse spatial response: %w", err)
	}

	if len(spatialResp.Errors) > 0 {
		return fmt.Errorf("spatial query GraphQL error: %s", spatialResp.Errors[0].Message)
	}

	entityCount := len(spatialResp.Data.SpatialSearch)
	result.Metrics["spatial_query_count"] = entityCount
	result.Metrics["spatial_query_latency_ms"] = latency.Milliseconds()

	// Collect entity IDs for logging
	entityIDs := make([]string, entityCount)
	for i, e := range spatialResp.Data.SpatialSearch {
		entityIDs[i] = e.ID
	}

	result.Details["spatial_query_test"] = map[string]any{
		"bounds": map[string]float64{
			"north": 37.78, "south": 37.77, "east": -122.41, "west": -122.43,
		},
		"entities_found": entityCount,
		"entity_ids":     entityIDs,
		"latency_ms":     latency.Milliseconds(),
		"message":        fmt.Sprintf("Spatial query returned %d entities within bounding box", entityCount),
	}

	// Note: We don't require a minimum count since spatial indexing depends on
	// the processor creating geo.location.* triples. If count is 0, it's a warning.
	if entityCount == 0 {
		result.Warnings = append(result.Warnings, "Spatial query returned 0 entities - check if geo triples are being indexed")
	}

	return nil
}

// executeTestTemporalQuery validates temporal index queries via GraphQL.
// Tests that entities can be found using time range search.
// Temporal query is a Tier 0 capability that runs on ALL tiers.
func (s *TieredScenario) executeTestTemporalQuery(ctx context.Context, result *Result) error {
	gatewayURL := s.config.GraphQLURL
	httpClient := &http.Client{Timeout: 10 * time.Second}

	// Temporal index uses entity UpdatedAt (current time), not historical timestamps from test data.
	// Query for entities updated in the last hour to capture recently processed entities.
	now := time.Now().UTC()
	startTime := now.Add(-1 * time.Hour).Format(time.RFC3339)
	endTime := now.Add(1 * time.Hour).Format(time.RFC3339)

	temporalQuery := map[string]any{
		"query": `query($startTime: DateTime!, $endTime: DateTime!, $limit: Int) {
			temporalSearch(startTime: $startTime, endTime: $endTime, limit: $limit) {
				id type
			}}`,
		"variables": map[string]any{
			"startTime": startTime,
			"endTime":   endTime,
			"limit":     100,
		},
	}

	queryJSON, err := json.Marshal(temporalQuery)
	if err != nil {
		return fmt.Errorf("failed to marshal temporal query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", gatewayURL, bytes.NewReader(queryJSON))
	if err != nil {
		return fmt.Errorf("failed to create temporal request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := httpClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		return fmt.Errorf("temporal request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("temporal query returned status %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read temporal response: %w", err)
	}

	var temporalResp struct {
		Data struct {
			TemporalSearch []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"temporalSearch"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(bodyBytes, &temporalResp); err != nil {
		return fmt.Errorf("failed to parse temporal response: %w", err)
	}

	if len(temporalResp.Errors) > 0 {
		return fmt.Errorf("temporal query GraphQL error: %s", temporalResp.Errors[0].Message)
	}

	entityCount := len(temporalResp.Data.TemporalSearch)
	result.Metrics["temporal_query_count"] = entityCount
	result.Metrics["temporal_query_latency_ms"] = latency.Milliseconds()

	// Collect entity IDs for logging (limit to first 10 for brevity)
	maxDisplay := 10
	if entityCount < maxDisplay {
		maxDisplay = entityCount
	}
	entityIDs := make([]string, maxDisplay)
	for i := 0; i < maxDisplay; i++ {
		entityIDs[i] = temporalResp.Data.TemporalSearch[i].ID
	}

	result.Details["temporal_query_test"] = map[string]any{
		"time_range": map[string]string{
			"start": "2024-11-15T00:00:00Z",
			"end":   "2024-11-16T00:00:00Z",
		},
		"entities_found":    entityCount,
		"entity_ids_sample": entityIDs,
		"latency_ms":        latency.Milliseconds(),
		"message":           fmt.Sprintf("Temporal query returned %d entities within time range", entityCount),
	}

	// Note: We don't require a minimum count since temporal indexing depends on
	// entity UpdatedAt timestamps. If count is 0, it's a warning.
	if entityCount == 0 {
		result.Warnings = append(result.Warnings, "Temporal query returned 0 entities - check if temporal index is being populated")
	}

	return nil
}

// executeTestZoneRelationships validates zone-based relationship queries.
// Tests that querying a zone entity's incoming edges returns all sensors in that zone.
// This validates the geo.location.zone relationship triple indexing.
func (s *TieredScenario) executeTestZoneRelationships(ctx context.Context, result *Result) error {
	gatewayURL := s.config.GraphQLURL
	httpClient := &http.Client{Timeout: 10 * time.Second}

	// Zone entity ID format: {org}.{platform}.facility.zone.{zoneType}.{locationID}
	// From test data sensors.jsonl, "cold-storage-1" is a known location with default zone type "area"
	// The IoT processor generates: c360.logistics.facility.zone.area.cold-storage-1
	zoneEntityID := "c360.logistics.facility.zone.area.cold-storage-1"

	// Query incoming relationships to the zone entity
	relationshipsQuery := map[string]any{
		"query": `query($entityId: ID!, $direction: RelationshipDirection) {
			relationships(entityId: $entityId, direction: $direction) {
				fromEntityId toEntityId edgeType
			}}`,
		"variables": map[string]any{
			"entityId":  zoneEntityID,
			"direction": "INCOMING",
		},
	}

	queryJSON, err := json.Marshal(relationshipsQuery)
	if err != nil {
		return fmt.Errorf("failed to marshal relationships query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", gatewayURL, bytes.NewReader(queryJSON))
	if err != nil {
		return fmt.Errorf("failed to create relationships request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := httpClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		return fmt.Errorf("relationships request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("relationships query returned status %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read relationships response: %w", err)
	}

	var relationshipsResp struct {
		Data struct {
			Relationships []struct {
				FromEntityID string `json:"fromEntityId"`
				ToEntityID   string `json:"toEntityId"`
				EdgeType     string `json:"edgeType"`
			} `json:"relationships"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(bodyBytes, &relationshipsResp); err != nil {
		return fmt.Errorf("failed to parse relationships response: %w", err)
	}

	if len(relationshipsResp.Errors) > 0 {
		return fmt.Errorf("relationships query GraphQL error: %s", relationshipsResp.Errors[0].Message)
	}

	relationships := relationshipsResp.Data.Relationships
	relationshipCount := len(relationships)
	result.Metrics["zone_relationships_count"] = relationshipCount
	result.Metrics["zone_relationships_latency_ms"] = latency.Milliseconds()

	// Count relationships by edge type
	edgeTypeCounts := make(map[string]int)
	sensorIDs := []string{}
	for _, rel := range relationships {
		edgeTypeCounts[rel.EdgeType]++
		// Collect sensor IDs (entities pointing to this zone)
		if rel.EdgeType == "geo.location.zone" {
			sensorIDs = append(sensorIDs, rel.FromEntityID)
		}
	}

	result.Details["zone_relationships_test"] = map[string]any{
		"zone_entity_id":      zoneEntityID,
		"total_relationships": relationshipCount,
		"edge_type_counts":    edgeTypeCounts,
		"sensor_ids":          sensorIDs,
		"latency_ms":          latency.Milliseconds(),
		"message":             fmt.Sprintf("Zone %s has %d incoming relationships", zoneEntityID, relationshipCount),
	}

	// Note: We don't require a minimum count since this depends on the zone existing
	// and sensors being in that zone. If count is 0, it's a warning.
	if relationshipCount == 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Zone %s has 0 incoming relationships - check if zone triples are being indexed", zoneEntityID))
	}

	return nil
}

// executeTestPathRAGBoundary validates PathRAG respects maxNodes limit
// PathRAG is a Tier 0 capability that runs on ALL tiers.
func (s *TieredScenario) executeTestPathRAGBoundary(ctx context.Context, result *Result) error {
	startEntity := s.getPathRAGSensorEntity()
	gatewayURL := s.config.GraphQLURL

	// Query with tight bounds to verify maxNodes is respected
	// Note: maxNodes=5 accounts for hierarchy container edges in statistical/semantic tiers
	// where temp-sensor-001 → skos:broader → temperature.group.container → skos:narrower → siblings
	graphqlQuery := map[string]any{
		"query": `query($startEntity: ID!, $maxDepth: Int, $maxNodes: Int) {
			pathSearch(startEntity: $startEntity, maxDepth: $maxDepth, maxNodes: $maxNodes) {
				entities { id type score } paths { from predicate to } truncated
			}}`,
		"variables": map[string]any{"startEntity": startEntity, "maxDepth": 2, "maxNodes": 5},
	}

	queryJSON, err := json.Marshal(graphqlQuery)
	if err != nil {
		return fmt.Errorf("failed to marshal PathRAG boundary query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", gatewayURL, bytes.NewReader(queryJSON))
	if err != nil {
		return fmt.Errorf("failed to create PathRAG boundary request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("PathRAG boundary request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("PathRAG boundary returned status %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read PathRAG boundary response: %w", err)
	}

	var graphqlResp pathRAGResponse
	if err := json.Unmarshal(bodyBytes, &graphqlResp); err != nil {
		return fmt.Errorf("failed to parse PathRAG boundary response: %w", err)
	}

	if len(graphqlResp.Errors) > 0 {
		return fmt.Errorf("PathRAG boundary GraphQL error: %s", graphqlResp.Errors[0].Message)
	}

	// Verify result count respects maxNodes limit
	// Note: maxNodes refers to traversal nodes, but start entity is always included
	// So total entities = start entity (1) + up to maxNodes traversed nodes
	entityCount := len(graphqlResp.Data.PathSearch.Entities)
	maxNodes := 5
	expectedMax := maxNodes + 1 // +1 for start entity which is always included

	result.Metrics["pathrag_boundary_entities"] = entityCount
	result.Metrics["pathrag_boundary_max_nodes"] = maxNodes
	result.Details["pathrag_boundary_test"] = map[string]any{
		"entities_returned":     entityCount,
		"max_nodes_limit":       maxNodes,
		"expected_max_total":    expectedMax,
		"respected_limit":       entityCount <= expectedMax,
		"includes_start_entity": true,
	}

	if entityCount > expectedMax {
		return fmt.Errorf("PathRAG maxNodes violated: got %d entities, expected <= %d (maxNodes=%d + start entity)", entityCount, expectedMax, maxNodes)
	}

	return nil
}

// executeTestEntityByAlias validates the entityByAlias GraphQL query.
// This tests REAL alias resolution via graph-index's ALIAS_INDEX using sensor serial numbers.
//
// The IoT sensor processor creates triples with predicate "iot.sensor.serial" which is
// registered as an alias predicate in the vocabulary system. graph-index uses
// vocabulary.DiscoverAliasPredicates() to detect these and index them in ALIAS_INDEX.
//
// Test data sensors.jsonl has sensors with serial numbers like "SN-TEMP-2024-001".
// This test queries by serial number and verifies it resolves to the correct entity.
//
// This is a Tier 0 capability that runs on ALL tiers (alias lookup is structural).
func (s *TieredScenario) executeTestEntityByAlias(ctx context.Context, result *Result) error {
	gatewayURL := s.config.GraphQLURL
	httpClient := &http.Client{Timeout: 10 * time.Second}

	// Test REAL alias resolution using sensor serial number
	// From testdata/semantic/sensors.jsonl: temp-sensor-001 has serial "SN-TEMP-2024-001"
	// Expected entity ID: c360.logistics.environmental.sensor.temperature.temp-sensor-001
	serialNumber := "SN-TEMP-2024-001"
	expectedEntityID := "c360.logistics.environmental.sensor.temperature.temp-sensor-001"

	aliasQuery := map[string]any{
		"query": `query($aliasOrID: String!) {
			entityByAlias(aliasOrID: $aliasOrID) {
				id
				type
				properties
			}
		}`,
		"variables": map[string]any{"aliasOrID": serialNumber},
	}

	queryJSON, err := json.Marshal(aliasQuery)
	if err != nil {
		return fmt.Errorf("failed to marshal entityByAlias query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", gatewayURL, bytes.NewReader(queryJSON))
	if err != nil {
		return fmt.Errorf("failed to create entityByAlias request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := httpClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		return fmt.Errorf("entityByAlias request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("entityByAlias returned status %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read entityByAlias response: %w", err)
	}

	var aliasResp struct {
		Data struct {
			EntityByAlias *struct {
				ID         string         `json:"id"`
				Type       string         `json:"type"`
				Properties map[string]any `json:"properties"`
			} `json:"entityByAlias"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(bodyBytes, &aliasResp); err != nil {
		return fmt.Errorf("failed to parse entityByAlias response: %w", err)
	}

	if len(aliasResp.Errors) > 0 {
		return fmt.Errorf("entityByAlias GraphQL error: %s", aliasResp.Errors[0].Message)
	}

	entity := aliasResp.Data.EntityByAlias

	result.Metrics["entity_by_alias_latency_ms"] = latency.Milliseconds()

	if entity == nil {
		// Alias not resolved - this is a HARD failure since we're testing real alias resolution
		result.Details["entity_by_alias_validation"] = map[string]any{
			"success":            false,
			"serial_number":      serialNumber,
			"expected_entity_id": expectedEntityID,
			"latency_ms":         latency.Milliseconds(),
			"message":            fmt.Sprintf("Alias resolution FAILED: serial number %s not found in ALIAS_INDEX", serialNumber),
		}
		return fmt.Errorf("entityByAlias failed to resolve serial number %s - alias not indexed (check iot.sensor.serial predicate indexing)", serialNumber)
	}

	// Validate the returned entity matches expected
	if entity.ID != expectedEntityID {
		result.Details["entity_by_alias_validation"] = map[string]any{
			"success":            false,
			"serial_number":      serialNumber,
			"expected_entity_id": expectedEntityID,
			"actual_entity_id":   entity.ID,
			"latency_ms":         latency.Milliseconds(),
			"message":            fmt.Sprintf("Alias resolved to wrong entity: expected %s, got %s", expectedEntityID, entity.ID),
		}
		return fmt.Errorf("entityByAlias resolved to wrong entity: expected %s, got %s", expectedEntityID, entity.ID)
	}

	result.Details["entity_by_alias_validation"] = map[string]any{
		"success":            true,
		"serial_number":      serialNumber,
		"expected_entity_id": expectedEntityID,
		"actual_entity_id":   entity.ID,
		"entity_type":        entity.Type,
		"latency_ms":         latency.Milliseconds(),
		"alias_resolved":     true, // Real alias resolution worked!
		"message":            fmt.Sprintf("Alias resolution SUCCESS: %s → %s", serialNumber, entity.ID),
	}

	return nil
}

// globalSearchResponse represents the parsed GraphQL response for globalSearch queries
type globalSearchResponse struct {
	Data struct {
		GlobalSearch struct {
			Entities []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"entities"`
			CommunitySummaries []struct {
				CommunityID string  `json:"communityId"`
				Summary     string  `json:"summary"`
				Relevance   float64 `json:"relevance"`
			} `json:"communitySummaries"`
			Count int `json:"count"`
		} `json:"globalSearch"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// sendNLQuery sends a natural language query through globalSearch and returns the response.
// This tests the classifier → strategy routing → filtered results pipeline.
func (s *TieredScenario) sendNLQuery(ctx context.Context, query string) (*globalSearchResponse, time.Duration, error) {
	gatewayURL := s.config.GraphQLURL
	httpClient := &http.Client{Timeout: 10 * time.Second}

	nlQuery := map[string]any{
		"query": `query($query: String!, $maxCommunities: Int) {
			globalSearch(query: $query, maxCommunities: $maxCommunities) {
				entities { id type }
				communitySummaries { communityId summary relevance }
				count
			}
		}`,
		"variables": map[string]any{
			"query":          query,
			"maxCommunities": 10,
		},
	}

	queryJSON, err := json.Marshal(nlQuery)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to marshal NL query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", gatewayURL, bytes.NewReader(queryJSON))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create NL query request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := httpClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		return nil, latency, fmt.Errorf("NL query request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, latency, fmt.Errorf("NL query returned status %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, latency, fmt.Errorf("failed to read NL query response: %w", err)
	}

	var graphqlResp globalSearchResponse
	if err := json.Unmarshal(bodyBytes, &graphqlResp); err != nil {
		return nil, latency, fmt.Errorf("failed to parse NL query response: %w", err)
	}

	if len(graphqlResp.Errors) > 0 {
		return nil, latency, fmt.Errorf("NL query GraphQL error: %s", graphqlResp.Errors[0].Message)
	}

	return &graphqlResp, latency, nil
}

// executeTestNLPathIntent validates that NL queries with path intent
// are properly routed through the classifier to PathRAG.
// Tests queries like "What is related to temp-sensor-001?" and "sensors in zone-cold-storage-1".
// This is a Tier 0 capability - graph traversal works without embeddings.
func (s *TieredScenario) executeTestNLPathIntent(ctx context.Context, result *Result) error {
	testCases := []struct {
		name          string
		query         string
		expectResults bool   // Whether we expect any results
		description   string // What this tests
	}{
		{
			name:          "path_intent_related_to",
			query:         "What is related to temp-sensor-001?",
			expectResults: true,
			description:   "Tests path intent with 'related to' + entity ID extraction",
		},
		{
			name:          "path_intent_connected_to",
			query:         "Show everything connected to humid-sensor-001",
			expectResults: true,
			description:   "Tests path intent with 'connected to' + entity ID extraction",
		},
		{
			name:          "zone_entity",
			query:         "What is related to cold-storage-1",
			expectResults: true,
			description:   "Tests path intent starting from zone entity (now created by IoT processor)",
		},
	}

	allResults := make([]map[string]any, 0, len(testCases))
	passedCount := 0

	for _, tc := range testCases {
		resp, latency, err := s.sendNLQuery(ctx, tc.query)

		testResult := map[string]any{
			"name":           tc.name,
			"query":          tc.query,
			"description":    tc.description,
			"latency_ms":     latency.Milliseconds(),
			"expect_results": tc.expectResults,
		}

		if err != nil {
			testResult["success"] = false
			testResult["error"] = err.Error()
			allResults = append(allResults, testResult)
			continue
		}

		entityCount := len(resp.Data.GlobalSearch.Entities)
		testResult["entity_count"] = entityCount

		// Collect entity IDs
		entityIDs := make([]string, entityCount)
		for i, e := range resp.Data.GlobalSearch.Entities {
			entityIDs[i] = e.ID
		}
		testResult["entity_ids"] = entityIDs

		// Determine success based on whether we expected results
		success := (tc.expectResults && entityCount > 0) || (!tc.expectResults && entityCount == 0)
		testResult["success"] = success

		if success {
			passedCount++
			testResult["message"] = fmt.Sprintf("NL path intent query returned %d entities", entityCount)
		} else if tc.expectResults && entityCount == 0 {
			testResult["message"] = "Expected results but got none - path routing may not be working"
		}

		allResults = append(allResults, testResult)
	}

	result.Metrics["nl_path_intent_tests_passed"] = passedCount
	result.Metrics["nl_path_intent_tests_total"] = len(testCases)

	result.Details["nl_path_intent_test"] = map[string]any{
		"tests_passed": passedCount,
		"tests_total":  len(testCases),
		"test_results": allResults,
		"message":      fmt.Sprintf("NL path intent: %d/%d tests passed", passedCount, len(testCases)),
	}

	// Warn if no tests passed, but don't fail - this allows gradual rollout
	if passedCount == 0 {
		result.Warnings = append(result.Warnings,
			"NL path intent tests returned no results - classifier routing may need attention")
	}

	return nil
}

// executeTestNLTemporalIntent validates that NL queries with temporal intent
// are properly routed through the classifier and results are filtered by time.
// Tests queries like "What happened in the last hour?".
// This runs on statistical+ tiers where temporal filtering is meaningful with search results.
func (s *TieredScenario) executeTestNLTemporalIntent(ctx context.Context, result *Result) error {
	testCases := []struct {
		name          string
		query         string
		expectResults bool
		description   string
	}{
		{
			name:          "temporal_last_hour",
			query:         "What happened in the last hour?",
			expectResults: true, // Entities were just created, should be in last hour
			description:   "Tests temporal intent with 'last hour' extraction",
		},
		{
			name:          "temporal_today",
			query:         "Show events from today",
			expectResults: true, // Entities created today
			description:   "Tests temporal intent with 'today' extraction",
		},
	}

	allResults := make([]map[string]any, 0, len(testCases))
	passedCount := 0

	for _, tc := range testCases {
		resp, latency, err := s.sendNLQuery(ctx, tc.query)

		testResult := map[string]any{
			"name":           tc.name,
			"query":          tc.query,
			"description":    tc.description,
			"latency_ms":     latency.Milliseconds(),
			"expect_results": tc.expectResults,
		}

		if err != nil {
			testResult["success"] = false
			testResult["error"] = err.Error()
			allResults = append(allResults, testResult)
			continue
		}

		entityCount := len(resp.Data.GlobalSearch.Entities)
		testResult["entity_count"] = entityCount

		// Collect entity IDs (limit to first 10 for brevity)
		maxDisplay := 10
		if entityCount < maxDisplay {
			maxDisplay = entityCount
		}
		entityIDs := make([]string, maxDisplay)
		for i := 0; i < maxDisplay; i++ {
			entityIDs[i] = resp.Data.GlobalSearch.Entities[i].ID
		}
		testResult["entity_ids_sample"] = entityIDs

		// Determine success
		success := (tc.expectResults && entityCount > 0) || (!tc.expectResults && entityCount == 0)
		testResult["success"] = success

		if success {
			passedCount++
			testResult["message"] = fmt.Sprintf("NL temporal query returned %d entities", entityCount)
		} else if tc.expectResults && entityCount == 0 {
			testResult["message"] = "Expected results but got none - temporal filtering may be too restrictive"
		}

		allResults = append(allResults, testResult)
	}

	result.Metrics["nl_temporal_intent_tests_passed"] = passedCount
	result.Metrics["nl_temporal_intent_tests_total"] = len(testCases)

	result.Details["nl_temporal_intent_test"] = map[string]any{
		"tests_passed": passedCount,
		"tests_total":  len(testCases),
		"test_results": allResults,
		"message":      fmt.Sprintf("NL temporal intent: %d/%d tests passed", passedCount, len(testCases)),
	}

	// Warn if no tests passed
	if passedCount == 0 {
		result.Warnings = append(result.Warnings,
			"NL temporal intent tests returned no results - temporal filtering may need attention")
	}

	return nil
}

// === Predicate Query Tests ===

// predicateListResponse represents the GraphQL response for predicates query.
type predicateListResponse struct {
	Data struct {
		Predicates struct {
			Predicates []struct {
				Predicate   string `json:"predicate"`
				EntityCount int    `json:"entityCount"`
			} `json:"predicates"`
			Total int `json:"total"`
		} `json:"predicates"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// predicateStatsResponse represents the GraphQL response for predicateStats query.
type predicateStatsResponse struct {
	Data struct {
		PredicateStats struct {
			Predicate      string   `json:"predicate"`
			EntityCount    int      `json:"entityCount"`
			SampleEntities []string `json:"sampleEntities"`
		} `json:"predicateStats"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// compoundPredicateResponse represents the GraphQL response for compoundPredicateQuery.
type compoundPredicateResponse struct {
	Data struct {
		CompoundPredicateQuery struct {
			Entities []string `json:"entities"`
			Operator string   `json:"operator"`
			Matched  int      `json:"matched"`
		} `json:"compoundPredicateQuery"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// executeTestPredicateList validates the predicates GraphQL query.
// Tests that we can list all predicates in the graph with their entity counts.
func (s *TieredScenario) executeTestPredicateList(ctx context.Context, result *Result) error {
	gatewayURL := s.config.GraphQLURL

	graphqlQuery := map[string]any{
		"query": `{
			predicates {
				predicates { predicate entityCount }
				total
			}
		}`,
	}

	queryJSON, err := json.Marshal(graphqlQuery)
	if err != nil {
		return fmt.Errorf("failed to marshal predicates query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", gatewayURL, bytes.NewReader(queryJSON))
	if err != nil {
		return fmt.Errorf("failed to create predicates request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		return fmt.Errorf("predicates request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read predicates response: %w", err)
	}

	var predicatesResp predicateListResponse
	if err := json.Unmarshal(bodyBytes, &predicatesResp); err != nil {
		return fmt.Errorf("failed to parse predicates response: %w", err)
	}

	if len(predicatesResp.Errors) > 0 {
		return fmt.Errorf("predicates query error: %s", predicatesResp.Errors[0].Message)
	}

	predicateCount := len(predicatesResp.Data.Predicates.Predicates)
	total := predicatesResp.Data.Predicates.Total

	result.Metrics["predicate_list_count"] = predicateCount
	result.Metrics["predicate_list_total"] = total
	result.Metrics["predicate_list_latency_ms"] = latency.Milliseconds()

	// Build summary of predicates found
	predicateSummary := make([]map[string]any, 0, predicateCount)
	for _, p := range predicatesResp.Data.Predicates.Predicates {
		predicateSummary = append(predicateSummary, map[string]any{
			"predicate":    p.Predicate,
			"entity_count": p.EntityCount,
		})
	}

	result.Details["predicate_list_test"] = map[string]any{
		"predicate_count": predicateCount,
		"total":           total,
		"latency_ms":      latency.Milliseconds(),
		"predicates":      predicateSummary,
		"success":         predicateCount > 0,
		"message":         fmt.Sprintf("Found %d predicates in graph", predicateCount),
	}

	if predicateCount == 0 {
		result.Warnings = append(result.Warnings,
			"No predicates found - graph may be empty or PREDICATE_INDEX not populated")
	}

	return nil
}

// executeTestPredicateStats validates the predicateStats GraphQL query.
// Tests that we can get detailed stats for a specific predicate.
func (s *TieredScenario) executeTestPredicateStats(ctx context.Context, result *Result) error {
	gatewayURL := s.config.GraphQLURL

	// First, get a predicate to query stats for
	listQuery := map[string]any{
		"query": `{ predicates { predicates { predicate } } }`,
	}
	listJSON, _ := json.Marshal(listQuery)

	listReq, err := http.NewRequestWithContext(ctx, "POST", gatewayURL, bytes.NewReader(listJSON))
	if err != nil {
		return fmt.Errorf("failed to create predicate list request: %w", err)
	}
	listReq.Header.Set("Content-Type", "application/json")

	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to list predicates: %v", err))
		return nil
	}
	defer listResp.Body.Close()

	listBody, _ := io.ReadAll(listResp.Body)
	var predicatesResp predicateListResponse
	if err := json.Unmarshal(listBody, &predicatesResp); err != nil || len(predicatesResp.Data.Predicates.Predicates) == 0 {
		result.Warnings = append(result.Warnings, "No predicates available for stats test")
		return nil
	}

	// Pick the first predicate
	targetPredicate := predicatesResp.Data.Predicates.Predicates[0].Predicate

	// Query stats for this predicate
	statsQuery := map[string]any{
		"query": `query($predicate: String!, $sampleLimit: Int) {
			predicateStats(predicate: $predicate, sampleLimit: $sampleLimit) {
				predicate entityCount sampleEntities
			}
		}`,
		"variables": map[string]any{
			"predicate":   targetPredicate,
			"sampleLimit": 5,
		},
	}

	statsJSON, err := json.Marshal(statsQuery)
	if err != nil {
		return fmt.Errorf("failed to marshal predicateStats query: %w", err)
	}

	statsReq, err := http.NewRequestWithContext(ctx, "POST", gatewayURL, bytes.NewReader(statsJSON))
	if err != nil {
		return fmt.Errorf("failed to create predicateStats request: %w", err)
	}
	statsReq.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := http.DefaultClient.Do(statsReq)
	latency := time.Since(start)
	if err != nil {
		return fmt.Errorf("predicateStats request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read predicateStats response: %w", err)
	}

	var statsResp predicateStatsResponse
	if err := json.Unmarshal(bodyBytes, &statsResp); err != nil {
		return fmt.Errorf("failed to parse predicateStats response: %w", err)
	}

	if len(statsResp.Errors) > 0 {
		return fmt.Errorf("predicateStats query error: %s", statsResp.Errors[0].Message)
	}

	entityCount := statsResp.Data.PredicateStats.EntityCount
	sampleCount := len(statsResp.Data.PredicateStats.SampleEntities)

	result.Metrics["predicate_stats_entity_count"] = entityCount
	result.Metrics["predicate_stats_sample_count"] = sampleCount
	result.Metrics["predicate_stats_latency_ms"] = latency.Milliseconds()

	result.Details["predicate_stats_test"] = map[string]any{
		"predicate":       targetPredicate,
		"entity_count":    entityCount,
		"sample_count":    sampleCount,
		"sample_entities": statsResp.Data.PredicateStats.SampleEntities,
		"latency_ms":      latency.Milliseconds(),
		"success":         entityCount > 0,
		"message":         fmt.Sprintf("Predicate '%s' has %d entities", targetPredicate, entityCount),
	}

	return nil
}

// compoundQueryResult holds the result of a compound predicate query.
type compoundQueryResult struct {
	matched int
	latency time.Duration
}

// sendCompoundPredicateQuery executes a compound predicate query and returns the result.
func (s *TieredScenario) sendCompoundPredicateQuery(ctx context.Context, predicates []string, operator string) (*compoundQueryResult, error) {
	query := map[string]any{
		"query": `query($predicates: [String!]!, $operator: String!, $limit: Int) {
			compoundPredicateQuery(predicates: $predicates, operator: $operator, limit: $limit) {
				entities operator matched
			}
		}`,
		"variables": map[string]any{
			"predicates": predicates,
			"operator":   operator,
			"limit":      100,
		},
	}

	queryJSON, _ := json.Marshal(query)
	req, _ := http.NewRequestWithContext(ctx, "POST", s.config.GraphQLURL, bytes.NewReader(queryJSON))
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		return nil, fmt.Errorf("compound %s query failed: %w", operator, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result compoundPredicateResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse compound %s response: %w", operator, err)
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("compound %s query error: %s", operator, result.Errors[0].Message)
	}

	return &compoundQueryResult{
		matched: result.Data.CompoundPredicateQuery.Matched,
		latency: latency,
	}, nil
}

// executeTestPredicateCompound validates the compoundPredicateQuery GraphQL query.
// Tests AND/OR logic across multiple predicates.
func (s *TieredScenario) executeTestPredicateCompound(ctx context.Context, result *Result) error {
	// First, get predicates to use in compound query
	listQuery := map[string]any{
		"query": `{ predicates { predicates { predicate entityCount } } }`,
	}
	listJSON, _ := json.Marshal(listQuery)

	listReq, err := http.NewRequestWithContext(ctx, "POST", s.config.GraphQLURL, bytes.NewReader(listJSON))
	if err != nil {
		return fmt.Errorf("failed to create predicate list request: %w", err)
	}
	listReq.Header.Set("Content-Type", "application/json")

	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to list predicates: %v", err))
		return nil
	}
	defer listResp.Body.Close()

	listBody, _ := io.ReadAll(listResp.Body)
	var predicatesResp predicateListResponse
	if err := json.Unmarshal(listBody, &predicatesResp); err != nil || len(predicatesResp.Data.Predicates.Predicates) < 2 {
		result.Warnings = append(result.Warnings, "Not enough predicates for compound query test (need at least 2)")
		return nil
	}

	// Pick two predicates for testing
	pred1 := predicatesResp.Data.Predicates.Predicates[0].Predicate
	pred2 := predicatesResp.Data.Predicates.Predicates[1].Predicate
	predicates := []string{pred1, pred2}

	// Test OR query (union)
	orResult, err := s.sendCompoundPredicateQuery(ctx, predicates, "OR")
	if err != nil {
		return err
	}

	// Test AND query (intersection)
	andResult, err := s.sendCompoundPredicateQuery(ctx, predicates, "AND")
	if err != nil {
		return err
	}

	result.Metrics["predicate_compound_or_matched"] = orResult.matched
	result.Metrics["predicate_compound_and_matched"] = andResult.matched
	result.Metrics["predicate_compound_or_latency_ms"] = orResult.latency.Milliseconds()
	result.Metrics["predicate_compound_and_latency_ms"] = andResult.latency.Milliseconds()

	// Validate set theory: AND <= OR (intersection is subset of union)
	setTheoryValid := andResult.matched <= orResult.matched

	result.Details["predicate_compound_test"] = map[string]any{
		"predicates_tested": predicates,
		"or_matched":        orResult.matched,
		"and_matched":       andResult.matched,
		"or_latency_ms":     orResult.latency.Milliseconds(),
		"and_latency_ms":    andResult.latency.Milliseconds(),
		"set_theory_valid":  setTheoryValid,
		"success":           setTheoryValid,
		"message":           fmt.Sprintf("Compound query: OR=%d, AND=%d (set theory %v)", orResult.matched, andResult.matched, setTheoryValid),
	}

	if !setTheoryValid {
		return fmt.Errorf("set theory violation: AND (%d) > OR (%d)", andResult.matched, orResult.matched)
	}

	return nil
}
