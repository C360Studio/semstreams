// Package flowgraph provides flow graph analysis and validation for component connections.
package flowgraph

import "github.com/c360studio/semstreams/component"

// FlowAnalysisResult contains the results of connectivity analysis
type FlowAnalysisResult struct {
	ConnectedComponents [][]string         `json:"connected_components"`
	ConnectedEdges      []FlowEdge         `json:"connected_edges"`
	DisconnectedNodes   []DisconnectedNode `json:"disconnected_nodes"`
	OrphanedPorts       []OrphanedPort     `json:"orphaned_ports"`
	ValidationStatus    string             `json:"validation_status"`
}

// DisconnectedNode represents a component with no connections
type DisconnectedNode struct {
	ComponentName string   `json:"component_name"`
	Issue         string   `json:"issue"`
	Suggestions   []string `json:"suggestions,omitempty"`
}

// OrphanedPort represents a port with no connections
type OrphanedPort struct {
	ComponentName string                       `json:"component_name"`
	PortName      string                       `json:"port_name"`
	Direction     component.Direction          `json:"direction"`
	ConnectionID  string                       `json:"connection_id"`
	Pattern       component.InteractionPattern `json:"pattern"`
	Issue         string                       `json:"issue"`
	Required      bool                         `json:"required"`
}

// StreamRequirementWarning represents a mismatch between JetStream subscriber and NATS publisher
type StreamRequirementWarning struct {
	Severity       string   `json:"severity"`
	SubscriberComp string   `json:"subscriber_component"`
	SubscriberPort string   `json:"subscriber_port"`
	Subjects       []string `json:"subjects"`
	PublisherComps []string `json:"publisher_components"`
	Issue          string   `json:"issue"`
}
