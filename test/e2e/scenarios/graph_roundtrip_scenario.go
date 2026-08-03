package scenarios

import (
	"context"
	"time"

	"github.com/c360studio/semstreams/test/e2e/client"
)

// GraphRoundTripScenario adapts the shared probe to the standalone core suite.
type GraphRoundTripScenario struct {
	natsURL    string
	serviceURL string
	graphqlURL string
	nats       *client.NATSValidationClient
}

// NewGraphRoundTripScenario creates the standalone core graph canary.
func NewGraphRoundTripScenario(natsURL, serviceURL, graphqlURL string) *GraphRoundTripScenario {
	return &GraphRoundTripScenario{natsURL: natsURL, serviceURL: serviceURL, graphqlURL: graphqlURL}
}

// Name returns the standalone core scenario identifier.
func (s *GraphRoundTripScenario) Name() string { return "core-graph-roundtrip" }

// Description summarizes the graph seams asserted by the scenario.
func (s *GraphRoundTripScenario) Description() string {
	return "Creates, replaces, stores, indexes, traces, and reads one graph entity through public seams"
}

// Setup opens the scenario-owned NATS validation connection.
func (s *GraphRoundTripScenario) Setup(ctx context.Context) error {
	nats, err := client.NewNATSValidationClient(ctx, s.natsURL)
	if err != nil {
		return err
	}
	s.nats = nats
	return nil
}

// Execute runs the shared graph round-trip probe.
func (s *GraphRoundTripScenario) Execute(ctx context.Context) (*Result, error) {
	result := &Result{
		ScenarioName: s.Name(), StartTime: time.Now(),
		Metrics: make(map[string]any), Details: make(map[string]any),
	}
	probe := NewGraphRoundTripProbe(s.nats, client.NewMessageLoggerClient(s.serviceURL), s.graphqlURL)
	if err := probe.Run(ctx, result); err != nil {
		result.Error = err.Error()
		result.Errors = []string{err.Error()}
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		return result, nil
	}
	result.Success = true
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	return result, nil
}

// Teardown closes the scenario-owned NATS connection.
func (s *GraphRoundTripScenario) Teardown(ctx context.Context) error {
	if s.nats == nil {
		return nil
	}
	return s.nats.Close(ctx)
}
