package researchclassify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/graph/query"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
)

// classifierChainAdapter adapts *query.ClassifierChain to the
// Classifier interface so tests can swap implementations without
// constructing a real chain.
type classifierChainAdapter struct {
	chain *query.ClassifierChain
}

// ClassifyQuery delegates to the underlying chain. A nil chain
// returns nil; the handler treats nil ClassificationResult as "fall
// through with no hints" rather than an error.
func (a *classifierChainAdapter) ClassifyQuery(ctx context.Context, topic string) *query.ClassificationResult {
	if a == nil || a.chain == nil {
		return nil
	}
	return a.chain.ClassifyQuery(ctx, topic)
}

// searchGraphSubject is the existing graph-query NATS surface for
// natural-language search. PR 2 reuses it (rather than introducing a
// new endpoint) so the candidate-retrieval path shares wiring with
// the search_graph agent tool (PR #54). PR 4 (execute_subqueries) is
// expected to use the same subject for cross-tier retrieval; sharing
// the surface keeps the evidence schema consistent.
const searchGraphSubject = "graph.query.searchGraph"

// searchGraphRetriever satisfies CandidateRetriever via a NATS
// request to graph.query.searchGraph. Returns degraded=true when the
// server-side fallback fired so PR 3's router can downgrade
// confidence.
type searchGraphRetriever struct {
	client  *natsclient.Client
	timeout time.Duration
}

func newSearchGraphRetriever(client *natsclient.Client, timeout time.Duration) *searchGraphRetriever {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &searchGraphRetriever{client: client, timeout: timeout}
}

// searchGraphRequest mirrors the agent tool's wire shape (see
// processor/agentic-tools/executors/search_graph.go). Limit is
// surfaced via max_communities — the server-side resolver uses it
// to cap the result fan-out.
type searchGraphRequest struct {
	Query          string `json:"query"`
	MaxCommunities int    `json:"max_communities,omitempty"`
}

// searchGraphResponse is a deliberate subset of
// processor/graph-query.GlobalSearchResponse — only the fields
// nl_classify needs. A future GlobalSearchResponse field addition
// won't surface here automatically; a contract test against the real
// type would catch drift but is deferred to PR 4 (where
// execute_subqueries consumes the same shape).
type searchGraphResponse struct {
	Strategy           string                `json:"strategy,omitempty"`
	Count              int                   `json:"count"`
	EntityDigests      []searchEntityDigest  `json:"entity_digests,omitempty"`
	CommunitySummaries []searchCommunityItem `json:"community_summaries,omitempty"`
	Degraded           bool                  `json:"degraded,omitempty"`
	DegradedReason     string                `json:"degraded_reason,omitempty"`
}

// searchEntityDigest mirrors processor/graph-query.EntityDigest
// (graphrag.go:180). The upstream type does NOT yet carry a snippet
// field, so we don't either — adding one here would silently decode
// as empty and create the "warning-not-fail masks integration drift"
// shape from the discipline memory. SnippetText on the emitted
// Candidate stays empty until upstream EntityDigest grows a snippet
// field; the route_search prompt (PR 3) treats absent snippets as
// "consult entity directly" rather than an error.
type searchEntityDigest struct {
	ID        string  `json:"id"`
	Type      string  `json:"type,omitempty"`
	Label     string  `json:"label,omitempty"`
	Relevance float64 `json:"relevance,omitempty"`
}

type searchCommunityItem struct {
	Summary string `json:"summary,omitempty"`
}

// FetchCandidates implements CandidateRetriever. Maps the
// graph.query.searchGraph response into research.Candidate items.
//
// The configured RetrieveTimeout is applied as a child context of
// the inbound ctx so the budget is the MIN of the caller's remaining
// deadline (typically the handler's ClassifyTimeout-derived context)
// and the per-call retrieval cap. Without the inner derivation,
// RetrieveTimeout would also be passed to client.Request as the
// underlying NATS request deadline and the two timeouts could
// double-count; the inner ctx wins both.
func (r *searchGraphRetriever) FetchCandidates(ctx context.Context, topic string, _ map[string]any, limit int) (CandidateSet, error) {
	if r.client == nil {
		return CandidateSet{}, errors.New("nats client not configured")
	}
	if strings.TrimSpace(topic) == "" {
		return CandidateSet{}, errors.New("topic required for candidate retrieval")
	}
	if limit <= 0 {
		limit = 25
	}
	if r.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.timeout)
		defer cancel()
	}
	req := searchGraphRequest{
		Query:          topic,
		MaxCommunities: limit,
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		return CandidateSet{}, fmt.Errorf("marshal search request: %w", err)
	}
	// gh#93 Phase 2: RequestClassified unifies transport + handler
	// errors. Both arrive via err return; caller wraps as a single
	// "search_graph" error. The upstream retriever drives fall-back
	// off the CandidateSet.Degraded field, not err class — so
	// flattening here is OK today.
	//
	// TODO(phase-4): consider surfacing the err class via
	// CandidateSet.DegradedReason so callers can distinguish
	// "search subsystem unreachable" from "search returned no
	// candidates legitimately" once header-classified errors are
	// the canonical path.
	respData, err := r.client.RequestClassified(ctx, searchGraphSubject, reqData, r.timeout)
	if err != nil {
		return CandidateSet{}, fmt.Errorf("search_graph: %w", err)
	}

	var resp searchGraphResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return CandidateSet{}, fmt.Errorf("decode search response: %w", err)
	}

	cands := make([]research.Candidate, 0, len(resp.EntityDigests))
	for i, d := range resp.EntityDigests {
		if i >= limit {
			break
		}
		if strings.TrimSpace(d.ID) == "" {
			continue
		}
		cands = append(cands, research.Candidate{
			EntityID:  d.ID,
			Label:     d.Label,
			Type:      d.Type,
			Relevance: d.Relevance,
			Tier:      "0",
			Source:    "search_graph",
		})
	}
	return CandidateSet{
		Candidates:     cands,
		Degraded:       resp.Degraded,
		DegradedReason: resp.DegradedReason,
	}, nil
}

// natsLoopStore adapts natsclient.KVStore to the LoopStore interface.
// Implementation choices:
//
//   - GetIntent reads research.request.received.<loopID> and decodes via
//     the production message.Decoder. NewTestDecoder isn't suitable
//     here because production traffic goes through this path; we use
//     a per-process decoder cached on the adapter.
//
//   - PutClassifierOutput writes to classify.complete.<loopID> using
//     KVStore.Put. Last-writer-wins is fine — the loop_id is unique
//     per research operation, so concurrent writes for the same key
//     shouldn't occur outside of retries (which write the same
//     bytes).
//
//   - PutSnapshot writes to classify.snapshot.<loopID>, a stable
//     non-trigger key for downstream queryability. Same Put
//     semantics.
type natsLoopStore struct {
	kv      *natsclient.KVStore
	decoder *message.Decoder
}

// newNATSLoopStore wires the LoopStore using the supplied KV store
// and the process-wide payload registry. registry must be non-nil —
// without it the BaseMessage envelope written by research_graph (PR
// 1) cannot be decoded. The factory caller (NewProcessor) is
// responsible for surfacing a nil-registry config error cleanly.
func newNATSLoopStore(kv *natsclient.KVStore, registry *payloadregistry.Registry) *natsLoopStore {
	return &natsLoopStore{
		kv:      kv,
		decoder: message.NewDecoder(registry),
	}
}

// loopStoreKeyIntent returns the AGENT_LOOPS key holding the
// research_intent payload. Mirrors processor/agentic-tools/executors/
// research_graph.go's researchTriggerKeyPrefix.
func loopStoreKeyIntent(loopID string) string { return "research.request.received." + loopID }

// loopStoreKeyClassifyComplete is R1's trigger key.
func loopStoreKeyClassifyComplete(loopID string) string { return "classify.complete." + loopID }

// loopStoreKeySnapshot is the non-trigger queryable snapshot key.
func loopStoreKeySnapshot(loopID string) string { return "classify.snapshot." + loopID }

// GetIntent decodes the research_intent payload from the trigger
// key. errIntentNotFound is returned when the key doesn't exist so
// the handler can log + drop without a retry storm.
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

// PutClassifierOutput writes the envelope at R1's trigger key.
func (s *natsLoopStore) PutClassifierOutput(ctx context.Context, loopID string, envelope []byte) error {
	_, err := s.kv.Put(ctx, loopStoreKeyClassifyComplete(loopID), envelope)
	return err
}

// PutSnapshot writes the envelope at the stable snapshot key. Best-
// effort: the handler logs and continues on failure rather than
// aborting the chain.
func (s *natsLoopStore) PutSnapshot(ctx context.Context, loopID string, envelope []byte) error {
	_, err := s.kv.Put(ctx, loopStoreKeySnapshot(loopID), envelope)
	return err
}
