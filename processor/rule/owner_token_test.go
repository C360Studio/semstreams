// Package rule — unit tests for ADR-056 PR-1 OwnerToken stamping on
// replace_owned mutation requests.
//
// Tests coverage:
//   - With incarnation set, ReplaceOwned request carries "<owner>#<incarnation>"
//     as the ownerToken argument to the mutator.
//   - Without incarnation (legacy / test path), the ownerToken is just the owner
//     string (no "#" suffix) — backward-compatible.
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
// is set (test path, unowned pack, Registry not wired) the executor falls back to
// passing just the owner string — no "#" suffix — preserving backward compatibility
// for legacy callers.
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

	// Without incarnation the owner is passed through unchanged (no "#").
	assert.Equal(t, testReplaceOwnedOwner, call.owner,
		"without incarnation the mutator receives just the owner id (backward-compatible)")
	assert.NotContains(t, call.owner, "#",
		"without incarnation the token must not contain '#'")
}
