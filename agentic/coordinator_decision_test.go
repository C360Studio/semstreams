package agentic_test

// gh#1094 / ADR-101 — the coordinator reply vocabulary and the typed decision
// carried on LoopCompletedEvent.
//
// Covers:
//  1. The additive `decision` wire field round-trips through the PRODUCTION
//     decoder (payload registry), and its absence decodes as nil.
//  2. IsUserFacingDecideAction is the one classifier of the reserved reply
//     vocabulary — exact match, no normalisation (owner item 7).
//  3. A PRESENT Decision with an empty Action or Reason fails
//     LoopCompletedEvent.Validate (C4) so the fail-closed terminal normalizer
//     Terms it instead of silently classifying it as a handoff; an unknown but
//     nonempty action stays a valid handoff.

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoopCompletedEventDecisionRoundTrip(t *testing.T) {
	t.Parallel()

	ev := &agentic.LoopCompletedEvent{
		LoopID:      "loop-decide-001",
		TaskID:      "task-decide-001",
		Outcome:     agentic.OutcomeSuccess,
		Role:        "coordinator",
		Result:      `{"action":"respond_direct","reason":"Optimized the flight plan."}`,
		Model:       "model-x",
		CompletedAt: time.Now().UTC().Truncate(time.Second),
		Decision: &agentic.CoordinatorDecision{
			Action: agentic.DecideActionRespondDirect,
			Reason: "Optimized the flight plan.",
		},
	}
	data, err := json.Marshal(message.NewBaseMessage(ev.Schema(), ev, "test"))
	require.NoError(t, err)
	assert.Contains(t, string(data), `"decision"`)

	decoded, err := payloadbuiltins.NewTestDecoder(t).Decode(data)
	require.NoError(t, err)
	got, ok := decoded.Payload().(*agentic.LoopCompletedEvent)
	require.True(t, ok, "expected *agentic.LoopCompletedEvent payload, got %T", decoded.Payload())
	require.NotNil(t, got.Decision)
	assert.Equal(t, agentic.DecideActionRespondDirect, got.Decision.Action)
	assert.Equal(t, "Optimized the flight plan.", got.Decision.Reason)
	assert.Equal(t, ev.Result, got.Result, "Result is unchanged by the typed decision")

	t.Run("absent decision decodes nil", func(t *testing.T) {
		bare := &agentic.LoopCompletedEvent{
			LoopID:      "loop-decide-002",
			TaskID:      "task-decide-002",
			Outcome:     agentic.OutcomeSuccess,
			Role:        "researcher",
			Result:      "plain text terminal",
			Model:       "model-x",
			CompletedAt: time.Now().UTC().Truncate(time.Second),
		}
		bareData, marshalErr := json.Marshal(message.NewBaseMessage(bare.Schema(), bare, "test"))
		require.NoError(t, marshalErr)
		assert.NotContains(t, string(bareData), `"decision"`)

		bareDecoded, decodeErr := payloadbuiltins.NewTestDecoder(t).Decode(bareData)
		require.NoError(t, decodeErr)
		bareGot, bareOK := bareDecoded.Payload().(*agentic.LoopCompletedEvent)
		require.True(t, bareOK, "expected *agentic.LoopCompletedEvent payload, got %T", bareDecoded.Payload())
		assert.Nil(t, bareGot.Decision)
	})
}

func TestIsUserFacingDecideActionTable(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		action string
		want   bool
	}{
		{action: agentic.DecideActionRespondDirect, want: true},
		{action: agentic.DecideActionAskUser, want: true},
		{action: "respond_direct", want: true},
		{action: "ask_user", want: true},
		{action: "autoresearch", want: false},
		{action: "research", want: false},
		{action: "needs_clarification", want: false},
		{action: "", want: false},
		// Exact match (owner item 7): no case folding, no hyphen coercion,
		// no trimming. The decide tool canonicalises only when an
		// action_allowlist is configured.
		{action: "Respond_Direct", want: false},
		{action: "RESPOND_DIRECT", want: false},
		{action: "respond-direct", want: false},
		{action: "Ask_User", want: false},
		{action: "ask-user", want: false},
		{action: " respond_direct", want: false},
		{action: "respond_direct ", want: false},
	} {
		t.Run(tc.action, func(t *testing.T) {
			assert.Equal(t, tc.want, agentic.IsUserFacingDecideAction(tc.action))
		})
	}
}

func TestLoopCompletedEventValidateRejectsPresentDecisionWithEmptyActionOrReason(t *testing.T) {
	t.Parallel()

	base := func() *agentic.LoopCompletedEvent {
		return &agentic.LoopCompletedEvent{
			LoopID:      "loop-validate-001",
			TaskID:      "task-validate-001",
			Outcome:     agentic.OutcomeSuccess,
			CompletedAt: time.Now().UTC(),
		}
	}

	t.Run("empty_action", func(t *testing.T) {
		ev := base()
		ev.Decision = &agentic.CoordinatorDecision{Action: "", Reason: "some reason"}
		require.Error(t, ev.Validate())
	})

	t.Run("empty_reason", func(t *testing.T) {
		ev := base()
		ev.Decision = &agentic.CoordinatorDecision{Action: agentic.DecideActionRespondDirect, Reason: ""}
		require.Error(t, ev.Validate())
	})

	t.Run("unknown_nonempty_action_valid", func(t *testing.T) {
		ev := base()
		ev.Decision = &agentic.CoordinatorDecision{Action: "autoresearch", Reason: "hand off to the chain"}
		require.NoError(t, ev.Validate())
	})

	t.Run("nil_decision_valid", func(t *testing.T) {
		ev := base()
		require.NoError(t, ev.Validate())
	})
}

// TestDecideToolNameHasOneDefinitionInNonTestSources pins the delta scenario
// "one tool-name home" as a source read rather than a claim: exactly one
// non-test Go source may DEFINE the decide tool-name literal, and it must be
// this package. Precedent: the port-grammar census reads sources the same way.
//
// Only `const … = "decide"` definitions count. Operation labels passed to
// errs.Wrap*, executor registration group keys, and e2e mock fixtures are a
// different vocabulary and are deliberately out of scope.
func TestDecideToolNameHasOneDefinitionInNonTestSources(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs("..")
	require.NoError(t, err)
	definition := regexp.MustCompile(`(?m)^\s*(?:const\s+)?\w+\s*(?:=|:=)\s*"decide"\s*$`)

	var definitions []string
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "bin", "worktrees":
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for index, line := range strings.Split(string(data), "\n") {
			if definition.MatchString(line) {
				relative, relErr := filepath.Rel(root, path)
				if relErr != nil {
					relative = path
				}
				definitions = append(definitions, fmt.Sprintf("%s:%d", relative, index+1))
			}
		}
		return nil
	}))

	require.Len(t, definitions, 1, "the decide tool name must have exactly one definition: %v", definitions)
	require.True(t, strings.HasPrefix(definitions[0], "agentic/tools.go:"),
		"the one definition must live in agentic, got %s", definitions[0])
	require.Equal(t, "decide", agentic.DecideToolName)
}
