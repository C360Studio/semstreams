package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/ownership"
	"github.com/c360studio/semstreams/pkg/projection"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/stretchr/testify/require"
)

type rulePackBindTestBinder struct {
	baseDiscoverable
	packID         string
	contracts      []projection.Contract
	preflightErr   error
	injectionErr   error
	preflightCalls int
	injectionCalls int
	startCalls     int
	injected       projection.OwnedReplacer
}

func (b *rulePackBindTestBinder) ProjectionBindings() (string, []projection.Contract) {
	return b.packID, b.contracts
}

func (b *rulePackBindTestBinder) PreflightProjectionMutations() error {
	b.preflightCalls++
	return b.preflightErr
}

func (b *rulePackBindTestBinder) SetOwnedReplacer(replacer projection.OwnedReplacer) error {
	b.injectionCalls++
	b.injected = replacer
	return b.injectionErr
}

func (b *rulePackBindTestBinder) Initialize() error {
	return nil
}

func (b *rulePackBindTestBinder) Start(context.Context) error {
	b.startCalls++
	return nil
}

func (b *rulePackBindTestBinder) Stop(time.Duration) error {
	return nil
}

func rulePackBindTestManager(
	nats *natsclient.Client,
	binders ...*rulePackBindTestBinder,
) *Manager {
	components := make(map[string]*component.ManagedComponent, len(binders))
	for i, binder := range binders {
		name := binder.packID
		if name == "" {
			name = "empty"
		}
		components[name+"-"+string(rune('a'+i))] = &component.ManagedComponent{
			Component: binder,
			State:     component.StateInitialized,
		}
	}
	manager := NewServiceManager(NewServiceRegistry())
	manager.natsClient = nats
	manager.services["component-manager"] = &ComponentManager{components: components}
	return manager
}

func rulePackBindTestContract(t testing.TB, name, predicate string) projection.Contract {
	t.Helper()
	return projection.Contract{
		Name:          name,
		MessageType:   "test.rule-pack.v1",
		EntityPattern: "acme.ops.test.system.record.*",
		Groups: []projection.PredicateGroup{{
			Name:       "state",
			Mode:       ownership.ModeReplaceOwned,
			Predicates: []string{predicate}, // predicate-audit:unrelated {"column":25,"surface":"go-field:Predicates:element","value":"","basis":"reviewed helper forwards a caller-supplied registered fixture predicate"}
		}},
	}
}

func TestBindRulePackContracts_ZeroAndEmptyPacksNeedNoMutationDependencies(t *testing.T) {
	t.Parallel()

	require.NoError(t, BindRulePackContracts(
		context.Background(),
		rulePackBindTestManager(nil),
		nil,
		nil,
		nil,
	))

	empty := &rulePackBindTestBinder{
		baseDiscoverable: baseDiscoverable{name: "empty-pack"},
		packID:           "empty-pack",
	}
	require.NoError(t, BindRulePackContracts(
		context.Background(),
		rulePackBindTestManager(nil, empty),
		nil,
		nil,
		nil,
	))
	require.Equal(t, 1, empty.preflightCalls)
	require.Zero(t, empty.injectionCalls)
}

func TestBindRulePackContracts_MissingMutationDependenciesFailBeforeInjection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		nats        *natsclient.Client
		registry    *ownership.Registry
		heartbeater *ownership.Heartbeater
		want        string
	}{
		{
			name:        "NATS",
			registry:    new(ownership.Registry),
			heartbeater: new(ownership.Heartbeater),
			want:        "requires NATS",
		},
		{
			name:        "registry",
			nats:        new(natsclient.Client),
			heartbeater: new(ownership.Heartbeater),
			want:        "require an ownership registry",
		},
		{
			name:     "heartbeater",
			nats:     new(natsclient.Client),
			registry: new(ownership.Registry),
			want:     "require a heartbeater",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			vocabulary.Register("test.rule-pack.deps")
			binder := &rulePackBindTestBinder{
				baseDiscoverable: baseDiscoverable{name: "deps-pack"},
				packID:           "deps-pack",
				contracts: []projection.Contract{
					rulePackBindTestContract(t, "deps-contract", "test.rule-pack.deps"),
				},
			}
			err := BindRulePackContracts(
				context.Background(),
				rulePackBindTestManager(test.nats, binder),
				test.registry,
				test.heartbeater,
				nil,
			)
			require.ErrorContains(t, err, test.want)
			require.Zero(t, binder.injectionCalls)
		})
	}
}

func TestBindRulePackContracts_PreflightFailurePreventsEveryInjectionAndStart(t *testing.T) {
	t.Parallel()
	vocabulary.Register("test.rule-pack.first")
	vocabulary.Register("test.rule-pack.second")

	preflightFailure := errors.New("pack target invalid")
	first := &rulePackBindTestBinder{
		baseDiscoverable: baseDiscoverable{name: "first-pack"},
		packID:           "first-pack",
		contracts: []projection.Contract{
			rulePackBindTestContract(t, "first-contract", "test.rule-pack.first"),
		},
	}
	second := &rulePackBindTestBinder{
		baseDiscoverable: baseDiscoverable{name: "second-pack"},
		packID:           "second-pack",
		contracts: []projection.Contract{
			rulePackBindTestContract(t, "second-contract", "test.rule-pack.second"),
		},
		preflightErr: preflightFailure,
	}

	err := BindRulePackContracts(
		context.Background(),
		rulePackBindTestManager(new(natsclient.Client), first, second),
		new(ownership.Registry),
		new(ownership.Heartbeater),
		nil,
	)
	require.ErrorIs(t, err, preflightFailure)
	require.Equal(t, 1, second.preflightCalls)
	require.Zero(t, first.injectionCalls)
	require.Zero(t, second.injectionCalls)
	require.Zero(t, first.startCalls)
	require.Zero(t, second.startCalls)
}

func TestBindRulePackContracts_PackOverlapFailsDuringWholeSetPreflight(t *testing.T) {
	t.Parallel()

	const predicate = "test.rule-pack.overlap"
	vocabulary.Register("test.rule-pack.overlap")
	first := &rulePackBindTestBinder{
		baseDiscoverable: baseDiscoverable{name: "first-overlap-pack"},
		packID:           "first-overlap-pack",
		contracts: []projection.Contract{
			rulePackBindTestContract(t, "first-overlap", predicate),
		},
	}
	secondContract := rulePackBindTestContract(t, "second-overlap", predicate)
	second := &rulePackBindTestBinder{
		baseDiscoverable: baseDiscoverable{name: "second-overlap-pack"},
		packID:           "second-overlap-pack",
		contracts:        []projection.Contract{secondContract},
	}

	err := BindRulePackContracts(
		context.Background(),
		rulePackBindTestManager(new(natsclient.Client), first, second),
		new(ownership.Registry),
		new(ownership.Heartbeater),
		nil,
	)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "overlap"), err)
	require.Zero(t, first.injectionCalls)
	require.Zero(t, second.injectionCalls)
	require.Zero(t, first.startCalls)
	require.Zero(t, second.startCalls)
}
