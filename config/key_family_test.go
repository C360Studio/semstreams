package config

import (
	"context"
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/require"
)

// A registered family is scoped to its own "<prefix>." keys; a family that
// claims one of the Manager's own key prefixes would reach the Manager's keys,
// so construction refuses it before any I/O.
func TestNewConfigManagerRefusesAFamilyOnTheManagersOwnKeys(t *testing.T) {
	t.Parallel()
	cfg := &Config{Platform: PlatformConfig{Org: "acme", ID: "dep"}}
	noop := func(KeyFamilyEntry) {}

	for _, prefix := range []string{"services", "components", "platform", "nats", "model_registry", "platform_identity"} {
		family, err := NewKeyFamily(prefix, noop)
		require.NoError(t, err)
		_, err = NewConfigManager(cfg, new(natsclient.Client), nil, WithKeyFamily(family))
		require.ErrorContains(t, err, "is a key the config manager owns", prefix)
	}

	rules, err := NewKeyFamily("rules", noop)
	require.NoError(t, err)
	_, err = NewConfigManager(cfg, new(natsclient.Client), nil, WithKeyFamily(rules))
	require.NoError(t, err)

	_, err = NewConfigManager(cfg, new(natsclient.Client), nil, nil)
	require.ErrorContains(t, err, "option cannot be nil")
}

// A family's prefix is one KV literal token: its watch is "<prefix>.*".
func TestNewKeyFamilyRefusesAPrefixThatIsNotOneToken(t *testing.T) {
	t.Parallel()
	noop := func(KeyFamilyEntry) {}
	for _, prefix := range []string{"", "a.b", "a*", ">", "a b"} {
		_, err := NewKeyFamily(prefix, noop)
		require.Error(t, err, prefix)
		require.True(t, errs.IsInvalid(err), "prefix %q: %v", prefix, err)
	}
}

// A bound family refuses a nil context and a name that is not one KV literal
// token before it touches the store. The store bound here is a zero KVStore
// with no bucket, so any store access panics: a returned invalid error is the
// proof that no I/O happened.
func TestBoundKeyFamilyRefusesBeforeAnyStoreAccess(t *testing.T) {
	t.Parallel()
	family, err := NewKeyFamily("rules", func(KeyFamilyEntry) {})
	require.NoError(t, err)
	family.bind(&natsclient.KVStore{}, slog.Default())

	var nilCtx context.Context
	requireInvalid := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		require.True(t, errs.IsInvalid(err), "want an invalid error, got %v", err)
	}

	t.Run("nil context", func(t *testing.T) {
		_, err := family.Get(nilCtx, "a")
		requireInvalid(t, err)
		requireInvalid(t, family.Put(nilCtx, "a", []byte(`"a"`)))
		requireInvalid(t, family.Create(nilCtx, "a", []byte(`"a"`)))
		requireInvalid(t, family.Delete(nilCtx, "a"))
		_, err = family.Names(nilCtx)
		requireInvalid(t, err)
	})

	ctx := context.Background()
	for _, name := range []string{"", "dotted.name", "a*", ">", "a b"} {
		t.Run("name "+name, func(t *testing.T) {
			_, err := family.Get(ctx, name)
			requireInvalid(t, err)
			requireInvalid(t, family.Put(ctx, name, []byte(`"a"`)))
			requireInvalid(t, family.Create(ctx, name, []byte(`"a"`)))
			requireInvalid(t, family.Delete(ctx, name))
		})
	}
}
