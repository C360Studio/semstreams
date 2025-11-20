// Package rule provides interfaces for rule processing
package rule

import (
	"github.com/c360/semstreams/message"
)

// Event represents a generic event that can be published by rules.
// This interface allows rules to be decoupled from specific event types
// while still integrating with graph events when EnableGraphIntegration is enabled.
type Event interface {
	// EventType returns the type identifier for this event
	EventType() string

	// Subject returns the NATS subject for publishing this event
	Subject() string

	// Payload returns the event data as a generic map
	Payload() map[string]any

	// Validate checks if the event is valid and ready to publish
	Validate() error
}

// Rule interface defines the event-based contract for processing rules.
// Rules evaluate messages and generate events when conditions are met.
type Rule interface {
	// Name returns the human-readable name of this rule
	Name() string

	// Subscribe returns the NATS subjects this rule should listen to
	Subscribe() []string

	// Evaluate checks if the rule conditions are met for the given messages
	Evaluate(messages []message.Message) bool

	// ExecuteEvents generates events when rule conditions are satisfied
	ExecuteEvents(messages []message.Message) ([]Event, error)
}

