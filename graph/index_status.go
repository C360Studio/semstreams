package graph

// IndexStatusResponse is the wire shape of graph.index.query.status (gh#397):
// the deterministic-fusion honesty envelope's readiness signal.
//
// Ready is load-bearing — only Ready permits a caller to treat an empty result
// as an authoritative not-found; when not Ready the caller must fall back (e.g.
// to grep) rather than conclude absence. Today Ready means the NAME_INDEX (the
// index deterministic symbol/prefix resolution depends on) has been populated
// (non-empty); a not-yet-populated index can't distinguish "absent" from
// "not-yet-indexed", so it reports not-ready (ADR-062 / gh#397).
//
// The JSON field names match pkg/fusion.IndexStatus so the fusion
// RetrievalClient decodes this response directly into its honesty envelope.
type IndexStatusResponse struct {
	Ready      bool   `json:"ready"`
	State      string `json:"state"` // "building" | "ready" | "degraded"
	Revision   string `json:"revision,omitempty"`
	LastSynced string `json:"last_synced,omitempty"`
}

// Index readiness states. Mirrors pkg/fusion.IndexState string values.
const (
	IndexStateBuilding = "building"
	IndexStateReady    = "ready"
	IndexStateDegraded = "degraded"
)
