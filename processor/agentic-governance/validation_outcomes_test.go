package agenticgovernance

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// MockFilter covers fixed policy results; this adapter exercises dependency
// failure and active cancellation through the same production Filter interface.
type validationFilterFunc func(context.Context, *Message) (*FilterResult, error)

func (validationFilterFunc) Name() string { return "validation-test" }
func (f validationFilterFunc) Process(ctx context.Context, msg *Message) (*FilterResult, error) {
	return f(ctx, msg)
}

// spec: agentic-governance / Governance validation settles after its declared consequence
func TestGovernanceProductionCallbacksDenyAndFilterFailure(t *testing.T) {
	for _, row := range []struct {
		port    string
		msgType MessageType
	}{
		{port: "task_validation", msgType: MessageTypeTask},
		{port: "request_validation", msgType: MessageTypeRequest},
		{port: "response_validation", msgType: MessageTypeResponse},
	} {
		t.Run(row.port, func(t *testing.T) {
			var logs bytes.Buffer
			discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{
				NATSClient: &natsclient.Client{}, Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
			})
			require.NoError(t, err)
			c := discoverable.(*Component)
			c.running = true
			c.waitForStreamInput = func(context.Context, string) error { return nil }
			callbacks := make(map[string]func(context.Context, jetstream.Msg))
			handles := make(map[string]*governanceSettlementHandle)
			c.consumeStream = func(_ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
				handle := &governanceSettlementHandle{closed: make(chan struct{})}
				callbacks[owner.Port] = callback
				handles[owner.Port] = handle
				return handle, nil
			}
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(func() {
				cancel()
				for _, binding := range c.consumers {
					<-binding.observerDone
				}
			})
			require.NoError(t, c.setupInputConsumers(ctx))
			callback, ok := callbacks[row.port]
			require.True(t, ok, "production setup did not bind %s", row.port)

			t.Run("deny_despite_audit_failure", func(t *testing.T) {
				c.chain.Filters = []Filter{&MockFilter{
					name: "deny", allowed: false, violation: &Violation{
						ID: "denied-1", FilterName: "deny", UserID: "user-1",
						Severity: SeverityLow, Action: ViolationActionBlocked,
					},
				}}
				msg := &governanceSettlementMsg{data: []byte(`{"id":"denied-1","content":{"text":"blocked"}}`)}
				callback(ctx, msg)
				require.Equal(t, int32(1), msg.acks.Load())
				require.Zero(t, msg.naks.Load()+msg.terms.Load())
				require.Contains(t, logs.String(), "Failed to store violation")
				require.Contains(t, logs.String(), "Failed to handle violation")
				require.Contains(t, logs.String(), "publish violation")
				require.Contains(t, logs.String(), "Message blocked by governance")
				require.Equal(t, int64(1), atomic.LoadInt64(&c.violationsCount))
				// The disconnected client cannot forward. Reaching the required
				// validated publication path would add a business error and NAK.
				require.Zero(t, atomic.LoadInt64(&c.errors))
			})

			t.Run("transient_filter_failure", func(t *testing.T) {
				var seenType MessageType
				c.chain.Filters = []Filter{validationFilterFunc(func(_ context.Context, msg *Message) (*FilterResult, error) {
					seenType = msg.Type
					return nil, errs.WrapTransient(errors.New("dependency unavailable"), "test", "Process", "filter dependency")
				})}
				msg := &governanceSettlementMsg{data: []byte(`{"id":"retry-1"}`)}
				callback(ctx, msg)
				require.Equal(t, row.msgType, seenType)
				require.Equal(t, int32(1), msg.naks.Load())
				require.Zero(t, msg.acks.Load()+msg.terms.Load())
				require.Contains(t, logs.String(), "dependency unavailable")
			})
			health := c.Health()
			require.True(t, health.Healthy)
			require.Empty(t, health.LastError)
			for owner, handle := range handles {
				require.Zero(t, handle.drains.Load(), "validation outcome drained owner %s", owner)
			}
		})
	}
}

// spec: agentic-governance / Governance validation settles after its declared consequence
func TestGovernanceProductionCallbacksJoinCancelledFilter(t *testing.T) {
	for _, port := range []string{"task_validation", "request_validation", "response_validation"} {
		t.Run(port, func(t *testing.T) {
			discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{NATSClient: &natsclient.Client{}})
			require.NoError(t, err)
			c := discoverable.(*Component)
			c.running = true
			c.waitForStreamInput = func(context.Context, string) error { return nil }
			callbacks := make(map[string]func(context.Context, jetstream.Msg))
			handles := make(map[string]*governanceSettlementHandle)
			c.consumeStream = func(_ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
				handle := &governanceSettlementHandle{closed: make(chan struct{})}
				callbacks[owner.Port] = callback
				handles[owner.Port] = handle
				return handle, nil
			}
			ownerCtx, cancelOwner := context.WithCancel(t.Context())
			t.Cleanup(func() {
				cancelOwner()
				for _, binding := range c.consumers {
					<-binding.observerDone
				}
			})
			require.NoError(t, c.setupInputConsumers(ownerCtx))
			callback, ok := callbacks[port]
			require.True(t, ok, "production setup did not bind %s", port)
			entered := make(chan struct{})
			cancelObserved := make(chan error, 1)
			release := make(chan struct{})
			joined := make(chan int32, 1)
			callbackDone := make(chan struct{})
			msg := &governanceSettlementMsg{data: []byte(`{"id":"cancel-1"}`)}
			c.chain.Filters = []Filter{validationFilterFunc(func(ctx context.Context, _ *Message) (*FilterResult, error) {
				defer func() { joined <- msg.acks.Load() + msg.naks.Load() + msg.terms.Load() }()
				close(entered)
				<-ctx.Done()
				cancelObserved <- ctx.Err()
				<-release
				return nil, ctx.Err()
			})}
			workCtx, cancelWork := context.WithCancel(ownerCtx)
			releaseFilter := sync.OnceFunc(func() { close(release) })
			go func() {
				defer close(callbackDone)
				callback(workCtx, msg)
			}()
			t.Cleanup(func() {
				cancelWork()
				releaseFilter()
				select {
				case <-callbackDone:
				case <-time.After(time.Second):
					t.Error("cancelled validation callback did not join during cleanup")
				}
			})
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("production callback did not enter the filter")
			}
			cancelWork()
			select {
			case observed := <-cancelObserved:
				require.ErrorIs(t, observed, context.Canceled)
			case <-time.After(time.Second):
				t.Fatal("active filter did not observe callback context cancellation")
			}
			select {
			case <-callbackDone:
				t.Fatal("callback returned before active filter work joined")
			default:
			}
			require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load(), "active filter work must precede settlement")
			releaseFilter()
			select {
			case <-callbackDone:
			case <-time.After(time.Second):
				t.Fatal("callback did not return after cancelled filter work joined")
			}
			select {
			case settledBeforeJoin := <-joined:
				require.Zero(t, settledBeforeJoin, "settlement preceded the filter's return")
			default:
				t.Fatal("callback returned before filter work joined")
			}
			require.Equal(t, int32(1), msg.naks.Load())
			require.Zero(t, msg.acks.Load()+msg.terms.Load())
			require.True(t, c.Health().Healthy)
			require.Empty(t, c.Health().LastError)
			for owner, handle := range handles {
				require.Zero(t, handle.drains.Load(), "cancelled filter drained owner %s", owner)
			}
		})
	}
}
