# Graph-Gateway Component Test Requirements

## Mode: Greenfield

This component does not exist yet. All tests are new and LOCKED.

## Requirements Covered

| Spec Requirement | Test Function |
|------------------|---------------|
| Valid config with ports | TestConfig_Validate_ValidConfig |
| Config validation (ports) | TestConfig_Validate_MissingPorts |
| Config validation (paths) | TestConfig_Validate_InvalidPaths |
| Apply defaults | TestConfig_ApplyDefaults |
| Default config valid | TestDefaultConfig_ReturnsValidConfig |
| Component type is "gateway" | TestComponent_Meta_ReturnsCorrectMetadata |
| HTTP input port | TestComponent_InputPorts_ReturnsHTTPPort |
| NATS request output port | TestComponent_OutputPorts_ReturnsNATSRequestPort |
| Config schema | TestComponent_ConfigSchema_ReturnsValidSchema |
| Health before start | TestComponent_Health_NotStarted |
| DataFlow metrics | TestComponent_DataFlow_ReturnsMetrics |
| Initialize success | TestComponent_Initialize_Success |
| Initialize invalid config | TestComponent_Initialize_InvalidConfig |
| Initialize idempotent | TestComponent_Initialize_Idempotent |
| Start before initialize fails | TestComponent_Start_BeforeInitialize |
| Stop before start safe | TestComponent_Stop_BeforeStart |
| RegisterHTTPHandlers GraphQL | TestComponent_RegisterHTTPHandlers_RegistersGraphQL |
| RegisterHTTPHandlers MCP | TestComponent_RegisterHTTPHandlers_RegistersMCP |
| RegisterHTTPHandlers Playground | TestComponent_RegisterHTTPHandlers_RegistersPlayground |
| Playground disabled by default | TestComponent_RegisterHTTPHandlers_NoPlaygroundByDefault |
| Prefix handling | TestComponent_RegisterHTTPHandlers_HandlesPrefix |
| Factory valid config | TestCreateGraphGateway_ValidConfig |
| Factory empty config | TestCreateGraphGateway_EmptyConfig |
| Factory invalid config | TestCreateGraphGateway_InvalidConfig |
| Factory missing deps | TestCreateGraphGateway_MissingDependencies |
| Factory partial config | TestCreateGraphGateway_PartialConfig |
| Registry registration | TestRegister_AddsToRegistry |
| Thread-safe health checks | TestComponent_ConcurrentHealthChecks_ThreadSafe |
| Thread-safe initialize | TestComponent_ConcurrentInitialize_ThreadSafe |
| Invalid ports error | TestComponent_InitializeError_InvalidPorts |
| Metrics accumulation | TestComponent_Metrics_AccumulateCorrectly |

## Files Created (LOCKED)

- `gateway/graph-gateway/component_test.go` (DO NOT EDIT header)

## Unit Test Inventory

### Config Tests
```
TestConfig_Validate_ValidConfig/valid_minimal_config
TestConfig_Validate_ValidConfig/valid_full_config_with_playground
TestConfig_Validate_MissingPorts/missing_ports_config
TestConfig_Validate_MissingPorts/empty_inputs
TestConfig_Validate_MissingPorts/empty_outputs
TestConfig_Validate_InvalidPaths/empty_GraphQL_path
TestConfig_Validate_InvalidPaths/empty_MCP_path
TestConfig_Validate_InvalidPaths/empty_bind_address
TestConfig_ApplyDefaults
TestDefaultConfig_ReturnsValidConfig
```

### Discoverable Interface Tests (6 methods)
```
TestComponent_Meta_ReturnsCorrectMetadata
TestComponent_InputPorts_ReturnsHTTPPort
TestComponent_OutputPorts_ReturnsNATSRequestPort
TestComponent_ConfigSchema_ReturnsValidSchema
TestComponent_Health_NotStarted
TestComponent_Health_Running (SKIPPED - integration test)
TestComponent_DataFlow_ReturnsMetrics
```

### LifecycleComponent Interface Tests (3 methods)
```
TestComponent_Initialize_Success
TestComponent_Initialize_InvalidConfig
TestComponent_Initialize_Idempotent
TestComponent_Start_Success (SKIPPED - integration test)
TestComponent_Start_BeforeInitialize
TestComponent_Start_AlreadyStarted (SKIPPED - integration test)
TestComponent_Stop_Success (SKIPPED - integration test)
TestComponent_Stop_BeforeStart
TestComponent_Stop_Timeout (SKIPPED - integration test)
```

### Gateway Interface Tests
```
TestComponent_RegisterHTTPHandlers_RegistersGraphQL
TestComponent_RegisterHTTPHandlers_RegistersMCP
TestComponent_RegisterHTTPHandlers_RegistersPlayground
TestComponent_RegisterHTTPHandlers_NoPlaygroundByDefault
TestComponent_RegisterHTTPHandlers_HandlesPrefix/with_trailing_slash
TestComponent_RegisterHTTPHandlers_HandlesPrefix/without_trailing_slash
TestComponent_RegisterHTTPHandlers_HandlesPrefix/root_prefix
TestComponent_RegisterHTTPHandlers_HandlesPrefix/nested_prefix
```

### Factory and Registration Tests
```
TestCreateGraphGateway_ValidConfig
TestCreateGraphGateway_EmptyConfig
TestCreateGraphGateway_InvalidConfig
TestCreateGraphGateway_MissingDependencies
TestCreateGraphGateway_PartialConfig
TestRegister_AddsToRegistry
```

### Context and Concurrency Tests
```
TestComponent_RespectsContext_Cancellation (SKIPPED - integration test)
TestComponent_RespectsContext_Timeout
TestComponent_ConcurrentHealthChecks_ThreadSafe
TestComponent_ConcurrentInitialize_ThreadSafe
```

### Error Handling Tests
```
TestComponent_InitializeError_InvalidPorts
TestComponent_Metrics_AccumulateCorrectly
```

## Integration Test Requirements

Builder must create integration tests (`//go:build integration`) covering:

### HTTP Server Integration
- [ ] Start HTTP server and verify listening on configured address
- [ ] GraphQL endpoint accepts POST requests
- [ ] MCP endpoint accepts SSE connections
- [ ] Playground serves HTML when enabled
- [ ] Graceful shutdown with timeout

### NATS Integration
- [ ] GraphQL mutations route to NATS subjects
- [ ] MCP tool calls route to NATS subjects
- [ ] Request/reply with actual NATS server
- [ ] Timeout handling for slow NATS responses

### QueryManager Integration
- [ ] Direct KV reads for queries
- [ ] GraphQL queries return entity data
- [ ] Semantic search integration
- [ ] Community queries work

### Full Stack Integration
- [ ] HTTP → QueryManager → KV (read path)
- [ ] HTTP → NATS → graph-ingest (write path)
- [ ] Concurrent requests handle correctly
- [ ] Context cancellation propagates

## Component Structure Required

The component MUST follow this structure (from plan):

```go
type Config struct {
    Ports           *component.PortConfig `json:"ports" schema:"..."`
    GraphQLPath     string                `json:"graphql_path" schema:"..."`
    MCPPath         string                `json:"mcp_path" schema:"..."`
    EnablePlayground bool                 `json:"enable_playground" schema:"..."`
    BindAddress     string                `json:"bind_address" schema:"..."`
}

func DefaultConfig() Config { ... }

var schema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

type Component struct {
    // Config
    name   string
    config Config
    
    // Dependencies
    natsClient      *natsclient.Client
    logger          *slog.Logger
    
    // HTTP servers (integrated, not wrapped)
    httpServer   *http.Server
    graphqlMux   *http.ServeMux
    
    // QueryManager for reads
    queryManager *querymanager.Manager
    
    // Lifecycle
    mu        sync.RWMutex
    running   bool
    startTime time.Time
    wg        sync.WaitGroup
    cancel    context.CancelFunc
    
    // Metrics (atomic)
    requestsProcessed int64
    errors           int64
    
    // Ports
    inputPorts  []component.Port
    outputPorts []component.Port
}

// All 9 required methods:
func (c *Component) Meta() component.Metadata
func (c *Component) InputPorts() []component.Port
func (c *Component) OutputPorts() []component.Port
func (c *Component) ConfigSchema() component.ConfigSchema
func (c *Component) Health() component.HealthStatus
func (c *Component) DataFlow() component.FlowMetrics
func (c *Component) Initialize() error
func (c *Component) Start(ctx context.Context) error
func (c *Component) Stop(ctx context.Context) error

// Gateway interface method:
func (c *Component) RegisterHTTPHandlers(prefix string, mux *http.ServeMux)

// Factory + Registration
func CreateGraphGateway(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error)
func Register(registry *component.Registry) error
```

## Port Definitions (from plan)

```yaml
inputs:
  - name: http
    type: http
    paths: ["/graphql", "/mcp"]
outputs:
  - name: mutations
    type: nats-request
    subject: "graph.mutation.>"
    interface: "semstreams.graph.mutation"
    required: true
reads:  # Direct KV access (not ports, just dependencies)
  - ENTITY_STATES
  - OUTGOING_INDEX
  - INCOMING_INDEX
  - ALIAS_INDEX
  - PREDICATE_INDEX
  - COMMUNITY_INDEX
```

## Reference Implementations

Use these as patterns:
- `gateway/http/http.go` - Gateway interface implementation
- `processor/graph/gateway/graphql/server.go` - GraphQL server logic
- `processor/graph/gateway/mcp/server.go` - MCP server logic
- `processor/graph-index/component.go` - Component structure pattern
- `processor/graph-ingest/component.go` - Component structure pattern

## Important Notes

1. **Component type is "gateway"** not "processor" (per plan and test)
2. **Tests that require Start() or real HTTP server are SKIPPED** with message "requires real HTTP server - move to integration tests"
3. **RegisterHTTPHandlers must handle prefix correctly** - with/without trailing slash
4. **Playground is optional** - disabled by default, enabled via config
5. **All 9 interface methods must be implemented** (6 Discoverable + 3 LifecycleComponent)
6. **Plus RegisterHTTPHandlers** from Gateway interface

## Handoff to Builder

Builder must:
1. Create `gateway/graph-gateway/component.go` with the structure above
2. Make all unit tests pass (cannot modify test file - DO NOT EDIT header)
3. Write integration tests per requirements above
4. Integrate GraphQL and MCP server logic from `processor/graph/gateway/`
5. Use QueryManager for direct KV reads
6. Route mutations via NATS request/reply
7. Run `task test` and `task test:integration` (when available)
8. Follow existing component patterns from graph-index and graph-ingest

## Test Execution

To run these tests after implementation:

```bash
# Run unit tests
go test -v ./gateway/graph-gateway/

# Run with race detector
go test -race -v ./gateway/graph-gateway/

# Run integration tests (when created)
go test -tags=integration -v ./gateway/graph-gateway/
```

Expected: All non-skipped tests pass, skipped tests are clearly marked.
