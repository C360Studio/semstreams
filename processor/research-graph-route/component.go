package researchroute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph/llm"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/processor/research-graph-llmwrap"
)

// Component implements the route_search processor. Struct field set
// is intentionally small; lifecycle methods own the NATS / model-
// registry plumbing and the per-message handler hands off to the
// pure routeDecision function in handler.go.
type Component struct {
	config Config
	deps   component.Dependencies
	logger *slog.Logger

	// Injected dependencies. Tests replace these directly via the
	// constructor; production wires them via Start() from the
	// natsclient + model registry on deps.
	router Router
	loops  LoopStore

	// triplePub stamps research.route.complete (+ action) on the
	// research-pipeline loop entity so R2 of the rule chain can fire
	// with action-level when clauses. Nil-safe per llmwrap contract.
	triplePub llmwrap.TriplePublisher

	// llmClient is the underlying graph/llm.Client owned by the
	// adapter. Held here so Stop can close it cleanly. Nil when
	// tests inject a Router fake directly.
	llmClient llm.Client

	// Lifecycle state. One mutex guards started/startTime so a
	// concurrent Health / DataFlow read can't see a torn read of the
	// lifecycle flag — same shape as research-graph-classify.
	mu        sync.RWMutex
	started   bool
	startTime time.Time
	wg        sync.WaitGroup

	// NATS subscriptions kept for cleanup at Stop.
	subscriptions []*natsclient.Subscription

	// Atomic counters for DataFlow / observability.
	messagesProcessed int64
	messagesEmitted   int64
	errors            int64
	lastActivity      atomic.Value // time.Time

	// Lifecycle reporter (COMPONENT_STATUS KV).
	lifecycleReporter component.LifecycleReporter
}

// Ensure interface compliance.
var (
	_ component.Discoverable       = (*Component)(nil)
	_ component.LifecycleComponent = (*Component)(nil)
)

// NewProcessor is the component-factory shape registered with the
// component registry. Parses + validates config, applies defaults,
// and constructs the Component with the injected production
// adapters. The LLM router is wired in Start() because the model
// registry isn't available at construction time.
func NewProcessor(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, errs.WrapInvalid(err, ComponentName, "NewProcessor", "config unmarshal")
	}
	if config.Ports == nil {
		config = DefaultConfig()
	}
	config.ApplyDefaults()
	if err := config.Validate(); err != nil {
		return nil, errs.WrapInvalid(err, ComponentName, "NewProcessor", "config validate")
	}

	if deps.NATSClient == nil {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, ComponentName, "NewProcessor", "NATSClient required")
	}

	logger := deps.GetLoggerWithComponent(ComponentName)

	return &Component{
		config:    config,
		deps:      deps,
		logger:    logger,
		triplePub: llmwrap.NewNATSTriplePublisher(deps.NATSClient),
		// router + loops wired in Start (router needs model registry;
		// loops needs the open KV bucket).
	}, nil
}

// Initialize is part of the LifecycleComponent contract.
// route_search has nothing to initialise pre-Start — bucket open +
// LLM client wire happen in Start so they can fail loudly and
// propagate.
func (c *Component) Initialize() error { return nil }

// Start opens the AGENT_LOOPS bucket, wires the LLM router from the
// model registry, subscribes to the configured input ports, and
// reports idle.
func (c *Component) Start(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, ComponentName, "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, ComponentName, "Start", "context already cancelled")
	}

	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return errs.WrapFatal(errs.ErrAlreadyStarted, ComponentName, "Start", "already started")
	}
	c.mu.Unlock()

	if err := c.openLoopsBucket(ctx); err != nil {
		return err
	}

	if err := c.initRouter(); err != nil {
		return err
	}

	if err := c.subscribeInputs(ctx); err != nil {
		return err
	}

	c.startLifecycleReporter(ctx)

	c.mu.Lock()
	c.started = true
	c.startTime = time.Now()
	c.mu.Unlock()

	if c.lifecycleReporter != nil {
		if err := c.lifecycleReporter.ReportStage(ctx, "idle"); err != nil {
			c.logger.Debug("failed to report idle stage", slog.Any("error", err))
		}
	}

	c.logger.Info("route_search component started",
		slog.String("loops_bucket", c.config.LoopsBucket),
		slog.Duration("route_timeout", c.config.RouteTimeout),
		slog.Int("max_candidates_in_prompt", c.config.MaxCandidatesInPrompt))
	return nil
}

// openLoopsBucket opens (or idempotently creates) the AGENT_LOOPS KV
// bucket and constructs the LoopStore adapter. Skips construction
// when c.loops is already non-nil (test-injected).
func (c *Component) openLoopsBucket(ctx context.Context) error {
	if c.loops != nil {
		return nil
	}
	bucket, err := c.deps.NATSClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      c.config.LoopsBucket,
		Description: "Agent loops bucket; shared with research-pipeline chain",
		History:     10,
		TTL:         24 * time.Hour,
	})
	if err != nil {
		return errs.WrapTransient(err, ComponentName, "Start", "open loops bucket")
	}
	if c.deps.PayloadRegistry == nil {
		return errs.WrapFatal(errs.ErrMissingConfig, ComponentName, "Start", "PayloadRegistry dependency required to decode research payloads")
	}
	store := c.deps.NATSClient.NewKVStore(bucket)
	c.loops = newNATSLoopStore(store, c.deps.PayloadRegistry)
	return nil
}

// initRouter resolves the CapabilityResearchRouting endpoint from
// the model registry and wires an llmRouterAdapter. Unlike
// research-graph-classify's optional LLM tier, route_search has no
// keyword fallback — absent capability is a startup error.
// Skips wiring when c.router is already non-nil (test-injected).
func (c *Component) initRouter() error {
	if c.router != nil {
		return nil
	}
	if c.deps.ModelRegistry == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, ComponentName, "Start",
			"model registry required (capability "+model.CapabilityResearchRouting+")")
	}
	resolved, ep, err := model.ResolveEndpointWithConfig(c.deps.ModelRegistry, model.CapabilityResearchRouting)
	if err != nil {
		return errs.WrapInvalid(err, ComponentName, "Start",
			"capability "+model.CapabilityResearchRouting+" required")
	}
	timeout := model.ResolveCapabilityTimeout(c.deps.ModelRegistry, model.CapabilityResearchRouting, c.config.RouteTimeout, c.logger)
	if timeout > 0 {
		c.config.RouteTimeout = timeout
	}
	cfg := llm.OpenAIConfigFromEndpoint(resolved, ep, c.logger)
	cfg.Timeout = c.config.RouteTimeout
	client, err := llm.NewOpenAIClient(cfg)
	if err != nil {
		return errs.WrapTransient(err, ComponentName, "Start", "construct LLM client")
	}
	// gh#189: c.llmClient + c.router writes under c.mu so Stop's
	// close+nil-assign (also under c.mu) cannot race the write. Without
	// the lock, `go test -race` flags the read/write pair. Holding the
	// lock only across the two-field assignment is safe — the I/O-ish
	// constructor work (ResolveEndpoint, NewOpenAIClient) ran above
	// without the lock, so we keep the critical section minimal.
	c.mu.Lock()
	c.llmClient = client
	c.router = newLLMRouterAdapter(client)
	c.mu.Unlock()
	c.logger.Info("LLM router wired",
		slog.String("model", resolved.Model),
		slog.Duration("timeout", c.config.RouteTimeout))
	return nil
}

// subscribeInputs wires NATS subscriptions for each input port.
func (c *Component) subscribeInputs(ctx context.Context) error {
	for _, port := range c.config.Ports.Inputs {
		if port.Subject == "" {
			continue
		}
		if port.Type != "nats" {
			c.logger.Warn("unsupported port type; skipping",
				slog.String("port", port.Name),
				slog.String("type", port.Type))
			continue
		}
		sub, err := c.deps.NATSClient.Subscribe(ctx, port.Subject, func(msgCtx context.Context, msg *nats.Msg) {
			c.handleMessage(msgCtx, msg.Subject, msg.Data)
		})
		if err != nil {
			return errs.WrapTransient(err, ComponentName, "Start",
				fmt.Sprintf("subscribe to %s", port.Subject))
		}
		c.subscriptions = append(c.subscriptions, sub)
		c.logger.Debug("subscribed to NATS subject",
			slog.String("port", port.Name),
			slog.String("subject", port.Subject))
	}
	return nil
}

// startLifecycleReporter wires the COMPONENT_STATUS KV reporter if
// available; degrades to a no-op reporter on failure.
func (c *Component) startLifecycleReporter(ctx context.Context) {
	statusBucket, err := c.deps.NATSClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      "COMPONENT_STATUS",
		Description: "Component lifecycle status tracking",
	})
	if err != nil {
		c.logger.Warn("COMPONENT_STATUS bucket unavailable; lifecycle reporting disabled",
			slog.Any("error", err))
		c.lifecycleReporter = component.NewNoOpLifecycleReporter()
		return
	}
	c.lifecycleReporter = component.NewLifecycleReporterFromConfig(component.LifecycleReporterConfig{
		KV:               statusBucket,
		ComponentName:    ComponentName,
		Logger:           c.logger,
		EnableThrottling: true,
	})
}

// Stop drains subscriptions, closes the LLM client, and flips
// `started` under c.mu so a concurrent Health / DataFlow read can't
// see a torn read of the lifecycle flag.
func (c *Component) Stop(timeout time.Duration) error {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return nil
	}
	c.started = false
	c.mu.Unlock()

	for _, sub := range c.subscriptions {
		if sub == nil {
			continue
		}
		if err := sub.Unsubscribe(); err != nil {
			c.logger.Debug("unsubscribe failed during stop", slog.Any("error", err))
		}
	}
	c.subscriptions = nil

	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		c.logger.Warn("Stop timeout reached with handlers in flight",
			slog.Duration("timeout", timeout))
	}

	// gh#189: c.mu guards the close+nil-assign. The matching write in
	// initRouter is ALSO under c.mu (see line ~225), so the data race
	// between Stop's nil-assign and a concurrent Start's write is now
	// eliminated — `go test -race` is clean on TestRoute_ConcurrentStartStop.
	//
	// Acquired AFTER wg.Wait (not during) because handlers route
	// through c.router, which transitively holds c.llmClient. wg.Wait
	// ensures every in-flight routeDecision call completes before we
	// close the client, preventing HTTP connection corruption mid-call.
	// Holding c.mu through wg.Wait would not deadlock today (handlers
	// don't touch c.mu) but ordering the close-after-drain is the
	// load-bearing invariant; the lock is for the assign race.
	c.mu.Lock()
	if c.llmClient != nil {
		if err := c.llmClient.Close(); err != nil {
			c.logger.Debug("LLM client close failed during stop", slog.Any("error", err))
		}
		c.llmClient = nil
	}
	c.mu.Unlock()
	return nil
}

// handleMessage is the per-message hot path. Recovers loop_id from
// the subject, loads upstream Intent + ClassifierOutput, runs
// routeDecision, writes the RouteDecision envelope + the
// route.complete trigger key. Errors are logged and counted; not
// propagated to NATS because the publisher is fire-and-forget.
//
// The ctx passed in comes from the NATS subscription callback and
// is intentionally tied to the subscription lifecycle — Stop's
// Unsubscribe cancels it so an in-flight LLM call drains fast.
// wg.Add(1) + Stop's wg.Wait give the bounded "drain by timeout"
// shape; ctx-cancel is the inner mechanism that surfaces as the
// LLM client's error return.
func (c *Component) handleMessage(ctx context.Context, subject string, _ []byte) {
	c.wg.Add(1)
	defer c.wg.Done()
	atomic.AddInt64(&c.messagesProcessed, 1)
	c.lastActivity.Store(time.Now())

	loopID := extractLoopIDFromSubject(subject)
	if loopID == "" {
		c.logger.Error("could not extract loop_id from subject; ignoring message",
			slog.String("subject", subject))
		atomic.AddInt64(&c.errors, 1)
		return
	}

	if c.config.RouteTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.config.RouteTimeout)
		defer cancel()
	}

	intent, err := c.loops.GetIntent(ctx, loopID)
	if err != nil {
		c.logger.Error("could not load research intent; ignoring message",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		atomic.AddInt64(&c.errors, 1)
		return
	}

	classifierOut, err := c.loops.GetClassifierOutput(ctx, loopID)
	if err != nil {
		// errClassifierOutputNotFound surfaces as a routing-config
		// bug (R1 fired without classify.complete written) — same
		// shape as nl_classify's errIntentNotFound handling.
		level := slog.LevelError
		if errors.Is(err, errClassifierOutputNotFound) {
			level = slog.LevelWarn
		}
		c.logger.Log(ctx, level, "could not load classifier output; ignoring message",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		atomic.AddInt64(&c.errors, 1)
		return
	}

	decision, err := routeDecision(ctx, c.router, intent, classifierOut, c.config.MaxResponseTokens, c.config.MaxCandidatesInPrompt, c.logger)
	if err != nil {
		c.logger.Error("route_decision failed; ignoring message",
			slog.String("loop_id", loopID),
			slog.String("topic", intent.Topic),
			slog.Any("error", err))
		atomic.AddInt64(&c.errors, 1)
		return
	}

	envelope := message.NewBaseMessage(decision.Schema(), decision, ComponentName)
	envelopeBytes, err := json.Marshal(envelope)
	if err != nil {
		c.logger.Error("marshal route decision envelope failed",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		atomic.AddInt64(&c.errors, 1)
		return
	}

	// Write snapshot first (queryable), then trigger key R2 watches.
	// Order matches nl_classify's snapshot-then-trigger discipline so
	// downstream readback can't miss the snapshot when R2 fires.
	if err := c.loops.PutSnapshot(ctx, loopID, envelopeBytes); err != nil {
		c.logger.Warn("snapshot write failed; chain continues but downstream readback may miss it",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
	}

	if err := c.loops.PutRouteDecision(ctx, loopID, envelopeBytes); err != nil {
		c.logger.Error("route.complete trigger write failed; R2 will not fire",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		atomic.AddInt64(&c.errors, 1)
		return
	}

	// Stamp research.route.complete + research.route.action on the
	// research-pipeline loop entity so R2 (ADR-045 rule chain) can
	// fire with action-level when clauses. Atomic batch landing keeps
	// action paired with complete so R2 never sees a stale action.
	if loopEntityID, entityErr := agentic.TryLoopExecutionEntityID(c.deps.Platform.Org, c.deps.Platform.Platform, loopID); entityErr != nil {
		c.logger.Warn("could not construct loop entity ID for triple stamp; R2 will not fire",
			slog.String("loop_id", loopID),
			slog.Any("error", entityErr))
	} else {
		triples := research.BuildRouteCompleteTriples(loopEntityID, decision.Action, time.Now())
		_ = llmwrap.StampOrchestrationTriples(ctx, c.triplePub, c.logger, ComponentName, loopID, triples)
	}

	atomic.AddInt64(&c.messagesEmitted, 1)
	c.logger.Info("route_search emitted RouteDecision",
		slog.String("loop_id", loopID),
		slog.String("topic", intent.Topic),
		slog.String("action", decision.Action))
}

// Meta implements Discoverable.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        ComponentName,
		Type:        "processor",
		Description: "ADR-045 route_search: structured-emit routing decision over upstream ClassifierOutput. Emits one of synthesize_directly / retighten / walk_seeds / decompose for R2 dispatch.",
		Version:     "0.1.0",
	}
}

// InputPorts implements Discoverable.
func (c *Component) InputPorts() []component.Port {
	ports := make([]component.Port, 0, len(c.config.Ports.Inputs))
	for _, p := range c.config.Ports.Inputs {
		ports = append(ports, component.Port{
			Name:      p.Name,
			Direction: component.DirectionInput,
			Required:  p.Required,
			Config: component.NATSPort{
				Subject: p.Subject,
			},
		})
	}
	return ports
}

// OutputPorts implements Discoverable. route_search has no NATS-
// publishing output port: emits are KV writes to AGENT_LOOPS.
func (c *Component) OutputPorts() []component.Port { return []component.Port{} }

// ConfigSchema implements Discoverable.
func (c *Component) ConfigSchema() component.ConfigSchema { return configSchema }

// Health implements Discoverable.
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return component.HealthStatus{
		Healthy:    c.started,
		LastCheck:  time.Now(),
		ErrorCount: int(atomic.LoadInt64(&c.errors)),
		Uptime:     time.Since(c.startTime),
	}
}

// DataFlow implements Discoverable.
func (c *Component) DataFlow() component.FlowMetrics {
	processed := atomic.LoadInt64(&c.messagesProcessed)
	errCount := atomic.LoadInt64(&c.errors)
	var errRate float64
	if processed > 0 {
		errRate = float64(errCount) / float64(processed)
	}
	last, _ := c.lastActivity.Load().(time.Time)
	return component.FlowMetrics{
		ErrorRate:    errRate,
		LastActivity: last,
	}
}
