package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	"github.com/c360/semstreams/natsclient"
)

// KVTestHelper provides utilities for KV-based testing with JSON-only format
type KVTestHelper struct {
	t       *testing.T
	kvStore *natsclient.KVStore // Use the new KVStore from KV-000
	bucket  string
	ctx     context.Context
}

// NewKVTestHelper creates an isolated test KV bucket
func NewKVTestHelper(t *testing.T, nc *natsclient.Client) *KVTestHelper {
	ctx := context.Background()
	js, err := nc.JetStream()
	require.NoError(t, err)

	// Create unique bucket name for test isolation
	// NATS bucket names must be alphanumeric with underscore s/hyphens only
	testName := strings.ReplaceAll(strings.ReplaceAll(t.Name(), "/", "_"), " ", "_")
	// Keep bucket name under reasonable length and make it unique
	timestamp := time.Now().UnixNano()
	bucketName := fmt.Sprintf("semstreams_test_%s_%d",
		testName, timestamp)
	// Ensure bucket name doesn't exceed NATS limits (simple truncation if needed)
	if len(bucketName) > 64 {
		bucketName = fmt.Sprintf("semstreams_test_%d", timestamp)
	}

	// Create KV bucket with proper config
	kv, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      bucketName,
		Description: "Test configuration bucket",
		History:     5,
		Replicas:    1,
	})
	require.NoError(t, err)

	// Create KVStore using the new abstraction from KV-000
	kvStore := nc.NewKVStore(kv)

	// Register cleanup
	t.Cleanup(func() {
		js.DeleteKeyValue(ctx, bucketName)
	})

	return &KVTestHelper{
		t:       t,
		kvStore: kvStore,
		bucket:  bucketName,
		ctx:     ctx,
	}
}

// WriteServiceConfig writes a complete service configuration (JSON-only)
func (h *KVTestHelper) WriteServiceConfig(service string, config map[string]any) uint64 {
	data, err := json.Marshal(config)
	require.NoError(h.t, err)

	key := fmt.Sprintf("services.%s", service)
	rev, err := h.kvStore.Put(h.ctx, key, data)
	require.NoError(h.t, err)

	return rev
}

// UpdateServiceConfig updates with optimistic locking (uses KVStore CAS)
func (h *KVTestHelper) UpdateServiceConfig(service string, updateFn func(config map[string]any) error) error {
	key := fmt.Sprintf("services.%s", service)

	// Use KVStore's UpdateJSON method from KV-000
	return h.kvStore.UpdateJSON(h.ctx, key, updateFn)
}

// GetServiceConfig reads current service configuration
func (h *KVTestHelper) GetServiceConfig(service string) (map[string]any, uint64, error) {
	key := fmt.Sprintf("services.%s", service)
	entry, err := h.kvStore.Get(h.ctx, key)
	if err != nil {
		return nil, 0, err
	}

	var config map[string]any
	err = json.Unmarshal(entry.Value, &config)
	return config, entry.Revision, err
}

// SimulateConcurrentUpdate tests optimistic locking behavior
func (h *KVTestHelper) SimulateConcurrentUpdate(service string) error {
	// Read current state
	config, rev, err := h.GetServiceConfig(service)
	if err != nil {
		return fmt.Errorf("get config: %w", err)
	}

	// Someone else updates (simulated)
	config["concurrent"] = true
	data, _ := json.Marshal(config)
	h.kvStore.Put(h.ctx, fmt.Sprintf("services.%s", service), data)

	// Try to update with old revision (should fail)
	config["enabled"] = false
	data, _ = json.Marshal(config)
	_, err = h.kvStore.Update(h.ctx, fmt.Sprintf("services.%s", service), data, rev)
	return err // Expect ErrKVRevisionMismatch
}

// WaitForConfigPropagation waits for config changes to propagate
func (h *KVTestHelper) WaitForConfigPropagation(_ time.Duration) bool {
	// Simple wait for now - real implementation would check watchers
	time.Sleep(50 * time.Millisecond)
	return true
}

// AssertValidKVKey validates key format per KV schema requirements
func (h *KVTestHelper) AssertValidKVKey(key string) {
	// Key must be lowercase with dots as separators
	require.Regexp(h.t, `^[a-z0-9_]+(\.[a-z0-9_]+)*$`, key,
		"Key must follow schema format (lowercase, dots for hierarchy)")

	// Key length limit
	require.LessOrEqual(h.t, len(key), 256,
		"Key must not exceed 256 characters")
}

// WriteComponentConfig writes component configuration (for future use)
func (h *KVTestHelper) WriteComponentConfig(componentType, name string, config map[string]any) uint64 {
	data, err := json.Marshal(config)
	require.NoError(h.t, err)

	key := fmt.Sprintf("components.%s.%s", componentType, name)
	rev, err := h.kvStore.Put(h.ctx, key, data)
	require.NoError(h.t, err)

	return rev
}
