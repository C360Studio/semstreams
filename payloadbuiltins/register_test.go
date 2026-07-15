package payloadbuiltins_test

import (
	"testing"

	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
)

func TestRegisterCoreExcludesCapabilityAndProductPayloads(t *testing.T) {
	registry := payloadregistry.New()
	if err := payloadbuiltins.Register(registry); err != nil {
		t.Fatalf("Register: %v", err)
	}

	for _, messageType := range []string{
		"oms.observation.v1", "github.webhook.v1", "research.intent.v1",
		"research.classification.v1", "research.search_result.v1",
	} {
		if _, ok := registry.GetRegistration(messageType); ok {
			t.Errorf("core payload registry unexpectedly contains %q", messageType)
		}
	}
}
