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
	"github.com/c360studio/semstreams/internal/deliverylane"
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

	// The cold fork's own refusal (#1330, owner ruling 2026-09-23 on the
	// round-2 docket's question 1). Its own lane label, because nothing
	// about it is structural: the message decoded, the loop is real, and the
	// turn is a turn no process can apply.
	taskIntakeColdForkLane             = "cold-fork"
	taskIntakeContinuationUnheldReason = "continuation_unheld"
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

	// requestEvidence overrides the production retained-request reader. Always
	// nil outside tests, which drive identity adoption's four arms through it.
	requestEvidence loopEvidenceReader

	// loopRevisions retains, per loop, the AGENT_LOOPS revision this process
	// last committed or observed for that loop. It is the compare-and-swap
	// input every non-terminal record write uses (#1330, owner ruling Q2).
	//
	// It is a retained observation, never a live re-read: a revision fetched
	// immediately before a write can capture a foreign writer's commit and
	// turn the CAS into the last-writer-wins Put it replaced. The entry is
	// seeded by the loop's own birth Create and refreshed by each successful
	// Update and by each cold record read; releaseLoopTransientState drops it
	// with the rest of the loop's per-loop state. Protected by mu.
	loopRevisions map[string]uint64

	// loopRecordMu serializes the record-write sequence — render the entity,
	// read its observed revision, compare-and-swap, remember the revision the
	// write committed at — so the read and the write are ONE critical section.
	//
	// They have to be. The compare-and-swap exists to catch a SECOND PROCESS
	// writing this loop, and a refused CAS cannot tell that apart from anything
	// else; but this process has several lanes that write the same loop — the
	// three L4a lanes through the carrier, the cancel lane, the approval lane,
	// the approval-timeout sweeper — on separate consumers, so two of them can
	// interleave. Without this lock one lane reads revision N, the other
	// commits N+1, and the first lane's write is refused; the loop is then
	// released as if a foreign process owned it and the delivery settles as
	// unknown-durability. CI run 35719155514 is exactly that: a cancel signal
	// lost its record write to the tool lane's advance, ownership was dropped,
	// and the loop never reached cancelled.
	//
	// One mutex, not one per loop: the sequence is a single bounded KV write,
	// a process has a handful of writers, and a per-loop lock is another map to
	// create, find and release with the loop it belongs to — machinery this
	// contention does not earn.
	loopRecordMu sync.Mutex

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
	consumers     []*deliverylane.Binding

	// Query subscription for trajectory requests
	trajectorySub            requestSubscription
	inflightSub              requestSubscription
	initializeKVBucketsInput func(context.Context) error
	waitForStreamInput       func(context.Context, string) error
	consumeStream            func(context.Context, context.Context, natsclient.PortConsumerContext, natsclient.StreamConsumerConfig, func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error)
	subscribeRequests        func(context.Context, string, func(context.Context, []byte) ([]byte, error)) (requestSubscription, error)
	waitConsumerClosed       func(context.Context, <-chan struct{}) error

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

type requestSubscription interface{ Drain(context.Context) error }

type inputHandler func(context.Context, []byte) error

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
	return func(ctx context.Context, data []byte) error {
		handler(ctx, data)
		return nil
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
		initialize := c.initializeKVBuckets
		if c.initializeKVBucketsInput != nil {
			initialize = c.initializeKVBucketsInput
		}
		if err := initialize(runCtx); err != nil {
			return errs.Wrap(err, "agentic-loop", "Start", "initialize KV buckets")
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
	for _, binding := range c.consumers {
		binding.Drain()
		closed := binding.Closed()
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
	for _, binding := range c.consumers {
		select {
		case <-binding.Done():
		case <-ctx.Done():
			cleanupErr = errors.Join(cleanupErr, ctx.Err())
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

// initializeKVBuckets initializes the KV buckets for loop and trajectory storage
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
			settleHandlerFn func(context.Context, []byte) (natsclient.DeliveryDecision, error)
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
			settleHandlerFn = c.handleSignalMessage
		case "agent.approval_response":
			settleHandlerFn = c.handleApprovalResponseMessage
		case "agent.toolcall.approved", "agent.toolcall.rejected":
			// Verdicts from rule-driven tool-call governance (ADR-039).
			// Both subjects route into the same demux — the dispatcher
			// reads decision + execution_id from the verdict payload.
			// Skip if no dispatcher is configured (disabled mode with
			// no fallback construction); the wildcard subscription is
			// still cheap to bind but never gets traffic in disabled
			// mode because nothing publishes to proposed.
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

// loopLaneDelivery is the delivery posture a single input lane runs under. It
// is resolved before the consumer config is built so the latency-class rules
// live in one place rather than inside the setup path.
type loopLaneDelivery struct {
	ackWait           time.Duration
	maxAckPending     int
	maxDeliver        int
	msgTimeout        time.Duration
	backOff           []time.Duration
	useHeartbeat      bool
	heartbeatInterval time.Duration
}

// resolveLoopLaneDelivery differentiates the posture by latency class:
// long-running ports (task, response, tool.result) need serial processing,
// heartbeats and graduated backoff to survive LLM-scale latency; the fast
// ports keep short timeouts and higher concurrency.
func (c *Component) resolveLoopLaneDelivery(
	portName string,
	consumerCfg component.ConsumerConfig,
	componentMaxAckPending int,
) loopLaneDelivery {
	lane := loopLaneDelivery{
		maxAckPending: componentMaxAckPending,
		backOff:       []time.Duration{30 * time.Second, 2 * time.Minute},
	}
	switch portName {
	case "agent.task", "agent.response", "tool.result":
		lane.ackWait = c.config.Consumer.ParsedAckWait()
		lane.maxDeliver = c.config.Consumer.MaxDeliver
		// The task adapter (taskInputHandler) owns the ordinary 30m work
		// deadline; the outer callback stays lifecycle-bound so a timed-out
		// task is attributed as a work error, not an outer cancellation.
		lane.msgTimeout = 30 * time.Minute
		lane.useHeartbeat = true
		lane.heartbeatInterval = c.config.Consumer.ParsedHeartbeatInterval()
	default: // agent.signal, agent.approval_response, agent.toolcall.* — fast
		lane.ackWait = 30 * time.Second
		// These four lanes are the ones this change gave a Retry
		// classification to, so they need the same bound the heartbeat lanes
		// have. What they were missing was not a delivery ceiling —
		// component.GetConsumerConfig already defaults MaxDeliver to 3
		// (component/port_jetstream.go:130), and these ports declare no
		// consumer config to override it — but a BackOff and a delay: their
		// Retry was a bare Nak against an empty BackOff, so every transient
		// error was redelivered at line rate until the ceiling burned through.
		// The resolved MaxDeliver is passed through and then held to the
		// BackOff length by validateLoopRetryPolicy at setup, which refuses
		// rather than repairs.
		lane.maxDeliver = consumerCfg.MaxDeliver
		lane.msgTimeout = c.messageTimeout
	}
	return lane
}

// recordDeliveryOwnerFatal latches the FIRST loss of delivery ownership into
// health. It runs synchronously inside the delivery callback as the lane
// admission's onFatal, before the result is buffered for the observer, so
// health can never read healthy after the FATAL observer has drained the exact
// handle. It is not ordered against cleanup's own Drain, which runs
// independently of this admission. Relocated
// here from delivery_owner.go when the latch moved to internal/deliverylane;
// the health semantics are untouched.
func (c *Component) recordDeliveryOwnerFatal(result natsclient.DeliveryResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.deliveryFatalErr != nil {
		return
	}
	c.deliveryFatalErr = result.Err()
}

// setupConsumer sets up a JetStream consumer for an input port.
func (c *Component) setupConsumer(
	setupCtx context.Context,
	consumerCtx context.Context,
	port component.Port,
	subject string,
	handler inputHandler,
	settleHandlerFn func(context.Context, []byte) (natsclient.DeliveryDecision, error),
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

	lane := c.resolveLoopLaneDelivery(port.Name, consumerCfg, componentMaxAckPending)

	cfg := natsclient.StreamConsumerConfig{
		StreamName:     streamName,
		ConsumerName:   consumerName,
		FilterSubject:  subject,
		DeliverPolicy:  consumerCfg.DeliverPolicy,
		AckPolicy:      consumerCfg.AckPolicy,
		MaxDeliver:     lane.maxDeliver,
		AckWait:        lane.ackWait,
		MaxAckPending:  lane.maxAckPending,
		BackOff:        lane.backOff,
		AutoCreate:     false,
		MessageTimeout: lane.msgTimeout,
		// agent.task applies its 30m ordinary-work deadline in taskInputHandler;
		// its outer context stays lifecycle-bound so the adapter's deadline is
		// the single authority on task-work timeout attribution.
		DisableMessageTimeout: port.Name == "agent.task",
	}
	var handlerFn func(context.Context, jetstream.Msg)
	var admission *deliverylane.Admission
	if lane.useHeartbeat {
		policy, policyErr := newLoopHeartbeatDeliveryPolicy(setupCtx, cfg, lane.heartbeatInterval, port.Name, handler)
		if policyErr != nil {
			return policyErr
		}
		admission = deliverylane.NewAdmission(c.recordDeliveryOwnerFatal, nil)
		handlerFn = func(msgCtx context.Context, msg jetstream.Msg) {
			result, admitted := deliverylane.Consume(msgCtx, msg, policy, admission)
			if admitted && result.Err() != nil && !result.OwnerStopRequired() {
				c.logger.Error("Message handler error", "port", port.Name, "error", result.Err())
			}
		}
	} else {
		if settleHandlerFn == nil {
			return errs.WrapInvalid(fmt.Errorf("input port %q has no typed settlement handler", port.Name),
				"agentic-loop", "setupConsumer", "missing settlement handler")
		}
		if err := validateLoopRetryPolicy(port.Name, cfg); err != nil {
			return err
		}
		// Same 30s delay the heartbeat lanes use. Without it a Retry is a bare
		// Nak, redelivered at line rate.
		settleRetry, retryErr := natsclient.DelayedDeliveryRetry(30 * time.Second)
		if retryErr != nil {
			return errs.WrapInvalid(retryErr, "agentic-loop", "setupConsumer",
				"construct settlement retry policy")
		}
		admission = deliverylane.NewAdmission(c.recordDeliveryOwnerFatal, nil)
		handlerFn = func(msgCtx context.Context, msg jetstream.Msg) {
			result, admitted := deliverylane.Settle(msgCtx, msg, settleRetry, admission, "loop", settleHandlerFn)
			// Early return, not a conjunct: a refused delivery returns the zero
			// result, whose Err() is non-nil by construction, so every branch
			// below must be unreachable on refusal — including ones added later.
			if !admitted {
				return
			}
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

	binding := deliverylane.NewBinding(handle)
	// Unreachable today: both useHeartbeat branches above assign admission.
	// Kept as a guard because the failure it prevents is silent — a nil
	// admission would append a binding to c.consumers with no observer, whose
	// Done() is pre-closed, so Stop would join nothing and a lost lane would
	// never drain its handle.
	if admission != nil {
		deliverylane.Observe(consumerCtx, binding, admission, func(result natsclient.DeliveryResult) {
			c.logger.Error("Loop delivery ownership lost", "port", port.Name, "error", result.Err())
		})
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
		func(workCtx context.Context, data []byte) (natsclient.DeliveryDecision, error) {
			handlerErr := handler(workCtx, data)
			if handlerErr == nil {
				return natsclient.DeliveryDecisionAck, nil
			}
			// Fatal is checked first and means one thing here: an external
			// effect may already have happened and we cannot tell. Retry is
			// only safe when re-execution is, so a commit-unknown error can
			// never fall through to it. The cancel lane already made this
			// call; the heartbeat lanes were the ones still blind-retrying.
			if errs.IsFatal(handlerErr) {
				return natsclient.DeliveryDecisionQuarantine, handlerErr
			}
			var permanent *natsclient.PermanentDeliveryError
			if errors.As(handlerErr, &permanent) {
				return natsclient.DeliveryDecisionTerminate, handlerErr
			}
			return natsclient.DeliveryDecisionRetry, handlerErr
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
	return func(consumerCtx context.Context, data []byte) error {
		workCtx, cancel := context.WithTimeout(consumerCtx, workTimeout)
		defer cancel()
		err := c.handleTaskMessage(workCtx, data)
		if err == nil && workCtx.Err() != nil {
			return workCtx.Err()
		}
		return err
	}
}

// refuseConflictingTaskIdentity stops a delivery whose TaskID is already
// running under a DIFFERENT loop than the one its message names.
//
// TaskID alone is a redelivery — the same work arriving twice — and the loop
// already running it is the answer; that is what the dedup branch in
// HandleTask serves, and it is unchanged. One TaskID naming TWO loops is not
// a redelivery: the message and durable state disagree about which loop this
// work is, and neither answer is available. Adopting the running loop runs
// this message's work in a conversation it does not name; answering with the
// running loop tells the producer its loop is live when no loop by that name
// exists anywhere. A second delivery resolves nothing, because the
// disagreement is IN the message. Fatal, so the lane quarantines with both
// tokens on the record (the heartbeat policy reads Fatal as Quarantine,
// :1196).
//
// It takes the producer's token rather than reading task.LoopID, because by
// now those differ: preflightDecodedTask reserves a fresh prospective UUID on
// every delivery of a lineage task that named no loop, so reading the field
// would classify an ordinary redelivery of such a task as a conflict. A task
// that named no loop keeps the intake exemption and is deduplicated.
func (c *Component) refuseConflictingTaskIdentity(task agentic.TaskMessage, suppliedLoopID string) error {
	if suppliedLoopID == "" {
		return nil
	}
	existingID, running := c.handler.loopManager.HasActiveLoopForTask(task.TaskID)
	if !running || existingID == suppliedLoopID {
		return nil
	}
	return errs.WrapFatal(
		fmt.Errorf("task %s is already running as loop %s but this message names loop %s",
			task.TaskID, existingID, suppliedLoopID),
		"agentic-loop", "handleTaskMessage", "reject conflicting task identity")
}

// settleUnheldContinuation settles a task that continues a loop no process
// holds, and reports whether it did.
//
// The record belongs to another task, so nothing in this process can apply
// this one: the loop's conversation lived in the process that is gone, and
// the turn's text is on neither the record nor the stream. The turn is
// acknowledged WITHOUT EFFECT — the settlement the warm refusal already takes
// when a continuation meets a busy loop (ErrLoopBusy, handleTaskMessage) — and
// re-sending it once a redelivered input has rebuilt the loop is the
// documented recovery (doc.go § Recovery across a process replacement).
//
// The other two settlements were rejected on this lane: Retry parks the whole
// task lane, which runs at MaxAckPending 1, for MaxDeliver attempts on a
// message no redelivery can fix, and Quarantine latches the lane and the
// component's health over a perfectly valid turn.
//
// A skip is a declared event: the warning names the record's task and the
// arriving one, and the reason value is counted on the existing
// task_intake_rejections_total (#1330, owner ruling 2026-09-23).
func (c *Component) settleUnheldContinuation(
	ctx context.Context, disposition taskDisposition, task agentic.TaskMessage, record loopRecord,
) bool {
	if disposition != taskContinuationUnheld {
		return false
	}
	c.logger.WarnContext(ctx, "Task refused — it continues a loop no process holds",
		slog.String("task_id", task.TaskID),
		slog.String("loop_id", task.LoopID),
		slog.String("record_task_id", record.entity.TaskID),
		slog.String("state", record.entity.State.String()))
	if c.metrics != nil {
		c.metrics.recordTaskIntakeRejection(taskIntakeColdForkLane, taskIntakeContinuationUnheldReason)
	}
	return true
}

// handleTaskMessage processes incoming task messages
func (c *Component) handleTaskMessage(ctx context.Context, data []byte) error {
	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
		c.logger.Error("Failed to unmarshal BaseMessage", "error", err)
		return nil
	}

	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	if !ok {
		c.logger.Error("Unexpected payload type", "type", fmt.Sprintf("%T", baseMsg.Payload()))
		return nil
	}
	// The loop token the PRODUCER sent, read before preflight can reserve one.
	// Everything below distinguishes "this message named a loop" from "intake
	// minted a prospective UUID for this delivery", and after
	// preflightDecodedTask they are the same field.
	suppliedLoopID := task.LoopID
	related, hasLineage, err := c.preflightDecodedTask(task)
	if err != nil {
		if c.metrics != nil {
			c.metrics.recordTaskIntakeRejection(taskIntakeRejectionLane, taskIntakeRejectionReason)
		}
		return natsclient.TerminateDelivery(err)
	}
	if err := c.refuseConflictingTaskIdentity(*task, suppliedLoopID); err != nil {
		c.logger.Error("Task refused — its identity conflicts with a running loop",
			"error", err, "task_id", task.TaskID, "loop_id", suppliedLoopID)
		return err
	}

	c.logger.Debug("Processing task message",
		slog.String("task_id", task.TaskID),
		slog.String("role", task.Role),
		slog.String("model", task.Model))

	// The cold fork (#1330, design § 5.1, owner ruling Q1): what this task
	// means for a loop this process has no memory of is answered by the
	// record, before anything is built in memory.
	disposition, record, err := c.classifyRedeliveredTask(ctx, *task)
	if err != nil {
		return err
	}
	if c.settleUnheldContinuation(ctx, disposition, *task, record) {
		return nil
	}
	if disposition == taskApplied {
		c.logger.Info("Task acknowledged without effect — its loop already moved past it",
			slog.String("task_id", task.TaskID),
			slog.String("loop_id", task.LoopID),
			slog.Int("iterations", record.entity.Iterations),
			// The request the record names is the fact that decides this arm
			// for a loop still at iteration zero — a within-iteration retry
			// reads as an untouched birth on every other field — so an
			// operator reading this line can see WHICH of the three facts
			// settled the task.
			slog.String("published_request_id", record.entity.PublishedRequestID),
			slog.String("state", record.entity.State.String()))
		return nil
	}

	// Handle the task using the message handler
	result, err := c.handler.HandleTask(ctx, *task)
	if err != nil {
		// A continuation refused because its loop is still working is ordinary
		// user behaviour — someone typed while the agent was thinking — not an
		// operator-actionable fault, and this path became common the moment
		// intake started attaching (#1227). ERROR here would manufacture a
		// false-alarm class out of a refusal that is working as designed. Every
		// other handler failure keeps ERROR.
		if errors.Is(err, ErrLoopBusy) {
			c.logger.Warn("Task refused — the loop it names still has work in flight",
				"error", err, "task_id", task.TaskID, "loop_id", task.LoopID)
			return nil
		}
		c.logger.Error("Failed to handle task", "error", err, "task_id", task.TaskID)
		return nil
	}

	// A deferred continuation is not a dedup and not a spawn: the loop already
	// exists, the turn is already in its context, and the durable effect this
	// delivery owns is the pending-continuation marker on the loop entity. There
	// is nothing to publish and no graph birth to do — the loop was born on its
	// first task. Persisting the entity is best-effort here exactly as it is on
	// the spawn path below; what the marker survives in-process is this
	// process, and restoring it across a replacement is L4's (#1330).
	if result.Deferred {
		c.logger.Debug("Task deferred behind the loop's outstanding model request",
			slog.String("loop_id", result.LoopID),
			slog.String("task_id", task.TaskID))
		c.recordTrajectoryObservations(ctx, result)
		if err := c.persistLoopState(ctx, result.LoopID); errors.Is(err, natsclient.ErrKVRevisionMismatch) {
			// A lost compare-and-swap already released this loop, so the
			// user's turn is held by nothing in this process. Acknowledging
			// here would discard it; the delivery retries into whichever
			// process now holds the record (#1330, docket OQ3). Every other
			// write failure stays best-effort, as it was.
			c.logger.Warn("Deferred continuation lost the record race — the turn is redelivered",
				"loop_id", result.LoopID, "task_id", task.TaskID, "error", err)
			return err
		}
		return nil
	}

	if !result.Created {
		pending, ok := c.pendingTaskResult(task.TaskID, result.LoopID)
		if !ok {
			c.logger.Debug("Task deduplicated — loop already active",
				slog.String("loop_id", result.LoopID),
				slog.String("task_id", task.TaskID))
			return nil
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
					return err
				}
				c.clearPendingTaskResult(task.TaskID, result.LoopID)
				c.logger.Error("graph_writer: lineage write failed — halting loop spawn",
					"loop_id", result.LoopID, "task_id", task.TaskID, "error", err)
				entity, _ := c.handler.GetLoop(result.LoopID)
				return c.handleSpawnIdentityFailure(ctx, result.LoopID, entity, err)
			}
		}
	}
	c.clearPendingTaskResult(task.TaskID, result.LoopID)

	// Record creation only after graph birth succeeds. Birth failures of any
	// class record creation inside handleSpawnIdentityFailure immediately
	// before the failure path so its active-loop decrement remains balanced.
	if c.metrics != nil {
		c.metrics.recordLoopCreated()
	}

	// The record is written BEFORE the first request goes out (#1330, owner
	// ruling Q1), and by Create, not Put.
	//
	// Both halves matter. Writing first means a task redelivered while the
	// record is still at iteration zero finds a record and republishes R1
	// rather than finding nothing and birthing a second loop; the reverse
	// order published a request no record named, and its error was discarded.
	// Create means a second consumer's birth for the same loop ID is REFUSED
	// rather than overwriting a record that may already carry iterations, a
	// published request and an applied set.
	//
	// A refused birth releases the loop this process just built in memory and
	// returns the delivery transient: the record belongs to whoever created
	// it, and the redelivery is owed to the process holding that loop. The
	// cold fork above is what resolves the expected case in place.
	if disposition == taskRepublishFirstRequest {
		// The record still names the loop's first request and this process
		// just rebuilt R1 from the same task that produced it — the grammar is
		// deterministic and the classifier checked the record's name, so the
		// name is the one the record already carries. There is nothing
		// to write: the record is already the truth, and writing it again
		// would move a revision no reader is waiting on. What IS taken is that
		// revision: this process is now the loop's holder, and its next
		// compare-and-swap has no other write to seed it from.
		c.rememberLoopRevision(result.LoopID, record.revision)
		// A rebuild is not a reprieve. This arm is the only reconstruction that
		// goes through the ORDINARY HandleTask, whose configureLoopMetadata
		// calls SetTimeout — which stamps StartedAt = now and TimeoutAt = now +
		// budget on the entity it just built. The cold response and tool arms
		// seat the record wholesale (restoreLoopFromRequest) and so inherit its
		// timing for free; without this overlay an expired record whose task
		// happened to redeliver first resumed on a full fresh budget, letting a
		// loop outlive the budget its caller set. The owner ruled that out:
		// "no refresh on rebuild … the loop's deadline means what its record
		// says" (#1330, 2026-09-23 — issuecomment-5781101792).
		//
		// A record with no deadline (no timeout configured at birth) overlays
		// zero onto zero, which is the same answer the wholesale seat gives.
		//
		// Neither call can fail for a loop HandleTask just built in this
		// process; if one somehow does, the delivery is refused rather than
		// republished, because the alternative is running the loop on a
		// deadline the ruling forbids. Released and returned transient for the
		// same reason the two birth arms below release.
		if err := c.restoreRecordedLoopDeadline(result.LoopID, record.entity); err != nil {
			c.logger.Error("Rebuilt loop could not be given its record's deadline — the request is not republished",
				"loop_id", result.LoopID, "task_id", task.TaskID, "error", err)
			c.releaseLoopTransientState(result.LoopID)
			return errs.WrapTransient(err, "agentic-loop", "handleTaskMessage",
				"restore the rebuilt loop's recorded deadline")
		}
		c.logger.Info("Task redelivered at iteration zero — republishing the loop's first request",
			"loop_id", result.LoopID, "task_id", task.TaskID,
			"published_request_id", record.entity.PublishedRequestID,
			"timeout_at", record.entity.TimeoutAt)
	} else if err := c.createLoopState(ctx, result.LoopID); err != nil {
		if errors.Is(err, natsclient.ErrKVKeyExists) {
			c.logger.Warn("Loop record already exists — this birth is not the one that created the loop",
				"loop_id", result.LoopID, "task_id", task.TaskID)
			c.releaseLoopTransientState(result.LoopID)
			return errs.WrapTransient(err, "agentic-loop", "handleTaskMessage",
				"create loop record at birth")
		}
		c.logger.Error("Failed to write the loop record at birth — the first request is not published",
			"loop_id", result.LoopID, "task_id", task.TaskID, "error", err)
		// Released for the same reason the conflict arm above and the
		// publish-failure arm below release: the redelivery has to reach the
		// record, and a loop left warm sends it into HandleTask's task-id
		// dedup instead, which answers "already active" and acknowledges. The
		// birth wrote no record and published no request, so that ACK is
		// silent task loss — a task nobody is owed and no loop is running.
		c.releaseLoopTransientState(result.LoopID)
		return errs.WrapTransient(err, "agentic-loop", "handleTaskMessage",
			"create loop record at birth")
	}

	// Publish output messages, and return what that publish answers.
	//
	// The record written above names R1, and I1 says that while the record
	// exists the stream retains the request it names. Discarding this error
	// ACKs a delivery that left exactly the state I1 declares impossible: a
	// record naming a request nothing retains, which every later cold read
	// answers with Quarantine (adoptNewerRetainedRequest's I1 arm). Returning
	// it hands the delivery to the task lane's classification — an ordinary
	// publish failure is Retry — so the request goes out on a redelivery
	// instead of never. Writing the record first is still Q1: the redelivery
	// needs a record to republish R1 from.
	//
	// The loop this process just built in memory is released with it, for the
	// same reason the refused birth above releases: the redelivery has to meet
	// the cold fork, which republishes R1 from the record. Keeping the loop
	// warm would send the redelivery into HandleTask's own dedup instead,
	// which answers "already active" and acknowledges — leaving a record that
	// names a request nothing ever published, which is the state I1 declares
	// impossible. The active-loops gauge counted this birth and the release
	// does not un-count it, exactly as the refused-birth branch above does not;
	// the redelivery's own birth counts again.
	if err := c.publishResults(ctx, result); err != nil {
		c.logger.Error("Birth did not publish the request its record names — the delivery is not acknowledged",
			"loop_id", result.LoopID, "task_id", task.TaskID, "error", err)
		c.releaseLoopTransientState(result.LoopID)
		return err
	}
	return nil
}

// restoreRecordedLoopDeadline overlays a loop record's own StartedAt and
// TimeoutAt onto the loop this process just rebuilt from its task.
//
// It exists for the ONE reconstruction that does not seat the record
// wholesale: the cold R1 arm runs HandleTask, and HandleTask stamps a fresh
// deadline. Everything else about that rebuild is deliberately the ordinary
// birth path, so the two fields the ruling protects are put back here rather
// than by teaching the birth path what a rebuild is.
func (c *Component) restoreRecordedLoopDeadline(loopID string, recorded agentic.LoopEntity) error {
	entity, err := c.handler.GetLoop(loopID)
	if err != nil {
		return err
	}
	entity.StartedAt = recorded.StartedAt
	entity.TimeoutAt = recorded.TimeoutAt
	return c.handler.UpdateLoop(entity)
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

	// Reserve only an identity value, with no loop-manager or persistence
	// side effect, so the complete prospective graph batch can be built and
	// validated before HandleTask creates any loop state.
	if task.LoopID == "" {
		task.LoopID = c.handler.loopManager.GenerateLoopID()
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
func (c *Component) handleSpawnIdentityFailure(ctx context.Context, loopID string, entity agentic.LoopEntity, err error) error {
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
	// Record creation immediately before the failure path so the failure
	// path's active-loop decrement remains balanced (creation is otherwise
	// recorded only after a successful graph birth).
	if c.metrics != nil && entity.ID != "" {
		c.metrics.recordLoopCreated()
	}
	return c.handleLoopFailure(ctx, loopID, entity, reason, err)
}

// handleResponseMessage processes incoming agent response messages
func (c *Component) handleResponseMessage(ctx context.Context, data []byte) error {
	response, loopID, err := c.extractAgentResponse(data)
	if err != nil {
		return err
	}
	if loopID == "" {
		rebuilt, err := c.settleResponseWithoutLoop(ctx, response.RequestID)
		if err != nil || !rebuilt {
			return err
		}
		// The loop is this process's now, so the delivery takes the ordinary
		// warm path from here — there is no second apply path for a recovered
		// loop, which is what keeps recovery from drifting from execution.
		loopID = c.findLoopIDForRequest(response.RequestID)
		if loopID == "" {
			return errs.WrapTransient(
				fmt.Errorf("response %q: its loop was rebuilt and its request still routes nowhere",
					response.RequestID),
				"agentic-loop", "handleResponseMessage", "route the response to the rebuilt loop")
		}
	}

	entity, _ := c.handler.GetLoop(loopID)

	result, err := c.handler.HandleModelResponse(ctx, loopID, *response)
	if err != nil {
		switch {
		case errors.Is(err, errRequestNotYetObservable):
			// Not a failure of this loop: the answer outran the record update
			// that names its question (#1330, W4). Retry until the record
			// catches up, and leave the loop exactly as it is — failing it
			// here would settle a running loop on a timing window.
			c.logger.Warn("Model response is not yet observable on the loop record — retrying",
				"loop_id", loopID, "request_id", response.RequestID, "error", err)
			return err
		case errors.Is(err, errResponseSuperseded):
			// The loop already applied this request's answer and moved on, so
			// the delivery is finished. It returns HERE rather than flowing an
			// empty result through the carrier: persistHandlerResult ends in a
			// compare-and-swap write, and a response that changed nothing must
			// not move the record's revision. The handler has already logged
			// it and counted it on model_responses_dropped_total.
			return nil
		case errors.Is(err, errResponseForeign):
			// Not this loop's request at all. Nothing orders it and no later
			// delivery will, which is the disposition the tool lane and both
			// cold arms already give the same input (#1330, design § 5.2).
			return errs.WrapFatal(err, "agentic-loop", "handleResponseMessage",
				"classify the response against the loop record")
		}
		c.recordTrajectoryObservations(ctx, result)
		// A handler error is this loop's business failure, and the delivery
		// that carried it is done once that failure is durable — not once it
		// has been logged. handleLoopFailure answers for the difference.
		return c.handleLoopFailure(ctx, loopID, entity, failureReasonForHandlerError(err), err)
	}

	c.recordResponseMetrics(response, result, entity)
	return c.persistHandlerResult(ctx, result, publishThenWrite)
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
//
// A decode or payload-type failure is returned as a PermanentDeliveryError:
// the bytes on this message will never decode, so the heartbeat policy
// terminates the delivery rather than discarding it behind a log line.
//
// An empty loop ID with a nil error is NOT a failure and not a decision — it
// means only that process memory does not route this RequestID. The caller
// classifies that against the loops bucket, because memory alone cannot tell a
// settled loop from one this process lost.
func (c *Component) extractAgentResponse(data []byte) (*agentic.AgentResponse, string, error) {
	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
		c.logger.Error("Failed to unmarshal BaseMessage", "error", err)
		return nil, "", natsclient.TerminateDelivery(
			fmt.Errorf("decode agent response BaseMessage: %w", err))
	}

	responsePtr, ok := baseMsg.Payload().(*agentic.AgentResponse)
	if !ok {
		c.logger.Error("Unexpected payload type", "type", fmt.Sprintf("%T", baseMsg.Payload()))
		return nil, "", natsclient.TerminateDelivery(
			fmt.Errorf("agent response payload is %T, not *agentic.AgentResponse", baseMsg.Payload()))
	}

	loopID := c.findLoopIDForRequest(responsePtr.RequestID)
	if loopID == "" {
		return responsePtr, "", nil
	}

	c.logger.Debug("Processing model response",
		slog.String("loop_id", loopID),
		slog.String("request_id", responsePtr.RequestID),
		slog.String("status", responsePtr.Status))

	return responsePtr, loopID, nil
}

// settleResponseWithoutLoop decides a model response whose RequestID routes to
// no loop in this process. A terminal loop's per-loop state is released
// (#1233), which takes its request routing with it, so a response arriving
// after settlement resolves nothing and is an expected drop. A response
// arriving after process replacement looks identical from memory and is the
// opposite case — the loop is live and still owed this response.
// It reports whether the loop was REBUILT here, in which case the caller goes
// on to apply the delivery warm.
func (c *Component) settleResponseWithoutLoop(ctx context.Context, requestID string) (bool, error) {
	loopID := loopIDFromStructuredID(requestID, ":req:")
	// Step 0 before anything else (#1330, design § 3.6): read the record and
	// make it name the loop's newest retained request, so whichever process
	// takes this delivery classifies against a current record rather than one
	// its predecessor died before updating. The read and the adopting write
	// are one critical section inside it.
	adopted, err := c.adoptNewerRetainedRequest(ctx, loopID)
	if err != nil {
		return false, err
	}
	if adopted.presence == loopPresenceStale {
		c.logger.Warn("No loop found for request", "request_id", requestID)
		if c.metrics != nil {
			c.metrics.recordModelResponseDropped("stale_request_id")
		}
		return false, nil
	}
	// Warned, not counted below. The delivery is still outstanding — it
	// retries — and model_responses_dropped_total means work this process
	// decided not to do. Counting a retry there would report one discarded
	// response per redelivery for a response nothing has discarded. The cancel
	// lane already drew this line (signals_dropped_total's help text); these
	// two lanes had not.
	//
	// A response for a request the loop has moved past is owed to nobody: no
	// process, warm or cold, can advance a loop with it. That is the one case
	// this arm acknowledges rather than retrying (#1330, design § 5.2).
	switch orderAgainstPublished(loopID, adopted.entity.PublishedRequestID, requestID) {
	case requestOrderApplied:
		c.logger.WarnContext(ctx, "Model response acknowledged without effect — its request is older than the loop's",
			"loop_id", loopID, "request_id", requestID,
			"published_request_id", adopted.entity.PublishedRequestID)
		if c.metrics != nil {
			c.metrics.recordModelResponseDropped("superseded_request")
		}
		return false, nil
	case requestOrderForeign:
		return false, errs.WrapFatal(
			fmt.Errorf("loop %s: response names request %q, which is not a request of this loop",
				loopID, requestID),
			"agentic-loop", "settleResponseWithoutLoop", "classify the response against the loop record")
	case requestOrderCurrent:
		// The loop is live, unfinished, and this is the answer to the question
		// its record names. Rebuild it here rather than retrying a delivery no
		// process can take (#1330, task 1.2).
		if err := c.restoreLoopFromEvidence(ctx, loopID, adopted, ""); err != nil {
			return false, err
		}
		return true, nil
	}
	// Unnamed — a record written before this field existed names nothing, so
	// the response cannot be ordered against it — and Ahead, where the record
	// has not yet caught up with the request this response answers. Neither
	// authorises a rebuild: there is no fact saying WHICH request to rebuild
	// from. The delivery stays owed, and retries — bounded by the consumer's
	// MaxDeliver, after which it stops being redelivered and is recorded in the
	// framework's MaxDeliver ledger (`internal/maxdelivery`). There is no
	// dead-letter subject; exhaustion is observed, not re-published.
	c.logger.Warn("Model response names a loop this process does not hold",
		"request_id", requestID, "loop_id", loopID)
	return false, fmt.Errorf("loop %q for request %q is not held by this process", loopID, requestID)
}

// handleLoopFailure records failure metrics and publishes failure events, and
// reports whether this loop's terminal failure was durably established.
//
// nil means the failed loop entity, its `COMPLETE_<loopID>` record, its graph
// stamp and every failure event are committed: the business failure is a
// finished effect and the delivery that produced it may ACK. A fatal-classified
// error means the loop is failed in memory behind a partial or absent durable
// record, which is the partial effect the lane quarantines — acknowledging it
// would settle a failure nothing downstream can observe, and redelivering it
// would meet a loop this process has already released. The one ordinary error
// is a loop that could not be transitioned at all: nothing was written, so
// there is no partial effect, and the redelivery resolves against the loop
// record instead of memory.
func (c *Component) handleLoopFailure(
	ctx context.Context, loopID string, entity agentic.LoopEntity, reason string, err error,
) error {
	// Failure-event construction reads token totals twice below. Release the
	// active aggregate only after those terminal consumers have returned.
	defer c.releaseLoopTransientState(loopID)

	c.logger.Error("Loop processing failed", "error", err, "loop_id", loopID, "reason", reason)

	// Transition loop to failed and persist — without this, the loop entity
	// in AGENT_LOOPS KV stays at state=running and downstream watchers
	// (execution-manager) never see the terminal state.
	if transErr := c.handler.loopManager.TransitionLoop(loopID, agentic.LoopStateFailed); transErr != nil {
		c.logger.Warn("Loop could not be transitioned to failed",
			"error", transErr, "loop_id", loopID, "reason", reason)
		return fmt.Errorf("transition loop %s to failed: %w", loopID, transErr)
	}
	c.handler.loopManager.UpdateCompletion(loopID, agentic.OutcomeFailed, "", err.Error())
	established := c.persistLoopState(ctx, loopID)

	if c.metrics != nil && entity.ID != "" {
		duration := time.Since(entity.StartedAt).Seconds()
		c.metrics.recordLoopFailed(reason, entity.Iterations, duration)
	}
	latest, _ := c.handler.GetLoop(loopID)
	failure, _, _ := c.handler.BuildFailureMessages(loopID, reason, err.Error())
	c.recordTerminalObservation(ctx, loopID, agentic.TrajectoryStatusFailed, agentic.TrajectoryErrorUnknown,
		trajectoryTerminalEvidence{Loop: latest, Failure: failure})

	// Every step still runs; the first failure is what the caller settles on.
	if publishErr := c.publishFailureEvents(ctx, loopID, reason, err.Error()); established == nil {
		established = publishErr
	}
	if established == nil {
		return nil
	}
	return errs.WrapFatal(established, "agentic-loop", "handleLoopFailure",
		"loop failure was not durably established")
}

// publishFailureEvents publishes failure events including workflow callback.
//
// Same write-before-publish ordering as persistHandlerResult (post-beta.57):
// KV state and graph triples are stamped BEFORE the JetStream publish so any
// subscriber consuming the failure event and immediately reading
// COMPLETE_{loopID} from the loops KV bucket — rules engine, execution-manager,
// future ops/analytics — finds the state already there. Pre-fix order had
// publish first, KV write last, leaving the same race that beta.57 closed for
// the success path. Audit finding 2026-05-08 (project_audit_findings_2026_05_08.md).
//
// Graph write goes through stampLoopFailureWithBudget so a degraded
// graph-gateway never holds the publish indefinitely (mirrors the beta.57
// stampLoopCompletionWithBudget pattern). KV write is a single fast Put;
// the existing errorCtx 5s detached timeout already bounds the whole
// function so no separate budget is needed.
//
// It returns the first step that did not commit, and attempts every later step
// anyway: a failure event that can still be published is worth publishing even
// though the delivery behind it will not ACK. A nil return is what lets
// handleLoopFailure claim the failure is durable.
func (c *Component) publishFailureEvents(ctx context.Context, loopID, reason, errorMsg string) error {
	errorCtx, cancel := natsclient.DetachContextWithTrace(ctx, 5*time.Second)
	defer cancel()

	failure, failMsgs, err := c.handler.BuildFailureMessages(loopID, reason, errorMsg)
	if err != nil {
		c.logger.Warn("Failed to build failure event", "error", err, "loop_id", loopID)
		return fmt.Errorf("build failure event for loop %s: %w", loopID, err)
	}

	var firstUncommitted error
	// Persist failure to KV first so watchers (rules engine,
	// execution-manager) see COMPLETE_{loopID} when they react to the
	// failure event below.
	if failure != nil {
		if persistErr := c.persistFailureState(errorCtx, loopID, failure); persistErr != nil {
			firstUncommitted = persistErr
		}
	}

	// Stamp graph triples second (under budget). The reorder is the
	// load-bearing change vs pre-fix; the budget cap mirrors the success
	// path's stampLoopCompletionWithBudget so a slow graph-gateway can't
	// stall the publish.
	if failure != nil {
		if stampErr := c.stampLoopFailureWithBudget(errorCtx, loopID, failure); stampErr != nil && firstUncommitted == nil {
			firstUncommitted = stampErr
		}
	}

	// Publish last — every observable side effect is now in place.
	// NATS-less deployments (test scaffolding) skip the publish, matching
	// publishResults' nil-client guard.
	if c.natsClient == nil {
		return firstUncommitted
	}
	for _, msg := range failMsgs {
		if pubErr := c.natsClient.PublishToStream(errorCtx, msg.Subject, msg.Data); pubErr != nil {
			c.logger.Error("Failed to publish failure event", "error", pubErr, "loop_id", loopID)
			if firstUncommitted == nil {
				firstUncommitted = fmt.Errorf("publish failure event %s: %w", msg.Subject, pubErr)
			}
		}
	}
	return firstUncommitted
}

// recordResponseMetrics records metrics and logs for a successful response.
func (c *Component) recordResponseMetrics(response *agentic.AgentResponse, result HandlerResult, entity agentic.LoopEntity) {
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

	var failureReason string
	switch response.Status {
	case agentic.StatusError:
		failureReason = "model_error"
	case agentic.StatusLengthTruncated:
		failureReason = "length_truncated"
	default:
		failureReason = "unknown"
	}
	c.recordTerminalState(result, entity, failureReason)
}

// recordTerminalState fires the active_loops decrement and the matching
// terminal counter for a loop that has just transitioned to LoopStateComplete
// or LoopStateFailed. No-op for non-terminal states. Pulled out of
// recordResponseMetrics so the tool-result path (handleToolResultMessage)
// can decrement the gauge when handleToolsComplete transitions a loop to
// LoopStateFailed (max iterations) without going through a model response —
// without this, every max-iterations failure leaks one unit on the gauge.
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

// graphWritePublishBudget bounds how long persistHandlerResult delays
// publishResults waiting for WriteLoopCompletion / WriteLoopFailure to
// stamp the loop-execution entity in graph KV. Each writeTriple inside
// the writer has its own 5s graphWriterTimeout with retry, so this
// budget caps the total tail latency when retries cascade or the NATS
// subscription hasn't propagated yet.
//
// 2s is generous for healthy graph-gateway (a typical completion stamps
// ~10-15 triples in well under a second). When the budget expires we
// fail the joined delivery and emit a Prom counter so operators can dashboard
// the tail. Tighten if production sees significant tail; widen only
// after confirming the writer's retry budget is the actual bottleneck.
const graphWritePublishBudget = 2 * time.Second

// carrierOrder names the two orders in which the carrier commits a handler
// result: the record write and the publications it implies.
//
// The distinction is not stylistic. Whichever runs first is the one a crash
// between them leaves durable, and the recovery a lane can perform depends on
// which of the two facts survived.
type carrierOrder int

const (
	// writeThenPublish records first and publishes after — the order every
	// lane took before #1330, and the order the approval lane, the
	// approval-timeout sweeper, and any result that CREATES an approval gate
	// keep until #1362.
	writeThenPublish carrierOrder = iota
	// publishThenWrite publishes first and records after, so the record is
	// written only against outputs that already PubAck'd. It is the order the
	// model-response and tool-result lanes take for a non-terminal result that
	// does not gate the loop for approval.
	publishThenWrite
)

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
// A required persistence or publication failure leaves the joined delivery in
// an unknown partial state; the caller quarantines rather than claiming done.
//
// order names which of the two carrier orders the calling lane takes. A
// non-terminal result on the model-response and tool-result lanes publishes
// FIRST and then writes (#1330 L4a), so the record's published_request_id is
// only ever written after that request's PubAck — which is exactly what makes
// it readable as "this request is retained". Every other caller keeps
// write-then-publish: the approval lane and the approval-timeout sweeper move
// in #1362, because the reject-minted crash window that reorder opens is
// closed only by the approval lane's own cold branch, which is #1362's. A
// terminal result keeps write-then-publish on every lane — the terminal record
// and its graph stamps must precede agent.complete, and the single terminal
// owner is #1362's.
//
// An awaiting_approval result keeps it too, whichever order its lane asked
// for, and the tool-result lane is the only producer of one (checkApprovalGate
// in handlers.go). The gate is a durable promise to a HUMAN: published first,
// a crash between the ApprovalPendingEvent and the record leaves an approval
// request visible with no gate behind it, and the replacement's
// approval-response handler stale-drops the answer and acknowledges it. What
// closes that window is the approval lane's own cold branch, which is #1362's
// (design § 5.4, moved task 2.7). Nothing is lost by keeping the old order
// here: a gate result mints no request — its only publication is the
// ApprovalPendingEvent, which carries no MsgID — so mintedRequestID returns ""
// for it and the stamp below is a no-op on this path either way.
func (c *Component) persistHandlerResult(ctx context.Context, result HandlerResult, order carrierOrder) error {
	terminal := result.State == agentic.LoopStateComplete || result.State == agentic.LoopStateFailed
	// A result that creates an approval gate keeps write-then-publish whatever
	// its lane asked for — see the order contract above.
	gated := result.State == agentic.LoopStateAwaitingApproval

	c.recordHandlerResultTrajectory(ctx, result)

	if order == publishThenWrite && !terminal && !gated {
		return c.publishThenPersistResultState(ctx, result)
	}

	// The stamp phase is re-runnable in isolation: persistLoopState
	// compare-and-swaps the whole current entity against the revision this
	// process observed, and persistCompletionState and persistFailureState
	// each Put the whole terminal record — replacement, not an append — while
	// the graph stamps go through WriteLoopCompletion and WriteLoopFailure,
	// which replace the loop entity's single-valued triples.
	//
	// The DELIVERY that would re-run it is not, and the classification answers
	// for the delivery. The handler has already moved this loop in memory
	// before we are called, so the redelivery does not arrive at the same loop
	// it left: a redelivered model response meets the terminal guard
	// (handlers.go:1322-1327) and returns an empty result — no completion
	// record, no publication — which persists nothing, publishes nothing and
	// ACKs. The completion the first attempt built is then gone, and
	// COMPLETE_<loopID> and agent.complete were never emitted. So a stamp
	// failure is a partial effect whose commit is unknown, and the lane
	// quarantines rather than retrying into a handler that will refuse to
	// rebuild the result. L4 (#1330) — replay that reproduces the original
	// result — is what relaxes this to Retry.
	// The stamp is here rather than at the mint for every lane, including the
	// ones that still write before they publish: one home for "the loop names
	// the request it minted" is what keeps a lane from silently losing it. On
	// THIS order the stamp precedes the publication — the window the approval
	// lane and the terminal paths already carry until #1362, where the record
	// names a request whose PubAck has not landed. Moving the call does not
	// change that order; it is the order these lanes already had.
	if err := c.stampPublishedRequest(result); err != nil {
		return errs.WrapFatal(err, "agentic-loop", "persistHandlerResult",
			"name the published request on the loop this process holds")
	}
	if err := c.persistResultState(ctx, result, terminal); err != nil {
		// A lost compare-and-swap is the one failure here that is NOT unknown:
		// nothing was written, nothing was published, and persistLoopState has
		// already released this loop's in-process state so the redelivery
		// re-enters against the record that won.
		if errors.Is(err, natsclient.ErrKVRevisionMismatch) {
			return err
		}
		return errs.WrapFatal(err, "agentic-loop", "persistHandlerResult",
			"handler result state has unknown durability after the loop was already mutated")
	}

	// publishResults is commit-unknown for its own second reason. It publishes
	// result.PublishedMessages one at a time, so a failure on the third leaves
	// two already PubAck'd — including tool.execute messages whose executors
	// are running. Until deterministic tool-call identity lands (L2) the re-run
	// mints fresh call IDs, so a Retry here would run the first two tools twice
	// and TOOL_CALL_OUTCOMES could not dedupe them: different CallIDs are
	// different calls. L4 (#1330) relaxes this to identity-based replay.
	if err := c.publishResults(ctx, result); err != nil {
		return errs.WrapFatal(err, "agentic-loop", "persistHandlerResult",
			"published results have unknown durability")
	}
	if terminal {
		c.releaseLoopTransientState(result.LoopID)
	}
	return nil
}

// publishThenPersistResultState is the L4a order for a non-terminal result on
// the model-response and tool-result lanes: publish, then compare-and-swap the
// record.
//
// The crash window it opens is the one the design names W4 — the next request
// is retained and the record still names the previous one — and it is closed
// by identity: the redelivery re-mints the same request name, finds it already
// retained, adopts it instead of publishing a second copy, and writes the
// record. The window the old order opened is the opposite one and has no such
// closure: a record that names a request nothing ever published leaves every
// later classification reading a request identity no reader can find.
func (c *Component) publishThenPersistResultState(ctx context.Context, result HandlerResult) error {
	if err := c.publishResults(ctx, result); err != nil {
		return errs.WrapFatal(err, "agentic-loop", "persistHandlerResult",
			"published results have unknown durability")
	}
	// The request this result minted is now retained, so this is the first
	// moment the loop may name it. Before the PubAck the name is an intention,
	// and a SIBLING LANE writing this same loop would have made that intention
	// durable on its behalf.
	if err := c.stampPublishedRequest(result); err != nil {
		return errs.WrapFatal(err, "agentic-loop", "persistHandlerResult",
			"name the published request on the loop this process holds")
	}
	if err := c.persistResultState(ctx, result, false); err != nil {
		// A compare-and-swap loss already released this loop and is transient:
		// the redelivery re-enters against the record that won. The test is
		// the sentinel, never errs.IsTransient — that one matches any error
		// whose TEXT contains "unavailable" or "timeout", which would hand a
		// commit-unknown KV failure a Retry it has not earned.
		if errors.Is(err, natsclient.ErrKVRevisionMismatch) {
			return err
		}
		return errs.WrapFatal(err, "agentic-loop", "persistHandlerResult",
			"handler result state has unknown durability after its results were published")
	}
	return nil
}

// persistResultState runs the whole stamp phase for one handler result: the
// loop entity, then — for a terminal result — its completion or failure record
// and the matching graph stamps. Every step writes a whole value, so the phase
// is idempotent on its own; its caller decides what a failure means for the
// delivery that produced it.
func (c *Component) persistResultState(ctx context.Context, result HandlerResult, terminal bool) error {
	if err := c.persistLoopState(ctx, result.LoopID); err != nil {
		return err
	}
	if !terminal {
		return nil
	}
	if result.CompletionState != nil {
		if err := c.persistCompletionState(ctx, result.LoopID, result.CompletionState); err != nil {
			return err
		}
		if err := c.stampLoopCompletionWithBudget(ctx, result.LoopID, result.CompletionState); err != nil {
			return err
		}
	} else if result.FailureState != nil {
		// The terminal RECORD before its graph triples, mirroring the
		// completion branch above. Without this the failure branch stamped
		// triples and ACKed with COMPLETE_<loopID> absent, so every watcher
		// that reads the terminal record out of KV — rules engine,
		// execution-manager, the SSE path — saw a loop that ended and no
		// result for it. persistFailureState is otherwise reachable only from
		// publishFailureEvents (:1700), which this route never enters: the
		// three results that carry a FailureState here return no error to
		// handleLoopFailure, and the one that does (HandleModelResponse's
		// timeout, handlers.go:1316) never reaches this function. So the write
		// happens exactly once on every path, and the failure event is
		// published exactly once — by publishResults here, or by
		// publishFailureEvents there, never both.
		if err := c.persistFailureState(ctx, result.LoopID, result.FailureState); err != nil {
			return err
		}
		if err := c.stampLoopFailureWithBudget(ctx, result.LoopID, result.FailureState); err != nil {
			return err
		}
	}
	// Terminal-tool-less synthesis (#133). Detected in
	// handleCompleteResponse; emitted here on the graph path so the
	// triples ride the same publish budget as the loop completion
	// stamp and downstream rules see them on the same KV revision
	// the agent.complete.* event refers to.
	if result.SyntheticDecide != nil {
		if err := c.stampSyntheticDecideWithBudget(ctx, result.SyntheticDecide); err != nil {
			return err
		}
	}
	return nil
}

// stampLoopCompletionWithBudget invokes WriteLoopCompletion under the
// graphWritePublishBudget and reports cancellation instead of publishing past it.
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
	timedOut := runWithBudget(ctx, graphWritePublishBudget, func(bctx context.Context) {
		c.graphWriter.WriteLoopCompletion(bctx, completion, evidenceIncomplete)
	})
	if timedOut {
		c.logger.Warn("graph write budget expired before completion stamp returned",
			"loop_id", loopID,
			"budget", graphWritePublishBudget,
			"state", "complete")
		if c.metrics != nil {
			c.metrics.recordGraphWritePublishTimeout("complete")
		}
		return fmt.Errorf("completion graph stamp for loop %s did not complete within lifecycle budget: %w",
			loopID, errors.Join(context.DeadlineExceeded, ctx.Err()))
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
	timedOut := runWithBudget(ctx, graphWritePublishBudget, func(bctx context.Context) {
		c.graphWriter.WriteSyntheticDecide(bctx, req.LoopID, req.Reason)
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
	return nil
}

// stampLoopFailureWithBudget mirrors stampLoopCompletionWithBudget for
// the failure branch. semteams smoke-#7 reproduced the race on
// researcher-failure specifically (failed loops still need
// agent.loop.parent visible for ancestry walks), so the failure path
// gets the same budgeted-write treatment as completion.
func (c *Component) stampLoopFailureWithBudget(ctx context.Context, loopID string, failure *agentic.LoopFailedEvent) error {
	if c.graphWriter == nil {
		return nil
	}
	evidenceIncomplete := c.trajectoryAuditLoss.observed(loopID)
	timedOut := runWithBudget(ctx, graphWritePublishBudget, func(bctx context.Context) {
		c.graphWriter.WriteLoopFailure(bctx, failure, evidenceIncomplete)
	})
	if timedOut {
		c.logger.Warn("graph write budget expired before failure stamp returned",
			"loop_id", loopID,
			"budget", graphWritePublishBudget,
			"state", "failure")
		if c.metrics != nil {
			c.metrics.recordGraphWritePublishTimeout("failure")
		}
		return fmt.Errorf("failure graph stamp for loop %s did not complete within lifecycle budget: %w",
			loopID, errors.Join(context.DeadlineExceeded, ctx.Err()))
	}
	return nil
}

// runWithBudget runs fn synchronously under a bounded child context. The caller
// never observes completion while delivery-derived work is still live: that is
// the whole point of the synchronous call, because the goroutine-plus-select
// shape this replaced returned while the work was still running and let a
// delivery settle before its own graph write finished.
//
// The consequence is that the budget bounds only a callee that honors
// cancellation. That is a contract, not an assumption: graph dependencies are
// lifecycle participants under ADR-049 (docs/adr/049-lifecycle-harness.md),
// which requires Stop and context cancellation to be honored, and a dependency
// that ignores bctx fails lifecycle review rather than being defended against
// here. A writer that blocked past the budget would hold the callback past the
// lane's AckWait and be redelivered while the first attempt still ran — the
// reason the residual is declared in the change's design.md rather than left
// implicit.
//
// Extracted so the timeout-vs-completion contract is unit-testable
// without mocking the natsclient or graphWriter. The function is
// deliberately small: testing it covers the bounded-wait shape;
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
func (c *Component) handleToolResultMessage(ctx context.Context, data []byte) error {
	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
		c.logger.Error("Failed to unmarshal BaseMessage", "error", err)
		return natsclient.TerminateDelivery(
			fmt.Errorf("decode tool result BaseMessage: %w", err))
	}

	toolResultPtr, ok := baseMsg.Payload().(*agentic.ToolResult)
	if !ok {
		c.logger.Error("Unexpected payload type", "type", fmt.Sprintf("%T", baseMsg.Payload()))
		return natsclient.TerminateDelivery(
			fmt.Errorf("tool result payload is %T, not *agentic.ToolResult", baseMsg.Payload()))
	}
	toolResult := *toolResultPtr

	// Find loop ID for this tool execution. Empty here means we drained the execution ID at
	// the previous turn boundary (GetAndClearToolResults evicts the routing
	// entry to drop late re-deliveries), the loop settled and released its
	// per-loop state (#1233), or we never tracked it. All three are expected
	// settled-drops, counted and warned, never errors.
	// Returning here is load-bearing — proceeding would land the late result
	// in PendingToolResults and surface as a duplicate tool message in the
	// next turn's request. It is also what keeps a released loop
	// indistinguishable from a present terminal one: DeleteLoop clears opaque
	// execution routing entries by their mapped owner, so the direct lookup
	// cannot resolve a loop that is gone and hand it to
	// HandleToolResult, which would fail instead of dropping.
	// The fourth case the comment above did not name: the loop is live and
	// this process is simply not the one holding it. Memory reads identically
	// to the three expected drops, so the loops bucket decides. The lookup key
	// is the framework execution identity, not the provider call id — the
	// routing entry is minted under it.
	loopID := c.findLoopIDForToolCall(toolResult.ExecutionID)
	if loopID == "" {
		rebuilt, err := c.settleToolResultWithoutLoop(ctx, toolResult)
		if err != nil || !rebuilt {
			return err
		}
		// Rebuilt: the routing entry the batch restore seated is what the
		// delivery now takes, through the ordinary warm path.
		loopID = c.findLoopIDForToolCall(toolResult.ExecutionID)
		if loopID == "" {
			return errs.WrapFatal(
				fmt.Errorf("tool result %q: the loop was rebuilt and its retained response dispatched no such execution",
					toolResult.ExecutionID),
				"agentic-loop", "handleToolResultMessage", "route the result to the rebuilt loop")
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

	// Classify the delivery against the loop's outstanding request BEFORE the
	// handler touches anything (#1330, design § 5.3; owner ruling Q7 for the
	// terminal arm, docket OQ5 for the placement). HandleToolResult stores the
	// result and acts on StopLoop ahead of its own terminal guard, so a result
	// the loop has moved past has to be settled here or not at all.
	//
	// A GetLoop error means the loop was released between the routing lookup
	// above and this line. Nothing is classified then — HandleToolResult
	// answers that race exactly as it did before this check existed.
	if entity, entErr := c.handler.GetLoop(loopID); entErr == nil {
		apply, classifyErr := c.classifyRedeliveredToolResult(ctx, loopID, entity, toolResult)
		if classifyErr != nil {
			return classifyErr
		}
		if !apply {
			return nil
		}
	} else {
		// A skipped classification is a declared event, not a private choice.
		// Continuing is safe — HandleToolResult answers this race exactly as
		// it did before the check existed, and a loop released between the
		// routing lookup and this line has nothing left to classify against —
		// but an operator reading "this result was applied to a settled loop"
		// needs the line that says the guard did not run.
		c.logger.WarnContext(ctx, "Tool result not classified against the loop — it was released mid-delivery",
			slog.String("loop_id", loopID),
			slog.String("execution_id", toolResult.ExecutionID),
			slog.String("call_id", toolResult.CallID),
			slog.String("error", entErr.Error()))
		if c.metrics != nil {
			c.metrics.recordRecoveryDegradation("tool_result_classification")
		}
	}

	// Handle the tool result using the message handler
	result, err := c.handler.HandleToolResult(ctx, loopID, toolResult)
	if err != nil {
		return c.settleFailedToolResult(ctx, loopID, result, err)
	}

	// Decrement active_loops if HandleToolResult drove the loop to a terminal
	// state. handleToolsComplete (handlers.go) transitions to LoopStateFailed
	// when max_iterations trips while tools were in flight; without this
	// recording the gauge would not be decremented for that path. The
	// model-response path (handleResponseMessage) records via
	// recordResponseMetrics and is unchanged.
	if result.State == agentic.LoopStateComplete || result.State == agentic.LoopStateFailed {
		failureReason := "unknown"
		if result.MaxIterationsReached {
			failureReason = "max_iterations"
		}
		if entity, entErr := c.handler.GetLoop(loopID); entErr == nil {
			c.recordTerminalState(result, entity, failureReason)
		}
	}

	// Publish results, persist state, and handle terminal states (StopLoop).
	// persistHandlerResult covers publishResults + persistLoopState for all states,
	// plus finalization and completion-state persistence
	// when the loop reaches a terminal state.
	return c.persistHandlerResult(ctx, result, publishThenWrite)
}

// settleFailedToolResult decides a tool result whose handler returned an error
// (#1343).
//
// This branch used to record the trajectory, log, and return nil, which is ACK:
// an executor's completed work was discarded behind a log line, and the worst
// case was the terminal one. HandleToolResult's timeout branch
// (handlers.go:2420-2433) transitions the loop to failed, builds its failure
// record and its failure publications, and returns them WITH the error — so the
// old branch acknowledged a terminal failure that was never written and never
// published. The loop record stayed non-terminal forever while the input that
// would have settled it was gone.
//
// A terminal result is therefore persisted like any other terminal result, and
// the delivery settles on what that returns: nil once the record and its
// publications are committed, and persistHandlerResult's own fatal
// classification when the write or the publish left the commit unknown.
//
// A non-terminal handler error produced nothing to persist and got as far as it
// got — StoreToolResult may have landed while RemovePendingTool did not, a
// queued call may have been popped and not dispatched — so the commit is
// unknown and the lane quarantines rather than ACKing work the executor really
// did.
//
// Cancellation is the one exception, and only the kind that is provably
// pre-mutation. HandleToolResult checks its context three times: once before it
// touches anything (handlers.go:2384) and twice on the tools-complete path
// (handlers.go:2684 in handleToolsComplete, :2802 in the publishIterationRequest
// it calls), after StoreToolResult, RemovePendingTool,
// IncrementIteration and GetAndClearToolResults have moved in-process state.
// Only the first carries errCancelledBeforeMutation, and only it retries: a
// shutting-down process that mutated nothing must not latch a false
// delivery-ownership fatal on every clean stop that catches a tool result in
// flight. The other two are cancellations after a mutation the message cannot
// rebuild — the probe in round 3 drove iterations 0 → 1 with zero publications,
// and the replay then hit the iteration budget and returned terminal
// max_iterations without ever issuing the request it interrupted — so they take
// the same Quarantine as any other partial effect. The cancellation is not
// always a shutdown either: delivery_settlement.go:366-373 cancels the work
// context when a heartbeat InProgress fails, in a process that is still alive.
// L4 (#1330) is what relaxes this to replay.
func (c *Component) settleFailedToolResult(
	ctx context.Context, loopID string, result HandlerResult, cause error,
) error {
	c.logger.Error("Failed to handle tool result", "error", cause, "loop_id", loopID)

	if result.State.IsTerminal() {
		// persistHandlerResult records the trajectory and releases the
		// per-loop aggregate itself once the terminal state is durable. A
		// terminal result keeps write-then-publish on every lane.
		return c.persistHandlerResult(ctx, result, writeThenPublish)
	}

	c.recordHandlerResultTrajectory(ctx, result)
	if errors.Is(cause, errCancelledBeforeMutation) {
		return cause
	}
	return errs.WrapFatal(cause, "agentic-loop", "handleToolResultMessage",
		"tool result handling left this loop in an unknown state")
}

// settleToolResultWithoutLoop decides a tool result whose CallID routes to no
// loop in this process. The ToolResult payload carries its own LoopID, and the
// structured CallID grammar carries one too; either identifies the record to
// read. Stale is the expected settled-drop the surrounding comment describes;
// live means an executor's completed work would be destroyed by an ACK.
// It reports whether the loop was REBUILT here, in which case the caller goes
// on to apply the delivery warm.
func (c *Component) settleToolResultWithoutLoop(ctx context.Context, toolResult agentic.ToolResult) (bool, error) {
	loopID := toolResult.LoopID
	if loopID == "" {
		loopID = loopIDFromStructuredID(toolResult.CallID, ":tool:")
	}
	// Step 0 before anything else, as on the response lane above.
	adopted, err := c.adoptNewerRetainedRequest(ctx, loopID)
	if err != nil {
		return false, err
	}
	if adopted.presence == loopPresenceStale {
		// Named for the identity the lookup actually failed on: the routing
		// entry is keyed by execution identity, so a miss is a stale
		// execution, not a stale call id. metrics.go documents the same word.
		c.logger.Warn("No loop found for tool execution",
			"execution_id", toolResult.ExecutionID, "call_id", toolResult.CallID)
		if c.metrics != nil {
			c.metrics.recordToolResultDropped("stale_execution")
		}
		return false, nil
	}
	// Warned, not counted below, for the same reason as the response lane
	// above: a retried tool result is not a dropped one, and an executor's
	// work is still owed to whichever process holds that loop.
	//
	// Classify against the record step 0 brought forward. This is the cold
	// half of the tool lane's classification (#1330, design § 5.3): the
	// process that will take the redelivery is not this one, but a result the
	// loop has already moved past is owed to nobody at all.
	switch orderAgainstPublished(loopID, adopted.entity.PublishedRequestID, toolResult.RequestID) {
	case requestOrderApplied:
		c.logger.WarnContext(ctx, "Tool result acknowledged without effect — its request is older than the loop's",
			"loop_id", loopID, "execution_id", toolResult.ExecutionID,
			"result_request_id", toolResult.RequestID,
			"published_request_id", adopted.entity.PublishedRequestID)
		if c.metrics != nil {
			c.metrics.recordToolResultDropped("older_request")
		}
		return false, nil
	case requestOrderForeign:
		return false, errs.WrapFatal(
			fmt.Errorf("loop %s: tool result names request %q, which is not a request of this loop",
				loopID, toolResult.RequestID),
			"agentic-loop", "settleToolResultWithoutLoop", "classify the tool result against the loop record")
	case requestOrderCurrent:
		// An execution the record has ALREADY applied is a replay, not new
		// work: a lost ACK is ordinary at-least-once delivery, and the
		// ordinary apply drops the route, so a same-process redelivery lands
		// here too. Ordering cannot see it — the batch still belongs to the
		// current request — so membership is the only fact that decides.
		//
		// Checked BEFORE the rebuild, for two reasons. restoreToolBatch
		// deliberately leaves applied executions unrouted, so the rebuild
		// would succeed and the lane would then find no route and Terminate,
		// quarantining a whole tool lane over a routine redelivery. And
		// touching nothing is what preserves the unfinished siblings: each of
		// them rebuilds the batch on its own arrival.
		//
		// The TERMINAL arm stays membership-free (owner ruling Q7): there,
		// re-deriving which side of the settlement a result fell on changes
		// nothing the delivery can do. Here it is the whole decision.
		if _, applied := adopted.entity.PendingToolResults[toolResult.ExecutionID]; applied {
			c.logger.WarnContext(ctx, "Tool result acknowledged without effect — the record already applied it",
				"loop_id", loopID, "execution_id", toolResult.ExecutionID,
				"request_id", toolResult.RequestID)
			if c.metrics != nil {
				c.metrics.recordToolResultDropped("already_applied")
			}
			return false, nil
		}
		// The executor's work belongs to the batch the record names, and no
		// process holds that loop. Rebuild it here — record, retained request,
		// retained response — rather than retrying a delivery nobody can take
		// (#1330, task 1.2).
		if err := c.restoreLoopFromEvidence(ctx, loopID, adopted, toolResult.ExecutionID); err != nil {
			return false, err
		}
		return true, nil
	}
	// Unnamed — neither side carries a request name — and Ahead, where the
	// record has not caught up with the request this result belongs to.
	// Neither authorises a rebuild, for the same reason as the response lane:
	// no fact names which request to rebuild from. The delivery retries,
	// bounded by the consumer's MaxDeliver, after which it stops being
	// redelivered and is recorded in the framework's MaxDeliver ledger
	// (`internal/maxdelivery`) — so an input no process can ever place does not
	// retry forever, and its exhaustion is observable rather than silent.
	c.logger.Warn("Tool result names a loop this process does not hold",
		"execution_id", toolResult.ExecutionID, "call_id", toolResult.CallID, "loop_id", loopID)
	return false, fmt.Errorf("loop %q for tool call %q is not held by this process", loopID, toolResult.CallID)
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
		// A minted model request is checked for identity before it goes out
		// (owner ruling Q4 on #1330). The duplicate window is a bonus, not the
		// guarantee: it is time-bounded, and the redelivery that re-mints this
		// request can arrive long after it closes.
		if msg.MsgID != "" {
			published, err := c.adoptRetainedRequest(ctx, result.LoopID, msg.MsgID)
			if err != nil {
				return err
			}
			if published {
				continue
			}
		}
		// Use JetStream for publishing to ensure delivery. A message that
		// carries a MsgID publishes through the Nats-Msg-Id path so the server
		// rejects a duplicate of the same logical message inside the stream's
		// Duplicates window (owner ruling Q5 on #1330). An empty MsgID is a
		// drop-in for PublishToStream.
		if err := c.natsClient.PublishToStreamWithMsgID(ctx, msg.Subject, msg.Data, msg.MsgID); err != nil {
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

// persistCompletionState persists the enriched completion state to KV.
// Key pattern: COMPLETE_{loopID} for rules engine to watch.
// The rules engine can then trigger follow-up actions based on completion data.
func (c *Component) persistCompletionState(ctx context.Context, loopID string, completion *agentic.LoopCompletedEvent) error {
	if c.loopsBucket == nil || completion == nil {
		return nil
	}

	data, err := json.Marshal(completion)
	if err != nil {
		return fmt.Errorf("marshal completion state for loop %s: %w", loopID, err)
	}

	// Key pattern: COMPLETE_{loopID} for rules engine to watch
	key := fmt.Sprintf("COMPLETE_%s", loopID)
	if _, err := c.loopsBucket.Put(ctx, key, data); err != nil {
		return fmt.Errorf("persist completion state for loop %s: %w", loopID, err)
	}

	c.logger.Debug("Persisted completion state",
		slog.String("loop_id", loopID),
		slog.String("key", key),
		slog.String("role", completion.Role))
	return nil
}

// persistFailureState persists the failure state to KV.
// Key pattern: COMPLETE_{loopID} — same as success, so watchers don't need
// to distinguish between success/failure key patterns. The outcome field
// in the serialized event tells them what happened.
//
// It reports its failure for the same reason persistCompletionState and
// persistCancellationState do: this record is what every downstream watcher
// reads to learn the loop is over, so a Put that did not land cannot be a log
// line under a delivery that then ACKs.
func (c *Component) persistFailureState(ctx context.Context, loopID string, failure *agentic.LoopFailedEvent) error {
	if c.loopsBucket == nil || failure == nil {
		return nil
	}

	data, err := json.Marshal(failure)
	if err != nil {
		return fmt.Errorf("marshal failure state for loop %s: %w", loopID, err)
	}

	key := fmt.Sprintf("COMPLETE_%s", loopID)
	if _, err := c.loopsBucket.Put(ctx, key, data); err != nil {
		return fmt.Errorf("persist failure state for loop %s: %w", loopID, err)
	}

	c.logger.Debug("Persisted failure state",
		slog.String("loop_id", loopID),
		slog.String("key", key),
		slog.String("reason", failure.Reason))
	return nil
}

// persistCancellationState persists the cancellation state to KV.
// Uses same COMPLETE_{loopID} key pattern so watchers handle all terminal states uniformly.
func (c *Component) persistCancellationState(ctx context.Context, loopID string, cancelled *agentic.LoopCancelledEvent) error {
	if c.loopsBucket == nil || cancelled == nil {
		return nil
	}

	data, err := json.Marshal(cancelled)
	if err != nil {
		return fmt.Errorf("marshal cancellation state for loop %s: %w", loopID, err)
	}

	key := fmt.Sprintf("COMPLETE_%s", loopID)
	if _, err := c.loopsBucket.Put(ctx, key, data); err != nil {
		return fmt.Errorf("persist cancellation state for loop %s: %w", loopID, err)
	}

	c.logger.Debug("Persisted cancellation state",
		slog.String("loop_id", loopID),
		slog.String("key", key),
		slog.String("cancelled_by", cancelled.CancelledBy))
	return nil
}

// rememberLoopRevision records the revision a write committed at, or a read
// observed, as the compare-and-swap input for this loop's next write.
func (c *Component) rememberLoopRevision(loopID string, revision uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loopRevisions == nil {
		c.loopRevisions = make(map[string]uint64)
	}
	c.loopRevisions[loopID] = revision
}

// observedLoopRevision answers the revision this process holds for the loop.
// The bool is load-bearing: revision 0 means "must not exist" to a NATS KV
// update, so an absent entry must never be spelled as a zero.
func (c *Component) observedLoopRevision(loopID string) (uint64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	revision, held := c.loopRevisions[loopID]
	return revision, held
}

// forgetLoopRevision drops the loop's retained revision. Called from
// releaseLoopTransientState with the rest of the loop's per-loop state, and
// from the compare-and-swap loss path, where the record this process was
// writing against is provably no longer the one in the bucket.
func (c *Component) forgetLoopRevision(loopID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.loopRevisions, loopID)
}

// createLoopState writes a loop's FIRST record, by Create rather than Put, and
// seeds the revision every later write compares against (#1330, docket OQ3).
//
// Create is what makes a second consumer's birth for the same loop ID fail
// instead of overwriting: a Put here silently replaced a record that already
// carried iterations, a published request and an applied set with a fresh
// iteration-zero record, which is the restart hole this change closes. A
// refused birth returns ErrKVKeyExists to the caller, which releases the loop
// this process just built in memory — the record in the bucket belongs to
// whoever created it.
func (c *Component) createLoopState(ctx context.Context, loopID string) error {
	if c.loopsBucket == nil {
		return nil
	}

	// Under the same lock as every other record write: birth is the write that
	// seeds the revision, and a write that started before it must not commit
	// after it with a revision it never saw.
	c.loopRecordMu.Lock()
	defer c.loopRecordMu.Unlock()

	data, err := c.marshalLoopRecord(loopID)
	if err != nil {
		return err
	}

	revision, err := c.loopsBucket.Create(ctx, loopID, data)
	if err != nil {
		if natsclient.IsKVConflictError(err) {
			return fmt.Errorf("create loop state %s: %w", loopID, natsclient.ErrKVKeyExists)
		}
		return fmt.Errorf("create loop state %s: %w", loopID, err)
	}
	c.rememberLoopRevision(loopID, revision)
	return nil
}

// mintedRequestID reports the AgentRequest this handler result minted, and ""
// when it minted none.
//
// PublishedMessage.MsgID already IS that identity: every request publish
// carries its deterministic RequestID there so the server can collapse a
// duplicate, and publishResults reads the same field to decide whether the
// stream already retains it. Reading it back here keeps one spelling of the
// fact rather than adding a second one to HandlerResult for the carrier to
// disagree with.
//
// A result with two different minted names is refused rather than resolved:
// one result mints at most one request (the three mint sites each append
// exactly one), so a second name means the shape changed underneath this
// function and the record has no unambiguous request to name.
func mintedRequestID(result HandlerResult) (string, error) {
	minted := ""
	for _, msg := range result.PublishedMessages {
		if msg.MsgID == "" || msg.MsgID == minted {
			continue
		}
		if minted != "" {
			return "", fmt.Errorf("loop %s: one handler result minted two requests, %q and %q",
				result.LoopID, minted, msg.MsgID)
		}
		minted = msg.MsgID
	}
	return minted, nil
}

// stampPublishedRequest records the request this result minted as the one the
// loop's record will name (LoopEntity.PublishedRequestID, invariant I1).
//
// It lives at the CARRIER, not at the mint, because I1 is a claim about
// durability and only the carrier knows when the request became durable. The
// two iteration mint sites — publishIterationRequest and emitRetryRequest —
// build the request and hand it to the carrier; on the model-response and
// tool-result lanes the carrier publishes first, so the stamp lands after the
// PubAck and before the record write (owner ruling #1330 Q1, 2026-09-23).
// Birth is the one lane that stamps at the mint, by the same ruling: it writes
// the record BEFORE the first publish, so the name has to exist first.
//
// The hazard this closes is a SIBLING LANE, not this one. Stamped at the mint,
// PublishedRequestID = R2 is visible to every writer of this loop the moment
// the request is built: a deferred continuation on the task lane, or a tool
// lane's compare-and-swap, renders the shared entity and commits a record
// naming R2 over a stream that still retains only R1 — and a crash there
// leaves a loop no replacement can adopt (the fatal older-retained-request
// branch) and no operator can settle.
//
// Under loopRecordMu for the same reason every record write is: the lanes of
// this process interleave, and a stamp landing inside another lane's
// render-observe-write critical section would put the record's name and the
// revision it was rendered from on opposite sides of a publication. Its own
// short critical section, not the caller's — persistLoopState takes the same
// lock next, and a sync.Mutex is not reentrant. Between the two the loop names
// a request that is already retained, so any lane that writes there is
// writing a true record.
//
// TrackRequest stays at the mint. Route, outstanding and the deferred turn's
// carrier are attach-order facts about what this process is doing, not claims
// about what the stream holds.
func (c *Component) stampPublishedRequest(result HandlerResult) error {
	minted, err := mintedRequestID(result)
	if err != nil {
		return err
	}
	if minted == "" {
		return nil
	}
	c.loopRecordMu.Lock()
	defer c.loopRecordMu.Unlock()
	return c.handler.loopManager.SetPublishedRequest(result.LoopID, minted)
}

// persistLoopState writes the loop's record under compare-and-swap against the
// revision this process observed (#1330, owner ruling Q2).
//
// Every caller takes this form. Two lanes reach it through persistHandlerResult
// AFTER their publications have PubAck'd, which is what makes the written
// PublishedRequestID mean "this request is durably retained" rather than "a
// process meant to publish one"; the rest write before they publish and keep
// that order until #1362.
//
// A lost CAS is not a retry-in-place. The record moved, so this process is
// holding a loop somebody else has advanced: its in-memory state is released
// and the delivery is returned transient, so the redelivery re-enters against
// the record that won rather than re-applying against a loop that no longer
// exists. Without the release the loser keeps a stale conversation forever.
func (c *Component) persistLoopState(ctx context.Context, loopID string) error {
	if c.loopsBucket == nil {
		return nil
	}

	// Render, observe and write as one critical section (loopRecordMu). Two
	// lanes of THIS process write the same loop — the carrier, cancel,
	// approval, the timeout sweeper, step 0's adopt — and a revision read
	// outside the lock is stale the moment another lane commits: the CAS then
	// refuses a write that has no conflict to report, and the loop is released
	// as though a foreign process had taken it. The render is inside for the
	// same reason: rendering before the lock would marshal a record from one
	// observation and commit it against another.
	//
	// What this lock does NOT do is freeze the loop's in-memory state.
	// marshalLoopRecord takes its snapshot through LoopManager.GetLoop, under
	// the MANAGER's mutex, and marshals it after that mutex is released — so a
	// mutation landing on the handler goroutine in between is not in these
	// bytes. That is a boundary, not a loss: the mutation belongs to another
	// delivery, and that delivery's own write carries it. What the snapshot
	// must be is a VALUE, which is why GetLoop copies the applied set:
	// marshalling a map another goroutine is writing is a data race.
	c.loopRecordMu.Lock()
	defer c.loopRecordMu.Unlock()

	data, err := c.marshalLoopRecord(loopID)
	if err != nil {
		return err
	}

	revision, held := c.observedLoopRevision(loopID)
	if !held {
		// No observation, no compare-and-swap. Writing anyway would mean
		// either a blind Put (the last-writer-wins shape this replaces) or an
		// update against revision zero, which NATS reads as "must not exist"
		// and which would refuse every live record for the wrong reason.
		//
		// Fatal, not transient: redelivering into the same process reaches the
		// same warm loop with the same missing observation. Every warm path
		// either created this record or read it, so arriving here at all means
		// a lane reached the carrier without ever observing the record.
		return errs.WrapFatal(
			fmt.Errorf("loop %s: this process holds no observed record revision to write against", loopID),
			"agentic-loop", "persistLoopState", "observe loop record revision")
	}

	committed, err := c.loopsBucket.Update(ctx, loopID, data, revision)
	if err != nil {
		if natsclient.IsKVConflictError(err) {
			c.releaseLoopTransientState(loopID)
			return errs.WrapTransient(
				fmt.Errorf("loop %s record moved past revision %d: %w", loopID, revision, natsclient.ErrKVRevisionMismatch),
				"agentic-loop", "persistLoopState", "compare-and-swap loop record")
		}
		return fmt.Errorf("persist loop state %s: %w", loopID, err)
	}
	c.rememberLoopRevision(loopID, committed)
	return nil
}

// marshalLoopRecord renders the loop's current in-memory entity as the bytes
// its record holds.
func (c *Component) marshalLoopRecord(loopID string) ([]byte, error) {
	entity, err := c.handler.GetLoop(loopID)
	if err != nil {
		return nil, fmt.Errorf("get loop %s for persistence: %w", loopID, err)
	}
	data, err := json.Marshal(entity)
	if err != nil {
		return nil, fmt.Errorf("marshal loop entity %s: %w", loopID, err)
	}
	return data, nil
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
		if err := c.handleCancelSignal(ctx, signal); err != nil {
			if errs.IsFatal(err) {
				return natsclient.DeliveryDecisionQuarantine, err
			}
			return natsclient.DeliveryDecisionRetry, err
		}
		return natsclient.DeliveryDecisionAck, nil
	default:
		return natsclient.DeliveryDecisionTerminate, fmt.Errorf("unsupported signal type %q for loop %q", signal.Type, signal.LoopID)
	}
}

// settleUncancellableLoop decides a cancel whose CancelLoop refused. Neither
// refusal is transient, and handleSignalMessage maps every non-fatal error to
// Retry, so a bare wrap left the signal redelivering against a decision that
// can never change.
//
// Already terminal is idempotent success: the loop is in exactly the state the
// operator asked for. Not found in memory is the same two-case question every
// other lane asks, and the loops bucket answers it: no record means nothing to
// cancel anywhere; a live record means the loop is running in another process
// and the cancel is still owed to it.
func (c *Component) settleUncancellableLoop(ctx context.Context, loopID string, cause error) error {
	if errs.IsInvalid(cause) {
		c.logger.Info("Cancel signal for an already-terminal loop; acknowledging without effect",
			"loop_id", loopID, "reason", cause)
		if c.metrics != nil {
			c.metrics.recordSignalDropped("already_terminal")
		}
		return nil
	}
	if errors.Is(cause, ErrLoopNotFound) && c.classifyMissingLoop(ctx, loopID) == loopPresenceStale {
		c.logger.Warn("Cancel signal for a loop with no durable record; acknowledging without effect",
			"loop_id", loopID)
		if c.metrics != nil {
			c.metrics.recordSignalDropped("stale_loop_id")
		}
		return nil
	}
	return fmt.Errorf("cancel loop %q: %w", loopID, cause)
}

// handleCancelSignal handles a cancel signal for a loop
func (c *Component) handleCancelSignal(ctx context.Context, signal agentic.UserSignal) error {
	loopID := signal.LoopID

	// Drain any in-flight tool calls into synth-results BEFORE the
	// CancelLoop transition so tool-pair integrity is preserved in
	// KV-persisted context. Mode (e) of orphan-tool-call recovery —
	// without this, a cancelled loop's stored context would carry
	// assistant tool_calls with no matching tool_results, 400ing any
	// downstream replay.
	c.handler.drainPendingToolFailures(loopID, fmt.Sprintf("loop cancelled by %s", signal.UserID))

	// Atomically cancel the loop and get the updated entity
	entity, err := c.handler.CancelLoop(loopID, signal.UserID)
	if err != nil {
		return c.settleUncancellableLoop(ctx, loopID, err)
	}
	// Persist loop state to KV
	if err := c.persistLoopState(ctx, loopID); err != nil {
		return errs.WrapFatal(err, "agentic-loop", "handleCancelSignal", "cancelled loop state has unknown durability")
	}

	// Record metrics
	if c.metrics != nil {
		duration := time.Since(entity.StartedAt).Seconds()
		c.metrics.recordLoopFailed("cancelled", entity.Iterations, duration)
	}

	// Publish completion event with workflow context for reactive workflows
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
	c.recordTerminalObservation(ctx, loopID, agentic.TrajectoryStatusCancelled, "",
		trajectoryTerminalEvidence{Loop: entity, Cancelled: &completion})

	completionMsg := message.NewBaseMessage(completion.Schema(), &completion, "agentic-loop")
	completionData, err := json.Marshal(completionMsg)
	if err != nil {
		return errs.WrapFatal(err, "agentic-loop", "handleCancelSignal", "marshal cancellation after state transition")
	}

	subject, err := component.ResolveSubject(c.config.Ports.Outputs, "agent.complete", loopID)
	if err != nil {
		return errs.WrapFatal(err, "agentic-loop", "handleCancelSignal", "resolve cancellation subject after state transition")
	}
	if err := c.natsClient.PublishToStream(ctx, subject, completionData); err != nil {
		return errs.WrapFatal(err, "agentic-loop", "handleCancelSignal", "cancellation completion has unknown durability")
	}

	// Emit cancellation entity to graph (non-fatal)
	// A cancelled loop can have lost evidence too — the terminal
	// observation above runs before this write.
	if c.graphWriter != nil {
		c.graphWriter.WriteLoopCancellation(ctx, &completion, c.trajectoryAuditLoss.observed(loopID))
		if err := ctx.Err(); err != nil {
			return errs.WrapFatal(err, "agentic-loop", "handleCancelSignal", "cancellation graph write has unknown durability")
		}
	}

	// Persist cancellation to KV so watchers detect it
	if err := c.persistCancellationState(ctx, loopID, &completion); err != nil {
		return errs.WrapFatal(err, "agentic-loop", "handleCancelSignal", "cancellation terminal state has unknown durability")
	}
	c.releaseLoopTransientState(loopID)
	c.logger.Info("Loop cancelled",
		slog.String("loop_id", loopID),
		slog.String("cancelled_by", signal.UserID))
	return nil
}

// handleToolCallVerdictMessage routes inbound verdicts from
// agent.toolcall.approved.> and agent.toolcall.rejected.> into the
// governance dispatcher (ADR-039). The dispatcher demuxes by execution_id
// to per-call waiter channels.
//
// Both wildcard subjects share this single handler because the
// existing input-port consumer wrapper discards the subject (see
// setupConsumer's adapter at component.go:1094, which passes msg.Data() and
// nothing else). The verdict's decision is read from the payload via
// VerdictPayload.EffectiveDecision — both authorship paths (approve action's
// top-level fields, publish action's nested Properties) are supported.
//
// Wire format: the rule engine's `approve` action publishes a
// `core.json.v1` BaseMessage; the canonical ADR-039 reject pattern
// (`publish` action + `deny`) publishes a raw map. This handler
// tolerates BOTH shapes — registry decode first, falling back to raw
// JSON. The discipline (every publish wraps in registry) governs new
// code; the fallback preserves the existing reject path. See
// feedback_nats_publishes_use_payload_registry.
//
// No-op when the dispatcher is nil (disabled-mode-without-construction
// edge case; should not occur in production because NewComponent always
// constructs a dispatcher).
func (c *Component) handleToolCallVerdictMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {
	dispatcher := c.handler.GovernanceDispatcher()
	if dispatcher == nil {
		return natsclient.DeliveryDecisionQuarantine, errors.New("tool-call verdict dispatcher is unavailable")
	}

	payload, ok := decodeVerdictPayload(c.decoder, data)
	if !ok {
		return natsclient.DeliveryDecisionTerminate, fmt.Errorf("decode tool-call verdict payload of %d bytes", len(data))
	}

	decision := payload.EffectiveDecision()
	executionID := payload.effectiveExecutionID()
	if decision == "" || executionID == "" {
		return natsclient.DeliveryDecisionTerminate,
			fmt.Errorf("tool-call verdict payload missing decision or execution_id (decision=%q execution_id=%q)", decision, executionID)
	}

	settled, err := dispatcher.HandleVerdict(decision, executionID, payload)
	if errors.Is(err, ErrNoGovernanceWaiter) {
		return c.settleVerdictWithoutWaiter(ctx, payload, executionID, err)
	}
	return settled, err
}

// settleVerdictWithoutWaiter decides a verdict whose execution identity has no
// waiter here. The dispatcher's own doc comment says such verdicts are expected
// in normal operation — audit mode, late arrivals, verdicts for other
// components' loops on a shared stream — and the
// RecordGovernanceVerdictMissingWaiter counter exists for exactly that, so
// Retrying them made a documented-normal input a hot redelivery loop. But the
// fourth case is real: after process replacement the loop is still waiting and
// the verdict is still owed, so the record decides which it is.
//
// The execution identity cannot answer that question — it is an opaque digest
// with no loop in it — so the loop comes from the payload's own loop_id, or
// from the RequestID grammar when a rule echoes only that.
//
// A payload carrying neither is malformed input, not a settled loop, and is
// terminated the way an undecodable verdict already is. Acknowledging it would
// be indistinguishable from "this loop finished", which is how an adopter rule
// echoing a non-canonical loop_id — an uppercase UUID, a braced form, a legacy
// token — would lose every verdict with no signal naming why.
func (c *Component) settleVerdictWithoutWaiter(
	ctx context.Context, payload VerdictPayload, executionID string, cause error,
) (natsclient.DeliveryDecision, error) {
	loopID := payload.effectiveLoopID()
	if loopID == "" {
		if c.metrics != nil {
			c.metrics.recordVerdictIdentityUnrecoverable()
		}
		c.logger.WarnContext(ctx, "Verdict carries no recoverable loop identity; terminating as malformed",
			slog.String("execution_id", executionID),
			slog.String("hint", "the rule must echo loop_id as the framework minted it, or request_id in the <loopID>:req:<iteration>:<retry> grammar"))
		return natsclient.DeliveryDecisionTerminate,
			fmt.Errorf("tool-call verdict for execution_id %q carries no recoverable loop identity: %w", executionID, cause)
	}
	if c.classifyMissingLoop(ctx, loopID) == loopPresenceStale {
		c.logger.Debug("Verdict has no waiter and its loop is finished or foreign; acknowledging",
			slog.String("execution_id", executionID), slog.String("loop_id", loopID))
		return natsclient.DeliveryDecisionAck, nil
	}
	c.logger.Warn("Verdict names a live loop this process does not hold",
		slog.String("execution_id", executionID), slog.String("loop_id", loopID))
	return natsclient.DeliveryDecisionRetry, cause
}

// decodeVerdictPayload reads a VerdictPayload from wire bytes,
// tolerating both authorship paths:
//
//  1. `approve` action — `core.json.v1` BaseMessage wrapping a
//     GenericJSONPayload whose Data map contains the verdict fields.
//     Decode via the registry, extract Data into VerdictPayload.
//  2. `publish` action (the ADR-039 reject pattern) — raw map JSON
//     with fields nested under `properties`. Decode via raw
//     json.Unmarshal.
//
// Returns the decoded VerdictPayload and true on success; false on
// double-fallback failure (neither shape parsed). The double-attempt
// is acceptable for verdict frequency (per-tool-call, not per-token).
func decodeVerdictPayload(decoder *message.Decoder, data []byte) (VerdictPayload, bool) {
	// Try registry decode first — the canonical post-beta.69 shape.
	if decoder != nil {
		if baseMsg, err := decoder.Decode(data); err == nil {
			if generic, ok := baseMsg.Payload().(*message.GenericJSONPayload); ok {
				return verdictPayloadFromMap(generic.Data), true
			}
		}
	}

	// Fallback: raw JSON, used by the canonical ADR-039 reject pattern
	// emitted via the `publish` action. Pre-existing wire shape; the
	// fallback preserves compatibility.
	var raw VerdictPayload
	if err := json.Unmarshal(data, &raw); err == nil {
		return raw, true
	}

	return VerdictPayload{}, false
}

// verdictPayloadFromMap translates a GenericJSONPayload.Data map into
// the typed VerdictPayload. This IS what the dispatcher receives — the
// original bytes go no further, because the two production shapes do not
// agree on where a field lives and only this decode knows both.
func verdictPayloadFromMap(data map[string]any) VerdictPayload {
	p := VerdictPayload{}
	if v, ok := data["decision"].(string); ok {
		p.Decision = v
	}
	if v, ok := data["call_id"].(string); ok {
		p.CallID = v
	}
	if v, ok := data["loop_id"].(string); ok {
		p.LoopID = v
	}
	if v, ok := data["request_id"].(string); ok {
		p.RequestID = v
	}
	if v, ok := data["execution_id"].(string); ok {
		p.ExecutionID = v
	}
	if v, ok := data["proposal_fingerprint"].(string); ok {
		p.ProposalFingerprint = v
	}
	if v, ok := data["rule_id"].(string); ok {
		p.RuleID = v
	}
	if v, ok := data["reason"].(string); ok {
		p.Reason = v
	}
	if v, ok := data["entity_id"].(string); ok {
		p.EntityID = v
	}
	if v, ok := data["timestamp"].(string); ok {
		p.Timestamp = v
	}
	if v, ok := data["properties"].(map[string]any); ok {
		p.Properties = v
	}
	return p
}
