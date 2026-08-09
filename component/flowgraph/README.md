# FlowGraph

Static analysis and validation for component port connections in SemStreams.

## Overview

The flowgraph package builds a directed graph representation of component port connections, enabling
static analysis to detect configuration issues before runtime. It validates that components are
correctly connected based on their port types and interaction patterns, identifying problems such as
missing publishers, dangling subscribers, or JetStream/NATS protocol mismatches.

This analysis runs during service startup to fail fast on misconfigured flows.

## Key Features

### Connectivity Analysis

Performs graph-based connectivity analysis to detect:

- **Orphaned Ports**: Ports with no matching connections (classified by severity)
- **Disconnected Nodes**: Components with zero edges (potentially misconfigured)
- **Connected Components**: Clusters of interconnected components (isolated subgraphs indicate
  configuration issues)

### NATS Pattern Matching

Validates connections using NATS wildcard semantics:

- **Exact Match**: `graph.ingest.data` matches `graph.ingest.data`
- **Single Wildcard**: `graph.*.data` matches `graph.ingest.data`
- **Multi Wildcard**: `graph.>` matches `graph.ingest.data`
- **Bidirectional**: Either side of a connection can be the pattern

### JetStream Stream Validation

Detects protocol mismatches between publishers and subscribers:

- **JetStream Requirement**: JetStream subscribers expect a durable stream to exist
- **Stream Creation**: Streams are only created by JetStream output ports via `EnsureStreams`
- **Critical Warning**: If a JetStream subscriber connects only to NATS publishers, no stream will
  be created and the subscriber will hang indefinitely

## Interaction Patterns

The package supports four interaction patterns:

| Pattern | Description | Direction | Matching |
|---------|-------------|-----------|----------|
| **stream** | NATS/JetStream pub-sub | Unidirectional | NATS wildcard |
| **request** | NATS request-reply | Bidirectional | Exact subject |
| **watch** | KV bucket observation | Unidirectional | Exact bucket |
| **network** | External boundaries | External | Exact address |

### Stream Pattern (NATS, JetStream)

Unidirectional flow from publishers to subscribers with NATS wildcard matching. Multiple publishers
and subscribers per subject are allowed.

### Request Pattern (NATS Request-Reply)

Bidirectional connections where any port can initiate requests. All ports with the same subject are
connected to each other for RPC-style communication.

### Watch Pattern (KV Bucket)

Unidirectional flow from KV writers to watchers. Multiple writers to the same bucket generates a
warning as this may indicate a design issue.

### Network Pattern (External)

External boundary ports such as HTTP, UDP, or file I/O. These are not connected within the graph as
they represent system boundaries. Multiple components binding to the same network address generates
an error.

## Usage Example

### Supported Construction Boundary

Production flow graphs are built by the framework service root from complete, admitted Registry
generation snapshots. `service.ComponentManager` owns that construction and its cache. Component
authors declare ports and use the normal component admission lifecycle; they do not construct a
`FlowGraph` or add nodes directly.

Direct downstream graph assembly is retired. Registry-to-node ingestion remains an internal
framework seam so flow validation consumes the same retained declaration and normalized facts as
the other Registry-backed runtime views.

### Flow Analysis

Obtain the graph through the running component manager, then use its read and analysis methods:

```go
graph, err := componentManager.GetFlowGraph()
if err != nil {
    return fmt.Errorf("build admitted component flow graph: %w", err)
}

// Analyze connectivity
result := graph.AnalyzeConnectivity()
if result.ValidationStatus == "warnings" {
    for _, orphan := range result.OrphanedPorts {
        log.Warn("orphaned port detected",
            "component", orphan.ComponentName,
            "port", orphan.PortName,
            "issue", orphan.Issue)
    }
}
```

### JetStream Stream Validation

```go
// Validate JetStream requirements
warnings := graph.ValidateStreamRequirements()
for _, w := range warnings {
    if w.Severity == "critical" {
        return fmt.Errorf("JetStream mismatch: %s", w.Issue)
    }
    log.Warn("stream requirement warning",
        "subscriber", w.SubscriberComp,
        "port", w.SubscriberPort,
        "publishers", w.PublisherComps)
}
```

## Analysis Results

The `AnalyzeConnectivity()` method returns a `FlowAnalysisResult` containing:

### ConnectedComponents

Clusters of interconnected components found via depth-first search. Multiple clusters indicate
isolated subgraphs in the flow, which may be intentional (separate processing pipelines) or
indicate configuration errors.

```go
// Example: [["input", "processor", "output"], ["monitor"]]
result.ConnectedComponents
```

### DisconnectedNodes

Components with zero edges. These components have no connections to any other components and may be
misconfigured or intentionally standalone (e.g., health monitors).

```go
for _, node := range result.DisconnectedNodes {
    fmt.Printf("Component %s: %s\n", node.ComponentName, node.Issue)
}
```

### OrphanedPorts

Ports with no matching connections, classified by issue type:

- **no_publishers**: Input port has no matching output ports (critical for required stream ports)
- **no_subscribers**: Output port has no matching input ports (warning for data flow)
- **optional_api_unused**: Request ports are optional by design
- **optional_interface_unused**: Interface-specific alternative ports (e.g., `write-graphable`)
- **optional_index_unwatched**: KV watch ports may be intentionally unused

```go
for _, orphan := range result.OrphanedPorts {
    fmt.Printf("Port %s.%s: %s (required: %v)\n",
        orphan.ComponentName,
        orphan.PortName,
        orphan.Issue,
        orphan.Required)
}
```

### ValidationStatus

Overall status of the flow graph:

- **healthy**: No issues detected, all required ports connected
- **warnings**: Issues detected (disconnected nodes, orphaned required ports)

## Thread Safety

FlowGraph is NOT safe for concurrent modification. It is designed for single-threaded construction
and analysis during service startup. Once analysis is complete, the graph should be treated as
read-only.

## Integration with Flow Service

The service layer requests a Registry-backed graph from `ComponentManager`; it does not rebuild
nodes from component instances:

```go
graph, err := componentManager.GetFlowGraph()
if err != nil {
    return fmt.Errorf("build admitted component flow graph: %w", err)
}

result := graph.AnalyzeConnectivity()
jsWarnings := graph.ValidateStreamRequirements()

// Fail startup if critical issues detected
if result.ValidationStatus == "warnings" || len(jsWarnings) > 0 {
    return errors.New("flow validation failed")
}
```

## Orphaned Port Classification

The package uses heuristics to classify orphaned ports by severity:

### Critical Issues

- Required stream input ports with no publishers
- Required stream output ports with no subscribers

### Warnings

- Optional ports with no connections
- Interface-specific alternative ports (detected by naming patterns like `write-graphable`)
- Request/reply ports (optional APIs)
- KV watch ports (may be intentionally unwatched indexes)

### Interface Alternative Detection

Ports are classified as optional interface alternatives if they:

1. Have an interface contract specified
2. Are not marked as required
3. Use naming patterns suggesting specialized variants (e.g., contain `-`)
4. Have connection IDs containing `.graphable`, `.typed`, or `.validated`

## See Also

- [component package](../README.md): Port types and component interfaces
- [service package](../../service/README.md): Uses flowgraph for flow validation
- [doc.go](doc.go): Complete package documentation with architecture details
