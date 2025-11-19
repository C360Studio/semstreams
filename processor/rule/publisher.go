package rule

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/c360/semstreams/errors"
	gtypes "github.com/c360/semstreams/types/graph"
)

// publishGraphEvents publishes a batch of graph events to appropriate NATS subjects
func (rp *Processor) publishGraphEvents(ctx context.Context, events []gtypes.Event) error {
	if len(events) == 0 {
		return nil
	}

	if !rp.config.EnableGraphIntegration {
		return nil // Graph integration disabled
	}

	for _, event := range events {
		// Validate event
		if err := event.Validate(); err != nil {
			return errors.WrapInvalid(err, "RuleProcessor", "publishEvent", "validate event")
		}

		// Get appropriate subject (e.g., "graph.events.entity.create")
		subject := event.Subject()

		// Marshal event
		data, err := json.Marshal(event)
		if err != nil {
			return errors.Wrap(err, "RuleProcessor", "publishEvent", "marshal event")
		}

		// Publish to NATS
		if err := rp.natsClient.Publish(ctx, subject, data); err != nil {
			return errors.WrapTransient(err, "RuleProcessor", "publishEvent", fmt.Sprintf("publish to %s", subject))
		}

		// Update metrics
		if rp.metrics != nil {
			rp.metrics.eventsPublishedTotal.WithLabelValues(subject, string(event.Type)).Inc()
		}

		atomic.AddInt64(&rp.eventsPublished, 1)
	}

	return nil
}

// publishRuleEvent publishes a rule-specific event to the configured output port
func (rp *Processor) publishRuleEvent(ctx context.Context, ruleName, eventType string) error {

	// Create rule event payload
	ruleEvent := map[string]any{
		"rule_name":  ruleName,
		"event_type": eventType,
		"timestamp":  time.Now().Format(time.RFC3339),
		"processor":  "rule-processor",
		"action":     eventType, // For backward compatibility with tests
	}

	// Marshal event
	data, err := json.Marshal(ruleEvent)
	if err != nil {
		return errors.Wrap(err, "RuleProcessor", "publishRuleEvent", "marshal rule event")
	}

	// Use configured output port subject, fallback to rule-specific subject
	subject := "events.rule.triggered" // Default subject
	if rp.config != nil && rp.config.Ports != nil {
		for _, port := range rp.config.Ports.Outputs {
			if port.Name == "rule_events" && port.Subject != "" {
				subject = port.Subject
				break
			}
		}
	}

	if err := rp.natsClient.Publish(ctx, subject, data); err != nil {
		return errors.WrapTransient(err, "RuleProcessor", "publishRuleEvent", fmt.Sprintf("publish to %s", subject))
	}

	// Update metrics
	if rp.metrics != nil {
		rp.metrics.eventsPublishedTotal.WithLabelValues(subject, eventType).Inc()
	}

	atomic.AddInt64(&rp.eventsPublished, 1)
	return nil
}
