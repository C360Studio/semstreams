// Package scenarios provides E2E test scenarios for SemStreams semantic processing
package scenarios

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/c360studio/semstreams/test/e2e/client"
)

// ============================================================================
// Epic B increment B0 — the thematic-answer eval (the RULER, not yet a GATE)
// ============================================================================
//
// This stage is the measurement instrument for GraphRAG THEMATIC SYNTHESIS —
// "group + summarize retrieved entities into a natural-language answer," the
// community layer that (per the pre-v1 program, docs/proposals/prev1-program.md)
// is broken and has never been measured. It drives the real GraphRAG thematic
// path — graph.query.globalSearch over the live NATS wire — with global/thematic
// questions grounded in the semantic-tier corpus (testdata/semantic/*.jsonl) and
// grades the synthesized `answer` five DETERMINISTIC ways. NO LLM judge.
//
// The five dimensions (all computed here, every run):
//
//	1. NON-DEGENERATE   — answer is non-empty whenever entities were retrieved.
//	                      (Anti-regression for synthesizeAnswer() returning "" on
//	                      empty community summaries — graphrag.go:1696.)
//	2. GROUNDED         — answer contains the expected real labels/keywords
//	                      (expect_any recall) AND a FABRICATION GUARD: the answer
//	                      must NOT contain a set of invented/wrong terms.
//	3. THEME-RECALL     — answer contains >= ThematicRecallThreshold (0.6) of a
//	                      fixture's required theme_terms. The actual fraction is
//	                      recorded.
//	4. TWO-RUN STABILITY — (a) level-0 community MEMBERSHIP-HASHES are identical
//	                      across a fresh detection cycle over the unchanged graph
//	                      (partition determinism; match ratio recorded, target
//	                      1.0); (b) the same global question asked twice yields an
//	                      identical answer theme-set (answer stability).
//	5. TIERED GRACEFUL FALLBACK (owner tenet) — the non-empty degraded FLOOR:
//	                      when synthesis degrades, the answer must still be
//	                      non-empty + grounded with Degraded=true + a
//	                      DegradedReason, NEVER empty or an error. This encodes
//	                      "structural/statistical is a correct floor, never
//	                      fail/empty." Graded against whatever degraded response
//	                      the current code actually produces (see the note in
//	                      recordDegradedFloor for why the cache-absent path is not
//	                      cleanly reachable this late in the tier).
//
// ─────────────────────────────────────────────────────────────────────────────
// B0 IS A RECORDER, NOT YET A HARD GATE — but this is a bootstrapping baseline,
// NOT warn-and-forget. Against today's broken thematic layer these dimensions
// WILL score poorly (unstable partition, empty answers, low theme-recall).
// Hard-failing them now would red the tier for everyone BEFORE B1 can fix it,
// so B0 computes every dimension, writes the numbers into result.Metrics +
// result.Details, and prints them prominently — but does NOT turn them into
// result.Errors hard-fails.
//
// EACH LATER INCREMENT CONVERTS ITS DIMENSION TO A HARD GATE — leaving a
// dimension report-only PAST its owning increment is exactly the drift this
// program exists to kill:
//
//	B1 → dimension 1 (non-empty floor) + dimension 4a (partition determinism)
//	B2 → dimension 2 (grounding + fabrication) + dimension 3 (theme-recall)
//	B3 → dimension 5 (Tier-2 / degraded-floor under re-enabled LLM synthesis)
//
// The ONE thing this stage MAY hard-fail today: if it cannot reach
// graph.query.globalSearch at all (transport broken). A reachable-but-bad answer
// is DATA; an unreachable path is a real failure.
// ─────────────────────────────────────────────────────────────────────────────

// ThematicRecallThreshold is the fraction of a question's theme_terms that a
// GOOD answer should surface. B0 records the actual fraction and whether it
// clears this bar; B2 converts theme-recall to a hard gate at this threshold.
const ThematicRecallThreshold = 0.6

// thematicQuestion is a single global/thematic eval fixture. Every field is
// grounded in testdata/semantic/*.jsonl — see thematicQuestions for the
// per-question provenance. Global search is a "what themes exist across these
// entities" question, so each query is a multi-word NL question (routes through
// the GraphRAG default/semantic strategy, NOT entity_lookup) whose answer must
// SYNTHESIZE across multiple corpus entities.
type thematicQuestion struct {
	// id is a short stable slug for metrics/details keys.
	id string
	// query is the natural-language global question sent to globalSearch.
	query string
	// themeTerms are the required theme vocabulary a coherent thematic answer
	// must surface. theme-recall = |present| / |themeTerms|. All lowercased;
	// matched case-insensitively as substrings of the answer.
	themeTerms []string
	// expectAny are real entity labels/keywords from the corpus — at least one
	// must appear in the answer for it to be GROUNDED (recall side).
	expectAny []string
	// mustNotContain are invented/out-of-domain terms that must NOT appear in
	// the answer (fabrication guard). These are drawn from domains the warehouse
	// corpus never mentions, so a hit is a hallucination, not a valid inference.
	mustNotContain []string
}

// thematicQuestions are the B0 global-question fixtures. The corpus is a
// warehouse/logistics knowledge base (5 files, ~74 entities): documents.jsonl
// (SOPs), maintenance.jsonl (work orders), observations.jsonl (safety
// findings), sensors.jsonl (telemetry) and sensor_docs.jsonl (sensor
// descriptions). The set is deliberately weighted toward the doc/maintenance
// corpus — the program's sharpest finding is that explicit-edge LPA clusters
// docs into document-boundary blobs, so thematic search is WEAKEST exactly
// where these questions probe.
//
// Provenance (why each is grounded), verified against the fixtures:
//   - forklift-maintenance: maint-001 "Forklift Hydraulic System Repair" (FL-042),
//     maint-008 "Forklift Battery Maintenance", doc-ops-001 forklift manual,
//     obs-002/obs-003 forklift safety — forklift is the single densest theme.
//   - cold-chain: sensor-temp-001 "Cold Storage Temperature Monitor",
//     sensor-temp-002 "Freezer Temperature Monitor", sensors.jsonl temp readings
//     in cold-storage-1/freezer-2.
//   - fire-emergency: doc-safety-001, doc-emergency-001 "Emergency Response Plan",
//     maint-005 "Emergency Lighting Test", maint-007 "Fire Suppression System Test".
//   - dock-equipment: maint-003 "Dock Leveler Hydraulic Service", maint-009
//     "Overhead Door Motor Replacement", maint-016 "Loading Dock Bumper
//     Replacement", doc-shipping-001.
//   - conveyor-systems: maint-002 "Conveyor Belt Tension Adjustment", obs-004
//     "Missing Safety Guard on Conveyor", doc-maint-001.
var thematicQuestions = []thematicQuestion{
	{
		id:             "forklift-maintenance",
		query:          "What maintenance and repairs have been performed on forklifts?",
		themeTerms:     []string{"forklift", "hydraulic", "battery", "repair"},
		expectAny:      []string{"forklift", "hydraulic", "fl-042", "battery"},
		mustNotContain: []string{"drone", "aircraft", "helicopter"},
	},
	{
		id:             "cold-chain",
		query:          "How is temperature monitored in cold storage and freezer units?",
		themeTerms:     []string{"temperature", "cold", "freezer", "monitoring", "storage"},
		expectAny:      []string{"temperature", "cold storage", "cold_storage", "freezer"},
		mustNotContain: []string{"drone", "cryptocurrency", "spacecraft"},
	},
	{
		id:             "fire-emergency",
		query:          "What fire safety and emergency response procedures are documented?",
		themeTerms:     []string{"fire", "emergency", "safety", "evacuation"},
		expectAny:      []string{"fire", "emergency", "safety", "evacuation", "sprinkler"},
		mustNotContain: []string{"drone", "aircraft", "vaccine"},
	},
	{
		id:             "dock-equipment",
		query:          "What equipment and repairs are associated with the loading docks?",
		themeTerms:     []string{"dock", "door", "hydraulic", "leveler"},
		expectAny:      []string{"dock", "leveler", "door", "bumper"},
		mustNotContain: []string{"drone", "aircraft", "submarine"},
	},
	{
		id:             "conveyor-systems",
		query:          "What problems affect the conveyor belt systems?",
		themeTerms:     []string{"conveyor", "belt", "tension"},
		expectAny:      []string{"conveyor", "belt"},
		mustNotContain: []string{"drone", "aircraft", "blockchain"},
	},
}

// globalSearchDirectResponse mirrors the fields of graph-query's
// GlobalSearchResponse (processor/graph-query/graphrag.go:128) that B0 grades.
// Driving graph.query.globalSearch DIRECTLY over NATS (not the GraphQL
// searchGraph resolver) is deliberate: the raw wire carries `answer`,
// `degraded` and `degraded_reason` — the thematic-synthesis fields the GraphQL
// bypass shaping drops — which are exactly what dimensions 1/5 grade.
type globalSearchDirectResponse struct {
	Strategy           string   `json:"strategy"`
	EntityIDs          []string `json:"entity_ids"`
	Count              int      `json:"count"`
	Summarized         bool     `json:"summarized"`
	Answer             string   `json:"answer"`
	AnswerModel        string   `json:"answer_model"`
	Degraded           bool     `json:"degraded"`
	DegradedReason     string   `json:"degraded_reason"`
	CommunitySummaries []struct {
		CommunityID string   `json:"community_id"`
		Summary     string   `json:"summary"`
		Keywords    []string `json:"keywords"`
		Level       int      `json:"level"`
	} `json:"community_summaries"`
	EntityDigests []struct {
		ID    string `json:"id"`
		Type  string `json:"type"`
		Label string `json:"label"`
	} `json:"entity_digests"`
	Entities []struct {
		ID string `json:"id"`
	} `json:"entities"`
}

// thematicGrade is the per-question graded outcome (dimensions 1-3), recorded
// verbatim into result.Details for the baseline ledger.
type thematicGrade struct {
	ID          string `json:"id"`
	Query       string `json:"query"`
	Strategy    string `json:"strategy"`
	Count       int    `json:"count"`
	AnswerLen   int    `json:"answer_len"`
	AnswerModel string `json:"answer_model"`
	Degraded    bool   `json:"degraded"`
	DegradedRsn string `json:"degraded_reason"`
	LatencyMs   int64  `json:"latency_ms"`
	AnswerHead  string `json:"answer_head"` // first ~240 chars for eyeballing
	// Dimension 1
	NonDegenerate bool `json:"non_degenerate"`
	// Dimension 2
	Grounded        bool     `json:"grounded"`
	GroundingHits   []string `json:"grounding_hits"`
	FabricationHits []string `json:"fabrication_hits"`
	// Dimension 3
	ThemeTermsPresent []string `json:"theme_terms_present"`
	ThemeTermsMissing []string `json:"theme_terms_missing"`
	ThemeRecall       float64  `json:"theme_recall"`
	ThemeRecallPass   bool     `json:"theme_recall_pass"`
}

// executeValidateThematicAnswerEval is the B0 stage: it drives every fixture
// question through graph.query.globalSearch, grades the synthesized answer on
// five deterministic dimensions, RECORDS the baseline into result.Metrics /
// result.Details, and prints it prominently. It does NOT hard-fail on a bad
// answer — only on an unreachable transport (see the header comment).
func (s *TieredScenario) executeValidateThematicAnswerEval(ctx context.Context, result *Result) error {
	if s.natsClient == nil {
		// Transport prerequisite genuinely absent — this IS the one hard-fail
		// case (we cannot reach graph.query.globalSearch at all).
		return fmt.Errorf("thematic-answer eval requires the NATS validation client, which was not initialized")
	}

	fmt.Println("\n========================================================================")
	fmt.Println("[B0 THEMATIC EVAL] GraphRAG thematic-answer baseline (RECORDER, not a gate)")
	fmt.Println("========================================================================")

	grades := make([]thematicGrade, 0, len(thematicQuestions))
	// firstResponses is kept for the answer-stability dimension (run 1 answers).
	firstResponses := make(map[string]*globalSearchDirectResponse, len(thematicQuestions))

	for _, q := range thematicQuestions {
		resp, latency, err := s.globalSearchDirect(ctx, q.query, 0)
		if err != nil {
			// A transport/handler fault means the thematic path is broken —
			// the ONE hard-fail B0 is allowed. An empty-but-valid answer is NOT
			// this: it decodes fine and is graded as data below.
			return fmt.Errorf("thematic-answer eval could not reach graph.query.globalSearch for %q: %w", q.id, err)
		}
		firstResponses[q.id] = resp
		grades = append(grades, gradeThematicAnswer(q, resp, latency))
	}

	// Aggregate dimensions 1-3 across all questions.
	agg := aggregateThematicGrades(grades)

	// Dimension 4a: partition determinism across a fresh detection cycle.
	partition := s.recordPartitionDeterminism(ctx, result)

	// Dimension 4b: answer stability — ask each question a 2nd time, compare the
	// theme-set present in each answer.
	stability := s.recordAnswerStability(ctx, firstResponses)

	// Dimension 5: tiered graceful-fallback / non-empty degraded floor.
	floor := recordDegradedFloor(grades)

	// ---- write metrics (numbers B1-B3 grade themselves against) ----
	result.Metrics["thematic_questions"] = len(grades)
	result.Metrics["thematic_nonempty_rate"] = agg.nonEmptyRate               // dim 1 (→ B1 gate)
	result.Metrics["thematic_empty_answer_count"] = agg.emptyAnswerCount      // dim 1
	result.Metrics["thematic_grounding_pass_rate"] = agg.groundingPassRate    // dim 2 (→ B2 gate)
	result.Metrics["thematic_fabrication_violations"] = agg.fabricationViols  // dim 2
	result.Metrics["thematic_theme_recall_mean"] = agg.themeRecallMean        // dim 3 (→ B2 gate)
	result.Metrics["thematic_theme_recall_pass_rate"] = agg.themeRecallPassRt // dim 3
	result.Metrics["thematic_partition_match_ratio"] = partition.matchRatio   // dim 4a (→ B1 gate)
	result.Metrics["thematic_answer_stability_rate"] = stability.stableRate   // dim 4b
	result.Metrics["thematic_degraded_count"] = floor.degradedCount           // dim 5 (→ B3 gate)
	result.Metrics["thematic_degraded_floor_violations"] = floor.floorViols   // dim 5

	// ---- write the full baseline ledger into Details ----
	result.Details["thematic_answer_eval"] = map[string]any{
		"threshold_theme_recall": ThematicRecallThreshold,
		"per_question":           grades,
		"aggregate":              agg.detail(),
		"partition_determinism":  partition.detail(),
		"answer_stability":       stability.detail(),
		"degraded_floor":         floor.detail(),
		"note": "B0 baseline (broken-state). B1 gates dims 1+4a, B2 gates dims 2+3, " +
			"B3 gates dim 5. Report-only past its owning increment is drift.",
	}

	printThematicBaseline(grades, agg, partition, stability, floor)
	return nil
}

// globalSearchDirect issues a graph.query.globalSearch request over the raw NATS
// wire and decodes the thematic-synthesis response. RequestClassified (ADR-060)
// so a backend fault surfaces as a classified error instead of decoding an
// "error: <msg>" body as a zero-valued success response.
func (s *TieredScenario) globalSearchDirect(ctx context.Context, query string, level int) (*globalSearchDirectResponse, time.Duration, error) {
	reqBody, err := json.Marshal(map[string]any{
		"query":           query,
		"level":           level,
		"max_communities": 10,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("marshal globalSearch request: %w", err)
	}
	start := time.Now()
	// 60s: classifier → semantic embedding search → community scoring → LLM
	// answer synthesis on local seminstruct models. Matches the known-answer
	// stage's budget; a slower real path would itself be a bug — EXCEPT the
	// qwen3-8b heavy-measurement run, where 8B synthesis legitimately runs tens
	// of seconds. SEMSTREAMS_E2E_GLOBALSEARCH_TIMEOUT raises the transport budget
	// for that run only (unset = 60s default). This is transport patience, NOT a
	// grading change — the five graded dimensions are untouched.
	raw, err := s.natsClient.RequestClassified(ctx, "graph.query.globalSearch", reqBody, globalSearchClientTimeout(60*time.Second))
	latency := time.Since(start)
	if err != nil {
		return nil, latency, fmt.Errorf("graph.query.globalSearch request failed: %w", err)
	}
	var resp globalSearchDirectResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, latency, fmt.Errorf("decode globalSearch response: %w", err)
	}
	return &resp, latency, nil
}

// gradeThematicAnswer computes dimensions 1-3 for one question.
func gradeThematicAnswer(q thematicQuestion, resp *globalSearchDirectResponse, latency time.Duration) thematicGrade {
	answerLower := strings.ToLower(resp.Answer)

	g := thematicGrade{
		ID:          q.id,
		Query:       q.query,
		Strategy:    resp.Strategy,
		Count:       resp.Count,
		AnswerLen:   len(resp.Answer),
		AnswerModel: resp.AnswerModel,
		Degraded:    resp.Degraded,
		DegradedRsn: resp.DegradedReason,
		LatencyMs:   latency.Milliseconds(),
		AnswerHead:  headOf(resp.Answer, 240),
	}

	// Dimension 1: non-degenerate — non-empty answer when entities were retrieved.
	// When Count == 0 the retrieval side found nothing, so an empty answer is not
	// a synthesis defect; NonDegenerate is vacuously true there and recorded so.
	g.NonDegenerate = resp.Count == 0 || strings.TrimSpace(resp.Answer) != ""

	// Dimension 2: grounding recall (expect_any) + fabrication guard (must_not).
	for _, sub := range q.expectAny {
		if strings.Contains(answerLower, sub) {
			g.GroundingHits = append(g.GroundingHits, sub)
		}
	}
	for _, bad := range q.mustNotContain {
		if strings.Contains(answerLower, bad) {
			g.FabricationHits = append(g.FabricationHits, bad)
		}
	}
	g.Grounded = len(g.GroundingHits) > 0 && len(g.FabricationHits) == 0

	// Dimension 3: theme-recall.
	for _, term := range q.themeTerms {
		if strings.Contains(answerLower, strings.ToLower(term)) {
			g.ThemeTermsPresent = append(g.ThemeTermsPresent, term)
		} else {
			g.ThemeTermsMissing = append(g.ThemeTermsMissing, term)
		}
	}
	if len(q.themeTerms) > 0 {
		g.ThemeRecall = float64(len(g.ThemeTermsPresent)) / float64(len(q.themeTerms))
	}
	g.ThemeRecallPass = g.ThemeRecall >= ThematicRecallThreshold

	return g
}

// thematicAggregate holds the cross-question rollups for dimensions 1-3.
type thematicAggregate struct {
	total             int
	withEntities      int // Count > 0
	emptyAnswerCount  int // Count > 0 but empty answer (dim 1 violations)
	nonEmptyRate      float64
	groundingPasses   int
	groundingPassRate float64
	fabricationViols  int
	themeRecallMean   float64
	themeRecallPasses int
	themeRecallPassRt float64
}

func aggregateThematicGrades(grades []thematicGrade) thematicAggregate {
	a := thematicAggregate{total: len(grades)}
	var recallSum float64
	for _, g := range grades {
		if g.Count > 0 {
			a.withEntities++
			// Dimension 1 violation: entities were retrieved but synthesis
			// produced no answer text.
			if g.AnswerLen == 0 {
				a.emptyAnswerCount++
			}
		}
		if g.Grounded {
			a.groundingPasses++
		}
		a.fabricationViols += len(g.FabricationHits)
		recallSum += g.ThemeRecall
		if g.ThemeRecallPass {
			a.themeRecallPasses++
		}
	}
	if a.withEntities > 0 {
		a.nonEmptyRate = float64(a.withEntities-a.emptyAnswerCount) / float64(a.withEntities)
	}
	if a.total > 0 {
		a.groundingPassRate = float64(a.groundingPasses) / float64(a.total)
		a.themeRecallMean = recallSum / float64(a.total)
		a.themeRecallPassRt = float64(a.themeRecallPasses) / float64(a.total)
	}
	return a
}

func (a thematicAggregate) detail() map[string]any {
	return map[string]any{
		"questions":             a.total,
		"with_entities":         a.withEntities,
		"empty_answer_count":    a.emptyAnswerCount,
		"nonempty_rate":         a.nonEmptyRate,
		"grounding_passes":      a.groundingPasses,
		"grounding_pass_rate":   a.groundingPassRate,
		"fabrication_viols":     a.fabricationViols,
		"theme_recall_mean":     a.themeRecallMean,
		"theme_recall_passes":   a.themeRecallPasses,
		"theme_recall_passrate": a.themeRecallPassRt,
	}
}

// ---- Dimension 4a: partition determinism ------------------------------------

// clusteringDetectionCountMetric is the histogram `_count` graph-clustering
// increments once per COMPLETED detection run (processor/graph-clustering/
// metrics.go observeDetectionDuration, called after DetectCommunities persists
// the new partition). Watching it bump is how B0 observes a FRESH detection
// cycle over the unchanged graph without a bespoke trigger — detection runs on
// a 30s timer in configs/semantic.json.
const clusteringDetectionCountMetric = "semstreams_graph_clustering_detection_duration_seconds_count"

type partitionDeterminism struct {
	measured     bool
	reason       string // why unmeasured, when !measured
	setA         int    // distinct level-0 membership hashes, run 1
	setB         int    // distinct level-0 membership hashes, run 2
	matchRatio   float64
	communities0 int
}

// recordPartitionDeterminism reads the level-0 membership-hash multiset, waits
// for one fresh detection cycle to complete over the unchanged graph, reads it
// again, and records the Jaccard match ratio. A DETERMINISTIC partition scores
// 1.0; a churning one scores lower — which is exactly the B0 finding B1 fixes.
// Reading the same cache twice (no detection boundary) would trivially score
// 1.0 and record a FALSE green, so this deliberately straddles a real re-run.
func (s *TieredScenario) recordPartitionDeterminism(ctx context.Context, result *Result) partitionDeterminism {
	_ = result
	// Run 1 partition, captured immediately before baselining the detection
	// counter so it corresponds to the pre-wait detection output.
	setA, err := s.level0MembershipHashes(ctx)
	if err != nil {
		return partitionDeterminism{reason: fmt.Sprintf("read level-0 partition (run 1): %v", err)}
	}
	baseline, err := s.metrics.SumMetricsByName(ctx, clusteringDetectionCountMetric)
	if err != nil {
		// Metric absent → cannot observe a fresh detection deterministically.
		// Record rather than guess (SumMetricsByName errors on name drift, not
		// a silent 0 — gh#615).
		return partitionDeterminism{reason: fmt.Sprintf("detection counter unavailable: %v", err)}
	}

	// Wait for a fresh detection cycle (30s timer + run time). 90s budget brackets
	// the ~4-24s run + up to a full interval of phase.
	waitErr := s.metrics.WaitForMetricChange(ctx, clusteringDetectionCountMetric, baseline, client.WaitOpts{
		Timeout:      90 * time.Second,
		PollInterval: 1 * time.Second,
	})
	if waitErr != nil {
		return partitionDeterminism{reason: fmt.Sprintf("no fresh detection cycle observed: %v", waitErr)}
	}
	// Small settle so the persisted partition is fully visible. observeDetection-
	// Duration bumps AFTER DetectCommunities persists, so the write is already
	// done at the bump; this is belt-and-suspenders against a mid-scan read.
	time.Sleep(1 * time.Second)

	setB, err := s.level0MembershipHashes(ctx)
	if err != nil {
		return partitionDeterminism{reason: fmt.Sprintf("read level-0 partition (run 2): %v", err)}
	}

	ratio := multisetJaccard(setA, setB)
	return partitionDeterminism{
		measured:     true,
		setA:         countKeys(setA),
		setB:         countKeys(setB),
		matchRatio:   ratio,
		communities0: countKeys(setB),
	}
}

func (p partitionDeterminism) detail() map[string]any {
	return map[string]any{
		"measured":               p.measured,
		"reason":                 p.reason,
		"run1_distinct_hashes":   p.setA,
		"run2_distinct_hashes":   p.setB,
		"membership_match_ratio": p.matchRatio,
		"level0_communities":     p.communities0,
	}
}

// level0MembershipHashes returns a multiset (hash → count) of level-0 community
// membership hashes. Each hash is over the sorted member list, so it is
// identity-independent: two runs match iff they partition the SAME entities the
// SAME way, regardless of community-ID churn (which is a separate B1 concern).
func (s *TieredScenario) level0MembershipHashes(ctx context.Context) (map[string]int, error) {
	comms, err := s.natsClient.GetAllCommunities(ctx)
	if err != nil {
		return nil, err
	}
	hashes := make(map[string]int)
	for _, c := range comms {
		if c == nil || c.Level != 0 || len(c.Members) == 0 {
			continue
		}
		members := append([]string(nil), c.Members...)
		sort.Strings(members)
		sum := sha256.Sum256([]byte(strings.Join(members, "\n")))
		hashes[hex.EncodeToString(sum[:])]++
	}
	return hashes, nil
}

// multisetJaccard returns |A ∩ B| / |A ∪ B| over two hash multisets (1.0 when
// the two partitions are identical, 0.0 when disjoint). An empty pair scores
// 1.0 (vacuously identical) — recorded, and the community count in the detail
// tells whether "identical" means "identically empty".
func multisetJaccard(a, b map[string]int) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	var inter, union int
	seen := make(map[string]bool)
	for h, ca := range a {
		seen[h] = true
		cb := b[h]
		inter += min(ca, cb)
		union += max(ca, cb)
	}
	for h, cb := range b {
		if seen[h] {
			continue
		}
		union += cb // ca == 0 here
	}
	if union == 0 {
		return 1.0
	}
	return float64(inter) / float64(union)
}

func countKeys(m map[string]int) int {
	n := 0
	for _, c := range m {
		n += c
	}
	return n
}

// ---- Dimension 4b: answer stability -----------------------------------------

type answerStability struct {
	total       int
	stable      int
	stableRate  float64
	perQuestion map[string]bool
}

// recordAnswerStability asks each question a SECOND time and records whether the
// set of theme_terms present in the answer is identical to run 1. Instability
// here comes from LLM nondeterminism or nondeterministic community
// selection/ordering — a B0 datum, not a fail.
func (s *TieredScenario) recordAnswerStability(ctx context.Context, firstResponses map[string]*globalSearchDirectResponse) answerStability {
	st := answerStability{perQuestion: make(map[string]bool, len(thematicQuestions))}
	for _, q := range thematicQuestions {
		first, ok := firstResponses[q.id]
		if !ok {
			continue
		}
		st.total++
		second, _, err := s.globalSearchDirect(ctx, q.query, 0)
		if err != nil {
			// A transient failure on the 2nd ask is recorded as unstable rather
			// than aborting the recorder — the transport hard-fail already ran
			// on the first pass.
			st.perQuestion[q.id] = false
			continue
		}
		same := themeSetEqual(q, first.Answer, second.Answer)
		st.perQuestion[q.id] = same
		if same {
			st.stable++
		}
	}
	if st.total > 0 {
		st.stableRate = float64(st.stable) / float64(st.total)
	}
	return st
}

func (st answerStability) detail() map[string]any {
	return map[string]any{
		"questions":    st.total,
		"stable":       st.stable,
		"stable_rate":  st.stableRate,
		"per_question": st.perQuestion,
	}
}

// themeSetEqual reports whether the set of theme_terms present is identical
// across two answers.
func themeSetEqual(q thematicQuestion, a, b string) bool {
	al, bl := strings.ToLower(a), strings.ToLower(b)
	for _, term := range q.themeTerms {
		t := strings.ToLower(term)
		if strings.Contains(al, t) != strings.Contains(bl, t) {
			return false
		}
	}
	return true
}

// ---- Dimension 5: tiered graceful fallback / non-empty degraded floor --------

type degradedFloor struct {
	degradedCount int
	floorViols    int // degraded responses that were empty OR ungrounded
	notes         []string
}

// recordDegradedFloor grades the owner tenet — "structural/statistical is a
// correct floor, never fail/empty" — against the degraded responses the level-0
// probes actually produced. For every response flagged Degraded=true, the FLOOR
// requires the answer still be non-empty AND grounded (a real DegradedReason is
// also expected); a degraded-but-empty or degraded-but-ungrounded answer is a
// floor VIOLATION.
//
// SCOPE HONESTY: the community-cache-ABSENT path ("communities not ready") is
// only cleanly observable EARLY in the run, before COMMUNITY_INDEX populates —
// this stage runs late (after community-structure validation), so the cache is
// warm. findCommunitiesForEntities ignores the request level (graphrag.go:1429),
// so a bogus high-level probe does NOT force the empty-community path either. B0
// therefore grades the floor against the REAL degraded responses observed and
// records that the cold path is deferred to a targeted B1 probe. The
// non-degenerate dimension (empty_answer_count) is the complementary "never
// empty" measurement on the WARM path.
func recordDegradedFloor(grades []thematicGrade) degradedFloor {
	f := degradedFloor{}
	for _, g := range grades {
		if !g.Degraded {
			continue
		}
		f.degradedCount++
		empty := g.AnswerLen == 0
		ungrounded := !g.Grounded
		if empty || ungrounded {
			f.floorViols++
			f.notes = append(f.notes, fmt.Sprintf(
				"%s: degraded floor violated (empty=%v grounded=%v reason=%q)",
				g.ID, empty, g.Grounded, g.DegradedRsn))
		}
	}
	if f.degradedCount == 0 {
		f.notes = append(f.notes, "no degraded responses on the warm-cache path this run; "+
			"cache-absent (not-ready) floor deferred to a targeted B1 cold-start probe")
	}
	return f
}

func (f degradedFloor) detail() map[string]any {
	return map[string]any{
		"degraded_count":   f.degradedCount,
		"floor_violations": f.floorViols,
		"notes":            f.notes,
	}
}

// ---- printing ---------------------------------------------------------------

func printThematicBaseline(grades []thematicGrade, agg thematicAggregate, p partitionDeterminism, st answerStability, f degradedFloor) {
	fmt.Println("\n[B0 THEMATIC EVAL] per-question grades:")
	for _, g := range grades {
		fmt.Printf("  - %-22s strategy=%-12s count=%-3d answer_len=%-4d degraded=%-5v recall=%.2f(%s) grounded=%v fab=%v\n",
			g.ID, g.Strategy, g.Count, g.AnswerLen, g.Degraded, g.ThemeRecall,
			recallFrac(g), g.Grounded, len(g.FabricationHits) > 0)
		if g.AnswerLen > 0 {
			fmt.Printf("      answer[:240]=%q\n", g.AnswerHead)
		} else {
			fmt.Printf("      answer=<EMPTY>\n")
		}
		if len(g.ThemeTermsMissing) > 0 {
			fmt.Printf("      theme_terms_missing=%v\n", g.ThemeTermsMissing)
		}
		if len(g.FabricationHits) > 0 {
			fmt.Printf("      FABRICATION_HITS=%v\n", g.FabricationHits)
		}
	}

	fmt.Println("\n[B0 THEMATIC EVAL] five-dimension baseline (RECORDER — no hard-fail):")
	fmt.Printf("  dim1 non-degenerate   : nonempty_rate=%.2f  empty_answer_count=%d/%d (→ B1 gate)\n",
		agg.nonEmptyRate, agg.emptyAnswerCount, agg.withEntities)
	fmt.Printf("  dim2 grounded         : grounding_pass_rate=%.2f  fabrication_violations=%d (→ B2 gate)\n",
		agg.groundingPassRate, agg.fabricationViols)
	fmt.Printf("  dim3 theme-recall     : mean=%.2f  pass_rate(>=%.2f)=%.2f (→ B2 gate)\n",
		agg.themeRecallMean, ThematicRecallThreshold, agg.themeRecallPassRt)
	if p.measured {
		fmt.Printf("  dim4a partition-determ: membership_match_ratio=%.2f (run1=%d run2=%d level0_comms=%d, target 1.0) (→ B1 gate)\n",
			p.matchRatio, p.setA, p.setB, p.communities0)
	} else {
		fmt.Printf("  dim4a partition-determ: UNMEASURED — %s\n", p.reason)
	}
	fmt.Printf("  dim4b answer-stability: stable_rate=%.2f (%d/%d identical theme-set across 2 asks)\n",
		st.stableRate, st.stable, st.total)
	fmt.Printf("  dim5  degraded-floor  : degraded_count=%d  floor_violations=%d (→ B3 gate)\n",
		f.degradedCount, f.floorViols)
	for _, n := range f.notes {
		fmt.Printf("      note: %s\n", n)
	}
	fmt.Println("========================================================================")
}

func recallFrac(g thematicGrade) string {
	return fmt.Sprintf("%d/%d", len(g.ThemeTermsPresent), len(g.ThemeTermsPresent)+len(g.ThemeTermsMissing))
}

// headOf returns the first n runes of s (best-effort, byte-truncated), for
// compact logging of a possibly-long synthesized answer.
func headOf(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n]
}
