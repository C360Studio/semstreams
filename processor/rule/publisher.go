package rule

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/errs"
)

type graphEventPublisher interface {
	Publish(context.Context, string, []byte) error
	PublishToStream(context.Context, string, []byte) error
}

type preparedGraphEvent struct {
	subject   string
	eventType string
	data      []byte
}

// actionPublisher implements the Publisher interface for ActionExecutor.
// It wraps the Processor's NATS client and port configuration to provide
// transparent publishing to either core NATS or JetStream based on config.
type actionPublisher struct {
	processor *Processor
}

// newActionPublisher creates a new publisher that uses the processor's NATS client.
func newActionPublisher(processor *Processor) *actionPublisher {
	return &actionPublisher{processor: processor}
}

// Publish sends a message to a NATS subject.
// It checks port configuration to determine whether to use JetStream or core NATS.
func (p *actionPublisher) Publish(ctx context.Context, subject string, data []byte) error {
	if p.processor.natsClient == nil {
		return errs.WrapFatal(fmt.Errorf("NATS client not configured"), "actionPublisher", "Publish", "client check")
	}

	var publishErr error
	if p.processor.isJetStreamPortBySubject(subject) {
		publishErr = p.processor.natsClient.PublishToStream(ctx, subject, data)
	} else {
		publishErr = p.processor.natsClient.Publish(ctx, subject, data)
	}

	if publishErr != nil {
		return errs.WrapTransient(publishErr, "actionPublisher", "Publish", fmt.Sprintf("publish to %s", subject))
	}

	// Update metrics
	if p.processor.metrics != nil {
		p.processor.metrics.eventsPublishedTotal.WithLabelValues(subject, "action_publish").Inc()
	}

	atomic.AddInt64(&p.processor.eventsPublished, 1)
	return nil
}

// isJetStreamPortBySubject checks if an output port with the given subject is configured for JetStream
func (rp *Processor) isJetStreamPortBySubject(subject string) bool {
	for _, port := range rp.outputPorts {
		facts, err := port.Facts()
		if err != nil {
			continue
		}
		if subjects := facts.NATSSubjects(); len(subjects) == 1 && subjects[0] == subject {
			return facts.Kind() == component.PortKindJetStream
		}
	}
	return false
}

// publishGraphEvents publishes a batch of graph events to appropriate NATS subjects
// Accepts generic Event interface, works with any event type that implements it
func (rp *Processor) publishGraphEvents(ctx context.Context, events []Event) error {
	if len(events) == 0 {
		return nil
	}

	prepared, err := rp.prepareGraphEvents(events)
	if err != nil {
		return err
	}
	return rp.publishPreparedGraphEvents(ctx, prepared)
}

func (rp *Processor) publishPreparedGraphEvents(ctx context.Context, events []preparedGraphEvent) error {
	if rp.config == nil || !rp.config.EnableGraphIntegration {
		return nil
	}
	if rp.graphEventPublisher == nil {
		return errs.WrapFatal(errs.ErrInvalidConfig, "RuleProcessor", "publishGraphEvents", "publisher is not configured")
	}

	for _, event := range events {
		// Publish to NATS, respecting port type configuration
		var publishErr error
		if rp.isJetStreamPortBySubject(event.subject) {
			publishErr = rp.graphEventPublisher.PublishToStream(ctx, event.subject, event.data)
		} else {
			publishErr = rp.graphEventPublisher.Publish(ctx, event.subject, event.data)
		}
		if publishErr != nil {
			return errs.WrapTransient(publishErr, "RuleProcessor", "publishEvent", fmt.Sprintf("publish to %s", event.subject))
		}

		// Update metrics
		if rp.metrics != nil {
			rp.metrics.eventsPublishedTotal.WithLabelValues(event.subject, event.eventType).Inc()
		}

		atomic.AddInt64(&rp.eventsPublished, 1)
	}

	return nil
}

const graphEventRejectionLane = "batch_preflight"

func (rp *Processor) prepareGraphEvents(events []Event) ([]preparedGraphEvent, error) {
	// Structural validation is a complete first pass, so a nil or invalid
	// member causes zero marshaling as well as zero external side effects.
	for _, event := range events {
		if event == nil || isNilRuleEvent(event) {
			rp.recordGraphEventRejection("nil_event")
			return nil, errs.WrapInvalid(errs.ErrInvalidData, "RuleProcessor", "publishGraphEvents", "event is nil")
		}
		if err := event.Validate(); err != nil {
			rp.recordGraphEventRejection("invalid_event")
			return nil, errs.WrapInvalid(err, "RuleProcessor", "publishGraphEvents", "validate event batch")
		}
	}

	// Encode every member before the first publish. JSON-invalid property
	// values (for example NaN) are not part of Event.Validate's envelope
	// contract, so encoding is the second half of atomic publication preflight.
	prepared := make([]preparedGraphEvent, 0, len(events))
	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			rp.recordGraphEventRejection("marshal_error")
			return nil, errs.WrapInvalid(err, "RuleProcessor", "publishGraphEvents", "marshal event batch")
		}
		prepared = append(prepared, preparedGraphEvent{
			subject:   event.Subject(),
			eventType: event.EventType(),
			data:      data,
		})
	}
	return prepared, nil
}

func (rp *Processor) recordGraphEventRejection(reason string) {
	if rp.metrics != nil && rp.metrics.graphEventRejections != nil {
		rp.metrics.graphEventRejections.WithLabelValues(graphEventRejectionLane, reason).Inc()
	}
}

func isNilRuleEvent(event Event) bool {
	value := reflect.ValueOf(event)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// publishRuleEvent publishes a rule-specific event to the configured output port
func (rp *Processor) publishRuleEvent(ctx context.Context, ruleName, eventType string) error {
	var subject string
	for _, port := range rp.outputPorts {
		if port.Name != "rule_events" {
			continue
		}
		facts, factsErr := port.Facts()
		if factsErr != nil {
			return factsErr
		}
		subjects := facts.NATSSubjects()
		if len(subjects) != 1 {
			return fmt.Errorf("rule_events output must declare one NATS subject")
		}
		subject = subjects[0]
		break
	}
	if subject == "" {
		return nil
	}
	if rp.natsClient == nil {
		return errs.WrapFatal(errs.ErrInvalidConfig, "RuleProcessor", "publishRuleEvent", "publisher is not configured")
	}

	// Construct notification payload only when the optional output is enabled.
	ruleEvent := map[string]any{
		"rule_name":  ruleName,
		"event_type": eventType,
		"timestamp":  time.Now().Format(time.RFC3339),
		"processor":  "rule-processor",
		"action":     eventType, // For backward compatibility with tests
	}
	data, err := json.Marshal(ruleEvent)
	if err != nil {
		return errs.Wrap(err, "RuleProcessor", "publishRuleEvent", "marshal rule event")
	}

	// Publish to NATS, respecting port type configuration
	var publishErr error
	if rp.isJetStreamPortBySubject(subject) {
		publishErr = rp.natsClient.PublishToStream(ctx, subject, data)
	} else {
		publishErr = rp.natsClient.Publish(ctx, subject, data)
	}
	if publishErr != nil {
		return errs.WrapTransient(publishErr, "RuleProcessor", "publishRuleEvent", fmt.Sprintf("publish to %s", subject))
	}

	// Update metrics
	if rp.metrics != nil {
		rp.metrics.eventsPublishedTotal.WithLabelValues(subject, eventType).Inc()
	}

	atomic.AddInt64(&rp.eventsPublished, 1)
	return nil
}
