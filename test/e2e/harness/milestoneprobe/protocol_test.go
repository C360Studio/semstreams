package milestoneprobe

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic/agentrun"
)

func TestSubjectsUseStableSingleTokenCorrelation(t *testing.T) {
	sourceMessageID := "id.with.subject.metacharacters.>"
	attempt := AttemptSubject(sourceMessageID)
	effect := EffectSubject(sourceMessageID)

	for name, pair := range map[string][2]string{
		"attempt": {attempt, AttemptSubjectPrefix},
		"effect":  {effect, EffectSubjectPrefix},
	} {
		subject, prefix := pair[0], pair[1]
		if !strings.HasPrefix(subject, prefix) {
			t.Fatalf("%s subject = %q, want prefix %q", name, subject, prefix)
		}
		token := strings.TrimPrefix(subject, prefix)
		if token == "" || strings.ContainsAny(token, ".*>") {
			t.Fatalf("%s subject token = %q, want one nonempty literal token", name, token)
		}
	}
	if attempt == effect {
		t.Fatal("attempt and effect subjects collided for one identity")
	}
	if got := AttemptSubject(sourceMessageID); got != attempt {
		t.Fatalf("AttemptSubject() changed: %q then %q", attempt, got)
	}
	if attempt == AttemptSubject("a-different-terminal") {
		t.Fatal("distinct source message IDs collided")
	}
}

// TestBehaviorFromRoleAddressesOnlyThisProbe is the gate that keeps the probe
// inert for the tier's real traffic: every ordinary terminal the agentic tier
// produces carries a role this probe must not answer to.
func TestBehaviorFromRoleAddressesOnlyThisProbe(t *testing.T) {
	for _, behavior := range []string{BehaviorExitBeforeAck, BehaviorPanicOnce, BehaviorTransient} {
		got, addressed := BehaviorFromRole(Role(behavior))
		if !addressed || got != behavior {
			t.Fatalf("BehaviorFromRole(Role(%q)) = %q,%v", behavior, got, addressed)
		}
	}
	for _, role := range []string{"", "general", "researcher", "architect", RolePrefix, "e2e-milestone-probe"} {
		if got, addressed := BehaviorFromRole(role); addressed {
			t.Errorf("BehaviorFromRole(%q) = %q,true, want not addressed", role, got)
		}
	}
}

// TestOrdinaryTerminalIsANoOpBeforeAnyIO pins the no-IO half of that gate. The
// handler holds a nil NATS client here, so any read or publish on an ordinary
// terminal would panic rather than quietly cost the tier a round trip.
func TestOrdinaryTerminalIsANoOpBeforeAnyIO(t *testing.T) {
	probe := &handler{}
	for _, role := range []string{"", "general", "researcher"} {
		if err := probe.OnLoopTerminal(context.Background(), agentrun.LoopTerminalEvent{
			SourceMessageID: "msg-1", LoopID: "loop-1", Role: role,
		}, nil); err != nil {
			t.Fatalf("OnLoopTerminal(role=%q) error = %v, want nil no-op", role, err)
		}
	}
}

// TestRegisterRefusesIncompleteWiring is the fail-closed half: an armed probe
// with nothing to register on is a harness defect that must be loud at boot,
// not a silently missing proof three stages later. Arming itself is
// internal/e2eboot's decision, so the refusal no longer depends on EnvVar.
func TestRegisterRefusesIncompleteWiring(t *testing.T) {
	t.Setenv(EnvVar, "")
	if err := Register(nil, nil, nil); err == nil {
		t.Fatal("Register(nil, nil, nil) error = nil, want a refusal")
	}
}

func TestAttemptValidatesExactTerminalCorrelation(t *testing.T) {
	attempt := Attempt{
		SourceMessageID: "msg-1",
		LoopID:          "loop-1",
		Behavior:        BehaviorExitBeforeAck,
		ProcessInstance: "process-a",
		ProcessID:       42,
		ObservedAt:      time.Unix(100, 0).UTC(),
	}
	if err := attempt.Validate("msg-1"); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := attempt.Validate(""); err == nil {
		t.Fatal("Validate(\"\") accepted an empty expected identity")
	}

	invalid := []Attempt{
		{SourceMessageID: "other", LoopID: "loop-1", Behavior: attempt.Behavior,
			ProcessInstance: "process-a", ProcessID: 42, ObservedAt: attempt.ObservedAt},
		{SourceMessageID: "msg-1", Behavior: attempt.Behavior,
			ProcessInstance: "process-a", ProcessID: 42, ObservedAt: attempt.ObservedAt},
		{SourceMessageID: "msg-1", LoopID: "loop-1",
			ProcessInstance: "process-a", ProcessID: 42, ObservedAt: attempt.ObservedAt},
		{SourceMessageID: "msg-1", LoopID: "loop-1", Behavior: attempt.Behavior,
			ProcessID: 42, ObservedAt: attempt.ObservedAt},
		{SourceMessageID: "msg-1", LoopID: "loop-1", Behavior: attempt.Behavior,
			ProcessInstance: "process-a", ObservedAt: attempt.ObservedAt},
		{SourceMessageID: "msg-1", LoopID: "loop-1", Behavior: attempt.Behavior,
			ProcessInstance: "process-a", ProcessID: 42},
	}
	for index, candidate := range invalid {
		if err := candidate.Validate("msg-1"); err == nil {
			t.Errorf("invalid attempt %d accepted: %#v", index, candidate)
		}
	}
}

func TestEffectValidatesExactTerminalCorrelation(t *testing.T) {
	effect := Effect{
		SourceMessageID: "msg-1",
		LoopID:          "loop-1",
		ProcessInstance: "process-a",
		CommittedAt:     time.Unix(100, 0).UTC(),
	}
	if err := effect.Validate("msg-1"); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := effect.Validate(""); err == nil {
		t.Fatal("Validate(\"\") accepted an empty expected identity")
	}

	invalid := []Effect{
		{SourceMessageID: "other", LoopID: "loop-1", ProcessInstance: "process-a", CommittedAt: effect.CommittedAt},
		{SourceMessageID: "msg-1", ProcessInstance: "process-a", CommittedAt: effect.CommittedAt},
		{SourceMessageID: "msg-1", LoopID: "loop-1", CommittedAt: effect.CommittedAt},
		{SourceMessageID: "msg-1", LoopID: "loop-1", ProcessInstance: "process-a"},
	}
	for index, candidate := range invalid {
		if err := candidate.Validate("msg-1"); err == nil {
			t.Errorf("invalid effect %d accepted: %#v", index, candidate)
		}
	}
}

func TestProcessInstancesAreUnique(t *testing.T) {
	first, err := newProcessInstance()
	if err != nil {
		t.Fatalf("newProcessInstance() first error = %v", err)
	}
	second, err := newProcessInstance()
	if err != nil {
		t.Fatalf("newProcessInstance() second error = %v", err)
	}
	if first == "" || second == "" || first == second {
		t.Fatalf("process instances = %q and %q, want distinct nonempty values", first, second)
	}
}
