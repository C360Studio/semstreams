// Package logforwarderpolicy owns the repository-internal semantics of the
// log-forwarder service's inner configuration.
package logforwarderpolicy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

const mandatoryWebSocketExclusion = "flow-service.websocket"

// Policy is the normalized log-forwarder policy consumed by boot composition.
type Policy struct {
	MinLevel       slog.Level
	ExcludeSources []string
}

type wireConfig struct {
	MinLevel       string   `json:"min_level"`
	ExcludeSources []string `json:"exclude_sources"`
}

// Resolve decodes, defaults, normalizes, and validates enabled log-forwarder
// inner configuration. Callers must check outer activation before invoking it.
func Resolve(raw json.RawMessage) (Policy, error) {
	cfg := wireConfig{}
	if len(bytes.TrimSpace(raw)) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&cfg); err != nil {
			return Policy{}, fmt.Errorf("decode log-forwarder config: %w", err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			if err == nil {
				return Policy{}, fmt.Errorf("decode log-forwarder config: multiple JSON values")
			}
			return Policy{}, fmt.Errorf("decode log-forwarder config: %w", err)
		}
	}

	if strings.TrimSpace(cfg.MinLevel) == "" {
		cfg.MinLevel = "INFO"
	}
	cfg.MinLevel = strings.ToUpper(strings.TrimSpace(cfg.MinLevel))
	cfg.ExcludeSources = normalizeExclusions(cfg.ExcludeSources)
	if err := ValidateFields(cfg.MinLevel, cfg.ExcludeSources); err != nil {
		return Policy{}, err
	}

	return Policy{
		MinLevel:       parseLevel(cfg.MinLevel),
		ExcludeSources: cfg.ExcludeSources,
	}, nil
}

// ValidateFields validates the public service type's field semantics without
// applying decode-time defaults or normalization.
func ValidateFields(minLevel string, _ []string) error {
	switch minLevel {
	case "DEBUG", "INFO", "WARN", "ERROR":
		return nil
	default:
		return fmt.Errorf("invalid log level: %s (must be DEBUG, INFO, WARN, or ERROR)", minLevel)
	}
}

func normalizeExclusions(configured []string) []string {
	result := make([]string, 0, len(configured)+1)
	seen := make(map[string]struct{}, len(configured)+1)
	appendUnique := func(source string) {
		source = strings.TrimSpace(source)
		if source == "" {
			return
		}
		if _, exists := seen[source]; exists {
			return
		}
		seen[source] = struct{}{}
		result = append(result, source)
	}

	appendUnique(mandatoryWebSocketExclusion)
	for _, source := range configured {
		appendUnique(source)
	}
	return result
}

func parseLevel(level string) slog.Level {
	switch level {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
