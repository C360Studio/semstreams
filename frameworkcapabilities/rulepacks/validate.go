// Package rulepacks validates rule-pack identities at the composition boundary.
package rulepacks

import (
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/config"
	rulepackcontract "github.com/c360studio/semstreams/pkg/rulepack"
	ruleprocessor "github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/types"
)

// ValidateConfig rejects invalid or duplicate enabled rule processors before
// the component manager constructs any component. Separate Config values are
// separate compositions, so replicas may reuse the same stable PackID.
func ValidateConfig(cfg *config.Config) error {
	if err := rulepackcontract.ValidateConfig(cfg); err != nil {
		return err
	}
	if cfg == nil {
		return nil
	}
	for instanceName, componentConfig := range cfg.Components {
		if componentConfig.Name != "rule-processor" {
			continue
		}
		var ruleConfig ruleprocessor.Config
		if err := json.Unmarshal(componentConfig.Config, &ruleConfig); err != nil {
			return fmt.Errorf("rule processor %q: decode config: %w", instanceName, err)
		}
		if err := ruleConfig.Validate(); err != nil {
			return fmt.Errorf("rule processor %q: %w", instanceName, err)
		}
	}
	return nil
}

// ValidateRuntimeUpdate rejects rule-processor composition changes that cannot
// preserve the static owner binding established before ComponentManager.Start.
// Rule definitions and the explicitly supported runtime fields have their own
// processor-local update path; replacing or newly enabling the component would
// construct an unbound processor after BindRulePackContracts has already run.
func ValidateRuntimeUpdate(instanceName string, previous *types.ComponentConfig, proposed types.ComponentConfig) error {
	return rulepackcontract.ValidateRuntimeUpdate(instanceName, previous, proposed)
}
