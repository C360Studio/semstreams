package component

import (
	"context"
	"log/slog"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	"github.com/c360studio/semstreams/pkg/security"
	"github.com/c360studio/semstreams/storage"
	"github.com/c360studio/semstreams/storage/storeregistry"
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

// StoreProvider is implemented by storage components that own one or more stores
// addressable by StorageInstance name (ADR-063). The ComponentManager reads this
// AFTER the component Starts to populate the shared StoreRegistry, and clears
// those entries when the component Stops — so a reconfig swaps the live handle.
//
// The map is keyed by the StorageInstance name each store STAMPS into refs
// (store.InstanceName()), so the registry key, the store-provide port token, and
// the ref's StorageInstance are the same value by construction. Returns nil/empty
// before Start (no store yet) or for a component that provides no store.
type StoreProvider interface {
	ProvidedStores() map[string]storage.StreamableStore
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

	// LifecycleManager is the shared pkg/lifecycle.Manager that
	// owns workflow-shaped entity instances (ADR-047). Apps that
	// declare Participant-implementing entities (drone missions,
	// sensor lifecycles, manufacturing batches, scenario executions)
	// build the Manager in main.go, call Manager.Register for each
	// app workflow, and pass it through Dependencies. Both the rule
	// processor (lifecycle_* actions + $entity.lifecycle.* condition
	// fields) and the lifecycle-gateway (operator HTTP API) read
	// from this field.
	//
	// Concrete type (not an interface) per the PayloadRegistry
	// precedent — pkg/lifecycle is a framework-owned leaf package,
	// not an external/pluggable surface, and consumers narrow to
	// their own minimum-surface interfaces locally (see
	// processor/rule.LifecycleManager) when they want to abstract
	// for testing.
	//
	// Can be nil — apps without any lifecycle-managed entity types
	// pay zero cost, and the consumers that DO read this field
	// loud-fail with a wiring error rather than silently no-op'ing.
	LifecycleManager *lifecycle.Manager

	// StoreRegistry is the shared {StorageInstance → storage.StreamableStore}
	// resolver (ADR-063). The ComponentManager populates it from storage
	// components' store-provide ports at Start and clears entries at Stop;
	// content-fetch consumers (graph-embedding, fusion) resolve a StorageRef's
	// StorageInstance through it, lazily per-fetch. Concrete framework-leaf type
	// per the PayloadRegistry / LifecycleManager precedent.
	//
	// Can be nil — deployments with no offloaded-content fetch pay zero cost;
	// consumers degrade (embedding falls back to its local store-read store or
	// reports content-unresolved) rather than panicking.
	StoreRegistry *storeregistry.Registry
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
