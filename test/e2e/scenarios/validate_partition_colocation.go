// Package scenarios provides E2E test scenarios for SemStreams semantic processing
package scenarios

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/c360studio/semstreams/graph/clustering"
)

// ============================================================================
// Epic B increment B2 — the PARTITION CO-LOCATION diagnostic (a RECORDER)
// ============================================================================
//
// This stage answers the B2 DECISION on the cheap 1.7b `task e2e:semantic` run:
// is the residual thematic-recall gap (measured by B0, validate_thematic_eval.go)
// caused by the PARTITION or by the SYNTHESIZER?
//
// GraphRAG global-search recall depends on whether related entities are grouped
// into the SAME community. B0 records per-query thematic RECALL against a known
// warehouse/logistics corpus (testdata/semantic/*.jsonl). This stage measures the
// COMPLEMENTARY signal: for each B0 thematic query's KNOWN expected entities, are
// they CO-LOCATED in one level-0 community, or SCATTERED across many?
//
// The partition (COMMUNITY_INDEX membership) is built by the LPA detector over
// graph EDGES — the LLM only writes community SUMMARIES, never membership — so
// this co-location signal is IDENTICAL at any tier. That is the whole point: it
// answers B2 deterministically with NO LLM, NO frontier model, NO 8B, on the
// same cheap 1.7b run that records the B0 recall it is cross-referenced against.
//
// ─────────────────────────────────────────────────────────────────────────────
// INTERPRETATION (read each query's plurality_share HERE against that same
// query's theme-recall in the B0 thematic_answer_eval details from the SAME run):
//
//	HIGH plurality_share (>= ~0.8) + LOW B0 recall on the same query
//	    → the expected entities ARE co-located in one level-0 community;
//	      the gap is DOWNSTREAM (community selection / summary / synthesis)
//	      → B2 partition-rebuild is LOW-ROI.
//
//	LOW plurality_share (expected entities scattered across many communities)
//	    → the partition works AGAINST thematic recall
//	      → B2 (semantic-kNN edges to co-locate related entities) HAS ROI.
//
// ─────────────────────────────────────────────────────────────────────────────
// THIS STAGE IS A RECORDER, NEVER A GATE. A bad co-location number is DATA, not a
// failure — it is exactly the B2 evidence this stage exists to produce. It
// computes every number, writes them into result.Metrics + result.Details, and
// prints them prominently, but NEVER turns a bad number into result.Errors.
//
// The ONE thing it MAY hard-fail: if GetAllCommunities is unreachable (transport
// broken). A reachable-but-scattered partition is DATA; an unreachable index is a
// real failure. This mirrors the B0 header contract exactly.
// ─────────────────────────────────────────────────────────────────────────────

// PartitionColocationHighShare is the plurality-share level at/above which a
// query's expected entities are considered CO-LOCATED (for the printed
// interpretation label only — this stage never gates on it).
const PartitionColocationHighShare = 0.8

// colocationExpectation is one B2 co-location fixture. It reuses the SAME id as a
// B0 thematicQuestion so the co-location table lines up 1:1 with the B0 recall
// table from the same run. expected are KNOWN source-record ids from the semantic
// corpus (testdata/semantic/*.jsonl) — each is the LAST dotted segment of the
// 6-part ingested EntityID (e.g. c360.logistics.maintenance.work.completed.maint-003
// → maint-003). All were verified present in the corpus.
type colocationExpectation struct {
	// id matches a thematicQuestions[].id (validate_thematic_eval.go) so the two
	// tables cross-reference by row.
	id string
	// expected are the known source-record ids a coherent thematic answer for
	// this query should draw on; co-location asks whether these land in ONE
	// level-0 community.
	expected []string
}

// partitionColocationExpectations mirrors thematicQuestions 1:1 by id. Each
// expected list is the KNOWN ground-truth entity set for that thematic query,
// grounded in testdata/semantic/*.jsonl (see thematicQuestions' provenance
// comment for why each entity belongs to its theme).
var partitionColocationExpectations = []colocationExpectation{
	{id: "forklift-maintenance", expected: []string{"maint-001", "maint-008", "doc-ops-001", "obs-002", "obs-003"}},
	{id: "cold-chain", expected: []string{"sensor-temp-001", "sensor-temp-002"}},
	{id: "fire-emergency", expected: []string{"doc-safety-001", "doc-emergency-001", "maint-005", "maint-007"}},
	{id: "dock-equipment", expected: []string{"maint-003", "maint-009", "maint-016", "doc-shipping-001"}},
	{id: "conveyor-systems", expected: []string{"maint-002", "obs-004", "doc-maint-001"}},
}

// colocationMatch records where ONE expected entity landed in the level-0
// partition. FullEntityID carries the full 6-part member id so any id-mismatch
// (a fixture expecting an id the corpus never produced) is visible in the print.
type colocationMatch struct {
	Expected     string `json:"expected"`
	FullEntityID string `json:"full_entity_id"`
	CommunityID  string `json:"community_id"`
	// AllCommunityIDs is populated (len > 1) ONLY when the entity abnormally
	// appears in more than one level-0 community. CommunityID is then the
	// sorted-first, chosen deterministically for the plurality tally.
	AllCommunityIDs []string `json:"all_community_ids,omitempty"`
}

// colocationGrade is the per-query co-location outcome, recorded verbatim into
// result.Details for the B2 ledger.
type colocationGrade struct {
	ID                  string            `json:"id"`
	ExpectedCount       int               `json:"expected_count"`
	FoundCount          int               `json:"found_count"`
	NotInPartitionCount int               `json:"not_in_partition_count"`
	NotInPartition      []string          `json:"not_in_partition"`
	DistinctCommunities int               `json:"distinct_communities"`
	PluralityCommunity  string            `json:"plurality_community_id"`
	PluralityCount      int               `json:"plurality_count"`
	PluralityShare      float64           `json:"plurality_share"`
	MultiMembership     []colocationMatch `json:"multi_membership,omitempty"`
	Matches             []colocationMatch `json:"matches"`
}

// executePartitionColocation is the B2 co-location diagnostic. It reads the
// level-0 partition, resolves each B0 thematic query's KNOWN expected entities to
// their level-0 community, records co-location vs scatter, and prints the block.
// It does NOT hard-fail on a scattered partition — only on an unreachable
// GetAllCommunities (see the header comment).
func (s *TieredScenario) executePartitionColocation(ctx context.Context, result *Result) error {
	if s.natsClient == nil {
		// Transport prerequisite genuinely absent — the one hard-fail case (we
		// cannot read the partition at all).
		return fmt.Errorf("partition-colocation diagnostic requires the NATS validation client, which was not initialized")
	}

	fmt.Println("\n========================================================================")
	fmt.Println("[B2 PARTITION COLOCATION] known-corpus co-location diagnostic (RECORDER, not a gate)")
	fmt.Println("========================================================================")

	comms, err := s.natsClient.GetAllCommunities(ctx)
	if err != nil {
		// Unreachable partition index — the ONE allowed hard-fail. A scattered or
		// empty-but-readable partition is DATA, graded below.
		return fmt.Errorf("partition-colocation diagnostic could not read COMMUNITY_INDEX via GetAllCommunities: %w", err)
	}

	idx := buildLevel0ColocationIndex(comms)

	grades := make([]colocationGrade, 0, len(partitionColocationExpectations))
	for _, e := range partitionColocationExpectations {
		grades = append(grades, gradeColocation(e, idx))
	}

	agg := aggregateColocation(grades, idx.level0Communities)

	// ---- metrics (the B2 numbers, cross-referenced against B0 recall) ----
	result.Metrics["partition_colocation_mean"] = agg.mean
	result.Metrics["partition_entities_not_in_community"] = agg.entitiesNotInCommunity
	result.Metrics["partition_level0_communities"] = agg.level0Communities

	// ---- full ledger into Details ----
	result.Details["partition_colocation"] = map[string]any{
		"high_share_threshold": PartitionColocationHighShare,
		"per_query":            grades,
		"aggregate":            agg.detail(),
		"note": "B2 decision recorder (no gate). Cross-reference each query's " +
			"plurality_share against that same query's theme-recall in the B0 " +
			"thematic_answer_eval details from the SAME run. HIGH plurality_share " +
			"(>=0.8) + LOW B0 recall => entities ARE co-located, residual gap is " +
			"downstream (selection/summary/synthesis) => partition-rebuild LOW-ROI. " +
			"LOW plurality_share (scattered) => the partition works against thematic " +
			"recall => semantic-kNN co-location edges HAVE ROI. The partition is " +
			"LPA over graph edges (LLM writes summaries only), so this signal is " +
			"identical at any tier. TRUST GUARD: a high colocation_mean is only " +
			"meaningful when entities_not_in_community is LOW and level0_communities " +
			"> 1. A query that found only 1 of its expected entities scores share " +
			"1.0 vacuously, and a collapsed single-community partition scores every " +
			"query 1.0 — both are low-coverage/degenerate artifacts that argue FOR " +
			"B2 ROI (more/better edges), not against it.",
	}

	printPartitionColocation(grades, agg)
	return nil
}

// level0ColocationIndex maps a source-record id (last dotted segment of a member
// EntityID) to the level-0 community/communities and full member ids that carry
// it. An entity should map to exactly one community; extras are recorded as
// abnormal multi-membership by the caller.
type level0ColocationIndex struct {
	// commIDs maps lastSegment -> sorted distinct level-0 community IDs.
	commIDs map[string][]string
	// fullIDs maps lastSegment -> sorted distinct full member EntityIDs.
	fullIDs map[string][]string
	// level0Communities counts the level-0 communities scanned (for the print).
	level0Communities int
}

// buildLevel0ColocationIndex indexes every level-0 community member by the last
// dotted segment of its EntityID. Only Level == 0 communities participate — the
// LPA base partition is where co-location is decided.
func buildLevel0ColocationIndex(comms []*clustering.Community) level0ColocationIndex {
	commSets := make(map[string]map[string]struct{})
	fullSets := make(map[string]map[string]struct{})
	level0 := 0
	for _, c := range comms {
		if c == nil || c.Level != 0 {
			continue
		}
		level0++
		for _, m := range c.Members {
			seg := lastSegment(m)
			if commSets[seg] == nil {
				commSets[seg] = make(map[string]struct{})
				fullSets[seg] = make(map[string]struct{})
			}
			commSets[seg][c.ID] = struct{}{}
			fullSets[seg][m] = struct{}{}
		}
	}
	idx := level0ColocationIndex{
		commIDs:           make(map[string][]string, len(commSets)),
		fullIDs:           make(map[string][]string, len(fullSets)),
		level0Communities: level0,
	}
	for seg, set := range commSets {
		idx.commIDs[seg] = sortedKeys(set)
	}
	for seg, set := range fullSets {
		idx.fullIDs[seg] = sortedKeys(set)
	}
	return idx
}

// gradeColocation resolves one query's expected entities against the level-0
// index and computes its co-location grade. Deterministic throughout: matches
// are sorted by expected id, plurality ties break on the sorted-first community
// id.
func gradeColocation(e colocationExpectation, idx level0ColocationIndex) colocationGrade {
	g := colocationGrade{
		ID:            e.id,
		ExpectedCount: len(e.expected),
	}

	// Resolve each expected id, deterministically over a sorted, de-duplicated
	// copy — a fixture that accidentally repeated an id must not double-count it
	// into found/plurality and skew the metric.
	expected := dedupeSorted(e.expected)

	// perCommunity counts how many of THIS query's found entities were assigned
	// to each community (each found entity counts once, toward its sorted-first
	// community when abnormally multi-homed).
	perCommunity := make(map[string]int)
	for _, id := range expected {
		commIDs := idx.commIDs[id]
		if len(commIDs) == 0 {
			g.NotInPartition = append(g.NotInPartition, id)
			continue
		}
		match := colocationMatch{
			Expected:     id,
			FullEntityID: strings.Join(idx.fullIDs[id], ","),
			CommunityID:  commIDs[0], // sorted-first: deterministic assignment
		}
		if len(commIDs) > 1 {
			match.AllCommunityIDs = commIDs
			g.MultiMembership = append(g.MultiMembership, match)
		}
		g.Matches = append(g.Matches, match)
		perCommunity[match.CommunityID]++
	}

	g.FoundCount = len(g.Matches)
	g.NotInPartitionCount = len(g.NotInPartition)
	g.DistinctCommunities = len(perCommunity)

	// Plurality community = the one holding the most found entities; ties break
	// on the sorted-first community id for stable output.
	plCommunities := sortedKeys(setFromKeys(perCommunity))
	for _, cid := range plCommunities {
		if perCommunity[cid] > g.PluralityCount {
			g.PluralityCount = perCommunity[cid]
			g.PluralityCommunity = cid
		}
	}
	if g.FoundCount > 0 {
		g.PluralityShare = float64(g.PluralityCount) / float64(g.FoundCount)
	}

	return g
}

// colocationAggregate rolls up the per-query grades for the B2 metrics.
type colocationAggregate struct {
	totalQueries           int
	includedQueries        int // queries with found_count > 0 (mean domain)
	mean                   float64
	entitiesNotInCommunity int
	// level0Communities is the total number of level-0 communities scanned — the
	// partition CARDINALITY. It is the trust guard on mean: a collapsed 1-community
	// partition scores mean 1.0 while being the worst possible partition, so a high
	// mean is only meaningful when this is > 1 (see the Details note).
	level0Communities int
}

// aggregateColocation computes partition_colocation_mean (mean plurality_share
// over queries that found at least one entity), the total count of expected
// entities absent from the partition, and carries the partition cardinality
// (level0Communities) through as the mean's trust guard.
func aggregateColocation(grades []colocationGrade, level0Communities int) colocationAggregate {
	a := colocationAggregate{totalQueries: len(grades), level0Communities: level0Communities}
	var shareSum float64
	for _, g := range grades {
		a.entitiesNotInCommunity += g.NotInPartitionCount
		if g.FoundCount > 0 {
			a.includedQueries++
			shareSum += g.PluralityShare
		}
	}
	if a.includedQueries > 0 {
		a.mean = shareSum / float64(a.includedQueries)
	}
	return a
}

func (a colocationAggregate) detail() map[string]any {
	return map[string]any{
		"total_queries":             a.totalQueries,
		"included_queries":          a.includedQueries, // found_count > 0
		"colocation_mean":           a.mean,
		"entities_not_in_community": a.entitiesNotInCommunity,
		"level0_communities":        a.level0Communities,
	}
}

// ---- printing ---------------------------------------------------------------

func printPartitionColocation(grades []colocationGrade, agg colocationAggregate) {
	fmt.Println("\n[B2 PARTITION COLOCATION] per-query co-location (cross-ref B0 theme-recall, same run):")
	for _, g := range grades {
		label := "SCATTERED"
		if g.FoundCount == 0 {
			label = "NONE-FOUND"
		} else if g.PluralityShare >= PartitionColocationHighShare {
			label = "CO-LOCATED"
		}
		fmt.Printf("  - %-22s expected=%-2d found=%-2d not_in_partition=%-2d distinct_comms=%-2d plurality_share=%.2f [%s]\n",
			g.ID, g.ExpectedCount, g.FoundCount, g.NotInPartitionCount,
			g.DistinctCommunities, g.PluralityShare, label)
		if g.PluralityCommunity != "" {
			fmt.Printf("      plurality_community=%s (%d/%d found here)\n",
				g.PluralityCommunity, g.PluralityCount, g.FoundCount)
		}
		for _, m := range g.Matches {
			fmt.Printf("      %-16s -> comm=%s  member=%s\n", m.Expected, m.CommunityID, m.FullEntityID)
		}
		if len(g.NotInPartition) > 0 {
			fmt.Printf("      NOT_IN_PARTITION=%v\n", g.NotInPartition)
		}
		for _, m := range g.MultiMembership {
			fmt.Printf("      MULTI_MEMBERSHIP %-16s in comms=%v\n", m.Expected, m.AllCommunityIDs)
		}
	}

	fmt.Println("\n[B2 PARTITION COLOCATION] aggregate (RECORDER — no hard-fail):")
	fmt.Printf("  partition_colocation_mean           = %.2f (mean plurality_share over %d/%d queries with found>0)\n",
		agg.mean, agg.includedQueries, agg.totalQueries)
	fmt.Printf("  partition_entities_not_in_community = %d\n", agg.entitiesNotInCommunity)
	fmt.Printf("  partition_level0_communities        = %d (partition cardinality — trust guard on mean)\n",
		agg.level0Communities)
	fmt.Println("  interpret: HIGH mean + LOW B0 recall => co-located, gap is downstream => partition-rebuild LOW-ROI.")
	fmt.Println("             LOW mean (scattered)       => partition fights thematic recall => kNN co-location HAS ROI.")
	fmt.Println("  TRUST GUARD: a HIGH mean is meaningful ONLY when entities_not_in_community is low AND level0_communities>1;")
	fmt.Println("               a 1-community partition or found=1/expected=N query scores 1.0 vacuously => argues FOR B2 ROI.")
	fmt.Println("========================================================================")
}

// ---- helpers ----------------------------------------------------------------

// lastSegment returns the final dotted segment of a 6-part EntityID — the
// source-record id used as ground-truth (e.g.
// c360.logistics.maintenance.work.completed.maint-003 -> maint-003). Exact
// segment equality (NOT substring/HasSuffix) is what the caller matches on, so
// maint-1 never matches maint-16.
func lastSegment(entityID string) string {
	if i := strings.LastIndex(entityID, "."); i >= 0 {
		return entityID[i+1:]
	}
	return entityID
}

// dedupeSorted returns a sorted copy of ids with duplicates removed, so a
// repeated expected id cannot double-count into found/plurality.
func dedupeSorted(ids []string) []string {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return sortedKeys(set)
}

// sortedKeys returns the sorted keys of a string set, for deterministic output.
func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// setFromKeys lifts a counter's key set into a set for sortedKeys reuse.
func setFromKeys(m map[string]int) map[string]struct{} {
	out := make(map[string]struct{}, len(m))
	for k := range m {
		out[k] = struct{}{}
	}
	return out
}
