package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"

	"os"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/llm"
	"github.com/c360studio/semstreams/internal/lifecyclecleanup"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/persona"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/loopbucket"
	"github.com/c360studio/semstreams/processor/agentic-loop/prompt"
	"github.com/nats-io/nats.go/jetstream"
)

// schema is the configuration schema for agentic-loop, generated from Config struct tags
var schema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

const (
	taskIntakeRejectionLane   = "decoded-task"
	taskIntakeRejectionReason = "structural-invalid"
)

// Component implements the agentic-loop processor
type Component struct {
	config     Config
	handler    *MessageHandler
	deps       component.Dependencies
	decoder    *message.Decoder
	natsClient *natsclient.Client
	logger     *slog.Logger

	// Parsed timeout for message processing
	messageTimeout time.Duration

	// Lifecycle state
	mu             sync.RWMutex
	lifecycleMu    sync.Mutex
	lifecycleUsed  bool
	terminal       bool
	stopping       bool
	cleanupPending bool
	startDone      chan struct{}
	cancel         context.CancelFunc
	started        bool
	startTime      time.Time

	// KV buckets
	loopsBucket           jetstream.KeyValue
	trajectoryBucket      jetstream.KeyValue
	trajectoryRecorder    *trajectoryRecorder
	trajectoryReader      *trajectoryReader
	trajectoryAuditHealth trajectoryAuditHealth
	trajectoryAuditLoss   loopAuditLoss

	// Ports (merged from config)
	inputPorts  []component.Port
	outputPorts []component.Port

	// Track consumers for cleanup
	consumerInfos []consumerInfo
	consumers     []streamConsumerBinding

	// Query subscription for trajectory requests
	trajectorySub            requestSubscription
	inflightSub              requestSubscription
	initializeKVBucketsInput func(context.Context) error
	waitForStreamInput       func(context.Context, string) error
	consumeStream            func(context.Context, context.Context, natsclient.PortConsumerContext, natsclient.StreamConsumerConfig, func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error)
	subscribeRequests        func(context.Context, string, func(context.Context, []byte) ([]byte, error)) (requestSubscription, error)
	waitConsumerClosed       func(context.Context, <-chan struct{}) error
	settlementEvidence       loopSettlementEvidenceReader

	// Approval-timeout sweeper lifecycle. cancel is called from Stop
	// to terminate the goroutine; done is closed by the goroutine on
	// exit so Stop can synchronize cleanly. nil when no sweeper is
	// running (component not started, or no approval flow active).
	sweeperCancel context.CancelFunc
	sweeperDone   chan struct{}

	// Metrics
	metrics          *loopMetrics
	deliveryFatalErr error

	// Graph writer for model endpoint and loop execution entities
	graphWriter *graphWriter

	// pendingTaskResults retains the not-yet-published spawn result when a
	// transient lineage write NAKs the task. Redelivery first hits HandleTask's
	// active-loop dedup path, then resumes from this result so the original
	// agent.request is not silently lost. Protected by mu.
	pendingTaskResults map[string]HandlerResult

	// testPublishHook, if non-nil, is called by publishApprovalResponseToWire
	// in place of the real NATS publish. Used in unit tests to capture
	// wire-level approval-response messages without a NATS connection.
	// Always nil in production.
	testPublishHook func(subject string, data []byte)
	// testLineageWriteHook injects lineage-write outcomes without NATS. Always
	// nil in production.
	testLineageWriteHook func(context.Context, string, map[string]any) error
}

type streamConsumerBinding struct {
	handle       jetstream.ConsumeContext
	drainOnce    *sync.Once
	observerDone <-chan struct{}
}
type requestSubscription interface{ Drain(context.Context) error }

type inputHandler func(context.Context, []byte) (natsclient.DeliveryDecision, error)

func rejectRetiredConfig(rawConfig json.RawMessage) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawConfig, &fields); err != nil {
		return err
	}
	// Match the case-folded field spellings consumed by encoding/json below.
	for name, raw := range fields {
		switch {
		case strings.EqualFold(name, "loops_bucket"):
			return fmt.Errorf("retired field loops_bucket is not supported; configure ports.outputs named loops with config.bucket")
		case strings.EqualFold(name, "approval_timeout"):
			var duration string
			if string(raw) == "null" || json.Unmarshal(raw, &duration) != nil {
				return fmt.Errorf("approval_timeout %s must be a JSON string duration in (0,12h]", raw)
			}
		}
	}
	for _, retired := range []string{
		"content_bucket", "trajectory_detail", "trajectory_cache_ttl",
		"trajectories_bucket", "trajectory_ttl", "trajectory_history",
	} {
		if _, exists := fields[retired]; exists {
			return fmt.Errorf("retired field %q is not supported", retired)
		}
	}
	return nil
}

func validateTrajectoryQueryInput(port component.Port, facts component.PortFacts) error {
	contract, ok := facts.Interface()
	subjects := facts.NATSSubjects()
	if port.Name != "trajectory_query" || !port.Required || facts.Kind() != component.PortKindNATSRequest ||
		len(subjects) != 1 || subjects[0] == "" || strings.ContainsAny(subjects[0], "*>") ||
		!ok || contract.Type != "agentic.query" || contract.Version != "v1" {
		return fmt.Errorf("trajectory_query must be a required exact nats-request input with interface agentic.query v1")
	}
	return nil
}

func validateTrajectoriesOutput(port component.Port, facts component.PortFacts) error {
	contract, ok := facts.Interface()
	if port.Name != "trajectories" || !port.Required || facts.Kind() != component.PortKindKVWrite ||
		facts.ResourceID() != "kv:"+agentic.TrajectoryBucketName || !ok ||
		contract.Type != "agentic.trajectory.fact" || contract.Version != "v1" {
		return fmt.Errorf("trajectories must be a required AGENT_TRAJECTORIES kv-write output with interface agentic.trajectory.fact v1")
	}
	return nil
}

func (c *Component) trajectoryQuerySubject() (string, error) {
	for _, port := range c.inputPorts {
		if port.Name != "trajectory_query" {
			continue
		}
		facts, err := port.Facts()
		if err != nil {
			return "", err
		}
		if err := validateTrajectoryQueryInput(port, facts); err != nil {
			return "", err
		}
		return facts.NATSSubjects()[0], nil
	}
	return "", errors.New("trajectory_query input required")
}

func adaptVoidInputHandler(handler func(context.Context, []byte)) inputHandler {
	return func(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {
		handler(ctx, data)
		return natsclient.DeliveryDecisionAck, nil
	}
}

// consumerInfo tracks JetStream consumer details for cleanup
type consumerInfo struct {
	streamName   string
	consumerName string
	// subject is the FilterSubject this consumer was bound with. Recorded so
	// the in-flight query (gh#733) can find the consumer this component
	// actually bound instead of re-deriving its name — a second derivation is
	// a thing that can drift, and a recorded binding is not.
	subject string
}

// DeclarePorts is the component.PortDeclarer for agentic-loop: the ports
// NewComponent will report for rawConfig, computed without dependencies.
func DeclarePorts(rawConfig json.RawMessage, _ string) (component.PortConfig, error) {
	_, inputs, outputs, err := resolveConfig(rawConfig)
	if err != nil {
		return component.PortConfig{}, err
	}
	return component.PortConfigFrom(inputs, outputs), nil
}

// resolveConfig parses rawConfig over the defaults, validates, merges the port
// overrides, and resolves the effective ports with their per-port kind checks.
// It is the one derivation DeclarePorts and NewComponent share.
func resolveConfig(rawConfig json.RawMessage) (Config, []component.Port, []component.Port, error) {
	// Parse configuration — start from defaults so JSON only overrides
	// explicitly provided fields. Without this, zero-valued fields like
	// compact_threshold (0.0) and headroom_tokens (0) cause compaction
	// to trigger on every iteration regardless of context utilization.
	config := DefaultConfig()
	if err := rejectRetiredConfig(rawConfig); err != nil {
		return Config{}, nil, nil, errs.WrapInvalid(err, "agentic-loop", "NewComponent", "validate explicit config fields")
	}
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return Config{}, nil, nil, errs.WrapInvalid(err, "agentic-loop", "NewComponent", "parse config")
	}
	config.Consumer.EnsureDefaults()
	config.Context.EnsureDefaults()
	config.ToolCallGovernance.EnsureDefaults()

	// Validate configuration
	if err := config.Validate(); err != nil {
		return Config{}, nil, nil, errs.WrapInvalid(err, "agentic-loop", "NewComponent", "validate config")
	}

	merged := *DefaultConfig().Ports
	if config.Ports != nil {
		var err error
		merged, err = component.MergePortConfig(merged, *config.Ports)
		if err != nil {
			return Config{}, nil, nil, errs.WrapInvalid(err, "agentic-loop", "NewComponent", "merge ports")
		}
	}
	config.Ports = &merged
	inputPorts := make([]component.Port, 0, len(merged.Inputs))
	for _, definition := range merged.Inputs {
		port, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			return Config{}, nil, nil, errs.WrapInvalid(err, "agentic-loop", "NewComponent", "resolve input port")
		}
		facts, err := port.Facts()
		if err != nil {
			return Config{}, nil, nil, errs.WrapInvalid(err, "agentic-loop", "NewComponent", "inspect input port")
		}
		if definition.Name == "trajectory_query" {
			if err := validateTrajectoryQueryInput(port, facts); err != nil {
				return Config{}, nil, nil, errs.WrapInvalid(err, "agentic-loop", "NewComponent", "validate trajectory query input")
			}
		} else if facts.Kind() != component.PortKindJetStream {
			return Config{}, nil, nil, errs.WrapInvalid(errs.ErrInvalidConfig, "agentic-loop", "NewComponent", "work input ports must be JetStream ports")
		}
		inputPorts = append(inputPorts, port)
	}
	outputPorts := make([]component.Port, 0, len(merged.Outputs))
	for _, definition := range merged.Outputs {
		port, err := definition.Resolve(component.DirectionOutput)
		if err != nil {
			return Config{}, nil, nil, errs.WrapInvalid(err, "agentic-loop", "NewComponent", "resolve output port")
		}
		if definition.Name == "trajectories" {
			facts, factsErr := port.Facts()
			if factsErr != nil {
				return Config{}, nil, nil, errs.WrapInvalid(factsErr, "agentic-loop", "NewComponent", "inspect trajectories output")
			}
			if err := validateTrajectoriesOutput(port, facts); err != nil {
				return Config{}, nil, nil, errs.WrapInvalid(err, "agentic-loop", "NewComponent", "validate trajectories output")
			}
		}
		outputPorts = append(outputPorts, port)
	}
	if _, err := loopBucketName(outputPorts); err != nil {
		return Config{}, nil, nil, errs.WrapInvalid(err, "agentic-loop", "NewComponent", "validate loops output")
	}
	return config, inputPorts, outputPorts, nil
}

func loopBucketName(outputs []component.Port) (string, error) {
	for _, port := range outputs {
		if port.Name != "loops" {
			continue
		}
		facts, err := port.Facts()
		if err != nil {
			return "", fmt.Errorf("loops output: %w", err)
		}
		if port.Direction != component.DirectionOutput || facts.Kind() != component.PortKindKVWrite {
			return "", fmt.Errorf("loops output must be kv-write")
		}
		return strings.TrimPrefix(facts.ResourceID(), "kv:"), nil
	}
	return "", fmt.Errorf("loops kv-write output is required")
}

// NewComponent creates a new agentic-loop component
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	config, inputPorts, outputPorts, err := resolveConfig(rawConfig)
	if err != nil {
		return nil, err
	}

	// Parse timeout for message processing
	messageTimeout, err := time.ParseDuration(config.Timeout)
	if err != nil {
		return nil, errs.WrapInvalid(err, "agentic-loop", "NewComponent", "parse timeout format")
	}

	// Create handler with model registry if available
	var loopOpts []LoopManagerOption
	if deps.ModelRegistry != nil {
		loopOpts = append(loopOpts, WithLoopManagerModelRegistry(deps.ModelRegistry))
	}
	handler := NewMessageHandler(config, loopOpts...)
	handler.modelRegistry = deps.ModelRegistry
	handler.toolRegistry = deps.ToolRegistry

	// Subject-mode tool-call governance dispatcher (ADR-039). Always
	// constructed — when Mode is "disabled" (the default) the
	// dispatcher is a pass-through with no governance gate. A nil
	// NATSClient (rare — almost always test scaffolding) yields a
	// publisher-less dispatcher: disabled mode is unaffected;
	// audit/enforce skip the publish step and log at Debug.
	var verdictPublisher VerdictPublisher
	if deps.NATSClient != nil {
		verdictPublisher = deps.NATSClient
	}
	handler.SetGovernanceDispatcher(NewGovernanceDispatcher(
		config.ToolCallGovernance, verdictPublisher, deps.GetLogger(),
		getMetrics(deps.MetricsRegistry),
	))

	// Wire LLM-backed summarizer for context compaction if model registry is available
	if deps.ModelRegistry != nil && config.Context.Enabled {
		if summarizer, modelName := createSummarizer(deps, deps.GetLogger()); summarizer != nil {
			handler.SetSummarizer(summarizer, modelName)
		}
	}

	// Wire the per-iteration write_todos reader (ADR-036 Stage 4) so
	// every iteration's prompt prefix carries the current working
	// list. NATS-less deployments (rare — most tests stub the client)
	// silently skip the read.
	if deps.NATSClient != nil {
		handler.SetTodoReader(NewNATSTodoReader(deps.NATSClient))
		// Wire the brief-assembly lesson reader (ADR-080 push-based memory) so
		// every dispatch's system prompt carries the active lessons matching the
		// loop's scope. NATS-less deployments skip injection (nil reader).
		handler.SetLessonReader(NewNATSLessonReader(deps.NATSClient))
	}
	handler.SetPlatform(deps.Platform)
	handler.SetMetrics(getMetrics(deps.MetricsRegistry))

	comp := &Component{
		config:         config,
		handler:        handler,
		deps:           deps,
		decoder:        message.NewDecoder(deps.PayloadRegistry),
		natsClient:     deps.NATSClient,
		logger:         deps.GetLogger(),
		messageTimeout: messageTimeout,
		inputPorts:     inputPorts,
		outputPorts:    outputPorts,
		metrics:        getMetrics(deps.MetricsRegistry),
		graphWriter: &graphWriter{
			natsClient:    deps.NATSClient,
			modelRegistry: deps.ModelRegistry,
			platform:      deps.Platform,
			logger:        deps.GetLogger(),
		},
	}
	if deps.NATSClient != nil {
		comp.settlementEvidence = natsLoopSettlementEvidenceReader{client: deps.NATSClient}
	}

	return comp, nil
}

// createSummarizer resolves the summarization endpoint from the model registry
// and returns an LLM-backed Summarizer plus the resolved endpoint name.
// Returns (nil, "") if the endpoint cannot be resolved.
func createSummarizer(deps component.Dependencies, logger *slog.Logger) (Summarizer, string) {
	endpointName := deps.ModelRegistry.ResolveSummarization()
	if endpointName == "" {
		logger.Debug("no summarization endpoint available, using stub compactor")
		return nil, ""
	}

	ep := deps.ModelRegistry.GetEndpoint(endpointName) // modelresolveaudit:allow already-resolved (endpointName from ResolveSummarization is a real endpoint)
	if ep == nil {
		logger.Warn("summarization endpoint not found in registry", "endpoint", endpointName)
		return nil, ""
	}

	apiKey := ""
	if ep.APIKeyEnv != "" {
		apiKey = os.Getenv(ep.APIKeyEnv)
	}

	client, err := llm.NewOpenAIClient(llm.OpenAIConfig{
		BaseURL: ep.URL,
		Model:   ep.Model,
		APIKey:  apiKey,
		Logger:  logger,
	})
	if err != nil {
		logger.Warn("failed to create summarization LLM client", "error", err, "endpoint", endpointName)
		return nil, ""
	}

	logger.Info("context compaction using LLM summarizer", "endpoint", endpointName, "model", ep.Model)
	return NewLLMSummarizer(client, logger), endpointName
}

// Meta returns component metadata
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "agentic-loop",
		Type:        "processor",
		Description: "Orchestrates agentic loops with tool calls and trajectory tracking",
		Version:     "1.0.0",
	}
}

// InputPorts returns input port definitions
func (c *Component) InputPorts() []component.Port {
	return c.inputPorts
}

// OutputPorts returns output port definitions
func (c *Component) OutputPorts() []component.Port {
	return c.outputPorts
}

// ConfigSchema returns the configuration schema
func (c *Component) ConfigSchema() component.ConfigSchema {
	return schema
}

// Health returns current health status
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	healthy := c.started
	uptime := time.Duration(0)
	if c.started {
		uptime = time.Since(c.startTime)
	}

	status := "stopped"
	if healthy {
		status = "running"
	}
	errorCount, lastError := c.trajectoryAuditHealth.snapshot()
	if c.deliveryFatalErr != nil {
		healthy = false
		status = "delivery ownership lost"
		errorCount++
		lastError = c.deliveryFatalErr.Error()
	} else if healthy && (!c.trajectoryProviderAvailable() || errorCount > 0) {
		healthy = false
		status = "degraded"
		if !c.trajectoryProviderAvailable() && lastError == "" {
			lastError = boundedTrajectoryDiagnostic(fmt.Sprintf("trajectory evidence provider %q unavailable", c.config.TrajectoryEvidenceStorageInstance))
		}
	}

	return component.HealthStatus{
		Healthy:    healthy,
		LastCheck:  time.Now(),
		ErrorCount: errorCount,
		LastError:  lastError,
		Uptime:     uptime,
		Status:     status,
	}
}

// DataFlow returns current data flow metrics
func (c *Component) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{
		MessagesPerSecond: 0,
		BytesPerSecond:    0,
		ErrorRate:         0,
		LastActivity:      time.Now(),
	}
}

// Initialize prepares the component (no-op for this component)
func (c *Component) Initialize() error {
	return nil
}

// Start starts the component.
// The context is used for cancellation during startup operations.
func (c *Component) Start(ctx context.Context) (startErr error) {
	// Validate context
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "agentic-loop", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "agentic-loop", "Start", "context already cancelled")
	}

	c.lifecycleMu.Lock()
	if c.lifecycleUsed {
		c.lifecycleMu.Unlock()
		return errs.ErrAlreadyStarted
	}
	parent := ctx
	runCtx, cancel := context.WithCancel(ctx)
	startDone := make(chan struct{})
	c.lifecycleUsed = true
	c.cleanupPending = true
	c.cancel = cancel
	c.startDone = startDone
	c.lifecycleMu.Unlock()
	committed := false
	defer func() {
		if !committed {
			rollbackErr := lifecyclecleanup.RollbackFailedStart(parent, c.cleanup)
			startErr = errors.Join(startErr, rollbackErr)
			c.lifecycleMu.Lock()
			if rollbackErr == nil {
				c.cleanupPending = false
				c.terminal = true
				c.clearLifecycleHandles()
			}
			close(startDone)
			c.startDone = nil
			c.lifecycleMu.Unlock()
			return
		}
		c.lifecycleMu.Lock()
		c.cleanupPending = false
		close(startDone)
		c.startDone = nil
		c.lifecycleMu.Unlock()
	}()

	// Initialize KV buckets if NATS client available
	if c.natsClient != nil {
		if err := c.initializeKVBucketsForStart(runCtx); err != nil {
			return errs.Wrap(err, "agentic-loop", "Start", "initialize KV buckets")
		}
		if err := c.restoreApprovalDeadlines(runCtx); err != nil {
			return errs.Wrap(err, "agentic-loop", "Start", "restore approval deadlines")
		}

		// Set up NATS subscriptions for input ports.
		if err := c.setupSubscriptions(runCtx, runCtx); err != nil {
			return errs.Wrap(err, "agentic-loop", "Start", "setup subscriptions")
		}

		// Set up trajectory query handler from the declared exact request input.
		querySubject, err := c.trajectoryQuerySubject()
		if err != nil {
			return errs.Wrap(err, "agentic-loop", "Start", "resolve trajectory query input")
		}
		subscribe := func(ctx context.Context, subject string, handler func(context.Context, []byte) ([]byte, error)) (requestSubscription, error) {
			return c.natsClient.SubscribeForRequests(ctx, subject, handler)
		}
		if c.subscribeRequests != nil {
			subscribe = c.subscribeRequests
		}
		sub, err := subscribe(runCtx, querySubject, c.handleTrajectoryQuery)
		if err != nil {
			return errs.Wrap(err, "agentic-loop", "Start", "subscribe to trajectory query")
		}
		c.lifecycleMu.Lock()
		c.trajectorySub = sub
		c.lifecycleMu.Unlock()

		// Set up in-flight query handler (gh#733). Same wire as the trajectory
		// query: the answer is served, never the consumer name it is derived from.
		inflightSub, err := subscribe(runCtx,
			InFlightQuerySubjectFor(c.config.ConsumerNameSuffix), c.handleInFlightQuery)
		if err != nil {
			return errs.Wrap(err, "agentic-loop", "Start", "subscribe to in-flight query")
		}
		c.lifecycleMu.Lock()
		c.inflightSub = inflightSub
		c.lifecycleMu.Unlock()
	}

	c.mu.Lock()
	c.started = true
	c.startTime = time.Now()
	c.mu.Unlock()

	// Start the approval-timeout sweeper. Derives a sub-context so
	// Stop can terminate the sweeper independently of whatever
	// passed in `ctx`. Cheap when no loops await approval — the
	// snapshot is a single map iteration under read-lock.
	//
	// Capture the done channel locally before launching the goroutine
	// — Stop nils c.sweeperDone before waiting on it, and the
	// goroutine's deferred close needs a stable reference that
	// survives that nilling.
	sweepCtx, cancelSweep := context.WithCancel(runCtx)
	done := make(chan struct{})
	c.mu.Lock()
	c.sweeperCancel = cancelSweep
	c.sweeperDone = done
	c.mu.Unlock()
	go func() {
		defer close(done)
		c.runApprovalTimeoutSweeper(sweepCtx)
	}()

	// Build the prompt-assembly registry: framework-universal + role
	// defaults from prompt.DefaultFragments, overridden by any product-
	// supplied personas in the PERSONAS KV bucket. See ADR-029 step 3b.
	// Best-effort — failure to open the bucket logs and proceeds with
	// defaults only. Nil NATSClient paths (pure unit tests) skip persona
	// loading silently.
	c.initPromptRegistry(runCtx)

	// Emit model endpoint entities to graph (non-fatal)
	if c.graphWriter != nil {
		c.graphWriter.WriteModelEndpoints(runCtx)
	}
	committed = true

	return nil
}

// initPromptRegistry seeds the handler's prompt.Registry with
// DefaultFragments and wires a persona.Manager as the live KV-backed
// override source. The handler refreshes from the source on every
// prompt build so runtime edits (CRUD tool calls that Create/Update a
// persona) take effect on the next loop without a component restart.
// Failures to open the PERSONAS bucket downgrade cleanly to defaults-
// only with a log; nil NATSClient paths (pure unit tests) skip persona
// wiring silently.
func (c *Component) initPromptRegistry(ctx context.Context) {
	reg := prompt.NewRegistry()
	reg.AddAll(prompt.DefaultFragments())
	c.handler.SetPromptRegistry(reg)

	if c.natsClient == nil {
		return
	}
	mgr, err := persona.NewManager(c.natsClient)
	if err != nil {
		c.logger.Debug("persona overrides disabled; using DefaultFragments only",
			slog.Any("error", err))
		return
	}

	// Seed once so the first loop sees whatever's already in the bucket
	// at boot (pre-populated fixtures, prior-run state after restart).
	// Subsequent loops pick up edits via the refresh path in the handler.
	if fragments, fragErr := mgr.Fragments(ctx); fragErr != nil {
		c.logger.Warn("failed to seed persona overrides; live refresh still active",
			slog.Any("error", fragErr))
	} else if len(fragments) > 0 {
		reg.UpsertAll(fragments)
		c.logger.Info("persona overrides seeded", slog.Int("count", len(fragments)))
	}

	c.handler.SetPersonaFragments(mgr)
}

// Stop stops the component within the given timeout.
func (c *Component) Stop(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "LifecycleComponent", "Stop", "nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for {
		c.lifecycleMu.Lock()
		if !c.lifecycleUsed {
			c.lifecycleUsed, c.terminal = true, true
			c.lifecycleMu.Unlock()
			return nil
		}
		if c.terminal {
			c.lifecycleMu.Unlock()
			return nil
		}
		if c.startDone != nil {
			done := c.startDone
			c.lifecycleMu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if c.stopping {
			c.lifecycleMu.Unlock()
			return errs.WrapTransient(errors.New("stop already in progress"), "agentic-loop", "Stop", "concurrent Stop")
		}
		retryable := c.cleanupPending
		c.stopping = true
		c.lifecycleMu.Unlock()
		stopErr := c.cleanup(ctx)
		c.lifecycleMu.Lock()
		c.stopping = false
		if retryable && stopErr != nil {
			c.lifecycleMu.Unlock()
			return stopErr
		}
		c.cleanupPending, c.terminal = false, true
		c.clearLifecycleHandles()
		c.lifecycleMu.Unlock()
		c.mu.Lock()
		c.started = false
		c.mu.Unlock()
		return stopErr
	}
}

func (c *Component) cleanup(ctx context.Context) error {
	c.mu.RLock()
	cancelSweep := c.sweeperCancel
	done := c.sweeperDone
	c.mu.RUnlock()
	c.lifecycleMu.Lock()
	trajectorySub := c.trajectorySub
	inflightSub := c.inflightSub
	c.lifecycleMu.Unlock()
	var cleanupErr error
	if trajectorySub != nil {
		if err := trajectorySub.Drain(ctx); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		} else {
			c.lifecycleMu.Lock()
			if c.trajectorySub == trajectorySub {
				c.trajectorySub = nil
			}
			c.lifecycleMu.Unlock()
		}
	}
	if inflightSub != nil {
		if err := inflightSub.Drain(ctx); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		} else {
			c.lifecycleMu.Lock()
			if c.inflightSub == inflightSub {
				c.inflightSub = nil
			}
			c.lifecycleMu.Unlock()
		}
	}
	for i := range c.consumers {
		binding := &c.consumers[i]
		binding.drain()
		closed := binding.handle.Closed()
		if c.waitConsumerClosed != nil {
			cleanupErr = errors.Join(cleanupErr, c.waitConsumerClosed(ctx, closed))
		} else {
			select {
			case <-closed:
			case <-ctx.Done():
				cleanupErr = errors.Join(cleanupErr, ctx.Err())
			}
		}
	}
	if cancelSweep != nil {
		cancelSweep()
	}
	if c.cancel != nil {
		c.cancel()
	}
	for i := range c.consumers {
		if observerDone := c.consumers[i].observerDone; observerDone != nil {
			select {
			case <-observerDone:
			case <-ctx.Done():
				cleanupErr = errors.Join(cleanupErr, ctx.Err())
			}
		}
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("wait for approval-timeout sweeper: %w", ctx.Err()))
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		cleanupErr = errors.Join(cleanupErr, ctxErr)
	}
	return cleanupErr
}

func (c *Component) clearLifecycleHandles() {
	c.consumers = nil
	c.cancel = nil
	c.trajectorySub = nil
	c.inflightSub = nil
	c.mu.Lock()
	c.sweeperCancel = nil
	c.sweeperDone = nil
	c.consumerInfos = nil
	c.mu.Unlock()
}

// initializeKVBucketsForStart preserves the startup injection seam without changing direct initializer calls.
func (c *Component) initializeKVBucketsForStart(ctx context.Context) error {
	if c.initializeKVBucketsInput != nil {
		return c.initializeKVBucketsInput(ctx)
	}
	return c.initializeKVBuckets(ctx)
}

// initializeKVBuckets initializes the KV buckets for loop and trajectory storage.
func (c *Component) initializeKVBuckets(ctx context.Context) error {
	js, err := c.natsClient.JetStream()
	if err != nil {
		return errs.WrapTransient(err, "agentic-loop", "initializeKVBuckets", "get JetStream")
	}

	name, err := loopBucketName(c.outputPorts)
	if err != nil {
		return errs.WrapInvalid(err, "agentic-loop", "initializeKVBuckets", "resolve loops output")
	}
	if err := c.config.Validate(); err != nil {
		return err
	}
	loopsBucket, err := loopbucket.AcquireOwner(ctx, js, name)
	if err != nil {
		return errs.Wrap(err, "agentic-loop", "initializeKVBuckets", "admit loop authority")
	}
	c.loopsBucket = loopsBucket

	// Immutable trajectory facts are best-effort audit state. A missing bucket
	// degrades observability but must not prevent the work consumers from starting.
	trajectoryBucket, trajectoryErr := js.KeyValue(ctx, agentic.TrajectoryBucketName)
	if errors.Is(trajectoryErr, jetstream.ErrBucketNotFound) {
		trajectoryBucket, trajectoryErr = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
			Bucket:  agentic.TrajectoryBucketName,
			History: 1,
		})
	}
	if trajectoryErr == nil {
		trajectoryErr = validateTrajectoryFactBucket(ctx, trajectoryBucket)
	}
	if trajectoryErr != nil {
		// Clean beta policy: an incompatible retained bucket is never written,
		// reconciled, or shimmed. The operator wipes it and restarts.
		trajectoryBucket = nil
	}
	c.trajectoryBucket = trajectoryBucket
	if trajectoryBucket != nil {
		c.trajectoryRecorder = newTrajectoryRecorder(
			trajectoryBucket,
			c.deps.StoreRegistry,
			c.config.TrajectoryEvidenceStorageInstance,
			c.reportTrajectoryAuditFailure,
		)
		c.trajectoryReader = newTrajectoryReader(trajectoryBucket)
	} else {
		c.trajectoryRecorder = nil
		c.trajectoryReader = nil
		// No recorder means nothing is ever attempted, so no loop can
		// produce a per-loop audit failure to observe — while every loop's
		// evidence is in fact missing. Latch the loss for every loop this
		// process will terminate, or total evidence loss would emit a graph
		// byte-identical to a healthy one. The report below carries no
		// LoopID and cannot do this job.
		c.trajectoryAuditLoss.observeAllLoops()
	}
	if trajectoryErr != nil {
		c.reportTrajectoryAuditFailure(trajectoryAuditFailure{
			Stage:  trajectoryStageFactVerify,
			Kind:   agentic.TrajectoryKindLoopStarted,
			Reason: trajectoryReasonBackend,
			Err:    fmt.Errorf("acquire trajectory fact bucket: %w", trajectoryErr),
		})
	}
	if !c.trajectoryProviderAvailable() {
		c.reportTrajectoryAuditFailure(trajectoryAuditFailure{
			Stage:  trajectoryStageProviderResolve,
			Kind:   agentic.TrajectoryKindLoopStarted,
			Reason: trajectoryReasonProviderUnavailable,
			Err:    fmt.Errorf("storage instance %q unavailable", c.config.TrajectoryEvidenceStorageInstance),
		})
	}

	return nil
}

func validateTrajectoryFactBucket(ctx context.Context, bucket jetstream.KeyValue) error {
	status, err := bucket.Status(ctx)
	if err != nil {
		return fmt.Errorf("read AGENT_TRAJECTORIES status: %w", err)
	}
	return validateTrajectoryFactBucketContract(status.History(), status.TTL())
}

func validateTrajectoryFactBucketContract(history int64, ttl time.Duration) error {
	if history != 1 || ttl != 0 {
		return fmt.Errorf(
			"AGENT_TRAJECTORIES has incompatible retained state (history=%d TTL/MaxAge=%s); clean break required: stop this component, wipe AGENT_TRAJECTORIES, and restart",
			history, ttl,
		)
	}
	return nil
}

// setupSubscriptions sets up JetStream consumers for input ports
func (c *Component) setupSubscriptions(setupCtx, consumerCtx context.Context) error {
	for _, port := range c.inputPorts {
		// Exact request/reply inputs own their own subscription lifecycle below;
		// they are not JetStream work consumers.
		if port.Name == "trajectory_query" {
			continue
		}
		facts, err := port.Facts()
		if err != nil {
			return err
		}
		stream, ok := facts.Stream()
		if !ok || len(stream.Subjects()) != 1 {
			return fmt.Errorf("input port %s must declare one JetStream subject", port.Name)
		}
		subject := stream.Subjects()[0]

		var (
			handler         inputHandler
			settleHandlerFn func(context.Context, string, []byte) (natsclient.DeliveryDecision, error)
		)

		// Route to appropriate handler based on port name
		switch port.Name {
		case "agent.task":
			handler = c.taskInputHandler(30 * time.Minute)
		case "agent.response":
			handler = c.handleResponseMessage
		case "tool.result":
			handler = c.handleToolResultMessage
		case "agent.signal":
			settleHandlerFn = func(ctx context.Context, _ string, data []byte) (natsclient.DeliveryDecision, error) {
				return c.handleSignalMessage(ctx, data)
			}
		case "agent.approval_response":
			settleHandlerFn = func(ctx context.Context, _ string, data []byte) (natsclient.DeliveryDecision, error) {
				return c.handleApprovalResponseMessage(ctx, data)
			}
		case "agent.toolcall.approved", "agent.toolcall.rejected":
			// Both ports validate the registered verdict against its actual subject.
			settleHandlerFn = c.handleToolCallVerdictMessage
		default:
			c.logger.Warn("Unknown input port", "port", port.Name)
			continue
		}

		if err := c.setupConsumer(setupCtx, consumerCtx, port, subject, handler, settleHandlerFn); err != nil {
			return errs.Wrap(err, "agentic-loop", "setupSubscriptions", fmt.Sprintf("setup consumer for %s", subject))
		}
	}

	return nil
}

// setupConsumer sets up a JetStream consumer for an input port.
func (c *Component) setupConsumer(
	setupCtx context.Context,
	consumerCtx context.Context,
	port component.Port,
	subject string,
	handler inputHandler,
	settleHandlerFn func(context.Context, string, []byte) (natsclient.DeliveryDecision, error),
) error {
	facts, err := port.Facts()
	if err != nil {
		return err
	}
	stream, ok := facts.Stream()
	if !ok {
		return fmt.Errorf("input port %s is not JetStream", port.Name)
	}
	streamName := stream.Name()

	// Wait for stream to be available
	waitForStream := c.waitForStream
	if c.waitForStreamInput != nil {
		waitForStream = c.waitForStreamInput
	}
	if err := waitForStream(setupCtx, streamName); err != nil {
		return errs.WrapTransient(err, "agentic-loop", "setupConsumer", fmt.Sprintf("wait for stream %s", streamName))
	}

	// Create durable consumer name
	consumerName := fmt.Sprintf("agentic-loop-%s", sanitizeSubject(subject))
	if c.config.ConsumerNameSuffix != "" {
		consumerName = consumerName + "-" + c.config.ConsumerNameSuffix
	}

	c.logger.Info("Setting up JetStream consumer",
		"stream", streamName,
		"consumer", consumerName,
		"filter_subject", subject,
		"port", port.Name)

	// Get consumer config from port (allows user configuration)
	// Defaults to "new" - only process new messages, don't replay old ones
	consumerCfg, componentMaxAckPending, consumerErr := agenticLoopConsumerPolicy(port)
	if consumerErr != nil {
		return errs.WrapInvalid(consumerErr, "agentic-loop", "setupConsumer", "resolve consumer config")
	}

	// Differentiate consumer config by latency class:
	// - Long-running ports (task, response, tool.result) need serial processing,
	//   heartbeats, and graduated backoff to handle LLM-scale latency.
	// - Fast ports (signal) keep short timeouts and higher concurrency.
	var (
		ackWait           time.Duration
		maxAckPending     int
		maxDeliver        int
		msgTimeout        time.Duration
		backOff           []time.Duration
		useHeartbeat      bool
		heartbeatInterval time.Duration
	)

	switch port.Name {
	case "agent.task":
		ackWait = c.config.Consumer.ParsedAckWait()
		maxAckPending = componentMaxAckPending
		maxDeliver = c.config.Consumer.MaxDeliver
		// The task adapter (taskInputHandler) owns the ordinary 30m work
		// deadline; the outer callback stays lifecycle-bound so a timed-out
		// task is attributed as a work error, not an outer cancellation.
		msgTimeout = 30 * time.Minute
		backOff = []time.Duration{30 * time.Second, 2 * time.Minute}
		useHeartbeat = true
		heartbeatInterval = c.config.Consumer.ParsedHeartbeatInterval()
	case "agent.response", "tool.result":
		ackWait = c.config.Consumer.ParsedAckWait()
		maxAckPending = componentMaxAckPending
		maxDeliver = c.config.Consumer.MaxDeliver
		msgTimeout = 30 * time.Minute
		backOff = []time.Duration{30 * time.Second, 2 * time.Minute}
		useHeartbeat = true
		heartbeatInterval = c.config.Consumer.ParsedHeartbeatInterval()
	default: // agent.signal — fast, advisory
		ackWait = 30 * time.Second
		maxAckPending = componentMaxAckPending
		maxDeliver = consumerCfg.MaxDeliver
		msgTimeout = c.messageTimeout
		useHeartbeat = false
	}

	cfg := natsclient.StreamConsumerConfig{
		StreamName:     streamName,
		ConsumerName:   consumerName,
		FilterSubject:  subject,
		DeliverPolicy:  consumerCfg.DeliverPolicy,
		AckPolicy:      consumerCfg.AckPolicy,
		MaxDeliver:     maxDeliver,
		AckWait:        ackWait,
		MaxAckPending:  maxAckPending,
		BackOff:        backOff,
		AutoCreate:     false,
		MessageTimeout: msgTimeout,
		// agent.task applies its 30m ordinary-work deadline in taskInputHandler;
		// its outer context stays lifecycle-bound so the adapter's deadline is
		// the single authority on task-work timeout attribution.
		DisableMessageTimeout: port.Name == "agent.task",
	}
	var (
		handlerFn func(context.Context, jetstream.Msg)
		admission *deliveryLaneAdmission
	)
	if useHeartbeat {
		policy, policyErr := newLoopHeartbeatDeliveryPolicy(setupCtx, cfg, heartbeatInterval, port.Name, handler)
		if policyErr != nil {
			return policyErr
		}
		admission = newDeliveryLaneAdmission(c.recordDeliveryOwnerFatal)
		handlerFn = func(msgCtx context.Context, msg jetstream.Msg) {
			result, admitted := consumeAdmittedDelivery(msgCtx, msg, policy, admission)
			if admitted && result.Err() != nil && !result.OwnerStopRequired() {
				c.logger.Error("Message handler error", "port", port.Name, "error", result.Err())
			}
		}
	} else {
		if settleHandlerFn == nil {
			return errs.WrapInvalid(fmt.Errorf("input port %q has no typed settlement handler", port.Name),
				"agentic-loop", "setupConsumer", "missing settlement handler")
		}
		admission = newDeliveryLaneAdmission(c.recordDeliveryOwnerFatal)
		handlerFn = func(msgCtx context.Context, msg jetstream.Msg) {
			if !admission.admit() {
				return
			}
			deliveredSubject := msg.Subject()
			decision, cause := runLoopDeliveryWork(msgCtx, msg.Data(), func(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {
				return settleHandlerFn(ctx, deliveredSubject, data)
			})
			result := natsclient.SettleDelivery(msg, decision, cause)
			admission.latch(result)
			if result.Err() != nil && !result.OwnerStopRequired() {
				c.logger.Error("Message delivery did not settle cleanly", "port", port.Name, "error", result.Err())
			}
		}
	}

	consume := c.natsClient.ConsumeStreamWithConfigContexts
	if c.consumeStream != nil {
		consume = c.consumeStream
	}
	handle, err := consume(setupCtx, consumerCtx, natsclient.PortConsumerContext{Component: c.Meta().Name, Port: port.Name, ComponentOwned: true}, cfg, handlerFn)
	if err != nil {
		return errs.Wrap(err, "agentic-loop", "setupConsumer", fmt.Sprintf("setup consumer for stream %s", streamName))
	}

	binding := newStreamConsumerBinding(handle)
	if admission != nil {
		c.observeDeliveryLane(consumerCtx, &binding, admission, port.Name)
	}
	// Track consumer for cleanup in Stop()
	c.lifecycleMu.Lock()
	c.consumers = append(c.consumers, binding)
	c.lifecycleMu.Unlock()
	c.mu.Lock()
	c.consumerInfos = append(c.consumerInfos, consumerInfo{
		streamName:   streamName,
		consumerName: consumerName,
		subject:      subject,
	})
	c.mu.Unlock()

	c.logger.Info("Subscribed (JetStream)",
		"subject", subject,
		"stream", streamName,
		"consumer", consumerName,
		"port", port.Name)
	return nil
}

func agenticLoopConsumerPolicy(port component.Port) (component.ConsumerConfig, int, error) {
	consumerConfig, err := component.GetConsumerConfig(port)
	if err != nil {
		return component.ConsumerConfig{}, 0, err
	}
	fixed := 10
	if port.Name == "agent.task" || port.Name == "agent.response" || port.Name == "tool.result" {
		fixed = 1
	}
	if consumerConfig.MaxAckPending != 0 {
		return component.ConsumerConfig{}, fixed, errs.WrapInvalid(
			fmt.Errorf("port %q max_ack_pending is component-owned at %d", port.Name, fixed),
			"agentic-loop", "consumerPolicy", "component-owned consumer policy")
	}
	return consumerConfig, fixed, nil
}

func validateLoopRetryPolicy(portName string, cfg natsclient.StreamConsumerConfig) error {
	if cfg.MaxDeliver >= len(cfg.BackOff) {
		return nil
	}
	return errs.WrapInvalid(
		fmt.Errorf("max_deliver %d is below required minimum %d for fixed BackOff", cfg.MaxDeliver, len(cfg.BackOff)),
		"agentic-loop",
		"setupConsumer",
		fmt.Sprintf("validate delivery policy for port %s", portName),
	)
}

func newLoopHeartbeatDeliveryPolicy(
	ctx context.Context,
	cfg natsclient.StreamConsumerConfig,
	heartbeatInterval time.Duration,
	portName string,
	handler inputHandler,
) (natsclient.HeartbeatDeliveryPolicy, error) {
	if err := validateLoopRetryPolicy(portName, cfg); err != nil {
		return natsclient.HeartbeatDeliveryPolicy{}, err
	}
	retryPolicy, err := natsclient.DelayedDeliveryRetry(30 * time.Second)
	if err != nil {
		return natsclient.HeartbeatDeliveryPolicy{}, errs.WrapInvalid(
			err, "agentic-loop", "setupConsumer", "construct delivery retry policy",
		)
	}
	policy, err := natsclient.ValidateHeartbeatDeliveryPolicy(
		ctx,
		cfg,
		heartbeatInterval,
		retryPolicy,
		func(workCtx context.Context, _ natsclient.DeliveryAttempt, data []byte) (natsclient.DeliveryDecision, error) {
			return handler(workCtx, data)
		},
	)
	if err != nil {
		return natsclient.HeartbeatDeliveryPolicy{}, errs.WrapInvalid(
			err,
			"agentic-loop",
			"setupConsumer",
			fmt.Sprintf("validate heartbeat delivery policy for port %s", portName),
		)
	}
	return policy, nil
}

// waitForStream waits for a JetStream stream to be available
func (c *Component) waitForStream(ctx context.Context, streamName string) error {
	js, err := c.natsClient.JetStream()
	if err != nil {
		return errs.WrapTransient(err, "agentic-loop", "waitForStream", "get JetStream context")
	}

	maxRetries := 30
	retryInterval := 100 * time.Millisecond
	maxInterval := 2 * time.Second

	for i := 0; i < maxRetries; i++ {
		_, err := js.Stream(ctx, streamName)
		if err == nil {
			return nil
		}
		if i < maxRetries-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryInterval):
				retryInterval = min(retryInterval*2, maxInterval)
			}
		}
	}

	return errs.WrapTransient(
		fmt.Errorf("stream %s not found after %d retries", streamName, maxRetries),
		"agentic-loop",
		"waitForStream",
		"find stream",
	)
}

// sanitizeSubject converts a subject pattern to a valid consumer name suffix
func sanitizeSubject(subject string) string {
	s := strings.ReplaceAll(subject, ".", "-")
	s = strings.ReplaceAll(s, ">", "all")
	s = strings.ReplaceAll(s, "*", "any")
	return s
}

// taskInputHandler wraps handleTaskMessage with the ordinary per-task work
// deadline. The consumer callback context stays lifecycle-bound (see
// setupConsumer's DisableMessageTimeout for agent.task); this adapter owns
// the work timeout so a timed-out task is attributed as a work error rather
// than an outer-callback cancellation.
func (c *Component) taskInputHandler(workTimeout time.Duration) inputHandler {
	return func(consumerCtx context.Context, data []byte) (natsclient.DeliveryDecision, error) {
		workCtx, cancel := context.WithTimeout(consumerCtx, workTimeout)
		defer cancel()
		decision, err := c.handleTaskMessage(workCtx, data)
		if err == nil && workCtx.Err() != nil {
			return natsclient.DeliveryDecisionRetry, workCtx.Err()
		}
		return decision, err
	}
}

// handleTaskMessage processes incoming task messages
func (c *Component) handleTaskMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {
	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
		return natsclient.DeliveryDecisionTerminate, fmt.Errorf("decode task message: %w", err)
	}

	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	if !ok {
		return natsclient.DeliveryDecisionTerminate,
			fmt.Errorf("unexpected task payload type %T", baseMsg.Payload())
	}
	related, hasLineage, err := c.preflightDecodedTask(task)
	if err != nil {
		if c.metrics != nil {
			c.metrics.recordTaskIntakeRejection(taskIntakeRejectionLane, taskIntakeRejectionReason)
		}
		return natsclient.DeliveryDecisionTerminate, err
	}

	c.logger.Debug("Processing task message",
		slog.String("task_id", task.TaskID),
		slog.String("role", task.Role),
		slog.String("model", task.Model))

	// Recover only when process-local correlation is absent. A cold delivery
	// first validates the committed task-to-loop mapping, then either reuses the
	// retained initial request or rebuilds that ordinary at-least-once output
	// from the same TaskMessage. No additional progress state is created.
	result := HandlerResult{}
	if _, active := c.handler.loopManager.HasActiveLoopForTask(task.TaskID); !active {
		result, err = c.recoverTaskDelivery(ctx, *task)
		if err != nil {
			return loopSettlementDecision(err), err
		}
		if result.LoopID != "" && result.State.IsTerminal() {
			return natsclient.DeliveryDecisionAck, nil
		}
	}
	if result.LoopID == "" {
		result, err = c.handler.HandleTask(ctx, *task)
	}
	if err != nil {
		return loopSettlementDecision(err),
			fmt.Errorf("handle task %q: %w", task.TaskID, err)
	}

	if !result.Created {
		pending, ok := c.pendingTaskResult(task.TaskID, result.LoopID)
		if !ok {
			c.logger.Debug("Task deduplicated — loop already active",
				slog.String("loop_id", result.LoopID),
				slog.String("task_id", task.TaskID))
			return natsclient.DeliveryDecisionAck, nil
		}
		result = pending
		c.logger.Debug("Resuming task after transient lineage-write NAK",
			slog.String("loop_id", result.LoopID),
			slog.String("task_id", task.TaskID))
	}

	c.logger.Debug("Loop created",
		slog.String("loop_id", result.LoopID),
		slog.String("task_id", task.TaskID))
	c.recordTrajectoryObservations(ctx, result)

	// Birth the loop-execution entity via entity.create. This gives the entity a
	// typed MessageType envelope and a proper origin contract.
	//
	// WriteSpawnIdentity returns an error on genuine birth failure (not
	// already-exists — idempotent re-spawn is fine). A failed birth means graph
	// semantics are NOT intact for this loop: subsequent completion/failure/
	// trajectory writes would reference an absent entity. We treat this as a hard precondition failure and halt the loop
	// so it enters a clean failure state rather than silently producing
	// unattributed graph mutations.
	//
	// Stamp cross-arc lineage triples (Metadata[MetadataKeyRelatedLoops]
	// set by rule.executePublishAgent from rule.Action.RelatedLoops)
	// on the same entity in a separate atomic batch. Downstream rules
	// read both families via the existing $entity.triple.<predicate>
	// substitution. No-op when the producer didn't set RelatedLoops.
	if c.graphWriter != nil {
		if err := c.graphWriter.WriteSpawnIdentity(ctx, result.LoopID, task); err != nil {
			c.logger.Error("graph_writer: loop-execution entity birth failed — halting loop spawn",
				"loop_id", result.LoopID, "task_id", task.TaskID, "error", err)
			entity, _ := c.handler.GetLoop(result.LoopID)
			return c.handleSpawnIdentityFailure(ctx, result.LoopID, entity, err)
		}
		if hasLineage {
			if err := c.writeLineageTriples(ctx, result.LoopID, related); err != nil {
				if errs.IsTransient(err) {
					c.rememberPendingTaskResult(task.TaskID, result)
					c.logger.Warn("graph_writer: transient lineage write failed — task will be redelivered",
						"loop_id", result.LoopID, "task_id", task.TaskID, "error", err)
					return natsclient.DeliveryDecisionRetry, err
				}
				c.clearPendingTaskResult(task.TaskID, result.LoopID)
				c.logger.Error("graph_writer: lineage write failed — halting loop spawn",
					"loop_id", result.LoopID, "task_id", task.TaskID, "error", err)
				entity, _ := c.handler.GetLoop(result.LoopID)
				return c.handleSpawnIdentityFailure(ctx, result.LoopID, entity, err)
			}
		}
	}
	// Record creation only after graph birth succeeds. Birth failures of any
	// class record creation inside handleSpawnIdentityFailure immediately
	// before the failure path so its active-loop decrement remains balanced.
	if c.metrics != nil {
		c.metrics.recordLoopCreated()
	}

	// Commit the loop before publishing its ordinary at-least-once outputs.
	// A redelivery can then recover the exact task-to-loop mapping rather than
	// treating an empty process map as proof that the task is stale.
	if err := c.persistLoopState(ctx, result.LoopID); err != nil {
		c.rememberPendingTaskResult(task.TaskID, result)
		return natsclient.DeliveryDecisionRetry, fmt.Errorf("persist loop state: %w", err)
	}
	c.rememberPendingTaskResult(task.TaskID, result)
	if err := c.publishResults(ctx, result); err != nil {
		return natsclient.DeliveryDecisionRetry, err
	}
	c.clearPendingTaskResult(task.TaskID, result.LoopID)
	return natsclient.DeliveryDecisionAck, nil
}

func (c *Component) writeLineageTriples(ctx context.Context, loopID string, related map[string]any) error {
	if c.testLineageWriteHook != nil {
		return c.testLineageWriteHook(ctx, loopID, related)
	}
	return c.graphWriter.WriteLineageTriples(ctx, loopID, related)
}

func (c *Component) rememberPendingTaskResult(taskID string, result HandlerResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pendingTaskResults == nil {
		c.pendingTaskResults = make(map[string]HandlerResult)
	}
	c.pendingTaskResults[taskID] = result
}

func (c *Component) pendingTaskResult(taskID, loopID string) (HandlerResult, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result, ok := c.pendingTaskResults[taskID]
	return result, ok && result.LoopID == loopID
}

func (c *Component) clearPendingTaskResult(taskID, loopID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	result, ok := c.pendingTaskResults[taskID]
	if ok && result.LoopID == loopID {
		delete(c.pendingTaskResults, taskID)
	}
}

func (c *Component) preflightDecodedTask(task *agentic.TaskMessage) (map[string]any, bool, error) {
	if err := task.Validate(); err != nil {
		return nil, false, errs.WrapInvalid(err, "agentic-loop", "handleTaskMessageWithLifecycle", "validate decoded task")
	}
	related, hasLineage, err := normalizedRelatedLoops(task.Metadata)
	if err != nil {
		return nil, false, errs.WrapInvalid(err, "agentic-loop", "handleTaskMessageWithLifecycle", "decode related_loops metadata")
	}
	if !hasLineage || len(related) == 0 {
		return related, hasLineage, nil
	}

	prospectiveSubject, err := agentic.TryLoopExecutionEntityID(
		c.deps.Platform.Org, c.deps.Platform.Platform, task.LoopID)
	if err != nil {
		return nil, false, errs.WrapInvalid(err, "agentic-loop", "handleTaskMessageWithLifecycle", "construct prospective lineage subject")
	}
	if _, err := buildLineageTriples(prospectiveSubject, related); err != nil {
		return nil, false, errs.WrapInvalid(err, "agentic-loop", "handleTaskMessageWithLifecycle", "preflight prospective lineage batch")
	}
	return related, true, nil
}

func normalizedRelatedLoops(metadata map[string]any) (map[string]any, bool, error) {
	raw, present := metadata[agentic.MetadataKeyRelatedLoops]
	if !present {
		return nil, false, nil
	}
	switch related := raw.(type) {
	case map[string]any:
		return related, true, nil
	case map[string]string:
		normalized := make(map[string]any, len(related))
		for key, value := range related {
			normalized[key] = value
		}
		return normalized, true, nil
	default:
		return nil, true, fmt.Errorf("metadata %q must be an object, got %T", agentic.MetadataKeyRelatedLoops, raw)
	}
}

// handleSpawnIdentityFailure routes a loop-execution birth failure into the
// loop's terminal business-failure lane.
//
// A typed graph.StateContractError (wire code graph_state_reset_required)
// means THIS loop's entity is poisoned — the code is per-entity, not a
// component-wide graph outage (poison-response-scoping D9). The loop fails
// with the typed error preserved in its failure record so operators and
// downstream rules see the graph_state_reset_required code; task intake and
// other loops continue unaffected. Repairing the entity (delete + recreate)
// lets the next spawn of that entity succeed without a component restart.
//
// Ordinary operational birth failures take the same path under the
// pre-existing spawn_identity_birth_failed reason.
func (c *Component) handleSpawnIdentityFailure(ctx context.Context, loopID string, entity agentic.LoopEntity, err error) (natsclient.DeliveryDecision, error) {
	defer c.releaseLoopTransientState(loopID)
	reason := "spawn_identity_birth_failed"
	if graph.IsStateContractError(err) {
		err = graph.ClassifyStateContractError(err)
		reason = graph.ErrorCodeGraphStateResetRequired
		c.logger.Error("loop touched poisoned authoritative entity state; failing this loop (task intake continues)",
			"loop_id", loopID,
			"code", graph.ErrorCodeGraphStateResetRequired,
			"class", errs.ErrorFatal.String(),
			"error", err)
	}
	// Graph birth can fail before ordinary task intake's initial checkpoint.
	// Establish its validated running authority without overwriting another owner.
	var revision uint64
	if c.loopsBucket != nil {
		if validateErr := entity.Validate(); validateErr != nil {
			return natsclient.DeliveryDecisionQuarantine, errs.WrapFatal(validateErr, "agentic-loop", "handleSpawnIdentityFailure", "validate birth record")
		}
		if entity.State != agentic.LoopStateRunning {
			return natsclient.DeliveryDecisionRetry, fmt.Errorf("loop %q is no longer a running birth", loopID)
		}
		data, marshalErr := json.Marshal(entity)
		if marshalErr != nil {
			return natsclient.DeliveryDecisionTerminate, errs.WrapInvalid(marshalErr,
				"agentic-loop", "handleSpawnIdentityFailure", "marshal initial loop")
		}
		var createErr error
		revision, createErr = c.loopsBucket.Create(ctx, loopID, data)
		if errors.Is(createErr, jetstream.ErrKeyExists) {
			current, observed, readErr := c.readLoopEntityRevision(ctx, loopID)
			if readErr != nil {
				return loopSettlementDecision(readErr), readErr
			}
			if observed == 0 || !reflect.DeepEqual(current, entity) {
				return natsclient.DeliveryDecisionRetry, fmt.Errorf("loop %q birth authority changed", loopID)
			}
			revision = observed
		} else if createErr != nil {
			return loopSettlementDecision(createErr), createErr
		}
	}
	// Record creation immediately before the failure path so the failure
	// path's active-loop decrement remains balanced (creation is otherwise
	// recorded only after a successful graph birth).
	if c.metrics != nil && entity.ID != "" {
		c.metrics.recordLoopCreated()
	}
	if settleErr := c.handleLoopFailure(ctx, loopID, entity, reason, err, revision); settleErr != nil {
		return loopSettlementDecision(settleErr), settleErr
	}
	return natsclient.DeliveryDecisionAck, nil
}

// handleResponseMessage processes incoming agent response messages
func (c *Component) handleResponseMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {
	response, err := c.extractAgentResponse(data)
	if err != nil {
		return loopSettlementDecision(err), err
	}
	entity, revision, err := c.ensureResponseLoop(ctx, *response)
	if err != nil {
		return loopSettlementDecision(err), err
	}
	loopID := entity.ID
	if entity.State.IsTerminal() {
		// Process state reaches terminal before the final durable marker. Read
		// authority even on a warm mapping so a concurrently redelivered
		// response cannot treat speculative process state as committed proof.
		durable, found, readErr := c.readLoopEntity(ctx, loopID)
		if readErr != nil {
			return loopSettlementDecision(readErr), readErr
		}
		if !found || !durable.State.IsTerminal() {
			return natsclient.DeliveryDecisionRetry,
				fmt.Errorf("terminal marker for response %q is not yet observable", response.RequestID)
		}
		// The exact current retained AgentRequest and final durable marker
		// validated by ensureResponseLoop and observed here together prove that
		// this model response was already applied.
		c.releaseLoopTransientState(loopID)
		return natsclient.DeliveryDecisionAck, nil
	}

	result, err := c.handler.handleModelResponse(ctx, loopID, *response, c.recoverGovernance)
	if err != nil {
		c.recordTrajectoryObservations(ctx, result)
		// A handler can return a prepared business-terminal failure (for
		// example, its ordinary loop timeout) alongside the cause. Settle that
		// prepared terminal result through the normal effects-first/final-marker
		// lane rather than attempting a second failure transition.
		if result.State.IsTerminal() && result.FailureState != nil {
			if persistErr := c.persistHandlerResult(ctx, result, revision); persistErr != nil {
				c.releaseLoopTransientState(loopID)
				return loopSettlementDecision(persistErr), persistErr
			}
			c.recordTerminalState(result, entity, failureReasonForHandlerError(err))
			return natsclient.DeliveryDecisionAck, nil
		}
		// Iteration exhaustion is a declared loop business failure. Lifecycle
		// cancellation, transient dependencies, and invariant errors are not:
		// discard their speculative process state and preserve the classified
		// retry/quarantine outcome without emitting terminal effects.
		if !errors.Is(err, ErrMaxIterationsReached) {
			c.releaseLoopTransientState(loopID)
			return loopSettlementDecision(err), err
		}
		if settleErr := c.handleLoopFailure(ctx, loopID, entity, failureReasonForHandlerError(err), err, revision); settleErr != nil {
			return loopSettlementDecision(settleErr), settleErr
		}
		return natsclient.DeliveryDecisionAck, nil
	}

	c.recordResponseMetrics(response)
	if err := c.persistHandlerResult(ctx, result, revision); err != nil {
		c.releaseLoopTransientState(loopID)
		return loopSettlementDecision(err), err
	}
	if result.State.IsTerminal() {
		failureReason := "unknown"
		switch response.Status {
		case agentic.StatusError:
			failureReason = "model_error"
		case agentic.StatusLengthTruncated:
			failureReason = "length_truncated"
		}
		c.recordTerminalState(result, entity, failureReason)
	}
	return natsclient.DeliveryDecisionAck, nil
}

// failureReasonForHandlerError classifies a HandleModelResponse error into
// the loop-terminal failure reason handleLoopFailure publishes. gh#529: every
// iteration-budget-exhaustion detection path must agree on the reason
// "max_iterations" — matched via errors.Is against the typed sentinel
// ErrMaxIterationsReached, never by string-matching err.Error(). All other
// handler errors keep the pre-existing generic "handler_error" reason.
func failureReasonForHandlerError(err error) string {
	if errors.Is(err, ErrMaxIterationsReached) {
		return "max_iterations"
	}
	return "handler_error"
}

// extractAgentResponse parses an agent response message and finds its loop.
// Returns the response and loop ID or a classified decode/correlation error.
func (c *Component) extractAgentResponse(data []byte) (*agentic.AgentResponse, error) {
	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
		return nil, errs.WrapInvalid(err, "agentic-loop", "extractAgentResponse", "decode response")
	}

	responsePtr, ok := baseMsg.Payload().(*agentic.AgentResponse)
	if !ok {
		return nil, errs.WrapInvalid(
			fmt.Errorf("unexpected response payload type %T", baseMsg.Payload()),
			"agentic-loop", "extractAgentResponse", "validate payload type",
		)
	}
	if err := responsePtr.Validate(); err != nil {
		return nil, errs.WrapInvalid(err, "agentic-loop", "extractAgentResponse", "validate response")
	}

	c.logger.Debug("Decoded model response",
		slog.String("request_id", responsePtr.RequestID),
		slog.String("status", responsePtr.Status))

	return responsePtr, nil
}

// handleLoopFailure records failure metrics and publishes failure events.
func (c *Component) handleLoopFailure(ctx context.Context, loopID string, entity agentic.LoopEntity, reason string, err error, revision uint64) error {
	// Keep the failed transition process-local until every required terminal
	// effect completes. The bare terminal LoopEntity is the final applied
	// marker; a failed attempt releases speculative state so redelivery reads
	// the prior nonterminal record.
	if transErr := c.handler.loopManager.TransitionLoop(loopID, agentic.LoopStateFailed); transErr == nil {
		c.handler.loopManager.UpdateCompletion(loopID, agentic.OutcomeFailed, "", err.Error())
	} else {
		return errs.WrapFatal(
			transErr, "agentic-loop", "handleLoopFailure", "impossible failure transition",
		)
	}
	defer c.releaseLoopTransientState(loopID)

	if err := c.publishFailureEvents(ctx, loopID, reason, err.Error(), revision); err != nil {
		return err
	}
	if c.metrics != nil && entity.ID != "" {
		duration := time.Since(entity.StartedAt).Seconds()
		c.metrics.recordLoopFailed(reason, entity.Iterations, duration)
	}
	c.logger.Error("Loop processing failed", "error", err, "loop_id", loopID, "reason", reason)
	return nil
}

// publishFailureEvents uses the existing bounded finalizer and selected terminal owner.
func (c *Component) publishFailureEvents(ctx context.Context, loopID, reason, errorMsg string, revision uint64) error {
	errorCtx, cancel := natsclient.DetachContextWithTrace(ctx, 5*time.Second)
	defer cancel()
	failure, messages, err := c.handler.BuildFailureMessages(loopID, reason, errorMsg)
	if err != nil {
		return fmt.Errorf("build failure event for loop %s: %w", loopID, err)
	}
	return c.persistHandlerResult(errorCtx, HandlerResult{LoopID: loopID, State: agentic.LoopStateFailed,
		FailureState: failure, PublishedMessages: messages}, revision)
}

// recordResponseMetrics records metrics and logs for a successful response.
func (c *Component) recordResponseMetrics(response *agentic.AgentResponse) {
	if c.metrics == nil {
		return
	}

	c.metrics.recordIteration()
	c.metrics.recordTrajectoryStep("model_call")
	c.metrics.recordRequestTokens(response.TokenUsage.PromptTokens, response.TokenUsage.CompletionTokens)

	// Record dispatched tool calls
	if response.Status == "tool_call" {
		for _, toolCall := range response.Message.ToolCalls {
			c.metrics.recordToolCallDispatched(toolCall.Name)
		}
	}
}

// recordTerminalState fires the active_loops decrement and matching terminal
// counter only after the final bare LoopEntity marker commits. No-op for
// non-terminal states. Both response and tool-result owners call it after
// persistHandlerResult so a failed pre-marker attempt cannot emit a positive
// terminal signal or decrement the gauge twice on redelivery.
func (c *Component) recordTerminalState(result HandlerResult, entity agentic.LoopEntity, failureReason string) {
	if c.metrics == nil || entity.ID == "" {
		return
	}
	duration := time.Since(entity.StartedAt).Seconds()
	switch result.State {
	case agentic.LoopStateComplete:
		c.metrics.recordLoopCompleted(entity.Iterations, duration)
		c.logger.Info("Loop completed",
			slog.String("loop_id", result.LoopID),
			slog.Int("iterations", entity.Iterations))
	case agentic.LoopStateFailed:
		c.metrics.recordLoopFailed(failureReason, entity.Iterations, duration)
		c.logger.Warn("Loop failed",
			slog.String("loop_id", result.LoopID),
			slog.Int("iterations", entity.Iterations),
			slog.String("reason", failureReason))
	}
}

// graphWritePublishBudget supplies the cooperative deadline for best-effort
// completion/failure graph evidence and settlement-required synthetic evidence.
// Each writeTriple inside the writer has its own 5s graphWriterTimeout with
// retry. Dependencies must honor cancellation; this synchronous joined call
// deliberately does not add a watchdog that masks missing lifecycle control.
//
// 2s is generous for healthy graph-gateway (a typical completion stamps
// ~10-15 triples in well under a second). When expiry is observed after the
// joined call returns, we emit a Prom counter so operators can dashboard it.
// Completion and failure evidence remains nonblocking; synthetic evidence
// fails the joined delivery. Tighten if production sees significant tail; widen only
// after confirming the writer's retry budget is the actual bottleneck.
const graphWritePublishBudget = 2 * time.Second

// persistHandlerResult publishes messages and persists state from a handler result.
//
// The terminal-state branch reorders graph writes BEFORE publishResults
// so any subscriber consuming agent.complete.<loop_id> from JetStream
// can immediately walk loop-entity triples (agent.loop.parent etc.)
// without racing the writer. Pre-fix order had publishResults first,
// which meant a fast subscriber could resolve ancestry against a
// missing parent triple. Concrete consumer was semteams ADR-038 PR B
// chain.evidence.* (project_open_work_2026_05_08.md bug class 4).
//
// A required pre-marker persistence or publication failure discards the
// speculative process-local terminal state so redelivery can cold-read the
// prior nonterminal record and exact retained evidence.
func (c *Component) persistHandlerResult(ctx context.Context, result HandlerResult, revision uint64) error {
	if result.State.IsTerminal() {
		var candidate message.Payload
		switch {
		case result.State == agentic.LoopStateComplete && result.CompletionState != nil:
			candidate = result.CompletionState
		case result.State == agentic.LoopStateFailed && result.FailureState != nil:
			candidate = result.FailureState
		}
		_, err := c.persistTerminalOutcome(ctx, result, candidate, revision)
		return err
	}
	c.recordHandlerResultTrajectory(ctx, result)
	if err := c.persistLoopState(ctx, result.LoopID); err != nil {
		return err
	}
	return c.publishResults(ctx, result)
}

// persistTerminalOutcome completes the selected effects before its source-correlated final marker.
func (c *Component) persistTerminalOutcome(ctx context.Context, result HandlerResult, candidate message.Payload, revision uint64) (natsclient.DeliveryDecision, error) {
	defer c.releaseLoopTransientState(result.LoopID)
	marker, err := c.handler.GetLoop(result.LoopID)
	if err != nil {
		return loopSettlementDecision(err), err
	}
	if err := marker.Validate(); err != nil {
		return natsclient.DeliveryDecisionQuarantine, errs.WrapFatal(err, "agentic-loop", "persistTerminalOutcome", "validate prepared terminal")
	}
	// Local validity is necessary but does not bind this payload to its prepared marker.
	if result.State != marker.State || !marker.State.IsTerminal() {
		return natsclient.DeliveryDecisionQuarantine, errs.WrapFatal(errors.New("terminal result conflicts with prepared state"),
			"agentic-loop", "persistTerminalOutcome", "validate terminal candidate")
	}
	switch prepared := candidate.(type) {
	case *agentic.LoopCompletedEvent:
		if prepared == nil || marker.State != agentic.LoopStateComplete || marker.Outcome != agentic.OutcomeSuccess || marker.Result != prepared.Result {
			return natsclient.DeliveryDecisionQuarantine, errs.WrapFatal(errors.New("completion conflicts with prepared marker"), "agentic-loop", "persistTerminalOutcome", "validate terminal candidate")
		}
	case *agentic.LoopFailedEvent:
		if prepared == nil || marker.State != agentic.LoopStateFailed || marker.Error != prepared.Error ||
			(marker.Outcome != agentic.OutcomeFailed && marker.Outcome != agentic.OutcomeTruncated) {
			return natsclient.DeliveryDecisionQuarantine, errs.WrapFatal(errors.New("failure conflicts with prepared marker"), "agentic-loop", "persistTerminalOutcome", "validate terminal candidate")
		}
	case *agentic.LoopCancelledEvent:
		if prepared == nil || marker.State != agentic.LoopStateCancelled || marker.Outcome != agentic.OutcomeCancelled || marker.CancelledBy != prepared.CancelledBy {
			return natsclient.DeliveryDecisionQuarantine, errs.WrapFatal(errors.New("cancellation conflicts with prepared marker"), "agentic-loop", "persistTerminalOutcome", "validate terminal candidate")
		}
	default:
		return natsclient.DeliveryDecisionQuarantine, errs.WrapFatal(errors.New("missing terminal payload"), "agentic-loop", "persistTerminalOutcome", "validate terminal candidate")
	}
	if c.loopsBucket != nil {
		current, observed, err := c.readLoopEntityRevision(ctx, result.LoopID)
		if err != nil {
			return loopSettlementDecision(err), err
		}
		if revision == 0 || observed != revision || current.State.IsTerminal() {
			return natsclient.DeliveryDecisionRetry, fmt.Errorf("terminal authority for loop %q changed or is not observable", result.LoopID)
		}
		if current.TaskID != marker.TaskID {
			return natsclient.DeliveryDecisionQuarantine, errs.WrapFatal(errors.New("terminal task identity conflicts with authority"),
				"agentic-loop", "persistTerminalOutcome", "terminal correlation conflict")
		}
	}
	selected, err := c.selectTerminalOutcome(ctx, result.LoopID, marker.TaskID, candidate)
	if err != nil {
		return loopSettlementDecision(err), err
	}
	port := "agent.complete"
	switch saved := selected.(type) {
	case *agentic.LoopCompletedEvent:
		prepared, ok := candidate.(*agentic.LoopCompletedEvent)
		if !ok || marker.State != agentic.LoopStateComplete || marker.Outcome != agentic.OutcomeSuccess || marker.Result != saved.Result ||
			prepared.Result != saved.Result || !reflect.DeepEqual(prepared.Decision, saved.Decision) {
			return natsclient.DeliveryDecisionRetry, fmt.Errorf("selected success for loop %q lacks this delivery's compatible applied proof", result.LoopID)
		}
		marker.Result, marker.Error, marker.CompletedAt = saved.Result, "", saved.CompletedAt
		result.CompletionState, result.FailureState = saved, nil
	case *agentic.LoopFailedEvent:
		prepared, ok := candidate.(*agentic.LoopFailedEvent)
		if !ok || marker.State != agentic.LoopStateFailed || marker.Error != saved.Error ||
			(marker.Outcome != agentic.OutcomeFailed && marker.Outcome != agentic.OutcomeTruncated) ||
			prepared.Reason != saved.Reason || prepared.Error != saved.Error {
			return natsclient.DeliveryDecisionRetry, fmt.Errorf("selected failure for loop %q lacks this delivery's compatible applied proof", result.LoopID)
		}
		// A truncated marker intentionally accompanies an ordinary failed event.
		marker.Result, marker.Error, marker.CompletedAt = "", saved.Error, saved.FailedAt
		result.CompletionState, result.FailureState = nil, saved
		port = "agent.failed"
	case *agentic.LoopCancelledEvent:
		prepared, ok := candidate.(*agentic.LoopCancelledEvent)
		if !ok || marker.State != agentic.LoopStateCancelled || marker.Outcome != agentic.OutcomeCancelled || prepared.CancelledBy != saved.CancelledBy {
			return natsclient.DeliveryDecisionRetry, fmt.Errorf("selected cancellation for loop %q lacks this delivery's compatible applied proof", result.LoopID)
		}
		marker.Result, marker.Error, marker.CompletedAt = "", "cancelled by user", saved.CancelledAt
		marker.CancelledBy, marker.CancelledAt = saved.CancelledBy, saved.CancelledAt
	}
	if err := marker.Validate(); err != nil {
		return natsclient.DeliveryDecisionQuarantine, errs.WrapFatal(err, "agentic-loop", "persistTerminalOutcome", "validate final terminal")
	}
	result.State, result.SyntheticDecide = marker.State, nil
	// From here approval retains its existing refusal after unknown effects.
	if cancelled, ok := selected.(*agentic.LoopCancelledEvent); ok {
		c.recordTerminalObservation(ctx, result.LoopID, agentic.TrajectoryStatusCancelled, "",
			trajectoryTerminalEvidence{Loop: marker, Cancelled: cancelled})
	} else {
		c.recordHandlerResultTrajectory(ctx, result)
	}
	switch saved := selected.(type) {
	case *agentic.LoopCompletedEvent:
		_ = c.stampLoopCompletionWithBudget(ctx, result.LoopID, saved)
		if saved.SyntheticDecideRequired {
			if err := c.stampSyntheticDecideWithBudget(ctx, &SyntheticDecideRequest{LoopID: saved.LoopID, Reason: saved.Result}); err != nil {
				return natsclient.DeliveryDecisionQuarantine, err
			}
		}
	case *agentic.LoopFailedEvent:
		_ = c.stampLoopFailureWithBudget(ctx, result.LoopID, saved)
	case *agentic.LoopCancelledEvent:
		if c.graphWriter != nil {
			c.graphWriter.WriteLoopCancellation(ctx, saved, c.trajectoryAuditLoss.observed(result.LoopID))
			if err := ctx.Err(); err != nil {
				return natsclient.DeliveryDecisionQuarantine, err
			}
		}
	}
	subject, err := component.ResolveSubject(c.config.Ports.Outputs, port, result.LoopID)
	if err != nil {
		return natsclient.DeliveryDecisionQuarantine, errs.WrapFatal(err, "agentic-loop", "persistTerminalOutcome", "resolve selected terminal subject")
	}
	data, err := json.Marshal(message.NewBaseMessage(selected.Schema(), selected, "agentic-loop"))
	if err != nil {
		return natsclient.DeliveryDecisionQuarantine, errs.WrapFatal(err, "agentic-loop", "persistTerminalOutcome", "marshal selected terminal")
	}
	result.PublishedMessages = []PublishedMessage{{Subject: subject, Data: data}}
	if err := c.publishResults(ctx, result); err != nil {
		return natsclient.DeliveryDecisionQuarantine, err
	}
	if c.loopsBucket == nil {
		return natsclient.DeliveryDecisionAck, nil // Existing unit seam, not production durability proof.
	}
	data, err = json.Marshal(marker)
	if err != nil {
		return natsclient.DeliveryDecisionQuarantine, errs.WrapFatal(err, "agentic-loop", "persistTerminalOutcome", "marshal terminal marker")
	}
	if _, err := c.loopsBucket.Update(ctx, result.LoopID, data, revision); err != nil {
		return loopSettlementDecision(err), err
	}
	return natsclient.DeliveryDecisionAck, nil
}

// selectTerminalOutcome preserves the existing ordinary terminal payload representation.
func (c *Component) selectTerminalOutcome(ctx context.Context, loopID, taskID string, candidate message.Payload) (message.Payload, error) {
	if candidate == nil {
		return nil, errs.WrapFatal(errors.New("missing terminal payload"), "agentic-loop", "selectTerminalOutcome", "validate terminal candidate")
	}
	if err := candidate.Validate(); err != nil {
		return nil, errs.WrapFatal(err, "agentic-loop", "selectTerminalOutcome", "validate terminal candidate")
	}
	data, err := json.Marshal(candidate)
	if err != nil {
		return nil, errs.WrapFatal(err, "agentic-loop", "selectTerminalOutcome", "marshal terminal candidate")
	}
	var header agentic.LoopCompletedEvent
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, errs.WrapFatal(err, "agentic-loop", "selectTerminalOutcome", "decode terminal candidate")
	}
	category := candidate.Schema().Category
	if header.LoopID != loopID || header.TaskID != taskID ||
		!((category == agentic.CategoryLoopCompleted && header.Outcome == agentic.OutcomeSuccess) ||
			(category == agentic.CategoryLoopFailed && header.Outcome == agentic.OutcomeFailed) ||
			(category == agentic.CategoryLoopCancelled && header.Outcome == agentic.OutcomeCancelled)) {
		return nil, errs.WrapFatal(errors.New("terminal identity or category/outcome conflict"), "agentic-loop", "selectTerminalOutcome", "validate terminal candidate")
	}
	if c.loopsBucket == nil {
		return candidate, nil
	}
	key := "COMPLETE_" + loopID
	if _, err := c.loopsBucket.Create(ctx, key, data); err == nil {
		return candidate, nil
	} else if !errors.Is(err, jetstream.ErrKeyExists) {
		return nil, err
	}
	entry, err := c.loopsBucket.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	data = entry.Value()
	header = agentic.LoopCompletedEvent{}
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, errs.WrapFatal(err, "agentic-loop", "selectTerminalOutcome", "decode saved terminal")
	}
	if header.LoopID != loopID || header.TaskID != taskID {
		return nil, errs.WrapFatal(errors.New("saved terminal identity conflict"), "agentic-loop", "selectTerminalOutcome", "validate saved terminal")
	}
	var selected message.Payload
	switch header.Outcome {
	case agentic.OutcomeSuccess:
		selected = &agentic.LoopCompletedEvent{}
	case agentic.OutcomeFailed:
		selected = &agentic.LoopFailedEvent{}
	case agentic.OutcomeCancelled:
		selected = &agentic.LoopCancelledEvent{}
	default:
		return nil, errs.WrapFatal(fmt.Errorf("invalid saved outcome %q", header.Outcome), "agentic-loop", "selectTerminalOutcome", "validate saved terminal")
	}
	if err := json.Unmarshal(data, selected); err != nil {
		return nil, errs.WrapFatal(err, "agentic-loop", "selectTerminalOutcome", "decode saved terminal")
	}
	if err := selected.Validate(); err != nil {
		return nil, errs.WrapFatal(err, "agentic-loop", "selectTerminalOutcome", "validate saved terminal")
	}
	return selected, nil
}

// stampLoopCompletionWithBudget invokes the best-effort completion graph batch
// under graphWritePublishBudget. A missed stamp remains observable but never
// blocks the authoritative terminal state, publication, final marker, or ACK.
func (c *Component) stampLoopCompletionWithBudget(ctx context.Context, loopID string, completion *agentic.LoopCompletedEvent) error {
	if c.graphWriter == nil {
		return nil
	}
	// Read the observed-audit-loss answer HERE, on the component that owns
	// it, and hand the writer the result. loopAuditLoss answers for both
	// scopes at once — this loop's own observed failures, and the
	// component-wide latch for a process that cannot record evidence at all
	// — so this seam cannot honour half the fact. Nothing re-derives it from
	// the counter. Every terminal observation for this loop has already been
	// recorded (recordHandlerResultTrajectory returns only after its batch
	// goroutine joins or its budget expires and reports synchronously), so
	// the answer is final by the time the stamp is built.
	evidenceIncomplete := c.trajectoryAuditLoss.observed(loopID)
	var writeErr error
	timedOut := runWithBudget(ctx, graphWritePublishBudget, func(bctx context.Context) {
		writeErr = c.graphWriter.WriteLoopCompletion(bctx, completion, evidenceIncomplete)
	})
	if timedOut {
		c.logger.Warn("graph write budget expired before completion stamp returned",
			"loop_id", loopID,
			"budget", graphWritePublishBudget,
			"state", "complete")
		if c.metrics != nil {
			c.metrics.recordGraphWritePublishTimeout("complete")
			c.metrics.recordGraphEvidenceFailure("complete", "timeout")
		}
		return nil
	}
	if writeErr != nil {
		c.logger.Warn("completion graph evidence write failed; terminal settlement continues",
			"loop_id", loopID, "error", writeErr)
		if c.metrics != nil {
			c.metrics.recordGraphEvidenceFailure("complete", "write_error")
		}
	}
	return nil
}

// stampSyntheticDecideWithBudget invokes WriteSyntheticDecide under the
// graphWritePublishBudget. Records a Prom timeout when the budget
// expires before the writer returns; publication is withheld. Same
// shape as stampLoopCompletionWithBudget — the synthetic-decide triples
// must reach the graph before downstream rules wake on the
// agent.complete.* event, otherwise the recovery rule fires before
// coordinator.next_action="needs_clarification" is visible.
func (c *Component) stampSyntheticDecideWithBudget(ctx context.Context, req *SyntheticDecideRequest) error {
	if c.graphWriter == nil {
		return nil
	}
	var writeErr error
	timedOut := runWithBudget(ctx, graphWritePublishBudget, func(bctx context.Context) {
		writeErr = c.graphWriter.WriteSyntheticDecide(bctx, req.LoopID, req.Reason)
	})
	if timedOut {
		c.logger.Warn("graph write budget expired before synthetic decide stamp returned",
			"loop_id", req.LoopID,
			"budget", graphWritePublishBudget,
			"state", "synthetic_decide")
		if c.metrics != nil {
			c.metrics.recordGraphWritePublishTimeout("synthetic_decide")
		}
		return fmt.Errorf("synthetic decide graph stamp for loop %s did not complete within lifecycle budget: %w",
			req.LoopID, errors.Join(context.DeadlineExceeded, ctx.Err()))
	}
	if writeErr != nil {
		return fmt.Errorf("synthetic decide graph stamp for loop %s: %w", req.LoopID, writeErr)
	}
	return nil
}

// stampLoopFailureWithBudget mirrors the nonblocking completion evidence write
// for the failure branch. The atomic batch remains useful graph evidence but is
// not authoritative loop state and is not a final-marker prerequisite.
func (c *Component) stampLoopFailureWithBudget(ctx context.Context, loopID string, failure *agentic.LoopFailedEvent) error {
	if c.graphWriter == nil {
		return nil
	}
	evidenceIncomplete := c.trajectoryAuditLoss.observed(loopID)
	var writeErr error
	timedOut := runWithBudget(ctx, graphWritePublishBudget, func(bctx context.Context) {
		writeErr = c.graphWriter.WriteLoopFailure(bctx, failure, evidenceIncomplete)
	})
	if timedOut {
		c.logger.Warn("graph write budget expired before failure stamp returned",
			"loop_id", loopID,
			"budget", graphWritePublishBudget,
			"state", "failure")
		if c.metrics != nil {
			c.metrics.recordGraphWritePublishTimeout("failure")
			c.metrics.recordGraphEvidenceFailure("failure", "timeout")
		}
		return nil
	}
	if writeErr != nil {
		c.logger.Warn("failure graph evidence write failed; terminal settlement continues",
			"loop_id", loopID, "error", writeErr)
		if c.metrics != nil {
			c.metrics.recordGraphEvidenceFailure("failure", "write_error")
		}
	}
	return nil
}

// runWithBudget runs fn synchronously under a bounded child context. The caller
// never observes completion while delivery-derived work is still live. Graph
// dependencies are lifecycle participants and must honor cancellation.
//
// Extracted so the timeout-vs-completion contract is unit-testable
// without mocking the natsclient or graphWriter. The function is
// deliberately small: testing it covers cooperative expiry and joined work;
// testing the reorder-before-publish behavior is left to e2e:agentic
// where the full graph-stamp-then-publish path runs against real
// NATS and subscribers can observe the ordering effect.
func runWithBudget(ctx context.Context, budget time.Duration, fn func(context.Context)) (timedOut bool) {
	bctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	fn(bctx)
	return bctx.Err() != nil
}

// handleToolResultMessage processes incoming tool result messages
func (c *Component) handleToolResultMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {
	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
		return natsclient.DeliveryDecisionTerminate, fmt.Errorf("decode tool result: %w", err)
	}

	toolResultPtr, ok := baseMsg.Payload().(*agentic.ToolResult)
	if !ok {
		return natsclient.DeliveryDecisionTerminate,
			fmt.Errorf("unexpected tool-result payload type %T", baseMsg.Payload())
	}
	if err := toolResultPtr.Validate(); err != nil {
		return natsclient.DeliveryDecisionTerminate,
			errs.WrapInvalid(err, "agentic-loop", "handleToolResultMessage", "validate tool result")
	}
	toolResult := *toolResultPtr

	// Find the process-local route for this tool execution. Empty routes take
	// the operation-specific cold read-through path. Only exact applied evidence
	// can settle an older result; current results rejoin the normal handler.
	var loopID string
	var observedRevision uint64
	// Gate-phase status needs current durable authority even with a warm route.
	// Recovery classifies it before restoring or mutating any process state.
	if !agentic.IsApprovalRequired(toolResult.Error) {
		loopID = c.findLoopIDForToolCall(toolResult.ExecutionID)
	}
	if loopID == "" {
		loopID, observedRevision, err = c.recoverToolResult(ctx, toolResult)
		if err != nil {
			return loopSettlementDecision(err), err
		}
		if loopID == "" {
			return natsclient.DeliveryDecisionAck, nil
		}
	}
	if toolResult, err = c.validateRoutedToolResult(loopID, toolResult); err != nil {
		return natsclient.DeliveryDecisionQuarantine, err
	}

	if observedRevision == 0 && c.loopsBucket != nil {
		current, observed, readErr := c.readLoopEntityRevision(ctx, loopID)
		observedRevision, err = observed, readErr
		if err != nil {
			return loopSettlementDecision(err), err
		}
		if observedRevision == 0 {
			return natsclient.DeliveryDecisionRetry, fmt.Errorf("loop %q is not yet observable", loopID)
		}
		process, getErr := c.handler.GetLoop(loopID)
		if getErr != nil {
			return loopSettlementDecision(getErr), getErr
		}
		if current.TaskID != process.TaskID || current.Role != process.Role || current.Model != process.Model {
			return natsclient.DeliveryDecisionQuarantine, errs.WrapFatal(errors.New("tool process authority conflicts"), "agentic-loop", "handleToolResultMessage", "tool correlation conflict")
		}
		if current.State.IsTerminal() || (process.PendingApproval != nil && !reflect.DeepEqual(process.PendingApproval, current.PendingApproval)) {
			c.releaseLoopTransientState(loopID)
			return natsclient.DeliveryDecisionRetry, fmt.Errorf("tool authority for loop %q changed", loopID)
		}
		// Approved work can complete before its approval owner's cleared-gate CAS.
		// Only that same routed execution may use the still-pending revision.
		if pending := current.PendingApproval; pending != nil &&
			(pending.RequestID != toolResult.RequestID || pending.ExecutionID != toolResult.ExecutionID ||
				pending.CallID != toolResult.CallID || pending.CallOrdinal != toolResult.CallOrdinal ||
				pending.ToolName != toolResult.Name || pending.TraceID != toolResult.TraceID) {
			c.releaseLoopTransientState(loopID)
			return natsclient.DeliveryDecisionRetry, fmt.Errorf("tool execution %q does not own the current gate", toolResult.ExecutionID)
		}
	}
	hasError := toolResult.Error != ""

	c.logger.Debug("Processing tool result",
		slog.String("loop_id", loopID),
		slog.String("call_id", toolResult.CallID),
		slog.Bool("has_error", hasError))

	// Record tool result received
	if c.metrics != nil {
		c.metrics.recordToolResultReceived(hasError)
		c.metrics.recordTrajectoryStep("tool_call")
		if c.config.ToolResultMaxBytes > 0 && len(toolResult.Content) > c.config.ToolResultMaxBytes {
			c.metrics.recordToolResultTruncated()
		}
	}

	// Handle the tool result using the message handler
	result, err := c.handler.HandleToolResult(ctx, loopID, toolResult)
	if err != nil {
		// Only a constructed terminal outcome can settle this delivery.
		// An existing terminal state does not prove that this result applied.
		if result.State.IsTerminal() && (result.CompletionState != nil || result.FailureState != nil) {
			entity, entErr := c.handler.GetLoop(loopID)
			if persistErr := c.persistHandlerResult(ctx, result, observedRevision); persistErr != nil {
				c.releaseLoopTransientState(loopID)
				return loopSettlementDecision(persistErr), persistErr
			}
			if entErr == nil {
				c.recordTerminalState(result, entity, "timeout")
			}
			return natsclient.DeliveryDecisionAck, nil
		}
		c.recordHandlerResultTrajectory(ctx, result)
		c.logger.Error("Failed to handle tool result", "error", err, "loop_id", loopID)
		c.releaseLoopTransientState(loopID)
		return loopSettlementDecision(err), err
	}

	if agentic.IsApprovalRequired(toolResult.Error) && result.State == agentic.LoopStateAwaitingApproval {
		if err := c.persistApprovalGate(ctx, result, observedRevision); err != nil {
			c.releaseLoopTransientState(loopID)
			return natsclient.DeliveryDecisionRetry, err
		}
		return natsclient.DeliveryDecisionAck, nil
	}

	// Capture terminal signal inputs before persistence releases process state.
	// The signal itself fires only after the final marker commits below.
	var terminalEntity agentic.LoopEntity
	terminalEntityFound := false
	failureReason := "unknown"
	if result.State.IsTerminal() {
		if result.MaxIterationsReached {
			failureReason = "max_iterations"
		}
		if entity, entErr := c.handler.GetLoop(loopID); entErr == nil {
			terminalEntity = entity
			terminalEntityFound = true
		}
	}

	// Publish results, persist state, and handle terminal states (StopLoop).
	// persistHandlerResult covers publishResults + persistLoopState for all states,
	// plus finalization and completion-state persistence
	// when the loop reaches a terminal state.
	if err := c.persistHandlerResult(ctx, result, observedRevision); err != nil {
		c.releaseLoopTransientState(loopID)
		return loopSettlementDecision(err), err
	}
	if terminalEntityFound {
		c.recordTerminalState(result, terminalEntity, failureReason)
	}
	return natsclient.DeliveryDecisionAck, nil
}

// validateRoutedToolResult checks dispatched correlation and restores an omitted
// name from the matched call before the result reaches authority or persistence.
func (c *Component) validateRoutedToolResult(loopID string, toolResult agentic.ToolResult) (agentic.ToolResult, error) {
	if toolResult.LoopID != "" && toolResult.LoopID != loopID {
		err := errs.WrapFatal(
			fmt.Errorf("execution %q maps to loop %q but payload names %q", toolResult.ExecutionID, loopID, toolResult.LoopID),
			"agentic-loop", "handleToolResultMessage", "tool correlation conflict",
		)
		return toolResult, err
	}
	if toolResult.RequestID == "" || toolResult.ExecutionID == "" || toolResult.CallOrdinal == 0 {
		err := errs.WrapFatal(
			fmt.Errorf("tool result requires request_id, execution_id, and positive call_ordinal"),
			"agentic-loop", "handleToolResultMessage", "tool correlation conflict",
		)
		return toolResult, err
	}
	requestLoopID, err := loopIDFromRequestID(toolResult.RequestID)
	if err != nil || requestLoopID != loopID {
		conflictErr := errs.WrapFatal(
			fmt.Errorf("tool result request %q conflicts with routed loop %q", toolResult.RequestID, loopID),
			"agentic-loop", "handleToolResultMessage", "tool correlation conflict",
		)
		return toolResult, conflictErr
	}
	wantExecutionID := deriveToolExecutionID(toolResult.RequestID, toolResult.CallID, toolResult.CallOrdinal)
	if toolResult.Name == "" {
		toolResult.Name = c.handler.resolveToolName(toolResult)
	}
	if toolResult.ExecutionID != wantExecutionID ||
		c.handler.loopManager.GetToolName(toolResult.ExecutionID) != toolResult.Name ||
		c.handler.loopManager.GetToolOrdinal(toolResult.ExecutionID) != toolResult.CallOrdinal {
		err := errs.WrapFatal(
			fmt.Errorf("execution %q conflicts with dispatched call correlation", toolResult.ExecutionID),
			"agentic-loop", "handleToolResultMessage", "tool correlation conflict",
		)
		return toolResult, err
	}
	return toolResult, nil
}

// persistApprovalGate binds a new gate to its pre-mutation authority observation.
// No prompt or durable trajectory consequence precedes the conditional commit.
func (c *Component) persistApprovalGate(ctx context.Context, result HandlerResult, revision uint64) error {
	entity, err := c.handler.GetLoop(result.LoopID)
	if err != nil {
		return fmt.Errorf("get new approval gate: %w", err)
	}
	data, err := json.Marshal(entity)
	if err != nil {
		return fmt.Errorf("marshal new approval gate: %w", err)
	}
	if _, err := c.loopsBucket.Update(ctx, result.LoopID, data, revision); err != nil {
		return fmt.Errorf("commit new approval gate: %w", err)
	}
	c.recordHandlerResultTrajectory(ctx, result)
	return c.publishResults(ctx, result)
}

// publishResults publishes all output messages from a handler result using JetStream.
// Defensive against nil natsClient — pure unit tests construct
// Components without one, and the approval-timeout sweeper goroutine
// can race with Stop's natsClient teardown. Mirrors the existing
// persistLoopState's loopsBucket-nil guard pattern.
func (c *Component) publishResults(ctx context.Context, result HandlerResult) error {
	if c.natsClient == nil {
		return nil
	}
	for _, msg := range result.PublishedMessages {
		// Use JetStream for publishing to ensure delivery
		if err := c.natsClient.PublishToStream(ctx, msg.Subject, msg.Data); err != nil {
			return fmt.Errorf("publish result %s: %w", msg.Subject, err)
		}
	}

	// Publish context events (compaction lifecycle) onto the AGENT stream for
	// observability consumers — the OTel span collector (output/otel) enriches
	// the active loop span with each event via its agent.> subscription.
	for _, event := range result.ContextEvents {
		if err := c.publishContextEvent(ctx, event); err != nil {
			return err
		}
	}

	// Emit context management metrics from events
	c.emitContextMetrics(result)
	return nil
}

// publishContextEvent publishes a context management event
func (c *Component) publishContextEvent(ctx context.Context, event agentic.ContextEvent) error {
	eventMsg := message.NewBaseMessage(event.Schema(), &event, "agentic-loop")
	data, err := json.Marshal(eventMsg)
	if err != nil {
		return fmt.Errorf("marshal context event %s: %w", event.Type, err)
	}

	subject, err := component.ResolveSubject(c.config.Ports.Outputs, "agent.context.compaction", event.LoopID)
	if err != nil {
		return fmt.Errorf("resolve context event subject: %w", err)
	}
	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		return fmt.Errorf("publish context event %s: %w", subject, err)
	}
	return nil
}

// emitContextMetrics emits Prometheus metrics from context management events.
func (c *Component) emitContextMetrics(result HandlerResult) {
	if c.metrics == nil {
		return
	}

	for _, event := range result.ContextEvents {
		switch event.Type {
		case "compaction_complete":
			c.metrics.recordContextCompaction(event.TokensSaved)
		}
	}

	// Update utilization and compacted region tokens from the live context manager
	cm := c.handler.GetContextManager(result.LoopID)
	if cm != nil {
		c.metrics.recordContextUtilization(cm.Utilization())
		c.metrics.recordCompactedRegionTokens(cm.GetRegionTokens(RegionCompactedHistory))
	}
}

// persistLoopState persists the loop state to KV
func (c *Component) persistLoopState(ctx context.Context, loopID string) error {
	if c.loopsBucket == nil {
		return nil
	}

	entity, err := c.handler.GetLoop(loopID)
	if err != nil {
		return fmt.Errorf("get loop %s for persistence: %w", loopID, err)
	}

	data, err := json.Marshal(entity)
	if err != nil {
		return fmt.Errorf("marshal loop entity %s: %w", loopID, err)
	}

	if _, err := c.loopsBucket.Put(ctx, loopID, data); err != nil {
		return fmt.Errorf("persist loop state %s: %w", loopID, err)
	}
	return nil
}

// handleTrajectoryQuery handles NATS request/reply for trajectory queries.
// The immutable KV fact log is the only authority; process memory and graph
// state are never consulted.
func (c *Component) handleTrajectoryQuery(ctx context.Context, data []byte) ([]byte, error) {
	maxPayload, err := c.natsClient.MaxPayload()
	if err != nil {
		return nil, errs.Classified(errs.ErrorTransient, fmt.Errorf("observe NATS max payload: %w", err))
	}
	return c.handleTrajectoryQueryWithMaxPayload(ctx, data, maxPayload)
}

func (c *Component) handleTrajectoryQueryWithMaxPayload(
	ctx context.Context,
	data []byte,
	maxPayload int64,
) ([]byte, error) {
	req, err := decodeTrajectoryQueryRequest(data)
	if err != nil {
		return nil, err
	}

	if c.trajectoryReader == nil {
		return nil, errs.Classified(errs.ErrorTransient, errors.New("trajectory fact storage unavailable"))
	}
	response, err := c.trajectoryReader.read(ctx, req, maxPayload)
	if errors.Is(err, errTrajectoryNotFound) {
		return nil, errs.Classified(errs.ErrorInvalid, fmt.Errorf("trajectory not found: %w", err))
	}
	if err != nil {
		var classified *errs.ClassifiedError
		if errors.As(err, &classified) {
			return nil, err
		}
		return nil, errs.Classified(errs.ErrorTransient, err)
	}
	return json.Marshal(response)
}

// findLoopIDForRequest finds the loop ID associated with a request ID,
// attempting recovery from structured ID if not found in cache.
func (c *Component) findLoopIDForRequest(requestID string) string {
	loopID, exists := c.handler.loopManager.GetLoopForRequestWithRecovery(requestID)
	if !exists {
		return ""
	}
	return loopID
}

// findLoopIDForToolCall finds the loop ID associated with a framework tool
// execution ID. Provider CallID is request-scoped conversation data and is
// never used as a routing fallback.
func (c *Component) findLoopIDForToolCall(executionID string) string {
	loopID, exists := c.handler.loopManager.GetLoopForToolCallWithRecovery(executionID)
	if !exists {
		return ""
	}
	return loopID
}

// handleSignalMessage processes incoming signal messages. Cancel is the only
// loop-control verb; pause/resume were deleted in #1239.
func (c *Component) handleSignalMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {
	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
		return natsclient.DeliveryDecisionTerminate, fmt.Errorf("decode signal message: %w", err)
	}

	signalPtr, ok := baseMsg.Payload().(*agentic.UserSignal)
	if !ok {
		return natsclient.DeliveryDecisionTerminate, fmt.Errorf("unexpected signal payload type %T", baseMsg.Payload())
	}
	signal := *signalPtr

	c.logger.Debug("Processing signal message",
		slog.String("signal_id", signal.SignalID),
		slog.String("type", signal.Type),
		slog.String("loop_id", signal.LoopID),
		slog.String("user_id", signal.UserID))

	// Handle based on signal type
	switch signal.Type {
	case agentic.SignalCancel:
		return c.handleCancelSignal(ctx, signal)
	default:
		return natsclient.DeliveryDecisionTerminate, fmt.Errorf("unsupported signal type %q for loop %q", signal.Type, signal.LoopID)
	}
}

// handleCancelSignal handles a cancel signal for a loop
func (c *Component) handleCancelSignal(ctx context.Context, signal agentic.UserSignal) (natsclient.DeliveryDecision, error) {
	loopID := signal.LoopID
	current, revision, err := c.readLoopEntityRevision(ctx, loopID)
	if err != nil {
		if errs.IsFatal(err) {
			return natsclient.DeliveryDecisionQuarantine, err
		}
		return natsclient.DeliveryDecisionRetry, err
	}
	if revision == 0 {
		return natsclient.DeliveryDecisionRetry, fmt.Errorf("loop %q is not yet observable for cancellation", loopID)
	}
	if current.State.IsTerminal() {
		c.logger.WarnContext(ctx, "cancellation inapplicable: authoritative loop is terminal; no cancellation effects required",
			slog.String("signal_id", signal.SignalID), slog.String("loop_id", loopID), slog.String("state", string(current.State)))
		if c.metrics != nil {
			c.metrics.cancellationsInapplicable.Inc()
		}
		c.releaseLoopTransientState(loopID)
		return natsclient.DeliveryDecisionAck, nil // This cancel is effect-free and inapplicable to a closed loop.
	}
	if _, err := c.handler.GetLoop(loopID); err != nil {
		if _, err := c.handler.loopManager.CreateLoopWithID(loopID, current.TaskID, current.Role, current.Model, current.MaxIterations); err != nil {
			if errs.IsFatal(err) {
				return natsclient.DeliveryDecisionQuarantine, err
			}
			return natsclient.DeliveryDecisionRetry, err
		}
	}
	defer c.releaseLoopTransientState(loopID)
	if err := c.handler.UpdateLoop(current); err != nil {
		if errs.IsFatal(err) {
			return natsclient.DeliveryDecisionQuarantine, err
		}
		return natsclient.DeliveryDecisionRetry, err
	}
	c.handler.drainPendingToolFailures(loopID, fmt.Sprintf("loop cancelled by %s", signal.UserID))
	entity, err := c.handler.CancelLoop(loopID, signal.UserID)
	if err != nil {
		err = fmt.Errorf("cancel loop %q: %w", loopID, err)
		if errs.IsFatal(err) {
			return natsclient.DeliveryDecisionQuarantine, err
		}
		return natsclient.DeliveryDecisionRetry, err
	}
	completion := agentic.LoopCancelledEvent{
		LoopID:       loopID,
		TaskID:       entity.TaskID,
		Outcome:      agentic.OutcomeCancelled,
		CancelledBy:  signal.UserID,
		ParentLoopID: entity.ParentLoopID,
		WorkflowSlug: entity.WorkflowSlug,
		WorkflowStep: entity.WorkflowStep,
		CancelledAt:  entity.CancelledAt,
		Metadata:     entity.Metadata,
		RunID:        entity.RunID,
		RunEntityID:  c.handler.resolveRunEntityID(entity.RunID),
	}
	if decision, err := c.persistTerminalOutcome(ctx, HandlerResult{LoopID: loopID, State: agentic.LoopStateCancelled}, &completion, revision); err != nil {
		return decision, err
	}
	if c.metrics != nil {
		c.metrics.recordLoopFailed("cancelled", entity.Iterations, time.Since(entity.StartedAt).Seconds())
	}
	c.logger.Info("Loop cancelled", slog.String("loop_id", loopID), slog.String("cancelled_by", signal.UserID))
	return natsclient.DeliveryDecisionAck, nil
}

// handleToolCallVerdictMessage routes inbound verdicts from
// agent.toolcall.approved.> and agent.toolcall.rejected.> into the
// governance dispatcher (ADR-039). The dispatcher demuxes by execution_id
// to per-call waiter channels.
//
// Both rule authoring paths use the registered GenericJSON carrier. The actual
// delivered subject must agree with normalized payload identity; wrapper metadata
// cannot repair or override it. Dispatch receives the typed value, not wire bytes.
//
// A missing dispatcher quarantines the delivery. NewComponent constructs one
// for every configured mode, including disabled mode.
func (c *Component) handleToolCallVerdictMessage(_ context.Context, subject string, data []byte) (natsclient.DeliveryDecision, error) {
	dispatcher := c.handler.GovernanceDispatcher()
	if dispatcher == nil {
		return natsclient.DeliveryDecisionQuarantine, errors.New("tool-call verdict dispatcher is unavailable")
	}

	payload, decision, err := decodeVerdictMessage(c.decoder, subject, data)
	if err != nil {
		return decision, err
	}
	return dispatcher.HandleVerdict(payload)
}

func decodeVerdictMessage(decoder *message.Decoder, subject string, data []byte) (VerdictPayload, natsclient.DeliveryDecision, error) {
	payload, decision, err := decodeVerdictPayload(decoder, data)
	if err != nil {
		return VerdictPayload{}, decision, err
	}
	if subject != "agent.toolcall."+payload.Decision+"."+payload.ExecutionID {
		return VerdictPayload{}, natsclient.DeliveryDecisionQuarantine, fmt.Errorf("verdict subject %q conflicts with decision/execution identity", subject)
	}
	return payload, decision, nil
}

// decodeVerdictPayload admits only validated registered GenericJSON, then uses
// the same correlation interpreter as direct typed dispatcher calls.
func decodeVerdictPayload(decoder *message.Decoder, data []byte) (VerdictPayload, natsclient.DeliveryDecision, error) {
	if decoder == nil {
		return VerdictPayload{}, natsclient.DeliveryDecisionTerminate, errors.New("verdict decoder is unavailable")
	}
	baseMsg, err := decoder.Decode(data)
	if err != nil {
		return VerdictPayload{}, natsclient.DeliveryDecisionTerminate, fmt.Errorf("decode verdict envelope: %w", err)
	}
	if err := baseMsg.Validate(); err != nil {
		return VerdictPayload{}, natsclient.DeliveryDecisionTerminate, fmt.Errorf("validate verdict envelope: %w", err)
	}
	generic, ok := baseMsg.Payload().(*message.GenericJSONPayload)
	if !ok {
		return VerdictPayload{}, natsclient.DeliveryDecisionTerminate, errors.New("verdict payload must be GenericJSON")
	}
	payload, err := verdictPayloadFromMap(generic.Data)
	if err != nil {
		return VerdictPayload{}, natsclient.DeliveryDecisionTerminate, err
	}
	return normalizeVerdictPayload(payload)
}

// verdictPayloadFromMap translates a GenericJSONPayload.Data map into
// the existing typed shape without silently dropping malformed correlation.
func verdictPayloadFromMap(data map[string]any) (VerdictPayload, error) {
	p := VerdictPayload{}
	for _, field := range []struct {
		name  string
		value *string
	}{
		{"decision", &p.Decision}, {"call_id", &p.CallID}, {"loop_id", &p.LoopID},
		{"request_id", &p.RequestID}, {"execution_id", &p.ExecutionID}, {"proposal_fingerprint", &p.ProposalFingerprint},
	} {
		if raw, supplied := data[field.name]; supplied {
			value, ok := raw.(string)
			if !ok {
				return VerdictPayload{}, fmt.Errorf("verdict %s must be a string", field.name)
			}
			*field.value = value
		}
	}
	if raw, supplied := data["properties"]; supplied {
		var ok bool
		p.Properties, ok = raw.(map[string]any)
		if !ok {
			return VerdictPayload{}, errors.New("verdict properties must be an object")
		}
	}
	p.RuleID, _ = data["rule_id"].(string)
	p.Reason, _ = data["reason"].(string)
	p.EntityID, _ = data["entity_id"].(string)
	p.Timestamp, _ = data["timestamp"].(string)
	return p, nil
}
