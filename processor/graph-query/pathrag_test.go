package graphquery

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/pkg/errs"
)

// TestVerifyEntityExists_ClassificationBranch pins gh#163: the 3-way
// branch on errs.IsInvalid / IsTransient / IsFatal inside
// verifyEntityExists so BFS callers can distinguish "entity genuinely
// absent" (Invalid) from "could not check due to server/transport
// failure" (Transient / Fatal).
//
// Without this fix both Invalid and Transient handler errors were
// returned verbatim as the same *errs.ClassifiedError, making the two
// cases indistinguishable to callers that branch on the class.
func TestVerifyEntityExists_ClassificationBranch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// setupMock configures the mock for this case.
		setupMock func(m *mockNATSClient)
		// wantErr indicates whether an error is expected.
		wantErr bool
		// wantInvalid asserts errs.IsInvalid(err) == true when set.
		wantInvalid bool
		// wantTransient asserts errs.IsTransient(err) == true when set.
		wantTransient bool
		// wantFatal asserts errs.IsFatal(err) == true when set.
		wantFatal bool
		// wantMsgPart is a substring the error message must contain.
		wantMsgPart string
	}{
		{
			name: "entity exists — success, nil error",
			setupMock: func(m *mockNATSClient) {
				m.requestFunc = func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
					if subject != "graph.ingest.query.entity" {
						t.Errorf("unexpected subject %q", subject)
					}
					return []byte(`{"id":"acme.test.foo.bar.baz.001","triples":[]}`), nil
				}
			},
		},
		{
			name: "entity not found — Invalid class, returned verbatim (definitive negative)",
			setupMock: func(m *mockNATSClient) {
				// Inject a classified not-found error directly via requestClassifiedFunc.
				// ADR-060 PR-D removed the legacy "error: " body-prefix fallback from
				// ClassifyReply; handler errors now arrive ONLY via X-Status header.
				// (nil, classifiedErr) from requestClassifiedFunc is the faithful
				// equivalent of the production wire after ClassifyReply reconstructs
				// from headers — errs.IsInvalid(err) true, message contains "not found".
				m.requestClassifiedFunc = func(_ context.Context, _ string, _ []byte, _ time.Duration) ([]byte, error) {
					return nil, errs.ClassifiedCode(errs.ErrorInvalid, "entity_not_found",
						errors.New("not found: acme.test.foo.bar.baz.001"))
				}
			},
			wantErr:     true,
			wantInvalid: true,
			wantMsgPart: "not found",
		},
		{
			name: "handler transient error — NOT Invalid; wrapped as server error",
			setupMock: func(m *mockNATSClient) {
				// Inject a Transient classified error directly, bypassing
				// ClassifyReply (the legacy body path only produces Invalid).
				// This simulates a graph-ingest handler responding with
				// X-Error-Class: transient (e.g. storage unavailable).
				m.requestClassifiedFunc = func(_ context.Context, _ string, _ []byte, _ time.Duration) ([]byte, error) {
					return nil, errs.Classified(errs.ErrorTransient, errors.New("storage unavailable"))
				}
			},
			wantErr:       true,
			wantTransient: true,
			wantMsgPart:   "server error",
		},
		{
			name: "handler fatal error — NOT Invalid; wrapped as server error",
			setupMock: func(m *mockNATSClient) {
				m.requestClassifiedFunc = func(_ context.Context, _ string, _ []byte, _ time.Duration) ([]byte, error) {
					return nil, errs.Classified(errs.ErrorFatal, errors.New("database corrupted"))
				}
			},
			wantErr:     true,
			wantFatal:   true,
			wantMsgPart: "server error",
		},
		{
			name: "raw transport failure — not a ClassifiedError; wrapped as server error",
			setupMock: func(m *mockNATSClient) {
				// Raw error from the transport layer (e.g. no NATS responders).
				// Does not implement *errs.ClassifiedError.
				m.requestFunc = func(_ context.Context, _ string, _ []byte, _ time.Duration) ([]byte, error) {
					return nil, errors.New("nats: timeout")
				}
			},
			wantErr:     true,
			wantMsgPart: "server error",
		},
	}

	for _, tt := range tests {
		tt := tt // capture range variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := newMockNATSClient()
			tt.setupMock(mock)

			p := NewPathSearcher(mock, 1*time.Second, 5, slog.Default())
			err := p.verifyEntityExists(context.Background(), "acme.test.foo.bar.baz.001")

			if !tt.wantErr {
				if err != nil {
					t.Fatalf("expected nil error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error, got nil")
			}

			// Assert error message contains expected substring.
			if tt.wantMsgPart != "" && !strings.Contains(err.Error(), tt.wantMsgPart) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantMsgPart)
			}

			// Assert error CLASS — this is the key distinction gh#163 fixes.
			if tt.wantInvalid && !errs.IsInvalid(err) {
				t.Errorf("expected errs.IsInvalid(err)=true, got false; err=%v", err)
			}
			if tt.wantTransient && !errs.IsTransient(err) {
				t.Errorf("expected errs.IsTransient(err)=true, got false; err=%v", err)
			}
			if tt.wantFatal && !errs.IsFatal(err) {
				t.Errorf("expected errs.IsFatal(err)=true, got false; err=%v", err)
			}

			// Mutual-exclusion check: Invalid errors must NOT look
			// like server failures to the BFS caller.
			if tt.wantInvalid && (errs.IsTransient(err) || errs.IsFatal(err)) {
				t.Errorf("Invalid error must not be classified Transient or Fatal; err=%v", err)
			}
			if (tt.wantTransient || tt.wantFatal) && errs.IsInvalid(err) {
				t.Errorf("server-error must not be classified Invalid; BFS would prune as absent; err=%v", err)
			}
		})
	}
}

func TestPathSearcher_RejectsStructurallyInvalidRelationshipEnvelopes(t *testing.T) {
	t.Parallel()
	for _, direction := range []string{DirectionOutgoing, DirectionIncoming} {
		direction := direction
		t.Run(direction, func(t *testing.T) {
			t.Parallel()
			for _, body := range []string{`{}`, `null`, `{"data":{}}`} {
				body := body
				t.Run(body, func(t *testing.T) {
					mock := newMockNATSClient()
					mock.requestFunc = func(_ context.Context, _ string, _ []byte, _ time.Duration) ([]byte, error) {
						return []byte(body), nil
					}
					p := NewPathSearcher(mock, time.Second, 5, slog.Default())
					_, err := p.getRelationships(context.Background(), "acme.ops.robotics.gcs.drone.001", direction, nil)
					if err == nil || !strings.Contains(err.Error(), "missing relationships") {
						t.Fatalf("expected structural response error for %s, got %v", body, err)
					}
				})
			}
		})
	}
}

func TestPathSearcher_BothDirectionPropagatesOneLegStructuralError(t *testing.T) {
	t.Parallel()
	mock := newMockNATSClient()
	mock.requestFunc = func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		switch subject {
		case "graph.index.query.outgoing":
			return []byte(`{"data":{"relationships":[]}}`), nil
		case "graph.index.query.incoming":
			return []byte(`{"data":{}}`), nil
		default:
			return nil, errors.New("unexpected subject")
		}
	}
	p := NewPathSearcher(mock, time.Second, 5, slog.Default())
	result, err := p.getRelationships(
		context.Background(),
		"acme.ops.robotics.gcs.drone.001",
		DirectionBoth,
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "incoming response invalid") {
		t.Fatalf("expected incoming-leg error, got result=%v err=%v", result, err)
	}
	if result != nil {
		t.Fatalf("both-direction failure returned partial relationships: %v", result)
	}
}

func TestPathSearcher_AcceptsExplicitEmptyRelationships(t *testing.T) {
	t.Parallel()
	mock := newMockNATSClient()
	mock.requestFunc = func(_ context.Context, _ string, _ []byte, _ time.Duration) ([]byte, error) {
		return []byte(`{"data":{"relationships":[]}}`), nil
	}
	p := NewPathSearcher(mock, time.Second, 5, slog.Default())
	result, err := p.getRelationships(
		context.Background(),
		"acme.ops.robotics.gcs.drone.001",
		DirectionOutgoing,
		nil,
	)
	if err != nil || len(result) != 0 || result == nil {
		t.Fatalf("explicit empty relationship list must be a valid empty success: result=%v err=%v", result, err)
	}
}
