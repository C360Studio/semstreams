package directorybridge

import (
	"context"
	"testing"
	"time"

	oasfgenerator "github.com/c360studio/semstreams/processor/oasf-generator"
)

func TestDirectoryClient_Register(t *testing.T) {
	mock := NewMockDirectory()
	defer mock.Close()

	client := NewDirectoryClient(mock.URL())

	record := &oasfgenerator.OASFRecord{
		Name:          "TestAgent",
		Version:       "1.0.0",
		SchemaVersion: "1.0.0",
		Description:   "A test agent",
		Skills: []oasfgenerator.OASFSkill{
			{
				ID:   9_100_001,
				Name: "test-skill",
			},
		},
	}

	req := &RegistrationRequest{
		AgentDID:   "did:key:z6MkTest123",
		OASFRecord: record,
		TTL:        300,
		Metadata: map[string]any{
			"source": "test",
		},
	}

	ctx := context.Background()
	resp, err := client.Register(ctx, req)

	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success=true, got false: %s", resp.Error)
	}

	if resp.RegistrationID == "" {
		t.Error("expected non-empty registration ID")
	}

	if resp.ExpiresAt.IsZero() {
		t.Error("expected non-zero ExpiresAt")
	}

	if mock.RegisterCalls != 1 {
		t.Errorf("expected 1 register call, got %d", mock.RegisterCalls)
	}

	// Verify registration stored
	stored := mock.GetRegistration(resp.RegistrationID)
	if stored == nil {
		t.Error("expected registration to be stored")
	}
	if stored.AgentDID != req.AgentDID {
		t.Errorf("stored AgentDID = %s, want %s", stored.AgentDID, req.AgentDID)
	}
}

func TestDirectoryClient_Register_Failure(t *testing.T) {
	mock := NewMockDirectory()
	defer mock.Close()
	mock.SetFailNextRegister(true)

	client := NewDirectoryClient(mock.URL())

	req := &RegistrationRequest{
		AgentDID: "did:key:z6MkTest123",
		OASFRecord: &oasfgenerator.OASFRecord{
			Name: "TestAgent",
		},
	}

	ctx := context.Background()
	_, err := client.Register(ctx, req)

	if err == nil {
		t.Error("expected error on failed registration")
	}
}

func TestDirectoryClient_Register_EmptyURL(t *testing.T) {
	client := NewDirectoryClient("")

	req := &RegistrationRequest{
		AgentDID: "did:key:z6MkTest123",
	}

	ctx := context.Background()
	_, err := client.Register(ctx, req)

	if err == nil {
		t.Error("expected error with empty URL")
	}
}

func TestDirectoryClient_Heartbeat(t *testing.T) {
	mock := NewMockDirectory()
	defer mock.Close()

	client := NewDirectoryClient(mock.URL())

	// First register an agent
	regResp, _ := client.Register(context.Background(), &RegistrationRequest{
		AgentDID: "did:key:z6MkTest123",
		OASFRecord: &oasfgenerator.OASFRecord{
			Name: "TestAgent",
		},
		TTL: 300,
	})

	// Send heartbeat
	req := &HeartbeatRequest{
		RegistrationID: regResp.RegistrationID,
		AgentDID:       "did:key:z6MkTest123",
	}

	ctx := context.Background()
	resp, err := client.Heartbeat(ctx, req)

	if err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success=true, got false: %s", resp.Error)
	}

	if resp.ExpiresAt.IsZero() {
		t.Error("expected non-zero ExpiresAt")
	}

	if mock.HeartbeatCalls != 1 {
		t.Errorf("expected 1 heartbeat call, got %d", mock.HeartbeatCalls)
	}
}

func TestDirectoryClient_Heartbeat_NotFound(t *testing.T) {
	mock := NewMockDirectory()
	defer mock.Close()

	client := NewDirectoryClient(mock.URL())

	req := &HeartbeatRequest{
		RegistrationID: "non-existent",
		AgentDID:       "did:key:z6MkTest123",
	}

	ctx := context.Background()
	_, err := client.Heartbeat(ctx, req)

	if err == nil {
		t.Error("expected error for non-existent registration")
	}
}

func TestDirectoryClient_Deregister(t *testing.T) {
	mock := NewMockDirectory()
	defer mock.Close()

	client := NewDirectoryClient(mock.URL())

	// First register an agent
	regResp, _ := client.Register(context.Background(), &RegistrationRequest{
		AgentDID: "did:key:z6MkTest123",
		OASFRecord: &oasfgenerator.OASFRecord{
			Name: "TestAgent",
		},
	})

	// Verify it's stored
	if mock.RegistrationCount() != 1 {
		t.Fatalf("expected 1 registration, got %d", mock.RegistrationCount())
	}

	// Deregister
	req := &DeregistrationRequest{
		RegistrationID: regResp.RegistrationID,
		AgentDID:       "did:key:z6MkTest123",
	}

	ctx := context.Background()
	err := client.Deregister(ctx, req)

	if err != nil {
		t.Fatalf("Deregister() error = %v", err)
	}

	if mock.DeregisterCalls != 1 {
		t.Errorf("expected 1 deregister call, got %d", mock.DeregisterCalls)
	}

	// Verify it's removed
	if mock.RegistrationCount() != 0 {
		t.Errorf("expected 0 registrations after deregister, got %d", mock.RegistrationCount())
	}
}

func TestDirectoryClient_Discover(t *testing.T) {
	mock := NewMockDirectory()
	defer mock.Close()

	client := NewDirectoryClient(mock.URL())

	// Register a couple agents
	for i := 0; i < 3; i++ {
		client.Register(context.Background(), &RegistrationRequest{
			AgentDID: "did:key:z6MkTest" + string(rune('A'+i)),
			OASFRecord: &oasfgenerator.OASFRecord{
				Name: "Agent" + string(rune('A'+i)),
			},
		})
	}

	// Discover with limit
	query := &DiscoveryQuery{
		Limit: 2,
	}

	ctx := context.Background()
	resp, err := client.Discover(ctx, query)

	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if resp.Total > 2 {
		t.Errorf("expected at most 2 agents, got %d", resp.Total)
	}

	if mock.DiscoverCalls != 1 {
		t.Errorf("expected 1 discover call, got %d", mock.DiscoverCalls)
	}
}

func TestDirectoryClient_ContextCancellation(t *testing.T) {
	mock := NewMockDirectory()
	defer mock.Close()
	mock.SetRegisterDelay(100 * time.Millisecond)

	client := NewDirectoryClient(mock.URL())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req := &RegistrationRequest{
		AgentDID: "did:key:z6MkTest123",
		OASFRecord: &oasfgenerator.OASFRecord{
			Name: "TestAgent",
		},
	}

	_, err := client.Register(ctx, req)

	if err == nil {
		t.Error("expected error with cancelled context")
	}
}
