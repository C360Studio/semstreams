# Rule Processor Test Plan

## Overview

This document outlines the test strategy for the Rule Processor component, split between integration tests (component-focused, faster) and e2e tests (system-focused, full pipeline).

## Current Status

**E2E Infrastructure**: ✅ Exists (`cmd/e2e/`, `test/e2e/scenarios/`, `docker-compose.e2e.yml`)
**Rule E2E Tests**: ❌ None exist yet
**Rule Integration Tests**: ✅ Exist but need expansion
**Unit Tests**: ✅ Pass

## Test Strategy

### Integration Tests (Component-Focused)
Location: `/Users/coby/Code/c360/semstreams/processor/rule/*_integration_test.go`
- Faster execution (no docker-compose required)
- Test component interactions with mocked/in-memory dependencies
- Focus on rule processor internal behavior

### E2E Tests (System-Focused)
Location: `/Users/coby/Code/c360/semstreams/test/e2e/scenarios/rules_*.go`
- Slower execution (requires docker-compose environment)
- Test complete pipelines across multiple services
- Focus on user-facing workflows and system integration

### UI Tests (Playwright)
Location: Separate UI test repository
- Full UI → Backend workflows already covered by Playwright tests
- No need for duplicate e2e coverage of UI configuration flows

## Integration Test Scenarios

### Integration Test 1: Factory Pattern with Multiple Rule Types

**File**: `processor/rule/factory_integration_test.go`

**Purpose**: Validate factory pattern with multiple rule types registered simultaneously

**Test Cases**:
- Register multiple factories (test_rule, battery_monitor, etc.)
- Create rules of different types from definitions
- Verify factory validation logic
- Test factory error handling (invalid definitions)
- Verify each factory creates correct Rule instances

**Success Criteria**:
- Multiple rule types register without conflicts
- Each factory validates its own rule definitions
- Factory.Create() returns correct Rule implementation
- Invalid definitions rejected with clear errors

---

### Integration Test 2: Runtime Configuration via KV

**File**: `processor/rule/kv_config_integration_test.go`

**Purpose**: Validate KV config watching and rule reloading

**Setup**: Use in-memory NATS server for testing

**Test Cases**:
1. **Add Rule**: Put rule definition in KV → Verify rule loaded and active
2. **Update Rule**: Update KV entry → Verify rule conditions updated
3. **Delete Rule**: Delete KV entry → Verify rule removed
4. **Batch Update**: Update multiple rules → Verify all changes applied
5. **Invalid Config**: Put invalid rule → Verify rejected with error

**Test Data**:
```go
// Add rule to KV bucket "semstreams_config"
key := "rules.test-001"
value := `{"id": "test-001", "type": "test_rule", "conditions": [...]}`

// Update rule
value := `{"id": "test-001", "type": "test_rule", "conditions": [...]}`  // Changed

// Delete rule
natsKV.Delete("rules.test-001")
```

**Success Criteria**:
- KV watcher detects changes within 2s
- Rules reload without processor restart
- Invalid configs rejected without affecting existing rules
- Config manager handles concurrent updates correctly

---

### Integration Test 3: Entity State Watching

**File**: `processor/rule/entity_watcher_integration_test.go`

**Purpose**: Validate entity state KV bucket watching and message generation

**Setup**: Use in-memory NATS server with KV bucket

**Test Cases**:
1. **Pattern Matching**: Update entities matching rule pattern → Verify messages generated
2. **Pattern Filtering**: Update entities NOT matching → Verify ignored
3. **Wildcard Support**: Test patterns with wildcards (`c360.*.robotics.>`)
4. **Multiple Buckets**: Watch multiple KV buckets simultaneously
5. **KV to Message**: Verify KV updates converted to proper Message format

**Test Data**:
```go
// Rule with entity pattern
rule := RuleDefinition{
    Entity: EntityConfig{
        Pattern: "c360.*.robotics.drone.>",
        WatchBuckets: []string{"ENTITY_STATES"},
    },
}

// KV updates
bucket.Put("c360.site1.robotics.drone.001", `{"status": "critical"}`)  // Match
bucket.Put("c360.site1.robotics.sensor.001", `{"status": "ok"}`)       // No match
```

**Success Criteria**:
- Entity watcher subscribes to correct KV patterns
- KV updates converted to messages with proper structure
- Pattern matching works (wildcards, hierarchy)
- Only matching entities generate messages
- Message metadata includes entity key and KV metadata

---

### Integration Test 4: Rule Evaluation Logic

**File**: `processor/rule/evaluation_integration_test.go` (expand existing)

**Purpose**: Comprehensive rule evaluation testing

**Test Cases**:
1. **Condition Operators**: Test all operators (eq, ne, lt, lte, gt, gte)
2. **Nested Fields**: Test dot notation (`battery.level`, `location.coordinates.lat`)
3. **Multiple Conditions**: Test AND logic across multiple conditions
4. **Type Coercion**: Test numeric comparison with different types (int, float64)
5. **Edge Cases**: Null values, missing fields, type mismatches

**Success Criteria**:
- All condition operators work correctly
- Nested field extraction works
- Multiple conditions evaluated with AND logic
- Type coercion handles int/float conversion
- Missing fields return false (don't trigger)

---

## E2E Test Scenarios

### E2E Scenario 1: Rules Graph Integration (`rules-graph`)

**Purpose**: Validate EnableGraphIntegration flag and graph event publishing

**Pipeline**: UDP → JSONGeneric → Rule Processor (EnableGraphIntegration=true) → Graph Processor

**Test Stages**:
1. **verify-components**: Confirm rule processor AND graph processor exist
2. **create-battery-rule**: Create battery_monitor rule (once factory exists)
3. **send-battery-data**: Send battery messages with low levels
4. **validate-graph-events**: Verify graph events were published to graph.events.* subjects
5. **validate-entities**: Confirm entities were created in graph processor

**Test Data**:
```json
Rule Definition:
{
  "id": "battery-001",
  "type": "battery_monitor",
  "name": "Low Battery Alert",
  "conditions": [
    {"field": "battery.level", "operator": "lt", "value": 20}
  ]
}

Test Messages:
{
  "entity_id": "drone-001",
  "battery": {"level": 15, "voltage": 11.2}  // Triggers alert
}
```

**Success Criteria**:
- Rule processor has EnableGraphIntegration=true
- Graph events published to "graph.events.entity.update"
- Graph processor receives events
- Alert entities created with correct properties
- Events include proper metadata (rule_name, timestamp, source)

**Configuration**:
```go
type RulesGraphConfig struct {
    EntityCount       int           // Default: 3
    MessageInterval   time.Duration // Default: 200ms
    ValidationDelay   time.Duration // Default: 5s
    MinAlertEntities  int           // Default: 2
}
```

---

### E2E Scenario 2: Rules Performance (`rules-performance`)

**Purpose**: Validate rule processor under load

**Pipeline**: High-volume message stream → Rule Processor with multiple rules

**Test Stages**:
1. **verify-components**: Confirm rule processor is healthy
2. **create-multiple-rules**: Create 10-20 rules with various conditions
3. **send-high-volume**: Send 1000+ messages rapidly
4. **validate-throughput**: Measure messages processed per second
5. **validate-latency**: Measure rule evaluation latency (p50, p95, p99)
6. **validate-accuracy**: Verify all expected triggers occurred

**Test Data**:
```json
Rules: 10-20 rules with varying complexity
Messages: 1000-5000 messages over 10 seconds
Metrics:
- Messages processed per second
- Rule evaluations per second
- Average evaluation latency
- Event generation rate
```

**Success Criteria**:
- Throughput > 100 messages/sec (adjust based on requirements)
- P95 latency < 50ms for rule evaluation
- No messages dropped
- All expected rule triggers occur
- Memory usage stable (no leaks)
- CPU usage reasonable

**Configuration**:
```go
type RulesPerformanceConfig struct {
    RuleCount         int           // Default: 10
    MessageCount      int           // Default: 1000
    MessageRate       int           // Default: 100/sec
    LatencyTargetP95  time.Duration // Default: 50ms
    ThroughputTarget  int           // Default: 100 msg/sec
}
```

---

## Implementation Plan

### Phase 1: Integration Tests (Week 1)
- [ ] Create `processor/rule/factory_integration_test.go`
  - Test multiple rule types registered simultaneously
  - Test factory validation and error handling
- [ ] Create `processor/rule/kv_config_integration_test.go`
  - Test KV watching with in-memory NATS
  - Test add/update/delete rule operations
  - Depends on KV config integration wiring
- [ ] Expand `processor/rule/evaluation_integration_test.go`
  - Add comprehensive condition operator tests
  - Test nested field extraction
  - Test edge cases and type coercion

### Phase 2: Entity Watcher Integration (Week 2)
- [ ] Create `processor/rule/entity_watcher_integration_test.go`
  - Test entity state KV bucket watching
  - Test pattern matching with wildcards
  - Test KV-to-message conversion
  - Depends on entity watcher implementation

### Phase 3: E2E Graph Integration (Week 2)
- [ ] Create `test/e2e/scenarios/rules_graph.go`
- [ ] Implement RulesGraphScenario following existing patterns
- [ ] Add graph event validation helpers
- [ ] Test EnableGraphIntegration flag behavior
- [ ] Validate end-to-end: Rule → NATS → Graph Processor
- [ ] Depends on battery_monitor factory implementation
- [ ] Add to `cmd/e2e/main.go` scenario registry

### Phase 4: E2E Performance Testing (Week 3)
- [ ] Create `test/e2e/scenarios/rules_performance.go`
- [ ] Add metrics collection helpers
- [ ] Implement load generation (10-20 rules, 1000+ messages)
- [ ] Add performance assertions (throughput, latency)
- [ ] Document baseline performance metrics

### Phase 5: CI/CD Integration (Week 4)
- [ ] Add integration tests to CI pipeline
- [ ] Create docker-compose profile for e2e rule tests
- [ ] Add to nightly test suite
- [ ] Document test execution procedures
- [ ] Set up performance regression alerts

## Helper Functions Needed

### For Integration Tests (processor/rule/testing.go)
```go
// NewTestNATSServer creates an in-memory NATS server for testing
func NewTestNATSServer() (*natsserver.Server, error)

// NewTestKVBucket creates a test KV bucket
func NewTestKVBucket(nc *nats.Conn, bucketName string) (nats.KeyValue, error)

// NewTestRuleProcessor creates a processor with test configuration
func NewTestRuleProcessor(natsURL string) (*Processor, error)
```

### For E2E Tests (test/e2e/client/observability.go)
```go
// SubscribeGraphEvents subscribes to graph events and collects them
func (c *ObservabilityClient) SubscribeGraphEvents(
    ctx context.Context,
    subject string,
    timeout time.Duration,
) ([]GraphEvent, error)

// GetGraphEntities queries graph processor for entities
func (c *ObservabilityClient) GetGraphEntities(
    ctx context.Context,
    entityPattern string,
) ([]Entity, error)
```

## Docker Compose Profile

Add to `docker-compose.e2e.yml`:

```yaml
services:
  # Rule processor with test configuration
  rule-processor-test:
    build: .
    environment:
      - ENABLE_GRAPH_INTEGRATION=true
      - RULE_CONFIG_KV_BUCKET=semstreams_config
      - ENTITY_STATE_KV_BUCKET=ENTITY_STATES
    depends_on:
      - nats
      - graph-processor
```

## Success Metrics

### Coverage Targets

**Integration Tests** (Fast, Component-Focused):
- ✅ Rule evaluation logic (conditions, operators, nested fields)
- ✅ Factory pattern with multiple types
- ✅ Runtime configuration via KV (add/update/delete)
- ✅ Entity state watching and pattern matching

**E2E Tests** (Slower, System-Focused):
- ✅ Complete pipeline: Rule → NATS → Graph Processor
- ✅ EnableGraphIntegration flag with real graph integration
- ✅ Performance under load (throughput, latency)

**UI Tests** (Playwright):
- ✅ UI → KV → Backend configuration workflow (already covered)

### Test Execution Time
- **Integration tests**: < 10s total (in-memory NATS, no docker)
- **E2E graph integration**: < 30s (requires docker-compose)
- **E2E performance**: < 2 minutes (load testing)
- **Full integration suite**: < 30s
- **Full e2e suite**: < 3 minutes

### Reliability
- All tests pass consistently in CI
- Integration tests use in-memory dependencies (no flakiness)
- E2E tests use explicit delays, not arbitrary sleeps
- Proper cleanup prevents test pollution

## Dependencies

### Immediate (Can Test Now)
- ✅ test_rule factory (exists)
- ✅ Generic Event interface (implemented)
- ✅ Rule processor basic functionality
- ✅ Integration test infrastructure (in-memory NATS)

### Short-term (Week 1-2)
- ⏳ KV config integration wiring (for KV config integration tests)
- ⏳ battery_monitor factory (for e2e graph integration tests)

### Medium-term (Week 2-3)
- ⏳ Entity watcher implementation (for entity watcher integration tests)

### Not Needed (Already Covered)
- ❌ UI configuration workflow (Playwright tests cover this)

## Notes

1. **Integration vs E2E**: Most rule testing should be at integration level (faster, more reliable)
2. **Test Isolation**: Each test should create its own rules and clean up afterward
3. **In-Memory Dependencies**: Integration tests use in-memory NATS (no docker required)
4. **Timing**: Use explicit delays in e2e tests; tune based on actual performance
5. **Error Handling**: All e2e scenarios should return Result even on failure for debugging
6. **Extensibility**: Design helpers to support future rule types and features
7. **UI Testing**: UI workflows already covered by Playwright tests with real backend

## References

- **Rule processor**: `/Users/coby/Code/c360/semstreams/processor/rule/`
- **Integration tests**: `/Users/coby/Code/c360/semstreams/processor/rule/*_integration_test.go`
- **E2E scenarios**: `/Users/coby/Code/c360/semstreams/test/e2e/scenarios/`
- **E2E main**: `/Users/coby/Code/c360/semstreams/cmd/e2e/main.go`
- **Docker Compose**: `/Users/coby/Code/c360/semstreams/docker-compose.e2e.yml`
