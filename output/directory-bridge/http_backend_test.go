package directorybridge

import (
	"context"
	"testing"
	"time"

	oasfgenerator "github.com/c360studio/semstreams/processor/oasf-generator"
)

// TestHTTPBackend_PublishRefreshWithdraw exercises the full Backend lifecycle
// against the in-package MockDirectory. The HTTP wire client itself is
// covered by TestDirectoryClient_*; this test verifies the domain↔wire
// translation in http_backend.go.
func TestHTTPBackend_PublishRefreshWithdraw(t *testing.T) {
	mock := NewMockDirectory()
	defer mock.Close()

	backend := NewHTTPBackend(mock.URL())
	ctx := context.Background()

	record := &oasfgenerator.OASFRecord{
		Name:          "test-agent",
		Version:       "1.0.0",
		SchemaVersion: "1.0.0",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		Skills:        []oasfgenerator.OASFSkill{{ID: 9_100_001, Name: "semstreams/test"}},
	}

	// --- Publish ---
	pub, err := backend.Publish(ctx, &PublishRequest{
		EntityID: "acme.ops.agentic.system.agent.test",
		AgentDID: "did:semstreams:test",
		Record:   record,
		TTL:      5 * time.Minute,
		Metadata: map[string]any{"semstreams_entity_id": "acme.ops.agentic.system.agent.test"},
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if pub.RecordID == "" {
		t.Error("RecordID empty after publish")
	}
	if pub.ExpiresAt.IsZero() {
		t.Error("ExpiresAt zero after publish — HTTP backend should populate it from the response")
	}
	if mock.RegisterCalls != 1 {
		t.Errorf("RegisterCalls = %d, want 1", mock.RegisterCalls)
	}

	// --- Refresh ---
	refreshed, err := backend.Refresh(ctx, &RefreshRequest{
		RecordID: pub.RecordID,
		AgentDID: "did:semstreams:test",
	})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if refreshed.RecordID != pub.RecordID {
		t.Errorf("RecordID after refresh = %q, want %q", refreshed.RecordID, pub.RecordID)
	}
	if mock.HeartbeatCalls != 1 {
		t.Errorf("HeartbeatCalls = %d, want 1", mock.HeartbeatCalls)
	}

	// --- Withdraw ---
	if err := backend.Withdraw(ctx, &WithdrawRequest{
		RecordID: pub.RecordID,
		AgentDID: "did:semstreams:test",
	}); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if mock.DeregisterCalls != 1 {
		t.Errorf("DeregisterCalls = %d, want 1", mock.DeregisterCalls)
	}
}

// TestHTTPBackend_PublishNilRequest documents that nil requests fail loudly
// rather than panicking on field access — the bridge should never produce
// a nil PublishRequest, but mistakes are cheaper to catch here.
func TestHTTPBackend_PublishNilRequest(t *testing.T) {
	backend := NewHTTPBackend("http://example.invalid")
	cases := []struct {
		name string
		fn   func() error
	}{
		{"publish", func() error { _, err := backend.Publish(context.Background(), nil); return err }},
		{"refresh", func() error { _, err := backend.Refresh(context.Background(), nil); return err }},
		{"withdraw", func() error { return backend.Withdraw(context.Background(), nil) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.fn(); err == nil {
				t.Error("expected error for nil request")
			}
		})
	}
}

// TestHTTPBackend_PublishPropagatesFailure ensures that a directory
// returning Success=false is treated as an error so the bridge retries
// instead of recording a fake registration.
func TestHTTPBackend_PublishPropagatesFailure(t *testing.T) {
	mock := NewMockDirectory()
	defer mock.Close()
	mock.SetFailNextRegister(true)

	backend := NewHTTPBackend(mock.URL())
	_, err := backend.Publish(context.Background(), &PublishRequest{
		EntityID: "x",
		AgentDID: "did:test",
		Record:   &oasfgenerator.OASFRecord{Name: "x", Version: "1.0.0", SchemaVersion: "1.0.0"},
		TTL:      time.Minute,
	})
	if err == nil {
		t.Fatal("expected error when mock returns Success=false")
	}
}

// Compile-time assertion that HTTPBackend implements Backend.
var _ Backend = (*HTTPBackend)(nil)
