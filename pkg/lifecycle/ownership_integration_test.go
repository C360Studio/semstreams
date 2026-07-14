//go:build integration

// Integration test for the ADR-056 Decision 5 embed: the lifecycle Manager as
// the FIRST framework consumer of pkg/ownership. Drives the production path —
// ownership.EnsureBuckets → Manager.AttachOwnership → Manager.Register — through
// a real NATS testcontainer, exercising the shared OWNER_CLAIMS epoch and the
// observe-only cross-owner overlap posture. Unit coverage of the pure derivation
// lives in ownership_test.go; this proves the wire.
//
// Run with `go test -tags=integration -race ./pkg/lifecycle/...`.

package lifecycle

import (
	"context"
	"testing"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/ownership"
	"github.com/stretchr/testify/require"
)

const ownershipTestEntity = "c360.p.lifecycle.gcs.mission.001"

// A Manager attached to the registry lands its workflow's claim in the shared
// epoch (OwnerOf resolves it), and a SECOND owner claiming the same cell is
// rejected by the substrate while the Manager stays observe-only (logs, still
// registers locally, does NOT mutate the incumbent's epoch entry).
func TestIntegration_ManagerOwnership_FirstConsumerEndToEnd(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	// Cancellable so the heartbeat goroutines AttachOwnership spawns are reaped
	// when the test ends (exercising the ctx-cancel shutdown path).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg, err := ownership.EnsureBuckets(ctx, tc.Client, nil, nil)
	require.NoError(t, err, "EnsureBuckets must create the framework buckets against real NATS")

	// Process A registers the fixture workflow through the registry.
	mgrA := NewManager(tc.Client, nil)
	mgrA.AttachOwnership(ctx, reg)
	require.NoError(t, mgrA.Register(lifecycle{}.fixtureWorkflow()))

	// The phase cell is now owned in the shared epoch.
	owner, ok, err := reg.OwnerOf(ctx, ownershipTestEntity, "mission.lifecycle.phase")
	require.NoError(t, err)
	require.True(t, ok, "phase cell must be owned after registration")
	require.Equal(t, "fixture", owner, "owner id is the workflow Name")

	// Process B: a DIFFERENT owner claiming the SAME pattern + phase predicate.
	// A separate Registry over the same (idempotently re-ensured) buckets
	// simulates a second process.
	regB, err := ownership.EnsureBuckets(ctx, tc.Client, nil, nil)
	require.NoError(t, err)
	mgrB := NewManager(tc.Client, nil)
	mgrB.AttachOwnership(ctx, regB)

	rival := lifecycle{}.fixtureWorkflow()
	rival.Name = "fixture-rival" // distinct owner, same EntityIDPattern + PhasePredicate ⇒ overlap

	// Observe-only: the Manager does NOT error on the cross-owner overlap (a
	// partial migration must not brick — Decision 5 runtime posture)…
	require.NoError(t, mgrB.Register(rival), "Manager registration is observe-only on cross-owner overlap")
	// …the workflow IS locally registered and functions…
	_, err = mgrB.lookupByWorkflow("fixture-rival")
	require.NoError(t, err, "rival workflow must still register locally")
	// …and the incumbent keeps the contested cell — the rival's claim never
	// entered the epoch.
	owner, ok, err = regB.OwnerOf(ctx, ownershipTestEntity, "mission.lifecycle.phase")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "fixture", owner, "the incumbent owner must keep the contested cell")

	// Prove the Manager swallowed a REAL ErrOwnershipOverlap, not a no-op: the
	// substrate rejects the rival's derived registration outright.
	meta, err := parseSchemaType(rival.Schema)
	require.NoError(t, err)
	err = regB.RegisterOwner(ctx, deriveOwnerRegistration(rival, meta))
	require.ErrorIs(t, err, ownership.ErrOwnershipOverlap, "substrate must reject the cross-owner overlap")
}

// A second owner whose EntityIDPattern is DISJOINT from the incumbent registers
// cleanly and both claims coexist in the shared epoch — overlap rejection fires
// only on a genuine cell collision, not on every co-registration.
func TestIntegration_ManagerOwnership_DisjointOwnersCoexist(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg, err := ownership.EnsureBuckets(ctx, tc.Client, nil, nil)
	require.NoError(t, err)

	mgr := NewManager(tc.Client, nil)
	mgr.AttachOwnership(ctx, reg)
	require.NoError(t, mgr.Register(lifecycle{}.fixtureWorkflow())) // mission pattern

	// A disjoint workflow type (different `type` segment ⇒ patterns never intersect).
	other := lifecycle{}.fixtureWorkflow()
	other.Name = "other"
	other.EntityIDPattern = "*.*.lifecycle.gcs.sensor.*"
	other.PhasePredicate = "sensor.phase"
	require.NoError(t, mgr.Register(other))

	missionOwner, ok, err := reg.OwnerOf(ctx, ownershipTestEntity, "mission.lifecycle.phase")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "fixture", missionOwner)

	sensorOwner, ok, err := reg.OwnerOf(ctx, "c360.p.lifecycle.gcs.sensor.001", "sensor.phase")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "other", sensorOwner)
}
