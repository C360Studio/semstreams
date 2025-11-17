package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCSVParser(t *testing.T) {
	parser := NewCSVParser()
	assert.Equal(t, "csv", parser.Format())

	t.Run("valid CSV", func(t *testing.T) {
		data := []byte("name,age,city\nJohn,30,NYC\nJane,25,LA")

		err := parser.Validate(data)
		require.NoError(t, err)

		result, err := parser.Parse(data)
		require.NoError(t, err)

		assert.Equal(t, "csv", result["format"])
		assert.Equal(t, string(data), result["raw"])
		assert.Equal(t, false, result["parsed"]) // Placeholder implementation
	})

	t.Run("empty data", func(t *testing.T) {
		err := parser.Validate([]byte{})
		assert.ErrorIs(t, err, ErrEmptyData)

		_, err = parser.Parse([]byte{})
		assert.ErrorIs(t, err, ErrEmptyData)
	})

	t.Run("single line CSV", func(t *testing.T) {
		data := []byte("header1,header2,header3")

		err := parser.Validate(data)
		require.NoError(t, err)

		result, err := parser.Parse(data)
		require.NoError(t, err)
		assert.Equal(t, "csv", result["format"])
	})
}
