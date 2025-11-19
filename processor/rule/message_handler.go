package rule

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/c360/semstreams/message"
	rtypes "github.com/c360/semstreams/types/rule"
)

// handleMessage processes incoming NATS messages with dual-format support
func (rp *Processor) handleMessage(ctx context.Context, subject string, data []byte) {
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
	var baseMsg message.BaseMessage
	if err := json.Unmarshal(data, &baseMsg); err != nil {
		rp.recordError(fmt.Sprintf("failed to unmarshal BaseMessage: %v", err))
		return
	}

	rp.logger.Debug("Successfully unmarshaled BaseMessage", "type", baseMsg.Type().String())

	// Process through rules
	rp.evaluateRulesForMessage(ctx, subject, &baseMsg)
}

// evaluateRulesForMessage performs rule evaluation for any message type
func (rp *Processor) evaluateRulesForMessage(ctx context.Context, subject string, msg message.Message) {
	// Increment evaluation counter for all messages (NATS and KV watcher)
	atomic.AddInt64(&rp.messagesEvaluated, 1)

	// Cache the message if needed
	if rp.messageCache != nil {
		cacheKey := fmt.Sprintf("%s_%d", subject, time.Now().UnixNano())
		rp.messageCache.Set(cacheKey, msg)
	}

	// Process through each rule
	for ruleName, ruleInstance := range rp.rules {
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

		if triggered {
			rp.logger.Info("Rule triggered", "rule_name", ruleName)

			// Execute rule events
			events, err := ruleInstance.ExecuteEvents(messages)
			if err != nil {
				rp.recordError(fmt.Sprintf("rule %s execution failed: %v", ruleName, err))
				continue
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
		} else {
			rp.logger.Debug("Rule did not trigger", "rule_name", ruleName)
		}
	}
}

// matchesRuleSubject checks if a NATS subject matches the rule's subscription pattern
func (rp *Processor) matchesRuleSubject(r rtypes.Rule, subject string) bool {
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
