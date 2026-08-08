package agenticloop

import (
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
)

func TestInputPortResolutionRequiresExplicitStreamIdentity(t *testing.T) {
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

	_, err = (component.PortDefinition{
		Name:   "tool.result",
		Config: component.JetStreamPort{Subjects: []string{"tool.result.>"}},
	}).Resolve(component.DirectionInput)
	if err == nil {
		t.Fatal("subject-only JetStream input resolved without an explicit backing stream")
	}
	for _, context := range []string{`port "tool.result"`, `kind "jetstream"`, `field "stream_name"`} {
		if !strings.Contains(err.Error(), context) {
			t.Fatalf("subject-only input error %q missing %q", err, context)
		}
	}
}
