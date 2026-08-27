package persona

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// bucketName is the NATS KV bucket where personas persist. Convention
// matches the other Pattern-B buckets: uppercase, type-keyed.
const bucketName = "PERSONAS"

// Manager provides CRUD against the PERSONAS KV bucket. Pattern-B
// per ADR-029. Not safe for concurrent use from a single caller only in
// the narrow sense that the underlying KVStore handles concurrency —
// multiple goroutines sharing a Manager instance is fine.
type Manager struct {
	kvStore *natsclient.KVStore
}

// NewManager opens (or creates) the PERSONAS bucket and returns a
// Manager for it. Returning an error on bucket-open failure lets main
// decide whether to skip registration or fail fast.
func NewManager(natsClient *natsclient.Client) (*Manager, error) {
	if natsClient == nil {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "persona", "NewManager", "nats client cannot be nil")
	}
	bucket, err := natsClient.CreateKeyValueBucket(context.Background(), jetstream.KeyValueConfig{
		Bucket:      bucketName,
		Description: "Agent-role prompt-fragment definitions",
		History:     5,
	})
	if err != nil {
		return nil, errs.WrapTransient(err, "persona", "NewManager", "create KV bucket")
	}
	return &Manager{kvStore: natsClient.NewKVStore(bucket)}, nil
}

// Create stores a new persona. Fails if ID already exists so callers
// must use Update for edits, which gives Pattern-B CRUD its
// optimistic-create semantics.
func (m *Manager) Create(ctx context.Context, p *Persona) error {
	if p == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "persona", "Create", "persona cannot be nil")
	}
	if err := p.Validate(); err != nil {
		return errs.WrapInvalid(err, "persona", "Create", "validate")
	}
	data, err := json.Marshal(p)
	if err != nil {
		return errs.WrapFatal(err, "persona", "Create", "marshal")
	}
	if _, err := m.kvStore.Create(ctx, p.ID, data); err != nil {
		if natsclient.IsKVConflictError(err) {
			return errs.WrapInvalid(err, "persona", "Create", fmt.Sprintf("persona %q already exists", p.ID))
		}
		return errs.WrapTransient(err, "persona", "Create", "create in KV")
	}
	return nil
}

// Update overwrites an existing persona. There is no version-based
// optimistic concurrency here — personas are edited less often and rarely
// concurrently; last-writer-wins is fine.
// If concurrent edit becomes a real concern we add a Version field to
// Persona and a CAS check.
func (m *Manager) Update(ctx context.Context, p *Persona) error {
	if p == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "persona", "Update", "persona cannot be nil")
	}
	if err := p.Validate(); err != nil {
		return errs.WrapInvalid(err, "persona", "Update", "validate")
	}
	// Confirm it exists so we don't silently "update" a missing record.
	if _, err := m.Get(ctx, p.ID); err != nil {
		return err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return errs.WrapFatal(err, "persona", "Update", "marshal")
	}
	if _, err := m.kvStore.Put(ctx, p.ID, data); err != nil {
		return errs.WrapTransient(err, "persona", "Update", "put to KV")
	}
	return nil
}

// Delete removes a persona by ID. Missing ID returns the underlying
// KV error so callers can distinguish "not found" from "transport
// failure" if they care.
func (m *Manager) Delete(ctx context.Context, id string) error {
	if id == "" {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "persona", "Delete", "id cannot be empty")
	}
	if err := m.kvStore.Delete(ctx, id); err != nil {
		return errs.WrapTransient(err, "persona", "Delete", "delete from KV")
	}
	return nil
}

// Get retrieves a persona by ID.
func (m *Manager) Get(ctx context.Context, id string) (*Persona, error) {
	if id == "" {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "persona", "Get", "id cannot be empty")
	}
	entry, err := m.kvStore.Get(ctx, id)
	if err != nil {
		return nil, errs.WrapTransient(err, "persona", "Get", "get from KV")
	}
	var p Persona
	if err := json.Unmarshal(entry.Value, &p); err != nil {
		return nil, errs.WrapFatal(err, "persona", "Get", "unmarshal")
	}
	return &p, nil
}

// Upsert creates or updates a persona. It attempts Create first; if and
// only if Create fails because the key already exists (ErrKVKeyExists),
// it falls through to Update. Any other Create error — transient NATS
// failure, invalid input, etc. — is returned directly so callers see
// the real cause rather than a misleading Update error.
//
// Update failure after a conflict-detected Create is unlikely (would
// require a concurrent Delete between the two calls) but propagated as
// a transient error if it occurs.
func (m *Manager) Upsert(ctx context.Context, p *Persona) error {
	if err := m.Create(ctx, p); err != nil {
		if !errors.Is(err, natsclient.ErrKVKeyExists) {
			return err
		}
		return m.Update(ctx, p)
	}
	return nil
}

// List returns every persona in the bucket. Small registries expected
// (dozens, not thousands); no pagination added. If a product's registry
// grows large, pagination lands alongside the use case.
func (m *Manager) List(ctx context.Context) (map[string]*Persona, error) {
	keys, err := m.kvStore.Keys(ctx)
	if err != nil {
		return nil, errs.WrapTransient(err, "persona", "List", "list KV keys")
	}
	personas := make(map[string]*Persona, len(keys))
	for _, key := range keys {
		p, err := m.Get(ctx, key)
		if err != nil {
			// Skip records that fail to decode rather than failing the
			// whole list. Operators inspecting the bucket will see the
			// stored bytes via `nats kv` tooling.
			continue
		}
		personas[key] = p
	}
	return personas, nil
}
