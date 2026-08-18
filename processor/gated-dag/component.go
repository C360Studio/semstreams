package gateddagexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/lifecycle"
)

const componentName = "gated-dag"

// Register registers the gated-DAG executor factory with the component registry.
func Register(registry *component.Registry) error {
	return registry.RegisterFactory(componentName, &component.Registration{
		Name:        componentName,
		Type:        "processor",
		Protocol:    "nats",
		Domain:      "graph",
		Description: "Gated-DAG dispatch executor (ADR-046 Phase 2): dispatches DAG units in dependency order with restart recovery, failure isolation, and stall detection.",
		Version:     "1.0.0",
		Factory:     CreateGatedDag,
		Schema:      DefaultConfig().Schema(),
	})
}

// Component is the gated-DAG executor processor.
type Component struct {
	cfg        Config
	natsClient *natsclient.Client
	mgr        *lifecycle.Manager
	metricsReg *metric.MetricsRegistry
	logger     *slog.Logger
	outputs    []component.Port

	mu        sync.RWMutex
	running   bool
	startTime time.Time
	exec      *executor
}

// CreateGatedDag is the component.Factory for the gated-DAG executor.
func CreateGatedDag(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	return NewComponent(rawConfig, deps)
}

// NewComponent parses + validates config and wires dependencies.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (*Component, error) {
	var cfg Config
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &cfg); err != nil {
			return nil, fmt.Errorf("unmarshal gated-dag config: %w", err)
		}
	}
	cfg = cfg.withDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid gated-dag config: %w", err)
	}
	dispatchRetention := "limits"
	if cfg.DispatchStreamRetention == "workqueue" {
		dispatchRetention = "work_queue"
	}
	outputDefinitions := []component.PortDefinition{
		{
			Name:        "dispatch",
			Required:    true,
			Description: "Dispatch reference (unit entity ID) for a dispatchable unit",
			Config: component.JetStreamPort{
				StreamName:      cfg.DispatchStream,
				Subjects:        []string{cfg.DispatchSubject},
				Storage:         "file",
				RetentionPolicy: dispatchRetention,
			},
		},
		{
			Name:     "graph_mutations",
			Required: true,
			Config: component.NATSRequestPort{
				Subject: graphmutation.SubjectFamily,
				Interface: &component.InterfaceContract{
					Type: graphmutation.InterfaceType, Version: graphmutation.InterfaceVersion,
				},
			},
		},
	}
	outputs := make([]component.Port, len(outputDefinitions))
	for index, definition := range outputDefinitions {
		output, err := definition.Resolve(component.DirectionOutput)
		if err != nil {
			return nil, fmt.Errorf("resolve gated-dag output port %q: %w", definition.Name, err)
		}
		outputs[index] = output
	}
	// The dispatch declaration is discovery/flow truth. Start remains the sole
	// physical provisioner because its bounded work-queue policy also owns
	// MaxBytes, discard, max-age, and deduplication settings that the generic
	// port declaration cannot completely represent.
	return &Component{
		cfg:        cfg,
		natsClient: deps.NATSClient,
		mgr:        deps.LifecycleManager,
		metricsReg: deps.MetricsRegistry,
		logger:     deps.GetLoggerWithComponent(componentName),
		outputs:    outputs,
	}, nil
}

// Initialize self-registers the framework FanOut workflow when the config uses
// the default workflow name, so mgr.Watch resolves it. A consumer that points
// FanOutWorkflow at its own workflow is responsible for registering that one.
// The executor requires a lifecycle Manager (it is the Watch substrate); a nil
// Manager is a wiring error surfaced loudly here rather than a silent no-op.
func (c *Component) Initialize() error {
	if c.mgr == nil {
		return fmt.Errorf("gated-dag: LifecycleManager is required (the Watch re-eval/recovery substrate) but was not wired")
	}
	if c.cfg.FanOutWorkflow == FanOutWorkflow {
		if err := c.mgr.Register(WorkflowDeclaration()); err != nil && !errors.Is(err, lifecycle.ErrWorkflowAlreadyRegistered) {
			return fmt.Errorf("gated-dag: register FanOut workflow: %w", err)
		}
	}
	return nil
}

// Start builds the executor and begins the eval loop. Component instances are
// boot-only: after one successful Start, callers must construct a fresh
// Component for another process lifetime.
func (c *Component) Start(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, componentName, "Start", "context cannot be nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.exec != nil {
		return fmt.Errorf("gated-dag: component instance cannot be restarted; construct a fresh component")
	}
	if c.running {
		return fmt.Errorf("gated-dag: already running")
	}
	if c.natsClient == nil {
		return fmt.Errorf("gated-dag: NATS client is required")
	}
	if c.mgr == nil {
		return fmt.Errorf("gated-dag: LifecycleManager is required")
	}

	qTimeout, err := c.cfg.queryTimeout()
	if err != nil {
		return err
	}

	// Provision the durable dispatch stream (ADR-070): it captures DispatchSubject,
	// so a dispatch published via PublishToStreamWithAck is persisted and delivered
	// whenever a consumer (re)subscribes — a lost dispatch can no longer strand a
	// claimed unit. Idempotent (get-or-create). A bounded MaxAge keeps an
	// unconsumed backlog from growing forever (a work stream, not the graph —
	// ADR-068's no-TTL rule is about ENTITY_STATES).
	streamMaxAge, err := c.cfg.dispatchStreamMaxAge()
	if err != nil {
		return err
	}
	dedupeWindow, err := c.cfg.dispatchDedupeWindow()
	if err != nil {
		return err
	}
	streamDiscard, err := c.cfg.dispatchStreamDiscard()
	if err != nil {
		return err
	}
	streamRetention, err := c.cfg.dispatchStreamRetention()
	if err != nil {
		return err
	}
	stream, err := c.natsClient.EnsureStream(ctx, jetstream.StreamConfig{
		Name:     c.cfg.DispatchStream,
		Subjects: []string{c.cfg.DispatchSubject},
		MaxAge:   streamMaxAge,
		// Work-queue by default: a dispatch is deleted once acked, so MaxBytes below
		// is reached by genuine backlog rather than by processed history. Under
		// "limits" retention the ceiling would fill with acked dispatches and the
		// discard policy would refuse all new work on a healthy system — Validate
		// refuses that combination.
		Retention: streamRetention,
		// Both size bounds are DECLARED, not left to the server: EnsureStream
		// refuses to create an ordinary stream without them, and an unbounded work
		// stream exhausts the account's whole storage tier rather than only itself.
		MaxBytes:   c.cfg.DispatchStreamMaxBytes,
		Discard:    streamDiscard,
		Duplicates: dedupeWindow, // server-side dedup on Nats-Msg-Id=unitID (ADR-070 B1)
	})
	if err != nil {
		return fmt.Errorf("gated-dag: ensure dispatch stream %q for subject %q: %w",
			c.cfg.DispatchStream, c.cfg.DispatchSubject, err)
	}
	// EnsureStream is get-or-create, NOT reconcile: a pre-existing stream of the
	// same name (e.g. another gated-dag executor sharing the default
	// DispatchStream while using a different DispatchSubject) is returned
	// UNCHANGED. Fail loud at Start if it does not capture our subject — otherwise
	// every publish hits "no stream matches subject" and loops claim/rollback
	// (HIGH footgun). Give each distinct DispatchSubject a distinct DispatchStream.
	if info := stream.CachedInfo(); info != nil && !subjectCovered(c.cfg.DispatchSubject, info.Config.Subjects) {
		return fmt.Errorf("gated-dag: dispatch stream %q exists but does not capture subject %q (stream subjects: %v) — EnsureStream does not reconcile a pre-existing stream; set a distinct dispatch_stream per dispatch_subject",
			c.cfg.DispatchStream, c.cfg.DispatchSubject, info.Config.Subjects)
	}

	var stall stallPublisher
	if c.cfg.StallSubject != "" {
		stall = &natsStallPublisher{
			nc:               c.natsClient,
			subject:          c.cfg.StallSubject,
			fanOutWorkflow:   c.cfg.FanOutWorkflow,
			fanOutInstanceID: c.cfg.FanOutInstanceID,
		}
	}

	execMetrics := newMetrics(c.metricsReg)
	exec := &executor{
		cfg:     c.cfg,
		log:     c.logger,
		mgr:     c.mgr,
		nc:      c.natsClient,
		stall:   stall,
		metrics: execMetrics,
		reader: &natsGraphReader{
			nc:       c.natsClient,
			prefix:   c.cfg.UnitEntityPrefix,
			maxUnits: c.cfg.MaxUnits,
			timeout:  qTimeout,
			// Cold-start read (gh#420): short probes up to the existing query
			// timeout as the readiness budget — succeeds the moment graph-ingest is
			// up, instead of hanging the full timeout on the boot race.
			readyProbe:  natsclient.DefaultReadinessProbeTimeout,
			readyBudget: qTimeout,
			onTrunc: func(returned, capN int) {
				c.logger.Warn("gated-dag: unit set exceeded max_units; reading a truncated set",
					slog.Int("returned", returned), slog.Int("max_units", capN))
			},
			onNeverReady: func(err error) {
				execMetrics.coldStartWait.Inc()
				if natsclient.IsNoResponders(err) {
					c.logger.Warn("gated-dag: graph-ingest prefix responder never appeared within the "+
						"readiness budget — is graph-ingest deployed/subscribed? (gh#420)",
						slog.String("subject", prefixQuerySubject), slog.Any("error", err))
				} else {
					// Covers both cold-start failure modes: readiness budget exhausted
					// (responder slow/absent) OR a handler-error reply (responder up but
					// errored) — the latter stops the readiness loop and returns verbatim.
					c.logger.Warn("gated-dag: cold-start authoritative read failed before graph-ingest "+
						"was confirmed ready (budget exhausted, or the responder replied with an error) (gh#420)",
						slog.String("subject", prefixQuerySubject), slog.Any("error", err))
				}
			},
		},
		claimer: newNATSClaimer(c.natsClient, c.cfg.ClaimPredicate),
		pub:     &natsPublisher{nc: c.natsClient, subject: c.cfg.DispatchSubject, fanOutWorkflow: c.cfg.FanOutWorkflow},
	}
	if err := exec.start(ctx); err != nil {
		return fmt.Errorf("gated-dag: start executor: %w", err)
	}

	c.exec = exec
	c.running = true
	c.startTime = time.Now()
	c.logger.Info("gated-dag executor started",
		slog.String("fan_out_workflow", c.cfg.FanOutWorkflow),
		slog.String("unit_prefix", c.cfg.UnitEntityPrefix),
		slog.String("dispatch_subject", c.cfg.DispatchSubject),
		slog.Int("workers", c.cfg.Workers),
		slog.String("backstop", c.cfg.BackstopInterval))
	return nil
}

// subjectCovered reports whether want is captured by the stream's configured
// subjects. gated-dag uses concrete (non-wildcard) dispatch subjects, so an exact
// membership check suffices — a stream this executor created lists want exactly;
// a name-collision with another executor's subject fails the check.
func subjectCovered(want string, subjects []string) bool {
	for _, s := range subjects {
		if s == want {
			return true
		}
	}
	return false
}

// Stop gracefully stops the executor. A failed Stop consumes this component's
// cancellation authority and is terminal; a later Stop does not retry cleanup
// or report a false successful completion.
func (c *Component) Stop(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "LifecycleComponent", "Stop", "nil context")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.exec == nil {
		return nil
	}
	if !c.running {
		return nil
	}
	if c.exec.cancel == nil {
		return fmt.Errorf("gated-dag: Stop already claimed without observed completion")
	}
	if err := c.exec.stop(ctx); err != nil {
		return err
	}
	c.running = false
	c.logger.Info("gated-dag executor stopped")
	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        componentName,
		Type:        "processor",
		Description: "Gated-DAG dispatch executor (ADR-046 Phase 2)",
		Version:     "1.0.0",
	}
}

// InputPorts returns the input ports — none (re-eval rides the lifecycle Watch,
// not a configured port).
func (c *Component) InputPorts() []component.Port { return nil }

// OutputPorts returns the dispatch subject and required graph mutation port.
func (c *Component) OutputPorts() []component.Port {
	return append([]component.Port(nil), c.outputs...)
}

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema { return c.cfg.Schema() }

// Health returns current health status.
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	running, startTime := c.running, c.startTime
	healthy := running && c.exec != nil && c.exec.cancel != nil
	if healthy {
		select {
		case <-c.exec.done:
			healthy = false
		default:
		}
	}
	c.mu.RUnlock()

	status := "stopped"
	if healthy {
		status = "running"
	} else if running {
		status = "unhealthy"
	}
	return component.HealthStatus{
		Healthy:   healthy,
		LastCheck: time.Now(),
		Uptime:    time.Since(startTime),
		Status:    status,
	}
}

// DataFlow returns data-flow metrics (the executor is event/timer driven; rate
// metrics are exposed via the prometheus collectors in metrics.go).
func (c *Component) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{LastActivity: time.Now()}
}
