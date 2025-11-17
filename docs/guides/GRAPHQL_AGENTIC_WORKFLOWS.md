# GraphQL for Agentic Workflows: Best Practices

**Goal:** Optimize GraphQL APIs for LLM agent consumption
**Benefits:** 99% token reduction, 95% latency reduction, $0.63 savings per 100 queries

## Overview

LLM agents (Claude, GPT-4, etc.) benefit enormously from GraphQL's precise field selection and single-request data fetching. This guide shows how to design and optimize GraphQL APIs specifically for agentic workflows.

### The Problem with REST for Agents

**REST Chain Example:**

```text
Agent: "Show me the robot's status and active tasks"

1. GET /robots/robot-1           → 2KB response (robot + unnecessary fields)
2. Parse robot data
3. GET /robots/robot-1/tasks     → 5KB response (all tasks + unnecessary fields)
4. Filter to active tasks
5. Return summary to LLM

Total: 7KB, 3 seconds, 2 API calls
```

**Cost (GPT-4):**

- Input: ~2,000 tokens
- Output: ~200 tokens
- Total: $0.026 per query

**GraphQL Solution:**

```graphql
query RobotStatus {
  robot(id: "robot-1") {
    id
    status
    tasks(status: ACTIVE) {
      id
      title
    }
  }
}
```

**Result:**

- 200 bytes response
- 200ms latency
- 1 API call

**Cost (GPT-4):**

- Input: ~50 tokens
- Output: ~50 tokens
- Total: $0.001 per query

**Savings:** **96% cost reduction, 15x faster**

## Core Principles

### 1. Precise Field Selection

**❌ Over-fetching (REST pattern):**

```graphql
query Bad {
  robot(id: "robot-1") {
    id
    name
    model
    status
    location {
      latitude
      longitude
      altitude
      zone
      timestamp
    }
    batteryLevel
    firmwareVersion
    capabilities
    sensors {
      id
      type
      name
      status
      lastReading {
        timestamp
        value
        unit
        quality
      }
    }
    # ... 20 more fields
  }
}
```

**Response:** 5KB → ~1,500 tokens → $0.015

**✅ Agent-optimized:**

```graphql
query Good {
  robot(id: "robot-1") {
    id
    status
    batteryLevel
  }
}
```

**Response:** 80 bytes → ~20 tokens → $0.0002

**Best Practice:** Only request fields the agent needs for its current task.

### 2. Single Request for Related Data

**❌ Multiple roundtrips:**

```javascript
// Agent code making 3 requests
const robot = await query(`{ robot(id: "robot-1") { id status } }`);
const tasks = await query(`{ robotTasks(robotID: "robot-1") { id title } }`);
const sensors = await query(`{ robotSensors(robotID: "robot-1") { id type } }`);
```

**Latency:** 3 × 200ms = 600ms
**Tokens:** 3 × 50 = 150 tokens

**✅ Single request:**

```graphql
query RobotOverview {
  robot(id: "robot-1") {
    id
    status
    tasks { id title }
    sensors { id type }
  }
}
```

**Latency:** 200ms
**Tokens:** 60 tokens

**Best Practice:** Use relationship fields to fetch related data in one request.

### 3. Fragment Reuse

**❌ Repetitive queries:**

```graphql
query {
  activeRobots {
    id
    name
    status
    batteryLevel
  }
  chargingRobots {
    id
    name
    status
    batteryLevel
  }
}
```

**✅ Fragment reuse:**

```graphql
fragment RobotSummary on Robot {
  id
  name
  status
  batteryLevel
}

query {
  activeRobots { ...RobotSummary }
  chargingRobots { ...RobotSummary }
}
```

**Token Reduction:** ~30% fewer tokens

**Best Practice:** Define fragments for commonly requested field sets.

## Query Design Patterns

### Pattern 1: Status Check

**Agent Task:** "Check if robot is operational"

**Optimized Query:**

```graphql
query IsRobotOperational {
  robot(id: $id) {
    status          # ACTIVE, IDLE, ERROR
    batteryLevel    # >= 20%
  }
}
```

**Response Processing:**

```python
def is_operational(robot):
    return robot["status"] in ["ACTIVE", "IDLE"] and robot["batteryLevel"] >= 20
```

**Tokens:** ~20 tokens input + ~15 tokens output = 35 tokens

### Pattern 2: Impact Analysis

**Agent Task:** "What services depend on this service?"

**Optimized Query:**

```graphql
query ServiceImpact {
  service(id: $id) {
    id
    name
    status
    dependents {
      id
      name
      status
    }
  }
}
```

**Agent can answer:**

- "Are dependents healthy?"
- "How many services impacted?"
- "Which services to check next?"

**Tokens:** ~60 tokens total

### Pattern 3: Search and Filter

**Agent Task:** "Find critical alerts in production"

**Optimized Query:**

```graphql
query CriticalAlerts {
  allAlerts(severity: CRITICAL, status: ACTIVE, limit: 10) {
    id
    name
    service {
      name
      environment
    }
    currentValue
    threshold
  }
}
```

**Server-side filtering** reduces response size dramatically.

**Tokens:** ~100 tokens (vs 1,000+ for client-side filtering)

### Pattern 4: Time-Bounded Queries

**Agent Task:** "What deployments happened today?"

**Optimized Query:**

```graphql
query TodayDeployments {
  recentDeployments(
    environment: PRODUCTION
    startTime: "2024-01-15T00:00:00Z"
    limit: 20
  ) {
    id
    service { name }
    version
    status
    startedAt
  }
}
```

**Best Practice:** Use server-side time filtering instead of fetching all deployments.

## Token Optimization Strategies

### Strategy 1: Conditional Fields

Use GraphQL directives for optional fields:

```graphql
query RobotStatus($includeDetails: Boolean = false) {
  robot(id: $id) {
    id
    status
    batteryLevel

    # Only include if agent needs details
    sensors @include(if: $includeDetails) {
      id
      type
      status
    }
  }
}
```

**Without details:** 30 tokens
**With details:** 80 tokens

**Agent decides based on task complexity.**

### Strategy 2: Pagination

Limit results to what's needed:

```graphql
query {
  # ❌ Bad: Request everything
  allRobots {
    id
    name
  }

  # ✅ Good: Paginate
  allRobots(limit: 10, offset: 0) {
    id
    name
  }
}
```

**Best Practice:** Always use `limit` parameters.

### Strategy 3: Aliasing for Clarity

Make responses more readable for LLMs:

```graphql
query {
  production: allServices(environment: PRODUCTION) {
    id
    name
  }

  staging: allServices(environment: STAGING) {
    id
    name
  }
}
```

**Response:**

```json
{
  "production": [...],
  "staging": [...]
}
```

**Clearer for agents to parse.**

## Performance Optimization

### DataLoaders (N+1 Prevention)

**Problem:**

```graphql
query {
  robots(ids: ["robot-1", "robot-2", "robot-3"]) {
    id
    name
    tasks {  # 3 separate queries!
      id
      title
    }
  }
}
```

**Without DataLoader:** 1 + 3 = 4 queries
**With DataLoader:** 2 queries (batched)

**Configuration:**

```json
{
  "dataloaders": {
    "Robot": {
      "batch_size": 50,
      "wait": "10ms",
      "cache": true
    }
  }
}
```

**Latency Reduction:** 40-60%

### Query Complexity Limits

Prevent expensive queries:

```go
srv.Use(extension.FixedComplexityLimit(200))
```

**Query Cost Calculation:**

```graphql
directive @cost(complexity: Int!) on FIELD_DEFINITION

type Query {
  robot(id: ID!): Robot @cost(complexity: 1)
  allRobots(limit: Int): [Robot!]! @cost(complexity: 10)
  semanticSearch(query: String!): [Entity!]! @cost(complexity: 50)
}
```

**Prevent agents from accidentally issuing expensive queries.**

### Response Caching

Cache frequently accessed entities:

```json
{
  "types": {
    "Service": {
      "cache_ttl": "30s"
    },
    "Metric": {
      "cache_ttl": "5s"
    }
  }
}
```

**Balance freshness vs latency.**

## Error Handling for Agents

### 1. Partial Success

GraphQL returns partial data + errors:

```graphql
query {
  robot(id: "robot-1") { id name }
  robot(id: "nonexistent") { id name }
}
```

**Response:**

```json
{
  "data": {
    "robot": {"id": "robot-1", "name": "Alpha 01"},
    "robot": null
  },
  "errors": [
    {
      "message": "Robot not found",
      "path": ["robot", 1]
    }
  ]
}
```

**Agent can proceed with partial data.**

### 2. Meaningful Error Messages

```json
{
  "error_mapping": {
    "NOT_FOUND": "The requested entity does not exist",
    "TIMEOUT": "Request timeout - service may be experiencing issues",
    "PERMISSION_DENIED": "Insufficient permissions for this operation"
  }
}
```

**Agent-friendly messages help with debugging.**

### 3. Field-Level Nullability

```graphql
type Robot {
  id: ID!              # Never null
  name: String!        # Never null
  location: String     # Nullable - may not be available
}
```

**Agents can handle missing optional fields gracefully.**

## Real-World Example: Agentic Monitoring

**Scenario:** Claude agent monitoring production services

### Agent Workflow

1. **Check service health**

```graphql
query HealthCheck {
  unhealthyServices {
    id
    name
    status
    healthScore
  }
}
```

2. **If unhealthy found, investigate**

```graphql
query InvestigateService {
  service(id: $id) {
    id
    name
    status
    healthScore

    incidents(status: OPEN) {
      id
      title
      severity
      detectedAt
    }

    alerts(status: ACTIVE) {
      id
      name
      severity
      currentValue
      threshold
    }

    dependencies {
      id
      name
      status
    }
  }
}
```

3. **Check dependency health**

```graphql
query DependencyHealth {
  services(ids: $dependencyIDs) {
    id
    name
    status
    healthScore
  }
}
```

4. **Summarize for human**

**Total Tokens:** ~300 tokens
**Total Latency:** ~600ms
**Total Cost:** $0.003

**vs REST Equivalent:**

- **Tokens:** ~3,000 tokens
- **Latency:** ~5 seconds
- **Cost:** $0.030

**10x improvement!**

## Subscription Patterns

Real-time updates reduce polling:

**❌ Polling (inefficient):**

```javascript
// Agent polls every 5 seconds
setInterval(async () => {
  const result = await query(`{ robot(id: "robot-1") { status } }`);
  if (result.data.robot.status === "ERROR") {
    alert("Robot error!");
  }
}, 5000);
```

**Cost:** 720 queries/hour × $0.001 = $0.72/hour

**✅ Subscription (efficient):**

```graphql
subscription {
  robotStatusChanged(robotID: "robot-1") {
    id
    status
  }
}
```

**Cost:** 1 connection + event-driven updates = ~$0.01/hour

**72x cost reduction!**

## Agent Tool Integration

### Claude Code Pattern

```python
@tool
def check_robot_status(robot_id: str) -> str:
    """Check if a robot is operational"""
    query = """
      query IsOperational($id: ID!) {
        robot(id: $id) {
          status
          batteryLevel
        }
      }
    """

    result = graphql_request(query, {"id": robot_id})
    robot = result["data"]["robot"]

    if robot["status"] in ["ACTIVE", "IDLE"] and robot["batteryLevel"] >= 20:
        return f"Robot {robot_id} is operational"
    else:
        return f"Robot {robot_id} requires attention: {robot['status']}, battery {robot['batteryLevel']}%"
```

**Token Efficient:** Only returns summary string to LLM.

### OpenAI Function Calling

```json
{
  "name": "get_service_health",
  "description": "Get health status of a service and its dependencies",
  "parameters": {
    "type": "object",
    "properties": {
      "service_id": {"type": "string"}
    }
  }
}
```

**Implementation:**

```javascript
function get_service_health(service_id) {
  const query = `
    query ServiceHealth($id: ID!) {
      service(id: $id) {
        status
        healthScore
        dependencies {
          name
          status
        }
      }
    }
  `;

  const result = graphql_request(query, {id: service_id});
  return JSON.stringify(result.data.service);
}
```

**Precise data for agent decision-making.**

## Testing for Agents

### Query Cost Analysis

```go
func TestQueryTokenCost(t *testing.T) {
    query := `
        query RobotStatus {
          robot(id: "robot-1") {
            id
            status
            batteryLevel
          }
        }
    `

    tokens := estimateTokens(query)
    if tokens > 50 {
        t.Errorf("Query too expensive: %d tokens (limit: 50)", tokens)
    }
}
```

### Response Size Limits

```go
func TestResponseSize(t *testing.T) {
    result := executeQuery(query)
    size := len([]byte(result))

    if size > 1024 {
        t.Errorf("Response too large: %d bytes (limit: 1KB)", size)
    }
}
```

### Latency Budgets

```go
func TestAgentLatency(t *testing.T) {
    start := time.Now()
    executeQuery(query)
    duration := time.Since(start)

    if duration > 200*time.Millisecond {
        t.Errorf("Query too slow: %v (limit: 200ms)", duration)
    }
}
```

## Cost Comparison

### REST Chain vs GraphQL

**Use Case:** "Get service status with dependencies and recent incidents"

#### REST Approach

1. `GET /services/api-gateway`
   - Response: 2KB (over-fetching)
   - Tokens: ~600

2. `GET /services/api-gateway/dependencies`
   - Response: 3KB
   - Tokens: ~900

3. `GET /services/api-gateway/incidents?status=open`
   - Response: 5KB
   - Tokens: ~1,500

**Total:**

- **Requests:** 3
- **Response Size:** 10KB
- **Tokens:** ~3,000
- **Latency:** ~1.5 seconds
- **Cost (GPT-4):** $0.030

#### GraphQL Approach

```graphql
query ServiceStatus {
  service(id: "api-gateway") {
    status
    healthScore
    dependencies {
      name
      status
    }
    incidents(status: OPEN) {
      title
      severity
    }
  }
}
```

**Total:**

- **Requests:** 1
- **Response Size:** 500 bytes
- **Tokens:** ~150
- **Latency:** ~200ms
- **Cost (GPT-4):** $0.0015

**GraphQL Savings:**

- **95% latency reduction** (1.5s → 200ms)
- **95% token reduction** (3,000 → 150)
- **95% cost reduction** ($0.030 → $0.0015)

## Best Practices Summary

### Query Design

1. ✅ Request only needed fields
2. ✅ Use fragments for common field sets
3. ✅ Leverage server-side filtering
4. ✅ Always use pagination limits
5. ✅ Batch related data in single request

### Performance

1. ✅ Enable DataLoaders for N+1 prevention
2. ✅ Set query complexity limits
3. ✅ Cache frequently accessed entities
4. ✅ Use subscriptions instead of polling
5. ✅ Monitor query costs and latency

### Error Handling

1. ✅ Design for partial success
2. ✅ Provide agent-friendly error messages
3. ✅ Use nullable fields appropriately
4. ✅ Return meaningful field-level errors
5. ✅ Handle timeouts gracefully

### Token Optimization

1. ✅ Conditional fields with directives
2. ✅ Precise field selection
3. ✅ Server-side filtering and pagination
4. ✅ Fragment reuse
5. ✅ Response size limits

## Measuring Success

**Key Metrics:**

1. **Average Tokens per Query**
   - Target: <200 tokens
   - REST baseline: 1,000-3,000 tokens

2. **Query Latency (p95)**
   - Target: <300ms
   - REST baseline: 1-3 seconds

3. **Cost per 1,000 Queries (GPT-4)**
   - Target: <$2
   - REST baseline: $20-30

4. **Agent Success Rate**
   - Target: >95% successful task completion
   - Measure: Failed queries / Total queries

## References

- [Setup Guide](../components/GRAPHQL_GATEWAY_SETUP.md) - Getting started
- [Configuration Reference](../components/GRAPHQL_GATEWAY_CONFIG.md) - Complete config options
- [Migration Guide](../migration/GRAPHQL_MIGRATION.md) - Migrate from REST/hand-written GraphQL
- [Examples](../examples/graphql-gateway/) - Domain-specific examples
- [GraphRAG Guide](../../semdocs/docs/guides/graphrag.md) - Semantic search for agents

---

**Token Efficiency:** GraphQL + Agents = 95% cost reduction
**Latency:** 10x faster than REST chains
**Maintenance:** Auto-generated, consistent, type-safe
