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
	// Status reports graph readiness. The honesty envelope's Ready flag is
	// load-bearing — only Ready permits a not-found conclusion.
	Status(ctx context.Context) (IndexStatus, error)
	// Resolve maps a query to seed entity IDs, most relevant first. Its
	// arguments are a struct rather than positional so the NL-only Scope does
	// not force symbol/prefix callers to pass an ignored value, and so a future
	// resolve dimension adds a field instead of re-breaking every impl and fake
	// (ADR-071).
	Resolve(ctx context.Context, q ResolveQuery) ([]string, error)
	// Entity returns an entity by ID, or (nil, nil) if absent.
	Entity(ctx context.Context, id string) (*Entity, error)
	// Entities batch-fetches entities by ID. Absent IDs are omitted; a non-nil
	// error means a BACKEND failure, which callers MUST distinguish from genuine
	// absence (an empty result on a Ready graph is a miss, not a fault).
	Entities(ctx context.Context, ids []string) ([]*Entity, error)
	// Neighbors returns edges from id along the given predicates in a direction.
	Neighbors(ctx context.Context, id string, predicates []string, dir Direction) ([]Edge, error)
	// Names suggests entity display names near a query (for a miss's did_you_mean).
	Names(ctx context.Context, query string, limit int) ([]string, error)
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
