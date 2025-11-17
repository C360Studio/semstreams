package graphclustering

import (
	"context"

	"github.com/c360/semstreams/pkg/graphinterfaces"
)

// Ensure Community implements the graphinterfaces.Community interface
var _ graphinterfaces.Community = (*Community)(nil)

// Community represents a detected community/cluster in the graph
type Community struct {
	// ID is the unique identifier for this community
	ID string

	// Level indicates the hierarchy level (0=bottom, 1=mid, 2=top)
	Level int

	// Members contains the entity IDs belonging to this community
	Members []string

	// ParentID references the parent community at the next level up (nil for top level)
	ParentID *string

	// StatisticalSummary is the fast statistical baseline summary (always present)
	// Generated using TF-IDF keyword extraction and template-based summarization
	StatisticalSummary string `json:"statistical_summary,omitempty"`

	// LLMSummary is the enhanced LLM-generated summary (populated asynchronously)
	// Empty until LLM enhancement completes successfully
	LLMSummary string `json:"llm_summary,omitempty"`

	// Keywords are extracted key terms representing this community's themes
	// e.g., ["autonomous", "navigation", "sensor-fusion"]
	Keywords []string `json:"keywords,omitempty"`

	// RepEntities contains IDs of representative entities within this community
	// These entities best exemplify the community's characteristics
	RepEntities []string `json:"rep_entities,omitempty"`

	// SummaryStatus tracks the summarization state
	// Values: "statistical" (initial), "llm-enhanced" (enhanced), "llm-failed" (enhancement failed)
	SummaryStatus string `json:"summary_status,omitempty"`

	// Metadata stores additional community properties
	Metadata map[string]interface{}
}

// CommunityDetector performs community detection on a graph
type CommunityDetector interface {
	// DetectCommunities runs community detection on the entire graph
	// Returns communities organized by hierarchical level
	DetectCommunities(ctx context.Context) (map[int][]*Community, error)

	// UpdateCommunities incrementally updates communities based on recent graph changes
	// entityIDs are entities that have been added/modified since last detection
	UpdateCommunities(ctx context.Context, entityIDs []string) error

	// GetCommunity retrieves a specific community by ID
	GetCommunity(ctx context.Context, id string) (*Community, error)

	// GetEntityCommunity returns the community containing the given entity
	// level specifies which hierarchical level to query (0=bottom, 1=mid, 2=top)
	GetEntityCommunity(ctx context.Context, entityID string, level int) (*Community, error)

	// GetCommunitiesByLevel returns all communities at a specific hierarchical level
	GetCommunitiesByLevel(ctx context.Context, level int) ([]*Community, error)
}

// GraphProvider abstracts the graph data source for community detection
type GraphProvider interface {
	// GetAllEntityIDs returns all entity IDs in the graph
	GetAllEntityIDs(ctx context.Context) ([]string, error)

	// GetNeighbors returns the entity IDs connected to the given entity
	// direction: "outgoing", "incoming", or "both"
	GetNeighbors(ctx context.Context, entityID string, direction string) ([]string, error)

	// GetEdgeWeight returns the weight of the edge between two entities
	// Returns 1.0 if edge exists but has no weight, 0.0 if no edge exists
	GetEdgeWeight(ctx context.Context, fromID, toID string) (float64, error)
}

// CommunityStorage abstracts persistence layer for communities
type CommunityStorage interface {
	// SaveCommunity persists a community
	SaveCommunity(ctx context.Context, community *Community) error

	// GetCommunity retrieves a community by ID
	GetCommunity(ctx context.Context, id string) (*Community, error)

	// GetCommunitiesByLevel retrieves all communities at a level
	GetCommunitiesByLevel(ctx context.Context, level int) ([]*Community, error)

	// GetEntityCommunity retrieves the community for an entity at a level
	GetEntityCommunity(ctx context.Context, entityID string, level int) (*Community, error)

	// DeleteCommunity removes a community
	DeleteCommunity(ctx context.Context, id string) error

	// Clear removes all communities (useful for full recomputation)
	Clear(ctx context.Context) error
}

// Getter methods to implement graphinterfaces.Community interface

// GetID returns the unique identifier for this community
func (c *Community) GetID() string {
	return c.ID
}

// GetLevel returns the hierarchy level (0=bottom, 1=mid, 2=top)
func (c *Community) GetLevel() int {
	return c.Level
}

// GetMembers returns the entity IDs belonging to this community
func (c *Community) GetMembers() []string {
	return c.Members
}

// GetParentID returns the parent community ID at the next level up (nil for top level)
func (c *Community) GetParentID() *string {
	return c.ParentID
}

// GetKeywords returns the extracted key terms representing this community's themes
func (c *Community) GetKeywords() []string {
	return c.Keywords
}

// GetRepEntities returns IDs of representative entities within this community
func (c *Community) GetRepEntities() []string {
	return c.RepEntities
}

// GetMetadata returns additional community properties
func (c *Community) GetMetadata() map[string]interface{} {
	return c.Metadata
}

// GetStatisticalSummary returns the fast statistical baseline summary
func (c *Community) GetStatisticalSummary() string {
	return c.StatisticalSummary
}

// GetLLMSummary returns the enhanced LLM-generated summary (may be empty)
func (c *Community) GetLLMSummary() string {
	return c.LLMSummary
}

// GetSummaryStatus returns the summarization state
func (c *Community) GetSummaryStatus() string {
	return c.SummaryStatus
}
