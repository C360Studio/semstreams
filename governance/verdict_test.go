package governance_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/governance"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
)

func TestVerdictEvent_Validate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		ev      governance.VerdictEvent
		wantErr bool
	}{
		{"valid deny", governance.VerdictEvent{Decision: governance.DecisionDeny, RuleID: "r1"}, false},
		{"valid approve", governance.VerdictEvent{Decision: governance.DecisionApprove, RuleID: "r1"}, false},
		{"bad decision", governance.VerdictEvent{Decision: "maybe", RuleID: "r1"}, true},
		{"empty decision", governance.VerdictEvent{RuleID: "r1"}, true},
		{"empty rule_id", governance.VerdictEvent{Decision: governance.DecisionDeny}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.ev.Validate()
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRuleToken_SubjectSafe is the load-bearing guard: rule IDs are free-form
// config strings that can contain the NATS token separator, wildcards, or
// spaces. The token MUST be subject-safe (hex only) regardless of input so it
// can never break subject publishing or filtering.
func TestRuleToken_SubjectSafe(t *testing.T) {
	t.Parallel()
	adversarial := []string{
		"role-gate-rule",
		"architect_complete_spawn_editor",
		"rule.with.dots",
		"rule with spaces",
		"rule*with>wildcards",
		"product.ns.rule",
		"",
		">",
		"*",
	}
	const hexDigits = "0123456789abcdef"
	for _, id := range adversarial {
		tok := governance.RuleToken(id)
		assert.Len(t, tok, 16, "token must be a fixed 16-char hex string for %q", id)
		for _, r := range tok {
			assert.True(t, strings.ContainsRune(hexDigits, r),
				"token for %q must be hex-only, got %q", id, tok)
		}
		assert.NotContains(t, tok, ".", "token must not contain the NATS separator")
	}
}

func TestRuleToken_Deterministic(t *testing.T) {
	t.Parallel()
	assert.Equal(t, governance.RuleToken("role-gate-rule"), governance.RuleToken("role-gate-rule"))
	assert.NotEqual(t, governance.RuleToken("rule-a"), governance.RuleToken("rule-b"),
		"distinct rule IDs should (overwhelmingly) get distinct tokens")
}

func TestVerdictSubject_FourSegments(t *testing.T) {
	t.Parallel()
	// A rule ID full of subject-hostile characters must still yield a clean
	// 4-token subject: governance . verdict . <decision> . <rule_token>.
	subj := governance.VerdictSubject(governance.DecisionDeny, "rule.with.dots and *")
	parts := strings.Split(subj, ".")
	require.Len(t, parts, 4, "verdict subject must have exactly 4 dot-separated tokens, got %q", subj)
	assert.Equal(t, "governance", parts[0])
	assert.Equal(t, "verdict", parts[1])
	assert.Equal(t, governance.DecisionDeny, parts[2])
	assert.Equal(t, governance.RuleToken("rule.with.dots and *"), parts[3])
	assert.True(t, strings.HasPrefix(subj, governance.SubjectPrefix+"."))
}

// TestVerdictEvent_ProductionRoundTrip wraps a verdict event in a BaseMessage
// envelope and decodes it through the production registry decoder (the real
// payloadbuiltins registration), proving an audit/ops reader can read verdict
// events off the wire — the §2 semantic-envelope requirement.
func TestVerdictEvent_ProductionRoundTrip(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 6, 12, 10, 30, 0, 0, time.UTC)
	original := &governance.VerdictEvent{
		Decision:  governance.DecisionDeny,
		RuleID:    "role-gate-rule",
		Reason:    "user viewer-1 is not admin",
		EntityID:  "acme.ops.test.svc.entity.001",
		Timestamp: ts,
		LoopID:    "loop-abc",
		CallID:    "call-001",
	}
	baseMsg := message.NewBaseMessage(original.Schema(), original, "rule_engine")
	data, err := json.Marshal(baseMsg)
	require.NoError(t, err)

	decoder := payloadbuiltins.NewTestDecoder(t)
	decoded, err := decoder.Decode(data)
	require.NoError(t, err)
	require.Equal(t, "governance.verdict.v1", decoded.Type().Key())

	got, ok := decoded.Payload().(*governance.VerdictEvent)
	require.True(t, ok, "decoded payload must be a *governance.VerdictEvent, got %T", decoded.Payload())
	assert.Equal(t, original.Decision, got.Decision)
	assert.Equal(t, original.RuleID, got.RuleID)
	assert.Equal(t, original.Reason, got.Reason)
	assert.Equal(t, original.EntityID, got.EntityID)
	assert.Equal(t, original.LoopID, got.LoopID)
	assert.Equal(t, original.CallID, got.CallID)
	assert.True(t, got.Timestamp.Equal(ts), "timestamp must round-trip")
}
