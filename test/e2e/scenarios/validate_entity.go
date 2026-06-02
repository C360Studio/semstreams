// Package scenarios provides E2E test scenarios for SemStreams semantic processing
package scenarios

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semstreams/test/e2e/client"
)

// Entity validation functions for tiered E2E tests

// entityStabilizationResult contains the result of waiting for entity count to stabilize.
type entityStabilizationResult struct {
	FinalCount   int
	WaitDuration time.Duration
	Stabilized   bool
	TimedOut     bool
	UsedSSE      bool // true if SSE streaming was used, false if fell back to polling
}

// waitForEntityCountStabilization waits for entity count to reach expectedCount using SSE
// streaming for real-time KV bucket watching. Falls back to polling if SSE is unavailable.
//
// Returns the stabilization result including final count and whether stabilization succeeded.
func (s *TieredScenario) waitForEntityCountStabilization(ctx context.Context, expectedCount int) entityStabilizationResult {
	if s.natsClient == nil {
		return entityStabilizationResult{
			FinalCount:   0,
			WaitDuration: 0,
			Stabilized:   false,
			TimedOut:     false,
			UsedSSE:      false,
		}
	}

	// Use SSE-enabled wait function that counts SOURCE entities only (excludes containers)
	// Container entities are created by hierarchy inference and should not be counted
	// when waiting for testdata to fully load
	result := s.natsClient.WaitForSourceEntityCountSSE(
		ctx,
		expectedCount,
		s.config.ValidationTimeout,
		s.sseClient,
	)

	return entityStabilizationResult{
		FinalCount:   result.FinalCount,
		WaitDuration: result.WaitDuration,
		Stabilized:   result.Stabilized,
		TimedOut:     result.TimedOut,
		UsedSSE:      result.UsedSSE,
	}
}

// executeWaitForEntityStabilization waits for entity count to stabilize (structural tier).
// This is needed because structural tier doesn't wait for embeddings, so entities may still
// be processing when we start validation.
func (s *TieredScenario) executeWaitForEntityStabilization(ctx context.Context, result *Result) error {
	const expectedEntities = 74 // All tiers expect 74 entities from testdata/semantic/

	// Retry SSE initialization if it failed at startup.
	// ENTITY_STATES bucket is created by graph-ingest after data flows,
	// so SSE health check at test start fails. Now that data has been sent,
	// the bucket should exist and SSE can work.
	if s.sseClient == nil {
		fmt.Printf("  SSE client not available, retrying...\n")
		s.sseClient = client.NewSSEClient(s.config.ServiceManagerURL)
		if err := s.sseClient.Health(ctx); err != nil {
			fmt.Printf("  SSE retry failed: %v (falling back to polling)\n", err)
			s.sseClient = nil // Still not available, will use polling
		} else {
			fmt.Printf("  SSE retry succeeded\n")
		}
	} else {
		fmt.Printf("  SSE client available\n")
	}

	stabilization := s.waitForEntityCountStabilization(ctx, expectedEntities)

	result.Details["entity_stabilization"] = map[string]any{
		"final_count":   stabilization.FinalCount,
		"expected":      expectedEntities,
		"wait_duration": stabilization.WaitDuration.String(),
		"stabilized":    stabilization.Stabilized,
		"timed_out":     stabilization.TimedOut,
		"used_sse":      stabilization.UsedSSE,
	}

	// Fail the stage if entities didn't stabilize. Prior code only
	// warned-and-returned-nil when `TimedOut && FinalCount < expected`,
	// which left a silent-pass hole on the SSE path (which returns
	// `TimedOut: false` unconditionally even when sourceCount=0). That
	// hole let downstream stages (test-pathrag-document, etc.) fire
	// against an empty graph under Docker pressure and surface as
	// "entity not found" with no useful diagnosis. Caught in the
	// v1.0.0-beta.91 pre-tag e2e:semantic flake. Per
	// [[feedback_warning_not_fail_masks_integration_drift]], the
	// stabilization gate is exactly the case where warning is wrong.
	if !stabilization.Stabilized {
		return fmt.Errorf("entity stabilization failed: got %d, expected %d (timed_out=%v used_sse=%v wait_duration=%s)",
			stabilization.FinalCount, expectedEntities, stabilization.TimedOut, stabilization.UsedSSE,
			stabilization.WaitDuration)
	}

	return nil
}

// executeVerifyEntityCount validates that entities from test data files are indexed
// and detects potential data loss by comparing expected vs actual entity counts.
// This function polls until minimum entities are loaded to handle file loader timing.
func (s *TieredScenario) executeVerifyEntityCount(ctx context.Context, result *Result) error {
	if s.natsClient == nil {
		result.Warnings = append(result.Warnings, "NATS client not available, skipping entity count verification")
		return nil
	}

	minRequired := s.getMinRequiredEntities()
	criticalEntities := s.getCriticalEntities()

	// Poll until entities are loaded
	actualCount, pollCount, criticalFound, lastErr := s.pollForEntities(ctx, minRequired, criticalEntities)
	result.Metrics["entity_load_poll_count"] = pollCount

	// Check for failures
	if err := s.validateEntityLoadResult(actualCount, minRequired, pollCount, criticalFound, criticalEntities, lastErr); err != nil {
		return err
	}

	// Record metrics and details
	s.recordEntityMetrics(result, actualCount)
	return nil
}

func (s *TieredScenario) getMinRequiredEntities() int {
	switch s.config.Variant {
	case "structural":
		return StructuralMinEntities
	case "statistical":
		return StatisticalMinEntities
	case "semantic":
		return SemanticMinEntities
	default:
		return s.config.MinExpectedEntities
	}
}

// getCriticalEntities returns entity IDs that downstream stages will
// query by exact ID. The pollForEntities gate (executeVerifyEntityCount)
// waits until ALL of these exist in ENTITY_STATES before proceeding,
// closing the cold-start race the v1.0.0-beta.91 sweep surfaced:
// testdata loads 99 entities; the bare `count >= 74` stabilization
// gate could trigger before file-loader processed every file (e.g.
// maintenance.jsonl might load last, after the count threshold is
// hit by sensors + observations + documents alone). Subsequent
// PathRAG test stages then queried `maint-001` and got "not found"
// despite the count gate passing. Each PathRAG test entity below is
// queried by exact ID at stage 12+; missing any of them = stage 14
// flake. Sensor and document entities included for both PathRAG
// shapes (sensor + document).
func (s *TieredScenario) getCriticalEntities() []string {
	switch s.config.Variant {
	case "structural":
		return []string{
			"c360.logistics.environmental.sensor.temperature.temp-sensor-001",
			"c360.logistics.maintenance.work.completed.maint-001",
		}
	case "statistical", "semantic":
		return []string{
			"c360.logistics.content.document.operations.doc-ops-001",
			"c360.logistics.environmental.sensor.temperature.temp-sensor-001",
			"c360.logistics.maintenance.work.completed.maint-001",
		}
	default:
		return []string{
			"c360.logistics.content.document.operations.doc-ops-001",
			"c360.logistics.environmental.sensor.temperature.temp-sensor-001",
			"c360.logistics.maintenance.work.completed.maint-001",
		}
	}
}

func (s *TieredScenario) pollForEntities(ctx context.Context, minRequired int, criticalEntities []string) (int, int, bool, error) {
	var actualCount int
	var lastErr error
	var criticalFound bool

	deadline := time.Now().Add(s.config.ValidationTimeout)
	pollCount := 0

	for time.Now().Before(deadline) {
		var err error
		actualCount, err = s.natsClient.CountEntities(ctx)
		if err != nil {
			lastErr = err
			time.Sleep(s.config.PollInterval)
			pollCount++
			continue
		}

		if actualCount >= minRequired {
			criticalFound = s.verifyCriticalEntities(ctx, criticalEntities)
			if criticalFound {
				break
			}
		}

		time.Sleep(s.config.PollInterval)
		pollCount++
	}
	return actualCount, pollCount, criticalFound, lastErr
}

func (s *TieredScenario) verifyCriticalEntities(ctx context.Context, criticalEntities []string) bool {
	for _, entityID := range criticalEntities {
		if _, err := s.natsClient.GetEntity(ctx, entityID); err != nil {
			return false
		}
	}
	return true
}

func (s *TieredScenario) validateEntityLoadResult(actualCount, minRequired, pollCount int, criticalFound bool, criticalEntities []string, lastErr error) error {
	if actualCount < minRequired {
		if lastErr != nil {
			return fmt.Errorf("entity loading timeout: got %d, need %d after %d polls (last error: %v)",
				actualCount, minRequired, pollCount, lastErr)
		}
		return fmt.Errorf("entity loading timeout: got %d, need %d after %d polls (waited %v)",
			actualCount, minRequired, pollCount, s.config.ValidationTimeout)
	}
	if !criticalFound {
		return fmt.Errorf("critical entities not found after %d polls: %v", pollCount, criticalEntities)
	}
	return nil
}

func (s *TieredScenario) recordEntityMetrics(result *Result, actualCount int) {
	// All tiers use testdata/semantic/*.jsonl (74 unique entities)
	expectedFromTestData := 74
	expectedFromUDP := 0
	totalExpected := expectedFromTestData + expectedFromUDP

	var dataLossPercent float64
	if totalExpected > 0 {
		dataLossPercent = 100.0 * float64(totalExpected-actualCount) / float64(totalExpected)
		if dataLossPercent < 0 {
			dataLossPercent = 0
		}
	}

	result.Metrics["entity_count"] = actualCount
	result.Metrics["expected_from_testdata"] = expectedFromTestData
	result.Metrics["expected_from_udp"] = expectedFromUDP
	result.Metrics["total_expected_entities"] = totalExpected
	result.Metrics["min_expected_entities"] = s.config.MinExpectedEntities
	result.Metrics["data_loss_percent"] = dataLossPercent

	dataLossThreshold := 10.0
	if dataLossPercent > dataLossThreshold {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Data loss detected: %.1f%% (expected %d, got %d)",
				dataLossPercent, totalExpected, actualCount))
	}
	if actualCount < s.config.MinExpectedEntities {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Entity count %d is below minimum expected %d", actualCount, s.config.MinExpectedEntities))
	}

	result.Details["entity_count_verification"] = map[string]any{
		"actual_count":        actualCount,
		"expected_from_data":  expectedFromTestData,
		"expected_from_udp":   expectedFromUDP,
		"total_expected":      totalExpected,
		"min_expected":        s.config.MinExpectedEntities,
		"data_loss_percent":   dataLossPercent,
		"meets_minimum":       actualCount >= s.config.MinExpectedEntities,
		"data_loss_threshold": dataLossThreshold,
		"data_loss_detected":  dataLossPercent > dataLossThreshold,
		"message":             fmt.Sprintf("Found %d entities (expected %d, loss: %.1f%%)", actualCount, totalExpected, dataLossPercent),
	}
}

// executeVerifyEntityRetrieval validates that specific known entities can be retrieved
func (s *TieredScenario) executeVerifyEntityRetrieval(ctx context.Context, result *Result) error {
	if s.natsClient == nil {
		result.Warnings = append(result.Warnings, "NATS client not available, skipping entity retrieval verification")
		return nil
	}

	// Test entities from test data files
	// These are fully-qualified entity IDs after processing with org_id=c360, platform=logistics
	// Format: {org}.{platform}.{domain}.{system}.{category/status/severity}.{id}
	testEntities := []struct {
		id           string
		expectedType string
		source       string
	}{
		{"c360.logistics.content.document.operations.doc-ops-001", "document", "documents.jsonl"},
		{"c360.logistics.content.document.quality.doc-quality-001", "document", "documents.jsonl"},
		{"c360.logistics.maintenance.work.completed.maint-001", "maintenance", "maintenance.jsonl"},
		{"c360.logistics.observation.record.high.obs-001", "observation", "observations.jsonl"},
		{"c360.logistics.sensor.document.temperature.sensor-temp-001", "sensor_doc", "sensor_docs.jsonl"},
	}

	foundEntities := 0
	missingEntities := []string{}
	entityDetails := make(map[string]any)

	for _, te := range testEntities {
		entity, err := s.natsClient.GetEntity(ctx, te.id)
		if err != nil {
			missingEntities = append(missingEntities, te.id)
			entityDetails[te.id] = map[string]any{
				"found":         false,
				"error":         err.Error(),
				"expected_type": te.expectedType,
				"source":        te.source,
			}
			continue
		}

		foundEntities++
		entityDetails[te.id] = map[string]any{
			"found":         true,
			"actual_type":   entity.Type,
			"expected_type": te.expectedType,
			"source":        te.source,
		}
	}

	result.Metrics["entities_retrieved"] = foundEntities
	result.Metrics["entities_missing"] = len(missingEntities)

	result.Details["entity_retrieval_verification"] = map[string]any{
		"tested":   len(testEntities),
		"found":    foundEntities,
		"missing":  missingEntities,
		"entities": entityDetails,
		"message":  fmt.Sprintf("Retrieved %d/%d test entities", foundEntities, len(testEntities)),
	}

	// Log as warning if some entities missing but don't fail
	if len(missingEntities) > 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Missing entities: %v", missingEntities))
	}

	return nil
}

// executeValidateEntityStructure validates entity data structure integrity
func (s *TieredScenario) executeValidateEntityStructure(ctx context.Context, result *Result) error {
	if s.natsClient == nil {
		result.Warnings = append(result.Warnings, "NATS client not available, skipping entity structure validation")
		return nil
	}

	// Sample up to 5 entities for structure validation
	entities, err := s.natsClient.GetEntitySample(ctx, 5)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to get entity sample: %v", err))
		return nil
	}

	if len(entities) == 0 {
		result.Warnings = append(result.Warnings, "No entities available for structure validation")
		return nil
	}

	validatedCount := 0
	validationErrors := []string{}
	entityDetails := make(map[string]any)

	for _, entity := range entities {
		entityValid := true
		issues := []string{}

		// Validate ID format (non-empty, should have dot-separated segments)
		if entity.ID == "" {
			issues = append(issues, "empty ID")
			entityValid = false
		} else if !strings.Contains(entity.ID, ".") {
			issues = append(issues, "ID missing expected format (no dot separators)")
			entityValid = false
		}

		// Validate Triples (should have at least one triple)
		if len(entity.Triples) == 0 {
			issues = append(issues, "no triples")
			entityValid = false
		} else {
			// Validate triple structure
			for i, triple := range entity.Triples {
				if triple.Subject == "" {
					issues = append(issues, fmt.Sprintf("triple[%d]: empty subject", i))
					entityValid = false
				}
				if triple.Predicate == "" {
					issues = append(issues, fmt.Sprintf("triple[%d]: empty predicate", i))
					entityValid = false
				}
			}
		}

		// Validate Version (should be positive)
		if entity.Version <= 0 {
			issues = append(issues, fmt.Sprintf("invalid version: %d", entity.Version))
			entityValid = false
		}

		// Validate UpdatedAt (should be non-empty and parseable if present)
		if entity.UpdatedAt != "" {
			// Try to parse as RFC3339 or similar format
			if _, err := time.Parse(time.RFC3339, entity.UpdatedAt); err != nil {
				// Try alternate format
				if _, err := time.Parse(time.RFC3339Nano, entity.UpdatedAt); err != nil {
					issues = append(issues, fmt.Sprintf("invalid timestamp format: %s", entity.UpdatedAt))
					// Don't fail validation for timestamp format issues
				}
			}
		}

		if entityValid {
			validatedCount++
		} else {
			validationErrors = append(validationErrors, fmt.Sprintf("%s: %v", entity.ID, issues))
		}

		entityDetails[entity.ID] = map[string]any{
			"valid":        entityValid,
			"issues":       issues,
			"triple_count": len(entity.Triples),
			"version":      entity.Version,
			"has_updated":  entity.UpdatedAt != "",
		}
	}

	result.Metrics["entities_validated"] = validatedCount
	result.Metrics["entities_sampled"] = len(entities)
	result.Metrics["validation_errors"] = len(validationErrors)

	result.Details["entity_structure_validation"] = map[string]any{
		"sampled":           len(entities),
		"validated":         validatedCount,
		"errors":            validationErrors,
		"entities":          entityDetails,
		"validation_passed": len(validationErrors) == 0,
		"message":           fmt.Sprintf("Validated %d/%d sampled entities", validatedCount, len(entities)),
	}

	if len(validationErrors) > 0 {
		// Fail loudly instead of warning. Pre-fix, this check silently
		// logged ~3 errors per run for 6+ weeks because stub entities
		// were written with Version=0 (see graph-ingest stub creation
		// fix). Now that the invariant holds, any validation failure is
		// a genuine regression and should block merge.
		return fmt.Errorf("entity structure validation failed for %d of %d sampled entities: %v",
			len(validationErrors), len(entities), validationErrors)
	}

	return nil
}
