package flowstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// Manager provides persistence for Flow entities using NATS KV.
// Pattern-B CRUD surface per ADR-029. Named Manager (not Store) so the
// name matches the other Pattern-B types (rule.ConfigManager,
// persona.Manager, flowtemplate.Manager). Methods preserve the
// optimistic-concurrency split (Create + Update + Version) that flow
// definitions need; the ADR's canonical "Save" collapses into the
// existing Create/Update pair here.
type Manager struct {
	bucket  jetstream.KeyValue  // Raw bucket for operations like Keys()
	kvStore *natsclient.KVStore // KVStore wrapper for CAS operations

	// beforeUpdateWrite is a package-private synchronization seam: Update calls
	// it (when non-nil) after it has read the stored record and built its
	// candidate, immediately before the revision-fenced write. It is nil in
	// production — nothing outside package flowstore can reach it — and exists
	// so the concurrency proof in this package can hold two Managers at the same
	// observed revision without sleeping. Never make it exported, an option, or
	// a constructor parameter.
	beforeUpdateWrite func(ctx context.Context)
}

// NewManager creates a new flow store
func NewManager(natsClient *natsclient.Client) (*Manager, error) {
	if natsClient == nil {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "flowstore", "NewManager", "nats client cannot be nil")
	}

	ctx := context.Background()
	bucket, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      "semstreams_flows",
		Description: "Visual flow definitions and metadata",
		History:     10, // Keep last 10 versions for history/recovery
	})
	if err != nil {
		return nil, errs.WrapTransient(err, "flowstore", "NewManager", "create KV bucket")
	}

	return &Manager{
		bucket:  bucket,
		kvStore: natsClient.NewKVStore(bucket),
	}, nil
}

// Create creates a new flow
func (s *Manager) Create(ctx context.Context, flow *Flow) error {
	if flow == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "flowstore", "Create", "flow cannot be nil")
	}
	if flow.ID == "" {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "flowstore", "Create", "flow ID cannot be empty")
	}

	// Validate flow structure before saving
	if err := flow.Validate(); err != nil {
		return err
	}

	// Initialize version and timestamps
	flow.Version = 1
	now := time.Now()
	flow.CreatedAt = now
	flow.UpdatedAt = now
	flow.LastModified = now

	// Marshal and store
	data, err := json.Marshal(flow)
	if err != nil {
		return errs.WrapFatal(err, "flowstore", "Create", "marshal flow")
	}

	// Use Create() to ensure it only creates if key doesn't exist
	if _, err := s.kvStore.Create(ctx, flow.ID, data); err != nil {
		if natsclient.IsKVConflictError(err) {
			return errs.WrapInvalid(err, "flowstore", "Create", "flow already exists")
		}
		return errs.WrapTransient(err, "flowstore", "Create", "create in KV")
	}

	return nil
}

// Get retrieves a flow by ID
func (s *Manager) Get(ctx context.Context, id string) (*Flow, error) {
	if id == "" {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "flowstore", "Get", "flow ID cannot be empty")
	}

	entry, err := s.kvStore.Get(ctx, id)
	if err != nil {
		return nil, errs.WrapTransient(err, "flowstore", "Get", "get from KV")
	}

	var flow Flow
	if err := json.Unmarshal(entry.Value, &flow); err != nil {
		return nil, errs.WrapFatal(err, "flowstore", "Get", "unmarshal flow")
	}

	return &flow, nil
}

// Update updates an existing flow with optimistic concurrency control.
//
// The server owns the audit fields: the persisted record keeps the stored
// CreatedAt, takes the stored version plus one, and carries one server-observed
// instant in both UpdatedAt and LastModified, whatever the request supplied.
// CreatedBy is persisted exactly as the caller sent it. The request's Version is
// a precondition, never a stored value.
//
// The write is revision-fenced against the revision the stored record was read
// at, so concurrent Updates through any number of Managers over one bucket
// commit exactly once. A stale request version and a lost fence are the same
// typed conflict: a classified invalid error carrying the ADR-060
// revision_mismatch code, so callers branch with
// errors.Is(err, errs.ErrRevisionMismatch) rather than on message text.
//
// flow is left untouched on every failure path and is assigned the committed
// record only after the fenced write succeeds.
func (s *Manager) Update(ctx context.Context, flow *Flow) error {
	if flow == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "flowstore", "Update", "flow cannot be nil")
	}
	if flow.ID == "" {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "flowstore", "Update", "flow ID cannot be empty")
	}

	// Validate flow structure before saving
	if err := flow.Validate(); err != nil {
		return err
	}

	// Read the stored record together with the KV revision the write will fence on
	entry, err := s.kvStore.Get(ctx, flow.ID)
	if err != nil {
		return errs.WrapTransient(err, "flowstore", "Update", "get current version")
	}
	var stored Flow
	if err := json.Unmarshal(entry.Value, &stored); err != nil {
		return errs.WrapFatal(err, "flowstore", "Update", "unmarshal stored flow")
	}

	// Check version for optimistic concurrency
	if stored.Version != flow.Version {
		return versionConflict(fmt.Errorf("version mismatch: expected %d, got %d", stored.Version, flow.Version))
	}

	// The candidate is a copy: the caller's value stays untouched until the write commits
	candidate := *flow
	candidate.CreatedAt = stored.CreatedAt
	candidate.Version = stored.Version + 1
	now := time.Now()
	candidate.UpdatedAt = now
	candidate.LastModified = now

	// Marshal and store
	data, err := json.Marshal(&candidate)
	if err != nil {
		return errs.WrapFatal(err, "flowstore", "Update", "marshal flow")
	}

	if s.beforeUpdateWrite != nil {
		s.beforeUpdateWrite(ctx)
	}

	if _, err := s.kvStore.Update(ctx, candidate.ID, data, entry.Revision); err != nil {
		if errors.Is(err, natsclient.ErrKVRevisionMismatch) {
			return versionConflict(fmt.Errorf("revision mismatch: flow %s was modified concurrently", candidate.ID))
		}
		return errs.WrapTransient(err, "flowstore", "Update", "update in KV")
	}

	*flow = candidate
	return nil
}

// versionConflict is the one typed optimistic-concurrency failure of Update: a
// classified invalid error carrying the ADR-060 revision_mismatch code, so a
// logical version mismatch and a lost revision fence are indistinguishable to a
// caller branching on errors.Is(err, errs.ErrRevisionMismatch).
func versionConflict(cause error) error {
	return errs.WrapInvalid(
		errs.ClassifiedCode(errs.ErrorInvalid, errs.ErrRevisionMismatch.Code, cause),
		"flowstore", "Update", "conflict: flow was modified by another user")
}

// Delete removes a flow by ID
func (s *Manager) Delete(ctx context.Context, id string) error {
	if id == "" {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "flowstore", "Delete", "flow ID cannot be empty")
	}

	if err := s.kvStore.Delete(ctx, id); err != nil {
		return errs.WrapTransient(err, "flowstore", "Delete", "delete from KV")
	}

	return nil
}

// List retrieves all flows
func (s *Manager) List(ctx context.Context) ([]*Flow, error) {
	keys, err := s.bucket.Keys(ctx)
	if err != nil {
		return nil, errs.WrapTransient(err, "flowstore", "List", "list KV keys")
	}

	flows := make([]*Flow, 0, len(keys))
	for _, key := range keys {
		flow, err := s.Get(ctx, key)
		if err != nil {
			return nil, errs.WrapTransient(err, "flowstore", "List",
				fmt.Sprintf("get flow %s", key))
		}
		flows = append(flows, flow)
	}

	return flows, nil
}

// Watch watches for changes to flows matching the pattern.
// Pattern supports wildcards: "*" matches any single token, ">" matches remaining tokens.
// Returns a KeyWatcher that emits updates on its Updates() channel.
func (s *Manager) Watch(ctx context.Context, pattern string) (jetstream.KeyWatcher, error) {
	return s.kvStore.Watch(ctx, pattern)
}
