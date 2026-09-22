package agentic_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/stretchr/testify/require"
)

// TestPublishedRequestIDSurvivesTheDurableRoundTrip is the whole reason the
// field exists: the value a process wrote must be the value the NEXT process
// reads out of AGENT_LOOPS. A field that marshalled under one name and
// unmarshalled under another would leave every cold classification reading an
// empty string, which classifies every redelivered input as "no request
// outstanding" — the fail-open shape I1 exists to close.
func TestPublishedRequestIDSurvivesTheDurableRoundTrip(t *testing.T) {
	t.Parallel()

	entity := agentic.NewLoopEntity("loop-1", "task-1", "general", "model", 5)
	entity.PublishedRequestID = "loop-1:req:3:1"

	data, err := json.Marshal(entity)
	require.NoError(t, err)
	require.Contains(t, string(data), `"published_request_id":"loop-1:req:3:1"`,
		"the record's wire name is what a replacement process reads")

	var decoded agentic.LoopEntity
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, "loop-1:req:3:1", decoded.PublishedRequestID)
}

// TestPublishedRequestIDIsOmittedWhenUnset keeps the field additive for every
// record written before it existed and for every sister that decodes this
// record into its own struct: an unset field writes no key at all.
func TestPublishedRequestIDIsOmittedWhenUnset(t *testing.T) {
	t.Parallel()

	entity := agentic.NewLoopEntity("loop-1", "task-1", "general", "model", 5)
	data, err := json.Marshal(entity)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(data), "published_request_id"),
		"an unset published_request_id must not appear on the wire")

	var decoded agentic.LoopEntity
	require.NoError(t, json.Unmarshal([]byte(`{"id":"loop-1","state":"exploring","max_iterations":5}`), &decoded))
	require.Empty(t, decoded.PublishedRequestID, "a record written before the field decodes to empty, never to a default")
}

// TestTransitionToKeepsTheOutstandingRequest: a state move is not a settlement.
// The request named by the record is still retained and still outstanding after
// the loop moves between workflow states, so the transition must carry it — a
// transition that dropped it would make the very next cold read classify the
// response for that request as "no request outstanding".
func TestTransitionToKeepsTheOutstandingRequest(t *testing.T) {
	t.Parallel()

	entity := agentic.NewLoopEntity("loop-1", "task-1", "general", "model", 5)
	entity.PublishedRequestID = "loop-1:req:2:0"

	require.NoError(t, entity.TransitionTo(agentic.LoopStateExecuting))
	require.Equal(t, "loop-1:req:2:0", entity.PublishedRequestID)

	require.NoError(t, entity.TransitionTo(agentic.LoopStateExecuting), "same-state transition is a no-op")
	require.Equal(t, "loop-1:req:2:0", entity.PublishedRequestID)

	require.Error(t, entity.TransitionTo(agentic.LoopState("paused")))
	require.Equal(t, "loop-1:req:2:0", entity.PublishedRequestID, "a refused transition changes nothing")

	require.NoError(t, entity.TransitionTo(agentic.LoopStateComplete))
	require.Equal(t, "loop-1:req:2:0", entity.PublishedRequestID,
		"a terminal record keeps the name of the request it last published")
}

// TestValidateIgnoresTheOutstandingRequest pins that the field is not a
// validation gate: records written before it existed, and terminal records
// adopted during recovery, must still validate.
func TestValidateIgnoresTheOutstandingRequest(t *testing.T) {
	t.Parallel()

	entity := agentic.NewLoopEntity("loop-1", "task-1", "general", "model", 5)
	require.NoError(t, entity.Validate())
	entity.PublishedRequestID = "loop-1:req:1:0"
	require.NoError(t, entity.Validate())
}
