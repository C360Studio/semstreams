package clustering

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/llm"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
)

// extractEntityType extracts the type from an entity ID using message.ParseEntityID
func extractEntityType(entityID string) string {
	eid, err := message.ParseEntityID(entityID)
	if err != nil {
		return ""
	}
	return eid.Type
}

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
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "StatisticalSummarizer",
			"SummarizeCommunity", "community is nil")
	}

	if len(entities) == 0 {
		return nil, errs.WrapInvalid(errs.ErrInvalidData, "StatisticalSummarizer",
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
		typeParts := strings.Split(extractEntityType(entity.ID), ".")
		for _, part := range typeParts {
			if part != "" {
				termFreq[part]++
			}
		}

		// Extract terms from property triples (string values only)
		for _, triple := range entity.Triples {
			if !triple.IsRelationship() {
				// Add predicate as a term
				termFreq[triple.Predicate]++

				// If value is string, extract terms
				if strVal, ok := triple.Object.(string); ok {
					// Split on common delimiters and extract terms
					terms := extractTerms(strVal)
					for _, term := range terms {
						termFreq[term]++
					}
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
	// Create Provider adapter from entity list
	provider := newEntityProvider(entities)

	// Extract community member IDs
	memberIDs := make([]string, len(entities))
	for i, entity := range entities {
		memberIDs[i] = entity.ID
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
		typeFreq[extractEntityType(entity.ID)]++
	}

	// Calculate scores
	scores := make([]entityScore, 0, len(entities))
	for _, entity := range entities {
		// Connectivity score (count relationship triples)
		relationshipCount := 0
		for _, triple := range entity.Triples {
			if triple.IsRelationship() {
				relationshipCount++
			}
		}
		connectivityScore := float64(relationshipCount)

		// Type representativeness score (normalized)
		typeScore := float64(typeFreq[extractEntityType(entity.ID)]) / float64(len(entities))

		// Combined score (weighted)
		score := (0.6 * connectivityScore) + (0.4 * typeScore)

		scores = append(scores, entityScore{
			id:    entity.ID,
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

// entityProvider implements Provider interface for in-memory entity list
type entityProvider struct {
	entities map[string]*gtypes.EntityState
}

// newEntityProvider creates a Provider from entity list
func newEntityProvider(entities []*gtypes.EntityState) *entityProvider {
	entitiesMap := make(map[string]*gtypes.EntityState, len(entities))
	for _, entity := range entities {
		entitiesMap[entity.ID] = entity
	}
	return &entityProvider{entities: entitiesMap}
}

func (p *entityProvider) GetAllEntityIDs(_ context.Context) ([]string, error) {
	ids := make([]string, 0, len(p.entities))
	for id := range p.entities {
		ids = append(ids, id)
	}
	return ids, nil
}

func (p *entityProvider) GetNeighbors(_ context.Context, entityID string, direction string) ([]string, error) {
	entity, ok := p.entities[entityID]
	if !ok {
		return []string{}, nil
	}

	neighborSet := make(map[string]bool) // Deduplicate

	// Handle outgoing relationships (stored in this entity's triples)
	if direction == "outgoing" || direction == "both" {
		for _, triple := range entity.Triples {
			if triple.IsRelationship() {
				if targetID, ok := triple.Object.(string); ok {
					neighborSet[targetID] = true
				}
			}
		}
	}

	// Handle incoming relationships (stored in other entities' triples pointing to this one)
	if direction == "incoming" || direction == "both" {
		for _, otherEntity := range p.entities {
			for _, triple := range otherEntity.Triples {
				if triple.IsRelationship() {
					if targetID, ok := triple.Object.(string); ok && targetID == entityID {
						neighborSet[otherEntity.ID] = true
					}
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

func (p *entityProvider) GetEdgeWeight(_ context.Context, fromID, toID string) (float64, error) {
	entity, ok := p.entities[fromID]
	if !ok {
		return 0.0, nil
	}

	// Check if relationship triple exists from fromID to toID
	for _, triple := range entity.Triples {
		if triple.IsRelationship() {
			if targetID, ok := triple.Object.(string); ok && targetID == toID {
				// Use triple confidence as weight (0.0 to 1.0)
				// Confidence indicates reliability of the relationship
				if triple.Confidence > 0 {
					return triple.Confidence, nil
				}
				return 1.0, nil
			}
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
		typeCount[extractEntityType(entity.ID)]++
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

// LLMSummarizer implements CommunitySummarizer using an OpenAI-compatible LLM API.
// This summarizer calls an external LLM service for higher quality natural language summaries.
//
// It works with any OpenAI-compatible backend:
//   - seminstruct (recommended for local llama.cpp inference)
//   - OpenAI cloud
//   - Ollama, vLLM, etc.
type LLMSummarizer struct {
	// Client is the LLM client for making chat completion requests.
	Client llm.Client

	// FallbackSummarizer is used if LLM service is unavailable.
	FallbackSummarizer *StatisticalSummarizer

	// MaxTokens limits the response length (default: 150).
	MaxTokens int

	// ContentFetcher optionally fetches entity content (title, abstract) for richer prompts.
	// If nil, prompts use only entity IDs and triple-derived keywords.
	ContentFetcher llm.ContentFetcher
}

// LLMSummarizerConfig configures the LLM summarizer.
type LLMSummarizerConfig struct {
	// Client is the LLM client (required).
	Client llm.Client

	// MaxTokens limits the response length (default: 150).
	MaxTokens int
}

// LLMSummarizerOption configures an LLMSummarizer using the functional options pattern.
// Options return errors for validation (following natsclient pattern).
type LLMSummarizerOption func(*LLMSummarizer) error

// WithContentFetcher sets the ContentFetcher for enriching prompts with entity content.
// If not set, prompts use only entity IDs and triple-derived keywords.
func WithContentFetcher(fetcher llm.ContentFetcher) LLMSummarizerOption {
	return func(s *LLMSummarizer) error {
		s.ContentFetcher = fetcher
		return nil
	}
}

// NewLLMSummarizer creates an LLM-based summarizer with the given configuration.
// Optional functional options can be provided to configure additional features.
func NewLLMSummarizer(cfg LLMSummarizerConfig, opts ...LLMSummarizerOption) (*LLMSummarizer, error) {
	if cfg.Client == nil {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "LLMSummarizer",
			"NewLLMSummarizer", "client is required")
	}

	maxTokens := cfg.MaxTokens
	if maxTokens == 0 {
		maxTokens = 150
	}

	s := &LLMSummarizer{
		Client:             cfg.Client,
		FallbackSummarizer: NewStatisticalSummarizer(),
		MaxTokens:          maxTokens,
	}

	// Apply functional options
	for _, opt := range opts {
		if err := opt(s); err != nil {
			return nil, err
		}
	}

	return s, nil
}

// SummarizeCommunity generates an LLM-based summary of the community.
// Implements CommunitySummarizer interface with 3-param signature.
// Content fetching happens internally using the optional ContentFetcher.
func (s *LLMSummarizer) SummarizeCommunity(
	ctx context.Context,
	community *Community,
	entities []*gtypes.EntityState,
) (*Community, error) {
	if community == nil {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "LLMSummarizer",
			"SummarizeCommunity", "community is nil")
	}

	if len(entities) == 0 {
		return nil, errs.WrapInvalid(errs.ErrInvalidData, "LLMSummarizer",
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

	// Fetch entity content internally if ContentFetcher is configured
	// Only fetch for representative entities (performance optimization)
	var entityContent map[string]*llm.EntityContent
	if s.ContentFetcher != nil {
		repEntitySet := make(map[string]bool, len(repEntities))
		for _, id := range repEntities {
			repEntitySet[id] = true
		}
		repEntityList := make([]*gtypes.EntityState, 0, len(repEntities))
		for _, entity := range entities {
			if repEntitySet[entity.ID] {
				repEntityList = append(repEntityList, entity)
			}
		}
		// Fetch content - errors are logged but not fatal (graceful degradation)
		entityContent, _ = s.ContentFetcher.FetchEntityContent(ctx, repEntityList)
	}

	// Build prompt data with optional entity content
	promptData := s.buildPromptData(entities, keywords, entityContent)

	// Render the prompt template using direct package variable
	rendered, err := llm.CommunityPrompt.Render(promptData)
	if err != nil {
		// If prompt rendering fails, fall back to statistical
		statSummary := s.FallbackSummarizer.generateSummary(entities, keywords)
		community.StatisticalSummary = statSummary
		community.Keywords = keywords
		community.RepEntities = repEntities
		community.SummaryStatus = "statistical-fallback"
		return community, nil
	}

	// Call LLM service for summary generation
	temperature := 0.7
	resp, err := s.Client.ChatCompletion(ctx, llm.ChatRequest{
		SystemPrompt: rendered.System,
		UserPrompt:   rendered.User,
		MaxTokens:    s.MaxTokens,
		Temperature:  &temperature,
	})
	if err != nil {
		// If community already has a statistical summary (progressive enhancement path),
		// return error so enhancement worker can mark as "llm-failed"
		if community.StatisticalSummary != "" {
			return nil, errs.WrapTransient(err, "LLMSummarizer",
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
	community.LLMSummary = resp.Content
	community.Keywords = keywords
	community.RepEntities = repEntities
	community.SummaryStatus = "llm-enhanced"

	return community, nil
}

// parseEntityID extracts the 6 parts from a federated entity ID.
// Entity ID format: {org}.{platform}.{domain}.{system}.{type}.{instance}
func parseEntityID(entityID string) llm.EntityParts {
	parts := strings.Split(entityID, ".")
	ep := llm.EntityParts{Full: entityID}
	if len(parts) >= 6 {
		ep.Org = parts[0]
		ep.Platform = parts[1]
		ep.Domain = parts[2]
		ep.System = parts[3]
		ep.Type = parts[4]
		ep.Instance = strings.Join(parts[5:], ".") // Instance may contain dots
	}
	return ep
}

// domainGroupBuilder is a helper for building DomainGroup.
type domainGroupBuilder struct {
	domain      string
	count       int
	systemTypes map[string]int
}

// buildPromptData creates the data structure for prompt template rendering.
// It parses 6-part entity IDs and groups by domain (part[2]).
func (s *LLMSummarizer) buildPromptData(
	entities []*gtypes.EntityState,
	keywords []string,
	entityContent map[string]*llm.EntityContent,
) llm.CommunitySummaryData {
	// Group by domain (part[2] of entity ID)
	domainGroups := make(map[string]*domainGroupBuilder)
	orgPlatforms := make(map[string]int)

	for _, entity := range entities {
		parsed := parseEntityID(entity.ID)

		// Track org.platform frequency
		op := parsed.Org + "." + parsed.Platform
		orgPlatforms[op]++

		// Group by domain
		dg, ok := domainGroups[parsed.Domain]
		if !ok {
			dg = &domainGroupBuilder{
				domain:      parsed.Domain,
				systemTypes: make(map[string]int),
			}
			domainGroups[parsed.Domain] = dg
		}
		dg.count++
		st := parsed.System + "." + parsed.Type
		dg.systemTypes[st]++
	}

	// Convert to slice
	domains := make([]llm.DomainGroup, 0, len(domainGroups))
	for _, dg := range domainGroups {
		sts := make([]llm.SystemType, 0, len(dg.systemTypes))
		for name, count := range dg.systemTypes {
			sts = append(sts, llm.SystemType{Name: name, Count: count})
		}
		domains = append(domains, llm.DomainGroup{
			Domain:      dg.domain,
			Count:       dg.count,
			SystemTypes: sts,
		})
	}

	// Find dominant domain (>2/3 of entities)
	dominantDomain := "mixed"
	total := len(entities)
	for _, dg := range domains {
		if dg.Count > total*2/3 {
			dominantDomain = dg.Domain
			break
		}
	}

	// Find common org.platform (if all entities share it)
	orgPlatform := ""
	for op, count := range orgPlatforms {
		if count == total {
			orgPlatform = op
			break
		}
	}

	// Build parsed sample entities with optional content
	maxSamples := min(5, len(entities))
	samples := make([]llm.EntityParts, maxSamples)
	for i := 0; i < maxSamples; i++ {
		samples[i] = parseEntityID(entities[i].ID)

		// Populate content if available
		if entityContent != nil {
			if content, ok := entityContent[entities[i].ID]; ok {
				samples[i].Title = content.Title
				samples[i].Abstract = content.Abstract
			}
		}
	}

	// Format keywords
	keywordStr := ""
	if len(keywords) > 0 {
		keywordStr = strings.Join(keywords[:min(5, len(keywords))], ", ")
	}

	return llm.CommunitySummaryData{
		EntityCount:    total,
		Domains:        domains,
		DominantDomain: dominantDomain,
		OrgPlatform:    orgPlatform,
		Keywords:       keywordStr,
		SampleEntities: samples,
	}
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
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "ProgressiveSummarizer",
			"SummarizeCommunity", "community is nil")
	}

	if len(entities) == 0 {
		return nil, errs.WrapInvalid(errs.ErrInvalidData, "ProgressiveSummarizer",
			"SummarizeCommunity", "entities list is empty")
	}

	// Generate statistical summary using existing summarizer
	statCommunity, err := s.statistical.SummarizeCommunity(ctx, community, entities)
	if err != nil {
		return nil, errs.WrapTransient(err, "ProgressiveSummarizer",
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
