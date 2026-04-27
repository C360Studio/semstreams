package payloadregistry

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// TestPayload is a simple payload implementation for testing
type TestPayload struct {
	Message string `json:"message"`
	Value   int    `json:"value"`
}

func (tp *TestPayload) Validate() error {
	if tp.Message == "" {
		return fmt.Errorf("message field is required")
	}
	return nil
}

func (tp *TestPayload) MarshalJSON() ([]byte, error) {
	// Use alias to avoid infinite recursion
	type Alias TestPayload
	return json.Marshal((*Alias)(tp))
}

func (tp *TestPayload) UnmarshalJSON(data []byte) error {
	// Use alias to avoid infinite recursion
	type Alias TestPayload
	return json.Unmarshal(data, (*Alias)(tp))
}

// PayloadTestFactory creates test payloads
func PayloadTestFactory() any {
	return &TestPayload{}
}

// testBuilder creates a test payload from field mappings
func testBuilder(fields map[string]any) (any, error) {
	payload := &TestPayload{}

	if msg, ok := fields["message"].(string); ok {
		payload.Message = msg
	}

	if val, ok := fields["value"].(int); ok {
		payload.Value = val
	} else if val, ok := fields["value"].(float64); ok {
		// JSON numbers decode as float64
		payload.Value = int(val)
	}

	return payload, nil
}

func TestRegistry_NewRegistry(t *testing.T) {
	registry := New()
	if registry == nil {
		t.Fatal("New() returned nil")
	}

	if registry.registrations == nil {
		t.Error("registry.registrations should be initialized")
	}

	if len(registry.registrations) != 0 {
		t.Error("new registry should be empty")
	}
}

func TestRegistry_RegisterPayload_Success(t *testing.T) {
	registry := New()

	registration := &Registration{
		Factory:     PayloadTestFactory,
		Builder:     testBuilder,
		Domain:      "test",
		Category:    "sample",
		Version:     "v1",
		Description: "Test payload for unit tests",
		Example: map[string]any{
			"message": "hello world",
			"value":   42,
		},
	}

	err := registry.Register(registration)
	if err != nil {
		t.Fatalf("RegisterPayload() failed: %v", err)
	}

	// Verify registration was stored
	if len(registry.registrations) != 1 {
		t.Error("registry should contain exactly one registration")
	}

	stored, exists := registry.registrations["test.sample.v1"]
	if !exists {
		t.Error("registration was not stored with correct key")
	}

	if stored.Domain != "test" || stored.Category != "sample" || stored.Version != "v1" {
		t.Error("stored registration has incorrect metadata")
	}
}

func TestRegistry_RegisterPayload_Validation(t *testing.T) {
	registry := New()

	tests := []struct {
		name         string
		registration *Registration
		expectError  string
	}{
		{
			name:         "nil registration",
			registration: nil,
			expectError:  "registration",
		},
		{
			name: "nil factory",
			registration: &Registration{
				Builder:  testBuilder,
				Domain:   "test",
				Category: "sample",
				Version:  "v1",
			},
			expectError: "factory",
		},
		{
			name: "empty domain",
			registration: &Registration{
				Factory:  PayloadTestFactory,
				Builder:  testBuilder,
				Category: "sample",
				Version:  "v1",
			},
			expectError: "domain",
		},
		{
			name: "empty category",
			registration: &Registration{
				Factory: PayloadTestFactory,
				Builder: testBuilder,
				Domain:  "test",
				Version: "v1",
			},
			expectError: "category",
		},
		{
			name: "empty version",
			registration: &Registration{
				Factory:  PayloadTestFactory,
				Builder:  testBuilder,
				Domain:   "test",
				Category: "sample",
			},
			expectError: "version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := registry.Register(tt.registration)
			if err == nil {
				t.Errorf("expected error containing %q, got nil", tt.expectError)
				return
			}

			if !strings.Contains(err.Error(), tt.expectError) {
				t.Errorf("expected error containing %q, got %q", tt.expectError, err.Error())
			}
		})
	}
}

func TestRegistry_RegisterPayload_DuplicateError(t *testing.T) {
	registry := New()

	registration := &Registration{
		Factory:  PayloadTestFactory,
		Builder:  testBuilder,
		Domain:   "test",
		Category: "sample",
		Version:  "v1",
	}

	// First registration should succeed
	err := registry.Register(registration)
	if err != nil {
		t.Fatalf("first registration failed: %v", err)
	}

	// Second registration should fail
	err = registry.Register(registration)
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}

	expectedError := "payload type 'test.sample.v1' is already registered"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("expected error containing %q, got %q", expectedError, err.Error())
	}
}

func TestRegistry_CreatePayload_Success(t *testing.T) {
	registry := New()

	registration := &Registration{
		Factory:  PayloadTestFactory,
		Builder:  testBuilder,
		Domain:   "test",
		Category: "sample",
		Version:  "v1",
	}

	err := registry.Register(registration)
	if err != nil {
		t.Fatalf("RegisterPayload() failed: %v", err)
	}

	payload := registry.Create("test", "sample", "v1")
	if payload == nil {
		t.Fatal("CreatePayload() returned nil for registered type")
	}

	// Verify it's the correct type
	testPayload, ok := payload.(*TestPayload)
	if !ok {
		t.Fatalf("payload is not a TestPayload, got %T", payload)
	}

	// Verify the payload was created correctly
	if testPayload.Message != "" || testPayload.Value != 0 {
		t.Error("expected zero-value payload")
	}
}

func TestRegistry_CreatePayload_UnknownType(t *testing.T) {
	registry := New()

	payload := registry.Create("unknown", "type", "v1")
	if payload != nil {
		t.Error("CreatePayload() should return nil for unknown type")
	}
}

func TestRegistry_GetRegistration(t *testing.T) {
	registry := New()

	registration := &Registration{
		Factory:     PayloadTestFactory,
		Builder:     testBuilder,
		Domain:      "test",
		Category:    "sample",
		Version:     "v1",
		Description: "Test payload",
		Example: map[string]any{
			"test": "data",
		},
	}

	err := registry.Register(registration)
	if err != nil {
		t.Fatalf("RegisterPayload() failed: %v", err)
	}

	// Test successful retrieval
	retrieved, exists := registry.GetRegistration("test.sample.v1")
	if !exists {
		t.Fatal("GetRegistration() should return true for existing type")
	}

	if retrieved.Domain != "test" || retrieved.Category != "sample" || retrieved.Version != "v1" {
		t.Error("retrieved registration has incorrect metadata")
	}

	if retrieved.Description != "Test payload" {
		t.Error("retrieved registration has incorrect description")
	}

	if retrieved.Factory != nil {
		t.Error("retrieved registration should not include factory for safety")
	}

	// Test non-existent type
	_, exists = registry.GetRegistration("nonexistent.type.v1")
	if exists {
		t.Error("GetRegistration() should return false for non-existent type")
	}
}

func TestRegistry_ListPayloads(t *testing.T) {
	registry := New()

	// Register multiple payloads
	registrations := []*Registration{
		{
			Factory:  PayloadTestFactory,
			Builder:  testBuilder,
			Domain:   "test",
			Category: "sample1",
			Version:  "v1",
		},
		{
			Factory:  PayloadTestFactory,
			Builder:  testBuilder,
			Domain:   "test",
			Category: "sample2",
			Version:  "v1",
		},
		{
			Factory:  PayloadTestFactory,
			Builder:  testBuilder,
			Domain:   "other",
			Category: "sample",
			Version:  "v2",
		},
	}

	for _, reg := range registrations {
		err := registry.Register(reg)
		if err != nil {
			t.Fatalf("RegisterPayload() failed: %v", err)
		}
	}

	list := registry.List()
	if len(list) != 3 {
		t.Errorf("expected 3 registrations, got %d", len(list))
	}

	expectedKeys := []string{"test.sample1.v1", "test.sample2.v1", "other.sample.v2"}
	for _, key := range expectedKeys {
		if _, exists := list[key]; !exists {
			t.Errorf("missing expected key: %s", key)
		}
	}

	// Verify factories are not included in the list
	for _, reg := range list {
		if reg.Factory != nil {
			t.Error("listed registration should not include factory for safety")
		}
	}
}

func TestRegistry_ListByDomain(t *testing.T) {
	registry := New()

	// Register payloads in different domains
	registrations := []*Registration{
		{Factory: PayloadTestFactory, Builder: testBuilder, Domain: "test", Category: "sample1", Version: "v1"},
		{Factory: PayloadTestFactory, Builder: testBuilder, Domain: "test", Category: "sample2", Version: "v1"},
		{Factory: PayloadTestFactory, Builder: testBuilder, Domain: "other", Category: "sample", Version: "v1"},
	}

	for _, reg := range registrations {
		err := registry.Register(reg)
		if err != nil {
			t.Fatalf("RegisterPayload() failed: %v", err)
		}
	}

	testDomainList := registry.ListByDomain("test")
	if len(testDomainList) != 2 {
		t.Errorf("expected 2 registrations in 'test' domain, got %d", len(testDomainList))
	}

	otherDomainList := registry.ListByDomain("other")
	if len(otherDomainList) != 1 {
		t.Errorf("expected 1 registration in 'other' domain, got %d", len(otherDomainList))
	}

	nonExistentList := registry.ListByDomain("nonexistent")
	if len(nonExistentList) != 0 {
		t.Errorf("expected 0 registrations in 'nonexistent' domain, got %d", len(nonExistentList))
	}
}

func TestRegistry_ThreadSafety(t *testing.T) {
	registry := New()

	// Test concurrent registration and access
	var wg sync.WaitGroup
	numGoroutines := 50

	// Concurrent registrations
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			registration := &Registration{
				Factory:  PayloadTestFactory,
				Builder:  testBuilder,
				Domain:   "test",
				Category: fmt.Sprintf("sample%d", id),
				Version:  "v1",
			}

			err := registry.Register(registration)
			if err != nil {
				t.Errorf("concurrent registration failed: %v", err)
			}
		}(i)
	}

	// Concurrent reads
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			// Try to create payload (may or may not exist yet due to concurrency)
			registry.Create("test", fmt.Sprintf("sample%d", id), "v1")

			// List operations should not panic
			registry.List()
			registry.ListByDomain("test")
		}(i)
	}

	wg.Wait()

	// Verify all registrations were successful
	list := registry.List()
	if len(list) != numGoroutines {
		t.Errorf("expected %d registrations after concurrent access, got %d", numGoroutines, len(list))
	}
}

func TestRegistration_MessageType(t *testing.T) {
	registration := &Registration{
		Domain:   "robotics",
		Category: "heartbeat",
		Version:  "v2",
	}

	expected := "robotics.heartbeat.v2"
	actual := registration.MessageType()

	if actual != expected {
		t.Errorf("MessageType() = %q, expected %q", actual, expected)
	}
}

func TestRegistry_RegisterPayload_BuilderOptional(t *testing.T) {
	registry := New()

	// Registration without Builder should succeed - Builder is optional
	registration := &Registration{
		Factory:  PayloadTestFactory,
		Builder:  nil, // Builder is optional
		Domain:   "test",
		Category: "sample",
		Version:  "v1",
	}

	err := registry.Register(registration)
	if err != nil {
		t.Fatalf("registration without Builder should succeed: %v", err)
	}

	// Verify payload can still be built using JSON fallback
	fields := map[string]any{
		"message": "fallback test",
		"value":   99,
	}

	payload, err := registry.Build("test", "sample", "v1", fields)
	if err != nil {
		t.Fatalf("BuildPayload() should use JSON fallback: %v", err)
	}

	testPayload, ok := payload.(*TestPayload)
	if !ok {
		t.Fatalf("payload should be *TestPayload, got %T", payload)
	}

	if testPayload.Message != "fallback test" {
		t.Errorf("expected message 'fallback test', got %q", testPayload.Message)
	}

	if testPayload.Value != 99 {
		t.Errorf("expected value 99, got %d", testPayload.Value)
	}
}

func TestRegistry_BuildPayload_Success(t *testing.T) {
	registry := New()

	registration := &Registration{
		Factory:  PayloadTestFactory,
		Builder:  testBuilder,
		Domain:   "test",
		Category: "sample",
		Version:  "v1",
	}

	err := registry.Register(registration)
	if err != nil {
		t.Fatalf("RegisterPayload() failed: %v", err)
	}

	fields := map[string]any{
		"message": "hello",
		"value":   42,
	}

	payload, err := registry.Build("test", "sample", "v1", fields)
	if err != nil {
		t.Fatalf("BuildPayload() failed: %v", err)
	}

	if payload == nil {
		t.Fatal("BuildPayload() returned nil payload")
	}

	testPayload, ok := payload.(*TestPayload)
	if !ok {
		t.Fatalf("payload is not a TestPayload, got %T", payload)
	}

	if testPayload.Message != "hello" {
		t.Errorf("expected message 'hello', got %q", testPayload.Message)
	}

	if testPayload.Value != 42 {
		t.Errorf("expected value 42, got %d", testPayload.Value)
	}
}

func TestRegistry_BuildPayload_UnknownType(t *testing.T) {
	registry := New()

	fields := map[string]any{
		"message": "hello",
		"value":   42,
	}

	payload, err := registry.Build("unknown", "type", "v1", fields)
	if err == nil {
		t.Fatal("expected error for unknown payload type")
	}

	if payload != nil {
		t.Error("expected nil payload for unknown type")
	}

	expectedError := "payload type \"unknown.type.v1\" not registered"
	if err.Error() != expectedError {
		t.Errorf("expected error %q, got %q", expectedError, err.Error())
	}
}
