package rule

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/pkg/ownership"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// The production binaries register the agentic vocabulary at boot
// (agvocab.Register() in cmd/*/main.go) before any rule config loads, so
// condition fields like agent.lesson.category resolve as declared canonical
// predicates. Mirror that here in a package init (runs once, before any
// parallel test reads the global registry — no race).
func init() {
	agvocab.Register()
}

const lessonLifecycleConfigPath = "../../configs/rules/lessons/lesson-lifecycle-rulepack.json"

// TestLessonLifecycleReferenceConfig proves the shipped lesson-lifecycle rule
// pack loads through the PRODUCTION Config decoder and that its replace_owned
// action on agent.lesson.status is inside the pack's declared owned envelope
// (reference-configs-verify discipline: an example config must be provably
// valid, not just illustrative). It also verifies the owned group matches the
// vocab consts and EXCLUDES the immutable birth predicates.
func TestLessonLifecycleReferenceConfig(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(lessonLifecycleConfigPath)
	require.NoError(t, err, "read reference config")

	var cfg Config
	require.NoError(t, json.Unmarshal(data, &cfg), "reference config must decode into rule.Config")
	require.NoError(t, cfg.Validate(), "reference config must pass Config.Validate")

	// The owned group is exactly the three MUTABLE lifecycle predicates.
	require.Len(t, cfg.ProjectionContracts, 1)
	groups := cfg.ProjectionContracts[0].Groups
	require.Len(t, groups, 1)
	assert.Equal(t, ownership.ModeReplaceOwned, groups[0].Mode)
	assert.ElementsMatch(t,
		[]string{agvocab.LessonStatus, agvocab.LessonSupersededBy, agvocab.LessonRetiredAt},
		groups[0].Predicates,
		"owned group must be exactly the mutable lifecycle predicates")

	// The immutable birth predicates must NOT be owned — a replace_owned action
	// must never be able to overwrite them.
	for _, birth := range []string{agvocab.LessonCreatedAt, agvocab.LessonCategory, agvocab.LessonEvidence, agvocab.LessonInjectionForm} {
		assert.NotContains(t, groups[0].Predicates, birth,
			"immutable birth predicate %q must be excluded from the owned group", birth)
	}

	// The replace_owned rule loads cleanly (its predicate is in-envelope).
	rp, err := NewProcessor(nil, &cfg)
	require.NoError(t, err, "NewProcessor")
	require.NoError(t, rp.loadRules(), "the replace_owned rule must be inside the pack's owned envelope")
	_, loaded := rp.rules["auto-retire-suppressed-lesson-at-birth"]
	assert.True(t, loaded, "the replace_owned lesson-lifecycle rule must be registered")
}

// TestLessonLifecycleReferenceConfig_BirthPredicateOutOfEnvelope proves the
// envelope guard bites: a replace_owned action retargeted at an IMMUTABLE birth
// predicate (agent.lesson.category) HARD-FAILS the load, so an operator cannot
// use the mechanical lane to clobber a lesson's identity facts.
func TestLessonLifecycleReferenceConfig_BirthPredicateOutOfEnvelope(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(lessonLifecycleConfigPath)
	require.NoError(t, err)
	var cfg Config
	require.NoError(t, json.Unmarshal(data, &cfg))

	// Retarget the rule's replace_owned action at an immutable birth predicate.
	cfg.InlineRules[0].OnEnter[0].Predicate = agvocab.LessonCategory

	rp, err := NewProcessor(nil, &cfg)
	require.NoError(t, err)
	err = rp.loadRules()
	require.Error(t, err, "replace_owned on an immutable birth predicate must HARD-FAIL the load")
	assert.Contains(t, err.Error(), agvocab.LessonCategory)
}
