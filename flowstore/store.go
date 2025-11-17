package flowstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// Store provides persistence for Flow entities using NATS KV
type Store struct {
	bucket  jetstream.KeyValue  // Raw bucket for operations like Keys()
	kvStore *natsclient.KVStore // KVStore wrapper for CAS operations
}

// NewStore creates a new flow store
func NewStore(natsClient *natsclient.Client) (*Store, error) {
	if natsClient == nil {
		return nil, errors.WrapInvalid(nil, "flowstore", "NewStore", "nats client cannot be nil")
	}

	ctx := context.Background()
	bucket, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      "semstreams_flows",
		Description: "Visual flow definitions and metadata",
		History:     10, // Keep last 10 versions for history/recovery
	})
	if err != nil {
		return nil, errors.WrapTransient(err, "flowstore", "NewStore", "create KV bucket")
	}

	return &Store{
		bucket:  bucket,
		kvStore: natsClient.NewKVStore(bucket),
	}, nil
}

// Create creates a new flow
func (s *Store) Create(ctx context.Context, flow *Flow) error {
	if flow == nil {
		return errors.WrapInvalid(nil, "flowstore", "Create", "flow cannot be nil")
	}
	if flow.ID == "" {
		return errors.WrapInvalid(nil, "flowstore", "Create", "flow ID cannot be empty")
	}

	// Initialize version and timestamps
	flow.Version = 1
	now := time.Now()
	flow.CreatedAt = now
	flow.UpdatedAt = now
	flow.LastModified = now

	// Default runtime state
	if flow.RuntimeState == "" {
		flow.RuntimeState = StateNotDeployed
	}

	// Marshal and store
	data, err := json.Marshal(flow)
	if err != nil {
		return errors.WrapFatal(err, "flowstore", "Create", "marshal flow")
	}

	// Use Create() to ensure it only creates if key doesn't exist
	if _, err := s.kvStore.Create(ctx, flow.ID, data); err != nil {
		if natsclient.IsKVConflictError(err) {
			return errors.WrapInvalid(err, "flowstore", "Create", "flow already exists")
		}
		return errors.WrapTransient(err, "flowstore", "Create", "create in KV")
	}

	return nil
}

// Get retrieves a flow by ID
func (s *Store) Get(ctx context.Context, id string) (*Flow, error) {
	if id == "" {
		return nil, errors.WrapInvalid(nil, "flowstore", "Get", "flow ID cannot be empty")
	}

	entry, err := s.kvStore.Get(ctx, id)
	if err != nil {
		return nil, errors.WrapTransient(err, "flowstore", "Get", "get from KV")
	}

	var flow Flow
	if err := json.Unmarshal(entry.Value, &flow); err != nil {
		return nil, errors.WrapFatal(err, "flowstore", "Get", "unmarshal flow")
	}

	return &flow, nil
}

// Update updates an existing flow with optimistic concurrency control
func (s *Store) Update(ctx context.Context, flow *Flow) error {
	if flow == nil {
		return errors.WrapInvalid(nil, "flowstore", "Update", "flow cannot be nil")
	}
	if flow.ID == "" {
		return errors.WrapInvalid(nil, "flowstore", "Update", "flow ID cannot be empty")
	}

	// Get current version from KV
	current, err := s.Get(ctx, flow.ID)
	if err != nil {
		return errors.WrapTransient(err, "flowstore", "Update", "get current version")
	}

	// Check version for optimistic concurrency
	if current.Version != flow.Version {
		return errors.WrapInvalid(
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
		return errors.WrapFatal(err, "flowstore", "Update", "marshal flow")
	}

	if _, err := s.kvStore.Put(ctx, flow.ID, data); err != nil {
		return errors.WrapTransient(err, "flowstore", "Update", "put to KV")
	}

	return nil
}

// Delete removes a flow by ID
func (s *Store) Delete(ctx context.Context, id string) error {
	if id == "" {
		return errors.WrapInvalid(nil, "flowstore", "Delete", "flow ID cannot be empty")
	}

	if err := s.kvStore.Delete(ctx, id); err != nil {
		return errors.WrapTransient(err, "flowstore", "Delete", "delete from KV")
	}

	return nil
}

// List retrieves all flows
func (s *Store) List(ctx context.Context) ([]*Flow, error) {
	keys, err := s.bucket.Keys(ctx)
	if err != nil {
		return nil, errors.WrapTransient(err, "flowstore", "List", "list KV keys")
	}

	flows := make([]*Flow, 0, len(keys))
	for _, key := range keys {
		flow, err := s.Get(ctx, key)
		if err != nil {
			return nil, errors.WrapTransient(err, "flowstore", "List",
				fmt.Sprintf("get flow %s", key))
		}
		flows = append(flows, flow)
	}

	return flows, nil
}
