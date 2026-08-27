//go:build integration

package rule_test

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/processor/rule/expression"
	"github.com/c360studio/semstreams/vocabulary/rulepacks"
)

// getTestNATSClient creates a NATS client for integration tests
func getTestNATSClient(t *testing.T) *natsclient.Client {
	// Create test client for this test
	// Build tag ensures this only runs with -tags=integration
	testClient, err := natsclient.NewSharedTestClient(
		natsclient.WithJetStream(),
		natsclient.WithKV(),
		natsclient.WithTestTimeout(5*time.Second),
		natsclient.WithStartTimeout(30*time.Second),
	)
	if err != nil {
		t.Fatalf("Failed to create test client: %v", err)
	}

	// Register cleanup
	t.Cleanup(func() {
		testClient.Terminate()
	})

	return testClient.Client
}

// createSemanticMessage creates a test message for testing
func createSemanticMessage(data map[string]any) ([]byte, error) {
	// Create a GenericJSON payload
	payload := message.NewGenericJSON(data)

	// Create a BaseMessage with the payload
	msgType := message.Type{
		Domain:   "core",
		Category: "json",
		Version:  "v1",
	}

	msg := message.NewBaseMessage(msgType, payload, "test-integration")
	return json.Marshal(msg)
}

// TestIntegration_KVEntityStateWatch tests KV entity state watching and rule triggering
func TestIntegration_KVEntityStateWatch(t *testing.T) {
	natsClient := getTestNATSClient(t)
	ctx := context.Background()

	// Create ENTITY_STATES KV bucket if it doesn't exist
	js, err := natsClient.JetStream()
	require.NoError(t, err)

	kv, err := js.KeyValue(ctx, "ENTITY_STATES")
	if err != nil {
		// Bucket doesn't exist, create it
		kv, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
			Bucket: "ENTITY_STATES",
		})
		require.NoError(t, err)
	}

	// Create a rule that triggers on battery level changes
	// NOTE: Field must match the FULL triple predicate path
	ruleDef := rule.Definition{
		ID:   "battery_low_watch",
		Type: "test_rule",
		Name: "Battery Low Watcher",
		Conditions: []expression.ConditionExpression{
			{
				Field:    "robotics.battery.level", // Must match full triple predicate
				Operator: "lte",
				Value:    25.0,
				Required: true,
			},
		},
		Logic:   "and",
		Enabled: true,
		Entity: rule.EntityConfig{
			Pattern:      "c360.platform1.test.*.*.*",
			WatchBuckets: []string{gtypes.BucketEntityStates},
		},
	}

	// Create rule processor config
	config, configErr := rule.NewConfig("rule-crud-integration-test")
	require.NoError(t, configErr)
	config.Ports = &component.PortConfig{
		Inputs: []component.PortDefinition{
			{
				Name: "entity_events", Config: component.NATSPort{Subject: "events.graph.entity.>", Interface: &component.InterfaceContract{Type: "core.entity.v1"}}, Required: true,
			},
		},
		Outputs: []component.PortDefinition{
			{
				Name: "rule_events", Config: component.NATSPort{Subject: "events.rule.triggered", Interface: &component.InterfaceContract{Type: "core.rule.v1"}}, Required: true,
			},
		},
	}
	config.InlineRules = []rule.Definition{ruleDef}
	config.EntityWatchBuckets = map[string][]string{gtypes.BucketEntityStates: {"c360.platform1.test.*.*.*"}}
	config.EnableGraphIntegration = false // We're testing KV watch, not graph integration

	// Create processor with metrics
	metricsRegistry := metric.NewMetricsRegistry()
	processor, err := rule.NewProcessorWithMetrics(natsClient, &config, metricsRegistry)
	require.NoError(t, err)
	// Production installs the deployment authority through CreateRuleProcessor
	// (processor/rule/factory.go:130); the rule engine mints its trigger identity
	// under it and fails closed on an empty pair (ADR-102 d2).
	processor.SetPlatform(component.PlatformMeta{Org: "c360", Platform: "platform1"})
	processor.SetDecoder(message.NewDecoder(payloadbuiltins.NewTestRegistry(t)))
	require.NotNil(t, processor)

	// Initialize and start
	err = processor.Initialize()
	require.NoError(t, err)

	testCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = processor.Start(testCtx)
	require.NoError(t, err)
	defer processor.Stop(context.Background())

	// Give processor time to set up watchers
	time.Sleep(200 * time.Millisecond)

	// Subscribe to rule events
	receivedEvents := make([]map[string]any, 0)
	var receiveMu sync.Mutex

	_, err = natsClient.Subscribe(testCtx, "events.rule.triggered", func(_ context.Context, msg *nats.Msg) {
		var event map[string]any
		if err := json.Unmarshal(msg.Data, &event); err == nil {
			receiveMu.Lock()
			receivedEvents = append(receivedEvents, event)
			receiveMu.Unlock()
		}
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Update entity state in KV bucket (simulating graph processor update)
	entityID := "c360.platform1.test.system.drone.drone1"
	entityState := gtypes.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{
				Subject:   entityID,
				Predicate: "robotics.battery.level",
				Object:    20.0, // Below threshold of 25
			},
			{
				Subject:   entityID,
				Predicate: "robotics.battery.voltage",
				Object:    3.2,
			},
		},
		Version:   1,
		UpdatedAt: time.Now(),
	}

	entityJSON, err := json.Marshal(entityState)
	require.NoError(t, err)

	_, err = kv.Put(ctx, entityID, entityJSON)
	require.NoError(t, err)

	// Wait for watcher to process and rule to evaluate
	time.Sleep(500 * time.Millisecond)

	// Verify rule was triggered
	receiveMu.Lock()
	assert.Greater(t, len(receivedEvents), 0, "Should have triggered rule event")
	receiveMu.Unlock()

	// Verify metrics were updated
	if processor.GetRuleMetrics() != nil {
		metrics := processor.GetRuleMetrics()
		assert.Greater(t, metrics["total_evaluated"].(int64), int64(0), "Should have evaluated rules")
	}
}

// TestIntegration_DynamicRuleCRUD tests runtime rule configuration updates
func TestIntegration_DynamicRuleCRUD(t *testing.T) {
	natsClient := getTestNATSClient(t)

	// Create processor with initial configuration
	config, configErr := rule.NewConfig("rule-file-integration-test")
	require.NoError(t, configErr)
	config.Ports = &component.PortConfig{
		Inputs: []component.PortDefinition{
			{
				Name: "semantic_input", Config: component.NATSPort{Subject: "process.test.crud", Interface: &component.InterfaceContract{Type: "core.semantic.v1"}}, Required: true,
			},
		},
		Outputs: []component.PortDefinition{
			{
				Name: "rule_events", Config: component.NATSPort{Subject: "events.rule.crud", Interface: &component.InterfaceContract{Type: "core.rule.v1"}}, Required: true,
			},
		},
	}
	config.EnableGraphIntegration = false

	processor, err := rule.NewProcessor(natsClient, &config)
	require.NoError(t, err)
	// Production installs the deployment authority through CreateRuleProcessor
	// (processor/rule/factory.go:130); the rule engine mints its trigger identity
	// under it and fails closed on an empty pair (ADR-102 d2).
	processor.SetPlatform(component.PlatformMeta{Org: "c360", Platform: "platform1"})
	processor.SetDecoder(message.NewDecoder(payloadbuiltins.NewTestRegistry(t)))
	require.NotNil(t, processor)

	err = processor.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = processor.Start(ctx)
	require.NoError(t, err)
	defer processor.Stop(context.Background())

	time.Sleep(100 * time.Millisecond)

	// Get initial rule count
	initialCount := len(processor.GetRuntimeConfig()["rules"].(map[string]any))

	// CREATE - Add new rule via runtime config
	newRule := map[string]any{
		"type": "test_rule",
		"name": "Dynamic Battery Rule",
		"conditions": []any{
			map[string]any{
				"field":    "$message.battery.level", // predicate-audit:invalid {"kind":"stored-predicate","value":"$message.battery.level","reason":"segment_start"}
				"operator": "lt",
				"value":    30.0,
				"required": true,
			},
		},
		"logic":   "and",
		"enabled": true,
	}

	changes := map[string]any{
		"rules": map[string]any{
			"dynamic_rule_001": newRule,
		},
	}

	// Validate and apply
	err = processor.ValidateConfigUpdate(changes)
	require.NoError(t, err)

	err = processor.ApplyConfigUpdate(changes)
	require.NoError(t, err)

	// READ - Verify rule was added
	currentConfig := processor.GetRuntimeConfig()
	rules := currentConfig["rules"].(map[string]any)
	assert.Contains(t, rules, "dynamic_rule_001")
	assert.Equal(t, initialCount+1, len(rules))

	// UPDATE - Modify the rule
	updatedRule := map[string]any{
		"type": "test_rule",
		"name": "Updated Dynamic Rule",
		"conditions": []any{
			map[string]any{
				"field":    "$message.battery.level", // predicate-audit:invalid {"kind":"stored-predicate","value":"$message.battery.level","reason":"segment_start"}
				"operator": "lt",
				"value":    15.0, // Changed threshold
				"required": true,
			},
		},
		"logic":   "and",
		"enabled": false, // Disabled
	}

	updateChanges := map[string]any{
		"rules": map[string]any{
			"dynamic_rule_001": updatedRule,
		},
	}

	err = processor.ApplyConfigUpdate(updateChanges)
	require.NoError(t, err)

	currentConfig = processor.GetRuntimeConfig()
	rules = currentConfig["rules"].(map[string]any)
	updatedRuleData := rules["dynamic_rule_001"].(map[string]any)
	assert.Equal(t, "Updated Dynamic Rule", updatedRuleData["name"])

	// DELETE would require removing from rules map (not currently implemented)
	// This is tracked as a limitation in runtime_config.go
}

// TestIntegration_JSONDSLRuleLoading tests loading rules from JSON files
func TestIntegration_JSONDSLRuleLoading(t *testing.T) {
	natsClient := getTestNATSClient(t)

	// Create a temporary JSON rule file
	tempDir := t.TempDir()
	ruleFile := tempDir + "/test_rule.json"

	ruleDef := rule.Definition{
		ID:   "json_dsl_test",
		Type: "test_rule",
		Name: "JSON DSL Test Rule",
		Conditions: []expression.ConditionExpression{
			{
				Field:    "$message.battery.level",
				Operator: "lte",
				Value:    20.0,
				Required: true,
			},
		},
		Logic:   "and",
		Enabled: true,
	}

	ruleJSON, err := json.Marshal(ruleDef)
	require.NoError(t, err)

	err = os.WriteFile(ruleFile, ruleJSON, 0644)
	require.NoError(t, err)

	// Create processor with rules_files config
	config, configErr := rule.NewConfig("rule-entity-integration-test")
	require.NoError(t, configErr)
	config.Ports = &component.PortConfig{
		Inputs: []component.PortDefinition{
			{
				Name: "semantic_input", Config: component.NATSPort{Subject: "process.test.json", Interface: &component.InterfaceContract{Type: "core.semantic.v1"}}, Required: true,
			},
		},
		Outputs: []component.PortDefinition{
			{
				Name: "rule_events", Config: component.NATSPort{Subject: "events.rule.json", Interface: &component.InterfaceContract{Type: "core.rule.v1"}}, Required: true,
			},
		},
	}
	config.RulesFiles = []string{ruleFile}
	config.EnableGraphIntegration = false

	processor, err := rule.NewProcessor(natsClient, &config)
	require.NoError(t, err)
	// Production installs the deployment authority through CreateRuleProcessor
	// (processor/rule/factory.go:130); the rule engine mints its trigger identity
	// under it and fails closed on an empty pair (ADR-102 d2).
	processor.SetPlatform(component.PlatformMeta{Org: "c360", Platform: "platform1"})
	processor.SetDecoder(message.NewDecoder(payloadbuiltins.NewTestRegistry(t)))
	require.NotNil(t, processor)

	err = processor.Initialize()
	require.NoError(t, err)

	// Verify rule was loaded from file
	runtimeConfig := processor.GetRuntimeConfig()
	assert.Equal(t, 1, runtimeConfig["rule_count"])
}

// TestIntegration_PrometheusMetrics tests metrics recording during rule processing
func TestIntegration_PrometheusMetrics(t *testing.T) {
	natsClient := getTestNATSClient(t)

	// Create processor with metrics registry
	metricsRegistry := metric.NewMetricsRegistry()
	require.NotNil(t, metricsRegistry)

	config, configErr := rule.NewConfig("rule-graph-integration-test")
	require.NoError(t, configErr)
	config.Ports = &component.PortConfig{
		Inputs: []component.PortDefinition{
			{
				Name: "semantic_input", Config: component.NATSPort{Subject: "process.test.metrics", Interface: &component.InterfaceContract{Type: "core.semantic.v1"}}, Required: true,
			},
		},
		Outputs: []component.PortDefinition{
			{
				Name: "rule_events", Config: component.NATSPort{Subject: "events.rule.metrics", Interface: &component.InterfaceContract{Type: "core.rule.v1"}}, Required: true,
			},
		},
	}

	// Add a test rule
	ruleDef := rule.Definition{
		ID:   "metrics_test_rule",
		Type: "test_rule",
		Name: "Metrics Test Rule",
		Conditions: []expression.ConditionExpression{
			{
				Field:    "$message.battery.level",
				Operator: "lt",
				Value:    50.0,
				Required: true,
			},
		},
		Logic:   "and",
		Enabled: true,
	}
	config.InlineRules = []rule.Definition{ruleDef}
	config.EnableGraphIntegration = false

	processor, err := rule.NewProcessorWithMetrics(natsClient, &config, metricsRegistry)
	require.NoError(t, err)
	// Production installs the deployment authority through CreateRuleProcessor
	// (processor/rule/factory.go:130); the rule engine mints its trigger identity
	// under it and fails closed on an empty pair (ADR-102 d2).
	processor.SetPlatform(component.PlatformMeta{Org: "c360", Platform: "platform1"})
	processor.SetDecoder(message.NewDecoder(payloadbuiltins.NewTestRegistry(t)))
	require.NotNil(t, processor)

	err = processor.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = processor.Start(ctx)
	require.NoError(t, err)
	defer processor.Stop(context.Background())

	time.Sleep(100 * time.Millisecond)

	// Publish a test message that should trigger the rule
	testMsg, err := createSemanticMessage(map[string]any{
		"battery": map[string]any{
			"level": 25.0,
		},
	})
	require.NoError(t, err)

	err = natsClient.Publish(ctx, "process.test.metrics", testMsg)
	require.NoError(t, err)

	// Wait for message processing
	time.Sleep(500 * time.Millisecond)

	// Verify metrics were recorded
	// Note: We can't directly access processor.metrics from test package
	// Instead, check via GetRuleMetrics or Health
	metrics := processor.GetRuleMetrics()
	assert.Greater(t, metrics["total_evaluated"].(int64), int64(0), "Should have evaluated messages")

	// Health check should show activity
	health := processor.Health()
	assert.True(t, health.Healthy)
	assert.Greater(t, health.Uptime.Seconds(), 0.0)
}

// TestIntegration_DynamicWatchPatterns tests runtime updates to entity watch patterns
func TestIntegration_DynamicWatchPatterns(t *testing.T) {
	natsClient := getTestNATSClient(t)
	ctx := context.Background()

	// Create ENTITY_STATES KV bucket
	js, err := natsClient.JetStream()
	require.NoError(t, err)

	kv, err := js.KeyValue(ctx, "ENTITY_STATES")
	if err != nil {
		kv, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
			Bucket: "ENTITY_STATES",
		})
		require.NoError(t, err)
	}

	// Create processor with initial watch patterns
	config, configErr := rule.NewConfig("rule-dynamic-watch-integration-test")
	require.NoError(t, configErr)
	config.Ports = &component.PortConfig{
		Inputs: []component.PortDefinition{
			{Name: "entity_events", Config: component.NATSPort{Subject: "events.graph.entity.>"}, Required: true},
		},
		Outputs: []component.PortDefinition{
			{Name: "rule_events", Config: component.NATSPort{Subject: "events.rule.triggered"}, Required: true},
		},
	}
	config.EntityWatchBuckets = map[string][]string{gtypes.BucketEntityStates: {"c360.platform1.test.*.*.*"}}
	config.EnableGraphIntegration = false

	// Add a rule that triggers on battery level
	ruleDef := rule.Definition{
		ID:   "dynamic_watch_test",
		Type: "test_rule",
		Name: "Dynamic Watch Test",
		Conditions: []expression.ConditionExpression{
			{Field: "robotics.battery.level", Operator: "lt", Value: 50.0, Required: true},
		},
		Logic:   "and",
		Enabled: true,
		Entity: rule.EntityConfig{
			Pattern:      "*.*.*.*.*.*", // Match all canonical entities
			WatchBuckets: []string{gtypes.BucketEntityStates},
		},
	}
	config.InlineRules = []rule.Definition{ruleDef}

	processor, err := rule.NewProcessor(natsClient, &config)
	require.NoError(t, err)
	// Production installs the deployment authority through CreateRuleProcessor
	// (processor/rule/factory.go:130); the rule engine mints its trigger identity
	// under it and fails closed on an empty pair (ADR-102 d2).
	processor.SetPlatform(component.PlatformMeta{Org: "c360", Platform: "platform1"})
	processor.SetDecoder(message.NewDecoder(payloadbuiltins.NewTestRegistry(t)))

	err = processor.Initialize()
	require.NoError(t, err)

	testCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err = processor.Start(testCtx)
	require.NoError(t, err)
	defer processor.Stop(context.Background())

	// Wait for watchers to start
	time.Sleep(300 * time.Millisecond)

	// Subscribe to rule events
	receivedEvents := make([]map[string]any, 0)
	var receiveMu sync.Mutex

	_, err = natsClient.Subscribe(testCtx, "events.rule.triggered", func(_ context.Context, msg *nats.Msg) {
		var event map[string]any
		if err := json.Unmarshal(msg.Data, &event); err == nil {
			receiveMu.Lock()
			receivedEvents = append(receivedEvents, event)
			receiveMu.Unlock()
		}
	})
	require.NoError(t, err)

	// Create entity that matches initial pattern
	entity1 := gtypes.EntityState{
		ID: "c360.platform1.test.system.drone.d001",
		Triples: []message.Triple{
			{Subject: "c360.platform1.test.system.drone.d001", Predicate: "robotics.battery.level", Object: 25.0},
		},
		Version:   1,
		UpdatedAt: time.Now(),
	}
	entity1JSON, _ := json.Marshal(entity1)
	_, err = kv.Put(ctx, entity1.ID, entity1JSON)
	require.NoError(t, err)

	time.Sleep(300 * time.Millisecond)

	// Verify rule triggered for entity1
	receiveMu.Lock()
	initialEventCount := len(receivedEvents)
	receiveMu.Unlock()
	assert.Greater(t, initialEventCount, 0, "Rule should have triggered for entity1")

	// Now dynamically add a new watch pattern
	changes := map[string]any{
		"entity_watch_buckets": map[string][]string{
			gtypes.BucketEntityStates: {
				"c360.platform1.test.*.*.*",
				"c360.platform2.test.*.*.*", // New pattern
			},
		},
	}
	err = processor.ApplyConfigUpdate(changes)
	require.NoError(t, err)

	// Wait for new watcher to start
	time.Sleep(300 * time.Millisecond)

	// Create entity that matches the new pattern
	entity2 := gtypes.EntityState{
		ID: "c360.platform2.test.system.drone.d002",
		Triples: []message.Triple{
			{Subject: "c360.platform2.test.system.drone.d002", Predicate: "robotics.battery.level", Object: 30.0},
		},
		Version:   1,
		UpdatedAt: time.Now(),
	}
	entity2JSON, _ := json.Marshal(entity2)
	_, err = kv.Put(ctx, entity2.ID, entity2JSON)
	require.NoError(t, err)

	time.Sleep(300 * time.Millisecond)

	// Verify rule triggered for entity2 (matching new pattern)
	receiveMu.Lock()
	finalEventCount := len(receivedEvents)
	receiveMu.Unlock()
	assert.Greater(t, finalEventCount, initialEventCount, "Rule should have triggered for entity2 after adding new pattern")

	// Verify runtime config shows updated patterns
	runtimeConfig := processor.GetRuntimeConfig()
	buckets := runtimeConfig["entity_watch_buckets"].(map[string][]string)
	assert.Contains(t, buckets[gtypes.BucketEntityStates], "c360.platform2.test.*.*.*")
}

// TestIntegration_GraphIntegration tests event publishing to graph processor
func TestIntegration_GraphIntegration(t *testing.T) {
	natsClient := getTestNATSClient(t)

	config, configErr := rule.NewConfig("rule-graph-publish-integration-test")
	require.NoError(t, configErr)
	config.Ports = &component.PortConfig{
		Inputs: []component.PortDefinition{
			{
				Name: "semantic_input", Config: component.NATSPort{Subject: "process.test.graph", Interface: &component.InterfaceContract{Type: "core.semantic.v1"}}, Required: true,
			},
		},
		Outputs: []component.PortDefinition{
			{
				Name: "rule_events", Config: component.NATSPort{Subject: "events.rule.graph", Interface: &component.InterfaceContract{Type: "core.rule.v1"}}, Required: true,
			},
			{
				Name: "graph_mutations", Config: component.NATSPort{Subject: "graph.mutations", Interface: &component.InterfaceContract{Type: "core.graph.v1"}}, Required: false,
			},
		},
	}

	// Add a rule that generates graph events
	ruleDef := rule.Definition{
		ID:   "graph_test_rule",
		Type: "test_rule",
		Name: "Graph Integration Rule",
		Conditions: []expression.ConditionExpression{
			{
				Field:    "$message.battery.level",
				Operator: "lt",
				Value:    15.0,
				Required: true,
			},
		},
		Logic:   "and",
		Enabled: true,
	}
	config.InlineRules = []rule.Definition{ruleDef}
	config.EnableGraphIntegration = true // Enable graph integration

	processor, err := rule.NewProcessor(natsClient, &config)
	require.NoError(t, err)
	// Production installs the deployment authority through CreateRuleProcessor
	// (processor/rule/factory.go:130); the rule engine mints its trigger identity
	// under it and fails closed on an empty pair (ADR-102 d2).
	processor.SetPlatform(component.PlatformMeta{Org: "c360", Platform: "platform1"})
	processor.SetDecoder(message.NewDecoder(payloadbuiltins.NewTestRegistry(t)))
	require.NotNil(t, processor)

	err = processor.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = processor.Start(ctx)
	require.NoError(t, err)
	defer processor.Stop(context.Background())

	time.Sleep(100 * time.Millisecond)

	// Subscribe to graph events (events are published to graph.events.*)
	receivedMutations := make([]map[string]any, 0)
	var receiveMu sync.Mutex

	_, err = natsClient.Subscribe(ctx, "graph.events.>", func(_ context.Context, msg *nats.Msg) {
		var mutation map[string]any
		if err := json.Unmarshal(msg.Data, &mutation); err == nil {
			receiveMu.Lock()
			receivedMutations = append(receivedMutations, mutation)
			receiveMu.Unlock()
		}
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Publish message that triggers rule
	testMsg, err := createSemanticMessage(map[string]any{
		"battery": map[string]any{
			"level": 10.0, // Below threshold
		},
	})
	require.NoError(t, err)

	err = natsClient.Publish(ctx, "process.test.graph", testMsg)
	require.NoError(t, err)

	// Wait for processing
	time.Sleep(500 * time.Millisecond)

	// Verify graph mutation events were published
	receiveMu.Lock()
	if config.EnableGraphIntegration {
		// Only check if graph integration is enabled
		assert.Greater(t, len(receivedMutations), 0, "Should have published graph mutations")
	}
	receiveMu.Unlock()
}

// TestIntegration_TransitionOperator_UpdateKV exercises the transition operator and
// update_kv action end-to-end on real NATS: entity state changes in ENTITY_STATES KV,
// the transition condition detects a valid from→to change, and the on_enter action
// writes to a domain KV bucket.
func TestIntegration_TransitionOperator_UpdateKV(t *testing.T) {
	natsClient := getTestNATSClient(t)
	ctx := context.Background()

	js, err := natsClient.JetStream()
	require.NoError(t, err)

	// Create ENTITY_STATES bucket for rule input
	entityKV, err := js.KeyValue(ctx, "ENTITY_STATES")
	if err != nil {
		entityKV, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
			Bucket: "ENTITY_STATES",
		})
		require.NoError(t, err)
	}

	// Create PLAN_STATES bucket for update_kv output
	planKV, err := js.KeyValue(ctx, "PLAN_STATES")
	if err != nil {
		planKV, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
			Bucket:  "PLAN_STATES",
			History: 5,
		})
		require.NoError(t, err)
	}

	// Seed PLAN_STATES with initial data to test merge semantics
	initialPlan, err := json.Marshal(map[string]any{
		"status": "created",
		"owner":  "alice",
	})
	require.NoError(t, err)
	_, err = planKV.Put(ctx, "my-plan", initialPlan)
	require.NoError(t, err)

	// Define a rule: when workflow.state.status transitions from "created" to "drafting",
	// merge {status: "drafting"} into PLAN_STATES/my-plan
	ruleDef := rule.Definition{
		ID:   "plan-created-to-drafting",
		Type: "expression",
		Name: "Plan Created to Drafting Transition",
		Conditions: []expression.ConditionExpression{
			{
				Field:    rulepacks.WorkflowStateStatus,
				Operator: "transition",
				Value:    "drafting",
				From:     []any{"created", "rejected"},
			},
		},
		Logic:   "and",
		Enabled: true,
		Entity: rule.EntityConfig{
			Pattern:      "c360.test.workflow.*.*.*",
			WatchBuckets: []string{gtypes.BucketEntityStates},
		},
		OnEnter: []rule.Action{
			{
				Type:   "update_kv",
				Bucket: "PLAN_STATES",
				Key:    "my-plan",
				Payload: map[string]any{
					"status":     "drafting",
					"drafted_by": "rule_engine",
					"entity_ref": "$entity.id",
					"drafted_at": "$now",
				},
				Merge: true,
			},
		},
	}

	// Configure processor
	config, configErr := rule.NewConfig("rule-transition-integration-test")
	require.NoError(t, configErr)
	config.Ports = &component.PortConfig{
		Inputs: []component.PortDefinition{
			{Name: "entity_events", Config: component.NATSPort{Subject: "events.graph.entity.>", Interface: &component.InterfaceContract{Type: "core.entity.v1"}}, Required: true},
		},
		Outputs: []component.PortDefinition{
			{Name: "rule_events", Config: component.NATSPort{Subject: "events.rule.triggered", Interface: &component.InterfaceContract{Type: "core.rule.v1"}}, Required: true},
		},
	}
	config.InlineRules = []rule.Definition{ruleDef}
	config.EntityWatchBuckets = map[string][]string{gtypes.BucketEntityStates: {"c360.test.workflow.*.*.*"}}
	config.EnableGraphIntegration = false

	metricsRegistry := metric.NewMetricsRegistry()
	processor, err := rule.NewProcessorWithMetrics(natsClient, &config, metricsRegistry)
	require.NoError(t, err)
	// Production installs the deployment authority through CreateRuleProcessor
	// (processor/rule/factory.go:130); the rule engine mints its trigger identity
	// under it and fails closed on an empty pair (ADR-102 d2).
	processor.SetPlatform(component.PlatformMeta{Org: "c360", Platform: "platform1"})
	processor.SetDecoder(message.NewDecoder(payloadbuiltins.NewTestRegistry(t)))

	err = processor.Initialize()
	require.NoError(t, err)

	testCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	err = processor.Start(testCtx)
	require.NoError(t, err)
	defer processor.Stop(context.Background())

	time.Sleep(300 * time.Millisecond) // Wait for watchers

	entityID := "c360.test.workflow.system.plan.plan1"

	// --- Step 1: Put entity with status "created" ---
	// This seeds the transition tracker's previous value. The transition condition
	// checks for "from: created → to: drafting", so this should NOT fire (current != target).
	entityCreated := gtypes.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: semantictest.Predicate(t, "workflow", "state", "status"), Object: "created", Source: "test", Timestamp: time.Now()},
		},
		Version:   1,
		UpdatedAt: time.Now(),
	}
	createdJSON, err := json.Marshal(entityCreated)
	require.NoError(t, err)
	_, err = entityKV.Put(ctx, entityID, createdJSON)
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond) // Wait for evaluation

	// Verify PLAN_STATES was NOT updated by the rule (status should still be "created")
	entry, err := planKV.Get(ctx, "my-plan")
	require.NoError(t, err)
	var planState map[string]any
	require.NoError(t, json.Unmarshal(entry.Value(), &planState))
	assert.Equal(t, "created", planState["status"], "Plan should still be 'created' — first evaluation seeds history but can't detect transition")
	assert.Equal(t, "alice", planState["owner"], "Owner should be preserved")
	assert.Nil(t, planState["drafted_by"], "drafted_by should not be set yet")

	// --- Step 2: Update entity to status "drafting" ---
	// Now the transition condition sees: previous="created" (tracked), current="drafting" (target).
	// From set includes "created", so this IS a valid transition → rule fires → update_kv merges.
	entityDrafting := gtypes.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: semantictest.Predicate(t, "workflow", "state", "status"), Object: "drafting", Source: "test", Timestamp: time.Now()},
		},
		Version:   2,
		UpdatedAt: time.Now(),
	}
	draftingJSON, err := json.Marshal(entityDrafting)
	require.NoError(t, err)
	_, err = entityKV.Put(ctx, entityID, draftingJSON)
	require.NoError(t, err)

	// Wait for evaluation + action execution
	// Poll for the KV update to appear (avoids flaky fixed sleeps)
	require.Eventually(t, func() bool {
		entry, err := planKV.Get(ctx, "my-plan")
		if err != nil {
			return false
		}
		var state map[string]any
		if err := json.Unmarshal(entry.Value(), &state); err != nil {
			return false
		}
		return state["status"] == "drafting"
	}, 5*time.Second, 100*time.Millisecond, "PLAN_STATES should be updated to 'drafting' by transition rule")

	// Verify merge semantics: original "owner" preserved, new fields added
	entry, err = planKV.Get(ctx, "my-plan")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(entry.Value(), &planState))

	assert.Equal(t, "drafting", planState["status"], "status should be updated")
	assert.Equal(t, "alice", planState["owner"], "owner should be preserved by merge")
	assert.Equal(t, "rule_engine", planState["drafted_by"], "drafted_by should be set by action")
	assert.Equal(t, entityID, planState["entity_ref"], "$entity.id should be substituted")

	// $now should be a valid RFC3339 timestamp
	if draftedAt, ok := planState["drafted_at"].(string); ok {
		_, parseErr := time.Parse(time.RFC3339, draftedAt)
		assert.NoError(t, parseErr, "drafted_at should be valid RFC3339: %s", draftedAt)
	} else {
		t.Error("drafted_at should be a string timestamp")
	}
}
