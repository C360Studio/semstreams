//go:build integration

package service

import (
	"testing"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/ownership"
	"github.com/c360studio/semstreams/pkg/projection"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/stretchr/testify/require"
)

func TestIntegration_StaticProjectionContractsBindAsOneOwnerSet(t *testing.T) {
	t.Cleanup(vocabulary.SnapshotRegistry())
	const (
		owner      = "static-contract-set-owner"
		predicateA = "test.static.a"
		predicateB = "test.static.b"
	)
	vocabulary.Register(predicateA)
	vocabulary.Register(predicateB)

	ctx := t.Context()
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	registry, err := ownership.EnsureBuckets(ctx, testClient.Client, nil, nil)
	require.NoError(t, err)
	heartbeater := registry.NewHeartbeater(ownership.HeartbeatInterval)
	contracts := []projection.Contract{
		{
			Name:          "test.static.a",
			EntityPattern: "acme.ops.test.system.widget.*",
			Groups: []projection.PredicateGroup{{
				Mode:       ownership.ModeReplaceOwned,
				Predicates: []string{predicateA},
			}},
		},
		{
			Name:          "test.static.b",
			EntityPattern: "acme.ops.test.system.widget.*",
			Groups: []projection.PredicateGroup{{
				Mode:       ownership.ModeReplaceOwned,
				Predicates: []string{predicateB},
			}},
		},
	}

	require.NoError(
		t,
		bindStaticProjectionContracts(ctx, registry, heartbeater, owner, contracts),
	)
	require.True(t, heartbeater.IsEnrolled(owner))
	for _, predicate := range []string{predicateA, predicateB} {
		gotOwner, found, ownerErr := registry.OwnerOf(
			ctx,
			"acme.ops.test.system.widget.001",
			predicate,
		)
		require.NoError(t, ownerErr)
		require.True(t, found)
		require.Equal(t, owner, gotOwner)
	}
}

func TestIntegration_StaticProjectionContractSetValidatesBeforeOwnerBinding(t *testing.T) {
	t.Cleanup(vocabulary.SnapshotRegistry())
	const (
		owner     = "static-contract-validation-owner"
		predicate = "test.static.overlap"
	)
	vocabulary.Register(predicate)

	ctx := t.Context()
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	registry, err := ownership.EnsureBuckets(ctx, testClient.Client, nil, nil)
	require.NoError(t, err)
	heartbeater := registry.NewHeartbeater(ownership.HeartbeatInterval)
	contract := projection.Contract{
		Name:          "test.static.overlap.a",
		EntityPattern: "acme.ops.test.system.widget.*",
		Groups: []projection.PredicateGroup{{
			Mode:       ownership.ModeReplaceOwned,
			Predicates: []string{predicate},
		}},
	}
	overlap := contract
	overlap.Name = "test.static.overlap.b"

	err = bindStaticProjectionContracts(
		ctx,
		registry,
		heartbeater,
		owner,
		[]projection.Contract{contract, overlap},
	)
	require.ErrorIs(t, err, projection.ErrInvalidContract)
	require.False(t, heartbeater.IsEnrolled(owner))
	_, found, ownerErr := registry.OwnerOf(
		ctx,
		"acme.ops.test.system.widget.001",
		predicate,
	)
	require.NoError(t, ownerErr)
	require.False(t, found)

	require.NoError(
		t,
		bindStaticProjectionContracts(
			ctx,
			registry,
			heartbeater,
			owner,
			[]projection.Contract{contract},
		),
		"aggregate validation failure must not consume the owner's one bind",
	)
}
