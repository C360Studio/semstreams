package rule

import (
	"testing"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/require"
)

// A rule ID is the member name of the `rules` key family, whose watch
// `rules.*` delivers one token, so configuration validation refuses a dotted
// inline rule ID loudly, naming the rule. Owner ruling on #1188, Q8 (b).
func TestConfigValidateRefusesARuleIDThatIsNotOneKVToken(t *testing.T) {
	t.Parallel()

	withRule := func(id string) Config {
		cfg := mustTestConfig(t, "rule-id-token-test")
		cfg.InlineRules = []Definition{{ID: id, Type: "expression", Name: id, Enabled: true}}
		return cfg
	}

	require.NoError(t, withRule("a-b").Validate())

	err := withRule("a.b").Validate()
	require.Error(t, err)
	require.True(t, errs.IsInvalid(err), "want an invalid error, got %v", err)
	require.ErrorContains(t, err, `rule "a.b" id must be one KV literal token`)
	require.ErrorContains(t, err, "inline_rules[0]")
}
