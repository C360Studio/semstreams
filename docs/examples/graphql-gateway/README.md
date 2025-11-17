# GraphQL Gateway Examples

Complete working examples of GraphQL gateways for different domains, demonstrating configuration patterns, schema design, and query strategies.

## Overview

Each example includes:
- **schema.graphql** - Complete GraphQL schema
- **config.json** - Configuration for `semstreams-gqlgen`
- **queries.md** - Sample queries with use cases

## Examples

### 1. Robotics Fleet Management

**Domain:** Autonomous robot monitoring and task management
**Path:** [robotics/](./robotics/)

**Entities:**
- Robot - Autonomous mobile robots with sensors
- Task - Delivery, patrol, inspection tasks
- Sensor - LIDAR, camera, IMU, GPS sensors
- Waypoint - Navigation points
- Telemetry - Real-time robot metrics

**Highlights:**
- Real-time telemetry subscriptions
- Multi-level relationships (Robot → Task → Waypoint)
- Sensor data aggregation
- Battery and health monitoring

**Use Cases:**
- Fleet management dashboard
- Task allocation and tracking
- Predictive maintenance
- Navigation monitoring

**Files:**
- [schema.graphql](./robotics/schema.graphql) - 11 types, 15+ queries, 4 subscriptions
- [config.json](./robotics/config.json) - Complete field mappings and DataLoaders
- [queries.md](./robotics/queries.md) - 20+ example queries

### 2. SaaS Monitoring Platform

**Domain:** Multi-tenant application observability
**Path:** [saas-monitoring/](./saas-monitoring/)

**Entities:**
- Service - APIs, workers, databases being monitored
- Metric - Time-series performance metrics
- Alert - Threshold-based alerting rules
- Incident - Critical issues requiring response
- Deployment - Release tracking and rollbacks

**Highlights:**
- Service dependency graphs
- Alert → Incident correlation
- Deployment impact tracking
- Time-series metrics

**Use Cases:**
- Production monitoring dashboard
- Incident response queue
- SLA compliance reporting
- Deployment tracking

**Files:**
- [schema.graphql](./saas-monitoring/schema.graphql) - 15 types, 20+ queries, 5 subscriptions
- [config.json](./saas-monitoring/config.json) - Comprehensive relationship mappings
- [queries.md](./saas-monitoring/queries.md) - 25+ example queries

### 3. SemMem (Reference Implementation)

**Domain:** Semantic memory for development context
**Path:** [../../semmem/](../../semmem/)

**Entities:**
- Spec - Specification documents
- Doc - Documentation files
- Issue - GitHub issues
- Discussion - GitHub discussions
- PullRequest - Pull requests
- Community - GraphRAG communities

**Highlights:**
- GraphRAG (hierarchical community detection)
- Local/Global semantic search
- Spec-driven development workflow
- GitHub integration

**Use Cases:**
- AI coding assistant context
- Issue tracking with traceability
- GraphRAG knowledge queries
- Spec → Issue → PR workflow

**Files:**
- [schema.graphql](../../semmem/output/graphql/schema.graphql)
- [graphql-config.json](../../semmem/graphql-config.json)
- [README.md](../../semmem/README.md)

## Choosing an Example

| Example | Best For | Complexity | Highlights |
|---------|----------|------------|------------|
| **Robotics** | IoT, real-time systems, sensors | Medium | Subscriptions, telemetry, multi-level relationships |
| **SaaS Monitoring** | Observability, alerting, SLAs | Medium-High | Dependency graphs, time-series, incident management |
| **SemMem** | Knowledge graphs, semantic search, AI | High | GraphRAG, community detection, semantic relationships |

## Getting Started

### 1. Choose an Example

Pick the example closest to your domain:

```bash
cd docs/examples/graphql-gateway/robotics
# or
cd docs/examples/graphql-gateway/saas-monitoring
```

### 2. Generate Code

```bash
# Install code generator
go install github.com/c360/semstreams/cmd/semstreams-gqlgen@latest

# Generate resolver code
semstreams-gqlgen -config=config.json -output=generated

# Generate gqlgen types
gqlgen generate
```

### 3. Run the Server

```bash
# Start NATS
docker run -d -p 4222:4222 nats:latest -js

# Run GraphQL server (see example code in queries.md)
go run server.go
```

### 4. Try Queries

Visit http://localhost:8080 for GraphQL Playground and try the example queries from `queries.md`.

## Common Patterns

### Entity → Property Mapping

All examples follow consistent mapping patterns:

```json
{
  "fields": {
    "TypeName.id": {
      "property": "id",
      "type": "string"
    },
    "TypeName.name": {
      "property": "properties.name",
      "type": "string"
    }
  }
}
```

### Relationship Mapping

```json
{
  "fields": {
    "Robot.tasks": {
      "resolver": "QueryRelationships",
      "subject": "graph.robot.tasks",
      "edge_type": "assigned_to"
    },
    "Task.assignedTo": {
      "resolver": "QueryRelationships",
      "subject": "graph.task.robot",
      "edge_type": "assigned_to",
      "direction": "incoming"
    }
  }
}
```

### DataLoader Configuration

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

## Customization Guide

### Adapting for Your Domain

1. **Start with closest example**
   - Copy schema.graphql and config.json
   - Modify types to match your domain

2. **Update entity types**
   ```json
   {
     "types": {
       "YourType": {
         "entity_type": "your_entity_type"
       }
     }
   }
   ```

3. **Map fields to your properties**
   ```json
   {
     "fields": {
       "YourType.yourField": {
         "property": "properties.your_field",
         "type": "string"
       }
     }
   }
   ```

4. **Configure NATS subjects**
   ```json
   {
     "queries": {
       "yourQuery": {
         "resolver": "QueryEntityByID",
         "subject": "graph.your_domain.get"
       }
     }
   }
   ```

### Adding Custom Queries

For domain-specific analytics queries not covered by BaseResolver:

```json
{
  "queries": {
    "lowBatteryRobots": {
      "custom_query": true
    }
  }
}
```

Implement manually:
```go
func (r *queryResolver) LowBatteryRobots(ctx context.Context, threshold int) ([]*Robot, error) {
    // Custom logic
    return filterLowBattery(threshold)
}
```

## Testing Examples

### Unit Tests

```bash
go test ./generated/...
```

### Integration Tests

```bash
# Start test NATS
docker run -d --name test-nats -p 14222:4222 nats:latest -js

# Run integration tests
NATS_URL=nats://localhost:14222 go test ./... -tags=integration

# Cleanup
docker stop test-nats && docker rm test-nats
```

### E2E Tests

Use example queries from `queries.md` in your E2E test suite.

## Performance Tuning

### Batch Size

Adjust based on typical query patterns:

```json
{
  "dataloaders": {
    "Metric": {
      "batch_size": 200,  // High-frequency entities
      "wait": "5ms"
    },
    "Service": {
      "batch_size": 50,   // Lower-frequency entities
      "wait": "10ms"
    }
  }
}
```

### Cache TTL

Balance freshness vs performance:

```json
{
  "types": {
    "Service": {
      "cache_ttl": "30s"  // Slower-changing entities
    },
    "Metric": {
      "cache_ttl": "5s"   // Fast-changing entities
    }
  }
}
```

## Troubleshooting

### Generated Code Doesn't Compile

```bash
# Regenerate all
rm -rf generated/
semstreams-gqlgen -config=config.json
gqlgen generate
go build ./...
```

### Property Not Found

Check property path in config matches entity structure:

```json
// Entity: {Properties: {"metadata": {"version": "1.0"}}}
{
  "Robot.version": {
    "property": "properties.metadata.version",  // ✅ Correct path
    "type": "string"
  }
}
```

### N+1 Queries

Enable DataLoaders for relationship fields:

```json
{
  "dataloaders": {
    "Robot": {"batch_size": 50, "wait": "10ms"}
  }
}
```

## Next Steps

- [Setup Guide](../../components/GRAPHQL_GATEWAY_SETUP.md) - Step-by-step setup
- [Configuration Reference](../../components/GRAPHQL_GATEWAY_CONFIG.md) - Complete config options
- [Migration Guide](../../migration/GRAPHQL_MIGRATION.md) - Migrate existing GraphQL
- [Best Practices](../../guides/GRAPHQL_AGENTIC_WORKFLOWS.md) - LLM optimization

## Contributing

To add a new example:

1. Create directory: `docs/examples/graphql-gateway/your-domain/`
2. Add schema.graphql, config.json, queries.md
3. Update this README with your example
4. Test code generation: `semstreams-gqlgen -config=config.json`
5. Submit PR

---

**Questions?** [Open an issue](https://github.com/c360/semstreams/issues) or see [FAQ](../../guides/GRAPHQL_FAQ.md)
