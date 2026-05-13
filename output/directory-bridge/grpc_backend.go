package directorybridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	corev1 "github.com/agntcy/dir/api/core/v1"
	storev1 "github.com/agntcy/dir/api/store/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"
)

// GRPCBackend implements Backend by talking to an AGNTCY-compatible
// directory over the agntcy/dir gRPC StoreService.
//
// The wire model differs from the HTTP backend in a few ways the rest of
// the bridge needs to understand:
//
//   - Records are content-addressed. PublishResult.RecordID is the CID the
//     server returns from Push; it never changes for an unchanged record.
//   - There is no expiry. PublishResult.ExpiresAt is always zero. The
//     bridge's heartbeat scheduler is documented to skip refresh when
//     ExpiresAt.IsZero() (see backend.go contract), so Refresh here is a
//     no-op that simply echoes the RecordID.
//   - Push and Delete are streaming RPCs. We send exactly one record/ref
//     per call because the bridge invokes us one entity at a time. The
//     ceremony around streams is local to this file.
//
// Authentication is not handled here. Operators pass grpc.DialOption values
// (TLS credentials, per-RPC OIDC tokens) at construction; this keeps
// auth strategies pluggable without growing the backend's surface.
type GRPCBackend struct {
	conn   *grpc.ClientConn
	client storev1.StoreServiceClient
}

// NewGRPCBackend dials the directory at target and returns a backend ready
// to Publish/Withdraw. Caller is responsible for closing it via Close().
//
// Pass grpc.WithTransportCredentials(...) for TLS; for the hosted hub at
// prod.api.ads.outshift.io that means real cert validation. For local
// dev / bufconn tests, pass grpc.WithTransportCredentials(insecure.NewCredentials()).
func NewGRPCBackend(target string, opts ...grpc.DialOption) (*GRPCBackend, error) {
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, fmt.Errorf("dial directory %q: %w", target, err)
	}
	return &GRPCBackend{
		conn:   conn,
		client: storev1.NewStoreServiceClient(conn),
	}, nil
}

// NewGRPCBackendFromClient is the test-side constructor: callers (notably
// the bufconn tests) build their own *grpc.ClientConn and inject it.
// Close() will still tear down the connection — tests that want to share
// a conn across backends should build one backend or call Close themselves.
func NewGRPCBackendFromClient(conn *grpc.ClientConn) *GRPCBackend {
	return &GRPCBackend{
		conn:   conn,
		client: storev1.NewStoreServiceClient(conn),
	}
}

// Publish marshals the OASF record into a structpb.Struct, drives one
// round of the StoreService.Push bidi stream, and returns the
// server-assigned CID. ExpiresAt is intentionally zero — see type comment.
func (b *GRPCBackend) Publish(ctx context.Context, req *PublishRequest) (*PublishResult, error) {
	if req == nil {
		return nil, fmt.Errorf("publish request is nil")
	}
	if req.Record == nil {
		return nil, fmt.Errorf("publish request record is nil")
	}

	record, err := oasfToProtoRecord(req.Record)
	if err != nil {
		return nil, fmt.Errorf("marshal OASF record: %w", err)
	}

	stream, err := b.client.Push(ctx)
	if err != nil {
		return nil, fmt.Errorf("open push stream: %w", err)
	}

	if err := stream.Send(record); err != nil {
		return nil, fmt.Errorf("send record: %w", err)
	}
	if err := stream.CloseSend(); err != nil {
		return nil, fmt.Errorf("close push send: %w", err)
	}

	// Server sends one RecordRef per pushed record. We sent exactly one,
	// so we expect exactly one ref followed by io.EOF.
	ref, err := stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("recv record ref: %w", err)
	}
	if ref.GetCid() == "" {
		return nil, fmt.Errorf("server returned empty CID")
	}

	// Drain to EOF so the underlying stream is cleanly closed; a second
	// non-EOF response from the server would indicate protocol drift.
	if extra, err := stream.Recv(); err != io.EOF {
		return nil, fmt.Errorf("unexpected extra ref %q (err=%v)", extra.GetCid(), err)
	}

	return &PublishResult{
		RecordID:  ref.GetCid(),
		ExpiresAt: time.Time{}, // CID-anchored: never expires on the publisher side.
	}, nil
}

// Refresh is a no-op for CID-anchored records. The bridge skips heartbeats
// when ExpiresAt.IsZero(), so this should not be called in practice; if it
// is (e.g. operator manually triggers), echo the RecordID back.
func (b *GRPCBackend) Refresh(_ context.Context, req *RefreshRequest) (*PublishResult, error) {
	if req == nil {
		return nil, fmt.Errorf("refresh request is nil")
	}
	return &PublishResult{
		RecordID:  req.RecordID,
		ExpiresAt: time.Time{},
	}, nil
}

// Withdraw drives the StoreService.Delete client-streaming RPC for one
// RecordRef and waits for the Empty response.
func (b *GRPCBackend) Withdraw(ctx context.Context, req *WithdrawRequest) error {
	if req == nil {
		return fmt.Errorf("withdraw request is nil")
	}
	if req.RecordID == "" {
		return fmt.Errorf("withdraw request RecordID is empty")
	}

	stream, err := b.client.Delete(ctx)
	if err != nil {
		return fmt.Errorf("open delete stream: %w", err)
	}
	if err := stream.Send(&corev1.RecordRef{Cid: req.RecordID}); err != nil {
		return fmt.Errorf("send record ref: %w", err)
	}
	if _, err := stream.CloseAndRecv(); err != nil {
		return fmt.Errorf("close delete stream: %w", err)
	}
	return nil
}

// Close tears down the underlying gRPC client connection. Safe to call
// once; subsequent calls return whatever grpc.ClientConn.Close returns
// (typically nil even when already closed).
func (b *GRPCBackend) Close() error {
	if b.conn == nil {
		return nil
	}
	return b.conn.Close()
}

// oasfToProtoRecord converts our domain OASFRecord into the wire-level
// core.v1.Record. The proto carries the record as an opaque
// google.protobuf.Struct (allowing OASF schema evolution without proto
// regeneration), so the safest bridge is JSON → structpb.
func oasfToProtoRecord(rec any) (*corev1.Record, error) {
	jsonBytes, err := json.Marshal(rec)
	if err != nil {
		return nil, fmt.Errorf("marshal record to JSON: %w", err)
	}
	data := &structpb.Struct{}
	if err := data.UnmarshalJSON(jsonBytes); err != nil {
		return nil, fmt.Errorf("unmarshal record to structpb: %w", err)
	}
	return &corev1.Record{Data: data}, nil
}
