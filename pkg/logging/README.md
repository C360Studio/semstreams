# Logging Package

Structured logging handlers with multi-destination support, NATS publishing, and graceful fallback behavior.

## Features

- **Multi-Handler Composition**: Dispatch logs to multiple destinations simultaneously
- **NATS Publishing**: Publish logs to JetStream for real-time streaming
- **Source Filtering**: Exclude specific sources from NATS publishing (prefix matching)
- **Graceful Degradation**: NATS failures never block stdout logging
- **Async Publishing**: Non-blocking NATS publish to avoid logging latency
- **Thread-Safe**: Safe for concurrent use across goroutines

## Installation

```go
import "github.com/c360/semstreams/pkg/logging"
```

## Quick Start

### Basic Multi-Handler Setup

```go
import (
    "log/slog"
    "os"
    
    "github.com/c360/semstreams/pkg/logging"
)

// Create stdout handler
stdoutHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelInfo,
})

// Create NATS handler
natsHandler := logging.NewNATSLogHandler(natsClient, logging.NATSLogHandlerConfig{
    MinLevel:       slog.LevelInfo,
    ExcludeSources: nil,
})

// Compose handlers - logs go to both stdout and NATS
multiHandler := logging.NewMultiHandler(stdoutHandler, natsHandler)
logger := slog.New(multiHandler)
slog.SetDefault(logger)
```

### With Source Filtering

```go
// Exclude WebSocket worker logs from NATS to prevent feedback loops
natsHandler := logging.NewNATSLogHandler(natsClient, logging.NATSLogHandlerConfig{
    MinLevel:       slog.LevelDebug,
    ExcludeSources: []string{"flow-service.websocket"},
})

// This goes to stdout only (source matches exclude prefix):
logger.With("source", "flow-service.websocket.health").Info("Health check")

// This goes to both stdout and NATS:
logger.With("source", "flow-service").Info("Flow diagram saved")
```

## Handlers

### MultiHandler

Composes multiple `slog.Handler` instances, dispatching log records to all of them.

```go
multi := logging.NewMultiHandler(handler1, handler2, handler3)
logger := slog.New(multi)

// Logs dispatched to all three handlers
logger.Info("Hello world")
```

**Key Behaviors:**
- If one handler fails, others continue processing
- `Enabled()` returns true if ANY handler is enabled for the level
- `WithAttrs()` and `WithGroup()` create new instances (immutable)

### NATSLogHandler

Publishes log records to NATS subjects in the format `logs.{level}.{source}`.

```go
natsHandler := logging.NewNATSLogHandler(natsClient, logging.NATSLogHandlerConfig{
    MinLevel:       slog.LevelInfo,
    ExcludeSources: []string{"noisy-component", "debug-only"},
})
```

**Configuration:**

| Field | Type | Description |
|-------|------|-------------|
| `MinLevel` | `slog.Level` | Minimum log level to publish to NATS |
| `ExcludeSources` | `[]string` | Source prefixes to exclude from NATS publishing |

## Source Extraction

NATSLogHandler extracts the source identifier from log attributes with priority:

1. `source` attribute (explicit)
2. `component` attribute (component-tagged logs)
3. `service` attribute (service-tagged logs)
4. `"system"` (default fallback)

```go
// Explicit source
logger.With("source", "my-service.worker").Info("Working")
// → logs.INFO.my-service.worker

// Component attribute
logger.With("component", "udp-input").Info("Packet received")
// → logs.INFO.udp-input

// No source attributes
slog.Info("System message")
// → logs.INFO.system
```

## Source Filtering

Exclude sources from NATS publishing using prefix matching:

```go
ExcludeSources: []string{"flow-service.websocket"}
```

**Matching Rules:**
- `"flow-service.websocket"` → excluded
- `"flow-service.websocket.health"` → excluded (prefix match)
- `"flow-service"` → NOT excluded (different prefix)
- `"flow-service.api"` → NOT excluded (different prefix)

**Use Case:** Prevent feedback loops where WebSocket worker debug logs are sent over the same WebSocket connection.

## NATS Subject Pattern

Logs are published to subjects following this pattern:

```
logs.{level}.{source}
  └── logs.INFO.udp-input
  └── logs.ERROR.graph-processor
  └── logs.WARN.system
  └── logs.DEBUG.flow-service
```

### JetStream Stream Configuration

Configure a LOGS stream with TTL and size limits:

```go
StreamConfig{
    Name:     "LOGS",
    Subjects: []string{"logs.>"},
    MaxAge:   1 * time.Hour,        // TTL: expire after 1 hour
    MaxBytes: 100 * 1024 * 1024,    // 100MB max storage
    Discard:  DiscardOld,           // Drop oldest when full
}
```

## Log Entry Format

Logs published to NATS are JSON-encoded:

```json
{
    "timestamp": "2024-01-15T10:30:00.123456789Z",
    "level": "INFO",
    "source": "udp-input",
    "message": "Packet received",
    "fields": {
        "bytes": 1024,
        "remote_addr": "192.168.1.1:5000"
    }
}
```

## Architecture

### Data Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                        slog.Logger                               │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                       MultiHandler                               │
│  ┌──────────────────────┐    ┌──────────────────────────────┐   │
│  │   TextHandler        │    │      NATSLogHandler          │   │
│  │   (stdout)           │    │                              │   │
│  │                      │    │  ┌────────────────────────┐  │   │
│  │                      │    │  │ Check MinLevel         │  │   │
│  │                      │    │  │ Extract Source         │  │   │
│  │                      │    │  │ Check ExcludeSources   │  │   │
│  │                      │    │  │ Async Publish to NATS  │  │   │
│  │                      │    │  └────────────────────────┘  │   │
│  └──────────────────────┘    └──────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
         │                                    │
         ▼                                    ▼
    ┌─────────┐                    ┌──────────────────┐
    │ stdout  │                    │  NATS JetStream  │
    └─────────┘                    │  (LOGS stream)   │
                                   └──────────────────┘
                                            │
                                            ▼
                                   ┌──────────────────┐
                                   │  WebSocket       │
                                   │  Status Stream   │
                                   └──────────────────┘
```

### Graceful Degradation

The architecture ensures logging never fails due to NATS issues:

| Scenario | Behavior |
|----------|----------|
| NATS connected | Logs go to stdout AND NATS |
| NATS disconnected | Logs go to stdout only (NATS errors silently dropped) |
| NATS publish slow | Async publish - stdout not blocked |
| Handler error | Other handlers continue (MultiHandler ignores errors) |

## Performance

### Benchmarks (M3 MacBook Pro)

| Operation | Time |
|-----------|------|
| MultiHandler dispatch (per handler) | ~50ns |
| NATSLogHandler (async setup) | ~100ns |
| Combined (stdout + NATS) | ~150ns |

At 10,000 logs/second: ~1.5ms total overhead per second.

### Memory

- **MultiHandler**: O(n) where n is number of handlers
- **NATSLogHandler**: O(1) for handler state
- **Per log**: O(m) where m is number of attributes
- **Async goroutines**: Short-lived, minimal impact

## Integration with WebSocket Status Stream

This package is designed to work with the service package's WebSocket status stream:

1. **Application logs** → Published to NATS via NATSLogHandler
2. **LOGS JetStream stream** → Stores logs with TTL (1hr default)
3. **WebSocket clients** → Subscribe to `logs.>` subjects
4. **Real-time streaming** → No slog interception timing issues

The `ExcludeSources` config prevents feedback loops where WebSocket worker logs would be sent over the WebSocket, generating more logs.

## Testing

```bash
# Run tests with race detector
go test -race ./pkg/logging

# Run benchmarks
go test -bench=. ./pkg/logging
```

## Example: Complete Application Setup

```go
package main

import (
    "context"
    "log/slog"
    "os"

    "github.com/c360/semstreams/config"
    "github.com/c360/semstreams/metric"
    "github.com/c360/semstreams/natsclient"
    "github.com/c360/semstreams/pkg/logging"
)

func main() {
    // Build the local/counter graph before client construction. Keep the
    // client logger non-forwarding so it can never publish through itself.
    metrics := metric.NewMetricsRegistry()
    stdoutHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
    counterHandler := logging.NewCounterHandler(metrics.CoreMetrics().LogEntriesTotal)
    localHandler := logging.NewMultiHandler(stdoutHandler, counterHandler)
    baseLogger := slog.New(localHandler).With("service", "example")
    clientLogger := baseLogger.With("component", "natsclient")
    slog.SetDefault(baseLogger)

    natsClient, err := natsclient.NewClient(
        "nats://localhost:4222",
        natsclient.WithLogger(clientLogger),
        natsclient.WithMetrics(metrics),
    )
    if err != nil {
        slog.Error("Failed to create NATS client", "error", err)
        os.Exit(1)
    }
    defer natsClient.Close(context.Background())
    if err := natsClient.Connect(context.Background()); err != nil {
        slog.Error("Failed to connect to NATS", "error", err)
        os.Exit(1)
    }

    // Ensure LOGS stream exists
    streamsManager := config.NewStreamsManager(natsClient, slog.Default())
    if err := streamsManager.EnsureStreams(context.Background(), cfg); err != nil {
        slog.Error("Failed to ensure streams", "error", err)
        os.Exit(1)
    }

    // Add NATS only to the steady-state application logger after LOGS exists.
    level := slog.LevelInfo
    natsHandler := logging.NewNATSLogHandler(natsClient, logging.NATSLogHandlerConfig{
        MinLevel:       level,
        ExcludeSources: []string{"flow-service.websocket"},
    })
    
    multiHandler := logging.NewMultiHandler(localHandler, natsHandler)
    logger := slog.New(multiHandler).With("service", "example")
    slog.SetDefault(logger)

    // Application logs now go to both stdout and NATS
    slog.Info("Application started", "version", "1.0.0")

    // Component-tagged logs
    componentLogger := slog.With("component", "processor")
    componentLogger.Info("Processing started", "batch_size", 100)

    // Source-tagged logs for fine-grained filtering
    workerLogger := slog.With("source", "flow-service.websocket")
    workerLogger.Debug("WebSocket worker tick") // Goes to stdout only
}
```

## Related Packages

- **[service](../../service)**: WebSocket status stream that consumes NATS logs
- **[config](../../config)**: LOGS stream configuration with TTL and size limits
- **[natsclient](../../natsclient)**: NATS client used for publishing

## License

See LICENSE file in repository root.
