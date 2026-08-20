# File Output Component

Writes NATS messages to disk files in multiple formats with buffered I/O and automatic flushing.

## Purpose

The file output component persists NATS message streams to disk files in JSON, JSONL, or raw formats.
It provides buffered writes with configurable batch sizes for performance, supporting both NATS core and
JetStream subscriptions. Files can be opened in append or overwrite mode for logging, archiving, and
data export workflows.

## Configuration

```yaml
component:
  type: file
  config:
    ports:
      inputs:
        - name: nats_input
          type: nats
          subject: "events.>"
          required: true
    directory: "/var/log/semstreams"
    file_prefix: "events"
    format: "jsonl"
    append: true
    buffer_size: 100
```

### Configuration Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `ports.inputs` | array | required | NATS or JetStream input port definitions |
| `directory` | string | `/tmp/streamkit` | Output directory path (created if missing) |
| `file_prefix` | string | `output` | Filename prefix before extension |
| `format` | enum | `jsonl` | Output format: `json`, `jsonl`, `raw` |
| `append` | bool | `true` | Append to existing file vs overwrite |
| `buffer_size` | int | `100` | Messages to buffer before flushing |

## Input/Output Ports

### Input Ports

The component accepts both NATS core and JetStream subscriptions:

**NATS Core Subscription:**

```yaml
inputs:
  - name: events_input
    type: nats
    subject: "app.events.>"
    required: true
```

**JetStream Consumer:**

```yaml
inputs:
  - name: stream_input
    type: jetstream
    subject: "orders.*"
    stream_name: "ORDERS"
    required: true
```

Multiple input ports can be configured. All inputs write to the same output file.

### Output Ports

File output components have no NATS output ports. Data flows terminate at the filesystem.

## File Formats

### JSON Lines (jsonl)

One JSON object per line, optimized for streaming and log processing:

```json
{"timestamp": "2026-02-11T10:00:00Z", "level": "info", "message": "user login"}
{"timestamp": "2026-02-11T10:00:01Z", "level": "warn", "message": "rate limit"}
```

Best for: Log aggregation, event streams, line-oriented tools (grep, awk)

### Pretty JSON (json)

Indented JSON with newlines, human-readable:

```json
{
  "timestamp": "2026-02-11T10:00:00Z",
  "level": "info",
  "message": "user login"
}
{
  "timestamp": "2026-02-11T10:00:01Z",
  "level": "warn",
  "message": "rate limit"
}
```

Best for: Manual inspection, debugging, configuration export

### Raw (raw)

Binary message data written directly without formatting:

```
<raw bytes><raw bytes><raw bytes>
```

Best for: Binary protocols, compact storage, non-JSON data

## File Naming and Rotation

### File Naming

Files are named using the pattern: `{file_prefix}.{format}`

Examples:

- `directory: /var/log`, `file_prefix: events`, `format: jsonl` → `/var/log/events.jsonl`
- `directory: /data/export`, `file_prefix: sensors`, `format: json` → `/data/export/sensors.json`

### Rotation Strategy

The component does **not** include built-in file rotation. Use external tools:

**Using logrotate:**

```conf
/var/log/semstreams/*.jsonl {
    daily
    rotate 7
    compress
    delaycompress
    missingok
    notifempty
    copytruncate
}
```

**Manual rotation:** Stop the component, move the file, then construct and Start a fresh component instance. With
`append: true`, the fresh instance creates or reopens the configured file.

## Buffering and Flushing

The component buffers messages in memory before writing to disk:

1. Messages accumulate in buffer (size: `buffer_size`)
2. Flush triggers when buffer is full **or** every 1 second (automatic)
3. Graceful shutdown flushes remaining buffer before closing file

**Performance considerations:**

- Larger `buffer_size` improves throughput but increases memory usage
- Smaller `buffer_size` reduces latency but increases disk I/O
- Automatic 1-second flush prevents unbounded latency for low-volume streams

## Lifecycle and Concurrency

Each component instance is one-shot. Call Start once, then call Stop with a caller-bounded context. A completed repeat
Stop is a nil no-op, but the same instance rejects restart. Construct a fresh component instance when restarting a
flow or completing manual file rotation.

Lifecycle transitions must be serialized by the composition owner; concurrent Start or Stop calls are not a supported
coordination contract. Message delivery and buffered writes remain safe while the component is running. Stop drains
the exact NATS subscriptions and JetStream consumers, joins the flush loop, flushes buffered data, and closes the file.

## Example Use Cases

### Application Event Logging

```yaml
component:
  type: file
  config:
    ports:
      inputs:
        - name: app_logs
          type: nats
          subject: "app.logs.>"
    directory: "/var/log/app"
    file_prefix: "events"
    format: "jsonl"
    append: true
    buffer_size: 200
```

Captures all application log events to a JSON Lines file with 200-message buffering.

### Sensor Data Export

```yaml
component:
  type: file
  config:
    ports:
      inputs:
        - name: sensors
          type: jetstream
          subject: "sensors.temperature.*"
          stream_name: "SENSORS"
    directory: "/data/export"
    file_prefix: "temperature-readings"
    format: "json"
    append: false
    buffer_size: 50
```

Exports temperature sensor data to human-readable JSON, overwriting file on each run.

### Binary Protocol Archive

```yaml
component:
  type: file
  config:
    ports:
      inputs:
        - name: protocol_stream
          type: nats
          subject: "protocol.frames"
    directory: "/archive"
    file_prefix: "protocol-capture"
    format: "raw"
    append: true
    buffer_size: 500
```

Archives raw binary protocol frames to disk for later analysis or replay.

### Multi-Subject Log Aggregation

```yaml
component:
  type: file
  config:
    ports:
      inputs:
        - name: errors
          type: nats
          subject: "logs.error"
        - name: warnings
          type: nats
          subject: "logs.warn"
        - name: info
          type: nats
          subject: "logs.info"
    directory: "/var/log/aggregated"
    file_prefix: "all-logs"
    format: "jsonl"
    append: true
    buffer_size: 300
```

Aggregates multiple log levels into a single chronological file.
