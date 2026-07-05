package fusion

import "context"

// Engine-computed Paths/Impact facets (ADR-062 gh#409). These are the outgoing/
// incoming siblings of the relations facet the engine already owns: same bounded
// RetrievalClient.Neighbors walk, generalized across lenses so a Want{Paths} or
// Want{Impact} is honored the way WantRelations is — retiring the per-consumer
// impact extensions (semsource's source/fusion/impact).

// Path is a bounded outgoing relation walk from a seed, rendered as a sequence
// of human names (never entity IDs), carried on Response.Paths. Truncated marks a
// path cut short at the depth cap or a cycle — the partial path is kept, not
// dropped.
type Path struct {
	Names     []string `json:"names"`
	Truncated bool     `json:"truncated,omitempty"`
}

// Impact summarizes the transitive reverse-relation closure of the response's
// seeds — the blast radius of changing them — carried on Response.Impact. It
// counts distinct affected nodes and files (a lens Location path); Truncated
// marks the node cap was hit, so the counts are a lower bound.
type Impact struct {
	Nodes     int  `json:"nodes"`
	Files     int  `json:"files"`
	Truncated bool `json:"truncated"`
}

// Facet bounds keep the walks cheap and predictable.
const (
	// maxImpactNodes caps the reverse-closure BFS; beyond it Impact.Truncated is
	// set and the counts are a lower bound.
	maxImpactNodes = 200
	// maxPaths caps the total number of outgoing paths emitted.
	maxPaths = 10
	// maxPathDepth caps a path's length in hops (a path has up to maxPathDepth+1
	// names).
	maxPathDepth = 4
)

// computeImpact returns the transitive reverse-relation closure of the seeds —
// the blast radius of changing them — as distinct affected-node and file counts.
// It BFSes Neighbors(…, Incoming) along the lens's edge predicates, excluding the
// seeds themselves, bounded by maxImpactNodes. Best-effort: a Neighbors/Entity
// error prunes that branch rather than failing the fuse.
func (e *Engine) computeImpact(ctx context.Context, seeds []*Entity, lens Lens) *Impact {
	preds, _, _ := edgePredicates(lens.Edges(), FacetImpact)
	imp := &Impact{}
	if len(preds) == 0 {
		return imp
	}

	seedSet := make(map[string]bool, len(seeds))
	frontier := make([]string, 0, len(seeds))
	for _, s := range seeds {
		seedSet[s.ID] = true
		frontier = append(frontier, s.ID)
	}

	affected := map[string]bool{}
	files := map[string]bool{}
	degraded := false
	for len(frontier) > 0 {
		var next []string
		for _, id := range frontier {
			edges, ok := e.neighbors(ctx, id, preds, Incoming)
			if !ok {
				degraded = true // a walk fault leaves the closure incomplete
			}
			for _, ed := range edges {
				dep := ed.Target // Incoming target is the node pointing AT id
				if seedSet[dep] || affected[dep] {
					continue
				}
				if len(affected) >= maxImpactNodes {
					imp.Nodes, imp.Files, imp.Truncated = len(affected), len(files), true
					return imp
				}
				affected[dep] = true
				if p := e.pathOf(ctx, dep, lens); p != "" {
					files[p] = true
				}
				next = append(next, dep)
			}
		}
		frontier = next
	}
	// Truncated is the honesty signal that counts are a lower bound — set it on
	// either the node cap OR a walk fault (degrade-don't-fail must not masquerade
	// as an exact closure).
	imp.Nodes, imp.Files, imp.Truncated = len(affected), len(files), degraded
	return imp
}

// computePaths returns up to maxPaths bounded outgoing relation paths from the
// seeds, each a sequence of human names. DFS over Neighbors(…, Outgoing) along
// the lens's edge predicates, depth-capped; a cycle truncates that branch (kept,
// not dropped).
func (e *Engine) computePaths(ctx context.Context, seeds []*Entity, lens Lens) []Path {
	preds, _, _ := edgePredicates(lens.Edges(), FacetPaths)
	if len(preds) == 0 {
		return nil
	}
	var out []Path
	for _, s := range seeds {
		if len(out) >= maxPaths {
			break
		}
		e.dfsPaths(ctx, s.ID, lens, preds, []string{lens.Label(s)}, map[string]bool{s.ID: true}, &out)
	}
	return out
}

// dfsPaths walks outgoing edges from id, emitting a Path at each terminal: a leaf
// (no forward edges), the depth cap, or a branch whose only continuations are
// cycles (emitted Truncated). Forward edges are followed; cycle edges are not
// (no infinite loop), so a node with both forward and cycle edges follows the
// forward ones without a spurious truncated emit.
func (e *Engine) dfsPaths(ctx context.Context, id string, lens Lens, preds []string, path []string, onPath map[string]bool, out *[]Path) {
	if len(*out) >= maxPaths {
		return
	}
	if len(path)-1 >= maxPathDepth { // depth cap in hops
		*out = append(*out, Path{Names: clonePath(path), Truncated: true})
		return
	}

	edges, ok := e.neighbors(ctx, id, preds, Outgoing)
	if !ok {
		// Walk fault mid-path: keep the partial path, marked truncated (honest
		// about incompleteness rather than emitting it as a complete leaf).
		if len(path) > 1 {
			*out = append(*out, Path{Names: clonePath(path), Truncated: true})
		}
		return
	}

	var forward []Edge
	cycle := false
	for _, ed := range edges {
		if onPath[ed.Target] {
			cycle = true
			continue
		}
		forward = append(forward, ed)
	}

	if len(forward) == 0 {
		// Leaf, or a branch whose only continuations loop. Don't emit a
		// single-name path (the seed alone is not a relation path).
		if len(path) > 1 {
			*out = append(*out, Path{Names: clonePath(path), Truncated: cycle})
		}
		return
	}

	for _, ed := range forward {
		if len(*out) >= maxPaths {
			return
		}
		name := e.nameOf(ctx, ed.Target, lens)
		onPath[ed.Target] = true
		e.dfsPaths(ctx, ed.Target, lens, preds, append(clonePath(path), name), onPath, out)
		onPath[ed.Target] = false
	}
}

// neighbors returns the edges from id in a direction and whether the fetch
// succeeded. A fault yields (nil, false) — the caller prunes the walk rather
// than failing the fuse (degrade-don't-fail), but records the incompleteness in
// the facet's Truncated flag so a partial result is never reported as exact.
func (e *Engine) neighbors(ctx context.Context, id string, preds []string, dir Direction) ([]Edge, bool) {
	edges, err := e.graph.Neighbors(ctx, id, preds, dir)
	if err != nil {
		return nil, false
	}
	return edges, true
}

// nameOf resolves an entity ID to its human name via the lens, falling back to
// the ID when the entity can't be fetched.
func (e *Engine) nameOf(ctx context.Context, id string, lens Lens) string {
	if te, err := e.graph.Entity(ctx, id); err == nil && te != nil {
		return lens.Label(te)
	}
	return id
}

// pathOf resolves an entity ID to its lens Location path, or "" when absent.
func (e *Engine) pathOf(ctx context.Context, id string, lens Lens) string {
	if te, err := e.graph.Entity(ctx, id); err == nil && te != nil {
		return lens.Location(te).Path
	}
	return ""
}

// clonePath copies a name slice so appends on recursive branches don't alias.
func clonePath(p []string) []string {
	return append([]string(nil), p...)
}
