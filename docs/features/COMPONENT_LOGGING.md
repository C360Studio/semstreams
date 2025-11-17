# Component Logging and SSE Log Streaming

## Overview

This feature implements structured logging for components with real-time streaming via Server-Sent Events (SSE) for the Flow Builder UI. Component logs are published to NATS and streamed to connected clients.

## Architecture

```
┌─────────────────┐
│   Component     │
│  (with logger)  │
└────────┬────────┘
         │ Publish logs
         │ to NATS
         ↓
┌──────────────────────────────┐
│ NATS (logs.{flow_id}.{comp}) │
└────────┬─────────────────────┘
         │ Subscribe
         ↓
┌─────────────────────────────┐
│ FlowService SSE Handler     │
│ /flows/{id}/runtime/logs    │
└────────┬────────────────────┘
         │ SSE Stream
         ↓
┌─────────────────┐
│  UI Logs Tab    │
└─────────────────┘
```

## Component Logging

### ComponentLogger

Components use `ComponentLogger` for structured logging that automatically publishes to NATS:

```go
import (
    "github.com/c360/semstreams/component"
    "log/slog"
)

// Create logger for a component
logger := component.NewComponentLogger(
    "my-component",  // component name
    "flow-123",      // flow ID
    natsConn,        // *nats.Conn
    slog.Default(),  // fallback logger
)

// Log at different levels
logger.Debug("Starting initialization")
logger.Info("Connected to upstream service")
logger.Warn("Retry attempt 3 of 5")
logger.Error("Failed to process message", err)
```

### Log Levels

- **DEBUG**: Detailed diagnostic information
- **INFO**: General informational messages
- **WARN**: Warning messages for potentially problematic situations
- **ERROR**: Error messages with optional stack traces

### Log Entry Format

```json
{
  "timestamp": "2025-11-17T14:23:01.234567890Z",
  "level": "INFO",
  "component": "udp-source",
  "flow_id": "flow-123",
  "message": "Listening on 0.0.0.0:5000",
  "stack": "optional stack trace for errors"
}
```

## SSE Streaming Endpoint

### Endpoint Specification

**URL**: `GET /flowbuilder/flows/{id}/runtime/logs`

**Query Parameters**:
- `level` (optional): Filter by log level (DEBUG, INFO, WARN, ERROR)
- `component` (optional): Filter by component name

**Response Format**: Server-Sent Events (text/event-stream)

### SSE Events

#### Connected Event
```
event: connected
data: {"flow_id":"flow-123","level_filter":"ERROR","component_filter":""}
```

#### Log Event
```
event: log
data: {"timestamp":"2025-11-17T14:23:01.234Z","level":"INFO","component":"udp-source","flow_id":"flow-123","message":"Started"}
```

#### Error Event
```
event: error
data: {"error":"subscription failed","details":"NATS connection lost"}
```

## Usage Examples

### Frontend - Subscribe to All Logs

```javascript
const eventSource = new EventSource('/flowbuilder/flows/flow-123/runtime/logs');

eventSource.addEventListener('connected', (e) => {
  const data = JSON.parse(e.data);
  console.log('Connected:', data);
});

eventSource.addEventListener('log', (e) => {
  const logEntry = JSON.parse(e.data);
  console.log(`[${logEntry.level}] ${logEntry.component}: ${logEntry.message}`);
});

eventSource.addEventListener('error', (e) => {
  const error = JSON.parse(e.data);
  console.error('Stream error:', error);
});

// Cleanup
eventSource.close();
```

### Frontend - Filter by Level and Component

```javascript
const eventSource = new EventSource(
  '/flowbuilder/flows/flow-123/runtime/logs?level=ERROR&component=processor'
);
// Only receives ERROR logs from "processor" component
```

### Backend - Component Integration

```go
package mycomponent

import (
    "github.com/c360/semstreams/component"
    "github.com/nats-io/nats.go"
    "log/slog"
)

type MyComponent struct {
    name   string
    flowID string
    logger *component.ComponentLogger
    // ... other fields
}

func New(name, flowID string, nc *nats.Conn) *MyComponent {
    return &MyComponent{
        name:   name,
        flowID: flowID,
        logger: component.NewComponentLogger(name, flowID, nc, slog.Default()),
    }
}

func (c *MyComponent) Start(ctx context.Context) error {
    c.logger.Info("Starting component")

    // Component logic...
    if err := c.connect(); err != nil {
        c.logger.Error("Failed to connect", err)
        return err
    }

    c.logger.Info("Component started successfully")
    return nil
}
```

## Implementation Details

### NATS Subject Pattern

Logs are published to: `logs.{flow_id}.{component_name}`

Example subjects:
- `logs.flow-123.udp-source`
- `logs.flow-123.processor`
- `logs.flow-123.websocket-output`

The SSE handler subscribes to `logs.{flow_id}.>` to receive all logs for a flow.

### Connection Management

- **Automatic Reconnection**: Browser automatically reconnects on disconnection
- **Graceful Shutdown**: Handler cleans up NATS subscription when client disconnects
- **Buffering**: 100-entry channel buffer to handle bursts (drops on overflow)
- **Context Cancellation**: Respects HTTP request context for clean shutdown

### Error Handling

1. **Invalid Parameters**: Returns HTTP 400 with error message
2. **Flow Not Found**: Returns HTTP 404
3. **NATS Unavailable**: Returns HTTP 503
4. **Marshal Errors**: Logged but don't interrupt stream
5. **Channel Full**: Logs warning and drops entry

## Testing

### Unit Tests

```bash
go test -v ./component -run TestComponentLogger
go test -v ./service -run TestHandleRuntimeLogs
```

### Integration Tests

```bash
INTEGRATION_TESTS=1 go test -v ./component -run TestComponentLogger_LogLevels
INTEGRATION_TESTS=1 go test -v ./service -run TestHandleRuntimeLogs
```

## Performance Considerations

1. **Log Volume**: Each log entry is ~200 bytes JSON
2. **NATS Overhead**: Minimal - pub/sub is highly efficient
3. **SSE Bandwidth**: Depends on log frequency (typically <10 KB/s per flow)
4. **Buffer Size**: 100 entries prevents blocking during bursts
5. **Filtering**: Applied in handler to reduce client bandwidth

## Security

- No authentication required (same security model as other flow endpoints)
- Log content should not include sensitive data (passwords, tokens, etc.)
- Subject pattern prevents cross-flow log leakage

## Future Enhancements

1. **Log Persistence**: Store logs in JetStream for historical queries
2. **Log Search**: Full-text search across component logs
3. **Log Aggregation**: Combine logs from multiple components
4. **Rate Limiting**: Protect against log flooding
5. **Sampling**: Option to sample high-frequency logs
