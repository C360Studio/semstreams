package boid

import (
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/payloadregistry"
)

// RegisterPayloads registers boid payload types (AgentPosition,
// SteeringSignal) with the supplied registry. Called from
// payloadbuiltins.Register at process bootstrap.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	registrations := []*payloadregistry.Registration{
		{
			Domain:      Domain,
			Category:    CategoryPosition,
			Version:     SchemaVersion,
			Description: "Agent position tracking for Boid coordination rules",
			Factory:     func() any { return &AgentPosition{} },
		},
		{
			Domain:      Domain,
			Category:    CategorySignal,
			Version:     SchemaVersion,
			Description: "Boid steering signal for agent coordination",
			Factory:     func() any { return &SteeringSignal{} },
		},
	}

	var errs []error
	for _, r := range registrations {
		if err := reg.Register(r); err != nil {
			errs = append(errs, fmt.Errorf("register %s: %w", r.MessageType(), err))
		}
	}
	return errors.Join(errs...)
}
