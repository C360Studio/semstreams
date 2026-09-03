package agenticloop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type loopPolicyHandle struct {
	drains atomic.Int32
	closed chan struct{}
}

func (*loopPolicyHandle) Stop()                     {}
func (h *loopPolicyHandle) Drain()                  { h.drains.Add(1) }
func (h *loopPolicyHandle) Closed() <-chan struct{} { return h.closed }

func TestAgenticLoopConsumerPolicyOwnsMaxAckPending(t *testing.T) {
	for _, tt := range []struct {
		portName string
		fixed    int
	}{
		{portName: "agent.task", fixed: 1},
		{portName: "agent.response", fixed: 1},
		{portName: "tool.result", fixed: 1},
		{portName: "agent.signal", fixed: 10},
	} {
		for _, requested := range []int{0, 4, -1} {
			t.Run(fmt.Sprintf("%s_requested_%d", tt.portName, requested), func(t *testing.T) {
				port, err := (component.PortDefinition{
					Name: tt.portName,
					Config: component.JetStreamPort{
						StreamName: "AGENT", Subjects: []string{tt.portName + ".*"}, MaxAckPending: requested,
					},
				}).Resolve(component.DirectionInput)
				if err != nil {
					t.Fatal(err)
				}

				cfg, fixed, err := agenticLoopConsumerPolicy(port)
				if fixed != tt.fixed {
					t.Fatalf("fixed = %d, want %d", fixed, tt.fixed)
				}
				if requested == 0 {
					if err != nil || cfg.MaxAckPending != 0 {
						t.Fatalf("policy = %+v, error = %v; want fixed component policy accepted", cfg, err)
					}
					return
				}
				if !errs.IsInvalid(err) || !strings.Contains(err.Error(), tt.portName) ||
					!strings.Contains(err.Error(), "max_ack_pending") ||
					!strings.Contains(err.Error(), fmt.Sprintf("at %d", tt.fixed)) {
					t.Fatalf("error = %v, want invalid error naming port, field, and fixed value", err)
				}
			})
		}
	}
}

// spec: agentic-loop / Long-running loop heartbeat policy is valid before acquisition
func TestLongRunningLoopHeartbeatPolicyValidatedBeforeConsumerAcquisition(t *testing.T) {
	for _, portName := range []string{"agent.task", "agent.response", "tool.result"} {
		for _, test := range []struct {
			name      string
			heartbeat string
			wantErr   bool
		}{
			{name: "legacy invalid", heartbeat: "60s", wantErr: true},
			{name: "default valid", heartbeat: "15s"},
		} {
			t.Run(portName+"/"+test.name, func(t *testing.T) {
				port, err := (component.PortDefinition{
					Name:     portName,
					Config:   component.JetStreamPort{StreamName: "AGENT", Subjects: []string{portName + ".>"}},
					Required: true,
				}).Resolve(component.DirectionInput)
				require.NoError(t, err)

				config := DefaultConfig()
				config.Consumer.HeartbeatInterval = test.heartbeat
				var acquired atomic.Int32
				var acquiredConfig natsclient.StreamConsumerConfig
				c := &Component{
					config:             config,
					logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
					waitForStreamInput: func(context.Context, string) error { return nil },
					consumeStream: func(_ context.Context, _ context.Context, _ natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig, _ func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
						acquired.Add(1)
						acquiredConfig = cfg
						return &loopPolicyHandle{closed: make(chan struct{})}, nil
					},
				}

				err = c.setupConsumer(
					t.Context(), t.Context(), port, portName+".>", func(context.Context, []byte) error { return nil },
				)
				if test.wantErr {
					require.Error(t, err)
					require.True(t, errs.IsInvalid(err))
					require.Contains(t, err.Error(), "agentic-loop")
					require.Contains(t, err.Error(), portName)
					require.Contains(t, err.Error(), "1m0s")
					require.Contains(t, err.Error(), "15s")
					require.Contains(t, err.Error(), "30s")
					require.Zero(t, acquired.Load(), "invalid policy must fail before allocation")
					return
				}
				require.NoError(t, err)
				require.Equal(t, int32(1), acquired.Load(), "valid policy must reach allocation")
				require.Equal(t, []time.Duration{30 * time.Second, 2 * time.Minute}, acquiredConfig.BackOff)
			})
		}
	}
}

// spec: agentic-loop / Long-running loop heartbeat policy is valid before acquisition
func TestLoopDefaultConsumerDeclaresValidHeartbeatPolicy(t *testing.T) {
	consumer := DefaultConsumerConfig()
	require.Equal(t, "90s", consumer.AckWait)
	require.Equal(t, "15s", consumer.HeartbeatInterval)
	require.Equal(t, 15*time.Second, consumer.ParsedHeartbeatInterval())
}

// spec: agentic-loop / Long-running loop heartbeat policy is valid before acquisition
func TestShippedLoopFixturesResolveValidHeartbeatPolicy(t *testing.T) {
	for _, path := range []string{
		"configs/agentic.json",
		"configs/examples/research-graph-pipeline.json",
		"configs/flows/crud-tools-test.json",
		"configs/flows/deep-research-test.json",
		"configs/flows/deep-research.json",
		"configs/flows/lesson-example.json",
		"configs/flows/ops-agent-test.json",
		"configs/flows/ops-agent.json",
		"configs/research-graph-e2e.json",
	} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			encoded, err := os.ReadFile(filepath.Join("..", "..", path))
			require.NoError(t, err)
			var assembly struct {
				Components map[string]struct {
					Config json.RawMessage `json:"config"`
				} `json:"components"`
			}
			require.NoError(t, json.Unmarshal(encoded, &assembly))
			loop, ok := assembly.Components["agentic-loop"]
			require.True(t, ok, "agentic-loop component missing")

			config, _, _, err := resolveConfig(loop.Config)
			require.NoError(t, err)
			heartbeat := config.Consumer.ParsedHeartbeatInterval()
			require.Positive(t, heartbeat)
			require.LessOrEqual(t, heartbeat, 15*time.Second)
		})
	}
}
