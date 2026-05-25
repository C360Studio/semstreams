//go:build integration

// Integration tests for pkg/lifecycle against real NATS via
// testcontainers. Build-tagged separately so unit tests stay fast;
// run with `go test -tags=integration -race ./pkg/lifecycle/...`.
//
// These tests pin the kvNATSStore adapter against real jetstream
// semantics. The unit tests (manager_test.go, manager_query_test.go)
// cover behavior against the kvMockStore; this file covers
// wire-correctness — particularly the CAS classifier in
// kvNATSStore.Update which the PR 1b core review flagged as
// "verify against real jetstream sentinels in the testcontainers
// slice."
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// integMissionState is the integration-test Participant fixture.
// Top-level fields only (parseStructTags doesn't recurse into
// embedded structs per the foundation contract). Bucket name is
// fixed across integration tests — each test gets a fresh
// testcontainer (and therefore a fresh KV bucket) so isolation is
// at the container level.
type integMissionState struct {
	EntityIDF      string `json:"entity_id" lifecycle:"id"`
	PhaseF         string `json:"phase" lifecycle:"phase,readonly"`
	OwnerOrgIDF    string `json:"owner_org_id" lifecycle:"operator_writable"`
	ParentMissionF string `json:"parent_mission,omitempty"`
}

func (m *integMissionState) EntityID() string             { return m.EntityIDF }
func (m *integMissionState) Workflow() string             { return "integ-mission" }
func (m *integMissionState) Phase() string                { return m.PhaseF }
func (m *integMissionState) IsTerminal() bool             { return integMissionTransitions.IsTerminal(m.PhaseF) }
func (m *integMissionState) KVBucket() string             { return integBucketName }
func (m *integMissionState) KVKey(entityID string) string { return "mission." + entityID }
func (m *integMissionState) ParentEntityID() string       { return m.ParentMissionF }

var integMissionTransitions = Transitions{
	"planning":  {"flying", "aborted"},
	"flying":    {"capturing", "landing", "aborted"},
	"capturing": {"flying"},
	"landing":   {"completed", "failed"},
	"completed": {},
	"failed":    {},
	"aborted":   {},
}

// integBucketName is the single bucket all integration tests use.
// Per-container isolation makes per-test bucket names unnecessary.
const integBucketName = "LIFECYCLE_INTEG"

// setupManager creates a NATS testcontainer, provisions the
// integBucketName KV bucket, wires NewManager against the live
// client, and registers the integ-mission workflow. Returns the
// manager + the underlying natsclient so tests can do raw KV ops
// (e.g. Delete) where Manager doesn't expose a public path.
func setupManager(t *testing.T) (*Manager, *natsclient.Client) {
	t.Helper()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	ctx := context.Background()

	_, err := tc.Client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      integBucketName,
		Description: "lifecycle integration test bucket",
		History:     10,
	})
	require.NoError(t, err, "create KV bucket %q", integBucketName)

	mgr := NewManager(tc.Client, nil)
	require.NoError(t, mgr.Register("integ-mission",
		func() Participant { return &integMissionState{} },
		integMissionTransitions),
		"Register integ-mission workflow")
	return mgr, tc.Client
}

// --- Round-trip: Register/Create/Get/Update/Transition ---

func TestIntegration_LifecycleRoundTrip(t *testing.T) {
	mgr, _ := setupManager(t)
	ctx := context.Background()

	require.NoError(t, mgr.Create(ctx, &integMissionState{
		EntityIDF:   "rt-1",
		PhaseF:      "planning",
		OwnerOrgIDF: "acme",
	}))

	got, err := mgr.Get(ctx, "integ-mission", "rt-1")
	require.NoError(t, err)
	gotMission := got.(*integMissionState)
	require.Equal(t, "planning", gotMission.PhaseF)
	require.Equal(t, "acme", gotMission.OwnerOrgIDF)

	require.NoError(t, mgr.Update(ctx, "integ-mission", "rt-1", func(p Participant) error {
		p.(*integMissionState).OwnerOrgIDF = "newcorp"
		return nil
	}))

	require.NoError(t, mgr.Transition(ctx, "integ-mission", "rt-1", "flying",
		TransitionSourceRule, ""))

	after, _ := mgr.Get(ctx, "integ-mission", "rt-1")
	require.Equal(t, "flying", after.Phase())
	require.Equal(t, "newcorp", after.(*integMissionState).OwnerOrgIDF)
}

// --- CAS classifier: verify against real jetstream sentinels ---

func TestIntegration_CASClassifierTriggersOnRealConflict(t *testing.T) {
	// Load-bearing test for the kvNATSStore.Update classifier
	// hierarchy (PR 1b core review C1 finding). Concurrently
	// mutate the same key from N goroutines; the losers MUST
	// surface as errKVRevisionMismatch so Manager.Update can
	// retry. If the classifier mis-classifies, the retry won't
	// trigger and at least one goroutine fails.
	mgr, _ := setupManager(t)
	ctx := context.Background()

	require.NoError(t, mgr.Create(ctx, &integMissionState{
		EntityIDF: "cas-1", PhaseF: "planning",
	}))

	const writers = updateRetries - 1
	var wg sync.WaitGroup
	wg.Add(writers)
	errs := make(chan error, writers)
	for i := range writers {
		go func(tag int) {
			defer wg.Done()
			err := mgr.Update(ctx, "integ-mission", "cas-1", func(p Participant) error {
				p.(*integMissionState).OwnerOrgIDF = fmt.Sprintf("writer-%d", tag)
				return nil
			})
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent update failed (CAS classifier likely mis-classified): %v", err)
	}

	final, err := mgr.Get(ctx, "integ-mission", "cas-1")
	require.NoError(t, err)
	finalOwner := final.(*integMissionState).OwnerOrgIDF
	require.True(t, strings.HasPrefix(finalOwner, "writer-"),
		"final owner should be one of the writers, got %q", finalOwner)
}

// --- Restart-recovery: state persists across Manager instances ---

func TestIntegration_RestartRecoveryPersistsState(t *testing.T) {
	mgr1, client := setupManager(t)
	ctx := context.Background()

	require.NoError(t, mgr1.Create(ctx, &integMissionState{
		EntityIDF:   "rr-1",
		PhaseF:      "planning",
		OwnerOrgIDF: "before-restart",
	}))
	require.NoError(t, mgr1.Transition(ctx, "integ-mission", "rr-1", "flying",
		TransitionSourceRule, ""))

	// Simulate a process restart: new Manager against the SAME
	// natsclient (same NATS server + same bucket). The bucket
	// already exists so the Register-time CreateKeyValueBucket
	// hits the "use existing" path; the prior state is intact.
	mgr2 := NewManager(client, nil)
	require.NoError(t, mgr2.Register("integ-mission",
		func() Participant { return &integMissionState{} },
		integMissionTransitions))

	got, err := mgr2.Get(ctx, "integ-mission", "rr-1")
	require.NoError(t, err)
	require.Equal(t, "flying", got.Phase(), "state should persist across Manager restart")
	require.Equal(t, "before-restart", got.(*integMissionState).OwnerOrgIDF)
}

// --- Delete + History tombstone ---

func TestIntegration_DeleteThenGetReturnsNotFound(t *testing.T) {
	mgr, _ := setupManager(t)
	ctx := context.Background()

	require.NoError(t, mgr.Create(ctx, &integMissionState{
		EntityIDF: "del-1", PhaseF: "planning",
	}))

	// Manager doesn't expose a public Delete (apps remove
	// entities via Transition to a terminal + retention policy,
	// NOT direct deletion). But the kvStore adapter handles
	// deletes for jetstream tombstone semantics — this test
	// pins that path via the registered store.
	mgr.mu.RLock()
	reg := mgr.registrations["integ-mission"]
	mgr.mu.RUnlock()

	require.NoError(t, reg.store.Delete(ctx, "mission.del-1"))

	_, err := mgr.Get(ctx, "integ-mission", "del-1")
	require.ErrorIs(t, err, ErrEntityNotFound,
		"Get after Delete must return ErrEntityNotFound")
}

func TestIntegration_HistoryIncludesDeleteTombstone(t *testing.T) {
	mgr, _ := setupManager(t)
	ctx := context.Background()

	require.NoError(t, mgr.Create(ctx, &integMissionState{
		EntityIDF: "ht-1", PhaseF: "planning",
	}))
	require.NoError(t, mgr.Transition(ctx, "integ-mission", "ht-1", "flying",
		TransitionSourceRule, ""))

	mgr.mu.RLock()
	reg := mgr.registrations["integ-mission"]
	mgr.mu.RUnlock()
	require.NoError(t, reg.store.Delete(ctx, "mission.ht-1"))

	events, err := mgr.History(ctx, "integ-mission", "ht-1")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(events), 2,
		"history should include at least Create + delete tombstone, got %+v", events)
	last := events[len(events)-1]
	require.Equal(t, "<deleted>", last.To, "last event should be deletion tombstone")
}

// --- History revision metadata ---

func TestIntegration_HistoryRevisionTimestampsAreMonotonic(t *testing.T) {
	mgr, _ := setupManager(t)
	ctx := context.Background()

	require.NoError(t, mgr.Create(ctx, &integMissionState{
		EntityIDF: "ts-1", PhaseF: "planning",
	}))
	time.Sleep(10 * time.Millisecond)
	require.NoError(t, mgr.Transition(ctx, "integ-mission", "ts-1", "flying",
		TransitionSourceRule, ""))
	time.Sleep(10 * time.Millisecond)
	require.NoError(t, mgr.Transition(ctx, "integ-mission", "ts-1", "capturing",
		TransitionSourceRule, ""))

	events, err := mgr.History(ctx, "integ-mission", "ts-1")
	require.NoError(t, err)
	require.Len(t, events, 3)

	for i, ev := range events {
		require.False(t, ev.At.IsZero(), "event %d has zero CreatedAt", i)
	}
	require.True(t, events[0].At.Before(events[1].At), "events[0] should precede events[1]")
	require.True(t, events[1].At.Before(events[2].At), "events[1] should precede events[2]")
}

// --- List against real NATS ListKeys ---

func TestIntegration_ListAgainstRealListKeys(t *testing.T) {
	mgr, _ := setupManager(t)
	ctx := context.Background()

	for i := range 3 {
		require.NoError(t, mgr.Create(ctx, &integMissionState{
			EntityIDF:   fmt.Sprintf("li-%d", i),
			PhaseF:      "planning",
			OwnerOrgIDF: "acme",
		}))
	}

	got, err := mgr.List(ctx, "integ-mission", ListOptions{})
	require.NoError(t, err)
	require.Len(t, got, 3, "List should return all 3 instances")

	filtered, err := mgr.List(ctx, "integ-mission", ListOptions{
		Match: map[string]any{"owner_org_id": "acme"},
	})
	require.NoError(t, err)
	require.Len(t, filtered, 3, "Match owner_org_id=acme should match all 3")
}

// --- Watch against real NATS WatchAll ---

func TestIntegration_WatchDeliversBootstrap(t *testing.T) {
	mgr, _ := setupManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i := range 2 {
		require.NoError(t, mgr.Create(ctx, &integMissionState{
			EntityIDF: fmt.Sprintf("wi-%d", i),
			PhaseF:    "planning",
		}))
	}

	ch, err := mgr.Watch(ctx, "integ-mission")
	require.NoError(t, err)

	seen := make(map[string]bool)
	timeout := time.After(5 * time.Second)
	for len(seen) < 2 {
		select {
		case p, ok := <-ch:
			if !ok {
				t.Fatalf("Watch channel closed early; got %d/2 entries", len(seen))
			}
			seen[p.EntityID()] = true
		case <-timeout:
			t.Fatalf("timed out waiting for Watch bootstrap; got %d/2 so far", len(seen))
		}
	}
	require.True(t, seen["wi-0"] && seen["wi-1"], "bootstrap missing entries: %+v", seen)
}

// --- Sanity: jetstream sentinel coverage ---

func TestIntegration_GetUnknownKeyReturnsNotFound(t *testing.T) {
	// Pins kvNATSStore.Get's jetstream.ErrKeyNotFound classifier
	// against the real jetstream sentinel. If this regresses, the
	// upstream classifier is silently broken.
	mgr, _ := setupManager(t)
	ctx := context.Background()

	_, err := mgr.Get(ctx, "integ-mission", "never-existed")
	require.True(t, errors.Is(err, ErrEntityNotFound),
		"Get on missing key should classify as ErrEntityNotFound, got %v", err)
}
