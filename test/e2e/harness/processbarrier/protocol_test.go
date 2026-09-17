package processbarrier

import (
	"strings"
	"testing"
	"time"
)

func TestSubjectsUseStableSingleTokenCorrelation(t *testing.T) {
	callID := "call.with.subject.metacharacters.>"
	evidence := EvidenceSubject(callID)
	release := ReleaseSubject(callID)

	if !strings.HasPrefix(evidence, EvidenceSubjectPrefix) {
		t.Fatalf("EvidenceSubject() = %q, want prefix %q", evidence, EvidenceSubjectPrefix)
	}
	if !strings.HasPrefix(release, ReleaseSubjectPrefix) {
		t.Fatalf("ReleaseSubject() = %q, want prefix %q", release, ReleaseSubjectPrefix)
	}
	for name, subject := range map[string]string{"evidence": evidence, "release": release} {
		token := strings.TrimPrefix(subject, map[string]string{
			"evidence": EvidenceSubjectPrefix,
			"release":  ReleaseSubjectPrefix,
		}[name])
		if token == "" || strings.ContainsAny(token, ".*>") {
			t.Fatalf("%s subject token = %q, want one nonempty literal token", name, token)
		}
	}
	if got := EvidenceSubject(callID); got != evidence {
		t.Fatalf("EvidenceSubject() changed: %q then %q", evidence, got)
	}
	if evidence == EvidenceSubject("different-call") {
		t.Fatal("distinct call IDs collided")
	}
}

func TestAttemptValidatesExactCallCorrelation(t *testing.T) {
	attempt := Attempt{
		CallID:          "call-1",
		AttemptID:       "call-1/process-a/1",
		ProcessInstance: "process-a",
		ProcessID:       42,
		EnteredAt:       time.Unix(100, 0).UTC(),
	}
	if err := attempt.Validate("call-1"); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	invalid := []Attempt{
		{CallID: "other", AttemptID: attempt.AttemptID, ProcessInstance: attempt.ProcessInstance, ProcessID: 42, EnteredAt: attempt.EnteredAt},
		{CallID: "call-1", ProcessInstance: attempt.ProcessInstance, ProcessID: 42, EnteredAt: attempt.EnteredAt},
		{CallID: "call-1", AttemptID: attempt.AttemptID, ProcessID: 42, EnteredAt: attempt.EnteredAt},
		{CallID: "call-1", AttemptID: attempt.AttemptID, ProcessInstance: attempt.ProcessInstance, EnteredAt: attempt.EnteredAt},
		{CallID: "call-1", AttemptID: attempt.AttemptID, ProcessInstance: attempt.ProcessInstance, ProcessID: 42},
	}
	for index, candidate := range invalid {
		if err := candidate.Validate("call-1"); err == nil {
			t.Errorf("invalid attempt %d accepted: %#v", index, candidate)
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
