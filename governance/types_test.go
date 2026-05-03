package governance

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWithContext_RoundTrip verifies that a Context stored via WithContext is
// recovered unchanged by FromContext.
func TestWithContext_RoundTrip(t *testing.T) {
	want := &Context{
		HasPii:               true,
		HasInjection:         false,
		HasContentModeration: true,
		HasRateLimiting:      false,
		ViolationCount:       2,
		MaxSeverity:          SeverityHigh,
		Allowed:              false,
	}
	ctx := WithContext(context.Background(), want)

	got, ok := FromContext(ctx)
	require.True(t, ok, "FromContext must return true when context was stored")
	assert.Equal(t, want, got)
}

// TestFromContext_NotPresent verifies that a fresh context returns nil, false.
func TestFromContext_NotPresent(t *testing.T) {
	got, ok := FromContext(context.Background())
	assert.False(t, ok)
	assert.Nil(t, got)
}

// TestFromContext_NilContext verifies that WithContext(ctx, nil) followed by
// FromContext returns nil, false. A nil *Context means "no governance
// evaluation in scope" — the same semantics as an absent context.
func TestFromContext_NilContext(t *testing.T) {
	ctx := WithContext(context.Background(), nil)
	got, ok := FromContext(ctx)
	assert.False(t, ok, "nil *Context stored in ctx must return ok=false")
	assert.Nil(t, got)
}

// TestFromContext_ForeignKeyIgnored confirms the typed context key prevents
// collisions with other packages that might use identical underlying types
// as keys. Mirrors auth.TestIdentityFromRequest_ForeignCtxKeyIgnored.
func TestFromContext_ForeignKeyIgnored(t *testing.T) {
	type foreignKey struct{}
	ctx := context.WithValue(context.Background(), foreignKey{}, &Context{HasPii: true})

	got, ok := FromContext(ctx)
	assert.False(t, ok, "foreign ctx key must be ignored")
	assert.Nil(t, got)
}
