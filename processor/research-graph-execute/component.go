package researchexecute

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
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/processor/research-graph-llmwrap"
)

// Component implements the execute_subqueries processor. Struct
// field set is intentionally small; lifecycle methods own the NATS
// plumbing and the per-message handler hands off to the pure
// executeAll function in handler.go.
type Component struct {
	config        Config
	deps          component.Dependencies
	logger        *slog.Logger
	inputs        []component.Port
	outputs       []component.Port
	querySubjects graphQueryAdapterSubjects

	// Injected dependencies. Tests replace these directly via the
	// constructor; production wires them via Start() from the
	// natsclient on deps.
	gq    GraphQueryClient
	loops LoopStore

	// triplePub stamps research.execute.complete (+ evidence_count) on
	// the research-pipeline loop entity so R3 of the rule chain can
	// dispatch assess_sufficiency. Nil-safe.
	triplePub llmwrap.TriplePublisher

	// Lifecycle state. One mutex guards started/startTime so
	// concurrent Health / DataFlow reads can't see a torn read of
	// the lifecycle flag.
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
	inputs := make([]component.Port, 0, len(config.Ports.Inputs))
	for _, definition := range config.Ports.Inputs {
		port, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			return nil, errs.WrapInvalid(err, ComponentName, "NewProcessor", "resolve input port")
		}
		facts, err := port.Facts()
		if err != nil || facts.Kind() != component.PortKindNATS || len(facts.NATSSubjects()) != 1 {
			return nil, errs.WrapInvalid(errs.ErrInvalidConfig, ComponentName, "NewProcessor", "input ports must each declare one NATS subject")
		}
		inputs = append(inputs, port)
	}
	outputs := make([]component.Port, 0, len(config.Ports.Outputs))
	for _, definition := range config.Ports.Outputs {
		port, err := definition.Resolve(component.DirectionOutput)
		if err != nil {
			return nil, errs.WrapInvalid(err, ComponentName, "NewProcessor", "resolve output port")
		}
		outputs = append(outputs, port)
	}
	querySubjects, err := resolveGraphQueryOutputs(outputs)
	if err != nil {
		return nil, errs.WrapInvalid(err, ComponentName, "NewProcessor", "resolve graph query outputs")
	}
	return &Component{
		config:        config,
		deps:          deps,
		logger:        logger,
		inputs:        inputs,
		outputs:       outputs,
		querySubjects: querySubjects,
		triplePub:     llmwrap.NewNATSTriplePublisher(deps.NATSClient),
	}, nil
}

func resolveGraphQueryOutputs(outputs []component.Port) (graphQueryAdapterSubjects, error) {
	if len(outputs) != 5 {
		return graphQueryAdapterSubjects{}, fmt.Errorf("exactly graph_mutations and four graph query outputs are required, got %d", len(outputs))
	}
	want := map[string]string{
		"batch":         "graph.query.batch",
		"relationships": "graph.query.relationships",
		"temporal":      "graph.query.temporal",
		"searchGraph":   "graph.query.searchGraph",
	}
	resolved := make(map[string]string, len(want))
	mutationFound := false
	for _, port := range outputs {
		if port.Name == "graph_mutations" {
			if mutationFound || !port.Required {
				return graphQueryAdapterSubjects{}, errors.New("graph_mutations output must be unique and required")
			}
			mutationFound = true
			continue
		}
		subject, ok := want[port.Name]
		if !ok || !port.Required {
			return graphQueryAdapterSubjects{}, fmt.Errorf("unexpected or invalid output port %q", port.Name)
		}
		if _, duplicate := resolved[port.Name]; duplicate {
			return graphQueryAdapterSubjects{}, fmt.Errorf("duplicate output port %q", port.Name)
		}
		facts, err := port.Facts()
		if err != nil {
			return graphQueryAdapterSubjects{}, err
		}
		contract, hasContract := facts.Interface()
		subjects := facts.NATSSubjects()
		if facts.Kind() != component.PortKindNATSRequest || !hasContract || contract.Type != "graph.query" ||
			contract.Version != "v1" || len(subjects) != 1 || subjects[0] != subject {
			return graphQueryAdapterSubjects{}, fmt.Errorf("output %s must be required nats-request graph.query v1 on %s", port.Name, subject)
		}
		resolved[port.Name] = subject
	}
	if !mutationFound || len(resolved) != len(want) {
		return graphQueryAdapterSubjects{}, errors.New("required graph mutation or graph query output is missing")
	}
	return graphQueryAdapterSubjects{
		batch: resolved["batch"], relationships: resolved["relationships"],
		temporal: resolved["temporal"], searchGraph: resolved["searchGraph"],
	}, nil
}

// Initialize is part of the LifecycleComponent contract — no pre-
// Start work.
func (c *Component) Initialize() error { return nil }

// Start opens the AGENT_LOOPS bucket, wires the GraphQueryClient
// adapter, subscribes inputs, reports idle.
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
	if c.gq == nil {
		c.gq = newGraphQueryAdapter(c.deps.NATSClient, c.querySubjects, c.config.ExecuteTimeout, c.logger)
	}
	if err := c.subscribeInputs(ctx); err != nil {
		return err
	}
	c.mu.Lock()
	c.started = true
	c.startTime = time.Now()
	c.mu.Unlock()

	c.logger.Info("execute_subqueries component started",
		slog.String("loops_bucket", c.config.LoopsBucket),
		slog.Duration("execute_timeout", c.config.ExecuteTimeout),
		slog.Int("max_parallelism", c.config.MaxParallelism),
		slog.Int("max_results_per_subquery", c.config.MaxResultsPerSubquery))
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

func (c *Component) subscribeInputs(ctx context.Context) error {
	for _, port := range c.inputs {
		facts, err := port.Facts()
		if err != nil {
			return errs.WrapInvalid(err, ComponentName, "Start", "resolve input facts")
		}
		subject := facts.NATSSubjects()[0]
		sub, err := c.deps.NATSClient.Subscribe(ctx, subject, func(msgCtx context.Context, msg *nats.Msg) {
			c.handleMessage(msgCtx, msg.Subject, msg.Data)
		})
		if err != nil {
			return errs.WrapTransient(err, ComponentName, "Start",
				fmt.Sprintf("subscribe to %s", subject))
		}
		c.subscriptions = append(c.subscriptions, sub)
		c.logger.Debug("subscribed to NATS subject",
			slog.String("port", port.Name),
			slog.String("subject", subject))
	}
	return nil
}

// Stop drains subscriptions and flips started under c.mu.
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
	return nil
}

// handleMessage is the per-message hot path. Loads upstream
// payloads, materializes sub-queries per the routing action,
// executes via the GraphQueryClient, writes ExecutionOutput +
// trigger key.
//
// The ctx passed in comes from the NATS subscription callback
// and is intentionally tied to the subscription lifecycle — Stop's
// Unsubscribe cancels it so an in-flight fan-out drains fast. Same
// shape as research-graph-route's handleMessage (see that file's
// doc comment for the full rationale).
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

	if c.config.ExecuteTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.config.ExecuteTimeout)
		defer cancel()
	}

	intent, classifierOut, decision, loadErr := c.loadUpstreamPayloads(ctx, loopID)
	if loadErr != nil {
		return
	}

	queries, droppedRef, materErr := c.materializeQueries(ctx, loopID, intent, classifierOut, decision)
	if materErr != nil {
		// Already emitted a degraded envelope inside the helper;
		// the materialiseQueries contract is "either return queries
		// or write a degraded trigger + return error".
		return
	}

	output, err := executeAll(ctx, c.gq, queries, intent, decision.Action, c.config.MaxParallelism, c.config.MaxResultsPerSubquery, c.logger)
	if err != nil {
		c.logger.Error("execute_all failed; emitting empty ExecutionOutput",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		c.emitDegradedOutput(ctx, loopID, intent.Topic, decision.Action, "execute_all: "+err.Error())
		return
	}

	// If any seed references were dropped during resolution,
	// surface that on the output's degraded fields (orchestrator
	// already does this for sub-query failures; this adds the
	// resolver-side coverage).
	if droppedRef > 0 {
		output.Degraded = true
		if output.DegradedReason != "" {
			output.DegradedReason += "; "
		}
		output.DegradedReason += fmt.Sprintf("%d seed references unresolved", droppedRef)
	}

	envelope := message.NewBaseMessage(output.Schema(), output, ComponentName)
	envelopeBytes, err := json.Marshal(envelope)
	if err != nil {
		c.logger.Error("marshal execution output envelope failed",
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
	if err := c.loops.PutExecutionOutput(ctx, loopID, envelopeBytes); err != nil {
		c.logger.Error("execute.complete trigger write failed; R3 will not fire",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		atomic.AddInt64(&c.errors, 1)
		return
	}
	c.stampExecuteComplete(ctx, loopID, len(output.Evidence))
	atomic.AddInt64(&c.messagesEmitted, 1)
	c.logger.Info("execute_subqueries emitted ExecutionOutput",
		slog.String("loop_id", loopID),
		slog.String("topic", intent.Topic),
		slog.String("action", decision.Action),
		slog.Int("evidence_count", len(output.Evidence)),
		slog.Int("subquery_count", output.SubQueryCount),
		slog.Bool("degraded", output.Degraded),
		slog.Int("budget_tokens_used", output.BudgetTokensUsed))
}

// stampExecuteComplete stamps research.execute.complete +
// research.execute.evidence_count on the research-pipeline loop entity
// so R3 of the ADR-045 rule chain can dispatch assess_sufficiency.
// Shared between the happy-path emit and the degraded-fallback emit so
// the rule chain advances even when execute degrades.
func (c *Component) stampExecuteComplete(ctx context.Context, loopID string, evidenceCount int) {
	loopEntityID, err := agentic.TryLoopExecutionEntityID(c.deps.Platform.Org, c.deps.Platform.Platform, loopID)
	if err != nil {
		c.logger.Warn("could not construct loop entity ID for triple stamp; R3 will not fire",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		return
	}
	triples := research.BuildExecuteCompleteTriples(loopEntityID, evidenceCount, time.Now())
	_ = llmwrap.StampOrchestrationTriples(ctx, c.triplePub, c.logger, ComponentName, loopID, triples)
}

// loadUpstreamPayloads loads the three KV-resident inputs the
// handler needs (Intent + ClassifierOutput + RouteDecision),
// records error metrics + logs on miss, and returns a non-nil
// error so the caller short-circuits. Extracted from handleMessage
// to keep the hot path under the function-length lint cap.
//
// Recovery contract:
//   - Intent missing/error: hard abort. Without the intent topic
//     we can't even populate a sensible degraded envelope, and
//     intent missing means R0 never wrote it (chain didn't
//     properly kick off).
//   - ClassifierOutput missing: emit degraded envelope so R3 still
//     fires (assess_sufficiency reads "empty evidence, degraded:
//     classifier output missing" and routes to refine OR
//     low-confidence synthesize). Same for RouteDecision missing.
//     The chain is racier during development; emitting degraded
//     keeps loops moving rather than stranding.
//   - Non-sentinel errors (decode failure, KV transport): hard
//     abort. These are infra failures, not chain-config races.
func (c *Component) loadUpstreamPayloads(ctx context.Context, loopID string) (*research.Intent, *research.ClassifierOutput, *research.RouteDecision, error) {
	intent, err := c.loops.GetIntent(ctx, loopID)
	if err != nil {
		c.logger.Error("could not load research intent; ignoring message",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		atomic.AddInt64(&c.errors, 1)
		return nil, nil, nil, err
	}
	classifierOut, err := c.loops.GetClassifierOutput(ctx, loopID)
	if err != nil {
		if errors.Is(err, errClassifierOutputMissing) {
			c.logger.Warn("classifier output missing; emitting degraded envelope so R3 fires",
				slog.String("loop_id", loopID))
			c.emitDegradedOutput(ctx, loopID, intent.Topic, "", "classifier output missing at execute_subqueries dispatch")
			return nil, nil, nil, err
		}
		c.logger.Error("could not load classifier output; ignoring message",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		atomic.AddInt64(&c.errors, 1)
		return nil, nil, nil, err
	}
	decision, err := c.loops.GetRouteDecision(ctx, loopID)
	if err != nil {
		if errors.Is(err, errRouteDecisionMissing) {
			c.logger.Warn("route decision missing; emitting degraded envelope so R3 fires",
				slog.String("loop_id", loopID))
			c.emitDegradedOutput(ctx, loopID, intent.Topic, "", "route decision missing at execute_subqueries dispatch")
			return nil, nil, nil, err
		}
		c.logger.Error("could not load route decision; ignoring message",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		atomic.AddInt64(&c.errors, 1)
		return nil, nil, nil, err
	}
	return intent, classifierOut, decision, nil
}

// materializeQueries dispatches per the routing action and returns
// the materialised sub-queries plus the count of dropped seed
// references. On any per-action validation failure it writes a
// degraded ExecutionOutput envelope (so R3 still fires) and
// returns a non-nil error so the caller short-circuits. Extracted
// from handleMessage to keep the hot path under the function-
// length lint cap.
//
// Only walk_seeds + decompose produce sub-queries — retighten +
// synthesize_directly shouldn't have reached R2's dispatch per
// the rule chain's `when` gating; if they do, treat as a routing-
// config bug, emit degraded, return error.
func (c *Component) materializeQueries(
	ctx context.Context,
	loopID string,
	intent *research.Intent,
	classifierOut *research.ClassifierOutput,
	decision *research.RouteDecision,
) ([]SubQuery, int, error) {
	switch decision.Action {
	case research.ActionWalkSeeds:
		walkArgs, parseErr := decision.ParseWalkSeedsArgs()
		if parseErr != nil {
			c.logger.Error("walk_seeds args invalid; emitting empty ExecutionOutput",
				slog.String("loop_id", loopID),
				slog.Any("error", parseErr))
			c.emitDegradedOutput(ctx, loopID, intent.Topic, decision.Action, "walk_seeds args invalid: "+parseErr.Error())
			return nil, 0, parseErr
		}
		resolved, dropped := resolveSeedRefs(walkArgs.Seeds, classifierOut.Candidates, c.logger)
		return materializeForWalkSeeds(intent, resolved), dropped, nil
	case research.ActionDecompose:
		decArgs, parseErr := decision.ParseDecomposeArgs()
		if parseErr != nil {
			c.logger.Error("decompose args invalid; emitting empty ExecutionOutput",
				slog.String("loop_id", loopID),
				slog.Any("error", parseErr))
			c.emitDegradedOutput(ctx, loopID, intent.Topic, decision.Action, "decompose args invalid: "+parseErr.Error())
			return nil, 0, parseErr
		}
		return materializeForDecompose(intent, decArgs, classifierOut.Candidates), 0, nil
	}
	// retighten / synthesize_directly should never reach R2's
	// dispatch to execute_subqueries.
	c.logger.Error("unexpected routing action reached execute_subqueries; rule-config bug",
		slog.String("loop_id", loopID),
		slog.String("action", decision.Action))
	c.emitDegradedOutput(ctx, loopID, intent.Topic, decision.Action, "unexpected action reached execute_subqueries: "+decision.Action)
	return nil, 0, fmt.Errorf("unexpected action %q", decision.Action)
}

// emitDegradedOutput writes an empty-evidence ExecutionOutput with
// Degraded=true so R3 still fires (assess_sufficiency reads the
// envelope, observes empty evidence, and routes to refine OR
// synthesize-with-low-confidence). Used by the per-action arg
// validation failure paths so a malformed router decision doesn't
// silently strand the loop.
// emitDegradedOutput writes an empty-evidence ExecutionOutput with
// Degraded=true so R3 still fires (assess_sufficiency reads the
// envelope, observes empty evidence, and routes to refine OR
// synthesize-with-low-confidence). Used by per-action arg
// validation failure paths so a malformed router decision doesn't
// silently strand the loop.
//
// Increments c.errors exactly ONCE per call regardless of which
// branch fires (marshal failure / snapshot failure / trigger
// failure / success). Callers that hit a degraded path should NOT
// double-count.
func (c *Component) emitDegradedOutput(ctx context.Context, loopID, topic, action, reason string) {
	atomic.AddInt64(&c.errors, 1)
	output := &research.ExecutionOutput{
		Topic:          topic,
		Action:         action,
		Degraded:       true,
		DegradedReason: reason,
	}
	envelope := message.NewBaseMessage(output.Schema(), output, ComponentName)
	envelopeBytes, err := json.Marshal(envelope)
	if err != nil {
		c.logger.Error("marshal degraded envelope failed",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		return
	}
	if err := c.loops.PutSnapshot(ctx, loopID, envelopeBytes); err != nil {
		c.logger.Warn("degraded snapshot write failed",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
	}
	if err := c.loops.PutExecutionOutput(ctx, loopID, envelopeBytes); err != nil {
		c.logger.Error("degraded execute.complete trigger write failed; R3 will not fire",
			slog.String("loop_id", loopID),
			slog.Any("error", err))
		return
	}
	// Stamp triples on the degraded path too — R3 advances the chain to
	// assess_sufficiency, which is the right next step even with zero
	// evidence (assess will fail-sufficient or refine, both observable).
	c.stampExecuteComplete(ctx, loopID, 0)
}

// Meta implements Discoverable.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        ComponentName,
		Type:        "processor",
		Description: "ADR-045 execute_subqueries: materialise sub-queries from RouteDecision intent + fan-out across Tier 0+1 retrieval + dedup + rank + budget. Emits ExecutionOutput for R3.",
		Version:     "0.1.0",
	}
}

// InputPorts implements Discoverable.
func (c *Component) InputPorts() []component.Port {
	return append([]component.Port(nil), c.inputs...)
}

// OutputPorts implements Discoverable.
func (c *Component) OutputPorts() []component.Port {
	return append([]component.Port(nil), c.outputs...)
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
