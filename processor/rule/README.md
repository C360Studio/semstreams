# pkg/processor/rule - Rule Processing Component

**Last Updated**: 2025-11-19
**Maintainer**: SemStreams Framework Team
**Status**: ✅ **Refactored & Genericized** - 75% reduction in complexity

## Purpose & Scope

**What this component does**: Processes message streams through configurable rules and evaluates conditions against semantic messages and entity state changes.

**Key responsibilities**:
- Processes message streams through configurable rules
- Watches ENTITY_STATES KV bucket for entity changes via KV watch pattern
- Evaluates rules against semantic messages and entity state changes
- Publishes rule trigger events and graph mutation requests
- Provides time-window analysis with message buffering

**NOT responsible for**:
- Rule creation/management (rules are loaded from configuration)
- Raw message processing (processes semantic messages only)
- Direct entity modification (publishes events for GraphProcessor to handle)

## Refactoring History (2025-11-18)

### Major Code Cleanup & Genericization

**Objective**: Remove domain-specific code, eliminate redundancy, and improve maintainability through modularization.

**Results**:
- **processor.go**: 1,845 LOC → 451 LOC (**75.5% reduction**)
- **Files Deleted**: 5 files (39.1K) - battery-specific + redundant code
- **Files Extracted**: 9 focused modules (1,501 LOC organized by responsibility)
- **Test Status**: ✅ All tests passing

### Dead Code Removal (2025-11-19)

**Objective**: Remove unimplemented features from codebase

**Changes**:
- **Deleted**: `variables.go` (89 LOC) - Variable substitution feature never integrated
- **Removed**: `Actions` field from RuleDefinition struct
- **Removed**: `ActionDefinition` type completely
- **Removed**: Skipped variable substitution test stub

**Rationale**: Variable substitution and Actions were planned for template-based event generation, but the
implementation uses direct event generation via ExecuteEvents() instead. Removing dead code improves maintainability.

### Files Removed
1. ❌ `battery_monitor_factory.go` (6.1K) - Domain-specific factory
2. ❌ `kv_battery_rule.go` (7.6K) - Domain-specific rule implementation
3. ❌ `kv_battery_rule_test.go` (7.5K) - Domain-specific tests
4. ❌ `buffer.go` (1.9K) - Redundant (use `pkg/buffer` instead)
5. ❌ `schema_discovery.go` (16K) - Unused dead code

### Modular File Structure

The monolithic `processor.go` has been refactored into focused modules:

| File | LOC | Purpose |
|------|-----|---------|
| **processor.go** | 451 | Core processor, lifecycle, ports, subscriptions |
| **metrics.go** | 119 | Prometheus metrics (RuleMetrics struct) |
| **config.go** | 100 | Config struct and defaults |
| **factory.go** | 107 | Component registration and factory |
| **publisher.go** | 91 | Event publishing to NATS |
| **entity_watcher.go** | 157 | KV entity state watching |
| **message_handler.go** | 182 | Message processing and rule evaluation |
| **rule_loader.go** | 122 | Rule loading from JSON files |
| **config_validation.go** | 279 | Configuration validation |
| **runtime_config.go** | 227 | Runtime configuration management |

### Key Benefits
✅ **Generic Implementation** - No domain-specific (battery/robotics) code
✅ **Greenfield Clean** - No legacy cruft
✅ **Maintainable** - Files average ~150 LOC (vs 1,845)
✅ **Single Responsibility** - Each file has one clear purpose
✅ **Testable** - Isolated concerns, easier to unit test
✅ **Navigable** - Clear organization by functionality

### Breaking Changes
- **Removed**: `buffer.go` internal buffering - Use `pkg/buffer` for time-window aggregation
- **Removed**: All battery-specific factories and rule types
- **Deprecated**: `CreateKVRuleFromConfig()` - Use JSON DSL via `rules_files` config instead

## Design Decisions

### Entity ID Format: Hierarchical Dotted Notation
**Decision**: Entity IDs use 6+ part hierarchical dotted notation for global uniqueness
```
Format: <org>.<platform>.<system>.<domain>.<type>.<instance>
Example: "c360.platform1.gcs1.robotics.drone.1"
See: /docs/architecture/CANONICAL_CORE_TYPES.md for authoritative definition
```

**Rationale**:
- NATS KV uses entity IDs as keys and supports wildcard patterns
- Dotted syntax aligns with NATS subject hierarchy (same wildcard rules)
- Enables federation across instances and environments
- Supports querying by pattern (e.g., "west-1.*.robotics.*")

### Metrics Pattern: "nil input = nil feature"
**Decision**: When metrics registry is nil, return nil metrics (no tracking)
```go
func newRuleMetrics(registry *metric.MetricsRegistry, _ string) *RuleMetrics {
    // Return nil if no registry provided (nil input = nil feature pattern)
    if registry == nil {
        return nil
    }
    // Only create metrics when registry is provided
    metrics := &RuleMetrics{...}
    return metrics
}
```

**Rationale**: 
- Zero overhead when metrics disabled
- Clear separation between enabled/disabled states
- Consistent across all framework components
- Resource efficient (no unnecessary Prometheus objects)

### Testing Pattern: Real Prometheus Validation
**Decision**: Use `prometheus/client_golang/prometheus/testutil` for metrics testing
**Rationale**:
- Tests validate actual behavior (can fail when broken)
- Consistent with other framework components (graph, robotics, WebSocket)
- Industry standard approach for Prometheus metrics testing
- Eliminates false positive test results from fake helpers

## Architecture Context

### Integration Points

### KV Watch Pattern
RuleProcessor watches ENTITY_STATES bucket directly for entity changes:
```go
// Watch specific patterns or all entities
watcher, _ := entityBucket.Watch("c360.platform1.robotics.*.drone.>")

// Process entity state changes
for entry := range watcher.Updates() {
    switch entry.Operation() {
    case nats.KeyValuePut:
        action := "UPDATED"
        if entry.Revision() == 1 {
            action = "CREATED"
        }
        // Process entity state from entry.Value()
    case nats.KeyValueDelete:
        // Handle entity deletion
    }
}
```

**Benefits**:
- Direct observation of source of truth (no duplicate events)
- Built-in history and replay capability
- Pattern-based watching for selective monitoring
- Guaranteed consistency with storage

- **Consumes from**: 
  - NATS semantic subjects (`process.*`) for messages
  - ENTITY_STATES KV bucket (via watch) for entity changes
- **Provides to**: Rule events (`rule.events.*`), graph mutation requests
- **External dependencies**: NATS/JetStream, Prometheus metrics registry

### Data Flow
```
process.* → Rule Evaluation → rule.events.*
                ↑
    ENTITY_STATES KV Watch → Rule Context
```

## Critical Behaviors (Testing Focus)

### Happy Path - What Should Work
1. **Rule Evaluation**: Normal rule processing against semantic messages
   - **Input**: Valid semantic message (e.g., battery payload)
   - **Expected**: Rule evaluated, metrics updated, events published if triggered
   - **Verification**: Check metrics for evaluations and triggers

2. **Message Processing**: NATS subscription and message handling
   - **Input**: Published message to subscribed subject
   - **Expected**: Message received, buffered, and processed through rules
   - **Verification**: Verify messages received metrics increment

### Error Conditions - What Should Fail Gracefully  
1. **Invalid Message Format**: Malformed JSON or missing fields
   - **Trigger**: Publish invalid JSON to semantic subject
   - **Expected**: Error recorded in metrics, processing continues
   - **Recovery**: Processor continues with next message

2. **Rule Evaluation Failure**: Rule logic throws exception
   - **Trigger**: Rule with invalid configuration or logic error
   - **Expected**: Error metrics increment, specific rule marked as failed
   - **Recovery**: Other rules continue processing normally

### Edge Cases - Boundary Conditions
- **Buffer Expiration**: Messages older than time window are expired
- **Alert Cooldown**: Rapid rule triggers respect cooldown periods  
- **High Message Volume**: System handles burst traffic without blocking

## Common Patterns

### Metrics Testing Pattern (Framework Standard)
```go
import (
    "testing"
    "github.com/prometheus/client_golang/prometheus/testutil"
    "github.com/stretchr/testify/assert"
)

func TestRuleProcessor_MetricsExample(t *testing.T) {
    registry := metric.NewMetricsRegistry()
    processor := NewProcessorWithMetrics(natsClient.Client, nil, registry)
    
    // Test counter with labels
    if processor.metrics != nil && processor.metrics.evaluationsTotal != nil {
        count := testutil.ToFloat64(processor.metrics.evaluationsTotal.WithLabelValues("rule_name", "success"))
        assert.True(t, count > 0, "Should have performed evaluations")
    }
    
    // Test gauge
    if processor.metrics != nil && processor.metrics.activeRules != nil {
        active := testutil.ToFloat64(processor.metrics.activeRules)
        assert.Equal(t, 1.0, active, "Should have 1 active rule")
    }
}
```

### Nil Safety Pattern
```go
// Always use nil safety when accessing metrics
if rp.metrics != nil {
    rp.metrics.messagesReceived.WithLabelValues(subject).Inc()
}
```

### Component Registration Pattern
```go
// Self-registration via init() - Standard framework pattern
func init() {
    component.DefaultCatalog.RegisterProcessor("rule", CreateRuleProcessor, ...)
}
```

## Usage Patterns

### Typical Usage (How Other Code Uses This)
```go
// Standard component creation through factory
config := component.ComponentConfig{
    Parameters: map[string]any{
        "input_subjects":             []string{"semantic.robotics.battery"},
        "enabled_rules":             []string{"battery_monitor"},
        "buffer_window_size":        "10m",
        "alert_cooldown_period":     "2m",
        "enable_graph_integration":  true,
    },
    NATSClient: natsClient,
}

component, err := CreateRuleProcessor(ctx, config)
if err != nil {
    // Handle error
}

err = component.Start(ctx)
```

### Direct Usage (Alternative)
```go
config := rule.DefaultConfig()
processor := rule.NewProcessor(natsClient, &config)
```

## Testing Strategy

### Test Categories
1. **Unit Tests**: Component methods and rule evaluation logic
2. **Integration Tests**: NATS integration and message processing
3. **Metrics Tests**: Prometheus metrics validation

### Test Quality Standards (Framework Reference)
- ✅ **Real metric validation**: Use `testutil.ToFloat64()` for actual prometheus metrics
- ✅ **Nil safety**: Always check `if processor.metrics != nil` before access
- ✅ **Label accuracy**: Use correct metric labels matching implementation
- ✅ **Behavioral testing**: Tests must be able to fail when implementation breaks
- ✅ **Real infrastructure**: Use testcontainers, not mocks
- ❌ **NO fake helpers**: Don't return hardcoded success values
- ❌ **NO signature tests**: Don't test just compilation/interfaces

### Testing Strategy (Reference Implementation)
This component demonstrates the **standard pattern** for metrics testing across the SemStreams framework:

**✅ Proper Pattern** (prometheus testutil):
```go
import "github.com/prometheus/client_golang/prometheus/testutil"

// Test counters directly
if processor.metrics != nil && processor.metrics.messagesReceived != nil {
    receivedCount := testutil.ToFloat64(processor.metrics.messagesReceived.WithLabelValues("subject"))
    assert.True(t, receivedCount > 0, "Should have received at least one message")
}

// Test gauges
if processor.metrics != nil && processor.metrics.activeRules != nil {
    activeRulesValue := testutil.ToFloat64(processor.metrics.activeRules)
    assert.Equal(t, 1.0, activeRulesValue, "Should have 1 active rule")
}
```

**❌ Anti-Pattern** (fake helpers):
```go
// DON'T DO THIS - returns hardcoded success values
func getCounterValue(t *testing.T, metrics map[string]float64, name string) float64 {
    return 1.0  // Can never fail!
}
```

### Integration Test Plan

Post-refactoring validation requires comprehensive integration tests with real NATS infrastructure.

**Test File**: `rule_integration_test.go`
**Pattern**: Follow `json_filter_integration_test.go` (testcontainers + shared NATS client)
**Guard**: `INTEGRATION_TESTS=1` environment variable

#### Integration Test Scenarios

1. **KV Entity State Watch and Rule Triggering**
   - **Setup**: Create rule processor with entity watch patterns
   - **Action**: Update entity state in ENTITY_STATES KV bucket
   - **Verify**: Rule evaluates entity state change, triggers event published to rule.events.*
   - **Validates**: entity_watcher.go, message_handler.go, publisher.go

2. **Dynamic Rule CRUD via Runtime Config**
   - **Setup**: Create processor with initial rules
   - **Action**: Use ApplyConfigUpdate() to add/modify/remove rules
   - **Verify**: Rules map updated, old rule engines stopped, new rules activated
   - **Validates**: runtime_config.go, config_validation.go

3. **JSON DSL Rule Loading from Files**
   - **Setup**: Create JSON rule definition file
   - **Action**: Configure rules_files parameter, initialize processor
   - **Verify**: Rules loaded from file, factory creates correct rule instances
   - **Validates**: rule_loader.go, factory pattern

4. **Prometheus Metrics Recording**
   - **Setup**: Processor with metrics registry
   - **Action**: Process messages, trigger rules, generate errors
   - **Verify**: All counters/gauges/histograms updated correctly
   - **Validates**: metrics.go

5. **Graph Integration Event Publishing**
   - **Setup**: Processor with enable_graph_integration=true
   - **Action**: Trigger rule that generates graph mutations
   - **Verify**: Events published to graph.mutations subject
   - **Validates**: publisher.go graph integration

#### E2E Test Scenario

**Full Pipeline Test** (`test/e2e/scenarios/rule_processor_e2e.yaml`):
1. Publish semantic message to process.* subject
2. Rule processor evaluates message against loaded rules
3. Rule triggers, publishes to rule.events.*
4. Graph processor consumes rule event, creates entity
5. Entity state change written to ENTITY_STATES KV
6. Rule processor KV watch detects change, evaluates secondary rule
7. Verify full event chain with real NATS streams

**Status**:
- ✅ Integration test file creation - COMPLETE (`rule_integration_test.go`)
- ⏸️ E2E scenario definition - PENDING

## Framework Reference
This component serves as a **reference implementation** for:
- Metrics testing patterns using prometheus testutil
- "nil input = nil feature" pattern for optional components  
- Real behavior testing with testcontainers
- Component self-registration via init()

Other SemStreams components should follow the patterns established here.

## Metrics

This component exposes the following Prometheus metrics to monitor rule evaluation performance and trigger rates:

| Metric | Type | Description | Labels |
|--------|------|-------------|--------|
| semstreams_rule_messages_received_total | Counter | Total messages received for rule evaluation | subject |
| semstreams_rule_evaluations_total | Counter | Total rule evaluations performed | rule_name, result |
| semstreams_rule_triggers_total | Counter | Total rule triggers (successful evaluations) | rule_name, severity |
| semstreams_rule_evaluation_duration_seconds | Histogram | Time spent evaluating individual rules | rule_name |
| semstreams_rule_buffer_size | Gauge | Current message buffer size per rule | rule_name |
| semstreams_rule_buffer_expired_total | Counter | Messages expired from time windows | rule_name |
| semstreams_rule_cooldown_active | Gauge | Rules currently in cooldown state | rule_name |
| semstreams_rule_events_published_total | Counter | Rule events published to NATS | subject, event_type |
| semstreams_rule_errors_total | Counter | Rule processing errors | rule_name, error_type |
| semstreams_rule_active_rules | Gauge | Number of active rules loaded | - |

### Key Performance Indicators
- **Rule Efficiency**: `semstreams_rule_triggers_total / semstreams_rule_evaluations_total` - Rule trigger success rate
- **Evaluation Latency**: `histogram_quantile(0.95, semstreams_rule_evaluation_duration_seconds)` - P95 rule evaluation time
- **Message Throughput**: `rate(semstreams_rule_messages_received_total[1m])` - Messages/sec being evaluated
- **Buffer Health**: `semstreams_rule_buffer_size` - Time-window buffer utilization
- **Error Rate**: `rate(semstreams_rule_errors_total[1m])` - Rule processing errors/sec

### Example Prometheus Queries
```promql
# Rule trigger rates by rule
sum(rate(semstreams_rule_triggers_total[1m])) by (rule_name)

# Rules with high evaluation latency
histogram_quantile(0.95, sum(semstreams_rule_evaluation_duration_seconds) by (rule_name, le)) > 0.01

# Rules currently in cooldown
semstreams_rule_cooldown_active == 1

# Buffer utilization per rule
avg(semstreams_rule_buffer_size) by (rule_name)

# Most frequently triggered rules
topk(5, sum(increase(semstreams_rule_triggers_total[1h])) by (rule_name))

# Rule processing error rates
sum(rate(semstreams_rule_errors_total[5m])) by (rule_name, error_type) > 0
```

### Alerting Rules
```yaml
# High rule evaluation latency
- alert: RuleProcessorSlowEvaluation
  expr: histogram_quantile(0.95, semstreams_rule_evaluation_duration_seconds) > 0.05
  for: 3m
  annotations:
    summary: "Rule processor P95 evaluation latency >50ms"

# Rule evaluation errors
- alert: RuleProcessorEvaluationErrors
  expr: rate(semstreams_rule_errors_total[5m]) > 0.5
  for: 2m
  annotations:
    summary: "Rule processor error rate >0.5 errors/sec for rule {{ $labels.rule_name }}"

# Buffer overflow risk
- alert: RuleProcessorBufferHigh
  expr: semstreams_rule_buffer_size > 1000
  for: 5m
  annotations:
    summary: "Rule processor buffer size >1000 for rule {{ $labels.rule_name }}"

# Rule never triggering (potential misconfiguration)
- alert: RuleNeverTriggering
  expr: sum(increase(semstreams_rule_triggers_total[24h])) by (rule_name) == 0 and sum(increase(semstreams_rule_evaluations_total[24h])) by (rule_name) > 100
  for: 1h
  annotations:
    summary: "Rule {{ $labels.rule_name }} evaluated 100+ times but never triggered"
```

## Implementation Notes

### Thread Safety
- **Concurrency model**: Goroutines with channel-based communication
- **Shared state**: Rule buffers protected by mutex synchronization
- **Critical sections**: Metrics updates and buffer operations

### Performance Considerations
- **Expected throughput**: 1000+ messages/sec per rule
- **Memory usage**: Proportional to buffer window size and rule count
- **Bottlenecks**: Rule evaluation complexity and buffer size

### Error Handling Philosophy
- **Error propagation**: Errors logged but don't stop processing
- **Retry strategy**: JetStream handles message redelivery
- **Circuit breaking**: Individual rule failures don't affect other rules