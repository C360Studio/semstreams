# semstreams-gqlgen

Code generator for GraphQL Gateway Phase 2 - transforms GraphQL schema and configuration into resolver code.

## Overview

This tool generates GraphQL resolver code that integrates with the SemStreams GraphQL gateway infrastructure (`gateway/graphql`). It uses gqlgen for schema parsing and Go templates for code generation.

## Features

- Parses GraphQL schemas using gqlgen's parser
- Validates configuration against schema
- Generates three files:
  - `resolver.go` - Query resolvers implementing gqlgen interface
  - `models.go` - Entity-to-GraphQL type converters
  - `converters.go` - Property extraction helpers

## Installation

```bash
go install github.com/c360/semstreams/cmd/semstreams-gqlgen@latest
```

Or build from source:

```bash
cd cmd/semstreams-gqlgen
go build .
```

## Usage

```bash
semstreams-gqlgen -config=graphql-config.json -output=generated
```

### Flags

- `-config` - Path to configuration file (default: `graphql-config.json`)
- `-output` - Output directory for generated code (default: `generated`)
- `-schema` - Path to GraphQL schema (overrides config file)

## Configuration Format

Create a JSON configuration file that maps GraphQL types to NATS queries:

```json
{
  "package": "robotapi",
  "schema_path": "schema.graphql",
  "queries": {
    "robot": {
      "resolver": "QueryEntityByID",
      "subject": "graph.robot.get"
    },
    "robots": {
      "resolver": "QueryEntitiesByIDs",
      "subject": "graph.robot.list"
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
    "Robot.tasks": {
      "resolver": "QueryRelationships",
      "subject": "graph.robot.tasks"
    }
  },
  "types": {
    "Robot": {
      "entity_type": "robot"
    }
  }
}
```

### Configuration Fields

**Top Level:**
- `package` - Go package name for generated code
- `schema_path` - Path to GraphQL schema file
- `queries` - Query resolver mappings
- `fields` - Field resolver mappings
- `types` - Entity type mappings

**Query Config:**
- `resolver` - BaseResolver method name (`QueryEntityByID`, `QueryEntitiesByIDs`, `QueryRelationships`, `SemanticSearch`)
- `subject` - NATS subject for the query

**Field Config:**
- `property` - Property path in Entity (e.g., `"properties.name"`)
- `type` - Go type (`string`, `int`, `float64`, `bool`, `[]string`, `map[string]interface{}`)
- `resolver` - Optional: BaseResolver method for relationship fields
- `subject` - Optional: NATS subject for relationship queries

**Type Config:**
- `entity_type` - Entity type name in the graph

## Example

See `testdata/` for a complete Robot/Task example:

- `testdata/robot-schema.graphql` - GraphQL schema
- `testdata/robot-config.json` - Configuration
- `testdata/generated/` - Generated code

### Running the Example

```bash
cd cmd/semstreams-gqlgen
go run . -config=testdata/robot-config.json -output=testdata/generated
```

## GraphRAG Example

The code generator fully supports GraphRAG (Graph Retrieval-Augmented Generation) with community detection and hierarchical search:

**Schema Example:**
```graphql
type Community {
  id: ID!
  level: Int!
  members: [String!]!
  label: String
  summary: String
  keywords: [String!]
  repEntities: [String!]
}

type Query {
  # Local search within entity's community (fast, focused)
  localSearch(entityID: ID!, query: String!, level: Int!): [Entity!]!

  # Global search across community summaries (comprehensive)
  globalSearch(query: String!, level: Int!, maxCommunities: Int!): [Entity!]!

  # Get community by ID
  community(id: ID!): Community

  # Get entity's community at specific level
  entityCommunity(entityID: ID!, level: Int!): Community
}
```

**Config Example:**
```json
{
  "queries": {
    "localSearch": {
      "resolver": "LocalSearch",
      "subject": "graph.semmem.localsearch"
    },
    "globalSearch": {
      "resolver": "GlobalSearch",
      "subject": "graph.semmem.globalsearch"
    },
    "community": {
      "resolver": "GetCommunity",
      "subject": "graph.community.get"
    },
    "entityCommunity": {
      "resolver": "GetEntityCommunity",
      "subject": "graph.community.entity"
    }
  },
  "types": {
    "Community": {
      "entity_type": "org.semmem.community"
    }
  },
  "fields": {
    "Community.id": {"property": "id", "type": "string"},
    "Community.level": {"property": "level", "type": "int"},
    "Community.members": {"property": "members", "type": "[]string"},
    "Community.summary": {"property": "summary", "type": "string", "nullable": true},
    "Community.keywords": {"property": "keywords", "type": "[]string"}
  }
}
```

See SemMem (`/semmem`) for a complete working implementation with GraphRAG.

**Storage Backend:**
Generated GraphRAG resolvers query from NATS-based infrastructure (progressive enhancement):
- **BaseResolver.GetCommunity()** → NATS KV bucket `COMMUNITY_INDEX` (statistical + optional LLM summaries)
- **BaseResolver.LocalSearch()** → BM25 embeddings (default) or HTTP embedder (optional)
- **BaseResolver.GlobalSearch()** → Community summaries across hierarchy levels
- **Entity queries** → NATS KV bucket `ENTITY_STATES`

**Zero-Dependency Core:** Works offline with pure Go BM25 embeddings and statistical summaries. HTTP embedder and LLM enhancement are optional for improved accuracy when available.

Unlike typical GraphRAG stacks (requires Pinecone + Neo4j + S3 + LLM API), SemStreams uses NATS JetStream for all persistence, enabling edge deployment without mandatory external services.

## Generated Code Structure

### resolver.go

Implements query resolvers that delegate to `BaseResolver`:

```go
func (r *queryResolver) Robot(ctx context.Context, id string) (*Robot, error) {
    entity, err := r.base.QueryEntityByID(ctx, id)
    if err != nil {
        return nil, err
    }
    return entityRobot(entity)
}
```

### models.go

Converts generic `Entity` to domain-specific GraphQL types:

```go
func entityRobot(e *graphql.Entity) (*Robot, error) {
    if e == nil {
        return nil, nil
    }

    return &Robot{
        ID:       getID(e),
        Name:     getName(e),
        Status:   getStatus(e),
        Location: getLocation(e),
    }, nil
}
```

### converters.go

Property extraction helpers using `BaseResolver` property accessors:

```go
func getName(e *graphql.Entity) string {
    return graphql.GetStringProp(e, "properties.name")
}

func getID(e *graphql.Entity) string {
    return graphql.GetStringProp(e, "id")
}
```

## Supported BaseResolver Methods

### Entity Queries
- `QueryEntityByID(ctx, id)` - Single entity query by ID
- `QueryEntityByAlias(ctx, aliasOrID)` - Entity query by alias or ID
- `QueryEntitiesByIDs(ctx, ids)` - Batch entity query
- `QueryEntitiesByType(ctx, entityType, limit)` - Query entities by type

### Relationship Queries
- `QueryRelationships(ctx, filters)` - Relationship query

### Search Queries
- `SemanticSearch(ctx, query, limit)` - Semantic search

### GraphRAG Community Queries
- `LocalSearch(ctx, entityID, query, level)` - Search within entity's community (fast, focused)
- `GlobalSearch(ctx, query, level, maxCommunities)` - Search across community summaries (comprehensive)
- `GetCommunity(ctx, communityID)` - Get community by ID
- `GetEntityCommunity(ctx, entityID, level)` - Get entity's community at a specific hierarchical level

## Testing

```bash
go test ./cmd/semstreams-gqlgen/...
```

Tests include:
- Config loading and validation
- Schema parsing with gqlgen
- Config-schema validation
- Code generation
- Field path parsing

## Integration

Generated code integrates with:

- `gateway/graphql.BaseResolver` - Generic query infrastructure
- `gateway/graphql.Entity` - Generic entity structure
- `gateway/graphql.NATSClient` - NATS integration layer

## Implementation Details

**Schema Parsing:** Uses `github.com/vektah/gqlparser/v2` to parse GraphQL schemas

**Templates:** Go `text/template` for code generation

**Validation:**
- All referenced queries/types exist in schema
- Field paths are valid
- Resolver methods match BaseResolver API
- Type conversions are supported

**Error Handling:** Uses `github.com/c360/semstreams/errors` classification (Invalid/Fatal)

## Files

- `main.go` (25 lines) - CLI entry point
- `config.go` (156 lines) - Config loading and validation
- `schema.go` (192 lines) - gqlgen schema parsing
- `templates.go` (329 lines) - Code generation templates
- `generate.go` (271 lines) - Generation logic
- `generator_test.go` (525 lines) - Unit tests

**Total:** 1,498 lines of code

## Success Criteria

- CLI runs with standard flag package
- Generates 3 files: resolver.go, models.go, converters.go
- Generated code compiles without errors
- Config validation catches schema mismatches
- All unit tests pass
- No external CLI framework dependencies

## Next Steps (Phase 3)

- Relationship field resolution
- Custom scalar type support
- Mutation support
- Subscription support via NATS streams
