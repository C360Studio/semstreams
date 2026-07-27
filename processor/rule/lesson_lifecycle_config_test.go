package rule

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/pkg/ownership"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

const lessonLifecycleConfigPath = "../../configs/rules/lessons/lesson-lifecycle-rulepack.json"

// TestLessonLifecycleReferenceConfigIsDeferred verifies the legacy lesson
// example remains unchanged while strict replace_owned selectors are adopted.
// It is intentionally not consumable until its projection ownership can be
// reconciled with the built-in lesson writer.
func TestLessonLifecycleReferenceConfigIsDeferred(t *testing.T) {
	t.Parallel()
	agvocab.Register()

	data, err := os.ReadFile(lessonLifecycleConfigPath)
	require.NoError(t, err, "read reference config")

	var cfg Config
	require.NoError(t, json.Unmarshal(data, &cfg), "reference config must decode into rule.Config")
	require.NoError(t, cfg.Validate(), "reference config must pass Config.Validate")

	// The owned group is exactly the three MUTABLE lifecycle predicates.
	require.Len(t, cfg.ProjectionContracts, 1)
	groups := cfg.ProjectionContracts[0].Groups
	require.Len(t, groups, 1)
	require.Equal(t, ownership.ModeReplaceOwned, groups[0].Mode)
	require.ElementsMatch(t,
		[]string{agvocab.LessonStatus, agvocab.LessonSupersededBy, agvocab.LessonRetiredAt},
		groups[0].Predicates,
		"owned group must be exactly the mutable lifecycle predicates")

	// The immutable birth predicates must NOT be owned — a replace_owned action
	// must never be able to overwrite them.
	for _, birth := range []string{agvocab.LessonCreatedAt, agvocab.LessonCategory, agvocab.LessonEvidence, agvocab.LessonInjectionForm} {
		require.NotContains(t, groups[0].Predicates, birth,
			"immutable birth predicate %q must be excluded from the owned group", birth)
	}

	rp, err := NewProcessor(nil, &cfg)
	require.NoError(t, err, "NewProcessor")
	err = rp.loadRules()
	require.ErrorContains(t, err, "projection_contract is required")
}
