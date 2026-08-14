package document

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/stretchr/testify/require"
)

func TestDocumentConsumerConfigPreservesLocalDefaults(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  string
	}{
		{
			name: "omitted max delivery",
			raw:  `{"name":"documents","config":{"kind":"jetstream","stream_name":"DOCUMENTS","subjects":["documents.>"]}}`,
		},
		{
			name: "explicit zero max delivery",
			raw:  `{"name":"documents","config":{"kind":"jetstream","stream_name":"DOCUMENTS","subjects":["documents.>"],"max_deliver":0}}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var definition component.PortDefinition
			require.NoError(t, json.Unmarshal([]byte(test.raw), &definition))
			port, err := definition.Resolve(component.DirectionInput)
			require.NoError(t, err)

			consumerCfg, err := documentConsumerConfig(port)
			require.NoError(t, err)
			require.Equal(t, "all", consumerCfg.DeliverPolicy)
			require.Equal(t, "explicit", consumerCfg.AckPolicy)
			require.Equal(t, 5, consumerCfg.MaxDeliver)
			require.Zero(t, consumerCfg.MaxAckPending)
		})
	}
}

func TestDocumentConsumerConfigHonorsExplicitPolicy(t *testing.T) {
	for _, maxAckPending := range []int{37, -1} {
		port, err := (component.PortDefinition{
			Name: "documents",
			Config: component.JetStreamPort{
				StreamName:    "DOCUMENTS",
				Subjects:      []string{"documents.>"},
				DeliverPolicy: "last",
				AckPolicy:     "all",
				MaxDeliver:    11,
				MaxAckPending: maxAckPending,
			},
		}).Resolve(component.DirectionInput)
		require.NoError(t, err)

		consumerCfg, err := documentConsumerConfig(port)
		require.NoError(t, err)
		require.Equal(t, "last", consumerCfg.DeliverPolicy)
		require.Equal(t, "all", consumerCfg.AckPolicy)
		require.Equal(t, 11, consumerCfg.MaxDeliver)
		require.Equal(t, maxAckPending, consumerCfg.MaxAckPending)
	}
}

func TestDocumentConsumerConfigPreservesCanonicalValidation(t *testing.T) {
	for _, config := range []component.Portable{
		component.NATSPort{Subject: "documents.>"},
		component.JetStreamPort{
			StreamName:    "DOCUMENTS",
			Subjects:      []string{"documents.>"},
			DeliverPolicy: "newest",
		},
	} {
		_, err := documentConsumerConfig(component.Port{
			Name:      "documents",
			Direction: component.DirectionInput,
			Config:    config,
		})
		require.Error(t, err)
	}
}
