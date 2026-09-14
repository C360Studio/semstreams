package agenticdispatch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/agentterminal"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func terminalEnvelopeForDispatch(t *testing.T, payload message.Payload) []byte {
	t.Helper()
	data, err := json.Marshal(message.NewBaseMessage(payload.Schema(), payload, "agentic-loop"))
	require.NoError(t, err)
	return data
}

func agentterminalEvent(userID, channelType, channelID string) agentterminal.Event {
	return agentterminal.Event{UserID: userID, ChannelType: channelType, ChannelID: channelID}
}

func terminalTestComponent(t *testing.T) *Component {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	reg := payloadregistry.NewWithSubset(t, agentic.RegisterPayloads)
	return &Component{
		config:  DefaultConfig(),
		logger:  logger,
		metrics: getMetrics(metric.NewMetricsRegistry()),
		decoder: message.NewDecoder(reg),
	}
}

func terminalReasonValue(c *Component, reason string) float64 {
	return testutil.ToFloat64(c.metrics.terminalSettlements.WithLabelValues(reason))
}

func requireOneTerminalReason(t *testing.T, c *Component, want string, before map[string]float64) {
	t.Helper()
	reasons := []string{
		string(agentterminal.ReasonEnvelope), string(agentterminal.ReasonPayload),
		string(agentterminal.ReasonTimestamp), string(agentterminal.ReasonIdentity),
		string(agentterminal.ReasonCollision), "routing_malformed", "routing_read_transient",
		"routing_collision_or_malformed", "terminal_route_unavailable",
		"response_publish_transient", "route_less_settled", "response_settled", "accepted",
		"handoff_settled", "origin_unresolvable",
	}
	var increments int
	for _, reason := range reasons {
		delta := terminalReasonValue(c, reason) - before[reason]
		if delta != 0 {
			require.Equal(t, 1.0, delta, reason)
			require.Equal(t, want, reason)
			increments++
		}
	}
	require.Equal(t, 1, increments, "one fixed terminal disposition per attempt")
}

func terminalReasonSnapshot(c *Component) map[string]float64 {
	reasons := []string{
		string(agentterminal.ReasonEnvelope), string(agentterminal.ReasonPayload),
		string(agentterminal.ReasonTimestamp), string(agentterminal.ReasonIdentity),
		string(agentterminal.ReasonCollision), "routing_malformed", "routing_read_transient",
		"routing_collision_or_malformed", "terminal_route_unavailable",
		"response_publish_transient", "route_less_settled", "response_settled", "accepted",
		"handoff_settled", "origin_unresolvable",
	}
	values := make(map[string]float64, len(reasons))
	for _, reason := range reasons {
		values[reason] = terminalReasonValue(c, reason)
	}
	return values
}

func TestSettleAgentTerminalPublishesStableSuccessWithOptionalUserID(t *testing.T) {
	c := terminalTestComponent(t)
	at := time.Unix(1_700_000_500, 0).UTC()
	persisted := &agentic.LoopEntity{
		ID: "35f24ee8-8bb9-4dc4-bc8e-000000000001", TaskID: "task-1", State: agentic.LoopStateComplete, MaxIterations: 3,
		ChannelType: "http", ChannelID: "session-1",
	}
	before := *persisted
	withPersistedLoops(c, map[string]*agentic.LoopEntity{persisted.ID: persisted})
	var got agentic.UserResponse
	var gotMsgID string
	c.sendTerminalResponseFn = func(_ context.Context, response agentic.UserResponse, msgID string) error {
		got, gotMsgID = response, msgID
		return nil
	}

	data := completionPayload(t, &agentic.LoopCompletedEvent{LoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000001", TaskID: "task-1", Outcome: agentic.OutcomeSuccess, Result: "the result", CompletedAt: at})
	var source struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(data, &source))
	require.NoError(t, c.settleAgentTerminal(context.Background(), data))

	wantID := terminalResponseIDPrefix + source.ID
	require.Equal(t, wantID, got.ResponseID)
	require.Equal(t, wantID, gotMsgID)
	require.Equal(t, agentic.ResponseTypeResult, got.Type)
	require.Equal(t, "the result", got.Content)
	require.Equal(t, "http", got.ChannelType)
	require.Equal(t, "session-1", got.ChannelID)
	require.Empty(t, got.UserID)
	require.Equal(t, at, got.Timestamp)
	require.Equal(t, before, *persisted, "terminal delivery does not mutate authority")
}

func TestSettleAgentTerminalProjectsCancellationFromCompletionLane(t *testing.T) {
	c := terminalTestComponent(t)
	at := time.Unix(1_700_000_600, 0).UTC()
	c.loadPersistedLoopFn = func(context.Context, string) (*agentic.LoopEntity, error) {
		return &agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000002", TaskID: "task-c", State: agentic.LoopStateCancelled, MaxIterations: 3, ChannelType: "http", ChannelID: "session-c"}, nil
	}
	var got agentic.UserResponse
	c.sendTerminalResponseFn = func(_ context.Context, response agentic.UserResponse, _ string) error { got = response; return nil }
	data := terminalEnvelopeForDispatch(t, &agentic.LoopCancelledEvent{LoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000002", TaskID: "task-c", Outcome: agentic.OutcomeCancelled, CancelledAt: at})
	require.NoError(t, c.settleAgentTerminal(context.Background(), data))
	require.Equal(t, agentic.ResponseTypeStatus, got.Type)
	require.Equal(t, "Loop 35f24ee8-8bb9-4dc4-bc8e-000000000002 cancelled.", got.Content)
	require.Equal(t, "http", got.ChannelType)
	require.Equal(t, "session-c", got.ChannelID)
	require.Equal(t, at, got.Timestamp)
}

func TestSettleAgentTerminalProjectsFailureResponse(t *testing.T) {
	c := terminalTestComponent(t)
	at := time.Unix(1_700_000_650, 0).UTC()
	c.loadPersistedLoopFn = func(context.Context, string) (*agentic.LoopEntity, error) {
		return &agentic.LoopEntity{
			ID: "35f24ee8-8bb9-4dc4-bc8e-000000000003", TaskID: "task-f", State: agentic.LoopStateFailed, MaxIterations: 3,
			ChannelType: "http", ChannelID: "session-f",
		}, nil
	}
	var got agentic.UserResponse
	c.sendTerminalResponseFn = func(_ context.Context, response agentic.UserResponse, _ string) error {
		got = response
		return nil
	}

	data := terminalEnvelopeForDispatch(t, &agentic.LoopFailedEvent{
		LoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000003", TaskID: "task-f", Outcome: agentic.OutcomeFailed, Error: "boom", FailedAt: at,
	})
	require.NoError(t, c.settleAgentTerminal(context.Background(), data))
	require.Equal(t, agentic.ResponseTypeError, got.Type)
	require.Equal(t, "Loop 35f24ee8-8bb9-4dc4-bc8e-000000000003 failed: boom", got.Content)
	require.Equal(t, at, got.Timestamp)
	require.Equal(t, "http", got.ChannelType)
	require.Equal(t, "session-f", got.ChannelID)
}

func TestReconcileTerminalRouteFieldWise(t *testing.T) {
	event := agentterminalEvent("terminal-user", "", "channel-id")
	persisted := &agentic.LoopEntity{ChannelType: "slack", UserID: "terminal-user"}
	route, err := reconcileTerminalRoute(event, persisted)
	require.NoError(t, err)
	require.Equal(t, terminalRoute{ChannelType: "slack", ChannelID: "channel-id", UserID: "terminal-user"}, route)

	for _, tc := range []struct {
		name      string
		event     agentterminal.Event
		persisted *agentic.LoopEntity
	}{
		{"channel type conflict", agentterminalEvent("", "slack", "id"), &agentic.LoopEntity{ChannelType: "http", ChannelID: "id"}},
		{"channel id conflict", agentterminalEvent("", "http", "b"), &agentic.LoopEntity{ChannelType: "http", ChannelID: "a"}},
		{"user id conflict", agentterminalEvent("b", "http", "id"), &agentic.LoopEntity{UserID: "a", ChannelType: "http", ChannelID: "id"}},
		{"partial type", agentterminalEvent("", "http", ""), &agentic.LoopEntity{}},
		{"partial id", agentterminalEvent("", "", "id"), &agentic.LoopEntity{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := reconcileTerminalRoute(tc.event, tc.persisted)
			require.Error(t, err)
			require.True(t, isPermanentTerminal(err))
		})
	}
}

func TestSettleAgentTerminalDispositionClasses(t *testing.T) {
	valid := &agentic.LoopCompletedEvent{LoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000004", TaskID: "task-d", Outcome: agentic.OutcomeSuccess, CompletedAt: time.Now()}

	t.Run("transient persisted read", func(t *testing.T) {
		c := terminalTestComponent(t)
		c.loadPersistedLoopFn = func(context.Context, string) (*agentic.LoopEntity, error) {
			return nil, errors.New("temporarily unavailable")
		}
		err := c.settleAgentTerminal(context.Background(), completionPayload(t, valid))
		require.Error(t, err)
		require.False(t, isPermanentTerminal(err))
	})

	t.Run("permanent malformed persisted state", func(t *testing.T) {
		c := terminalTestComponent(t)
		c.loadPersistedLoopFn = func(context.Context, string) (*agentic.LoopEntity, error) { return nil, permanentTerminal("malformed") }
		err := c.settleAgentTerminal(context.Background(), completionPayload(t, valid))
		require.True(t, isPermanentTerminal(err))
	})

	t.Run("route-less settles without response", func(t *testing.T) {
		c := terminalTestComponent(t)
		c.loadPersistedLoopFn = func(context.Context, string) (*agentic.LoopEntity, error) {
			return &agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000004", TaskID: "task-d", State: agentic.LoopStateComplete, MaxIterations: 3}, nil
		}
		called := false
		c.sendTerminalResponseFn = func(context.Context, agentic.UserResponse, string) error { called = true; return nil }
		require.NoError(t, c.settleAgentTerminal(context.Background(), completionPayload(t, valid)))
		require.False(t, called)
	})

	t.Run("transient publish", func(t *testing.T) {
		c := terminalTestComponent(t)
		c.loadPersistedLoopFn = func(context.Context, string) (*agentic.LoopEntity, error) {
			return &agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000004", TaskID: "task-d", State: agentic.LoopStateComplete, MaxIterations: 3, ChannelType: "http", ChannelID: "id"}, nil
		}
		c.sendTerminalResponseFn = func(context.Context, agentic.UserResponse, string) error { return errors.New("no puback") }
		err := c.settleAgentTerminal(context.Background(), completionPayload(t, valid))
		require.Error(t, err)
		require.False(t, isPermanentTerminal(err))
	})
}

func TestHandleTerminalDeliveryDecisionMatrix(t *testing.T) {
	valid := &agentic.LoopCompletedEvent{LoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000005", TaskID: "task-decision", Outcome: agentic.OutcomeSuccess, CompletedAt: time.Now()}

	t.Run("immutable terminal poison", func(t *testing.T) {
		c := terminalTestComponent(t)
		decision, err := c.handleTerminalDelivery(t.Context(), []byte(`{"bad":true}`))
		require.Equal(t, natsclient.DeliveryDecisionTerminate, decision)
		require.True(t, isPermanentTerminal(err))
	})

	for _, tt := range []struct {
		name    string
		readErr error
		reason  string
	}{
		{"own record not found", jetstream.ErrKeyNotFound, "terminal_route_unavailable"},
		{"own record deleted", jetstream.ErrKeyDeleted, "terminal_route_unavailable"},
		{"infrastructure read unavailable", errors.New("read unavailable"), "routing_read_transient"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := terminalTestComponent(t)
			c.loadPersistedLoopFn = func(context.Context, string) (*agentic.LoopEntity, error) {
				return nil, tt.readErr
			}
			c.sendTerminalResponseFn = func(context.Context, agentic.UserResponse, string) error {
				t.Fatal("an unread own record cannot authorize a user response")
				return nil
			}
			before := terminalReasonSnapshot(c)
			decision, err := c.handleTerminalDelivery(t.Context(), completionPayload(t, valid))
			require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
			require.ErrorIs(t, err, tt.readErr)
			requireOneTerminalReason(t, c, tt.reason, before)
		})
	}

	t.Run("unknown publish outcome quarantines", func(t *testing.T) {
		c := terminalTestComponent(t)
		c.loadPersistedLoopFn = func(context.Context, string) (*agentic.LoopEntity, error) {
			return &agentic.LoopEntity{ID: valid.LoopID, TaskID: valid.TaskID, State: agentic.LoopStateComplete, MaxIterations: 3, ChannelType: "http", ChannelID: "id"}, nil
		}
		c.sendTerminalResponseFn = func(context.Context, agentic.UserResponse, string) error { return errors.New("no puback") }
		decision, err := c.handleTerminalDelivery(t.Context(), completionPayload(t, valid))
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
		require.True(t, isUnknownTerminalPublication(err))
	})

	t.Run("durable non-publish completion acks", func(t *testing.T) {
		c := terminalTestComponent(t)
		c.loadPersistedLoopFn = func(context.Context, string) (*agentic.LoopEntity, error) {
			return &agentic.LoopEntity{ID: valid.LoopID, TaskID: valid.TaskID, State: agentic.LoopStateComplete, MaxIterations: 3}, nil
		}
		decision, err := c.handleTerminalDelivery(t.Context(), completionPayload(t, valid))
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)
		require.NoError(t, err)
	})
}

func TestSettleAgentTerminalRecordsExactlyOneFixedDisposition(t *testing.T) {
	valid := &agentic.LoopCompletedEvent{
		LoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000006", TaskID: "task-m", Outcome: agentic.OutcomeSuccess, CompletedAt: time.Now(),
	}
	tests := []struct {
		name string
		want string
		prep func(*Component) []byte
	}{
		{"decode", string(agentterminal.ReasonEnvelope), func(_ *Component) []byte { return []byte(`{"bad":true}`) }},
		{"routing transient", "routing_read_transient", func(c *Component) []byte {
			c.loadPersistedLoopFn = func(context.Context, string) (*agentic.LoopEntity, error) {
				return nil, errors.New("kv unavailable")
			}
			return completionPayload(t, valid)
		}},
		{"routing malformed", "routing_malformed", func(c *Component) []byte {
			c.loadPersistedLoopFn = func(context.Context, string) (*agentic.LoopEntity, error) {
				return nil, permanentTerminal("malformed AGENT_LOOPS value")
			}
			return completionPayload(t, valid)
		}},
		{"routing collision", "routing_collision_or_malformed", func(c *Component) []byte {
			c.loadPersistedLoopFn = func(context.Context, string) (*agentic.LoopEntity, error) {
				return &agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000006", State: agentic.LoopStateComplete, MaxIterations: 3, ChannelType: "http", ChannelID: "persisted"}, nil
			}
			conflicting := *valid
			conflicting.ChannelType = "http"
			conflicting.ChannelID = "event"
			return completionPayload(t, &conflicting)
		}},
		{"route less", "route_less_settled", func(c *Component) []byte {
			c.loadPersistedLoopFn = func(context.Context, string) (*agentic.LoopEntity, error) {
				return &agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000006", State: agentic.LoopStateComplete, MaxIterations: 3}, nil
			}
			return completionPayload(t, valid)
		}},
		{"publish transient", "response_publish_transient", func(c *Component) []byte {
			c.loadPersistedLoopFn = func(context.Context, string) (*agentic.LoopEntity, error) {
				return &agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000006", State: agentic.LoopStateComplete, MaxIterations: 3, ChannelType: "http", ChannelID: "id"}, nil
			}
			c.sendTerminalResponseFn = func(context.Context, agentic.UserResponse, string) error {
				return errors.New("no puback")
			}
			return completionPayload(t, valid)
		}},
		{"settled", "response_settled", func(c *Component) []byte {
			c.loadPersistedLoopFn = func(context.Context, string) (*agentic.LoopEntity, error) {
				return &agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000006", State: agentic.LoopStateComplete, MaxIterations: 3, ChannelType: "http", ChannelID: "id"}, nil
			}
			c.sendTerminalResponseFn = func(context.Context, agentic.UserResponse, string) error { return nil }
			return completionPayload(t, valid)
		}},
		{"handoff", "handoff_settled", func(c *Component) []byte {
			c.loadPersistedLoopFn = func(context.Context, string) (*agentic.LoopEntity, error) {
				return &agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000006", State: agentic.LoopStateComplete, MaxIterations: 3, ChannelType: "http", ChannelID: "id"}, nil
			}
			c.sendTerminalResponseFn = func(context.Context, agentic.UserResponse, string) error { return nil }
			handoff := *valid
			handoff.Decision = &agentic.CoordinatorDecision{Action: "autoresearch", Reason: "hand off"}
			return completionPayload(t, &handoff)
		}},
		{"origin unresolvable", "origin_unresolvable", func(c *Component) []byte {
			c.loadPersistedLoopFn = func(_ context.Context, loopID string) (*agentic.LoopEntity, error) {
				if loopID != "35f24ee8-8bb9-4dc4-bc8e-000000000006" {
					return nil, loopRecordAbsent(loopID)
				}
				return &agentic.LoopEntity{ID: "35f24ee8-8bb9-4dc4-bc8e-000000000006", State: agentic.LoopStateComplete, MaxIterations: 3, ParentLoopID: "35f24ee8-8bb9-4dc4-bc8e-000000000007"}, nil
			}
			c.sendTerminalResponseFn = func(context.Context, agentic.UserResponse, string) error { return nil }
			reply := *valid
			reply.Decision = &agentic.CoordinatorDecision{Action: agentic.DecideActionRespondDirect, Reason: "answered"}
			return completionPayload(t, &reply)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := terminalTestComponent(t)
			data := tt.prep(c)
			before := terminalReasonSnapshot(c)
			_ = c.settleAgentTerminal(context.Background(), data)
			requireOneTerminalReason(t, c, tt.want, before)
		})
	}
}
