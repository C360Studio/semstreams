// Package flowgraph provides flow graph analysis and validation for component connections.
package flowgraph

import (
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/pkg/errs"
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
	Pattern      component.InteractionPattern
	Interface    *component.InterfaceContract
	Required     bool // Whether this port is required for the component to function
	kind         component.PortKind
	exclusive    bool
	natsSubjects []string
}

// FlowEdge represents a connection between two component ports
type FlowEdge struct {
	From         ComponentPortRef             `json:"from"`
	To           ComponentPortRef             `json:"to"`
	Pattern      component.InteractionPattern `json:"pattern"`
	ConnectionID string                       `json:"connection_id"` // Subject, bucket, or network addr
	Metadata     EdgeMetadata                 `json:"metadata"`      // Pattern-specific validation data
}

// ComponentPortRef references a specific port on a component
type ComponentPortRef struct {
	ComponentName string `json:"component_name"`
	PortName      string `json:"port_name"`
}

// Issue type constants for orphaned port classification
const (
	IssueNoPublishers            = "no_publishers"
	IssueNoSubscribers           = "no_subscribers"
	IssueOptionalAPIUnused       = "optional_api_unused"
	IssueOptionalIndexUnwatched  = "optional_index_unwatched"
	IssueOptionalInterfaceUnused = "optional_interface_unused"
)

// EdgeMetadata contains pattern-specific metadata
type EdgeMetadata struct {
	InterfaceContract *component.InterfaceContract `json:"interface_contract,omitempty"`
	Timeout           string                       `json:"timeout,omitempty"` // Request pattern
	Queue             string                       `json:"queue,omitempty"`   // Stream pattern
	Keys              []string                     `json:"keys,omitempty"`    // Watch pattern
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
		for index := range nodeCopy.InputPorts {
			nodeCopy.InputPorts[index].natsSubjects = append([]string(nil), v.InputPorts[index].natsSubjects...)
			if contract := v.InputPorts[index].Interface; contract != nil {
				copyOfContract := *contract
				copyOfContract.Compatible = append([]string(nil), contract.Compatible...)
				nodeCopy.InputPorts[index].Interface = &copyOfContract
			}
		}
		for index := range nodeCopy.OutputPorts {
			nodeCopy.OutputPorts[index].natsSubjects = append([]string(nil), v.OutputPorts[index].natsSubjects...)
			if contract := v.OutputPorts[index].Interface; contract != nil {
				copyOfContract := *contract
				copyOfContract.Compatible = append([]string(nil), contract.Compatible...)
				nodeCopy.OutputPorts[index].Interface = &copyOfContract
			}
		}

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
		return errs.WrapInvalid(errs.ErrInvalidConfig, "FlowGraph", "AddComponentNode", "component name cannot be empty")
	}
	if comp == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "FlowGraph", "AddComponentNode", "component cannot be nil")
	}
	if _, exists := g.nodes[name]; exists {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "FlowGraph", "AddComponentNode", fmt.Sprintf("component %s already exists", name))
	}

	inputPorts, err := g.extractPortInfo(comp.InputPorts())
	if err != nil {
		return fmt.Errorf("component %q input ports: %w", name, err)
	}
	outputPorts, err := g.extractPortInfo(comp.OutputPorts())
	if err != nil {
		return fmt.Errorf("component %q output ports: %w", name, err)
	}
	node := &ComponentNode{
		ComponentName: name,
		Component:     comp,
		InputPorts:    inputPorts,
		OutputPorts:   outputPorts,
	}

	g.nodes[name] = node
	return nil
}

// extractPortInfo converts component ports to PortInfo for graph analysis
func (g *FlowGraph) extractPortInfo(ports []component.Port) ([]PortInfo, error) {
	result := make([]PortInfo, 0, len(ports))

	for _, port := range ports {
		facts, err := port.Facts()
		if err != nil {
			return nil, err
		}
		connections := facts.ConnectionIDs()
		if len(connections) == 0 {
			return nil, fmt.Errorf("port %q has no connection identity", port.Name)
		}
		var interfaceContract *component.InterfaceContract
		if contract, ok := facts.Interface(); ok {
			interfaceContract = &contract
		}
		portInfo := PortInfo{
			Name:         port.Name,
			Direction:    port.Direction,
			ConnectionID: connections[0],
			Pattern:      facts.InteractionPattern(),
			Interface:    interfaceContract,
			Required:     port.Required,
			kind:         facts.Kind(),
			exclusive:    facts.IsExclusive(),
			natsSubjects: facts.NATSSubjects(),
		}

		result = append(result, portInfo)
	}

	return result, nil
}

// ConnectComponentsByPatterns builds edges by matching connection patterns
func (g *FlowGraph) ConnectComponentsByPatterns() error {
	// Clear existing edges
	g.edges = g.edges[:0]
	if err := g.validateGraphMutationProviders(); err != nil {
		return err
	}

	// Build connection maps by pattern and connection ID
	publishers := g.buildPublisherMap()   // Output ports
	subscribers := g.buildSubscriberMap() // Input ports

	var warnings []string

	// Connect based on interaction patterns
	g.connectStreamPorts(publishers[component.PatternStream], subscribers[component.PatternStream])
	g.connectRequestPorts(publishers[component.PatternRequest], subscribers[component.PatternRequest])
	g.validateKVWriters(publishers[component.PatternWatch], &warnings)
	g.connectKVPorts(publishers[component.PatternWatch], subscribers[component.PatternWatch], component.PatternWatch)
	g.connectKVPorts(publishers[component.PatternWatch], subscribers[component.PatternRead], component.PatternRead)
	g.connectStorePorts(publishers[component.PatternStore], subscribers[component.PatternStore])

	// Validate network ports for conflicts
	conflicts := g.validateNetworkPorts(publishers[component.PatternNetwork], subscribers[component.PatternNetwork])
	warnings = append(warnings, conflicts...)

	// Return error if there are critical warnings
	if len(warnings) > 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "FlowGraph", "ConnectComponentsByPatterns", fmt.Sprintf("validation warnings: %v", warnings))
	}

	return nil
}

// validateGraphMutationProviders validates only the declared component graph.
// It does not inspect NATS subscriptions or processes and therefore makes no
// leader-election or account-wide cardinality claim.
func (g *FlowGraph) validateGraphMutationProviders() error {
	candidates := 0
	providers := 0
	for componentName, node := range g.nodes {
		for _, port := range append(append([]PortInfo(nil), node.InputPorts...), node.OutputPorts...) {
			isMutation := (port.Pattern == component.PatternRequest && strings.HasPrefix(port.ConnectionID, "graph.mutation.")) ||
				(port.Interface != nil && port.Interface.Type == graphmutation.InterfaceType)
			if !isMutation {
				continue
			}
			candidates++
			if port.Pattern != component.PatternRequest || port.ConnectionID != graphmutation.SubjectFamily ||
				port.Interface == nil || port.Interface.Type != graphmutation.InterfaceType ||
				port.Interface.Version != graphmutation.InterfaceVersion || !port.Required {
				return errs.WrapInvalid(errs.ErrInvalidConfig, "FlowGraph", "validateGraphMutationProviders",
					fmt.Sprintf("component %s port %s is not a required nats-request %s %s port on %s",
						componentName, port.Name, graphmutation.InterfaceType, graphmutation.InterfaceVersion,
						graphmutation.SubjectFamily))
			}
			if port.Direction == component.DirectionInput {
				providers++
			}
		}
	}
	if candidates == 0 {
		return nil
	}
	if providers != 1 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "FlowGraph", "validateGraphMutationProviders",
			fmt.Sprintf("graph mutation flow requires exactly one compatible provider, found %d", providers))
	}
	return nil
}

// buildPublisherMap creates a map of connection IDs to output ports by pattern.
// For JetStream ports that have a StreamName connection ID, extra entries are
// added for each Subject in the Subjects list so that NATS subject pattern
// matching in connectStreamPorts can find them.
func (g *FlowGraph) buildPublisherMap() map[component.InteractionPattern]map[string][]ComponentPortRef {
	publishers := make(map[component.InteractionPattern]map[string][]ComponentPortRef)

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

			// For JetStream ports keyed by StreamName, also add entries for each
			// subject so connectStreamPorts can match them via NATS pattern matching.
			if port.kind == component.PortKindJetStream {
				if port.ConnectionID != "" {
					for _, subject := range port.natsSubjects {
						if subject != "" && subject != port.ConnectionID {
							publishers[port.Pattern][subject] = append(
								publishers[port.Pattern][subject],
								portRef,
							)
						}
					}
				}
			}
		}
	}

	return publishers
}

// buildSubscriberMap creates a map of connection IDs to input ports by pattern.
// For JetStream ports that have a StreamName connection ID, extra entries are
// added for each Subject in the Subjects list so that NATS subject pattern
// matching in connectStreamPorts can find them.
func (g *FlowGraph) buildSubscriberMap() map[component.InteractionPattern]map[string][]ComponentPortRef {
	subscribers := make(map[component.InteractionPattern]map[string][]ComponentPortRef)

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

			// For JetStream ports keyed by StreamName, also add entries for each
			// subject so connectStreamPorts can match them via NATS pattern matching.
			if port.kind == component.PortKindJetStream {
				if port.ConnectionID != "" {
					for _, subject := range port.natsSubjects {
						if subject != "" && subject != port.ConnectionID {
							subscribers[port.Pattern][subject] = append(
								subscribers[port.Pattern][subject],
								portRef,
							)
						}
					}
				}
			}
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

// connectStreamPorts connects stream pattern ports (NATS, JetStream).
// A deduplication set prevents the same (publisher, subscriber) pair from
// producing multiple edges when a JetStream port is indexed under both its
// StreamName and its individual subjects.
func (g *FlowGraph) connectStreamPorts(publishers, subscribers map[string][]ComponentPortRef) {
	type edgeKey struct {
		from, to ComponentPortRef
	}
	seen := make(map[edgeKey]bool)

	// Stream pattern: publishers -> subscribers with NATS pattern matching
	for pubConnID, pubs := range publishers {
		for subConnID, subs := range subscribers {
			// Check if publisher subject matches subscriber pattern or vice versa
			if matchNATSPattern(pubConnID, subConnID) || matchNATSPattern(subConnID, pubConnID) {
				// Use the more concrete (non-wildcard) connection ID for the edge.
				connID := pubConnID
				if strings.Contains(pubConnID, "*") || strings.Contains(pubConnID, ">") {
					connID = subConnID
				}
				// Connect all matching publishers to subscribers
				for _, pub := range pubs {
					for _, sub := range subs {
						// Avoid self-connections and duplicate edges
						if pub.ComponentName == sub.ComponentName {
							continue
						}
						k := edgeKey{pub, sub}
						if seen[k] {
							continue
						}
						seen[k] = true
						g.edges = append(g.edges, FlowEdge{
							From:         pub,
							To:           sub,
							Pattern:      component.PatternStream,
							ConnectionID: connID,
							Metadata:     EdgeMetadata{},
						})
					}
				}
			}
		}
	}
}

// connectStorePorts renders the ADR-063 store federation: every store-provide
// (a storage component owning a StorageInstance) connects to every store-read (a
// content-fetch consumer). Advisory/visibility only — a consumer resolves a
// producer-chosen instance per-fetch at runtime and MAY read any registered
// instance, so "can read from" is the honest edge; the exact pairing is dynamic
// and deliberately not asserted here. The edge connID is the provider's
// store:<instance>.
func (g *FlowGraph) connectStorePorts(publishers, subscribers map[string][]ComponentPortRef) {
	type edgeKey struct{ from, to ComponentPortRef }
	seen := make(map[edgeKey]bool)

	for pubConnID, pubs := range publishers {
		for _, subs := range subscribers {
			for _, pub := range pubs {
				for _, sub := range subs {
					if pub.ComponentName == sub.ComponentName {
						continue
					}
					k := edgeKey{pub, sub}
					if seen[k] {
						continue
					}
					seen[k] = true
					g.edges = append(g.edges, FlowEdge{
						From:         pub,
						To:           sub,
						Pattern:      component.PatternStore,
						ConnectionID: pubConnID,
						Metadata:     EdgeMetadata{},
					})
				}
			}
		}
	}
}

// connectRequestPorts connects request pattern ports (bidirectional NATS request-reply).
// Unlike the original exact-key grouping, this uses NATS pattern matching across all
// publisher/subscriber connection IDs so that wildcard subjects (e.g. graph.query.*)
// and concrete subjects (e.g. graph.query.search) are recognised as the same endpoint.
func (g *FlowGraph) connectRequestPorts(publishers, subscribers map[string][]ComponentPortRef) {
	// Track edges we have already created to avoid duplicates. We key on a
	// canonical (sorted) pair of ComponentPortRefs so the check is direction-agnostic.
	type edgeKey struct {
		a, b ComponentPortRef // a.ComponentName <= b.ComponentName (lexicographic)
	}
	seen := make(map[edgeKey]bool)

	addEdge := func(p1, p2 ComponentPortRef, connID string) {
		// Avoid self-connections
		if p1.ComponentName == p2.ComponentName {
			return
		}
		// Canonical ordering
		a, b := p1, p2
		if a.ComponentName > b.ComponentName ||
			(a.ComponentName == b.ComponentName && a.PortName > b.PortName) {
			a, b = b, a
		}
		k := edgeKey{a, b}
		if seen[k] {
			return
		}
		seen[k] = true
		g.edges = append(g.edges, FlowEdge{
			From:         p1,
			To:           p2,
			Pattern:      component.PatternRequest,
			ConnectionID: connID,
			Metadata:     EdgeMetadata{},
		})
	}

	// Cross-match every publisher connection ID against every subscriber connection
	// ID using NATS pattern matching (handles * and > wildcards in either position).
	for pubConnID, pubs := range publishers {
		for subConnID, subs := range subscribers {
			if matchNATSPattern(pubConnID, subConnID) || matchNATSPattern(subConnID, pubConnID) {
				// Use the more concrete (non-wildcard) ID as the edge connection ID.
				connID := pubConnID
				if strings.Contains(pubConnID, "*") || strings.Contains(pubConnID, ">") {
					connID = subConnID
				}
				for _, pub := range pubs {
					for _, sub := range subs {
						addEdge(pub, sub, connID)
					}
				}
			}
		}
	}

	// Also connect ports that are both publishers (output) and subscribers (input)
	// on matching subjects — request ports are bidirectional so either side may
	// appear in either map.
	for pubConnID1, ports1 := range publishers {
		for pubConnID2, ports2 := range publishers {
			if pubConnID1 == pubConnID2 {
				continue
			}
			if matchNATSPattern(pubConnID1, pubConnID2) || matchNATSPattern(pubConnID2, pubConnID1) {
				connID := pubConnID1
				if strings.Contains(pubConnID1, "*") || strings.Contains(pubConnID1, ">") {
					connID = pubConnID2
				}
				for _, p1 := range ports1 {
					for _, p2 := range ports2 {
						addEdge(p1, p2, connID)
					}
				}
			}
		}
	}
	for subConnID1, ports1 := range subscribers {
		for subConnID2, ports2 := range subscribers {
			if subConnID1 == subConnID2 {
				continue
			}
			if matchNATSPattern(subConnID1, subConnID2) || matchNATSPattern(subConnID2, subConnID1) {
				connID := subConnID1
				if strings.Contains(subConnID1, "*") || strings.Contains(subConnID1, ">") {
					connID = subConnID2
				}
				for _, p1 := range ports1 {
					for _, p2 := range ports2 {
						addEdge(p1, p2, connID)
					}
				}
			}
		}
	}
}

// connectWatchPorts connects watch pattern ports (KV bucket observation)
func (g *FlowGraph) validateKVWriters(publishers map[string][]ComponentPortRef, warnings *[]string) {
	for connectionID, pubs := range publishers {
		if len(pubs) > 1 {
			*warnings = append(*warnings, fmt.Sprintf("multiple writers to KV bucket %s: %v", connectionID, pubs))
		}
	}
}

// connectKVPorts connects KV writers to either change watchers or exact readers
// without collapsing their distinct interaction semantics.
func (g *FlowGraph) connectKVPorts(publishers, subscribers map[string][]ComponentPortRef, pattern component.InteractionPattern) {
	for connectionID, pubs := range publishers {
		if subs, exists := subscribers[connectionID]; exists {
			for _, pub := range pubs {
				for _, sub := range subs {
					edge := FlowEdge{
						From:         pub,
						To:           sub,
						Pattern:      pattern,
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
	for connID, candidates := range publishers {
		ports := g.exclusivePortRefs(candidates, component.DirectionOutput)
		if len(ports) > 1 {
			conflicts = append(conflicts,
				fmt.Sprintf("network port conflict on %s: multiple components binding: %v", connID, ports))
		}
		if len(ports) != 0 {
			allPorts[connID] = ports
		}
	}

	// Check if subscribers conflict with publishers (both trying to bind same port)
	for connID, candidates := range subscribers {
		ports := g.exclusivePortRefs(candidates, component.DirectionInput)
		if len(ports) == 0 {
			continue
		}
		if existing, exists := allPorts[connID]; exists {
			conflicts = append(conflicts,
				fmt.Sprintf("network port conflict on %s: %v and %v both trying to bind", connID, existing, ports))
		} else if len(ports) > 1 {
			conflicts = append(conflicts,
				fmt.Sprintf("network port conflict on %s: multiple components binding: %v", connID, ports))
		}
	}

	// Network ports are external connections - no edges created in the graph
	return conflicts
}

func (g *FlowGraph) exclusivePortRefs(candidates []ComponentPortRef, direction component.Direction) []ComponentPortRef {
	result := make([]ComponentPortRef, 0, len(candidates))
	for _, candidate := range candidates {
		node := g.nodes[candidate.ComponentName]
		if node == nil {
			continue
		}
		ports := node.InputPorts
		if direction == component.DirectionOutput {
			ports = node.OutputPorts
		}
		for _, port := range ports {
			if port.Name == candidate.PortName && port.exclusive {
				result = append(result, candidate)
				break
			}
		}
	}
	return result
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
				Issue:         "component has no connections",
				Suggestions:   []string{"connect to other components", "verify component configuration"},
			})
		}
	}

	// Determine validation status based on severity
	hasCriticalIssues := false
	for _, port := range result.OrphanedPorts {
		// Check if this is a critical issue
		if port.Issue == IssueNoPublishers || port.Issue == IssueNoSubscribers {
			// Only required stream connections are critical
			// Optional ports without connections are acceptable
			if port.Pattern == component.PatternStream && port.Required {
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
				// Skip external boundary inputs — they ARE the external source.
				// component.PatternNetwork binds a local listener; component.PatternHTTPClient initiates
				// an outbound connection. Both are legitimately unmatched in the
				// internal graph because their "publisher" is outside the system.
				// component.PatternStore (ADR-063) is a federation-capability declaration whose
				// pairing is producer-chosen at runtime — an unmatched store-read is
				// not orphaned. component.PatternTimer is a cadence/scheduler boundary that
				// drives the component on an interval, not a data edge.
				if port.Pattern == component.PatternNetwork || port.Pattern == component.PatternHTTPClient || port.Pattern == component.PatternStore || port.Pattern == component.PatternTimer {
					continue // Not orphaned, it's an external/federation/cadence input
				}

				// Determine issue type based on pattern
				issue := IssueNoPublishers
				if port.Pattern == component.PatternRequest {
					issue = IssueOptionalAPIUnused // Request APIs are optional
				} else if g.isInterfaceAlternativePort(port) {
					// Interface-specific alternatives are optional specialized paths
					issue = IssueOptionalInterfaceUnused
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
				// Skip external boundary outputs — they ARE the external sink.
				// component.PatternHTTPClient is also skipped: the component owns the
				// outbound connection so no internal subscriber is expected.
				// component.PatternStore (ADR-063): a store-provide with no consumer is a
				// legitimately-unread store, not an orphan. component.PatternTimer is a
				// cadence boundary, symmetric with the input side.
				if port.Pattern == component.PatternNetwork || port.Pattern == component.PatternHTTPClient || port.Pattern == component.PatternStore || port.Pattern == component.PatternTimer {
					continue // Not orphaned, it's an external/federation output
				}

				// Determine issue type based on pattern
				issue := IssueNoSubscribers
				if port.Pattern == component.PatternRequest {
					issue = IssueOptionalAPIUnused // Request APIs are optional
				}
				if port.Pattern == component.PatternWatch {
					issue = IssueOptionalIndexUnwatched // KV indexes may be intentionally unwatched
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

// ValidateStreamRequirements checks that JetStream subscribers have corresponding
// JetStream publishers. When a component subscribes via JetStream, it expects a
// durable stream to exist. Streams are only created by components that publish
// with JetStream output ports (via EnsureStreams). If a JetStream subscriber is
// connected only to NATS publishers, no stream will be created and the subscriber
// will hang waiting for a stream that never appears.
func (g *FlowGraph) ValidateStreamRequirements() []StreamRequirementWarning {
	var warnings []StreamRequirementWarning

	// For each edge, check if the subscriber is JetStream and publisher is NATS
	for _, edge := range g.edges {
		if edge.Pattern != component.PatternStream {
			continue
		}

		// Get the subscriber's port info
		subscriberNode, ok := g.nodes[edge.To.ComponentName]
		if !ok {
			continue
		}

		var subscriberPort *PortInfo
		for i := range subscriberNode.InputPorts {
			if subscriberNode.InputPorts[i].Name == edge.To.PortName {
				subscriberPort = &subscriberNode.InputPorts[i]
				break
			}
		}
		if subscriberPort == nil {
			continue
		}

		// Check if subscriber is JetStream
		if subscriberPort.kind != component.PortKindJetStream {
			continue // Subscriber is not JetStream, no stream requirement
		}

		// Get the publisher's port info
		publisherNode, ok := g.nodes[edge.From.ComponentName]
		if !ok {
			continue
		}

		var publisherPort *PortInfo
		for i := range publisherNode.OutputPorts {
			if publisherNode.OutputPorts[i].Name == edge.From.PortName {
				publisherPort = &publisherNode.OutputPorts[i]
				break
			}
		}
		if publisherPort == nil {
			continue
		}

		// Check if publisher is NOT JetStream (i.e., won't create stream)
		if publisherPort.kind == component.PortKindJetStream {
			continue // Publisher is JetStream, will create stream - OK
		}

		// Publisher is NATS but subscriber expects JetStream - this is a problem!
		warnings = append(warnings, StreamRequirementWarning{
			Severity:       "critical",
			SubscriberComp: edge.To.ComponentName,
			SubscriberPort: edge.To.PortName,
			Subjects:       append([]string(nil), subscriberPort.natsSubjects...),
			PublisherComps: []string{edge.From.ComponentName},
			Issue: fmt.Sprintf(
				"JetStream subscriber expects stream for subjects %v but publisher '%s' uses NATS (no stream will be created)",
				subscriberPort.natsSubjects, edge.From.ComponentName,
			),
		})
	}

	// Deduplicate warnings by subscriber port (multiple publishers may connect to same subscriber)
	deduped := make(map[string]*StreamRequirementWarning)
	for i := range warnings {
		w := &warnings[i]
		key := fmt.Sprintf("%s:%s", w.SubscriberComp, w.SubscriberPort)
		if existing, ok := deduped[key]; ok {
			// Merge publisher components
			existing.PublisherComps = append(existing.PublisherComps, w.PublisherComps...)
		} else {
			deduped[key] = w
		}
	}

	result := make([]StreamRequirementWarning, 0, len(deduped))
	for _, w := range deduped {
		result = append(result, *w)
	}

	return result
}
