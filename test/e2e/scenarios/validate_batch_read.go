// Package scenarios provides E2E test scenarios for SemStreams semantic processing
package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/fusion"
	"github.com/c360studio/semstreams/pkg/fusion/fusionnats"
)

// Graph batch/semantic read-path reconciliation stage (gh#599 / gh#597).
//
// This stage is the e2e:semantic coverage the unit/integration suites cannot
// exercise: the reconciliation contracts shipped in gh#604 (ADR-084) driven over
// the REAL NATS wire against the running Docker stack, plus the gh#597 soak signal
// batch_query_missing_total{reason}. gh#599's named contracts are delivered over
// BOTH surfaces: the RAW graph.query.batch wire (assertion 1) AND the production
// reconciliation client fusionnats.Client.Entities → reconcileHydration (assertion
// 2, which asserts the missing→unhydrated mapping the client owns).
//
// Every deterministic assertion here HARD-FAILS (returns an error, which
// executeStages turns into result.Success=false) — the warn-not-fail e2e pattern is
// exactly what this pre-v1 program exists to retire. The gh#597 counter is
// LOAD-BEARING, not merely reported: the deliberately-absent IDs it requests must
// each increment reason=not_found, so assertBatchMissingVerdict HARD-FAILS unless the
// observed not_found delta is >= that expected-absent count (a LOWER BOUND — an
// undercount means reportBatchMissing stopped incrementing, a silent-stop a presence
// check would miss). The counter is process-global, so an EXCESS is unrelated traffic
// and is recorded, not failed; a gh#597 cross-store gap (a known-present entity
// coming back not_found) is caught deterministically, with entity-ID attribution, by
// the hydration guards (assertion 1/2 + assertAllHydrate).
//
// SCOPE HONESTY — two axes are NOT exercised in this profile and are NOT faked:
//
//   - The gh#604 REORDER-under-cache-miss. The graph-ingest entity cache is a
//     hard-coded 5000-entry / 30s hybrid (processor/graph-ingest/component.go) and
//     the semantic dataset is ~74 entities, so every read here is an all-cache-hit;
//     fetchEntitiesConcurrent already returns all-cache-hits in requested order, so
//     reconcileHydration's reorder is a no-op and breaking it would leave this
//     green. The reorder is covered by its unit fixture (pkg/fusion/fusionnats
//     reconcile_test.go); a LIVE reorder exercise needs a cache-control seam and is
//     deferred to gh#643 (the cache-seam follow-up).
//   - A real #597 cache-RESIDENCY / real-KV-read soak. With that same cache the
//     repeated reads never evict, so the reconciliation regression guard below
//     (runReconciliationRegressionGuard) is a GUARD, not an eviction soak — it
//     re-reads the present + ranked sets and hard-fails on any present entity
//     surfacing in `missing`. A true cache-residency soak also needs the
//     cache-control seam (gh#643).
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

	// expectedAbsent tracks the deliberately-absent IDs this stage requests (one on the
	// raw wire in assertion 1, one through the production client in assertion 2), so the
	// gh#597 verdict can gate the not_found delta as a LOWER BOUND against them.
	expectedAbsent := 0

	// Baseline the counter BEFORE any request in this stage so the whole window
	// (both absent IDs included) is captured.
	before, err := s.scrapeBatchMissingByReason(ctx)
	if err != nil {
		return err
	}

	// Assertion 1: batch reconciliation / unhydrated reporting on the RAW wire.
	if err := s.assertBatchReconciliation(ctx, presentIDs, &expectedAbsent, result); err != nil {
		return err
	}
	// Assertion 2: the PRODUCTION reconciliation client's missing→unhydrated mapping.
	if err := s.assertBatchReconciliationClient(ctx, presentIDs, &expectedAbsent, result); err != nil {
		return err
	}
	// Assertion 3: score/rank observability on the raw semantic wire.
	rankedIDs, err := s.assertSemanticScores(ctx, result)
	if err != nil {
		return err
	}
	// Reconciliation regression guard: repeated reconciled reads of present + ranked.
	if err := s.runReconciliationRegressionGuard(ctx, presentIDs, rankedIDs, result); err != nil {
		return err
	}

	// Assertion 4: gate the gh#597 counter delta and record the datum.
	after, err := s.scrapeBatchMissingByReason(ctx)
	if err != nil {
		return err
	}
	return s.assertBatchMissingVerdict(before, after, expectedAbsent, result)
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

// assertReconciledExactlyOnce is the shared exactly-once guard (Finding 5): every
// requested ID must appear EXACTLY ONCE across the hydrated set ∪ the unhydrated
// set — no drop, no duplicate, no invented ID. It is the antidote to count-only
// accounting that this program exists to retire: a reply [A,A] for requested [A,B]
// fails here even though both lengths look plausible. Callers pass a UNIQUE
// requested slice (present is a sorted KV key scan; ranked IDs are deduped first).
func assertReconciledExactlyOnce(requested, hydratedIDs, unhydratedIDs []string) error {
	requestedSet := make(map[string]bool, len(requested))
	for _, id := range requested {
		requestedSet[id] = true
	}
	seen := make(map[string]int, len(requested))
	for _, id := range hydratedIDs {
		seen[id]++
	}
	for _, id := range unhydratedIDs {
		seen[id]++
	}
	for id, n := range seen {
		if !requestedSet[id] {
			return fmt.Errorf("reply named %q which was never requested (invented ID)", id)
		}
		if n != 1 {
			return fmt.Errorf("requested ID %q appears %d times across hydrated∪unhydrated, want exactly 1 (duplicated)", id, n)
		}
	}
	for id := range requestedSet {
		if seen[id] != 1 {
			return fmt.Errorf("requested ID %q appears %d times across hydrated∪unhydrated, want exactly 1 (dropped)", id, seen[id])
		}
	}
	return nil
}

// batchResponseExactlyOnce runs the shared exactly-once guard over a RAW batch reply.
func batchResponseExactlyOnce(requested []string, resp graph.EntityBatchResponse) error {
	hydrated := make([]string, 0, len(resp.Entities))
	for _, e := range resp.Entities {
		hydrated = append(hydrated, e.ID)
	}
	missing := make([]string, 0, len(resp.Missing))
	for _, m := range resp.Missing {
		missing = append(missing, m.ID)
	}
	return assertReconciledExactlyOnce(requested, hydrated, missing)
}

// hydrationExactlyOnce runs the shared exactly-once guard over a PRODUCTION-client
// fusion.Hydration (Entities ∪ Unhydrated).
func hydrationExactlyOnce(requested []string, hyd fusion.Hydration) error {
	hydrated := make([]string, 0, len(hyd.Entities))
	for _, e := range hyd.Entities {
		hydrated = append(hydrated, e.ID)
	}
	unhydrated := make([]string, 0, len(hyd.Unhydrated))
	for _, u := range hyd.Unhydrated {
		unhydrated = append(unhydrated, u.Handle)
	}
	return assertReconciledExactlyOnce(requested, hydrated, unhydrated)
}

// assertBatchReconciliation is assertion 1: a batch of known-present IDs plus one
// deliberately-absent ID must report {id: absent, reason: not_found} on the RAW
// graph.query.batch wire and account for every requested ID exactly once across
// entities ∪ missing.
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
	// invented ID (Finding 5 shared guard).
	if err := batchResponseExactlyOnce(requested, resp); err != nil {
		return fmt.Errorf("assertion-1: %w", err)
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

// assertBatchReconciliationClient is assertion 2: it drives the PRODUCTION
// reconciliation client (fusionnats.Client.Entities → reconcileHydration) over a
// real graph.query.batch request — known-present IDs plus one deliberately-absent
// ID — and asserts the production half of gh#599's contract that no other stage
// exercises: the missing→unhydrated mapping. The absent ID must surface as an EXACT
// fusion.Unhydrated{Handle: absentID, Reason: not_found} entry (a value in the
// CLOSED fusion.UnhydratedReason set), the present set must all hydrate, and every
// requested ID must be accounted for exactly once across Entities ∪ Unhydrated.
//
// DOWNSCOPE (Finding 1 — the gh#604 REORDER is NOT exercised here). This call
// requests the present sample in REVERSED order and asserts the hydrated entities
// come back in requested order, but that is a deterministic property of
// reconcileHydration's request-order ITERATION, NOT a live exercise of the
// reorder-under-cache-miss fix. The graph-ingest entity cache is a hard-coded
// 5000-entry / 30s hybrid and the semantic dataset is ~74 entities, so every read
// here is an all-cache-hit; fetchEntitiesConcurrent already returns all-cache-hits
// in requested order, so a reconcile that trusted the raw order would pass this
// unchanged. The reorder is covered by its unit fixture (pkg/fusion/fusionnats
// reconcile_test.go); a LIVE reorder exercise needs a cache-control seam and is
// deferred to gh#643 (the cache-seam follow-up). What this call DOES deterministically
// prove is the production client's missing→unhydrated reconciliation.
func (s *TieredScenario) assertBatchReconciliationClient(ctx context.Context, present []string, expectedAbsent *int, result *Result) error {
	fc := fusionnats.New(s.natsClient.Client(), 5*time.Second)
	defer fc.Close()

	sample := present
	if len(sample) > 5 {
		sample = sample[:5]
	}
	// Request the present IDs in REVERSED order (see downscope note) plus one
	// guaranteed-absent ID, so the production client must reconcile a missing entry.
	absentID := fmt.Sprintf("c360.e2e.reconcile.client.absent.%d", time.Now().UnixNano())
	requested := make([]string, 0, len(sample)+1)
	for i := range sample {
		requested = append(requested, sample[len(sample)-1-i])
	}
	presentRequested := append([]string(nil), requested...) // reversed present, before absent
	requested = append(requested, absentID)
	*expectedAbsent++ // absentID legitimately increments reason=not_found

	hyd, err := fc.Entities(ctx, requested)
	if err != nil {
		return fmt.Errorf("assertion-2 fusionnats batch hydration failed: %w", err)
	}

	// The present sample must all hydrate, in requested order (deterministic
	// request-order-iteration property of reconcileHydration; see downscope note —
	// this does NOT prove reorder-under-cache-miss).
	if len(hyd.Entities) != len(sample) {
		return fmt.Errorf("assertion-2: production client hydrated %d entities, want %d present", len(hyd.Entities), len(sample))
	}
	for i, e := range hyd.Entities {
		if e.ID != presentRequested[i] {
			return fmt.Errorf("assertion-2: hydrated position %d is %q, want %q — reconcileHydration did not return present entities in requested order", i, e.ID, presentRequested[i])
		}
	}

	// The absent ID must surface as an EXACT unhydrated entry: its handle + a reason
	// in the CLOSED fusion.UnhydratedReason set, specifically not_found. This is the
	// production-client half of gh#599's unhydrated reporting (Finding 2) — the
	// missing→unhydrated mapping reconcileHydration owns.
	if len(hyd.Unhydrated) != 1 {
		return fmt.Errorf("assertion-2: production client reported %d unhydrated entries, want exactly 1 (the deliberate absent ID): %+v", len(hyd.Unhydrated), hyd.Unhydrated)
	}
	u := hyd.Unhydrated[0]
	if u.Handle != absentID {
		return fmt.Errorf("assertion-2: unhydrated handle is %q, want the deliberate absent %q", u.Handle, absentID)
	}
	if u.Reason != fusion.UnhydratedNotFound {
		return fmt.Errorf("assertion-2: absent %q reported unhydrated reason %q, want %q (closed fusion.UnhydratedReason set)", absentID, u.Reason, fusion.UnhydratedNotFound)
	}

	// Exactly-once accounting across Entities ∪ Unhydrated: no drop, no duplicate, no
	// invented ID — the production-client mirror of assertion 1's raw-wire guard.
	if err := hydrationExactlyOnce(requested, hyd); err != nil {
		return fmt.Errorf("assertion-2: %w", err)
	}

	result.Metrics["batch_client_hydrated"] = len(hyd.Entities)
	result.Metrics["batch_client_unhydrated"] = len(hyd.Unhydrated)
	fmt.Printf("[BATCH RECON] assertion-2 OK: production client hydrated %d present (requested order) + reconciled 1 absent → unhydrated{not_found}\n",
		len(hyd.Entities))
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

// runReconciliationRegressionGuard is a deterministic regression GUARD for the
// gh#604 reconciliation contract — NOT a cache-eviction soak (Finding 4).
//
// With the hard-coded 5000-entry / 30s entity cache over ~74 entities, the repeated
// reads below NEVER evict, so this does NOT exercise the #597 cache-residency /
// real-KV-read axis (that needs the cache-control seam — gh#643).
// What it deterministically does is re-read the full present set (in chunks) and the
// entities the semantic index actually ranked, and HARD-FAIL — via the shared
// exactly-once guard (Finding 5) — on any present entity appearing in `missing`,
// any drop, any duplicate, or any invented ID. Every entity read here is
// KNOWN-PRESENT and ingest is settled, so a present ID in `missing` is the gh#597
// cross-store gap; there is no legitimate serve-under-lag window this late. The
// batch_query_missing_total delta the caller gates is the corroborating datum.
func (s *TieredScenario) runReconciliationRegressionGuard(ctx context.Context, present, rankedIDs []string, result *Result) error {
	const rounds = 2
	for round := 0; round < rounds; round++ {
		if err := s.readPresentInChunks(ctx, present); err != nil {
			return fmt.Errorf("regression guard present-read round %d: %w", round, err)
		}
		// Interleave semantic queries: they warm graph.embedding.query.search and add
		// timing interleave; they do NOT touch the graph-ingest entity cache.
		if _, err := s.assertSemanticScores(ctx, result); err != nil {
			return fmt.Errorf("regression guard semantic query round %d: %w", round, err)
		}
	}
	// The ranked set, re-read after the interleaved reads — the exact gh#597 shape.
	// Dedupe first so the exactly-once guard sees a unique requested set.
	ranked := uniqueStrings(rankedIDs)
	if len(ranked) > 0 {
		if err := s.assertAllHydrate(ctx, ranked); err != nil {
			return fmt.Errorf("regression guard ranked-entity read: %w", err)
		}
	}

	result.Metrics["batch_soak_present_read"] = len(present)
	result.Metrics["batch_soak_ranked_read"] = len(ranked)
	fmt.Printf("[BATCH RECON] reconciliation guard OK: re-read %d present entities x%d rounds + %d ranked entities, all reconciled exactly-once\n",
		len(present), rounds, len(ranked))
	return nil
}

// readPresentInChunks batch-reads the full present set in chunks, asserting each
// chunk reconciles completely and exactly-once.
func (s *TieredScenario) readPresentInChunks(ctx context.Context, present []string) error {
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

// assertAllHydrate batch-reads known-present IDs and hard-fails if the reply is not
// an exactly-once accounting of the request (Finding 5: no drop, no duplicate, no
// invented ID) or if any known-present ID comes back in `missing` (the gh#597
// cross-store gap). Callers pass a UNIQUE id slice.
func (s *TieredScenario) assertAllHydrate(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	resp, err := s.batchQuery(ctx, ids)
	if err != nil {
		return fmt.Errorf("batch query for %d present IDs failed: %w", len(ids), err)
	}
	// Exactly-once accounting first: a [A,A] reply for requested [A,B] fails here even
	// though a length check would pass — the count-only-accounting flake class.
	if err := batchResponseExactlyOnce(ids, resp); err != nil {
		return fmt.Errorf("reconciliation over %d present IDs: %w", len(ids), err)
	}
	// Every requested ID is known-present, so ANY that came back in `missing` is the
	// gh#597 cross-store gap — hard-fail.
	if len(resp.Missing) > 0 {
		missing := make([]string, 0, len(resp.Missing))
		for _, m := range resp.Missing {
			missing = append(missing, fmt.Sprintf("%s(%s)", m.ID, m.Reason))
		}
		return fmt.Errorf("%d known-present entities came back unhydrated (cross-store gap gh#597): %v", len(resp.Missing), missing)
	}
	return nil
}

// uniqueStrings returns ids with duplicates removed, preserving first-seen order.
func uniqueStrings(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
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

// assertBatchMissingVerdict is assertion 4: it makes the gh#597 counter LOAD-BEARING
// (Finding 3) and records the delta datum.
//
// The counter is PROCESS-GLOBAL (a sync.Once CounterVec in graph-ingest), so the
// window delta cannot be perfectly attributed to this stage's own requests. The gate
// is therefore a LOWER BOUND, not an exact match. The deliberately-absent IDs this
// stage requests (expectedAbsent — one raw-wire in assertion 1, one production-client
// in assertion 2) MUST each increment reason=not_found once (handleQueryBatchNATS →
// reportBatchMissing, processor/graph-ingest/query.go; live-registry scrape, no
// scrape-interval lag, request/reply synchronous), so:
//
//	deltaNotFound >= expectedAbsent   (and never < 0)
//
// HARD-FAILS (neither can be produced by process-global noise, which only ADDS
// increments — it can never push the delta below expectedAbsent):
//   - deltaNotFound < 0              → the counter RESET (a monotonic counter went backwards).
//   - deltaNotFound < expectedAbsent → reportBatchMissing STOPPED incrementing reason=not_found
//     for a known-absent ID. A presence check (SumMetricInSubsystem above) would still pass —
//     this is exactly the silent-stop Finding 3 closes: the counter is no longer load-bearing.
//
// The gh#597 cross-store gap (a not_found on a KNOWN-PRESENT entity) is detected
// DETERMINISTICALLY and with entity-ID attribution by the assertion-1/2 hydration
// checks and assertAllHydrate — NOT by an upper bound on this process-global counter,
// which would false-positive on any concurrent batch-miss. An excess is therefore
// RECORDED to corroborate those gates, not hard-failed.
func (s *TieredScenario) assertBatchMissingVerdict(before, after map[string]float64, expectedAbsent int, result *Result) error {
	deltaNotFound := after[string(graph.MissingNotFound)] - before[string(graph.MissingNotFound)]
	deltaError := after[string(graph.MissingError)] - before[string(graph.MissingError)]

	// Record the datums regardless of verdict.
	result.Metrics["batch_missing_not_found_delta"] = deltaNotFound
	result.Metrics["batch_missing_error_delta"] = deltaError
	result.Metrics["batch_missing_expected_absent"] = expectedAbsent

	if deltaNotFound < 0 {
		result.Metrics["gh597_verdict"] = "counter_reset"
		return fmt.Errorf("gh#597 counter reset: reason=not_found delta is %.0f (< 0) — a monotonic counter went backwards", deltaNotFound)
	}
	if deltaNotFound < float64(expectedAbsent) {
		result.Metrics["gh597_verdict"] = "counter_undercount"
		return fmt.Errorf("gh#597 counter NOT load-bearing: reason=not_found delta %.0f < %d deliberately-absent IDs requested — reportBatchMissing stopped incrementing (a presence check would still pass)",
			deltaNotFound, expectedAbsent)
	}
	// deltaNotFound >= expectedAbsent: the counter is load-bearing — every known-absent
	// ID incremented it. An EXCESS over expectedAbsent is process-global counter noise
	// (a concurrent batch-miss elsewhere in graph-ingest), RECORDED as a corroborating
	// observation — never a hard-fail. A real gh#597 present-entity gap is caught
	// deterministically, with entity-ID attribution, by the assertion-1/2 hydration
	// checks and assertAllHydrate; it does not rely on this process-global upper bound.
	excess := deltaNotFound - float64(expectedAbsent)
	verdict := "counter_load_bearing_ok"
	if excess > 0 {
		verdict = "counter_load_bearing_ok_excess_observed"
	}
	result.Metrics["gh597_verdict"] = verdict
	result.Metrics["batch_missing_present_gap_excess"] = excess
	result.Details["gh597_soak"] = map[string]any{
		"before":          before,
		"after":           after,
		"not_found_delta": deltaNotFound,
		"error_delta":     deltaError,
		"expected_absent": expectedAbsent,
		"excess":          excess,
		"verdict":         verdict,
		"interpretation": "This stage validates the gh#604 reconciliation contract over BOTH the raw " +
			"graph.query.batch wire and the production reconcileHydration client, and makes " +
			"batch_query_missing_total LOAD-BEARING via a LOWER BOUND: the deliberately-absent IDs this " +
			"stage requests must each increment reason=not_found, so deltaNotFound must be >= expected_absent " +
			"(and never < 0) — an undercount is a silent-stop of the counter (Finding 3). The counter is " +
			"process-global, so an EXCESS over expected_absent is unrelated traffic and is recorded, not " +
			"failed; the gh#597 cross-store gap (a not_found on a known-present entity) is caught " +
			"deterministically with entity-ID attribution by the hydration guards. This stage does NOT " +
			"exercise the gh#604 reorder-under-cache-miss or a real #597 cache-residency soak: the " +
			"hard-coded 5000/30s entity cache over ~74 entities never evicts, so those axes need a " +
			"cache-control seam (gh#643) and are honestly deferred, not faked.",
	}

	fmt.Printf("[BATCH RECON] assertion-4 OK: gh#597 counter load-bearing — reason=not_found delta=%.0f >= expected-absent=%d (excess=%.0f), error delta=%.0f\n",
		deltaNotFound, expectedAbsent, excess, deltaError)
	return nil
}
