package operatingmodel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
)

// defaultLessonsLimit caps how many lessons GraphProfileReader.ReadLessons
// returns when the caller passes limit <= 0. Bounds the KV-traversal cost
// for users that accumulate a long lesson history; the assembler typically
// only renders 5–10 anyway.
const defaultLessonsLimit = 50

// kvGetter is the minimal slice of natsclient.KVStore that GraphProfileReader
// needs. Narrowing the dependency to this interface lets unit tests inject a
// fake without spinning up NATS.
type kvGetter interface {
	Get(ctx context.Context, key string) (*natsclient.KVEntry, error)
}

// GraphProfileReader implements ProfileReader by querying the ENTITY_STATES
// KV bucket for operating-model entities.
//
// It traverses the graph via the relationship triples written by graph-ingest:
//
//	profile entity --user.operating_model.has_layer--> layer entity
//	layer entity   --om.layer.has_entry--> entry entity
//
// All reads are scoped under the requested user's profile entity, which is
// itself user-scoped by ID. This guarantees one user's call cannot return
// another user's entries.
//
// All KV operations go through semstreams' natsclient.KVStore wrapper.
type GraphProfileReader struct {
	kv     kvGetter
	logger *slog.Logger
}

// NewGraphProfileReader creates a reader backed by the named KV bucket.
// The caller passes the natsclient.Client and bucket name; the reader opens
// the bucket and wraps it in a KVStore. Returns an error if the bucket
// doesn't exist or isn't reachable.
func NewGraphProfileReader(ctx context.Context, nc *natsclient.Client, bucketName string, logger *slog.Logger) (*GraphProfileReader, error) {
	bucket, err := nc.GetKeyValueBucket(ctx, bucketName)
	if err != nil {
		return nil, fmt.Errorf("open KV bucket %q: %w", bucketName, err)
	}
	kv := nc.NewKVStore(bucket)
	if logger == nil {
		logger = slog.Default()
	}
	return &GraphProfileReader{kv: kv, logger: logger}, nil
}

// ReadProfileVersion implements ProfileReader. It loads only the user's
// profile entity (one KV get) and returns the user.operating_model.version
// triple. Returns (0, nil) when the profile does not exist yet, and
// (0, error) when the KV get returns a transport error so callers can
// distinguish "no prior profile" from "graph unavailable" (the dispatch
// re-run path uses the err signal to log a warning and fall back to
// version 1, instead of silently picking 1 on a real outage).
func (r *GraphProfileReader) ReadProfileVersion(ctx context.Context, org, platform, userID string) (int, error) {
	if org == "" || platform == "" || userID == "" {
		return 0, nil
	}

	profileID := ProfileEntityID(org, platform, userID)
	entry, err := r.kv.Get(ctx, profileID)
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return 0, nil
		}
		return 0, fmt.Errorf("read profile %q: %w", profileID, err)
	}
	var state graph.EntityState
	if err := json.Unmarshal(entry.Value, &state); err != nil {
		// Corrupt state is also surfaced — same reasoning: the caller's
		// fallback should only kick in for real "no profile" cases, not
		// for data we couldn't parse.
		return 0, fmt.Errorf("decode profile %q: %w", profileID, err)
	}
	return readVersionFromState(&state), nil
}

// ReadLessons implements ProfileReader. It traverses the profile's
// has_lesson edges, fetches each referenced lesson entity, and returns
// up to `limit` lessons sorted most-recent-first by LearnedAt.
//
// Reads: 1 profile + N lesson entity gets where N is min(actual_lessons,
// defaultLessonsLimit). Lessons that fail to load (KV miss, malformed
// state) are skipped silently — a missing lesson is not a hard failure for
// the caller's sake of rendering a profile-context preamble.
func (r *GraphProfileReader) ReadLessons(ctx context.Context, org, platform, userID string, limit int) ([]Lesson, error) {
	if org == "" || platform == "" || userID == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = defaultLessonsLimit
	}

	profile := r.getState(ctx, ProfileEntityID(org, platform, userID))
	if profile == nil {
		return nil, nil
	}
	lessonIDs := relationshipTargets(profile, PredicateProfileHasLesson)
	if len(lessonIDs) == 0 {
		return nil, nil
	}

	out := make([]Lesson, 0, len(lessonIDs))
	for _, lessonID := range lessonIDs {
		l, ok := r.readLesson(ctx, lessonID)
		if ok {
			out = append(out, l)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].LearnedAt.After(out[j].LearnedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// readLesson hydrates a single Lesson entity from its triples. Returns
// (Lesson{}, false) for missing or malformed entities (logged at Debug).
func (r *GraphProfileReader) readLesson(ctx context.Context, entityID string) (Lesson, bool) {
	state := r.getState(ctx, entityID)
	if state == nil {
		return Lesson{}, false
	}
	l := Lesson{LessonID: lastDotSegment(entityID)}

	if v, ok := state.GetPropertyValue(PredicateLessonSummary); ok {
		l.Summary = objToString(v)
	}
	if v, ok := state.GetPropertyValue(PredicateLessonSessionID); ok {
		l.SessionID = objToString(v)
	}
	if v, ok := state.GetPropertyValue(PredicateLessonLearnedAt); ok {
		if s := objToString(v); s != "" {
			if parsed, err := time.Parse(time.RFC3339, s); err == nil {
				l.LearnedAt = parsed
			}
		}
	}
	if l.Summary == "" {
		r.logger.Debug("skipping lesson with missing summary",
			"entity_id", entityID)
		return Lesson{}, false
	}
	return l, true
}

// ReadOperatingModel implements ProfileReader. It walks the user's profile
// entity through has_layer and has_entry relationship triples, returning
// only entries reachable from the requested user's profile.
func (r *GraphProfileReader) ReadOperatingModel(ctx context.Context, org, platform, userID string) (*ProfileResult, error) {
	if org == "" || platform == "" || userID == "" {
		return nil, nil
	}

	profileID := ProfileEntityID(org, platform, userID)
	profile := r.getState(ctx, profileID)
	if profile == nil {
		return &ProfileResult{}, nil
	}

	version := readVersionFromState(profile)

	layerIDs := relationshipTargets(profile, PredicateProfileHasLayer)
	if len(layerIDs) == 0 {
		return &ProfileResult{Version: version}, nil
	}

	entryIDs := r.collectEntryIDs(ctx, layerIDs)
	if len(entryIDs) == 0 {
		return &ProfileResult{Version: version}, nil
	}

	entries := make([]Entry, 0, len(entryIDs))
	for _, entryID := range entryIDs {
		entry, ok := r.readEntry(ctx, entryID)
		if ok {
			entries = append(entries, entry)
		}
	}
	return &ProfileResult{Entries: entries, Version: version}, nil
}

// collectEntryIDs walks each layer's has_entry triples and returns a
// deduplicated list of entry entity IDs in encounter order.
func (r *GraphProfileReader) collectEntryIDs(ctx context.Context, layerIDs []string) []string {
	seen := make(map[string]struct{}, len(layerIDs)*4)
	var entryIDs []string
	for _, layerID := range layerIDs {
		layer := r.getState(ctx, layerID)
		if layer == nil {
			continue
		}
		for _, target := range relationshipTargets(layer, PredicateLayerHasEntry) {
			if _, dup := seen[target]; dup {
				continue
			}
			seen[target] = struct{}{}
			entryIDs = append(entryIDs, target)
		}
	}
	return entryIDs
}

func (r *GraphProfileReader) readEntry(ctx context.Context, entityID string) (Entry, bool) {
	state := r.getState(ctx, entityID)
	if state == nil {
		return Entry{}, false
	}

	e := Entry{EntryID: lastDotSegment(entityID)}

	if v, ok := state.GetPropertyValue(PredicateEntryTitle); ok {
		e.Title = objToString(v)
	}
	if v, ok := state.GetPropertyValue(PredicateEntrySummary); ok {
		e.Summary = objToString(v)
	}
	if v, ok := state.GetPropertyValue(PredicateEntryCadence); ok {
		e.Cadence = objToString(v)
	}
	if v, ok := state.GetPropertyValue(PredicateEntryTrigger); ok {
		e.Trigger = objToString(v)
	}
	if v, ok := state.GetPropertyValue(PredicateEntryInputs); ok {
		e.Inputs = objToStrings(v)
	}
	if v, ok := state.GetPropertyValue(PredicateEntryStakeholders); ok {
		e.Stakeholders = objToStrings(v)
	}
	if v, ok := state.GetPropertyValue(PredicateEntryConstraints); ok {
		e.Constraints = objToStrings(v)
	}
	if v, ok := state.GetPropertyValue(PredicateEntrySourceConfidence); ok {
		e.SourceConfidence = objToString(v)
	}
	if v, ok := state.GetPropertyValue(PredicateEntryStatus); ok {
		e.Status = objToString(v)
	}

	if e.Title == "" || e.Summary == "" {
		r.logger.Debug("skipping entry with missing required fields",
			"entity_id", entityID,
			"title_empty", e.Title == "",
			"summary_empty", e.Summary == "")
		return Entry{}, false
	}
	return e, true
}

func (r *GraphProfileReader) getState(ctx context.Context, entityID string) *graph.EntityState {
	entry, err := r.kv.Get(ctx, entityID)
	if err != nil {
		if !errors.Is(err, natsclient.ErrKVKeyNotFound) {
			r.logger.Debug("KV get failed", "entity_id", entityID, "error", err)
		}
		return nil
	}
	var state graph.EntityState
	if err := json.Unmarshal(entry.Value, &state); err != nil {
		r.logger.Warn("corrupt entity state in KV",
			"entity_id", entityID, "error", err)
		return nil
	}
	return &state
}

// relationshipTargets returns the Object value of every triple on `state`
// whose predicate matches `predicate` and whose Object is a string. Used
// to walk multi-valued has_layer / has_entry edges where EntityState's
// single-value GetPropertyValue helper isn't enough.
func relationshipTargets(state *graph.EntityState, predicate string) []string {
	if state == nil {
		return nil
	}
	var out []string
	for _, t := range state.Triples {
		if t.Predicate != predicate {
			continue
		}
		if s, ok := t.Object.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// readVersionFromState extracts the profile version from an already-loaded
// profile state. Splitting this out avoids a second KV round-trip for the
// version.
func readVersionFromState(profile *graph.EntityState) int {
	if profile == nil {
		return 0
	}
	val, ok := profile.GetPropertyValue(PredicateProfileVersion)
	if !ok {
		return 0
	}
	return objToInt(val)
}

// --- triple-object type conversion helpers ---

func objToString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func objToInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}

func objToStrings(v any) []string {
	switch s := v.(type) {
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			out = append(out, objToString(item))
		}
		return out
	case []string:
		return s
	}
	return nil
}

// lastDotSegment extracts the instance part (last segment) from a 6-part
// entity ID.
func lastDotSegment(id string) string {
	for i := len(id) - 1; i >= 0; i-- {
		if id[i] == '.' {
			return id[i+1:]
		}
	}
	return id
}
