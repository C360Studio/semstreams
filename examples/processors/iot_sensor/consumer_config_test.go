package iotsensor

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/stretchr/testify/require"
)

func TestIoTSensorConsumerConfigPreservesLocalDefaults(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  string
	}{
		{
			name: "omitted max delivery",
			raw:  `{"name":"sensors","config":{"kind":"jetstream","stream_name":"SENSORS","subjects":["sensors.>"]}}`,
		},
		{
			name: "explicit zero max delivery",
			raw:  `{"name":"sensors","config":{"kind":"jetstream","stream_name":"SENSORS","subjects":["sensors.>"],"max_deliver":0}}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var definition component.PortDefinition
			require.NoError(t, json.Unmarshal([]byte(test.raw), &definition))
			port, err := definition.Resolve(component.DirectionInput)
			require.NoError(t, err)

			consumerCfg, err := iotSensorConsumerConfig(port)
			require.NoError(t, err)
			require.Equal(t, "all", consumerCfg.DeliverPolicy)
			require.Equal(t, "explicit", consumerCfg.AckPolicy)
			require.Equal(t, 5, consumerCfg.MaxDeliver)
			require.Zero(t, consumerCfg.MaxAckPending)
		})
	}
}

func TestIoTSensorConsumerConfigHonorsExplicitPolicy(t *testing.T) {
	for _, maxAckPending := range []int{37, -1} {
		port, err := (component.PortDefinition{
			Name: "sensors",
			Config: component.JetStreamPort{
				StreamName:    "SENSORS",
				Subjects:      []string{"sensors.>"},
				DeliverPolicy: "last",
				AckPolicy:     "all",
				MaxDeliver:    11,
				MaxAckPending: maxAckPending,
			},
		}).Resolve(component.DirectionInput)
		require.NoError(t, err)

		consumerCfg, err := iotSensorConsumerConfig(port)
		require.NoError(t, err)
		require.Equal(t, "last", consumerCfg.DeliverPolicy)
		require.Equal(t, "all", consumerCfg.AckPolicy)
		require.Equal(t, 11, consumerCfg.MaxDeliver)
		require.Equal(t, maxAckPending, consumerCfg.MaxAckPending)
	}
}

func TestIoTSensorConsumerConfigPreservesCanonicalValidation(t *testing.T) {
	for _, config := range []component.Portable{
		component.NATSPort{Subject: "sensors.>"},
		component.JetStreamPort{
			StreamName:    "SENSORS",
			Subjects:      []string{"sensors.>"},
			DeliverPolicy: "newest",
		},
	} {
		_, err := iotSensorConsumerConfig(component.Port{
			Name:      "sensors",
			Direction: component.DirectionInput,
			Config:    config,
		})
		require.Error(t, err)
	}
}
