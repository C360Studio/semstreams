# Scenario: core-federation

## Purpose

Validates federation data flow between edge and cloud instances, testing the UDP → WebSocket → File pipeline with ack/nack protocol verification.

## Category

**Tier**: Core  
**Priority**: Medium

## What It Tests

This scenario tests cross-instance data federation:
1. Edge instance receives data via UDP
2. Edge forwards data via WebSocket
3. Cloud instance receives and stores data
4. Ack/nack protocol ensures delivery confirmation

### Test Stages (6 total)

| Stage | Purpose | Assertion Type |
|-------|---------|----------------|
| verify_edge_health | Edge instance is healthy | Hard failure |
| verify_cloud_health | Cloud instance is healthy | Hard failure |
| send_test_data | Send UDP messages to edge | Hard failure (if 0 sent) |
| verify_federation_flow | Messages flowed edge→cloud | Component count only |
| verify_ack_protocol | Ack/nack messages exchanged | Soft (warning) |
| verify_metrics | Metrics exist on both sides | Soft (warning) |

## Assertions

### Strong Assertions

1. **Edge Health** (lines 115-125)
   - Edge instance must report "healthy" status
   - Hard failure if unhealthy

2. **Cloud Health** (lines 127-137)
   - Cloud instance must report "healthy" status
   - Hard failure if unhealthy

3. **Test Data Sent** (lines 139-175)
   - At least one UDP message must be sent successfully
   - Fails if `sent == 0`

### Weak Assertions

1. **Federation Flow** (lines 177-209)
   - Only checks component count exists
   - Does NOT verify messages actually flowed:
   ```go
   if len(cloudComponents) == 0 {
       return fmt.Errorf("no components running on cloud instance")
   }
   // No verification that messages arrived
   ```

2. **Ack Protocol** (lines 211-251)
   - Sends one WebSocket message
   - Only logs warning if no ack received:
   ```go
   if err := wsConn.ReadJSON(&response); err != nil {
       result.Warnings = append(result.Warnings, 
           fmt.Sprintf("no ack/nack received: %v", err))
   }
   ```

3. **Metrics Verification** (lines 253-275)
   - Only checks components exist
   - Uses warnings for all errors

## Configuration

```go
type CoreFederationConfig struct {
    MessageCount       int            // Default: 20
    MessageInterval    time.Duration  // Default: 100ms
    ValidationDelay    time.Duration  // Default: 5s
    MinMessagesOnCloud int            // Default: 15 (unused!)
    AckVerification    bool           // Default: true
}
```

**Gap**: `MinMessagesOnCloud` is defined but never used in validation.

## Prerequisites

- Two SemStreams instances running (edge and cloud)
- Edge UDP endpoint accessible (default: localhost:14550)
- WebSocket endpoint accessible (default: ws://localhost:8082/stream)
- Federation bridge configured between instances

## Running

**No task or docker-compose file runs this scenario.** Verified: `task --list | grep e2e:federation` returns
nothing, and no file under `docker/compose/` mentions federation (`grep -rln federation docker/compose/` is
empty). The scenario is registered only in `cmd/e2e`'s `--scenario` switch (`cmd/e2e/main.go:383`), and even a
manual run is missing a prerequisite: `configs/cloud-federation.json`'s WebSocket client dials the literal
hostname `ws://edge:8082/stream` (`websocket_input.config.client.url`), which resolves only inside a docker
network that defines an `edge` service — no compose file in this repo defines one, and neither federation config
sets a distinct HTTP API port, so two instances started from `configs/edge-federation.json` and
`configs/cloud-federation.json` on the same host will also collide on the default API port.

To run it manually today you have to supply all of that infrastructure yourself: start two `semstreams`
instances (`configs/edge-federation.json` and `configs/cloud-federation.json`), give them distinct API ports and
a way for the cloud instance to resolve `edge` (a shared docker network, an `/etc/hosts` entry, or an edited
`client.url`), then invoke the CLI with flags — not the `run <scenario>` subcommand form this doc previously
showed, which the CLI does not support (see the `--scenario` invocations in `test/e2e/README.md`'s "Direct CLI"
section):

```bash
cd cmd/e2e
# --udp-endpoint sends test data into the edge instance (configs/edge-federation.json binds 0.0.0.0:14550).
# --ws-endpoint is dialed directly BY THE TEST HARNESS as a WebSocket client (test/e2e/scenarios/core_federation.go),
# so it must point at the edge instance's WS server (configs/edge-federation.json binds 0.0.0.0:8082/stream) —
# not at the cloud instance.
./e2e --scenario core-federation \
  --base-url <edge HTTP API address> \
  --cloud-url <cloud HTTP API address> \
  --udp-endpoint <edge UDP address> \
  --ws-endpoint <edge WS address>
```

## Known Gaps

### 1. MinMessagesOnCloud Never Used

Configuration defines delivery threshold but it's never checked:

```go
MinMessagesOnCloud: 15, // At least 75% should make it through
```

But `verifyFederationFlow` only checks component count, not message delivery.

**Recommendation**: Actually validate message arrival:
```go
// Should validate:
if messagesOnCloud < s.config.MinMessagesOnCloud {
    return fmt.Errorf("only %d/%d messages arrived on cloud", 
        messagesOnCloud, s.config.MessageCount)
}
```

### 2. No Message Correlation

Test doesn't verify specific messages arrived:

```go
// Sends messages with IDs like "fed-test-0", "fed-test-1"
testData := map[string]interface{}{
    "message_id": fmt.Sprintf("fed-test-%d", i),
    ...
}
// But never verifies these IDs arrived on cloud
```

**Recommendation**: Query cloud for specific message IDs or use sequence validation.

### 3. Federation Flow Only Checks Component Existence

```go
// From verifyFederationFlow (lines 197-208)
if len(cloudComponents) == 0 {
    return fmt.Errorf("no components running on cloud instance")
}

result.Metrics["edge_components"] = len(edgeComponents)
result.Metrics["cloud_components"] = len(cloudComponents)

return nil  // Passes if cloud has ANY components
```

**Recommendation**: Check for:
- WebSocket Input component on cloud
- Message count metrics
- File output evidence

### 4. Ack Protocol Test is Shallow

Only tests one message, only logs warning on failure:

```go
// Send one test message
if err := wsConn.WriteJSON(testMsg); err != nil {
    return fmt.Errorf("failed to send test message: %w", err)
}

// But ack failure is just a warning
if err := wsConn.ReadJSON(&response); err != nil {
    result.Warnings = append(result.Warnings, ...)
}
```

**Recommendation**: 
- Test multiple ack/nack scenarios
- Verify ack contains correct message ID
- Hard fail if ack protocol is broken

### 5. No Latency Measurement

Doesn't measure edge-to-cloud latency:

**Recommendation**: Add timing between send and cloud receipt.

## Example Output

```json
{
  "scenario_name": "core-federation",
  "success": true,
  "metrics": {
    "messages_sent": 20,
    "edge_component_count": 3,
    "cloud_component_count": 2,
    "ack_type": "ack"
  },
  "details": {
    "edge_health": {"status": "healthy"},
    "cloud_health": {"status": "healthy"},
    "test_data_sent": {
      "total_messages": 20,
      "sent": 20,
      "failed": 0
    },
    "edge_components": [...],
    "cloud_components": [...],
    "ack_response": {"type": "ack", "id": "..."}
  }
}
```

## Architecture Reference

```
┌─────────────────┐         ┌─────────────────┐
│   Edge Instance │         │  Cloud Instance │
│                 │         │                 │
│  ┌───────────┐  │  WS     │  ┌───────────┐  │
│  │UDP Input  │──┼────────►│──│WS Input   │  │
│  └───────────┘  │         │  └───────────┘  │
│       │         │  ack    │       │         │
│       ▼         │◄────────┼───────┘         │
│  ┌───────────┐  │         │       ▼         │
│  │WS Output  │  │         │  ┌───────────┐  │
│  └───────────┘  │         │  │File Output│  │
│                 │         │  └───────────┘  │
└─────────────────┘         └─────────────────┘
```

## Related Scenarios

- **core-health**: Validates single instance health
- **core-dataflow**: Tests data flow within single instance
