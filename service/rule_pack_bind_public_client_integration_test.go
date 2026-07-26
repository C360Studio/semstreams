//go:build integration

package service

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/ownership"
	"github.com/c360studio/semstreams/pkg/projection"
	rule "github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type observingRuleProcessorBinder struct {
	*rule.Processor
	injectionAttempts int
	injected          projection.OwnedReplacer
	startCalls        int
}

func (binder *observingRuleProcessorBinder) SetOwnedReplacer(replacer projection.OwnedReplacer) error {
	binder.injectionAttempts++
	if err := binder.Processor.SetOwnedReplacer(replacer); err != nil {
		return err
	}
	binder.injected = replacer
	return nil
}

func (binder *observingRuleProcessorBinder) Start(context.Context) error {
	binder.startCalls++
	return errors.New("test rule processor must not start during composition")
}

func birthOnlyRuleProcessorBindManager(
	nats *natsclient.Client,
	binder *observingRuleProcessorBinder,
) *Manager {
	manager := NewServiceManager(NewServiceRegistry())
	manager.natsClient = nats
	manager.services["component-manager"] = &ComponentManager{
		components: map[string]*component.ManagedComponent{
			"birth-only-rule-processor": {
				Component: binder,
				State:     component.StateInitialized,
			},
		},
	}
	return manager
}

func requireNoBindTransport(
	t *testing.T,
	ctx context.Context,
	client *natsclient.Client,
	observed <-chan string,
) {
	t.Helper()
	const barrier = "test.rule-pack.bind-barrier"
	require.NoError(t, client.Publish(ctx, barrier, nil))
	select {
	case subject := <-observed:
		require.Equal(t, barrier, subject, "composition must not publish application transport")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for NATS transport barrier")
	}
}

func newRulePackBindIntegrationRegistry(
	t *testing.T,
	client *natsclient.Client,
) (*ownership.Registry, *ownership.Heartbeater) {
	t.Helper()
	registry, err := ownership.EnsureBuckets(
		t.Context(),
		client,
		slog.Default(),
		vocabulary.InverseResolver,
	)
	require.NoError(t, err)
	return registry, registry.NewHeartbeater(time.Hour)
}

func TestIntegration_BindRulePackContractsInjectsOneClientPerPack(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	registry, heartbeater := newRulePackBindIntegrationRegistry(t, testClient.Client)
	vocabulary.Register("test.rule-pack.first-client")
	vocabulary.Register("test.rule-pack.second-client")
	first := &rulePackBindTestBinder{
		baseDiscoverable: baseDiscoverable{name: "first-client-pack"},
		packID:           "first-client-pack",
		contracts: []projection.Contract{
			rulePackBindTestContract(t, "first-client-contract", "test.rule-pack.first-client"),
		},
	}
	second := &rulePackBindTestBinder{
		baseDiscoverable: baseDiscoverable{name: "second-client-pack"},
		packID:           "second-client-pack",
		contracts: []projection.Contract{
			rulePackBindTestContract(t, "second-client-contract", "test.rule-pack.second-client"),
		},
	}

	err := BindRulePackContracts(
		context.Background(),
		rulePackBindTestManager(testClient.Client, first, second),
		registry,
		heartbeater,
		slog.Default(),
	)
	require.NoError(t, err)
	for _, binder := range []*rulePackBindTestBinder{first, second} {
		require.Equal(t, 1, binder.preflightCalls)
		require.Equal(t, 1, binder.injectionCalls)
		require.NotNil(t, binder.injected)
		require.True(t, heartbeater.IsEnrolled("rule-pack."+binder.packID))
	}
}

func TestIntegration_BindRulePackContractsInjectionFailureIsFatal(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	registry, heartbeater := newRulePackBindIntegrationRegistry(t, testClient.Client)
	vocabulary.Register("test.rule-pack.injection")
	injectionFailure := errors.New("processor rejected mutation client")
	binder := &rulePackBindTestBinder{
		baseDiscoverable: baseDiscoverable{name: "injection-failure-pack"},
		packID:           "injection-failure-pack",
		contracts: []projection.Contract{
			rulePackBindTestContract(t, "injection-failure-contract", "test.rule-pack.injection"),
		},
		injectionErr: injectionFailure,
	}

	err := BindRulePackContracts(
		context.Background(),
		rulePackBindTestManager(testClient.Client, binder),
		registry,
		heartbeater,
		slog.Default(),
	)
	require.ErrorIs(t, err, injectionFailure)
	require.Equal(t, 1, binder.preflightCalls)
	require.Equal(t, 1, binder.injectionCalls)
	require.NotNil(t, binder.injected)
	require.True(t, heartbeater.IsEnrolled("rule-pack.injection-failure-pack"),
		"successful bind remains visible even though boot must abort on injection failure")

	owner, found, ownerErr := registry.OwnerOf(
		t.Context(),
		"acme.ops.test.system.record.001",
		"test.rule-pack.injection",
	)
	require.NoError(t, ownerErr)
	require.True(t, found)
	require.Equal(t, "rule-pack.injection-failure-pack", owner)
}

func TestIntegration_BindRulePackContractsBirthOnlyRepeatFailsAtOneTimeInjection(t *testing.T) {
	ctx := context.Background()
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	vocabulary.Register("test.rule-pack.created-at")

	config, err := rule.NewConfig("birth-only-v1")
	require.NoError(t, err)
	config.EnableGraphIntegration = false
	config.ProjectionContracts = []projection.Contract{{
		Name:            "birth-only-record",
		MessageType:     "test.rule-pack.birth-only.v1",
		EntityPattern:   "acme.ops.test.system.record.*",
		BirthPredicates: []string{"test.rule-pack.created-at"},
	}}
	processor, err := rule.NewProcessor(testClient.Client, &config)
	require.NoError(t, err)
	binder := &observingRuleProcessorBinder{Processor: processor}
	manager := birthOnlyRuleProcessorBindManager(testClient.Client, binder)

	observed := make(chan string, 8)
	subscription, err := testClient.Client.Subscribe(ctx, ">", func(_ context.Context, message *nats.Msg) {
		observed <- message.Subject
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, subscription.Unsubscribe()) })
	requireNoBindTransport(t, ctx, testClient.Client, observed)

	require.NoError(t, BindRulePackContracts(ctx, manager, nil, nil, slog.Default()))
	requireNoBindTransport(t, ctx, testClient.Client, observed)
	require.Equal(t, 1, binder.injectionAttempts)
	require.Zero(t, binder.startCalls)

	client, ok := binder.injected.(*projection.MutationClient)
	require.True(t, ok, "real rule processor must receive the public mutation client")
	token := reflect.ValueOf(client).Elem().FieldByName("token")
	require.True(t, token.IsValid())
	require.True(t, token.IsZero(), "birth-only mutation client must not mint an owner token")

	err = BindRulePackContracts(ctx, manager, nil, nil, slog.Default())
	require.ErrorContains(t, err, "owned replacer is already configured")
	require.NotErrorIs(t, err, ownership.ErrOwnerAlreadyBound)
	requireNoBindTransport(t, ctx, testClient.Client, observed)
	require.Equal(t, 2, binder.injectionAttempts)
	require.Same(t, client, binder.injected, "failed repeat injection must preserve the first client")
	require.Zero(t, binder.startCalls)

	_, claimsErr := testClient.Client.GetKeyValueBucket(ctx, ownership.BucketOwnerClaims)
	require.ErrorIs(t, claimsErr, jetstream.ErrBucketNotFound,
		"birth-only composition must not create ownership registration")
	_, presenceErr := testClient.Client.GetKeyValueBucket(ctx, ownership.BucketOwnerPresence)
	require.ErrorIs(t, presenceErr, jetstream.ErrBucketNotFound,
		"birth-only composition must not create heartbeat presence")
}
