// Package agenticloop — subject-mode tool-call governance dispatcher (ADR-039).
//
// The dispatcher sits between the model's tool-call response and the
// loop's serial-dispatch fan-out. It implements three modes per
// ToolCallGovernanceConfig.Mode:
//
//   - disabled: pass-through. All calls return as approved without
//     publishing. No governance gate active. (Beta.70 retired the
//     beta.67+68 in-process ToolCallFilter wiring; subject-mode is
//     now the sole governance path.)
//   - audit: publishes each proposed call to agent.toolcall.proposed.*
//     but returns all calls as approved IMMEDIATELY. Verdicts that
//     arrive later are recorded for observability only. Shadow mode
//     for rule development.
//   - enforce: subscribes-then-publishes per ADR-039 race-fix option
//     3 (per-call waiter registration BEFORE publish), waits up to
//     ToolCallGovernanceConfig.Timeout for a per-call verdict, and
//     returns approved/rejected sets. Fail-closed on timeout.
//
// # Race-condition fix: subscribe before publish
//
// JetStream consumer binding is async with publish. If the rule
// processor fires faster than the loop's verdict subscription binds,
// the verdict can be published to a subject with no subscribers, the
// loop times out, the call fails — same class as the natsclient
// handler-error payload convention bug.
//
// Fix: the Component sets up a SINGLE wildcard subscription to
// `agent.toolcall.approved.>` and `agent.toolcall.rejected.>` at Start.
// The dispatcher REGISTERS per-call waiter channels in-process BEFORE
// publishing the proposed call. The wildcard subscription is bound
// before any task arrives, and the in-process waiter registration is
// synchronous with the publish call. Verdicts that arrive demux by
// call_id (trailing path segment) into the registered waiter channel.
//
// # Approved subject shape (ADR-039 open question 1)
//
// Implementations consume verdict subjects of the shape
// `agent.toolcall.{approved,rejected}.<loop_id>.<call_id>`. The call_id
// is the trailing path segment. Operators templating verdict subjects
// in their rule actions are expected to use
// `$message.loop_id.$message.call_id`; ADR-039 §"Implementation" lists
// this contract.
package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
)

// VerdictPublisher is the minimum surface the governance dispatcher
// needs to publish proposed-call payloads. *natsclient.Client satisfies
// it via PublishToStream. Kept as an interface here so tests can stub
// the publish path without a real NATS connection.
type VerdictPublisher interface {
	PublishToStream(ctx context.Context, subject string, data []byte) error
}

// DispatcherMetrics is the optional metrics-sink interface the
// dispatcher calls into for observability. The Component's
// *loopMetrics satisfies it; tests pass nil.
//
// Kept narrow so the dispatcher stays free of Prometheus types and the
// metrics layer can evolve independently. Implementations should be
// non-blocking — the dispatcher is on the loop's hot path.
type DispatcherMetrics interface {
	// RecordGovernanceVerdict observes the wait duration and counts the
	// verdict. decision is "approved" | "rejected" | "timeout"; mode is
	// "audit" | "enforce".
	RecordGovernanceVerdict(decision, mode string, duration float64)

	// RecordGovernanceVerdictMissingWaiter signals that a verdict
	// arrived for a call_id without a registered waiter — points at
	// the subscribe-before-publish race regressing or a late arrival.
	RecordGovernanceVerdictMissingWaiter()
}

// ToolCallRejection records why a tool call was denied by the
// governance dispatcher. The downstream serial-dispatch path consumes
// this to write per-call error results for the model to react to on
// its next iteration.
type ToolCallRejection struct {
	Call   agentic.ToolCall
	Reason string
}

// DispatcherResult is the verdict for a batch of proposed calls. Empty
// Rejected with full Approved is the disabled-mode/audit-mode steady
// state. Partial splits are produced by enforce mode.
type DispatcherResult struct {
	Approved []agentic.ToolCall
	Rejected []ToolCallRejection
}

// ProposedToolCallPayload is the wire shape published to
// agent.toolcall.proposed.<loop_id> in audit and enforce modes. Rules
// match on these field paths via `$message.<field>` substitution.
//
// ParentLoopID is nullable from day one (ADR-039) so rule conditions
// can match across loop hierarchies for sub-agent governance
// inheritance. Empty string means no parent.
type ProposedToolCallPayload struct {
	LoopID       string         `json:"loop_id"`
	ParentLoopID string         `json:"parent_loop_id,omitempty"`
	CallID       string         `json:"call_id"`
	ToolName     string         `json:"tool_name"`
	Command      string         `json:"command,omitempty"`
	URL          string         `json:"url,omitempty"`
	Arguments    map[string]any `json:"arguments,omitempty"`
}

// VerdictPayload is the wire shape inbound on
// agent.toolcall.{approved,rejected}.<loop_id>.<call_id>. Produced by
// the rule engine's executeApprove action (top-level fields) OR by a
// rule `publish` action emitting a rejection (fields nested under
// Properties — the canonical ADR-039 reject pattern).
//
// Both shapes are supported because the ADR's example rule uses
// `publish` + `deny` together for rejections (no extra `reject`
// action). EffectiveCallID and EffectiveDecision fall through the two
// shapes so callers don't write the same parsing code twice.
type VerdictPayload struct {
	Decision   string         `json:"decision,omitempty"`
	CallID     string         `json:"call_id,omitempty"`
	LoopID     string         `json:"loop_id,omitempty"`
	RuleID     string         `json:"rule_id,omitempty"`
	Reason     string         `json:"reason,omitempty"`
	EntityID   string         `json:"entity_id,omitempty"`
	Timestamp  string         `json:"timestamp,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
}

// EffectiveCallID returns the routing call_id from the payload,
// preferring the top-level CallID (approve-action shape) and falling
// back to Properties["call_id"] (publish-action shape).
func (v VerdictPayload) EffectiveCallID() string {
	if v.CallID != "" {
		return v.CallID
	}
	if v.Properties != nil {
		if cid, ok := v.Properties["call_id"].(string); ok {
			return cid
		}
	}
	return ""
}

// EffectiveDecision returns the routing decision from the payload,
// preferring the top-level Decision (approve-action shape) and falling
// back to Properties["decision"] (publish-action shape).
func (v VerdictPayload) EffectiveDecision() string {
	if v.Decision != "" {
		return v.Decision
	}
	if v.Properties != nil {
		if d, ok := v.Properties["decision"].(string); ok {
			return d
		}
	}
	return ""
}

// EffectiveReason returns the human-readable reason, with the same
// fall-through semantics as EffectiveCallID.
func (v VerdictPayload) EffectiveReason() string {
	if v.Reason != "" {
		return v.Reason
	}
	if v.Properties != nil {
		if r, ok := v.Properties["reason"].(string); ok {
			return r
		}
	}
	return ""
}

// GovernanceDispatcher abstracts the subject-mode tool-call governance
// flow. The Component holds one instance per loop component and
// delegates from handleToolCallResponse before the existing serial-
// dispatch. Verdict arrival from the JetStream subscription is fed in
// via HandleVerdict.
type GovernanceDispatcher interface {
	// Propose submits a batch of tool calls for governance review.
	// Returns the approved/rejected split. Disabled mode passes
	// through; audit mode publishes-but-passes-through; enforce mode
	// waits up to the configured timeout per call and fails-closed on
	// timeout (treats absent verdict as a reject).
	//
	// loopID is the originating loop's bare UUID. parentLoopID is the
	// spawn-tree parent (empty for top-level loops). Both ride along
	// in the proposed payload so rule conditions can match across the
	// hierarchy from day one.
	Propose(ctx context.Context, loopID, parentLoopID string, calls []agentic.ToolCall) (DispatcherResult, error)

	// HandleVerdict feeds an inbound verdict to the dispatcher. The
	// Component's wildcard subscription handlers call this for every
	// verdict, after extracting the decision and call_id from the
	// payload via VerdictPayload.EffectiveCallID/EffectiveDecision.
	// The dispatcher demuxes by call_id to the appropriate waiter
	// channel. No-op when no waiter is registered for the call_id
	// (audit mode, late arrivals, verdicts for other components'
	// loops on a shared stream). Data is retained for audit logging
	// only — routing decisions are made from decision + callID.
	HandleVerdict(decision, callID string, data []byte) (natsclient.DeliveryDecision, error)

	// Mode returns the configured mode for inspection. Useful for
	// observability gauges and conditional logging in the handler.
	Mode() string
}

// NewGovernanceDispatcher constructs the dispatcher matching the
// configured mode. Publisher may be nil in disabled mode (no publishes
// happen). Metrics may be nil — observability records are skipped when
// unset. The returned dispatcher's HandleVerdict is safe to call from
// JetStream consumer callbacks.
//
// The dispatcher does NOT subscribe — the Component owns the wildcard
// JetStream subscription and routes verdicts through HandleVerdict.
// That keeps the consumer lifecycle (create / bind / cleanup-on-stop)
// in one place and lets the dispatcher stay free of NATS dependencies.
func NewGovernanceDispatcher(cfg ToolCallGovernanceConfig, publisher VerdictPublisher, logger *slog.Logger, metrics DispatcherMetrics) GovernanceDispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	switch cfg.Mode {
	case ToolCallGovernanceModeAudit:
		return &auditDispatcher{
			publisher: publisher,
			logger:    logger,
			metrics:   metrics,
		}
	case ToolCallGovernanceModeEnforce:
		return &enforceDispatcher{
			publisher: publisher,
			logger:    logger,
			metrics:   metrics,
			timeout:   cfg.ParsedTimeout(),
			waiters:   map[string]chan verdictArrival{},
		}
	default:
		// disabled (and any unknown value — Validate is the safety net)
		return &disabledDispatcher{logger: logger}
	}
}

// --- disabled mode ---------------------------------------------------

type disabledDispatcher struct {
	logger *slog.Logger
}

func (d *disabledDispatcher) Propose(_ context.Context, _, _ string, calls []agentic.ToolCall) (DispatcherResult, error) {
	return DispatcherResult{Approved: calls}, nil
}

func (d *disabledDispatcher) HandleVerdict(decision, callID string, _ []byte) (natsclient.DeliveryDecision, error) {
	// No-op: in disabled mode the loop didn't publish a proposed call,
	// so any verdict arriving is from another flow (or a misconfigured
	// rule). Log at Debug only — not actionable.
	d.logger.Debug("Verdict received in disabled-mode governance dispatcher; ignoring",
		slog.String("decision", decision),
		slog.String("call_id", callID))
	return natsclient.DeliveryDecisionAck, nil
}

func (d *disabledDispatcher) Mode() string { return ToolCallGovernanceModeDisabled }

// --- audit mode -----------------------------------------------------

type auditDispatcher struct {
	publisher VerdictPublisher
	logger    *slog.Logger
	metrics   DispatcherMetrics
}

func (d *auditDispatcher) Propose(ctx context.Context, loopID, parentLoopID string, calls []agentic.ToolCall) (DispatcherResult, error) {
	// Audit mode publishes every proposed call but does NOT block on
	// verdicts. Publish failure is logged at Warn — audit is
	// best-effort observability, not a gate. Returning an error here
	// would fail tool calls operators explicitly chose NOT to gate.
	for _, call := range calls {
		if err := publishProposed(ctx, d.publisher, loopID, parentLoopID, call, d.logger); err != nil {
			d.logger.Warn("Audit-mode publish failed; dispatch continues",
				slog.String("loop_id", loopID),
				slog.String("call_id", call.ID),
				slog.String("tool_name", call.Name),
				slog.Any("error", err))
		}
	}
	return DispatcherResult{Approved: calls}, nil
}

func (d *auditDispatcher) HandleVerdict(decision, callID string, data []byte) (natsclient.DeliveryDecision, error) {
	// Audit mode logs verdicts for visibility but takes no action.
	// Operators developing rules see their verdicts firing without
	// gating real traffic.
	var payload VerdictPayload
	_ = json.Unmarshal(data, &payload)
	d.logger.Info("Audit-mode verdict observed",
		slog.String("decision", decision),
		slog.String("call_id", callID),
		slog.String("rule_id", payload.RuleID),
		slog.String("reason", payload.EffectiveReason()))
	if d.metrics != nil {
		// Audit mode can't measure latency (no per-call start time
		// kept) — count the verdict with zero duration. Duration
		// histogram is still useful for verdict-arrival rate visualisation.
		d.metrics.RecordGovernanceVerdict(decision, ToolCallGovernanceModeAudit, 0)
	}
	return natsclient.DeliveryDecisionAck, nil
}

func (d *auditDispatcher) Mode() string { return ToolCallGovernanceModeAudit }

// --- enforce mode ---------------------------------------------------

// verdictArrival is the message passed from HandleVerdict (running on
// the JetStream consumer callback) to a Propose call waiting on its
// waiter channel.
type verdictArrival struct {
	decision string
	reason   string
	ruleID   string
}

type enforceDispatcher struct {
	publisher VerdictPublisher
	logger    *slog.Logger
	metrics   DispatcherMetrics
	timeout   time.Duration

	mu      sync.Mutex
	waiters map[string]chan verdictArrival
}

// registerWaiter installs a per-call waiter channel BEFORE publish.
// Buffered with capacity 1 so an early-arriving verdict (after publish
// returns but before Propose enters the select) doesn't block the
// HandleVerdict goroutine. The caller is responsible for releasing the
// waiter via releaseWaiter exactly once (defer at the call site).
func (d *enforceDispatcher) registerWaiter(callID string) chan verdictArrival {
	ch := make(chan verdictArrival, 1)
	d.mu.Lock()
	d.waiters[callID] = ch
	d.mu.Unlock()
	return ch
}

func (d *enforceDispatcher) releaseWaiter(callID string) {
	d.mu.Lock()
	delete(d.waiters, callID)
	d.mu.Unlock()
}

func (d *enforceDispatcher) lookupWaiter(callID string) (chan verdictArrival, bool) {
	d.mu.Lock()
	ch, ok := d.waiters[callID]
	d.mu.Unlock()
	return ch, ok
}

func (d *enforceDispatcher) Propose(ctx context.Context, loopID, parentLoopID string, calls []agentic.ToolCall) (DispatcherResult, error) {
	// Pre-register every waiter BEFORE the first publish. Closes the
	// subscribe-before-publish race window: even if a verdict arrives
	// between publish-return and the per-call select below, the
	// HandleVerdict path will find the waiter and deliver into the
	// buffered channel.
	channels := make(map[string]chan verdictArrival, len(calls))
	for _, call := range calls {
		channels[call.ID] = d.registerWaiter(call.ID)
	}
	defer func() {
		for _, call := range calls {
			d.releaseWaiter(call.ID)
		}
	}()

	// Publish all proposed calls. Publish failure on any call is
	// fail-closed: treat that call as rejected with the publish error
	// as the reason. The rest of the batch continues — operators get
	// per-call observability rather than a batch-level all-or-nothing.
	publishFailures := map[string]error{}
	for _, call := range calls {
		if err := publishProposed(ctx, d.publisher, loopID, parentLoopID, call, d.logger); err != nil {
			publishFailures[call.ID] = err
		}
	}

	// Per-call wait. Approved/rejected sets are accumulated in order
	// so the downstream serial dispatcher sees calls in the same
	// order the model emitted them.
	var (
		approved []agentic.ToolCall
		rejected []ToolCallRejection
	)
	for _, call := range calls {
		if pubErr, failed := publishFailures[call.ID]; failed {
			rejected = append(rejected, ToolCallRejection{
				Call:   call,
				Reason: fmt.Sprintf("governance publish failed: %v", pubErr),
			})
			continue
		}

		waitStart := time.Now()
		decision, reason := d.awaitVerdict(ctx, channels[call.ID], call, loopID)
		waitDuration := time.Since(waitStart).Seconds()
		if d.metrics != nil {
			// Record under the verdict's canonical decision label —
			// "timeout" is its own label distinct from "rejected".
			d.metrics.RecordGovernanceVerdict(decision, ToolCallGovernanceModeEnforce, waitDuration)
		}

		switch decision {
		case "approved":
			approved = append(approved, call)
		case "rejected":
			rejected = append(rejected, ToolCallRejection{Call: call, Reason: reason})
		default:
			// timeout or unknown — fail-closed
			rejected = append(rejected, ToolCallRejection{Call: call, Reason: reason})
		}
	}
	return DispatcherResult{Approved: approved, Rejected: rejected}, nil
}

// awaitVerdict blocks until a verdict arrives, the context is cancelled,
// or the configured timeout elapses. Returns the decision string
// ("approved" | "rejected" | "timeout") and a human-readable reason.
func (d *enforceDispatcher) awaitVerdict(ctx context.Context, ch chan verdictArrival, call agentic.ToolCall, loopID string) (string, string) {
	// Use a fresh timer per call. time.After leaks the underlying
	// timer until the channel fires; NewTimer + Stop is the idiomatic
	// per-iteration pattern.
	timer := time.NewTimer(d.timeout)
	defer timer.Stop()

	select {
	case v := <-ch:
		if v.decision == "" {
			// Shouldn't happen — HandleVerdict only sends with a known
			// decision. Defensive: treat as fail-closed.
			d.logger.Warn("Enforce-mode verdict missing decision; rejecting fail-closed",
				slog.String("loop_id", loopID),
				slog.String("call_id", call.ID))
			return "rejected", "governance verdict missing decision (fail-closed)"
		}
		return v.decision, fmtVerdictReason(v)
	case <-timer.C:
		d.logger.Warn("Enforce-mode verdict timeout; rejecting fail-closed",
			slog.String("loop_id", loopID),
			slog.String("call_id", call.ID),
			slog.String("tool_name", call.Name),
			slog.Duration("timeout", d.timeout))
		return "timeout", fmt.Sprintf("governance verdict timeout after %s (fail-closed)", d.timeout)
	case <-ctx.Done():
		return "timeout", fmt.Sprintf("governance wait cancelled: %v", ctx.Err())
	}
}

func (d *enforceDispatcher) HandleVerdict(decision, callID string, data []byte) (natsclient.DeliveryDecision, error) {
	if decision == "" || callID == "" {
		d.logger.Warn("Verdict missing decision or call_id; ignoring",
			slog.String("decision", decision),
			slog.String("call_id", callID))
		return natsclient.DeliveryDecisionTerminate, errors.New("verdict missing decision or call_id")
	}

	// Payload is optional audit context — routing relies on the
	// (decision, callID) pair supplied by the caller. An unmarshal
	// failure here downgrades the verdict reason but doesn't block
	// dispatch.
	var payload VerdictPayload
	_ = json.Unmarshal(data, &payload)

	ch, ok := d.lookupWaiter(callID)
	if !ok {
		// Either late arrival (Propose already returned via timeout)
		// or a verdict for a different loop's call. Common in healthy
		// operation when retries happen; log at Debug.
		d.logger.Debug("Verdict arrived after waiter released; ignoring",
			slog.String("call_id", callID),
			slog.String("decision", decision))
		if d.metrics != nil {
			d.metrics.RecordGovernanceVerdictMissingWaiter()
		}
		return natsclient.DeliveryDecisionRetry, fmt.Errorf("no active governance waiter for call_id %q", callID)
	}

	// Non-blocking send via the buffered channel. If somehow a second
	// verdict arrives for the same call_id (operator misconfigured a
	// rule into a duplicate publish), the second is dropped — the
	// first wins. Same as the natsclient request/response convention.
	select {
	case ch <- verdictArrival{decision: decision, reason: payload.EffectiveReason(), ruleID: payload.RuleID}:
		return natsclient.DeliveryDecisionAck, nil
	default:
		d.logger.Debug("Verdict channel already full (duplicate verdict?); dropping",
			slog.String("call_id", callID))
		return natsclient.DeliveryDecisionQuarantine, fmt.Errorf("governance waiter for call_id %q is full", callID)
	}
}

func (d *enforceDispatcher) Mode() string { return ToolCallGovernanceModeEnforce }

// --- shared helpers -------------------------------------------------

// publishProposed serialises a single tool call as a
// ProposedToolCallPayload and publishes to
// agent.toolcall.proposed.<loop_id>. The subject shape matches the
// ADR-039 commitment: rules subscribe to `agent.toolcall.proposed.*`
// and match against the payload via $message.* substitution.
//
// Returns an error from marshal/publish — the caller decides whether
// the error is fatal (enforce mode treats it as a fail-closed reject)
// or best-effort (audit mode logs and proceeds).
func publishProposed(ctx context.Context, publisher VerdictPublisher, loopID, parentLoopID string, call agentic.ToolCall, logger *slog.Logger) error {
	if publisher == nil {
		// No publisher: dev/test scaffold without a NATS connection.
		// Log at Debug and treat as a no-op — disabled mode never
		// reaches here; audit mode is best-effort; enforce mode will
		// time out because no verdict can arrive.
		if logger != nil {
			logger.Debug("Skipping proposed-call publish (no publisher configured)",
				slog.String("loop_id", loopID),
				slog.String("call_id", call.ID))
		}
		return nil
	}

	payload := ProposedToolCallPayload{
		LoopID:       loopID,
		ParentLoopID: parentLoopID,
		CallID:       call.ID,
		ToolName:     call.Name,
		Arguments:    coerceArguments(call.Arguments),
	}
	// Lift bash/http top-level conveniences out of the arguments map
	// when present. Rule authors typically reach for $message.command /
	// $message.url rather than $message.arguments.command — pre-flatten
	// the two highest-value common fields so the rule DSL stays
	// readable. The full Arguments map remains available for deep
	// matching.
	if cmd, ok := payload.Arguments["command"].(string); ok {
		payload.Command = cmd
	}
	if url, ok := payload.Arguments["url"].(string); ok {
		payload.URL = url
	}

	// Wrap in a `core.json.v1` BaseMessage envelope so the rule
	// processor's decoder can parse this off the wire. The rule
	// processor requires wireFormat-shaped messages (id/type/meta/
	// payload); a raw `json.Marshal(ProposedToolCallPayload)` would
	// fail decode and the rules would never see proposed calls. The
	// `Data` map round-trips through GenericJSONPayload so
	// `$message.<field>` substitution reads the proposed-call fields
	// directly via `extractMessageData` in
	// processor/rule/message_handler.go.
	data, err := payloadToBaseMessageBytes(payload, "agentic-loop")
	if err != nil {
		return fmt.Errorf("marshal proposed-call payload: %w", err)
	}

	subject := "agent.toolcall.proposed." + loopID
	if err := publisher.PublishToStream(ctx, subject, data); err != nil {
		return fmt.Errorf("publish proposed to %s: %w", subject, err)
	}
	return nil
}

// payloadToBaseMessageBytes converts a ProposedToolCallPayload into a
// wire-format BaseMessage envelope wrapping a `core.json.v1`
// GenericJSONPayload. Returns the marshaled bytes ready to publish.
//
// The rule processor decodes via `message.NewDecoder(reg).Decode(data)`
// which expects a wireFormat envelope; raw struct JSON fails that
// decode path. GenericJSONPayload is the existing well-known type for
// arbitrary JSON data; rule conditions and `$message.*` substitution
// already consume `GenericJSONPayload.Data`.
//
// Round-trips the proposed payload via JSON to land in
// `map[string]any` because GenericJSONPayload.Data is map-typed. The
// allocation cost is acceptable on the per-tool-call hot path
// (proposed-call publish is once per tool call; not once per
// iteration).
func payloadToBaseMessageBytes(payload ProposedToolCallPayload, source string) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal proposed-call to map: %w", err)
	}
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err != nil {
		return nil, fmt.Errorf("re-decode proposed-call to map: %w", err)
	}
	generic := message.NewGenericJSON(asMap)
	baseMsg := message.NewBaseMessage(generic.Schema(), generic, source)
	return json.Marshal(baseMsg)
}

// coerceArguments normalises ToolCall.Arguments to a map[string]any so
// the proposed payload's Arguments field has consistent JSON shape.
// agentic.ToolCall.Arguments is already map[string]any in current
// builds — this is defensive against future shape changes.
func coerceArguments(args map[string]any) map[string]any {
	if args == nil {
		return nil
	}
	out := make(map[string]any, len(args))
	maps.Copy(out, args)
	return out
}

// decisionFromVerdictSubject parses the third segment of a verdict
// subject (`agent.toolcall.{approved,rejected}.…`) into the decision
// label. Returns "" when the subject doesn't begin with
// `agent.toolcall.<approved|rejected>.` so callers can fall back to
// payload-based extraction.
//
// The Component routes BOTH wildcard ports (approved / rejected) to a
// shared handler; this helper plus VerdictPayload.EffectiveDecision
// together cover both authorship paths (executeApprove writes the
// decision field; executePublish leaves it in Properties).
func decisionFromVerdictSubject(subject string) string {
	parts := strings.SplitN(subject, ".", 4)
	if len(parts) < 3 {
		return ""
	}
	if parts[0] != "agent" || parts[1] != "toolcall" {
		return ""
	}
	switch parts[2] {
	case "approved":
		return "approved"
	case "rejected":
		return "rejected"
	}
	return ""
}

// fmtVerdictReason formats the human-readable reason for an arrived
// verdict, falling back to a default when the rule didn't supply one.
func fmtVerdictReason(v verdictArrival) string {
	switch {
	case v.reason != "" && v.ruleID != "":
		return fmt.Sprintf("%s (rule_id=%s)", v.reason, v.ruleID)
	case v.reason != "":
		return v.reason
	case v.ruleID != "":
		return fmt.Sprintf("rule_id=%s", v.ruleID)
	default:
		return "governance verdict (no reason)"
	}
}

// ErrGovernancePublishFailed is returned by Propose only when the
// dispatcher is unable to make any forward progress at all — currently
// unused (per-call publish failures are recorded as per-call
// rejections). Reserved for a future architectural failure mode that
// short-circuits the entire batch.
var ErrGovernancePublishFailed = errors.New("governance publish failed")
