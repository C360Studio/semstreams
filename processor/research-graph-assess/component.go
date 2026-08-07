package researchassess

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

// Component implements the assess_sufficiency processor. Same shape
// as research-graph-route — lifecycle methods own the NATS / model-
// registry plumbing and the per-message handler hands off to the
// pure assessSufficiency function in handler.go.
type Component struct {
	config Config
	deps   component.Dependencies
	logger *slog.Logger

	// Injected dependencies. Tests replace these directly via the
	// constructor; production wires them via Start() from the
	// natsclient + model registry on deps.
	assessor Assessor
	loops    LoopStore

	// triplePub stamps research.assess.complete (+ sufficient) on the
	// research-pipeline loop entity so R4 of the rule chain can branch
	// (sufficient → synthesize; insufficient → refine via execute). Nil-safe.
	triplePub llmwrap.TriplePublisher

	// llmClient is the underlying graph/llm.Client owned by the
	// adapter. Held here so Stop can close it cleanly. Nil when
	// tests inject an Assessor fake directly.
	llmClient llm.Client

	// Lifecycle state. One mutex guards started/startTime so a
	// concurrent Health / DataFlow read can't see a torn read of the
	// lifecycle flag — same shape as research-graph-route.
	mu        sync.RWMutex
	started   bool
	startTime time.Time
	wg        sync.WaitGroup

	subscriptions []*natsclient.Subscription

	messagesProcessed int64
	messagesEmitted   int64
	errors            int64
	lastActivity      atomic.Value // time.Time

}

var (
	_ component.Discoverable       = (*Component)(nil)
	_ component.LifecycleComponent = (*Component)(nil)
)

// NewProcessor is the component-factory shape registered with the
// component registry. Parses + validates config, applies defaults,
// and constructs the Component with the injected production
// adapters. The LLM assessor is wired in Start() because the model
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
	}, nil
}

// Initialize is part of the LifecycleComponent contract. Nothing
// pre-Start.
func (c *Component) Initialize() error { return nil }

// Start opens the AGENT_LOOPS bucket, wires the LLM assessor from the
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
	if err := c.initAssessor(); err != nil {
		return err
	}
	if err := c.subscribeInputs(ctx); err != nil {
		return err
	}
	c.mu.Lock()
	c.started = true
	c.startTime = time.Now()
	c.mu.Unlock()

	c.logger.Info("assess_sufficiency component started",
		slog.String("loops_bucket", c.config.LoopsBucket),
		slog.Duration("assess_timeout", c.config.AssessTimeout),
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

// initAssessor resolves the CapabilityResearchAssessment endpoint
// from the model registry and wires an llmAssessorAdapter. Absent
// capability is a startup error — assess_sufficiency has no
// keyword-only fallback. Skips wiring when c.assessor is already
// non-nil (test-injected).
func (c *Component) initAssessor() error {
	if c.assessor != nil {
		return nil
	}
	if c.deps.ModelRegistry == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, ComponentName, "Start",
			"model registry required (capability "+model.CapabilityResearchAssessment+")")
	}
	resolved, ep, err := model.ResolveEndpointWithConfig(c.deps.ModelRegistry, model.CapabilityResearchAssessment)
	if err != nil {
		return errs.WrapInvalid(err, ComponentName, "Start",
			"capability "+model.CapabilityResearchAssessment+" required")
	}
	timeout := model.ResolveCapabilityTimeout(c.deps.ModelRegistry, model.CapabilityResearchAssessment, c.config.AssessTimeout, c.logger)
	if timeout > 0 {
		c.config.AssessTimeout = timeout
	}
	cfg := llm.OpenAIConfigFromEndpoint(resolved, ep, c.logger)
	cfg.Timeout = c.config.AssessTimeout
	client, err := llm.NewOpenAIClient(cfg)
	if err != nil {
		return errs.WrapTransient(err, ComponentName, "Start", "construct LLM client")
	}
	c.llmClient = client
	c.assessor = newLLMAssessorAdapter(client)
	c.logger.Info("LLM assessor wired",
		slog.String("model", resolved.Model),
		slog.Duration("timeout", c.config.AssessTimeout))
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

// Stop drains subscriptions, closes the LLM client, and flips
// `started` under c.mu.
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
// ExecutionOutput, runs assessSufficiency, writes the
// AssessmentOutput envelope + assess.complete trigger key. Errors
// are logged and counted; not propagated to NATS because the
// publisher is fire-and-forget.
//
// Recovery contract:
//   - Intent missing/error: hard abort. Without the intent topic we
//     can't even populate a sensible degraded envelope.
//   - ExecutionOutput missing: emit degraded envelope (Sufficient=
//     false, Degraded=true, refined_queries empty) so R3 still
//     fires — refine path runs as a safety net. The chain stays
//     moving rather than stranding.
//   - assessSufficiency failure (LLM error, parse failure): emit
//     degraded envelope with the same shape. Trajectory review
//     surfaces the failure cause via DegradedReason.
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

	if c.config.AssessTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.config.AssessTimeout)
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
		c.logger.Log(ctx, level, "could not load execution output; emitting degraded assessment",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		c.emitDegraded(ctx, loopID, intent.Topic, "execute.complete missing or undecodeable: "+err.Error())
		return
	}

	assessment, err := assessSufficiency(ctx, c.assessor, intent, exec, c.config.MaxResponseTokens, c.config.MaxEvidenceInPrompt, c.config.MaxSnippetCharsInPrompt, c.logger)
	if err != nil {
		c.logger.Error("assess_sufficiency failed; emitting degraded assessment",
			slog.String("loop_id", loopID),
			slog.String("topic", intent.Topic),
			slog.Any("error", err))
		c.emitDegraded(ctx, loopID, intent.Topic, "assessor: "+err.Error())
		return
	}

	c.writeAssessment(ctx, loopID, assessment)
}

// emitDegraded constructs and writes a safety-net AssessmentOutput
// envelope (Sufficient=false, Degraded=true, RefinedQueries empty)
// so R3 still fires and the chain doesn't strand. EvidenceCount is
// zero — the assessor either didn't run or ran on missing input;
// downstream consumers should treat the field as un-populated.
func (c *Component) emitDegraded(ctx context.Context, loopID, topic, reason string) {
	if topic == "" {
		// Shouldn't happen — handleMessage hard-aborts on missing
		// Intent — but defend so a future refactor doesn't ship an
		// envelope that fails AssessmentOutput.Validate.
		topic = "(unknown)"
	}
	out := &research.AssessmentOutput{
		Topic:          topic,
		Sufficient:     false,
		Degraded:       true,
		DegradedReason: reason,
	}
	c.writeAssessment(ctx, loopID, out)
}

// writeAssessment marshals and writes the assessment envelope. Used
// by both the happy path and the degraded path so envelope
// construction stays consistent (snapshot first for queryability,
// then trigger key for R3).
//
// Validate runs before marshal even on the degraded path: emitDegraded
// constructs Topic + Sufficient=false today, but a future change to
// either the degraded shape or AssessmentOutput.Validate's rules
// could ship a wire-invalid envelope silently. Surfacing the
// validation failure as a logged-and-dropped emit keeps the contract
// honest at the boundary.
func (c *Component) writeAssessment(ctx context.Context, loopID string, out *research.AssessmentOutput) {
	if err := out.Validate(); err != nil {
		c.logger.Error("assessment output failed validation; refusing to emit",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		atomic.AddInt64(&c.errors, 1)
		return
	}
	envelope := message.NewBaseMessage(out.Schema(), out, ComponentName)
	envelopeBytes, err := json.Marshal(envelope)
	if err != nil {
		c.logger.Error("marshal assessment output envelope failed",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		atomic.AddInt64(&c.errors, 1)
		return
	}

	// Write snapshot first (queryable), then trigger key R3 watches.
	// Order matches route_search's snapshot-then-trigger discipline so
	// downstream readback can't miss the snapshot when R3 fires.
	if err := c.loops.PutSnapshot(ctx, loopID, envelopeBytes); err != nil {
		c.logger.Warn("snapshot write failed; chain continues but downstream readback may miss it",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
	}

	if err := c.loops.PutAssessmentOutput(ctx, loopID, envelopeBytes); err != nil {
		c.logger.Error("assess.complete trigger write failed; R4 will not fire",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		atomic.AddInt64(&c.errors, 1)
		return
	}

	// Stamp research.assess.complete + research.assess.sufficient on
	// the research-pipeline loop entity so R4 of the ADR-045 rule chain
	// can branch via action-level when clauses (sufficient → synthesize,
	// insufficient → refine).
	if loopEntityID, entityErr := agentic.TryLoopExecutionEntityID(c.deps.Platform.Org, c.deps.Platform.Platform, loopID); entityErr != nil {
		c.logger.Warn("could not construct loop entity ID for triple stamp; R4 will not fire",
			slog.String("loop_id", loopID),
			slog.Any("error", entityErr))
	} else {
		triples := research.BuildAssessCompleteTriples(loopEntityID, out.Sufficient, time.Now())
		_ = llmwrap.StampOrchestrationTriples(ctx, c.triplePub, c.logger, ComponentName, loopID, triples)
	}

	atomic.AddInt64(&c.messagesEmitted, 1)
	c.logger.Info("assess_sufficiency emitted AssessmentOutput",
		slog.String("loop_id", loopID),
		slog.String("topic", out.Topic),
		slog.Bool("sufficient", out.Sufficient),
		slog.Int("refined_query_count", len(out.RefinedQueries)),
		slog.Bool("degraded", out.Degraded))
}

// Meta implements Discoverable.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        ComponentName,
		Type:        "processor",
		Description: "ADR-045 assess_sufficiency: structured-emit sufficient/refine decision over upstream ExecutionOutput evidence. Drives R3's synthesize-or-refine branch.",
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

// OutputPorts implements Discoverable.
func (c *Component) OutputPorts() []component.Port {
	ports := make([]component.Port, 0, len(c.config.Ports.Outputs))
	for _, definition := range c.config.Ports.Outputs {
		ports = append(ports, component.BuildPortFromDefinition(definition, component.DirectionOutput))
	}
	return ports
}

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
