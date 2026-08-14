# Distributed Tracing

Correlate messages across services using W3C-compliant trace context propagation.

## Quick Start

All natsclient publish/request methods auto-generate traces:

```go
// Trace automatically created and propagated
err := client.Publish(ctx, "events.user", data)
```

No configuration needed. Every outbound message includes trace headers automatically.

## How It Works

```text
Service A                    NATS                    Service B
    |                         |                         |
    |-- Publish(ctx, data) -->|                         |
    |   [auto-gen trace]      |                         |
    |   Headers:              |                         |
    |   traceparent: 00-...   |-- Message + Headers --->|
    |   X-Trace-ID: abc123    |                         |
    |                         |                         |-- ExtractTrace(msg)
    |                         |                         |   tc.TraceID = "abc123"
    |                         |                         |
    |                         |<-- Response + Headers --|
    |                         |   [same trace ID]       |
```

Every outbound NATS message includes:

| Header | Format | Example |
|--------|--------|---------|
| `traceparent` | `00-{traceID}-{spanID}-{flags}` | `00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01` |
| `X-Trace-ID` | 32 hex chars | `0af7651916cd43dd8448eb211c80319c` |
| `X-Span-ID` | 16 hex chars | `b7ad6b7169203331` |
| `X-Parent-Span-ID` | 16 hex chars (optional) | `00f067aa0ba902b7` |

The `traceparent` header follows the [W3C Trace Context](https://www.w3.org/TR/trace-context/) specification for interoperability with OpenTelemetry and other tracing systems.

## Trace Context API

### Creating Traces

```go
// Auto-generated (recommended for most cases)
err := client.Publish(ctx, "subject", data)

// Explicit creation
tc := natsclient.NewTraceContext()
ctx = natsclient.ContextWithTrace(ctx, tc)
err = client.Publish(ctx, "subject", data)

// Access trace info
log.Printf("Trace: %s, Span: %s", tc.TraceID, tc.SpanID)
```

### Extracting from Messages

When using `SubscribeForRequests`, trace extraction is automatic:

```go
// Trace is automatically extracted and added to ctx
client.SubscribeForRequests(ctx, "service.action", func(ctx context.Context, data []byte) ([]byte, error) {
    // ctx already contains trace from incoming request
    // Downstream calls automatically inherit the trace
    return client.Request(ctx, "downstream.service", data, timeout)
})
```

For custom handlers using raw `Subscribe`, extract manually:

```go
func handleMessage(ctx context.Context, msg *nats.Msg) {
    tc := natsclient.ExtractTrace(msg)
    if tc != nil {
        ctx = natsclient.ContextWithTrace(ctx, tc)
    }

    // All downstream calls inherit trace
    client.Publish(ctx, "next.subject", response)
}
```

### Creating Child Spans

For nested operations, create child spans to preserve parent-child relationships:

```go
// Extract parent trace
parentTC := natsclient.ExtractTrace(msg)

// Create child span
childTC := parentTC.NewSpan()
// childTC.TraceID == parentTC.TraceID (same trace)
// childTC.ParentSpanID == parentTC.SpanID (linked to parent)
// childTC.SpanID == new unique ID

childCtx := natsclient.ContextWithTrace(ctx, childTC)
client.Request(childCtx, "service.action", data, timeout)
```

### JetStream Messages

When using `ConsumeStreamWithConfig`, trace extraction is automatic:

```go
// Trace is automatically extracted and added to ctx
client.ConsumeStreamWithConfig(ctx,
    natsclient.PortConsumerContext{Component: "example", Port: "events"},
    cfg, func(ctx context.Context, msg jetstream.Msg) {
    // ctx already contains trace from message headers
    // Downstream calls automatically inherit the trace
    client.Publish(ctx, "processed.event", result)
    msg.Ack()
})
```

For custom JetStream handlers, extract manually:

```go
func handleJetStreamMessage(ctx context.Context, msg jetstream.Msg) {
    tc := natsclient.ExtractTraceFromJetStream(msg.Headers())
    if tc != nil {
        ctx = natsclient.ContextWithTrace(ctx, tc)
    }
    // Process message...
}
```

## Methods with Trace Propagation

### Outbound (auto-generate and inject)

| Method | Behavior |
|--------|----------|
| `Publish` | Auto-generates trace, injects into message headers |
| `Request` | Auto-generates trace, injects into request message |
| `RequestWithHeaders` | Auto-generates trace, merges with user headers |
| `RequestWithRetry` | Same trace across all retry attempts |
| `ReplyWithHeaders` | Auto-generates trace, injects into reply message |
| `PublishToStream` | Auto-generates trace for JetStream publish |

### Inbound (auto-extract to context)

| Method | Behavior |
|--------|----------|
| `Subscribe` | Extracts trace from message, adds to handler context |
| `SubscribeForRequests` | Extracts trace from request, adds to handler context |
| `ConsumeStream` | Extracts trace from JetStream message headers |
| `ConsumeStreamWithConfig` | Extracts trace from JetStream message headers |

## Integration with Tracing Backends

### Jaeger / Tempo

Export traces to Jaeger or Grafana Tempo by bridging the trace context:

```go
import "go.opentelemetry.io/otel/trace"

func bridgeToOTel(tc *natsclient.TraceContext) trace.SpanContext {
    traceID, _ := trace.TraceIDFromHex(tc.TraceID)
    spanID, _ := trace.SpanIDFromHex(tc.SpanID)

    return trace.NewSpanContext(trace.SpanContextConfig{
        TraceID:    traceID,
        SpanID:     spanID,
        TraceFlags: trace.FlagsSampled,
        Remote:     true,
    })
}
```

### Message Logger Queries

Query traces via the message-logger service:

```bash
# Get all messages for a trace
curl "http://localhost:8080/api/v1/traces/{traceID}"

# Example response
{
  "trace_id": "0af7651916cd43dd8448eb211c80319c",
  "spans": [
    {"span_id": "b7ad6b7169203331", "subject": "events.user.created", "timestamp": "..."},
    {"span_id": "00f067aa0ba902b7", "subject": "notifications.send", "parent_span_id": "b7ad6b7169203331", "timestamp": "..."}
  ]
}
```

## Troubleshooting

### Missing Traces

**Symptom**: Messages don't have trace headers.

**Causes & Solutions**:

1. **Using raw NATS connection instead of natsclient**
   ```go
   // Wrong - bypasses trace injection
   conn.Publish(subject, data)

   // Correct - uses natsclient with trace injection
   client.Publish(ctx, subject, data)
   ```

2. **Context not passed through**
   ```go
   // Wrong - loses trace context
   go func() {
       client.Publish(context.Background(), subject, data)
   }()

   // Correct - propagates trace context
   go func(ctx context.Context) {
       client.Publish(ctx, subject, data)
   }(ctx)
   ```

### Trace Gaps Between Services

**Symptom**: Trace IDs don't match across services.

**Cause**: Not extracting and propagating trace from incoming messages.

**Solution**:
```go
func handleMessage(ctx context.Context, msg *nats.Msg) {
    // Extract trace from incoming message
    tc := natsclient.ExtractTrace(msg)
    if tc != nil {
        ctx = natsclient.ContextWithTrace(ctx, tc)
    }

    // Now downstream calls use same trace
    client.Publish(ctx, "downstream", response)
}
```

### Invalid Traceparent Format

**Symptom**: `ParseTraceparent` returns error.

**Valid format**: `{version}-{trace_id}-{span_id}-{flags}`
- version: `00` (2 chars)
- trace_id: 32 hex chars
- span_id: 16 hex chars
- flags: `00` (not sampled) or `01` (sampled)

Example: `00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01`

## Next Steps

- [Local Monitoring](01-local-monitoring.md) - Prometheus and Grafana setup
- [Troubleshooting](02-troubleshooting.md) - Common issues and diagnostics
