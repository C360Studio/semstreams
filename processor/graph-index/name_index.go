package graphindex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// nameIndexKey is the NAME_INDEX KV key for a name: the hex sha256 of the
// case-folded, trimmed name. Names contain arbitrary characters (spaces,
// punctuation, unicode) that are not KV-key-safe, so the index keys on a hash;
// the original-case name rides in the stored value for exact-case ranking.
// Folding the key gives case-insensitive recall.
func nameIndexKey(name string) string {
	sum := sha256.Sum256([]byte(normalizeName(name)))
	return hex.EncodeToString(sum[:])
}

// normalizeName case-folds and trims a name for case-insensitive matching.
func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// UpdateNameIndex records that entityID carries name under the given label
// predicate. CAS read-modify-write; de-duplicates by (entityID, predicate) so
// re-indexing the same entity is idempotent. Multiple entities may share a name
// (names are not unique) — all are kept and ranked at query time.
func (c *Component) UpdateNameIndex(ctx context.Context, name, entityID, predicate string, priority int) error {
	if name == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdateNameIndex", "name cannot be empty")
	}
	if entityID == "" {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdateNameIndex", "entity ID cannot be empty")
	}
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdateNameIndex", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "UpdateNameIndex", "context cancelled")
	}

	key := nameIndexKey(name)
	err := c.nameBucket.UpdateWithRetry(ctx, key, func(current []byte) ([]byte, error) {
		var entry graph.NameIndexEntry
		if len(current) > 0 {
			if unmarshalErr := json.Unmarshal(current, &entry); unmarshalErr != nil {
				entry = graph.NameIndexEntry{}
			}
		}
		entry.Name = normalizeName(name)

		// De-dup by (entityID, predicate): an entity carrying the same name under
		// the same predicate updates in place (refresh original-case + priority);
		// the same name under a different predicate is a distinct item.
		for i := range entry.Items {
			if entry.Items[i].EntityID == entityID && entry.Items[i].Predicate == predicate {
				entry.Items[i].Name = name
				entry.Items[i].Priority = priority
				return json.Marshal(entry)
			}
		}
		entry.Items = append(entry.Items, graph.NameIndexItem{
			EntityID:  entityID,
			Name:      name,
			Predicate: predicate,
			Priority:  priority,
		})
		return json.Marshal(entry)
	})
	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "UpdateNameIndex", "CAS update")
	}

	atomic.AddInt64(&c.messagesProcessed, 1)
	c.lastActivity.Store(time.Now())
	// gh#397: the NAME_INDEX now has at least this entry — mark ready (sticky).
	c.nameIndexReady.Store(true)
	if c.metrics != nil {
		c.metrics.recordIndexUpdate("name")
		c.metrics.recordKVOperation("put", "name")
	}
	return nil
}

// nameIndexIsReady reports whether the NAME_INDEX has been populated (gh#397).
// Sticky-fast once the index is known non-empty; otherwise it does a one-time
// bucket scan, which handles restart with a pre-populated index (the in-memory
// sticky flag starts false after a restart). An index does not un-build, so the
// flag never flips back to false. Any list error — including an empty bucket
// (ErrNoKeysFound) or a transient backend fault — reports NOT ready, the
// conservative honest answer (the caller must fall back rather than treat empty
// as an authoritative not-found).
func (c *Component) nameIndexIsReady(ctx context.Context) bool {
	if c.nameIndexReady.Load() {
		return true
	}
	keys, err := c.nameBucket.Keys(ctx)
	if err != nil || len(keys) == 0 {
		return false
	}
	c.nameIndexReady.Store(true)
	return true
}

// handleQueryStatusNATS serves graph.index.query.status (gh#397, enriched by
// ADR-066): the deterministic-fusion honesty-envelope readiness signal. Ready now
// means the index is CAUGHT UP (revision-lag: IndexedRevision >= query-time
// TargetRevision), not merely "indexing started" (the old sticky NAME_INDEX signal
// that fired minutes early, gh#431). Takes no request body; the response JSON shape
// matches pkg/fusion.IndexStatus.
func (c *Component) handleQueryStatusNATS(ctx context.Context, _ []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	data, err := json.Marshal(c.computeIndexStatus(ctx))
	if err != nil {
		// Unreachable (an all-scalar struct never fails to marshal), but classify it
		// like every sibling handler so a caller decoding the reply cannot mistake an
		// error body for a zero-value success status.
		return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, errors.New("internal error"))
	}
	return data, nil
}

// handleQueryByNameNATS resolves a name to ranked entity IDs (gh#376). Request
// {name, limit}; response graph.NameData with matches ordered exact-case-first,
// then by label-predicate salience, then entity ID. Empty matches (not an error)
// when the name is unknown — the caller distinguishes ready-but-absent from a
// backend failure (the latter is a classified error).
func (c *Component) handleQueryByNameNATS(ctx context.Context, data []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var req struct {
		Name  string `json:"name"`
		Limit int    `json:"limit,omitempty"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest, errors.New("invalid request"))
	}
	if req.Name == "" {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest, errors.New("invalid request: empty name"))
	}

	entry, err := c.nameBucket.Get(ctx, nameIndexKey(req.Name))
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			return json.Marshal(graph.NewQueryResponse(graph.NameData{Matches: []graph.NameMatch{}}))
		}
		return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, errors.New("internal error"))
	}

	var stored graph.NameIndexEntry
	if unmarshalErr := json.Unmarshal(entry.Value, &stored); unmarshalErr != nil {
		return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, errors.New("internal error"))
	}

	matches := rankNameMatches(stored.Items, req.Name, req.Limit)
	return json.Marshal(graph.NewQueryResponse(graph.NameData{Matches: matches}))
}

// nameRank pairs a wire match with its label-predicate priority for ordering.
type nameRank struct {
	match    graph.NameMatch
	priority int
}

// nameRankLess is the total order for name ranking, used both to pick an
// entity's best item and to sort the final result: exact-case before fold-only,
// then lower predicate priority (higher salience), then entity ID ascending.
func nameRankLess(a, b nameRank) bool {
	if a.match.ExactCase != b.match.ExactCase {
		return a.match.ExactCase // exact-case first
	}
	if a.priority != b.priority {
		return a.priority < b.priority // lower priority = higher salience
	}
	return a.match.EntityID < b.match.EntityID
}

// rankNameMatches orders stored items for a query: exact-case match first, then
// label-predicate salience (lower priority = higher), then entity ID for a
// deterministic tiebreak. Collapses to one match per entity (its best-ranked
// item), then applies limit (<=0 means unbounded).
func rankNameMatches(items []graph.NameIndexItem, query string, limit int) []graph.NameMatch {
	best := make(map[string]nameRank)
	for _, it := range items {
		cand := nameRank{
			match: graph.NameMatch{
				EntityID:    it.EntityID,
				MatchedName: it.Name,
				Predicate:   it.Predicate,
				ExactCase:   it.Name == query,
			},
			priority: it.Priority,
		}
		if cur, seen := best[it.EntityID]; !seen || nameRankLess(cand, cur) {
			best[it.EntityID] = cand
		}
	}

	out := make([]nameRank, 0, len(best))
	for _, r := range best {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return nameRankLess(out[i], out[j]) })

	matches := make([]graph.NameMatch, 0, len(out))
	for _, r := range out {
		matches = append(matches, r.match)
	}
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	return matches
}
