// Package lifecyclegateway — Discoverable + Gateway + factory.
package lifecyclegateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/lifecycle"
)

// ComponentName is the registry name. Exported so apps can resolve
// the component from ComponentRegistry by string without copying the
// literal.
const ComponentName = "lifecycle-gateway"

// Config is the operator-facing configuration surface. Field tags
// populate the generated OpenAPI schema (`schemas/lifecycle-gateway.v1.json`)
// — every field MUST carry `schema:"type:...,description:...,category:..."`
// so operator dashboards can render the config surface. See
// `component/schema_tags.go` for the conventions.
type Config struct {
	// PathPrefix is mounted under the parent component prefix (the
	// arg to RegisterHTTPHandlers). Default "workflows" so the
	// fully-resolved routes look like /lifecycle-gateway/workflows
	// or /workflows when mounted at root. Leading slash is added
	// automatically; trailing slash is normalized.
	PathPrefix string `json:"path_prefix,omitempty" schema:"type:string,description:URL path prefix mounted under the parent component prefix (e.g. \"workflows\" → /lifecycle-gateway/workflows). Default \"workflows\". Must be non-empty after stripping leading/trailing slashes.,category:basic"`

	// EnableWebSocket toggles the WS upgrade path for
	// {prefix}/{type}?stream=true. Default true. Operators that
	// don't need live updates (purely poll-based dashboards) can
	// disable it by setting `"enable_websocket": false` in config.
	//
	// Pointer type (vs bare bool) so ApplyDefaults can distinguish
	// "field absent from config" (→ default true) from "field
	// explicitly set to false" (→ disable). Matches the
	// MaxIterations precedent in processor/rule/actions.go.
	// Callers read via the wsEnabled() helper, never field-direct.
	EnableWebSocket *bool `json:"enable_websocket,omitempty" schema:"type:bool,description:Enable WebSocket streaming on GET {prefix}/{type}?stream=true via Manager.Watch. Default true when omitted. Set to false to disable live-update streaming and keep the gateway poll-only.,category:basic"`

	// MaxBodyBytes caps the size of operator-supplied JSON bodies on
	// POST {type}/{id}/state and POST {type}/{id}/transition (S2
	// reviewer fix — unbounded reads were a DoS vector). Default 1MB.
	// Operators that genuinely need larger patch bodies (uncommon —
	// operator_writable surface should be narrow) raise this
	// explicitly. Zero or negative means "no override" → default.
	MaxBodyBytes int64 `json:"max_body_bytes,omitempty" schema:"type:int,description:Maximum bytes accepted in POST .../state and POST .../transition request bodies. Default 1048576 (1 MiB). Zero or negative means use default.,category:advanced"`

	// AllowedOrigins is the WebSocket-upgrade origin allowlist
	// (S7 reviewer fix — the gateway's POST surface mutates state,
	// so default-permissive cross-origin is a real risk; an explicit
	// allowlist forces operators to make the cross-origin decision
	// rather than inheriting the default). Empty list means
	// "permit all" — the gateway emits a Warn log at Start time so
	// operators see the policy choice in their logs. Wildcards are
	// not supported; explicit origins only.
	AllowedOrigins []string `json:"allowed_origins,omitempty" schema:"type:array,description:WebSocket upgrade Origin allowlist as exact-match strings. Empty list permits all origins and logs a Warn at Start; set explicitly to restrict cross-origin upgrades.,category:advanced"`
}

// Validate enforces operator-friendly constraints on the config.
// Defaults are applied by ApplyDefaults; Validate runs AFTER defaults
// so any invariant violation is genuine.
func (c *Config) Validate() error {
	// PathPrefix-after-trim must be non-empty (S4 reviewer fix —
	// "/" or "//" trim to "" and broke the registered mux pattern).
	if strings.Trim(c.PathPrefix, "/") == "" {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate",
			"path_prefix must be non-empty after stripping leading/trailing slashes")
	}
	return nil
}

// DefaultMaxBodyBytes is the request-body size cap applied to
// operator-supplied JSON on POST endpoints when Config.MaxBodyBytes
// is zero or negative. 1 MiB is generous for `operator_writable`
// patches (the protected-field surface should be narrow) and small
// enough to make resource-exhaustion attacks visible.
const DefaultMaxBodyBytes int64 = 1 << 20

// ApplyDefaults populates omitted fields. Called by the factory
// before Validate so operator-provided values stay sticky and
// unset fields fall back to framework conventions.
//
// EnableWebSocket is the only nullable field: nil means "operator
// omitted the key" → resolve to true. A non-nil pointer (even to
// false) is honored as-is. Same shape as MaxIterations in
// processor/rule/actions.go.
func (c *Config) ApplyDefaults() {
	if c.PathPrefix == "" {
		c.PathPrefix = "workflows"
	}
	if c.MaxBodyBytes <= 0 {
		c.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if c.EnableWebSocket == nil {
		v := true
		c.EnableWebSocket = &v
	}
}

// wsEnabled returns the effective EnableWebSocket value. nil pointer
// (operator omitted the key) is treated as true to match the
// documented default; a non-nil pointer is dereferenced. Centralized
// so callers never field-access the pointer directly.
func (c *Config) wsEnabled() bool {
	if c.EnableWebSocket == nil {
		return true
	}
	return *c.EnableWebSocket
}

// DefaultConfig returns a Config ready for use in tests +
// reasonable-default deployments.
func DefaultConfig() Config {
	t := true
	return Config{
		PathPrefix:      "workflows",
		EnableWebSocket: &t,
		MaxBodyBytes:    DefaultMaxBodyBytes,
	}
}

var schema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// LifecycleManager is the subset of *lifecycle.Manager used by the
// gateway. Defined as an interface here (consumer-defined, matching
// the rule processor's pattern in processor/rule.LifecycleManager)
// so tests can swap in a mock without depending on pkg/lifecycle
// internals (kvMockStore is unexported by design).
//
// The interface is wide enough to support all 8 endpoints + the WS
// watch surface; production deployments pass the concrete
// *lifecycle.Manager via Dependencies.LifecycleManager which
// implements every method implicitly.
type LifecycleManager interface {
	ListWorkflows() []lifecycle.WorkflowDef
	List(ctx context.Context, workflow string, opts lifecycle.ListOptions) ([]lifecycle.Participant, error)
	Get(ctx context.Context, workflow, entityID string) (lifecycle.Participant, error)
	History(ctx context.Context, workflow, entityID string) ([]lifecycle.TransitionEvent, error)
	Children(ctx context.Context, parentEntityID string) ([]lifecycle.Participant, error)
	UpdateFromOperator(ctx context.Context, workflow, entityID string, patch map[string]any) error
	Transition(ctx context.Context, workflow, entityID, newPhase string, source lifecycle.TransitionSource, note string) error
	Watch(ctx context.Context, workflow string) (<-chan lifecycle.Participant, error)
}

// Component is the lifecycle-gateway runtime. Wraps the shared
// Manager + emits the HTTP/WS surface.
type Component struct {
	name    string
	config  Config
	manager LifecycleManager
	logger  *slog.Logger
	upgrade websocket.Upgrader

	// Lifecycle state (atomic).
	running atomic.Bool

	// Metrics.
	requestsTotal   atomic.Uint64
	requestsSuccess atomic.Uint64
	requestsFailed  atomic.Uint64
	wsConnections   atomic.Int64 // current live WS connections

	mu         sync.RWMutex
	startTime  time.Time
	lastErr    string
	cachedRoot string // set by RegisterHTTPHandlers; read by serveHTTP
}

// CreateLifecycleGateway is the component-factory function. The
// rule processor and lifecycle-gateway both pull the same
// Manager from deps.LifecycleManager. Refusing to instantiate when
// the manager is nil is the loud-fail signal: a deployment that
// configures the gateway but doesn't wire a Manager has no
// workflows to expose — failing at boot surfaces the gap before an
// operator hits an empty endpoint and assumes "no instances."
func CreateLifecycleGateway(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	if deps.LifecycleManager == nil {
		return nil, errs.WrapFatal(errs.ErrMissingConfig, "Component", "CreateLifecycleGateway",
			"deps.LifecycleManager is nil — the gateway requires a registered pkg/lifecycle.Manager (build it in main.go, call Manager.Register for each workflow, then pass via Dependencies.LifecycleManager)")
	}

	var cfg Config
	if len(rawConfig) > 0 {
		if err := component.SafeUnmarshal(rawConfig, &cfg); err != nil {
			return nil, errs.WrapInvalid(err, "Component", "CreateLifecycleGateway",
				"config unmarshal")
		}
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, errs.Wrap(err, "Component", "CreateLifecycleGateway",
			"config validation")
	}

	logger := deps.GetLoggerWithComponent(ComponentName)

	return &Component{
		name:    ComponentName,
		config:  cfg,
		manager: deps.LifecycleManager,
		logger:  logger,
		upgrade: websocket.Upgrader{
			CheckOrigin: makeOriginCheck(cfg.AllowedOrigins),
		},
	}, nil
}

// makeOriginCheck builds the WebSocket Upgrader's CheckOrigin
// function from the configured allowlist. Empty allowlist =
// permit-all (with a Warn log at Start time so operators see the
// choice). Non-empty allowlist = exact-match the Origin header
// against the list.
func makeOriginCheck(allowed []string) func(*http.Request) bool {
	if len(allowed) == 0 {
		return func(_ *http.Request) bool { return true }
	}
	allow := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		allow[o] = struct{}{}
	}
	return func(r *http.Request) bool {
		_, ok := allow[r.Header.Get("Origin")]
		return ok
	}
}

// Register registers the lifecycle-gateway factory.
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        ComponentName,
		Factory:     CreateLifecycleGateway,
		Schema:      schema,
		Type:        "gateway",
		Protocol:    "http",
		Domain:      "lifecycle",
		Description: "Operator HTTP + WebSocket gateway over pkg/lifecycle.Manager (ADR-047)",
		Version:     "0.1.0",
	})
}

// Meta implements component.Discoverable.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        c.name,
		Type:        "gateway",
		Description: "Lifecycle harness operator gateway",
		Version:     "0.1.0",
	}
}

// InputPorts returns no input ports — the gateway is request-driven, not NATS-fed.
func (c *Component) InputPorts() []component.Port { return []component.Port{} }

// OutputPorts returns no output ports — the gateway is request-driven, not NATS-fed.
func (c *Component) OutputPorts() []component.Port { return []component.Port{} }

// ConfigSchema implements component.Discoverable.
func (c *Component) ConfigSchema() component.ConfigSchema { return schema }

// Initialize is a no-op; the Manager is wired at factory time.
func (c *Component) Initialize() error { return nil }

// Start records the start time + flips running. The gateway has no
// background goroutines of its own — handlers serve on demand via
// the parent HTTP server.
func (c *Component) Start(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "Start",
			"context cannot be nil")
	}
	if c.running.Load() {
		return errs.WrapFatal(errs.ErrAlreadyStarted, "Component", "Start",
			"gateway already running")
	}
	c.mu.Lock()
	c.startTime = time.Now()
	c.mu.Unlock()
	c.running.Store(true)

	// Loud-log the security-sensitive default-permissive origin
	// policy so operators see the choice in their startup logs
	// rather than discovering it after a cross-origin incident.
	if c.config.wsEnabled() && len(c.config.AllowedOrigins) == 0 {
		c.logger.Warn("lifecycle-gateway: WebSocket origin check is default-permissive",
			slog.String("policy", "AllowedOrigins is empty; cross-origin upgrades will be accepted"),
			slog.String("hint", "set allowed_origins in config to enforce an explicit allowlist"))
	}
	return nil
}

// Stop flips running off. Live WebSocket connections + in-flight
// HTTP requests are owned by the parent HTTP server — they close as
// part of the server's graceful shutdown, not via any check inside
// this component (we don't gate handlers on running.Load() — graph-
// gateway and the http gateway follow the same convention).
func (c *Component) Stop(_ time.Duration) error {
	c.running.Store(false)
	return nil
}

// Health reports gateway health. Healthy = running. ErrorCount maps
// to failed-request count so operator dashboards see uniform
// across-gateway metrics.
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	start := c.startTime
	lastErr := c.lastErr
	c.mu.RUnlock()

	running := c.running.Load()
	healthy := running

	return component.HealthStatus{
		Healthy:    healthy,
		LastError:  lastErr,
		LastCheck:  time.Now(),
		ErrorCount: int(c.requestsFailed.Load()),
		Uptime:     time.Since(start),
	}
}

// DataFlow reports gateway metrics. Throughput is averaged since
// startup; sliding-window per-second is a future addition matching
// the http gateway TODO.
func (c *Component) DataFlow() component.FlowMetrics {
	c.mu.RLock()
	start := c.startTime
	c.mu.RUnlock()

	total := c.requestsTotal.Load()
	failed := c.requestsFailed.Load()

	var errorRate float64
	if total > 0 {
		errorRate = float64(failed) / float64(total)
	}
	var mps float64
	if uptime := time.Since(start).Seconds(); uptime > 0 {
		mps = float64(total) / uptime
	}

	return component.FlowMetrics{
		MessagesPerSecond: mps,
		ErrorRate:         errorRate,
		LastActivity:      time.Now(),
	}
}

// RegisterHTTPHandlers mounts the lifecycle gateway routes under
// the parent prefix. The routing logic is intentionally
// non-mux-based at the leaf — net/http.ServeMux only handles
// prefix-based dispatch, so the gateway uses a single catch-all
// HandleFunc rooted at `{prefix}{path_prefix}/` and parses the
// remaining path segments inside the handler. This keeps the
// ServeMux contract simple (one entry per gateway component
// instance) and the path-parsing testable in isolation.
func (c *Component) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	root := strings.TrimSuffix(prefix, "/") + "/" + strings.Trim(c.config.PathPrefix, "/")
	if !strings.HasPrefix(root, "/") {
		root = "/" + root
	}
	c.setCachedRoot(root)
	// Two patterns: root exact-match (GET /workflows) and root +
	// slash for everything below. ServeMux dispatches by longest
	// match so both registrations coexist cleanly.
	mux.HandleFunc(root, c.serveHTTP)
	mux.HandleFunc(root+"/", c.serveHTTP)

	c.logger.Info("lifecycle-gateway routes registered",
		slog.String("root", root),
		slog.Bool("websocket_enabled", c.config.wsEnabled()))
}

// recordRequest tracks a request outcome for metrics.
func (c *Component) recordRequest(ok bool, errMsg string) {
	c.requestsTotal.Add(1)
	if ok {
		c.requestsSuccess.Add(1)
		return
	}
	c.requestsFailed.Add(1)
	if errMsg != "" {
		c.mu.Lock()
		c.lastErr = errMsg
		c.mu.Unlock()
	}
}
