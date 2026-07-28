package clustering

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// Summary record status values. A record is either an LLM-enhanced summary or a
// record of a failed enhancement (which the worker re-attempts only after a
// backoff). These are SEPARATE from the detector-owned Community.SummaryStatus
// enum on COMMUNITY_INDEX — the split is the whole point of ADR-087.
const (
	// SummaryStatusEnhanced marks a record that carries a usable LLM summary.
	SummaryStatusEnhanced = "llm-enhanced"
	// SummaryStatusFailed marks a record whose enhancement failed; the worker
	// retries it only after the failed-retry backoff elapses.
	SummaryStatusFailed = "llm-failed"
)

// CommunitySummaryRecord is the worker-owned, content-addressed LLM summary for a
// community membership. It is stored in COMMUNITY_SUMMARIES keyed by
// {level}.{membership_hash}. It carries NO full member snapshot — the membership
// hash IS the identity; storing the members would reintroduce a divergence
// surface. Keywords and the statistical summary stay detector-owned on
// COMMUNITY_INDEX; only the LLM prose lives here.
type CommunitySummaryRecord struct {
	// MembershipHash is the content-address (clustering.MembershipHash) of the
	// membership this summary describes. It is the hash half of the KV key.
	MembershipHash string `json:"membership_hash"`

	// Level is the hierarchy level of the community. It is the level half of the
	// KV key; the hash itself is level-independent.
	Level int `json:"level"`

	// LLMSummary is the generated natural-language summary (empty on a failed record).
	LLMSummary string `json:"llm_summary,omitempty"`

	// Model is the LLM model identifier that produced the summary.
	Model string `json:"model,omitempty"`

	// Status is SummaryStatusEnhanced or SummaryStatusFailed.
	Status string `json:"status"`

	// Truncated is true when the LLM summary hit the token budget (finish_reason
	// "length").
	Truncated bool `json:"truncated,omitempty"`

	// MemberCount is the size of the membership that was summarized (observability;
	// the hash is the identity).
	MemberCount int `json:"member_count"`

	// GeneratedAt is when the record was written. The failed-retry backoff is
	// measured from this timestamp.
	GeneratedAt time.Time `json:"generated_at"`
}

// SummaryStore abstracts persistence for community LLM summaries keyed
// {level}.{membership_hash}. It is worker-exclusive: the enhancement worker is the
// SOLE writer (single-writer invariant, ADR-087). Read-only consumers (the
// graph-query community cache) watch the bucket directly rather than through this
// interface.
type SummaryStore interface {
	// GetSummary returns the summary record for a membership hash at a level, or
	// (nil, nil) when no record exists.
	GetSummary(ctx context.Context, level int, membershipHash string) (*CommunitySummaryRecord, error)

	// PutSummary writes (or overwrites) the record for its {level}.{hash} key. A
	// same-membership double-write is idempotent by construction (content-addressed
	// key), not an error.
	PutSummary(ctx context.Context, rec *CommunitySummaryRecord) error

	// CountSummaries returns the number of stored summary records (for the
	// bucket-size gauge / future bounded-GC decision).
	CountSummaries(ctx context.Context) (int, error)
}

// NATSSummaryStore implements SummaryStore over a NATS KV bucket.
type NATSSummaryStore struct {
	kv jetstream.KeyValue
}

// NewNATSSummaryStore creates a NATS-backed summary store over the given bucket.
func NewNATSSummaryStore(kv jetstream.KeyValue) *NATSSummaryStore {
	return &NATSSummaryStore{kv: kv}
}

// GetSummary returns the stored record for {level}.{hash}, or (nil, nil) on miss.
func (s *NATSSummaryStore) GetSummary(ctx context.Context, level int, membershipHash string) (*CommunitySummaryRecord, error) {
	if s.kv == nil {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "NATSSummaryStore", "GetSummary", "kv is nil")
	}
	entry, err := s.kv.Get(ctx, SummaryKey(level, membershipHash))
	if err != nil {
		// Cover both not-found sentinels: a never-written key and a tombstoned key.
		if natsclient.IsKVNotFoundError(err) {
			return nil, nil
		}
		return nil, errs.WrapTransient(err, "NATSSummaryStore", "GetSummary", "get summary")
	}
	var rec CommunitySummaryRecord
	if err := json.Unmarshal(entry.Value(), &rec); err != nil {
		return nil, errs.WrapInvalid(err, "NATSSummaryStore", "GetSummary", "unmarshal summary")
	}
	return &rec, nil
}

// PutSummary writes the record for its {level}.{hash} key.
func (s *NATSSummaryStore) PutSummary(ctx context.Context, rec *CommunitySummaryRecord) error {
	if s.kv == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, "NATSSummaryStore", "PutSummary", "kv is nil")
	}
	if rec == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, "NATSSummaryStore", "PutSummary", "record is nil")
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return errs.WrapInvalid(err, "NATSSummaryStore", "PutSummary", "marshal summary")
	}
	if _, err := s.kv.Put(ctx, SummaryKey(rec.Level, rec.MembershipHash), data); err != nil {
		return errs.WrapTransient(err, "NATSSummaryStore", "PutSummary", "put summary")
	}
	return nil
}

// CountSummaries returns the number of stored summary records.
func (s *NATSSummaryStore) CountSummaries(ctx context.Context) (int, error) {
	if s.kv == nil {
		return 0, errs.WrapInvalid(errs.ErrMissingConfig, "NATSSummaryStore", "CountSummaries", "kv is nil")
	}
	keys, err := s.kv.Keys(ctx)
	if err != nil {
		// An empty bucket reports a not-found / no-keys sentinel, which is a count of 0.
		if stderrors.Is(err, jetstream.ErrKeyNotFound) || stderrors.Is(err, jetstream.ErrNoKeysFound) ||
			strings.Contains(err.Error(), "no keys found") {
			return 0, nil
		}
		if stderrors.Is(err, context.Canceled) {
			return 0, nil
		}
		return 0, errs.WrapTransient(err, "NATSSummaryStore", "CountSummaries", "list keys")
	}
	return len(keys), nil
}

// SummaryKey builds the {level}.{membership_hash} COMMUNITY_SUMMARIES KV key. It
// is the ONE definition of the key format, shared by the worker's store and the
// graph-query read-join so the two cannot build keys that never match. The hash is
// hex (no dots), so a first-dot split unambiguously recovers the level.
func SummaryKey(level int, membershipHash string) string {
	return fmt.Sprintf("%d.%s", level, membershipHash)
}
