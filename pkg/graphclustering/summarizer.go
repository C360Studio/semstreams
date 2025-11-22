package graphclustering

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/c360/semstreams/errors"
	gtypes "github.com/c360/semstreams/graph"
)

// CommunitySummarizer generates summaries for communities
type CommunitySummarizer interface {
	// SummarizeCommunity generates a summary for a community
	// Returns updated Community with Summary, Keywords, RepEntities, and Summarizer fields populated
	SummarizeCommunity(ctx context.Context, community *Community, entities []*gtypes.EntityState) (*Community, error)
}

// StatisticalSummarizer implements CommunitySummarizer using statistical methods
// This is the default summarizer that doesn't require external LLM services
type StatisticalSummarizer struct {
	// MaxKeywords limits the number of keywords extracted
	MaxKeywords int

	// MaxRepEntities limits the number of representative entities
	MaxRepEntities int
}

// NewStatisticalSummarizer creates a statistical summarizer with default settings
func NewStatisticalSummarizer() *StatisticalSummarizer {
	return &StatisticalSummarizer{
		MaxKeywords:    10,
		MaxRepEntities: 5,
	}
}

// SummarizeCommunity generates a statistical summary of the community
func (s *StatisticalSummarizer) SummarizeCommunity(
	ctx context.Context,
	community *Community,
	entities []*gtypes.EntityState,
) (*Community, error) {
	if community == nil {
		return nil, errors.WrapInvalid(errors.ErrMissingConfig, "StatisticalSummarizer",
			"SummarizeCommunity", "community is nil")
	}

	if len(entities) == 0 {
		return nil, errors.WrapInvalid(errors.ErrInvalidData, "StatisticalSummarizer",
			"SummarizeCommunity", "entities list is empty")
	}

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Extract keywords using TF-IDF-like approach
	keywords := s.extractKeywords(entities)

	// Find representative entities using PageRank
	repEntities := s.findRepresentativeEntities(ctx, entities)

	// Generate summary text
	summary := s.generateSummary(entities, keywords)

	// Update community with summary fields
	community.StatisticalSummary = summary
	community.Keywords = keywords
	community.RepEntities = repEntities
	community.SummaryStatus = "statistical"

	return community, nil
}

// extractKeywords extracts key terms from entity types and properties
func (s *StatisticalSummarizer) extractKeywords(entities []*gtypes.EntityState) []string {
	termFreq := make(map[string]int)

	for _, entity := range entities {
		// Extract terms from entity type
		// e.g., "robotics.drone" -> ["robotics", "drone"]
		typeParts := strings.Split(entity.Node.Type, ".")
		for _, part := range typeParts {
			if part != "" {
				termFreq[part]++
			}
		}

		// Extract terms from property keys and string values
		for key, value := range entity.Node.Properties {
			// Add property key as a term
			termFreq[key]++

			// If value is string, extract terms
			if strVal, ok := value.(string); ok {
				// Split on common delimiters and extract terms
				terms := extractTerms(strVal)
				for _, term := range terms {
					termFreq[term]++
				}
			}
		}
	}

	// Sort terms by frequency
	type termScore struct {
		term  string
		score float64
	}

	scores := make([]termScore, 0, len(termFreq))
	totalEntities := float64(len(entities))

	for term, freq := range termFreq {
		// Calculate TF-IDF-like score
		// TF: term frequency normalized by total entities
		// We don't have document frequency, so we use frequency as importance
		tf := float64(freq) / totalEntities
		score := tf * math.Log(1.0+float64(freq))

		scores = append(scores, termScore{
			term:  term,
			score: score,
		})
	}

	// Sort by score descending
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	// Take top N keywords
	maxKeywords := s.MaxKeywords
	if len(scores) < maxKeywords {
		maxKeywords = len(scores)
	}

	keywords := make([]string, maxKeywords)
	for i := 0; i < maxKeywords; i++ {
		keywords[i] = scores[i].term
	}

	return keywords
}

// findRepresentativeEntities identifies entities that best represent the community
// Uses PageRank algorithm for graph centrality with fallback to degree centrality
func (s *StatisticalSummarizer) findRepresentativeEntities(ctx context.Context, entities []*gtypes.EntityState) []string {
	// Create GraphProvider adapter from entity list
	provider := newEntityGraphProvider(entities)

	// Extract community member IDs
	memberIDs := make([]string, len(entities))
	for i, entity := range entities {
		memberIDs[i] = entity.Node.ID
	}

	// Use PageRank to compute representative entities
	ranked, _, err := ComputeRepresentativeEntities(ctx, provider, memberIDs, s.MaxRepEntities)
	if err != nil {
		// Should not happen with in-memory provider, but handle gracefully
		// Fall back to simple degree centrality on error
		return s.findRepresentativeEntitiesFallback(entities)
	}

	return ranked
}

// findRepresentativeEntitiesFallback is the legacy degree-centrality implementation
// Used as fallback if PageRank computation fails
func (s *StatisticalSummarizer) findRepresentativeEntitiesFallback(entities []*gtypes.EntityState) []string {
	type entityScore struct {
		id    string
		score float64
	}

	// Count type frequencies
	typeFreq := make(map[string]int)
	for _, entity := range entities {
		typeFreq[entity.Node.Type]++
	}

	// Calculate scores
	scores := make([]entityScore, 0, len(entities))
	for _, entity := range entities {
		// Connectivity score (normalized)
		connectivityScore := float64(len(entity.Edges))

		// Type representativeness score (normalized)
		typeScore := float64(typeFreq[entity.Node.Type]) / float64(len(entities))

		// Combined score (weighted)
		score := (0.6 * connectivityScore) + (0.4 * typeScore)

		scores = append(scores, entityScore{
			id:    entity.Node.ID,
			score: score,
		})
	}

	// Sort by score descending
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	// Take top N entities
	maxRep := s.MaxRepEntities
	if len(scores) < maxRep {
		maxRep = len(scores)
	}

	repEntities := make([]string, maxRep)
	for i := 0; i < maxRep; i++ {
		repEntities[i] = scores[i].id
	}

	return repEntities
}

// entityGraphProvider implements GraphProvider interface for in-memory entity list
type entityGraphProvider struct {
	entities map[string]*gtypes.EntityState
}

// newEntityGraphProvider creates a GraphProvider from entity list
func newEntityGraphProvider(entities []*gtypes.EntityState) *entityGraphProvider {
	entitiesMap := make(map[string]*gtypes.EntityState, len(entities))
	for _, entity := range entities {
		entitiesMap[entity.Node.ID] = entity
	}
	return &entityGraphProvider{entities: entitiesMap}
}

func (p *entityGraphProvider) GetAllEntityIDs(_ context.Context) ([]string, error) {
	ids := make([]string, 0, len(p.entities))
	for id := range p.entities {
		ids = append(ids, id)
	}
	return ids, nil
}

func (p *entityGraphProvider) GetNeighbors(_ context.Context, entityID string, direction string) ([]string, error) {
	entity, ok := p.entities[entityID]
	if !ok {
		return []string{}, nil
	}

	neighborSet := make(map[string]bool) // Deduplicate

	// Handle outgoing edges (stored in this entity)
	if direction == "outgoing" || direction == "both" {
		for _, edge := range entity.Edges {
			neighborSet[edge.ToEntityID] = true
		}
	}

	// Handle incoming edges (stored in other entities pointing to this one)
	if direction == "incoming" || direction == "both" {
		for _, otherEntity := range p.entities {
			for _, edge := range otherEntity.Edges {
				if edge.ToEntityID == entityID {
					neighborSet[otherEntity.Node.ID] = true
				}
			}
		}
	}

	neighbors := make([]string, 0, len(neighborSet))
	for neighborID := range neighborSet {
		neighbors = append(neighbors, neighborID)
	}

	return neighbors, nil
}

func (p *entityGraphProvider) GetEdgeWeight(_ context.Context, fromID, toID string) (float64, error) {
	entity, ok := p.entities[fromID]
	if !ok {
		return 0.0, nil
	}

	// Check if edge exists from fromID to toID
	for _, edge := range entity.Edges {
		if edge.ToEntityID == toID {
			// Use edge weight if available, otherwise default to 1.0
			if edge.Weight > 0 {
				return edge.Weight, nil
			}
			return 1.0, nil
		}
	}

	return 0.0, nil
}

// generateSummary creates a natural language summary of the community
func (s *StatisticalSummarizer) generateSummary(
	entities []*gtypes.EntityState,
	keywords []string,
) string {
	// Count entity types
	typeCount := make(map[string]int)
	for _, entity := range entities {
		typeCount[entity.Node.Type]++
	}

	// Find most common types
	type typeFreq struct {
		typeName string
		count    int
	}

	typeFreqs := make([]typeFreq, 0, len(typeCount))
	for typeName, count := range typeCount {
		typeFreqs = append(typeFreqs, typeFreq{
			typeName: typeName,
			count:    count,
		})
	}

	sort.Slice(typeFreqs, func(i, j int) bool {
		return typeFreqs[i].count > typeFreqs[j].count
	})

	// Build summary text
	var summary strings.Builder

	// Community size
	summary.WriteString(fmt.Sprintf("Community of %d entities", len(entities)))

	// Top entity types (up to 3)
	if len(typeFreqs) > 0 {
		summary.WriteString(" including ")
		maxTypes := 3
		if len(typeFreqs) < maxTypes {
			maxTypes = len(typeFreqs)
		}

		typeDescriptions := make([]string, maxTypes)
		for i := 0; i < maxTypes; i++ {
			// Extract simple type name (last part after dot)
			parts := strings.Split(typeFreqs[i].typeName, ".")
			simpleType := parts[len(parts)-1]

			typeDescriptions[i] = fmt.Sprintf("%d %s", typeFreqs[i].count, simpleType)
			if typeFreqs[i].count > 1 {
				typeDescriptions[i] += "s" // Simple pluralization
			}
		}

		summary.WriteString(strings.Join(typeDescriptions, ", "))
	}

	// Key themes (top keywords, up to 5)
	if len(keywords) > 0 {
		summary.WriteString(". Key themes: ")
		maxThemes := 5
		if len(keywords) < maxThemes {
			maxThemes = len(keywords)
		}

		themes := make([]string, maxThemes)
		copy(themes, keywords[:maxThemes])
		summary.WriteString(strings.Join(themes, ", "))
	}

	summary.WriteString(".")

	return summary.String()
}

// extractTerms splits a string into terms, filtering out common stop words
func extractTerms(text string) []string {
	// Simple term extraction: split on non-alphanumeric, lowercase, filter short terms
	text = strings.ToLower(text)

	// Split on common delimiters
	words := strings.FieldsFunc(text, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})

	// Filter out stop words and short terms
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true, "but": true,
		"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
		"with": true, "by": true, "from": true, "as": true, "is": true, "was": true,
		"are": true, "be": true, "has": true, "have": true, "had": true, "do": true,
		"does": true, "did": true, "will": true, "would": true, "could": true, "should": true,
	}

	terms := make([]string, 0, len(words))
	for _, word := range words {
		if len(word) >= 3 && !stopWords[word] {
			terms = append(terms, word)
		}
	}

	return terms
}

// HTTPLLMSummarizer implements CommunitySummarizer using HTTP LLM API (semsummarize)
// This summarizer calls an external LLM service for higher quality natural language summaries
type HTTPLLMSummarizer struct {
	// BaseURL is the endpoint URL for the summarization service (e.g., "http://semsummarize:8083")
	BaseURL string

	// Timeout for HTTP requests
	Timeout time.Duration

	// FallbackSummarizer is used if HTTP service is unavailable
	FallbackSummarizer *StatisticalSummarizer

	// HTTPClient for making requests
	client *http.Client
}

// summarizeRequest matches semsummarize API request format
type summarizeRequest struct {
	Text        string  `json:"text"`
	MaxLength   int     `json:"max_length,omitempty"`
	MinLength   int     `json:"min_length,omitempty"`
	NumBeams    int     `json:"num_beams,omitempty"`
	DoSample    bool    `json:"do_sample,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
}

// summarizeResponse matches semsummarize API response format
type summarizeResponse struct {
	Summary string  `json:"summary"`
	Model   string  `json:"model"`
	Latency float64 `json:"latency_ms"`
}

// NewHTTPLLMSummarizer creates an HTTP-based LLM summarizer with default settings
func NewHTTPLLMSummarizer(baseURL string) *HTTPLLMSummarizer {
	return &HTTPLLMSummarizer{
		BaseURL:            baseURL,
		Timeout:            10 * time.Second,
		FallbackSummarizer: NewStatisticalSummarizer(),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SummarizeCommunity generates an LLM-based summary of the community
func (s *HTTPLLMSummarizer) SummarizeCommunity(
	ctx context.Context,
	community *Community,
	entities []*gtypes.EntityState,
) (*Community, error) {
	if community == nil {
		return nil, errors.WrapInvalid(errors.ErrMissingConfig, "HTTPLLMSummarizer",
			"SummarizeCommunity", "community is nil")
	}

	if len(entities) == 0 {
		return nil, errors.WrapInvalid(errors.ErrInvalidData, "HTTPLLMSummarizer",
			"SummarizeCommunity", "entities list is empty")
	}

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Use statistical summarizer for keywords and rep entities
	// (LLM will generate the narrative summary)
	keywords := s.FallbackSummarizer.extractKeywords(entities)
	repEntities := s.FallbackSummarizer.findRepresentativeEntities(ctx, entities)

	// Build input text for LLM from entity information
	inputText := s.buildLLMInputText(entities, keywords)

	// Call LLM service for summary generation
	summary, err := s.callLLMService(ctx, inputText)
	if err != nil {
		// If community already has a statistical summary (progressive enhancement path),
		// return error so enhancement worker can mark as "llm-failed"
		if community.StatisticalSummary != "" {
			return nil, errors.WrapTransient(err, "HTTPLLMSummarizer",
				"SummarizeCommunity", "LLM service unavailable")
		}

		// Otherwise, gracefully fall back to statistical summarization (direct call path)
		statSummary := s.FallbackSummarizer.generateSummary(entities, keywords)
		community.StatisticalSummary = statSummary
		community.Keywords = keywords
		community.RepEntities = repEntities
		community.SummaryStatus = "statistical-fallback"
		return community, nil // Graceful degradation for direct calls
	}

	// Update community with LLM-generated summary
	community.LLMSummary = summary
	community.Keywords = keywords
	community.RepEntities = repEntities
	community.SummaryStatus = "llm-enhanced"

	return community, nil
}

// buildLLMInputText creates a structured text representation of the community for the LLM
func (s *HTTPLLMSummarizer) buildLLMInputText(
	entities []*gtypes.EntityState,
	keywords []string,
) string {
	var builder strings.Builder

	// Count entity types
	typeCount := make(map[string]int)
	for _, entity := range entities {
		typeCount[entity.Node.Type]++
	}

	// Build prompt for LLM
	builder.WriteString(fmt.Sprintf("Summarize this community of %d entities:\n\n", len(entities)))

	// Entity type distribution
	builder.WriteString("Entity types:\n")
	for typeName, count := range typeCount {
		builder.WriteString(fmt.Sprintf("- %s: %d\n", typeName, count))
	}
	builder.WriteString("\n")

	// Key themes
	if len(keywords) > 0 {
		builder.WriteString("Key themes: ")
		builder.WriteString(strings.Join(keywords[:min(5, len(keywords))], ", "))
		builder.WriteString("\n\n")
	}

	// Sample entity details (first 5 entities)
	builder.WriteString("Sample entities:\n")
	maxSamples := min(5, len(entities))
	for i := 0; i < maxSamples; i++ {
		entity := entities[i]
		builder.WriteString(fmt.Sprintf("- %s (%s)", entity.Node.ID, entity.Node.Type))
		if name, ok := entity.Node.Properties["name"].(string); ok {
			builder.WriteString(fmt.Sprintf(": %s", name))
		}
		builder.WriteString("\n")
	}

	builder.WriteString("\nGenerate a concise, natural language summary (1-2 sentences) describing what this community represents.")

	return builder.String()
}

// callLLMService makes HTTP request to semsummarize service
func (s *HTTPLLMSummarizer) callLLMService(ctx context.Context, text string) (string, error) {
	// Create request
	reqBody := summarizeRequest{
		Text:      text,
		MaxLength: 100, // Max tokens for summary
		MinLength: 20,  // Min tokens for summary
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", errors.WrapInvalid(err, "HTTPLLMSummarizer", "callLLMService", "marshal request")
	}

	// Make HTTP request with context
	url := fmt.Sprintf("%s/summarize", s.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", errors.WrapInvalid(err, "HTTPLLMSummarizer", "callLLMService", "create request")
	}

	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := s.client.Do(req)
	if err != nil {
		return "", errors.WrapTransient(err, "HTTPLLMSummarizer", "callLLMService",
			"HTTP request failed (service may be unavailable)")
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", errors.WrapTransient(
			fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body)),
			"HTTPLLMSummarizer", "callLLMService", "non-OK status code")
	}

	// Parse response
	var result summarizeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", errors.WrapInvalid(err, "HTTPLLMSummarizer", "callLLMService", "decode response")
	}

	if result.Summary == "" {
		return "", errors.WrapInvalid(errors.ErrInvalidData, "HTTPLLMSummarizer",
			"callLLMService", "empty summary returned")
	}

	return result.Summary, nil
}

// ProgressiveSummarizer provides progressive enhancement - statistical summary immediately,
// LLM enhancement asynchronously via events
type ProgressiveSummarizer struct {
	statistical *StatisticalSummarizer
}

// NewProgressiveSummarizer creates a progressive summarizer with default settings
func NewProgressiveSummarizer() *ProgressiveSummarizer {
	return &ProgressiveSummarizer{
		statistical: NewStatisticalSummarizer(),
	}
}

// SummarizeCommunity generates an immediate statistical summary
// Caller is responsible for saving and publishing community.detected event for async LLM enhancement
func (s *ProgressiveSummarizer) SummarizeCommunity(
	ctx context.Context,
	community *Community,
	entities []*gtypes.EntityState,
) (*Community, error) {
	if community == nil {
		return nil, errors.WrapInvalid(errors.ErrMissingConfig, "ProgressiveSummarizer",
			"SummarizeCommunity", "community is nil")
	}

	if len(entities) == 0 {
		return nil, errors.WrapInvalid(errors.ErrInvalidData, "ProgressiveSummarizer",
			"SummarizeCommunity", "entities list is empty")
	}

	// Generate statistical summary using existing summarizer
	statCommunity, err := s.statistical.SummarizeCommunity(ctx, community, entities)
	if err != nil {
		return nil, errors.WrapTransient(err, "ProgressiveSummarizer",
			"SummarizeCommunity", "statistical summarization failed")
	}

	// Populate statistical fields
	community.StatisticalSummary = statCommunity.StatisticalSummary
	community.Keywords = statCommunity.Keywords
	community.RepEntities = statCommunity.RepEntities
	community.SummaryStatus = "statistical"

	// LLMSummary remains empty until async enhancement completes
	community.LLMSummary = ""

	return community, nil
}

// min returns the smaller of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
