// Package graph provides types for entity state storage in the graph system.
package graph

import (
	"encoding/json"
	"time"

	"github.com/c360/semstreams/message"
)

// EntityStatus represents the operational status of an entity in the graph.
// This enum provides type-safe status values for entity monitoring and alerting.
type EntityStatus string

const (
	// StatusActive indicates the entity is operating normally.
	// This is the default healthy state for most entities.
	StatusActive EntityStatus = "active"

	// StatusWarning indicates the entity has detected issues but is still operational.
	// Example: low battery, performance degradation, minor failures
	StatusWarning EntityStatus = "warning"

	// StatusCritical indicates the entity has serious issues that require immediate attention.
	// Example: very low battery, major component failure, safety concerns
	StatusCritical EntityStatus = "critical"

	// StatusEmergency indicates the entity is in an emergency state requiring immediate intervention.
	// Example: critical battery level, system failure, safety hazard
	StatusEmergency EntityStatus = "emergency"

	// StatusInactive indicates the entity is not currently active or operational.
	// Example: powered down, offline, disabled
	StatusInactive EntityStatus = "inactive"

	// StatusUnknown indicates the entity status cannot be determined.
	// This is used when status information is unavailable or invalid.
	StatusUnknown EntityStatus = "unknown"
)

// String returns the string representation of the EntityStatus.
func (es EntityStatus) String() string {
	return string(es)
}

// MarshalJSON implements json.Marshaler to ensure EntityStatus serializes as a string.
func (es EntityStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(es))
}

// UnmarshalJSON implements json.Unmarshaler to deserialize EntityStatus from string.
// This provides backward compatibility with existing string status values.
func (es *EntityStatus) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*es = EntityStatus(s)
	return nil
}

// IsValid checks if the EntityStatus is one of the defined constants.
func (es EntityStatus) IsValid() bool {
	switch es {
	case StatusActive, StatusWarning, StatusCritical, StatusEmergency, StatusInactive, StatusUnknown:
		return true
	default:
		return false
	}
}

// IsHealthy returns true if the entity status indicates normal operation.
func (es EntityStatus) IsHealthy() bool {
	return es == StatusActive
}

// NeedsAttention returns true if the entity status indicates issues requiring attention.
func (es EntityStatus) NeedsAttention() bool {
	return es == StatusWarning || es == StatusCritical || es == StatusEmergency
}

// IsOperational returns true if the entity is currently operational (active or warning).
func (es EntityStatus) IsOperational() bool {
	return es == StatusActive || es == StatusWarning
}

// EntityState represents complete local graph state for an entity
type EntityState struct {
	Node      NodeProperties   `json:"node"`       // Entity properties
	Edges     []Edge           `json:"edges"`      // Outgoing edges ONLY
	Triples   []message.Triple `json:"triples"`    // Semantic triples (properties + relationships)
	ObjectRef string           `json:"object_ref"` // Reference to full message in ObjectStore
	Version   uint64           `json:"version"`    // Explicit versioning for conflict resolution
	UpdatedAt time.Time        `json:"updated_at"`
}

// NodeProperties contains entity identification and query-essential properties
type NodeProperties struct {
	ID         string         `json:"id"`         // e.g., "drone_001"
	Type       string         `json:"type"`       // e.g., "robotics.drone"
	Properties map[string]any `json:"properties"` // Query-essential only
	Position   *Position      `json:"position,omitempty"`
	Status     EntityStatus   `json:"status"` // Entity operational status
}

// Edge represents a directed relationship (outgoing only)
type Edge struct {
	ToEntityID string         `json:"to_entity_id"`
	EdgeType   string         `json:"edge_type"`            // "POWERED_BY", "NEAR", etc.
	Weight     float64        `json:"weight,omitempty"`     // Distance, strength, etc.
	Confidence float64        `json:"confidence,omitempty"` // 0.0-1.0
	Properties map[string]any `json:"properties,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	ExpiresAt  *time.Time     `json:"expires_at,omitempty"` // For temporary relationships
}

// Position represents geographic location
type Position struct {
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lon"`
	Altitude  float64 `json:"alt,omitempty"`
}

// AddEdge adds or updates an edge
func (es *EntityState) AddEdge(edge Edge) {
	// Replace existing edge of same type to same entity
	for i, e := range es.Edges {
		if e.EdgeType == edge.EdgeType && e.ToEntityID == edge.ToEntityID {
			es.Edges[i] = edge
			return
		}
	}
	es.Edges = append(es.Edges, edge)
}

// RemoveEdge removes an edge to the specified entity, optionally matching edge type
// Returns true if an edge was removed, false if not found
func (es *EntityState) RemoveEdge(toEntityID string, edgeType string) bool {
	for i, edge := range es.Edges {
		if edge.ToEntityID == toEntityID && (edgeType == "" || edge.EdgeType == edgeType) {
			// Remove edge by slicing
			es.Edges = append(es.Edges[:i], es.Edges[i+1:]...)
			return true
		}
	}
	return false
}

// RemoveExpiredEdges removes edges past expiration
func (es *EntityState) RemoveExpiredEdges() {
	now := time.Now()
	filtered := es.Edges[:0]
	for _, edge := range es.Edges {
		if edge.ExpiresAt == nil || edge.ExpiresAt.After(now) {
			filtered = append(filtered, edge)
		}
	}
	es.Edges = filtered
}

// UpdateProperties merges new properties with existing
func (np *NodeProperties) UpdateProperties(updates map[string]any) {
	if np.Properties == nil {
		np.Properties = make(map[string]any)
	}
	for k, v := range updates {
		np.Properties[k] = v
	}
}
