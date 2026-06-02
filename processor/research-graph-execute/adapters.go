package researchexecute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
)

// Graph-query NATS-direct subjects this component requests against.
// Centralised here so a subject rename in graph-query (rare; subject
// names are part of the public contract) updates exactly one site.
const (
	subjectGraphQueryBatch         = "graph.query.batch"
	subjectGraphQueryRelationships = "graph.query.relationships"
	subjectGraphQueryTemporal      = "graph.query.temporal"
	subjectGraphQuerySearchGraph   = "graph.query.searchGraph"
)

// graphQueryAdapter wraps natsclient.Client into the GraphQueryClient
// interface. Production wires this adapter via Start; tests inject a
// fake GraphQueryClient directly into the Component and skip this
// path entirely.
//
// gh#93 contract: all wire calls go through RequestClassified so
// handler-side errors surface as *errs.ClassifiedError rather than
// silent legacy-text-body corruption. Per
// [[feedback_silent_handler_error_payload_audit]] this is the right
// default for every new natsclient.Request caller.
type graphQueryAdapter struct {
	client  *natsclient.Client
	timeout time.Duration
}

func newGraphQueryAdapter(client *natsclient.Client, timeout time.Duration) *graphQueryAdapter {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &graphQueryAdapter{client: client, timeout: timeout}
}

// EntityState implements GraphQueryClient via graph.query.batch
// (passthrough to graph-ingest's handleQueryBatchNATS). Returns one
// Evidence per requested entity ID that resolves; missing IDs are
// silently omitted by the upstream handler. Verified shapes:
// request `{ids: [...]}`, response `{entities: [<EntityState>...]}`
// where each entity uses the `id` field (graph/types.go EntityState).
func (a *graphQueryAdapter) EntityState(ctx context.Context, args EntityStateArgs, tier, source string, limit int) ([]research.Evidence, error) {
	if a == nil || a.client == nil {
		return nil, errors.New("nats client not configured")
	}
	if len(args.EntityIDs) == 0 {
		return nil, nil
	}
	req := struct {
		IDs []string `json:"ids"`
	}{IDs: args.EntityIDs}
	respData, err := a.request(ctx, subjectGraphQueryBatch, req)
	if err != nil {
		return nil, fmt.Errorf("entity_state via %s: %w", subjectGraphQueryBatch, err)
	}
	// graph.query.batch returns {entities: [<EntityState>]} where
	// EntityState uses `id` (not `entity_id`). EntityState carries
	// Triples but PR 4 only needs the ID for downstream dedup +
	// budget; snippet extraction from triples is Phase 2 work.
	var resp struct {
		Entities []struct {
			ID string `json:"id"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("decode entity_state response: %w", err)
	}
	out := make([]research.Evidence, 0, len(resp.Entities))
	for i, e := range resp.Entities {
		if limit > 0 && i >= limit {
			break
		}
		if strings.TrimSpace(e.ID) == "" {
			continue
		}
		out = append(out, research.Evidence{
			EntityID: e.ID,
			Tier:     tier,
			Source:   source,
		})
	}
	return out, nil
}

// PredicateWalk implements GraphQueryClient via
// graph.query.relationships. Per-seed call (relationships handler
// is single-entity per request); errgroup at the orchestrator
// parallelises across seeds. Verified shapes: request
// `{entity_id, direction}` (processor/graph-query/query.go),
// response is a bare JSON array `[{from_entity_id, to_entity_id,
// edge_type}]` (NOT wrapped). Phase 1 walks outgoing edges only —
// incoming is a Phase 2 extension; widening here is one extra
// request per seed and doubles fan-out cost without a clear
// quality win in trial runs.
//
// MaxHops > 1 is accepted but ignored (the relationships handler
// is single-hop); Phase 2 either extends the handler or composes
// multi-hop via repeated walks on the orchestrator side.
// args.Predicates is accepted but the handler doesn't currently
// filter — kept on the SubQuery type for forward-compat with a
// Phase 2 handler extension.
func (a *graphQueryAdapter) PredicateWalk(ctx context.Context, args PredicateWalkArgs, tier, source string, limit int) ([]research.Evidence, error) {
	if a == nil || a.client == nil {
		return nil, errors.New("nats client not configured")
	}
	if len(args.Seeds) == 0 {
		return nil, nil
	}
	var allEvidence []research.Evidence
	for _, seed := range args.Seeds {
		if strings.TrimSpace(seed) == "" {
			continue
		}
		req := struct {
			EntityID  string `json:"entity_id"`
			Direction string `json:"direction"`
		}{EntityID: seed, Direction: "outgoing"}
		respData, err := a.request(ctx, subjectGraphQueryRelationships, req)
		if err != nil {
			return nil, fmt.Errorf("predicate_walk seed=%s via %s: %w", seed, subjectGraphQueryRelationships, err)
		}
		// graph.query.relationships returns a BARE array; no envelope.
		var resp []struct {
			FromEntityID string `json:"from_entity_id"`
			ToEntityID   string `json:"to_entity_id"`
			EdgeType     string `json:"edge_type"`
		}
		if err := json.Unmarshal(respData, &resp); err != nil {
			return nil, fmt.Errorf("decode predicate_walk response (seed=%s): %w", seed, err)
		}
		for i, r := range resp {
			if limit > 0 && i >= limit {
				break
			}
			// Walk-OUT picks the to-entity as the neighbor.
			id := strings.TrimSpace(r.ToEntityID)
			if id == "" || id == seed {
				continue
			}
			allEvidence = append(allEvidence, research.Evidence{
				EntityID: id,
				Tier:     tier,
				Source:   source,
			})
		}
	}
	return allEvidence, nil
}

// TemporalRange implements GraphQueryClient via
// graph.query.temporal (passthrough to graph-index-temporal's
// handleQueryRangeNATS). Verified shapes: request
// `{startTime, endTime, limit}` (RFC3339 strings), response is a
// bare array `[{id, type}]` (TemporalResult struct). The temporal
// handler does NOT filter by topic — the materializer's Topic
// field is kept for prompt construction / future Phase 2 server-
// side filter, but not forwarded to the request.
//
// Temporal index is optional in operator deployments; an unwired
// index returns transport error → RequestClassified surfaces it →
// orchestrator marks degraded.
func (a *graphQueryAdapter) TemporalRange(ctx context.Context, args TemporalRangeArgs, tier, source string, limit int) ([]research.Evidence, error) {
	if a == nil || a.client == nil {
		return nil, errors.New("nats client not configured")
	}
	req := struct {
		StartTime string `json:"startTime"`
		EndTime   string `json:"endTime"`
		Limit     int    `json:"limit,omitempty"`
	}{StartTime: args.Start, EndTime: args.End, Limit: limit}
	respData, err := a.request(ctx, subjectGraphQueryTemporal, req)
	if err != nil {
		return nil, fmt.Errorf("temporal_range via %s: %w", subjectGraphQueryTemporal, err)
	}
	// graph.temporal.query.range returns a BARE array of
	// TemporalResult {id, type}; no envelope.
	var resp []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("decode temporal_range response: %w", err)
	}
	out := make([]research.Evidence, 0, len(resp))
	for i, r := range resp {
		if limit > 0 && i >= limit {
			break
		}
		if strings.TrimSpace(r.ID) == "" {
			continue
		}
		out = append(out, research.Evidence{
			EntityID: r.ID,
			Tier:     tier,
			Source:   source,
		})
	}
	return out, nil
}

// BM25 implements GraphQueryClient via graph.query.searchGraph —
// the same surface research-graph-classify uses for initial
// candidate retrieval. Sharing the surface keeps the evidence
// schema consistent (Evidence here ≡ subset of GlobalSearchResponse
// entity digests, same projection as nl_classify's Candidate).
func (a *graphQueryAdapter) BM25(ctx context.Context, args BM25Args, tier, source string, limit int) ([]research.Evidence, error) {
	if a == nil || a.client == nil {
		return nil, errors.New("nats client not configured")
	}
	if strings.TrimSpace(args.Query) == "" {
		return nil, errors.New("bm25 query is empty")
	}
	limitCap := args.Limit
	if limitCap <= 0 {
		limitCap = limit
	}
	req := struct {
		Query          string `json:"query"`
		MaxCommunities int    `json:"max_communities,omitempty"`
	}{Query: args.Query, MaxCommunities: limitCap}
	respData, err := a.request(ctx, subjectGraphQuerySearchGraph, req)
	if err != nil {
		return nil, fmt.Errorf("bm25 via %s: %w", subjectGraphQuerySearchGraph, err)
	}
	var resp struct {
		EntityDigests []struct {
			ID        string  `json:"id"`
			Relevance float64 `json:"relevance,omitempty"`
		} `json:"entity_digests"`
	}
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("decode bm25 response: %w", err)
	}
	// BM25 surface uses a different ID field name (id, not entity_id)
	// and exposes relevance instead of score. Project to the unified
	// Evidence shape inline.
	out := make([]research.Evidence, 0, len(resp.EntityDigests))
	for i, d := range resp.EntityDigests {
		if i >= limitCap && limitCap > 0 {
			break
		}
		if strings.TrimSpace(d.ID) == "" {
			continue
		}
		out = append(out, research.Evidence{
			EntityID: d.ID,
			Tier:     tier,
			Source:   source,
			Score:    d.Relevance,
		})
	}
	return out, nil
}

// request marshals + ships the request body via RequestClassified.
// Applies a.timeout on the context (so per-call deadline is the
// MIN of caller's ctx and a.timeout) and passes 0 for the wire-
// level timeout to avoid double-timer accounting — the context
// deadline is the single source of truth. Mirrors the
// research-graph-classify adapter pattern.
func (a *graphQueryAdapter) request(ctx context.Context, subject string, body any) ([]byte, error) {
	reqData, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	if a.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.timeout)
		defer cancel()
	}
	return a.client.RequestClassified(ctx, subject, reqData, 0)
}

// natsLoopStore adapts natsclient.KVStore to the LoopStore
// interface. Same shape as research-graph-route's adapter; reads
// three upstream payloads (Intent + ClassifierOutput +
// RouteDecision), writes ExecutionOutput envelope + snapshot.
type natsLoopStore struct {
	kv      *natsclient.KVStore
	decoder *message.Decoder
}

func newNATSLoopStore(kv *natsclient.KVStore, registry *payloadregistry.Registry) *natsLoopStore {
	return &natsLoopStore{
		kv:      kv,
		decoder: message.NewDecoder(registry),
	}
}

// Key helpers — package-private.

func loopStoreKeyIntent(loopID string) string           { return "research.requested." + loopID }
func loopStoreKeyClassifyComplete(loopID string) string { return "classify.complete." + loopID }
func loopStoreKeyRouteComplete(loopID string) string    { return "route.complete." + loopID }
func loopStoreKeyExecuteComplete(loopID string) string  { return "execute.complete." + loopID }
func loopStoreKeyExecuteSnapshot(loopID string) string  { return "execute.snapshot." + loopID }

// GetIntent decodes research_intent from the trigger key.
func (s *natsLoopStore) GetIntent(ctx context.Context, loopID string) (*research.Intent, error) {
	entry, err := s.kv.Get(ctx, loopStoreKeyIntent(loopID))
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return nil, errIntentNotFound
		}
		return nil, fmt.Errorf("kv get %s: %w", loopStoreKeyIntent(loopID), err)
	}
	decoded, err := s.decoder.Decode(entry.Value)
	if err != nil {
		return nil, fmt.Errorf("decode intent: %w", err)
	}
	intent, ok := decoded.Payload().(*research.Intent)
	if !ok {
		return nil, fmt.Errorf("decoded payload is %T, expected *research.Intent", decoded.Payload())
	}
	return intent, nil
}

// GetClassifierOutput decodes classifier_output from R1's trigger key.
func (s *natsLoopStore) GetClassifierOutput(ctx context.Context, loopID string) (*research.ClassifierOutput, error) {
	entry, err := s.kv.Get(ctx, loopStoreKeyClassifyComplete(loopID))
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return nil, errClassifierOutputMissing
		}
		return nil, fmt.Errorf("kv get %s: %w", loopStoreKeyClassifyComplete(loopID), err)
	}
	decoded, err := s.decoder.Decode(entry.Value)
	if err != nil {
		return nil, fmt.Errorf("decode classifier output: %w", err)
	}
	out, ok := decoded.Payload().(*research.ClassifierOutput)
	if !ok {
		return nil, fmt.Errorf("decoded payload is %T, expected *research.ClassifierOutput", decoded.Payload())
	}
	return out, nil
}

// GetRouteDecision decodes route_decision from R2's trigger key.
func (s *natsLoopStore) GetRouteDecision(ctx context.Context, loopID string) (*research.RouteDecision, error) {
	entry, err := s.kv.Get(ctx, loopStoreKeyRouteComplete(loopID))
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return nil, errRouteDecisionMissing
		}
		return nil, fmt.Errorf("kv get %s: %w", loopStoreKeyRouteComplete(loopID), err)
	}
	decoded, err := s.decoder.Decode(entry.Value)
	if err != nil {
		return nil, fmt.Errorf("decode route decision: %w", err)
	}
	dec, ok := decoded.Payload().(*research.RouteDecision)
	if !ok {
		return nil, fmt.Errorf("decoded payload is %T, expected *research.RouteDecision", decoded.Payload())
	}
	return dec, nil
}

// PutExecutionOutput writes envelope at R3's trigger key.
func (s *natsLoopStore) PutExecutionOutput(ctx context.Context, loopID string, envelope []byte) error {
	_, err := s.kv.Put(ctx, loopStoreKeyExecuteComplete(loopID), envelope)
	return err
}

// PutSnapshot writes envelope at the stable snapshot key. Best-
// effort: handler logs + continues on failure.
func (s *natsLoopStore) PutSnapshot(ctx context.Context, loopID string, envelope []byte) error {
	_, err := s.kv.Put(ctx, loopStoreKeyExecuteSnapshot(loopID), envelope)
	return err
}
