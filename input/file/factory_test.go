package file

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateInputAppliesDefaultsAfterPartialDecode(t *testing.T) {
	rawConfig := json.RawMessage(`{
		"path": "/tmp/events.jsonl",
		"ports": {
			"outputs": [{
				"name": "nats_output",
				"config": {"kind": "nats", "subject": "test.file"}
			}]
		}
	}`)

	discoverable, err := CreateInput(rawConfig, component.Dependencies{NATSClient: &natsclient.Client{}})
	require.NoError(t, err)
	input := discoverable.(*Input)

	assert.Equal(t, "jsonl", input.config.Format)
	assert.Equal(t, "10ms", input.config.Interval)
	assert.False(t, input.config.Loop)
	require.Len(t, input.OutputPorts(), 1)
}
