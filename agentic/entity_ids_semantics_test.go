package agentic_test

import (
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	gateddagexec "github.com/c360studio/semstreams/processor/gated-dag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFrameworkBuildersMintUnderDeploymentAuthorityInCanonicalOrder pins, for
// every framework identity family, that positions 1-2 are the supplied
// deployment authority and positions 3-4 are `<component>.<reserved-domain>`
// in the canonical org.platform.system.domain.type.instance order (ADR-102
// d1, d2, d4). ADR-076's fixed `semstreams.framework` namespace is gone.
func TestFrameworkBuildersMintUnderDeploymentAuthorityInCanonicalOrder(t *testing.T) {
	t.Parallel()

	const org, platform = "acme", "dep1"
	metadata := graph.EventMetadata{
		RuleName:  "battery-rule",
		Timestamp: time.Date(2026, time.July, 16, 12, 34, 56, 789123456, time.UTC),
		Source:    "rule-processor",
		Reason:    "battery below threshold",
	}
	source := agentic.LoopExecutionEntityID(org, platform, "src-loop")

	tests := []struct {
		name     string
		build    func(t *testing.T) string
		wantMid  string // positions 3-5: system.domain.type
		wantLeaf string // position 6 when fixed by the fixture, "" when derived
	}{
		{"model endpoint", func(*testing.T) string { return agentic.ModelEndpointEntityID(org, platform, "claude-sonnet") }, "model-registry.agent.endpoint", "claude-sonnet"},
		{"loop execution", func(*testing.T) string { return agentic.LoopExecutionEntityID(org, platform, "abc123") }, "agentic-loop.agent.execution", "abc123"},
		{"loop execution (Try)", func(t *testing.T) string {
			id, err := agentic.TryLoopExecutionEntityID(org, platform, "abc123")
			require.NoError(t, err)
			return id
		}, "agentic-loop.agent.execution", "abc123"},
		{"chain execution", func(*testing.T) string { return agentic.ChainExecutionEntityID(org, platform, "chain1") }, "chain.agent.execution", "chain1"},
		{"chain execution (Try)", func(t *testing.T) string {
			id, err := agentic.TryChainExecutionEntityID(org, platform, "chain1")
			require.NoError(t, err)
			return id
		}, "chain.agent.execution", "chain1"},
		{"lesson", func(*testing.T) string { return agentic.AgentLessonEntityID(org, platform, "lesson-1") }, "lesson.agent.record", "lesson-1"},
		{"web observation", func(t *testing.T) string {
			id, _, err := agentic.TryWebObservationEntityID(org, platform, "https://example.org/a")
			require.NoError(t, err)
			return id
		}, "web.agent.observation", ""},
		{"ops diagnosis", func(*testing.T) string { return agentic.OpsDiagnosisEntityID(org, platform, "diag-1") }, "diagnosis.ops.finding", "diag-1"},
		{"rule alert", func(t *testing.T) string {
			event, err := graph.NewAlertEvent(org, platform, "battery_low", source, map[string]any{"value": 10}, metadata)
			require.NoError(t, err)
			return event.EntityID
		}, "rules.graph.alert", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			id := tt.build(t)
			require.NoError(t, semtypes.ValidateEntityID(id), id)
			parsed, err := semtypes.ParseEntityID(id)
			require.NoError(t, err)
			assert.Equal(t, org, parsed.Org, id)
			assert.Equal(t, platform, parsed.Platform, id)
			assert.Equal(t, tt.wantMid, parsed.System+"."+parsed.Domain+"."+parsed.Type, id)
			assert.True(t, semtypes.IsFrameworkEntityDomain(parsed.Domain), "domain %q is not framework-reserved: %s", parsed.Domain, id)
			if tt.wantLeaf != "" {
				assert.Equal(t, tt.wantLeaf, parsed.Instance, id)
			}
			assert.False(t, strings.Contains(id, "semstreams.framework"), "ADR-076 fixed namespace survives in %s", id)
		})
	}
}

// TestFrameworkPrefixesAndPatternsFollowCanonicalOrder pins the record prefix,
// the gated-DAG fan-out pattern (re-slotted under `agent`, O-9), and the
// loop-id extractor against the canonical order.
func TestFrameworkPrefixesAndPatternsFollowCanonicalOrder(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "acme.dep1.lesson.agent.record", agentic.AgentLessonRecordPrefix("acme", "dep1"))
	require.NoError(t, semtypes.ValidateEntityIDPrefix(agentic.AgentLessonRecordPrefix("acme", "dep1")))
	assert.True(t, strings.HasPrefix(agentic.AgentLessonEntityID("acme", "dep1", "x"), agentic.AgentLessonRecordPrefix("acme", "dep1")+"."))

	assert.Equal(t, "*.*.gated-dag.agent.fanout.*", gateddagexec.FanOutEntityIDPattern)
	matched, err := semtypes.MatchEntityIDPattern(gateddagexec.FanOutEntityIDPattern, "acme.dep1.gated-dag.agent.fanout.f1")
	require.NoError(t, err)
	assert.True(t, matched)

	loopID, ok := agentic.LoopIDFromExecutionEntityID("acme.dep1.agentic-loop.agent.execution.abc123")
	assert.True(t, ok)
	assert.Equal(t, "abc123", loopID)
	_, ok = agentic.LoopIDFromExecutionEntityID("acme.dep1.agent.agentic-loop.execution.abc123")
	assert.False(t, ok, "the retired order must not be admitted as a loop execution")
}

// TestAlertIdentityCarriesTheDeploymentAuthority pins that two deployments
// running the same rule on the same source mint DIFFERENT alert entities —
// the collision ADR-076 d1's fixed namespace produced (inventory PF-7).
func TestAlertIdentityCarriesTheDeploymentAuthority(t *testing.T) {
	t.Parallel()

	metadata := graph.EventMetadata{RuleName: "r", Timestamp: time.Unix(1_700_000_000, 0).UTC(), Source: "rule-processor"}
	source := agentic.LoopExecutionEntityID("acme", "dep1", "loop")
	first, err := graph.NewAlertEvent("acme", "dep1", "threshold", source, nil, metadata)
	require.NoError(t, err)
	second, err := graph.NewAlertEvent("acme", "dep2", "threshold", source, nil, metadata)
	require.NoError(t, err)
	assert.NotEqual(t, first.EntityID, second.EntityID)
	assert.True(t, strings.HasPrefix(first.EntityID, "acme.dep1.rules.graph.alert."), first.EntityID)
	assert.True(t, strings.HasPrefix(second.EntityID, "acme.dep2.rules.graph.alert."), second.EntityID)
	assert.Equal(t, len("acme")+len("dep1")+semtypes.RuleAlertIdentityFamily().FixedBytes(), len(first.EntityID))

	_, err = graph.NewAlertEvent("", "dep1", "threshold", source, nil, metadata)
	require.Error(t, err, "an empty authority fails closed")
	_, err = graph.NewAlertEvent("acme", "de.p1", "threshold", source, nil, metadata)
	require.Error(t, err, "a dotted authority fails closed")
}
