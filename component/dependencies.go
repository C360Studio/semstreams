package component

import (
	"context"
	"log/slog"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/security"
	"github.com/c360studio/semstreams/types"
)

// PlatformMeta provides platform identity to components.
// Type alias to avoid import cycles while maintaining compatibility.
type PlatformMeta = types.PlatformMeta

// Lookup provides read-only access to sibling components at call time.
// Lazy lookup avoids stale pointers when ComponentManager restarts components.
type Lookup interface {
	Component(name string) Discoverable
}

// ToolRegistryReader is the dependency-side surface of the agentic-
// tools executor registry. *agentictools.ExecutorRegistry satisfies
// it implicitly. Defined here in the component package so all
// component consumers can refer to it through Dependencies without
// importing agentic-tools — mirrors how model.RegistryReader is wired.
//
// On a tool miss, Execute returns a wrapped agentic.ErrToolNotFound
// sentinel. Callers detect via errors.Is — the previous string-match
// fallback in agentic-tools/component.go was the source of repeated
// extension friction and is gone with this contract.
type ToolRegistryReader interface {
	Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error)
	ListTools() []agentic.ToolDefinition
}

// PayloadRegistryReader is the dependency-side surface of the payload
// registry. *payloadregistry.Registry satisfies it implicitly. Defined
// here in the component package so all component consumers can refer
// to it through Dependencies without depending on payloadregistry's
// concrete type — mirrors ToolRegistryReader.
//
// Beta.18 deliverable: payloads previously registered via init() side
// effects on a package-level singleton; that singleton is being
// retired in favor of a constructor-injected *Registry plumbed through
// this interface. message.BaseMessage.UnmarshalJSON looks up
// type-discriminator → empty-instance via this interface; callers
// that build a BaseMessage for unmarshaling supply a registry via
// message.NewBaseMessageForUnmarshal or message.NewDecoder.
type PayloadRegistryReader interface {
	Create(domain, category, version string) any
	Build(domain, category, version string, fields map[string]any) (any, error)
	List() map[string]*payloadregistry.Registration
}

// Dependencies provides all external dependencies needed by components.
type Dependencies struct {
	NATSClient        *natsclient.Client      // NATS client for messaging
	MetricsRegistry   *metric.MetricsRegistry // Metrics registry for Prometheus (can be nil)
	Logger            *slog.Logger            // Structured logger (can be nil, defaults to slog.Default())
	Platform          PlatformMeta            // Platform identity (organization and platform)
	Security          security.Config         // Platform-wide security configuration
	ModelRegistry     model.RegistryReader    // Unified model registry (can be nil)
	ToolRegistry      ToolRegistryReader      // Shared tool executor registry (can be nil; agentic-tools requires it)
	PayloadRegistry   PayloadRegistryReader   // Shared payload registry (can be nil; components unmarshaling BaseMessage require it)
	ComponentRegistry Lookup                  // Sibling component lookup (can be nil)
}

// GetLogger returns the configured logger or a default logger if none is provided
func (d *Dependencies) GetLogger() *slog.Logger {
	if d.Logger != nil {
		return d.Logger
	}
	return slog.Default()
}

// GetLoggerWithComponent returns a logger configured with component context
func (d *Dependencies) GetLoggerWithComponent(componentName string) *slog.Logger {
	return d.GetLogger().With("component", componentName)
}
