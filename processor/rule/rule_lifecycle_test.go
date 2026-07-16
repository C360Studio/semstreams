package rule

import (
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
)

// createTestRuleComponent creates a test instance for lifecycle testing.
func createTestRuleComponent() component.LifecycleComponent {
	// Create unconnected NATS client (won't actually connect)
	natsClient, err := natsclient.NewClient("nats://localhost:4222")
	if err != nil {
		panic("failed to create NATS client: " + err.Error())
	}

	config, err := NewConfig("rule-lifecycle-test")
	if err != nil {
		panic("failed to create rule config: " + err.Error())
	}

	comp, err := NewProcessor(natsClient, &config)
	if err != nil {
		panic("failed to create component: " + err.Error())
	}

	return comp
}

// TestRule_ComprehensiveLifecycle runs the complete lifecycle test suite
func TestRule_ComprehensiveLifecycle(t *testing.T) {
	component.StandardLifecycleTests(t, createTestRuleComponent)
}
