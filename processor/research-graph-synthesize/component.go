package researchsynthesize

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

// Component implements the synthesize_answer processor. Same
// structural shape as research-graph-route + research-graph-assess.
type Component struct {
	config Config
	deps   component.Dependencies
	logger *slog.Logger

	synthesizer Synthesizer
	loops       LoopStore

	// triplePub stamps research.search_result.complete (+ ref) on the
	// research-pipeline loop entity — the terminal stage signal R6
	// (continuation) watches to route the result back to the parent
	// loop. Nil-safe.
	triplePub llmwrap.TriplePublisher

	llmClient llm.Client

	mu        sync.RWMutex
	started   bool
	startTime time.Time
	wg        sync.WaitGroup

	subscriptions []*natsclient.Subscription

	messagesProcessed int64
	messagesEmitted   int64
	errors            int64
	lastActivity      atomic.Value // time.Time

	lifecycleReporter component.LifecycleReporter
}

var (
	_ component.Discoverable       = (*Component)(nil)
	_ component.LifecycleComponent = (*Component)(nil)
)

// NewProcessor is the component-factory shape registered with the
// component registry.
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
	}, nil
}

// Initialize is part of the LifecycleComponent contract.
func (c *Component) Initialize() error { return nil }

// Start opens AGENT_LOOPS, wires the LLM synthesizer, subscribes
// inputs, reports idle.
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
	if err := c.initSynthesizer(); err != nil {
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

	c.logger.Info("synthesize_answer component started",
		slog.String("loops_bucket", c.config.LoopsBucket),
		slog.Duration("synthesize_timeout", c.config.SynthesizeTimeout),
		slog.Int("max_evidence_in_prompt", c.config.MaxEvidenceInPrompt))
	return nil
}

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

func (c *Component) initSynthesizer() error {
	if c.synthesizer != nil {
		return nil
	}
	if c.deps.ModelRegistry == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, ComponentName, "Start",
			"model registry required (capability "+model.CapabilityResearchSynthesis+")")
	}
	resolved, ep, err := model.ResolveEndpointWithConfig(c.deps.ModelRegistry, model.CapabilityResearchSynthesis)
	if err != nil {
		return errs.WrapInvalid(err, ComponentName, "Start",
			"capability "+model.CapabilityResearchSynthesis+" required")
	}
	timeout := model.ResolveCapabilityTimeout(c.deps.ModelRegistry, model.CapabilityResearchSynthesis, c.config.SynthesizeTimeout, c.logger)
	if timeout > 0 {
		c.config.SynthesizeTimeout = timeout
	}
	cfg := llm.OpenAIConfigFromEndpoint(resolved, ep, c.logger)
	cfg.Timeout = c.config.SynthesizeTimeout
	client, err := llm.NewOpenAIClient(cfg)
	if err != nil {
		return errs.WrapTransient(err, ComponentName, "Start", "construct LLM client")
	}
	c.llmClient = client
	c.synthesizer = newLLMSynthesizerAdapter(client)
	c.logger.Info("LLM synthesizer wired",
		slog.String("model", resolved.Model),
		slog.Duration("timeout", c.config.SynthesizeTimeout))
	return nil
}

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

// Stop drains subscriptions, closes the LLM client.
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

	if c.llmClient != nil {
		if err := c.llmClient.Close(); err != nil {
			c.logger.Debug("LLM client close failed during stop", slog.Any("error", err))
		}
		c.llmClient = nil
	}
	return nil
}

// handleMessage is the per-message hot path. Loads upstream Intent +
// ExecutionOutput (+ optional RouteDecision), runs synthesizeAnswer,
// writes the SearchResult envelope + synthesize.complete trigger key.
//
// Recovery contract:
//   - Intent missing/error: hard abort. Without the topic we can't
//     populate a meaningful degraded synthesis.
//   - ExecutionOutput missing: emit degraded SearchResult so the
//     continuation rule still fires and the parent gets a clear "no
//     evidence" signal instead of stranding on a missing key.
//   - synthesizeAnswer failure: emit degraded SearchResult; trajectory
//     review surfaces the failure cause.
//   - RouteDecision missing: NOT an error. Synthesis proceeds with a
//     DecompTrace built from ExecutionOutput alone.
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

	if c.config.SynthesizeTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.config.SynthesizeTimeout)
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

	exec, err := c.loops.GetExecutionOutput(ctx, loopID)
	if err != nil {
		level := slog.LevelError
		if errors.Is(err, errExecutionOutputNotFound) {
			level = slog.LevelWarn
		}
		c.logger.Log(ctx, level, "could not load execution output; emitting degraded synthesis",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		c.emitDegraded(ctx, loopID, intent.Topic, "execute.complete missing or undecodeable: "+err.Error())
		return
	}

	route, err := c.loops.GetRouteDecision(ctx, loopID)
	if err != nil {
		// RouteDecision is observability-grade; a decode error logs
		// but doesn't fail the synthesis. Treat the same as missing.
		c.logger.Warn("route decision load failed; proceeding without DecompTrace router action",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		route = nil
	}

	result, err := synthesizeAnswer(ctx, c.synthesizer, intent, exec, route, c.config.MaxResponseTokens, c.config.MaxEvidenceInPrompt, c.config.MaxSnippetCharsInPrompt, c.logger)
	if err != nil {
		c.logger.Error("synthesize_answer failed; emitting degraded synthesis",
			slog.String("loop_id", loopID),
			slog.String("topic", intent.Topic),
			slog.Any("error", err))
		c.emitDegraded(ctx, loopID, intent.Topic, "synthesizer: "+err.Error())
		return
	}

	c.writeResult(ctx, loopID, result)
}

// emitDegraded constructs and writes a safety-net SearchResult so the
// continuation rule fires even when synthesis couldn't run cleanly.
// Synthesis text marks the failure; Evidence is nil; DecompTrace
// records the upstream RouterAction when available (best effort).
func (c *Component) emitDegraded(ctx context.Context, loopID, topic, reason string) {
	if topic == "" {
		topic = "(unknown)"
	}
	result := &research.SearchResult{
		Synthesis: "Synthesis could not be produced for topic " + topic + ". Reason: " + reason,
	}
	c.writeResult(ctx, loopID, result)
}

// writeResult marshals and writes the SearchResult envelope. Snapshot
// first (queryable), then trigger key the continuation rule watches.
//
// Validate runs before marshal even on the degraded path: emitDegraded
// constructs a Synthesis string today, but a future change to either
// the degraded shape or SearchResult.Validate's rules could ship a
// wire-invalid envelope silently. Surfacing the validation failure
// as a logged-and-dropped emit keeps the contract honest at the
// boundary.
func (c *Component) writeResult(ctx context.Context, loopID string, result *research.SearchResult) {
	if err := result.Validate(); err != nil {
		c.logger.Error("search result failed validation; refusing to emit",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		atomic.AddInt64(&c.errors, 1)
		return
	}
	envelope := message.NewBaseMessage(result.Schema(), result, ComponentName)
	envelopeBytes, err := json.Marshal(envelope)
	if err != nil {
		c.logger.Error("marshal search result envelope failed",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		atomic.AddInt64(&c.errors, 1)
		return
	}

	if err := c.loops.PutSnapshot(ctx, loopID, envelopeBytes); err != nil {
		c.logger.Warn("snapshot write failed; chain continues but downstream readback may miss it",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
	}

	if err := c.loops.PutSearchResult(ctx, loopID, envelopeBytes); err != nil {
		c.logger.Error("synthesize.complete trigger write failed; continuation rule will not fire",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		atomic.AddInt64(&c.errors, 1)
		return
	}

	// Write a duplicate of the SearchResult envelope at the
	// COMPLETE_<loopID> key so the parent agent can fetch it via the
	// existing read_loop_result tool (which reads COMPLETE_ keys). The
	// search_result.complete.<loopID> key remains the per-stage
	// trigger envelope; this is the operator-consumption surface.
	// Failure is logged but non-fatal — search_result.complete already
	// landed, R6 still fires, the parent just can't read the result
	// via read_loop_result. Operator dashboards see the gap.
	if err := c.loops.PutLoopCompletion(ctx, loopID, envelopeBytes); err != nil {
		c.logger.Warn("COMPLETE_ write failed; parent's read_loop_result will return key-not-found",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
	}

	// Stamp the terminal triple batch on the research-pipeline loop
	// entity: research.search_result.complete + .ref so R6 (the
	// ADR-045 continuation rule) can route the SearchResult back to
	// the parent loop via publish_agent. The ref points at the
	// AGENT_LOOPS key carrying the SearchResult envelope per ADR-028
	// (rules carry references, not content).
	resultRef := "search_result.complete." + loopID
	if loopEntityID, entityErr := agentic.TryLoopExecutionEntityID(c.deps.Platform.Org, c.deps.Platform.Platform, loopID); entityErr != nil {
		c.logger.Warn("could not construct loop entity ID for triple stamp; continuation rule will not fire",
			slog.String("loop_id", loopID),
			slog.Any("error", entityErr))
	} else {
		triples := research.BuildSearchResultCompleteTriples(loopEntityID, resultRef, time.Now())
		_ = llmwrap.StampOrchestrationTriples(ctx, c.triplePub, c.logger, ComponentName, loopID, triples)
	}

	atomic.AddInt64(&c.messagesEmitted, 1)
	c.logger.Info("synthesize_answer emitted SearchResult",
		slog.String("loop_id", loopID),
		slog.Int("evidence_count", len(result.Evidence)),
		slog.Int("synthesis_bytes", len(result.Synthesis)))
}

// Meta implements Discoverable.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        ComponentName,
		Type:        "processor",
		Description: "ADR-045 synthesize_answer: terminal LLM stage. Quote-back-validated synthesis grounded in upstream evidence. Drives the continuation rule.",
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

// OutputPorts implements Discoverable. synthesize_answer has no
// NATS-publishing output port: emits are KV writes to AGENT_LOOPS.
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
