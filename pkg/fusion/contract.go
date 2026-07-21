package fusion

import (
	"errors"

	"github.com/c360studio/semstreams/graph"
)

// The fused response contract (ADR-062 lens-driven entry). Lifted from
// semsource's validated source/fusion contract.go — the response shape an agent
// acts on: verbatim body + the structure around it, keyed by what a human reads
// (never an entity ID), with an honesty envelope (readiness + provenance) so a
// caller can calibrate trust and never mistake "not ready" for "not found".
//
// Paths/Impact facets and ontology-coherence ranking are deliberately deferred
// (a follow-on + gh#376 increment 5); this contract carries Nodes + relations +
// the honesty envelope + misses.

// ContractVersion identifies the wire shape of Request/Response.
const ContractVersion = "1"

// Want enumerates the optional facets a caller can request. Empty defaults to
// body plus immediate relations.
type Want string

// The requestable facets.
const (
	WantBody      Want = "body"      // verbatim source/passage
	WantRelations Want = "relations" // callers/callees, links/sections
	WantPaths     Want = "paths"     // bounded outgoing relation paths from the seeds
	WantImpact    Want = "impact"    // transitive reverse-relation closure of the seeds
	WantGraph     Want = "graph"     // lossless structured projection: typed facts + directed edges + evidence
)

// Budget bounds a response. Zero fields take engine defaults.
type Budget struct {
	MaxNodes int `json:"max_nodes,omitempty"`
	MaxBytes int `json:"max_bytes,omitempty"`
}

// Request is the fused query, keyed by what an agent already knows — never an
// entity ID.
type Request struct {
	Query string `json:"query"`
	Want  []Want `json:"want,omitempty"`
	// Scope optionally constrains NL seed resolution to entities whose ID
	// matches at least one of these dot-delimited entity-ID prefixes
	// (OR-matched). Empty/absent means no filter — today's behavior. It lets a
	// lens instance over a shared embedding index retrieve only its own domain
	// so a smaller domain is not diluted by a larger co-resident one (ADR-071).
	// A list, not a scalar: one domain often spans several prefixes (semsource
	// "all code" = golang/python/ts/svelte). NL-only; ignored by symbol/prefix
	// resolve modes.
	Scope  []string `json:"scope,omitempty"`
	Budget Budget   `json:"budget,omitzero"`
	// IncludeScores asks for per-node rank and resolve similarity. Opt-in so the
	// default response is byte-unchanged: scores are debugging and calibration
	// signal, and most callers act on the ordering rather than the numbers.
	IncludeScores bool `json:"include_scores,omitempty"`
}

// Provenance records how an answer was produced so callers can calibrate trust.
type Provenance string

// The provenance tiers, in increasing order of uncertainty.
const (
	ProvenanceDeterministic Provenance = "deterministic" // exact lookup + structural walk
	ProvenanceEmbedding     Provenance = "embedding"     // seeds came from semantic search
	ProvenanceLLM           Provenance = "llm"           // an LLM reasoned over the result
)

// IndexState mirrors the graph readiness phase.
type IndexState string

// The readiness phases. NOTE: none of them licenses a not-found conclusion — see
// IndexStatus.Ready. Ready reports COVERAGE (the index applied every committed revision
// up to its target); it says nothing about whether a source ever published the thing you
// were looking for, which is the question an absence claim actually turns on (ADR-084).
const (
	StateBuilding      IndexState = "building"
	StateReady         IndexState = "ready"
	StateDegraded      IndexState = "degraded"
	StateResetRequired IndexState = "reset_required"
)

// IndexStatus is attached to every response: the honesty envelope a caller uses to
// calibrate trust.
//
// Ready means the index is CAUGHT UP (revision-lag, ADR-066), not merely started. It
// does NOT license an authoritative not-found, and a response is no longer withheld
// merely because it is false — ADR-084 retired that license and regated reads on
// HEALTH (State + BootstrapComplete). Coverage cannot answer absence: an index caught
// up to every revision ever committed still knows nothing about a source that never
// published. Callers wanting "is my write visible?" compare their own revision against
// IndexedRevision; callers wanting "is this data fresh enough?" read StalenessMs.
//
// Field-identical to graph.IndexStatusResponse — the RetrievalClient decodes the
// producer's envelope directly into this; the two change together.
type IndexStatus struct {
	Ready  bool       `json:"ready"`
	State  IndexState `json:"state"`
	Code   string     `json:"code,omitempty"`
	Reason string     `json:"reason,omitempty"`
	// BootstrapComplete reports whether the producer finished its initial build in
	// its current process lifetime (ADR-084 D2) — the wire-observable form of the
	// gh#474 cutover window. Absent reads false (fail closed). See
	// graph.IndexStatusResponse.BootstrapComplete for the full contract; fusion
	// mirrors the field verbatim so the direct decode keeps working.
	BootstrapComplete bool `json:"bootstrap_complete"`
	// IndexedRevision / TargetRevision / Lag expose the exact revision-lag so a
	// caller that knows its own target revision can gate on IndexedRevision >=
	// myRev rather than the coarse global Ready bool (ADR-066).
	IndexedRevision uint64 `json:"indexed_revision,omitempty"`
	TargetRevision  uint64 `json:"target_revision,omitempty"`
	Lag             uint64 `json:"lag,omitempty"`
	// StalenessMs is the age of the view in milliseconds (ADR-083). 0 carries no
	// information (Ready, or not computable); any computed staleness is >= 1ms.
	// See graph.IndexStatusResponse.StalenessMs for the full presence encoding —
	// fusion mirrors that field verbatim so the direct decode keeps working.
	StalenessMs uint64 `json:"staleness_ms,omitempty"`
	Phase       string `json:"phase,omitempty"`
	Revision    string `json:"revision,omitempty"`
	LastSynced  string `json:"last_synced,omitempty"`
}

// DeferReasonAllSeedsUnhydrated is the engine-level defer cause for a query whose seeds
// all resolved but none could be read. It is not a graph.DeferReason — no readiness gate
// produced it; the index was healthy and the hydration failed — so it is named here
// alongside the other engine-level causes that reach Response.DeferReason.
const DeferReasonAllSeedsUnhydrated = "all_seeds_unhydrated"

// ErrReadinessUnknown marks a readiness answer the RetrievalClient cannot vouch for:
// the producer never published, or its feed went quiet past the freshness window
// holding a last-known value. It is DISTINCT from a wiring failure (a transport with no
// KV capability, a bucket that cannot be opened) because the two want different
// responses — an unknown feed is a fail-closed degrade the caller sees as an honest
// empty envelope, while broken wiring is an operator's bug that must stay loud rather
// than masquerading as "the graph is busy" forever (ADR-084 D6).
//
// Wrap it with %w; callers match with errors.Is.
var ErrReadinessUnknown = errors.New("fusion: graph readiness is unknown")

// readinessEnvelope projects this status into the shared gate input. The two structs
// are field-identical by contract (ADR-066 §5) — see the round-trip test that proves
// every field survives this copy, which is what keeps a newly added field from silently
// changing gate behavior by being dropped here.
func (s IndexStatus) readinessEnvelope() graph.IndexStatusResponse {
	return graph.IndexStatusResponse{
		Ready:             s.Ready,
		State:             string(s.State),
		Code:              s.Code,
		Reason:            s.Reason,
		BootstrapComplete: s.BootstrapComplete,
		IndexedRevision:   s.IndexedRevision,
		TargetRevision:    s.TargetRevision,
		Lag:               s.Lag,
		StalenessMs:       s.StalenessMs,
		Phase:             s.Phase,
		Revision:          s.Revision,
		LastSynced:        s.LastSynced,
	}
}

// Ref points to a node by what a human reads — never the entity ID.
type Ref struct {
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	Fragment string `json:"fragment,omitempty"`
	Line     int    `json:"line,omitempty"`
}

// Node is one fused result: verbatim body plus the structure around it. Domains
// differ only in which roles populate Relations (code: callers/callees; docs:
// links/sections).
type Node struct {
	Name     string `json:"name"`
	Kind     string `json:"kind,omitempty"`
	Path     string `json:"path,omitempty"`
	Fragment string `json:"fragment,omitempty"`
	// Lines is [start,end] for code, nil for line-less domains (docs) — a slice
	// so omitempty actually omits it rather than emitting a spurious [0,0].
	Lines     []int            `json:"lines,omitempty"`
	Body      string           `json:"body,omitempty"`
	Relations map[string][]Ref `json:"relations,omitempty"`
	// Class is the BFO/CCO class IRI (provenance/debug; the agent ignores it).
	Class string `json:"class,omitempty"`
	// Handle is an opaque continuation token (internally the entity ID). Not an
	// addressing scheme: never parse or construct it.
	Handle string `json:"handle,omitempty"`
	// Rank is this node's 1-based position in RESOLVE order — where the index put it
	// before the engine re-ranked. Present only when the request set IncludeScores.
	//
	// Deliberately not the node's position in the response: that is the array index,
	// which the caller can already count. Ranking reorders (lexical, ontology,
	// salience), so the GAP between resolve rank and response position is the whole
	// signal — it is what makes "why did this come out third?" answerable.
	Rank int `json:"rank,omitempty"`
	// Similarity is the resolve mode's own relevance score, present only when
	// IncludeScores was set AND the mode reports one (semantic does; symbol and prefix
	// do not). Joined to this node by entity ID, never by position.
	//
	// A POINTER so the wire is self-describing: absent means the mode does not score,
	// present means this is the score — including a genuine 0.0, which a bare
	// `float64,omitempty` would erase into indistinguishability from "unavailable".
	// A separate has_similarity bool round-trips correctly in Go but forces every
	// non-Go consumer to learn that an absent key means zero rather than nothing.
	Similarity *float64 `json:"similarity,omitempty"`
}

// Miss reports a query that resolved to nothing, with near-matches.
//
// A Miss is NOT an absence proof and is no longer tied to Ready (ADR-084): it is
// reachable under ordinary lag now that healthy-but-behind indexes serve, and it was
// never evidence the entity does not exist — only that this lookup found nothing.
// Callers that must distinguish "found nothing" from "could not read" want Unhydrated,
// which is a statement about the read.
type Miss struct {
	Query      string   `json:"query"`
	DidYouMean []string `json:"did_you_mean,omitempty"`
}

// Response is the fused answer. Nodes is the payload; Index and Provenance are
// the honesty envelope. Paths, Impact, and Graph are optional facets, present
// only when the request Wants them (WantPaths / WantImpact / WantGraph).
type Response struct {
	Index      IndexStatus `json:"index"`
	Provenance Provenance  `json:"provenance"`
	Nodes      []Node      `json:"nodes,omitempty"`
	Paths      []Path      `json:"paths,omitempty"`
	Impact     *Impact     `json:"impact,omitempty"`
	// Graph is the structured graph projection (WantGraph): typed property
	// facts, explicit directed edges, and verbatim evidence, with its own caps
	// and view-revision contract. Its truncation metadata is self-contained —
	// graph-facet truncation NEVER sets the top-level Truncated bit, and a
	// request without the graph want omits the field entirely (the v1 wire
	// shape is unchanged for non-requesting callers).
	Graph  *GraphProjection `json:"graph,omitempty"`
	Misses []Miss           `json:"misses,omitempty"`
	// Unhydrated names requested seeds that did not load — DISTINCT from Misses. A
	// Miss says "the graph was asked and had nothing"; an Unhydrated entry says "we
	// could not read this one", a statement about the read rather than about the
	// world. Neither licenses an absence conclusion (ADR-084), and the
	// all-seeds-unhydrated case deliberately synthesizes NO Miss: claiming a miss
	// there would assert exactly the thing the failed read left unknown.
	//
	// Omitted when everything hydrated, so a complete response is byte-unchanged.
	Unhydrated []Unhydrated `json:"unhydrated,omitempty"`
	// Deferred reports that the engine WITHHELD rather than answered: the empty
	// result is a refusal to look, not a finding.
	//
	// It is an explicit field because the honesty envelope cannot carry the fact.
	// Index describes the GRAPH-INDEX producer, and a defer can be caused by
	// something else entirely — an internal read against graph-ingest or
	// graph-embedding returning the readiness transient while graph-index is
	// perfectly healthy. Re-sampling graph-index then yields a HEALTHY envelope
	// attached to an empty response, so a consumer applying the canonical gate to
	// Index would conclude "healthy, and it found nothing" — precisely the
	// misreading ADR-084 exists to prevent, arrived at from the other side.
	//
	// Check this, not Index, to answer "did I get an answer?".
	Deferred bool `json:"deferred,omitempty"`
	// DeferReason is why, from graph.DeferReason's closed set plus the engine-level
	// causes (an internal dependency's readiness transient). Empty when not deferred.
	DeferReason     string `json:"defer_reason,omitempty"`
	Truncated       bool   `json:"truncated"`
	ContractVersion string `json:"contract_version"`
}

const (
	defaultMaxNodes = 20
	defaultMaxBytes = 60000
)

// wantSet expands a Want slice into a lookup set, applying defaults when empty.
func wantSet(wants []Want) map[Want]bool {
	if len(wants) == 0 {
		return map[Want]bool{WantBody: true, WantRelations: true}
	}
	set := make(map[Want]bool, len(wants))
	for _, w := range wants {
		set[w] = true
	}
	return set
}

// budgeter accumulates nodes up to the request budget, reporting truncation.
type budgeter struct {
	maxNodes, maxBytes, nodes, bytes int
}

// newBudget builds a budgeter from a request budget, applying defaults.
func newBudget(b Budget) *budgeter {
	bg := &budgeter{maxNodes: b.MaxNodes, maxBytes: b.MaxBytes}
	if bg.maxNodes <= 0 {
		bg.maxNodes = defaultMaxNodes
	}
	if bg.maxBytes <= 0 {
		bg.maxBytes = defaultMaxBytes
	}
	return bg
}

// admit reports whether a node carrying bodyBytes still fits, updating totals.
// At least one node is always admitted so a single oversized node is not dropped.
func (b *budgeter) admit(bodyBytes int) bool {
	if b.nodes >= b.maxNodes {
		return false
	}
	if b.nodes > 0 && b.bytes+bodyBytes > b.maxBytes {
		return false
	}
	b.nodes++
	b.bytes += bodyBytes
	return true
}
