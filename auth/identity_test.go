package auth

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWithIdentity_RoundTrip verifies that an identity stored via
// WithIdentity is recovered unchanged by IdentityFromContext.
func TestWithIdentity_RoundTrip(t *testing.T) {
	want := &Identity{ID: "alice", Role: "admin", Org: "acme", Source: SourceCtx}
	ctx := WithIdentity(context.Background(), want)

	got, ok := IdentityFromContext(ctx)
	require.True(t, ok, "IdentityFromContext must return true when identity was stored")
	assert.Equal(t, want, got)
}

// TestIdentityFromContext_NotPresent verifies that a fresh context
// returns nil, false.
func TestIdentityFromContext_NotPresent(t *testing.T) {
	got, ok := IdentityFromContext(context.Background())
	assert.False(t, ok)
	assert.Nil(t, got)
}

// TestIdentityFromContext_NilIdentity verifies that WithIdentity(ctx, nil)
// followed by IdentityFromContext returns nil, false. A nil *Identity
// means "no identity in scope" — the same semantics as no identity at all.
func TestIdentityFromContext_NilIdentity(t *testing.T) {
	ctx := WithIdentity(context.Background(), nil)
	got, ok := IdentityFromContext(ctx)
	assert.False(t, ok, "nil *Identity stored in ctx must return ok=false")
	assert.Nil(t, got)
}

// TestIdentityFromRequest_CtxWins verifies that a ctx-attached identity
// takes precedence over the fallbackBody argument.
func TestIdentityFromRequest_CtxWins(t *testing.T) {
	stored := &Identity{ID: "alice", Role: "admin", Org: "acme"}
	r := httptest.NewRequest("POST", "/", nil)
	r = r.WithContext(WithIdentity(r.Context(), stored))

	got := IdentityFromRequest(r, "ignored-body-value")

	require.NotNil(t, got)
	assert.Equal(t, "alice", got.ID)
	assert.Equal(t, "admin", got.Role)
	assert.Equal(t, "acme", got.Org)
	assert.Equal(t, SourceCtx, got.Source, "Source must reflect ctx branch")
}

// TestIdentityFromRequest_BodyClaimWhenNoCtx verifies that the body
// claim is used when ctx has no identity.
func TestIdentityFromRequest_BodyClaimWhenNoCtx(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	// No ctx identity set.

	got := IdentityFromRequest(r, "bob")

	require.NotNil(t, got)
	assert.Equal(t, "bob", got.ID)
	assert.Equal(t, SourceBodyClaim, got.Source, "Source must reflect body_claim branch")
	assert.Empty(t, got.Role, "Role not in body claim — must be empty")
	assert.Empty(t, got.Org, "Org not in body claim — must be empty")
}

// TestIdentityFromRequest_DefaultWhenNeither verifies the final fallback
// to DefaultIdentityID when neither ctx nor body has a value.
func TestIdentityFromRequest_DefaultWhenNeither(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)

	got := IdentityFromRequest(r, "")

	require.NotNil(t, got, "IdentityFromRequest must never return nil")
	assert.Equal(t, DefaultIdentityID, got.ID)
	assert.Equal(t, SourceDefault, got.Source)
}

// TestIdentityFromRequest_BodyEmptyDoesNotInheritCtxBody is the
// security-shaped regression guard. When ctx has an identity and the
// body claim is empty, the result must be the ctx identity — the empty
// body does NOT silently inherit from ctx meaning "no body = use ctx
// anyway." The direction is ctx > body, not body-empty = ctx fallback.
//
// In other words: empty body falls through TO ctx (ctx wins), not
// AWAY FROM ctx (body-empty does not demote ctx to default).
func TestIdentityFromRequest_BodyEmptyDoesNotInheritCtxBody(t *testing.T) {
	stored := &Identity{ID: "ctx-user", Role: "admin", Org: "acme"}
	r := httptest.NewRequest("POST", "/", nil)
	r = r.WithContext(WithIdentity(r.Context(), stored))

	// body claim is empty — should not override ctx
	got := IdentityFromRequest(r, "")

	require.NotNil(t, got)
	assert.Equal(t, "ctx-user", got.ID, "ctx identity must win even when body is empty")
	assert.Equal(t, SourceCtx, got.Source)
}

// TestIdentityFromRequest_SourceTracking is a table-driven test that
// checks Source is correctly set to the winning branch for all three cases.
func TestIdentityFromRequest_SourceTracking(t *testing.T) {
	tests := []struct {
		name         string
		ctxIdentity  *Identity // nil = no ctx identity
		bodyFallback string
		wantSource   string
		wantID       string
	}{
		{
			name:        "ctx wins",
			ctxIdentity: &Identity{ID: "ctx-user"},
			wantSource:  SourceCtx,
			wantID:      "ctx-user",
		},
		{
			name:         "body claim when no ctx",
			ctxIdentity:  nil,
			bodyFallback: "body-user",
			wantSource:   SourceBodyClaim,
			wantID:       "body-user",
		},
		{
			name:         "default when neither",
			ctxIdentity:  nil,
			bodyFallback: "",
			wantSource:   SourceDefault,
			wantID:       DefaultIdentityID,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", nil)
			if tc.ctxIdentity != nil {
				r = r.WithContext(WithIdentity(r.Context(), tc.ctxIdentity))
			}

			got := IdentityFromRequest(r, tc.bodyFallback)

			require.NotNil(t, got)
			assert.Equal(t, tc.wantID, got.ID)
			assert.Equal(t, tc.wantSource, got.Source)
		})
	}
}

// TestIdentityFromRequest_ForeignCtxKeyIgnored confirms the typed
// context key prevents collisions with other packages.
func TestIdentityFromRequest_ForeignCtxKeyIgnored(t *testing.T) {
	type foreignKey struct{}
	r := httptest.NewRequest("POST", "/", nil)
	r = r.WithContext(context.WithValue(r.Context(), foreignKey{}, &Identity{ID: "imposter"}))

	got := IdentityFromRequest(r, "")

	require.NotNil(t, got)
	assert.Equal(t, DefaultIdentityID, got.ID, "foreign ctx key must be ignored")
}
