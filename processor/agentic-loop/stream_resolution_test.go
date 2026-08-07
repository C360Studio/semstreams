package agenticloop

import (
	"testing"

	"github.com/c360studio/semstreams/component"
)

func TestInputPortResolutionAcceptsExplicitSubjectBinding(t *testing.T) {
	valid, err := (component.PortDefinition{
		Name:   "tool.result",
		Config: component.JetStreamPort{StreamName: "TOOL", Subjects: []string{"tool.result.>"}},
	}).Resolve(component.DirectionInput)
	if err != nil {
		t.Fatalf("resolve valid input: %v", err)
	}
	facts, err := valid.Facts()
	if err != nil {
		t.Fatalf("project valid input: %v", err)
	}
	stream, ok := facts.Stream()
	if !ok || stream.Name() != "TOOL" {
		t.Fatalf("stream facts = %#v, %t", stream, ok)
	}

	subjectBound, err := (component.PortDefinition{
		Name:   "tool.result",
		Config: component.JetStreamPort{Subjects: []string{"tool.result.>"}},
	}).Resolve(component.DirectionInput)
	if err != nil {
		t.Fatalf("resolve subject-bound input: %v", err)
	}
	subjectFacts, err := subjectBound.Facts()
	if err != nil {
		t.Fatalf("project subject-bound input: %v", err)
	}
	subjectStream, ok := subjectFacts.Stream()
	if !ok || subjectStream.Name() != "" || len(subjectStream.Subjects()) != 1 || subjectStream.Subjects()[0] != "tool.result.>" {
		t.Fatalf("subject-bound stream facts = %#v, %t", subjectStream, ok)
	}
}
