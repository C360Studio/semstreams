// Package testutil provides core test utilities for StreamKit with ZERO semantic concepts.
// NO EntityID, NO MAVLink, NO SOSA/SSN, NO semantic domain knowledge.
package testutil

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

// MockComponent is a generic component for testing that implements basic lifecycle.
// This is core infrastructure - no domain concepts.
type MockComponent struct {
	mu sync.Mutex

	// Lifecycle control
	StartFunc func(ctx context.Context) error
	StopFunc  func(ctx context.Context) error

	// Processing
	ProcessFunc func(data any) (any, error)

	// State tracking
	Started bool
	Stopped bool
	Enabled bool

	// Call counts for verification
	StartCalls   int
	StopCalls    int
	ProcessCalls int
}

// NewMockComponent creates a new mock component with default no-op implementations.
func NewMockComponent() *MockComponent {
	return &MockComponent{
		Enabled: true,
		StartFunc: func(_ context.Context) error {
			return nil
		},
		StopFunc: func(_ context.Context) error {
			return nil
		},
		ProcessFunc: func(data any) (any, error) {
			return data, nil
		},
	}
}

// Start starts the mock component.
func (m *MockComponent) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.StartCalls++
	m.Started = true

	if m.StartFunc != nil {
		return m.StartFunc(ctx)
	}
	return nil
}

// Stop stops the mock component.
func (m *MockComponent) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.StopCalls++
	m.Stopped = true

	if m.StopFunc != nil {
		return m.StopFunc(ctx)
	}
	return nil
}

// Process processes data through the mock component.
func (m *MockComponent) Process(data any) (any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ProcessCalls++

	if m.ProcessFunc != nil {
		return m.ProcessFunc(data)
	}
	return data, nil
}

// GetInfo returns mock component info (core metadata only).
func (m *MockComponent) GetInfo() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()

	return map[string]any{
		"type":        "mock",
		"protocol":    "test",
		"domain":      "testing",
		"description": "Mock component for testing",
		"started":     m.Started,
		"stopped":     m.Stopped,
		"enabled":     m.Enabled,
	}
}

// MockPort represents a generic NATS port for testing (core message passing).
type MockPort struct {
	Subject  string
	Messages []any
	mu       sync.Mutex
}

// NewMockPort creates a new mock port.
func NewMockPort(subject string) *MockPort {
	return &MockPort{
		Subject:  subject,
		Messages: make([]any, 0),
	}
}

// Publish publishes a message to the mock port.
func (p *MockPort) Publish(msg any) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.Messages = append(p.Messages, msg)
	return nil
}

// GetMessages returns all published messages.
func (p *MockPort) GetMessages() []any {
	p.mu.Lock()
	defer p.mu.Unlock()

	result := make([]any, len(p.Messages))
	copy(result, p.Messages)
	return result
}

// Clear clears all messages.
func (p *MockPort) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.Messages = make([]any, 0)
}

// MockError is a generic error for testing error paths.
type MockError struct {
	Message string
	Code    string
}

func (e *MockError) Error() string {
	return e.Message
}

// NewMockError creates a new mock error.
func NewMockError(message, code string) error {
	return &MockError{
		Message: message,
		Code:    code,
	}
}

// Common test errors
var (
	ErrMockFailed     = errors.New("mock operation failed")
	ErrMockTimeout    = errors.New("mock operation timed out")
	ErrMockNotFound   = errors.New("mock resource not found")
	ErrMockInvalid    = errors.New("mock invalid input")
	ErrMockConnection = errors.New("mock connection error")
)

// MockConfig represents a generic configuration for testing.
type MockConfig struct {
	Name       string         `json:"name"`
	Enabled    bool           `json:"enabled"`
	Port       int            `json:"port"`
	Host       string         `json:"host"`
	Timeout    int            `json:"timeout"`
	BufferSize int            `json:"buffer_size"`
	Metadata   map[string]any `json:"metadata"`
}

// NewMockConfig creates a mock config with sensible defaults.
func NewMockConfig() *MockConfig {
	return &MockConfig{
		Name:       "test-component",
		Enabled:    true,
		Port:       8080,
		Host:       "localhost",
		Timeout:    30,
		BufferSize: 1000,
		Metadata:   make(map[string]any),
	}
}

// ToJSON converts the mock config to JSON.
func (c *MockConfig) ToJSON() ([]byte, error) {
	return json.Marshal(c)
}

// ToRawMessage converts the mock config to json.RawMessage.
func (c *MockConfig) ToRawMessage() (json.RawMessage, error) {
	return c.ToJSON()
}
