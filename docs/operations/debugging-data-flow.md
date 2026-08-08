# Debugging Data Flow

When data isn't flowing through your SemStreams pipeline as expected, use these tools to diagnose the issue.

## Quick Diagnosis

```bash
# 1. Check component health
curl -s http://localhost:8080/health | jq '.sub_statuses[] | {component, status}'

# 2. Check message flow statistics
curl -s http://localhost:8080/message-logger/stats | jq .

# 3. View recent messages
curl -s "http://localhost:8080/message-logger/entries?limit=20" | jq '.[] | {subject, message_type, timestamp}'

# 4. Check JetStream streams
curl -s "http://localhost:8222/jsz?streams=1" | jq '[.account_details[0].stream_detail[] | {name, messages: .state.messages}]'
```

## Message Logger Service

The message-logger service captures all NATS messages flowing through your pipeline. It's enabled by default.

### View Recent Messages

```bash
# All recent messages
curl -s "http://localhost:8080/message-logger/entries?limit=50" | jq .

# Filter by subject pattern
curl -s "http://localhost:8080/message-logger/entries?subject=raw.*" | jq .
curl -s "http://localhost:8080/message-logger/entries?subject=sensor.processed.*" | jq .
```

### Trace a Request

Every message includes a W3C trace ID. Use it to follow a message through all components:

```bash
# Find a trace ID from recent messages
TRACE_ID=$(curl -s "http://localhost:8080/message-logger/entries?limit=1" | jq -r '.[0].trace_id')

# Get all messages for that trace
curl -s "http://localhost:8080/message-logger/trace/$TRACE_ID" | jq .
```

The trace shows the message's journey:
```json
{
  "trace_id": "0af7651916cd43dd8448eb211c80319c",
  "count": 3,
  "entries": [
    {"subject": "raw.sensor.udp", "timestamp": "..."},
    {"subject": "sensor.processed.entity", "timestamp": "..."},
    {"subject": "graph.ingest.entity", "timestamp": "..."}
  ]
}
```

### Watch Messages in Real-Time

Stream messages as they flow through the system:

```bash
# Watch all messages (Ctrl+C to stop)
curl -N "http://localhost:8080/message-logger/entries?limit=1" && \
  while true; do sleep 1; curl -s "http://localhost:8080/message-logger/entries?limit=5"; done
```

### Watch KV Bucket Changes

Monitor entity state changes in real-time using Server-Sent Events:

```bash
# Watch all entity state changes
curl -N "http://localhost:8080/message-logger/kv/ENTITY_STATES/watch"

# Watch specific entity pattern
curl -N "http://localhost:8080/message-logger/kv/ENTITY_STATES/watch?pattern=demo.hello.*"
```

## Common Issues

### Data Received but Not Processed

**Symptom**: UDP/input metrics show data received, but downstream components show nothing.

**Diagnosis**:
```bash
# Check if messages reach the RAW stream
curl -s "http://localhost:8222/jsz?streams=1" | jq '.account_details[0].stream_detail[] | select(.name=="RAW") | .state'

# Check message logger for the subject
curl -s "http://localhost:8080/message-logger/entries?subject=raw.*" | jq 'length'
```

**Common Causes**:
1. **Subject mismatch**: Processor subscribes to wrong subject
2. **Consumer not started**: Check component status
3. **Stream not created**: JetStream stream might not exist

### Messages Stuck in Stream

**Symptom**: Stream has messages but consumer isn't processing them.

**Diagnosis**:
```bash
# Check consumer status
curl -s "http://localhost:8222/jsz?consumers=1" | jq '.account_details[0].stream_detail[].consumer_detail'
```

**Common Causes**:
1. **Consumer filter mismatch**: Consumer filter doesn't match message subjects
2. **Ack pending**: Messages delivered but not acknowledged (check for errors in processor)

### GraphQL Query Timeout

**Symptom**: GraphQL queries return `{"errors": [{"message": "request timeout"}]}`

**Diagnosis**:
```bash
# Check if graph-query component is healthy
curl -s http://localhost:8080/health | jq '.sub_statuses[] | select(.component | contains("graph"))'

# Check if ENTITY_STATES bucket has data
curl -s "http://localhost:8080/message-logger/kv/ENTITY_STATES?limit=5" | jq .
```

**Common Causes**:
1. **No data ingested**: ENTITY_STATES bucket is empty
2. **graph-query not started**: Component failed to start
3. **NATS request timeout**: Check NATS connectivity

### Entity Not Found

**Symptom**: `{"errors": [{"message": "not found: <entity-id>"}]}`

**Diagnosis**:
```bash
# Check what entities exist
curl -s "http://localhost:8080/message-logger/kv/ENTITY_STATES?limit=10" | jq '.entries[].key'

# Search by prefix
curl -s 'http://localhost:8084/graphql' \
  -H "Content-Type: application/json" \
  -d '{"query":"{ entitiesByPrefix(prefix: \"demo\", limit: 10) { entities { id } next_cursor } }"}' | jq .
```

Inspect `data.entitiesByPrefix.entities`. When `next_cursor` is present, pass
that opaque value back as `cursor` to continue the same prefix scan; an absent
or empty value means the scan is complete.

**Common Causes**:
1. **Wrong entity ID format**: Check the processor's EntityID() implementation
2. **Data not ingested yet**: Processing pipeline hasn't completed
3. **Different org/platform**: Entity ID uses different org than expected

## Configuration

### Enable Verbose Logging

```json
{
  "services": {
    "message-logger": {
      "enabled": true,
      "config": {
        "monitor_subjects": ["*"],
        "max_entries": 10000,
        "output_to_stdout": true,
        "log_level": "DEBUG",
        "sample_rate": 1
      }
    }
  }
}
```

| Option | Description |
|--------|-------------|
| `monitor_subjects` | Subjects to monitor. `["*"]` auto-discovers from component configs |
| `max_entries` | Circular buffer size (1,000-100,000) |
| `output_to_stdout` | Print messages to console |
| `log_level` | DEBUG, INFO, WARN, ERROR |
| `sample_rate` | 1 = all messages, 10 = 10% sample |

## API Reference

| Endpoint | Description |
|----------|-------------|
| `GET /message-logger/entries` | Recent messages (params: `limit`, `subject`) |
| `GET /message-logger/stats` | Message statistics |
| `GET /message-logger/subjects` | Monitored subjects |
| `GET /message-logger/trace/{traceID}` | All messages for a trace |
| `GET /message-logger/kv/{bucket}` | Query KV bucket (params: `pattern`, `limit`) |
| `GET /message-logger/kv/{bucket}/watch` | Stream KV changes via SSE |

## Task Commands

```bash
# View recent messages
task dev:messages

# Watch messages in real-time
task dev:watch

# Trace a specific request
task dev:trace TRACE_ID=<trace-id>
```
