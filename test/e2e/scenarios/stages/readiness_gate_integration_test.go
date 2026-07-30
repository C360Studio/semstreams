//go:build integration

package stages

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/readiness"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/stretchr/testify/require"
)

// publishReadiness writes one producer's envelope to its GRAPH_STATUS key.
func publishReadiness(
	ctx context.Context, t *testing.T, tc *natsclient.TestClient,
	key string, status graph.IndexStatusResponse,
) {
	t.Helper()
	bucket, err := tc.CreateKVBucket(ctx, readiness.BucketGraphStatus)
	require.NoError(t, err)
	payload, err := json.Marshal(status)
	require.NoError(t, err)
	_, err = bucket.Put(ctx, key, payload)
	require.NoError(t, err)
}

func notCaughtUp() graph.IndexStatusResponse {
	return graph.IndexStatusResponse{
		State:             graph.IndexStateReady,
		Ready:             false,
		BootstrapComplete: true,
		Lag:               250, // healthy, but genuinely behind
	}
}

func caughtUp() graph.IndexStatusResponse {
	return graph.IndexStatusResponse{
		State:             graph.IndexStateReady,
		Ready:             true,
		BootstrapComplete: true,
		Lag:               0,
	}
}

// TestIntegration_StageWithholdsUntilProducersAreCovered closes the verification gap
// the Codex review raised.
//
// Both reported e2e runs showed `entity_load_poll_count=0`, i.e. the producers were
// already covered at the FIRST observation. That proves the keys were present and
// readable — but an implementation that simply published ready/zero unconditionally
// would produce an identical artifact. The e2e evidence therefore could not
// distinguish "the fold controls snapshot timing" from "the fold always says yes".
//
// This arranges a REAL not-covered observation: the stage must withhold while a
// producer reports outstanding work, and proceed only once every declared producer
// reports drained. Both directions are asserted, so neither an always-yes nor an
// always-no implementation passes.
func TestIntegration_StageWithholdsUntilProducersAreCovered(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	defer func() { _ = tc.Terminate() }()

	// Every declared key drained EXCEPT graph-ingest, which is behind.
	publishReadiness(ctx, t, tc, readiness.KeyGraphIndex, caughtUp())
	publishReadiness(ctx, t, tc, readiness.KeyRule, caughtUp())
	publishReadiness(ctx, t, tc, readiness.KeyGraphIngest, notCaughtUp())

	vc, err := client.NewNATSValidationClient(ctx, tc.URL)
	require.NoError(t, err)
	defer func() { _ = vc.Close(ctx) }()

	v := &EntityVerifier{
		NATSClient:        vc,
		Variant:           "structural",
		ValidationTimeout: 3 * time.Second,
		PollInterval:      50 * time.Millisecond,
	}

	// WITHHOLD: with one producer behind, the gate must not proceed.
	pollCount := 0
	err = v.awaitProducersCaughtUp(ctx, &pollCount)
	require.Error(t, err,
		"the stage proceeded while graph-ingest reported 250 messages outstanding — "+
			"an always-yes fold would pass the e2e tier identically")
	require.Contains(t, err.Error(), readiness.KeyGraphIngest,
		"the timeout must name the producer that withheld")
	require.Positive(t, pollCount,
		"a real not-covered observation must show polling; poll_count 0 is exactly the "+
			"artifact that could not distinguish withholding from always-proceeding")

	// RELEASE: the same stage must now proceed, promptly.
	publishReadiness(ctx, t, tc, readiness.KeyGraphIngest, caughtUp())

	released := 0
	require.Eventually(t, func() bool {
		v2 := &EntityVerifier{
			NATSClient:        vc,
			Variant:           "structural",
			ValidationTimeout: 3 * time.Second,
			PollInterval:      50 * time.Millisecond,
		}
		return v2.awaitProducersCaughtUp(ctx, &released) == nil
	}, 30*time.Second, 200*time.Millisecond,
		"the stage must proceed once every declared producer reports drained")
}

// TestIntegration_StageWithholdsOnAnAbsentProducer covers the other reason a fold can
// legitimately withhold: a declared key nobody publishes is UNKNOWN, not ready. This
// is the fail-closed leg at the stage level rather than the unit level.
func TestIntegration_StageWithholdsOnAnAbsentProducer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	defer func() { _ = tc.Terminate() }()

	// graph-index and rule report drained; graph-ingest is never published.
	publishReadiness(ctx, t, tc, readiness.KeyGraphIndex, caughtUp())
	publishReadiness(ctx, t, tc, readiness.KeyRule, caughtUp())

	vc, err := client.NewNATSValidationClient(ctx, tc.URL)
	require.NoError(t, err)
	defer func() { _ = vc.Close(ctx) }()

	v := &EntityVerifier{
		NATSClient:        vc,
		Variant:           "structural",
		ValidationTimeout: 3 * time.Second,
		PollInterval:      50 * time.Millisecond,
	}

	pollCount := 0
	err = v.awaitProducersCaughtUp(ctx, &pollCount)
	require.Error(t, err, "an absent declared producer must withhold, not read as ready")
	require.Contains(t, err.Error(), "status_unknown",
		"an absent key is UNKNOWN — distinct from a producer that published not-ready")
}
