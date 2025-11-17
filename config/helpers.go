package config

import (
	"fmt"
)

// Safe type assertion helpers prevent panics when accessing dynamic configuration

// GetString safely extracts a string value from a config map
func GetString(cfg map[string]any, key string, defaultVal string) string {
	if val, ok := cfg[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return defaultVal
}

// GetInt safely extracts an integer value from a config map
func GetInt(cfg map[string]any, key string, defaultVal int) int {
	if val, ok := cfg[key]; ok {
		switch v := val.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case int32:
			return int(v)
		case float64:
			return int(v)
		case float32:
			return int(v)
		}
	}
	return defaultVal
}

// GetFloat64 safely extracts a float64 value from a config map
func GetFloat64(cfg map[string]any, key string, defaultVal float64) float64 {
	if val, ok := cfg[key]; ok {
		switch v := val.(type) {
		case float64:
			return v
		case float32:
			return float64(v)
		case int:
			return float64(v)
		case int64:
			return float64(v)
		case int32:
			return float64(v)
		}
	}
	return defaultVal
}

// GetBool safely extracts a boolean value from a config map
func GetBool(cfg map[string]any, key string, defaultVal bool) bool {
	if val, ok := cfg[key]; ok {
		if boolVal, ok := val.(bool); ok {
			return boolVal
		}
	}
	return defaultVal
}

// GetStringSlice safely extracts a string slice from a config map
func GetStringSlice(cfg map[string]any, key string, defaultVal []string) []string {
	if val, ok := cfg[key]; ok {
		if slice, ok := val.([]string); ok {
			return slice
		}
		// Try to convert []any to []string
		if interfaceSlice, ok := val.([]any); ok {
			result := make([]string, 0, len(interfaceSlice))
			for _, item := range interfaceSlice {
				if str, ok := item.(string); ok {
					result = append(result, str)
				}
			}
			if len(result) == len(interfaceSlice) {
				return result
			}
		}
	}
	return defaultVal
}

// GetComponentConfig safely extracts a component configuration section
func GetComponentConfig(cfg map[string]any, name string) (map[string]any, error) {
	val, ok := cfg["components"]
	if !ok {
		return nil, fmt.Errorf("components section not found")
	}

	components, ok := val.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("components section invalid type: expected map[string]any, got %T", val)
	}

	compCfg, ok := components[name]
	if !ok {
		return nil, fmt.Errorf("component %s not found", name)
	}

	result, ok := compCfg.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("component %s config invalid type: expected map[string]any, got %T", name, compCfg)
	}

	return result, nil
}

// GetNestedString safely extracts a nested string value from a config map
func GetNestedString(cfg map[string]any, keys []string, defaultVal string) string {
	current := cfg
	for i, key := range keys {
		val, ok := current[key]
		if !ok {
			return defaultVal
		}

		// If this is the last key, try to extract the string value
		if i == len(keys)-1 {
			if str, ok := val.(string); ok {
				return str
			}
			return defaultVal
		}

		// Otherwise, descend into the nested map
		nested, ok := val.(map[string]any)
		if !ok {
			return defaultVal
		}
		current = nested
	}
	return defaultVal
}

// GetNestedInt safely extracts a nested integer value from a config map
func GetNestedInt(cfg map[string]any, keys []string, defaultVal int) int {
	current := cfg
	for i, key := range keys {
		val, ok := current[key]
		if !ok {
			return defaultVal
		}

		// If this is the last key, try to extract the int value
		if i == len(keys)-1 {
			return GetInt(map[string]any{key: val}, key, defaultVal)
		}

		// Otherwise, descend into the nested map
		nested, ok := val.(map[string]any)
		if !ok {
			return defaultVal
		}
		current = nested
	}
	return defaultVal
}

// GetNestedBool safely extracts a nested boolean value from a config map
func GetNestedBool(cfg map[string]any, keys []string, defaultVal bool) bool {
	current := cfg
	for i, key := range keys {
		val, ok := current[key]
		if !ok {
			return defaultVal
		}

		// If this is the last key, try to extract the bool value
		if i == len(keys)-1 {
			if boolVal, ok := val.(bool); ok {
				return boolVal
			}
			return defaultVal
		}

		// Otherwise, descend into the nested map
		nested, ok := val.(map[string]any)
		if !ok {
			return defaultVal
		}
		current = nested
	}
	return defaultVal
}

// HasKey checks if a key exists in the config map
func HasKey(cfg map[string]any, key string) bool {
	_, ok := cfg[key]
	return ok
}

// HasNestedKey checks if a nested key path exists in the config map
func HasNestedKey(cfg map[string]any, keys []string) bool {
	current := cfg
	for i, key := range keys {
		val, ok := current[key]
		if !ok {
			return false
		}

		// If this is the last key, we found it
		if i == len(keys)-1 {
			return true
		}

		// Otherwise, descend into the nested map
		nested, ok := val.(map[string]any)
		if !ok {
			return false
		}
		current = nested
	}
	return true
}
