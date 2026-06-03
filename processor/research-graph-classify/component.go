package researchclassify

import (
	"context"
	"encoding/json"
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
	"github.com/c360studio/semstreams/graph/query"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/processor/research-graph-llmwrap"
)

// Component implements the nl_classify processor. The struct field
// set is intentionally small; the lifecycle methods (Start/Stop)
// own the NATS / model-registry plumbing and the per-message handler
// hands off to the pure classifyAndRetrieve function in handler.go.
type Component struct {
	config Config
	deps   component.Dependencies
	logger *slog.Logger

	// Injected dependencies. Tests replace these directly via the
	// constructor; production wires them via Start() from the
	// natsclient + model registry on deps.
	classifier Classifier
	retriever  CandidateRetriever
	loops      LoopStore

	// triplePub is the orchestration-triple stamp surface. Used to
	// emit research.classify.complete (+ candidate_count + degraded)
	// on the research-pipeline loop entity after each per-message
	// success. R1 of the rule chain (ADR-045) watches ENTITY_STATES
	// for research.classify.complete and dispatches route_search.
	// Nil-safe: a nil publisher logs warn and continues (degraded
	// observability, not crash).
	triplePub llmwrap.TriplePublisher

	// llmClient is non-nil when EnableLLMClassifier is true and the
	// model registry yields a query_classification capability. Owned
	// here so Stop can close it.
	llmClient llm.Client

	// Lifecycle state. One mutex guards started/startTime/
	// subscriptions across Start, Stop, Health, and DataFlow — the
	// pair-of-mutexes shape that graph-query and json_map use opened
	// a race window between Stop's `!started` guard and Health's
	// `started` read (caught in PR 2 review). One mutex collapses
	// the window without losing the Start/Stop serialisation, since
	// neither method ever returns to the caller mid-mutation while
	// holding the lock.
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

	// Lifecycle reporter (COMPONENT_STATUS KV; same convention as
	// json_map).
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
// adapters (NATS-backed LoopStore + searchGraphRetriever). The
// classifier chain is built keyword-only at construction; the LLM
// tier is wired in Start() when the model registry supplies a
// query_classification capability.
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
		config:     config,
		deps:       deps,
		logger:     logger,
		classifier: &classifierChainAdapter{chain: query.NewClassifierChain(query.NewKeywordClassifier(), nil, nil)},
		retriever:  newSearchGraphRetriever(deps.NATSClient, config.RetrieveTimeout),
		loops:      nil, // wired in Start() once the bucket is open
		triplePub:  llmwrap.NewNATSTriplePublisher(deps.NATSClient),
	}, nil
}

// Initialize is part of the LifecycleComponent contract. nl_classify
// has nothing to initialise pre-Start — bucket open and LLM client
// wire happen in Start() where they can fail loudly and propagate
// without breaking other components in the same Initialize phase.
func (c *Component) Initialize() error { return nil }

// Start opens the AGENT_LOOPS bucket, optionally wires the LLM tier
// into the classifier chain, subscribes to the configured input
// ports, and reports idle.
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

	c.initLLMClassifierIfEnabled()

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

	c.logger.Info("nl_classify component started",
		slog.String("loops_bucket", c.config.LoopsBucket),
		slog.Bool("llm_classifier", c.llmClient != nil),
		slog.Int("max_candidates", c.config.MaxCandidates))
	return nil
}

// openLoopsBucket opens (or idempotently creates) the AGENT_LOOPS KV
// bucket and constructs the LoopStore adapter. Mirrors json_map's
// COMPONENT_STATUS open pattern.
func (c *Component) openLoopsBucket(ctx context.Context) error {
	bucket, err := c.deps.NATSClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      c.config.LoopsBucket,
		Description: "Agent loops bucket; shared with agentic-loop and research_graph tool",
		History:     10,
		TTL:         24 * time.Hour,
	})
	if err != nil {
		return errs.WrapTransient(err, ComponentName, "Start", "open loops bucket")
	}
	if c.deps.PayloadRegistry == nil {
		return errs.WrapFatal(errs.ErrMissingConfig, ComponentName, "Start", "PayloadRegistry dependency required to decode research_intent from AGENT_LOOPS")
	}
	store := c.deps.NATSClient.NewKVStore(bucket)
	c.loops = newNATSLoopStore(store, c.deps.PayloadRegistry)
	return nil
}

// initLLMClassifierIfEnabled mirrors graph-query's initLLMClassifier:
// resolves the query_classification capability against the model
// registry and wires the LLM tier when available. Absence is a
// clean keyword-only fallback, not a startup error.
func (c *Component) initLLMClassifierIfEnabled() {
	if !c.config.EnableLLMClassifier {
		return
	}
	if c.deps.ModelRegistry == nil {
		c.logger.Debug("LLM classifier enable requested but no model registry available; using keyword-only")
		return
	}
	resolved, ep, err := model.ResolveEndpointWithConfig(c.deps.ModelRegistry, model.CapabilityQueryClassification)
	if err != nil {
		c.logger.Debug("no query_classification capability configured; keyword-only classifier",
			slog.Any("error", err))
		return
	}
	timeout := model.ResolveCapabilityTimeout(c.deps.ModelRegistry, model.CapabilityQueryClassification, query.DefaultClassificationTimeout, c.logger)
	cfg := llm.OpenAIConfigFromEndpoint(resolved, ep, c.logger)
	cfg.Timeout = timeout
	client, err := llm.NewOpenAIClient(cfg)
	if err != nil {
		c.logger.Warn("failed to construct LLM classifier client; using keyword-only",
			slog.Any("error", err))
		return
	}
	adapter := query.NewLLMClientAdapter(client, timeout)
	llmClassifier := query.NewLLMClassifier(adapter, nil)
	// gh#189: c.llmClient + c.classifier writes under c.mu so Stop's
	// close+nil-assign (also under c.mu) cannot race the write.
	// Critical section held only across the field assignments — the
	// constructor work above runs without the lock.
	c.mu.Lock()
	c.llmClient = client
	c.classifier = &classifierChainAdapter{
		chain: query.NewClassifierChain(query.NewKeywordClassifier(), nil, llmClassifier),
	}
	c.mu.Unlock()
	c.logger.Info("LLM tier wired into classifier chain",
		slog.String("model", resolved.Model),
		slog.Duration("timeout", timeout))
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
// available; degrades to a no-op reporter on failure (same pattern as
// json_map).
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

// Stop drains subscriptions, closes the LLM client, and reports
// shutdown. Flips `started` under c.mu so a concurrent Health /
// DataFlow read can't see a torn read of the lifecycle flag.
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

	// Wait for in-flight handlers to finish, bounded by the caller's
	// timeout. Goroutine-leak avoidance per the standard pattern.
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
	// initLLMTier (line ~232) is ALSO under c.mu, so the data race
	// between Stop's nil-assign and a concurrent Start's write is now
	// eliminated — `go test -race` is clean on
	// TestClassify_ConcurrentStartStop. Same shape as the
	// research-graph-route fix; paired per the gh#189 issue body
	// noting both components share this pattern.
	//
	// Acquired AFTER wg.Wait so handlers (which use c.classifier
	// transitively holding c.llmClient) drain before Close runs,
	// preventing HTTP connection corruption mid-call.
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
// the subject, loads the intent, runs classifyAndRetrieve, and
// writes the classifier_output envelope + the classify.complete
// trigger key. Errors are logged and counted; we do not propagate
// to NATS because the publisher is fire-and-forget (no reply
// expected) — propagation would silently drop the message.
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

	if c.config.ClassifyTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.config.ClassifyTimeout)
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

	output, err := classifyAndRetrieve(ctx, c.classifier, c.retriever, intent, c.config.MaxCandidates)
	if err != nil {
		c.logger.Error("classify+retrieve failed; ignoring message",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		atomic.AddInt64(&c.errors, 1)
		return
	}

	envelope := message.NewBaseMessage(output.Schema(), output, ComponentName)
	envelopeBytes, err := json.Marshal(envelope)
	if err != nil {
		c.logger.Error("marshal classifier output envelope failed",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		atomic.AddInt64(&c.errors, 1)
		return
	}

	// Write snapshot first (for queryability), then the trigger key
	// that R1 watches. Order matches research_graph's LoopEntity-then-
	// trigger discipline (PR 1) — R1's downstream actions need the
	// snapshot to be readable when they fire.
	if err := c.loops.PutSnapshot(ctx, loopID, envelopeBytes); err != nil {
		c.logger.Warn("snapshot write failed; chain continues but downstream readback may miss it",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
	}

	if err := c.loops.PutClassifierOutput(ctx, loopID, envelopeBytes); err != nil {
		c.logger.Error("classify.complete trigger write failed; R1 will not fire",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		atomic.AddInt64(&c.errors, 1)
		return
	}

	// Stamp the orchestration triple batch on the research-pipeline
	// loop entity in ENTITY_STATES so R1 (ADR-045 rule chain) can
	// fire on research.classify.complete. The KV envelope above is
	// for the next stage's payload consumption; the triples are for
	// rule-engine orchestration. Stamping failure is logged but not
	// fatal — the chain stalls observably in trajectory data rather
	// than crashing the handler.
	loopEntityID, entityErr := agentic.TryLoopExecutionEntityID(c.deps.Platform.Org, c.deps.Platform.Platform, loopID)
	if entityErr != nil {
		c.logger.Warn("could not construct loop entity ID for triple stamp; R1 will not fire",
			slog.String("loop_id", loopID),
			slog.Any("error", entityErr))
	} else {
		triples := research.BuildClassifyCompleteTriples(loopEntityID, len(output.Candidates), output.Degraded, time.Now())
		_ = llmwrap.StampOrchestrationTriples(ctx, c.triplePub, c.logger, ComponentName, loopID, triples)
	}

	atomic.AddInt64(&c.messagesEmitted, 1)
	c.logger.Info("nl_classify produced classifier_output",
		slog.String("loop_id", loopID),
		slog.String("topic", intent.Topic),
		slog.String("tier", output.Tier),
		slog.Int("candidates", len(output.Candidates)),
		slog.Bool("degraded", output.Degraded))
}

// Meta implements Discoverable.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        ComponentName,
		Type:        "processor",
		Description: "ADR-045 nl_classify: classifies a research topic via graph/query.ClassifierChain and surfaces initial candidate entities for downstream route_search.",
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

// OutputPorts implements Discoverable. nl_classify has no NATS-
// publishing output port: its emits are KV writes to AGENT_LOOPS.
// The empty slice surfaces honestly in flow-discovery so operators
// don't expect a NATS subject they'll never see.
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
