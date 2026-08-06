# UDP Input Component

High-performance UDP datagram receiver that publishes to NATS JetStream or core NATS subjects.

## Purpose

The UDP input component enables receiving datagram messages over UDP with built-in buffer overflow handling, retry
logic, and NATS integration for message distribution. It provides MAVLink reception capabilities and implements the
SemStreams component interfaces for lifecycle management and observability. The component is designed for
high-throughput scenarios with concurrent message processing and graceful degradation under load.

## Configuration

### Basic Configuration

```yaml
ports:
  inputs:
    - name: "udp_socket"
      type: "network"
      subject: "udp://0.0.0.0:14550"
      required: true
      description: "UDP socket listening for incoming data"
  outputs:
    - name: "nats_output"
      type: "nats"
      subject: "input.udp.mavlink"
      required: true
      description: "NATS subject for publishing received UDP data"
```

### JetStream Output Configuration

```yaml
ports:
  inputs:
    - name: "udp_socket"
      type: "network"
      subject: "udp://0.0.0.0:5000"
      required: true
  outputs:
    - name: "stream_output"
      type: "jetstream"
      subject: "sensors.telemetry"
      required: true
```

### Configuration Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `ports.inputs[].subject` | string | `udp://0.0.0.0:14550` | UDP bind address in format `udp://host:port` |
| `ports.outputs[].subject` | string | `input.udp.mavlink` | NATS subject for publishing received data |
| `ports.outputs[].type` | string | `nats` | Output type: `nats` or `jetstream` |

### Internal Defaults

The component uses the following non-configurable defaults:

- **Buffer Size**: 5000 messages (circular buffer)
- **Max Datagram Size**: 65536 bytes (full UDP packet size)
- **Overflow Policy**: Drop oldest messages when buffer is full
- **Socket Buffer**: 2MB OS-level receive buffer
- **Batch Size**: 100 messages per processing cycle
- **Retry Policy**: 3 attempts with exponential backoff

## Input/Output Ports

### Input Ports

**udp_socket** (network)

- Direction: Input
- Protocol: UDP
- Configuration: `udp://host:port` format
- Description: UDP socket listening for incoming datagrams
- Behavior: Binds to specified address and port, receives datagrams up to 65536 bytes

### Output Ports

**nats_output** (nats or jetstream)

- Direction: Output
- Configuration: NATS subject string
- Description: Publishes received UDP data to NATS
- Behavior:
  - **Core NATS** (`type: nats`): Fire-and-forget publish
  - **JetStream** (`type: jetstream`): Persistent stream publish with acknowledgment

## Performance Considerations

### Throughput Characteristics

- **Packet Rate**: 10,000+ datagrams/second (8KB datagrams)
- **Latency**: Sub-millisecond for buffered messages
- **CPU Overhead**: Minimal (single goroutine for reception, batched processing)
- **Memory Usage**: O(BufferSize) + per-datagram allocations
- **Concurrency**: Single receiver goroutine, batch processing for NATS publishing

### Buffer Management

The component uses a circular buffer with overflow protection:

```go
// Buffer Configuration (internal)
Capacity: 5000 messages
Policy: Drop oldest when full
Batch Processing: 100 messages per cycle
```

**Buffer Overflow Behavior:**

1. Overflow counter increments
2. Oldest message dropped automatically
3. Warning logged at first overflow
4. Processing continues without disruption
5. Metrics updated (`packets_dropped_total`)

### Network Tuning

**OS Socket Buffer:** The component sets a 2MB receive buffer to prevent kernel-level drops during traffic bursts. If
the system limits buffer size, a warning is logged but operation continues.

**Read Deadline:** 100ms read timeout allows periodic shutdown checks without blocking indefinitely.

### Retry Logic

Transient errors (network issues, NATS connection problems) trigger automatic retry with exponential backoff:

- **Max Attempts**: 3
- **Initial Delay**: 100ms
- **Max Delay**: 5s
- **Backoff Multiplier**: 2.0
- **Jitter**: Enabled

Permanent errors (configuration issues) fail immediately without retry.

### Metrics

The component exports Prometheus metrics for monitoring:

- `semstreams_udp_packets_received_total`: Total UDP packets received
- `semstreams_udp_bytes_received_total`: Total bytes received
- `semstreams_udp_packets_dropped_total`: Packets dropped due to buffer overflow
- `semstreams_udp_buffer_utilization_ratio`: Buffer usage (0-1)
- `semstreams_udp_batch_size`: Distribution of processing batch sizes
- `semstreams_udp_publish_duration_seconds`: NATS publish latency
- `semstreams_udp_socket_errors_total`: Socket read errors encountered
- `semstreams_udp_last_activity_timestamp`: Unix timestamp of last received packet

## Example Use Cases

### IoT Sensor Data Collection

Receive sensor datagrams from hundreds of devices with burst handling:

```yaml
ports:
  inputs:
    - name: "sensor_socket"
      type: "network"
      subject: "udp://0.0.0.0:5000"
      required: true
  outputs:
    - name: "sensor_stream"
      type: "jetstream"
      subject: "iot.sensors.telemetry"
      required: true
```

**Characteristics:**

- Small packets (1KB typical)
- High burst traffic (hundreds of sensors reporting simultaneously)
- 5000-message buffer handles concurrent sensor reports
- JetStream persistence prevents data loss

### Network Monitoring (Syslog/NetFlow)

Receive syslog or NetFlow data with newest-first priority:

```yaml
ports:
  inputs:
    - name: "syslog_socket"
      type: "network"
      subject: "udp://0.0.0.0:514"
      required: true
  outputs:
    - name: "log_stream"
      type: "jetstream"
      subject: "network.syslog"
      required: true
```

**Characteristics:**

- Variable packet sizes (512-2048 bytes)
- Continuous traffic with periodic spikes
- Drop oldest policy keeps newest logs during overflow
- JetStream enables log replay and analysis

### MAVLink Telemetry (Default Configuration)

Receive MAVLink messages from drones or robotics systems:

```yaml
ports:
  inputs:
    - name: "mavlink_socket"
      type: "network"
      subject: "udp://0.0.0.0:14550"
      required: true
  outputs:
    - name: "telemetry_output"
      type: "nats"
      subject: "input.udp.mavlink"
      required: true
```

**Characteristics:**

- MAVLink standard port (14550)
- Small packets (280 bytes typical)
- Real-time telemetry with low latency requirements
- Core NATS for minimal overhead

### Multicast Reception

Join multicast group for broadcast data reception:

```yaml
ports:
  inputs:
    - name: "multicast_socket"
      type: "network"
      subject: "udp://239.0.0.1:9999"
      required: true
  outputs:
    - name: "broadcast_stream"
      type: "jetstream"
      subject: "multicast.data"
      required: true
```

**Characteristics:**

- Multicast address (239.0.0.1)
- Large packets (8KB+)
- High bandwidth broadcast data
- JetStream handles multiple subscribers efficiently

## Thread Safety

The component is fully thread-safe with proper synchronization:

- **Start/Stop**: Can be called from any goroutine (idempotent operations)
- **Metrics**: Atomic operations for lock-free updates
- **Buffer Access**: Internal mutex protection
- **Socket Operations**: Protected by RWMutex during shutdown

## Lifecycle Management

### Initialization

```go
input.Initialize() // Validates configuration, does not start receiving
```

### Starting

```go
input.Start(ctx) // Begins receiving datagrams and publishing to NATS
```

**Startup Sequence:**

1. Bind UDP socket with retry
2. Start read loop goroutine

### Stopping

```go
input.Stop(5 * time.Second) // Graceful shutdown with timeout
```

**Shutdown Sequence:**

1. Signal shutdown to goroutines
2. Close UDP socket (unblocks read loop)
3. Wait for goroutine completion (with timeout)
4. Process remaining buffered messages
5. Clean up resources

## Error Handling

The component uses SemStreams error classification for consistent behavior:

- **Invalid Configuration**: `errs.WrapInvalid` (port out of range, empty subject, nil client)
- **Network Errors**: `errs.WrapTransient` (socket binding failures, read errors)
- **NATS Errors**: `errs.WrapTransient` (connection issues, publish failures)

**Error Behavior:**

- Configuration errors fail component initialization
- Transient errors trigger retry with exponential backoff
- Socket errors increment error counter but continue processing
- Individual message publish failures don't stop the component

## Testing

Run the component test suites:

```bash
# Unit tests (fast, no external dependencies)
go test ./input/udp -v

# Integration tests (requires Docker for NATS testcontainer)
go test -tags=integration ./input/udp -v

# Race detector (concurrency validation)
go test ./input/udp -race

# Coverage report
go test ./input/udp -cover
```

## Limitations

Current version constraints:

- **IPv4 Only**: IPv6 support planned for future release
- **Single Socket**: One UDP socket per component instance
- **No Deduplication**: Application-level deduplication must be implemented separately
- **Unordered Delivery**: UDP provides no ordering guarantees (consider sequence numbering in payload)
- **No Fragmentation Handling**: Assumes datagrams fit in 65536 bytes (UDP maximum)

## Health Monitoring

Query component health status:

```go
health := input.Health()
// health.Healthy: true if running and socket connected
// health.ErrorCount: Total errors encountered since start
// health.Uptime: Duration since Start() was called
```

Query data flow metrics:

```go
flow := input.DataFlow()
// flow.MessagesPerSecond: Current throughput rate
// flow.BytesPerSecond: Current byte rate
// flow.ErrorRate: Error percentage (errors / messages)
// flow.LastActivity: Timestamp of last received packet
```

## Related Components

- **processor/graph**: Process received data into knowledge graph triples
- **processor/jsonmap**: Transform JSON payloads from UDP data
- **output/websocket**: Forward processed data to WebSocket clients
- **gateway/http**: Query knowledge graph built from UDP-ingested data
