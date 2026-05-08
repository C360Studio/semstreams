package agenticloop

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"

	"os"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph/llm"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/persona"
	"github.com/c360studio/semstreams/pkg/cache"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/workflow"
	"github.com/c360studio/semstreams/processor/agentic-loop/prompt"
	"github.com/c360studio/semstreams/processor/rule/boid"
	"github.com/c360studio/semstreams/storage/objectstore"
	"github.com/nats-io/nats.go/jetstream"
)

// schema is the configuration schema for agentic-loop, generated from Config struct tags
var schema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// maxTraversalVectorSize is the maximum number of predicates to track in an agent's traversal vector.
// This limits memory usage while retaining enough history for alignment rule evaluation.
const maxTraversalVectorSize = 5

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
	loopsBucket     jetstream.KeyValue
	positionsBucket jetstream.KeyValue

	// Trajectory cache (replaces KV bucket — durable content in ObjectStore + graph)
	trajectoryCache cache.Cache[*agentic.Trajectory]

	// Boid coordination handler
	boidHandler *BoidHandler

	// Ports (merged from config)
	inputPorts  []component.Port
	outputPorts []component.Port

	// Track consumers for cleanup
	consumerInfos []consumerInfo

	// Query subscription for trajectory requests
	trajectorySub *natsclient.Subscription

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
}

// consumerInfo tracks JetStream consumer details for cleanup
type consumerInfo struct {
	streamName   string
	consumerName string
}

// NewComponent creates a new agentic-loop component
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	// Parse configuration — start from defaults so JSON only overrides
	// explicitly provided fields. Without this, zero-valued fields like
	// compact_threshold (0.0) and headroom_tokens (0) cause compaction
	// to trigger on every iteration regardless of context utilization.
	config := DefaultConfig()
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, errs.WrapInvalid(err, "agentic-loop", "NewComponent", "parse config")
	}
	config.Consumer.EnsureDefaults()
	config.Context.EnsureDefaults()

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, errs.WrapInvalid(err, "agentic-loop", "NewComponent", "validate config")
	}

	// Merge ports with defaults
	var inputPorts []component.Port
	var outputPorts []component.Port

	if config.Ports != nil && len(config.Ports.Inputs) > 0 {
		inputPorts = component.MergePortConfigs(
			buildDefaultInputPorts(),
			config.Ports.Inputs,
			component.DirectionInput,
		)
	} else {
		inputPorts = buildDefaultInputPorts()
	}

	if config.Ports != nil && len(config.Ports.Outputs) > 0 {
		outputPorts = component.MergePortConfigs(
			buildDefaultOutputPorts(),
			config.Ports.Outputs,
			component.DirectionOutput,
		)
	} else {
		outputPorts = buildDefaultOutputPorts()
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

	// Wire LLM-backed summarizer for context compaction if model registry is available
	if deps.ModelRegistry != nil && config.Context.Enabled {
		if summarizer, modelName := createSummarizer(deps, deps.GetLogger()); summarizer != nil {
			handler.SetSummarizer(summarizer, modelName)
		}
	}

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

	ep := deps.ModelRegistry.GetEndpoint(endpointName)
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

	return component.HealthStatus{
		Healthy:   healthy,
		LastCheck: time.Now(),
		Uptime:    uptime,
		Status:    status,
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

		// Set up NATS subscriptions for input ports
		if err := c.setupSubscriptions(ctx); err != nil {
			return errs.Wrap(err, "agentic-loop", "Start", "setup subscriptions")
		}

		// Set up trajectory query handler
		sub, err := c.natsClient.SubscribeForRequests(ctx, "agentic.query.trajectory", c.handleTrajectoryQuery)
		if err != nil {
			return errs.Wrap(err, "agentic-loop", "Start", "subscribe to trajectory query")
		}
		c.trajectorySub = sub
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

	// Unsubscribe from trajectory query handler
	if c.trajectorySub != nil {
		_ = c.trajectorySub.Unsubscribe()
		c.trajectorySub = nil
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

// buildDefaultInputPorts creates default input ports
func buildDefaultInputPorts() []component.Port {
	defaultCfg := DefaultConfig()
	ports := make([]component.Port, len(defaultCfg.Ports.Inputs))
	for i, portDef := range defaultCfg.Ports.Inputs {
		ports[i] = component.Port{
			Name:        portDef.Name,
			Direction:   component.DirectionInput,
			Required:    portDef.Required,
			Description: portDef.Description,
			Config: component.JetStreamPort{
				StreamName: portDef.StreamName,
				Subjects:   []string{portDef.Subject},
			},
		}
	}
	return ports
}

// buildDefaultOutputPorts creates default output ports
func buildDefaultOutputPorts() []component.Port {
	defaultCfg := DefaultConfig()
	ports := make([]component.Port, len(defaultCfg.Ports.Outputs))
	for i, portDef := range defaultCfg.Ports.Outputs {
		ports[i] = component.Port{
			Name:        portDef.Name,
			Direction:   component.DirectionOutput,
			Required:    false,
			Description: portDef.Description,
			Config: component.JetStreamPort{
				StreamName: portDef.StreamName,
				Subjects:   []string{portDef.Subject},
			},
		}
	}
	return ports
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

	// Initialize trajectory cache. Durable content is in ObjectStore + graph entities.
	trajCacheTTL := 4 * time.Hour // default
	if c.config.TrajectoryCacheTTL != "" {
		if parsed, parseErr := time.ParseDuration(c.config.TrajectoryCacheTTL); parseErr == nil {
			trajCacheTTL = parsed
		}
	}
	cleanupInterval := trajCacheTTL / 4
	if cleanupInterval < time.Minute {
		cleanupInterval = time.Minute
	}
	trajectoryCache, err := cache.NewTTL[*agentic.Trajectory](ctx, trajCacheTTL, cleanupInterval)
	if err != nil {
		return errs.Wrap(err, "agentic-loop", "initializeKVBuckets", "create trajectory cache")
	}
	c.trajectoryCache = trajectoryCache

	// Initialize content store for trajectory step content (tool results, model responses)
	contentBucket := c.config.ContentBucket
	if contentBucket == "" {
		contentBucket = "AGENT_CONTENT"
	}
	contentStore, err := objectstore.NewStoreWithConfig(ctx, c.natsClient, objectstore.Config{
		BucketName: contentBucket,
	})
	if err != nil {
		c.logger.Warn("Failed to create content store for trajectory steps, content storage disabled",
			"bucket", contentBucket, "error", err)
	} else if c.graphWriter != nil {
		c.graphWriter.contentStore = contentStore
	}

	// Initialize positions bucket if Boid coordination is enabled
	if c.config.BoidEnabled {
		bucketName := c.config.PositionsBucket
		if bucketName == "" {
			bucketName = "AGENT_POSITIONS"
		}

		positionsBucket, err := js.KeyValue(ctx, bucketName)
		if err != nil {
			// Bucket doesn't exist, try to create it
			positionsBucket, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
				Bucket: bucketName,
			})
			if err != nil {
				return errs.Wrap(err, "agentic-loop", "initializeKVBuckets", "create positions bucket")
			}
		}
		c.positionsBucket = positionsBucket

		// Parse signal TTL (use default if not specified or invalid)
		signalTTL := defaultSignalTTL
		if c.config.BoidSignalTTL != "" {
			if parsed, err := time.ParseDuration(c.config.BoidSignalTTL); err == nil && parsed > 0 {
				signalTTL = parsed
			}
		}

		// Initialize the Boid handler with configured TTL
		c.boidHandler = NewBoidHandlerWithTTL(c.positionsBucket, c.logger, c.decoder, signalTTL)
		c.logger.Info("Boid coordination enabled", "positions_bucket", bucketName, "signal_ttl", signalTTL)
	}

	return nil
}

// setupSubscriptions sets up JetStream consumers for input ports
func (c *Component) setupSubscriptions(ctx context.Context) error {
	for _, port := range c.inputPorts {
		var subject string

		// Handle both JetStreamPort and NATSPort for backward compatibility
		switch p := port.Config.(type) {
		case component.JetStreamPort:
			if len(p.Subjects) > 0 {
				subject = p.Subjects[0]
			}
		case component.NATSPort:
			subject = p.Subject
		}

		if subject == "" {
			continue
		}

		var handler func(context.Context, []byte)

		// Route to appropriate handler based on port name
		switch port.Name {
		case "agent.task":
			handler = c.handleTaskMessage
		case "agent.response":
			handler = c.handleResponseMessage
		case "tool.result":
			handler = c.handleToolResultMessage
		case "agent.signal":
			handler = c.handleSignalMessage
		case "agent.approval_response":
			handler = c.handleApprovalResponseMessage
		case "agent.boid":
			// Only subscribe to Boid signals if Boid coordination is enabled
			if !c.config.BoidEnabled {
				continue
			}
			handler = c.handleBoidSignalMessage
		default:
			c.logger.Warn("Unknown input port", "port", port.Name)
			continue
		}

		if err := c.setupConsumer(ctx, port, subject, handler); err != nil {
			return errs.Wrap(err, "agentic-loop", "setupSubscriptions", fmt.Sprintf("setup consumer for %s", subject))
		}
	}

	return nil
}

// resolveStreamName picks the JetStream stream a consumer should bind to
// for the given input port. Extracted as a pure function so the resolution
// rules can be unit-tested without spinning up NATS.
//
// Resolution order:
//  1. JetStreamPort.StreamName on the port (flow config's per-port override).
//  2. componentStreamName (component-wide default, typically c.config.StreamName).
//  3. Hardcoded "AGENT" fallback.
//
// Per-port stream_name lets a single component consume from multiple streams
// — e.g. agentic-loop watching agent.task on AGENT and tool.result on TOOL.
// Without this the component-wide default wins and the per-port flow-config
// field is silently ignored.
func resolveStreamName(port component.Port, componentStreamName string) string {
	if jsPort, ok := port.Config.(component.JetStreamPort); ok && jsPort.StreamName != "" {
		return jsPort.StreamName
	}
	if componentStreamName != "" {
		return componentStreamName
	}
	return "AGENT"
}

// setupConsumer sets up a JetStream consumer for an input port. The
// stream to bind to is resolved by resolveStreamName above.
func (c *Component) setupConsumer(ctx context.Context, port component.Port, subject string, handler func(context.Context, []byte)) error {
	streamName := resolveStreamName(port, c.config.StreamName)

	// Wait for stream to be available
	if err := c.waitForStream(ctx, streamName); err != nil {
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
	consumerCfg := component.GetConsumerConfig(port)

	// Differentiate consumer config by latency class:
	// - Long-running ports (task, response, tool.result) need serial processing,
	//   heartbeats, and graduated backoff to handle LLM-scale latency.
	// - Fast ports (signal, boid) keep short timeouts and higher concurrency.
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
	case "agent.task", "agent.response", "tool.result":
		ackWait = c.config.Consumer.ParsedAckWait()
		maxAckPending = 1
		maxDeliver = c.config.Consumer.MaxDeliver
		msgTimeout = 30 * time.Minute
		backOff = []time.Duration{30 * time.Second, 2 * time.Minute}
		useHeartbeat = true
		heartbeatInterval = c.config.Consumer.ParsedHeartbeatInterval()
	default: // agent.signal, agent.boid — fast, advisory
		ackWait = 30 * time.Second
		maxAckPending = 10
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
	}

	var handlerFn func(context.Context, jetstream.Msg)
	if useHeartbeat {
		hi := heartbeatInterval
		handlerFn = func(msgCtx context.Context, msg jetstream.Msg) {
			if err := natsclient.ConsumeWithHeartbeat(msgCtx, msg, hi,
				func(workCtx context.Context) error {
					handler(workCtx, msg.Data())
					return nil
				},
			); err != nil {
				c.logger.Error("Message handler error", "port", port.Name, "error", err)
			}
		}
	} else {
		handlerFn = func(msgCtx context.Context, msg jetstream.Msg) {
			handler(msgCtx, msg.Data())
			if ackErr := msg.Ack(); ackErr != nil {
				c.logger.Error("Failed to ack JetStream message", "error", ackErr)
			}
		}
	}

	err := c.natsClient.ConsumeStreamWithConfig(ctx, cfg, handlerFn)
	if err != nil {
		return errs.Wrap(err, "agentic-loop", "setupConsumer", fmt.Sprintf("setup consumer for stream %s", streamName))
	}

	// Track consumer for cleanup in Stop()
	c.consumerInfos = append(c.consumerInfos, consumerInfo{
		streamName:   streamName,
		consumerName: consumerName,
	})

	c.logger.Info("Subscribed (JetStream)",
		"subject", subject,
		"stream", streamName,
		"consumer", consumerName,
		"port", port.Name)
	return nil
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

// handleTaskMessage processes incoming task messages
func (c *Component) handleTaskMessage(ctx context.Context, data []byte) {
	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
		c.logger.Error("Failed to unmarshal BaseMessage", "error", err)
		return
	}

	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	if !ok {
		c.logger.Error("Unexpected payload type", "type", fmt.Sprintf("%T", baseMsg.Payload()))
		return
	}

	c.logger.Debug("Processing task message",
		slog.String("task_id", task.TaskID),
		slog.String("role", task.Role),
		slog.String("model", task.Model))

	// Handle the task using the message handler
	result, err := c.handler.HandleTask(ctx, *task)
	if err != nil {
		c.logger.Error("Failed to handle task", "error", err, "task_id", task.TaskID)
		return
	}

	// Record loop creation only when HandleTask actually created a new loop.
	// The dedup short-circuit (handlers.go HandleTask) returns Created=false
	// for redelivered TaskMessages whose task_id already has an active loop.
	// Gating here prevents the active_loops gauge from drifting upward on
	// every JetStream redelivery — the original loop's eventual completion
	// only fires one Dec(), so each ungated redelivery would leak +1.
	if c.metrics != nil && result.Created {
		c.metrics.recordLoopCreated()
	}

	if !result.Created {
		c.logger.Debug("Task deduplicated — loop already active",
			slog.String("loop_id", result.LoopID),
			slog.String("task_id", task.TaskID))
		return
	}

	c.logger.Debug("Loop created",
		slog.String("loop_id", result.LoopID),
		slog.String("task_id", task.TaskID))

	// Stamp cross-arc lineage triples on the spawned loop's entity.
	// Each entry in task.Metadata[MetadataKeyRelatedLoops] (set by
	// rule.executePublishAgent from rule.Action.RelatedLoops) becomes
	// a lineage.<key> triple. Downstream rules read via the existing
	// $entity.triple.<predicate> substitution. No-op when the producer
	// didn't set RelatedLoops (back-compat).
	if c.graphWriter != nil {
		if rawLineage, ok := task.Metadata[agentic.MetadataKeyRelatedLoops].(map[string]any); ok {
			c.graphWriter.WriteLineageTriples(ctx, result.LoopID, rawLineage)
		}
	}

	// Publish output messages
	c.publishResults(ctx, result)

	// Persist loop state to KV
	c.persistLoopState(ctx, result.LoopID)

	// Write initial Boid position for coordination
	c.writeInitialBoidPosition(ctx, result.LoopID, task)
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
		c.handleLoopFailure(ctx, loopID, entity, "handler_error", err)
		return
	}

	c.recordResponseMetrics(response, result, entity)
	c.persistHandlerResult(ctx, result)
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
	c.persistLoopState(ctx, result.LoopID)

	if result.State == agentic.LoopStateComplete || result.State == agentic.LoopStateFailed {
		c.finalizeTrajectory(ctx, result.LoopID, result.State)
		c.cleanupBoidPosition(ctx, result.LoopID)
		if result.CompletionState != nil {
			c.persistCompletionState(ctx, result.LoopID, result.CompletionState)
			c.stampLoopCompletionWithBudget(ctx, result.LoopID, result.CompletionState)
		} else if result.FailureState != nil {
			c.stampLoopFailureWithBudget(ctx, result.LoopID, result.FailureState)
		}
		c.writeTrajectoryToGraph(ctx, result.LoopID)
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
		return false
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

	// Apply accumulated Boid steering signals before handler processes result
	// This ensures steering affects context before the next model call is built
	c.applyAccumulatedSteering(loopID)

	// Handle the tool result using the message handler
	result, err := c.handler.HandleToolResult(ctx, loopID, toolResult)
	if err != nil {
		c.logger.Error("Failed to handle tool result", "error", err, "loop_id", loopID)
		return
	}

	// Update Boid position if enabled
	c.updateBoidPositionFromToolResult(ctx, loopID, toolResult)

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
	// plus finalizeTrajectory, persistCompletionState, and writeTrajectoryToGraph
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

	// Publish context events for agentic-memory to consume
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

	subject := component.ResolveSubject(c.config.Ports.Outputs, "agent.context.compaction", event.LoopID)
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

// writeTrajectoryToGraph reads the trajectory from the in-memory TrajectoryManager,
// stores step content in ObjectStore, and emits graph triples for each step.
func (c *Component) writeTrajectoryToGraph(ctx context.Context, loopID string) {
	if c.graphWriter == nil {
		return
	}
	traj, err := c.handler.trajectoryManager.GetTrajectory(loopID)
	if err != nil {
		c.logger.Debug("graph_writer: trajectory not found for graph emission",
			"loop_id", loopID, "error", err)
		return
	}
	c.graphWriter.WriteTrajectorySteps(ctx, loopID, &traj)
}

// finalizeTrajectory reads the trajectory from the in-memory TrajectoryManager,
// marks it complete, and caches it for post-completion queries.
func (c *Component) finalizeTrajectory(_ context.Context, loopID string, state agentic.LoopState) {
	traj, err := c.handler.trajectoryManager.GetTrajectory(loopID)
	if err != nil {
		c.logger.Debug("finalizeTrajectory: trajectory not found", "loop_id", loopID)
		return
	}
	if state == agentic.LoopStateComplete {
		traj.Complete("complete")
	} else {
		traj.Complete("failed")
	}
	if c.trajectoryCache != nil {
		if _, setErr := c.trajectoryCache.Set(loopID, &traj); setErr != nil {
			c.logger.Warn("Failed to cache finalized trajectory", "loop_id", loopID, "error", setErr)
		}
	}
}

// handleTrajectoryQuery handles NATS request/reply for trajectory queries.
// Serves from TTL cache for recently completed loops, or from the in-memory
// TrajectoryManager for active loops.
func (c *Component) handleTrajectoryQuery(_ context.Context, data []byte) ([]byte, error) {
	var req struct {
		LoopID string `json:"loopId"`
		Limit  int    `json:"limit,omitempty"`
	}
	if err := json.Unmarshal(data, &req); err != nil || req.LoopID == "" {
		return nil, fmt.Errorf("loopId required")
	}

	// Try cache first (finalized trajectories).
	var traj *agentic.Trajectory
	if c.trajectoryCache != nil {
		traj, _ = c.trajectoryCache.Get(req.LoopID)
	}

	// Fall back to in-memory TrajectoryManager (active loops).
	if traj == nil {
		t, err := c.handler.trajectoryManager.GetTrajectory(req.LoopID)
		if err != nil {
			return nil, fmt.Errorf("trajectory not found: %w", err)
		}
		traj = &t
	}

	if req.Limit > 0 && len(traj.Steps) > req.Limit {
		limited := *traj
		limited.Steps = limited.Steps[:req.Limit]
		return json.Marshal(&limited)
	}
	return json.Marshal(traj)
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
		WorkflowSlug: entity.WorkflowSlug,
		WorkflowStep: entity.WorkflowStep,
		CancelledAt:  entity.CancelledAt,
		Metadata:     entity.Metadata,
	}

	completionMsg := message.NewBaseMessage(completion.Schema(), &completion, "agentic-loop")
	completionData, err := json.Marshal(completionMsg)
	if err != nil {
		c.logger.Error("Failed to marshal completion",
			slog.String("error", err.Error()),
			slog.String("loop_id", loopID))
		return
	}

	subject := component.ResolveSubject(c.config.Ports.Outputs, "agent.complete", loopID)
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

	// Finalize trajectory and cleanup Boid position
	c.finalizeTrajectory(ctx, loopID, agentic.LoopStateCancelled)
	c.writeTrajectoryToGraph(ctx, loopID)
	c.cleanupBoidPosition(ctx, loopID)

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

// handleBoidSignalMessage processes incoming Boid steering signal messages.
// These signals come from Boid rules (separation, cohesion, alignment) and
// guide agent behavior for coordination.
func (c *Component) handleBoidSignalMessage(ctx context.Context, data []byte) {
	if c.boidHandler == nil {
		return
	}

	// Delegate to BoidHandler for parsing and processing
	signalType := c.boidHandler.HandleSteeringSignalMessage(ctx, data, c.getContextManagerForLoop)

	// Record metrics if signal was successfully processed
	if signalType != "" && c.metrics != nil {
		c.metrics.recordBoidSignalReceived(signalType)
	}
}

// getContextManagerForLoop returns the ContextManager for a given loop ID.
// This is used by BoidHandler to apply steering signals to context.
func (c *Component) getContextManagerForLoop(loopID string) *ContextManager {
	return c.handler.GetContextManager(loopID)
}

// updateBoidPositionFromToolResult updates the agent's position in the Boid coordination system
// based on entities accessed in tool results.
func (c *Component) updateBoidPositionFromToolResult(ctx context.Context, loopID string, toolResult agentic.ToolResult) {
	// Skip if Boid coordination is not enabled
	if c.boidHandler == nil {
		return
	}

	// Extract entities from tool result content
	entities := c.boidHandler.ExtractEntitiesFromToolResult(toolResult.Content)
	if len(entities) == 0 {
		return
	}

	// Get current position (or create new one)
	pos, err := c.boidHandler.GetPosition(ctx, loopID)
	if err != nil {
		// Position doesn't exist yet, create a new one
		entity, loopErr := c.handler.GetLoop(loopID)
		if loopErr != nil {
			c.logger.Debug("Failed to get loop for position update",
				"loop_id", loopID, "error", loopErr)
			return
		}

		pos = &boid.AgentPosition{
			LoopID:        loopID,
			Role:          entity.Role,
			FocusEntities: entities,
			Iteration:     entity.Iterations,
		}
	} else {
		// Calculate velocity based on position change
		oldFocus := pos.FocusEntities
		pos.Velocity = c.boidHandler.CalculateVelocity(oldFocus, entities)

		// Merge entities (keep recent focus + new entities)
		pos.FocusEntities = mergeEntities(pos.FocusEntities, entities, 10)
		pos.Iteration++
	}

	// Extract predicates for alignment rule data
	predicates := c.boidHandler.ExtractPredicatesFromToolResult(toolResult.Content)
	if len(predicates) > 0 {
		pos.TraversalVector = mergeStrings(predicates, pos.TraversalVector, maxTraversalVectorSize)
	}

	// Update position in KV
	if err := c.boidHandler.UpdatePosition(ctx, pos); err != nil {
		c.logger.Debug("Failed to update Boid position",
			"loop_id", loopID, "error", err)
	} else if c.metrics != nil {
		c.metrics.recordBoidPositionUpdate()
	}
}

// writeInitialBoidPosition writes an initial position at task arrival.
// This enables Boid rules to fire before the first tool result arrives.
func (c *Component) writeInitialBoidPosition(ctx context.Context, loopID string, task *agentic.TaskMessage) {
	if c.boidHandler == nil {
		return
	}

	// Determine initial focus entities (priority order)
	var focusEntities []string
	switch {
	case task.Context != nil && len(task.Context.Entities) > 0:
		// 1. Pre-constructed context entities (most reliable)
		focusEntities = task.Context.Entities
	default:
		// 2. Extract from prompt using existing entity pattern
		focusEntities = c.boidHandler.ExtractEntitiesFromToolResult(task.Prompt)
	}

	// Ensure non-nil slice for JSON serialization consistency
	if focusEntities == nil {
		focusEntities = []string{}
	}

	pos := &boid.AgentPosition{
		LoopID:        loopID,
		Role:          task.Role,
		FocusEntities: focusEntities,
		Velocity:      0.0, // Not moving yet
		Iteration:     0,   // Pre-first-iteration
		LastUpdate:    time.Now(),
	}

	if err := c.boidHandler.UpdatePosition(ctx, pos); err != nil {
		c.logger.Debug("Failed to write initial Boid position",
			"loop_id", loopID, "error", err)
		return
	}

	c.logger.Debug("Wrote initial Boid position",
		"loop_id", loopID,
		"role", task.Role,
		"focus_count", len(focusEntities))
}

// applyAccumulatedSteering applies all active Boid steering signals to a loop's context.
// This should be called before building the next model request to ensure steering
// signals affect entity prioritization in the context.
//
// Note: There is a small window between signal retrieval and application where signals
// could be modified. This is acceptable since signals are advisory steering guidance.
func (c *Component) applyAccumulatedSteering(loopID string) {
	if c.boidHandler == nil {
		return
	}

	cm := c.handler.GetContextManager(loopID)
	if cm == nil {
		c.logger.Debug("No context manager for loop, skipping steering",
			"loop_id", loopID)
		return
	}

	signals := c.boidHandler.GetActiveSignals(loopID)
	if len(signals) == 0 {
		return
	}

	steering := BoidSteeringConfig{}
	for _, signal := range signals {
		switch signal.SignalType {
		case boid.SignalTypeSeparation:
			steering.AvoidEntities = append(steering.AvoidEntities, signal.AvoidEntities...)
		case boid.SignalTypeCohesion:
			steering.PrioritizeEntities = append(steering.PrioritizeEntities, signal.SuggestedFocus...)
		case boid.SignalTypeAlignment:
			steering.AlignPatterns = append(steering.AlignPatterns, signal.AlignWith...)
		}
	}

	cm.ApplyBoidSteering(steering)
}

// mergeStringSlices merges two string slices, keeping the most recent up to maxCount.
// New items are added first, then existing items are added if not already present.
// This is the shared implementation for merging entities and predicates.
func mergeStringSlices(newItems, existing []string, maxCount int) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, maxCount)

	// Add new items first (they're more recent)
	for _, s := range newItems {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}

	// Add existing items
	for _, s := range existing {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}

	// Limit to maxCount
	if len(result) > maxCount {
		result = result[:maxCount]
	}

	return result
}

// mergeEntities merges two entity lists, keeping the most recent up to maxCount.
func mergeEntities(existing, newEntities []string, maxCount int) []string {
	return mergeStringSlices(newEntities, existing, maxCount)
}

// mergeStrings merges two string lists, keeping the most recent up to maxCount.
func mergeStrings(newItems, existing []string, maxCount int) []string {
	return mergeStringSlices(newItems, existing, maxCount)
}

// cleanupBoidPosition removes an agent's position and signals when the loop completes.
func (c *Component) cleanupBoidPosition(ctx context.Context, loopID string) {
	if c.boidHandler == nil {
		return
	}

	// Clear stored steering signals for this loop
	c.boidHandler.ClearSignals(loopID)

	// Delete position from KV bucket
	if err := c.boidHandler.DeletePosition(ctx, loopID); err != nil {
		c.logger.Debug("Failed to cleanup Boid position",
			"loop_id", loopID, "error", err)
	}
}

// WorkflowParticipant interface implementation.
// Agentic-loop handles multiple workflows dynamically, so it returns empty WorkflowID.

// WorkflowID returns empty string since this component handles multiple workflows dynamically.
// The workflow context is tracked per-loop via WorkflowSlug/WorkflowStep fields.
func (c *Component) WorkflowID() string {
	return ""
}

// Phase returns the workflow phase this component represents.
func (c *Component) Phase() string {
	return "agentic-execution"
}

// StateManager returns nil since agentic-loop manages its own state internally.
// Workflows interact with agentic-loop via events and KV watches, not direct state access.
func (c *Component) StateManager() *workflow.StateManager {
	return nil
}
