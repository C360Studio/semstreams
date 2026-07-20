package fusionnats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/readiness"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/fusion"
	semtypes "github.com/c360studio/semstreams/pkg/types"
)

// Public graph query subjects the retrieval client maps onto. These are the
// stable external surface (graph-query / graph-index passthroughs), NOT the
// internal graph.ingest.*/graph.embedding.* subjects, so a standalone fusion
// service reaches them through the same boundary as any other consumer.
// Status is deliberately absent: ADR-083 removed `graph.index.query.status` and
// readiness now travels as GRAPH_STATUS KV state (see Status below). Every other
// subject here is untouched.
const (
	subjectByName        = "graph.query.byName"
	subjectPrefix        = "graph.query.prefix"
	subjectSemantic      = "graph.query.semantic"
	subjectEntity        = "graph.query.entity"
	subjectBatch         = "graph.query.batch"
	subjectRelationships = "graph.query.relationships"
)

// defaultTimeout is used when New is given a non-positive timeout. Matches the
// 5s the existing graph/query client uses for index lookups.
const defaultTimeout = 5 * time.Second

// requester is the minimal NATS surface the retrieval client needs: classified
// request/reply. *natsclient.Client satisfies it. Kept as a local interface so
// the client unit-tests against a fake without a live NATS, and so this package
// does not have to import natsclient for its production path.
type requester interface {
	RequestClassified(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error)
}

// Client is the production NATS implementation of fusion.RetrievalClient. Every
// method is safe to call from any goroutine: the subject calls are stateless, and
// the readiness watch is guarded by statusMu.
type Client struct {
	nats    requester
	timeout time.Duration

	// statusMu guards the lazily-bound readiness watch below. A mutex rather than a
	// sync.Once because Close must be able to stop a watch that a concurrent Status
	// may be binding, and Once gives no way to interlock with that.
	statusMu sync.Mutex
	// statusBound records that binding was ATTEMPTED, so a permanent wiring failure
	// (a transport with no KV) is not retried on every call.
	statusBound bool
	// statusWaited records that the one-shot bind wait has been spent; see Status.
	statusWaited bool
	statusClosed bool
	statusErr    error
	statusWatch  *readiness.Watcher
}

// Compile-time assertion that Client satisfies the engine's retrieval surface.
var _ fusion.RetrievalClient = (*Client)(nil)

// New builds a retrieval client over the given NATS requester. A non-positive
// timeout falls back to defaultTimeout.
//
// The signature is load-bearing: sister repos construct this as
// `fusionnats.New(natsClient, 0)` with no lifecycle, so ADR-083's move of readiness
// onto a KV WATCH must not become a construction change. The watch is therefore bound
// lazily inside Status (see statusWatcher) rather than here.
func New(nats requester, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{nats: nats, timeout: timeout}
}

// Close stops the readiness watch. It is OPTIONAL and additive — a client that is
// never closed keeps one watch goroutine for the process lifetime, which is what a
// long-lived retrieval client wants; Close exists so tests and short-lived embedders
// can reclaim it. Safe to call more than once, and safe on a client that never bound
// a watch.
func (c *Client) Close() {
	c.statusMu.Lock()
	if c.statusClosed {
		c.statusMu.Unlock()
		return
	}
	c.statusClosed = true
	watch := c.statusWatch
	c.statusWatch = nil
	c.statusMu.Unlock()

	// Stop outside the lock: it waits for the watch goroutine to exit, and holding
	// statusMu across that would block a concurrent Status behind shutdown.
	if watch != nil {
		watch.Stop()
	}
}

// statusWatcher returns the readiness watch, binding it on first use. Every caller is
// serialized on statusMu, so the bind happens exactly once no matter how many
// goroutines call Status concurrently.
//
// The watch is started with the CALLER'S context stripped of cancellation
// (context.WithoutCancel): it must outlive the request that happened to trigger the
// bind — a watch cancelled with the first query would leave every later one unknown —
// while still carrying that context's values. Close is the only thing that stops it.
func (c *Client) statusWatcher(ctx context.Context) (*readiness.Watcher, error) {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()

	if c.statusClosed {
		return nil, errors.New("fusionnats: client closed")
	}
	if c.statusBound {
		return c.statusWatch, c.statusErr
	}
	c.statusBound = true

	// The KV watch is an OPTIONAL CAPABILITY of the transport, asserted through the
	// narrow method set with its exact signature (a divergent embedded signature would
	// silently fail the structural match). *natsclient.Client satisfies it, so no
	// caller has to change its construction; a transport that does not is a wiring
	// error, and saying so beats silently reporting a not-ready graph forever.
	src, ok := c.nats.(readiness.BucketSource)
	if !ok {
		c.statusErr = fmt.Errorf(
			"fusionnats: transport %T cannot watch %s (readiness moved to KV in ADR-083)",
			c.nats, readiness.BucketGraphStatus)
		return nil, c.statusErr
	}
	watch := readiness.NewWatcher(src, readiness.KeyGraphIndex)
	if err := watch.Start(context.WithoutCancel(ctx)); err != nil {
		c.statusErr = fmt.Errorf("fusionnats: start readiness watch on %s/%s: %w",
			readiness.BucketGraphStatus, readiness.KeyGraphIndex, err)
		return nil, c.statusErr
	}
	c.statusWatch = watch
	return watch, nil
}

// awaitFirstStatus spends the ONE bounded wait for the watch's first delivery, so a
// cold client's first Status does not report unknown merely because the watch has not
// delivered yet. The budget is the client's own timeout — the exact budget the status
// request/reply had — so the worst case of the first call is unchanged and every later
// call is a local read. It is spent once: with no graph-index deployed nothing ever
// arrives, and waiting per call would turn that deployment into a per-call stall.
func (c *Client) awaitFirstStatus(ctx context.Context, watch *readiness.Watcher) {
	c.statusMu.Lock()
	if c.statusWaited {
		c.statusMu.Unlock()
		return
	}
	c.statusWaited = true
	c.statusMu.Unlock()

	waitCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	// The error is deliberately dropped: a wait that times out is not itself the
	// verdict. Read below is, and it fails closed on exactly the same unknown.
	_ = watch.WaitForFirst(waitCtx)
}

// Status reports graph readiness for the honesty envelope.
//
// ADR-083 moved the source from the `graph.index.query.status` request/reply to the
// shared GRAPH_STATUS watch (graph/readiness). Only the transport moved: every failure
// to establish readiness still returns an error, exactly as a failed request did, and a
// received envelope is still handed up verbatim, so Fuse's top gate and its
// ErrorCodeIndexNotReady degrade paths see the same inputs they saw before. This is
// `graph.GateExact` at the engine's top gate and `graph.GateDegradeHonest` in Fuse's
// internal-read recovery — naming, not new behavior.
//
// Watching rather than reading per call is the point of the change: a per-decision
// round-trip travels the same connection as the ENTITY_STATES firehose in a
// single-binary deployment, so it times out under exactly the load that makes readiness
// interesting (gh#590). The producer now pays one write per heartbeat and this client
// holds the answer.
//
// The unknown set is what the subject already covered — no bucket, no key, a deleted
// key, a backend fault, an undecodable value, a transport with no KV — plus one case
// state adds that a request could not have: a feed that has gone quiet past the
// freshness window, i.e. a producer that died holding a Ready key. All of them are one
// error, because all of them were one error before.
func (c *Client) Status(ctx context.Context) (fusion.IndexStatus, error) {
	watch, err := c.statusWatcher(ctx)
	if err != nil {
		return fusion.IndexStatus{}, err
	}
	c.awaitFirstStatus(ctx, watch)

	reading := watch.Read()
	if !reading.Fresh {
		// Unknown readiness: never received, gone quiet, or a stale key held by a dead
		// producer. Under request/reply every one of these was an error, and it stays
		// one — a dead graph-index must not serve its final Ready=true forever, or the
		// honesty envelope licenses authoritative-absence claims against a frozen index.
		return fusion.IndexStatus{}, fmt.Errorf(
			"fusionnats: readiness on %s/%s is unknown (known=%t age=%s): %w",
			readiness.BucketGraphStatus, readiness.KeyGraphIndex,
			reading.Known, reading.Age, unknownCause(reading))
	}
	// Decode straight into the target: graph.IndexStatusResponse and
	// fusion.IndexStatus are field-identical by contract (ADR-066 §5), so a direct
	// unmarshal keeps them changing together — a hand-copied remap silently drops
	// any field added to the wire (as it did for IndexedRevision/Lag before the
	// round-trip test below), which reads as a false-caught-up (Lag==0) downstream.
	// This is why the reading carries the RAW wire bytes alongside its decoded
	// envelope: taking the decoded struct would reintroduce exactly that remap.
	var resp fusion.IndexStatus
	if err := json.Unmarshal(reading.Raw, &resp); err != nil {
		return fusion.IndexStatus{}, fmt.Errorf("fusionnats: decode status: %w", err)
	}
	return resp, nil
}

// unknownCause names why readiness could not be established, so the returned error is
// actionable rather than a bare "unknown". A quiet feed carries no watch error at all,
// hence the fallback.
func unknownCause(reading readiness.Reading) error {
	if reading.Err != nil {
		return reading.Err
	}
	if reading.Known {
		return errors.New("graph-index stopped publishing its readiness envelope")
	}
	return errors.New("no readiness envelope has been published")
}

// Resolve maps a query to seed entity IDs by mode, most relevant first. The mode
// selects the subject; an unknown mode is an error rather than a silent default
// (ResolveMode is an open string enum).
func (c *Client) Resolve(ctx context.Context, q fusion.ResolveQuery) ([]string, error) {
	switch q.Mode {
	case fusion.ResolveModeSymbol:
		return c.resolveByName(ctx, q.Query, q.Limit)
	case fusion.ResolveModePrefix:
		return c.resolvePrefix(ctx, q.Query, q.Limit)
	case fusion.ResolveModeNL:
		return c.resolveSemantic(ctx, q.Query, q.Scope, q.Limit)
	default:
		return nil, fmt.Errorf("fusionnats: unsupported resolve mode %q", q.Mode)
	}
}

// resolveByName resolves a symbol to ranked entity IDs via graph.query.byName.
func (c *Client) resolveByName(ctx context.Context, query string, limit int) ([]string, error) {
	matches, err := c.byNameMatches(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(matches))
	for _, m := range matches {
		ids = append(ids, m.EntityID)
	}
	return ids, nil
}

// resolvePrefix resolves an ID prefix to entity IDs via graph.query.prefix. Only
// the first page is taken — resolve seeds are bounded by limit, not exhaustive.
func (c *Client) resolvePrefix(ctx context.Context, query string, limit int) ([]string, error) {
	if err := validatePrefix(query); err != nil {
		return nil, err
	}
	raw, err := c.request(ctx, subjectPrefix, graph.PrefixQueryRequest{Prefix: query, Limit: limit})
	if err != nil {
		return nil, err
	}
	var resp graph.PrefixQueryResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("fusionnats: decode prefix: %w", err)
	}
	if err := graph.ValidateDecodedEntityStates(resp.Entities); err != nil {
		return nil, fmt.Errorf("fusionnats: validate prefix: %w", err)
	}
	ids := make([]string, 0, len(resp.Entities))
	for i := range resp.Entities {
		ids = append(ids, resp.Entities[i].ID)
	}
	return ids, nil
}

// resolveSemantic resolves a natural-language query to embedding-ranked entity
// IDs via graph.query.semantic. A non-empty scope constrains candidates to the
// given entity-ID prefixes at the source (ADR-071); it is inserted into the
// request body ONLY when non-empty, so an unscoped call is byte-identical to the
// pre-scope wire shape (every existing caller, and every symbol/prefix path,
// sends none).
func (c *Client) resolveSemantic(ctx context.Context, query string, scope []string, limit int) ([]string, error) {
	if err := validateScope(scope); err != nil {
		return nil, err
	}
	body := map[string]any{"query": query, "limit": limit}
	if len(scope) > 0 {
		body["scope"] = scope
	}
	raw, err := c.request(ctx, subjectSemantic, body)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Results []struct {
			EntityID string `json:"entity_id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("fusionnats: decode semantic: %w", err)
	}
	ids := make([]string, 0, len(resp.Results))
	for _, r := range resp.Results {
		ids = append(ids, r.EntityID)
	}
	if err := graph.ValidateDecodedEntityIDs(ids); err != nil {
		return nil, fmt.Errorf("fusionnats: validate semantic: %w", err)
	}
	return ids, nil
}

func validatePrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	return semtypes.ValidateEntityIDPrefix(prefix)
}

func validateScope(scope []string) error {
	for _, prefix := range scope {
		// An explicit empty member is the documented match-all scope.
		if prefix == "" {
			continue
		}
		if err := semtypes.ValidateEntityIDPrefix(prefix); err != nil {
			return err
		}
	}
	return nil
}

// Entity returns an entity by ID, or (nil, nil) if absent. The graph-ingest
// handler classifies a missing entity as ErrorInvalid with code
// entity_not_found; that is the ONE error we translate to absence — every other
// error propagates so the engine never reads a backend fault as a not-found.
func (c *Client) Entity(ctx context.Context, id string) (*fusion.Entity, error) {
	raw, err := c.request(ctx, subjectEntity, map[string]string{"id": id})
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var es graph.EntityState
	if err := json.Unmarshal(raw, &es); err != nil {
		return nil, fmt.Errorf("fusionnats: decode entity %q: %w", id, err)
	}
	if err := graph.ValidateDecodedEntityState(&es); err != nil {
		return nil, fmt.Errorf("fusionnats: validate entity %q: %w", id, err)
	}
	return &fusion.Entity{ID: es.ID, Triples: es.Triples}, nil
}

// Entities batch-fetches entities by ID via graph.query.batch. Absent IDs are
// omitted by the handler (partial success); a non-nil error means a backend
// failure, which the engine distinguishes from genuine absence.
func (c *Client) Entities(ctx context.Context, ids []string) ([]*fusion.Entity, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	raw, err := c.request(ctx, subjectBatch, map[string]any{"ids": ids})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Entities []graph.EntityState `json:"entities"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("fusionnats: decode batch: %w", err)
	}
	if err := graph.ValidateDecodedEntityStates(resp.Entities); err != nil {
		return nil, fmt.Errorf("fusionnats: validate batch: %w", err)
	}
	out := make([]*fusion.Entity, 0, len(resp.Entities))
	for i := range resp.Entities {
		out = append(out, &fusion.Entity{ID: resp.Entities[i].ID, Triples: resp.Entities[i].Triples})
	}
	return out, nil
}

// Neighbors returns edges from id along the given predicates in a direction via
// graph.query.relationships. The handler returns ALL edges in the direction;
// this filters to the requested predicates (empty predicates means no filter).
func (c *Client) Neighbors(ctx context.Context, id string, predicates []string, dir fusion.Direction) ([]fusion.Edge, error) {
	raw, err := c.request(ctx, subjectRelationships, map[string]string{
		"entity_id": id,
		"direction": directionString(dir),
	})
	if err != nil {
		return nil, err
	}
	var rels []struct {
		FromEntityID string `json:"from_entity_id"`
		ToEntityID   string `json:"to_entity_id"`
		EdgeType     string `json:"edge_type"`
	}
	if err := json.Unmarshal(raw, &rels); err != nil {
		return nil, fmt.Errorf("fusionnats: decode relationships: %w", err)
	}
	endpointIDs := make([]string, 0, len(rels)*2)
	for _, relationship := range rels {
		endpointIDs = append(endpointIDs, relationship.FromEntityID, relationship.ToEntityID)
	}
	if err := graph.ValidateDecodedEntityIDs(endpointIDs); err != nil {
		return nil, fmt.Errorf("fusionnats: validate relationships: %w", err)
	}
	want := predicateSet(predicates)
	edges := make([]fusion.Edge, 0, len(rels))
	for _, r := range rels {
		if want != nil && !want[r.EdgeType] {
			continue
		}
		// The target is the OTHER end of the edge: outgoing points to
		// to_entity_id, incoming comes from from_entity_id.
		target := r.ToEntityID
		if dir == fusion.Incoming {
			target = r.FromEntityID
		}
		edges = append(edges, fusion.Edge{Predicate: r.EdgeType, Target: target})
	}
	return edges, nil
}

// Names suggests entity display names near a query for a miss's did_you_mean,
// via graph.query.byName. Multiple entities may share a name; names are
// de-duplicated preserving rank order and capped at limit.
func (c *Client) Names(ctx context.Context, query string, limit int) ([]string, error) {
	matches, err := c.byNameMatches(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(matches))
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		if m.MatchedName == "" || seen[m.MatchedName] {
			continue
		}
		seen[m.MatchedName] = true
		names = append(names, m.MatchedName)
		if limit > 0 && len(names) >= limit {
			break
		}
	}
	return names, nil
}

// byNameMatches calls graph.query.byName and returns the ranked matches. Shared
// by Resolve(symbol) (which reads entity IDs) and Names (which reads names).
func (c *Client) byNameMatches(ctx context.Context, query string, limit int) ([]graph.NameMatch, error) {
	raw, err := c.request(ctx, subjectByName, map[string]any{"name": query, "limit": limit})
	if err != nil {
		return nil, err
	}
	var resp graph.QueryResponse[graph.NameData]
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("fusionnats: decode byName: %w", err)
	}
	entityIDs := make([]string, 0, len(resp.Data.Matches))
	for _, match := range resp.Data.Matches {
		entityIDs = append(entityIDs, match.EntityID)
	}
	if err := graph.ValidateDecodedEntityIDs(entityIDs); err != nil {
		return nil, fmt.Errorf("fusionnats: validate byName: %w", err)
	}
	return resp.Data.Matches, nil
}

// request marshals req and issues a classified request/reply on subject.
func (c *Client) request(ctx context.Context, subject string, req any) ([]byte, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("fusionnats: marshal %s request: %w", subject, err)
	}
	return c.nats.RequestClassified(ctx, subject, body, c.timeout)
}

// directionString maps a fusion.Direction to the relationship query's wire value.
func directionString(dir fusion.Direction) string {
	if dir == fusion.Incoming {
		return "incoming"
	}
	return "outgoing"
}

// predicateSet builds a lookup set from predicates, or nil when none are given
// (nil means "no filter" — accept every predicate).
func predicateSet(predicates []string) map[string]bool {
	if len(predicates) == 0 {
		return nil
	}
	set := make(map[string]bool, len(predicates))
	for _, p := range predicates {
		set[p] = true
	}
	return set
}

// isNotFound reports whether err is the graph's entity_not_found classified
// error (the only error Entity treats as absence rather than a fault).
func isNotFound(err error) bool {
	var ce *errs.ClassifiedError
	return errors.As(err, &ce) && ce.Code == graph.ErrorCodeEntityNotFound
}
