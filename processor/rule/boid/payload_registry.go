package boid

import "github.com/c360studio/semstreams/payloadregistry"

func init() {
	// Register AgentPosition payload factory
	err := payloadregistry.Register(&payloadregistry.Registration{
		Domain:      Domain,
		Category:    CategoryPosition,
		Version:     SchemaVersion,
		Description: "Agent position tracking for Boid coordination rules",
		Factory:     func() any { return &AgentPosition{} },
	})
	if err != nil {
		panic("failed to register boid.position payload: " + err.Error())
	}

	// Register SteeringSignal payload factory
	err = payloadregistry.Register(&payloadregistry.Registration{
		Domain:      Domain,
		Category:    CategorySignal,
		Version:     SchemaVersion,
		Description: "Boid steering signal for agent coordination",
		Factory:     func() any { return &SteeringSignal{} },
	})
	if err != nil {
		panic("failed to register boid.signal payload: " + err.Error())
	}
}
