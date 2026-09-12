package agenticmodel

import (
	"context"
	"encoding/json"
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

type modelPolicyHandle struct {
	drains atomic.Int32
	closed chan struct{}
}

func (*modelPolicyHandle) Stop()                     {}
func (h *modelPolicyHandle) Drain()                  { h.drains.Add(1) }
func (h *modelPolicyHandle) Closed() <-chan struct{} { return h.closed }

func TestAgenticModelConsumerPolicyOwnsMaxAckPending(t *testing.T) {
	for _, requested := range []int{0, 4, -1} {
		t.Run(testNameForMaxAckPending(requested), func(t *testing.T) {
			port, err := (component.PortDefinition{
				Name: "agent.request",
				Config: component.JetStreamPort{
					StreamName: "AGENT", Subjects: []string{"agent.request.*"}, MaxAckPending: requested,
				},
			}).Resolve(component.DirectionInput)
			if err != nil {
				t.Fatal(err)
			}

			cfg, err := agenticModelConsumerPolicy(port)
			if requested == 0 {
				if err != nil || cfg.MaxAckPending != 0 {
					t.Fatalf("policy = %+v, error = %v; want fixed component policy accepted", cfg, err)
				}
				return
			}
			if !errs.IsInvalid(err) || !strings.Contains(err.Error(), "agent.request") ||
				!strings.Contains(err.Error(), "max_ack_pending") || !strings.Contains(err.Error(), "at 1") {
				t.Fatalf("error = %v, want invalid error naming port, field, and fixed value", err)
			}
		})
	}
}

func testNameForMaxAckPending(value int) string {
	switch value {
	case 0:
		return "omitted"
	case -1:
		return "unlimited_rejected"
	default:
		return "positive_rejected"
	}
}

// spec: agentic-model / Model heartbeat policy is valid before acquisition
func TestModelHeartbeatPolicyValidatedBeforeConsumerAcquisition(t *testing.T) {
	for _, test := range []struct {
		name      string
		heartbeat string
		wantErr   bool
	}{
		{name: "legacy invalid", heartbeat: "90s", wantErr: true},
		{name: "default valid", heartbeat: "60s"},
	} {
		t.Run(test.name, func(t *testing.T) {
			port, err := (component.PortDefinition{
				Name: "agent.request",
				Config: component.JetStreamPort{
					StreamName: "AGENT", Subjects: []string{"agent.request.>"},
					AckWait: "120s", HeartbeatInterval: test.heartbeat,
				},
				Required: true,
			}).Resolve(component.DirectionInput)
			require.NoError(t, err)

			var acquired atomic.Int32
			var acquiredConfig natsclient.StreamConsumerConfig
			c := &Component{
				name: "agentic-model", config: DefaultConfig(),
				logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
				waitForStreamInput: func(context.Context, string) error { return nil },
				consumeStream: func(_ context.Context, _ natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig, _ func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
					acquired.Add(1)
					acquiredConfig = cfg
					return &modelPolicyHandle{closed: make(chan struct{})}, nil
				},
			}

			err = c.setupConsumer(t.Context(), port)
			if test.wantErr {
				require.Error(t, err)
				require.True(t, errs.IsInvalid(err))
				require.Contains(t, err.Error(), "agentic-model")
				require.Contains(t, err.Error(), "agent.request")
				require.Contains(t, err.Error(), "1m30s")
				require.Contains(t, err.Error(), "2m0s")
				require.Contains(t, err.Error(), "1m0s")
				require.Zero(t, acquired.Load(), "invalid policy must fail before allocation")
				return
			}
			require.NoError(t, err)
			require.Equal(t, int32(1), acquired.Load(), "valid policy must reach allocation")
			require.Equal(t, 2*time.Minute, acquiredConfig.AckWait)
			require.Empty(t, acquiredConfig.BackOff)
		})
	}
}

// spec: agentic-model / Model heartbeat policy is valid before acquisition
func TestModelDefaultPortDeclaresValidHeartbeatPolicy(t *testing.T) {
	config := DefaultConfig()
	require.NotNil(t, config.Ports)
	require.Len(t, config.Ports.Inputs, 1)
	port, err := config.Ports.Inputs[0].Resolve(component.DirectionInput)
	require.NoError(t, err)
	consumer, err := component.GetConsumerConfig(port)
	require.NoError(t, err)
	require.Equal(t, 2*time.Minute, consumer.AckWait)
	require.Equal(t, 60*time.Second, consumer.HeartbeatInterval)
}

// spec: agentic-model / Model heartbeat policy is valid before acquisition
func TestShippedModelFixturesResolveValidHeartbeatPolicy(t *testing.T) {
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
			model, ok := assembly.Components["agentic-model"]
			require.True(t, ok, "agentic-model component missing")

			_, inputs, _, err := resolveConfig(model.Config, "TestShippedModelFixturesResolveValidHeartbeatPolicy")
			require.NoError(t, err)
			var requestPort *component.Port
			for i := range inputs {
				if inputs[i].Name == "agent.request" {
					requestPort = &inputs[i]
					break
				}
			}
			require.NotNil(t, requestPort, "agent.request input missing")
			consumer, err := component.GetConsumerConfig(*requestPort)
			require.NoError(t, err)
			require.Equal(t, 2*time.Minute, consumer.AckWait)
			require.Equal(t, time.Minute, consumer.HeartbeatInterval)
		})
	}
}
