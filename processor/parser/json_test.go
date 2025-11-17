package parser

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONParser(t *testing.T) {
	parser := NewJSONParser()
	assert.Equal(t, "json", parser.Format())

	t.Run("valid JSON", func(t *testing.T) {
		data := []byte(`{"name": "test", "value": 42}`)

		err := parser.Validate(data)
		require.NoError(t, err)

		result, err := parser.Parse(data)
		require.NoError(t, err)

		assert.Equal(t, "test", result["name"])
		assert.Equal(t, float64(42), result["value"]) // JSON numbers are float64
	})

	t.Run("empty data", func(t *testing.T) {
		err := parser.Validate([]byte{})
		assert.ErrorIs(t, err, ErrEmptyData)

		_, err = parser.Parse([]byte{})
		assert.ErrorIs(t, err, ErrEmptyData)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		data := []byte(`{invalid json}`)

		err := parser.Validate(data)
		assert.Error(t, err)

		_, err = parser.Parse(data)
		assert.Error(t, err)
	})

	t.Run("complex JSON", func(t *testing.T) {
		complexData := map[string]any{
			"string":  "value",
			"number":  123,
			"boolean": true,
			"array":   []any{"a", "b", "c"},
			"object":  map[string]any{"nested": "value"},
		}

		data, err := json.Marshal(complexData)
		require.NoError(t, err)

		err = parser.Validate(data)
		require.NoError(t, err)

		result, err := parser.Parse(data)
		require.NoError(t, err)

		assert.Equal(t, "value", result["string"])
		assert.Equal(t, float64(123), result["number"])
		assert.Equal(t, true, result["boolean"])

		// Arrays and objects are preserved as any types
		array, ok := result["array"].([]any)
		require.True(t, ok)
		assert.Len(t, array, 3)
		assert.Equal(t, "a", array[0])

		object, ok := result["object"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "value", object["nested"])
	})
}
