package parser

import (
	"encoding/json"

	"github.com/c360/semstreams/errors"
)

// JSONParser handles JSON format data
type JSONParser struct{}

// NewJSONParser creates a new JSON parser
func NewJSONParser() *JSONParser {
	return &JSONParser{}
}

// Parse parses JSON data into a map
func (p *JSONParser) Parse(data []byte) (map[string]any, error) {
	if len(data) == 0 {
		return nil, ErrEmptyData
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, errors.WrapInvalid(err, "JSONParser", "Parse", "json parsing failed")
	}

	return result, nil
}

// Format returns the format name
func (p *JSONParser) Format() string {
	return "json"
}

// Validate checks if the data is valid JSON
func (p *JSONParser) Validate(data []byte) error {
	if len(data) == 0 {
		return ErrEmptyData
	}

	var temp any
	if err := json.Unmarshal(data, &temp); err != nil {
		return errors.WrapInvalid(err, "JSONParser", "Validate", "invalid json format")
	}

	return nil
}
