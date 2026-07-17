package graphindex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
)

// nameQueryMaxHydration bounds how many NAME_INDEX memberships graph.query.byName will
// serially hydrate before refusing with ErrorCodeResourceExhausted (gh#474 Codex P2a
// interim guard). The bounded-parallel / paginated redesign is gh#381.
const nameQueryMaxHydration = 2000

// nameIndexKey is the NAME_INDEX KV key PREFIX for a name: the hex sha256 of the
// case-folded, trimmed name. Names contain arbitrary characters (spaces,
// punctuation, unicode) that are not KV-key-safe, so the index keys on a hash;
// the original-case name rides in the stored value for exact-case ranking.
// Folding the key gives case-insensitive recall.
//
// After composite-key sharding (gh#474, design.md D1), the full key for one
// membership is hash(name) + "." + entityID + "." + predicate. This function
// returns only the hash prefix — use nameCompositeKey for the full key and
// nameCompositePrefix for prefix scans.
func nameIndexKey(name string) string {
	sum := sha256.Sum256([]byte(normalizeName(name)))
	return hex.EncodeToString(sum[:])
}

// normalizeName case-folds and trims a name for case-insensitive matching.
func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// nameCompositeKey builds the full NAME_INDEX composite key for one
// (name, entityID, predicate) membership:
// key = hash(name) + "." + entityID + "." + hex(predicate).
//
// The predicate is hex-encoded (encodePredicateToken, gh#474 Codex P1a) for the
// same KV-safety reason as INCOMING; the reader decodes it back in nameEntryFromKey.
//
// Unconditional Put, no CAS — ADR-065 footgun comment: each (name, entityID,
// predicate) triple is globally unique; concurrent writers on different triples
// never contend on the same key. The CAS UpdateWithRetry this replaces cost an
// extra round-trip per name-write; with composite keys the write is a single Put.
func nameCompositeKey(nameHash, entityID, predicate string) string {
	return nameHash + "." + entityID + "." + encodePredicateToken(predicate)
}

// nameCompositePrefix builds the KeysByPrefix argument that enumerates every
// entity currently carrying a given name (case-insensitive, folded to the same
// hash).
func nameCompositePrefix(name string) string {
	return nameIndexKey(name) + "."
}

// nameIndexEntityFilter enumerates every name membership owned by entityID:
// hash(name).entity6.hex(predicate).
func nameIndexEntityFilter(entityID string) string {
	return "*." + entityID + ".*"
}

// nameCompositeValue is the JSON value stored at each NAME_INDEX composite key.
// The original-case name and predicate priority are NOT recoverable from the
// hashed prefix alone, so they ride in the value.
type nameCompositeValue struct {
	Name     string `json:"name"`     // original-case name as stored on the entity
	Priority int    `json:"priority"` // label-predicate salience (lower = higher)
}

type nameIndexWrite struct {
	name      string
	predicate string
	priority  int
}

func (c *Component) reconcileNameIndex(ctx context.Context, entityID string, writes []nameIndexWrite) error {
	if err := semtypes.ValidateEntityID(entityID); err != nil {
		return err
	}
	desired := make(map[string][]byte, len(writes))
	for _, write := range writes {
		if write.name == "" || !validateNameKeyInputs(entityID, write.predicate, c.logger) {
			return errs.WrapInvalid(errs.ErrInvalidData, "Component", "reconcileNameIndex",
				"name membership contains an invalid name, entity ID, or predicate")
		}
		value, err := json.Marshal(nameCompositeValue{Name: write.name, Priority: write.priority})
		if err != nil {
			return errs.Wrap(err, "Component", "reconcileNameIndex", "marshal value")
		}
		desired[nameCompositeKey(nameIndexKey(write.name), entityID, write.predicate)] = value
	}

	// Overwrite retained keys because original-case spelling or priority lives in
	// the value and may change while the normalized composite key remains stable.
	return c.reconcileOwnedRows(ctx, "name", c.nameBucket,
		nameIndexEntityFilter(entityID), desired, true)
}

// nameEntryFromKey extracts entityID and predicate from a NAME_INDEX composite
// key. The key format is "hash(name).entityID.predicate", where the hash prefix
// is already known (stripped by nameCompositePrefix). entityID is exactly 6
// dot-separated tokens; predicate is everything after the 6th token.
// Returns ("", "", false) when the key is malformed.
func nameEntryFromKey(key, nameHash string) (entityID, predicate string, ok bool) {
	prefix := nameHash + "."
	if !strings.HasPrefix(key, prefix) {
		return "", "", false
	}
	suffix := key[len(prefix):]

	// suffix = "entityID.hex(predicate)"; entityID is exactly 6 dot-separated
	// tokens; the hex predicate token is dot-free by construction.
	parts := strings.SplitN(suffix, ".", 7)
	if len(parts) < 7 {
		return "", "", false
	}
	entityID = strings.Join(parts[:6], ".")
	predicate, decoded := decodePredicateToken(parts[6])
	if !decoded || predicate == "" {
		return "", "", false
	}
	if err := semtypes.ValidateEntityID(entityID); err != nil {
		return "", "", false
	}
	if _, err := vocabulary.ParsePredicate(predicate); err != nil {
		return "", "", false
	}
	return entityID, predicate, true
}

// validateNameKeyInputs checks that entityID and predicate are valid for
// constructing a NAME_INDEX composite key. Returns false and logs at Debug on
// failure. Structural, mirroring validateIncomingKeyInputs / validateContextKeyInputs
// (design.md D1): an invalid entity ID or empty predicate would produce a key the
// reader silently rejects (data loss) or mis-splits (silent corruption) — exactly
// the class this change closes, so the write must skip rather than store it.
func validateNameKeyInputs(entityID, predicate string, logger *slog.Logger) bool {
	if !message.IsValidEntityID(entityID) {
		logger.Debug("name index: invalid entity ID, skipping",
			slog.String("entity_id", entityID))
		return false
	}
	if predicate == "" {
		logger.Debug("name index: empty predicate would produce trailing-dot key, skipping",
			slog.String("entity_id", entityID))
		return false
	}
	if _, err := vocabulary.ParsePredicate(predicate); err != nil {
		logger.Debug("name index: invalid predicate, skipping",
			slog.String("entity_id", entityID), slog.Any("error", err))
		return false
	}
	return true
}

// UpdateNameIndex records that entityID carries name under the given label
// predicate. Writes one composite-key entry; de-duplication by (entityID,
// predicate) is automatic — the same (name, entityID, predicate) triple always
// maps to the same composite key, and unconditional Put is idempotent.
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

	// Structural key-input guard (design.md D1 MUST): skip-log a malformed entity
	// ID or empty predicate rather than store a key the reader would reject or
	// mis-split. Symmetric with INCOMING (validateIncomingKeyInputs) and CONTEXT.
	if !validateNameKeyInputs(entityID, predicate, c.logger) {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdateNameIndex",
			"invalid entity ID or predicate")
	}

	nameHash := nameIndexKey(name)
	key := nameCompositeKey(nameHash, entityID, predicate)
	if err := natsclient.ValidateKVLiteralKey(key); err != nil {
		return err
	}

	value, err := json.Marshal(nameCompositeValue{
		Name:     name,
		Priority: priority,
	})
	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "UpdateNameIndex", "marshal value")
	}

	if _, err := c.nameBucket.Put(ctx, key, value); err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "UpdateNameIndex", "KV put")
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
// as an authoritative not-found). The Keys() check remains valid after
// composite-key sharding: composite keys still exist and still have len>0.
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
//
// After composite-key sharding (gh#474), the reader scans the prefix
// hash(name).">" to enumerate all (entityID, predicate) pairs, then fetches each
// entry's value for original-case name and priority. The wire response type
// (graph.NameMatch, graph.NameData) is unchanged.
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

	nameHash := nameIndexKey(req.Name)
	prefix := nameCompositePrefix(req.Name)
	if err := validateKVPrefix(prefix); err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest, err)
	}
	if err := c.ensureQueryReady(ctx); err != nil {
		return nil, err
	}

	keys, err := c.nameBucket.KeysByPrefix(ctx, prefix)
	if err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, errors.New("internal error"))
	}
	// Filtered listings are watcher snapshots and may repeat a key concurrent with a
	// Put. Deduplicate before applying the hydration budget so duplicates cannot
	// spuriously exhaust it or trigger redundant serial Gets.
	keys = uniqueSortedStrings(keys)
	// Hard read budget (gh#474 Codex P2a interim): this handler does one serial Get per
	// membership below, so a name shared by a huge number of entities is an unbounded
	// N+1 under the 5s timeout. Refuse with a typed resource-exhausted error rather than
	// grind; the caller narrows the query. The bounded-parallel/paginated redesign is gh#381.
	if len(keys) > nameQueryMaxHydration {
		return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeResourceExhausted,
			fmt.Errorf("name lookup exceeds read budget: %d memberships > %d", len(keys), nameQueryMaxHydration))
	}
	if len(keys) == 0 {
		// No entries — name not yet indexed or bucket empty.
		return json.Marshal(graph.NewQueryResponse(graph.NameData{Matches: []graph.NameMatch{}}))
	}

	// Reconstruct NameIndexItems from composite keys + per-key values.
	items := make([]graph.NameIndexItem, 0, len(keys))
	for _, key := range keys {
		entityID, predicate, ok := nameEntryFromKey(key, nameHash)
		if !ok {
			c.logger.Debug("name index: skipping malformed composite key",
				slog.String("key", key))
			continue
		}

		entry, err := c.nameBucket.Get(ctx, key)
		if err != nil {
			if natsclient.IsKVNotFoundError(err) {
				continue // concurrently deleted — skip
			}
			return nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, errors.New("internal error"))
		}

		var v nameCompositeValue
		if unmarshalErr := json.Unmarshal(entry.Value, &v); unmarshalErr != nil {
			c.logger.Debug("name index: skipping malformed value",
				slog.String("key", key),
				slog.Any("error", unmarshalErr))
			continue
		}

		items = append(items, graph.NameIndexItem{
			EntityID:  entityID,
			Name:      v.Name,
			Predicate: predicate,
			Priority:  v.Priority,
		})
	}

	matches := rankNameMatches(items, req.Name, req.Limit)
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
