package rule

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/types"
)

// TestInitializeStateTrackerRefusesAbsentDeploymentAuthority pins the third of
// the LOUD paths, on the seam that is MORE accessible than the factory one
// CreateRuleProcessor guards.
//
// NewProcessor and NewProcessorWithMetrics are exported, take no authority, and
// never populate rp.platform; SetPlatform is an OPTIONAL setter whose doc says
// "called by the component factory" — a convention, not a guard. So an adopter
// outside this repository can reach initializeStateTracker with a zero
// PlatformMeta and a live NATS client, which is exactly the state
// CreateRuleProcessor refuses one hop earlier.
//
// Left unguarded, that executor answers foreignFiringEntity TRUE for every
// firing entity (the guard fails closed by design), so every framework write to
// a firing entity — rule.task.spawned under any run_scope, plus the run-anchor
// pair under run_scope=new — is skipped and merely counted. A rule chained off
// $entity.triple.rule.spawned_task stops firing with no error anywhere.
//
// The refusal must therefore be the FIRST thing initializeStateTracker does, and
// the assertions below pin the CLASS of the error, not merely its presence: an
// empty natsclient.Client fails at JetStream() with a TRANSIENT error a few
// lines later, so `require.Error` alone would pass with the refusal deleted.
func TestInitializeStateTrackerRefusesAbsentDeploymentAuthority(t *testing.T) {
	for _, tc := range []struct {
		name        string
		setPlatform bool
		platform    types.PlatformMeta
	}{
		{name: "setter never called", setPlatform: false},
		{name: "org absent", setPlatform: true, platform: types.PlatformMeta{Platform: "dep1"}},
		{name: "platform absent", setPlatform: true, platform: types.PlatformMeta{Org: "acme"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, err := NewConfig("direct-constructor-pin")
			require.NoError(t, err)

			processor, err := NewProcessor(&natsclient.Client{}, &config)
			require.NoError(t, err,
				"the exported constructor does not take the authority — that IS the seam under test")
			if tc.setPlatform {
				processor.SetPlatform(tc.platform)
			}

			err = processor.initializeStateTracker(t.Context())
			require.Error(t, err,
				"a NATS-backed processor with no deployment authority must refuse to build a writing executor")
			assert.True(t, errs.IsInvalid(err),
				"the refusal must be classified INVALID like CreateRuleProcessor's, not the transient "+
					"JetStream error the next line would produce: %v", err)
			assert.Contains(t, err.Error(), "platform.org",
				"the refusal must name what an operator has to fix")
			assert.Nil(t, processor.actionExecutor,
				"no executor may be built alongside the refusal")
		})
	}
}

// TestInitializeStateTrackerPastTheAuthorityCheckFailsElsewhere is the negative
// space: the refusal is about the ABSENT pair specifically. With a concrete
// authority the same fixture gets PAST it and fails on the empty client's
// JetStream context instead — a different error, differently classified.
func TestInitializeStateTrackerPastTheAuthorityCheckFailsElsewhere(t *testing.T) {
	config, err := NewConfig("direct-constructor-pin")
	require.NoError(t, err)

	processor, err := NewProcessor(&natsclient.Client{}, &config)
	require.NoError(t, err)
	processor.SetPlatform(types.PlatformMeta{Org: "acme", Platform: "dep1"})

	err = processor.initializeStateTracker(t.Context())
	require.Error(t, err, "the empty client cannot supply a JetStream context")
	assert.NotContains(t, err.Error(), "platform.org",
		"a processor carrying the authority must not be refused for lacking it")
	assert.False(t, errs.IsInvalid(err),
		"the JetStream failure is transient, not an invalid-configuration refusal: %v", err)
}
