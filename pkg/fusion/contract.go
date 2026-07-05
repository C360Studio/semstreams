package fusion

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

// The readiness phases. Only Ready permits a not-found conclusion.
const (
	StateBuilding IndexState = "building"
	StateReady    IndexState = "ready"
	StateDegraded IndexState = "degraded"
)

// IndexStatus is attached to every response. Ready is load-bearing: when false
// the caller must fall back (e.g. to grep) rather than treat empty as not-found.
// Ready now means the index is CAUGHT UP (revision-lag), not merely started
// (ADR-066). Field-identical to graph.IndexStatusResponse — the RetrievalClient
// decodes graph.index.query.status directly into this; the two change together.
type IndexStatus struct {
	Ready bool       `json:"ready"`
	State IndexState `json:"state"`
	// IndexedRevision / TargetRevision / Lag expose the exact revision-lag so a
	// caller that knows its own target revision can gate on IndexedRevision >=
	// myRev rather than the coarse global Ready bool (ADR-066).
	IndexedRevision uint64 `json:"indexed_revision,omitempty"`
	TargetRevision  uint64 `json:"target_revision,omitempty"`
	Lag             uint64 `json:"lag,omitempty"`
	Phase           string `json:"phase,omitempty"`
	Revision        string `json:"revision,omitempty"`
	LastSynced      string `json:"last_synced,omitempty"`
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
}

// Miss reports a query that resolved to nothing while the graph was ready, with
// near-matches. A Miss only appears when Ready is true.
type Miss struct {
	Query      string   `json:"query"`
	DidYouMean []string `json:"did_you_mean,omitempty"`
}

// Response is the fused answer. Nodes is the payload; Index and Provenance are
// the honesty envelope. Paths and Impact are optional facets, present only when
// the request Wants them (WantPaths / WantImpact).
type Response struct {
	Index           IndexStatus `json:"index"`
	Provenance      Provenance  `json:"provenance"`
	Nodes           []Node      `json:"nodes,omitempty"`
	Paths           []Path      `json:"paths,omitempty"`
	Impact          *Impact     `json:"impact,omitempty"`
	Misses          []Miss      `json:"misses,omitempty"`
	Truncated       bool        `json:"truncated"`
	ContractVersion string      `json:"contract_version"`
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
