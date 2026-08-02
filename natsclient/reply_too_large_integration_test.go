//go:build integration

package natsclient

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/pkg/errs"
)

// TestIntegration_OversizedReplyAnswersTyped pins D3's contract end-to-end on
// real NATS: a handler whose reply exceeds the server's advertised limit must
// produce a fast, Invalid-classified "too large" error at the CALLER — never a
// timeout. This is the branch the payload-bounds spec calls the sister-facing
// honesty gain; without this test it executed nowhere (2026-08-02 review,
// HIGH).
//
// Note the caller-side contract is class + message (the classified reply
// headers reconstruct an *errs.ClassifiedError); the ErrPayloadTooLarge
// sentinel is an in-process identity and does not cross the wire.
func TestIntegration_OversizedReplyAnswersTyped(t *testing.T) {
	tc := NewTestClient(t)
	client := tc.Client
	ctx := context.Background()

	limit := client.ServerPayloadLimit()
	if limit <= 0 {
		t.Fatalf("live server advertised no payload limit (got %d)", limit)
	}

	subject := "t.reply.toolarge"
	sub, err := client.SubscribeForRequests(ctx, subject, func(_ context.Context, _ []byte) ([]byte, error) {
		return make([]byte, limit+1), nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer func() { _ = sub.Unsubscribe() }()

	requestTimeout := 10 * time.Second
	start := time.Now()
	_, err = client.RequestClassified(ctx, subject, []byte(`{}`), requestTimeout)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("oversized reply must surface as an error at the caller")
	}
	if !errs.IsInvalid(err) {
		t.Fatalf("caller must receive the Invalid-classified too-large error, got class %v: %v",
			errs.Classify(err), err)
	}
	if !strings.Contains(err.Error(), "byte") {
		t.Fatalf("error must name the size facts, got: %v", err)
	}
	// The whole point: typed error in normal reply latency, not a timeout.
	if elapsed > requestTimeout/2 {
		t.Fatalf("caller waited %v — that is the timeout pathology D3 retires, not a typed reply", elapsed)
	}
}
