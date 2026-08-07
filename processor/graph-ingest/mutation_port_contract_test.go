package graphingest

import (
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/graphmutation"
)

func TestDefaultConfigDeclaresRequiredMutationProvider(t *testing.T) {
	config := DefaultConfig()
	for _, definition := range config.Ports.Inputs {
		port, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			t.Fatalf("resolve mutation provider: %v", err)
		}
		request, ok := port.Config.(component.NATSRequestPort)
		if !ok || request.Interface == nil || request.Interface.Type != graphmutation.InterfaceType {
			continue
		}
		if !port.Required || request.Subject != graphmutation.SubjectFamily ||
			request.Interface.Version != graphmutation.InterfaceVersion {
			t.Fatalf("mutation provider = %#v", port)
		}
		return
	}
	t.Fatal("default config has no typed graph mutation provider")
}

func TestResolveMutationSubjectRequiresDeclaredProvider(t *testing.T) {
	got, err := graphmutation.ResolveSubject(graphmutation.SubjectFamily, graphmutation.ReconcilePredicates)
	if err != nil {
		t.Fatalf("ResolveSubject: %v", err)
	}
	if got != "graph.mutation.entity.reconcile" {
		t.Fatalf("subject = %q", got)
	}

	for _, family := range []string{"", "graph.mutation.*", "graph.mutation.entity.create"} {
		if subject, err := graphmutation.ResolveSubject(family, graphmutation.CreateEntity); err == nil {
			t.Fatalf("invalid family %q resolved to %q", family, subject)
		}
	}
}
