//go:build integration

package agentictools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
)

func TestStartDiscoverySubscriptionFailureIsTransientAndRestartable(t *testing.T) {
	testClient := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(natsclient.TestStreamConfig{
			Name: "AGENT", Subjects: []string{"tool.execute.>", "tool.result.>"},
		}),
	)
	comp := newAtomicStartTestComponent(t, testClient.Client, "discovery-failure")

	nativeConnection := testClient.GetNativeConnection()
	testClient.Client.SetConnection(nil)
	t.Cleanup(func() { testClient.Client.SetConnection(nativeConnection) })

	err := comp.Start(t.Context())
	if err == nil {
		t.Fatal("Start() succeeded without a discovery subscription")
	}
	if !errs.IsTransient(err) {
		t.Errorf("Start() error class = %v, want transient", err)
	}
	if !errors.Is(err, natsclient.ErrNotConnected) {
		t.Errorf("Start() error = %v, want errors.Is(..., ErrNotConnected)", err)
	}
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) || classified.Component != "Component" || classified.Operation != "Start" {
		t.Errorf("Start() error context = %#v, want Component/Start", classified)
	}
	if !strings.Contains(err.Error(), "subscribe to tool.list discovery") {
		t.Errorf("Start() error = %v, want discovery-subscribe context", err)
	}
	assertAtomicStartResourcesCleared(t, comp)

	testClient.Client.SetConnection(nativeConnection)
	if err := comp.Start(t.Context()); err != nil {
		t.Fatalf("Start() after restoring the connection: %v", err)
	}
	if !comp.running || comp.toolListSub == nil || len(comp.consumerInfos) != 1 {
		t.Fatalf("successful restart resources: running=%t toolListSub=%v consumers=%v",
			comp.running, comp.toolListSub != nil, comp.consumerInfos)
	}
	if err := comp.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() after successful restart: %v", err)
	}
}

func TestStartLaterConsumerFailureRollsBackLocallyPreservesDurableAndRestarts(t *testing.T) {
	testClient := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithStreams(natsclient.TestStreamConfig{
			Name: "AGENT", Subjects: []string{"tool.execute.>", "tool.result.>"},
		}),
	)
	comp := newAtomicStartTestComponent(t, testClient.Client, "consumer-failure")
	latePort, err := (component.PortDefinition{
		Name: "late.tool.execute",
		Config: component.JetStreamPort{
			StreamName: "LATE_AGENT",
			Subjects:   []string{"late.tool.execute.>"},
		},
		Required: true,
	}).Resolve(component.DirectionInput)
	if err != nil {
		t.Fatal(err)
	}
	comp.inputs = append(comp.inputs, latePort)

	startCtx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	err = comp.Start(startCtx)
	if err == nil {
		t.Fatal("Start() succeeded with a missing later consumer stream")
	}
	if !errs.IsTransient(err) {
		t.Errorf("Start() later-consumer error class = %v, want transient", err)
	}
	assertAtomicStartResourcesCleared(t, comp)

	if _, err := testClient.GetNativeConnection().Request("discovery.tool.list", []byte("{}"), 100*time.Millisecond); !errors.Is(err, nats.ErrNoResponders) && !errors.Is(err, nats.ErrTimeout) {
		t.Errorf("discovery request after rollback error = %v, want no responder", err)
	}
	stream, err := testClient.GetStream(t.Context(), "AGENT")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Consumer(t.Context(), "agentic-tools-tool-execute-all-consumer-failure"); err != nil {
		t.Fatalf("rollback deleted the durable consumer instead of stopping only local consumption: %v", err)
	}

	if _, err := testClient.CreateStream(t.Context(), "LATE_AGENT", []string{"late.tool.execute.>"}); err != nil {
		t.Fatal(err)
	}
	if err := comp.Start(t.Context()); err != nil {
		t.Fatalf("Start() after provisioning the later stream: %v", err)
	}
	if !comp.running || comp.toolListSub == nil || len(comp.consumerInfos) != 2 {
		t.Fatalf("successful restart resources: running=%t toolListSub=%v consumers=%v",
			comp.running, comp.toolListSub != nil, comp.consumerInfos)
	}
	if err := comp.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() after successful restart: %v", err)
	}
}

func newAtomicStartTestComponent(t *testing.T, client *natsclient.Client, suffix string) *Component {
	t.Helper()
	config := DefaultConfig()
	config.ConsumerNameSuffix = suffix
	config.DeleteConsumerOnStop = true
	rawConfig, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	discoverable, err := NewComponent(rawConfig, component.Dependencies{NATSClient: client})
	if err != nil {
		t.Fatal(err)
	}
	comp, ok := discoverable.(*Component)
	if !ok {
		t.Fatalf("NewComponent() returned %T, want *Component", discoverable)
	}
	return comp
}

func assertAtomicStartResourcesCleared(t *testing.T, comp *Component) {
	t.Helper()
	if comp.running || comp.toolListSub != nil || len(comp.consumerInfos) != 0 {
		t.Fatalf("failed Start() leaked resources: running=%t toolListSub=%v consumers=%v",
			comp.running, comp.toolListSub != nil, comp.consumerInfos)
	}
	if comp.Health().Healthy {
		t.Error("failed Start() reported healthy")
	}
}
