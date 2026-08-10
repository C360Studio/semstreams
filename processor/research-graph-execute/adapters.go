package researchexecute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/fusion"
)

type graphQueryAdapterSubjects struct {
	batch         string
	relationships string
	temporal      string
	searchGraph   string
}

// graphQueryAdapter wraps natsclient.Client into the fusion.GraphQueryClient
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
	client interface {
		RequestClassified(context.Context, string, []byte, time.Duration) ([]byte, error)
	}
	subjects graphQueryAdapterSubjects
	timeout  time.Duration
	// logger reports partial hydration. Nil-safe: tests that construct the adapter
	// directly get a silent one rather than a panic.
	logger *slog.Logger
}

func newGraphQueryAdapter(client *natsclient.Client, subjects graphQueryAdapterSubjects, timeout time.Duration, logger *slog.Logger) *graphQueryAdapter {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &graphQueryAdapter{client: client, subjects: subjects, timeout: timeout, logger: logger}
}

// EntityState implements fusion.GraphQueryClient via graph.query.batch
// (passthrough to graph-ingest's handleQueryBatchNATS). Returns one
// Evidence per requested entity ID that resolves.
//
// Unhydrated IDs are no longer silently omitted upstream: the handler
// reports them as `missing: [{id, reason}]` (ADR-084 D4 / gh#597), and
// this adapter LOGS them. It does not fail the call and does not emit
// Evidence for them — evidence is a claim about something we read, and
// an ID we could not read supports no claim. Logging is the right
// weight here because a research walk over a broad seed set legitimately
// includes IDs that no longer exist; what was missing before was any way
// to notice when that stopped being legitimate.
//
// Verified shapes: request `{ids: [...]}`, response
// `{entities: [<EntityState>...], missing: [{id, reason}]}` where each
// entity uses the `id` field (graph/types.go EntityState).
func (a *graphQueryAdapter) EntityState(ctx context.Context, args EntityStateArgs, tier, source string, limit int) ([]fusion.Evidence, error) {
	if a == nil || a.client == nil {
		return nil, errors.New("nats client not configured")
	}
	if len(args.EntityIDs) == 0 {
		return nil, nil
	}
	req := struct {
		IDs []string `json:"ids"`
	}{IDs: args.EntityIDs}
	respData, err := a.request(ctx, a.subjects.batch, req)
	if err != nil {
		return nil, fmt.Errorf("entity_state via %s: %w", a.subjects.batch, err)
	}
	entities, missing, err := decodeEntityStateResponse(respData)
	if err != nil {
		return nil, err
	}
	a.logMissing(args.EntityIDs, missing)
	out := make([]fusion.Evidence, 0, len(entities))
	for i, entity := range entities {
		if limit > 0 && i >= limit {
			break
		}
		out = append(out, fusion.Evidence{
			EntityID: entity.ID,
			Tier:     tier,
			Source:   source,
		})
	}
	return out, nil
}

func decodeEntityStateResponse(data []byte) ([]graph.EntityState, []graph.MissingEntity, error) {
	// Decode the complete EntityState candidates even though this adapter only
	// projects IDs. A partial {id} shape would let poisoned subjects/references
	// cross an authoritative batch boundary unseen.
	var resp graph.EntityBatchResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, nil, fmt.Errorf("decode entity_state response: %w", err)
	}
	if err := graph.ValidateDecodedEntityStates(resp.Entities); err != nil {
		return nil, nil, fmt.Errorf("validate entity_state response: %w", err)
	}
	return resp.Entities, resp.Missing, nil
}

// logMissing reports IDs the batch could not hydrate. A research walk that quietly
// evidences 3 of 40 seeds and one that evidences 3 because 37 do not exist look
// identical in the trajectory otherwise.
//
// It CONSUMES the handler's report rather than reconciling against the requested set
// (task 4.6 allowed either). The requested count is carried for context only, so an
// under-reporting handler stays invisible here — that reconciliation lives in
// fusionnats.Entities, on the path where a dropped seed actually changes an answer.
func (a *graphQueryAdapter) logMissing(requested []string, missing []graph.MissingEntity) {
	if a.logger == nil {
		return
	}
	if len(missing) == 0 {
		return
	}
	a.logger.Warn("entity_state batch did not hydrate every requested ID",
		slog.Int("requested", len(requested)),
		slog.Int("missing", len(missing)),
		slog.Any("missing_ids", missingIDs(missing)))
}

// missingIDs projects the reported entries to a bounded ID list for the log.
func missingIDs(missing []graph.MissingEntity) []string {
	const maxLogged = 20
	if len(missing) > maxLogged {
		missing = missing[:maxLogged]
	}
	out := make([]string, 0, len(missing))
	for _, m := range missing {
		out = append(out, m.ID+"("+string(m.Reason)+")")
	}
	return out
}

// PredicateWalk implements fusion.GraphQueryClient via
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
func (a *graphQueryAdapter) PredicateWalk(ctx context.Context, args PredicateWalkArgs, tier, source string, limit int) ([]fusion.Evidence, error) {
	if a == nil || a.client == nil {
		return nil, errors.New("nats client not configured")
	}
	if len(args.Seeds) == 0 {
		return nil, nil
	}
	var allEvidence []fusion.Evidence
	for _, seed := range args.Seeds {
		if strings.TrimSpace(seed) == "" {
			continue
		}
		req := struct {
			EntityID  string `json:"entity_id"`
			Direction string `json:"direction"`
		}{EntityID: seed, Direction: "outgoing"}
		respData, err := a.request(ctx, a.subjects.relationships, req)
		if err != nil {
			return nil, fmt.Errorf("predicate_walk seed=%s via %s: %w", seed, a.subjects.relationships, err)
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
			allEvidence = append(allEvidence, fusion.Evidence{
				EntityID: id,
				Tier:     tier,
				Source:   source,
			})
		}
	}
	return allEvidence, nil
}

// TemporalRange implements fusion.GraphQueryClient via
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
func (a *graphQueryAdapter) TemporalRange(ctx context.Context, args TemporalRangeArgs, tier, source string, limit int) ([]fusion.Evidence, error) {
	if a == nil || a.client == nil {
		return nil, errors.New("nats client not configured")
	}
	req := struct {
		StartTime string `json:"startTime"`
		EndTime   string `json:"endTime"`
		Limit     int    `json:"limit,omitempty"`
	}{StartTime: args.Start, EndTime: args.End, Limit: limit}
	respData, err := a.request(ctx, a.subjects.temporal, req)
	if err != nil {
		return nil, fmt.Errorf("temporal_range via %s: %w", a.subjects.temporal, err)
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
	out := make([]fusion.Evidence, 0, len(resp))
	for i, r := range resp {
		if limit > 0 && i >= limit {
			break
		}
		if strings.TrimSpace(r.ID) == "" {
			continue
		}
		out = append(out, fusion.Evidence{
			EntityID: r.ID,
			Tier:     tier,
			Source:   source,
		})
	}
	return out, nil
}

// BM25 implements fusion.GraphQueryClient via graph.query.searchGraph —
// the same surface research-graph-classify uses for initial
// candidate retrieval. Sharing the surface keeps the evidence
// schema consistent (Evidence here ≡ subset of GlobalSearchResponse
// entity digests, same projection as nl_classify's Candidate).
func (a *graphQueryAdapter) BM25(ctx context.Context, args BM25Args, tier, source string, limit int) ([]fusion.Evidence, error) {
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
	respData, err := a.request(ctx, a.subjects.searchGraph, req)
	if err != nil {
		return nil, fmt.Errorf("bm25 via %s: %w", a.subjects.searchGraph, err)
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
	out := make([]fusion.Evidence, 0, len(resp.EntityDigests))
	for i, d := range resp.EntityDigests {
		if i >= limitCap && limitCap > 0 {
			break
		}
		if strings.TrimSpace(d.ID) == "" {
			continue
		}
		out = append(out, fusion.Evidence{
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
	response, err := a.client.RequestClassified(ctx, subject, reqData, 0)
	if err != nil {
		return nil, err
	}
	response, _ = graph.UnwrapQueryResponse(response)
	return response, nil
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

func loopStoreKeyIntent(loopID string) string           { return "research.request.received." + loopID }
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
