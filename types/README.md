# Types Package

Shared domain types used across the SemStreams platform to avoid circular dependencies.

## Purpose

This package provides foundational types that multiple packages need to reference. By placing these types in a low-level package with minimal dependencies, we break circular import chains between higher-level packages like `config`, `component`, and `service`.

## Design Principles

1. **Minimal dependencies** - Only imports `pkg/errs` and standard library
2. **No sub-packages** - Flat structure keeps the dependency graph simple
3. **Pure domain types** - Configuration structures and enums, no business logic
4. **Shared contracts** - Types used by multiple packages to communicate

## Types

### ComponentType

Enumeration of component categories in the processing pipeline:

```go
const (
    ComponentTypeInput     ComponentType = "input"
    ComponentTypeProcessor ComponentType = "processor"
    ComponentTypeOutput    ComponentType = "output"
    ComponentTypeStorage   ComponentType = "storage"
    ComponentTypeGateway   ComponentType = "gateway"
)
```

### ComponentConfig

Configuration structure for component instances, shared between `config` and `component` packages:

```go
type ComponentConfig struct {
    Type    ComponentType   `json:"type"`    // Component category
    Name    string          `json:"name"`    // Factory name (e.g., "udp", "websocket")
    Enabled bool            `json:"enabled"` // Desired component activation
    Config  json.RawMessage `json:"config"`  // Component-specific configuration
}
```

### ServiceConfig

Configuration structure for service instances:

```go
type ServiceConfig struct {
    Enabled bool            `json:"enabled"` // Desired next-boot activation
    Config  json.RawMessage `json:"config"`  // Service-specific configuration
}
```

The containing map key is the sole service identity. Service activation and
inner configuration changes take effect on process restart.

### PlatformMeta

Platform identity information decoupled from config structures:

```go
type PlatformMeta struct {
    Org      string // Organization namespace (e.g., "c360")
    Platform string // Platform identifier (e.g., "platform1")
}
```

## Usage

```go
import "github.com/c360/semstreams/types"

// Component configuration
cfg := types.ComponentConfig{
    Type:    types.ComponentTypeProcessor,
    Name:    "json_filter",
    Enabled: true,
    Config:  json.RawMessage(`{"field": "status"}`),
}

if err := cfg.Validate(); err != nil {
    // Handle validation error
}

// Service configuration
svcCfg := types.ServiceConfig{
    Enabled: true,
    Config:  json.RawMessage(`{"port":9090,"path":"/metrics"}`),
}
```

## Import Graph

```text
pkg/errs (leaf package)
    ↑
  types (this package)
    ↑
  ┌──┴──┐
config  component  service  ...
```

This package sits just above `pkg/errs` in the dependency hierarchy, allowing it to be imported by most other packages without creating cycles.
