// Package graphinterfaces provides shared interfaces to break import cycles
// between graph-related packages (graphclustering, querymanager, etc.)
package graphinterfaces

// Community represents a community/cluster in the graph
// This interface allows querymanager to work with communities without
// directly importing the graphclustering package (avoiding import cycles)
type Community interface {
	// GetID returns the unique identifier for this community
	GetID() string

	// GetLevel returns the hierarchy level (0=bottom, 1=mid, 2=top)
	GetLevel() int

	// GetMembers returns the entity IDs belonging to this community
	GetMembers() []string

	// GetParentID returns the parent community ID at the next level up (nil for top level)
	GetParentID() *string

	// GetKeywords returns the extracted key terms representing this community's themes
	GetKeywords() []string

	// GetRepEntities returns IDs of representative entities within this community
	GetRepEntities() []string

	// GetStatisticalSummary returns the fast statistical baseline summary (always present)
	GetStatisticalSummary() string

	// GetLLMSummary returns the enhanced LLM-generated summary (may be empty until enhanced)
	GetLLMSummary() string

	// GetSummaryStatus returns the summarization state ("statistical", "llm-enhanced", "llm-failed")
	GetSummaryStatus() string

	// GetMetadata returns additional community properties
	GetMetadata() map[string]interface{}
}
