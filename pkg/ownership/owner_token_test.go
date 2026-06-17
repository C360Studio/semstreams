// Package ownership — unit tests for the ADR-056 PR-1 incarnation fence.
//
// Tests coverage:
//   - Registry.Incarnation is non-empty, stable across calls within a process,
//     and DIFFERENT across two NewRegistry constructions (proves per-process
//     uniqueness).
//   - The copy-stamp that RegisterOwner applies preserves a claim's sibling
//     fields (unit-level field-preservation only). The GENUINE register-time
//     storage assertion (real RegisterOwner → epoch read-back) lives in
//     registry_integration_test.go (TestRegistry_RegisterStampsIncarnationOnStoredClaim).
//   - OwnerToken composition (ADR-056 PR-3.5): Registry.OwnerToken mints
//     "<owner>#<incarnation>"; the producer mint and the verifier reconstruction
//     (ExpectedOwnerToken) agree; an empty incarnation collapses to the zero
//     token; two processes mint distinct tokens for the same owner (the fence).
package ownership

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRegistry_Incarnation_NonEmpty proves the boot nonce is not an empty
// string — a zero incarnation would make every OwnerToken identical across
// processes, defeating the fence entirely.
func TestRegistry_Incarnation_NonEmpty(t *testing.T) {
	t.Parallel()
	reg := newNoopRegistry(t)
	assert.NotEmpty(t, reg.Incarnation(), "incarnation must be non-empty immediately after NewRegistry")
}

// TestRegistry_Incarnation_StableWithinProcess proves the incarnation returned
// by consecutive Incarnation() calls is the same value (it is generated once at
// construction and never changes).
func TestRegistry_Incarnation_StableWithinProcess(t *testing.T) {
	t.Parallel()
	reg := newNoopRegistry(t)
	first := reg.Incarnation()
	second := reg.Incarnation()
	third := reg.Incarnation()
	assert.Equal(t, first, second, "incarnation must be stable across calls")
	assert.Equal(t, first, third, "incarnation must be stable across calls")
}

// TestRegistry_Incarnation_UniqueAcrossConstructions proves two independent
// NewRegistry calls produce different incarnations. This is the key property
// that makes the fence useful: a revived writer that re-registers the same
// owner id in a new process will have a DIFFERENT incarnation, so the
// graph-ingest lease check (a later PR) can reject the stale writer.
func TestRegistry_Incarnation_UniqueAcrossConstructions(t *testing.T) {
	t.Parallel()
	r1 := newNoopRegistry(t)
	r2 := newNoopRegistry(t)
	assert.NotEqual(t, r1.Incarnation(), r2.Incarnation(),
		"two distinct NewRegistry constructions must produce different incarnations (per-process uniqueness)")
}

// TestRegistry_Incarnation_HexFormat proves the incarnation is a valid
// lowercase hex string of exactly 16 characters (8 bytes).
func TestRegistry_Incarnation_HexFormat(t *testing.T) {
	t.Parallel()
	reg := newNoopRegistry(t)
	inc := reg.Incarnation()
	assert.Len(t, inc, 16, "incarnation must be exactly 16 hex chars (8 bytes)")
	for _, c := range inc {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
			"incarnation must be lowercase hex, got char %q", c)
	}
}

// TestOwnerClaim_StampPreservesSiblingFields verifies, in isolation, the
// copy-stamp semantics RegisterOwner uses: stamping Incarnation onto a claim
// copy carries the registry nonce WITHOUT mutating the claim's other fields.
//
// This is deliberately a narrow UNIT test of the copy logic — it does NOT drive
// RegisterOwner (which needs NATS) and therefore does NOT prove the epoch
// actually persists the incarnation. That genuine register-time storage
// assertion (real RegisterOwner → epoch read-back, would fail if the stamp loop
// were deleted) lives in registry_integration_test.go
// (TestRegistry_RegisterStampsIncarnationOnStoredClaim).
func TestOwnerClaim_StampPreservesSiblingFields(t *testing.T) {
	t.Parallel()
	reg := newNoopRegistry(t)
	inc := reg.Incarnation()

	// Register an owner. We call the internal stampedClaims logic directly
	// by asserting on the Registration the struct would build — we don't
	// drive through RegisterOwner (which needs NATS) but test the stamp
	// logic by constructing what RegisterOwner builds.
	//
	// The stamp logic: reg.RegisterOwner copies r.Claims and sets
	// c.Incarnation = reg.incarnation on each copy before writing to the
	// epoch. We verify that logic by replicating the copy here.
	original := OwnerClaim{
		Owner:      "rule-pack.test",
		Pattern:    "acme.ops.*.*.*.* ",
		Predicates: []string{"status.phase"},
		Mode:       ModeReplaceOwned,
	}
	// Fix the pattern — must be a valid 6-part glob.
	original.Pattern = "acme.ops.robotics.gcs.drone.*"

	// Replicate what RegisterOwner does: copy + stamp.
	stamped := original
	stamped.Incarnation = reg.incarnation

	assert.Equal(t, inc, stamped.Incarnation,
		"stamped claim must carry the registry's incarnation nonce")
	assert.Equal(t, original.Owner, stamped.Owner,
		"stamp must not mutate other fields")
	assert.Equal(t, original.Pattern, stamped.Pattern,
		"stamp must not mutate other fields")
	assert.Equal(t, original.Predicates, stamped.Predicates,
		"stamp must not mutate other fields")
}

// TestOwnerToken_Wire_Format proves Registry.OwnerToken mints a token whose wire
// form is exactly "<owner>#<incarnation>" — the credential producers stamp on
// mutation requests (ADR-056 PR-3.5). The format lives only in pkg/ownership;
// this test is the single place that asserts the literal "#" composition.
func TestOwnerToken_Wire_Format(t *testing.T) {
	t.Parallel()
	reg := newNoopRegistry(t)
	tok := reg.OwnerToken("rule-pack.demo")
	assert.False(t, tok.IsZero(), "a token minted from a live registry is not the zero token")
	assert.Equal(t, "rule-pack.demo#"+reg.Incarnation(), tok.Wire(),
		"minted token Wire() must be exactly <owner>#<incarnation>")
}

// TestOwnerToken_ProducerMatchesVerifier proves the producer mint
// (Registry.OwnerToken) and the verifier reconstruction (ExpectedOwnerToken) agree
// for the same owner + incarnation — the core lease-check round-trip. If they
// diverged, every legitimate write would log a false mismatch.
func TestOwnerToken_ProducerMatchesVerifier(t *testing.T) {
	t.Parallel()
	reg := newNoopRegistry(t)
	const owner = "mission-planner"
	produced := reg.OwnerToken(owner)
	expected := ExpectedOwnerToken(owner, reg.Incarnation())
	assert.Equal(t, produced.Wire(), expected.Wire(),
		"producer mint and verifier reconstruction must yield the same wire token")
	assert.NotEmpty(t, produced.Wire(), "round-trip token must be non-empty")
}

// TestOwnerToken_EmptyIncarnation_IsZero proves the two-state contract: an empty
// incarnation (a legacy / pre-fence owner, or no Registry wired) collapses to the
// zero token whose Wire() is "" — never a bare "<owner>#" or "<owner>". The
// graph-ingest lease check skips an empty token.
func TestOwnerToken_EmptyIncarnation_IsZero(t *testing.T) {
	t.Parallel()
	tok := ExpectedOwnerToken("some-owner", "")
	assert.True(t, tok.IsZero(), "empty incarnation must yield the zero token")
	assert.Equal(t, "", tok.Wire(), "zero token Wire() must be the empty string, never a bare owner")
	assert.True(t, OwnerToken{}.IsZero(), "the zero value is the zero token")
	assert.Equal(t, "", OwnerToken{}.Wire(), "the zero value Wire() is empty")
}

// TestOwnerToken_DistinctAcrossIncarnations proves two processes (two NewRegistry
// constructions) mint DIFFERENT tokens for the SAME owner — the fence that lets
// the lease check reject a revived-stale writer that re-registered the same owner
// id in a new process.
func TestOwnerToken_DistinctAcrossIncarnations(t *testing.T) {
	t.Parallel()
	r1 := newNoopRegistry(t)
	r2 := newNoopRegistry(t)
	assert.NotEqual(t, r1.OwnerToken("same-owner").Wire(), r2.OwnerToken("same-owner").Wire(),
		"two processes must mint different tokens for the same owner (the incarnation fence)")
}

// newNoopRegistry builds a Registry with nil KV stores for unit tests that
// only exercise the incarnation field and don't issue any NATS I/O.
func newNoopRegistry(t *testing.T) *Registry {
	t.Helper()
	return NewRegistry(nil, nil, nil)
}
