package rule

import (
	"context"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

func TestPublishRuleEventNotificationContract(t *testing.T) {
	t.Parallel()

	t.Run("absent output is notification disabled", func(t *testing.T) {
		t.Parallel()
		processor := &Processor{}
		require.NoError(t, processor.publishRuleEvent(
			context.Background(), "rule", "triggered",
		))
	})

	t.Run("configured malformed output is an error", func(t *testing.T) {
		t.Parallel()
		processor := &Processor{outputPorts: []component.Port{{
			Name:      "rule_events",
			Direction: component.DirectionOutput,
			Config:    component.NATSPort{},
		}}}
		require.Error(t, processor.publishRuleEvent(
			context.Background(), "rule", "triggered",
		))
	})

	t.Run("configured publish failure is an error", func(t *testing.T) {
		t.Parallel()
		port, err := (component.PortDefinition{
			Name:   "rule_events",
			Config: component.NATSPort{Subject: "events.rule.triggered"},
		}).Resolve(component.DirectionOutput)
		require.NoError(t, err)
		client := &natsclient.Client{}
		processor := &Processor{
			outputPorts: []component.Port{port},
			natsClient:  client,
		}
		require.Error(t, processor.publishRuleEvent(
			context.Background(), "rule", "triggered",
		))
	})
}
