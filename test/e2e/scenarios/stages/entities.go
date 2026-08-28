package stages

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semstreams/graph/readiness"
	"github.com/c360studio/semstreams/test/e2e/client"
)

// Entity count expectations by variant
const (
	StructuralMinEntities  = 50 // All tiers now use testdata/semantic/*.jsonl - 74 unique entities
	StatisticalMinEntities = 50 // testdata/semantic/*.jsonl - 74 unique entities
	SemanticMinEntities    = 50 // Same as statistical
)

// EntityVerifier handles entity verification stages
type EntityVerifier struct {
	NATSClient        *client.NATSValidationClient
	Variant           string
	ValidationTimeout time.Duration
	PollInterval      time.Duration
	MinExpectedConfig int // From config, used as fallback
}

// EntityCountResult contains the results of entity count verification
type EntityCountResult struct {
	ActualCount      int      `json:"actual_count"`
	ExpectedFromData int      `json:"expected_from_data"`
	ExpectedFromUDP  int      `json:"expected_from_udp"`
	TotalExpected    int      `json:"total_expected"`
	MinExpected      int      `json:"min_expected"`
	DataLossPercent  float64  `json:"data_loss_percent"`
	MeetsMinimum     bool     `json:"meets_minimum"`
	CriticalFound    bool     `json:"critical_found"`
	PollCount        int      `json:"poll_count"`
	Warnings         []string `json:"warnings,omitempty"`
}

// ReadinessKeys are the producers this stage depends on before it can call the graph
// settled. The list is the CONSUMER'S, deliberately: there is no framework-declared
// "all producers" set, because the producer set is deployment-dependent and a
// framework default would either make this stage defer on an outage it does not care
// about or silently omit one it does.
//
// graph-index is included because every tier this stage runs on wires it; graph-ingest
// and rule are the two producers this change adds.
//
// VERIFIED 2026-07-30 against every config this stage runs on (e2e-structural,
// structural, statistical, semantic, semantic-8b, semantic-frontier): all three
// components are present AND enabled in each. Declaring a key whose producer a tier
// does not run would make this stage wait out its full timeout on an absent key — so
// re-check this list when a tier's component set changes. The failure is at least
// self-describing: the timeout names the key and reports status_unknown.
var ReadinessKeys = []string{
	readiness.KeyGraphIngest,
	readiness.KeyGraphIndex,
	readiness.KeyRule,
}

// awaitProducersCaughtUp blocks until every declared producer reports healthy AND
// drained, or the validation timeout expires. It returns the last defer reason so a
// timeout says WHICH producer was not ready and WHY, instead of "expected 50 entities,
// got 43" — which named a symptom and left the cause to guesswork.
func (v *EntityVerifier) awaitProducersCaughtUp(ctx context.Context, pollCount *int) error {
	set := readiness.NewSet(v.NATSClient.Client(), ReadinessKeys)
	if err := set.Start(ctx); err != nil {
		return fmt.Errorf("start readiness watchers: %w", err)
	}
	defer set.Stop()

	deadline := time.Now().Add(v.ValidationTimeout)
	var last readiness.Verdict

	for time.Now().Before(deadline) {
		last = set.FullyCovered()
		if last.OK {
			return nil
		}
		time.Sleep(v.PollInterval)
		*pollCount++
	}

	// Include the per-key dump: on a timeout the operator wants every producer's
	// state at once, not just the first one that deferred.
	return fmt.Errorf(
		"producers not caught up within %s (first defer: key=%q reason=%q); per-key state: %+v",
		v.ValidationTimeout, last.Key, last.Reason, set.Dumps())
}

// VerifyEntityCount checks that enough entities have been loaded
func (v *EntityVerifier) VerifyEntityCount(ctx context.Context) (*EntityCountResult, error) {
	if v.NATSClient == nil {
		return nil, fmt.Errorf("NATS client not available")
	}

	minRequired := v.getMinRequired()
	criticalEntities := v.getCriticalEntities()
	expectedFromTestData := v.getExpectedFromTestData()

	var actualCount int
	var lastErr error
	var criticalFound bool

	// WAIT ON THE PRODUCERS' OWN CAUGHT-UP SIGNAL, not on a count reaching a
	// threshold (gh#712). The count-plus-critical-entities poll this replaced was a
	// workaround for a signal that did not exist: it declared success the moment
	// enough entities happened to be present, which is true long before ingest has
	// finished applying, and it could equally declare failure on a slow stack that
	// was working perfectly. Both readings were about TIMING, not about truth.
	//
	// FullyCovered is the right predicate here rather than the health gate: this is a
	// snapshot caller, and ADR-085 bans coverage as admission control for READS while
	// explicitly deferring the non-read case to the consumer's evidence. gh#712 is
	// that evidence.
	pollCount := 0
	// UNCONDITIONAL. A coverage failure must be able to turn the tier red on its own.
	// This previously assigned to lastErr, which is read only under
	// `actualCount < minRequired` — so a stack where no producer ever caught up still
	// passed as long as enough entities happened to be present. That is the exact
	// failure mode of the count-threshold poll this replaced: the count decides, and
	// the new signal proves nothing. Returning here is what makes the fold a real
	// consumer rather than a decorative one.
	if err := v.awaitProducersCaughtUp(ctx, &pollCount); err != nil {
		return &EntityCountResult{PollCount: pollCount}, err
	}

	// The counts are still verified — the signal says the producers are done, and
	// these say they did the right thing. A caught-up stack with the wrong entities
	// is still a failure, it is just a different one, and it is now attributable
	// rather than confounded with "we did not wait long enough".
	var err error
	actualCount, err = v.NATSClient.CountEntities(ctx)
	if err != nil {
		lastErr = err
	}
	criticalFound = true
	for _, entityID := range criticalEntities {
		if _, gerr := v.NATSClient.GetEntity(ctx, entityID); gerr != nil {
			criticalFound = false
			break
		}
	}

	result := &EntityCountResult{
		ActualCount:      actualCount,
		ExpectedFromData: expectedFromTestData,
		ExpectedFromUDP:  0, // UDP telemetry not counted (json_generic disabled)
		TotalExpected:    expectedFromTestData,
		MinExpected:      v.MinExpectedConfig,
		MeetsMinimum:     actualCount >= v.MinExpectedConfig,
		CriticalFound:    criticalFound,
		PollCount:        pollCount,
	}

	// Calculate data loss
	if result.TotalExpected > 0 {
		result.DataLossPercent = 100.0 * float64(result.TotalExpected-actualCount) / float64(result.TotalExpected)
		if result.DataLossPercent < 0 {
			result.DataLossPercent = 0
		}
	}

	// Check for warnings
	if result.DataLossPercent > 10.0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Data loss detected: %.1f%% (expected %d, got %d)",
				result.DataLossPercent, result.TotalExpected, actualCount))
	}

	if actualCount < v.MinExpectedConfig {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Entity count %d is below minimum expected %d", actualCount, v.MinExpectedConfig))
	}

	// Return error if below minimum or critical not found
	if actualCount < minRequired {
		if lastErr != nil {
			return result, fmt.Errorf("entity loading timeout: got %d, need %d after %d polls (last error: %v)",
				actualCount, minRequired, pollCount, lastErr)
		}
		return result, fmt.Errorf("entity loading timeout: got %d, need %d after %d polls",
			actualCount, minRequired, pollCount)
	}

	if !criticalFound {
		return result, fmt.Errorf("critical entities not found after %d polls: %v", pollCount, criticalEntities)
	}

	return result, nil
}

func (v *EntityVerifier) getMinRequired() int {
	switch v.Variant {
	case "structural":
		return StructuralMinEntities
	case "statistical":
		return StatisticalMinEntities
	case "semantic":
		return SemanticMinEntities
	default:
		return v.MinExpectedConfig
	}
}

func (v *EntityVerifier) getCriticalEntities() []string {
	switch v.Variant {
	case "structural":
		// All tiers now use testdata/semantic/sensors.jsonl
		return []string{
			"c360.logistics.sensor.environmental.temperature.temp-sensor-001",
		}
	default:
		return []string{
			"c360.logistics.document.content.operations.doc-ops-001",
		}
	}
}

func (v *EntityVerifier) getExpectedFromTestData() int {
	switch v.Variant {
	case "structural":
		return 74 // All tiers now use same testdata (74 entities)
	default:
		return 74
	}
}

// EntityRetrievalResult contains the results of entity retrieval verification
type EntityRetrievalResult struct {
	Tested   int                    `json:"tested"`
	Found    int                    `json:"found"`
	Missing  []string               `json:"missing,omitempty"`
	Entities map[string]interface{} `json:"entities"`
	Warnings []string               `json:"warnings,omitempty"`
}

// TestEntity represents a test entity to verify
type TestEntity struct {
	ID           string
	ExpectedType string
	Source       string
}

// DefaultTestEntities returns the standard test entities to verify
func DefaultTestEntities() []TestEntity {
	return []TestEntity{
		{"c360.logistics.document.content.operations.doc-ops-001", "document", "documents.jsonl"},
		{"c360.logistics.document.content.quality.doc-quality-001", "document", "documents.jsonl"},
		{"c360.logistics.work.maintenance.completed.maint-001", "maintenance", "maintenance.jsonl"},
		{"c360.logistics.record.observation.high.obs-001", "observation", "observations.jsonl"},
		{"c360.logistics.document.sensor.temperature.sensor-temp-001", "sensor_doc", "sensor_docs.jsonl"},
	}
}

// VerifyEntityRetrieval checks that specific known entities can be retrieved
func (v *EntityVerifier) VerifyEntityRetrieval(ctx context.Context, testEntities []TestEntity) (*EntityRetrievalResult, error) {
	if v.NATSClient == nil {
		return nil, fmt.Errorf("NATS client not available")
	}

	result := &EntityRetrievalResult{
		Tested:   len(testEntities),
		Entities: make(map[string]interface{}),
	}

	for _, te := range testEntities {
		entity, err := v.NATSClient.GetEntity(ctx, te.ID)
		if err != nil {
			result.Missing = append(result.Missing, te.ID)
			result.Entities[te.ID] = map[string]any{
				"found":         false,
				"error":         err.Error(),
				"expected_type": te.ExpectedType,
				"source":        te.Source,
			}
			continue
		}

		result.Found++
		result.Entities[te.ID] = map[string]any{
			"found":         true,
			"actual_type":   entity.Type,
			"expected_type": te.ExpectedType,
			"source":        te.Source,
		}
	}

	if len(result.Missing) > 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Missing entities: %v", result.Missing))
	}

	return result, nil
}

// EntityStructureResult contains the results of entity structure validation
type EntityStructureResult struct {
	Sampled   int                    `json:"sampled"`
	Validated int                    `json:"validated"`
	Errors    []string               `json:"errors,omitempty"`
	Entities  map[string]interface{} `json:"entities"`
	AllValid  bool                   `json:"all_valid"`
	Warnings  []string               `json:"warnings,omitempty"`
}

// ValidateEntityStructure validates entity data structure integrity
func (v *EntityVerifier) ValidateEntityStructure(ctx context.Context, sampleSize int) (*EntityStructureResult, error) {
	if v.NATSClient == nil {
		return nil, fmt.Errorf("NATS client not available")
	}

	entities, err := v.NATSClient.GetEntitySample(ctx, sampleSize)
	if err != nil {
		return nil, fmt.Errorf("failed to get entity sample: %w", err)
	}

	if len(entities) == 0 {
		return nil, fmt.Errorf("no entities available for structure validation")
	}

	result := &EntityStructureResult{
		Sampled:  len(entities),
		Entities: make(map[string]interface{}),
	}

	for _, entity := range entities {
		entityValid := true
		var issues []string

		// Validate ID format
		if entity.ID == "" {
			issues = append(issues, "empty ID")
			entityValid = false
		} else if !strings.Contains(entity.ID, ".") {
			issues = append(issues, "ID missing expected format (no dot separators)")
			entityValid = false
		}

		// Validate Triples
		if len(entity.Triples) == 0 {
			issues = append(issues, "no triples")
			entityValid = false
		} else {
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

		// Validate Version
		if entity.Version <= 0 {
			issues = append(issues, fmt.Sprintf("invalid version: %d", entity.Version))
			entityValid = false
		}

		// Validate UpdatedAt format (non-blocking)
		if entity.UpdatedAt != "" {
			if _, err := time.Parse(time.RFC3339, entity.UpdatedAt); err != nil {
				if _, err := time.Parse(time.RFC3339Nano, entity.UpdatedAt); err != nil {
					issues = append(issues, fmt.Sprintf("invalid timestamp format: %s", entity.UpdatedAt))
				}
			}
		}

		if entityValid {
			result.Validated++
		} else {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", entity.ID, issues))
		}

		result.Entities[entity.ID] = map[string]any{
			"valid":        entityValid,
			"issues":       issues,
			"triple_count": len(entity.Triples),
			"version":      entity.Version,
			"has_updated":  entity.UpdatedAt != "",
		}
	}

	result.AllValid = len(result.Errors) == 0

	if !result.AllValid {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Entity structure validation issues: %v", result.Errors))
	}

	return result, nil
}
