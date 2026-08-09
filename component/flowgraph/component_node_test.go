package flowgraph

import "github.com/c360studio/semstreams/component"

// addTestComponentNode adapts standalone unit fixtures to the retained-port
// ingestion helper. Production flowgraphs are built only from Registry
// snapshots through BuildFromRegistry.
func addTestComponentNode(graph *FlowGraph, name string, comp component.Discoverable) error {
	if comp == nil {
		return graph.addComponentNode(name, comp, nil, nil, nil, nil)
	}
	inputs := comp.InputPorts()
	inputFacts, err := testPortFacts(inputs)
	if err != nil {
		return err
	}
	outputs := comp.OutputPorts()
	outputFacts, err := testPortFacts(outputs)
	if err != nil {
		return err
	}
	return graph.addComponentNode(name, comp, inputs, inputFacts, outputs, outputFacts)
}

func extractTestPortInfo(graph *FlowGraph, ports []component.Port) ([]PortInfo, error) {
	facts, err := testPortFacts(ports)
	if err != nil {
		return nil, err
	}
	return graph.extractPortInfo(ports, facts)
}

func testPortFacts(ports []component.Port) ([]component.PortFacts, error) {
	facts := make([]component.PortFacts, len(ports))
	for index, port := range ports {
		projected, err := port.Facts()
		if err != nil {
			return nil, err
		}
		facts[index] = projected
	}
	return facts, nil
}
