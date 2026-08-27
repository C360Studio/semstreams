package composition_test

import (
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/composition"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/types"
)

func TestMermaidIsDeterministic(t *testing.T) {
	registry := fakeRegistry(t,
		fakeSpec{name: "src", typ: "input", outputs: []component.PortDefinition{jetStreamOut("out", "DATA", "data.>")}},
		fakeSpec{name: "proc", typ: "processor",
			inputs:  []component.PortDefinition{jetStreamIn("in", "DATA", "data.>", true)},
			outputs: []component.PortDefinition{natsOut("out", "proc.done", nil)}},
		fakeSpec{name: "sink", typ: "output", inputs: []component.PortDefinition{natsIn("in", "proc.done", true, nil)}},
	)
	cfg := compositionOf(config.ComponentConfigs{
		"b-src":  instance("src", types.ComponentTypeInput),
		"a-src":  instance("src", types.ComponentTypeInput),
		"proc":   instance("proc", types.ComponentTypeProcessor),
		"z-sink": instance("sink", types.ComponentTypeOutput),
		"y-sink": instance("sink", types.ComponentTypeOutput),
	})
	var renders []string
	for i := 0; i < 5; i++ {
		result, err := composition.Validate(registry, cfg)
		if err != nil {
			t.Fatal(err)
		}
		renders = append(renders, composition.Mermaid(result.Graph))
	}
	for i := 1; i < len(renders); i++ {
		if renders[0] != renders[i] {
			t.Fatalf("render %d differs from render 0:\n%s\n---\n%s", i, renders[0], renders[i])
		}
	}
	if !strings.HasPrefix(renders[0], "flowchart LR") {
		t.Fatalf("Mermaid output does not start with a flowchart header:\n%s", renders[0])
	}
	for _, instance := range []string{"a-src", "b-src", "proc", "y-sink", "z-sink"} {
		if !strings.Contains(renders[0], instance) {
			t.Errorf("Mermaid output lacks node %s:\n%s", instance, renders[0])
		}
	}
	if strings.Count(renders[0], "-->") != 4 {
		t.Fatalf("Mermaid output renders %d edges, want 4:\n%s", strings.Count(renders[0], "-->"), renders[0])
	}
}
