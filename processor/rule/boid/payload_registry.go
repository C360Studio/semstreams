package boid

import (
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/payloadregistry"
)

// init populates the package-level global registry for transitional
// compat. Removed in C4 along with the global itself.
func init() {
	if err := RegisterPayloads(payloadregistry.Global()); err != nil {
		panic("boid.init: " + err.Error())
	}
}

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
