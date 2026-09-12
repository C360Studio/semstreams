package agenticdispatch

// gh#1094 / ADR-101 — dispatch selects the user-facing terminal by the typed
// decision and resolves a route-less reply decision's origin from persisted
// AGENT_LOOPS ancestry (R3, R4′).
//
// Every captured response is decoded into a FRESH agentic.UserResponse value.
// The persisted-loop seam is the production one (loadPersistedLoopFn stands in
// for the KV read); absent keys are served as the production absence error so
// the resolver's absent/transient split is exercised, not simulated.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/agentterminal"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// loopRecordAbsent is the error shape loadPersistedLoop returns for a key that
// is not in AGENT_LOOPS (expired 24h TTL, or a best-effort Put that never
// succeeded). It is NOT a transient read failure.
func loopRecordAbsent(loopID string) error {
	return fmt.Errorf("loop state %q not yet observable: %w", loopID, jetstream.ErrKeyNotFound)
}

// ancestryLoader serves a fixed set of AGENT_LOOPS records and records the
// exact read sequence. Any key not in records is served as absent.
type ancestryLoader struct {
	records  map[string]agentic.LoopEntity
	errors   map[string]error
	sequence []string
}

func (l *ancestryLoader) load(_ context.Context, loopID string) (*agentic.LoopEntity, error) {
	l.sequence = append(l.sequence, loopID)
	if err, ok := l.errors[loopID]; ok {
		return nil, err
	}
	record, ok := l.records[loopID]
	if !ok {
		return nil, loopRecordAbsent(loopID)
	}
	fresh := record
	return &fresh, nil
}

func newAncestryLoader(records ...agentic.LoopEntity) *ancestryLoader {
	byID := make(map[string]agentic.LoopEntity, len(records))
	for _, record := range records {
		byID[record.ID] = record
	}
	return &ancestryLoader{records: byID, errors: map[string]error{}}
}

// terminalTestComponentWithLog is terminalTestComponent with a captured log
// sink, for the dispositions whose contract includes what the Warn names.
func terminalTestComponentWithLog(t *testing.T) (*Component, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	reg := payloadregistry.NewWithSubset(t, agentic.RegisterPayloads)
	return &Component{
		config:  DefaultConfig(),
		logger:  logger,
		metrics: getMetrics(metric.NewMetricsRegistry()),
		decoder: message.NewDecoder(reg),
	}, &buf
}

func decideCompletion(loopID, action, reason string) *agentic.LoopCompletedEvent {
	return &agentic.LoopCompletedEvent{
		LoopID:      loopID,
		TaskID:      "task-" + loopID,
		Outcome:     agentic.OutcomeSuccess,
		Role:        "coordinator",
		Result:      fmt.Sprintf(`{"action":%q,"reason":%q}`, action, reason),
		CompletedAt: time.Unix(1_700_100_000, 0).UTC(),
		Decision:    &agentic.CoordinatorDecision{Action: action, Reason: reason},
	}
}

// captureResponse installs the terminal publish seam and returns a getter that
// decodes the captured wire bytes into a FRESH UserResponse value.
func captureResponse(t *testing.T, c *Component) func() (agentic.UserResponse, string, int) {
	t.Helper()
	var data []byte
	var msgID string
	count := 0
	c.sendTerminalResponseFn = func(_ context.Context, response agentic.UserResponse, id string) error {
		count++
		msgID = id
		encoded, err := json.Marshal(message.NewBaseMessage(response.Schema(), &response, "agentic-dispatch"))
		require.NoError(t, err)
		data = encoded
		return nil
	}
	return func() (agentic.UserResponse, string, int) {
		if count == 0 {
			return agentic.UserResponse{}, "", 0
		}
		decoded, err := c.decoder.Decode(data)
		require.NoError(t, err)
		fresh, ok := decoded.Payload().(*agentic.UserResponse)
		require.True(t, ok, "expected *agentic.UserResponse, got %T", decoded.Payload())
		return *fresh, msgID, count
	}
}

func TestSettleAgentTerminalHandoffDecisionOnRoutedLoopPublishesNothing(t *testing.T) {
	c := terminalTestComponent(t)
	loader := newAncestryLoader(agentic.LoopEntity{
		ID: "35f24ee8-8bb9-4dc4-bc8e-000000000015", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000015", State: agentic.LoopStateComplete, MaxIterations: 3,
		ChannelType: "http", ChannelID: "origin-1", UserID: "user-1",
	})
	c.loadPersistedLoopFn = loader.load
	get := captureResponse(t, c)

	before := terminalReasonSnapshot(c)
	require.NoError(t, c.settleAgentTerminal(context.Background(),
		completionPayload(t, decideCompletion("35f24ee8-8bb9-4dc4-bc8e-000000000015", "autoresearch", "hand off to the chain"))))

	_, _, count := get()
	require.Zero(t, count, "a routed handoff decision must publish nothing")
	requireOneTerminalReason(t, c, "handoff_settled", before)
}

func TestSettleAgentTerminalHandoffDecisionOnRouteLessLoopPublishesNothing(t *testing.T) {
	c := terminalTestComponent(t)
	loader := newAncestryLoader(
		agentic.LoopEntity{
			ID: "35f24ee8-8bb9-4dc4-bc8e-000000000016", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000016", State: agentic.LoopStateComplete, MaxIterations: 3,
			ParentLoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000015", RunID: "35f24ee8-8bb9-4dc4-bc8e-000000000015",
		},
		agentic.LoopEntity{
			ID: "35f24ee8-8bb9-4dc4-bc8e-000000000015", State: agentic.LoopStateComplete, MaxIterations: 3,
			ChannelType: "http", ChannelID: "origin-1",
		},
	)
	c.loadPersistedLoopFn = loader.load
	get := captureResponse(t, c)

	before := terminalReasonSnapshot(c)
	require.NoError(t, c.settleAgentTerminal(context.Background(),
		completionPayload(t, decideCompletion("35f24ee8-8bb9-4dc4-bc8e-000000000016", "synthesize", "enough evidence"))))

	_, _, count := get()
	require.Zero(t, count, "a handoff decision never borrows an origin")
	require.Equal(t, []string{"35f24ee8-8bb9-4dc4-bc8e-000000000016"}, loader.sequence, "a handoff must not walk ancestry")
	requireOneTerminalReason(t, c, "handoff_settled", before)
}

func TestSettleAgentTerminalRespondDirectOnRoutedLoopPublishesResultWithReason(t *testing.T) {
	c := terminalTestComponent(t)
	loader := newAncestryLoader(agentic.LoopEntity{
		ID: "35f24ee8-8bb9-4dc4-bc8e-000000000017", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000017", State: agentic.LoopStateComplete, MaxIterations: 3,
		ChannelType: "http", ChannelID: "origin-1", UserID: "user-1",
	})
	c.loadPersistedLoopFn = loader.load
	get := captureResponse(t, c)

	before := terminalReasonSnapshot(c)
	require.NoError(t, c.settleAgentTerminal(context.Background(),
		completionPayload(t, decideCompletion("35f24ee8-8bb9-4dc4-bc8e-000000000017", agentic.DecideActionRespondDirect, "Optimized the flight plan."))))

	response, _, count := get()
	require.Equal(t, 1, count)
	require.Equal(t, agentic.ResponseTypeResult, response.Type)
	require.Equal(t, "Optimized the flight plan.", response.Content, "content is the decision reason, not the decision JSON")
	require.Equal(t, "http", response.ChannelType)
	require.Equal(t, "origin-1", response.ChannelID)
	require.Equal(t, "user-1", response.UserID)
	require.Equal(t, "35f24ee8-8bb9-4dc4-bc8e-000000000017", response.InReplyTo)
	require.Equal(t, []string{"35f24ee8-8bb9-4dc4-bc8e-000000000017"}, loader.sequence, "an own-routed reply resolves no ancestry")
	requireOneTerminalReason(t, c, "response_settled", before)
}

func TestSettleAgentTerminalAskUserDecisionPublishesPromptToOrigin(t *testing.T) {
	c := terminalTestComponent(t)
	loader := newAncestryLoader(
		agentic.LoopEntity{
			ID: "35f24ee8-8bb9-4dc4-bc8e-000000000018", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000018", State: agentic.LoopStateComplete, MaxIterations: 3,
			ParentLoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000015",
		},
		agentic.LoopEntity{
			ID: "35f24ee8-8bb9-4dc4-bc8e-000000000015", State: agentic.LoopStateComplete, MaxIterations: 3,
			ChannelType: "slack", ChannelID: "C123", UserID: "user-7",
		},
	)
	c.loadPersistedLoopFn = loader.load
	get := captureResponse(t, c)

	before := terminalReasonSnapshot(c)
	require.NoError(t, c.settleAgentTerminal(context.Background(),
		completionPayload(t, decideCompletion("35f24ee8-8bb9-4dc4-bc8e-000000000018", agentic.DecideActionAskUser, "Which airframe?"))))

	response, _, count := get()
	require.Equal(t, 1, count)
	require.Equal(t, agentic.ResponseTypePrompt, response.Type)
	require.Equal(t, "Which airframe?", response.Content)
	require.Equal(t, "slack", response.ChannelType)
	require.Equal(t, "C123", response.ChannelID)
	require.Equal(t, "35f24ee8-8bb9-4dc4-bc8e-000000000018", response.InReplyTo, "the reply re-enters at the deciding loop")
	requireOneTerminalReason(t, c, "response_settled", before)
}

func TestSettleAgentTerminalUserFacingDecisionResolvesOriginByAncestry(t *testing.T) {
	c := terminalTestComponent(t)
	// Unthreaded chain: no RunID anywhere, three deep, authority reads only.
	loader := newAncestryLoader(
		agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000019", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000019", State: agentic.LoopStateComplete, MaxIterations: 3, ParentLoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000016"},
		agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000016", State: agentic.LoopStateComplete, MaxIterations: 3, ParentLoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000015"},
		agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000015", State: agentic.LoopStateComplete, MaxIterations: 3, ChannelType: "http", ChannelID: "origin-1", UserID: "user-1"},
	)
	c.loadPersistedLoopFn = loader.load
	get := captureResponse(t, c)

	before := terminalReasonSnapshot(c)
	require.NoError(t, c.settleAgentTerminal(context.Background(),
		completionPayload(t, decideCompletion("35f24ee8-8bb9-4dc4-bc8e-000000000019", agentic.DecideActionRespondDirect, "Here is the answer."))))

	response, _, count := get()
	require.Equal(t, 1, count)
	require.Equal(t, agentic.ResponseTypeResult, response.Type)
	require.Equal(t, "Here is the answer.", response.Content)
	require.Equal(t, "http", response.ChannelType)
	require.Equal(t, "origin-1", response.ChannelID)
	require.Equal(t, "user-1", response.UserID)
	require.Equal(t, "35f24ee8-8bb9-4dc4-bc8e-000000000019", response.InReplyTo)
	require.Equal(t, []string{"35f24ee8-8bb9-4dc4-bc8e-000000000019", "35f24ee8-8bb9-4dc4-bc8e-000000000016", "35f24ee8-8bb9-4dc4-bc8e-000000000015"}, loader.sequence)
	requireOneTerminalReason(t, c, "response_settled", before)
}

func TestSettleAgentTerminalMissingParentFallsBackToRunID(t *testing.T) {
	t.Run("parent_key_absent", func(t *testing.T) {
		c := terminalTestComponent(t)
		loader := newAncestryLoader(
			agentic.LoopEntity{
				ID: "35f24ee8-8bb9-4dc4-bc8e-000000000019", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000019", State: agentic.LoopStateComplete, MaxIterations: 3,
				ParentLoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000007", RunID: "35f24ee8-8bb9-4dc4-bc8e-000000000015",
			},
			agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000015", State: agentic.LoopStateComplete, MaxIterations: 3, ChannelType: "http", ChannelID: "origin-1"},
		)
		c.loadPersistedLoopFn = loader.load
		get := captureResponse(t, c)

		before := terminalReasonSnapshot(c)
		require.NoError(t, c.settleAgentTerminal(context.Background(),
			completionPayload(t, decideCompletion("35f24ee8-8bb9-4dc4-bc8e-000000000019", agentic.DecideActionRespondDirect, "answered"))))

		response, _, count := get()
		require.Equal(t, 1, count, "an absent parent key must not settle while a durable RunID is in hand")
		require.Equal(t, "http", response.ChannelType)
		require.Equal(t, "origin-1", response.ChannelID)
		requireOneTerminalReason(t, c, "response_settled", before)
	})

	t.Run("parent_link_empty", func(t *testing.T) {
		c := terminalTestComponent(t)
		loader := newAncestryLoader(
			agentic.LoopEntity{
				ID: "35f24ee8-8bb9-4dc4-bc8e-000000000019", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000019", State: agentic.LoopStateComplete, MaxIterations: 3,
				RunID: "35f24ee8-8bb9-4dc4-bc8e-000000000015",
			},
			agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000015", State: agentic.LoopStateComplete, MaxIterations: 3, ChannelType: "http", ChannelID: "origin-1"},
		)
		c.loadPersistedLoopFn = loader.load
		get := captureResponse(t, c)

		before := terminalReasonSnapshot(c)
		require.NoError(t, c.settleAgentTerminal(context.Background(),
			completionPayload(t, decideCompletion("35f24ee8-8bb9-4dc4-bc8e-000000000019", agentic.DecideActionRespondDirect, "answered"))))

		response, _, count := get()
		require.Equal(t, 1, count, "a severed parent link must not settle while a durable RunID is in hand")
		require.Equal(t, "origin-1", response.ChannelID)
		requireOneTerminalReason(t, c, "response_settled", before)
	})

	t.Run("typed_lookup_precedes_parent_walk", func(t *testing.T) {
		c := terminalTestComponent(t)
		loader := newAncestryLoader(
			agentic.LoopEntity{
				ID: "35f24ee8-8bb9-4dc4-bc8e-000000000019", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000019", State: agentic.LoopStateComplete, MaxIterations: 3,
				ParentLoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000016", RunID: "35f24ee8-8bb9-4dc4-bc8e-000000000015",
			},
			agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000016", State: agentic.LoopStateComplete, MaxIterations: 3, ParentLoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000015"},
			agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000015", State: agentic.LoopStateComplete, MaxIterations: 3, ChannelType: "http", ChannelID: "origin-1"},
		)
		c.loadPersistedLoopFn = loader.load
		get := captureResponse(t, c)

		require.NoError(t, c.settleAgentTerminal(context.Background(),
			completionPayload(t, decideCompletion("35f24ee8-8bb9-4dc4-bc8e-000000000019", agentic.DecideActionRespondDirect, "answered"))))

		_, _, count := get()
		require.Equal(t, 1, count)
		require.Equal(t, []string{"35f24ee8-8bb9-4dc4-bc8e-000000000019", "35f24ee8-8bb9-4dc4-bc8e-000000000015"}, loader.sequence,
			"the run anchor is read first; the parent key is never read")
	})

	t.Run("intermediate_run_anchor_after_absent_parent", func(t *testing.T) {
		// The C1 retry inside the walk: the terminal carries no run anchor,
		// but an intermediate record does, and its parent key is gone.
		c := terminalTestComponent(t)
		loader := newAncestryLoader(
			agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000019", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000019", State: agentic.LoopStateComplete, MaxIterations: 3, ParentLoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000016"},
			agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000016", State: agentic.LoopStateComplete, MaxIterations: 3, ParentLoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000007", RunID: "35f24ee8-8bb9-4dc4-bc8e-000000000015"},
			agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000015", State: agentic.LoopStateComplete, MaxIterations: 3, ChannelType: "http", ChannelID: "origin-1"},
		)
		c.loadPersistedLoopFn = loader.load
		get := captureResponse(t, c)

		before := terminalReasonSnapshot(c)
		require.NoError(t, c.settleAgentTerminal(context.Background(),
			completionPayload(t, decideCompletion("35f24ee8-8bb9-4dc4-bc8e-000000000019", agentic.DecideActionRespondDirect, "answered"))))

		response, _, count := get()
		require.Equal(t, 1, count)
		require.Equal(t, "origin-1", response.ChannelID)
		requireOneTerminalReason(t, c, "response_settled", before)
	})
}

func TestSettleAgentTerminalNoDecisionRouteLessLoopStaysRouteLess(t *testing.T) {
	c := terminalTestComponent(t)
	loader := newAncestryLoader(
		agentic.LoopEntity{
			ID: "35f24ee8-8bb9-4dc4-bc8e-000000000020", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000020", State: agentic.LoopStateComplete, MaxIterations: 3,
			ParentLoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000015", RunID: "35f24ee8-8bb9-4dc4-bc8e-000000000015",
		},
		agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000015", State: agentic.LoopStateComplete, MaxIterations: 3, ChannelType: "http", ChannelID: "origin-1"},
	)
	c.loadPersistedLoopFn = loader.load
	get := captureResponse(t, c)

	before := terminalReasonSnapshot(c)
	require.NoError(t, c.settleAgentTerminal(context.Background(), completionPayload(t, &agentic.LoopCompletedEvent{
		LoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000020", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000020", Outcome: agentic.OutcomeSuccess,
		Result: "baseline gathered", CompletedAt: time.Unix(1_700_100_100, 0).UTC(),
	})))

	_, _, count := get()
	require.Zero(t, count, "an internal phase completion never reaches the user channel")
	require.Equal(t, []string{"35f24ee8-8bb9-4dc4-bc8e-000000000020"}, loader.sequence, "a terminal without a decision resolves no origin")
	requireOneTerminalReason(t, c, "route_less_settled", before)
}

func TestSettleAgentTerminalReplyDecisionWithRouteLessRootSettlesRouteLess(t *testing.T) {
	c := terminalTestComponent(t)
	// A bus-submitted root: no parent, no run anchor, no route. There was no
	// origin — nothing pointed at something unobservable.
	loader := newAncestryLoader(agentic.LoopEntity{
		ID: "35f24ee8-8bb9-4dc4-bc8e-000000000021", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000021", State: agentic.LoopStateComplete, MaxIterations: 3,
	})
	c.loadPersistedLoopFn = loader.load
	get := captureResponse(t, c)

	before := terminalReasonSnapshot(c)
	require.NoError(t, c.settleAgentTerminal(context.Background(),
		completionPayload(t, decideCompletion("35f24ee8-8bb9-4dc4-bc8e-000000000021", agentic.DecideActionRespondDirect, "answered nobody"))))

	_, _, count := get()
	require.Zero(t, count)
	requireOneTerminalReason(t, c, "route_less_settled", before)
}

func TestSettleAgentTerminalUserFacingDecisionKeepsStableIdentityOnRedelivery(t *testing.T) {
	c := terminalTestComponent(t)
	loader := newAncestryLoader(
		agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000019", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000019", State: agentic.LoopStateComplete, MaxIterations: 3, ParentLoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000015"},
		agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000015", State: agentic.LoopStateComplete, MaxIterations: 3, ChannelType: "http", ChannelID: "origin-1"},
	)
	c.loadPersistedLoopFn = loader.load

	var ids []string
	var routes []string
	c.sendTerminalResponseFn = func(_ context.Context, response agentic.UserResponse, msgID string) error {
		ids = append(ids, msgID)
		routes = append(routes, response.ChannelType+"."+response.ChannelID)
		require.Equal(t, msgID, response.ResponseID)
		return nil
	}

	data := completionPayload(t, decideCompletion("35f24ee8-8bb9-4dc4-bc8e-000000000019", agentic.DecideActionRespondDirect, "answered"))
	var source struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(data, &source))

	require.NoError(t, c.settleAgentTerminal(context.Background(), data))
	require.NoError(t, c.settleAgentTerminal(context.Background(), data))

	require.Equal(t, []string{terminalResponseIDPrefix + source.ID, terminalResponseIDPrefix + source.ID}, ids)
	require.Equal(t, []string{"http.origin-1", "http.origin-1"}, routes, "a redelivery reuses the same origin")
}

func TestResolveOriginRouteBoundsHopsAndDetectsCycles(t *testing.T) {
	t.Run("cycle", func(t *testing.T) {
		c := terminalTestComponent(t)
		loader := newAncestryLoader(
			agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000022", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000022", State: agentic.LoopStateComplete, MaxIterations: 3, ParentLoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000023"},
			agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000023", State: agentic.LoopStateComplete, MaxIterations: 3, ParentLoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000022"},
		)
		c.loadPersistedLoopFn = loader.load
		get := captureResponse(t, c)

		before := terminalReasonSnapshot(c)
		require.NoError(t, c.settleAgentTerminal(context.Background(),
			completionPayload(t, decideCompletion("35f24ee8-8bb9-4dc4-bc8e-000000000022", agentic.DecideActionRespondDirect, "answered"))))

		_, _, count := get()
		require.Zero(t, count)
		require.Less(t, len(loader.sequence), 8, "a cycle must be detected, not walked")
		requireOneTerminalReason(t, c, "origin_unresolvable", before)
	})

	t.Run("hop_bound", func(t *testing.T) {
		c := terminalTestComponent(t)
		records := make([]agentic.LoopEntity, 0, 64)
		for i := range 64 {
			records = append(records, agentic.LoopEntity{
				ID:     fmt.Sprintf("bf8ec69d-4520-4b29-8c21-%012d", i),
				TaskID: fmt.Sprintf("task-bf8ec69d-4520-4b29-8c21-%012d", i),
				State:  agentic.LoopStateComplete, MaxIterations: 3,
				ParentLoopID: fmt.Sprintf("bf8ec69d-4520-4b29-8c21-%012d", i+1),
			})
		}
		loader := newAncestryLoader(records...)
		c.loadPersistedLoopFn = loader.load
		get := captureResponse(t, c)

		before := terminalReasonSnapshot(c)
		require.NoError(t, c.settleAgentTerminal(context.Background(),
			completionPayload(t, decideCompletion("bf8ec69d-4520-4b29-8c21-000000000000", agentic.DecideActionRespondDirect, "answered"))))

		_, _, count := get()
		require.Zero(t, count)
		require.LessOrEqual(t, len(loader.sequence), 34, "the walk is bounded at 32 hops")
		requireOneTerminalReason(t, c, "origin_unresolvable", before)
	})
}

func TestResolveOriginRouteSettlesOriginUnresolvableOnlyAfterParentAndRunIDExhausted(t *testing.T) {
	t.Run("absent_parent_and_absent_run_anchor", func(t *testing.T) {
		c, logs := terminalTestComponentWithLog(t)
		loader := newAncestryLoader(agentic.LoopEntity{
			ID: "35f24ee8-8bb9-4dc4-bc8e-000000000019", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000019", State: agentic.LoopStateComplete, MaxIterations: 3,
			ParentLoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000007", RunID: "35f24ee8-8bb9-4dc4-bc8e-000000000024",
		})
		c.loadPersistedLoopFn = loader.load
		get := captureResponse(t, c)

		before := terminalReasonSnapshot(c)
		require.NoError(t, c.settleAgentTerminal(context.Background(),
			completionPayload(t, decideCompletion("35f24ee8-8bb9-4dc4-bc8e-000000000019", agentic.DecideActionRespondDirect, "answered"))))

		_, _, count := get()
		require.Zero(t, count)
		require.Contains(t, loader.sequence, "35f24ee8-8bb9-4dc4-bc8e-000000000024", "the run anchor must be tried")
		require.Contains(t, loader.sequence, "35f24ee8-8bb9-4dc4-bc8e-000000000007", "the parent chain must be tried")
		requireOneTerminalReason(t, c, "origin_unresolvable", before)
		require.Contains(t, logs.String(), "35f24ee8-8bb9-4dc4-bc8e-000000000007", "the warning names the absent loop")
		require.Contains(t, logs.String(), "35f24ee8-8bb9-4dc4-bc8e-000000000024", "the warning names the run anchor")
	})

	t.Run("absent_run_anchor_then_linkless_end", func(t *testing.T) {
		// The shape the delta's two sentences could be read against each
		// other (owner item 8: origin_unresolvable is DISTINCT from
		// route_less_settled). A durable run anchor pointed at a record that
		// could not be observed, and the walk then ran out of links: there
		// WAS an origin, it just is not observable, so this is the
		// retention/persistence alert, never "there was no origin".
		c, logs := terminalTestComponentWithLog(t)
		loader := newAncestryLoader(agentic.LoopEntity{
			ID: "35f24ee8-8bb9-4dc4-bc8e-000000000019", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000019", State: agentic.LoopStateComplete, MaxIterations: 3,
			RunID: "35f24ee8-8bb9-4dc4-bc8e-000000000024",
		})
		c.loadPersistedLoopFn = loader.load
		get := captureResponse(t, c)

		before := terminalReasonSnapshot(c)
		require.NoError(t, c.settleAgentTerminal(context.Background(),
			completionPayload(t, decideCompletion("35f24ee8-8bb9-4dc4-bc8e-000000000019", agentic.DecideActionRespondDirect, "answered"))))

		_, _, count := get()
		require.Zero(t, count)
		require.Equal(t, []string{"35f24ee8-8bb9-4dc4-bc8e-000000000019", "35f24ee8-8bb9-4dc4-bc8e-000000000024"}, loader.sequence)
		requireOneTerminalReason(t, c, "origin_unresolvable", before)
		require.Contains(t, logs.String(), "35f24ee8-8bb9-4dc4-bc8e-000000000024", "the warning names the absent run anchor")
		require.Contains(t, logs.String(), "no further link", "the warning states the parent chain ran out")
	})

	t.Run("absent_parent_and_no_run_anchor", func(t *testing.T) {
		c, logs := terminalTestComponentWithLog(t)
		loader := newAncestryLoader(agentic.LoopEntity{
			ID: "35f24ee8-8bb9-4dc4-bc8e-000000000019", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000019", State: agentic.LoopStateComplete, MaxIterations: 3,
			ParentLoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000007",
		})
		c.loadPersistedLoopFn = loader.load
		get := captureResponse(t, c)

		before := terminalReasonSnapshot(c)
		require.NoError(t, c.settleAgentTerminal(context.Background(),
			completionPayload(t, decideCompletion("35f24ee8-8bb9-4dc4-bc8e-000000000019", agentic.DecideActionRespondDirect, "answered"))))

		_, _, count := get()
		require.Zero(t, count)
		requireOneTerminalReason(t, c, "origin_unresolvable", before)
		require.Contains(t, logs.String(), "35f24ee8-8bb9-4dc4-bc8e-000000000007", "the warning names the absent loop")
		require.Contains(t, logs.String(), "none", "the warning states there was no run anchor")
	})
}

func TestResolveOriginRouteTransientReadDelaysNak(t *testing.T) {
	c := terminalTestComponent(t)
	loader := newAncestryLoader(agentic.LoopEntity{
		ID: "35f24ee8-8bb9-4dc4-bc8e-000000000019", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000019", State: agentic.LoopStateComplete, MaxIterations: 3, ParentLoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000015",
	})
	loader.errors["35f24ee8-8bb9-4dc4-bc8e-000000000015"] = errors.New("kv unavailable")
	c.loadPersistedLoopFn = loader.load
	get := captureResponse(t, c)

	before := terminalReasonSnapshot(c)
	err := c.settleAgentTerminal(context.Background(),
		completionPayload(t, decideCompletion("35f24ee8-8bb9-4dc4-bc8e-000000000019", agentic.DecideActionRespondDirect, "answered")))
	require.Error(t, err)
	require.False(t, isPermanentTerminal(err), "a transient ancestor read is redelivered, never classified")

	_, _, count := get()
	require.Zero(t, count)
	requireOneTerminalReason(t, c, "routing_read_transient", before)
}

func TestResolveOriginRouteMalformedAncestorIsPermanent(t *testing.T) {
	t.Run("malformed_record", func(t *testing.T) {
		c := terminalTestComponent(t)
		loader := newAncestryLoader(agentic.LoopEntity{
			ID: "35f24ee8-8bb9-4dc4-bc8e-000000000019", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000019", State: agentic.LoopStateComplete, MaxIterations: 3, ParentLoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000015",
		})
		loader.errors["35f24ee8-8bb9-4dc4-bc8e-000000000015"] = permanentTerminal("malformed AGENT_LOOPS/35f24ee8-8bb9-4dc4-bc8e-000000000015")
		c.loadPersistedLoopFn = loader.load

		before := terminalReasonSnapshot(c)
		err := c.settleAgentTerminal(context.Background(),
			completionPayload(t, decideCompletion("35f24ee8-8bb9-4dc4-bc8e-000000000019", agentic.DecideActionRespondDirect, "answered")))
		require.Error(t, err)
		require.True(t, isPermanentTerminal(err))
		requireOneTerminalReason(t, c, "routing_malformed", before)
	})

	t.Run("partial_route_on_ancestor", func(t *testing.T) {
		c := terminalTestComponent(t)
		loader := newAncestryLoader(
			agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000019", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000019", State: agentic.LoopStateComplete, MaxIterations: 3, ParentLoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000015"},
			agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000015", State: agentic.LoopStateComplete, MaxIterations: 3, ChannelType: "http"},
		)
		c.loadPersistedLoopFn = loader.load

		before := terminalReasonSnapshot(c)
		err := c.settleAgentTerminal(context.Background(),
			completionPayload(t, decideCompletion("35f24ee8-8bb9-4dc4-bc8e-000000000019", agentic.DecideActionRespondDirect, "answered")))
		require.Error(t, err)
		require.True(t, isPermanentTerminal(err))
		requireOneTerminalReason(t, c, "routing_malformed", before)
	})
}

func TestSettleAgentTerminalRouteLessRunRootContinuesToRoutedAncestor(t *testing.T) {
	t.Run("routed ancestor above a route-less run root", func(t *testing.T) {
		// A product-minted run can sit BELOW the loop that owns the channel:
		// the run root is route-less, and the front door is its parent. The
		// typed-first lookup must continue the walk from the root instead of
		// settling on it.
		c := terminalTestComponent(t)
		loader := newAncestryLoader(
			agentic.LoopEntity{
				ID: "35f24ee8-8bb9-4dc4-bc8e-000000000019", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000019", State: agentic.LoopStateComplete, MaxIterations: 3,
				RunID: "35f24ee8-8bb9-4dc4-bc8e-000000000025",
			},
			agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000025", State: agentic.LoopStateComplete, MaxIterations: 3, ParentLoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000017"},
			agentic.LoopEntity{
				ID: "35f24ee8-8bb9-4dc4-bc8e-000000000017", State: agentic.LoopStateComplete, MaxIterations: 3,
				ChannelType: "http", ChannelID: "origin-1", UserID: "user-1",
			},
		)
		c.loadPersistedLoopFn = loader.load
		get := captureResponse(t, c)

		before := terminalReasonSnapshot(c)
		require.NoError(t, c.settleAgentTerminal(context.Background(),
			completionPayload(t, decideCompletion("35f24ee8-8bb9-4dc4-bc8e-000000000019", agentic.DecideActionRespondDirect, "answered"))))

		response, _, count := get()
		require.Equal(t, 1, count)
		require.Equal(t, "http", response.ChannelType)
		require.Equal(t, "origin-1", response.ChannelID)
		require.Equal(t, []string{"35f24ee8-8bb9-4dc4-bc8e-000000000019", "35f24ee8-8bb9-4dc4-bc8e-000000000025", "35f24ee8-8bb9-4dc4-bc8e-000000000017"}, loader.sequence,
			"the walk continues FROM the route-less run root, it does not settle on it")
		requireOneTerminalReason(t, c, "response_settled", before)
	})

	t.Run("severed ancestry above a route-less run root", func(t *testing.T) {
		// Same shape, but the hop above the run root was fired from a
		// non-loop entity: no parent link, no run anchor, no route. Nothing
		// pointed at an unobservable record, so the answer is "there was no
		// origin", not "the origin could not be observed".
		c := terminalTestComponent(t)
		loader := newAncestryLoader(
			agentic.LoopEntity{
				ID: "35f24ee8-8bb9-4dc4-bc8e-000000000019", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000019", State: agentic.LoopStateComplete, MaxIterations: 3,
				RunID: "35f24ee8-8bb9-4dc4-bc8e-000000000025",
			},
			agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000025", State: agentic.LoopStateComplete, MaxIterations: 3, ParentLoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000026"},
			agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000026", State: agentic.LoopStateComplete, MaxIterations: 3},
		)
		c.loadPersistedLoopFn = loader.load
		get := captureResponse(t, c)

		before := terminalReasonSnapshot(c)
		require.NoError(t, c.settleAgentTerminal(context.Background(),
			completionPayload(t, decideCompletion("35f24ee8-8bb9-4dc4-bc8e-000000000019", agentic.DecideActionRespondDirect, "answered"))))

		_, _, count := get()
		require.Zero(t, count)
		require.Equal(t, []string{"35f24ee8-8bb9-4dc4-bc8e-000000000019", "35f24ee8-8bb9-4dc4-bc8e-000000000025", "35f24ee8-8bb9-4dc4-bc8e-000000000026"}, loader.sequence)
		requireOneTerminalReason(t, c, "route_less_settled", before)
	})
}

func TestSettleAgentTerminalAskUserOnRoutedLoopPublishesPromptToItsOwnRoute(t *testing.T) {
	// The 35f24ee8-8bb9-4dc4-bc8e-000000000017 shape of ask_user: the deciding loop owns the channel,
	// so no ancestry is walked, and the projection is still a prompt.
	c := terminalTestComponent(t)
	loader := newAncestryLoader(agentic.LoopEntity{
		ID: "35f24ee8-8bb9-4dc4-bc8e-000000000017", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000017", State: agentic.LoopStateComplete, MaxIterations: 3,
		ChannelType: "http", ChannelID: "origin-1", UserID: "user-1",
	})
	c.loadPersistedLoopFn = loader.load
	get := captureResponse(t, c)

	before := terminalReasonSnapshot(c)
	require.NoError(t, c.settleAgentTerminal(context.Background(),
		completionPayload(t, decideCompletion("35f24ee8-8bb9-4dc4-bc8e-000000000017", agentic.DecideActionAskUser, "Which airframe?"))))

	response, _, count := get()
	require.Equal(t, 1, count)
	require.Equal(t, agentic.ResponseTypePrompt, response.Type)
	require.Equal(t, "Which airframe?", response.Content)
	require.Equal(t, "origin-1", response.ChannelID)
	require.Equal(t, "35f24ee8-8bb9-4dc4-bc8e-000000000017", response.InReplyTo)
	require.Equal(t, []string{"35f24ee8-8bb9-4dc4-bc8e-000000000017"}, loader.sequence, "an own-routed prompt resolves no ancestry")
	requireOneTerminalReason(t, c, "response_settled", before)
}

// completionEnvelopeWithRawDecision builds a terminal envelope whose payload
// carries an arbitrary `decision` object. It splices the field into the marshalled
// wire bytes deliberately: BaseMessage.MarshalJSON VALIDATES its payload, so the
// framework cannot emit a malformed decision at all — only a foreign producer can
// put one on the wire, and that is the shape the decode-side guard exists for.
func completionEnvelopeWithRawDecision(t *testing.T, event *agentic.LoopCompletedEvent, decision map[string]any) []byte {
	t.Helper()
	valid := *event
	valid.Decision = &agentic.CoordinatorDecision{Action: agentic.DecideActionRespondDirect, Reason: "placeholder"}
	data := completionPayload(t, &valid)

	var envelope map[string]any
	require.NoError(t, json.Unmarshal(data, &envelope))
	payload, ok := envelope["payload"].(map[string]any)
	require.True(t, ok, "envelope must carry an object payload")
	payload["decision"] = decision
	envelope["payload"] = payload
	spliced, err := json.Marshal(envelope)
	require.NoError(t, err)
	return spliced
}

func TestSettleAgentTerminalMalformedPresentDecisionIsRejectedNeverAHandoff(t *testing.T) {
	// A present decision with an empty field must be Termed by the fail-closed
	// normalizer (C4), never silently classified as a handoff or as route-less.
	for _, tc := range []struct {
		name     string
		decision map[string]any
	}{
		{name: "empty action", decision: map[string]any{"action": "", "reason": "answered"}},
		{name: "empty reason", decision: map[string]any{"action": agentic.DecideActionRespondDirect, "reason": ""}},
		{name: "absent fields", decision: map[string]any{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := terminalTestComponent(t)
			loader := newAncestryLoader(agentic.LoopEntity{
				ID: "35f24ee8-8bb9-4dc4-bc8e-000000000027", TaskID: "task-35f24ee8-8bb9-4dc4-bc8e-000000000027", State: agentic.LoopStateComplete, MaxIterations: 3,
				ChannelType: "http", ChannelID: "origin-1",
			})
			c.loadPersistedLoopFn = loader.load
			get := captureResponse(t, c)

			data := completionEnvelopeWithRawDecision(t,
				decideCompletion("35f24ee8-8bb9-4dc4-bc8e-000000000027", agentic.DecideActionRespondDirect, "answered"), tc.decision)

			before := terminalReasonSnapshot(c)
			err := c.settleAgentTerminal(context.Background(), data)
			require.Error(t, err)
			require.True(t, isPermanentTerminal(err), "a malformed decision is Termed, not retried")

			_, _, count := get()
			require.Zero(t, count)
			require.Empty(t, loader.sequence, "rejection happens at decode, before any routing read")
			requireOneTerminalReason(t, c, string(agentterminal.ReasonPayload), before)
			require.Equal(t, before["handoff_settled"], terminalReasonValue(c, "handoff_settled"),
				"a malformed decision is never a handoff")
			require.Equal(t, before["route_less_settled"], terminalReasonValue(c, "route_less_settled"),
				"a malformed decision is never route-less")
		})
	}
}
