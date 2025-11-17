package message_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360/semstreams/message"
)

func TestGenericJSON_Schema(t *testing.T) {
	payload := &message.GenericJSONPayload{
		Data: map[string]any{
			"test": "value",
		},
	}

	schema := payload.Schema()
	assert.Equal(t, "core", schema.Domain)
	assert.Equal(t, "json", schema.Category)
	assert.Equal(t, "v1", schema.Version)
}

func TestGenericJSON_Validate(t *testing.T) {
	tests := []struct {
		name      string
		payload   *message.GenericJSONPayload
		wantError bool
	}{
		{
			name: "valid payload with data",
			payload: &message.GenericJSONPayload{
				Data: map[string]any{
					"sensor": "temp-001",
					"value":  23.5,
				},
			},
			wantError: false,
		},
		{
			name: "valid payload with empty map",
			payload: &message.GenericJSONPayload{
				Data: map[string]any{},
			},
			wantError: false,
		},
		{
			name: "invalid payload with nil data",
			payload: &message.GenericJSONPayload{
				Data: nil,
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.payload.Validate()
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGenericJSON_MarshalJSON(t *testing.T) {
	payload := &message.GenericJSONPayload{
		Data: map[string]any{
			"sensor_id":   "temp-001",
			"temperature": 23.5,
			"unit":        "celsius",
		},
	}

	data, err := json.Marshal(payload)
	require.NoError(t, err)

	// Verify JSON structure
	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	dataField, ok := result["data"].(map[string]any)
	require.True(t, ok, "Should have data field")
	assert.Equal(t, "temp-001", dataField["sensor_id"])
	assert.Equal(t, 23.5, dataField["temperature"])
	assert.Equal(t, "celsius", dataField["unit"])
}

func TestGenericJSON_UnmarshalJSON(t *testing.T) {
	jsonData := []byte(`{
		"data": {
			"sensor_id": "temp-001",
			"temperature": 23.5,
			"unit": "celsius"
		}
	}`)

	var payload message.GenericJSONPayload
	err := json.Unmarshal(jsonData, &payload)
	require.NoError(t, err)

	assert.NotNil(t, payload.Data)
	assert.Equal(t, "temp-001", payload.Data["sensor_id"])
	assert.Equal(t, 23.5, payload.Data["temperature"])
	assert.Equal(t, "celsius", payload.Data["unit"])
}

func TestGenericJSON_RoundTrip(t *testing.T) {
	original := &message.GenericJSONPayload{
		Data: map[string]any{
			"string_field":  "test",
			"number_field":  42.0,
			"boolean_field": true,
			"nested_field": map[string]any{
				"inner": "value",
			},
		},
	}

	// Marshal
	data, err := json.Marshal(original)
	require.NoError(t, err)

	// Unmarshal
	var decoded message.GenericJSONPayload
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	// Verify
	assert.Equal(t, original.Data["string_field"], decoded.Data["string_field"])
	assert.Equal(t, original.Data["number_field"], decoded.Data["number_field"])
	assert.Equal(t, original.Data["boolean_field"], decoded.Data["boolean_field"])

	nestedOriginal := original.Data["nested_field"].(map[string]any)
	nestedDecoded := decoded.Data["nested_field"].(map[string]any)
	assert.Equal(t, nestedOriginal["inner"], nestedDecoded["inner"])
}

func TestNewGenericJSON(t *testing.T) {
	data := map[string]any{
		"test": "value",
	}

	payload := message.NewGenericJSON(data)
	require.NotNil(t, payload)
	assert.Equal(t, data, payload.Data)

	// Verify Schema
	schema := payload.Schema()
	assert.Equal(t, message.Type{
		Domain:   "core",
		Category: "json",
		Version:  "v1",
	}, schema)
}

func TestGenericJSON_NestedStructures(t *testing.T) {
	payload := &message.GenericJSONPayload{
		Data: map[string]any{
			"sensors": []any{
				map[string]any{"id": "s1", "value": 10.5},
				map[string]any{"id": "s2", "value": 20.3},
			},
			"metadata": map[string]any{
				"location":  "warehouse-1",
				"timestamp": "2024-01-13T10:30:00Z",
			},
		},
	}

	// Validate
	err := payload.Validate()
	require.NoError(t, err)

	// Round-trip
	data, err := json.Marshal(payload)
	require.NoError(t, err)

	var decoded message.GenericJSONPayload
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	// Verify nested array
	sensors, ok := decoded.Data["sensors"].([]any)
	require.True(t, ok)
	assert.Len(t, sensors, 2)

	// Verify nested map
	metadata, ok := decoded.Data["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "warehouse-1", metadata["location"])
}
