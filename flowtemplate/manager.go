package flowtemplate

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// bucketName is the NATS KV bucket where flow templates persist.
const bucketName = "FLOW_TEMPLATES"

// Manager provides CRUD against the FLOW_TEMPLATES KV bucket plus the
// Instantiate operation. Pattern-B per ADR-029 with one type-specific
// extension (Instantiate) on top of the canonical Save/Get/Delete/List.
type Manager struct {
	kvStore *natsclient.KVStore
}

// NewManager opens (or creates) the FLOW_TEMPLATES bucket.
func NewManager(natsClient *natsclient.Client) (*Manager, error) {
	if natsClient == nil {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "flowtemplate", "NewManager", "nats client cannot be nil")
	}
	bucket, err := natsClient.CreateKeyValueBucket(context.Background(), jetstream.KeyValueConfig{
		Bucket:      bucketName,
		Description: "Parameterised flow definitions (templates)",
		History:     5,
	})
	if err != nil {
		return nil, errs.WrapTransient(err, "flowtemplate", "NewManager", "create KV bucket")
	}
	return &Manager{kvStore: natsClient.NewKVStore(bucket)}, nil
}

// Create stores a new template. Fails if ID exists.
func (m *Manager) Create(ctx context.Context, t *Template) error {
	if t == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "flowtemplate", "Create", "template cannot be nil")
	}
	if err := t.Validate(); err != nil {
		return errs.WrapInvalid(err, "flowtemplate", "Create", "validate")
	}
	data, err := json.Marshal(t)
	if err != nil {
		return errs.WrapFatal(err, "flowtemplate", "Create", "marshal")
	}
	if _, err := m.kvStore.Create(ctx, t.ID, data); err != nil {
		if natsclient.IsKVConflictError(err) {
			return errs.WrapInvalid(err, "flowtemplate", "Create", fmt.Sprintf("template %q already exists", t.ID))
		}
		return errs.WrapTransient(err, "flowtemplate", "Create", "create in KV")
	}
	return nil
}

// Update overwrites an existing template. Last-writer-wins; see
// persona.Manager.Update for the same rationale.
func (m *Manager) Update(ctx context.Context, t *Template) error {
	if t == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "flowtemplate", "Update", "template cannot be nil")
	}
	if err := t.Validate(); err != nil {
		return errs.WrapInvalid(err, "flowtemplate", "Update", "validate")
	}
	if _, err := m.Get(ctx, t.ID); err != nil {
		return err
	}
	data, err := json.Marshal(t)
	if err != nil {
		return errs.WrapFatal(err, "flowtemplate", "Update", "marshal")
	}
	if _, err := m.kvStore.Put(ctx, t.ID, data); err != nil {
		return errs.WrapTransient(err, "flowtemplate", "Update", "put to KV")
	}
	return nil
}

// Delete removes a template by ID.
func (m *Manager) Delete(ctx context.Context, id string) error {
	if id == "" {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "flowtemplate", "Delete", "id cannot be empty")
	}
	if err := m.kvStore.Delete(ctx, id); err != nil {
		return errs.WrapTransient(err, "flowtemplate", "Delete", "delete from KV")
	}
	return nil
}

// Get retrieves a template by ID.
func (m *Manager) Get(ctx context.Context, id string) (*Template, error) {
	if id == "" {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "flowtemplate", "Get", "id cannot be empty")
	}
	entry, err := m.kvStore.Get(ctx, id)
	if err != nil {
		return nil, errs.WrapTransient(err, "flowtemplate", "Get", "get from KV")
	}
	var t Template
	if err := json.Unmarshal(entry.Value, &t); err != nil {
		return nil, errs.WrapFatal(err, "flowtemplate", "Get", "unmarshal")
	}
	return &t, nil
}

// List returns every template keyed by ID. Small bucket expected — no
// pagination — same reasoning as persona.Manager.List.
func (m *Manager) List(ctx context.Context) (map[string]*Template, error) {
	keys, err := m.kvStore.Keys(ctx)
	if err != nil {
		return nil, errs.WrapTransient(err, "flowtemplate", "List", "list KV keys")
	}
	out := make(map[string]*Template, len(keys))
	for _, key := range keys {
		t, err := m.Get(ctx, key)
		if err != nil {
			continue
		}
		out[key] = t
	}
	return out, nil
}
