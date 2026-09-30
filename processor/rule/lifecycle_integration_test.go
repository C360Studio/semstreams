//go:build integration

package rule_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/rule"
	"github.com/stretchr/testify/require"
)

// ruleLifecycleObservation forwards every lifecycle call to the real
// processor. Stop records only what this caller can observe: finite caller
// authority and a completed invocation, not a full internal join after abort.
type ruleLifecycleObservation struct {
	component.LifecycleComponent
	mu           sync.Mutex
	stopCalls    int
	stopReturned int
	stopFinite   bool
}

func (o *ruleLifecycleObservation) Stop(ctx context.Context) error {
	if ctx == nil { // The portable nil-context probe must reach the real component.
		return o.LifecycleComponent.Stop(ctx)
	}
	_, finite := ctx.Deadline()
	o.mu.Lock()
	o.stopCalls++
	o.stopFinite = o.stopFinite && finite
	o.mu.Unlock()
	err := o.LifecycleComponent.Stop(ctx)
	o.mu.Lock()
	o.stopReturned++
	o.mu.Unlock()
	return err
}

func (o *ruleLifecycleObservation) observed() (int, int, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.stopCalls, o.stopReturned, o.stopFinite
}

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
	var processors []*ruleLifecycleObservation
	var factoryErrors []error
	// Registered after NewTestClient so this assertion runs before its NATS
	// teardown. The shared suite owns and finalizes each returned instance.
	t.Cleanup(func() {
		mu.Lock()
		owned := append([]*ruleLifecycleObservation(nil), processors...)
		errors := append([]error(nil), factoryErrors...)
		mu.Unlock()
		for _, err := range errors {
			t.Errorf("rule lifecycle factory: %v", err)
		}
		for i, processor := range owned {
			calls, returned, finite := processor.observed()
			if calls < 1 || calls > 2 || returned != calls || !finite {
				t.Errorf("rule lifecycle instance %d: nonnil Stop calls=%d returns=%d finite=%t", i, calls, returned, finite)
			}
		}
	})

	component.StandardLifecycleTests(t, func() component.LifecycleComponent {
		created, err := rule.CreateRuleProcessor(rawConfig, deps)
		if err != nil {
			mu.Lock()
			factoryErrors = append(factoryErrors, err)
			mu.Unlock()
			return nil
		}
		processor, ok := created.(component.LifecycleComponent)
		if !ok {
			mu.Lock()
			factoryErrors = append(factoryErrors, fmt.Errorf("factory returned %T without LifecycleComponent", created))
			mu.Unlock()
			return nil
		}
		observed := &ruleLifecycleObservation{LifecycleComponent: processor, stopFinite: true}
		mu.Lock()
		processors = append(processors, observed)
		mu.Unlock()
		return observed
	})
}
