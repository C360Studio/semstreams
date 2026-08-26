package composition

import (
	"fmt"
	"sort"
	"strings"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/component/flowgraph"
	"github.com/c360studio/semstreams/config"
)

// Analyze is the graph-level half of composition validation: it connects the
// declarations, analyzes connectivity, stream requirements, and interface
// contracts, and projects the graph. It is what ComponentManager runs at boot
// over the admitted declarations and what Validate runs over declared ones.
//
// streams is the configuration's explicit `streams` block (nil when it has
// none). Stream provisioning derives streams from JetStream OUTPUT ports and
// from this block; a JetStream subscriber whose subjects an explicit stream
// covers is fed even when its publishers use core NATS, so it is not a
// stream_requirement finding.
func Analyze(declarations []component.Declaration, streams config.StreamConfigs) *Result {
	result := newResult()
	sorted := append([]component.Declaration(nil), declarations...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].InstanceName < sorted[j].InstanceName })

	if len(sorted) == 0 {
		result.add(Finding{
			Type: TypeEmptyComposition, Severity: severityOf(TypeEmptyComposition, nil),
			Component: "(none)",
			Message:   "composition declares no enabled components",
			Suggestions: []string{
				"Add at least one enabled component to the configuration",
				"A services-only process boots; this is a warning, not a refusal",
			},
		})
		result.finalize()
		return result
	}

	graph, err := flowgraph.BuildFromDeclarations(sorted)
	if err != nil {
		result.add(Finding{
			Type: TypeConnectionPatternError, Severity: severityOf(TypeConnectionPatternError, nil),
			Component: "(composition)",
			Message:   err.Error(),
			Suggestions: []string{
				"Give each network listener its own address",
				"Let exactly one component write each KV bucket",
				"Declare graph-mutation ports with the canonical required nats-request shape",
			},
		})
		result.Graph = graphOf(sorted, nil)
		result.finalize()
		return result
	}

	analysis := graph.AnalyzeConnectivity()
	external := externalInputs(sorted)
	for _, node := range analysis.DisconnectedNodes {
		result.add(Finding{
			Type: TypeDisconnectedNode, Severity: severityOf(TypeDisconnectedNode, nil),
			Component:   node.ComponentName,
			Message:     node.Issue,
			Suggestions: append([]string{}, node.Suggestions...),
		})
	}
	for _, port := range analysis.OrphanedPorts {
		port := port
		if port.Issue == flowgraph.IssueNoPublishers && external[portKey{port.ComponentName, port.PortName}] {
			// The operator declared this input fed from outside the composition
			// (PortDefinition.External): no in-graph publisher is expected, so
			// the no-publisher orphan is not a finding. Every other finding on
			// the port — stream requirement, interface contracts — is unaffected.
			continue
		}
		result.add(orphanedPortFinding(port))
	}
	for _, warning := range graph.ValidateStreamRequirements() {
		if explicitStreamCovers(streams, warning.Subjects) {
			continue // an explicit stream captures the core-NATS publishes; the subscriber is fed
		}
		publishers := append([]string(nil), warning.PublisherComps...)
		sort.Strings(publishers)
		result.add(Finding{
			Type: TypeStreamRequirement, Severity: severityOf(TypeStreamRequirement, nil),
			Component: warning.SubscriberComp,
			Port:      warning.SubscriberPort,
			Message: fmt.Sprintf(
				"JetStream subscriber expects a stream for subjects %v but its publishers [%s] use core NATS (no stream will be created)",
				warning.Subjects, strings.Join(publishers, ", ")),
			Suggestions: []string{
				"Publish through a jetstream output port on: " + strings.Join(publishers, ", "),
				"Or subscribe through a nats input port if durability is not required",
			},
		})
	}
	for _, finding := range interfaceFindings(sorted, graph) {
		result.add(finding)
	}
	result.Graph = graphOf(sorted, graph)
	result.finalize()
	return result
}

// explicitStreamCovers reports whether every subscriber subject overlaps a
// subject of some explicitly declared stream. An empty subject list is not
// covered: a subscriber with no subjects has nothing a stream could feed.
func explicitStreamCovers(streams config.StreamConfigs, subjects []string) bool {
	if len(subjects) == 0 || len(streams) == 0 {
		return false
	}
	for _, subject := range subjects {
		covered := false
		for _, stream := range streams {
			for _, declared := range stream.Subjects {
				if flowgraph.SubjectMatches(subject, declared) {
					covered = true
					break
				}
			}
			if covered {
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}

type portKey struct{ instance, port string }

// externalInputs indexes the inputs declared fed from outside the composition.
func externalInputs(declarations []component.Declaration) map[portKey]bool {
	external := map[portKey]bool{}
	for _, declaration := range declarations {
		for _, port := range declaration.InputPorts {
			if port.External {
				external[portKey{declaration.InstanceName, port.Name}] = true
			}
		}
	}
	return external
}

func orphanedPortFinding(port flowgraph.OrphanedPort) Finding {
	severity := severityOf(TypeOrphanedPort, &port)
	var suggestions []string
	switch port.Issue {
	case flowgraph.IssueNoPublishers:
		if severity == SeverityError {
			suggestions = []string{
				"Connect an output from another component",
				"Check that source component is configured correctly",
			}
		} else {
			suggestions = []string{"This port is optional and can remain unconnected"}
		}
	case flowgraph.IssueNoSubscribers:
		if port.Required && port.Pattern == component.PatternStream {
			suggestions = []string{
				"Consider connecting to a processor or output component",
				fmt.Sprintf("Data will be published to %s but not consumed", port.ConnectionID),
			}
		} else {
			suggestions = []string{"This port is optional and can remain unconnected"}
		}
	case flowgraph.IssueOptionalAPIUnused:
		suggestions = []string{"This API port is optional"}
	case flowgraph.IssueOptionalInterfaceUnused:
		suggestions = []string{"This interface-specific port is optional"}
	case flowgraph.IssueOptionalIndexUnwatched:
		suggestions = []string{"KV index ports are optional observation points"}
	default:
		suggestions = []string{}
	}
	return Finding{
		Type: TypeOrphanedPort, Severity: severity,
		Component:   port.ComponentName,
		Port:        port.PortName,
		Message:     fmt.Sprintf("%s port '%s' (%s): %s", port.Direction, port.PortName, port.Pattern, port.Issue),
		Suggestions: suggestions,
	}
}

// interfaceFindings checks the interface contract on every derived edge: an
// exact match is compatible (the rule formerly at engine/validator.go:612-623);
// a declared-vs-declared difference is an error and a source without a
// contract feeding a target that requires one is a warning.
func interfaceFindings(declarations []component.Declaration, graph *flowgraph.FlowGraph) []Finding {
	// name → port name → interface type, inputs first then outputs, as the
	// engine's lookup was shaped.
	contracts := make(map[string]map[string]string, len(declarations))
	for _, declaration := range declarations {
		byPort := make(map[string]string)
		for index, port := range declaration.InputPorts {
			byPort[port.Name] = interfaceType(declaration.InputFacts, index)
		}
		for index, port := range declaration.OutputPorts {
			byPort[port.Name] = interfaceType(declaration.OutputFacts, index)
		}
		contracts[declaration.InstanceName] = byPort
	}

	var findings []Finding
	for _, edge := range graph.GetEdges() {
		source, ok := contracts[edge.From.ComponentName]
		if !ok {
			continue
		}
		sourceType, ok := source[edge.From.PortName]
		if !ok {
			continue
		}
		target, ok := contracts[edge.To.ComponentName]
		if !ok {
			continue
		}
		targetType, ok := target[edge.To.PortName]
		if !ok {
			continue
		}
		componentPair := fmt.Sprintf("%s → %s", edge.From.ComponentName, edge.To.ComponentName)
		portPair := fmt.Sprintf("%s → %s", edge.From.PortName, edge.To.PortName)
		switch {
		case targetType != "" && sourceType != "" && sourceType != targetType:
			findings = append(findings, Finding{
				Type: TypeInterfaceMismatch, Severity: severityOf(TypeInterfaceMismatch, nil),
				Component: componentPair, Port: portPair,
				Message: fmt.Sprintf(
					"Interface mismatch: source port '%s' provides '%s' but target port '%s' requires '%s'",
					edge.From.PortName, sourceType, edge.To.PortName, targetType),
				Suggestions: []string{
					"Check that connected components have compatible interfaces",
					"Verify port interface contracts in component documentation",
					fmt.Sprintf("Source provides: %s", sourceType),
					fmt.Sprintf("Target requires: %s", targetType),
				},
			})
		case targetType != "" && sourceType == "":
			findings = append(findings, Finding{
				Type: TypeMissingInterface, Severity: severityOf(TypeMissingInterface, nil),
				Component: componentPair, Port: portPair,
				Message: fmt.Sprintf(
					"Source port '%s' does not declare an interface, but target port '%s' requires '%s'",
					edge.From.PortName, edge.To.PortName, targetType),
				Suggestions: []string{
					"Verify that source component produces compatible data",
					"Check component documentation for interface contracts",
					fmt.Sprintf("Target requires: %s", targetType),
				},
			})
		}
	}
	return findings
}

func interfaceType(facts []component.PortFacts, index int) string {
	if index >= len(facts) {
		return ""
	}
	if contract, ok := facts[index].Interface(); ok {
		return contract.Type
	}
	return ""
}
