// Package scenarios provides E2E test scenarios for SemStreams semantic processing
package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/fusion/fusionnats"
)

// Graph batch/semantic read-path reconciliation stage (gh#599 / gh#597).
//
// This stage is the e2e:semantic coverage the unit/integration suites cannot
// exercise: the reconciliation contracts shipped in gh#604 (ADR-084) driven over
// the REAL NATS wire against the running Docker stack, plus the gh#597 soak signal
// batch_query_missing_total{reason}.
//
// Every deterministic assertion here HARD-FAILS (returns an error, which
// executeStages turns into result.Success=false) — the warn-not-fail e2e pattern
// is exactly what this pre-v1 program exists to retire. Only the gh#597 counter
// DELTA is reported rather than gated, because it is a soak datum; the underlying
// "a known-present entity came back not_found" defect is still hard-failed
// deterministically per response, so the report and the gate never disagree.
//
// WHERE THIS STOPS — re-homed to gh#391, NOT attempted here. The fusion.Fuse
// engine envelope (ADR-084's top health-gate reversal: a healthy index under write
// SERVES instead of returning empty; the fusion Response staleness_ms; the
// per-node integer rank/score) is UNREACHABLE in e2e:semantic because no component
// in configs/semantic.json calls fusion.Fuse — the profile runs graph-query +
// graph-embedding but wires no fusion/research-graph route. That coverage belongs
// to gh#391 (research-graph→fusion). This stage asserts only the surfaces the
// semantic stack actually exposes on the wire: graph.query.batch (reconciled
// hydration + missing reporting + the counter) and graph.query.semantic (scored,
// ranked hits).

// batchMissingMetricName is the fully qualified Prometheus counter graph-ingest
// increments once per requested entity ID a batch query could not hydrate,
// labelled by the closed graph.MissingReason set. It is registered at graph-ingest
// construction (processor/graph-ingest/component.go getBatchMissingMetric) and
// pre-seeds both handler-emitted reason series, so they are scrapeable at zero
// before any batch query runs.
const batchMissingMetricName = "semstreams_graph_ingest_batch_query_missing_total"

// batchMissingSubsystem is a prefix of batchMissingMetricName — its presence in a
// scrape proves graph-ingest is deployed and reachable (SumMetricInSubsystem).
const batchMissingSubsystem = "semstreams_graph_ingest"

// batchMissingReasons is the closed set of HANDLER-emitted reasons. `unknown` is
// synthesized client-side (fusion.reconcileHydration) and never emitted here, so
// it is not scraped.
var batchMissingReasons = []string{
	string(graph.MissingNotFound),
	string(graph.MissingError),
}

// executeValidateBatchReadReconciliation runs assertions 1-4 (gh#599) over the
// production wire against the running semantic stack.
func (s *TieredScenario) executeValidateBatchReadReconciliation(ctx context.Context, result *Result) error {
	if s.natsClient == nil {
		return fmt.Errorf("batch-read reconciliation requires the NATS validation client, which was not initialized")
	}

	fmt.Println("[BATCH RECON] Validating graph batch/semantic read-path reconciliation (gh#604) + gh#597 soak signal...")

	// Confirm the gh#597 counter is scrapeable before relying on it. SumMetricInSubsystem
	// separates "graph-ingest not deployed" from "metric renamed" (gh#615): both would
	// otherwise read as a silent 0 and let the soak pass measuring nothing.
	presence, err := s.metrics.SumMetricInSubsystem(ctx, batchMissingSubsystem, batchMissingMetricName)
	if err != nil {
		return fmt.Errorf("gh#597 counter unverifiable: %w", err)
	}
	if !presence.SubsystemPresent {
		return fmt.Errorf("gh#597 counter unverifiable: %s subsystem not scraped — graph-ingest metrics missing", batchMissingSubsystem)
	}

	// Deterministic present IDs from the live ENTITY_STATES key scan. GetAllEntityIDs
	// reads the SAME bucket handleQueryBatchNATS reads, so a key it returns is gettable
	// now (static testdata, no concurrent delete) — this is the contiguous-readiness
	// input, not a bare count or a fixed sleep.
	presentIDs, err := s.presentSourceEntityIDs(ctx)
	if err != nil {
		return err
	}
	const minPresent = 4
	if len(presentIDs) < minPresent {
		return fmt.Errorf("batch-read reconciliation needs at least %d present source entities, ENTITY_STATES has %d", minPresent, len(presentIDs))
	}

	// expectedAbsent tracks the deliberately-absent IDs this stage requests, so the
	// gh#597 verdict can ACCOUNT for their legitimate not_found increments and treat
	// only the excess as a cross-store gap on a known-present entity.
	expectedAbsent := 0

	// Baseline the counter BEFORE any request in this stage so the whole window
	// (assertion 1's absent ID included) is captured.
	before, err := s.scrapeBatchMissingByReason(ctx)
	if err != nil {
		return err
	}

	// Assertion 1: batch reconciliation / unhydrated reporting.
	if err := s.assertBatchReconciliation(ctx, presentIDs, &expectedAbsent, result); err != nil {
		return err
	}
	// Assertion 2: requested-order preservation (via the production fusion client).
	if err := s.assertBatchRequestOrder(ctx, presentIDs, result); err != nil {
		return err
	}
	// Assertion 3: score/rank observability on the raw semantic wire.
	rankedIDs, err := s.assertSemanticScores(ctx, result)
	if err != nil {
		return err
	}
	// gh#597 soak phase: churn the cache, then read entities the index actually ranks.
	if err := s.runBatchMissingSoak(ctx, presentIDs, rankedIDs, result); err != nil {
		return err
	}

	// Assertion 4: report per-reason deltas + the gh#597 verdict.
	after, err := s.scrapeBatchMissingByReason(ctx)
	if err != nil {
		return err
	}
	s.recordBatchMissingVerdict(before, after, expectedAbsent, result)
	return nil
}

// presentSourceEntityIDs returns non-container entity IDs from ENTITY_STATES,
// sorted for deterministic selection.
func (s *TieredScenario) presentSourceEntityIDs(ctx context.Context) ([]string, error) {
	allIDs, err := s.natsClient.GetAllEntityIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list entity IDs for batch reconciliation: %w", err)
	}
	source := make([]string, 0, len(allIDs))
	for _, id := range allIDs {
		if !isContainerEntity(id) {
			source = append(source, id)
		}
	}
	sort.Strings(source)
	return source, nil
}

// batchQuery issues a batch entity query over the public production subject and
// decodes the reconciled reply.
//
// graph.query.batch is the consumer-facing surface (semconnect / CS API gateway).
// graph-query forwards it byte-unchanged to graph-ingest's handleQueryBatchNATS —
// the SAME wire path that increments batch_query_missing_total — so the missing
// list and the counter are one path. RequestClassified (ADR-060) so a backend
// fault surfaces as a classified error instead of a zero-valued success body.
func (s *TieredScenario) batchQuery(ctx context.Context, ids []string) (graph.EntityBatchResponse, error) {
	reqBody, err := json.Marshal(map[string]any{"ids": ids})
	if err != nil {
		return graph.EntityBatchResponse{}, fmt.Errorf("marshal batch request: %w", err)
	}
	raw, err := s.natsClient.RequestClassified(ctx, "graph.query.batch", reqBody, 10*time.Second)
	if err != nil {
		return graph.EntityBatchResponse{}, fmt.Errorf("graph.query.batch request failed: %w", err)
	}
	var resp graph.EntityBatchResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return graph.EntityBatchResponse{}, fmt.Errorf("decode batch response: %w", err)
	}
	return resp, nil
}

// assertBatchReconciliation is assertion 1: a batch of known-present IDs plus one
// deliberately-absent ID must report {id: absent, reason: not_found} and account
// for every requested ID exactly once across entities ∪ missing.
func (s *TieredScenario) assertBatchReconciliation(ctx context.Context, present []string, expectedAbsent *int, result *Result) error {
	sample := present
	if len(sample) > 5 {
		sample = sample[:5]
	}
	// A syntactically valid 6-part ID that is guaranteed absent from ENTITY_STATES.
	absentID := fmt.Sprintf("c360.e2e.reconcile.batch.absent.%d", time.Now().UnixNano())
	requested := make([]string, 0, len(sample)+1)
	requested = append(requested, sample...)
	requested = append(requested, absentID)
	*expectedAbsent++ // absentID legitimately increments reason=not_found

	resp, err := s.batchQuery(ctx, requested)
	if err != nil {
		return fmt.Errorf("assertion-1 batch query failed: %w", err)
	}

	// The absent ID must be reported {id, reason: not_found}.
	absentSeen := false
	var absentReason graph.MissingReason
	for _, m := range resp.Missing {
		if m.ID == absentID {
			absentSeen = true
			absentReason = m.Reason
		}
	}
	if !absentSeen {
		return fmt.Errorf("assertion-1: deliberately-absent %q was not reported in the batch reply's missing list (reconciliation dropped it)", absentID)
	}
	if absentReason != graph.MissingNotFound {
		return fmt.Errorf("assertion-1: absent %q reported with reason %q, want %q", absentID, absentReason, graph.MissingNotFound)
	}

	// Exactly-once accounting across entities ∪ missing: no drop, no duplicate, no
	// invented ID.
	requestedSet := make(map[string]bool, len(requested))
	for _, id := range requested {
		requestedSet[id] = true
	}
	seen := make(map[string]int, len(requested))
	for _, e := range resp.Entities {
		seen[e.ID]++
	}
	for _, m := range resp.Missing {
		seen[m.ID]++
	}
	for id, n := range seen {
		if !requestedSet[id] {
			return fmt.Errorf("assertion-1: batch reply named %q which was never requested (invented ID)", id)
		}
		if n != 1 {
			return fmt.Errorf("assertion-1: requested ID %q appears %d times across entities∪missing, want exactly 1 (duplicated)", id, n)
		}
	}
	for _, id := range requested {
		if seen[id] != 1 {
			return fmt.Errorf("assertion-1: requested ID %q appears %d times across entities∪missing, want exactly 1 (dropped)", id, seen[id])
		}
	}

	// Every present-sample ID must hydrate (belt-and-braces on the reconciliation).
	hydrated := make(map[string]bool, len(resp.Entities))
	for _, e := range resp.Entities {
		hydrated[e.ID] = true
	}
	for _, id := range sample {
		if !hydrated[id] {
			return fmt.Errorf("assertion-1: known-present %q did not hydrate (reported missing) — cross-store gap", id)
		}
	}

	result.Metrics["batch_recon_requested"] = len(requested)
	result.Metrics["batch_recon_hydrated"] = len(resp.Entities)
	result.Metrics["batch_recon_missing"] = len(resp.Missing)
	fmt.Printf("[BATCH RECON] assertion-1 OK: %d requested (%d present + 1 absent), %d hydrated, %d missing (absent reported not_found)\n",
		len(requested), len(sample), len(resp.Entities), len(resp.Missing))
	return nil
}

// assertBatchRequestOrder is assertion 2: the hydrated entities come back in
// REQUESTED order.
//
// Order preservation is the gh#604 fix, and it lives in the production fusion
// retrieval client (fusionnats.Client.Entities → reconcileHydration), NOT on the
// raw batch wire: fetchEntitiesConcurrent returns cache-hits-then-misses order by
// contract, and only that layer — where request order is still known — restores
// it. So this assertion drives the PRODUCTION fusionnats client (a real
// graph.query.batch request over the same NATS connection) and asserts it returns
// request order. That is the correct layer, and it is still the production wire.
func (s *TieredScenario) assertBatchRequestOrder(ctx context.Context, present []string, result *Result) error {
	fc := fusionnats.New(s.natsClient.Client(), 5*time.Second)
	defer fc.Close()

	sample := present
	if len(sample) > 5 {
		sample = sample[:5]
	}
	// Request in REVERSED sorted order so the requested order differs from the
	// handler's natural (sorted / cache-residency) order — a failure to reorder to
	// request order would be visible rather than coincidentally masked.
	requested := make([]string, len(sample))
	for i := range sample {
		requested[i] = sample[len(sample)-1-i]
	}

	hyd, err := fc.Entities(ctx, requested)
	if err != nil {
		return fmt.Errorf("assertion-2 fusionnats batch hydration failed: %w", err)
	}
	if len(hyd.Unhydrated) != 0 {
		return fmt.Errorf("assertion-2: %d known-present IDs did not hydrate: %+v", len(hyd.Unhydrated), hyd.Unhydrated)
	}
	if len(hyd.Entities) != len(requested) {
		return fmt.Errorf("assertion-2: hydrated %d entities, requested %d", len(hyd.Entities), len(requested))
	}
	for i, e := range hyd.Entities {
		if e.ID != requested[i] {
			return fmt.Errorf("assertion-2: position %d is %q, want %q — request order not preserved (gh#604 regression)", i, e.ID, requested[i])
		}
	}

	result.Metrics["batch_order_entities"] = len(hyd.Entities)
	fmt.Printf("[BATCH RECON] assertion-2 OK: %d entities returned in exact requested order (gh#604)\n", len(hyd.Entities))
	return nil
}

// assertSemanticScores is assertion 3: graph.query.semantic returns scored,
// ranked hits. Returns the ranked entity IDs for the gh#597 soak phase.
//
// graph.query.semantic → graph-query passthrough → graph.embedding.query.search.
// The wire returns {results:[{entity_id, similarity}], ...} sorted by similarity
// descending, so RANK == array position. include_scores is a fusion-ENGINE option
// (pkg/fusion/engine_lens.go), NOT a raw-wire param — the raw wire ALWAYS carries
// per-result similarity, so it is sent here only to document intent and is ignored
// by the unknown-field-tolerant SearchRequest decoder. The explicit per-node
// INTEGER `rank` field is a fusion.Node/Response concept, unreachable in this stack
// — folded into the gh#391 re-homing; the ranked ORDER (position) and the
// similarity score ARE on this wire and are asserted here.
func (s *TieredScenario) assertSemanticScores(ctx context.Context, result *Result) ([]string, error) {
	// A query the known-answer suite already proves returns matches at score >= 0.3.
	const query = "cold storage temperature monitoring refrigeration"
	reqBody, err := json.Marshal(map[string]any{"query": query, "limit": 10, "include_scores": true})
	if err != nil {
		return nil, fmt.Errorf("marshal semantic request: %w", err)
	}
	raw, err := s.natsClient.RequestClassified(ctx, "graph.query.semantic", reqBody, 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("assertion-3 semantic query failed: %w", err)
	}
	var resp struct {
		Results []struct {
			EntityID   string  `json:"entity_id"`
			Similarity float64 `json:"similarity"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("assertion-3 decode semantic response: %w", err)
	}
	if len(resp.Results) == 0 {
		return nil, fmt.Errorf("assertion-3: graph.query.semantic returned 0 scored hits for %q — the semantic index served no ranked results", query)
	}
	if resp.Results[0].Similarity <= 0 {
		return nil, fmt.Errorf("assertion-3: top semantic hit %q has non-positive similarity %.4f — score not populated",
			resp.Results[0].EntityID, resp.Results[0].Similarity)
	}

	rankedIDs := make([]string, 0, len(resp.Results))
	prev := resp.Results[0].Similarity
	for i, r := range resp.Results {
		if r.EntityID == "" {
			return nil, fmt.Errorf("assertion-3: semantic hit at rank %d has empty entity_id", i)
		}
		if r.Similarity > prev {
			return nil, fmt.Errorf("assertion-3: semantic results not ranked — rank %d similarity %.4f exceeds previous %.4f (rank==position violated)",
				i, r.Similarity, prev)
		}
		prev = r.Similarity
		rankedIDs = append(rankedIDs, r.EntityID)
	}

	result.Metrics["semantic_scored_hits"] = len(resp.Results)
	result.Metrics["semantic_top_similarity"] = resp.Results[0].Similarity
	result.Details["semantic_wire_query"] = query
	fmt.Printf("[BATCH RECON] assertion-3 OK: %d ranked semantic hits over graph.query.semantic, top similarity=%.4f\n",
		len(resp.Results), resp.Results[0].Similarity)
	return rankedIDs, nil
}

// runBatchMissingSoak is the gh#597 soak phase. The diversity axis gh#597 tracks
// comes SOLELY from chunked batch reads of the full present set (churnPresentReads),
// which force real ENTITY_STATES re-reads through the graph-ingest read-through
// cache. The interleaved semantic queries warm the embedding search path
// (graph.embedding.query.search) and add timing interleave — they do NOT touch the
// graph-ingest entity cache. After the churn it batch-reads the entities the
// semantic index actually ranked. Every entity read here is KNOWN-PRESENT, so any
// that comes back in `missing` is the cross-store gap gh#597 describes — hard-fail
// (deterministic: static testdata means no legitimate serve-under-lag window remains
// this late). The counter DELTA the caller reports is the soak datum that
// corroborates it.
func (s *TieredScenario) runBatchMissingSoak(ctx context.Context, present, rankedIDs []string, result *Result) error {
	const rounds = 2
	for round := 0; round < rounds; round++ {
		if err := s.churnPresentReads(ctx, present); err != nil {
			return fmt.Errorf("soak churn round %d: %w", round, err)
		}
		if _, err := s.assertSemanticScores(ctx, result); err != nil {
			return fmt.Errorf("soak semantic query round %d: %w", round, err)
		}
	}
	// The ranked set, read right after cache churn — the exact gh#597 shape.
	if len(rankedIDs) > 0 {
		if err := s.assertAllHydrate(ctx, rankedIDs); err != nil {
			return fmt.Errorf("soak ranked-entity read: %w", err)
		}
	}

	result.Metrics["batch_soak_present_read"] = len(present)
	result.Metrics["batch_soak_ranked_read"] = len(rankedIDs)
	fmt.Printf("[BATCH RECON] soak OK: churned %d present entities x%d rounds, read %d ranked entities, all hydrated\n",
		len(present), rounds, len(rankedIDs))
	return nil
}

// churnPresentReads batch-reads the full present set in chunks, asserting each
// chunk hydrates completely.
func (s *TieredScenario) churnPresentReads(ctx context.Context, present []string) error {
	const chunk = 20
	for i := 0; i < len(present); i += chunk {
		end := i + chunk
		if end > len(present) {
			end = len(present)
		}
		if err := s.assertAllHydrate(ctx, present[i:end]); err != nil {
			return err
		}
	}
	return nil
}

// assertAllHydrate batch-reads known-present IDs and hard-fails if any is reported
// missing (the gh#597 cross-store gap) or the reconciliation under-reports.
func (s *TieredScenario) assertAllHydrate(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	resp, err := s.batchQuery(ctx, ids)
	if err != nil {
		return fmt.Errorf("batch query for %d present IDs failed: %w", len(ids), err)
	}
	if len(resp.Missing) > 0 {
		missing := make([]string, 0, len(resp.Missing))
		for _, m := range resp.Missing {
			missing = append(missing, fmt.Sprintf("%s(%s)", m.ID, m.Reason))
		}
		return fmt.Errorf("%d known-present entities came back unhydrated (cross-store gap gh#597): %v", len(resp.Missing), missing)
	}
	if len(resp.Entities) != len(ids) {
		return fmt.Errorf("batch read of %d present IDs returned %d entities with no missing report — under-reported reconciliation",
			len(ids), len(resp.Entities))
	}
	return nil
}

// scrapeBatchMissingByReason reads batch_query_missing_total for each
// handler-emitted reason label.
func (s *TieredScenario) scrapeBatchMissingByReason(ctx context.Context) (map[string]float64, error) {
	out := make(map[string]float64, len(batchMissingReasons))
	for _, reason := range batchMissingReasons {
		metrics, err := s.metrics.GetMetricByLabels(ctx, batchMissingMetricName, map[string]string{"reason": reason})
		if err != nil {
			return nil, fmt.Errorf("scrape %s{reason=%q}: %w", batchMissingMetricName, reason, err)
		}
		var sum float64
		for _, m := range metrics {
			sum += m.Value
		}
		out[reason] = sum
	}
	return out, nil
}

// recordBatchMissingVerdict is assertion 4: report the per-reason counter deltas
// and the gh#597 verdict.
//
// The not_found delta is attributed: expectedAbsent increments are the
// deliberately-absent IDs this stage requested; anything ABOVE that baseline is a
// not_found on a KNOWN-PRESENT entity — the gh#597 cross-store gap. The
// deterministic assertAllHydrate checks above already hard-fail on that exact
// condition, so this verdict corroborates the gate rather than replacing it (the
// counter is process-global and a soak signal, not a naive pass/fail).
func (s *TieredScenario) recordBatchMissingVerdict(before, after map[string]float64, expectedAbsent int, result *Result) {
	deltaNotFound := after[string(graph.MissingNotFound)] - before[string(graph.MissingNotFound)]
	deltaError := after[string(graph.MissingError)] - before[string(graph.MissingError)]
	excess := deltaNotFound - float64(expectedAbsent)

	// This stage runs after ingest settles, so the active-write cross-store race
	// gh#597 describes CANNOT fire in this profile — a clean delta is NOT proof the
	// race is closed. It only establishes that no known-present entity dropped under
	// cache churn (the gh#604 reconciliation contract held). The counter is a
	// process-global soak signal, so the verdict NAMES THE OBSERVATION, it does not
	// claim resolution. Only a real write-concurrent workload can exercise the race.
	verdict := "no_present_gap_observed"
	if excess > 0 {
		verdict = "present_entity_gap" // not_found on a known-present entity — the gh#597 cross-store gap
	}

	result.Metrics["batch_missing_not_found_delta"] = deltaNotFound
	result.Metrics["batch_missing_error_delta"] = deltaError
	result.Metrics["batch_missing_expected_absent"] = expectedAbsent
	result.Metrics["batch_missing_present_gap_count"] = excess
	result.Metrics["gh597_verdict"] = verdict
	result.Details["gh597_soak"] = map[string]any{
		"before":          before,
		"after":           after,
		"not_found_delta": deltaNotFound,
		"error_delta":     deltaError,
		"expected_absent": expectedAbsent,
		"present_gap":     excess,
		"verdict":         verdict,
		"interpretation": "This stage validates the gh#604 reconciliation contract, installs a deterministic " +
			"regression guard (a known-present entity in `missing` hard-fails), and wires batch_query_missing_total " +
			"as the workload soak signal. It does NOT exercise the gh#597 active-write cross-store race — ingest is " +
			"settled by the time it runs — so a clean delta (== expected-absent baseline) is NOT proof gh#597 is " +
			"closed; it means no present-entity gap was observed under cache churn. A delta ABOVE baseline is a " +
			"not_found on a known-present entity: the cross-store gap, which only a real write-concurrent workload " +
			"can surface here.",
	}

	fmt.Printf("[BATCH RECON] gh#597 soak: not_found delta=%.0f (expected-absent baseline=%d, present-entity gap=%.0f), error delta=%.0f → verdict=%s\n",
		deltaNotFound, expectedAbsent, excess, deltaError, verdict)
}
