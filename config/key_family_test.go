package config

import (
	"testing"

	"github.com/c360studio/semstreams/natsclient"
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
