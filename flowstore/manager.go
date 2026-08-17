package flowstore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/internal/jsoncanon"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/google/uuid"
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

	activationMu sync.RWMutex
	bootID       string
	bootConfig   config.ComponentConfigs
	desired      func() config.ComponentConfigs
}

// NewManager creates a new flow store
func NewManager(ctx context.Context, natsClient *natsclient.Client) (*Manager, error) {
	if ctx == nil {
		return nil, errs.WrapInvalid(errs.ErrInvalidData, "flowstore", "NewManager", "context cannot be nil")
	}
	if natsClient == nil {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "flowstore", "NewManager", "nats client cannot be nil")
	}

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
	if flow.DesiredState == "" {
		flow.DesiredState = DesiredAbsent
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
	data, err := marshalPersistedFlow(flow)
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

	s.decorate(&flow)
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
	data, err := marshalPersistedFlow(flow)
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

// SealBootActivation captures the exact desired component snapshot selected by
// this boot. Later desired writes are compared with this immutable snapshot;
// they never mutate it.
func (s *Manager) SealBootActivation(desired *config.Manager) {
	if desired == nil {
		return
	}
	safe := desired.GetConfig()
	if safe == nil {
		return
	}
	current := safe.Get()
	s.activationMu.Lock()
	s.bootID = newBootID()
	s.bootConfig = cloneComponentConfigs(current.Components)
	s.desired = func() config.ComponentConfigs {
		safe := desired.GetConfig()
		if safe == nil {
			return nil
		}
		return safe.Get().Components
	}
	s.activationMu.Unlock()
}

func (s *Manager) decorate(flow *Flow) {
	if flow == nil {
		return
	}
	s.activationMu.RLock()
	bootID := s.bootID
	boot := cloneComponentConfigs(s.bootConfig)
	desiredReader := s.desired
	s.activationMu.RUnlock()

	flow.EffectiveState = EffectiveUnknown
	flow.DesiredProvenance = nil
	flow.BootAppliedProvenance = nil
	flow.RestartRequired = false
	if desiredReader == nil || bootID == "" {
		return
	}
	desired := desiredReader()
	desiredDigest := digestFlowComponents(flow, desired)
	bootDigest := digestFlowComponents(flow, boot)
	flow.DesiredProvenance = &ConfigProvenance{Digest: desiredDigest}
	// The sealed boot digest is sufficient to compute drift, but it is not
	// evidence that the runtime applied that configuration. Without an
	// authoritative observer, boot-applied provenance remains unknown.
	flow.RestartRequired = desiredDigest != bootDigest
}

func marshalPersistedFlow(flow *Flow) ([]byte, error) {
	encoded, err := json.Marshal(flow)
	if err != nil {
		return nil, err
	}
	var persisted map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		return nil, err
	}
	delete(persisted, "effective_state")
	delete(persisted, "desired_provenance")
	delete(persisted, "boot_applied_provenance")
	delete(persisted, "restart_required")
	return json.Marshal(persisted)
}

func digestFlowComponents(flow *Flow, components config.ComponentConfigs) string {
	selected := make(config.ComponentConfigs)
	if flow.DesiredState != DesiredAbsent {
		for _, node := range flow.Nodes {
			if componentConfig, ok := components[node.Name]; ok {
				if canonical, valid := jsoncanon.Normalize(componentConfig.Config); valid {
					componentConfig.Config = canonical
				}
				selected[node.Name] = componentConfig
			}
		}
	}
	encoded, _ := json.Marshal(selected)
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("sha256:%x", digest[:])
}

func cloneComponentConfigs(source config.ComponentConfigs) config.ComponentConfigs {
	result := make(config.ComponentConfigs, len(source))
	for name, componentConfig := range source {
		cloned := componentConfig
		cloned.Config = append(json.RawMessage(nil), componentConfig.Config...)
		result[name] = cloned
	}
	return result
}

func newBootID() string {
	return uuid.NewString()
}
