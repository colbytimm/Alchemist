package test

import (
	"testing"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/stretchr/testify/assert"
)

func TestFormatOutput(t *testing.T) {
	// Test JSON output format (default passthrough)
	t.Run("JSON format", func(t *testing.T) {
		input := `{"items":[{"id":"1","name":"test"}]}`
		output, err := cmd.FormatOutput(input, cmd.JSON)
		assert.NoError(t, err)
		assert.Equal(t, input, output)
	})

	// Test RAW output format
	t.Run("RAW format", func(t *testing.T) {
		input := `{"items": [{"id": "1", "name": "test"}]}`
		expectedOutput := `{"items":[{"id":"1","name":"test"}]}`
		output, err := cmd.FormatOutput(input, cmd.RAW)
		assert.NoError(t, err)
		assert.Equal(t, expectedOutput, output)
	})

	// Test RAW format with invalid JSON
	t.Run("RAW format with invalid JSON", func(t *testing.T) {
		input := `{"items": [{"id": "1", "name": "test"`
		_, err := cmd.FormatOutput(input, cmd.RAW)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "error parsing JSON")
	})

	// Test TABLE format (not fully implemented)
	t.Run("TABLE format", func(t *testing.T) {
		input := `{"items":[{"id":"1","name":"test"}]}`
		output, err := cmd.FormatOutput(input, cmd.TABLE)
		assert.NoError(t, err)
		assert.Contains(t, output, "Table formatting not fully implemented yet")
		assert.Contains(t, output, input)
	})

	// Test default format (same as JSON)
	t.Run("Default format", func(t *testing.T) {
		input := `{"items":[{"id":"1","name":"test"}]}`
		output, err := cmd.FormatOutput(input, "unknown")
		assert.NoError(t, err)
		assert.Equal(t, input, output)
	})
}
