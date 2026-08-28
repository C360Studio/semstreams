//go:build integration

package graphingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/nats-io/nats.go/jetstream"
)

var sharedLifecycleNATSClient *natsclient.TestClient

func TestMain(m *testing.M) {
	// Declare the test-only predicates used by projection/ownership contracts.
	// Runtime graph writes require canonical syntax, while authoring surfaces
	// additionally require explicit vocabulary declaration.
	vocabulary.Register("mission.state.phase")
	vocabulary.Register("sensorml.component.is-hosted-by")
	vocabulary.Register("test.anyproducer.hosted-by")
	vocabulary.Register("test.strict.hosted-by")

	streams := []natsclient.TestStreamConfig{
		{Name: "ENTITY", Subjects: []string{"entity.>"}},
	}
	var err error
	sharedLifecycleNATSClient, err = natsclient.NewSharedTestClient(
		natsclient.WithKV(),
		natsclient.WithStreams(streams...),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create shared lifecycle NATS client: %v\n", err)
		os.Exit(1)
	}

	// Run tests
	code := m.Run()

	// Cleanup
	if err := sharedLifecycleNATSClient.Terminate(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to clean up shared lifecycle NATS client: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func getSharedNATSClient(t *testing.T) *natsclient.TestClient {
	if sharedLifecycleNATSClient == nil {
		t.Fatal("shared NATS client not initialized")
	}
	return sharedLifecycleNATSClient
}

// createTestComponentForLifecycle creates a test instance for lifecycle testing.
func createTestComponentForLifecycle() *Component {
	tc := sharedLifecycleNATSClient
	if tc == nil {
		panic("shared NATS client not initialized - run with -tags=integration")
	}

	config := DefaultConfig()
	deps := component.Dependencies{
		NATSClient:      tc.Client,
		PayloadRegistry: mustTestPayloadRegistry(),
		Platform:        component.PlatformMeta{Org: testDeploymentOrg, Platform: testDeploymentPlatform},
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		panic("failed to marshal config: " + err.Error())
	}

	comp, err := CreateGraphIngest(configJSON, deps)
	if err != nil {
		panic("failed to create component: " + err.Error())
	}

	return comp.(*Component)
}

func TestGraphIngest_OneShotLifecycleAgainstNATS(t *testing.T) {
	comp := createTestComponentForLifecycle()
	if err := comp.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := comp.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := comp.Stop(t.Context()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := comp.Stop(t.Context()); err != nil {
		t.Fatalf("repeated completed Stop: %v", err)
	}
	if err := comp.Start(t.Context()); !errors.Is(err, errs.ErrAlreadyStarted) {
		t.Fatalf("Start after terminal Stop error = %v, want ErrAlreadyStarted", err)
	}
}

type graphIngestTrackingConsumeContext struct {
	jetstream.ConsumeContext
	drains atomic.Int32
}

func (c *graphIngestTrackingConsumeContext) Drain() {
	c.drains.Add(1)
	c.ConsumeContext.Drain()
}

type graphIngestObservedContext struct {
	context.Context
	doneOnce sync.Once
	doneSeen chan struct{}
}

func (c *graphIngestObservedContext) Done() <-chan struct{} {
	c.doneOnce.Do(func() { close(c.doneSeen) })
	return c.Context.Done()
}

func TestGraphIngest_FailedStartRollbackOwnsExactConsumerAndPreservesDurable(t *testing.T) {
	config := DefaultConfig()
	mutation := config.Ports.Inputs[1]
	config.Ports.Inputs = []component.PortDefinition{
		{Name: "entity_one", Config: component.JetStreamPort{StreamName: "ENTITY", Subjects: []string{"entity.one"}, DeliverPolicy: "all"}},
		{Name: "entity_two", Config: component.JetStreamPort{StreamName: "ENTITY", Subjects: []string{"entity.two"}, DeliverPolicy: "all"}},
		mutation,
	}
	configJSON, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	created, err := CreateGraphIngest(configJSON, component.Dependencies{NATSClient: getSharedNATSClient(t).Client, PayloadRegistry: newTestPayloadRegistry(t)})
	if err != nil {
		t.Fatalf("CreateGraphIngest: %v", err)
	}
	comp := created.(*Component)
	if err := comp.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	secondEntered := make(chan struct{})
	releaseSecond := make(chan struct{})
	sentinel := errors.New("second consumer acquisition failed")
	var calls atomic.Int32
	var first *graphIngestTrackingConsumeContext
	comp.consumeStream = func(
		ctx context.Context,
		owner natsclient.PortConsumerContext,
		cfg natsclient.StreamConsumerConfig,
		handler func(context.Context, jetstream.Msg),
	) (jetstream.ConsumeContext, error) {
		if calls.Add(1) == 1 {
			handle, consumeErr := comp.natsClient.ConsumeStreamWithConfig(ctx, owner, cfg, handler)
			if consumeErr != nil {
				return nil, consumeErr
			}
			first = &graphIngestTrackingConsumeContext{ConsumeContext: handle}
			return first, nil
		}
		close(secondEntered)
		<-releaseSecond
		return nil, sentinel
	}

	startResult := make(chan error, 1)
	go func() { startResult <- comp.Start(t.Context()) }()
	<-secondEntered
	comp.lifecycleMu.Lock()
	if len(comp.consumers) != 1 || comp.consumers[0].handle != first {
		comp.lifecycleMu.Unlock()
		t.Fatal("first exact handle was not published before second acquisition")
	}
	comp.lifecycleMu.Unlock()

	stopCtx := &graphIngestObservedContext{Context: t.Context(), doneSeen: make(chan struct{})}
	stopResult := make(chan error, 1)
	go func() { stopResult <- comp.Stop(stopCtx) }()
	<-stopCtx.doneSeen
	select {
	case stopErr := <-stopResult:
		t.Fatalf("Stop returned before Start finalized: %v", stopErr)
	default:
	}
	close(releaseSecond)
	if startErr := <-startResult; !errors.Is(startErr, sentinel) {
		t.Fatalf("Start error = %v, want sentinel", startErr)
	}
	if stopErr := <-stopResult; stopErr != nil {
		t.Fatalf("overlapping Stop: %v", stopErr)
	}
	if first == nil || first.drains.Load() != 1 {
		t.Fatalf("first native Drain calls = %v, want 1", first)
	}

	consumerName := "graph-ingest-" + strings.ReplaceAll("entity.one", ".", "-")
	if _, observeErr := comp.observeOutstandingWork(t.Context(), "ENTITY", consumerName); observeErr != nil {
		t.Fatalf("durable consumer was deleted during failed-Start rollback: %v", observeErr)
	}
}
