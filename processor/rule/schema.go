package rule

import (
	"reflect"

	"github.com/c360studio/semstreams/component"
	gtypes "github.com/c360studio/semstreams/graph"
	entitytypes "github.com/c360studio/semstreams/pkg/types"
)

func buildRuleProcessorSchema() component.ConfigSchema {
	schema := component.GenerateConfigSchema(reflect.TypeOf(Config{}))
	maximumPackIDBytes := maxRulePackIDBytes
	minimumPackIDBytes := 1
	packID := schema.Properties["pack_id"]
	packID.MinLength = &minimumPackIDBytes
	packID.MaxLength = &maximumPackIDBytes
	packID.Pattern = packIDPattern
	schema.Properties["pack_id"] = packID
	schema.Required = appendRequiredProperty(schema.Required, "pack_id")

	maximumEntityIDBytes := entitytypes.MaxEntityIDBytes
	disallowAdditional := false
	entityPattern := component.PropertySchema{
		Type:        "string",
		Description: "Exact six-position entity ID pattern; each position is a canonical literal segment or *.",
		MaxLength:   &maximumEntityIDBytes,
		Pattern:     entitytypes.EntityIDDeclarationPattern,
	}
	entityBucketNames := component.PropertySchema{
		Type:        "array",
		Description: "Typed state buckets consumed by this EntityState evaluator; only ENTITY_STATES is supported.",
		Items: &component.PropertySchema{
			Type: "string",
			Enum: []string{gtypes.BucketEntityStates},
		},
	}

	schema.Properties["entity_watch_buckets"] = component.PropertySchema{
		Type:        "object",
		Description: "ENTITY_STATES watch patterns for the rule processor's typed EntityState evaluator.",
		Properties: map[string]component.PropertySchema{
			gtypes.BucketEntityStates: {
				Type:        "array",
				Description: "Exact six-position entity ID patterns.",
				Items:       &entityPattern,
			},
		},
		AdditionalProperties: &disallowAdditional,
		Category:             "advanced",
	}

	inlineRules := schema.Properties["inline_rules"]
	if inlineRules.Items != nil {
		entity := inlineRules.Items.Properties["entity"]
		entity.Properties = map[string]component.PropertySchema{
			"pattern":       entityPattern,
			"watch_buckets": entityBucketNames,
		}
		entity.Required = []string{"pattern"}
		entity.AdditionalProperties = &disallowAdditional
		inlineRules.Items.Properties["entity"] = entity
		schema.Properties["inline_rules"] = inlineRules
	}

	// Add "rules" property for runtime-only dynamic rule definitions. This
	// field is absent from Config because it exists only on the update surface.
	schema.Properties["rules"] = component.PropertySchema{
		Type:        "object",
		Description: "Dynamic rule definitions (rules.{rule_id} pattern)",
		Default:     map[string]interface{}{},
		Category:    "advanced",
	}

	return schema
}

func appendRequiredProperty(required []string, property string) []string {
	for _, existing := range required {
		if existing == property {
			return required
		}
	}
	return append(required, property)
}
