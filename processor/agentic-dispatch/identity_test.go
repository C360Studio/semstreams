package agenticdispatch

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIdentityFromRequest_CtxWins(t *testing.T) {
	r := httptest.NewRequest("POST", "/loops/loop-1/approval", nil)
	r = r.WithContext(WithIdentity(r.Context(), "alice@example.com"))

	got := IdentityFromRequest(r, "ignored-body-value")
	assert.Equal(t, "alice@example.com", got, "ctx-supplied identity must win over body")
}

func TestIdentityFromRequest_BodyFallback(t *testing.T) {
	r := httptest.NewRequest("POST", "/loops/loop-1/approval", nil)
	// No ctx identity set.

	got := IdentityFromRequest(r, "bob")
	assert.Equal(t, "bob", got, "body fallback used when ctx empty")
}

func TestIdentityFromRequest_DefaultFallback(t *testing.T) {
	r := httptest.NewRequest("POST", "/loops/loop-1/approval", nil)

	got := IdentityFromRequest(r, "")
	assert.Equal(t, DefaultIdentity, got, "default applied when ctx empty AND body empty")
}

// TestIdentityFromRequest_EmptyCtxFallsThroughToBody is the
// regression guard for the resolution order: a ctx value set to ""
// must NOT short-circuit to default — it should be treated the same
// as "ctx not set" and fall through to body. Otherwise middleware
// that explicitly sets WithIdentity(ctx, "") would silently override
// the body value with the default.
func TestIdentityFromRequest_EmptyCtxFallsThroughToBody(t *testing.T) {
	r := httptest.NewRequest("POST", "/loops/loop-1/approval", nil)
	r = r.WithContext(WithIdentity(r.Context(), ""))

	got := IdentityFromRequest(r, "carol")
	assert.Equal(t, "carol", got, "empty ctx must fall through to body, not short-circuit to default")
}

// TestIdentityFromRequest_ForeignCtxKeyIgnored confirms the typed
// context key prevents collisions with other packages that might
// stash a string under a context key called "identity" or similar.
func TestIdentityFromRequest_ForeignCtxKeyIgnored(t *testing.T) {
	type otherPackageKey struct{}

	r := httptest.NewRequest("POST", "/loops/loop-1/approval", nil)
	r = r.WithContext(context.WithValue(r.Context(), otherPackageKey{}, "imposter"))

	got := IdentityFromRequest(r, "")
	assert.Equal(t, DefaultIdentity, got, "different ctx-key type must be ignored")
}

// TestIdentityFromRequest_BodyEmptyDoesNotInheritCtxBody is the
// security-shaped regression guard for the precedence order. When
// middleware lands (ADR-030), an authenticated identity in ctx
// will dominate. But a body with explicit `user_id: ""` must NOT
// silently inherit the ctx identity if the caller intended to send
// no body claim — the helper's contract is "ctx wins over absent
// body," and an empty-string body is "absent claim," not "use ctx."
//
// In practice today this distinction is moot (no middleware sets
// ctx), but the precedence shape should never invert. If a future
// refactor makes body=="" silently inherit ctx, that's a privilege-
// escalation-shaped surprise this test catches.
func TestIdentityFromRequest_BodyEmptyDoesNotInheritCtxBody(t *testing.T) {
	r := httptest.NewRequest("POST", "/loops/loop-1/approval", nil)
	r = r.WithContext(WithIdentity(r.Context(), "ctx-authenticated-user"))

	got := IdentityFromRequest(r, "")
	assert.Equal(t, "ctx-authenticated-user", got,
		"empty body falls through to ctx (not the other way around)")
}

func TestWithIdentity_Roundtrip(t *testing.T) {
	ctx := context.Background()
	ctx = WithIdentity(ctx, "dave")

	r := httptest.NewRequest("POST", "/loops/loop-1/approval", nil).WithContext(ctx)
	got := IdentityFromRequest(r, "")
	assert.Equal(t, "dave", got)
}
