// Package scenarios provides E2E test scenarios for SemStreams semantic processing
package scenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/c360studio/semstreams/test/e2e/scenarios/community"
)

// Statistical variant validation functions (GraphRAG, community validation)

// graphRAGLocalResponse represents the parsed GraphQL response for local search queries
type graphRAGLocalResponse struct {
	Data struct {
		LocalSearch struct {
			Entities []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"entities"`
			CommunityID string `json:"communityId"`
			Count       int    `json:"count"`
		} `json:"localSearch"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// graphRAGGlobalResponse represents the parsed GraphQL response for global search queries
type graphRAGGlobalResponse struct {
	Data struct {
		GlobalSearch struct {
			Entities []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"entities"`
			CommunitySummaries []struct {
				CommunityID string   `json:"communityId"`
				Summary     string   `json:"summary"`
				Keywords    []string `json:"keywords"`
				Level       int      `json:"level"`
				Relevance   float64  `json:"relevance"`
				MemberCount int      `json:"member_count"`
				Entities    []struct {
					ID        string  `json:"id"`
					Type      string  `json:"type"`
					Label     string  `json:"label"`
					Relevance float64 `json:"relevance"`
				} `json:"entities"`
			} `json:"communitySummaries"`
			Count       int    `json:"count"`
			Answer      string `json:"answer"`
			AnswerModel string `json:"answer_model"`
		} `json:"globalSearch"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// executeTestGraphRAGLocal validates GraphRAG local search (within community context)
func (s *TieredScenario) executeTestGraphRAGLocal(ctx context.Context, result *Result) error {
	gatewayURL := s.config.GraphQLURL // Use GraphQL gateway URL, not api-gateway
	searchQuery := "temperature sensor monitoring"

	// Find an entity that's in a community (non-container, non-group entity)
	startEntity, err := s.findEntityInCommunity(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Could not find entity in community: %v", err))
		return nil
	}

	result.Details["graphrag_local_discovered_entity"] = startEntity

	resp, latency, err := s.sendGraphRAGLocalRequest(ctx, startEntity, searchQuery, gatewayURL)
	if err != nil {
		result.Details["graphrag_local_test"] = map[string]any{
			"start_entity": startEntity, "query": searchQuery, "error": err.Error(),
		}
		// GraphRAG local may fail if entity not in a community - warn but don't fail
		result.Warnings = append(result.Warnings, fmt.Sprintf("GraphRAG local search failed: %v", err))
		return nil
	}

	result.Metrics["graphrag_local_latency_ms"] = latency.Milliseconds()
	return s.validateGraphRAGLocalResult(resp, startEntity, searchQuery, latency, result)
}

// findEntityInCommunity queries communities and returns a non-container entity that's in a level 0 community.
// We filter to level 0 because the GraphRAG query uses level 0.
func (s *TieredScenario) findEntityInCommunity(ctx context.Context) (string, error) {
	if s.natsClient == nil {
		return "", fmt.Errorf("NATS client not available")
	}

	// Get all communities
	communities, err := s.natsClient.GetAllCommunities(ctx)
	if err != nil {
		return "", fmt.Errorf("get communities: %w", err)
	}

	if len(communities) == 0 {
		return "", fmt.Errorf("no communities found")
	}

	// Look for a non-container entity in a level 0 community with multiple members
	for _, comm := range communities {
		if comm.Level != 0 {
			continue // Only consider level 0 communities since we query at level 0
		}
		if len(comm.Members) < 2 {
			continue // Skip singleton communities
		}
		for _, member := range comm.Members {
			// Skip container entities (group, container, level suffixes)
			if isContainerEntity(member) {
				continue
			}
			return member, nil
		}
	}

	// Fallback: any non-container member from any level 0 community
	for _, comm := range communities {
		if comm.Level != 0 {
			continue
		}
		for _, member := range comm.Members {
			if !isContainerEntity(member) {
				return member, nil
			}
		}
	}

	return "", fmt.Errorf("no suitable entity found in any level 0 community")
}

// sendGraphRAGLocalRequest sends the GraphRAG local search query
func (s *TieredScenario) sendGraphRAGLocalRequest(ctx context.Context, entityID, query, gatewayURL string) (*graphRAGLocalResponse, time.Duration, error) {
	graphqlQuery := map[string]any{
		"query": `query($entityId: ID!, $query: String!, $level: Int) {
			localSearch(entityId: $entityId, query: $query, level: $level) {
				entities { id type } communityId count
			}}`,
		"variables": map[string]any{"entityId": entityID, "query": query, "level": 0},
	}

	queryJSON, err := json.Marshal(graphqlQuery)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to marshal query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", gatewayURL, bytes.NewReader(queryJSON))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 10 * time.Second}
	start := time.Now()
	resp, err := httpClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		return nil, latency, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, latency, fmt.Errorf("returned status %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, latency, fmt.Errorf("failed to read response: %w", err)
	}

	var graphqlResp graphRAGLocalResponse
	if err := json.Unmarshal(bodyBytes, &graphqlResp); err != nil {
		return nil, latency, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(graphqlResp.Errors) > 0 {
		return nil, latency, fmt.Errorf("GraphQL error: %s", graphqlResp.Errors[0].Message)
	}

	return &graphqlResp, latency, nil
}

// validateGraphRAGLocalResult validates the local search response
func (s *TieredScenario) validateGraphRAGLocalResult(resp *graphRAGLocalResponse, entityID, query string, latency time.Duration, result *Result) error {
	ls := resp.Data.LocalSearch
	entityCount := len(ls.Entities)

	result.Metrics["graphrag_local_entities_found"] = entityCount
	result.Metrics["graphrag_local_community_id"] = ls.CommunityID

	entityIDs := make([]string, 0, len(ls.Entities))
	for _, e := range ls.Entities {
		entityIDs = append(entityIDs, e.ID)
	}

	result.Details["graphrag_local"] = map[string]any{
		"query":            query,
		"entities_used":    entityCount,
		"communities_used": 1, // Single community context for local search
		"latency_ms":       latency.Milliseconds(),
		"success":          ls.CommunityID != "",
		// Additional fields for debugging
		"start_entity": entityID,
		"community_id": ls.CommunityID,
		"entity_ids":   entityIDs,
	}

	// Community context check - with storage fallback this should always succeed
	if ls.CommunityID == "" {
		return fmt.Errorf("GraphRAG local search missing community context for entity %s", entityID)
	}

	// Validate at least one entity is returned when community was found
	if entityCount == 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf(
			"GraphRAG local search returned no entities for query %q in community %s",
			query, ls.CommunityID))
	}

	return nil
}

// executeTestGraphRAGGlobal validates GraphRAG global search (across community summaries)
func (s *TieredScenario) executeTestGraphRAGGlobal(ctx context.Context, result *Result) error {
	gatewayURL := s.config.GraphQLURL // Use GraphQL gateway URL, not api-gateway
	searchQuery := "logistics warehouse operations"

	resp, latency, err := s.sendGraphRAGGlobalRequest(ctx, searchQuery, gatewayURL)
	if err != nil {
		result.Details["graphrag_global"] = map[string]any{
			"query":   searchQuery,
			"error":   err.Error(),
			"success": false,
		}
		// GraphRAG global may fail if no communities exist - warn but don't fail
		result.Warnings = append(result.Warnings, fmt.Sprintf("GraphRAG global search failed: %v", err))
		return nil
	}

	result.Metrics["graphrag_global_latency_ms"] = latency.Milliseconds()
	return s.validateGraphRAGGlobalResult(resp, searchQuery, latency, result)
}

// sendGraphRAGGlobalRequest sends the GraphRAG global search query
func (s *TieredScenario) sendGraphRAGGlobalRequest(ctx context.Context, query, gatewayURL string) (*graphRAGGlobalResponse, time.Duration, error) {
	graphqlQuery := map[string]any{
		"query": `query($query: String!, $level: Int, $maxCommunities: Int) {
			globalSearch(query: $query, level: $level, maxCommunities: $maxCommunities) {
				entities { id type }
				communitySummaries { communityId summary keywords level relevance member_count entities { id type label relevance } }
				count
				answer
				answer_model
			}}`,
		"variables": map[string]any{"query": query, "level": 1, "maxCommunities": 5},
	}

	queryJSON, err := json.Marshal(graphqlQuery)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to marshal query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", gatewayURL, bytes.NewReader(queryJSON))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 10 * time.Second}
	start := time.Now()
	resp, err := httpClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		return nil, latency, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, latency, fmt.Errorf("returned status %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, latency, fmt.Errorf("failed to read response: %w", err)
	}

	var graphqlResp graphRAGGlobalResponse
	if err := json.Unmarshal(bodyBytes, &graphqlResp); err != nil {
		return nil, latency, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(graphqlResp.Errors) > 0 {
		return nil, latency, fmt.Errorf("GraphQL error: %s", graphqlResp.Errors[0].Message)
	}

	return &graphqlResp, latency, nil
}

// validateGraphRAGGlobalResult validates the global search response
func (s *TieredScenario) validateGraphRAGGlobalResult(resp *graphRAGGlobalResponse, query string, latency time.Duration, result *Result) error {
	gs := resp.Data.GlobalSearch
	entityCount := len(gs.Entities)
	communityCount := len(gs.CommunitySummaries)

	result.Metrics["graphrag_global_entities_found"] = entityCount
	result.Metrics["graphrag_global_communities_found"] = communityCount

	entityIDs := make([]string, 0, len(gs.Entities))
	for _, e := range gs.Entities {
		entityIDs = append(entityIDs, e.ID)
	}

	communityDetails := make([]map[string]any, 0, len(gs.CommunitySummaries))
	for _, cs := range gs.CommunitySummaries {
		communityDetails = append(communityDetails, map[string]any{
			"community_id": cs.CommunityID,
			"keywords":     cs.Keywords,
			"level":        cs.Level,
			"relevance":    cs.Relevance,
			"has_summary":  cs.Summary != "",
		})
	}

	result.Details["graphrag_global"] = map[string]any{
		"query":            query,
		"entities_used":    entityCount,
		"communities_used": communityCount,
		"latency_ms":       latency.Milliseconds(),
		"success":          true,
		"has_answer":       gs.Answer != "",
		"answer_model":     gs.AnswerModel,
		// Additional fields for debugging
		"entity_ids":  entityIDs,
		"communities": communityDetails,
	}

	// Phase 2 improvement: Validate multi-community results for broad queries
	if communityCount < 2 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("GraphRAG global search returned only %d communities for broad query %q, expected >= 2", communityCount, query))
	}

	// Validate each community has a summary
	for _, cs := range gs.CommunitySummaries {
		if cs.Summary == "" {
			return fmt.Errorf("GraphRAG global search: community %s missing summary", cs.CommunityID)
		}
	}

	// Validate answer synthesis — should always be populated when communities exist
	if gs.Answer == "" && communityCount > 0 {
		result.Warnings = append(result.Warnings,
			"GraphRAG global search: answer field empty despite having community summaries")
	}

	// Validate enriched community summaries have member counts
	for _, cs := range gs.CommunitySummaries {
		if cs.MemberCount == 0 {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("GraphRAG global search: community %s missing member_count", cs.CommunityID))
		}
	}

	return nil
}

// executeValidateCommunityStructure validates that community detection produced valid structure
func (s *TieredScenario) executeValidateCommunityStructure(ctx context.Context, result *Result) error {
	if s.natsClient == nil {
		result.Warnings = append(result.Warnings, "NATS client not available, skipping community structure validation")
		return nil
	}

	// Wait for communities to be available (community detection may still be running)
	communities, err := s.waitForCommunities(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to get communities: %v", err))
		return nil
	}

	// Enhancement status lives in COMMUNITY_SUMMARIES after the B3 ownership split
	// (ADR-087); COMMUNITY_INDEX.SummaryStatus is no longer written by the worker.
	// Join the store here — with the same membership-hash join the authoritative
	// validate-llm-enhancement stage and GraphRAG use — so this stage's enhancement
	// counts stay consistent with that stage in the semantic variant (both read the
	// same store) and correctly report 0 in the statistical variant (its summary
	// store is empty). A read failure degrades to the statistical floor rather than
	// aborting; the partition structure below is still valid.
	summaries, err := s.natsClient.GetCommunitySummaries(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to read community summaries: %v", err))
		summaries = map[string]*clustering.CommunitySummaryRecord{}
	}

	totalCount := len(communities)
	nonSingletonCount := 0
	largestSize := 0
	totalNonSingletonSize := 0
	communitiesWithKeywords := 0
	llmEnhancedCount := 0
	statisticalOnlyCount := 0

	for _, comm := range communities {
		memberCount := len(comm.Members)
		if memberCount > 1 {
			nonSingletonCount++
			totalNonSingletonSize += memberCount
			if memberCount > largestSize {
				largestSize = memberCount
			}
		}
		if len(comm.Keywords) > 0 {
			communitiesWithKeywords++
		} else {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Community %s has no keywords", comm.ID))
		}
		// Track LLM enhancement by JOINING the COMMUNITY_SUMMARIES store by membership
		// hash (ADR-087) — mirroring validate-llm-enhancement's analyzeCommunities — not
		// the post-split always-empty COMMUNITY_INDEX.SummaryStatus field. "Enhanced" =
		// a usable llm-enhanced record exists; everything else (missing/failed/pending)
		// serves the statistical floor.
		if _, ok := joinedEnhancedSummary(comm, summaries); ok {
			llmEnhancedCount++
		} else {
			statisticalOnlyCount++
		}
	}

	avgNonSingletonSize := 0.0
	if nonSingletonCount > 0 {
		avgNonSingletonSize = float64(totalNonSingletonSize) / float64(nonSingletonCount)
	}

	result.Metrics["communities_total"] = totalCount
	result.Metrics["communities_non_singleton"] = nonSingletonCount
	result.Metrics["communities_largest_size"] = largestSize
	result.Metrics["communities_avg_size"] = avgNonSingletonSize
	result.Metrics["communities_with_keywords"] = communitiesWithKeywords
	result.Metrics["communities_llm_enhanced"] = llmEnhancedCount
	result.Metrics["communities_statistical_only"] = statisticalOnlyCount

	result.Details["community_structure_validation"] = map[string]any{
		"total_communities":      totalCount,
		"non_singleton_count":    nonSingletonCount,
		"largest_community":      largestSize,
		"avg_non_singleton_size": avgNonSingletonSize,
		"with_keywords":          communitiesWithKeywords,
		"llm_enhanced":           llmEnhancedCount,
		"statistical_only":       statisticalOnlyCount,
		"message": fmt.Sprintf("Community structure: %d total, %d non-singleton (avg size: %.1f), %d LLM-enhanced",
			totalCount, nonSingletonCount, avgNonSingletonSize, llmEnhancedCount),
	}

	// For statistical tier, we require at least some non-singleton communities
	// to verify that graph connectivity (incoming edges) is working correctly.
	// Without incoming edges, LPA produces all singletons because nodes appear isolated.
	if nonSingletonCount == 0 && totalCount > 0 {
		return fmt.Errorf("no non-singleton communities found (%d total) - graph connectivity may be broken", totalCount)
	}

	// Run community ground truth validation (semantic coherence checks)
	groundTruthResult := s.validateCommunityGroundTruth(communities, result)
	if groundTruthResult != nil && !groundTruthResult.Passed() {
		// Record violations as warnings (don't fail the test, just report)
		for _, v := range groundTruthResult.Violations {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Community ground truth violation [%s]: %s - %s",
					v.Type, v.ExpectationName, v.Details))
		}
	}

	return nil
}

// validateCommunityGroundTruth runs semantic coherence validation against expected groupings.
func (s *TieredScenario) validateCommunityGroundTruth(communities []*clustering.Community, result *Result) *community.ValidationResult {
	validator := community.NewDefaultValidator()
	groundTruthResult := validator.Validate(communities)

	// Record metrics
	result.Metrics["community_ground_truth_total"] = groundTruthResult.ExpectationsTotal
	result.Metrics["community_ground_truth_passed"] = groundTruthResult.ExpectationsPassed

	// Record detailed results
	violationDetails := make([]map[string]any, 0, len(groundTruthResult.Violations))
	for _, v := range groundTruthResult.Violations {
		violationDetails = append(violationDetails, map[string]any{
			"expectation": v.ExpectationName,
			"type":        string(v.Type),
			"details":     v.Details,
			"entities":    v.Entities,
			"communities": v.CommunityIDs,
		})
	}

	result.Details["community_ground_truth"] = map[string]any{
		"expectations_total":  groundTruthResult.ExpectationsTotal,
		"expectations_passed": groundTruthResult.ExpectationsPassed,
		"passed":              groundTruthResult.Passed(),
		"violations":          violationDetails,
	}

	return groundTruthResult
}

// executeValidateKCoreIndex validates k-core decomposition index for statistical/semantic tiers
func (s *TieredScenario) executeValidateKCoreIndex(ctx context.Context, result *Result) error {
	if s.natsClient == nil {
		result.Warnings = append(result.Warnings, "NATS client not available, skipping k-core validation")
		return nil
	}

	// Get structural index info from NATS
	info, err := s.natsClient.GetStructuralIndexInfo(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to get structural index info: %v", err))
		return nil
	}

	if !info.BucketExists {
		result.Warnings = append(result.Warnings, "STRUCTURAL_INDEX bucket does not exist")
		return nil
	}

	if info.KCore == nil {
		result.Warnings = append(result.Warnings, "K-core metadata not found in STRUCTURAL_INDEX")
		result.Details["kcore_validation"] = map[string]any{
			"valid":   false,
			"error":   "metadata not found",
			"message": "K-core index not computed - check graph_analysis.structural_index config",
		}
		return nil
	}

	kcore := info.KCore
	valid := true
	issues := []string{}

	// Validate entity count
	if kcore.EntityCount == 0 {
		issues = append(issues, "entity count is 0")
		valid = false
	}

	// Validate MaxCore (should be >= 0, ideally > 0 for statistical tier with community edges)
	if kcore.MaxCore < 0 {
		issues = append(issues, fmt.Sprintf("invalid MaxCore: %d", kcore.MaxCore))
		valid = false
	}

	// Validate core buckets sum equals entity count
	totalInBuckets := 0
	for _, count := range kcore.CoreBuckets {
		totalInBuckets += count
	}
	if totalInBuckets > 0 && totalInBuckets != kcore.EntityCount {
		issues = append(issues, fmt.Sprintf("core buckets sum (%d) != entity count (%d)", totalInBuckets, kcore.EntityCount))
	}

	// For statistical tier, expect some structure (MaxCore > 0) from community detection edges
	if kcore.MaxCore == 0 {
		result.Warnings = append(result.Warnings,
			"K-core MaxCore is 0 - graph may lack sufficient edge density")
	}

	// Calculate percentage of entities in core >= 2 (non-leaf nodes)
	entitiesInCore2Plus := 0
	for core, count := range kcore.CoreBuckets {
		if core >= 2 {
			entitiesInCore2Plus += count
		}
	}
	core2PlusPercent := 0.0
	if kcore.EntityCount > 0 {
		core2PlusPercent = 100.0 * float64(entitiesInCore2Plus) / float64(kcore.EntityCount)
	}

	result.Metrics["kcore_entity_count"] = kcore.EntityCount
	result.Metrics["kcore_max_core"] = kcore.MaxCore
	result.Metrics["kcore_core2_plus_percent"] = core2PlusPercent
	result.Metrics["kcore_valid"] = valid

	result.Details["kcore_validation"] = map[string]any{
		"valid":              valid,
		"entity_count":       kcore.EntityCount,
		"max_core":           kcore.MaxCore,
		"core_buckets":       kcore.CoreBuckets,
		"total_in_buckets":   totalInBuckets,
		"core2_plus_count":   entitiesInCore2Plus,
		"core2_plus_percent": core2PlusPercent,
		"computed_at":        kcore.ComputedAt,
		"issues":             issues,
		"message": fmt.Sprintf("K-core: %d entities, MaxCore=%d, %.1f%% in core>=2",
			kcore.EntityCount, kcore.MaxCore, core2PlusPercent),
	}

	if !valid {
		return fmt.Errorf("k-core validation failed: %v", issues)
	}

	return nil
}

// executeValidatePivotIndex validates pivot distance index for statistical/semantic tiers
func (s *TieredScenario) executeValidatePivotIndex(ctx context.Context, result *Result) error {
	if s.natsClient == nil {
		result.Warnings = append(result.Warnings, "NATS client not available, skipping pivot validation")
		return nil
	}

	// Get structural index info from NATS
	info, err := s.natsClient.GetStructuralIndexInfo(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to get structural index info: %v", err))
		return nil
	}

	if !info.BucketExists {
		result.Warnings = append(result.Warnings, "STRUCTURAL_INDEX bucket does not exist")
		return nil
	}

	if info.Pivot == nil {
		result.Warnings = append(result.Warnings, "Pivot metadata not found in STRUCTURAL_INDEX")
		result.Details["pivot_validation"] = map[string]any{
			"valid":   false,
			"error":   "metadata not found",
			"message": "Pivot index not computed - check graph_analysis.structural_index config",
		}
		return nil
	}

	pivot := info.Pivot
	valid := true
	issues := []string{}

	// Validate we have pivots
	if len(pivot.Pivots) == 0 {
		issues = append(issues, "no pivots selected")
		valid = false
	}

	// Validate entity count
	if pivot.EntityCount == 0 {
		issues = append(issues, "entity count is 0")
		valid = false
	}

	// Validate pivots are non-empty strings
	emptyPivots := 0
	for _, p := range pivot.Pivots {
		if p == "" {
			emptyPivots++
		}
	}
	if emptyPivots > 0 {
		issues = append(issues, fmt.Sprintf("%d empty pivot IDs", emptyPivots))
		valid = false
	}

	// Expected pivot count (from config, typically 16)
	expectedPivots := 16
	if len(pivot.Pivots) < expectedPivots {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Only %d/%d pivots selected (graph may be small)", len(pivot.Pivots), expectedPivots))
	}

	result.Metrics["pivot_count"] = len(pivot.Pivots)
	result.Metrics["pivot_entity_count"] = pivot.EntityCount
	result.Metrics["pivot_valid"] = valid

	result.Details["pivot_validation"] = map[string]any{
		"valid":           valid,
		"pivot_count":     len(pivot.Pivots),
		"expected_pivots": expectedPivots,
		"entity_count":    pivot.EntityCount,
		"pivots":          pivot.Pivots,
		"computed_at":     pivot.ComputedAt,
		"issues":          issues,
		"message": fmt.Sprintf("Pivot: %d pivots for %d entities",
			len(pivot.Pivots), pivot.EntityCount),
	}

	if !valid {
		return fmt.Errorf("pivot validation failed: %v", issues)
	}

	return nil
}
