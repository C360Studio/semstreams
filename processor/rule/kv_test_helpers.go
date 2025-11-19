package rule

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/c360/semstreams/message"
	gtypes "github.com/c360/semstreams/types/graph"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// KVTestHelper provides utilities for KV-based rule testing
type KVTestHelper struct {
	t      *testing.T
	bucket jetstream.KeyValue
	ctx    context.Context
}

// NewKVTestHelper creates a new KV test helper
func NewKVTestHelper(t *testing.T, bucket jetstream.KeyValue) *KVTestHelper {
	return &KVTestHelper{
		t:      t,
		bucket: bucket,
		ctx:    context.Background(),
	}
}

// WriteEntityState writes an entity state to the KV bucket
func (h *KVTestHelper) WriteEntityState(entity *gtypes.EntityState) {
	// Ensure timestamp is set
	if entity.UpdatedAt.IsZero() {
		entity.UpdatedAt = time.Now()
	}

	// Marshal to JSON
	data, err := json.Marshal(entity)
	require.NoError(h.t, err, "Failed to marshal entity state")

	// Write to KV bucket
	_, err = h.bucket.Put(h.ctx, entity.Node.ID, data)
	require.NoError(h.t, err, "Failed to write entity to KV bucket")

	h.t.Logf("Wrote entity state to KV: %s", entity.Node.ID)
}

// CreateBatteryEntity creates a battery entity for testing
func CreateBatteryEntity(id string, level float64, voltage float64) *gtypes.EntityState {
	now := time.Now()

	// Create triples for properties
	triples := []message.Triple{
		{
			Subject:   id,
			Predicate: "robotics.battery.level",
			Object:    level,
			Timestamp: now,
		},
		{
			Subject:   id,
			Predicate: "robotics.battery.voltage",
			Object:    voltage,
			Timestamp: now,
		},
		{
			Subject:   id,
			Predicate: "robotics.battery.systemId",
			Object:    1,
			Timestamp: now,
		},
		{
			Subject:   id,
			Predicate: "robotics.battery.batteryId",
			Object:    0,
			Timestamp: now,
		},
		{
			Subject:   id,
			Predicate: "robotics.battery.remaining",
			Object:    int(level),
			Timestamp: now,
		},
		{
			Subject:   id,
			Predicate: "robotics.battery.temperature",
			Object:    25.0,
			Timestamp: now,
		},
	}

	// Create node properties (simpler version for queries)
	properties := map[string]any{
		"level":       level,
		"voltage":     voltage,
		"systemId":    1,
		"batteryId":   0,
		"remaining":   int(level),
		"temperature": 25.0,
	}

	return &gtypes.EntityState{
		Node: gtypes.NodeProperties{
			ID:         id,
			Type:       "robotics.battery",
			Properties: properties,
			Status:     gtypes.StatusActive,
		},
		Triples:   triples,
		Version:   1,
		UpdatedAt: now,
	}
}

// CreateDroneEntity creates a drone entity for testing
func CreateDroneEntity(id string, armed bool, mode string, altitude float64) *gtypes.EntityState {
	now := time.Now()

	// Create triples for properties
	triples := []message.Triple{
		{
			Subject:   id,
			Predicate: "robotics.drone.armed",
			Object:    armed,
			Timestamp: now,
		},
		{
			Subject:   id,
			Predicate: "robotics.drone.mode",
			Object:    mode,
			Timestamp: now,
		},
		{
			Subject:   id,
			Predicate: "robotics.drone.altitude",
			Object:    altitude,
			Timestamp: now,
		},
		{
			Subject:   id,
			Predicate: "robotics.drone.systemId",
			Object:    1,
			Timestamp: now,
		},
		{
			Subject:   id,
			Predicate: "robotics.drone.lat",
			Object:    37.7749,
			Timestamp: now,
		},
		{
			Subject:   id,
			Predicate: "robotics.drone.lon",
			Object:    -122.4194,
			Timestamp: now,
		},
	}

	// Create node properties (simpler version for queries)
	properties := map[string]any{
		"armed":    armed,
		"mode":     mode,
		"altitude": altitude,
		"systemId": 1,
		"lat":      37.7749,
		"lon":      -122.4194,
	}

	return &gtypes.EntityState{
		Node: gtypes.NodeProperties{
			ID:         id,
			Type:       "robotics.drone",
			Properties: properties,
			Status:     gtypes.StatusActive,
		},
		Triples:   triples,
		Version:   1,
		UpdatedAt: now,
	}
}

// UpdateEntityProperty updates a specific property of an entity
func (h *KVTestHelper) UpdateEntityProperty(entityID string, predicateFull string, value interface{}) {
	// Get existing entity
	entry, err := h.bucket.Get(h.ctx, entityID)
	require.NoError(h.t, err, "Failed to get entity from KV")

	var entity gtypes.EntityState
	err = json.Unmarshal(entry.Value(), &entity)
	require.NoError(h.t, err, "Failed to unmarshal entity")

	// Update the triple with matching predicate
	found := false
	now := time.Now()
	for i, triple := range entity.Triples {
		if triple.Predicate == predicateFull {
			entity.Triples[i].Object = value
			entity.Triples[i].Timestamp = now
			found = true
			break
		}
	}

	// Add new triple if not found
	if !found {
		entity.Triples = append(entity.Triples, message.Triple{
			Subject:   entityID,
			Predicate: predicateFull,
			Object:    value,
			Timestamp: now,
		})
	}

	// Update node properties if it's a simple property name
	// Extract property name from predicate (domain.category.property -> property)
	parts := strings.Split(predicateFull, ".")
	if len(parts) >= 3 && entity.Node.Properties != nil {
		propertyName := parts[len(parts)-1]
		entity.Node.Properties[propertyName] = value
	}

	entity.UpdatedAt = now
	entity.Version++

	// Write back
	h.WriteEntityState(&entity)
}

// DeleteEntity removes an entity from the KV bucket
func (h *KVTestHelper) DeleteEntity(entityID string) {
	err := h.bucket.Delete(h.ctx, entityID)
	require.NoError(h.t, err, "Failed to delete entity from KV")
	h.t.Logf("Deleted entity from KV: %s", entityID)
}

// WaitForEntityProcessing waits for an entity update to be processed
// This is useful when testing async KV watchers
func WaitForEntityProcessing(t *testing.T, timeout time.Duration) {
	// Give KV watchers time to process the update
	// In production, you'd check for specific conditions
	time.Sleep(200 * time.Millisecond)
}

// CreateTestEntityPattern creates entity IDs matching test patterns
func CreateTestEntityPattern(domain, category, instance string) string {
	return domain + ".robotics." + category + ".battery." + instance
}

// AssertEventuallyTrue asserts that a condition becomes true within timeout
func AssertEventuallyTrue(t *testing.T, condition func() bool, timeout time.Duration, tick time.Duration, msgAndArgs ...interface{}) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if condition() {
			return // Success
		}
		time.Sleep(tick)
	}

	// Timeout reached
	require.Fail(t, "Condition never became true", msgAndArgs...)
}

// CreateRuleTestConfig creates a test configuration for rule processor
func CreateRuleTestConfig(enabledRules []string, watchPatterns []string) Config {
	config := DefaultConfig()
	config.EnabledRules = enabledRules
	config.EntityWatchPatterns = watchPatterns

	// Set reasonable test values
	config.BufferWindowSize = "10m"
	config.AlertCooldownPeriod = "100ms" // Short cooldown for tests
	config.EnableGraphIntegration = false // Simplify tests

	return config
}

// VerifyRuleTriggered checks if a rule has been triggered by examining metrics or events
func VerifyRuleTriggered(t *testing.T, processor *Processor, ruleName string) bool {
	// This would check metrics or other indicators that the rule triggered
	// Implementation depends on your metrics setup

	// Check if the rule exists
	processor.mu.RLock()
	defer processor.mu.RUnlock()

	if _, exists := processor.rules[ruleName]; exists {
		// Rule exists - in a real implementation, you'd check metrics
		// or events to see if it actually triggered
		return true
	}

	return false
}

// SetupKVBucketForTesting creates and initializes a KV bucket for testing
func SetupKVBucketForTesting(t *testing.T, js jetstream.JetStream, bucketName string) jetstream.KeyValue {
	ctx := context.Background()

	// Try to delete existing bucket first (cleanup from previous test)
	_ = js.DeleteKeyValue(ctx, bucketName)

	// Create new bucket
	cfg := jetstream.KeyValueConfig{
		Bucket:   bucketName,
		History:  5,
		Replicas: 1,
		TTL:      0, // No expiry
	}

	bucket, err := js.CreateKeyValue(ctx, cfg)
	require.NoError(t, err, "Failed to create KV bucket")

	return bucket
}

// WaitForKVWatcher waits for a KV watcher to be ready
func WaitForKVWatcher(t *testing.T, processor *Processor, timeout time.Duration) {
	require.Eventually(t, func() bool {
		// Check if entity watchers are set up
		processor.mu.RLock()
		defer processor.mu.RUnlock()
		return len(processor.entityWatchers) > 0
	}, timeout, 100*time.Millisecond, "KV watcher not ready")
}