package rule

import (
	"testing"
)

func intPtr(n int) *int { return &n }

func TestEffectiveID_HonoursExplicitID(t *testing.T) {
	a := Action{Type: ActionTypePublishAgent, ID: "explicit-id"}
	if got := a.effectiveID("rule-x"); got != "explicit-id" {
		t.Errorf("effectiveID = %q, want explicit-id", got)
	}
}

func TestFingerprint_StableAcrossInvocations(t *testing.T) {
	a := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.researcher",
		Role:    "researcher",
	}
	got1 := a.fingerprint("rule-x")
	got2 := a.fingerprint("rule-x")
	if got1 != got2 {
		t.Errorf("fingerprint not deterministic: %q vs %q", got1, got2)
	}
	if len(got1) != fingerprintLen {
		t.Errorf("fingerprint len = %d, want %d", len(got1), fingerprintLen)
	}
}

func TestFingerprint_ChangesWhenShapeChanges(t *testing.T) {
	base := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.researcher",
		Role:    "researcher",
	}
	baseFP := base.fingerprint("rule-x")

	tests := []struct {
		name string
		mut  func(Action) Action
	}{
		{"different rule_id", func(a Action) Action { return a }}, // tested below via direct param change
		{"different action type", func(a Action) Action { a.Type = ActionTypeAddTriple; a.Predicate = "test.fixture.x"; return a }},
		{"different subject", func(a Action) Action { a.Subject = "agent.task.reviewer"; return a }},
		{"different role", func(a Action) Action { a.Role = "reviewer"; return a }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modified := tt.mut(base)
			fp := modified.fingerprint("rule-x")
			if tt.name == "different rule_id" {
				fp = base.fingerprint("rule-y")
			}
			if fp == baseFP {
				t.Errorf("fingerprint did NOT change for %s; both = %q", tt.name, fp)
			}
		})
	}
}

func TestFingerprint_StableAcrossUnrelatedFieldChanges(t *testing.T) {
	// Adding a prompt or modifying TTL should NOT reset the counter:
	// these aren't shape-defining for a publish_agent action.
	base := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.researcher",
		Role:    "researcher",
	}
	withPrompt := base
	withPrompt.Prompt = "totally different prompt that shouldn't matter"
	if base.fingerprint("rule-x") != withPrompt.fingerprint("rule-x") {
		t.Error("fingerprint changed for unrelated Prompt edit; design intent is shape-only invalidation")
	}
}

func TestEffectiveMaxIterations_NilUsesFrameworkDefault(t *testing.T) {
	a := Action{}
	maxIter, fromDefault := a.effectiveMaxIterations()
	if maxIter != DefaultActionMaxIterations {
		t.Errorf("maxIter = %d, want DefaultActionMaxIterations (%d)", maxIter, DefaultActionMaxIterations)
	}
	if !fromDefault {
		t.Error("fromDefault = false, want true (nil pointer should select framework default)")
	}
}

func TestEffectiveMaxIterations_ExplicitZeroIsUnlimited(t *testing.T) {
	a := Action{MaxIterations: intPtr(0)}
	maxIter, fromDefault := a.effectiveMaxIterations()
	if maxIter != 0 {
		t.Errorf("maxIter = %d, want 0 (operator's explicit unlimited sentinel)", maxIter)
	}
	if fromDefault {
		t.Error("fromDefault = true, want false (explicit pointer is operator config)")
	}
	if !isUnlimited(maxIter) {
		t.Error("isUnlimited(0) = false, want true")
	}
}

func TestEffectiveMaxIterations_ExplicitNonZero(t *testing.T) {
	a := Action{MaxIterations: intPtr(7)}
	maxIter, fromDefault := a.effectiveMaxIterations()
	if maxIter != 7 {
		t.Errorf("maxIter = %d, want 7", maxIter)
	}
	if fromDefault {
		t.Error("fromDefault = true, want false")
	}
	if isUnlimited(maxIter) {
		t.Error("isUnlimited(7) = true, want false")
	}
}

// TestFingerprint_DistinguisherByActionType pins the distinguisher
// rule per semteams's design (2026-05-05): publish/publish_agent use
// Subject; triple actions use Predicate; update_kv uses Bucket+Key.
// Two actions of different types with the same Subject MUST get
// different fingerprints — otherwise a publish and a publish_agent
// rule that happen to share the same subject string would share a
// firing counter.
func TestFingerprint_DistinguisherByActionType(t *testing.T) {
	publish := Action{Type: ActionTypePublish, Subject: "events.foo", Role: "x"}
	addTriple := Action{Type: ActionTypeAddTriple, Subject: "events.foo", Predicate: "test.rel.foo", Role: "x"}
	updateKV := Action{Type: ActionTypeUpdateKV, Bucket: "BUCKET", Key: "key", Role: "x"}

	fp1 := publish.fingerprint("r")
	fp2 := addTriple.fingerprint("r")
	fp3 := updateKV.fingerprint("r")

	if fp1 == fp2 || fp1 == fp3 || fp2 == fp3 {
		t.Errorf("fingerprints should differ across action types: publish=%q add_triple=%q update_kv=%q",
			fp1, fp2, fp3)
	}
}
