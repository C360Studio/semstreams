package component

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/c360/semstreams/errors"
)

// ConfigValidator provides secure validation for component configurations
type ConfigValidator struct {
	maxDepth     int
	maxArraySize int
	maxStringLen int
	maxJSONSize  int
}

// NewConfigValidator creates a validator with secure defaults
func NewConfigValidator() *ConfigValidator {
	return &ConfigValidator{
		maxDepth:     10,   // Prevent deeply nested JSON attacks
		maxArraySize: 1000, // Prevent memory exhaustion from huge arrays
		maxStringLen: MaxStringLength,
		maxJSONSize:  MaxJSONSize,
	}
}

// ValidateConfig performs comprehensive validation on raw JSON config
// This prevents injection attacks, resource exhaustion, and malformed input
func (v *ConfigValidator) ValidateConfig(rawConfig json.RawMessage) error {
	// Check JSON size first
	if len(rawConfig) > v.maxJSONSize {
		return errors.WrapInvalid(
			fmt.Errorf("config size %d exceeds maximum %d", len(rawConfig), v.maxJSONSize),
			"ConfigValidator", "ValidateConfig", "size check")
	}

	// Empty config is valid (components can have defaults)
	if len(rawConfig) == 0 {
		return nil
	}

	// Parse into any for deep validation
	var config any
	decoder := json.NewDecoder(strings.NewReader(string(rawConfig)))
	decoder.UseNumber() // Prevent float overflow attacks

	if err := decoder.Decode(&config); err != nil {
		return errors.WrapInvalid(err, "ConfigValidator", "ValidateConfig", "JSON parsing")
	}

	// Perform deep validation on the parsed structure
	if err := v.validateValue(config, 0); err != nil {
		return errors.Wrap(err, "ConfigValidator", "ValidateConfig", "deep validation")
	}

	return nil
}

// validateValue recursively validates a JSON value
func (v *ConfigValidator) validateValue(value any, depth int) error {
	// Check depth to prevent stack overflow
	if depth > v.maxDepth {
		return errors.WrapInvalid(
			fmt.Errorf("JSON depth %d exceeds maximum %d", depth, v.maxDepth),
			"ConfigValidator", "validateValue", "depth check")
	}

	switch val := value.(type) {
	case string:
		if len(val) > v.maxStringLen {
			return errors.WrapInvalid(
				fmt.Errorf("string length %d exceeds maximum %d", len(val), v.maxStringLen),
				"ConfigValidator", "validateValue", "string length check")
		}
		// Check for potential injection patterns
		if err := v.validateStringContent(val); err != nil {
			return err
		}

	case json.Number:
		// Validate number is within safe bounds
		if _, err := val.Int64(); err != nil {
			if _, err := val.Float64(); err != nil {
				return errors.WrapInvalid(err, "ConfigValidator", "validateValue", "number validation")
			}
		}

	case []any:
		if len(val) > v.maxArraySize {
			return errors.WrapInvalid(
				fmt.Errorf("array size %d exceeds maximum %d", len(val), v.maxArraySize),
				"ConfigValidator", "validateValue", "array size check")
		}
		// Recursively validate array elements
		for i, elem := range val {
			if err := v.validateValue(elem, depth+1); err != nil {
				return errors.Wrap(err, "ConfigValidator", "validateValue",
					fmt.Sprintf("array element %d", i))
			}
		}

	case map[string]any:
		// Validate all keys and values
		for key, val := range val {
			// Validate key
			if len(key) > v.maxStringLen {
				return errors.WrapInvalid(
					fmt.Errorf("key '%s' length exceeds maximum", key),
					"ConfigValidator", "validateValue", "key length check")
			}
			if err := v.validateStringContent(key); err != nil {
				return errors.Wrap(err, "ConfigValidator", "validateValue", "key validation")
			}
			// Recursively validate value
			if err := v.validateValue(val, depth+1); err != nil {
				return errors.Wrap(err, "ConfigValidator", "validateValue",
					fmt.Sprintf("object field '%s'", key))
			}
		}

	case bool, nil:
		// These types are always safe

	default:
		// Reject unexpected types
		return errors.WrapInvalid(
			fmt.Errorf("unexpected type %T in config", value),
			"ConfigValidator", "validateValue", "type check")
	}

	return nil
}

// validateStringContent checks for dangerous patterns in strings
func (v *ConfigValidator) validateStringContent(s string) error {
	// Check for null bytes (can cause issues in C bindings)
	if strings.Contains(s, "\x00") {
		return errors.WrapInvalid(
			fmt.Errorf("string contains null byte"),
			"ConfigValidator", "validateStringContent", "null byte check")
	}

	// Check for control characters (except common ones like \n, \r, \t)
	for _, r := range s {
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
			return errors.WrapInvalid(
				fmt.Errorf("string contains control character: 0x%02x", r),
				"ConfigValidator", "validateStringContent", "control character check")
		}
	}

	return nil
}

// ValidateFactoryConfig performs validation before passing to factory
// This is the main security gate for all component configurations
func ValidateFactoryConfig(rawConfig json.RawMessage) error {
	validator := NewConfigValidator()
	return validator.ValidateConfig(rawConfig)
}

// SafeUnmarshal performs validated unmarshaling into a target struct
// It validates the JSON first, then unmarshals with additional type checking
func SafeUnmarshal(rawConfig json.RawMessage, target any) error {
	// First validate the raw JSON
	if err := ValidateFactoryConfig(rawConfig); err != nil {
		return errors.Wrap(err, "ConfigValidator", "SafeUnmarshal", "config validation")
	}

	// Empty config is valid - target will have defaults
	if len(rawConfig) == 0 {
		return nil
	}

	// Ensure target is a pointer to a struct
	targetType := reflect.TypeOf(target)
	if targetType.Kind() != reflect.Ptr {
		return errors.WrapInvalid(
			fmt.Errorf("target must be a pointer, got %T", target),
			"ConfigValidator", "SafeUnmarshal", "target type check")
	}

	// Unmarshal config - ignores unknown fields (Go's default behavior)
	if err := json.Unmarshal(rawConfig, target); err != nil {
		return errors.WrapInvalid(err, "ConfigValidator", "SafeUnmarshal", "JSON unmarshaling")
	}

	// Perform struct validation if target implements Validatable
	if validatable, ok := target.(Validatable); ok {
		if err := validatable.Validate(); err != nil {
			return errors.Wrap(err, "ConfigValidator", "SafeUnmarshal", "struct validation")
		}
	}

	return nil
}

// Validatable interface for configs that can self-validate
type Validatable interface {
	Validate() error
}

// ValidateNetworkConfig validates network configuration including port and bind address
func ValidateNetworkConfig(port int, bindAddr string) error {
	// Validate port range
	if err := ValidatePortNumber(port); err != nil {
		return err
	}

	// Validate bind address
	if bindAddr != "" && bindAddr != "*" {
		// Check for valid IP address format
		parts := strings.Split(bindAddr, ".")
		if len(parts) != 4 {
			return errors.WrapInvalid(
				fmt.Errorf("invalid bind address format: %s", bindAddr),
				"ConfigValidator", "ValidateNetworkConfig", "address format check")
		}
		for _, part := range parts {
			// Simple validation - could be enhanced
			if len(part) == 0 || len(part) > 3 {
				return errors.WrapInvalid(
					fmt.Errorf("invalid bind address segment: %s", part),
					"ConfigValidator", "ValidateNetworkConfig", "address segment check")
			}
		}
	}

	return nil
}
