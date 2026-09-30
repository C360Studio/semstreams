# Tiered-Semantic Scenario Review

**Reviewed**: 2025-12-20  
**Files**: `test/e2e/scenarios/tiered.go`, `test/e2e/scenarios/tiered_semantic.go`  
**Status**: Good LLM Validation, Missing RAG Pattern Tests

---

## Scenario Overview

The `tiered --variant semantic` scenario validates the full ML stack with neural embeddings and LLM-enhanced community summaries. This is Tier 2 - the highest capability tier requiring SemEmbed and SemInstruct services.

**Duration**: ~11.5 min on a CI runner; 4m17s with `E2E_PATH_ONLY=1`, the per-PR path-only shape (first CI run, gh#1117)  
**Tier**: Semantic (Tier 2)  
**Dependencies**: NATS + SemEmbed + SemInstruct

---

## What's Tested

### All Stages for Semantic Variant

| Stage | Purpose | Assertion Strength |
|-------|---------|-------------------|
| `verify-components` | Check required components exist | Strong |
| `send-mixed-data` | Send telemetry + regular messages | Medium |
| `validate-processing` | Verify graph processor running | Medium |
| `verify-entity-count` | Count entities in ENTITY_STATES | Medium |
| `verify-entity-retrieval` | Retrieve specific known entities | Medium |
| `validate-entity-structure` | Validate entity data structure | Medium |
| `verify-index-population` | Check 7 indexes populated | Strong |
| `test-semantic-search` | Test neural embedding search | Strong |
| `verify-search-quality` | Validate search result quality | Medium |
| `test-http-gateway` | Test HTTP gateway search | Medium |
| `test-embedding-fallback` | Verify hybrid mode | Medium |
| `compare-statistical-semantic` | Capture comparison data | Weak |
| **`compare-communities`** | LLM enhancement validation | **Strong** |
| `validate-rules` | Verify rules evaluated/triggered | Medium |
| `validate-metrics` | Check Prometheus metrics exposed | Strong |
| `verify-outputs` | Verify output components exist | Medium |

---

## Correctness Assessment

### Correct

1. **LLM enhancement validation** (`tiered_semantic.go:186-236`)
   - Waits for LLM enhancement to complete (2 min timeout)
   - Counts LLM-enhanced vs statistical-only communities
   - **Fails if zero LLM-enhanced communities** (hard failure, line 215)
   - Calculates summary length ratio and word overlap

2. **SemEmbed health check** (`tiered.go:659-678`)
   - Checks `/health` endpoint on port 8081
   - Detects semantic vs statistical mode correctly

3. **Neural embedding validation** (`tiered.go:680-746`)
   - Waits for embeddings using event-driven polling
   - Validates embedding metrics in Prometheus

4. **Community comparison** (`tiered_semantic.go:186-254`)
   - Compares statistical vs LLM summaries
   - Calculates Jaccard similarity on word sets
   - Reports non-singleton community statistics

### Issues Found

| Issue | Severity | Description |
|-------|----------|-------------|
| GraphRAG not tested | **High** | Full semantic search but no GraphRAG query test |
| LLM summary quality not validated | **Medium** | Checks summary exists, not that it's coherent |
| No semantic search ranking test | **Medium** | Doesn't verify neural embeddings outperform BM25 |
| Community structure assumptions | **Medium** | Warns on zero non-singletons but doesn't fail |
| Comparison stage doesn't actually compare | **Low** | Just captures data, comparison is external |

---

## Gap Analysis

### GraphRAG Still Not Tested

Even at Tier 2 (semantic), GraphRAG queries are not tested. The test validates:
- Neural embeddings generated ✓
- LLM summaries created ✓
- Communities formed ✓

But does NOT test:
- Querying communities by topic (GraphRAG global search)
- Entity-to-community context retrieval (GraphRAG local search)
- LLM-generated answers from community context

### Features Documented but Not Tested

| Feature | Documentation Source | Gap |
|---------|---------------------|-----|
| **GraphRAG local search** | `docs/concepts/09-graphrag-pattern.md` | No test |
| **GraphRAG global search** | `docs/concepts/09-graphrag-pattern.md` | No test |
| LLM-generated answers | GraphRAG returns answers | Not tested |
| LLM summary coherence | Summaries should be readable | Not validated |
| Neural vs BM25 quality | Semantic should outperform | Not compared |
| Semantic edge creation | Virtual edges from similarity | Not validated |
| PathRAG (still works at Tier 2) | Should still function | Not tested |

### LLM Summary Quality Not Validated

The test checks:
```go
// From tiered_semantic.go:215
if variant == "semantic" && stats.llmEnhancedCount == 0 {
    return fmt.Errorf("semantic tier requires at least one LLM-enhanced community...")
}
```

But doesn't validate:
- Summary is coherent English
- Summary reflects community content
- Summary length is reasonable
- Keywords appear in summary

---

## Community Analysis Details

The `executeCompareCommunities` stage performs detailed community analysis:

```go
type CommunityComparison struct {
    CommunityID        string
    Level              int
    MemberCount        int
    StatisticalSummary string
    LLMSummary         string
    SummaryStatus      string
    Keywords           []string
    SummaryLengthRatio float64
    WordOverlap        float64
}
```

**Metrics captured**:
- `communities_total` - Total communities found
- `communities_llm_enhanced` - With LLM summaries
- `communities_statistical_only` - Without LLM summaries
- `avg_summary_length_ratio` - LLM/statistical length
- `avg_word_overlap` - Jaccard similarity
- `communities_non_singleton` - Communities with >1 member
- `largest_community_size` - Biggest community
- `avg_non_singleton_size` - Average membership

**Good**: This provides useful debugging information.  
**Issue**: Doesn't assert quality thresholds on these metrics.

---

## Recommendations

### Priority: High (Must Fix)

1. **Add GraphRAG e2e test**
   
   Even more critical at Tier 2 since full LLM capabilities are available:
   
   ```go
   // Example: GraphRAG Q&A test
   graphRAGQATests := []struct {
       question       string
       expectedTopics []string
       minCommunities int
   }{
       {
           question:       "What's happening with temperature sensors?",
           expectedTopics: []string{"temperature", "sensor"},
           minCommunities: 1,
       },
   }
   ```

2. **Validate LLM summary quality**
   
   Add assertions:
   - Summary length > 50 characters (non-trivial)
   - Summary doesn't start with error text
   - At least 1 keyword appears in summary
   
   ```go
   if len(comm.LLMSummary) < 50 {
       result.Warnings = append(result.Warnings, "LLM summary too short")
   }
   ```

### Priority: Medium (Should Fix)

3. **Compare neural vs BM25 search quality**
   
   Run same queries on statistical and semantic variants, verify:
   - Semantic returns higher relevance scores
   - Semantic finds more subtle matches
   
4. **Test semantic edge creation**
   
   Verify that similar entities (by embedding) have edges:
   - Query entities with similar descriptions
   - Check they're connected via semantic edges

5. **Validate non-singleton community requirement**
   
   Make non-singleton check a hard failure for semantic:
   ```go
   if variant == "semantic" && stats.nonSingletonCount == 0 {
       return fmt.Errorf("semantic tier should produce non-singleton communities")
   }
   ```

### Priority: Low (Nice to Have)

6. **Add LLM answer generation test**
   
   Test the full GraphRAG Q&A flow:
   - Submit natural language question
   - Receive LLM-generated answer
   - Verify answer references entities from test data

7. **Test graceful degradation**
   
   If SemInstruct fails:
   - Statistical summaries should still work
   - Test should detect and report degradation

---

## Feature Coverage Matrix

| Documented Feature | Tested? | Notes |
|--------------------|---------|-------|
| Neural embeddings (SemEmbed) | Yes | Health check + metrics |
| LLM summaries (SemInstruct) | Yes | Enhancement wait + count |
| Community formation | Yes | Wait + count |
| LLM-enhanced summaries | Yes | Hard failure if 0 |
| Summary length ratio | Yes | Calculated but not asserted |
| Word overlap (Jaccard) | Yes | Calculated but not asserted |
| Non-singleton communities | Partial | Warning only |
| **GraphRAG local search** | **No** | Critical gap |
| **GraphRAG global search** | **No** | Critical gap |
| LLM Q&A generation | **No** | Not tested |
| LLM summary coherence | **No** | Not validated |
| Neural vs BM25 comparison | **No** | Not tested |
| Semantic edges | **No** | Not validated |
| PathRAG at Tier 2 | **No** | Not tested |

---

## Test Output Files

The semantic variant produces comparison output files:

```
test/e2e/results/
├── comparison-semantic-{timestamp}.json
└── community-comparison-semantic-{timestamp}.json
```

These contain detailed data for external analysis but are not used for pass/fail decisions.

---

## Conclusion

**Overall Assessment**: The `tiered --variant semantic` scenario provides **strong validation** of LLM enhancement workflow, correctly requiring at least one LLM-enhanced community. However, it shares the **critical gap** with other tiers: GraphRAG patterns are not tested despite being the primary use case for community-based search.

**Strengths**:
- Hard failure on zero LLM enhancements (good)
- Detailed community comparison metrics (good)
- SemEmbed health verification (good)
- Wait for LLM enhancement with timeout (good)

**Gaps**:
1. GraphRAG (local and global search) not tested
2. LLM answer generation not tested
3. Summary quality not validated
4. Neural vs BM25 quality not compared

**Recommendation**: Add GraphRAG e2e test to validate the full semantic search → LLM answer pipeline. This is the flagship feature of Tier 2 and is currently untested.
