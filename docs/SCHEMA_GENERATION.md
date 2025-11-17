# Build-Time Schema Generation System

## Overview

SemStreams uses a build-time schema generation system that automatically creates JSON Schemas and OpenAPI specifications from Go struct tags. This ensures component configurations, API contracts, and TypeScript types stay perfectly synchronized throughout the development lifecycle.

**Key Benefits**:
- ✅ Schemas always match code
- ✅ Compile-time validation
- ✅ Automatic synchronization
- ✅ Type-safe frontend-backend communication
- ✅ Contract testing without running services
- ✅ Single source of truth

## System Architecture

### Generation Flow

```
┌─────────────────────────────────────────────────────────────────┐
│  Component Implementation                                        │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │ type Config struct {                                      │  │
│  │   Port int `schema:"type:int,desc:Port,default:8080"`    │  │
│  │ }                                                         │  │
│  │                                                           │  │
│  │ func (c *Component) ConfigSchema() ConfigSchema {        │  │
│  │   return ExtractSchema(Config{})                         │  │
│  │ }                                                         │  │
│  └───────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────┐
│  Component Registration                                          │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │ registry.RegisterFactory("component", &Registration{     │  │
│  │   Description: "Component description",                  │  │
│  │   Type:        "input",                                  │  │
│  │   Schema:      Config{},                                 │  │
│  │ })                                                       │  │
│  └───────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────┐
│  Schema Generation (Build Time)                                  │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │ $ task schema:generate                                   │  │
│  │                                                           │  │
│  │ cmd/schema-exporter/main.go                              │  │
│  │   ├── Extract schemas from registry                      │  │
│  │   ├── Validate against meta-schema                       │  │
│  │   ├── Generate JSON schemas                              │  │
│  │   └── Generate OpenAPI spec                              │  │
│  └───────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────┐
│  Generated Artifacts                                             │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │ schemas/component.v1.json     (JSON Schema Draft-07)    │  │
│  │ specs/openapi.v3.yaml         (OpenAPI 3.0.3)           │  │
│  └───────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────┐
│  TypeScript Type Generation                                      │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │ $ task generate-types                                    │  │
│  │                                                           │  │
│  │ npx openapi-typescript specs/openapi.v3.yaml             │  │
│  │   -o src/lib/types/api.generated.ts                      │  │
│  └───────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────┐
│  Contract Validation (CI/CD)                                     │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │ go test ./test/contract      (Backend)                   │  │
│  │ npm test -- src/lib/contract (Frontend)                  │  │
│  │                                                           │  │
│  │ Validates:                                               │  │
│  │  ✓ Committed schemas match code                          │  │
│  │  ✓ OpenAPI spec is valid                                 │  │
│  │  ✓ TypeScript types are current                          │  │
│  │  ✓ Cross-repo compatibility                              │  │
│  └───────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

## Quick Start

### Creating a New Component with Schema

**Step 1**: Create component with schema tags:

```go
// output/mycomponent/mycomponent.go
package mycomponent

import (
    "context"

    "github.com/c360/semstreams/component"
    "github.com/c360/semstreams/message"
)

// Config defines component configuration
type Config struct {
    URL     string `schema:"type:string,desc:Target endpoint URL"`
    Timeout int    `schema:"type:int,desc:Request timeout in seconds,min:1,max:300,default:30"`
    Retries int    `schema:"type:int,desc:Retry attempts,min:0,max:10,default:3,category:advanced"`
}

// MyComponent implementation
type MyComponent struct {
    config Config
}

// ConfigSchema returns the schema for this component
func (m *MyComponent) ConfigSchema() component.ConfigSchema {
    return component.ExtractSchema(Config{})
}

// Initialize configures the component
func (m *MyComponent) Initialize(ctx context.Context, config interface{}) error {
    cfg, ok := config.(map[string]interface{})
    if !ok {
        return fmt.Errorf("invalid config type")
    }

    // Use config values
    m.config.URL = cfg["url"].(string)
    m.config.Timeout = int(cfg["timeout"].(float64))
    m.config.Retries = int(cfg["retries"].(float64))

    return nil
}

// Process implements message processing
func (m *MyComponent) Process(ctx context.Context, msg message.Message) error {
    // Implementation
    return nil
}
```

**Step 2**: Register component:

```go
// componentregistry/register.go
package componentregistry

import (
    "github.com/c360/semstreams/component"
    "github.com/c360/semstreams/output/mycomponent"
)

func Register(registry *component.Registry) error {
    registry.RegisterFactory("my-component", &component.Registration{
        Description: "My custom output component",
        Type:        "output",
        Protocol:    "http",
        Domain:      "network",
        Version:     "1.0.0",
        Factory: func() (component.Component, error) {
            return &mycomponent.MyComponent{}, nil
        },
        Schema: mycomponent.Config{}, // Config struct with schema tags
    })

    return nil
}
```

**Step 3**: Generate schemas:

```bash
task schema:generate
```

**Output**:
```
Schema Exporter
  Registry: ./componentregistry
  Output dir: ./schemas
  OpenAPI spec: ./specs/openapi.v3.yaml
Found 12 component types
Using meta-schema: specs/component-schema-meta.json
  ✓ Generated: schemas/my-component.v1.json
  ✓ Generated OpenAPI spec: specs/openapi.v3.yaml
✅ Schema generation complete!
```

**Step 4**: Verify generated schema:

```bash
cat schemas/my-component.v1.json
```

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "$id": "my-component.v1.json",
  "type": "object",
  "title": "my-component Configuration",
  "description": "My custom output component",
  "properties": {
    "url": {
      "type": "string",
      "description": "Target endpoint URL"
    },
    "timeout": {
      "type": "number",
      "description": "Request timeout in seconds",
      "minimum": 1,
      "maximum": 300,
      "default": 30
    },
    "retries": {
      "type": "number",
      "description": "Retry attempts",
      "minimum": 0,
      "maximum": 10,
      "default": 3,
      "category": "advanced"
    }
  },
  "required": ["url"],
  "x-component-metadata": {
    "name": "my-component",
    "type": "output",
    "protocol": "http",
    "domain": "network",
    "version": "1.0.0"
  }
}
```

**Step 5**: Regenerate frontend types:

```bash
cd semstreams-ui
task generate-types
```

**Step 6**: Run contract tests:

```bash
# Backend tests
cd semstreams
go test ./test/contract -v

# Frontend tests
cd semstreams-ui
npm test -- src/lib/contract
```

**Step 7**: Commit everything:

```bash
git add output/mycomponent/
git add componentregistry/register.go
git add schemas/my-component.v1.json
git add specs/openapi.v3.yaml
git commit -m "feat(my-component): add new output component"
```

## Schema Tag Reference

### Basic Tags

#### type - Field Type (Required)

Specifies the JSON Schema type.

**Values**: `string`, `int`, `float`, `bool`, `array`, `object`, `enum`, `ports`, `cache`

**Examples**:
```go
Name     string  `schema:"type:string,desc:Component name"`
Count    int     `schema:"type:int,desc:Item count"`
Enabled  bool    `schema:"type:bool,desc:Enable feature"`
Tags     []string `schema:"type:array,desc:Tag list"`
```

#### desc - Description (Required)

Human-readable field description.

**Best Practice**: Explain purpose and include units if applicable.

**Examples**:
```go
// ❌ Bad - just repeats field name
Timeout int `schema:"type:int,desc:Timeout"`

// ✅ Good - explains purpose and units
Timeout int `schema:"type:int,desc:Connection timeout in seconds"`
```

### Optional Tags

#### default - Default Value

Specifies default value when field is omitted.

**Note**: Fields without defaults are automatically required.

**Examples**:
```go
Port    int    `schema:"type:int,desc:Server port,default:8080"`
Host    string `schema:"type:string,desc:Hostname,default:localhost"`
Enabled bool   `schema:"type:bool,desc:Enable debug,default:false"`
```

#### min / max - Numeric Constraints

Minimum and maximum values for numeric fields.

**Examples**:
```go
Port       int `schema:"type:int,desc:Port,min:1,max:65535"`
Threads    int `schema:"type:int,desc:Workers,min:1,max:100"`
Percentage int `schema:"type:int,desc:Rate,min:0,max:100"`
```

#### enum - Enumerated Values

Restricts field to specific allowed values.

**Syntax**: `enum:value1|value2|value3` (pipe-separated)

**Examples**:
```go
LogLevel string `schema:"type:enum,desc:Log level,enum:debug|info|warn|error"`
Format   string `schema:"type:enum,desc:Format,enum:json|yaml|xml"`
Method   string `schema:"type:enum,desc:HTTP method,enum:GET|POST|PUT|DELETE"`
```

#### category - UI Organization

Groups fields for UI presentation.

**Values**: `basic` (default, always visible), `advanced` (collapsed by default)

**Examples**:
```go
type Config struct {
    // Basic settings (always visible)
    Host string `schema:"type:string,desc:Server host,category:basic"`
    Port int    `schema:"type:int,desc:Server port,category:basic"`

    // Advanced settings (collapsed)
    Timeout    int  `schema:"type:int,desc:Timeout,category:advanced,default:30"`
    MaxRetries int  `schema:"type:int,desc:Retries,category:advanced,default:3"`
    Debug      bool `schema:"type:bool,desc:Debug mode,category:advanced,default:false"`
}
```

## Complete Examples

### Input Component (UDP)

```go
package udp

type Config struct {
    Port    int    `schema:"type:int,desc:UDP port to listen on,min:1,max:65535,default:14550"`
    Address string `schema:"type:string,desc:Bind address,default:0.0.0.0"`
    Buffer  int    `schema:"type:int,desc:Buffer size in bytes,min:1024,default:8192,category:advanced"`
}

func (u *UDP) ConfigSchema() component.ConfigSchema {
    return component.ExtractSchema(Config{})
}
```

**Registration**:
```go
registry.RegisterFactory("udp", &component.Registration{
    Description: "UDP input component for receiving MAVLink and other UDP data",
    Type:        "input",
    Protocol:    "udp",
    Domain:      "network",
    Version:     "1.0.0",
    Factory:     func() (component.Component, error) { return &udp.UDP{}, nil },
    Schema:      udp.Config{},
})
```

**Generated Schema**: `schemas/udp.v1.json`

### Processor Component (Graph Processor)

```go
package graph

type ProcessorConfig struct {
    Ports component.Ports `schema:"type:ports,desc:Input and output ports for message flow"`
    Cache component.Cache `schema:"type:cache,desc:Entity state storage"`

    BatchSize    int  `schema:"type:int,desc:Batch size for bulk operations,min:1,max:1000,default:100"`
    EnableIndex  bool `schema:"type:bool,desc:Enable semantic indexing,default:true"`
    IndexModel   string `schema:"type:string,desc:Embedding model name,default:all-MiniLM-L6-v2,category:advanced"`
}

func (g *GraphProcessor) ConfigSchema() component.ConfigSchema {
    return component.ExtractSchema(ProcessorConfig{})
}
```

**Registration**:
```go
registry.RegisterFactory("graph-processor", &component.Registration{
    Description: "Semantic graph processor with entity state management",
    Type:        "processor",
    Protocol:    "nats",
    Domain:      "semantic",
    Version:     "1.0.0",
    Factory:     func() (component.Component, error) { return &graph.GraphProcessor{}, nil },
    Schema:      graph.ProcessorConfig{},
})
```

### Output Component (HTTP POST)

```go
package httppost

type Config struct {
    URL         string `schema:"type:string,desc:HTTP endpoint URL"`
    Method      string `schema:"type:enum,desc:HTTP method,enum:POST|PUT|PATCH,default:POST"`
    Timeout     int    `schema:"type:int,desc:Request timeout in seconds,min:1,max:300,default:30"`
    Retries     int    `schema:"type:int,desc:Number of retry attempts,min:0,max:10,default:3"`
    ContentType string `schema:"type:string,desc:Content-Type header,default:application/json,category:advanced"`
}

func (h *HTTPPost) ConfigSchema() component.ConfigSchema {
    return component.ExtractSchema(Config{})
}
```

**Registration**:
```go
registry.RegisterFactory("httppost", &component.Registration{
    Description: "HTTP POST output for sending messages to HTTP endpoints",
    Type:        "output",
    Protocol:    "http",
    Domain:      "network",
    Version:     "1.0.0",
    Factory:     func() (component.Component, error) { return &httppost.HTTPPost{}, nil },
    Schema:      httppost.Config{},
})
```

### Storage Component (Object Store)

```go
package objectstore

type Config struct {
    Bucket      string `schema:"type:string,desc:NATS KV bucket name"`
    TTL         int    `schema:"type:int,desc:Object TTL in seconds,min:0,default:0,category:advanced"`
    Replicas    int    `schema:"type:int,desc:Number of replicas,min:1,max:5,default:3,category:advanced"`
    Compression bool   `schema:"type:bool,desc:Enable compression,default:true,category:advanced"`
}

func (o *ObjectStore) ConfigSchema() component.ConfigSchema {
    return component.ExtractSchema(Config{})
}
```

## Component Patterns

### Connection Configuration

```go
type ConnectionConfig struct {
    Host           string `schema:"type:string,desc:Server hostname,default:localhost"`
    Port           int    `schema:"type:int,desc:Server port,min:1,max:65535,default:8080"`
    Timeout        int    `schema:"type:int,desc:Connection timeout in seconds,min:1,default:30,category:advanced"`
    MaxRetries     int    `schema:"type:int,desc:Maximum retry attempts,min:0,max:10,default:3,category:advanced"`
    RetryDelay     int    `schema:"type:int,desc:Delay between retries in ms,min:100,default:1000,category:advanced"`
}
```

### Feature Flags

```go
type FeatureConfig struct {
    EnableMetrics    bool `schema:"type:bool,desc:Collect performance metrics,default:true"`
    EnableTracing    bool `schema:"type:bool,desc:Enable distributed tracing,default:false,category:advanced"`
    EnableValidation bool `schema:"type:bool,desc:Validate input messages,default:true"`
}
```

### Resource Limits

```go
type ResourceConfig struct {
    MaxMemory     int `schema:"type:int,desc:Maximum memory usage in MB,min:100,max:10000,default:1024"`
    MaxCPU        int `schema:"type:int,desc:CPU limit as percentage,min:1,max:100,default:80"`
    WorkerThreads int `schema:"type:int,desc:Worker thread pool size,min:1,max:100,default:4"`
    QueueSize     int `schema:"type:int,desc:Message queue capacity,min:100,max:100000,default:1000"`
}
```

### Nested Configuration

```go
type ServiceConfig struct {
    Connection ConnectionConfig `schema:"type:object,desc:Connection settings"`
    Features   FeatureConfig    `schema:"type:object,desc:Feature flags"`
    Resources  ResourceConfig   `schema:"type:object,desc:Resource limits"`
}

type ConnectionConfig struct {
    Host string `schema:"type:string,desc:Server host"`
    Port int    `schema:"type:int,desc:Server port"`
}

type FeatureConfig struct {
    EnableMetrics bool `schema:"type:bool,desc:Enable metrics"`
}

type ResourceConfig struct {
    MaxMemory int `schema:"type:int,desc:Max memory in MB"`
}
```

## Development Workflow

### Daily Development

1. **Make code changes**:
   ```go
   type Config struct {
       NewField string `schema:"type:string,desc:New feature,default:value"`
   }
   ```

2. **Regenerate schemas**:
   ```bash
   task schema:generate
   ```

3. **Run contract tests**:
   ```bash
   go test ./test/contract -v
   ```

4. **Regenerate frontend types**:
   ```bash
   cd semstreams-ui && task generate-types
   ```

5. **Commit all changes**:
   ```bash
   git add .
   git commit -m "feat: add new configuration field"
   ```

### CI/CD Pipeline

**Backend** (`.github/workflows/backend.yml`):
```yaml
schema-validation:
  - name: Generate schemas
    run: task schema:generate

  - name: Check for uncommitted schema changes
    run: |
      git diff --exit-code schemas/ specs/openapi.v3.yaml

  - name: Run contract tests
    run: go test ./test/contract -v
```

**Frontend** (`.github/workflows/frontend.yml`):
```yaml
type-validation:
  - name: Generate TypeScript types
    run: task generate-types

  - name: Check for uncommitted type changes
    run: |
      git diff --exit-code src/lib/types/api.generated.ts

  - name: Run contract tests
    run: npm test -- src/lib/contract
```

## Versioning Strategy

### Schema Versioning

**Filename Convention**: `{component}.v{MAJOR}.json`

**Examples**:
- `udp.v1.json` - Current version
- `udp.v2.json` - Next major version

### Backward Compatible Changes (MINOR)

Adding optional fields is backward compatible:

```go
// v1.0
type Config struct {
    Port int `schema:"type:int,desc:Port,default:8080"`
}

// v1.1 - Add optional field (backward compatible)
type Config struct {
    Port    int `schema:"type:int,desc:Port,default:8080"`
    Timeout int `schema:"type:int,desc:Timeout,default:30"` // NEW - has default
}
```

**Impact**: ✅ Old configs still valid

### Breaking Changes (MAJOR)

Removing fields or changing types requires major version:

```go
// v1
type Config struct {
    Port       int    `schema:"type:int,desc:Port"`
    OldField   string `schema:"type:string,desc:Old field"`
}

// v2 - Remove field (BREAKING)
type Config struct {
    Port int `schema:"type:int,desc:Port"`
    // OldField removed - requires v2
}
```

**Impact**: ❌ Old configs invalid

**Migration Path**:
1. v1.x: Deprecate field (keep both old and new)
2. Grace period (3 months or 2 minor releases)
3. v2.0: Remove deprecated field

See [SCHEMA_VERSIONING.md](./SCHEMA_VERSIONING.md) for complete versioning strategy.

## Contract Testing

### Backend Tests

**Schema-Code Synchronization** (`test/contract/schema_contract_test.go`):

```go
func TestCommittedSchemasMatchCode(t *testing.T) {
    // Load committed schemas
    committedSchemas := loadCommittedSchemas("./schemas")

    // Generate schemas from code
    registry := component.NewRegistry()
    componentregistry.Register(registry)
    generatedSchemas := extractSchemas(registry)

    // Compare
    for name, committed := range committedSchemas {
        generated := generatedSchemas[name]

        if diff := cmp.Diff(committed, generated); diff != "" {
            t.Errorf("Schema mismatch for %s:\n%s", name, diff)
        }
    }
}
```

**OpenAPI Spec Validation** (`test/contract/openapi_contract_test.go`):

```go
func TestOpenAPISpecContainsAllComponents(t *testing.T) {
    spec := loadOpenAPISpec("./specs/openapi.v3.yaml")

    registry := component.NewRegistry()
    componentregistry.Register(registry)
    factories := registry.ListFactories()

    for name := range factories {
        if !specContainsComponent(spec, name) {
            t.Errorf("Component %s not in OpenAPI spec", name)
        }
    }
}
```

### Frontend Tests

**OpenAPI Contract** (`src/lib/contract/openapi.contract.test.ts`):

```typescript
describe('OpenAPI Specification Contract Tests', () => {
  it('should load and parse the committed OpenAPI spec', () => {
    const yamlContent = readFileSync(OPENAPI_SPEC_PATH, 'utf8');
    const spec = YAML.parse(yamlContent);

    expect(spec.openapi).toBe('3.0.3');
    expect(spec.info.title).toBe('SemStreams Component API');
  });

  it('should have ComponentType schema with references', () => {
    const componentType = spec.components.schemas.ComponentType;
    expect(componentType.properties.schema.oneOf).toBeDefined();
  });
});
```

See [CONTRACT_TESTING.md](./CONTRACT_TESTING.md) for complete testing strategy.

## Troubleshooting

### Schema Not Generated

**Problem**: Component schema not appearing in `schemas/` directory

**Solution**:
1. Ensure `ConfigSchema()` method is implemented
2. Check component is registered in `componentregistry/register.go`
3. Verify schema tags are valid
4. Run `task schema:generate` with verbose output

### Schema Validation Errors

**Problem**: `task schema:generate` fails with validation errors

**Common Causes**:
```go
// ❌ Missing required tag
Field string `schema:"type:string"` // Missing desc

// ❌ Invalid enum syntax
Level string `schema:"type:enum,enum:[a,b,c]"` // Use | not []

// ❌ Invalid constraint
Port int `schema:"type:int,min:100,max:10"` // min > max

// ✅ Correct
Field string `schema:"type:string,desc:Description"`
Level string `schema:"type:enum,desc:Level,enum:a|b|c"`
Port int `schema:"type:int,desc:Port,min:10,max:100"`
```

### Contract Tests Failing

**Problem**: Contract tests fail after schema changes

**Solution**:
```bash
# Regenerate everything
task schema:generate

# Verify git status
git status schemas/ specs/

# If schemas changed, commit them
git add schemas/ specs/
git commit -m "chore: regenerate schemas"

# Run tests again
go test ./test/contract -v
```

### Type Generation Issues

**Problem**: Frontend types don't match backend schemas

**Solution**:
```bash
# Ensure backend schemas are up to date
cd semstreams
task schema:generate

# Regenerate frontend types
cd ../semstreams-ui
task generate-types

# Verify types
cat src/lib/types/api.generated.ts

# Run tests
npm test -- src/lib/contract
```

## Best Practices

### 1. Always Provide Descriptions

```go
// ❌ Bad
Port int `schema:"type:int"`

// ✅ Good
Port int `schema:"type:int,desc:Server port for incoming connections"`
```

### 2. Use Sensible Defaults

```go
// ✅ Good - users rarely need to change this
Timeout int `schema:"type:int,desc:Connection timeout in seconds,default:30"`

// ✅ Good - common use case
LogLevel string `schema:"type:enum,desc:Logging verbosity,enum:debug|info|warn|error,default:info"`
```

### 3. Add Constraints for Validation

```go
// ✅ Good - prevents invalid port numbers
Port int `schema:"type:int,desc:Server port,min:1,max:65535"`

// ✅ Good - reasonable thread count
Workers int `schema:"type:int,desc:Worker thread count,min:1,max:100,default:4"`
```

### 4. Use Categories for Complex Configs

```go
type Config struct {
    // Basic - always visible
    URL  string `schema:"type:string,desc:API endpoint,category:basic"`
    Port int    `schema:"type:int,desc:Listen port,category:basic"`

    // Advanced - collapsed by default
    Timeout        int    `schema:"type:int,desc:Timeout in ms,category:advanced,default:5000"`
    MaxConnections int    `schema:"type:int,desc:Connection pool size,category:advanced,default:10"`
    EnableDebug    bool   `schema:"type:bool,desc:Verbose logging,category:advanced,default:false"`
}
```

### 5. Commit Generated Files

```bash
# Always commit schemas with code changes
git add componentregistry/
git add output/mycomponent/
git add schemas/
git add specs/

git commit -m "feat(my-component): add new component"
```

### 6. Run Contract Tests

```bash
# Before committing
go test ./test/contract -v
npm test -- src/lib/contract

# In CI/CD pipeline
# Tests will fail if schemas are out of sync
```

## Documentation Reference

### Core Documentation

- **[SCHEMA_TAGS_GUIDE.md](./SCHEMA_TAGS_GUIDE.md)** - Complete reference for writing schema tags
- **[SCHEMA_VERSIONING.md](./SCHEMA_VERSIONING.md)** - Versioning strategy and migration paths
- **[MIGRATION_GUIDE.md](./MIGRATION_GUIDE.md)** - Migrating from manual to build-time schemas
- **[OPENAPI_INTEGRATION.md](./OPENAPI_INTEGRATION.md)** - OpenAPI spec generation and usage
- **[CONTRACT_TESTING.md](./CONTRACT_TESTING.md)** - Contract testing strategy
- **[CI_SCHEMA_INTEGRATION.md](./CI_SCHEMA_INTEGRATION.md)** - CI/CD integration

### Quick Reference

| Need to... | See... |
|---|---|
| Write schema tags | [SCHEMA_TAGS_GUIDE.md](./SCHEMA_TAGS_GUIDE.md) |
| Version schemas | [SCHEMA_VERSIONING.md](./SCHEMA_VERSIONING.md) |
| Migrate component | [MIGRATION_GUIDE.md](./MIGRATION_GUIDE.md) |
| Understand OpenAPI | [OPENAPI_INTEGRATION.md](./OPENAPI_INTEGRATION.md) |
| Set up contract tests | [CONTRACT_TESTING.md](./CONTRACT_TESTING.md) |
| Configure CI | [CI_SCHEMA_INTEGRATION.md](./CI_SCHEMA_INTEGRATION.md) |

## Summary

**Build-Time Schema Generation System**:
- ✅ Schemas defined in code with struct tags
- ✅ Automatic generation during build
- ✅ Contract validation in CI/CD
- ✅ Type-safe frontend-backend communication
- ✅ Single source of truth

**Key Commands**:
```bash
task schema:generate         # Generate schemas and OpenAPI spec
task generate-types          # Generate TypeScript types (frontend)
go test ./test/contract -v   # Run backend contract tests
npm test -- src/lib/contract # Run frontend contract tests
```

**Workflow**:
1. Write component with schema tags
2. Register component
3. Run `task schema:generate`
4. Run contract tests
5. Commit all changes together

Following this system ensures your component configurations, API contracts, and TypeScript types stay perfectly synchronized throughout the development lifecycle!
