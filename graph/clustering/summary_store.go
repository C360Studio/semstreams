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

	// PutSummary writes (or overwrites) the record for its {level}.{hash} key. It is
	// the SUCCESS lane: a same-membership double-write is idempotent by construction
	// (content-addressed key), and a success unconditionally overwrites a prior
	// llm-failed record (desired recovery) or an equivalent prior success. It is NOT
	// used for llm-failed records — those go through PutFailedUnlessEnhanced so a
	// failure can never clobber a success.
	PutSummary(ctx context.Context, rec *CommunitySummaryRecord) error

	// PutFailedUnlessEnhanced writes rec (an llm-failed record) for its
	// {level}.{hash} key, but NEVER overwrites an existing llm-enhanced record — a
	// successful summary always wins over a later failure (the ADR-087 idempotency
	// guarantee). It is race-safe against a concurrent success: a plain
	// read-then-write is TOCTOU (a concurrent llm-enhanced Put can land between the
	// read and the write), so the implementation reads the record and its revision,
	// then writes conditionally (Create on a miss, revision-checked Update on an
	// existing failed record). A concurrent success occupies the key / changes the
	// revision, the conditional write conflicts, and the retry re-reads, sees the
	// llm-enhanced record, and skips. A skip is success (nil), not an error.
	PutFailedUnlessEnhanced(ctx context.Context, rec *CommunitySummaryRecord) error

	// CountSummaries returns the number of stored summary records (for the
	// bucket-size gauge / future bounded-GC decision).
	CountSummaries(ctx context.Context) (int, error)
}

// summaryFailedCASMaxRetries bounds the read/conditional-write CAS loop in
// PutFailedUnlessEnhanced. Only a concurrent writer of the SAME membership hash can
// cause a conflict, and once a success (llm-enhanced) lands the loop sees it and
// skips, so contention self-terminates in one or two iterations; the bound only
// guards against pathological churn.
const summaryFailedCASMaxRetries = 8

// NATSSummaryStore implements SummaryStore over a NATS KV bucket, routed
// through the client's GUARDED KVStore lane (payload-bounds spec): every
// write refuses oversized values with the classified permanent error before
// I/O instead of dying server-side — this store's raw-handle writes were one
// of the enumerated gh#857 bypasses.
type NATSSummaryStore struct {
	store *natsclient.KVStore

	// afterFailedRead, when non-nil, is invoked inside PutFailedUnlessEnhanced
	// AFTER reading the current record but BEFORE the conditional write. It is a
	// TEST-ONLY seam (nil in production) that lets a test deterministically land a
	// concurrent llm-enhanced Put inside the CAS window, proving the read-then-write
	// TOCTOU is closed.
	afterFailedRead func()
}

// NewNATSSummaryStore creates a NATS-backed summary store over the given
// bucket, on the owning client's guarded write lane. client carries the
// live/cached server payload limit the writes are checked against; a nil
// client is rejected at the worker's config seam (NewEnhancementWorker).
func NewNATSSummaryStore(client *natsclient.Client, kv jetstream.KeyValue) *NATSSummaryStore {
	if client == nil || kv == nil {
		return &NATSSummaryStore{}
	}
	return &NATSSummaryStore{store: client.NewKVStore(kv)}
}

// GetSummary returns the stored record for {level}.{hash}, or (nil, nil) on miss.
func (s *NATSSummaryStore) GetSummary(ctx context.Context, level int, membershipHash string) (*CommunitySummaryRecord, error) {
	if s.store == nil {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "NATSSummaryStore", "GetSummary", "kv is nil")
	}
	entry, err := s.store.Get(ctx, SummaryKey(level, membershipHash))
	if err != nil {
		// The guarded lane collapses both not-found sentinels (never-written and
		// tombstoned) to ErrKVKeyNotFound.
		if stderrors.Is(err, natsclient.ErrKVKeyNotFound) {
			return nil, nil
		}
		return nil, errs.WrapTransient(err, "NATSSummaryStore", "GetSummary", "get summary")
	}
	var rec CommunitySummaryRecord
	if err := json.Unmarshal(entry.Value, &rec); err != nil {
		return nil, errs.WrapInvalid(err, "NATSSummaryStore", "GetSummary", "unmarshal summary")
	}
	return &rec, nil
}

// PutSummary writes the record for its {level}.{hash} key through the guarded
// lane. A size refusal (or server-side oversize residue) surfaces AS the
// classified permanent error — never re-wrapped transient, which was the
// gh#857 mis-classification this store carried.
func (s *NATSSummaryStore) PutSummary(ctx context.Context, rec *CommunitySummaryRecord) error {
	if s.store == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, "NATSSummaryStore", "PutSummary", "kv is nil")
	}
	if rec == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, "NATSSummaryStore", "PutSummary", "record is nil")
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return errs.WrapInvalid(err, "NATSSummaryStore", "PutSummary", "marshal summary")
	}
	if _, err := s.store.Put(ctx, SummaryKey(rec.Level, rec.MembershipHash), data); err != nil {
		if errs.IsInvalid(err) {
			return err // classified permanent (e.g. oversized) — do not launder to transient
		}
		return errs.WrapTransient(err, "NATSSummaryStore", "PutSummary", "put summary")
	}
	return nil
}

// PutFailedUnlessEnhanced writes rec (an llm-failed record) unless an llm-enhanced
// record already occupies its {level}.{hash} key. See the SummaryStore interface
// doc for the full race-safety argument. It uses revision CAS so a concurrent
// llm-enhanced success can never be downgraded to llm-failed.
func (s *NATSSummaryStore) PutFailedUnlessEnhanced(ctx context.Context, rec *CommunitySummaryRecord) error {
	if s.store == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, "NATSSummaryStore", "PutFailedUnlessEnhanced", "kv is nil")
	}
	if rec == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, "NATSSummaryStore", "PutFailedUnlessEnhanced", "record is nil")
	}
	key := SummaryKey(rec.Level, rec.MembershipHash)
	data, err := json.Marshal(rec)
	if err != nil {
		return errs.WrapInvalid(err, "NATSSummaryStore", "PutFailedUnlessEnhanced", "marshal summary")
	}

	for attempt := 0; attempt < summaryFailedCASMaxRetries; attempt++ {
		entry, getErr := s.store.Get(ctx, key)
		missing := getErr != nil && stderrors.Is(getErr, natsclient.ErrKVKeyNotFound)
		if getErr != nil && !missing {
			return errs.WrapTransient(getErr, "NATSSummaryStore", "PutFailedUnlessEnhanced", "get summary")
		}

		// Test-only injection point (nil in production): lets a test land a
		// concurrent llm-enhanced Put inside the read→write CAS window.
		if s.afterFailedRead != nil {
			s.afterFailedRead()
		}

		if !missing {
			var existing CommunitySummaryRecord
			if uerr := json.Unmarshal(entry.Value, &existing); uerr != nil {
				return errs.WrapInvalid(uerr, "NATSSummaryStore", "PutFailedUnlessEnhanced", "unmarshal summary")
			}
			if existing.Status == SummaryStatusEnhanced {
				// A success already occupies this membership — never downgrade it.
				return nil
			}
		}

		var writeErr error
		if missing {
			// No record yet: Create fails if a concurrent writer beat us to the key,
			// so we cannot blindly overwrite a success that landed since our read.
			// The guarded lane maps that conflict to ErrKVKeyExists.
			_, writeErr = s.store.Create(ctx, key, data)
		} else {
			// Existing (non-enhanced) record: revision-checked Update fails if a
			// concurrent write changed the key since our read. The guarded lane
			// maps that conflict to ErrKVRevisionMismatch.
			_, writeErr = s.store.Update(ctx, key, data, entry.Revision)
		}
		if writeErr == nil {
			return nil
		}
		if stderrors.Is(writeErr, natsclient.ErrKVKeyExists) ||
			stderrors.Is(writeErr, natsclient.ErrKVRevisionMismatch) ||
			natsclient.IsKVConflictError(writeErr) {
			// A concurrent write changed the key under us. Re-read and re-decide: if
			// it was an llm-enhanced success, the next iteration sees it and skips, so
			// the success is preserved.
			continue
		}
		if errs.IsInvalid(writeErr) {
			return writeErr // classified permanent (e.g. oversized) — not transient
		}
		return errs.WrapTransient(writeErr, "NATSSummaryStore", "PutFailedUnlessEnhanced", "write failed record")
	}
	return errs.WrapTransient(natsclient.ErrKVMaxRetriesExceeded, "NATSSummaryStore",
		"PutFailedUnlessEnhanced", "CAS retries exhausted")
}

// CountSummaries returns the number of stored summary records.
func (s *NATSSummaryStore) CountSummaries(ctx context.Context) (int, error) {
	if s.store == nil {
		return 0, errs.WrapInvalid(errs.ErrMissingConfig, "NATSSummaryStore", "CountSummaries", "kv is nil")
	}
	keys, err := s.store.Keys(ctx)
	if err != nil {
		// An empty bucket reports a not-found / no-keys sentinel, which is a
		// count of 0. The guarded lane resolves the common case itself; the
		// checks here cover wrapped sentinel variants (parity with the raw
		// handle this store migrated off).
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
