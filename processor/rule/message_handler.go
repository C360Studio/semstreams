package rule

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	entitytypes "github.com/c360studio/semstreams/pkg/types"
)

// snapshotRules copies rp.rules, rp.ruleDefinitions, and rp.matchCounters
// under a single RLock and returns the three snapshots. Callers iterate the
// snapshots without holding the lock, so hot-reload writes do not race.
func (rp *Processor) snapshotRules() (map[string]Rule, map[string]Definition, map[string]*atomic.Int64) {
	rp.mu.RLock()
	rules := make(map[string]Rule, len(rp.rules))
	for k, v := range rp.rules {
		rules[k] = v
	}
	defs := make(map[string]Definition, len(rp.ruleDefinitions))
	for k, v := range rp.ruleDefinitions {
		defs[k] = v
	}
	counters := make(map[string]*atomic.Int64, len(rp.matchCounters))
	for k, v := range rp.matchCounters { // safe: range over nil map is a no-op
		counters[k] = v
	}
	rp.mu.RUnlock()
	return rules, defs, counters
}

// shouldFireAction returns true when the rule's FireEveryNEvents gate allows
// an action to fire for this match. It increments the match counter atomically
// and evaluates the modulo. N=0 and N=1 always return true (fire every match).
// The counter pointer must not be nil; callers guarantee this via snapshotRules.
func shouldFireAction(n int, counter *atomic.Int64) bool {
	if n <= 1 {
		return true
	}
	count := counter.Add(1)
	return count%int64(n) == 0
}

// reportEvaluating reports the evaluating stage (throttled to avoid KV spam)
func (rp *Processor) reportEvaluating(ctx context.Context) {
	if rp.lifecycleReporter != nil {
		if err := rp.lifecycleReporter.ReportStage(ctx, "evaluating"); err != nil {
			rp.logger.Debug("failed to report lifecycle stage", slog.String("stage", "evaluating"), slog.Any("error", err))
		}
	}
}

// handleMessage processes incoming NATS messages with dual-format support
func (rp *Processor) handleMessage(ctx context.Context, subject string, data []byte) {
	// Report evaluating stage for lifecycle observability
	rp.reportEvaluating(ctx)

	// Update metrics for received messages
	if rp.metrics != nil {
		rp.metrics.messagesReceived.WithLabelValues(subject).Inc()
	}

	rp.logger.Debug("Received message", "subject", subject)

	rp.mu.Lock()
	rp.lastActivity = time.Now()
	rp.mu.Unlock()

	// All messages are now semantic messages since entity events come via KV watch
	rp.handleSemanticMessage(ctx, subject, data)
}

// handleSemanticMessage processes semantic messages (BaseMessage format)
func (rp *Processor) handleSemanticMessage(ctx context.Context, subject string, data []byte) {
	baseMsg, err := rp.decoder.Decode(data)
	if err != nil {
		rp.recordError(fmt.Sprintf("failed to unmarshal BaseMessage: %v", err))
		return
	}

	rp.logger.Debug("Successfully unmarshaled BaseMessage", "type", baseMsg.Type().String())

	// Process through rules
	rp.evaluateRulesForMessage(ctx, subject, baseMsg)
}

// evaluateRulesForMessage performs rule evaluation for any message type
func (rp *Processor) evaluateRulesForMessage(ctx context.Context, subject string, msg message.Message) {
	if !rp.graphRuleEvaluationReady() {
		return
	}
	// Increment evaluation counter for all messages (NATS and KV watcher)
	atomic.AddInt64(&rp.messagesEvaluated, 1)

	// Cache the message if needed
	if rp.messageCache != nil {
		cacheKey := fmt.Sprintf("%s_%d", subject, time.Now().UnixNano())
		rp.messageCache.Set(cacheKey, msg)
	}

	// Snapshot all three maps so hot-reload writes don't race with iteration.
	rules, ruleDefs, counters := rp.snapshotRules()

	// Process through each rule
	for ruleName, ruleInstance := range rules {
		ruleDef, hasDefinition := ruleDefs[ruleName]
		if hasDefinition && ruleDef.Entity.Pattern != "" {
			continue
		}

		// Check if rule is interested in this subject
		if !rp.matchesRuleSubject(ruleInstance, subject) {
			continue
		}

		// TODO: Time-window buffering removed - use pkg/buffer if needed for aggregation rules
		// For now, evaluate rules on single messages
		messages := []message.Message{msg}

		// Evaluate rule with metrics timing
		rp.logger.Debug("Evaluating rule", "rule_name", ruleName)
		start := time.Now()
		triggered := ruleInstance.Evaluate(messages)
		evaluationDuration := time.Since(start)

		// Update metrics
		if rp.metrics != nil {
			rp.metrics.evaluationDuration.WithLabelValues(ruleName).Observe(evaluationDuration.Seconds())
			if triggered {
				rp.metrics.evaluationsTotal.WithLabelValues(ruleName, "triggered").Inc()
				// For severity, we'll use a default "info" since we don't have severity in the interface
				rp.metrics.triggersTotal.WithLabelValues(ruleName, "info").Inc()
			} else {
				rp.metrics.evaluationsTotal.WithLabelValues(ruleName, "not_triggered").Inc()
			}
		}

		// Get rule definition for stateful evaluation from the local snapshot.
		hasStatefulActions := hasDefinition && hasStatefulRuleActions(ruleDef)

		// Handle stateful evaluation if rule has OnEnter/OnExit/WhileTrue actions
		if hasStatefulActions && rp.statefulEvaluator != nil {
			// Extract entity ID from message payload for state tracking
			entityID := extractEntityID(msg)

			// Perform stateful evaluation. Message-path rules have no KV
			// revision and no bootstrap phase, so those fields stay zero.
			// MessageData carries the inbound payload for $message.*
			// substitution in action templates (ADR-039).
			transition, err := rp.statefulEvaluator.Evaluate(ctx, Evaluation{
				Rule:              ruleDef,
				EntityID:          entityID,
				CurrentlyMatching: triggered,
				MessageData:       extractMessageData(msg),
			})
			if err != nil {
				rp.logger.Warn("Stateful evaluation failed", "rule_name", ruleName, "error", err)
			} else if transition != TransitionNone {
				rp.logger.Debug("Rule state transition",
					"rule_name", ruleName,
					"transition", transition,
					"entity_id", entityID)

				// Update state transition metrics
				if rp.metrics != nil {
					rp.metrics.stateTransitionsTotal.WithLabelValues(ruleName, string(transition)).Inc()
				}
			}
		}

		if triggered {
			rp.fireRuleActions(ctx, ruleName, hasDefinition, ruleDef, counters, ruleInstance, messages)
		} else {
			rp.logger.Debug("Rule did not trigger", "rule_name", ruleName)
		}
	}
}

// fireRuleActions applies the FireEveryNEvents gate and, if the gate passes,
// executes events and publishes them. It is the shared "action" leg for both
// NATS-message and entity-state evaluation paths.
func (rp *Processor) fireRuleActions(
	ctx context.Context,
	ruleName string,
	hasDefinition bool,
	ruleDef Definition,
	counters map[string]*atomic.Int64,
	ruleInstance Rule,
	messages []message.Message,
) {
	if !rp.graphRuleEvaluationReady() {
		return
	}
	// Apply FireEveryNEvents gate: match counted; action fires only on Nth match.
	counter := counters[ruleName]
	n := 0
	if hasDefinition {
		n = ruleDef.FireEveryNEvents
	}
	if counter == nil || !shouldFireAction(n, counter) {
		rp.logger.Debug("Rule matched but action gated by fire_every_n_events",
			"rule_name", ruleName, "fire_every_n_events", n)
		return
	}

	rp.logger.Debug("Rule triggered", "rule_name", ruleName)

	// Execute rule events
	events, err := ruleInstance.ExecuteEvents(messages)
	if err != nil {
		rp.recordError(fmt.Sprintf("rule %s execution failed: %v", ruleName, err))
		return
	}

	// Publish rule event notification
	if err := rp.publishRuleEvent(ctx, ruleName, "triggered"); err != nil {
		rp.logger.Warn("Failed to publish rule event", "error", err)
	}

	// Publish graph events
	if err := rp.publishGraphEvents(ctx, events); err != nil {
		rp.recordError(fmt.Sprintf("failed to publish events from rule %s: %v", ruleName, err))
	} else {
		atomic.AddInt64(&rp.rulesTriggered, 1)
	}
}

// evaluateRulesForEntityState performs rule evaluation directly against
// EntityState triples, bypassing the message transformation layer.
//
// snap carries the entity state, the CRUD action label, and the KV revision
// that triggered this evaluation. bootstrap is true while the watcher is
// replaying initial state on startup — stateful rules use this to re-fire
// OnEnter/OnRecovery for entities that were matching before a restart.
//
// When snap.Revision is non-zero, each rule is individually checked against
// the per-rule feedback tracker: a rule's own write is skipped only by that
// rule, so sibling rules watching the same bucket still fire.
func (rp *Processor) evaluateRulesForEntityState(ctx context.Context, entityKey string, snap entitySnapshot, bootstrap bool) {
	// Re-check at the final evaluation seam. Watch and coalescer callers gate
	// earlier too, but a concurrent poison observation can race an already
	// dispatched evaluation. No rule, metric, state transition, or action may
	// derive from graph state after reset-required has latched.
	if !rp.graphRuleEvaluationReady() {
		return
	}
	atomic.AddInt64(&rp.messagesEvaluated, 1)

	// Snapshot all three maps so hot-reload writes don't race with iteration.
	rules, ruleDefs, counters := rp.snapshotRules()
	entityID := entityKey
	if snap.State != nil {
		entityID = snap.State.ID
	}

	for ruleName, ruleInstance := range rules {
		ruleDef, hasDefinition := ruleDefs[ruleName]
		if !hasDefinition || ruleDef.Entity.Pattern == "" {
			continue
		}
		matches, err := entitytypes.MatchEntityIDPattern(ruleDef.Entity.Pattern, entityID)
		if err != nil {
			rp.logger.Error("Validated rule entity pattern failed matching",
				"rule_name", ruleName,
				"entity_id", entityID,
				"error", err)
			continue
		}
		if !matches {
			continue
		}

		// Per-rule feedback loop prevention: if this rule generated the
		// revision that the watcher just delivered, skip the rule (the
		// revision is consumed one-time so subsequent non-self writes still
		// fire it). Other rules continue to evaluate.
		if snap.Revision > 0 && rp.shouldSkipRule(ruleName, entityID, snap.Revision) {
			rp.logger.Debug("Skipping self-generated update for rule",
				"rule_name", ruleName,
				"entity_id", entityID,
				"revision", snap.Revision)
			continue
		}

		rp.logger.Debug("Evaluating rule against EntityState",
			"rule_name", ruleName,
			"entity_id", entityID,
			"action", snap.Action,
			"bootstrap", bootstrap)

		start := time.Now()
		var triggered bool

		if snap.State != nil {
			entityEval, ok := ruleInstance.(EntityStateEvaluator)
			if !ok {
				rp.logger.Debug("Rule doesn't support EntityState evaluation, skipping",
					"rule_name", ruleName)
				continue
			}
			triggered = entityEval.EvaluateEntityState(snap.State)
		}

		evaluationDuration := time.Since(start)

		if rp.metrics != nil {
			rp.metrics.evaluationDuration.WithLabelValues(ruleName).Observe(evaluationDuration.Seconds())
			if triggered {
				rp.metrics.evaluationsTotal.WithLabelValues(ruleName, "triggered").Inc()
				rp.metrics.triggersTotal.WithLabelValues(ruleName, "info").Inc()
			} else {
				rp.metrics.evaluationsTotal.WithLabelValues(ruleName, "not_triggered").Inc()
			}
		}

		hasStatefulActions := hasDefinition && hasStatefulRuleActions(ruleDef)

		if hasStatefulActions && rp.statefulEvaluator != nil {
			// entityState is passed for When-clause access; revision/bootstrap
			// propagate the KV-watch context so SourceRevision persists and
			// OnRecovery can fire on restart.
			transition, err := rp.statefulEvaluator.Evaluate(ctx, Evaluation{
				Rule:              ruleDef,
				EntityID:          entityID,
				CurrentlyMatching: triggered,
				Entity:            snap.State,
				Revision:          snap.Revision,
				Bootstrap:         bootstrap,
			})
			if err != nil {
				rp.logger.Warn("Stateful evaluation failed", "rule_name", ruleName, "error", err)
			} else if transition != TransitionNone {
				rp.logger.Debug("Rule state transition",
					"rule_name", ruleName,
					"transition", transition,
					"entity_id", entityID)

				// Update state transition metrics
				if rp.metrics != nil {
					rp.metrics.stateTransitionsTotal.WithLabelValues(ruleName, string(transition)).Inc()
				}
			}
		}

		if triggered {
			// Wrap the entity state in a minimal message so ExecuteEvents can
			// run through the same path as subject-delivered messages.
			msg := rp.entityStateToMinimalMessage(snap.State)
			messages := []message.Message{msg}
			rp.fireRuleActions(ctx, ruleName, hasDefinition, ruleDef, counters, ruleInstance, messages)
		} else {
			rp.logger.Debug("Rule did not trigger", "rule_name", ruleName)
		}
	}
}

func hasStatefulRuleActions(def Definition) bool {
	return len(def.OnEnter) > 0 || len(def.OnExit) > 0 || len(def.WhileTrue) > 0 || len(def.OnRecovery) > 0
}

// entityStateToMinimalMessage creates a minimal message wrapper for ExecuteEvents compatibility
func (rp *Processor) entityStateToMinimalMessage(entityState *gtypes.EntityState) message.Message {
	msgType := message.Type{
		Domain:   "entity",
		Category: "state",
		Version:  "v1",
	}

	payloadData := map[string]any{
		"entity_id":  entityState.ID,
		"timestamp":  time.Now(),
		"source":     "kv-watch",
		"version":    entityState.Version,
		"updated_at": entityState.UpdatedAt,
	}

	payload := message.NewGenericJSON(payloadData)
	return message.NewBaseMessage(msgType, payload, "kv-watch")
}

// matchesRuleSubject checks if a NATS subject matches the rule's subscription pattern
func (rp *Processor) matchesRuleSubject(r Rule, subject string) bool {
	ruleSubjects := r.Subscribe()

	// Check against all rule subscription patterns
	for _, ruleSubject := range ruleSubjects {
		// Simple wildcard matching - in production, use proper NATS subject matching
		if ruleSubject == ">" || ruleSubject == subject {
			return true
		}

		// Handle basic wildcard patterns like "process.robotics.>"
		if len(ruleSubject) > 2 && ruleSubject[len(ruleSubject)-2:] == ".>" {
			prefix := ruleSubject[:len(ruleSubject)-2]
			if len(subject) >= len(prefix) && subject[:len(prefix)] == prefix {
				return true
			}
		}
	}

	return false
}

// extractEntityID extracts the entity ID from a message for state tracking
func extractEntityID(msg message.Message) string {
	// Try to get entity_id from payload data
	if payload := msg.Payload(); payload != nil {
		if genericPayload, ok := payload.(*message.GenericJSONPayload); ok {
			if entityID, exists := genericPayload.Data["entity_id"]; exists {
				if id, ok := entityID.(string); ok {
					return id
				}
			}
		}
	}

	// Fallback to message ID if no entity_id in payload
	return msg.ID()
}

// extractMessageData returns the inbound message's payload as a generic
// map for `$message.*` substitution. Mirrors the path the expression
// evaluator uses at `expression_factory.go:97` — only
// `GenericJSONPayload` exposes its data as a generic map, so other
// payload types yield nil and the substitution layer falls back to
// silent-pass + warning per the unresolvedTemplateVarRe contract.
//
// nil is a valid return: it signals "no message-data scope for this
// evaluation" to downstream substitution. Authors who reach for
// $message.* in templates fired by entity-state or cron rules will see
// the unresolved-template warning, which is the correct surfacing.
func extractMessageData(msg message.Message) map[string]any {
	if msg == nil {
		return nil
	}
	payload := msg.Payload()
	if payload == nil {
		return nil
	}
	if generic, ok := payload.(*message.GenericJSONPayload); ok {
		return generic.Data
	}
	return nil
}

// recordError records an error and updates health status
func (rp *Processor) recordError(errorMsg string) {
	atomic.AddInt64(&rp.errorCount, 1)

	// Update metrics - try to extract rule name and error type from error message
	if rp.metrics != nil {
		ruleName := "unknown"
		errorType := "generic"

		// Try to extract rule name from error message patterns
		if strings.Contains(errorMsg, "rule ") {
			// Extract rule name between "rule " and next space or punctuation
			parts := strings.Split(errorMsg, "rule ")
			if len(parts) > 1 {
				ruleNamePart := strings.Fields(parts[1])
				if len(ruleNamePart) > 0 {
					ruleName = ruleNamePart[0]
				}
			}
		}

		// Categorize error type
		if strings.Contains(errorMsg, "unmarshal") || strings.Contains(errorMsg, "marshal") {
			errorType = "serialization"
		} else if strings.Contains(errorMsg, "publish") {
			errorType = "publishing"
		} else if strings.Contains(errorMsg, "execution") || strings.Contains(errorMsg, "evaluate") {
			errorType = "rule_execution"
		} else if strings.Contains(errorMsg, "validation") || strings.Contains(errorMsg, "validate") {
			errorType = "validation"
		}

		rp.metrics.errorsTotal.WithLabelValues(ruleName, errorType).Inc()
	}

	rp.mu.Lock()
	rp.lastError = errorMsg
	rp.health.LastError = errorMsg
	rp.mu.Unlock()

	rp.logger.Error("Rule processor error", "error", errorMsg)
}
