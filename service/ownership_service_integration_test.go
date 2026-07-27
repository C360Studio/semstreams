//go:build integration

package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/internal/builtinprojection"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	"github.com/c360studio/semstreams/pkg/ownership"
	"github.com/c360studio/semstreams/pkg/projection"
	"github.com/c360studio/semstreams/vocabulary"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
	"github.com/c360studio/semstreams/vocabulary/builtins"
)

func TestIntegration_WireOwnershipBindsAggregateContractsOnce(t *testing.T) {
	builtins.Register()
	ctx := context.Background()
	client := natsclient.NewTestClient(t, natsclient.WithKV()).Client
	manager := lifecycle.NewManager(client, slog.Default())
	hbCtx, shutdown := WireOwnershipShutdown(ctx, manager)
	defer shutdown()

	contracts := builtinprojection.Contracts()
	registry, heartbeater, mutations, err := WireOwnership(
		hbCtx,
		client,
		manager,
		slog.Default(),
		contracts...,
	)
	if err != nil {
		t.Fatalf("WireOwnership: %v", err)
	}
	if registry == nil || heartbeater == nil || mutations == nil {
		t.Fatal("aggregate wiring returned a nil dependency")
	}
	if !heartbeater.IsEnrolled(builtinprojection.OwnerID) {
		t.Fatalf("aggregate owner %q is not enrolled for heartbeat", builtinprojection.OwnerID)
	}

	assertOwner := func(entityID, predicate string) {
		t.Helper()
		owner, found, readErr := registry.OwnerOf(ctx, entityID, predicate)
		if readErr != nil {
			t.Fatalf("OwnerOf(%q, %q): %v", entityID, predicate, readErr)
		}
		if !found || owner != builtinprojection.OwnerID {
			t.Fatalf(
				"OwnerOf(%q, %q) = (%q, %v), want (%q, true)",
				entityID,
				predicate,
				owner,
				found,
				builtinprojection.OwnerID,
			)
		}
	}
	todoEntity := "acme.ops.agent.agentic-loop.execution.loop-1"
	lessonEntity := "acme.ops.agent.lesson.record.11111111-1111-5111-8111-111111111111"
	assertOwner(todoEntity, agvocab.TodoStatus)
	assertOwner(lessonEntity, agvocab.LessonStatus)

	_, err = projection.BindMutationClient(ctx, projection.MutationClientConfig{
		NATS:        client,
		Registry:    registry,
		Heartbeater: heartbeater,
		Owner:       builtinprojection.OwnerID,
		Contracts:   contracts[:1],
	})
	if !errors.Is(err, ownership.ErrOwnerAlreadyBound) {
		t.Fatalf("subset same-owner bind error = %v, want ErrOwnerAlreadyBound", err)
	}
	assertOwner(todoEntity, agvocab.TodoStatus)
	assertOwner(lessonEntity, agvocab.LessonStatus)
}

func TestIntegration_WireOwnershipBindsCustomContractSetAsOneOwner(t *testing.T) {
	t.Cleanup(vocabulary.SnapshotRegistry())
	vocabulary.Register("test.static.a")
	vocabulary.Register("test.static.b")
	contracts := []projection.Contract{
		{
			Name:          "test.static.a",
			EntityPattern: "acme.ops.test.system.widget.*",
			Groups: []projection.PredicateGroup{{
				Mode:       ownership.ModeReplaceOwned,
				Predicates: []string{"test.static.a"},
			}},
		},
		{
			Name:          "test.static.b",
			EntityPattern: "acme.ops.test.system.widget.*",
			Groups: []projection.PredicateGroup{{
				Mode:       ownership.ModeReplaceOwned,
				Predicates: []string{"test.static.b"},
			}},
		},
	}

	ctx := context.Background()
	client := natsclient.NewTestClient(t, natsclient.WithKV()).Client
	manager := lifecycle.NewManager(client, slog.Default())
	hbCtx, shutdown := WireOwnershipShutdown(ctx, manager)
	defer shutdown()
	registry, heartbeater, mutations, err := WireOwnership(
		hbCtx,
		client,
		manager,
		slog.Default(),
		contracts...,
	)
	if err != nil {
		t.Fatalf("WireOwnership custom contracts: %v", err)
	}
	if registry == nil || heartbeater == nil || mutations == nil {
		t.Fatal("custom aggregate wiring returned a nil dependency")
	}
	if !heartbeater.IsEnrolled(builtinprojection.OwnerID) {
		t.Fatalf("custom aggregate owner %q is not enrolled", builtinprojection.OwnerID)
	}
	for _, predicate := range []string{"test.static.a", "test.static.b"} {
		owner, found, readErr := registry.OwnerOf(
			ctx,
			"acme.ops.test.system.widget.001",
			predicate,
		)
		if readErr != nil || !found || owner != builtinprojection.OwnerID {
			t.Fatalf("OwnerOf(%q) = (%q, %v, %v)", predicate, owner, found, readErr)
		}
	}
}

func TestIntegration_AggregateValidationFailureDoesNotConsumeOwnerBinding(t *testing.T) {
	t.Cleanup(vocabulary.SnapshotRegistry())
	vocabulary.Register("test.static.overlap")
	contract := projection.Contract{
		Name:          "test.static.overlap.a",
		EntityPattern: "acme.ops.test.system.widget.*",
		Groups: []projection.PredicateGroup{{
			Mode:       ownership.ModeReplaceOwned,
			Predicates: []string{"test.static.overlap"},
		}},
	}
	overlap := contract
	overlap.Name = "test.static.overlap.b"

	ctx := context.Background()
	client := natsclient.NewTestClient(t, natsclient.WithKV()).Client
	registry, err := ownership.EnsureBuckets(
		ctx,
		client,
		slog.Default(),
		vocabulary.InverseResolver,
	)
	if err != nil {
		t.Fatalf("EnsureBuckets: %v", err)
	}
	heartbeater := registry.NewHeartbeater(ownership.HeartbeatInterval)
	_, err = projection.BindMutationClient(ctx, projection.MutationClientConfig{
		NATS:        client,
		Registry:    registry,
		Heartbeater: heartbeater,
		Owner:       builtinprojection.OwnerID,
		Contracts:   []projection.Contract{contract, overlap},
	})
	if !errors.Is(err, projection.ErrInvalidContract) {
		t.Fatalf("invalid aggregate bind error = %v", err)
	}
	if heartbeater.IsEnrolled(builtinprojection.OwnerID) {
		t.Fatal("invalid aggregate enrolled the owner heartbeat")
	}
	if _, found, readErr := registry.OwnerOf(
		ctx,
		"acme.ops.test.system.widget.001",
		"test.static.overlap",
	); readErr != nil || found {
		t.Fatalf("invalid aggregate claim found=%v err=%v", found, readErr)
	}

	if _, err := projection.BindMutationClient(ctx, projection.MutationClientConfig{
		NATS:        client,
		Registry:    registry,
		Heartbeater: heartbeater,
		Owner:       builtinprojection.OwnerID,
		Contracts:   []projection.Contract{contract},
	}); err != nil {
		t.Fatalf("valid retry after aggregate validation failure: %v", err)
	}
}

func TestIntegration_WireOwnershipBindAndOverlapFailuresBlockBoot(t *testing.T) {
	builtins.Register()

	t.Run("invalid aggregate bind", func(t *testing.T) {
		client := natsclient.NewTestClient(t, natsclient.WithKV()).Client
		manager := lifecycle.NewManager(client, slog.Default())
		hbCtx, shutdown := WireOwnershipShutdown(context.Background(), manager)
		defer shutdown()
		contracts := builtinprojection.Contracts()
		contracts = append(contracts, contracts[0])

		registry, heartbeater, mutations, err := WireOwnership(
			hbCtx,
			client,
			manager,
			slog.Default(),
			contracts...,
		)
		if err == nil || !strings.Contains(err.Error(), "bind static projection mutation client") {
			t.Fatalf("invalid aggregate bind error = %v", err)
		}
		if registry != nil || heartbeater != nil || mutations != nil {
			t.Fatal("invalid bind returned partial wiring")
		}
	})

	t.Run("live overlap", func(t *testing.T) {
		ctx := context.Background()
		client := natsclient.NewTestClient(t, natsclient.WithKV()).Client
		rivalRegistry, err := ownership.EnsureBuckets(
			ctx,
			client,
			slog.Default(),
			vocabulary.InverseResolver,
		)
		if err != nil {
			t.Fatalf("EnsureBuckets rival: %v", err)
		}
		rivalHeartbeat := rivalRegistry.NewHeartbeater(ownership.HeartbeatInterval)
		_, err = projection.BindMutationClient(ctx, projection.MutationClientConfig{
			NATS:        client,
			Registry:    rivalRegistry,
			Heartbeater: rivalHeartbeat,
			Owner:       "rival-loop-writer",
			Contracts:   builtinprojection.Contracts()[:1],
		})
		if err != nil {
			t.Fatalf("bind rival: %v", err)
		}

		manager := lifecycle.NewManager(client, slog.Default())
		hbCtx, shutdown := WireOwnershipShutdown(ctx, manager)
		defer shutdown()
		registry, heartbeater, mutations, err := WireOwnership(
			hbCtx,
			client,
			manager,
			slog.Default(),
			builtinprojection.Contracts()...,
		)
		if err == nil || !strings.Contains(err.Error(), "overlap") {
			t.Fatalf("overlap bind error = %v", err)
		}
		if registry != nil || heartbeater != nil || mutations != nil {
			t.Fatal("overlap bind returned partial wiring")
		}
	})
}
