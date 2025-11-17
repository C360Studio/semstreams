// Package flowgraph provides flow graph analysis and validation for component connections.
package flowgraph

import (
	"fmt"
	"log"
	"strings"

	"github.com/c360/semstreams/component"
)

// FlowGraph represents a directed graph of component connections
type FlowGraph struct {
	nodes map[string]*ComponentNode // componentName -> node
	edges []FlowEdge                // all connections unified
}

// ComponentNode represents a component in the flow graph
type ComponentNode struct {
	ComponentName string
	Component     component.Discoverable
	InputPorts    []PortInfo
	OutputPorts   []PortInfo
}

// PortInfo contains port metadata for graph analysis
type PortInfo struct {
	Name         string
	Direction    component.Direction
	ConnectionID string // Subject, bucket, or network address
	Pattern      InteractionPattern
	Interface    *component.InterfaceContract
	Required     bool // Whether this port is required for the component to function
}

// FlowEdge represents a connection between two component ports
type FlowEdge struct {
	From         ComponentPortRef   `json:"from"`
	To           ComponentPortRef   `json:"to"`
	Pattern      InteractionPattern `json:"pattern"`
	ConnectionID string             `json:"connection_id"` // Subject, bucket, or network addr
	Metadata     EdgeMetadata       `json:"metadata"`      // Pattern-specific validation data
}

// ComponentPortRef references a specific port on a component
type ComponentPortRef struct {
	ComponentName string `json:"component_name"`
	PortName      string `json:"port_name"`
}

// InteractionPattern defines the type of interaction between components
type InteractionPattern string

const (
	// PatternStream represents NATSPort and JetStreamPort interactions
	PatternStream InteractionPattern = "stream"
	// PatternRequest represents NATSRequestPort (bidirectional) interactions
	PatternRequest InteractionPattern = "request"
	// PatternWatch represents KVWatchPort (observation) interactions
	PatternWatch InteractionPattern = "watch"
	// PatternNetwork represents NetworkPort (external) interactions
	PatternNetwork InteractionPattern = "network"
)

// EdgeMetadata contains pattern-specific metadata
type EdgeMetadata struct {
	InterfaceContract *component.InterfaceContract `json:"interface_contract,omitempty"`
	Timeout           string                       `json:"timeout,omitempty"` // Request pattern
	Queue             string                       `json:"queue,omitempty"`   // Stream pattern
	Keys              []string                     `json:"keys,omitempty"`    // Watch pattern
}

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
	ComponentName string              `json:"component_name"`
	PortName      string              `json:"port_name"`
	Direction     component.Direction `json:"direction"`
	ConnectionID  string              `json:"connection_id"`
	Pattern       InteractionPattern  `json:"pattern"`
	Issue         string              `json:"issue"`
	Required      bool                `json:"required"`
}

// NewFlowGraph creates a new empty FlowGraph
func NewFlowGraph() *FlowGraph {
	return &FlowGraph{
		nodes: make(map[string]*ComponentNode),
		edges: make([]FlowEdge, 0),
	}
}

// GetNodes returns a deep copy of component nodes to prevent external modification
func (g *FlowGraph) GetNodes() map[string]*ComponentNode {
	result := make(map[string]*ComponentNode, len(g.nodes))
	for k, v := range g.nodes {
		// Deep copy the ComponentNode
		nodeCopy := &ComponentNode{
			ComponentName: v.ComponentName,
			Component:     v.Component, // Interface reference - safe to share (read-only)
			// Deep copy port slices
			InputPorts:  make([]PortInfo, len(v.InputPorts)),
			OutputPorts: make([]PortInfo, len(v.OutputPorts)),
		}

		// Copy port info
		copy(nodeCopy.InputPorts, v.InputPorts)
		copy(nodeCopy.OutputPorts, v.OutputPorts)

		result[k] = nodeCopy
	}
	return result
}

// GetEdges returns the edges in the graph
func (g *FlowGraph) GetEdges() []FlowEdge {
	// Return a copy to prevent external modification
	result := make([]FlowEdge, len(g.edges))
	copy(result, g.edges)
	return result
}

// AddComponentNode adds a component as a node in the graph
func (g *FlowGraph) AddComponentNode(name string, comp component.Discoverable) error {
	if name == "" {
		return fmt.Errorf("component name cannot be empty")
	}
	if comp == nil {
		return fmt.Errorf("component cannot be nil")
	}
	if _, exists := g.nodes[name]; exists {
		return fmt.Errorf("component %s already exists in graph", name)
	}

	node := &ComponentNode{
		ComponentName: name,
		Component:     comp,
		InputPorts:    g.extractPortInfo(comp.InputPorts()),
		OutputPorts:   g.extractPortInfo(comp.OutputPorts()),
	}

	g.nodes[name] = node
	return nil
}

// extractPortInfo converts component ports to PortInfo for graph analysis
func (g *FlowGraph) extractPortInfo(ports []component.Port) []PortInfo {
	result := make([]PortInfo, 0, len(ports))

	for _, port := range ports {
		portInfo := PortInfo{
			Name:      port.Name,
			Direction: port.Direction,
			Pattern:   g.classifyInteractionPattern(port.Config),
			Interface: g.extractInterfaceContract(port.Config),
			Required:  port.Required,
		}

		// Extract connection ID based on port type
		portInfo.ConnectionID = g.extractConnectionID(port.Config)

		result = append(result, portInfo)
	}

	return result
}

// extractInterfaceContract extracts the interface contract from port configurations
func (g *FlowGraph) extractInterfaceContract(portConfig component.Portable) *component.InterfaceContract {
	switch config := portConfig.(type) {
	case component.NATSPort:
		return config.Interface
	case component.NATSRequestPort:
		return config.Interface
	case component.JetStreamPort:
		return config.Interface
	case component.KVWatchPort:
		return config.Interface
	case component.KVWritePort:
		return config.Interface
	case component.NetworkPort:
		// NetworkPort has no interface contract
		return nil
	case component.FilePort:
		// FilePort has no interface contract
		return nil
	default:
		return nil
	}
}

// classifyInteractionPattern determines the interaction pattern using type switches
func (g *FlowGraph) classifyInteractionPattern(portConfig component.Portable) InteractionPattern {
	switch portConfig.(type) {
	case component.NATSPort:
		return PatternStream
	case component.NATSRequestPort:
		return PatternRequest
	case component.JetStreamPort:
		return PatternStream // JetStream is async stream pattern
	case component.KVWatchPort:
		return PatternWatch
	case component.KVWritePort:
		return PatternWatch
	case component.NetworkPort:
		return PatternNetwork
	case component.FilePort:
		return PatternNetwork // File I/O is external like network
	default:
		// Log warning for unknown types
		return PatternStream // Safe default
	}
}

// extractConnectionID gets the connection identifier from port config
func (g *FlowGraph) extractConnectionID(portConfig component.Portable) string {
	if portConfig == nil {
		return "nil_port_config"
	}

	switch config := portConfig.(type) {
	case component.NATSPort:
		if config.Subject == "" {
			// Check if this was actually meant to be a different type
			// For graph processor output ports that should be KVWritePort
			return "nats_missing_subject"
		}
		return config.Subject
	case component.NATSRequestPort:
		if config.Subject == "" {
			return "nats_request_missing_subject"
		}
		return config.Subject
	case component.JetStreamPort:
		// Use stream name as primary identifier, or first subject if available
		if config.StreamName != "" {
			return config.StreamName
		}
		if len(config.Subjects) > 0 {
			return config.Subjects[0]
		}
		return "jetstream_unknown"
	case component.KVWatchPort:
		if config.Bucket == "" {
			return "kv_missing_bucket"
		}
		return config.Bucket
	case component.KVWritePort:
		if config.Bucket == "" {
			return "kv_missing_bucket"
		}
		return config.Bucket
	case component.NetworkPort:
		if config.Host == "" || config.Port == 0 {
			return fmt.Sprintf("network_incomplete_%s_%d", config.Host, config.Port)
		}
		return fmt.Sprintf("%s:%s:%d", config.Protocol, config.Host, config.Port)
	case component.FilePort:
		// Use path as connection identifier
		if config.Path != "" {
			return config.Path
		}
		return "file_unknown"
	default:
		// Log warning for unknown types (better than silent failure)
		return fmt.Sprintf("unknown_type_%T", config)
	}
}

// ConnectComponentsByPatterns builds edges by matching connection patterns
func (g *FlowGraph) ConnectComponentsByPatterns() error {
	// Clear existing edges
	g.edges = g.edges[:0]

	// Build connection maps by pattern and connection ID
	publishers := g.buildPublisherMap()   // Output ports
	subscribers := g.buildSubscriberMap() // Input ports

	// DEBUG: Log publishers by pattern
	log.Printf("[FlowGraph] Publishers by pattern:")
	for pattern, connMap := range publishers {
		log.Printf("  Pattern=%s, connections=%d", pattern, len(connMap))
		for connID, ports := range connMap {
			log.Printf("    ConnectionID=%s, ports=%d", connID, len(ports))
			for _, port := range ports {
				log.Printf("      Component=%s, Port=%s", port.ComponentName, port.PortName)
			}
		}
	}

	log.Printf("[FlowGraph] Subscribers by pattern:")
	for pattern, connMap := range subscribers {
		log.Printf("  Pattern=%s, connections=%d", pattern, len(connMap))
		for connID, ports := range connMap {
			log.Printf("    ConnectionID=%s, ports=%d", connID, len(ports))
			for _, port := range ports {
				log.Printf("      Component=%s, Port=%s", port.ComponentName, port.PortName)
			}
		}
	}

	var warnings []string

	// Connect based on interaction patterns
	g.connectStreamPorts(publishers[PatternStream], subscribers[PatternStream])
	g.connectRequestPorts(publishers[PatternRequest], subscribers[PatternRequest])
	g.connectWatchPorts(publishers[PatternWatch], subscribers[PatternWatch], &warnings)

	// Validate network ports for conflicts
	conflicts := g.validateNetworkPorts(publishers[PatternNetwork], subscribers[PatternNetwork])
	warnings = append(warnings, conflicts...)

	// Return error if there are critical warnings
	if len(warnings) > 0 {
		return fmt.Errorf("flow graph validation warnings: %v", warnings)
	}

	return nil
}

// buildPublisherMap creates a map of connection IDs to output ports by pattern
func (g *FlowGraph) buildPublisherMap() map[InteractionPattern]map[string][]ComponentPortRef {
	publishers := make(map[InteractionPattern]map[string][]ComponentPortRef)

	for componentName, node := range g.nodes {
		for _, port := range node.OutputPorts {
			if publishers[port.Pattern] == nil {
				publishers[port.Pattern] = make(map[string][]ComponentPortRef)
			}

			portRef := ComponentPortRef{
				ComponentName: componentName,
				PortName:      port.Name,
			}

			publishers[port.Pattern][port.ConnectionID] = append(
				publishers[port.Pattern][port.ConnectionID],
				portRef,
			)
		}
	}

	return publishers
}

// buildSubscriberMap creates a map of connection IDs to input ports by pattern
func (g *FlowGraph) buildSubscriberMap() map[InteractionPattern]map[string][]ComponentPortRef {
	subscribers := make(map[InteractionPattern]map[string][]ComponentPortRef)

	for componentName, node := range g.nodes {
		for _, port := range node.InputPorts {
			if subscribers[port.Pattern] == nil {
				subscribers[port.Pattern] = make(map[string][]ComponentPortRef)
			}

			portRef := ComponentPortRef{
				ComponentName: componentName,
				PortName:      port.Name,
			}

			subscribers[port.Pattern][port.ConnectionID] = append(
				subscribers[port.Pattern][port.ConnectionID],
				portRef,
			)
		}
	}

	return subscribers
}

// matchNATSPattern checks if a subject matches a NATS pattern
// Following NATS subject matching semantics:
// * matches exactly one token
// > matches one or more tokens
// This function works bidirectionally - either parameter can be the pattern
func matchNATSPattern(subject, pattern string) bool {
	// Handle exact match first (optimization)
	if subject == pattern {
		return true
	}

	// Check if subject has wildcards (pattern matching in reverse)
	subjectHasWildcards := strings.Contains(subject, "*") || strings.Contains(subject, ">")
	patternHasWildcards := strings.Contains(pattern, "*") || strings.Contains(pattern, ">")

	// If neither has wildcards, we already checked exact match above
	if !subjectHasWildcards && !patternHasWildcards {
		return false
	}

	// If both have wildcards, do pattern matching in both directions
	if subjectHasWildcards && patternHasWildcards {
		subjectTokens := strings.Split(subject, ".")
		patternTokens := strings.Split(pattern, ".")
		return matchTokens(subjectTokens, patternTokens) || matchTokens(patternTokens, subjectTokens)
	}

	// One has wildcards, one doesn't - do normal pattern matching
	if patternHasWildcards {
		subjectTokens := strings.Split(subject, ".")
		patternTokens := strings.Split(pattern, ".")
		return matchTokens(subjectTokens, patternTokens)
	}

	// Subject has wildcards, pattern doesn't - swap them
	subjectTokens := strings.Split(subject, ".")
	patternTokens := strings.Split(pattern, ".")
	return matchTokens(patternTokens, subjectTokens)
}

func matchTokens(subjectTokens, patternTokens []string) bool {
	i, j := 0, 0

	for i < len(patternTokens) {
		if patternTokens[i] == ">" {
			// '>' matches everything remaining
			return true
		}

		if j >= len(subjectTokens) {
			// Pattern has more tokens than subject
			return false
		}

		if patternTokens[i] == "*" {
			// '*' matches any single token
			i++
			j++
			continue
		}

		if patternTokens[i] != subjectTokens[j] {
			// Literal token mismatch
			return false
		}

		i++
		j++
	}

	// Both must be exhausted for a match
	return i == len(patternTokens) && j == len(subjectTokens)
}

// connectStreamPorts connects stream pattern ports (NATS, JetStream)
func (g *FlowGraph) connectStreamPorts(publishers, subscribers map[string][]ComponentPortRef) {
	log.Printf("[connectStreamPorts] Publishers count: %d, Subscribers count: %d", len(publishers), len(subscribers))

	// Stream pattern: publishers -> subscribers with NATS pattern matching
	for pubConnID, pubs := range publishers {
		for subConnID, subs := range subscribers {
			// Check if publisher subject matches subscriber pattern or vice versa
			match := matchNATSPattern(pubConnID, subConnID) || matchNATSPattern(subConnID, pubConnID)
			log.Printf("[connectStreamPorts] Comparing pub=%s sub=%s match=%v", pubConnID, subConnID, match)

			if match {
				log.Printf(
					"[connectStreamPorts] MATCH FOUND! Creating edges between pub=%s and sub=%s",
					pubConnID,
					subConnID,
				)
				// Connect all matching publishers to subscribers
				for _, pub := range pubs {
					for _, sub := range subs {
						edge := FlowEdge{
							From:         pub,
							To:           sub,
							Pattern:      PatternStream,
							ConnectionID: pubConnID, // Use actual subject, not pattern
							Metadata:     EdgeMetadata{},
						}
						g.edges = append(g.edges, edge)
						log.Printf(
							"[connectStreamPorts] Created edge: %s.%s -> %s.%s",
							pub.ComponentName,
							pub.PortName,
							sub.ComponentName,
							sub.PortName,
						)
					}
				}
			}
		}
	}

	log.Printf("[connectStreamPorts] Total edges created: %d", len(g.edges))
}

// connectRequestPorts connects request pattern ports (bidirectional NATS request-reply)
func (g *FlowGraph) connectRequestPorts(publishers, subscribers map[string][]ComponentPortRef) {
	// Request pattern is bidirectional - both sides can initiate requests
	// Merge all ports that share the same subject
	allPorts := make(map[string][]ComponentPortRef)

	// Collect all ports (both publishers and subscribers) by connection ID
	for connID, ports := range publishers {
		allPorts[connID] = append(allPorts[connID], ports...)
	}
	for connID, ports := range subscribers {
		if _, exists := allPorts[connID]; exists {
			allPorts[connID] = append(allPorts[connID], ports...)
		} else {
			allPorts[connID] = ports
		}
	}

	// Connect all ports with same subject bidirectionally
	for connectionID, ports := range allPorts {
		for i, port1 := range ports {
			for j, port2 := range ports {
				if i < j { // Avoid duplicate edges
					// Create bidirectional edge
					edge := FlowEdge{
						From:         port1,
						To:           port2,
						Pattern:      PatternRequest,
						ConnectionID: connectionID,
						Metadata:     EdgeMetadata{},
					}
					g.edges = append(g.edges, edge)
				}
			}
		}
	}
}

// connectWatchPorts connects watch pattern ports (KV bucket observation)
func (g *FlowGraph) connectWatchPorts(publishers, subscribers map[string][]ComponentPortRef, warnings *[]string) {
	// Watch pattern: writers (output) -> KV bucket <- watchers (input)
	// Validate single writer per bucket
	for connectionID, pubs := range publishers {
		if len(pubs) > 1 {
			// Warning: Multiple writers to same KV bucket
			if warnings != nil {
				*warnings = append(*warnings,
					fmt.Sprintf("Multiple writers to KV bucket %s: %v", connectionID, pubs))
			}
		}
		if subs, exists := subscribers[connectionID]; exists {
			// Connect writer(s) to all watchers
			for _, pub := range pubs {
				for _, sub := range subs {
					edge := FlowEdge{
						From:         pub,
						To:           sub,
						Pattern:      PatternWatch,
						ConnectionID: connectionID,
						Metadata:     EdgeMetadata{},
					}
					g.edges = append(g.edges, edge)
				}
			}
		}
	}
}

// validateNetworkPorts detects network port binding conflicts
func (g *FlowGraph) validateNetworkPorts(publishers, subscribers map[string][]ComponentPortRef) []string {
	// Network ports need exclusive binding - detect conflicts
	conflicts := []string{}
	allPorts := make(map[string][]ComponentPortRef)

	// Check publishers for conflicts
	for connID, ports := range publishers {
		if len(ports) > 1 {
			conflicts = append(conflicts,
				fmt.Sprintf("Network port conflict on %s: multiple components binding: %v", connID, ports))
		}
		allPorts[connID] = ports
	}

	// Check if subscribers conflict with publishers (both trying to bind same port)
	for connID, ports := range subscribers {
		if existing, exists := allPorts[connID]; exists {
			conflicts = append(conflicts,
				fmt.Sprintf("Network port conflict on %s: %v and %v both trying to bind", connID, existing, ports))
		} else if len(ports) > 1 {
			conflicts = append(conflicts,
				fmt.Sprintf("Network port conflict on %s: multiple components binding: %v", connID, ports))
		}
	}

	// Network ports are external connections - no edges created in the graph
	return conflicts
}

// AnalyzeConnectivity performs graph connectivity analysis
func (g *FlowGraph) AnalyzeConnectivity() *FlowAnalysisResult {
	result := &FlowAnalysisResult{
		ConnectedEdges:      g.edges,
		ValidationStatus:    "healthy",
		DisconnectedNodes:   []DisconnectedNode{}, // Initialize empty slice
		ConnectedComponents: [][]string{},         // Initialize empty slice
		OrphanedPorts:       []OrphanedPort{},     // Initialize empty slice
	}

	// Find connected components using standard graph algorithms
	components := g.findConnectedComponents()
	if components != nil {
		result.ConnectedComponents = components
	}

	// Detect orphaned ports
	orphans := g.findOrphanedPorts()
	if orphans != nil {
		result.OrphanedPorts = orphans
	}

	// Find disconnected nodes (nodes with no edges)
	for name := range g.nodes {
		hasConnection := false
		for _, edge := range g.edges {
			if edge.From.ComponentName == name || edge.To.ComponentName == name {
				hasConnection = true
				break
			}
		}
		if !hasConnection {
			result.DisconnectedNodes = append(result.DisconnectedNodes, DisconnectedNode{
				ComponentName: name,
				Issue:         "Component has no connections",
				Suggestions:   []string{"Connect to other components", "Verify component configuration"},
			})
		}
	}

	// Determine validation status based on severity
	hasCriticalIssues := false
	for _, port := range result.OrphanedPorts {
		// Check if this is a critical issue
		if port.Issue == "no_publishers" || port.Issue == "no_subscribers" {
			// Only required stream connections are critical
			// Optional ports without connections are acceptable
			if port.Pattern == PatternStream && port.Required {
				hasCriticalIssues = true
				break
			}
		}
	}

	// Set validation status
	if len(result.DisconnectedNodes) > 0 || hasCriticalIssues {
		result.ValidationStatus = "warnings"
	}

	return result
}

// findConnectedComponents uses DFS to find connected components in the graph
func (g *FlowGraph) findConnectedComponents() [][]string {
	visited := make(map[string]bool)
	var components [][]string

	// Build adjacency list from edges (treat as undirected for connectivity)
	adj := make(map[string][]string)
	for _, edge := range g.edges {
		from := edge.From.ComponentName
		to := edge.To.ComponentName

		adj[from] = append(adj[from], to)
		adj[to] = append(adj[to], from)
	}

	// DFS to find connected components
	for componentName := range g.nodes {
		if !visited[componentName] {
			var cluster []string
			g.dfs(componentName, adj, visited, &cluster)
			components = append(components, cluster)
		}
	}

	return components
}

// dfs performs depth-first search for connected components
func (g *FlowGraph) dfs(node string, adj map[string][]string, visited map[string]bool, cluster *[]string) {
	visited[node] = true
	*cluster = append(*cluster, node)

	for _, neighbor := range adj[node] {
		if !visited[neighbor] {
			g.dfs(neighbor, adj, visited, cluster)
		}
	}
}

// findOrphanedPorts identifies ports with no connections
// Network boundary ports are excluded as they are external interfaces
// Request/Response and Watch ports are marked as optional
func (g *FlowGraph) findOrphanedPorts() []OrphanedPort {
	var orphaned []OrphanedPort

	// Track which ports have connections
	connectedPorts := make(map[string]map[string]bool) // component -> port -> connected

	for _, edge := range g.edges {
		// Mark ports as connected
		if connectedPorts[edge.From.ComponentName] == nil {
			connectedPorts[edge.From.ComponentName] = make(map[string]bool)
		}
		if connectedPorts[edge.To.ComponentName] == nil {
			connectedPorts[edge.To.ComponentName] = make(map[string]bool)
		}

		connectedPorts[edge.From.ComponentName][edge.From.PortName] = true
		connectedPorts[edge.To.ComponentName][edge.To.PortName] = true
	}

	// Check all ports for orphans
	for componentName, node := range g.nodes {
		// Check input ports
		for _, port := range node.InputPorts {
			if connectedPorts[componentName] == nil || !connectedPorts[componentName][port.Name] {
				// Skip network boundary inputs - they ARE the external source
				if port.Pattern == PatternNetwork {
					continue // Not orphaned, it's an external input
				}

				// Determine issue type based on pattern
				issue := "no_publishers"
				if port.Pattern == PatternRequest {
					issue = "optional_api_unused" // Request APIs are optional
				} else if g.isInterfaceAlternativePort(port) {
					// Interface-specific alternatives are optional specialized paths
					issue = "optional_interface_unused"
				}

				orphaned = append(orphaned, OrphanedPort{
					ComponentName: componentName,
					PortName:      port.Name,
					Direction:     port.Direction,
					ConnectionID:  port.ConnectionID,
					Pattern:       port.Pattern,
					Issue:         issue,
					Required:      port.Required,
				})
			}
		}

		// Check output ports
		for _, port := range node.OutputPorts {
			if connectedPorts[componentName] == nil || !connectedPorts[componentName][port.Name] {
				// Skip network boundary outputs - they ARE the external sink
				if port.Pattern == PatternNetwork {
					continue // Not orphaned, it's an external output
				}

				// Determine issue type based on pattern
				issue := "no_subscribers"
				if port.Pattern == PatternRequest {
					issue = "optional_api_unused" // Request APIs are optional
				}
				if port.Pattern == PatternWatch {
					issue = "optional_index_unwatched" // KV indexes may be intentionally unwatched
				}

				orphaned = append(orphaned, OrphanedPort{
					ComponentName: componentName,
					PortName:      port.Name,
					Direction:     port.Direction,
					ConnectionID:  port.ConnectionID,
					Pattern:       port.Pattern,
					Issue:         issue,
					Required:      port.Required,
				})
			}
		}
	}

	return orphaned
}

// isInterfaceAlternativePort determines if a port is an optional interface-specific alternative
func (g *FlowGraph) isInterfaceAlternativePort(port PortInfo) bool {
	// Heuristic: A port is likely an interface alternative if:
	// 1. It has an interface contract specified
	// 2. It's not marked as required
	// 3. Port name suggests it's a specialized variant (contains "-" or specific suffixes)

	if port.Interface == nil {
		return false // No interface contract, not an interface alternative
	}

	if port.Required {
		return false // Required ports are not optional alternatives
	}

	// Check for naming patterns that suggest specialized variants
	// Examples: "write-graphable", "input-typed", "data-validated"
	if strings.Contains(port.Name, "-") {
		// Check if there's a base port name without the suffix
		// e.g., "write-graphable" suggests "write" is the primary port
		baseName := strings.Split(port.Name, "-")[0]
		if baseName != "" && baseName != port.Name {
			return true
		}
	}

	// Additional heuristic: ports with strict interface contracts and
	// subjects containing ".graphable" or similar patterns
	if strings.Contains(port.ConnectionID, ".graphable") ||
		strings.Contains(port.ConnectionID, ".typed") ||
		strings.Contains(port.ConnectionID, ".validated") {
		return true
	}

	return false
}
