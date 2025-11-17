# Robotics GraphQL Example Queries

Sample queries for the robotics fleet management system.

## Single Entity Queries

### Get Robot by ID

```graphql
query GetRobot {
  robot(id: "robot-alpha-01") {
    id
    name
    model
    status
    location {
      latitude
      longitude
      zone
    }
    batteryLevel
    firmwareVersion
    capabilities
    lastHeartbeat
  }
}
```

**Use Case:** Display robot details in dashboard

**Response:**
```json
{
  "data": {
    "robot": {
      "id": "robot-alpha-01",
      "name": "Alpha 01",
      "model": "AMR-3000",
      "status": "ACTIVE",
      "location": {
        "latitude": 37.7749,
        "longitude": -122.4194,
        "zone": "warehouse-a"
      },
      "batteryLevel": 87,
      "firmwareVersion": "2.1.4",
      "capabilities": ["DELIVERY", "PATROL", "INSPECTION"],
      "lastHeartbeat": "2024-01-15T10:30:00Z"
    }
  }
}
```

### Get Task with Assigned Robot

```graphql
query GetTask {
  task(id": "task-delivery-001") {
    id
    title
    description
    type
    priority
    status
    assignedTo {
      id
      name
      status
      batteryLevel
    }
    scheduledStart
    waypoints {
      id
      location {
        latitude
        longitude
      }
      action
      sequence
    }
  }
}
```

**Use Case:** Task tracking with robot assignment

## List Queries

### List All Active Robots

```graphql
query ActiveRobots {
  allRobots(limit: 10, status: ACTIVE) {
    id
    name
    status
    location {
      zone
    }
    batteryLevel
    tasks {
      id
      title
      status
    }
  }
}
```

**Use Case:** Fleet overview dashboard

### List Pending Tasks

```graphql
query PendingTasks {
  allTasks(limit: 20, status: PENDING) {
    id
    title
    type
    priority
    scheduledStart
    estimatedDuration
  }
}
```

**Use Case:** Task queue for dispatcher

## Relationship Queries

### Robot with All Tasks

```graphql
query RobotWithTasks {
  robot(id: "robot-alpha-01") {
    id
    name
    status
    tasks {
      id
      title
      status
      priority
      scheduledStart
      completedAt
    }
  }
}
```

**Use Case:** Robot workload analysis

### Robot with Sensors

```graphql
query RobotSensors {
  robot(id: "robot-alpha-01") {
    id
    name
    sensors {
      id
      type
      name
      status
      lastReading {
        timestamp
        value
        unit
      }
    }
  }
}
```

**Use Case:** Robot diagnostics

### Task with Waypoints

```graphql
query TaskNavigation {
  task(id: "task-patrol-005") {
    id
    title
    status
    waypoints {
      id
      sequence
      location {
        latitude
        longitude
        zone
      }
      action
      reachedAt
    }
    assignedTo {
      id
      name
      location {
        latitude
        longitude
      }
    }
  }
}
```

**Use Case:** Navigation tracking

## Search Queries

### Semantic Search for Robots

```graphql
query SearchRobots {
  searchRobots(query: "autonomous delivery warehouse", limit: 5) {
    id
    name
    model
    capabilities
    status
    location {
      zone
    }
  }
}
```

**Use Case:** Natural language robot search

### Search Tasks by Description

```graphql
query SearchTasks {
  searchTasks(query: "urgent maintenance inspection", limit: 10) {
    id
    title
    description
    type
    priority
    status
  }
}
```

**Use Case:** Find tasks by description

## Analytics Queries

### Low Battery Robots

```graphql
query LowBatteryRobots {
  lowBatteryRobots(threshold: 20) {
    id
    name
    batteryLevel
    location {
      zone
    }
    status
    tasks {
      id
      title
      status
    }
  }
}
```

**Use Case:** Proactive charging management

### High Priority Tasks

```graphql
query HighPriorityTasks {
  tasksByPriority(minPriority: 8) {
    id
    title
    priority
    status
    assignedTo {
      id
      name
      status
    }
    scheduledStart
  }
}
```

**Use Case:** Priority queue management

### Robots by Zone

```graphql
query WarehouseRobots {
  robotsByZone(zone: "warehouse-a") {
    id
    name
    status
    batteryLevel
    tasks {
      id
      title
      status
    }
  }
}
```

**Use Case:** Zone-based fleet monitoring

## Subscription Examples

### Watch Robot Status Changes

```graphql
subscription RobotStatus {
  robotStatusChanged(robotID: "robot-alpha-01") {
    id
    name
    status
    location {
      zone
    }
    batteryLevel
    lastHeartbeat
  }
}
```

**Use Case:** Real-time status monitoring

### Low Battery Alerts

```graphql
subscription BatteryAlerts {
  lowBatteryAlert(threshold: 15) {
    id
    name
    batteryLevel
    location {
      zone
    }
    status
  }
}
```

**Use Case:** Automatic charging alerts

### Task Updates

```graphql
subscription TaskUpdates {
  taskStatusChanged(taskID: "task-delivery-001") {
    id
    title
    status
    assignedTo {
      id
      name
    }
    completedAt
  }
}
```

**Use Case:** Real-time task tracking

### Robot Telemetry Stream

```graphql
subscription RobotTelemetry {
  robotTelemetry(robotID: "robot-alpha-01") {
    robotID
    timestamp
    velocity
    heading
    battery
    cpuUsage
    memoryUsage
    networkLatency
    errorCount
  }
}
```

**Use Case:** Real-time robot monitoring

## Complex Queries

### Fleet Overview

```graphql
query FleetOverview {
  allRobots(limit: 50) {
    id
    name
    status
    batteryLevel
    location {
      zone
    }
    tasks {
      id
      status
    }
    sensors {
      id
      type
      status
    }
  }
}
```

**Use Case:** Complete fleet dashboard

### Task Allocation View

```graphql
query TaskAllocation {
  allTasks(limit: 100) {
    id
    title
    priority
    status
    assignedTo {
      id
      name
      batteryLevel
      status
    }
    estimatedDuration
    actualDuration
  }
}
```

**Use Case:** Dispatcher task allocation

## LLM Agent Queries

### Agent: "Show me robots that need attention"

```graphql
query RobotsNeedingAttention {
  lowBatteryRobots(threshold: 20) {
    id
    name
    batteryLevel
    status
  }

  allRobots(limit: 100, status: ERROR) {
    id
    name
    status
    lastHeartbeat
  }
}
```

**Token Efficiency:** Single request vs multiple REST calls

### Agent: "What's the status of delivery tasks?"

```graphql
query DeliveryTaskStatus {
  allTasks(limit: 50, status: IN_PROGRESS) {
    id
    title
    assignedTo {
      id
      name
      location {
        zone
      }
    }
    scheduledStart
    estimatedDuration
  }
}
```

**Precise Fields:** Only what the agent needs

## Performance Optimizations

### DataLoader Batching

Multiple robots with tasks - batched efficiently:

```graphql
query MultipleRobotsWithTasks {
  robots(ids: ["robot-1", "robot-2", "robot-3"]) {
    id
    name
    tasks {
      id
      title
    }
  }
}
```

**Without DataLoader:** 1 + 3 queries (N+1 problem)
**With DataLoader:** 2 queries (batched)

### Fragment Reuse

```graphql
fragment RobotSummary on Robot {
  id
  name
  status
  batteryLevel
  location {
    zone
  }
}

query Dashboard {
  activeRobots: allRobots(status: ACTIVE, limit: 10) {
    ...RobotSummary
  }

  chargingRobots: allRobots(status: CHARGING, limit: 10) {
    ...RobotSummary
  }
}
```

**Token Reduction:** Reuse fragments across queries

## Error Handling

### Handle Missing Robot

```graphql
query MaybeRobot {
  robot(id: "nonexistent") {
    id
    name
  }
}
```

**Response:**
```json
{
  "data": {
    "robot": null
  },
  "errors": [
    {
      "message": "Robot, task, or sensor not found",
      "path": ["robot"]
    }
  ]
}
```

### Handle Partial Failures

```graphql
query PartialBatch {
  robots(ids: ["robot-1", "nonexistent", "robot-2"]) {
    id
    name
  }
}
```

**Response:**
```json
{
  "data": {
    "robots": [
      {"id": "robot-1", "name": "Robot 1"},
      {"id": "robot-2", "name": "Robot 2"}
    ]
  },
  "errors": [
    {
      "message": "Some robots not found",
      "extensions": {
        "missing": ["nonexistent"]
      }
    }
  ]
}
```

## Best Practices

### 1. Request Only Needed Fields

```graphql
# ❌ Over-fetching
query Bad {
  robots {
    id
    name
    status
    location { latitude longitude altitude zone }
    batteryLevel
    firmwareVersion
    capabilities
    tasks { id title status }
    sensors { id type status }
    telemetry { velocity heading battery }
  }
}

# ✅ Precise fields
query Good {
  robots {
    id
    name
    status
    batteryLevel
  }
}
```

### 2. Use Limits

```graphql
# ✅ Always specify limits
query {
  allRobots(limit: 20) {
    id
    name
  }
}
```

### 3. Paginate Large Results

```graphql
# Future: Add pagination
query {
  allRobots(limit: 20, offset: 40) {
    id
    name
  }
}
```

### 4. Leverage Subscriptions

Real-time data via WebSocket instead of polling:

```graphql
# ❌ Polling (inefficient)
setInterval(() => {
  query { robot(id: "robot-1") { status } }
}, 1000)

# ✅ Subscription (efficient)
subscription {
  robotStatusChanged(robotID: "robot-1") { status }
}
```

---

**Related Examples:**
- [SaaS Monitoring](../saas-monitoring/queries.md)
- [SemMem](../../semmem/queries.md)

**Documentation:**
- [Setup Guide](../../../components/GRAPHQL_GATEWAY_SETUP.md)
- [Configuration Reference](../../../components/GRAPHQL_GATEWAY_CONFIG.md)
