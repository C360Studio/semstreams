// Package rulepack owns the process-level identity contract for composed rule
// processors. It depends on configuration types and the public NATS KV-key
// contract so lifecycle services can enforce identity without importing the
// rule implementation.
package rulepack

import (
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
)

const (
	// PackIDCharset is the exact ASCII alphabet accepted for rule-pack IDs.
	PackIDCharset = "[A-Za-z0-9_=-]"
	// MaxPackIDBytes preserves the 256-byte owner-key budget after the
	// "rule-pack." prefix.
	MaxPackIDBytes = 246
)

type packIdentity struct {
	PackID string `json:"pack_id"`
}

// ValidateID validates the public, stable rule-pack identity contract.
func ValidateID(packID string) error {
	if packID == "" {
		return fmt.Errorf("rule config: pack_id is required")
	}
	if len(packID) > MaxPackIDBytes {
		return fmt.Errorf(
			"rule config: pack_id is %d bytes; maximum is %d so owner id rule-pack.<pack_id> stays within 256 bytes",
			len(packID), MaxPackIDBytes,
		)
	}
	if err := natsclient.ValidateKVLiteralToken(packID); err != nil {
		return fmt.Errorf(
			"rule config: invalid pack_id %q — owner id rule-pack.%s must use one literal KV token from %s: %w",
			packID, packID, PackIDCharset, err,
		)
	}
	for _, char := range packID {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9':
		case char == '-', char == '_', char == '=':
		default:
			return fmt.Errorf(
				"rule config: invalid pack_id %q — owner id rule-pack.%s must use only %s (offending char %q)",
				packID, packID, PackIDCharset, string(char),
			)
		}
	}
	return nil
}

// ValidateConfig validates every declared rule-pack identity and rejects a
// duplicate among enabled instances. Disabled instances still require a valid
// identity so enabling one can never introduce an unvalidated producer.
func ValidateConfig(cfg *config.Config) error {
	if cfg == nil {
		return nil
	}
	seen := make(map[string]string)
	for instanceName, componentConfig := range cfg.Components {
		if componentConfig.Name != "rule-processor" {
			continue
		}
		packID, err := decodePackID(instanceName, componentConfig)
		if err != nil {
			return err
		}
		if !componentConfig.Enabled {
			continue
		}
		if first, duplicate := seen[packID]; duplicate {
			return fmt.Errorf(
				"duplicate enabled rule pack_id %q in one composition: components %q and %q",
				packID, first, instanceName,
			)
		}
		seen[packID] = instanceName
	}
	return nil
}

// ValidateRuntimeUpdate rejects rule-processor composition changes that cannot
// preserve the static owner binding established before ComponentManager.Start.
func ValidateRuntimeUpdate(instanceName string, previous *types.ComponentConfig, proposed types.ComponentConfig) error {
	if proposed.Name != "rule-processor" {
		return nil
	}
	proposedPackID, err := decodePackID(instanceName, proposed)
	if err != nil {
		return err
	}
	if previous == nil || previous.Name != "rule-processor" || !previous.Enabled {
		if proposed.Enabled {
			return fmt.Errorf(
				"rule processor %q cannot be enabled through component hot reload; restart the process so pack ownership is bound before activation",
				instanceName,
			)
		}
		return nil
	}
	if previous.Equal(proposed) {
		return nil
	}

	previousPackID, err := decodePackID(instanceName, *previous)
	if err != nil {
		return err
	}
	if previousPackID != proposedPackID {
		return fmt.Errorf(
			"rule processor %q pack_id is static for the process lifetime: %q cannot change to %q",
			instanceName, previousPackID, proposedPackID,
		)
	}
	if proposed.Enabled {
		return fmt.Errorf(
			"rule processor %q component config is static after pack ownership is bound; use rule runtime configuration or restart the process",
			instanceName,
		)
	}
	return nil
}

func decodePackID(instanceName string, componentConfig types.ComponentConfig) (string, error) {
	if len(componentConfig.Config) == 0 {
		return "", fmt.Errorf("rule processor %q: config with explicit pack_id is required", instanceName)
	}
	var identity packIdentity
	if err := json.Unmarshal(componentConfig.Config, &identity); err != nil {
		return "", fmt.Errorf("rule processor %q: decode config: %w", instanceName, err)
	}
	if err := ValidateID(identity.PackID); err != nil {
		return "", fmt.Errorf("rule processor %q: %w", instanceName, err)
	}
	return identity.PackID, nil
}
