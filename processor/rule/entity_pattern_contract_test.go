package rule

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

func TestConfigValidateEntityWatchContract(t *testing.T) {
	t.Parallel()

	valid := mustTestConfig(t, "entity-pattern-valid-test")
	valid.EntityWatchBuckets = map[string][]string{
		gtypes.BucketEntityStates: {"acme.*.robotics.*.drone.*"},
	}
	require.NoError(t, valid.Validate())

	for name, buckets := range map[string]map[string][]string{
		"unsupported bucket": {"AGENT_LOOPS": {"*.*.*.*.*.*"}},
		"wrong arity":        {gtypes.BucketEntityStates: {"acme.ops.robotics.gcs.drone"}},
		"terminal wildcard":  {gtypes.BucketEntityStates: {"acme.ops.robotics.>"}},
		"duplicate": {
			gtypes.BucketEntityStates: {"acme.*.*.*.*.*", "acme.*.*.*.*.*"},
		},
	} {
		name, buckets := name, buckets
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := mustTestConfig(t, "entity-pattern-invalid-test")
			cfg.EntityWatchBuckets = buckets
			require.Error(t, cfg.Validate())
		})
	}
}

func TestValidateDefinitionRejectsMalformedEntityPattern(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateDefinition(Definition{
		ID: "entity-rule", Entity: EntityConfig{Pattern: "*.*.robotics.*.drone.*"},
	}))
	require.Error(t, ValidateDefinition(Definition{
		ID: "entity-rule", Entity: EntityConfig{Pattern: "*.robotics.>"},
	}))
}

func TestEntityPatternDoesNotBecomeMessageSubscription(t *testing.T) {
	t.Parallel()

	rule, err := NewExpressionRule(testPlatform, "entity-pattern-rule-test", Definition{
		ID: "entity-only", Type: "expression", Enabled: true,
		Entity: EntityConfig{Pattern: "acme.*.robotics.*.drone.*"},
	})
	require.NoError(t, err)
	require.Empty(t, rule.Subscribe())
}

func TestWatcherPatternRejectsBeforeNATSIO(t *testing.T) {
	t.Parallel()

	processor := &Processor{
		logger:           slog.Default(),
		entityWatcherMap: make(map[string]jetstream.KeyWatcher),
	}
	require.Error(t, processor.startWatcherForBucketPattern(
		context.Background(), gtypes.BucketEntityStates, "acme.ops.robotics.>",
	))

	err := processor.startWatcherForBucketPattern(context.Background(), "AGENT_LOOPS", "*.*.*.*.*.*")
	require.Error(t, err)
	var classified *errs.ClassifiedError
	require.True(t, errors.As(err, &classified))
	require.Equal(t, ErrorCodeEntityWatchBucketUnsupported, classified.Code)
	require.Empty(t, processor.entityWatcherMap)
}

func TestUpdateWatchBucketsRejectsBeforeMutation(t *testing.T) {
	t.Parallel()

	original := map[string][]string{gtypes.BucketEntityStates: {"acme.*.*.*.*.*"}}
	cfg := mustTestConfig(t, "entity-pattern-update-test")
	cfg.EntityWatchBuckets = original
	processor := &Processor{
		logger:           slog.Default(),
		config:           &cfg,
		entityWatcherMap: make(map[string]jetstream.KeyWatcher),
	}

	require.Error(t, processor.UpdateWatchBuckets(map[string][]string{
		gtypes.BucketEntityStates: {"acme.ops.robotics.>"},
	}))
	require.Equal(t, original, processor.config.EntityWatchBuckets)
	require.Empty(t, processor.entityWatcherMap)
}
