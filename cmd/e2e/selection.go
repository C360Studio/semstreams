package main

import "fmt"

// scenarioSelection is the command's resolved execution scope. The CLI owns
// selection; individual scenarios own the behavioral checks they declare.
type scenarioSelection struct {
	flags    cliFlags
	label    string
	members  []string
	required bool
}

func resolveScenarioSelection(input *cliFlags) (scenarioSelection, error) {
	if input == nil {
		return scenarioSelection{}, fmt.Errorf("scenario flags are required")
	}
	selection := scenarioSelection{flags: *input}
	name := input.scenarioName
	variant := input.variant

	switch name {
	case "", "all":
		if variant != "" {
			return scenarioSelection{}, fmt.Errorf("variant %q does not apply to core scenarios", variant)
		}
		selection.flags.scenarioName = "all"
		selection.label = "core-scenarios"
		selection.members = []string{"core-health", "core-dataflow"}
		selection.required = true
	case "rules", "structural", "statistical", "semantic", "tiered":
		resolvedVariant := variant
		if name != "tiered" {
			resolvedVariant = name
			if name == "rules" {
				resolvedVariant = "structural"
			}
			if variant != "" && variant != resolvedVariant {
				return scenarioSelection{}, fmt.Errorf("scenario %q conflicts with variant %q", name, variant)
			}
		}
		if name == "tiered" && resolvedVariant == "semantic-fallback" {
			selection.flags.variant = resolvedVariant
			selection.label = "semantic-fallback"
			selection.members = []string{"tiered:semantic-fallback"}
			return selection, nil
		}
		switch resolvedVariant {
		case "structural", "statistical", "semantic":
		default:
			return scenarioSelection{}, fmt.Errorf("tiered scenario requires structural, statistical or semantic variant; got %q", resolvedVariant)
		}
		selection.flags.scenarioName = "tiered"
		selection.flags.variant = resolvedVariant
		selection.label = resolvedVariant
		selection.members = []string{"tiered:" + resolvedVariant}
		selection.required = true
	default:
		if variant != "" {
			return scenarioSelection{}, fmt.Errorf("variant %q does not apply to scenario %q", variant, name)
		}
		switch name {
		case "health":
			selection.members = []string{"core-health"}
			selection.label = "core-health"
			selection.required = true
		case "dataflow":
			selection.members = []string{"core-dataflow"}
			selection.label = "core-dataflow"
			selection.required = true
		case "graph-roundtrip":
			selection.members = []string{"core-graph-roundtrip"}
			selection.label = "core-graph-roundtrip"
			selection.required = true
		case "slow-consumer":
			selection.members = []string{"core-slow-consumer"}
			selection.label = "core-slow-consumer"
			selection.required = true
		case "core-health", "core-dataflow", "core-graph-roundtrip", "agentic", "core-slow-consumer":
			selection.members = []string{name}
			selection.label = name
			selection.required = true
		case "minted-authority", "core-minted-authority":
			selection.flags.scenarioName = "core-minted-authority"
			selection.members = []string{"core-minted-authority"}
			selection.label = "core-minted-authority"
			selection.required = true
		case "core-pre-identity-seed":
			selection.members = []string{"core-pre-identity-bucket-seed"}
			selection.label = "core-pre-identity-bucket-seed"
		case "core-pre-identity-assert":
			selection.members = []string{"core-pre-identity-bucket-assert"}
			selection.label = "core-pre-identity-bucket-assert"
			selection.required = true
		default:
			selection.members = []string{name}
			selection.label = name
		}
	}
	return selection, nil
}
