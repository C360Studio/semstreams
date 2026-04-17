package operatingmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
)

// GraphProfileReader implements ProfileReader by querying the ENTITY_STATES
// KV bucket for operating-model entities. It reconstructs []Entry from
// predicate-per-field triples stored by the graph-ingest component.
//
// All KV operations go through semstreams' natsclient.KVStore wrapper.
type GraphProfileReader struct {
	kv     *natsclient.KVStore
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

// ReadOperatingModel implements ProfileReader. It fetches the user's profile
// entity for the version number, then scans for all om-entry.* entities and
// reconstructs typed Entry objects from their triples.
func (r *GraphProfileReader) ReadOperatingModel(ctx context.Context, org, platform, userID string) (*ProfileResult, error) {
	if org == "" || platform == "" || userID == "" {
		return nil, nil
	}

	profileVersion := r.readProfileVersion(ctx, org, platform, userID)

	entryPrefix := fmt.Sprintf("%s.%s.user.teams.om-entry.", org, platform)
	keys, err := r.kv.KeysByPrefix(ctx, entryPrefix)
	if err != nil {
		return nil, fmt.Errorf("list entry keys: %w", err)
	}
	if len(keys) == 0 {
		return &ProfileResult{Version: profileVersion}, nil
	}

	entries := make([]Entry, 0, len(keys))
	for _, key := range keys {
		entry, ok := r.readEntry(ctx, key)
		if ok {
			entries = append(entries, entry)
		}
	}
	return &ProfileResult{Entries: entries, Version: profileVersion}, nil
}

func (r *GraphProfileReader) readProfileVersion(ctx context.Context, org, platform, userID string) int {
	profileID := ProfileEntityID(org, platform, userID)
	state := r.getState(ctx, profileID)
	if state == nil {
		return 0
	}
	val, ok := state.GetPropertyValue(PredicateProfileVersion)
	if !ok {
		return 0
	}
	return objToInt(val)
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
		if !natsclient.IsKVNotFoundError(err) {
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
