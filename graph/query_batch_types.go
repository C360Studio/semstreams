package graph

// Batch entity-query wire types (`graph.ingest.query.batch`, lifted to the public
// surface as `graph.query.batch`).
//
// Before ADR-084 the batch response was `{"entities":[…]}` and an ID whose KV read
// came back not-found was simply absent from that list. A caller could not tell an
// entity that does not exist from one the read failed to reach, and — because the
// list carries no correspondence to the requested order or membership — could not even
// tell WHICH id was missing without diffing the sets itself. Nothing did. That is the
// confirmed drop path in gh#597: a top-ranked seed vanished between resolve and
// hydration and the response looked like an ordinary shorter list.
//
// Reporting omission does NOT license the inverse inference. `missing` with reason
// `not_found` says this read did not find the key; it does not say the entity never
// existed, and no caller may treat it as an absence proof (ADR-084: readiness licenses
// health, never absence). It exists so partial hydration is VISIBLE — the bug was
// invisibility, not partiality.

// MissingReason is why one requested ID is absent from a batch response. The set is
// CLOSED so a consumer can switch on it exhaustively.
type MissingReason string

// The closed missing-reason set.
const (
	// MissingNotFound is a KV read that returned not-found for the key — the
	// overwhelmingly common case.
	MissingNotFound MissingReason = "not_found"
	// MissingError is a per-ID fault that did not fail the whole call. The first-error
	// contract still stands for READ faults — a non-not-found backend error fails the
	// batch — so the only current emitter is a malformed requested ID (the empty
	// string), which is never looked up at all and therefore cannot honestly be called
	// not_found.
	MissingError MissingReason = "error"
	// MissingUnknown is synthesized CLIENT-side for a requested ID that appears in
	// neither the entities nor the missing list. A handler never emits it; it exists
	// so a client's reconciliation can be total (one entry per requested ID) without
	// inventing a reason it does not have.
	MissingUnknown MissingReason = "unknown"
)

// MissingEntity names one requested ID that is not in the response's entity list.
type MissingEntity struct {
	ID     string        `json:"id"`
	Reason MissingReason `json:"reason"`
}

// EntityBatchResponse is the batch-query reply. Entities keeps its field name and
// shape, so a consumer reading only `entities` is unaffected; `missing` is additive and
// omitempty, so a fully-hydrated batch is byte-identical to the pre-ADR-084 response.
type EntityBatchResponse struct {
	Entities []EntityState `json:"entities"`
	// Missing names the requested IDs not present in Entities, with a reason each.
	// Omitted entirely when nothing was missing.
	Missing []MissingEntity `json:"missing,omitempty"`
}
