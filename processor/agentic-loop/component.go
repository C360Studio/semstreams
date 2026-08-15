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
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/persona"
	"github.com/c360studio/semstreams/pkg/errs"
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
	mu        sync.RWMutex
	started   bool
	startTime time.Time

	// KV buckets
	loopsBucket           jetstream.KeyValue
	trajectoryBucket      jetstream.KeyValue
	trajectoryRecorder    *trajectoryRecorder
	trajectoryReader      *trajectoryReader
	trajectoryAuditHealth trajectoryAuditHealth

	// Ports (merged from config)
	inputPorts  []component.Port
	outputPorts []component.Port

	// Track consumers for cleanup
	consumerInfos  []consumerInfo
	consumerCancel context.CancelFunc

	// Query subscription for trajectory requests
	trajectorySub *natsclient.Subscription
	inflightSub   *natsclient.Subscription

	// Approval-timeout sweeper lifecycle. cancel is called from Stop
	// to terminate the goroutine; done is closed by the goroutine on
	// exit so Stop can synchronize cleanly. nil when no sweeper is
	// running (component not started, or no approval flow active).
	sweeperCancel context.CancelFunc
	sweeperDone   chan struct{}

	// Metrics
	metrics *loopMetrics

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

type inputHandler func(context.Context, []byte) error

func rejectRetiredTrajectoryConfig(rawConfig json.RawMessage) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawConfig, &fields); err != nil {
		return err
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

func newConsumerLifecycleContext(startCtx context.Context) (context.Context, context.CancelFunc) {
	// Preserve trace/value propagation from Start while intentionally detaching
	// startup cancellation/deadlines. Component.Stop owns this lifecycle.
	return context.WithCancel(context.WithoutCancel(startCtx))
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

// NewComponent creates a new agentic-loop component
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	// Parse configuration — start from defaults so JSON only overrides
	// explicitly provided fields. Without this, zero-valued fields like
	// compact_threshold (0.0) and headroom_tokens (0) cause compaction
	// to trigger on every iteration regardless of context utilization.
	config := DefaultConfig()
	if err := rejectRetiredTrajectoryConfig(rawConfig); err != nil {
		return nil, errs.WrapInvalid(err, "agentic-loop", "NewComponent", "reject retired trajectory config")
	}
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, errs.WrapInvalid(err, "agentic-loop", "NewComponent", "parse config")
	}
	config.Consumer.EnsureDefaults()
	config.Context.EnsureDefaults()
	config.ToolCallGovernance.EnsureDefaults()

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, errs.WrapInvalid(err, "agentic-loop", "NewComponent", "validate config")
	}

	merged := *DefaultConfig().Ports
	if config.Ports != nil {
		var err error
		merged, err = component.MergePortConfig(merged, *config.Ports)
		if err != nil {
			return nil, errs.WrapInvalid(err, "agentic-loop", "NewComponent", "merge ports")
		}
	}
	config.Ports = &merged
	inputPorts := make([]component.Port, 0, len(merged.Inputs))
	for _, definition := range merged.Inputs {
		port, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			return nil, errs.WrapInvalid(err, "agentic-loop", "NewComponent", "resolve input port")
		}
		facts, err := port.Facts()
		if err != nil {
			return nil, errs.WrapInvalid(err, "agentic-loop", "NewComponent", "inspect input port")
		}
		if definition.Name == "trajectory_query" {
			if err := validateTrajectoryQueryInput(port, facts); err != nil {
				return nil, errs.WrapInvalid(err, "agentic-loop", "NewComponent", "validate trajectory query input")
			}
		} else if facts.Kind() != component.PortKindJetStream {
			return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "agentic-loop", "NewComponent", "work input ports must be JetStream ports")
		}
		inputPorts = append(inputPorts, port)
	}
	outputPorts := make([]component.Port, 0, len(merged.Outputs))
	for _, definition := range merged.Outputs {
		port, err := definition.Resolve(component.DirectionOutput)
		if err != nil {
			return nil, errs.WrapInvalid(err, "agentic-loop", "NewComponent", "resolve output port")
		}
		if definition.Name == "trajectories" {
			facts, factsErr := port.Facts()
			if factsErr != nil {
				return nil, errs.WrapInvalid(factsErr, "agentic-loop", "NewComponent", "inspect trajectories output")
			}
			if err := validateTrajectoriesOutput(port, facts); err != nil {
				return nil, errs.WrapInvalid(err, "agentic-loop", "NewComponent", "validate trajectories output")
			}
		}
		outputPorts = append(outputPorts, port)
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
	if healthy && (!c.trajectoryProviderAvailable() || errorCount > 0) {
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
func (c *Component) Start(ctx context.Context) error {
	// Validate context
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "agentic-loop", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "agentic-loop", "Start", "context already cancelled")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return errs.ErrAlreadyStarted
	}

	// Initialize KV buckets if NATS client available
	if c.natsClient != nil {
		if err := c.initializeKVBuckets(ctx); err != nil {
			return errs.Wrap(err, "agentic-loop", "Start", "initialize KV buckets")
		}

		// Consumer callbacks use a component-owned lifecycle context rather than
		// the caller's startup deadline. Stop cancels it so in-flight deliveries
		// are released (NAKed once) during shutdown.
		consumerCtx, consumerCancel := newConsumerLifecycleContext(ctx)
		c.consumerCancel = consumerCancel

		// Set up NATS subscriptions for input ports.
		if err := c.setupSubscriptions(ctx, consumerCtx); err != nil {
			c.cleanupConsumersAfterStartFailure()
			return errs.Wrap(err, "agentic-loop", "Start", "setup subscriptions")
		}

		// Set up trajectory query handler from the declared exact request input.
		querySubject, err := c.trajectoryQuerySubject()
		if err != nil {
			c.cleanupConsumersAfterStartFailure()
			return errs.Wrap(err, "agentic-loop", "Start", "resolve trajectory query input")
		}
		sub, err := c.natsClient.SubscribeForRequests(ctx, querySubject, c.handleTrajectoryQuery)
		if err != nil {
			c.cleanupConsumersAfterStartFailure()
			return errs.Wrap(err, "agentic-loop", "Start", "subscribe to trajectory query")
		}
		c.trajectorySub = sub

		// Set up in-flight query handler (gh#733). Same wire as the trajectory
		// query: the answer is served, never the consumer name it is derived from.
		inflightSub, err := c.natsClient.SubscribeForRequests(ctx,
			InFlightQuerySubjectFor(c.config.ConsumerNameSuffix), c.handleInFlightQuery)
		if err != nil {
			c.cleanupConsumersAfterStartFailure()
			return errs.Wrap(err, "agentic-loop", "Start", "subscribe to in-flight query")
		}
		c.inflightSub = inflightSub
	}

	c.started = true
	c.startTime = time.Now()

	// Start the approval-timeout sweeper. Derives a sub-context so
	// Stop can terminate the sweeper independently of whatever
	// passed in `ctx`. Cheap when no loops await approval — the
	// snapshot is a single map iteration under read-lock.
	//
	// Capture the done channel locally before launching the goroutine
	// — Stop nils c.sweeperDone before waiting on it, and the
	// goroutine's deferred close needs a stable reference that
	// survives that nilling.
	sweepCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	c.sweeperCancel = cancel
	c.sweeperDone = done
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
	c.initPromptRegistry(ctx)

	// Emit model endpoint entities to graph (non-fatal)
	if c.graphWriter != nil {
		c.graphWriter.WriteModelEndpoints(ctx)
	}

	return nil
}

func (c *Component) cleanupConsumersAfterStartFailure() {
	if c.consumerCancel != nil {
		c.consumerCancel()
		c.consumerCancel = nil
	}
	if c.natsClient != nil {
		for _, info := range c.consumerInfos {
			c.natsClient.StopConsumer(info.streamName, info.consumerName)
		}
	}
	c.consumerInfos = nil

	// Request subscriptions must be torn down here too. Start installs them in
	// sequence, so a failure on the Nth leaves the first N-1 live while `started`
	// stays false — and Stop returns early when not started, so nothing ever
	// reaps them. A Start retry would then install a SECOND responder on the same
	// subject, and NATS would deliver requests to both.
	c.unsubscribeRequestHandlers()
}

// unsubscribeRequestHandlers tears down every installed request/reply subscription
// and nils it, so the operation is idempotent and safe on both the start-failure
// and the normal-Stop path.
func (c *Component) unsubscribeRequestHandlers() {
	if c.trajectorySub != nil {
		_ = c.trajectorySub.Unsubscribe()
		c.trajectorySub = nil
	}
	if c.inflightSub != nil {
		_ = c.inflightSub.Unsubscribe()
		c.inflightSub = nil
	}
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
func (c *Component) Stop(timeout time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return nil
	}

	// Stop the approval-timeout sweeper before unsubscribing — the
	// sweeper calls into HandleApprovalResponse which may publish
	// results via NATS, and we'd rather drain those callbacks
	// before the consumers tear down.
	//
	// Capture the cancel + done channel under c.mu, then release the
	// lock BEFORE waiting on the goroutine. Holding c.mu across the
	// wait is a latent deadlock: the sweeper's per-candidate path
	// (publishResults / persistLoopState / handler.GetLoop) doesn't
	// take c.mu today, but a future defensive guard there would
	// block the in-flight goroutine on c.mu while Stop blocks on
	// sweeperDone. Release-then-wait keeps the cleanup race-free.
	cancel := c.sweeperCancel
	done := c.sweeperDone
	c.sweeperCancel = nil
	c.sweeperDone = nil
	if cancel != nil {
		c.mu.Unlock()
		cancel()
		if done != nil {
			select {
			case <-done:
			case <-time.After(timeout):
				c.logger.Warn("approval-timeout sweeper did not exit within Stop timeout",
					slog.Duration("timeout", timeout))
			}
		}
		c.mu.Lock()
	}

	// Unsubscribe every request handler. Once the in-flight one is gone a caller
	// sees no-responders — which is UNKNOWN, not zero (gh#733).
	c.unsubscribeRequestHandlers()

	// Cancel the component-owned consumer lifecycle before stopping consumers.
	// In-flight deliveries observe this cancellation and are NAKed exactly once.
	if c.consumerCancel != nil {
		c.consumerCancel()
		c.consumerCancel = nil
	}

	// Stop all JetStream consumers
	for _, info := range c.consumerInfos {
		if c.config.DeleteConsumerOnStop {
			// Delete consumer from server (for test cleanup)
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			if err := c.natsClient.StopAndDeleteConsumer(ctx, info.streamName, info.consumerName); err != nil {
				c.logger.Debug("Failed to delete consumer", "stream", info.streamName, "consumer", info.consumerName, "error", err)
			} else {
				c.logger.Debug("Stopped and deleted consumer", "stream", info.streamName, "consumer", info.consumerName)
			}
			cancel()
		} else {
			// Just stop local consumption (keep durable consumer for resume)
			c.natsClient.StopConsumer(info.streamName, info.consumerName)
			c.logger.Debug("Stopped consumer", "stream", info.streamName, "consumer", info.consumerName)
		}
	}
	c.consumerInfos = nil

	c.started = false
	return nil
}

// initializeKVBuckets initializes the KV buckets for loop and trajectory storage
func (c *Component) initializeKVBuckets(ctx context.Context) error {
	js, err := c.natsClient.JetStream()
	if err != nil {
		return errs.WrapTransient(err, "agentic-loop", "initializeKVBuckets", "get JetStream")
	}

	// Initialize loops bucket
	loopsBucket, err := js.KeyValue(ctx, c.config.LoopsBucket)
	if err != nil {
		// Bucket doesn't exist, try to create it
		loopsBucket, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
			Bucket:  c.config.LoopsBucket,
			History: 10,
			TTL:     24 * time.Hour,
		})
		if err != nil {
			return errs.Wrap(err, "agentic-loop", "initializeKVBuckets", "create loops bucket")
		}
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

		var handler inputHandler

		// Route to appropriate handler based on port name
		switch port.Name {
		case "agent.task":
			handler = c.taskInputHandler(30 * time.Minute)
		case "agent.response":
			handler = adaptVoidInputHandler(c.handleResponseMessage)
		case "tool.result":
			handler = adaptVoidInputHandler(c.handleToolResultMessage)
		case "agent.signal":
			handler = adaptVoidInputHandler(c.handleSignalMessage)
		case "agent.approval_response":
			handler = adaptVoidInputHandler(c.handleApprovalResponseMessage)
		case "agent.toolcall.approved", "agent.toolcall.rejected":
			// Verdicts from rule-driven tool-call governance (ADR-039).
			// Both subjects route into the same demux — the dispatcher
			// uses the subject path to extract decision + call_id.
			// Skip if no dispatcher is configured (disabled mode with
			// no fallback construction); the wildcard subscription is
			// still cheap to bind but never gets traffic in disabled
			// mode because nothing publishes to proposed.
			handler = adaptVoidInputHandler(c.handleToolCallVerdictMessage)
		default:
			c.logger.Warn("Unknown input port", "port", port.Name)
			continue
		}

		if err := c.setupConsumer(setupCtx, consumerCtx, port, subject, handler); err != nil {
			return errs.Wrap(err, "agentic-loop", "setupSubscriptions", fmt.Sprintf("setup consumer for %s", subject))
		}
	}

	return nil
}

// setupConsumer sets up a JetStream consumer for an input port.
func (c *Component) setupConsumer(setupCtx, consumerCtx context.Context, port component.Port, subject string, handler inputHandler) error {
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
	if err := c.waitForStream(setupCtx, streamName); err != nil {
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

	var handlerFn func(context.Context, jetstream.Msg)
	if useHeartbeat {
		hi := heartbeatInterval
		handlerFn = func(msgCtx context.Context, msg jetstream.Msg) {
			if err := consumeLongRunningInput(msgCtx, msg, hi, handler); err != nil {
				c.logger.Error("Message handler error", "port", port.Name, "error", err)
			}
		}
	} else {
		handlerFn = func(msgCtx context.Context, msg jetstream.Msg) {
			if err := handler(msgCtx, msg.Data()); err != nil {
				_ = msg.Nak()
				c.logger.Error("Message handler error", "port", port.Name, "error", err)
				return
			}
			if ackErr := msg.Ack(); ackErr != nil {
				c.logger.Error("Failed to ack JetStream message", "error", ackErr)
			}
		}
	}

	err = c.natsClient.ConsumeStreamWithConfigContexts(setupCtx, consumerCtx, natsclient.PortConsumerContext{Component: c.Meta().Name, Port: port.Name, ComponentOwned: true}, cfg, handlerFn)
	if err != nil {
		return errs.Wrap(err, "agentic-loop", "setupConsumer", fmt.Sprintf("setup consumer for stream %s", streamName))
	}

	// Track consumer for cleanup in Stop()
	c.consumerInfos = append(c.consumerInfos, consumerInfo{
		streamName:   streamName,
		consumerName: consumerName,
		subject:      subject,
	})

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

func consumeLongRunningInput(
	ctx context.Context,
	msg jetstream.Msg,
	heartbeatInterval time.Duration,
	handler inputHandler,
) error {
	return natsclient.ConsumeWithHeartbeat(ctx, msg, heartbeatInterval, func(workCtx context.Context) error {
		return handler(workCtx, msg.Data())
	})
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
	related, hasLineage, err := c.preflightDecodedTask(task)
	if err != nil {
		if c.metrics != nil {
			c.metrics.recordTaskIntakeRejection(taskIntakeRejectionLane, taskIntakeRejectionReason)
		}
		return natsclient.TerminateDelivery(err)
	}

	c.logger.Debug("Processing task message",
		slog.String("task_id", task.TaskID),
		slog.String("role", task.Role),
		slog.String("model", task.Model))

	// Handle the task using the message handler
	result, err := c.handler.HandleTask(ctx, *task)
	if err != nil {
		c.logger.Error("Failed to handle task", "error", err, "task_id", task.TaskID)
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

	// Publish output messages
	c.publishResults(ctx, result)

	// Persist loop state to KV
	c.persistLoopState(ctx, result.LoopID)
	return nil
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
	c.handleLoopFailure(ctx, loopID, entity, reason, err)
	return nil
}

// handleResponseMessage processes incoming agent response messages
func (c *Component) handleResponseMessage(ctx context.Context, data []byte) {
	response, loopID, ok := c.extractAgentResponse(data)
	if !ok {
		return
	}

	entity, _ := c.handler.GetLoop(loopID)

	result, err := c.handler.HandleModelResponse(ctx, loopID, *response)
	if err != nil {
		c.recordTrajectoryObservations(ctx, result)
		c.handleLoopFailure(ctx, loopID, entity, failureReasonForHandlerError(err), err)
		return
	}

	c.recordResponseMetrics(response, result, entity)
	c.persistHandlerResult(ctx, result)
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
// Returns the response, loop ID, and success flag.
func (c *Component) extractAgentResponse(data []byte) (*agentic.AgentResponse, string, bool) {
	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
		c.logger.Error("Failed to unmarshal BaseMessage", "error", err)
		return nil, "", false
	}

	responsePtr, ok := baseMsg.Payload().(*agentic.AgentResponse)
	if !ok {
		c.logger.Error("Unexpected payload type", "type", fmt.Sprintf("%T", baseMsg.Payload()))
		return nil, "", false
	}

	loopID := c.findLoopIDForRequest(responsePtr.RequestID)
	if loopID == "" {
		c.logger.Warn("No loop found for request", "request_id", responsePtr.RequestID)
		return nil, "", false
	}

	c.logger.Debug("Processing model response",
		slog.String("loop_id", loopID),
		slog.String("request_id", responsePtr.RequestID),
		slog.String("status", responsePtr.Status))

	return responsePtr, loopID, true
}

// handleLoopFailure records failure metrics and publishes failure events.
func (c *Component) handleLoopFailure(ctx context.Context, loopID string, entity agentic.LoopEntity, reason string, err error) {
	// Failure-event construction reads token totals twice below. Release the
	// active aggregate only after those terminal consumers have returned.
	defer c.handler.trajectoryManager.discardTrajectory(loopID)

	c.logger.Error("Loop processing failed", "error", err, "loop_id", loopID, "reason", reason)

	// Transition loop to failed and persist — without this, the loop entity
	// in AGENT_LOOPS KV stays at state=running and downstream watchers
	// (execution-manager) never see the terminal state.
	if transErr := c.handler.loopManager.TransitionLoop(loopID, agentic.LoopStateFailed); transErr == nil {
		c.handler.loopManager.UpdateCompletion(loopID, agentic.OutcomeFailed, "", err.Error())
		c.persistLoopState(ctx, loopID)
	}

	if c.metrics != nil && entity.ID != "" {
		duration := time.Since(entity.StartedAt).Seconds()
		c.metrics.recordLoopFailed(reason, entity.Iterations, duration)
	}
	latest, _ := c.handler.GetLoop(loopID)
	failure, _, _ := c.handler.BuildFailureMessages(loopID, reason, err.Error())
	c.recordTerminalObservation(ctx, loopID, agentic.TrajectoryStatusFailed, agentic.TrajectoryErrorUnknown,
		trajectoryTerminalEvidence{Loop: latest, Failure: failure})

	c.publishFailureEvents(ctx, loopID, reason, err.Error())
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
func (c *Component) publishFailureEvents(ctx context.Context, loopID, reason, errorMsg string) {
	errorCtx, cancel := natsclient.DetachContextWithTrace(ctx, 5*time.Second)
	defer cancel()

	failure, failMsgs, err := c.handler.BuildFailureMessages(loopID, reason, errorMsg)
	if err != nil {
		c.logger.Warn("Failed to build failure event", "error", err, "loop_id", loopID)
		return
	}

	// Persist failure to KV first so watchers (rules engine,
	// execution-manager) see COMPLETE_{loopID} when they react to the
	// failure event below.
	if failure != nil {
		c.persistFailureState(errorCtx, loopID, failure)
	}

	// Stamp graph triples second (under budget). The reorder is the
	// load-bearing change vs pre-fix; the budget cap mirrors the success
	// path's stampLoopCompletionWithBudget so a slow graph-gateway can't
	// stall the publish.
	if failure != nil {
		c.stampLoopFailureWithBudget(errorCtx, loopID, failure)
	}

	// Publish last — every observable side effect is now in place.
	// NATS-less deployments (test scaffolding) skip the publish, matching
	// publishResults' nil-client guard.
	if c.natsClient == nil {
		return
	}
	for _, msg := range failMsgs {
		if pubErr := c.natsClient.PublishToStream(errorCtx, msg.Subject, msg.Data); pubErr != nil {
			c.logger.Error("Failed to publish failure event", "error", pubErr, "loop_id", loopID)
		}
	}
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
// publish anyway and emit a Prom counter so operators can dashboard
// the tail. Tighten if production sees significant tail; widen only
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
// runWithBudget caps how long we delay the publish on a slow graph
// write — graph-gateway hiccups must NOT silently swallow the
// agent.complete.* event downstream rules wait on. On budget timeout,
// publish proceeds with a loud log and Prom counter increment.
func (c *Component) persistHandlerResult(ctx context.Context, result HandlerResult) {
	if result.State == agentic.LoopStateComplete || result.State == agentic.LoopStateFailed {
		// MessageHandler has already extracted terminal token/step data into
		// result. Keep the aggregate alive through persistence/publication, then
		// release it even when an adjacent terminal side effect degrades.
		defer c.handler.trajectoryManager.discardTrajectory(result.LoopID)
	}

	c.recordHandlerResultTrajectory(ctx, result)
	c.persistLoopState(ctx, result.LoopID)

	if result.State == agentic.LoopStateComplete || result.State == agentic.LoopStateFailed {
		if result.CompletionState != nil {
			c.persistCompletionState(ctx, result.LoopID, result.CompletionState)
			c.stampLoopCompletionWithBudget(ctx, result.LoopID, result.CompletionState)
		} else if result.FailureState != nil {
			c.stampLoopFailureWithBudget(ctx, result.LoopID, result.FailureState)
		}
		// Terminal-tool-less synthesis (#133). Detected in
		// handleCompleteResponse; emitted here on the graph path so the
		// triples ride the same publish budget as the loop completion
		// stamp and downstream rules see them on the same KV revision
		// the agent.complete.* event refers to.
		if result.SyntheticDecide != nil {
			c.stampSyntheticDecideWithBudget(ctx, result.SyntheticDecide)
		}
	}

	c.publishResults(ctx, result)
}

// stampLoopCompletionWithBudget invokes WriteLoopCompletion under the
// graphWritePublishBudget. Records a Prom timeout when the budget
// expires before the writer returns; publish proceeds either way.
func (c *Component) stampLoopCompletionWithBudget(ctx context.Context, loopID string, completion *agentic.LoopCompletedEvent) {
	if c.graphWriter == nil {
		return
	}
	timedOut := runWithBudget(ctx, graphWritePublishBudget, func(bctx context.Context) {
		c.graphWriter.WriteLoopCompletion(bctx, completion)
	})
	if timedOut {
		c.logger.Warn("graph write budget expired before completion stamp returned; publishing agent.complete anyway",
			"loop_id", loopID,
			"budget", graphWritePublishBudget,
			"state", "complete")
		if c.metrics != nil {
			c.metrics.recordGraphWritePublishTimeout("complete")
		}
	}
}

// stampSyntheticDecideWithBudget invokes WriteSyntheticDecide under the
// graphWritePublishBudget. Records a Prom timeout when the budget
// expires before the writer returns; publish proceeds either way. Same
// shape as stampLoopCompletionWithBudget — the synthetic-decide triples
// must reach the graph before downstream rules wake on the
// agent.complete.* event, otherwise the recovery rule fires before
// coordinator.next_action="needs_clarification" is visible.
func (c *Component) stampSyntheticDecideWithBudget(ctx context.Context, req *SyntheticDecideRequest) {
	if c.graphWriter == nil {
		return
	}
	timedOut := runWithBudget(ctx, graphWritePublishBudget, func(bctx context.Context) {
		c.graphWriter.WriteSyntheticDecide(bctx, req.LoopID, req.Reason)
	})
	if timedOut {
		c.logger.Warn("graph write budget expired before synthetic decide stamp returned; publishing agent.complete anyway",
			"loop_id", req.LoopID,
			"budget", graphWritePublishBudget,
			"state", "synthetic_decide")
		if c.metrics != nil {
			c.metrics.recordGraphWritePublishTimeout("synthetic_decide")
		}
	}
}

// stampLoopFailureWithBudget mirrors stampLoopCompletionWithBudget for
// the failure branch. semteams smoke-#7 reproduced the race on
// researcher-failure specifically (failed loops still need
// agent.loop.parent visible for ancestry walks), so the failure path
// gets the same budgeted-write treatment as completion.
func (c *Component) stampLoopFailureWithBudget(ctx context.Context, loopID string, failure *agentic.LoopFailedEvent) {
	if c.graphWriter == nil {
		return
	}
	timedOut := runWithBudget(ctx, graphWritePublishBudget, func(bctx context.Context) {
		c.graphWriter.WriteLoopFailure(bctx, failure)
	})
	if timedOut {
		c.logger.Warn("graph write budget expired before failure stamp returned; publishing agent.complete anyway",
			"loop_id", loopID,
			"budget", graphWritePublishBudget,
			"state", "failure")
		if c.metrics != nil {
			c.metrics.recordGraphWritePublishTimeout("failure")
		}
	}
}

// runWithBudget runs fn in a goroutine and waits for it to return,
// bounded by budget. Returns true if the budget expired before fn
// returned (the goroutine continues running with a cancelled child
// context; its inner NATS calls will see ctx.Done and abort cleanly).
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

	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(bctx)
	}()

	select {
	case <-done:
		// fn returned. But "returned" is ambiguous when bctx was
		// cancelled before/while fn ran — the goroutine may have
		// observed bctx.Done() and returned promptly, which is the
		// timed-out (or parent-cancelled) case, NOT a within-budget
		// completion. When parent ctx is pre-cancelled, bctx is born
		// cancelled and both channels become ready simultaneously;
		// Go's select picks one at random, so without this check the
		// function returns the wrong answer ~50% of the time on the
		// pre-cancel path (caught by
		// TestRunWithBudget_ParentContextCancellationPropagates
		// flaking on CI). Inspect bctx to disambiguate.
		return bctx.Err() != nil
	case <-bctx.Done():
		return true
	}
}

// handleToolResultMessage processes incoming tool result messages
func (c *Component) handleToolResultMessage(ctx context.Context, data []byte) {
	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
		c.logger.Error("Failed to unmarshal BaseMessage", "error", err)
		return
	}

	toolResultPtr, ok := baseMsg.Payload().(*agentic.ToolResult)
	if !ok {
		c.logger.Error("Unexpected payload type", "type", fmt.Sprintf("%T", baseMsg.Payload()))
		return
	}
	toolResult := *toolResultPtr

	// Find loop ID for this tool call. Empty here means either we drained the
	// CallID at the previous turn boundary (GetAndClearToolResults evicts the
	// routing entry to drop late re-deliveries) or we never tracked it.
	// Returning here is load-bearing — proceeding would land the late result
	// in PendingToolResults and surface as a duplicate tool message in the
	// next turn's request.
	loopID := c.findLoopIDForToolCall(toolResult.CallID)
	if loopID == "" {
		c.logger.Warn("No loop found for tool call", "call_id", toolResult.CallID)
		if c.metrics != nil {
			c.metrics.recordToolResultDropped("stale_callid")
		}
		return
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
		c.recordHandlerResultTrajectory(ctx, result)
		if result.State.IsTerminal() {
			// The handler has already built terminal failure state (including
			// token totals). This error branch bypasses persistHandlerResult, so
			// release the active aggregate after its terminal audit completes.
			c.handler.trajectoryManager.discardTrajectory(loopID)
		}
		c.logger.Error("Failed to handle tool result", "error", err, "loop_id", loopID)
		return
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
	c.persistHandlerResult(ctx, result)
}

// publishResults publishes all output messages from a handler result using JetStream.
// Defensive against nil natsClient — pure unit tests construct
// Components without one, and the approval-timeout sweeper goroutine
// can race with Stop's natsClient teardown. Mirrors the existing
// persistLoopState's loopsBucket-nil guard pattern.
func (c *Component) publishResults(ctx context.Context, result HandlerResult) {
	if c.natsClient == nil {
		return
	}
	for _, msg := range result.PublishedMessages {
		// Use JetStream for publishing to ensure delivery
		if err := c.natsClient.PublishToStream(ctx, msg.Subject, msg.Data); err != nil {
			c.logger.Error("Failed to publish message", "error", err, "subject", msg.Subject)
		}
	}

	// Publish context events (compaction lifecycle) onto the AGENT stream for
	// observability consumers — the OTel span collector (output/otel) enriches
	// the active loop span with each event via its agent.> subscription.
	for _, event := range result.ContextEvents {
		c.publishContextEvent(ctx, event)
	}

	// Emit context management metrics from events
	c.emitContextMetrics(result)
}

// publishContextEvent publishes a context management event
func (c *Component) publishContextEvent(ctx context.Context, event agentic.ContextEvent) {
	eventMsg := message.NewBaseMessage(event.Schema(), &event, "agentic-loop")
	data, err := json.Marshal(eventMsg)
	if err != nil {
		c.logger.Error("Failed to marshal context event", "error", err, "type", event.Type)
		return
	}

	subject, err := component.ResolveSubject(c.config.Ports.Outputs, "agent.context.compaction", event.LoopID)
	if err != nil {
		c.logger.Error("Failed to resolve context event subject", "error", err)
		return
	}
	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		c.logger.Error("Failed to publish context event", "error", err, "subject", subject)
	}
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
func (c *Component) persistCompletionState(ctx context.Context, loopID string, completion *agentic.LoopCompletedEvent) {
	if c.loopsBucket == nil || completion == nil {
		return
	}

	data, err := json.Marshal(completion)
	if err != nil {
		c.logger.Error("Failed to marshal completion state", "error", err, "loop_id", loopID)
		return
	}

	// Key pattern: COMPLETE_{loopID} for rules engine to watch
	key := fmt.Sprintf("COMPLETE_%s", loopID)
	if _, err := c.loopsBucket.Put(ctx, key, data); err != nil {
		c.logger.Error("Failed to persist completion state", "error", err, "loop_id", loopID)
		return
	}

	c.logger.Debug("Persisted completion state",
		slog.String("loop_id", loopID),
		slog.String("key", key),
		slog.String("role", completion.Role))
}

// persistFailureState persists the failure state to KV.
// Key pattern: COMPLETE_{loopID} — same as success, so watchers don't need
// to distinguish between success/failure key patterns. The outcome field
// in the serialized event tells them what happened.
func (c *Component) persistFailureState(ctx context.Context, loopID string, failure *agentic.LoopFailedEvent) {
	if c.loopsBucket == nil || failure == nil {
		return
	}

	data, err := json.Marshal(failure)
	if err != nil {
		c.logger.Error("Failed to marshal failure state", "error", err, "loop_id", loopID)
		return
	}

	key := fmt.Sprintf("COMPLETE_%s", loopID)
	if _, err := c.loopsBucket.Put(ctx, key, data); err != nil {
		c.logger.Error("Failed to persist failure state", "error", err, "loop_id", loopID)
		return
	}

	c.logger.Debug("Persisted failure state",
		slog.String("loop_id", loopID),
		slog.String("key", key),
		slog.String("reason", failure.Reason))
}

// persistCancellationState persists the cancellation state to KV.
// Uses same COMPLETE_{loopID} key pattern so watchers handle all terminal states uniformly.
func (c *Component) persistCancellationState(ctx context.Context, loopID string, cancelled *agentic.LoopCancelledEvent) {
	if c.loopsBucket == nil || cancelled == nil {
		return
	}

	data, err := json.Marshal(cancelled)
	if err != nil {
		c.logger.Error("Failed to marshal cancellation state", "error", err, "loop_id", loopID)
		return
	}

	key := fmt.Sprintf("COMPLETE_%s", loopID)
	if _, err := c.loopsBucket.Put(ctx, key, data); err != nil {
		c.logger.Error("Failed to persist cancellation state", "error", err, "loop_id", loopID)
		return
	}

	c.logger.Debug("Persisted cancellation state",
		slog.String("loop_id", loopID),
		slog.String("key", key),
		slog.String("cancelled_by", cancelled.CancelledBy))
}

// persistLoopState persists the loop state to KV
func (c *Component) persistLoopState(ctx context.Context, loopID string) {
	if c.loopsBucket == nil {
		return
	}

	entity, err := c.handler.GetLoop(loopID)
	if err != nil {
		c.logger.Error("Failed to get loop for persistence", "error", err, "loop_id", loopID)
		return
	}

	data, err := json.Marshal(entity)
	if err != nil {
		c.logger.Error("Failed to marshal loop entity", "error", err, "loop_id", loopID)
		return
	}

	if _, err := c.loopsBucket.Put(ctx, loopID, data); err != nil {
		c.logger.Error("Failed to persist loop state", "error", err, "loop_id", loopID)
	}
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

// findLoopIDForToolCall finds the loop ID associated with a tool call ID,
// attempting recovery from structured ID if not found in cache.
func (c *Component) findLoopIDForToolCall(callID string) string {
	loopID, exists := c.handler.loopManager.GetLoopForToolCallWithRecovery(callID)
	if !exists {
		return ""
	}
	return loopID
}

// handleSignalMessage processes incoming signal messages (cancel, pause, etc.)
func (c *Component) handleSignalMessage(ctx context.Context, data []byte) {
	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
		c.logger.Error("Failed to unmarshal BaseMessage", "error", err)
		return
	}

	signalPtr, ok := baseMsg.Payload().(*agentic.UserSignal)
	if !ok {
		c.logger.Error("Unexpected payload type", "type", fmt.Sprintf("%T", baseMsg.Payload()))
		return
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
		c.handleCancelSignal(ctx, signal)
	case agentic.SignalPause:
		c.handlePauseSignal(ctx, signal)
	case agentic.SignalResume:
		c.handleResumeSignal(ctx, signal)
	default:
		c.logger.Warn("Unsupported signal type",
			slog.String("type", signal.Type),
			slog.String("loop_id", signal.LoopID))
	}
}

// handleCancelSignal handles a cancel signal for a loop
func (c *Component) handleCancelSignal(ctx context.Context, signal agentic.UserSignal) {
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
		c.logger.Error("Failed to cancel loop",
			slog.String("error", err.Error()),
			slog.String("loop_id", loopID))
		return
	}
	// Cancellation has no aggregate token/step consumer. Defer cleanup so all
	// terminal observation and publication work sees a stable active-loop
	// lifetime, including early-return degradation paths below.
	defer c.handler.trajectoryManager.discardTrajectory(loopID)

	// Persist loop state to KV
	c.persistLoopState(ctx, loopID)

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
		c.logger.Error("Failed to marshal completion",
			slog.String("error", err.Error()),
			slog.String("loop_id", loopID))
		return
	}

	subject, err := component.ResolveSubject(c.config.Ports.Outputs, "agent.complete", loopID)
	if err != nil {
		c.logger.Error("Failed to resolve completion subject", slog.String("error", err.Error()))
		return
	}
	if err := c.natsClient.PublishToStream(ctx, subject, completionData); err != nil {
		c.logger.Error("Failed to publish completion",
			slog.String("error", err.Error()),
			slog.String("loop_id", loopID))
		return
	}

	// Emit cancellation entity to graph (non-fatal)
	if c.graphWriter != nil {
		c.graphWriter.WriteLoopCancellation(ctx, &completion)
	}

	// Persist cancellation to KV so watchers detect it
	c.persistCancellationState(ctx, loopID, &completion)
	c.logger.Info("Loop cancelled",
		slog.String("loop_id", loopID),
		slog.String("cancelled_by", signal.UserID))
}

// handlePauseSignal handles a pause signal for a loop
func (c *Component) handlePauseSignal(ctx context.Context, signal agentic.UserSignal) {
	loopID := signal.LoopID

	// Get current loop state
	entity, err := c.handler.GetLoop(loopID)
	if err != nil {
		c.logger.Error("Failed to get loop for pause",
			slog.String("error", err.Error()),
			slog.String("loop_id", loopID))
		return
	}

	// Check if loop can be paused
	if entity.State.IsTerminal() || entity.State == agentic.LoopStatePaused {
		c.logger.Warn("Cannot pause loop",
			slog.String("loop_id", loopID),
			slog.String("state", string(entity.State)))
		return
	}

	// Set pause requested flag
	entity.PauseRequested = true

	// Update in handler
	if err := c.handler.UpdateLoop(entity); err != nil {
		c.logger.Error("Failed to update loop state",
			slog.String("error", err.Error()),
			slog.String("loop_id", loopID))
		return
	}

	// Persist loop state to KV
	c.persistLoopState(ctx, loopID)

	c.logger.Info("Pause requested for loop",
		slog.String("loop_id", loopID),
		slog.String("requested_by", signal.UserID))
}

// handleResumeSignal handles a resume signal for a paused loop
func (c *Component) handleResumeSignal(ctx context.Context, signal agentic.UserSignal) {
	loopID := signal.LoopID

	// Get current loop state
	entity, err := c.handler.GetLoop(loopID)
	if err != nil {
		c.logger.Error("Failed to get loop for resume",
			slog.String("error", err.Error()),
			slog.String("loop_id", loopID))
		return
	}

	// Check if loop can be resumed
	if entity.State != agentic.LoopStatePaused {
		c.logger.Warn("Cannot resume non-paused loop",
			slog.String("loop_id", loopID),
			slog.String("state", string(entity.State)))
		return
	}

	// Clear pause state and restore to executing
	entity.State = agentic.LoopStateExecuting
	entity.PauseRequested = false

	// Update in handler
	if err := c.handler.UpdateLoop(entity); err != nil {
		c.logger.Error("Failed to update loop state",
			slog.String("error", err.Error()),
			slog.String("loop_id", loopID))
		return
	}

	// Persist loop state to KV
	c.persistLoopState(ctx, loopID)

	c.logger.Info("Loop resumed",
		slog.String("loop_id", loopID),
		slog.String("resumed_by", signal.UserID))
}

// handleToolCallVerdictMessage routes inbound verdicts from
// agent.toolcall.approved.> and agent.toolcall.rejected.> into the
// governance dispatcher (ADR-039). The dispatcher demuxes by call_id
// to per-call waiter channels.
//
// Both wildcard subjects share this single handler because the
// existing input-port consumer wrapper discards the subject (see
// setupConsumer's adapter at component.go:805). The verdict's decision
// is read from the payload via VerdictPayload.EffectiveDecision — both
// authorship paths (approve action's top-level fields, publish action's
// nested Properties) are supported.
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
func (c *Component) handleToolCallVerdictMessage(_ context.Context, data []byte) {
	dispatcher := c.handler.GovernanceDispatcher()
	if dispatcher == nil {
		return
	}

	payload, ok := decodeVerdictPayload(c.decoder, data)
	if !ok {
		c.logger.Warn("Failed to decode tool-call verdict payload; ignoring",
			slog.Int("size", len(data)))
		return
	}

	decision := payload.EffectiveDecision()
	callID := payload.EffectiveCallID()
	if decision == "" || callID == "" {
		c.logger.Warn("Tool-call verdict payload missing decision or call_id; ignoring",
			slog.String("decision", decision),
			slog.String("call_id", callID))
		return
	}

	dispatcher.HandleVerdict(decision, callID, data)
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
// the typed VerdictPayload. Only the routing-relevant fields are
// extracted; the original bytes are still passed to the dispatcher's
// HandleVerdict for context logging.
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
