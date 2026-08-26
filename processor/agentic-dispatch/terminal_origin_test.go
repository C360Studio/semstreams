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
	reg := payloadregistry.NewWithSubset(t, agentic.RegisterPayloads, RegisterPayloads)
	return &Component{
		config:      DefaultConfig(),
		logger:      logger,
		loopTracker: NewLoopTrackerWithLogger(logger),
		metrics:     getMetrics(metric.NewMetricsRegistry()),
		decoder:     message.NewDecoder(reg),
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
		ID: "root-loop", TaskID: "task-root-loop", State: agentic.LoopStateComplete,
		ChannelType: "http", ChannelID: "origin-1", UserID: "user-1",
	})
	c.loadPersistedLoopFn = loader.load
	get := captureResponse(t, c)

	before := terminalReasonSnapshot(c)
	require.NoError(t, c.settleAgentTerminal(context.Background(),
		completionPayload(t, decideCompletion("root-loop", "autoresearch", "hand off to the chain"))))

	_, _, count := get()
	require.Zero(t, count, "a routed handoff decision must publish nothing")
	requireOneTerminalReason(t, c, "handoff_settled", before)
}

func TestSettleAgentTerminalHandoffDecisionOnRouteLessLoopPublishesNothing(t *testing.T) {
	c := terminalTestComponent(t)
	loader := newAncestryLoader(
		agentic.LoopEntity{
			ID: "mid-loop", TaskID: "task-mid-loop", State: agentic.LoopStateComplete,
			ParentLoopID: "root-loop", RunID: "root-loop",
		},
		agentic.LoopEntity{
			ID: "root-loop", State: agentic.LoopStateComplete,
			ChannelType: "http", ChannelID: "origin-1",
		},
	)
	c.loadPersistedLoopFn = loader.load
	get := captureResponse(t, c)

	before := terminalReasonSnapshot(c)
	require.NoError(t, c.settleAgentTerminal(context.Background(),
		completionPayload(t, decideCompletion("mid-loop", "synthesize", "enough evidence"))))

	_, _, count := get()
	require.Zero(t, count, "a handoff decision never borrows an origin")
	require.Equal(t, []string{"mid-loop"}, loader.sequence, "a handoff must not walk ancestry")
	requireOneTerminalReason(t, c, "handoff_settled", before)
}

func TestSettleAgentTerminalRespondDirectOnRoutedLoopPublishesResultWithReason(t *testing.T) {
	c := terminalTestComponent(t)
	loader := newAncestryLoader(agentic.LoopEntity{
		ID: "front-door", TaskID: "task-front-door", State: agentic.LoopStateComplete,
		ChannelType: "http", ChannelID: "origin-1", UserID: "user-1",
	})
	c.loadPersistedLoopFn = loader.load
	get := captureResponse(t, c)

	before := terminalReasonSnapshot(c)
	require.NoError(t, c.settleAgentTerminal(context.Background(),
		completionPayload(t, decideCompletion("front-door", agentic.DecideActionRespondDirect, "Optimized the flight plan."))))

	response, _, count := get()
	require.Equal(t, 1, count)
	require.Equal(t, agentic.ResponseTypeResult, response.Type)
	require.Equal(t, "Optimized the flight plan.", response.Content, "content is the decision reason, not the decision JSON")
	require.Equal(t, "http", response.ChannelType)
	require.Equal(t, "origin-1", response.ChannelID)
	require.Equal(t, "user-1", response.UserID)
	require.Equal(t, "front-door", response.InReplyTo)
	require.Equal(t, []string{"front-door"}, loader.sequence, "an own-routed reply resolves no ancestry")
	requireOneTerminalReason(t, c, "response_settled", before)
}

func TestSettleAgentTerminalAskUserDecisionPublishesPromptToOrigin(t *testing.T) {
	c := terminalTestComponent(t)
	loader := newAncestryLoader(
		agentic.LoopEntity{
			ID: "wakeup-loop", TaskID: "task-wakeup-loop", State: agentic.LoopStateComplete,
			ParentLoopID: "root-loop",
		},
		agentic.LoopEntity{
			ID: "root-loop", State: agentic.LoopStateComplete,
			ChannelType: "slack", ChannelID: "C123", UserID: "user-7",
		},
	)
	c.loadPersistedLoopFn = loader.load
	get := captureResponse(t, c)

	before := terminalReasonSnapshot(c)
	require.NoError(t, c.settleAgentTerminal(context.Background(),
		completionPayload(t, decideCompletion("wakeup-loop", agentic.DecideActionAskUser, "Which airframe?"))))

	response, _, count := get()
	require.Equal(t, 1, count)
	require.Equal(t, agentic.ResponseTypePrompt, response.Type)
	require.Equal(t, "Which airframe?", response.Content)
	require.Equal(t, "slack", response.ChannelType)
	require.Equal(t, "C123", response.ChannelID)
	require.Equal(t, "wakeup-loop", response.InReplyTo, "the reply re-enters at the deciding loop")
	requireOneTerminalReason(t, c, "response_settled", before)
}

func TestSettleAgentTerminalUserFacingDecisionResolvesOriginByAncestry(t *testing.T) {
	c := terminalTestComponent(t)
	// Unthreaded chain: no RunID anywhere, three deep, tracker empty.
	loader := newAncestryLoader(
		agentic.LoopEntity{ID: "terminal-loop", TaskID: "task-terminal-loop", State: agentic.LoopStateComplete, ParentLoopID: "mid-loop"},
		agentic.LoopEntity{ID: "mid-loop", State: agentic.LoopStateComplete, ParentLoopID: "root-loop"},
		agentic.LoopEntity{ID: "root-loop", State: agentic.LoopStateComplete, ChannelType: "http", ChannelID: "origin-1", UserID: "user-1"},
	)
	c.loadPersistedLoopFn = loader.load
	get := captureResponse(t, c)

	before := terminalReasonSnapshot(c)
	require.NoError(t, c.settleAgentTerminal(context.Background(),
		completionPayload(t, decideCompletion("terminal-loop", agentic.DecideActionRespondDirect, "Here is the answer."))))

	response, _, count := get()
	require.Equal(t, 1, count)
	require.Equal(t, agentic.ResponseTypeResult, response.Type)
	require.Equal(t, "Here is the answer.", response.Content)
	require.Equal(t, "http", response.ChannelType)
	require.Equal(t, "origin-1", response.ChannelID)
	require.Equal(t, "user-1", response.UserID)
	require.Equal(t, "terminal-loop", response.InReplyTo)
	require.Equal(t, []string{"terminal-loop", "mid-loop", "root-loop"}, loader.sequence)
	require.Nil(t, c.loopTracker.Get("terminal-loop"), "ancestry is never resolved from the process tracker")
	requireOneTerminalReason(t, c, "response_settled", before)
}

func TestSettleAgentTerminalMissingParentFallsBackToRunID(t *testing.T) {
	t.Run("parent_key_absent", func(t *testing.T) {
		c := terminalTestComponent(t)
		loader := newAncestryLoader(
			agentic.LoopEntity{
				ID: "terminal-loop", TaskID: "task-terminal-loop", State: agentic.LoopStateComplete,
				ParentLoopID: "evicted-parent", RunID: "root-loop",
			},
			agentic.LoopEntity{ID: "root-loop", State: agentic.LoopStateComplete, ChannelType: "http", ChannelID: "origin-1"},
		)
		c.loadPersistedLoopFn = loader.load
		get := captureResponse(t, c)

		before := terminalReasonSnapshot(c)
		require.NoError(t, c.settleAgentTerminal(context.Background(),
			completionPayload(t, decideCompletion("terminal-loop", agentic.DecideActionRespondDirect, "answered"))))

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
				ID: "terminal-loop", TaskID: "task-terminal-loop", State: agentic.LoopStateComplete,
				RunID: "root-loop",
			},
			agentic.LoopEntity{ID: "root-loop", State: agentic.LoopStateComplete, ChannelType: "http", ChannelID: "origin-1"},
		)
		c.loadPersistedLoopFn = loader.load
		get := captureResponse(t, c)

		before := terminalReasonSnapshot(c)
		require.NoError(t, c.settleAgentTerminal(context.Background(),
			completionPayload(t, decideCompletion("terminal-loop", agentic.DecideActionRespondDirect, "answered"))))

		response, _, count := get()
		require.Equal(t, 1, count, "a severed parent link must not settle while a durable RunID is in hand")
		require.Equal(t, "origin-1", response.ChannelID)
		requireOneTerminalReason(t, c, "response_settled", before)
	})

	t.Run("typed_lookup_precedes_parent_walk", func(t *testing.T) {
		c := terminalTestComponent(t)
		loader := newAncestryLoader(
			agentic.LoopEntity{
				ID: "terminal-loop", TaskID: "task-terminal-loop", State: agentic.LoopStateComplete,
				ParentLoopID: "mid-loop", RunID: "root-loop",
			},
			agentic.LoopEntity{ID: "mid-loop", State: agentic.LoopStateComplete, ParentLoopID: "root-loop"},
			agentic.LoopEntity{ID: "root-loop", State: agentic.LoopStateComplete, ChannelType: "http", ChannelID: "origin-1"},
		)
		c.loadPersistedLoopFn = loader.load
		get := captureResponse(t, c)

		require.NoError(t, c.settleAgentTerminal(context.Background(),
			completionPayload(t, decideCompletion("terminal-loop", agentic.DecideActionRespondDirect, "answered"))))

		_, _, count := get()
		require.Equal(t, 1, count)
		require.Equal(t, []string{"terminal-loop", "root-loop"}, loader.sequence,
			"the run anchor is read first; the parent key is never read")
	})

	t.Run("intermediate_run_anchor_after_absent_parent", func(t *testing.T) {
		// The C1 retry inside the walk: the terminal carries no run anchor,
		// but an intermediate record does, and its parent key is gone.
		c := terminalTestComponent(t)
		loader := newAncestryLoader(
			agentic.LoopEntity{ID: "terminal-loop", TaskID: "task-terminal-loop", State: agentic.LoopStateComplete, ParentLoopID: "mid-loop"},
			agentic.LoopEntity{ID: "mid-loop", State: agentic.LoopStateComplete, ParentLoopID: "evicted-parent", RunID: "root-loop"},
			agentic.LoopEntity{ID: "root-loop", State: agentic.LoopStateComplete, ChannelType: "http", ChannelID: "origin-1"},
		)
		c.loadPersistedLoopFn = loader.load
		get := captureResponse(t, c)

		before := terminalReasonSnapshot(c)
		require.NoError(t, c.settleAgentTerminal(context.Background(),
			completionPayload(t, decideCompletion("terminal-loop", agentic.DecideActionRespondDirect, "answered"))))

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
			ID: "phase-loop", TaskID: "task-phase-loop", State: agentic.LoopStateComplete,
			ParentLoopID: "root-loop", RunID: "root-loop",
		},
		agentic.LoopEntity{ID: "root-loop", State: agentic.LoopStateComplete, ChannelType: "http", ChannelID: "origin-1"},
	)
	c.loadPersistedLoopFn = loader.load
	get := captureResponse(t, c)

	before := terminalReasonSnapshot(c)
	require.NoError(t, c.settleAgentTerminal(context.Background(), completionPayload(t, &agentic.LoopCompletedEvent{
		LoopID: "phase-loop", TaskID: "task-phase-loop", Outcome: agentic.OutcomeSuccess,
		Result: "baseline gathered", CompletedAt: time.Unix(1_700_100_100, 0).UTC(),
	})))

	_, _, count := get()
	require.Zero(t, count, "an internal phase completion never reaches the user channel")
	require.Equal(t, []string{"phase-loop"}, loader.sequence, "a terminal without a decision resolves no origin")
	requireOneTerminalReason(t, c, "route_less_settled", before)
}

func TestSettleAgentTerminalReplyDecisionWithRouteLessRootSettlesRouteLess(t *testing.T) {
	c := terminalTestComponent(t)
	// A bus-submitted root: no parent, no run anchor, no route. There was no
	// origin — nothing pointed at something unobservable.
	loader := newAncestryLoader(agentic.LoopEntity{
		ID: "bus-root", TaskID: "task-bus-root", State: agentic.LoopStateComplete,
	})
	c.loadPersistedLoopFn = loader.load
	get := captureResponse(t, c)

	before := terminalReasonSnapshot(c)
	require.NoError(t, c.settleAgentTerminal(context.Background(),
		completionPayload(t, decideCompletion("bus-root", agentic.DecideActionRespondDirect, "answered nobody"))))

	_, _, count := get()
	require.Zero(t, count)
	requireOneTerminalReason(t, c, "route_less_settled", before)
}

func TestSettleAgentTerminalUserFacingDecisionKeepsStableIdentityOnRedelivery(t *testing.T) {
	c := terminalTestComponent(t)
	loader := newAncestryLoader(
		agentic.LoopEntity{ID: "terminal-loop", TaskID: "task-terminal-loop", State: agentic.LoopStateComplete, ParentLoopID: "root-loop"},
		agentic.LoopEntity{ID: "root-loop", State: agentic.LoopStateComplete, ChannelType: "http", ChannelID: "origin-1"},
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

	data := completionPayload(t, decideCompletion("terminal-loop", agentic.DecideActionRespondDirect, "answered"))
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
			agentic.LoopEntity{ID: "a-loop", TaskID: "task-a-loop", State: agentic.LoopStateComplete, ParentLoopID: "b-loop"},
			agentic.LoopEntity{ID: "b-loop", State: agentic.LoopStateComplete, ParentLoopID: "a-loop"},
		)
		c.loadPersistedLoopFn = loader.load
		get := captureResponse(t, c)

		before := terminalReasonSnapshot(c)
		require.NoError(t, c.settleAgentTerminal(context.Background(),
			completionPayload(t, decideCompletion("a-loop", agentic.DecideActionRespondDirect, "answered"))))

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
				ID:           fmt.Sprintf("loop-%02d", i),
				TaskID:       fmt.Sprintf("task-loop-%02d", i),
				State:        agentic.LoopStateComplete,
				ParentLoopID: fmt.Sprintf("loop-%02d", i+1),
			})
		}
		loader := newAncestryLoader(records...)
		c.loadPersistedLoopFn = loader.load
		get := captureResponse(t, c)

		before := terminalReasonSnapshot(c)
		require.NoError(t, c.settleAgentTerminal(context.Background(),
			completionPayload(t, decideCompletion("loop-00", agentic.DecideActionRespondDirect, "answered"))))

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
			ID: "terminal-loop", TaskID: "task-terminal-loop", State: agentic.LoopStateComplete,
			ParentLoopID: "evicted-parent", RunID: "evicted-root",
		})
		c.loadPersistedLoopFn = loader.load
		get := captureResponse(t, c)

		before := terminalReasonSnapshot(c)
		require.NoError(t, c.settleAgentTerminal(context.Background(),
			completionPayload(t, decideCompletion("terminal-loop", agentic.DecideActionRespondDirect, "answered"))))

		_, _, count := get()
		require.Zero(t, count)
		require.Contains(t, loader.sequence, "evicted-root", "the run anchor must be tried")
		require.Contains(t, loader.sequence, "evicted-parent", "the parent chain must be tried")
		requireOneTerminalReason(t, c, "origin_unresolvable", before)
		require.Contains(t, logs.String(), "evicted-parent", "the warning names the absent loop")
		require.Contains(t, logs.String(), "evicted-root", "the warning names the run anchor")
	})

	t.Run("absent_parent_and_no_run_anchor", func(t *testing.T) {
		c, logs := terminalTestComponentWithLog(t)
		loader := newAncestryLoader(agentic.LoopEntity{
			ID: "terminal-loop", TaskID: "task-terminal-loop", State: agentic.LoopStateComplete,
			ParentLoopID: "evicted-parent",
		})
		c.loadPersistedLoopFn = loader.load
		get := captureResponse(t, c)

		before := terminalReasonSnapshot(c)
		require.NoError(t, c.settleAgentTerminal(context.Background(),
			completionPayload(t, decideCompletion("terminal-loop", agentic.DecideActionRespondDirect, "answered"))))

		_, _, count := get()
		require.Zero(t, count)
		requireOneTerminalReason(t, c, "origin_unresolvable", before)
		require.Contains(t, logs.String(), "evicted-parent", "the warning names the absent loop")
		require.Contains(t, logs.String(), "none", "the warning states there was no run anchor")
	})
}

func TestResolveOriginRouteTransientReadDelaysNak(t *testing.T) {
	c := terminalTestComponent(t)
	loader := newAncestryLoader(agentic.LoopEntity{
		ID: "terminal-loop", TaskID: "task-terminal-loop", State: agentic.LoopStateComplete, ParentLoopID: "root-loop",
	})
	loader.errors["root-loop"] = errors.New("kv unavailable")
	c.loadPersistedLoopFn = loader.load
	get := captureResponse(t, c)

	before := terminalReasonSnapshot(c)
	err := c.settleAgentTerminal(context.Background(),
		completionPayload(t, decideCompletion("terminal-loop", agentic.DecideActionRespondDirect, "answered")))
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
			ID: "terminal-loop", TaskID: "task-terminal-loop", State: agentic.LoopStateComplete, ParentLoopID: "root-loop",
		})
		loader.errors["root-loop"] = permanentTerminal("malformed AGENT_LOOPS/root-loop")
		c.loadPersistedLoopFn = loader.load

		before := terminalReasonSnapshot(c)
		err := c.settleAgentTerminal(context.Background(),
			completionPayload(t, decideCompletion("terminal-loop", agentic.DecideActionRespondDirect, "answered")))
		require.Error(t, err)
		require.True(t, isPermanentTerminal(err))
		requireOneTerminalReason(t, c, "routing_malformed", before)
	})

	t.Run("partial_route_on_ancestor", func(t *testing.T) {
		c := terminalTestComponent(t)
		loader := newAncestryLoader(
			agentic.LoopEntity{ID: "terminal-loop", TaskID: "task-terminal-loop", State: agentic.LoopStateComplete, ParentLoopID: "root-loop"},
			agentic.LoopEntity{ID: "root-loop", State: agentic.LoopStateComplete, ChannelType: "http"},
		)
		c.loadPersistedLoopFn = loader.load

		before := terminalReasonSnapshot(c)
		err := c.settleAgentTerminal(context.Background(),
			completionPayload(t, decideCompletion("terminal-loop", agentic.DecideActionRespondDirect, "answered")))
		require.Error(t, err)
		require.True(t, isPermanentTerminal(err))
		requireOneTerminalReason(t, c, "routing_malformed", before)
	})
}
