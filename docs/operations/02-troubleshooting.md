# Troubleshooting Guide

Common issues and solutions for SemStreams deployment and operation.

## Quick Diagnostics

```bash
# Check component health
curl http://localhost:8080/health

# View NATS connection status
nats server info

# Check running components
curl http://localhost:8080/api/components

# View recent logs
docker logs semstreams-app --tail 100
```

## Agentic System Issues

### Loop Stuck in State

**Symptoms**: Loop entity shows same state for extended period.

**Diagnosis**:
```bash
nats kv get AGENT_LOOPS <loop_id>
```

Check `pending_tools` — if non-empty, tools haven't returned results.

**Common causes**:
1. agentic-tools not running or not subscribed
2. Tool executor threw panic (check logs)
3. Tool timeout exceeded
4. Missing payload registry (see below)

**Resolution**:
- Restart agentic-tools
- Check tool executor logs
- Verify tool timeout configuration

### Model Returns Empty Response

**Symptoms**: AgentResponse has empty `content` and no `tool_calls`.

**Diagnosis**: Check agentic-model logs for HTTP errors.

```bash
docker logs semstreams-app 2>&1 | grep agentic-model
```

**Common causes**:
1. Model endpoint unreachable
2. Invalid API key
3. Rate limited
4. Request too large (context length exceeded)

**Resolution**:
- Verify endpoint URL in config
- Check API key environment variable
- Review request size in trajectory

### Tool Not Found

**Symptoms**: Tool result contains "tool not found" error.

**Diagnosis**:
```go
// List registered tools (via the deps-injected shared registry).
// In tests or scratch code, build your own registry directly.
for _, t := range deps.ToolRegistry.ListTools() {
    fmt.Println(t.Name)
}
```

**Common causes**:
1. Tool executor not registered
2. Tool name mismatch (case sensitive)
3. Tool blocked by allowlist

**Resolution**:
- Register executor before starting component
- Verify tool names match exactly
- Check `allowed_tools` in config

### High Token Usage

**Symptoms**: Metrics show unexpectedly high token counts.

**Diagnosis**:
```bash
nats kv get AGENT_TRAJECTORIES <loop_id>
```

Review trajectory for patterns.

**Common causes**:
1. Large tool results included in every message
2. Long system prompts repeated each turn
3. Loop iterations accumulating context

**Resolution**:
- Summarize tool results
- Optimize prompts
- Reduce max_iterations
- Enable context compaction

## Payload Registry Issues

### Missing MarshalJSON Method

**Symptoms**:
- Payload serializes without type wrapper
- Consumer receives `GenericPayload` instead of typed struct
- JSON looks like `{"field":"value"}` instead of `{"type":{...},"payload":{...}}`

**Diagnosis**:
```go
// Check serialized structure
data, _ := json.MarshalIndent(msg, "", "  ")
fmt.Println(string(data))

// Expected:
// {
//   "type": {
//     "domain": "agentic",
//     "category": "task",
//     "version": "v1"
//   },
//   "payload": { ... }
// }
```

**Resolution**: Implement `MarshalJSON` on the payload type:

```go
func (t *MyMessage) MarshalJSON() ([]byte, error) {
    type Alias MyMessage
    return json.Marshal(&message.BaseMessage{
        Type: message.MessageType{
            Domain:   "mydomain",
            Category: "mycategory",
            Version:  "v1",
        },
        Payload: (*Alias)(t),
    })
}
```

### Payload Type Not Registered

**Symptoms**:
- Deserialization returns `GenericPayload`
- Type assertion fails at runtime

**Diagnosis**:
```go
// List all registered payloads
payloads := component.GlobalPayloadRegistry().ListPayloads()
for msgType := range payloads {
    fmt.Println(msgType)
}
```

**Resolution**:
1. Ensure `init()` registers the payload in `payload_registry.go`
2. Ensure the package is imported somewhere (use blank import if needed)

```go
import _ "github.com/c360studio/semstreams/mypackage"
```

### Type Mismatch Between Registration and Serialization

**Symptoms**: Registration exists but deserialization still fails.

**Diagnosis**: Compare registration to MarshalJSON:

```go
// Registration
component.RegisterPayload(&component.PayloadRegistration{
    Domain:   "agentic",
    Category: "task",      // <-- Must match exactly
    Version:  "v1",
})

// MarshalJSON
Type: message.MessageType{
    Domain:   "agentic",
    Category: "task",      // <-- This must match registration
    Version:  "v1",
}
```

**Resolution**: Use constants to ensure consistency:

```go
const (
    Domain      = "agentic"
    CategoryTask = "task"
    Version     = "v1"
)
```

## Workflow Issues

### Workflow Never Completes

**Symptoms**: Execution stuck in "running" state.

**Diagnosis**:
```bash
nats kv get WORKFLOW_EXECUTIONS exec-<id>
```

Check `current_step` and `step_results`.

**Common causes**:
1. Missing termination condition in loop
2. Step action hanging
3. Infinite loop without `max_iterations`

**Resolution**:
- Add `on_success: "complete"` to terminal steps
- Add `max_iterations` to workflow
- Check step action timeouts

### Step Times Out

**Symptoms**: Step fails with timeout error.

**Diagnosis**: Check step duration in execution state.

**Common causes**:
1. External service slow
2. Timeout too short for operation
3. Deadlock in action handler

**Resolution**:
- Increase step `timeout`
- Add retry with backoff
- Profile external service

### Variable Not Resolved

**Symptoms**: Payload contains literal `${...}` instead of value.

**Common causes**:
1. Wrong variable path
2. Step output doesn't exist
3. Typo in variable name

**Resolution**:
- Check step name in `${steps.X.output}`
- Verify step completed successfully before reference
- Use exact field path from step output

## NATS Connectivity Issues

### Connection Refused

**Symptoms**: `dial tcp: connection refused`

**Diagnosis**:
```bash
# Check NATS is running
docker ps | grep nats

# Test connection
nats server ping
```

**Resolution**:
- Start NATS server
- Verify port mapping
- Check firewall rules

### Stream Not Found

**Symptoms**: `stream not found` error.

**Diagnosis**:
```bash
nats stream ls
```

**Resolution**:
- Streams are auto-created on first use in most configs
- Manually create if needed:
```bash
nats stream add AGENT --subjects "agent.>" --retention limits
```

### Consumer Lag

**Symptoms**: Messages processing slowly, high pending count.

**Diagnosis**:
```bash
nats consumer info AGENT <consumer-name>
```

Check `num_pending` and `num_ack_pending`.

**Resolution**:
- Scale consumers
- Increase batch size
- Check for slow message handlers
- Review consumer ack policy

## Graph Processor Issues

### Entity Not Indexed

**Symptoms**: Entity exists but not found in index queries.

**Diagnosis**:
```bash
# Check entity exists
nats kv get ENTITY_STATES <entity_id>

# Check predicate index
nats kv get PREDICATE_INDEX <predicate>
```

**Common causes**:
1. Indexing lag
2. Entity missing required predicates
3. Index bucket not created

**Resolution**:
- Wait for indexing to complete
- Verify entity has required predicates
- Check graph processor logs

### Triple Update Not Reflected

**Symptoms**: Entity has old triple values.

**Diagnosis**:
```bash
nats kv history ENTITY_STATES <entity_id>
```

Check revision history.

**Resolution**:
- Check for concurrent updates (CAS failures)
- Verify update actually published
- Check graph processor subscription

## E2E Test Failures

### Port Conflicts

**Symptoms**: `address already in use`

**Diagnosis**:
```bash
task e2e:check-ports
lsof -i :8080
```

**Resolution**:
```bash
# Kill processes using the port
lsof -ti:8080 | xargs kill -9

# Clean up Docker
task e2e:clean
```

### Services Not Healthy

**Symptoms**: Health check fails, services not ready.

**Diagnosis**:
```bash
docker compose -f docker/compose/e2e.yml ps
docker logs semstreams-e2e-app
```

**Resolution**:
- Check container logs for startup errors
- Increase health check timeout
- Verify configuration files

### Mock LLM Not Responding

**Symptoms**: agentic-model hangs waiting for response.

**Diagnosis**:
```bash
docker logs mock-llm
curl http://localhost:8089/health
```

**Resolution**:
- Restart mock-llm container
- Check mock-llm configuration
- Verify network connectivity

## Log Analysis

### Log Levels

| Level | Description |
|-------|-------------|
| DEBUG | Variable resolution, detailed flow |
| INFO | Component lifecycle, normal operations |
| WARN | Retries, recoverable issues |
| ERROR | Failures requiring attention |

### Key Log Patterns

**Component startup**:
```
level=INFO msg="component started" component=agentic-loop
```

**Message processing**:
```
level=INFO msg="message received" subject=agent.task.* loop_id=abc123
```

**Errors**:
```
level=ERROR msg="handler failed" error="connection timeout" loop_id=abc123
```

### Structured Log Queries

With JSON logging:
```bash
# Find errors for specific loop
cat logs.json | jq 'select(.loop_id == "abc123" and .level == "ERROR")'

# Count errors by component
cat logs.json | jq 'select(.level == "ERROR") | .component' | sort | uniq -c
```

## Performance Issues

### High Memory Usage

**Diagnosis**:
```bash
# Check Go heap
curl http://localhost:9090/debug/pprof/heap > heap.out
go tool pprof heap.out
```

**Common causes**:
1. Large tool results in context
2. Many concurrent loops
3. Memory leak in executor

**Resolution**:
- Enable context compaction
- Reduce max_iterations
- Profile with pprof

### High Latency

**Diagnosis**:
```bash
# Check metrics
curl http://localhost:9090/metrics | grep duration
```

**Common causes**:
1. Slow LLM endpoint
2. Tool execution time
3. NATS consumer lag

**Resolution**:
- Use local LLM for testing
- Add tool timeout
- Scale consumers

## Getting Help

### Information to Gather

Before seeking help, collect:

1. **Configuration**: Relevant config sections (redact secrets)
2. **Logs**: Error messages and surrounding context
3. **State**: KV bucket contents for affected entities
4. **Metrics**: Prometheus metrics around the issue time
5. **Version**: SemStreams version and Go version

### Useful Commands

```bash
# Export configuration
cat configs/agentic.json | jq 'del(.endpoints[].api_key)'

# Export loop state
nats kv get AGENT_LOOPS <loop_id> > loop-state.json

# Export recent logs
docker logs semstreams-app --since 10m > recent-logs.txt

# Export metrics
curl http://localhost:9090/metrics > metrics.txt
```

## Related Documentation

- [Agentic Components](../advanced/08-agentic-components.md) — Component details
- [Payload Registry](../concepts/15-payload-registry.md) — Serialization patterns
- [E2E Tests](../contributing/02-e2e-tests.md) — Test infrastructure
- [Local Monitoring](01-local-monitoring.md) — Observability setup
