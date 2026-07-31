//go:build integration

package service

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/nats-io/nats.go/jetstream"

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

// TestIntegration_WireOwnershipSubstrateWithZeroStaticContracts is gh#812's
// reproduction, promoted to a test.
//
// A downstream composition that has completed a framework-only ownership
// cutover has NO enabled static projection owner, so it has no contracts to
// bind — but it still needs the whole Phase-A substrate: retention backstop,
// ownership buckets, lifecycle attachment, and the shared heartbeater that a
// later BindRulePackContracts enrols against.
//
// Before the substrate split, the only public path to that substrate was
// WireOwnership, which unconditionally binds the static projection client and
// fails with "mutation client has no contracts" on an empty set. The one
// non-empty contract set the binaries use comes from internal/builtinprojection,
// which downstream correctly cannot import — so the public helper had a
// composition path only framework binaries could walk.
//
// This drives the PRODUCTION wire, not a re-implementation of it: the point of
// the issue is that composing the pieces by hand duplicates an evolving
// sequence, which is what the framework owes a helper for.
func TestIntegration_WireOwnershipSubstrateWithZeroStaticContracts(t *testing.T) {
	builtins.Register()
	ctx := context.Background()
	client := natsclient.NewTestClient(t, natsclient.WithKV()).Client
	manager := lifecycle.NewManager(client, slog.Default())
	hbCtx, shutdown := WireOwnershipShutdown(ctx, manager)
	defer shutdown()

	t.Cleanup(vocabulary.SnapshotRegistry())
	vocabulary.Register("test.gh812.downstream")

	// Pre-dirty a no-lifecycle catalog bucket so the ADR-068 D1 retention
	// backstop's ABSENCE is observable. Without this the backstop step could be
	// deleted from the substrate and this test would still pass — measured:
	// that mutation survived the first version of this test.
	dirtyBucket := dirtyFirstNoLifecycleCatalogBucket(t, client)

	registry, heartbeater, err := WireOwnershipSubstrate(hbCtx, client, manager, slog.Default())
	if err != nil {
		t.Fatalf("WireOwnershipSubstrate with zero static contracts: %v", err)
	}
	if registry == nil {
		t.Fatal("substrate returned a nil ownership registry")
	}
	if heartbeater == nil {
		t.Fatal("substrate returned a nil shared heartbeater — later rule-pack binding enrols against it")
	}

	// GUARD 1 — the retention backstop ran.
	if ttl := bucketTTL(t, client, dirtyBucket); ttl != 0 {
		t.Errorf("bucket %q still has TTL %v after the substrate ran — the retention backstop was skipped", dirtyBucket, ttl)
	}

	// GUARD 2 — the lifecycle manager is ownership-ATTACHED, not merely
	// constructed. Manager.Register reads m.ownerRegistry (set only by
	// AttachOwnership) and calls RegisterOwner; with the attach skipped the
	// registry never learns the owner and OwnerOf finds nothing. Measured: the
	// attach could be deleted and the first version of this test still passed.
	t.Cleanup(vocabulary.SnapshotRegistry())
	vocabulary.Register("test.gh812.phase")
	vocabulary.Register("test.gh812.downstream")

	if err := manager.Register(lifecycle.Workflow{
		Name:            "gh812wf",
		EntityIDPattern: "*.*.test.system.widget.*",
		Phases:          []string{"open", "closed"},
		Transitions:     lifecycle.Transitions{"open": {"closed"}, "closed": {}},
		PhasePredicate:  "test.gh812.phase",
		Schema:          reflect.TypeOf(gh812State{}),
	}); err != nil {
		t.Fatalf("register lifecycle workflow against the substrate: %v", err)
	}
	owner, found, err := registry.OwnerOf(ctx, "acme.ops.test.system.widget.1", "test.gh812.phase")
	if err != nil {
		t.Fatalf("OwnerOf after workflow registration: %v", err)
	}
	if !found || owner != "gh812wf" {
		t.Fatalf("OwnerOf = (%q, %v), want (%q, true) — the substrate did not attach ownership to the lifecycle manager",
			owner, found, "gh812wf")
	}

	// GUARD 3 — the shared heartbeater is usable by a LATER contract-bearing
	// owner, which is the whole reason it is returned.
	const downstreamOwner = "gh812-downstream-owner"
	if heartbeater.IsEnrolled(downstreamOwner) {
		t.Fatalf("owner %q is enrolled before anything enrolled it", downstreamOwner)
	}
	mutations, bindErr := projection.BindMutationClient(hbCtx, projection.MutationClientConfig{
		NATS:        client,
		Registry:    registry,
		Heartbeater: heartbeater,
		Owner:       downstreamOwner,
		Contracts: []projection.Contract{{
			Name:          "gh812.downstream",
			EntityPattern: "acme.ops.test.system.widget.*",
			Groups: []projection.PredicateGroup{{
				Mode:       ownership.ModeReplaceOwned,
				Predicates: []string{"test.gh812.downstream"},
			}},
		}},
	})
	if bindErr != nil {
		t.Fatalf("bind a downstream owner against the substrate heartbeater: %v", bindErr)
	}
	if mutations == nil {
		t.Fatal("downstream bind returned a nil mutation client")
	}
	if !heartbeater.IsEnrolled(downstreamOwner) {
		t.Fatalf("owner %q not enrolled on the substrate's shared heartbeater", downstreamOwner)
	}

	// GUARD 5 — THE ONE THAT WOULD HAVE CAUGHT THE SHIPPED DEFECT. The
	// substrate CONSTRUCTS the heartbeater; OwnershipService.Start is what RUNS
	// it (and WatchRevival). The first version of this note+test omitted this
	// step entirely, so the adopter recipe handed downstream a heartbeater
	// nobody ran: presence ages out at PresenceTTL and the next registrant
	// compacts the owning entry out of the epoch.
	//
	// NOT asserted here, deliberately: an actual heartbeat landing. Run waits
	// for the first tick and HeartbeatInterval is 30s, so observing a real beat
	// costs 30s of wall clock — too slow for this suite. What IS asserted is
	// the composition the recipe prescribes: the service is built over THIS
	// registry and THIS heartbeater, starts, and reports running.
	ownershipSvc := NewOwnershipService(registry, heartbeater, nil, slog.Default())
	if err := ownershipSvc.Start(hbCtx); err != nil {
		t.Fatalf("start the ownership service over the substrate: %v", err)
	}
	t.Cleanup(func() { _ = ownershipSvc.Stop(2 * time.Second) })
}

// gh812State is a minimal Participant for the ownership-attachment guard.
type gh812State struct {
	ID     string `json:"entity_id" lifecycle:"id"`
	PhaseF string `json:"phase" lifecycle:"phase,predicate=test.gh812.phase"`
}

func (s *gh812State) EntityID() string       { return s.ID }
func (s *gh812State) Workflow() string       { return "gh812wf" }
func (s *gh812State) Phase() string          { return s.PhaseF }
func (s *gh812State) IsTerminal() bool       { return s.PhaseF == "closed" }
func (s *gh812State) ParentEntityID() string { return "" }

// dirtyFirstNoLifecycleCatalogBucket creates the first no-lifecycle catalog
// bucket WITH a TTL, so the retention backstop has something to strip. Returns
// the bucket name.
func dirtyFirstNoLifecycleCatalogBucket(t *testing.T, client *natsclient.Client) string {
	t.Helper()
	js, err := client.JetStream()
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	for _, spec := range graph.KVCatalog() {
		if spec.Retention.Kind != natsclient.RetentionNoLifecycle {
			continue
		}
		if _, err := js.CreateKeyValue(context.Background(), jetstream.KeyValueConfig{
			Bucket: spec.Name,
			TTL:    time.Hour, // the dirt the backstop must strip
		}); err != nil {
			t.Fatalf("create dirty bucket %q: %v", spec.Name, err)
		}
		return spec.Name
	}
	t.Fatal("no no-lifecycle bucket in the KV catalog — this guard is not measuring what it claims")
	return ""
}

func bucketTTL(t *testing.T, client *natsclient.Client, bucket string) time.Duration {
	t.Helper()
	js, err := client.JetStream()
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	kv, err := js.KeyValue(context.Background(), bucket)
	if err != nil {
		t.Fatalf("open bucket %q: %v", bucket, err)
	}
	status, err := kv.Status(context.Background())
	if err != nil {
		t.Fatalf("bucket %q status: %v", bucket, err)
	}
	return status.TTL()
}

// TestIntegration_WireOwnershipStillRejectsAnEmptyRequestedBind pins the other
// half of the gh#812 ruling: SPLIT, do not skip.
//
// The rejected alternative was to make WireOwnership skip the bind when no
// contracts are supplied and return a nil client — which turns one function's
// behavior into a silent mode switch on input emptiness and hands back a
// maybe-nil capability. Asking for a bind with nothing to bind stays an ERROR,
// because that guard is correct exactly where a bind was actually requested.
func TestIntegration_WireOwnershipStillRejectsAnEmptyRequestedBind(t *testing.T) {
	builtins.Register()
	ctx := context.Background()
	client := natsclient.NewTestClient(t, natsclient.WithKV()).Client
	manager := lifecycle.NewManager(client, slog.Default())
	hbCtx, shutdown := WireOwnershipShutdown(ctx, manager)
	defer shutdown()

	_, _, mutations, err := WireOwnership(hbCtx, client, manager, slog.Default())
	if err == nil {
		t.Fatal("WireOwnership with zero contracts succeeded — a requested static bind with nothing to bind must fail closed, not silently skip")
	}
	if mutations != nil {
		t.Fatal("failed WireOwnership returned a non-nil mutation client")
	}
	if !strings.Contains(err.Error(), "no contracts") {
		t.Errorf("error = %q, want it to name the empty contract set", err)
	}
}
