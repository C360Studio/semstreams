package graphquery

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// TestVerifyEntityExists_NotFoundProtocol pins ADR-036 Stage 4.1's
// fix: graph-ingest's handleQueryEntityNATS returns a Go error on
// missing-entity, but natsclient.SubscribeForRequests wraps that
// into the response payload as "error: not found: <id>" with err=nil.
// The previous implementation only branched on err and silently
// passed verification when an entity was missing. Sister-bug to the
// fix in processor/agentic-loop/todos.go.
func TestVerifyEntityExists_NotFoundProtocol(t *testing.T) {
	tests := []struct {
		name        string
		respData    []byte
		respErr     error
		wantErr     bool
		wantMsgPart string
	}{
		{
			name:     "entity exists — empty err, body is entity JSON",
			respData: []byte(`{"id":"acme.test.foo.bar.baz.001","triples":[]}`),
		},
		{
			name:        "missing entity — handler error wrapped as response payload",
			respData:    []byte("error: not found: acme.test.foo.bar.baz.001"),
			wantErr:     true,
			wantMsgPart: "not found",
		},
		{
			name:        "other handler error (e.g. internal) — surfaced too",
			respData:    []byte("error: internal error: db down"),
			wantErr:     true,
			wantMsgPart: "internal error",
		},
		{
			name:        "transport failure — propagated via Go err",
			respErr:     errors.New("nats: timeout"),
			wantErr:     true,
			wantMsgPart: "query entity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockNATSClient()
			mock.requestFunc = func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
				if subject != "graph.ingest.query.entity" {
					t.Errorf("unexpected subject %q", subject)
				}
				return tt.respData, tt.respErr
			}

			p := NewPathSearcher(mock, 1*time.Second, 5, slog.Default())
			err := p.verifyEntityExists(context.Background(), "acme.test.foo.bar.baz.001")

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.wantMsgPart != "" && !strings.Contains(err.Error(), tt.wantMsgPart) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantMsgPart)
				}
				return
			}
			if err != nil {
				t.Errorf("expected nil error, got %v", err)
			}
		})
	}
}
