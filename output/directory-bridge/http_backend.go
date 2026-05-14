package directorybridge

import (
	"context"
	"fmt"
)

// HTTPBackend implements Backend by delegating to the in-package
// DirectoryClient, which speaks the SemStreams-defined HTTP/JSON
// directory protocol (POST /v1/agents, POST /v1/agents/{id}/heartbeat,
// DELETE /v1/agents/{id}).
//
// This is the only backend wired today; the test mock and any privately
// hosted directory server speak this protocol. A second backend
// implementing the AGNTCY agntcy/dir gRPC service is planned but not yet
// in tree.
type HTTPBackend struct {
	client *DirectoryClient
}

// NewHTTPBackend constructs a backend pointing at baseURL. baseURL must be
// non-empty; the underlying client returns errors at call time if it is,
// preserving the original behavior.
func NewHTTPBackend(baseURL string) *HTTPBackend {
	return &HTTPBackend{client: NewDirectoryClient(baseURL)}
}

// NewHTTPBackendFromClient lets callers (notably tests) inject a
// pre-configured *DirectoryClient. Keeps the existing DirectoryClient
// unit tests usable as-is.
func NewHTTPBackendFromClient(client *DirectoryClient) *HTTPBackend {
	return &HTTPBackend{client: client}
}

// Publish translates the domain request into the HTTP RegistrationRequest
// and adapts the response back. A non-success response from a 2xx status
// is treated as an error so the bridge can retry the same way it did
// before the refactor.
func (b *HTTPBackend) Publish(ctx context.Context, req *PublishRequest) (*PublishResult, error) {
	if req == nil {
		return nil, fmt.Errorf("publish request is nil")
	}

	httpReq := &RegistrationRequest{
		AgentDID:   req.AgentDID,
		OASFRecord: req.Record,
		TTL:        int(req.TTL.Seconds()),
		Metadata:   req.Metadata,
	}

	resp, err := b.client.Register(ctx, httpReq)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, &RegistrationError{EntityID: req.EntityID, Message: resp.Error}
	}

	return &PublishResult{
		RecordID:  resp.RegistrationID,
		ExpiresAt: resp.ExpiresAt,
	}, nil
}

// Refresh sends a heartbeat to extend the registration's lifetime.
func (b *HTTPBackend) Refresh(ctx context.Context, req *RefreshRequest) (*PublishResult, error) {
	if req == nil {
		return nil, fmt.Errorf("refresh request is nil")
	}

	resp, err := b.client.Heartbeat(ctx, &HeartbeatRequest{
		RegistrationID: req.RecordID,
		AgentDID:       req.AgentDID,
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("heartbeat failed: %s", resp.Error)
	}

	return &PublishResult{
		RecordID:  req.RecordID,
		ExpiresAt: resp.ExpiresAt,
	}, nil
}

// Withdraw deregisters a record by ID.
func (b *HTTPBackend) Withdraw(ctx context.Context, req *WithdrawRequest) error {
	if req == nil {
		return fmt.Errorf("withdraw request is nil")
	}
	return b.client.Deregister(ctx, &DeregistrationRequest{
		RegistrationID: req.RecordID,
		AgentDID:       req.AgentDID,
	})
}

// Close is a no-op for the HTTP backend — net/http's Transport pools idle
// connections internally and reclaims them on GC. No backend-owned resources
// to release.
func (b *HTTPBackend) Close() error {
	return nil
}
