# GraphQL Gateway Configuration Reference

**Component**: GraphQL Gateway
**Config Type**: `graphql-config.json`
**Code Generator**: `semstreams-gqlgen`
**Version**: v0.8.0+

## Overview

This document provides a complete reference for GraphQL gateway configuration. The configuration file (`graphql-config.json`) defines how GraphQL types, queries, and fields map to NATS subjects and entity properties.

## Configuration Structure

```json
{
  "package": "string",              // Required: Go package name
  "schema_path": "string",          // Required: Path to schema.graphql
  "output_dir": "string",           // Optional: Output directory (default: generated)

  "queries": {                      // Required: Query resolver mappings
    "queryName": { /* QueryConfig */ }
  },

  "mutations": {                    // Optional: Mutation resolver mappings
    "mutationName": { /* MutationConfig */ }
  },

  "subscriptions": {                // Optional: Subscription mappings
    "subscriptionName": { /* SubscriptionConfig */ }
  },

  "types": {                        // Required: Type-to-entity mappings
    "TypeName": { /* TypeConfig */ }
  },

  "fields": {                       // Required: Field resolver mappings
    "TypeName.fieldName": { /* FieldConfig */ }
  },

  "error_mapping": {                // Optional: Error code translations
    "ERROR_CODE": "User-friendly message"
  },

  "dataloaders": {                  // Optional: DataLoader configuration
    "TypeName": { /* DataLoaderConfig */ }
  }
}
```

## Top-Level Fields

### `package` (string, required)

Go package name for generated code.

**Example:**
```json
{
  "package": "robotapi"
}
```

Generated files will use:
```go
package robotapi
```

### `schema_path` (string, required)

Path to GraphQL schema file (relative to config file).

**Example:**
```json
{
  "schema_path": "./schema/schema.graphql"
}
```

**Supported formats:**
- Relative paths: `./schema.graphql`, `../shared/schema.graphql`
- Absolute paths: `/path/to/schema.graphql`

### `output_dir` (string, optional)

Output directory for generated code. Default: `generated`

**Example:**
```json
{
  "output_dir": "internal/graphql/generated"
}
```

## Query Configuration

Maps GraphQL queries to BaseResolver methods and NATS subjects.

### QueryConfig Schema

```json
{
  "resolver": "string",    // Required: BaseResolver method name
  "subject": "string",     // Required: NATS subject pattern
  "timeout": "string",     // Optional: Request timeout (default: 30s)
  "cache_ttl": "string"    // Optional: Cache duration (default: none)
}
```

### Supported Resolvers

#### `QueryEntityByID`

Single entity query by ID.

**GraphQL Schema:**
```graphql
type Query {
  robot(id: ID!): Robot
}
```

**Config:**
```json
{
  "queries": {
    "robot": {
      "resolver": "QueryEntityByID",
      "subject": "graph.robot.get"
    }
  }
}
```

**NATS Request:**
```json
Subject: graph.robot.get
Payload: {"id": "robot-1"}
```

#### `QueryEntityByAlias`

Entity query by alias or ID (tries alias first, falls back to ID).

**GraphQL Schema:**
```graphql
type Query {
  specByAlias(aliasOrID: String!): Spec
}
```

**Config:**
```json
{
  "queries": {
    "specByAlias": {
      "resolver": "QueryEntityByAlias",
      "subject": "graph.spec.get"
    }
  }
}
```

#### `QueryEntitiesByIDs`

Batch entity query by multiple IDs.

**GraphQL Schema:**
```graphql
type Query {
  robots(ids: [ID!]!): [Robot!]!
}
```

**Config:**
```json
{
  "queries": {
    "robots": {
      "resolver": "QueryEntitiesByIDs",
      "subject": "graph.robot.list"
    }
  }
}
```

**NATS Request:**
```json
Subject: graph.robot.list
Payload: {"ids": ["robot-1", "robot-2", "robot-3"]}
```

#### `QueryEntitiesByType`

Query all entities of a specific type.

**GraphQL Schema:**
```graphql
type Query {
  allRobots(limit: Int): [Robot!]!
}
```

**Config:**
```json
{
  "queries": {
    "allRobots": {
      "resolver": "QueryEntitiesByType",
      "subject": "graph.robot.list",
      "entity_type": "robot"
    }
  }
}
```

#### `QueryRelationships`

Relationship traversal query.

**GraphQL Schema:**
```graphql
type Query {
  robotTasks(robotID: ID!, edgeType: String!): [Task!]!
}
```

**Config:**
```json
{
  "queries": {
    "robotTasks": {
      "resolver": "QueryRelationships",
      "subject": "graph.robot.relationships"
    }
  }
}
```

#### `SemanticSearch`

Full-text semantic search.

**GraphQL Schema:**
```graphql
type Query {
  search(query: String!, limit: Int): [Entity!]!
}
```

**Config:**
```json
{
  "queries": {
    "search": {
      "resolver": "SemanticSearch",
      "subject": "graph.query.semantic"
    }
  }
}
```

#### `LocalSearch` (GraphRAG)

Search within entity's community (fast, focused).

**GraphQL Schema:**
```graphql
type Query {
  localSearch(entityID: ID!, query: String!, level: Int!): [Entity!]!
}
```

**Config:**
```json
{
  "queries": {
    "localSearch": {
      "resolver": "LocalSearch",
      "subject": "graph.semmem.localsearch"
    }
  }
}
```

#### `GlobalSearch` (GraphRAG)

Search across community summaries (comprehensive).

**GraphQL Schema:**
```graphql
type Query {
  globalSearch(query: String!, level: Int!, maxCommunities: Int!): [Entity!]!
}
```

**Config:**
```json
{
  "queries": {
    "globalSearch": {
      "resolver": "GlobalSearch",
      "subject": "graph.semmem.globalsearch"
    }
  }
}
```

#### `GetCommunity` (GraphRAG)

Get community by ID.

**GraphQL Schema:**
```graphql
type Query {
  community(id: ID!): Community
}
```

**Config:**
```json
{
  "queries": {
    "community": {
      "resolver": "GetCommunity",
      "subject": "graph.community.get"
    }
  }
}
```

#### `GetEntityCommunity` (GraphRAG)

Get entity's community at specific hierarchical level.

**GraphQL Schema:**
```graphql
type Query {
  entityCommunity(entityID: ID!, level: Int!): Community
}
```

**Config:**
```json
{
  "queries": {
    "entityCommunity": {
      "resolver": "GetEntityCommunity",
      "subject": "graph.community.entity"
    }
  }
}
```

## Type Configuration

Maps GraphQL types to entity types in the graph.

### TypeConfig Schema

```json
{
  "entity_type": "string",     // Required: Entity type name
  "id_field": "string",        // Optional: ID field name (default: "id")
  "cache_ttl": "string"        // Optional: Cache duration
}
```

### Example

**GraphQL Schema:**
```graphql
type Robot {
  id: ID!
  name: String!
}

type Task {
  id: ID!
  title: String!
}
```

**Config:**
```json
{
  "types": {
    "Robot": {
      "entity_type": "robot",
      "id_field": "id"
    },
    "Task": {
      "entity_type": "task",
      "id_field": "id"
    }
  }
}
```

## Field Configuration

Maps GraphQL fields to entity properties or resolvers.

### FieldConfig Schema

**Simple Property Mapping:**
```json
{
  "property": "string",        // Required: Property path in Entity
  "type": "string",            // Required: Go type
  "nullable": boolean,         // Optional: Allow null (default: false)
  "default": any               // Optional: Default value if missing
}
```

**Relationship Mapping:**
```json
{
  "resolver": "string",        // Required: BaseResolver method
  "subject": "string",         // Required: NATS subject
  "edge_type": "string",       // Optional: Edge type filter
  "direction": "string"        // Optional: "outgoing"|"incoming"|"both"
}
```

**Custom Resolver:**
```json
{
  "custom_resolver": true      // Requires manual implementation
}
```

### Property Paths

Property paths use dot notation to navigate Entity structure:

```go
type Entity struct {
    ID         string
    Type       string
    Properties map[string]interface{}
    UpdatedAt  time.Time
}
```

**Path Examples:**
- `id` → Entity.ID
- `type` → Entity.Type
- `properties.name` → Entity.Properties["name"]
- `properties.metadata.version` → Entity.Properties["metadata"]["version"]
- `updatedAt` → Entity.UpdatedAt

### Supported Types

| Go Type | GraphQL Type | Example |
|---------|-------------|---------|
| `string` | String | "hello" |
| `int` | Int | 42 |
| `float64` | Float | 3.14 |
| `bool` | Boolean | true |
| `[]string` | [String!] | ["a", "b"] |
| `[]int` | [Int!] | [1, 2, 3] |
| `map[string]interface{}` | JSON | {"key": "value"} |
| `time.Time` | String (ISO8601) | "2024-01-01T00:00:00Z" |

### Field Examples

**Simple String Field:**
```json
{
  "fields": {
    "Robot.name": {
      "property": "properties.name",
      "type": "string"
    }
  }
}
```

**Nullable String Field:**
```json
{
  "fields": {
    "Robot.location": {
      "property": "properties.location",
      "type": "string",
      "nullable": true
    }
  }
}
```

**Integer Field with Default:**
```json
{
  "fields": {
    "Robot.batteryLevel": {
      "property": "properties.battery",
      "type": "int",
      "default": 0
    }
  }
}
```

**Array Field:**
```json
{
  "fields": {
    "Robot.tags": {
      "property": "properties.tags",
      "type": "[]string"
    }
  }
}
```

**Nested Property:**
```json
{
  "fields": {
    "Robot.version": {
      "property": "properties.metadata.version",
      "type": "string"
    }
  }
}
```

**Relationship Field:**
```json
{
  "fields": {
    "Robot.tasks": {
      "resolver": "QueryRelationships",
      "subject": "graph.robot.tasks",
      "edge_type": "assigned_to",
      "direction": "outgoing"
    }
  }
}
```

**Reverse Relationship:**
```json
{
  "fields": {
    "Task.assignedTo": {
      "resolver": "QueryRelationships",
      "subject": "graph.task.robot",
      "edge_type": "assigned_to",
      "direction": "incoming"
    }
  }
}
```

**Custom Resolver:**
```json
{
  "fields": {
    "Robot.healthScore": {
      "custom_resolver": true
    }
  }
}
```

Requires manual implementation:
```go
func (r *robotResolver) HealthScore(ctx context.Context, obj *Robot) (float64, error) {
    // Custom logic
    return calculateHealthScore(obj), nil
}
```

## Subscription Configuration

Real-time updates via WebSocket subscriptions.

### SubscriptionConfig Schema

```json
{
  "subject": "string",         // Required: NATS subject pattern
  "entity_type": "string",     // Optional: Entity type filter
  "stream": "string"           // Optional: JetStream stream name
}
```

### Example

**GraphQL Schema:**
```graphql
type Subscription {
  robotStatusChanged(robotID: ID!): Robot!
  taskUpdates: Task!
}
```

**Config:**
```json
{
  "subscriptions": {
    "robotStatusChanged": {
      "subject": "robot.status.changed.{robotID}",
      "entity_type": "robot"
    },
    "taskUpdates": {
      "subject": "task.updates.>",
      "entity_type": "task",
      "stream": "TASK_EVENTS"
    }
  }
}
```

**Subject Patterns:**
- `{robotID}` - Parameter substitution
- `>` - Multi-token wildcard
- `*` - Single-token wildcard

## Error Mapping

Map NATS error codes to user-friendly messages.

### Example

```json
{
  "error_mapping": {
    "NOT_FOUND": "The requested entity does not exist",
    "INVALID_INPUT": "Invalid query parameters provided",
    "TIMEOUT": "Request timeout - please try again",
    "PERMISSION_DENIED": "You do not have permission to access this resource",
    "INTERNAL_ERROR": "An internal error occurred"
  }
}
```

Generated error handling:
```go
if err != nil {
    if msg, ok := errorMapping[code]; ok {
        return nil, fmt.Errorf("%s: %w", msg, err)
    }
    return nil, err
}
```

## DataLoader Configuration

Optimize N+1 queries with batching.

### DataLoaderConfig Schema

```json
{
  "batch_size": number,        // Max batch size (default: 100)
  "wait": "string",            // Wait before batching (default: 16ms)
  "cache": boolean             // Enable caching (default: true)
}
```

### Example

```json
{
  "dataloaders": {
    "Robot": {
      "batch_size": 50,
      "wait": "10ms",
      "cache": true
    },
    "Task": {
      "batch_size": 100,
      "wait": "16ms",
      "cache": true
    }
  }
}
```

Generated DataLoader:
```go
type RobotLoader struct {
    maxBatch int
    wait     time.Duration
    fetch    func(ctx context.Context, keys []string) ([]*Robot, []error)
}
```

## Complete Example

**schema.graphql:**
```graphql
type Robot {
  id: ID!
  name: String!
  status: String!
  location: String
  batteryLevel: Int!
  tasks: [Task!]
  healthScore: Float!
}

type Task {
  id: ID!
  title: String!
  priority: Int!
  completed: Boolean!
  assignedTo: Robot
}

type Community {
  id: ID!
  level: Int!
  members: [String!]!
  keywords: [String!]
}

type Query {
  robot(id: ID!): Robot
  robots(ids: [ID!]!): [Robot!]!
  allRobots(limit: Int): [Robot!]!
  task(id: ID!): Task
  search(query: String!, limit: Int): [Entity!]!
  localSearch(entityID: ID!, query: String!, level: Int!): [Entity!]!
  community(id: ID!): Community
}

type Subscription {
  robotStatusChanged(robotID: ID!): Robot!
}
```

**graphql-config.json:**
```json
{
  "package": "robotapi",
  "schema_path": "schema/schema.graphql",
  "output_dir": "generated",

  "queries": {
    "robot": {
      "resolver": "QueryEntityByID",
      "subject": "graph.robot.get",
      "timeout": "5s"
    },
    "robots": {
      "resolver": "QueryEntitiesByIDs",
      "subject": "graph.robot.list"
    },
    "allRobots": {
      "resolver": "QueryEntitiesByType",
      "subject": "graph.robot.list",
      "entity_type": "robot"
    },
    "task": {
      "resolver": "QueryEntityByID",
      "subject": "graph.task.get"
    },
    "search": {
      "resolver": "SemanticSearch",
      "subject": "graph.query.semantic"
    },
    "localSearch": {
      "resolver": "LocalSearch",
      "subject": "graph.query.localsearch"
    },
    "community": {
      "resolver": "GetCommunity",
      "subject": "graph.community.get"
    }
  },

  "subscriptions": {
    "robotStatusChanged": {
      "subject": "robot.status.changed.{robotID}",
      "entity_type": "robot"
    }
  },

  "types": {
    "Robot": {
      "entity_type": "robot",
      "id_field": "id"
    },
    "Task": {
      "entity_type": "task",
      "id_field": "id"
    },
    "Community": {
      "entity_type": "org.semmem.community"
    }
  },

  "fields": {
    "Robot.id": {
      "property": "id",
      "type": "string"
    },
    "Robot.name": {
      "property": "properties.name",
      "type": "string"
    },
    "Robot.status": {
      "property": "properties.status",
      "type": "string"
    },
    "Robot.location": {
      "property": "properties.location",
      "type": "string",
      "nullable": true
    },
    "Robot.batteryLevel": {
      "property": "properties.battery",
      "type": "int",
      "default": 0
    },
    "Robot.tasks": {
      "resolver": "QueryRelationships",
      "subject": "graph.robot.tasks",
      "edge_type": "assigned_to"
    },
    "Robot.healthScore": {
      "custom_resolver": true
    },

    "Task.id": {
      "property": "id",
      "type": "string"
    },
    "Task.title": {
      "property": "properties.title",
      "type": "string"
    },
    "Task.priority": {
      "property": "properties.priority",
      "type": "int"
    },
    "Task.completed": {
      "property": "properties.completed",
      "type": "bool",
      "default": false
    },
    "Task.assignedTo": {
      "resolver": "QueryRelationships",
      "subject": "graph.task.robot",
      "edge_type": "assigned_to",
      "direction": "incoming"
    },

    "Community.id": {
      "property": "id",
      "type": "string"
    },
    "Community.level": {
      "property": "level",
      "type": "int"
    },
    "Community.members": {
      "property": "members",
      "type": "[]string"
    },
    "Community.keywords": {
      "property": "keywords",
      "type": "[]string"
    }
  },

  "error_mapping": {
    "NOT_FOUND": "Entity not found",
    "INVALID_INPUT": "Invalid parameters",
    "TIMEOUT": "Request timeout"
  },

  "dataloaders": {
    "Robot": {
      "batch_size": 50,
      "wait": "10ms"
    },
    "Task": {
      "batch_size": 100,
      "wait": "16ms"
    }
  }
}
```

## Validation Rules

The code generator validates configuration:

1. **Schema exists:** `schema_path` must point to valid GraphQL schema
2. **Query exists:** All queries in config must exist in schema
3. **Type exists:** All types in config must exist in schema
4. **Field exists:** All fields in config must exist on their types
5. **Resolver valid:** Resolver names must match BaseResolver methods
6. **Type compatibility:** Go types must be compatible with GraphQL types
7. **Required fields:** All required config fields must be present

**Validation errors halt generation:**
```
Error: Query 'invalidQuery' not found in schema
Error: Field 'Robot.unknownField' does not exist
Error: Invalid resolver 'BadResolver' for query 'robot'
```

## Best Practices

### Naming Conventions

**NATS Subjects:**
- Use hierarchical structure: `graph.{domain}.{operation}`
- Examples: `graph.robot.get`, `graph.query.semantic`

**Entity Types:**
- Lowercase with dots: `robot`, `org.semmem.spec`
- Match your domain model

**Field Names:**
- camelCase in GraphQL schema
- snake_case in entity properties
- Config maps between them

### Performance

**Use DataLoaders:**
```json
{
  "dataloaders": {
    "Robot": {"batch_size": 50, "wait": "10ms"}
  }
}
```

**Set Timeouts:**
```json
{
  "queries": {
    "search": {
      "resolver": "SemanticSearch",
      "timeout": "5s"
    }
  }
}
```

**Cache Frequently Accessed:**
```json
{
  "types": {
    "Robot": {
      "cache_ttl": "5m"
    }
  }
}
```

### Security

**Disable Introspection in Production:**
```go
srv := handler.NewDefaultServer(schema)
srv.Use(extension.Introspection{})  // Only in dev
```

**Add Authentication:**
```go
srv.AroundResponses(authMiddleware)
```

**Limit Complexity:**
```go
srv.Use(extension.FixedComplexityLimit(200))
```

## Troubleshooting

**Issue:** "Query 'robot' not found in schema"

**Solution:** Ensure query exists in schema.graphql:
```graphql
type Query {
  robot(id: ID!): Robot
}
```

**Issue:** "Invalid type 'Robot' for field"

**Solution:** Add type mapping:
```json
{
  "types": {
    "Robot": {"entity_type": "robot"}
  }
}
```

**Issue:** "Property path 'properties.name' returns null"

**Solution:** Verify entity has the property in NATS KV. Check property path matches entity structure.

## Reference

- [Setup Guide](GRAPHQL_GATEWAY_SETUP.md) - Step-by-step setup
- [Migration Guide](../migration/GRAPHQL_MIGRATION.md) - Migrate existing GraphQL
- [Best Practices](../guides/GRAPHQL_AGENTIC_WORKFLOWS.md) - LLM optimization
- [Code Generator](../../semstreams/cmd/semstreams-gqlgen/README.md) - semstreams-gqlgen docs

---

**Questions?** See [FAQ](../guides/GRAPHQL_FAQ.md) or [open an issue](https://github.com/c360/semstreams/issues).
