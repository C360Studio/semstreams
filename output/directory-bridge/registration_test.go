package directorybridge

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	oasfgenerator "github.com/c360studio/semstreams/processor/oasf-generator"
)

func TestRegistrationManager_RegisterAgent(t *testing.T) {
	mock := NewMockDirectory()
	defer mock.Close()

	backend := NewHTTPBackend(mock.URL())
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	config := DefaultConfig()
	config.DirectoryURL = mock.URL()

	rm := NewRegistrationManager(backend, nil, config, logger)

	record := &oasfgenerator.OASFRecord{
		Name:          "TestAgent",
		Version:       "1.0.0",
		SchemaVersion: "1.0.0",
		Description:   "A test agent",
	}

	ctx := context.Background()
	err := rm.RegisterAgent(ctx, "entity-1", record, nil)

	if err != nil {
		t.Fatalf("RegisterAgent() error = %v", err)
	}

	// Verify registration stored
	reg := rm.GetRegistration("entity-1")
	if reg == nil {
		t.Fatal("expected registration to be stored")
	}

	if reg.EntityID != "entity-1" {
		t.Errorf("EntityID = %s, want entity-1", reg.EntityID)
	}

	if reg.RegistrationID == "" {
		t.Error("expected non-empty RegistrationID")
	}

	if reg.OASFRecord.Name != "TestAgent" {
		t.Errorf("OASFRecord.Name = %s, want TestAgent", reg.OASFRecord.Name)
	}
}

func TestRegistrationManager_UpdateRegistration(t *testing.T) {
	mock := NewMockDirectory()
	defer mock.Close()

	backend := NewHTTPBackend(mock.URL())
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	config := DefaultConfig()
	rm := NewRegistrationManager(backend, nil, config, logger)

	// Initial registration
	record1 := &oasfgenerator.OASFRecord{
		Name:    "TestAgent",
		Version: "1.0.0",
	}

	ctx := context.Background()
	err := rm.UpdateRegistration(ctx, "entity-1", record1)
	if err != nil {
		t.Fatalf("initial UpdateRegistration() error = %v", err)
	}

	// Update with new record
	record2 := &oasfgenerator.OASFRecord{
		Name:    "TestAgent",
		Version: "2.0.0",
	}

	err = rm.UpdateRegistration(ctx, "entity-1", record2)
	if err != nil {
		t.Fatalf("second UpdateRegistration() error = %v", err)
	}

	// Verify updated
	reg := rm.GetRegistration("entity-1")
	if reg == nil {
		t.Fatal("expected registration to exist")
	}

	if reg.OASFRecord.Version != "2.0.0" {
		t.Errorf("OASFRecord.Version = %s, want 2.0.0", reg.OASFRecord.Version)
	}

	// Should have called register twice
	if mock.RegisterCalls != 2 {
		t.Errorf("RegisterCalls = %d, want 2", mock.RegisterCalls)
	}
}

func TestRegistrationManager_Deregister(t *testing.T) {
	mock := NewMockDirectory()
	defer mock.Close()

	backend := NewHTTPBackend(mock.URL())
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	config := DefaultConfig()
	rm := NewRegistrationManager(backend, nil, config, logger)

	// Register first
	record := &oasfgenerator.OASFRecord{
		Name: "TestAgent",
	}

	ctx := context.Background()
	rm.RegisterAgent(ctx, "entity-1", record, nil)

	// Verify registered
	if rm.GetRegistration("entity-1") == nil {
		t.Fatal("expected registration to exist before deregister")
	}

	// Deregister
	err := rm.Deregister(ctx, "entity-1")
	if err != nil {
		t.Fatalf("Deregister() error = %v", err)
	}

	// Verify removed
	if rm.GetRegistration("entity-1") != nil {
		t.Error("expected registration to be removed after deregister")
	}

	// Deregistering non-existent should be OK
	err = rm.Deregister(ctx, "non-existent")
	if err != nil {
		t.Errorf("Deregister(non-existent) error = %v", err)
	}
}

func TestRegistrationManager_ListRegistrations(t *testing.T) {
	mock := NewMockDirectory()
	defer mock.Close()

	backend := NewHTTPBackend(mock.URL())
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	config := DefaultConfig()
	rm := NewRegistrationManager(backend, nil, config, logger)

	// Start empty
	if len(rm.ListRegistrations()) != 0 {
		t.Error("expected empty list initially")
	}

	ctx := context.Background()

	// Add some registrations
	for i := 0; i < 3; i++ {
		rm.RegisterAgent(ctx, "entity-"+string(rune('A'+i)), &oasfgenerator.OASFRecord{
			Name: "Agent" + string(rune('A'+i)),
		}, nil)
	}

	regs := rm.ListRegistrations()
	if len(regs) != 3 {
		t.Errorf("expected 3 registrations, got %d", len(regs))
	}
}

func TestRegistrationManager_StartStop(t *testing.T) {
	mock := NewMockDirectory()
	defer mock.Close()

	backend := NewHTTPBackend(mock.URL())
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	config := DefaultConfig()
	config.HeartbeatInterval = "50ms" // Short for testing
	rm := NewRegistrationManager(backend, nil, config, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start heartbeat loop
	err := rm.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Register an agent with short TTL
	rm.RegisterAgent(ctx, "entity-1", &oasfgenerator.OASFRecord{
		Name: "TestAgent",
	}, nil)

	// Manually set expiry to trigger heartbeat
	rm.mu.Lock()
	if reg, ok := rm.registrations["entity-1"]; ok {
		reg.ExpiresAt = time.Now().Add(50 * time.Millisecond)
	}
	rm.mu.Unlock()

	// Wait for potential heartbeat
	time.Sleep(150 * time.Millisecond)

	// Stop
	err = rm.Stop(ctx)
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	// Should have deregistered
	if mock.DeregisterCalls < 1 {
		t.Error("expected at least 1 deregister call on stop")
	}
}

func TestRegistrationError(t *testing.T) {
	err := &RegistrationError{
		EntityID: "entity-1",
		Message:  "something went wrong",
	}

	expected := "registration failed for entity-1: something went wrong"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}
