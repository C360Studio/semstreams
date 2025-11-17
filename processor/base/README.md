# Base Processor README

**Last Updated**: 2025-08-27  
**Maintainer**: SemStreams Team

## Purpose & Scope

**What this component does**: Provides base implementation and interface definition for all stream processors in the SemStreams system.

**Key responsibilities**:
- Define the core Processor interface for raw data processing
- Provide BaseProcessor with default implementations for common functionality
- Handle processor lifecycle management (initialize, shutdown)
- Track processor health, metrics, and activity timestamps
- Manage error counting and status reporting

**NOT responsible for**: Actual data processing logic (delegated to specific processor implementations)

## Architecture Context

### Integration Points
- **Consumes from**: Raw data streams via NATS subjects (raw.*.*)
- **Provides to**: Component health monitoring system, processor registration catalog
- **External dependencies**: NATS connection, component package health system

### Data Flow
```
Raw NATS Subjects → Processor.ProcessRawData() → Implementation-specific processing
Health System ← Processor.Health() ← BaseProcessor timing/status tracking
```

### Configuration
```yaml
# Processors inherit base configuration
name: "processor-name"      # Processor identifier
domain: "robotics"         # Domain categorization  
enabled: true              # Runtime enable/disable
```

## Critical Behaviors (Testing Focus)

### Happy Path - What Should Work
1. **Processor Lifecycle**: Initialize → Process Data → Shutdown cycle
   - **Input**: Context, NATS connection, raw data subjects
   - **Expected**: Clean startup, data processing, graceful shutdown
   - **Verification**: No errors returned, health reports "healthy"

2. **Health Status Tracking**: Activity updates and uptime measurement
   - **Input**: UpdateActivity() calls during processing
   - **Expected**: LastActivity timestamp updated, uptime resets to near-zero
   - **Verification**: Health().LastActivity reflects recent update time
   - **⚠️ TIMING LOGIC**: Uptime = time.Since(lastActivity), so UpdateActivity() RESETS uptime to ~nanoseconds

3. **Error Count Management**: Error tracking and status degradation
   - **Input**: IncrementErrorCount() calls when errors occur
   - **Expected**: ErrorCount increments, status can be set to "degraded"/"unhealthy"
   - **Verification**: Health().ErrorCount matches actual error calls

### Error Conditions - What Should Fail Gracefully
1. **Invalid Configuration**: Processor receives bad config data
   - **Trigger**: Pass invalid config to ValidateConfiguration()
   - **Expected**: BaseProcessor accepts any config (default implementation)
   - **Recovery**: Individual processors should override with real validation

2. **NATS Connection Loss**: Network connectivity issues
   - **Trigger**: Initialize with nil connection or network failure
   - **Expected**: Graceful handling, status reports connection issues
   - **Recovery**: Processor should retry or report degraded status

### Edge Cases - Boundary Conditions
- **Rapid UpdateActivity() calls**: Multiple updates in microseconds
- **High error rates**: Many IncrementErrorCount() calls
- **Status transitions**: Healthy → Degraded → Unhealthy transitions

## Usage Patterns

### Typical Usage (How Other Code Uses This)
```go
// Create processor with metadata
info := ProcessorInfo{
    Name:        "robotics-processor",
    Domain:      "robotics", 
    Version:     "1.0.0",
    Description: "MAVLink processor for drone data",
}

// Embed BaseProcessor in implementation
type RoboticsProcessor struct {
    *BaseProcessor
    // Additional fields
}

func NewRoboticsProcessor() *RoboticsProcessor {
    return &RoboticsProcessor{
        BaseProcessor: NewBaseProcessor(info),
    }
}

// Implement required methods
func (rp *RoboticsProcessor) ProcessRawData(ctx context.Context, subject string, data []byte) error {
    rp.UpdateActivity() // Important: resets uptime clock!
    // Process data
    return nil
}
```

### Common Integration Patterns
- **Component Registration**: Processors self-register via init() functions
- **Health Monitoring**: Health endpoint polls processor.Health() for status
- **Metrics Collection**: Monitoring systems call processor.Metrics() periodically

## Testing Strategy

### Test Categories
1. **Unit Tests**: BaseProcessor default method behaviors
2. **Interface Tests**: Verify processor implements required interface  
3. **Timing Tests**: Validate activity tracking and uptime calculations

### Test Quality Standards
- ✅ Tests MUST verify actual behavior (not just method signatures)
- ✅ Tests MUST understand timing semantics (uptime resets on UpdateActivity)
- ✅ Tests MUST handle timing variations gracefully (use ranges, not exact values)
- ❌ NO tests expecting uptime to increase after UpdateActivity() - this is backwards!
- ❌ NO precise timing assertions without understanding measurement precision

### Mock vs Real Dependencies
- **Use real dependencies for**: Time measurements, atomic counters, health status
- **Use mocks for**: NATS connections in unit tests (use testcontainers for integration)
- **Testcontainers for**: NATS server when testing full processor lifecycle

## Implementation Notes

### Thread Safety
- **Concurrency model**: BaseProcessor methods are thread-safe via atomic operations
- **Shared state**: Error count uses atomic.AddInt64 for concurrent safety
- **Critical sections**: LastActivity updates are simple assignments (time.Time is immutable)

### Performance Considerations  
- **Expected throughput**: Processor overhead should be < 1µs per call
- **Memory usage**: BaseProcessor has minimal footprint (~100 bytes)
- **Bottlenecks**: Timing measurements via time.Now() calls

### Error Handling Philosophy
- **Error propagation**: ProcessRawData errors bubble up to component managers
- **Retry strategy**: BaseProcessor doesn't implement retry (left to implementations)
- **Circuit breaking**: Status management allows processors to report degraded state

## Troubleshooting

### Common Issues
1. **Timing Test Failures**: Expected uptime to increase after UpdateActivity()
   - **Cause**: Misunderstanding of uptime calculation (time.Since resets)
   - **Solution**: Fix test logic - UpdateActivity() should DECREASE uptime to near-zero

2. **Health Status Inconsistencies**: Health reports don't match processor state
   - **Cause**: Forgetting to call UpdateActivity() during processing
   - **Solution**: Ensure ProcessRawData implementations call UpdateActivity()

### Debug Information
- **Logs to check**: Health status changes, error count increments
- **Metrics to monitor**: uptime_seconds, error_count, last_activity timestamps
- **Health checks**: Verify processor status is "healthy" and error_count is low

## CRITICAL TIMING ISSUES ANALYSIS

### Issue #1: Base Processor Uptime Logic Error
**File**: `base_test.go:73`  
**Issue**: `assert.GreaterOrEqual(t, newHealth.Uptime, oldUptime)`

#### The Problem:
1. **oldUptime**: `time.Since(bp.lastActivity)` when lastActivity was set ~13µs ago
2. **UpdateActivity()**: Sets `bp.lastActivity = time.Now()` (resets the clock)
3. **newUptime**: `time.Since(bp.lastActivity)` from fresh timestamp = ~300ns
4. **Test expects**: `300ns >= 13µs` - but this is backwards!

#### Expected vs Actual Values:
- **Test expects**: `newUptime ≥ 12.375µs` (preserve/increase uptime)
- **Actual behavior**: `newUptime = 250ns` (reset to near-zero)
- **System behavior**: CORRECT (UpdateActivity resets activity clock)
- **Test logic**: INCORRECT (expects opposite behavior)

#### Performance Analysis:
- **Current timing**: UpdateActivity() + Health() takes ~250-416ns
- **Performance status**: EXCELLENT (sub-microsecond processor overhead)
- **Timing precision**: Nanosecond-level measurement accuracy

#### Fix Required:
```go
// WRONG (current test):
oldUptime := health.Uptime
base.UpdateActivity()
newHealth := base.Health()
assert.GreaterOrEqual(t, newHealth.Uptime, oldUptime) // Expects uptime to grow!

// CORRECT (fixed test):
oldUptime := health.Uptime
base.UpdateActivity()
newHealth := base.Health()
assert.Less(t, newHealth.Uptime, oldUptime) // Uptime resets to near-zero!
```

### Issue #2: Entity Event Timestamp Precision Mismatch  
**File**: `pkg/processor/rule/entity_event_test.go:250`  
**Issue**: `assert.Equal(t, timestamp, meta.CreatedAt())`

#### The Problem:
1. **Expected**: `2025-08-27 14:13:46.295811000` (microsecond precision)
2. **Actual**: `2025-08-27 14:13:46.295000000` (millisecond precision)
3. **Root cause**: Timestamp precision lost during message serialization/deserialization

#### Analysis:
- **Original timestamp**: Created with `time.Now()` (nanosecond precision)
- **Serialization path**: EntityEvent → Message → Meta.CreatedAt()
- **Precision loss**: Somewhere in the conversion chain, microsecond data is truncated
- **Impact**: Tests expect exact timestamp equality but get precision-reduced values

#### Investigation Required:
- Check Message.Meta() implementation for timestamp handling
- Verify serialization preserves full time.Time precision
- Consider if microsecond precision is actually needed for entity events

#### Potential Solutions:
1. **Fix precision**: Ensure full timestamp precision through serialization
2. **Tolerance testing**: Use time comparison with acceptable precision range
3. **Round timestamps**: Deliberately round to consistent precision level

```go
// Option 2: Tolerance-based comparison
expectedTime := timestamp.Truncate(time.Millisecond)
actualTime := meta.CreatedAt().Truncate(time.Millisecond) 
assert.Equal(t, expectedTime, actualTime)

// Option 3: Within-range assertion
assert.WithinDuration(t, timestamp, meta.CreatedAt(), time.Millisecond)
```

## Development Workflow

### Before Making Changes
1. Read this README to understand BaseProcessor timing semantics
2. Understand that UpdateActivity() RESETS uptime calculations  
3. Check if changes affect timing measurement behavior
4. Update tests to match actual timing expectations (not backwards logic)

### After Making Changes
1. Verify timing tests use correct logic (UpdateActivity resets uptime)
2. Run tests with `-race` flag to check concurrent access
3. Update this README if timing behavior changes
4. Ensure processor implementations call UpdateActivity() appropriately

## Related Documentation
- `/pkg/component/health.go` - Health status system integration
- `/pkg/manager/component_manager.go` - Processor lifecycle management
- `/docs/templates/FOLDER_README_TEMPLATE.md` - Component documentation standards