//go:build integration

package agenticloop

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

// The whole point of the classifier is that the two cases are opposite
// settlements, so they are proved against the same real bucket in one test:
// a terminal record and an absent record are both stale; a non-terminal
// record is live and must not be acknowledged away.
func TestClassifyMissingLoopReadsTheLoopsBucketNotMemory_Integration(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	ctx := t.Context()

	c := newStampTestComponent(t, testClient.Client, DefaultConfig())
	c.natsClient = testClient.Client
	require.NoError(t, c.initializeKVBuckets(ctx))

	const (
		liveLoopID     = "b0f7a1e2-2c4d-4a5b-8e6f-1d2c3b4a5e60"
		terminalLoopID = "c1e8b2f3-3d5e-4b6c-9f70-2e3d4c5b6f71"
		absentLoopID   = "d2f9c304-4e6f-4c7d-a081-3f4e5d6c7082"
	)

	// Nothing here is in c.handler's memory — that is the precondition every
	// call site has when it asks.
	putLoopRecord := func(entity agentic.LoopEntity) {
		t.Helper()
		data, err := json.Marshal(entity)
		require.NoError(t, err)
		_, err = c.loopsBucket.Put(ctx, entity.ID, data)
		require.NoError(t, err)
	}
	putLoopRecord(agentic.LoopEntity{ID: liveLoopID, State: agentic.LoopStateExploring})
	putLoopRecord(agentic.LoopEntity{ID: terminalLoopID, State: agentic.LoopStateComplete})

	require.Equal(t, loopPresenceLive, c.classifyMissingLoop(ctx, liveLoopID),
		"a non-terminal record means some process still owes this input work")
	require.Equal(t, loopPresenceStale, c.classifyMissingLoop(ctx, terminalLoopID),
		"a finished loop can never apply another input")
	require.Equal(t, loopPresenceStale, c.classifyMissingLoop(ctx, absentLoopID),
		"no record at all is the ordinary settled-drop")
}
