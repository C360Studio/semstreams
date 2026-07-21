package fusion

import "context"

// RetrievalClient is the resolve/expand surface the lens-driven Engine composes
// over (ADR-062 lens-driven entry). It is DISTINCT from GraphQueryClient (the
// sub-query executor surface the package-level Fuse consumes): this one maps a
// query to seeds and walks structure, whereas GraphQueryClient runs pre-built
// sub-queries. The ADR's eventual convergence unifies them; for now they serve
// the two engine entries side by side.
//
// Production wraps NATS request/reply (graph.query.{status,byName,prefix,
// semantic,batch,relationships,entity}); tests use an in-memory fake. Keeping the
// engine behind this interface is what lets it stay deterministic and
// unit-testable without a live graph (the production impl + the readiness status
// subject are PR B / gh#397).
type RetrievalClient interface {
	// Status reports graph readiness for the honesty envelope. Callers gate on
	// HEALTH (graph.EvaluateReadinessGate), not on the Ready coverage bit — which
	// never licensed a not-found conclusion and no longer withholds a response
	// (ADR-084). A quiet or unvouchable feed returns ErrReadinessUnknown; a wiring
	// failure returns a plain error.
	Status(ctx context.Context) (IndexStatus, error)
	// Resolve maps a query to seed entity IDs, most relevant first. Its
	// arguments are a struct rather than positional so the NL-only Scope does
	// not force symbol/prefix callers to pass an ignored value, and so a future
	// resolve dimension adds a field instead of re-breaking every impl and fake
	// (ADR-071).
	Resolve(ctx context.Context, q ResolveQuery) ([]string, error)
	// Entity returns an entity by ID, or (nil, nil) if the read found nothing.
	// "Found nothing" is not proof of absence — see Hydration.
	Entity(ctx context.Context, id string) (*Entity, error)
	// Entities batch-fetches entities by ID and reports what it could not hydrate.
	// A non-nil error means a BACKEND failure, which callers MUST distinguish from
	// an entity that is simply absent.
	//
	// It returns a struct rather than a bare slice for the same reason Resolve takes
	// one (see above): partial hydration needed a second output, and a future one
	// should add a field instead of re-breaking every impl and fake.
	//
	// IMPLEMENTATIONS MUST return Entities in the REQUESTED ORDER. The engine's
	// resolve-rank base is position-derived, so hydration order is the ranking prior,
	// not a presentation detail — a transport that returns "whatever order was
	// convenient" silently reorders results by cache residency (see
	// fusionnats.Client.Entities, which restores order for exactly this reason).
	Entities(ctx context.Context, ids []string) (Hydration, error)
	// Neighbors returns edges from id along the given predicates in a direction.
	Neighbors(ctx context.Context, id string, predicates []string, dir Direction) ([]Edge, error)
	// Names suggests entity display names near a query (for a miss's did_you_mean).
	Names(ctx context.Context, query string, limit int) ([]string, error)
}

// Hydration is a batch fetch's outcome: the entities that loaded, in requested order,
// plus every requested ID that did not, with a reason.
//
// The two lists together account for every requested ID exactly once. That totality is
// the point: before it, an ID whose read came back not-found was simply missing from a
// shorter slice, and no caller could tell which one — or whether anything had gone
// wrong at all (gh#597).
type Hydration struct {
	// Entities are the hydrated entities IN REQUESTED ORDER.
	Entities []*Entity
	// Unhydrated names every requested ID absent from Entities. Nil when complete.
	Unhydrated []Unhydrated
}

// UnhydratedReason is why one requested ID did not hydrate. The set is CLOSED and
// mirrors graph.MissingReason value-for-value (pinned by a test) — fusion keeps its own
// type because it is the product-facing contract and its wire name differs
// (`unhydrated` here, `missing` on the batch subject).
type UnhydratedReason string

// The closed unhydrated-reason set.
const (
	// UnhydratedNotFound is a read that did not find the key. It does NOT license the
	// conclusion that the entity never existed — see the Response.Unhydrated docs.
	UnhydratedNotFound UnhydratedReason = "not_found"
	// UnhydratedError is a per-ID fault that did not fail the whole call. Reserved
	// while the handler's first-error contract stands.
	UnhydratedError UnhydratedReason = "error"
	// UnhydratedUnknown is synthesized when a requested ID appears in neither the
	// handler's entity list nor its missing list — a handler that under-reported.
	// Naming it beats inventing not_found, which would assert something unobserved.
	UnhydratedUnknown UnhydratedReason = "unknown"
)

// Unhydrated names one requested ID that did not load.
type Unhydrated struct {
	ID     string           `json:"id"`
	Reason UnhydratedReason `json:"reason"`
}

// ResolveQuery is the argument set for RetrievalClient.Resolve. Mode selects
// the resolve strategy; Scope is honored only for ResolveModeNL (a filter on
// embedding candidates), where empty/nil means no filter.
type ResolveQuery struct {
	Query string
	Mode  ResolveMode
	Scope []string
	Limit int
}

// Direction selects edge traversal direction.
type Direction int

// Outgoing follows a subject's predicates to targets; Incoming follows the
// reverse (who points at this entity).
const (
	Outgoing Direction = iota
	Incoming
)

// Edge is a relationship from a subject entity to a target entity.
type Edge struct {
	Predicate string
	Target    string
}
