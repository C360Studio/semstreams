# Configuration Package README

**Last Updated**: 2025-09-12  
**Maintainer**: SemStreams core Team

## Purpose & Scope

**What this component does**: Manages application configuration for SemStreams with support for JSON file loading, environment variable overrides, NATS KV-based dynamic configuration, and thread-safe configuration updates.

**Key responsibilities**:

- Load and parse JSON configuration files with layered overrides and defaults
- Provide thread-safe concurrent access to configuration data via SafeConfig wrapper
- Monitor NATS KV bucket for real-time configuration changes using Manager
- Validate configuration data with extensible validation framework
- Merge base configuration with dynamic KV overrides
- Provide channel-based configuration updates to services
- Support both typed (strongly structured) and flexible (map-based) configuration formats

**NOT responsible for**: Component lifecycle management, NATS connection establishment, business logic implementation, or configuration persistence (beyond JSON file writing).

## Architecture Context

### Integration Points

- **Consumes from**: JSON configuration files, environment variables, NATS KV bucket "semstreams_config"
- **Provides to**: All components requiring configuration (processors, managers, services, input handlers)
- **External dependencies**: NATS JetStream for KV operations, file system for JSON file I/O

### Data Flow

```text
JSON Files + Env Vars → Loader → Config → SafeConfig → Services/Components
                                    ↓
NATS KV Changes → Manager → Channel Updates → Service/Component Updates
```

### Configuration Structure

The configuration has been simplified to four main sections:

```json
{
  "platform": {
    "org": "c360",                         // Organization namespace
    "id": "vessel-001",                    // Platform identifier
    "type": "vessel",                      // Platform type (vessel, shore, buoy, satellite)
    "region": "gulf_mexico",               // Geographic region
    "capabilities": ["radar", "ctd"],      // Platform capabilities
    "instance_id": "west-1",               // Instance identifier for federation
    "environment": "prod"                  // Environment (prod, dev, test)
  },
  "nats": {
    "urls": ["nats://localhost:4222"],     // NATS server URLs
    "max_reconnects": -1,                  // Unlimited reconnection attempts
    "reconnect_wait": "2s",                // Wait between reconnect attempts
    "username": "",                        // Optional username
    "password": "",                        // Optional password
    "token": "",                           // Optional auth token
    "tls": {                               // TLS configuration
      "enabled": false,
      "cert_file": "",
      "key_file": "",
      "ca_file": ""
    },
    "jetstream": {                         // JetStream configuration
      "enabled": true,
      "domain": "semstreams"
    }
  },
  "services": {                           // Service configurations (JSON-only format)
    "metrics": {                          // Each service gets complete JSON config
      "enabled": true,
      "port": 9090,
      "path": "/metrics"
    },
    "message_logger": {
      "enabled": true,
      "monitor_subjects": ["process.>", "input.>"],
      "max_entries": 10000,
      "output_to_stdout": false
    },
    "health": {
      "enabled": true,
      "port": 8080,
      "path": "/health"
    }
  },
  "components": {                         // Component instance configurations
    "udp-mavlink": {                      // Instance name as map key
      "type": "udp-input",
      "enabled": true,
      "config": {                        // Component-specific configuration
        "port": 14550,
        "bind": "0.0.0.0",
        "subject": "input.udp.mavlink"
      }
    },
    "graph-processor": {
      "type": "graph-processor",
      "enabled": true,
      "config": {
        "entity_bucket": "ENTITY_STATES",
        "cache_size": 10000
      }
    },
    "websocket-output": {
      "type": "websocket-output",
      "enabled": true,
      "config": {
        "port": 8081,
        "subjects": ["output.>"]
      }
    }
  }
}
```

## Critical Behaviors (Testing Focus)

### Happy Path - What Should Work

1. **JSON Configuration Loading**: Parse configuration files with defaults and overrides
   - **Input**: Valid JSON file path with proper structure
   - **Expected**: Config struct populated with merged values (defaults + file + env)
   - **Verification**: Assert specific field values match expected merged result

2. **Thread-Safe Configuration Access**: Concurrent read/write operations on SafeConfig
   - **Input**: Multiple goroutines calling Get() and Update() simultaneously
   - **Expected**: No race conditions, consistent data returned via deep copying
   - **Verification**: Use `-race` flag, verify no data corruption under concurrent load

3. **NATS KV Configuration Watching**: Real-time configuration change detection
   - **Input**: Write configuration change to NATS KV bucket
   - **Expected**: Configuration updates received via channel within timeout
   - **Verification**: Channel receives Update with correct path and updated config

4. **Configuration Validation**: Reject invalid configuration data
   - **Input**: Config with invalid platform.id, missing required fields, or malformed URLs
   - **Expected**: Specific validation errors returned, config rejected
   - **Verification**: Assert exact error messages match expected validation failures

5. **Environment Variable Override**: Limited environment variable support for specific fields
   - **Input**: Set STREAMKIT_PLATFORM_ID="env_test" with different file value
   - **Expected**: Final config has environment variable value (if non-empty string)
   - **Verification**: Assert Platform.ID equals environment variable, not file value
   - **Limitations**: Cannot set booleans to false or integers to 0 (empty string check)
   - **Supported Fields**: Platform.ID, Platform.Type, NATS.URLs, NATS.Username, NATS.Password, NATS.Token

6. **Dynamic Configuration Merging**: KV overrides merge with base configuration
   - **Input**: Base config with KV key "platform.identity" containing {"type": "shore"}
   - **Expected**: GetConfig() returns merged config with KV override applied
   - **Verification**: Assert Platform.Type equals KV value, other fields unchanged
   - **Note**: KV uses JSON-only format - entire objects, not individual fields

### Error Conditions - What Should Fail Gracefully

1. **Invalid JSON Configuration**: Malformed JSON files handled gracefully
   - **Trigger**: Load file with syntax errors, missing braces, invalid JSON
   - **Expected**: Specific parsing error with file path and line information
   - **Recovery**: System continues with defaults, logs error for debugging

2. **Missing Required Configuration**: Required fields validation failures
   - **Trigger**: Config with empty platform.id or missing platform.org
   - **Expected**: ValidationError with specific field names and requirements
   - **Recovery**: Configuration loading fails early before component initialization

3. **NATS KV Connection Loss**: Handle KV bucket unavailability
   - **Trigger**: NATS connection lost or KV bucket deleted during operation
   - **Expected**: Manager continues with current config, channel updates pause
   - **Recovery**: Reconnects when NATS available, resumes KV watching automatically

4. **Configuration Update Channel Blocking**: Handle slow consumers
   - **Trigger**: Service doesn't read from config update channel quickly
   - **Expected**: Non-blocking sends, slow consumers miss updates (buffered channel)
   - **Recovery**: Manager continues monitoring, consumers get latest state on next read

5. **Invalid Environment Variable Values**: Environment override validation failures
   - **Trigger**: Set STREAMKIT_NATS_URLS="invalid-url-format"
   - **Expected**: Validation error during config loading with specific field
   - **Recovery**: Use file or default value, log validation failure

6. **Concurrent Configuration Updates**: Race condition prevention during updates
   - **Trigger**: Multiple threads calling Update() with different configs simultaneously
   - **Expected**: All updates serialized, final state consistent (last writer wins)
   - **Recovery**: No data corruption, all readers get valid configuration

### Edge Cases - Boundary Conditions

- **Large Configuration Files**: Handle 10MB+ JSON files within memory limits
- **High-Frequency KV Changes**: Process rapid-fire configuration updates without dropping changes
- **Deep Configuration Nesting**: Support 10+ levels of nested configuration objects
- **Unicode Configuration Values**: Properly handle UTF-8 characters in config values
- **Long-Running Watchers**: Manager operates reliably for days

## Usage Patterns

### Typical Usage (How Other Code Uses This)

```go
// Load configuration with file layering (map-based merge)
loader := NewLoader()
loader.AddLayer("configs/base.json")      // Base configuration
loader.AddLayer("configs/production.json") // Override layer (CAN set zero values)
loader.EnableValidation(true)

// Map-based merging preserves JSON semantics:
// - Fields with zero values in overlay WILL override non-zero base values
// - Fields not present in overlay keep their base values
config, err := loader.Load()
if err != nil {
    log.Fatalf("Failed to load config: %v", err)
}

// Create Manager for centralized configuration management
configManager, err := NewConfigManager(config, natsClient)
if err != nil {
    log.Fatalf("Failed to create config manager: %v", err)
}

// Start Manager to watch for KV changes
if err := configManager.Start(ctx); err != nil {
    log.Fatalf("Failed to start config manager: %v", err)
}
defer configManager.Stop()

// Subscribe to configuration changes using pattern-based subscriptions
configChan := make(chan Update, 10)
configManager.Subscribe(ctx, "services.*", configChan)

// Process configuration updates in a goroutine
go func() {
    for update := range configChan {
        log.Printf("Config changed: %s", update.Path)
        cfg := update.Config.Get()
        // Apply new configuration to relevant services
        // Services receive complete JSON configs, not individual fields
    }
}()

// Use configuration safely in components
safeConfig := configManager.GetConfig()
cfg := safeConfig.Get()
platformID := cfg.Platform.ID
natsUrls := cfg.NATS.URLs

// Access service configurations (JSON format)
if metricsConfig, exists := cfg.Services["metrics"]; exists {
    // Parse service-specific configuration
    var metrics MetricsConfig
    json.Unmarshal(metricsConfig.Config, &metrics)
}
```

### Common Integration Patterns

- **Component Initialization**: Components receive ComponentConfig from the components map
- **Service Initialization**: Services receive ServiceConfig JSON from the services map
- **Dynamic Reconfiguration**: Services subscribe to Manager channels for real-time updates
- **Pattern-Based Subscriptions**: Use wildcards like "services.*" or "components.*"
- **JSON-Only Updates**: All configuration updates replace entire JSON objects, not individual fields

## Testing Strategy

### Test Categories

1. **Unit Tests**: Individual functions (validation, parsing, merging) with isolated inputs
2. **Integration Tests**: Full config loading with real NATS server and file system operations
3. **Concurrency Tests**: Thread safety verification with race detection enabled
4. **Error Tests**: Failure mode handling and recovery behavior validation

### Test Quality Standards

- ✅ Tests MUST create real configuration objects and verify actual behavior
- ✅ Tests MUST verify thread safety using `go test -race` without warnings
- ✅ Tests MUST test with real NATS KV buckets (use testcontainers for isolation)
- ✅ Tests MUST verify configuration changes trigger handler callbacks
- ✅ Tests MUST validate specific error messages and types for failure cases
- ❌ NO tests that only verify struct field assignment without behavior validation
- ❌ NO tests that mock the core configuration loading logic
- ❌ NO tests that skip validation of concurrent access patterns

### Mock vs Real Dependencies

- **Use real dependencies for**: File system operations, JSON parsing, NATS KV buckets, configuration validation
- **Use mocks for**: Component-specific configuration consumers that would create circular dependencies
- **Testcontainers for**: NATS server instances to ensure isolated testing environment

## Implementation Notes

### Configuration Merge Implementation

The loader uses **map-based merging** that correctly handles zero values:

```go
// Files are loaded as map[string]any, not structs
rawConfig, err := loadRawJSON("production.json")
// rawConfig only contains fields present in the JSON file

// Deep merge preserves JSON semantics
func deepMergeMaps(base, override map[string]any) map[string]any {
    // Zero values in JSON (false, 0, "") are NOT nil in maps
    // They WILL override non-zero base values
}
```

**Key Properties:**

- ✅ CAN override `true` with `false`
- ✅ CAN override `9090` with `0`
- ✅ CAN override `["a","b"]` with `[]`
- ✅ Fields not in overlay file are preserved from base

**Note:** This is different from struct-based merging, which would have zero-value ambiguity.

### Thread Safety

- **Concurrency model**: SafeConfig uses RWMutex for protecting configuration access and updates
- **Deep Copying**: Get() returns a deep copy using JSON marshal/unmarshal to prevent mutations
- **Shared state**: Configuration data protected by mutex, Manager sends updates via channels
- **Critical sections**: Configuration updates (Update method) and KV override application require write locks

### Performance Considerations

- **Expected throughput**: Handle 100+ configuration changes per second through KV watching
- **Memory usage**: Configuration structures typically under 1MB, deep copying for safety
- **Bottlenecks**: JSON marshaling/unmarshaling for deep copying, NATS KV round-trip latency

### Error Handling Philosophy

- **Error propagation**: Validation errors bubble up with specific field context and helpful messages
- **Retry strategy**: Manager automatically reconnects to NATS, updates resume when available
- **Circuit breaking**: Invalid configurations rejected early, system continues with known-good config

## Troubleshooting

### Common Issues

1. **Manager Not Receiving Updates**: KV changes not detected
   - **Cause**: Channel subscription after KV changes, or NATS connection loss
   - **Solution**: Subscribe to patterns before changes, check NATS connection health

2. **Configuration Validation Failures**: Valid configuration rejected by validator
   - **Cause**: Strict validation rules, missing required fields, or format mismatches
   - **Solution**: Review ValidationError details, check field requirements and format constraints

3. **Thread Safety Race Conditions**: Data corruption under concurrent access
   - **Cause**: Direct Config access bypassing SafeConfig wrapper
   - **Solution**: Always use SafeConfig.Get() for reading, Update() for writing configuration

4. **Environment Variables Not Applied**: Environment overrides ignored
   - **Cause**: Incorrect variable naming, env vars set after loader creation
   - **Solution**: Use STREAMKIT_ prefix, verify environment variables before loader.Load()

5. **Channel Buffer Overflow**: Missing configuration updates
   - **Cause**: Slow consumer not reading from channel, buffer fills up
   - **Solution**: Process updates promptly, increase buffer size, or use select with default

### Debug Information

- **Logs to check**: Configuration loading progress, validation failures, KV watcher status
- **Metrics to monitor**: Number of subscribers, configuration change frequency, channel buffer usage
- **Health checks**: NATS connection state, KV bucket accessibility, channel status

## Development Workflow

### Before Making Changes

1. Read this README to understand component purpose and integration points
2. Check validation system to understand current constraints and extension points  
3. Identify which behaviors need testing (configuration loading, validation, KV watching)
4. Update tests BEFORE changing code (TDD approach for configuration behavior)

### After Making Changes

1. Verify all existing tests still pass with `go test -race` enabled
2. Add tests for new configuration fields or validation rules
3. Update this README if responsibilities or integration points changed
4. Check integration points still work (component initialization, dynamic reconfiguration)
5. Update configuration examples if new fields or formats added

## Related Documentation

- [Component Architecture Guide](/docs/architecture/V1_BACKEND_ARCHITECTURE.md) - Overall system design
- [KV Configuration Schema](/docs/architecture/KV_CONFIG_SCHEMA.md) - NATS KV configuration contract
- [Manager Design](/docs/architecture/CONFIG_MANAGER_DESIGN.md) - Channel-based config updates
- [Development Standards](/CLAUDE.md) - Code quality and testing standards
- [Integration Testing Guide](/tests/e2e/README.md) - End-to-end testing approach
