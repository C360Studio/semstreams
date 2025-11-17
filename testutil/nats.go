package testutil

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// MockNATSClient is a simple in-memory NATS client for testing (core message passing).
// Matches the natsclient.Client interface for Subscribe/Publish methods.
// Thread-safe for concurrent use from multiple goroutines.
type MockNATSClient struct {
	mu            sync.RWMutex
	messages      map[string][][]byte
	subscriptions map[string][]func(context.Context, []byte)
	closed        bool
}

// NewMockNATSClient creates a new mock NATS client.
func NewMockNATSClient() *MockNATSClient {
	return &MockNATSClient{
		messages:      make(map[string][][]byte),
		subscriptions: make(map[string][]func(context.Context, []byte)),
		closed:        false,
	}
}

// Publish publishes a message to a subject (matches natsclient.Client signature).
func (c *MockNATSClient) Publish(ctx context.Context, subject string, data []byte) error {
	c.mu.Lock()

	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("client is closed")
	}

	if c.messages[subject] == nil {
		c.messages[subject] = make([][]byte, 0)
	}

	c.messages[subject] = append(c.messages[subject], data)

	// Copy handlers to avoid holding lock during callbacks
	var handlers []func(context.Context, []byte)
	if h, ok := c.subscriptions[subject]; ok {
		handlers = make([]func(context.Context, []byte), len(h))
		copy(handlers, h)
	}
	c.mu.Unlock()

	// Call handlers outside the lock to prevent deadlock
	// Create per-message context with 30s timeout (matches real client)
	for _, handler := range handlers {
		msgCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		handler(msgCtx, data)
		cancel()
	}

	return nil
}

// Subscribe creates a subscription to a subject (matches natsclient.Client signature).
func (c *MockNATSClient) Subscribe(ctx context.Context, subject string, handler func(context.Context, []byte)) error {
	// Check for cancellation before subscribing
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return fmt.Errorf("client is closed")
	}

	if c.subscriptions[subject] == nil {
		c.subscriptions[subject] = make([]func(context.Context, []byte), 0)
	}

	c.subscriptions[subject] = append(c.subscriptions[subject], handler)
	return nil
}

// GetMessages returns all messages for a subject as [][]byte.
func (c *MockNATSClient) GetMessages(subject string) [][]byte {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Return a copy to prevent races on the returned slice
	msgs := c.messages[subject]
	if msgs == nil {
		return nil
	}
	result := make([][]byte, len(msgs))
	copy(result, msgs)
	return result
}

// GetMessagesAsInterface returns all messages for a subject (for backward compatibility).
func (c *MockNATSClient) GetMessagesAsInterface(subject string) []any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	msgs := c.messages[subject]
	result := make([]any, len(msgs))
	for i, msg := range msgs {
		result[i] = msg
	}
	return result
}

// GetMessageCount returns the number of messages on a subject.
func (c *MockNATSClient) GetMessageCount(subject string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.messages[subject])
}

// Clear clears all messages from a subject.
func (c *MockNATSClient) Clear(subject string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages[subject] = make([][]byte, 0)
}

// ClearAll clears all messages from all subjects.
func (c *MockNATSClient) ClearAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = make(map[string][][]byte)
}

// Close closes the mock client.
func (c *MockNATSClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// IsClosed returns whether the client is closed.
func (c *MockNATSClient) IsClosed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.closed
}

// MockKVStore is a simple in-memory key-value store for testing.
// Thread-safe for concurrent use from multiple goroutines.
type MockKVStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// NewMockKVStore creates a new mock KV store.
func NewMockKVStore() *MockKVStore {
	return &MockKVStore{
		data: make(map[string][]byte),
	}
}

// Put stores a value.
func (kv *MockKVStore) Put(key string, value []byte) error {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	kv.data[key] = value
	return nil
}

// Get retrieves a value.
func (kv *MockKVStore) Get(key string) ([]byte, error) {
	kv.mu.RLock()
	defer kv.mu.RUnlock()

	if val, ok := kv.data[key]; ok {
		// Return a copy to prevent races on the returned slice
		result := make([]byte, len(val))
		copy(result, val)
		return result, nil
	}
	return nil, fmt.Errorf("key not found: %s", key)
}

// Delete removes a key.
func (kv *MockKVStore) Delete(key string) error {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	delete(kv.data, key)
	return nil
}

// Keys returns all keys.
func (kv *MockKVStore) Keys() []string {
	kv.mu.RLock()
	defer kv.mu.RUnlock()

	keys := make([]string, 0, len(kv.data))
	for k := range kv.data {
		keys = append(keys, k)
	}
	return keys
}

// Clear removes all keys.
func (kv *MockKVStore) Clear() {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	kv.data = make(map[string][]byte)
}

// WaitForMessage is a test helper that waits for a message on a subject (with timeout).
func WaitForMessage(t *testing.T, client *MockNATSClient, subject string, timeout time.Duration) []byte {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timeout waiting for message on subject %s", subject)
			return nil
		case <-ticker.C:
			messages := client.GetMessages(subject)
			if len(messages) > 0 {
				return messages[len(messages)-1] // Return latest message
			}
		}
	}
}

// WaitForMessageCount waits for a specific number of messages (with timeout).
func WaitForMessageCount(t *testing.T, client *MockNATSClient, subject string, count int, timeout time.Duration) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			got := client.GetMessageCount(subject)
			t.Fatalf("timeout waiting for %d messages on subject %s (got %d)", count, subject, got)
			return
		case <-ticker.C:
			if client.GetMessageCount(subject) >= count {
				return
			}
		}
	}
}

// AssertMessageReceived checks that a message was received on a subject.
func AssertMessageReceived(t *testing.T, client *MockNATSClient, subject string) {
	t.Helper()

	messages := client.GetMessages(subject)
	if len(messages) == 0 {
		t.Fatalf("expected message on subject %s, got none", subject)
	}
}

// AssertNoMessages checks that no messages were received on a subject.
func AssertNoMessages(t *testing.T, client *MockNATSClient, subject string) {
	t.Helper()

	messages := client.GetMessages(subject)
	if len(messages) > 0 {
		t.Fatalf("expected no messages on subject %s, got %d", subject, len(messages))
	}
}
