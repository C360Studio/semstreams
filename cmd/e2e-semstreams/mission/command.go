// Package mission — Command payload + processor component.
//
// A MissionCommand is a Graphable that stamps a `mission.command`
// triple on the mission entity's ENTITY_STATES record. The rule
// engine watches ENTITY_STATES; when `mission.command=launch`
// appears on a mission entity, the lifecycle-mission-launch rule
// fires lifecycle_transition to drive the Manager state.
//
// The processor is intentionally tiny — pure NATS Subscribe (not
// JetStream consumer), no batching, no retry — because its only
// job in the e2e tier is to translate UDP → Graphable. Production
// apps wanting a similar shape would use the iot_sensor example as
// a template for richer transport handling.
package mission

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
)

// CommandPayloadDomain / Category / Version is the payload-registry
// type discriminator for MissionCommand JSON envelopes.
const (
	CommandPayloadDomain   = "mission"
	CommandPayloadCategory = "command"
	CommandPayloadVersion  = "v1"
)

// CommandComponentName is the registered component-factory key.
const CommandComponentName = "mission-command"

// Command is a Graphable that targets a mission entity by ID and
// stamps a single `mission.command` triple. The Subject of every
// triple is the federated 6-part entity ID composed from OrgID +
// Platform + MissionID — must match the State.EntityID() of the
// mission instance the rule should transition.
type Command struct {
	OrgID     string `json:"org_id"`
	Platform  string `json:"platform"`
	MissionID string `json:"mission_id"`
	Command   string `json:"command"`
}

// EntityIDFor returns the federated 6-part entity ID for a mission
// given the standard parts. Exposed as a helper so the e2e scenario
// / seeder can use the same shape.
func EntityIDFor(orgID, platform, missionID string) string {
	return fmt.Sprintf("%s.%s.lifecycle.gcs.mission.%s", orgID, platform, missionID)
}

// EntityID returns the federated identifier the command targets.
func (c *Command) EntityID() string {
	return EntityIDFor(c.OrgID, c.Platform, c.MissionID)
}

// Triples returns the single command triple. graph-ingest writes
// this into ENTITY_STATES; the rule engine fires on the predicate.
func (c *Command) Triples() []message.Triple {
	return []message.Triple{{
		Subject:    c.EntityID(),
		Predicate:  PredicateCommand,
		Object:     c.Command,
		Source:     "mission-command",
		Timestamp:  time.Now(),
		Confidence: 1.0,
	}}
}

// Validate enforces the minimal field set.
func (c *Command) Validate() error {
	if c.OrgID == "" || c.Platform == "" || c.MissionID == "" || c.Command == "" {
		return fmt.Errorf("mission.Command: org_id, platform, mission_id, command all required")
	}
	return nil
}

// Schema implements message.Payload.
func (c *Command) Schema() message.Type {
	return message.Type{
		Domain:   CommandPayloadDomain,
		Category: CommandPayloadCategory,
		Version:  CommandPayloadVersion,
	}
}

// MarshalJSON implements json.Marshaler via the alias-recursion
// pattern. Required for message.NewBaseMessage to accept Command as
// a Payload (the Payload interface includes json.Marshaler).
func (c *Command) MarshalJSON() ([]byte, error) {
	type Alias Command
	return json.Marshal((*Alias)(c))
}

// UnmarshalJSON implements json.Unmarshaler symmetrically.
func (c *Command) UnmarshalJSON(data []byte) error {
	type Alias Command
	return json.Unmarshal(data, (*Alias)(c))
}

// RegisterPayloads registers the mission.command.v1 envelope shape.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	return reg.Register(&payloadregistry.Registration{
		Domain:      CommandPayloadDomain,
		Category:    CommandPayloadCategory,
		Version:     CommandPayloadVersion,
		Description: "Mission command stamping a mission.command triple on the targeted mission entity",
		Factory:     func() any { return &Command{} },
	})
}

// --- Processor component ---

// ComponentConfig configures the mission-command processor.
type ComponentConfig struct {
	Ports    *component.PortConfig `json:"ports"`
	OrgID    string                `json:"org_id"`
	Platform string                `json:"platform"`
}

var commandSchema = component.GenerateConfigSchema(reflect.TypeOf(ComponentConfig{}))

// Component subscribes to a single core-NATS subject, parses JSON
// commands, and republishes them as BaseMessage-wrapped Graphables
// to the configured output subject. Output flows through graph-ingest
// which lands the triple in ENTITY_STATES, where the rule engine
// picks it up.
type Component struct {
	name       string
	inputSubj  string
	outputSubj string
	cfg        ComponentConfig

	nats   *natsclient.Client
	logger *slog.Logger

	mu        sync.Mutex
	running   atomic.Bool
	startTime time.Time
	subs      []*natsclient.Subscription
}

// Register registers the mission-command factory with the
// component registry.
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        CommandComponentName,
		Factory:     NewComponent,
		Schema:      commandSchema,
		Type:        "processor",
		Protocol:    "mission",
		Domain:      "mission",
		Description: "Translates mission-command UDP JSON into Graphable triples for the lifecycle e2e tier",
		Version:     "0.1.0",
	})
}

// NewComponent is the factory called by the component registry.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var cfg ComponentConfig
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, errs.WrapInvalid(err, "mission.Component", "NewComponent", "config unmarshal")
	}
	if cfg.Ports == nil || len(cfg.Ports.Inputs) == 0 || len(cfg.Ports.Outputs) == 0 {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "mission.Component", "NewComponent",
			"ports.inputs and ports.outputs are required")
	}
	if cfg.OrgID == "" || cfg.Platform == "" {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "mission.Component", "NewComponent",
			"org_id and platform are required")
	}
	return &Component{
		name:       CommandComponentName,
		inputSubj:  cfg.Ports.Inputs[0].Subject,
		outputSubj: cfg.Ports.Outputs[0].Subject,
		cfg:        cfg,
		nats:       deps.NATSClient,
		logger:     deps.GetLoggerWithComponent(CommandComponentName),
	}, nil
}

// Meta implements component.Discoverable.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name: c.name, Type: "processor",
		Description: "Mission command → Graphable triple",
		Version:     "0.1.0",
	}
}

// InputPorts returns the configured input ports.
func (c *Component) InputPorts() []component.Port {
	return []component.Port{{
		Name: "command_in", Direction: component.DirectionInput, Required: true,
		Config: component.NATSPort{Subject: c.inputSubj},
	}}
}

// OutputPorts returns the configured output ports.
func (c *Component) OutputPorts() []component.Port {
	return []component.Port{{
		Name: "command_out", Direction: component.DirectionOutput, Required: true,
		Config: component.NATSPort{Subject: c.outputSubj},
	}}
}

// ConfigSchema implements component.Discoverable.
func (c *Component) ConfigSchema() component.ConfigSchema { return commandSchema }

// Initialize is a no-op — subscriptions land in Start.
func (c *Component) Initialize() error { return nil }

// Start subscribes to the input subject.
func (c *Component) Start(ctx context.Context) error {
	if c.running.Load() {
		return errs.WrapFatal(errs.ErrAlreadyStarted, "mission.Component", "Start", "already running")
	}
	if c.nats == nil {
		return errs.WrapFatal(errs.ErrMissingConfig, "mission.Component", "Start", "NATS client required")
	}
	sub, err := c.nats.Subscribe(ctx, c.inputSubj, func(ctx context.Context, msg *nats.Msg) {
		c.handle(ctx, msg.Data)
	})
	if err != nil {
		return errs.WrapTransient(err, "mission.Component", "Start", "subscribe to "+c.inputSubj)
	}
	c.mu.Lock()
	c.subs = append(c.subs, sub)
	c.startTime = time.Now()
	c.mu.Unlock()
	c.running.Store(true)
	c.logger.Info("mission-command started",
		slog.String("input", c.inputSubj),
		slog.String("output", c.outputSubj))
	return nil
}

// Stop unsubscribes + flips running.
func (c *Component) Stop(_ time.Duration) error {
	if !c.running.Load() {
		return nil
	}
	c.mu.Lock()
	for _, sub := range c.subs {
		_ = sub.Unsubscribe()
	}
	c.subs = nil
	c.mu.Unlock()
	c.running.Store(false)
	return nil
}

// Health reports component health.
func (c *Component) Health() component.HealthStatus {
	c.mu.Lock()
	start := c.startTime
	c.mu.Unlock()
	return component.HealthStatus{
		Healthy:   c.running.Load(),
		LastCheck: time.Now(),
		Uptime:    time.Since(start),
	}
}

// DataFlow returns a minimal flow metric stub (this processor is
// e2e-only, not a production hotpath).
func (c *Component) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{LastActivity: time.Now()}
}

// IsStarted reports running state.
func (c *Component) IsStarted() bool { return c.running.Load() }

// handle parses a single UDP/NATS message into a Command, validates,
// and republishes wrapped in BaseMessage to the output JetStream
// subject. Errors are logged + dropped — this is an e2e fixture, not
// a production wire.
func (c *Component) handle(ctx context.Context, data []byte) {
	var cmd Command
	if err := json.Unmarshal(data, &cmd); err != nil {
		c.logger.Warn("mission-command: bad JSON", slog.String("error", err.Error()))
		return
	}
	if cmd.OrgID == "" {
		cmd.OrgID = c.cfg.OrgID
	}
	if cmd.Platform == "" {
		cmd.Platform = c.cfg.Platform
	}
	if err := cmd.Validate(); err != nil {
		c.logger.Warn("mission-command: invalid command", slog.String("error", err.Error()))
		return
	}

	baseMsg := message.NewBaseMessage(message.Type{
		Domain:   CommandPayloadDomain,
		Category: CommandPayloadCategory,
		Version:  CommandPayloadVersion,
	}, &cmd, c.name)

	wire, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("mission-command: marshal failed", slog.String("error", err.Error()))
		return
	}
	if err := c.nats.PublishToStream(ctx, c.outputSubj, wire); err != nil {
		c.logger.Error("mission-command: publish failed",
			slog.String("subject", c.outputSubj),
			slog.String("error", err.Error()))
		return
	}
	c.logger.Debug("mission-command emitted",
		slog.String("entity_id", cmd.EntityID()),
		slog.String("command", cmd.Command))
}
