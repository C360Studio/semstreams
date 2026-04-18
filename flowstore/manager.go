package flowstore

import (
	"context"
	"encoding/json"
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

	// Set defaults before validation
	if flow.RuntimeState == "" {
		flow.RuntimeState = StateNotDeployed
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

// Update updates an existing flow with optimistic concurrency control
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

	// Get current version from KV
	current, err := s.Get(ctx, flow.ID)
	if err != nil {
		return errs.WrapTransient(err, "flowstore", "Update", "get current version")
	}

	// Check version for optimistic concurrency
	if current.Version != flow.Version {
		return errs.WrapInvalid(
			fmt.Errorf("version mismatch: expected %d, got %d", current.Version, flow.Version),
			"flowstore", "Update", "conflict: flow was modified by another user")
	}

	// Increment version
	flow.Version++
	flow.UpdatedAt = time.Now()
	flow.LastModified = time.Now()

	// Marshal and store
	data, err := json.Marshal(flow)
	if err != nil {
		return errs.WrapFatal(err, "flowstore", "Update", "marshal flow")
	}

	if _, err := s.kvStore.Put(ctx, flow.ID, data); err != nil {
		return errs.WrapTransient(err, "flowstore", "Update", "put to KV")
	}

	return nil
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
