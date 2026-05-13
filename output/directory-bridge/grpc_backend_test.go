package directorybridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	corev1 "github.com/agntcy/dir/api/core/v1"
	storev1 "github.com/agntcy/dir/api/store/v1"
	oasfgenerator "github.com/c360studio/semstreams/processor/oasf-generator"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
)

// fakeStoreServer is a deterministic in-memory StoreService used to test
// GRPCBackend's wire translation. CIDs are sha256-hex of the marshaled
// record bytes — close enough to a real content address that we can
// assert determinism without pulling in real multihash code in tests.
type fakeStoreServer struct {
	storev1.UnimplementedStoreServiceServer

	mu             sync.Mutex
	storedRecords  map[string]*corev1.Record // cid → record
	deletedCIDs    []string
	pushCalls      int
	deleteCalls    int
	pushFailNext   bool
	deleteFailNext bool
}

func newFakeStoreServer() *fakeStoreServer {
	return &fakeStoreServer{storedRecords: make(map[string]*corev1.Record)}
}

func (s *fakeStoreServer) Push(stream storev1.StoreService_PushServer) error {
	s.mu.Lock()
	s.pushCalls++
	if s.pushFailNext {
		s.pushFailNext = false
		s.mu.Unlock()
		return errors.New("simulated push failure")
	}
	s.mu.Unlock()

	for {
		rec, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		// Compute a deterministic CID from the serialized struct.
		raw, err := rec.GetData().MarshalJSON()
		if err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		cid := "test:" + hex.EncodeToString(sum[:])

		s.mu.Lock()
		s.storedRecords[cid] = rec
		s.mu.Unlock()

		if err := stream.Send(&corev1.RecordRef{Cid: cid}); err != nil {
			return err
		}
	}
}

func (s *fakeStoreServer) Delete(stream storev1.StoreService_DeleteServer) error {
	s.mu.Lock()
	s.deleteCalls++
	if s.deleteFailNext {
		s.deleteFailNext = false
		s.mu.Unlock()
		return errors.New("simulated delete failure")
	}
	s.mu.Unlock()

	for {
		ref, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&emptypb.Empty{})
		}
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.deletedCIDs = append(s.deletedCIDs, ref.GetCid())
		delete(s.storedRecords, ref.GetCid())
		s.mu.Unlock()
	}
}

// startTestServer wires a fakeStoreServer onto a bufconn listener and
// returns a connected GRPCBackend plus a teardown closure. The teardown
// closes both the client connection and the gRPC server.
func startTestServer(t *testing.T) (*fakeStoreServer, *GRPCBackend, func()) {
	t.Helper()

	listener := bufconn.Listen(1 << 20) // 1 MiB
	server := grpc.NewServer()
	fake := newFakeStoreServer()
	storev1.RegisterStoreServiceServer(server, fake)

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(listener)
	}()

	dialer := func(_ context.Context, _ string) (net.Conn, error) {
		return listener.DialContext(context.Background())
	}
	conn, err := grpc.NewClient(
		"passthrough://bufconn",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}

	backend := NewGRPCBackendFromClient(conn)

	teardown := func() {
		_ = backend.Close()
		server.GracefulStop()
		_ = listener.Close()
		// Drain server goroutine so it does not outlive the test.
		select {
		case <-serverErr:
		case <-time.After(2 * time.Second):
			t.Logf("warning: grpc server did not stop within 2s")
		}
	}
	return fake, backend, teardown
}

// TestGRPCBackend_PublishWithdrawRoundTrip exercises the streaming
// translation end-to-end against an in-memory StoreServiceServer.
// Validates: Push receives a structpb-marshalled record, returns a CID;
// GRPCBackend echoes the CID as PublishResult.RecordID with ExpiresAt
// zero (CID-anchored, no expiry); Withdraw drives Delete and removes
// the record from the fake's store.
func TestGRPCBackend_PublishWithdrawRoundTrip(t *testing.T) {
	fake, backend, teardown := startTestServer(t)
	defer teardown()

	ctx := context.Background()
	record := &oasfgenerator.OASFRecord{
		Name:          "test-agent",
		Version:       "1.0.0",
		SchemaVersion: "1.0.0",
		Description:   "grpc backend round-trip",
		Skills:        []oasfgenerator.OASFSkill{{ID: "test", Name: "Test"}},
	}

	// Publish
	res, err := backend.Publish(ctx, &PublishRequest{
		EntityID: "acme.ops.agentic.system.agent.test",
		AgentDID: "did:semstreams:test",
		Record:   record,
		TTL:      5 * time.Minute, // honored by the contract but ignored by gRPC backend
		Metadata: map[string]any{"semstreams_entity_id": "acme.ops.agentic.system.agent.test"},
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if res.RecordID == "" {
		t.Fatal("expected non-empty RecordID (CID)")
	}
	if !res.ExpiresAt.IsZero() {
		t.Errorf("ExpiresAt = %v, want zero (CID-anchored backends never expire)", res.ExpiresAt)
	}
	if fake.pushCalls != 1 {
		t.Errorf("pushCalls = %d, want 1", fake.pushCalls)
	}
	fake.mu.Lock()
	stored, ok := fake.storedRecords[res.RecordID]
	fake.mu.Unlock()
	if !ok {
		t.Fatalf("server did not store record under CID %q", res.RecordID)
	}
	// Verify the OASF name round-tripped through the structpb.
	if stored.GetData().GetFields()["name"].GetStringValue() != "test-agent" {
		t.Errorf("server received unexpected record payload: %v", stored.GetData())
	}

	// Withdraw
	if err := backend.Withdraw(ctx, &WithdrawRequest{
		RecordID: res.RecordID,
		AgentDID: "did:semstreams:test",
	}); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if fake.deleteCalls != 1 {
		t.Errorf("deleteCalls = %d, want 1", fake.deleteCalls)
	}
	fake.mu.Lock()
	_, stillStored := fake.storedRecords[res.RecordID]
	fake.mu.Unlock()
	if stillStored {
		t.Error("server still has the record after Withdraw")
	}
}

// TestGRPCBackend_RefreshIsNoop documents the CID-anchored contract:
// Refresh never hits the wire (fake.pushCalls stays at 0) and returns
// the same RecordID with ExpiresAt zero so the bridge keeps skipping
// heartbeats for this registration.
func TestGRPCBackend_RefreshIsNoop(t *testing.T) {
	fake, backend, teardown := startTestServer(t)
	defer teardown()

	res, err := backend.Refresh(context.Background(), &RefreshRequest{
		RecordID: "test:abcdef",
		AgentDID: "did:test",
	})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if res.RecordID != "test:abcdef" {
		t.Errorf("RecordID = %q, want echo of input", res.RecordID)
	}
	if !res.ExpiresAt.IsZero() {
		t.Errorf("ExpiresAt = %v, want zero", res.ExpiresAt)
	}
	if fake.pushCalls != 0 || fake.deleteCalls != 0 {
		t.Errorf("Refresh hit the wire: push=%d delete=%d, want 0/0", fake.pushCalls, fake.deleteCalls)
	}
}

// TestGRPCBackend_PublishPropagatesServerError ensures that a streaming
// error from the StoreServiceServer surfaces as a publish error so the
// bridge retries instead of recording a phantom registration.
func TestGRPCBackend_PublishPropagatesServerError(t *testing.T) {
	fake, backend, teardown := startTestServer(t)
	defer teardown()
	fake.pushFailNext = true

	_, err := backend.Publish(context.Background(), &PublishRequest{
		EntityID: "x",
		AgentDID: "did:test",
		Record:   &oasfgenerator.OASFRecord{Name: "x", Version: "1.0.0", SchemaVersion: "1.0.0"},
		TTL:      time.Minute,
	})
	if err == nil {
		t.Fatal("expected error when server fails Push")
	}
}

// TestGRPCBackend_NilRequests catches programmer error early — the bridge
// never produces a nil request, but a typo upstream should not panic
// inside the stream setup.
func TestGRPCBackend_NilRequests(t *testing.T) {
	_, backend, teardown := startTestServer(t)
	defer teardown()
	ctx := context.Background()

	cases := []struct {
		name string
		fn   func() error
	}{
		{"publish-nil", func() error { _, err := backend.Publish(ctx, nil); return err }},
		{"publish-nil-record", func() error {
			_, err := backend.Publish(ctx, &PublishRequest{EntityID: "x", AgentDID: "did:test"})
			return err
		}},
		{"refresh-nil", func() error { _, err := backend.Refresh(ctx, nil); return err }},
		{"withdraw-nil", func() error { return backend.Withdraw(ctx, nil) }},
		{"withdraw-empty-cid", func() error { return backend.Withdraw(ctx, &WithdrawRequest{}) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.fn(); err == nil {
				t.Error("expected error")
			}
		})
	}
}

// Compile-time assertion that GRPCBackend implements Backend.
var _ Backend = (*GRPCBackend)(nil)
