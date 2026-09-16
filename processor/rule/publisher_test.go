package rule

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/c360studio/semstreams/component"
	appconfig "github.com/c360studio/semstreams/config"
	"github.com/stretchr/testify/require"
)

// spec: rule-agent-publishing / Publish-agent classification uses canonical wildcard coverage and durable publication
func TestActionPublisher_ClassifiesResolvedOutputCoverage(t *testing.T) {
	t.Parallel()
	jetStream := component.PortDefinition{
		Name: "arbitrary-output-name",
		Config: component.JetStreamPort{
			StreamName: "AGENT", Subjects: []string{"agent.task.*"},
		},
	}
	exact := component.PortDefinition{
		Name: "exact-output",
		Config: component.JetStreamPort{
			StreamName: "AGENT", Subjects: []string{"agent.task.research"},
		},
	}
	verdict := component.PortDefinition{
		Name: "verdict-output",
		Config: component.JetStreamPort{
			StreamName: "VERDICT", Subjects: []string{"verdict.>"},
		},
	}
	core := component.PortDefinition{
		Name: "agent.task", Config: component.NATSPort{Subject: "agent.task.research"},
	}
	multiple := component.PortDefinition{
		Name: "multiple-subjects",
		Config: component.JetStreamPort{
			StreamName: "AGENT", Subjects: []string{"agent.response.*", "agent.task.*"},
		},
	}

	// Exercise the publisher's use of the existing matcher. Exhaustive matcher
	// semantics remain in component/flowgraph/subject_cover_test.go.
	for _, tt := range []struct {
		name    string
		outputs []component.PortDefinition
		subject string
		want    bool
	}{
		{"task wildcard and irrelevant port name", []component.PortDefinition{jetStream}, "agent.task.research", true},
		{"exact JetStream", []component.PortDefinition{exact}, "agent.task.research", true},
		{"verdict tail one token", []component.PortDefinition{verdict}, "verdict.approved", true},
		{"verdict tail multiple tokens", []component.PortDefinition{verdict}, "verdict.approved.execution", true},
		{"task wildcard needs one token", []component.PortDefinition{jetStream}, "agent.task", false},
		{"task wildcard rejects extra depth", []component.PortDefinition{jetStream}, "agent.task.research.extra", false},
		{"tail needs at least one token", []component.PortDefinition{verdict}, "verdict", false},
		{"coverage is directional", []component.PortDefinition{exact}, "agent.task.*", false},
		{"task wildcard does not cover tail", []component.PortDefinition{jetStream}, "agent.task.>", false},
		{"unrelated subject", []component.PortDefinition{jetStream}, "agent.response.research", false},
		{"empty subject", []component.PortDefinition{jetStream}, "", false},
		{"empty subject token", []component.PortDefinition{jetStream}, "agent.task.", false},
		{"partial wildcard token", []component.PortDefinition{jetStream}, "agent.task.re*search", false},
		{"nonterminal tail", []component.PortDefinition{verdict}, "verdict.>.execution", false},
		{"core only despite task port name", []component.PortDefinition{core}, "agent.task.research", false},
		{"no outputs", nil, "agent.task.research", false},
		{"core before covering wildcard", []component.PortDefinition{core, jetStream}, "agent.task.research", true},
		{"core after covering wildcard", []component.PortDefinition{jetStream, core}, "agent.task.research", true},
		{"core before covering exact", []component.PortDefinition{core, exact}, "agent.task.research", true},
		{"second subject covers", []component.PortDefinition{multiple}, "agent.task.research", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			processor := &Processor{config: &Config{Ports: &component.PortConfig{Outputs: tt.outputs}}}
			require.NoError(t, processor.setupPorts())
			require.Equal(t, tt.want, processor.isJetStreamPortBySubject(tt.subject), "subject %q", tt.subject)
		})
	}
}

// spec: rule-agent-publishing / Publish-agent classification uses canonical wildcard coverage and durable publication
func TestActionPublisher_ShippedTaskOutputsUseJetStream(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		path            string
		packID          string
		portName        string
		declarationOnly bool
	}{
		{"configs/flows/deep-research.json", "deep-research-rules", "agent.task", false},
		{"configs/flows/deep-research-test.json", "deep-research-test-rules", "agent.task", false},
		{"configs/flows/crud-tools-test.json", "crud-tools-test-rules", "agent.task", true},
		{"configs/examples/research-graph-pipeline.json", "research-graph-example-rules", "agent.task", false},
		{"configs/agentic.json", "agentic-rules", "agent_task", true},
		{"configs/research-graph-e2e.json", "research-graph-e2e-rules", "agent.task", false},
	} {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join("..", "..", tt.path))
			require.NoError(t, err)
			var shipped appconfig.Config
			require.NoError(t, json.Unmarshal(data, &shipped))
			matches := 0
			for _, configured := range shipped.Components {
				if configured.Name != "rule-processor" || !configured.Enabled {
					continue
				}
				var cfg Config
				require.NoError(t, json.Unmarshal(configured.Config, &cfg))
				if cfg.PackID != tt.packID {
					continue
				}
				matches++
				require.NotNil(t, cfg.Ports)
				require.Equal(t, tt.declarationOnly, len(cfg.RulesFiles) == 0)
				processor := &Processor{config: &cfg}
				require.NoError(t, processor.setupPorts())
				foundOutput := false
				for _, port := range processor.OutputPorts() {
					if port.Name != tt.portName {
						continue
					}
					foundOutput = true
					facts, err := port.Facts()
					require.NoError(t, err)
					require.Equal(t, component.PortKindJetStream, facts.Kind())
					require.Contains(t, facts.NATSSubjects(), "agent.task.*")
				}
				require.True(t, foundOutput, "configured task output %q disappeared", tt.portName)
				require.True(t, processor.isJetStreamPortBySubject("agent.task.research"),
					"covered concrete task must use JetStream, including declaration-only configurations")
			}
			require.Equal(t, 1, matches, "expected one enabled rule processor for pack %q", tt.packID)
		})
	}
}

// spec: rule-agent-publishing / Publish-agent classification uses canonical wildcard coverage and durable publication
// Graph events share the classifier; keep their existing preflight and counters
// while selecting the declared wildcard transport.
func TestActionPublisher_GraphEventsUseCoveringJetStreamOutput(t *testing.T) {
	t.Parallel()
	spy := &graphPublisherSpy{}
	processor := &Processor{
		config: &Config{
			EnableGraphIntegration: true,
			Ports: &component.PortConfig{Outputs: []component.PortDefinition{{
				Name: "graph-output",
				Config: component.JetStreamPort{
					StreamName: "GRAPH", Subjects: []string{"graph.events.>"},
				},
			}}},
		},
		graphEventPublisher: spy,
	}
	require.NoError(t, processor.setupPorts())
	require.NoError(t, processor.publishGraphEvents(t.Context(), []Event{publisherContractEvent(t, nil)}))
	require.EqualValues(t, 1, spy.streamCalls.Load())
	require.Zero(t, spy.coreCalls.Load())
	require.EqualValues(t, 1, processor.eventsPublished)
}
