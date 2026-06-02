//go:build integration

package natsclient

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/pkg/errs"
)

// TestIntegration_RequestClassified_RoundTripPreservesClass is the
// load-bearing gh#93 contract: a handler that returns
// errs.WrapInvalid(...) MUST surface to the caller as a classified
// error where errs.IsInvalid(err) == true. This is the regression
// net that catches Phase 1 wire-format gaps.
func TestIntegration_RequestClassified_RoundTripPreservesClass(t *testing.T) {
	ctx := context.Background()

	natsContainer, natsURL := startNATSContainer(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	defer client.Close(ctx)

	cases := []struct {
		name        string
		handlerErr  error
		isInvalid   bool
		isTransient bool
		isFatal     bool
		wantInMsg   string
	}{
		{
			name:       "invalid_class_round_trips",
			handlerErr: errs.WrapInvalid(errors.New("bad request shape"), "Test", "Handle", "validate"),
			isInvalid:  true,
			wantInMsg:  "bad request shape",
		},
		{
			name:        "transient_class_round_trips",
			handlerErr:  errs.WrapTransient(errors.New("kv temporarily unavailable"), "Test", "Handle", "fetch"),
			isTransient: true,
			wantInMsg:   "kv temporarily unavailable",
		},
		{
			name:       "fatal_class_round_trips",
			handlerErr: errs.WrapFatal(errors.New("kv permanently unreachable"), "Test", "Handle", "fetch"),
			isFatal:    true,
			wantInMsg:  "kv permanently unreachable",
		},
		{
			name:        "unclassified_plain_error_defaults_transient",
			handlerErr:  errors.New("plain — pkg/errs Classify defaults to transient"),
			isTransient: true,
			wantInMsg:   "plain",
		},
	}

	for i, tc := range cases {
		tc := tc
		i := i
		t.Run(tc.name, func(t *testing.T) {
			subject := "test.classified." + tc.name
			_, err = client.SubscribeForRequests(ctx, subject, func(_ context.Context, _ []byte) ([]byte, error) {
				return nil, tc.handlerErr
			})
			require.NoError(t, err)

			// Give subscription time to be established. Pattern
			// copied from sibling integration tests above; not a
			// race — NATS interest propagation is fast but not
			// instant.
			time.Sleep(50 * time.Millisecond)

			data, err := client.RequestClassified(ctx, subject, []byte("ping"), 2*time.Second)
			if data != nil {
				t.Errorf("data should be nil on classified error; got %q", data)
			}
			if err == nil {
				t.Fatal("expected classified error")
			}
			if errs.IsInvalid(err) != tc.isInvalid {
				t.Errorf("IsInvalid=%v, want %v (err=%v)", errs.IsInvalid(err), tc.isInvalid, err)
			}
			if errs.IsTransient(err) != tc.isTransient {
				t.Errorf("IsTransient=%v, want %v (err=%v)", errs.IsTransient(err), tc.isTransient, err)
			}
			if errs.IsFatal(err) != tc.isFatal {
				t.Errorf("IsFatal=%v, want %v (err=%v)", errs.IsFatal(err), tc.isFatal, err)
			}
			if !strings.Contains(err.Error(), tc.wantInMsg) {
				t.Errorf("case %d err=%q, want substring %q", i, err.Error(), tc.wantInMsg)
			}
		})
	}
}

// TestIntegration_LegacyBodyPrefix_StillReadable confirms the
// backward-compat contract: a pre-#93 caller using plain Request +
// bytes.HasPrefix("error: ") continues to see the legacy body shape
// unchanged. This is the wire-compat lock on Phase 1.
func TestIntegration_LegacyBodyPrefix_StillReadable(t *testing.T) {
	ctx := context.Background()

	natsContainer, natsURL := startNATSContainer(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	defer client.Close(ctx)

	subject := "test.legacy.body.prefix"
	handlerErr := errs.WrapInvalid(errors.New("legacy compat case"), "Test", "Handle", "validate")
	_, err = client.SubscribeForRequests(ctx, subject, func(_ context.Context, _ []byte) ([]byte, error) {
		return nil, handlerErr
	})
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)

	// Plain Request — pre-#93 path. Should see "error: " body
	// prefix, unchanged from before.
	data, err := client.Request(ctx, subject, []byte("ping"), 2*time.Second)
	require.NoError(t, err) // Handler errors don't surface in err return per legacy contract.
	if !strings.HasPrefix(string(data), "error: ") {
		t.Fatalf("legacy body prefix missing; got %q", data)
	}
	if !strings.Contains(string(data), "legacy compat case") {
		t.Errorf("body missing original message; got %q", data)
	}
}

// TestIntegration_LegacyRequest_SuccessBodyUnchanged is the
// success-path counterpart to TestIntegration_LegacyBodyPrefix_StillReadable.
// Pre-#93 callers using plain Request() against a Phase-1 handler
// must see exactly the bytes the handler returned — Phase 1's
// SubscribeForRequests change must NOT introduce headers or trace
// the byte payload on the success path. Locks the byte invariant
// that Phase 4 (legacy body drop) would otherwise break first here.
func TestIntegration_LegacyRequest_SuccessBodyUnchanged(t *testing.T) {
	ctx := context.Background()

	natsContainer, natsURL := startNATSContainer(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	defer client.Close(ctx)

	subject := "test.legacy.success.body"
	want := []byte(`{"hello":"world","number":42}`)
	_, err = client.SubscribeForRequests(ctx, subject, func(_ context.Context, _ []byte) ([]byte, error) {
		return want, nil
	})
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)

	data, err := client.Request(ctx, subject, []byte("ping"), 2*time.Second)
	require.NoError(t, err)
	if !bytes.Equal(data, want) {
		t.Fatalf("Request success body diverged from handler bytes:\n  got  %q\n  want %q", data, want)
	}
}

// TestIntegration_RequestClassified_SuccessPath confirms the
// non-error path: handler returns (data, nil), caller gets the same
// data, nil error.
func TestIntegration_RequestClassified_SuccessPath(t *testing.T) {
	ctx := context.Background()

	natsContainer, natsURL := startNATSContainer(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	defer client.Close(ctx)

	subject := "test.classified.success"
	want := []byte(`{"ok":true}`)
	_, err = client.SubscribeForRequests(ctx, subject, func(_ context.Context, _ []byte) ([]byte, error) {
		return want, nil
	})
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)

	data, err := client.RequestClassified(ctx, subject, []byte("ping"), 2*time.Second)
	require.NoError(t, err)
	if string(data) != string(want) {
		t.Fatalf("data = %q, want %q", data, want)
	}
}

// TestIntegration_RequestWithRetryClassified_RoundTripPreservesClass
// is the gh#192 matrix-gap-closure regression net. Mirrors the
// RequestClassified case table but drives through the retry-aware
// entry point — proves the retry loop preserves the classified
// contract instead of returning the legacy text-body shape (which
// is the Footgun semteams shipped a `json.Valid` workaround for in
// cmd/semteams/tools/addsource/executor.go).
func TestIntegration_RequestWithRetryClassified_RoundTripPreservesClass(t *testing.T) {
	ctx := context.Background()

	natsContainer, natsURL := startNATSContainer(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	defer client.Close(ctx)

	cases := []struct {
		name        string
		handlerErr  error
		isInvalid   bool
		isTransient bool
		isFatal     bool
		wantInMsg   string
	}{
		{
			name:       "invalid_class_round_trips",
			handlerErr: errs.WrapInvalid(errors.New("bad mutation shape"), "Test", "Handle", "validate"),
			isInvalid:  true,
			wantInMsg:  "bad mutation shape",
		},
		{
			name:        "transient_class_round_trips",
			handlerErr:  errs.WrapTransient(errors.New("kv temporarily unavailable"), "Test", "Handle", "write"),
			isTransient: true,
			wantInMsg:   "kv temporarily unavailable",
		},
		{
			name:       "fatal_class_round_trips",
			handlerErr: errs.WrapFatal(errors.New("kv permanently unreachable"), "Test", "Handle", "write"),
			isFatal:    true,
			wantInMsg:  "kv permanently unreachable",
		},
	}

	retry := DefaultRetryConfig()
	retry.InitialBackoff = 20 * time.Millisecond
	retry.MaxRetries = 2

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			subject := "test.retry_classified." + tc.name
			_, err = client.SubscribeForRequests(ctx, subject, func(_ context.Context, _ []byte) ([]byte, error) {
				return nil, tc.handlerErr
			})
			require.NoError(t, err)
			time.Sleep(50 * time.Millisecond)

			data, err := client.RequestWithRetryClassified(ctx, subject, []byte("write"), 2*time.Second, retry)
			if data != nil {
				t.Errorf("data should be nil on classified error; got %q", data)
			}
			if err == nil {
				t.Fatal("expected classified error")
			}
			if errs.IsInvalid(err) != tc.isInvalid {
				t.Errorf("IsInvalid=%v, want %v (err=%v)", errs.IsInvalid(err), tc.isInvalid, err)
			}
			if errs.IsTransient(err) != tc.isTransient {
				t.Errorf("IsTransient=%v, want %v (err=%v)", errs.IsTransient(err), tc.isTransient, err)
			}
			if errs.IsFatal(err) != tc.isFatal {
				t.Errorf("IsFatal=%v, want %v (err=%v)", errs.IsFatal(err), tc.isFatal, err)
			}
			if !strings.Contains(err.Error(), tc.wantInMsg) {
				t.Errorf("err=%q, want substring %q", err.Error(), tc.wantInMsg)
			}
		})
	}
}

// TestIntegration_RequestWithRetryClassified_SuccessPath confirms
// the non-error path through the retry-aware entry point: handler
// returns (data, nil), caller gets the same data, nil error.
func TestIntegration_RequestWithRetryClassified_SuccessPath(t *testing.T) {
	ctx := context.Background()

	natsContainer, natsURL := startNATSContainer(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	defer client.Close(ctx)

	subject := "test.retry_classified.success"
	want := []byte(`{"ok":true,"id":"abc123"}`)
	_, err = client.SubscribeForRequests(ctx, subject, func(_ context.Context, _ []byte) ([]byte, error) {
		return want, nil
	})
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)

	data, err := client.RequestWithRetryClassified(ctx, subject, []byte("write"), 2*time.Second, DefaultRetryConfig())
	require.NoError(t, err)
	if !bytes.Equal(data, want) {
		t.Fatalf("data = %q, want %q", data, want)
	}
}

// TestIntegration_RequestWithRetryClassified_RetriesThenSucceeds
// pins that the retry loop actually retries — responder starts late,
// the request retries through the responder-not-ready window, then
// succeeds with the classified contract intact. Catches a refactor
// that accidentally bypasses retry (e.g. delegating to
// RequestClassified directly).
func TestIntegration_RequestWithRetryClassified_RetriesThenSucceeds(t *testing.T) {
	ctx := context.Background()

	natsContainer, natsURL := startNATSContainer(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	defer client.Close(ctx)

	subject := "test.retry_classified.late_responder"
	want := []byte(`{"ack":"after-delay"}`)

	go func() {
		time.Sleep(150 * time.Millisecond)
		_, err := client.SubscribeForRequests(ctx, subject, func(_ context.Context, _ []byte) ([]byte, error) {
			return want, nil
		})
		if err != nil {
			t.Logf("late subscribe failed: %v", err)
		}
	}()

	retry := DefaultRetryConfig()
	retry.InitialBackoff = 50 * time.Millisecond
	retry.MaxRetries = 5
	retry.BackoffMultiplier = 1.5

	data, err := client.RequestWithRetryClassified(ctx, subject, []byte("write"), 2*time.Second, retry)
	require.NoError(t, err, "retry+classified should succeed once late responder comes up")
	if !bytes.Equal(data, want) {
		t.Fatalf("data = %q, want %q", data, want)
	}
}
