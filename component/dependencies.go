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

// Dependencies provides all external dependencies needed by components.
//
// PayloadRegistry uses the concrete *payloadregistry.Registry rather
// than an interface (the way ToolRegistry uses ToolRegistryReader) on
// purpose: payloadregistry is a leaf package this package already
// imports, so there's no cycle to dodge, and message.NewDecoder also
// requires the concrete type. An interface here would force callers
// to type-assert at every Decoder construction site — pure friction
// for no abstraction win.
type Dependencies struct {
	NATSClient        *natsclient.Client        // NATS client for messaging
	MetricsRegistry   *metric.MetricsRegistry   // Metrics registry for Prometheus (can be nil)
	Logger            *slog.Logger              // Structured logger (can be nil, defaults to slog.Default())
	Platform          PlatformMeta              // Platform identity (organization and platform)
	Security          security.Config           // Platform-wide security configuration
	ModelRegistry     model.RegistryReader      // Unified model registry (can be nil)
	ToolRegistry      ToolRegistryReader        // Shared tool executor registry (can be nil; agentic-tools requires it)
	PayloadRegistry   *payloadregistry.Registry // Shared payload registry (can be nil; components unmarshaling BaseMessage require it)
	ComponentRegistry Lookup                    // Sibling component lookup (can be nil)
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
