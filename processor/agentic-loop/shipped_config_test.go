package agenticloop_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/c360studio/semstreams/component"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

func TestShippedAgenticAssembliesInheritTrajectoryPortsAndProvideEvidenceStore(t *testing.T) {
	paths := []string{
		"configs/agentic.json",
		"configs/flows/ops-agent.json",
		"configs/flows/ops-agent-test.json",
		"configs/flows/lesson-example.json",
		"configs/flows/crud-tools-test.json",
		"configs/flows/deep-research-test.json",
		"configs/flows/deep-research.json",
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			encoded, err := os.ReadFile(filepath.Join("..", "..", path))
			if err != nil {
				t.Fatal(err)
			}
			var assembly struct {
				Components map[string]struct {
					Type    string          `json:"type"`
					Name    string          `json:"name"`
					Enabled bool            `json:"enabled"`
					Config  json.RawMessage `json:"config"`
				} `json:"components"`
			}
			if err := json.Unmarshal(encoded, &assembly); err != nil {
				t.Fatal(err)
			}

			provider, ok := assembly.Components["objectstore"]
			if !ok || !provider.Enabled || provider.Type != "storage" || provider.Name != "objectstore" {
				t.Fatalf("objectstore provider contract drift: %#v", provider)
			}
			var providerConfig struct {
				BucketName string `json:"bucket_name"`
			}
			if err := json.Unmarshal(provider.Config, &providerConfig); err != nil {
				t.Fatal(err)
			}
			if providerConfig.BucketName != "AGENT_CONTENT" {
				t.Fatalf("objectstore bucket = %q, want AGENT_CONTENT", providerConfig.BucketName)
			}

			loopConfig, ok := assembly.Components["agentic-loop"]
			if !ok {
				t.Fatal("agentic-loop component missing")
			}
			var rawLoopConfig struct {
				Ports struct {
					Outputs []struct {
						Name string `json:"name"`
					} `json:"outputs"`
				} `json:"ports"`
			}
			if err := json.Unmarshal(loopConfig.Config, &rawLoopConfig); err != nil {
				t.Fatal(err)
			}
			for _, output := range rawLoopConfig.Ports.Outputs {
				if output.Name == "trajectories" {
					t.Fatal("redundant trajectories override must be absent")
				}
			}

			discoverable, err := agenticloop.NewComponent(loopConfig.Config, component.Dependencies{})
			if err != nil {
				t.Fatalf("construct agentic-loop from shipped config: %v", err)
			}
			assertCanonicalTrajectoryPorts(t, discoverable)
		})
	}
}

func assertCanonicalTrajectoryPorts(t *testing.T, discoverable component.Discoverable) {
	t.Helper()
	var queryFound, factsFound bool
	for _, port := range discoverable.InputPorts() {
		if port.Name != "trajectory_query" {
			continue
		}
		facts, err := port.Facts()
		if err != nil {
			t.Fatal(err)
		}
		contract, ok := facts.Interface()
		queryFound = port.Required && facts.Kind() == component.PortKindNATSRequest &&
			len(facts.NATSSubjects()) == 1 && facts.NATSSubjects()[0] == "agentic.query.trajectory" &&
			ok && contract.Type == "agentic.query" && contract.Version == "v1"
	}
	for _, port := range discoverable.OutputPorts() {
		if port.Name != "trajectories" {
			continue
		}
		facts, err := port.Facts()
		if err != nil {
			t.Fatal(err)
		}
		contract, ok := facts.Interface()
		factsFound = port.Required && facts.Kind() == component.PortKindKVWrite &&
			facts.ResourceID() == "kv:AGENT_TRAJECTORIES" && ok &&
			contract.Type == "agentic.trajectory.fact" && contract.Version == "v1"
	}
	if !queryFound || !factsFound {
		t.Fatalf("canonical trajectory ports missing: query=%t facts=%t", queryFound, factsFound)
	}
}
