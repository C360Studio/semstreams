// Package rule — unit tests for ADR-056 PR-1 OwnerToken stamping on
// replace_owned mutation requests.
//
// Tests coverage:
//   - With incarnation set, ReplaceOwned request carries "<owner>#<incarnation>"
//     as the ownerToken argument to the mutator.
//   - Without incarnation (resourceless deploy / Registry not wired / test path)
//     the ownerToken degrades to the EMPTY string — matching the lifecycle
//     producer's two-state wire contract (empty = the deferred lease check skips
//     it); it is NEVER a bare unfenced "<owner>".
//   - The UpdateEntityWithTriplesRequest.OwnerToken field is wired correctly in
//     the production tripleMutator.ReplaceOwned implementation (via round-trip on
//     the capturing mock).
package rule

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testIncarnation = "deadbeef01234567"

// executorWithOwnerAndIncarnation builds a fully-wired executor carrying both
// the owner and the per-process incarnation, matching the production path where
// BindRulePackContracts calls SetProjectionIncarnation before Start.
func executorWithOwnerAndIncarnation(mutator TripleMutator, owner, incarnation string) *ActionExecutor {
	e := NewActionExecutorFull(nil, mutator, nil)
	e.SetProjectionOwner(owner)
	e.SetProjectionIncarnation(incarnation)
	return e
}

// TestReplaceOwned_OwnerToken_WithIncarnation proves the executor composes the
// full "<owner>#<incarnation>" token when an incarnation is wired, and passes it
// as the ownerToken argument to TripleMutator.ReplaceOwned.
func TestReplaceOwned_OwnerToken_WithIncarnation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mutator := &mockTripleMutator{}
	executor := executorWithOwnerAndIncarnation(mutator, testReplaceOwnedOwner, testIncarnation)

	action := Action{Type: ActionTypeReplaceOwned, Predicate: ownedPredicate, Object: "running"}
	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "acme.ops.robotics.gcs.drone.001"}))

	require.Len(t, mutator.replaceOwnedCalls, 1)
	call := mutator.replaceOwnedCalls[0]

	// The ownerToken passed to the mutator must be "<owner>#<incarnation>".
	wantToken := testReplaceOwnedOwner + "#" + testIncarnation
	assert.Equal(t, wantToken, call.owner,
		"mutator must receive full OwnerToken = <owner>#<incarnation>")
	// Sanity: the token contains exactly one "#" separator.
	parts := strings.SplitN(call.owner, "#", 2)
	require.Len(t, parts, 2, "token must have exactly one '#' separator")
	assert.Equal(t, testReplaceOwnedOwner, parts[0], "token prefix must be the owner id")
	assert.Equal(t, testIncarnation, parts[1], "token suffix must be the incarnation")
}

// TestReplaceOwned_OwnerToken_WithoutIncarnation proves that when no incarnation
// is set (resourceless deploy, Registry not wired, test path) the executor emits
// an EMPTY ownerToken — NOT a bare "<owner>". This matches the lifecycle
// producer's two-state contract so the deferred lease check has a single rule:
// empty = skip. An unfenced "<owner>" would be an uninterpretable third state.
func TestReplaceOwned_OwnerToken_WithoutIncarnation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mutator := &mockTripleMutator{}
	// No SetProjectionIncarnation — incarnation field stays "".
	executor := executorWithOwner(mutator, testReplaceOwnedOwner)

	action := Action{Type: ActionTypeReplaceOwned, Predicate: ownedPredicate, Object: "running"}
	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "acme.ops.robotics.gcs.drone.001"}))

	require.Len(t, mutator.replaceOwnedCalls, 1)
	call := mutator.replaceOwnedCalls[0]

	// Without incarnation the token is EMPTY (two-state contract with lifecycle);
	// the deferred graph-ingest lease check skips an empty token.
	assert.Empty(t, call.owner,
		"without incarnation the mutator must receive an EMPTY token, never a bare '<owner>'")
}
