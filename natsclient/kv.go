package natsclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/retry"
	"github.com/nats-io/nats.go/jetstream"
)

// KVEntry wraps a KV entry with its revision for CAS operations
type KVEntry struct {
	Key      string
	Value    []byte
	Revision uint64
}

// KVOptions configures KV operations behavior
type KVOptions struct {
	MaxRetries            int           // Maximum CAS retry attempts
	RetryDelay            time.Duration // Initial delay between retries
	Timeout               time.Duration // Operation timeout
	MaxValueSize          int           // Maximum size for values (default: 1MB)
	UseExponentialBackoff bool          // Enable exponential backoff with jitter
	MaxRetryDelay         time.Duration // Maximum delay between retries
}

// DefaultKVOptions returns sensible defaults matching Graph processor
func DefaultKVOptions() KVOptions {
	return KVOptions{
		MaxRetries:            10, // Increased for high-contention scenarios
		RetryDelay:            10 * time.Millisecond,
		Timeout:               5 * time.Second,
		MaxValueSize:          1024 * 1024, // 1MB default max value size
		UseExponentialBackoff: true,
		MaxRetryDelay:         time.Second,
	}
}

// KVStore provides high-level KV operations with built-in CAS support
type KVStore struct {
	bucket  jetstream.KeyValue
	options KVOptions
	logger  *slog.Logger
}

// NewKVStore creates a new KV store with the given bucket
func (c *Client) NewKVStore(bucket jetstream.KeyValue, opts ...func(*KVOptions)) *KVStore {
	options := DefaultKVOptions()
	for _, opt := range opts {
		opt(&options)
	}

	return &KVStore{
		bucket:  bucket,
		options: options,
		logger:  c.logger,
	}
}

// applyTimeout applies the configured timeout to the context if set
func (kv *KVStore) applyTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if kv.options.Timeout > 0 {
		return context.WithTimeout(ctx, kv.options.Timeout)
	}
	return ctx, func() {} // no-op cancel
}

// Get retrieves a value with its revision for CAS operations
func (kv *KVStore) Get(ctx context.Context, key string) (*KVEntry, error) {
	ctx, cancel := kv.applyTimeout(ctx)
	defer cancel()

	entry, err := kv.bucket.Get(ctx, key)
	if err != nil {
		if IsKVNotFoundError(err) {
			return nil, ErrKVKeyNotFound
		}
		return nil, fmt.Errorf("kv get %s: %w", key, err)
	}

	return &KVEntry{
		Key:      key,
		Value:    entry.Value(),
		Revision: entry.Revision(),
	}, nil
}

// BucketLastSeq returns the backing stream's LastSeq for a KV bucket — the highest
// sequence ever assigned to the bucket, read fresh from the server on every call.
//
// It is the query-time "target" for revision-lag readiness (ADR-066): every
// committed write to the bucket has a Revision() <= LastSeq, and LastSeq is
// monotonic even under History=1 — a purge raises FirstSeq but never lowers
// LastSeq. LastSeq shares the same sequence space as KeyValueEntry.Revision()
// (both are stream sequence numbers), so a caller can compare an indexed-revision
// watermark directly against it. Prefer this over the last watch entry's
// Delta()==0: that cache is stale exactly in the committed-but-not-yet-delivered
// window this target must see through.
func BucketLastSeq(ctx context.Context, bucket jetstream.KeyValue) (uint64, error) {
	status, err := bucket.Status(ctx)
	if err != nil {
		return 0, fmt.Errorf("kv status: %w", err)
	}
	// The concrete JetStream-backed status exposes the backing stream info; the
	// KeyValueStatus interface itself does not surface LastSeq.
	bucketStatus, ok := status.(*jetstream.KeyValueBucketStatus)
	if !ok {
		return 0, fmt.Errorf("kv status: unexpected type %T, want *jetstream.KeyValueBucketStatus", status)
	}
	info := bucketStatus.StreamInfo()
	if info == nil {
		return 0, fmt.Errorf("kv status: nil backing stream info")
	}
	return info.State.LastSeq, nil
}

// BucketRetention returns a bucket's lifecycle-eviction config from its backing
// stream: maxAge (the KV bucket's TTL — age eviction) and maxBytes (size
// eviction; 0/negative = unlimited). Callers on the live graph assert both are
// non-binding — NATS age/size eviction is reachability-blind and would drop
// entities with live inbound edges (ADR-068 D1). Mirrors BucketLastSeq: the
// KeyValueStatus interface does not surface these, so we read the concrete
// JetStream-backed status's stream config.
func BucketRetention(ctx context.Context, bucket jetstream.KeyValue) (maxAge time.Duration, maxBytes int64, err error) {
	status, err := bucket.Status(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("kv status: %w", err)
	}
	bucketStatus, ok := status.(*jetstream.KeyValueBucketStatus)
	if !ok {
		return 0, 0, fmt.Errorf("kv status: unexpected type %T, want *jetstream.KeyValueBucketStatus", status)
	}
	info := bucketStatus.StreamInfo()
	if info == nil {
		return 0, 0, fmt.Errorf("kv status: nil backing stream info")
	}
	return info.Config.MaxAge, info.Config.MaxBytes, nil
}

// ErrGraphBucketRetention is returned by CheckNoLifecycleRetention when a live
// graph bucket carries a lifecycle-eviction config. Sentinel so a boot path can
// classify it (fail-closed) distinctly from a transient status error.
var ErrGraphBucketRetention = errors.New("live graph bucket has lifecycle retention (ADR-068 D1: forbidden)")

// CheckNoLifecycleRetention is the pure D1 guardrail: it errors if a bucket's
// retention config is binding. maxAge > 0 (a TTL) is always a violation. maxBytes
// > 0 is a violation too — a size cap on the live graph evicts by size, which is
// reachability-blind; a deployment that genuinely wants a non-binding crash
// backstop should size it far above steady state and treat hitting it as an
// alert, not configure it here. Pure + no I/O so it is unit-testable; the boot
// path pairs it with BucketRetention.
func CheckNoLifecycleRetention(name string, maxAge time.Duration, maxBytes int64) error {
	if maxAge > 0 {
		return fmt.Errorf("%w: bucket %q has TTL/MaxAge %s", ErrGraphBucketRetention, name, maxAge)
	}
	if maxBytes > 0 {
		return fmt.Errorf("%w: bucket %q has MaxBytes %d", ErrGraphBucketRetention, name, maxBytes)
	}
	return nil
}

// AssertNoLifecycleRetention reads this bucket's retention config and returns
// ErrGraphBucketRetention if it is binding — the boot-time D1 guardrail for a
// live graph bucket (ADR-068). A retention config here means some process won
// the get-or-create race with a TTL/size cap; fail-closed rather than silently
// expire graph state.
func (kv *KVStore) AssertNoLifecycleRetention(ctx context.Context, name string) error {
	maxAge, maxBytes, err := BucketRetention(ctx, kv.bucket)
	if err != nil {
		return err
	}
	return CheckNoLifecycleRetention(name, maxAge, maxBytes)
}

// Put creates or updates a key without revision check (last writer wins)
func (kv *KVStore) Put(ctx context.Context, key string, value []byte) (uint64, error) {
	ctx, cancel := kv.applyTimeout(ctx)
	defer cancel()

	rev, err := kv.bucket.Put(ctx, key, value)
	if err != nil {
		return 0, fmt.Errorf("kv put %s: %w", key, err)
	}

	if kv.logger != nil {
		kv.logger.Debug("KV put", slog.String("key", key), slog.Uint64("revision", rev))
	}

	return rev, nil
}

// Create only creates if key doesn't exist (returns error if exists)
func (kv *KVStore) Create(ctx context.Context, key string, value []byte) (uint64, error) {
	ctx, cancel := kv.applyTimeout(ctx)
	defer cancel()

	rev, err := kv.bucket.Create(ctx, key, value)
	if err != nil {
		if IsKVConflictError(err) {
			return 0, ErrKVKeyExists
		}
		return 0, fmt.Errorf("kv create %s: %w", key, err)
	}

	if kv.logger != nil {
		kv.logger.Debug("KV create", slog.String("key", key), slog.Uint64("revision", rev))
	}

	return rev, nil
}

// Update performs CAS update with explicit revision
func (kv *KVStore) Update(ctx context.Context, key string, value []byte, revision uint64) (uint64, error) {
	ctx, cancel := kv.applyTimeout(ctx)
	defer cancel()

	rev, err := kv.bucket.Update(ctx, key, value, revision)
	if err != nil {
		if IsKVConflictError(err) {
			return 0, ErrKVRevisionMismatch
		}
		return 0, fmt.Errorf("kv update %s: %w", key, err)
	}

	if kv.logger != nil {
		kv.logger.Debug("KV update", slog.String("key", key), slog.Uint64("old_revision", revision), slog.Uint64("new_revision", rev))
	}

	return rev, nil
}

// getRetryConfig returns the retry configuration for this KV store
func (kv *KVStore) getRetryConfig() retry.Config {
	config := retry.Config{
		MaxAttempts:  kv.options.MaxRetries + 1, // +1 because MaxRetries is additional attempts
		InitialDelay: kv.options.RetryDelay,
		MaxDelay:     kv.options.MaxRetryDelay,
		AddJitter:    true, // Always use jitter to prevent thundering herd
	}

	if kv.options.UseExponentialBackoff {
		config.Multiplier = 2.0
	} else {
		// No exponential backoff, use constant delay
		config.Multiplier = 1.0
	}

	return config
}

// UpdateWithRetry performs CAS update with automatic retry on conflicts
// If the key doesn't exist, it creates it.
//
// Use UpdateWithRetryRev when the caller must ATTRIBUTE the write to itself:
// this variant discards the committed revision, and re-reading it afterwards is
// not equivalent — another writer can commit in between, and the re-read then
// returns that writer's revision.
func (kv *KVStore) UpdateWithRetry(ctx context.Context, key string,
	updateFn func(current []byte) ([]byte, error)) error {
	_, err := kv.UpdateWithRetryRev(ctx, key, updateFn)
	return err
}

// UpdateWithRetryRev is UpdateWithRetry that also returns the EXACT KV revision
// the committed write produced.
//
// This exists because a post-hoc `Get` is not a substitute. Between the CAS and
// the re-read, another writer can commit; the re-read then reports THAT
// writer's revision, and any caller that attributes the reported revision to
// its own write is now holding someone else's. The rule engine's per-rule
// feedback-loop tracker does exactly that attribution, and `shouldSkipRule`
// consumes the recorded revision once and returns true — so a mis-attributed
// revision makes the rule silently DROP the external writer's genuine change.
//
// Note this is a strictly different safety property from the readiness
// consumer's `IndexedRevision >= myRev` check, which tolerates over-reporting
// because revisions are monotonic. Two consumers, two properties; do not
// generalize from the tolerant one.
//
// On any non-nil error the returned revision is 0 — nothing committed.
func (kv *KVStore) UpdateWithRetryRev(ctx context.Context, key string,
	updateFn func(current []byte) ([]byte, error)) (uint64, error) {

	// Apply timeout to the entire retry operation
	ctx, cancel := kv.applyTimeout(ctx)
	defer cancel()

	// committedRevision is ASSIGNED (never accumulated) on each successful
	// write, so a CAS retry replaces a losing attempt's value rather than
	// leaving a stale one behind. retry.Do runs the closure synchronously on
	// this goroutine, so the capture needs no synchronization.
	var committedRevision uint64

	// Get retry configuration
	retryConfig := kv.getRetryConfig()

	// Track attempt number for logging
	attemptNum := 0

	// Use the retry package for automatic retry with exponential backoff
	// Note: All errors will be retried except when max attempts is reached.
	// We use consistent error message formatting to indicate error types:
	// - Retryable errors (conflicts): returned as-is to trigger retry
	// - Non-retryable errors: wrapped with context about why they failed
	err := retry.Do(ctx, retryConfig, func() error {
		attemptNum++

		// Get current value with revision
		entry, err := kv.Get(ctx, key)

		var currentValue []byte
		var revision uint64

		if err != nil {
			// Check if key doesn't exist
			if IsKVNotFoundError(err) {
				// Key doesn't exist - treat as empty value with revision 0
				currentValue = nil
				revision = 0
			} else {
				// Network/permission errors - will retry but likely to fail again
				// Wrapped to provide context about the operation that failed
				return fmt.Errorf("kv get failed during update: %w", err)
			}
		} else {
			// Key exists - use its value and revision
			currentValue = entry.Value
			revision = entry.Revision
		}

		// Apply update function to current value
		newValue, err := updateFn(currentValue)
		if err != nil {
			// User logic error - should not retry as it will fail again
			// Wrapped as non-retryable to fail fast
			return retry.NonRetryable(fmt.Errorf("update function error: %w", err))
		}

		// Check value size limit
		if kv.options.MaxValueSize > 0 && len(newValue) > kv.options.MaxValueSize {
			// Validation error - should not retry as it will always fail
			// Wrapped as non-retryable to fail fast
			return retry.NonRetryable(fmt.Errorf("value size validation failed: size %d exceeds maximum %d",
				len(newValue), kv.options.MaxValueSize))
		}

		// Create or update based on whether key exists
		if revision == 0 {
			// Key doesn't exist - create it
			var createdRevision uint64
			createdRevision, err = kv.bucket.Create(ctx, key, newValue)
			if err == nil {
				committedRevision = createdRevision
				return nil // Success!
			}
			// Conflict error - WILL be retried (this is the intended retry case)
			if IsKVConflictError(err) {
				if kv.logger != nil {
					kv.logger.Debug("KV create conflict, retrying",
						slog.String("key", key),
						slog.Int("attempt", attemptNum),
						slog.Int("max_attempts", retryConfig.MaxAttempts),
					)
				}
				// Return conflict error as-is for retry
				return err
			}
			// Infrastructure error - will retry but may not succeed
			// Wrapped to provide context about the operation
			return fmt.Errorf("kv create failed: %w", err)
		}

		// Key exists - update with CAS
		var updatedRevision uint64
		updatedRevision, err = kv.Update(ctx, key, newValue, revision)
		if err == nil {
			committedRevision = updatedRevision
			return nil // Success!
		}
		// Conflict error - WILL be retried (this is the intended retry case)
		if IsKVConflictError(err) {
			if kv.logger != nil {
				kv.logger.Debug("KV update conflict, retrying",
					slog.String("key", key),
					slog.Int("attempt", attemptNum),
					slog.Int("max_attempts", retryConfig.MaxAttempts),
				)
			}
			// Return conflict error as-is for retry
			return err
		}
		// Infrastructure error - will retry but may not succeed
		// Wrapped to provide context about the operation
		return fmt.Errorf("kv update failed: %w", err)
	})

	// Check if we exceeded max retries on a conflict error
	if err != nil && IsKVConflictError(err) {
		return 0, ErrKVMaxRetriesExceeded
	}
	if err != nil {
		return 0, err
	}

	return committedRevision, nil
}

// UpdateJSON performs CAS update on JSON data with automatic retry
func (kv *KVStore) UpdateJSON(ctx context.Context, key string,
	updateFn func(current map[string]any) error) error {

	return kv.UpdateWithRetry(ctx, key, func(currentBytes []byte) ([]byte, error) {
		// Parse current JSON
		var current map[string]any
		if len(currentBytes) > 0 {
			if err := json.Unmarshal(currentBytes, &current); err != nil {
				// JSON parse error - should not retry as data is corrupt
				return nil, retry.NonRetryable(fmt.Errorf("unmarshal current: %w", err))
			}
		} else {
			current = make(map[string]any)
		}

		// Apply update
		if err := updateFn(current); err != nil {
			return nil, err
		}

		// Marshal back to JSON
		return json.Marshal(current)
	})
}

// Delete removes a key from the bucket
func (kv *KVStore) Delete(ctx context.Context, key string) error {
	ctx, cancel := kv.applyTimeout(ctx)
	defer cancel()

	err := kv.bucket.Delete(ctx, key)
	if err != nil {
		if IsKVNotFoundError(err) {
			return ErrKVKeyNotFound
		}
		return fmt.Errorf("kv delete %s: %w", key, err)
	}

	if kv.logger != nil {
		kv.logger.Debug("KV delete", slog.String("key", key))
	}

	return nil
}

// DeleteAtRevision removes a key only when revision is still current.
// It makes one revision-fenced delete attempt and never reads or retries.
func (kv *KVStore) DeleteAtRevision(ctx context.Context, key string, revision uint64) error {
	if revision == 0 {
		return errs.WrapInvalid(errors.New("revision must be nonzero"), "KVStore", "DeleteAtRevision", "validate revision")
	}
	if err := ValidateKVLiteralKey(key); err != nil {
		return err
	}

	ctx, cancel := kv.applyTimeout(ctx)
	defer cancel()

	if err := kv.bucket.Delete(ctx, key, jetstream.LastRevision(revision)); err != nil {
		if IsKVNotFoundError(err) {
			return ErrKVKeyNotFound
		}
		if IsKVConflictError(err) {
			return ErrKVRevisionMismatch
		}
		return fmt.Errorf("kv delete %s at revision %d: %w", key, revision, err)
	}

	if kv.logger != nil {
		kv.logger.Debug("KV delete at revision", slog.String("key", key), slog.Uint64("revision", revision))
	}

	return nil
}

// Keys returns all keys in the bucket
func (kv *KVStore) Keys(ctx context.Context) ([]string, error) {
	ctx, cancel := kv.applyTimeout(ctx)
	defer cancel()

	keys, err := kv.bucket.Keys(ctx)
	if err != nil {
		// Handle empty bucket case
		if err == jetstream.ErrNoKeysFound {
			return nil, nil
		}
		return nil, fmt.Errorf("kv keys: %w", err)
	}

	return keys, nil
}

// KeysByPrefix returns all keys matching the given prefix.
// Uses JetStream KV's native key filtering for efficient server-side filtering.
// The prefix is automatically converted to a NATS wildcard pattern (prefix + ">").
func (kv *KVStore) KeysByPrefix(ctx context.Context, prefix string) ([]string, error) {
	// Convert prefix to NATS wildcard pattern
	// "entity.sensor." becomes "entity.sensor.>" to match all keys with that prefix
	return kv.KeysByFilter(ctx, prefix+">")
}

// KeysByFilter returns all keys matching an exact NATS subject filter. Unlike
// KeysByPrefix, this supports fixed-position wildcards such as
// "*.org.platform.domain.system.type.instance".
//
// A filtered key listing is a correctness boundary for derived-index
// reconciliation. The NATS KeyLister closes its channel when ctx is cancelled,
// but it does not expose a terminal error. Callers must therefore reject the
// collected slice when the context expires; returning it as success would turn a
// partial owner snapshot into authoritative truth.
func (kv *KVStore) KeysByFilter(ctx context.Context, pattern string) ([]string, error) {
	ctx, cancel := kv.applyTimeout(ctx)
	defer cancel()

	lister, err := kv.bucket.ListKeysFiltered(ctx, pattern)
	if err != nil {
		// Handle empty bucket case
		if err == jetstream.ErrNoKeysFound {
			return nil, nil
		}
		return nil, fmt.Errorf("kv keys by filter %q: %w", pattern, err)
	}

	keys, err := collectFilteredKeys(ctx, lister)
	if err != nil {
		return nil, fmt.Errorf("kv keys by filter %q: %w", pattern, err)
	}
	return keys, nil
}

// FilteredKeys returns keys matching a NATS wildcard pattern from a KV reader.
// The pattern should be a valid NATS subject filter (e.g., "0.>" for level-0 community keys).
// Returns nil, nil when no keys match.
func FilteredKeys(ctx context.Context, kv interface {
	ListKeysFiltered(ctx context.Context, filters ...string) (jetstream.KeyLister, error)
}, pattern string) ([]string, error) {
	lister, err := kv.ListKeysFiltered(ctx, pattern)
	if err != nil {
		if errors.Is(err, jetstream.ErrNoKeysFound) || errors.Is(err, jetstream.ErrKeyNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("kv filtered keys %q: %w", pattern, err)
	}

	keys, err := collectFilteredKeys(ctx, lister)
	if err != nil {
		return nil, fmt.Errorf("kv filtered keys %q: %w", pattern, err)
	}
	return keys, nil
}

// collectFilteredKeys drains a NATS KeyLister without ever returning a partial
// result as success. KeyLister has no terminal Error method: cancellation is
// represented by channel closure, so ctx must be checked both while draining and
// after closure.
func collectFilteredKeys(ctx context.Context, lister jetstream.KeyLister) ([]string, error) {
	defer func() { _ = lister.Stop() }()

	var keys []string
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case key, ok := <-lister.Keys():
			if !ok {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				return keys, nil
			}
			keys = append(keys, key)
		}
	}
}

// Watch creates a watcher for key changes
// Note: Watch does not apply timeout as it creates a long-lived watcher
func (kv *KVStore) Watch(ctx context.Context, pattern string) (jetstream.KeyWatcher, error) {
	watcher, err := kv.bucket.Watch(ctx, pattern)
	if err != nil {
		return nil, fmt.Errorf("kv watch %s: %w", pattern, err)
	}
	return watcher, nil
}

// Error detection helpers - based on Graph processor experience

// IsKVNotFoundError checks if error indicates key absence — either a never-created
// key (jetstream.ErrKeyNotFound) or a tombstoned key (jetstream.ErrKeyDeleted).
// Both surface to callers as "the key is not there"; UpdateWithRetry relies on
// this to set revision=0 and route the updateFn down the create path.
//
// The NATS Go SDK's public Get already maps ErrKeyDeleted → ErrKeyNotFound, so
// in the common KVStore.Get path only ErrKeyNotFound reaches us. The ErrKeyDeleted
// branch defends paths that bypass that mapping (Watch handler entry-error chains,
// GetRevision wrappers, future SDK changes). See issue #122 and
// feedback_jetstream_sentinel_set_coverage.
func IsKVNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrKVKeyNotFound) {
		return true
	}
	if errors.Is(err, jetstream.ErrKeyNotFound) || errors.Is(err, jetstream.ErrKeyDeleted) {
		return true
	}
	errMsg := err.Error()
	return strings.Contains(errMsg, "key not found") ||
		strings.Contains(errMsg, "key was deleted") ||
		strings.Contains(errMsg, "10037")
}

// IsKVConflictError checks if error indicates a conflict (key exists or wrong revision)
func IsKVConflictError(err error) bool {
	if err == nil {
		return false
	}
	// Check for our custom errors
	if errors.Is(err, ErrKVRevisionMismatch) || errors.Is(err, ErrKVKeyExists) {
		return true
	}
	// Check for raw NATS errors
	errMsg := err.Error()
	return strings.Contains(errMsg, "wrong last sequence") ||
		strings.Contains(errMsg, "10071") ||
		strings.Contains(errMsg, "key exists") ||
		strings.Contains(errMsg, "10058")
}

// Well-known errors matching Graph processor patterns
var (
	ErrKVKeyNotFound        = errors.New("kv: key not found")
	ErrKVKeyExists          = errors.New("kv: key already exists")
	ErrKVRevisionMismatch   = errors.New("kv: revision mismatch (concurrent update)")
	ErrKVMaxRetriesExceeded = errors.New("kv: max retries exceeded")
)
