//go:build integration

package natsclient

import (
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
