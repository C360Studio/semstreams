//go:build integration

package natsclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readyTestClient(ctx context.Context, t *testing.T) *Client {
	t.Helper()
	natsContainer, natsURL := startNATSContainer(ctx, t)
	t.Cleanup(func() { _ = natsContainer.Terminate(ctx) })
	client, err := NewClient(natsURL)
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	t.Cleanup(func() { _ = client.Close(ctx) })
	return client
}

// A responder that subscribes AFTER the reader starts must still be found: this
// is the cold-start race gated-dag hit (gh#420). A single-shot short request
// would fail; RequestReady retries within its budget and succeeds once the
// responder is up.
func TestIntegration_RequestReady_ResponderAppearsLate(t *testing.T) {
	ctx := context.Background()
	client := readyTestClient(ctx, t)
	const subject = "test.ready.late"

	// Subscribe the responder only after a delay that exceeds the probe timeout,
	// so success proves the loop RETRIED (a single 300ms probe would have failed).
	go func() {
		time.Sleep(700 * time.Millisecond)
		_, _ = client.SubscribeForRequests(ctx, subject, func(_ context.Context, _ []byte) ([]byte, error) {
			return []byte("pong"), nil
		})
	}()

	start := time.Now()
	// probe 300ms (< the 700ms responder delay), budget 10s (generous).
	resp, err := client.RequestReady(ctx, subject, []byte("ping"), 300*time.Millisecond, 10*time.Second)
	elapsed := time.Since(start)

	require.NoError(t, err, "readiness read must succeed once the responder appears")
	assert.Equal(t, []byte("pong"), resp)
	// Well under the budget — proves it converged on the responder, not the cap.
	assert.Less(t, elapsed, 5*time.Second, "should return shortly after the responder appears, not near the budget")
}

// No responder ever appears: the read must surface an error BOUNDED by the
// budget, not hang to a full per-attempt query timeout (the gh#420 30s hang).
func TestIntegration_RequestReady_NeverReady_BoundedByBudget(t *testing.T) {
	ctx := context.Background()
	client := readyTestClient(ctx, t)

	start := time.Now()
	_, err := client.RequestReady(ctx, "test.ready.absent", []byte("ping"), 300*time.Millisecond, 1*time.Second)
	elapsed := time.Since(start)

	require.Error(t, err)
	// Bounded by the budget (1s) with a ≥3× ceiling for CI/container jitter.
	// The point is it does NOT hang past the budget.
	assert.Less(t, elapsed, 3*time.Second, "must be bounded by the readiness budget, not a full query timeout")
}

// An immediately-available responder returns on the first attempt.
func TestIntegration_RequestReady_ImmediateResponder(t *testing.T) {
	ctx := context.Background()
	client := readyTestClient(ctx, t)
	const subject = "test.ready.immediate"

	_, err := client.SubscribeForRequests(ctx, subject, func(_ context.Context, _ []byte) ([]byte, error) {
		return []byte("pong"), nil
	})
	require.NoError(t, err)

	// No sleep: RequestReady tolerates not-yet-propagated interest by retrying —
	// that is the whole point. A present responder returns quickly.
	resp, err := client.RequestReady(ctx, subject, []byte("ping"), 300*time.Millisecond, 10*time.Second)
	require.NoError(t, err)
	assert.Equal(t, []byte("pong"), resp)
}

// A reply — even a handler-ERROR reply — means the responder is UP, so the loop
// must STOP (not retry to the budget) and return the classified error promptly.
func TestIntegration_RequestReadyClassified_HandlerErrorStopsLoop(t *testing.T) {
	ctx := context.Background()
	client := readyTestClient(ctx, t)
	const subject = "test.ready.handlererr"

	const marker = "handler-rejected-marker"
	_, err := client.SubscribeForRequests(ctx, subject, func(_ context.Context, _ []byte) ([]byte, error) {
		return nil, errors.New(marker) // handler error → classified error reply
	})
	require.NoError(t, err)

	start := time.Now()
	// Budget is large; if the loop wrongly retried the handler error it would burn
	// the whole budget. It must return promptly because a reply WAS received.
	_, err = client.RequestReadyClassified(ctx, subject, []byte("ping"), 300*time.Millisecond, 10*time.Second)
	elapsed := time.Since(start)

	require.Error(t, err)
	// The error must be the round-tripped HANDLER error (proving a reply was
	// received + classified), not a transport/budget error (which would say
	// "readiness budget" or "no responders").
	assert.Contains(t, err.Error(), marker, "must return the classified handler error, not a transport error")
	assert.NotContains(t, err.Error(), "readiness budget", "must not have retried to budget exhaustion")
	assert.Less(t, elapsed, 3*time.Second, "a received (error) reply means responder is up — must not retry to the budget")
}
