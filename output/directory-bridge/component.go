package directorybridge

import (
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"sync"
	"time"

	"github.com/c360studio/semstreams/agentic/identity"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/retry"
	oasfgenerator "github.com/c360studio/semstreams/processor/oasf-generator"
	"github.com/nats-io/nats.go/jetstream"
)

// componentSchema defines the configuration schema
var componentSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Ensure Component implements required interfaces
var (
	_ component.Discoverable       = (*Component)(nil)
	_ component.LifecycleComponent = (*Component)(nil)
)

// Component implements the directory bridge output component.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	// Backend and registration manager. The backend is selected during
	// Initialize; today only the HTTP wire format is wired, but the
	// Backend interface accommodates a future gRPC (agntcy/dir) impl.
	backend    Backend
	regManager *RegistrationManager
	idProvider identity.Provider

	// Lifecycle management
	running   bool
	startTime time.Time
	mu        sync.RWMutex

	// KV watcher for OASF records
	kvWatcher jetstream.KeyWatcher

	// Metrics tracking
	registrations int64
	errors        int64
	lastActivity  time.Time

	// Context for background operations
	ctx    context.Context
	cancel context.CancelFunc
}

// NewComponent creates a new directory bridge component.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponent", "unmarshal config")
	}

	// Use default config if ports not set
	if config.Ports == nil {
		config = DefaultConfig()
		// Re-unmarshal to get user-provided values
		if err := json.Unmarshal(rawConfig, &config); err != nil {
			return nil, errs.WrapInvalid(err, "Component", "NewComponent", "unmarshal config")
		}
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "NewComponent", "validate config")
	}

	return &Component{
		name:       "directory-bridge",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	// Select backend. PR-A wires the HTTP backend exclusively; backend
	// selection from config lands in a follow-up PR.
	c.backend = NewHTTPBackend(c.config.DirectoryURL)

	// Create identity provider
	var err error
	c.idProvider, err = identity.DefaultProviderFactory(identity.ProviderConfig{
		ProviderType: c.config.IdentityProvider,
	})
	if err != nil {
		return errs.Wrap(err, "Component", "Initialize", "create identity provider")
	}

	// Create registration manager
	c.regManager = NewRegistrationManager(c.backend, c.idProvider, c.config, c.logger)

	return nil
}

// Start begins watching for OASF records and registering agents.
func (c *Component) Start(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Component", "Start", "context already cancelled")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return errs.WrapFatal(errs.ErrAlreadyStarted, "Component", "Start", "check running state")
	}

	if c.natsClient == nil {
		return errs.WrapFatal(errs.ErrNoConnection, "Component", "Start", "check NATS client")
	}

	// Create cancellable context for background operations
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start registration manager (heartbeat loop)
	if err := c.regManager.Start(c.ctx); err != nil {
		c.cancel()
		return errs.Wrap(err, "Component", "Start", "start registration manager")
	}

	// Start KV watcher for OASF records
	if err := c.startKVWatcher(c.ctx); err != nil {
		c.regManager.Stop(c.ctx)
		c.cancel()
		return errs.Wrap(err, "Component", "Start", "start KV watcher")
	}

	c.running = true
	c.startTime = time.Now()

	c.logger.Info("Directory bridge started",
		slog.String("directory_url", c.config.DirectoryURL),
		slog.String("oasf_kv_bucket", c.config.OASFKVBucket))

	return nil
}

// startKVWatcher starts watching the OASF KV bucket for changes.
// If the bucket doesn't exist yet, it starts a background retry loop using pkg/retry.
func (c *Component) startKVWatcher(ctx context.Context) error {
	// Start background goroutine to wait for bucket and watch
	go c.watchWithRetry(ctx)
	return nil
}

// watchWithRetry waits for the OASF KV bucket to exist, then watches it.
func (c *Component) watchWithRetry(ctx context.Context) {
	// Use Persistent config for long-running retry (30 attempts over ~60s)
	cfg := retry.Persistent()

	kv, err := retry.DoWithResult(ctx, cfg, func() (jetstream.KeyValue, error) {
		bucket, err := c.natsClient.GetKeyValueBucket(ctx, c.config.OASFKVBucket)
		if err != nil {
			c.logger.Debug("OASF KV bucket not available yet, retrying",
				slog.String("bucket", c.config.OASFKVBucket))
			return nil, err
		}
		return bucket, nil
	})

	if err != nil {
		c.logger.Error("Failed to connect to OASF KV bucket after retries",
			slog.String("bucket", c.config.OASFKVBucket),
			slog.Any("error", err))
		return
	}

	// Create watcher for all keys
	watcher, err := kv.Watch(ctx, ">", jetstream.IgnoreDeletes())
	if err != nil {
		c.logger.Error("Failed to create KV watcher",
			slog.String("bucket", c.config.OASFKVBucket),
			slog.Any("error", err))
		return
	}

	c.mu.Lock()
	c.kvWatcher = watcher
	c.mu.Unlock()

	c.logger.Info("Successfully connected to OASF KV bucket",
		slog.String("bucket", c.config.OASFKVBucket))

	// Start watch loop
	c.watchLoop(ctx)
}

// watchLoop processes OASF record updates in a background goroutine.
func (c *Component) watchLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			c.logger.Debug("Watch loop stopped: context cancelled")
			return
		case entry, ok := <-c.kvWatcher.Updates():
			if !ok {
				c.logger.Warn("KV watcher channel closed unexpectedly")
				return
			}
			if entry == nil {
				// Initial values complete
				continue
			}

			c.handleOASFRecord(ctx, entry)
		}
	}
}

// handleOASFRecord processes a single OASF record from KV.
func (c *Component) handleOASFRecord(ctx context.Context, entry jetstream.KeyValueEntry) {
	c.mu.Lock()
	c.lastActivity = time.Now()
	c.mu.Unlock()

	entityID := entry.Key()

	// Parse OASF record
	var record oasfgenerator.OASFRecord
	if err := json.Unmarshal(entry.Value(), &record); err != nil {
		c.logger.Error("Failed to parse OASF record",
			slog.String("entity_id", entityID),
			slog.Any("error", err))
		c.incrementErrors()
		return
	}

	// Register or update with directory
	if err := c.regManager.UpdateRegistration(ctx, entityID, &record); err != nil {
		c.logger.Error("Failed to register agent with directory",
			slog.String("entity_id", entityID),
			slog.Any("error", err))
		c.incrementErrors()
		return
	}

	c.mu.Lock()
	c.registrations++
	c.mu.Unlock()

	c.logger.Debug("Processed OASF record for directory registration",
		slog.String("entity_id", entityID),
		slog.String("agent_name", record.Name))
}

// incrementErrors safely increments the error counter.
func (c *Component) incrementErrors() {
	c.mu.Lock()
	c.errors++
	c.mu.Unlock()
}

// Stop gracefully stops the component.
func (c *Component) Stop(timeout time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	// Cancel background context first to signal goroutines to stop
	if c.cancel != nil {
		c.cancel()
	}

	// Create timeout context for shutdown operations
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Stop KV watcher
	if c.kvWatcher != nil {
		if err := c.kvWatcher.Stop(); err != nil {
			c.logger.Warn("Failed to stop KV watcher", slog.Any("error", err))
		}
		c.kvWatcher = nil
	}

	// Stop registration manager (deregisters all agents)
	if c.regManager != nil {
		if err := c.regManager.Stop(ctx); err != nil {
			c.logger.Warn("Failed to stop registration manager", slog.Any("error", err))
		}
	}

	c.running = false
	c.logger.Info("Directory bridge stopped")

	return nil
}

// Discoverable interface implementation

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "directory-bridge",
		Type:        "output",
		Description: "Registers agents with AGNTCY directories using OASF records",
		Version:     "1.0.0",
	}
}

// InputPorts returns configured input port definitions.
func (c *Component) InputPorts() []component.Port {
	if c.config.Ports == nil {
		return []component.Port{}
	}

	ports := make([]component.Port, len(c.config.Ports.Inputs))
	for i, portDef := range c.config.Ports.Inputs {
		ports[i] = component.Port{
			Name:        portDef.Name,
			Direction:   component.DirectionInput,
			Required:    portDef.Required,
			Description: portDef.Description,
			Config: component.NATSPort{
				Subject: portDef.Subject,
			},
		}
	}
	return ports
}

// OutputPorts returns configured output port definitions.
func (c *Component) OutputPorts() []component.Port {
	if c.config.Ports == nil {
		return []component.Port{}
	}

	ports := make([]component.Port, len(c.config.Ports.Outputs))
	for i, portDef := range c.config.Ports.Outputs {
		port := component.Port{
			Name:        portDef.Name,
			Direction:   component.DirectionOutput,
			Required:    portDef.Required,
			Description: portDef.Description,
		}
		if portDef.Type == "jetstream" {
			port.Config = component.JetStreamPort{
				StreamName: portDef.StreamName,
				Subjects:   []string{portDef.Subject},
			}
		} else {
			port.Config = component.NATSPort{
				Subject: portDef.Subject,
			}
		}
		ports[i] = port
	}
	return ports
}

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return componentSchema
}

// Health returns the current health status.
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	status := "stopped"
	if c.running {
		status = "running"
	}

	return component.HealthStatus{
		Healthy:    c.running,
		LastCheck:  time.Now(),
		ErrorCount: int(c.errors),
		Uptime:     time.Since(c.startTime),
		Status:     status,
	}
}

// DataFlow returns current data flow metrics.
func (c *Component) DataFlow() component.FlowMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var errorRate float64
	total := c.registrations + c.errors
	if total > 0 {
		errorRate = float64(c.errors) / float64(total)
	}

	return component.FlowMetrics{
		MessagesPerSecond: 0,
		BytesPerSecond:    0,
		ErrorRate:         errorRate,
		LastActivity:      c.lastActivity,
	}
}

// GetRegistrations returns all active registrations.
func (c *Component) GetRegistrations() []*Registration {
	if c.regManager == nil {
		return nil
	}
	return c.regManager.ListRegistrations()
}
