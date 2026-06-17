// Package lifecycle — unit tests for ADR-056 PR-1 OwnerToken stamping on
// lifecycle-driven mutation requests.
//
// Tests coverage:
//   - When ownerRegistry is wired, Manager.Transition stamps
//     UpdateEntityWithTriplesRequest.OwnerToken = "<workflowName>#<incarnation>".
//   - When ownerRegistry is nil (default, test paths), OwnerToken is empty
//     (lease check skipped at graph-ingest — backward-compatible).
//   - Manager.Create stamps OwnerToken on the create request.
package lifecycle

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/ownership"
)

// tokenRecorder wraps fakeEmitter and records the OwnerTokens seen on
// update and create requests — the production wire shape we're asserting.
type tokenRecorder struct {
	*fakeEmitter
	updateTokens []string
	createTokens []string
}

func newTokenRecorder(bucket *fakeBucket) *tokenRecorder {
	fe := &fakeEmitter{bucket: bucket}
	return &tokenRecorder{fakeEmitter: fe}
}

func (r *tokenRecorder) update(ctx context.Context, req *graph.UpdateEntityWithTriplesRequest) (*graph.UpdateEntityWithTriplesResponse, error) {
	r.updateTokens = append(r.updateTokens, req.OwnerToken)
	return r.fakeEmitter.update(ctx, req)
}

func (r *tokenRecorder) create(ctx context.Context, req *graph.CreateEntityWithTriplesRequest) (*graph.CreateEntityWithTriplesResponse, error) {
	r.createTokens = append(r.createTokens, req.OwnerToken)
	return r.fakeEmitter.create(ctx, req)
}

const fixtureEntityID = "c360.platform1.lifecycle.gcs.mission.owntok"

// newRecordingTestManager builds a Manager with a tokenRecorder emitter.
func newRecordingTestManager(t *testing.T) (*Manager, *tokenRecorder) {
	t.Helper()
	bucket := newFakeBucket()
	rec := newTokenRecorder(bucket)
	mgr := newManagerForTest(nil, rec, bucket)
	wf := lifecycle{}.fixtureWorkflow()
	require.NoError(t, mgr.Register(wf), "Register fixture workflow")
	return mgr, rec
}

// TestManager_Transition_StampsOwnerToken proves that when AttachOwnership is
// called with a non-nil Registry, Transition stamps the outgoing
// UpdateEntityWithTriplesRequest with the "<workflowName>#<incarnation>" token.
func TestManager_Transition_StampsOwnerToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mgr, rec := newRecordingTestManager(t)

	// Seed the entity in the initial phase.
	err := mgr.Create(ctx, &fixtureMission{ID: fixtureEntityID, PhaseF: "planning"})
	require.NoError(t, err)

	// Attach a real Registry (nil KV — only the Incarnation field matters here).
	reg := ownership.NewRegistry(nil, nil, nil)
	mgr.mu.Lock()
	mgr.ownerRegistry = reg
	mgr.mu.Unlock()

	err = mgr.Transition(ctx, "fixture", fixtureEntityID, "flying", TransitionSourceOperator, "taking off")
	require.NoError(t, err)

	// The last update request (the Transition write) must carry the token.
	require.NotEmpty(t, rec.updateTokens, "Transition must have issued at least one update request")
	lastToken := rec.updateTokens[len(rec.updateTokens)-1]
	wantToken := "fixture" + "#" + reg.Incarnation()
	assert.Equal(t, wantToken, lastToken,
		"Transition update request must carry OwnerToken = <workflowName>#<incarnation>")
}

// TestManager_Transition_NoRegistryEmptyToken proves that without AttachOwnership
// the OwnerToken is empty — graph-ingest skips the lease check, so behaviour is
// unchanged for unmigrated deploys and test paths.
func TestManager_Transition_NoRegistryEmptyToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mgr, rec := newRecordingTestManager(t)
	// ownerRegistry stays nil (the default from newManagerForTest).

	id := "c360.platform1.lifecycle.gcs.mission.noregistry"
	err := mgr.Create(ctx, &fixtureMission{ID: id, PhaseF: "planning"})
	require.NoError(t, err)

	err = mgr.Transition(ctx, "fixture", id, "flying", TransitionSourceOperator, "")
	require.NoError(t, err)

	require.NotEmpty(t, rec.updateTokens)
	lastToken := rec.updateTokens[len(rec.updateTokens)-1]
	assert.Empty(t, lastToken,
		"without an attached Registry the OwnerToken must be empty (backward-compatible)")
}

// TestManager_Create_StampsOwnerToken proves that Manager.Create stamps the
// OwnerToken on the create request (the entity-absent path).
func TestManager_Create_StampsOwnerToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mgr, rec := newRecordingTestManager(t)

	reg := ownership.NewRegistry(nil, nil, nil)
	mgr.mu.Lock()
	mgr.ownerRegistry = reg
	mgr.mu.Unlock()

	id := "c360.platform1.lifecycle.gcs.mission.createtok"
	err := mgr.Create(ctx, &fixtureMission{ID: id, PhaseF: "planning"})
	require.NoError(t, err)

	require.NotEmpty(t, rec.createTokens, "Create must have issued a create request")
	token := rec.createTokens[0]
	wantToken := "fixture" + "#" + reg.Incarnation()
	assert.Equal(t, wantToken, token,
		"Create request must carry OwnerToken = <workflowName>#<incarnation>")
}
