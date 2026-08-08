// Package llm provides LLM client and prompt templates for graph processing.
package llm

import (
	"context"

	gtypes "github.com/c360studio/semstreams/graph"
)

// ContentFetcher abstracts content retrieval for LLM prompts.
// Implementations are injected by the owning composition and may use registered
// Store access or mocks.
type ContentFetcher interface {
	// FetchEntityContent retrieves title/abstract for entities with StorageRefs.
	// Returns map[entityID]*EntityContent for successful fetches.
	// Missing content is not an error - returns partial results gracefully.
	// Entities without StorageRef are skipped silently.
	FetchEntityContent(ctx context.Context, entities []*gtypes.EntityState) (map[string]*EntityContent, error)
}
