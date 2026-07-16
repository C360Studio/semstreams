package rule

import (
	"encoding/json"
	"strings"
	"testing"

	gtypes "github.com/c360studio/semstreams/graph"
	entitytypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/stretchr/testify/require"
	"github.com/xeipuuv/gojsonschema"
)

func TestRuleProcessorSchemaDescribesPackProducerIdentity(t *testing.T) {
	t.Parallel()

	packID := schema.Properties["pack_id"]
	require.Contains(t, strings.ToLower(packID.Description), "graph-event producer identity")
	require.NotNil(t, packID.MaxLength)
	require.Equal(t, maxRulePackIDBytes, *packID.MaxLength)
	require.NotNil(t, packID.MinLength)
	require.Equal(t, 1, *packID.MinLength)
	require.Equal(t, packIDPattern, packID.Pattern)
	require.Contains(t, schema.Required, "pack_id")
	integration := schema.Properties["enable_graph_integration"]
	require.Equal(t, false, integration.Default)

	assertConfigSchemaValidation(t, map[string]any{
		"enable_graph_integration": false,
	}, false)
	assertConfigSchemaValidation(t, map[string]any{
		"enable_graph_integration": false,
		"pack_id":                  "non-graph-pack",
	}, true)
	assertConfigSchemaValidation(t, map[string]any{
		"enable_graph_integration": true,
	}, false)
	assertConfigSchemaValidation(t, map[string]any{
		"enable_graph_integration": true,
		"pack_id":                  "contract-pack",
	}, true)
	assertConfigSchemaValidation(t, map[string]any{
		"enable_graph_integration": true,
		"pack_id":                  strings.Repeat("p", maxRulePackIDBytes+1),
	}, false)
	assertConfigSchemaValidation(t, map[string]any{
		"enable_graph_integration": true,
		"pack_id":                  "",
	}, false)
	assertConfigSchemaValidation(t, map[string]any{
		"enable_graph_integration": true,
		"pack_id":                  "bad:pack",
	}, false)
	assertConfigSchemaValidation(t, map[string]any{
		"enable_graph_integration": true,
		"pack_id":                  strings.Repeat("é", maxRulePackIDBytes),
	}, false)
}

func TestRuleProcessorSchemaConstrainsEntityWatchBuckets(t *testing.T) {
	t.Parallel()

	property := schema.Properties["entity_watch_buckets"]
	require.NotNil(t, property.AdditionalProperties)
	require.False(t, *property.AdditionalProperties)
	patterns := property.Properties[gtypes.BucketEntityStates]
	require.NotNil(t, patterns.Items)
	require.Equal(t, entitytypes.EntityIDDeclarationPattern, patterns.Items.Pattern)
	require.NotNil(t, patterns.Items.MaxLength)
	require.Equal(t, entitytypes.MaxEntityIDBytes, *patterns.Items.MaxLength)

	assertSchemaValidation(t, property, map[string]any{
		gtypes.BucketEntityStates: []any{"acme.*.robotics.gcs.drone.*"},
	}, true)
	assertSchemaValidation(t, property, map[string]any{
		gtypes.BucketEntityStates: []any{">"},
	}, false)
	assertSchemaValidation(t, property, map[string]any{
		"AGENT_LOOPS": []any{"COMPLETE_*"},
	}, false)
}

func TestRuleProcessorSchemaConstrainsInlineEntityDeclaration(t *testing.T) {
	t.Parallel()

	inlineRules := schema.Properties["inline_rules"]
	require.NotNil(t, inlineRules.Items)
	entity := inlineRules.Items.Properties["entity"]
	require.Equal(t, []string{"pattern"}, entity.Required)
	pattern := entity.Properties["pattern"]
	require.Equal(t, entitytypes.EntityIDDeclarationPattern, pattern.Pattern)
	require.NotNil(t, pattern.MaxLength)
	require.Equal(t, entitytypes.MaxEntityIDBytes, *pattern.MaxLength)
	buckets := entity.Properties["watch_buckets"]
	require.NotNil(t, buckets.Items)
	require.Equal(t, []string{gtypes.BucketEntityStates}, buckets.Items.Enum)

	assertSchemaValidation(t, entity, map[string]any{
		"pattern":       "acme.*.robotics.gcs.drone.*",
		"watch_buckets": []any{gtypes.BucketEntityStates},
	}, true)
	assertSchemaValidation(t, entity, map[string]any{
		"pattern":       "loop.agentic.task.*",
		"watch_buckets": []any{"AGENT_LOOPS"},
	}, false)
	assertSchemaValidation(t, entity, map[string]any{
		"watch_buckets": []any{gtypes.BucketEntityStates},
	}, false)
}

func assertSchemaValidation(t *testing.T, property any, document any, wantValid bool) {
	t.Helper()
	schemaJSON, err := json.Marshal(property)
	require.NoError(t, err)
	documentJSON, err := json.Marshal(document)
	require.NoError(t, err)
	result, err := gojsonschema.Validate(
		gojsonschema.NewBytesLoader(schemaJSON),
		gojsonschema.NewBytesLoader(documentJSON),
	)
	require.NoError(t, err)
	require.Equal(t, wantValid, result.Valid(), "schema errors: %v", result.Errors())
}

func assertConfigSchemaValidation(t *testing.T, document any, wantValid bool) {
	t.Helper()
	packID := schema.Properties["pack_id"]
	integration := schema.Properties["enable_graph_integration"]
	schemaJSON, err := json.Marshal(map[string]any{
		"type":     "object",
		"required": schema.Required,
		"properties": map[string]any{
			"pack_id": map[string]any{
				"type":      "string",
				"minLength": packID.MinLength,
				"maxLength": packID.MaxLength,
				"pattern":   packID.Pattern,
			},
			"enable_graph_integration": map[string]any{
				"type":    "boolean",
				"default": integration.Default,
			},
		},
	})
	require.NoError(t, err)
	documentJSON, err := json.Marshal(document)
	require.NoError(t, err)
	result, err := gojsonschema.Validate(
		gojsonschema.NewBytesLoader(schemaJSON),
		gojsonschema.NewBytesLoader(documentJSON),
	)
	require.NoError(t, err)
	require.Equal(t, wantValid, result.Valid(), "schema errors: %v", result.Errors())
}
