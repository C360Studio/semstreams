// Package graph provides query types for graph operations
package graph

// QueryDirection represents the direction of a query
type QueryDirection int

const (
	// QueryDirectionOutgoing queries outgoing edges/relationships
	QueryDirectionOutgoing QueryDirection = iota
	// QueryDirectionIncoming queries incoming edges/relationships
	QueryDirectionIncoming
	// QueryDirectionBidirectional queries both directions
	QueryDirectionBidirectional
)

// String returns the string representation of QueryDirection
func (qd QueryDirection) String() string {
	switch qd {
	case QueryDirectionOutgoing:
		return "outgoing"
	case QueryDirectionIncoming:
		return "incoming"
	case QueryDirectionBidirectional:
		return "bidirectional"
	default:
		return "unknown"
	}
}

// EntityCriteria represents criteria for querying entities
type EntityCriteria struct {
	EntityID   string         `json:"entity_id,omitempty"`
	Type       string         `json:"type,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
}

// RelationshipCriteria represents criteria for querying relationships
type RelationshipCriteria struct {
	FromID     string         `json:"from_id,omitempty"`
	ToID       string         `json:"to_id,omitempty"`
	Type       string         `json:"type,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Direction  QueryDirection `json:"direction,omitempty"`
}

// QueryResult represents the result of a graph query
type QueryResult struct {
	Entities      []map[string]any `json:"entities"`
	Relationships []map[string]any `json:"relationships"`
	Count         int              `json:"count"`
}
