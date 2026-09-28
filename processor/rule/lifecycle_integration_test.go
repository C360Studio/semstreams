//go:build integration

package rule_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/rule"
	"github.com/stretchr/testify/require"
)

func TestIntegration_RuleStandardLifecycle(t *testing.T) {
	// The suite owns one fresh server. Its concurrent fresh processors share
	// readiness writes, but assert lifecycle results, not readiness values.
	tc := natsclient.NewTestClient(t, natsclient.WithKVBuckets(graph.BucketEntityStates))
	config, err := rule.NewConfig("lifecycle-suite")
	require.NoError(t, err)
	// The default KV input port alone does not acquire an entity watcher.
	config.EntityWatchBuckets = map[string][]string{
		graph.BucketEntityStates: {"c360.lifecycle.test.*.*.*"},
	}
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)
	deps := component.Dependencies{
		NATSClient: tc.Client,
		Platform:   component.PlatformMeta{Org: "c360", Platform: "lifecycle"},
	}

	var mu sync.Mutex
	var processors []component.LifecycleComponent
	// Registered after NewTestClient: even an early suite failure gets a Stop
	// attempt before the NATS substrate is torn down. One cooperative budget
	// covers the whole cohort, rather than multiplying it by the instance count.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		mu.Lock()
		owned := append([]component.LifecycleComponent(nil), processors...)
		mu.Unlock()
		for i, processor := range owned {
			if err := ctx.Err(); err != nil {
				t.Errorf("rule lifecycle cleanup: %d processors unattempted after aggregate budget: %v", len(owned)-i, err)
				return
			}
			if err := processor.Stop(ctx); err != nil {
				t.Errorf("rule lifecycle cleanup: processor %d of %d: %v", i+1, len(owned), err)
			}
		}
	})

	component.StandardLifecycleTests(t, func() component.LifecycleComponent {
		created, err := rule.CreateRuleProcessor(rawConfig, deps)
		if err != nil {
			// The suite also invokes this factory from worker goroutines.
			t.Errorf("create rule lifecycle fixture: %v", err)
			return nil
		}
		processor, ok := created.(component.LifecycleComponent)
		if !ok {
			t.Errorf("rule factory returned %T, which does not implement LifecycleComponent", created)
			return nil
		}
		mu.Lock()
		processors = append(processors, processor)
		mu.Unlock()
		return processor
	})
}
