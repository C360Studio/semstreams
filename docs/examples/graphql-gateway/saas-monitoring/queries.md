# SaaS Monitoring GraphQL Example Queries

Sample queries for the multi-tenant application monitoring and observability platform.

## Service Queries

### Get Service with Dependencies

```graphql
query GetService {
  service(id: "api-gateway-prod") {
    id
    name
    displayName
    type
    status
    healthScore
    version
    environment
    owner
    dependencies {
      id
      name
      status
      healthScore
    }
    dependents {
      id
      name
      type
    }
  }
}
```

**Use Case:** Service dependency visualization

### List Production Services

```graphql
query ProductionServices {
  allServices(limit: 50, environment: PRODUCTION) {
    id
    name
    type
    status
    healthScore
    incidents {
      id
      title
      severity
      status
    }
  }
}
```

**Use Case:** Production service monitoring dashboard

## Alert and Incident Queries

### Get Critical Alerts

```graphql
query CriticalAlerts {
  allAlerts(limit: 20, severity: CRITICAL, status: ACTIVE) {
    id
    name
    service {
      id
      name
      environment
    }
    condition
    threshold
    currentValue
    triggeredAt
    incidents {
      id
      title
      status
    }
  }
}
```

**Use Case:** Alert management dashboard

### Get Open Incidents

```graphql
query OpenIncidents {
  allIncidents(limit: 30, status: OPEN) {
    id
    title
    description
    severity
    service {
      id
      name
      type
    }
    alerts {
      id
      name
      severity
    }
    assignee
    detectedAt
    impactedUsers
  }
}
```

**Use Case:** Incident response queue

### Get Incident Details with Timeline

```graphql
query IncidentDetails {
  incident(id: "inc-2024-001") {
    id
    title
    description
    severity
    status
    service {
      id
      name
      environment
      dependencies {
        id
        name
        status
      }
    }
    alerts {
      id
      name
      triggeredAt
      condition
      threshold
      currentValue
    }
    assignee
    rootCause
    resolution
    impactedUsers
    detectedAt
    acknowledgedAt
    resolvedAt
    duration
  }
}
```

**Use Case:** Postmortem analysis

## Metrics and Monitoring

### Service with Recent Metrics

```graphql
query ServiceMetrics {
  service(id: "api-gateway-prod") {
    id
    name
    metrics {
      id
      name
      type
      value
      unit
      timestamp
      labels {
        key
        value
      }
    }
  }
}
```

**Use Case:** Real-time metrics visualization

### Service Health Time Series

```graphql
query HealthTimeSeries {
  serviceHealthTimeSeries(
    serviceID: "api-gateway-prod"
    startTime: "2024-01-15T00:00:00Z"
    endTime: "2024-01-15T23:59:59Z"
  ) {
    timestamp
    value
  }
}
```

**Use Case:** Health score trending

## Deployment Queries

### Recent Deployments

```graphql
query RecentDeployments {
  recentDeployments(environment: PRODUCTION, limit: 10) {
    id
    service {
      id
      name
    }
    version
    status
    deployedBy
    commitSHA
    startedAt
    completedAt
    duration
    rollback
    incidents {
      id
      title
      severity
    }
  }
}
```

**Use Case:** Deployment dashboard

### Deployment with Impact

```graphql
query DeploymentImpact {
  deployment(id: "deploy-2024-001") {
    id
    service {
      id
      name
    }
    version
    status
    deployedBy
    commitSHA
    startedAt
    completedAt
    incidents {
      id
      title
      severity
      status
      detectedAt
      impactedUsers
    }
  }
}
```

**Use Case:** Deployment impact analysis

## Relationship Queries

### Service Dependency Map

```graphql
query DependencyMap {
  service(id: "api-gateway-prod") {
    id
    name
    dependencies {
      id
      name
      status
      healthScore
      dependencies {
        id
        name
        status
      }
    }
  }
}
```

**Use Case:** Multi-level dependency visualization

### Service Alert Chain

```graphql
query ServiceAlertChain {
  service(id: "api-gateway-prod") {
    id
    name
    alerts {
      id
      name
      severity
      status
      incidents {
        id
        title
        status
        assignee
      }
    }
  }
}
```

**Use Case:** Alert → incident correlation

## Search Queries

### Search Services by Keywords

```graphql
query SearchServices {
  searchServices(query: "payment api gateway", limit: 10) {
    id
    name
    displayName
    type
    environment
    status
    owner
  }
}
```

**Use Case:** Natural language service discovery

### Search Incidents

```graphql
query SearchIncidents {
  searchIncidents(query: "database timeout production", limit: 20) {
    id
    title
    description
    severity
    status
    service {
      id
      name
    }
    detectedAt
  }
}
```

**Use Case:** Incident search and discovery

## Analytics Queries

### Unhealthy Services

```graphql
query UnhealthyServices {
  unhealthyServices {
    id
    name
    type
    status
    healthScore
    environment
    incidents {
      id
      severity
      status
    }
    alerts {
      id
      severity
      status
    }
  }
}
```

**Use Case:** Health monitoring dashboard

### Service SLA Status

```graphql
query ServiceSLA {
  service(id: "api-gateway-prod") {
    id
    name
    healthScore
    incidents {
      id
      severity
      duration
      impactedUsers
      detectedAt
      resolvedAt
    }
    deployments {
      id
      status
      startedAt
      completedAt
      rollback
    }
  }
}
```

**Use Case:** SLA compliance reporting

## Subscription Examples

### Watch Service Status

```graphql
subscription ServiceStatus {
  serviceStatusChanged(serviceID: "api-gateway-prod") {
    id
    name
    status
    healthScore
    updatedAt
  }
}
```

**Use Case:** Real-time service monitoring

### Alert Triggers

```graphql
subscription AlertTriggers {
  alertTriggered(severity: CRITICAL) {
    id
    name
    service {
      id
      name
      environment
    }
    severity
    condition
    currentValue
    threshold
    triggeredAt
  }
}
```

**Use Case:** Real-time alert notifications

### Incident Updates

```graphql
subscription IncidentUpdates {
  incidentUpdated(severity: CRITICAL) {
    id
    title
    severity
    status
    service {
      id
      name
    }
    assignee
    updatedAt
  }
}
```

**Use Case:** Incident room updates

### Deployment Events

```graphql
subscription DeploymentEvents {
  deploymentStatusChanged(environment: PRODUCTION) {
    id
    service {
      id
      name
    }
    version
    status
    deployedBy
    startedAt
    completedAt
  }
}
```

**Use Case:** Real-time deployment tracking

### Metric Stream

```graphql
subscription MetricStream {
  metricUpdated(
    serviceID: "api-gateway-prod"
    metricName: "request_rate"
  ) {
    id
    name
    value
    unit
    timestamp
  }
}
```

**Use Case:** Real-time metric visualization

## Complex Queries

### Full Service Overview

```graphql
query ServiceOverview {
  service(id: "api-gateway-prod") {
    id
    name
    displayName
    type
    status
    healthScore
    version
    environment
    owner

    dependencies {
      id
      name
      status
      healthScore
    }

    dependents {
      id
      name
      type
    }

    metrics {
      id
      name
      value
      unit
      timestamp
    }

    alerts {
      id
      name
      severity
      status
      currentValue
      threshold
    }

    incidents {
      id
      title
      severity
      status
      impactedUsers
      detectedAt
    }

    deployments {
      id
      version
      status
      deployedBy
      startedAt
      rollback
    }
  }
}
```

**Use Case:** Complete service dashboard

### Environment Health

```graphql
query EnvironmentHealth {
  allServices(environment: PRODUCTION) {
    id
    name
    type
    status
    healthScore

    alerts {
      id
      severity
      status
    }

    incidents {
      id
      severity
      status
    }
  }
}
```

**Use Case:** Environment-wide health monitoring

## LLM Agent Queries

### Agent: "Show me critical issues in production"

```graphql
query CriticalProductionIssues {
  criticalAlerts {
    id
    name
    service {
      id
      name
      environment
    }
    currentValue
    threshold
    triggeredAt
  }

  openIncidents {
    id
    title
    severity
    service {
      id
      name
    }
    impactedUsers
    detectedAt
  }
}
```

**Token Efficiency:** Single request for multiple analytics queries

### Agent: "What's the deployment status?"

```graphql
query DeploymentStatus {
  recentDeployments(environment: PRODUCTION, limit: 5) {
    id
    service {
      name
    }
    version
    status
    deployedBy
    startedAt
    incidents {
      id
      severity
    }
  }
}
```

**Precise Fields:** Only deployment essentials

## Performance Patterns

### DataLoader Batching

Multiple services with dependencies:

```graphql
query MultipleServicesWithDeps {
  services(ids: ["svc-1", "svc-2", "svc-3"]) {
    id
    name
    dependencies {
      id
      name
    }
  }
}
```

**Without DataLoader:** 1 + 3 queries
**With DataLoader:** 2 queries (batched)

### Fragment Reuse

```graphql
fragment ServiceSummary on Service {
  id
  name
  type
  status
  healthScore
  environment
}

query Dashboard {
  healthy: allServices(status: HEALTHY, limit: 10) {
    ...ServiceSummary
  }

  degraded: allServices(status: DEGRADED, limit: 10) {
    ...ServiceSummary
  }

  unhealthy: allServices(status: UNHEALTHY, limit: 10) {
    ...ServiceSummary
  }
}
```

**Token Reduction:** Reuse fragment across status types

## Best Practices

### 1. Request Only Needed Fields

```graphql
# ❌ Over-fetching
query Bad {
  service(id: "svc-1") {
    id
    name
    type
    status
    healthScore
    version
    environment
    owner
    dependencies { id name }
    dependents { id name }
    metrics { id name value }
    alerts { id name severity }
    incidents { id title severity }
    deployments { id version status }
  }
}

# ✅ Precise fields
query Good {
  service(id: "svc-1") {
    id
    name
    status
    healthScore
  }
}
```

### 2. Use Filters

```graphql
# ✅ Filter at query level
query {
  allAlerts(severity: CRITICAL, status: ACTIVE, limit: 20) {
    id
    name
  }
}
```

### 3. Leverage Subscriptions

```graphql
# ❌ Polling (inefficient)
setInterval(() => {
  query { service(id: "svc-1") { status healthScore } }
}, 5000)

# ✅ Subscription (efficient)
subscription {
  serviceStatusChanged(serviceID: "svc-1") {
    status
    healthScore
  }
}
```

### 4. Batch Related Queries

```graphql
# ✅ Single request
query Dashboard {
  criticalAlerts { id name }
  openIncidents { id title }
  unhealthyServices { id name }
}
```

---

**Related Examples:**
- [Robotics](../robotics/queries.md)
- [SemMem](../../semmem/queries.md)

**Documentation:**
- [Setup Guide](../../../components/GRAPHQL_GATEWAY_SETUP.md)
- [Configuration Reference](../../../components/GRAPHQL_GATEWAY_CONFIG.md)
