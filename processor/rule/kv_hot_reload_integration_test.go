//go:build integration

package rule

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// pollReconcileCount polls rcm.ReconcileCount until it reaches at least want or
// deadline expires. Returns the final count.
func pollReconcileCount(rcm *ConfigManager, want int, deadline time.Duration) int {
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if n := rcm.ReconcileCount(); n >= want {
			return n
		}
		time.Sleep(25 * time.Millisecond)
	}
	return rcm.ReconcileCount()
}

// buildHotReloadProcessor creates a minimal rule.Processor with two inline
// rules suitable for hot-reload integration tests. The caller is responsible
// for calling Stop.
func buildHotReloadProcessor(t *testing.T, natsClient *natsclient.Client) *Processor {
	t.Helper()

	cfg := mustTestConfig(t, "rule-test-pack")
	cfg.PackID = "kv-hot-reload-test"
	cfg.Ports = &component.PortConfig{
		Inputs: []component.PortDefinition{
			{
				Name: "entity_events", Config: component.NATSPort{Subject: "events.graph.entity.>", Interface: &component.InterfaceContract{Type: "core.entity.v1"}}, Required: true,
			},
		},
		Outputs: []component.PortDefinition{
			{
				Name: "rule_events", Config: component.NATSPort{Subject: "events.rule.triggered", Interface: &component.InterfaceContract{Type: "core.rule.v1"}}, Required: true,
			},
		},
	}
	cfg.InlineRules = []Definition{
		{
			ID:   "rule_alpha",
			Type: "expression",
			Name: "Alpha",
			Conditions: []expression.ConditionExpression{
				{Field: "test.entity.field", Operator: "eq", Value: "alpha", Required: true},
			},
			Logic:   "and",
			Enabled: true,
		},
		{
			ID:   "rule_beta",
			Type: "expression",
			Name: "Beta",
			Conditions: []expression.ConditionExpression{
				{Field: "test.entity.field", Operator: "eq", Value: "beta", Required: true},
			},
			Logic:   "and",
			Enabled: true,
		},
	}

	proc, err := NewProcessor(natsClient, &cfg)
	require.NoError(t, err)
	proc.SetPlatform(component.PlatformMeta{Org: "c360", Platform: "platform1"})
	require.NoError(t, proc.Initialize())
	return proc
}

// pollRulesCount polls proc.rules until the expected count is reached or
// deadline expires.  Returns true if the target was reached.
func pollRulesCount(proc *Processor, want int, deadline time.Duration) bool {
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		proc.mu.RLock()
		n := len(proc.rules)
		proc.mu.RUnlock()
		if n == want {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// gammaRule is the rule the CRUD path adds in the hot-reload tests.
func gammaRule(id string) Definition {
	return Definition{
		ID:      id,
		Type:    "expression",
		Name:    id,
		Enabled: true,
		Conditions: []expression.ConditionExpression{
			{Field: "test.entity.field", Operator: "eq", Value: id, Required: true},
		},
		Logic: "and",
	}
}

// TestHotReload_SeedIdempotency seeds twice and asserts that no duplicate
// keys are created and that the second seed changes nothing.
func TestHotReload_SeedIdempotency(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKV())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	proc := buildHotReloadProcessor(t, tc.Client)
	require.NoError(t, proc.Start(ctx))
	defer proc.Stop(context.Background()) //nolint:errcheck

	rcm, _ := startRulesFamily(t, ctx, tc)
	targets := []HotReloadTarget{proc}
	rcm.seed(ctx, targets)
	rcm.seed(ctx, targets)

	rules, err := rcm.ListRules(ctx)
	require.NoError(t, err)
	assert.Len(t, rules, 2)
	_, hasAlpha := rules["rule_alpha"]
	_, hasBeta := rules["rule_beta"]
	assert.True(t, hasAlpha, "rule_alpha must be in KV after seed")
	assert.True(t, hasBeta, "rule_beta must be in KV after seed")
}

// TestHotReload_SeedRespectsOperatorEdits verifies that seeding does not
// overwrite a rule an operator has already placed in KV.
func TestHotReload_SeedRespectsOperatorEdits(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKV())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	proc := buildHotReloadProcessor(t, tc.Client)
	require.NoError(t, proc.Start(ctx))
	defer proc.Stop(context.Background()) //nolint:errcheck

	rcm, bucket := startRulesFamily(t, ctx, tc)

	operatorDef := Definition{
		ID:      "rule_alpha",
		Type:    "expression",
		Name:    "Alpha-OperatorEdit",
		Enabled: false,
	}
	data, err := json.Marshal(operatorDef)
	require.NoError(t, err)
	_, err = bucket.Put(ctx, "rules.rule_alpha", data)
	require.NoError(t, err)

	rcm.seed(ctx, []HotReloadTarget{proc})

	got, err := rcm.GetRule(ctx, "rule_alpha")
	require.NoError(t, err)
	assert.Equal(t, "Alpha-OperatorEdit", got.Name, "operator edit must be preserved after seed")
	assert.False(t, got.Enabled, "operator edit (disabled) must be preserved after seed")
}

// TestHotReload_ReconcileFromKV saves a rule through the CRUD path, reconciles
// directly, and asserts the processor's rules map is updated.
func TestHotReload_ReconcileFromKV(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKV())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	proc := buildHotReloadProcessor(t, tc.Client)
	require.NoError(t, proc.Start(ctx))
	defer proc.Stop(context.Background()) //nolint:errcheck

	rcm, _ := startRulesFamily(t, ctx, tc)
	targets := []HotReloadTarget{proc}
	rcm.seed(ctx, targets)
	require.NoError(t, rcm.SaveRule(ctx, "rule_gamma", gammaRule("rule_gamma")))

	require.NoError(t, rcm.reconcile(ctx, targets))

	proc.mu.RLock()
	_, hasGamma := proc.rules["rule_gamma"]
	rulesCount := len(proc.rules)
	gammaDef, hasGammaDef := proc.ruleDefinitions["rule_gamma"]
	proc.mu.RUnlock()

	assert.True(t, hasGamma, "rule_gamma must be in processor.rules after reconcile")
	assert.Equal(t, 3, rulesCount, "processor must have 3 rules after reconcile")
	assert.True(t, hasGammaDef, "rule_gamma must be in processor.ruleDefinitions after hot-reload reconcile")
	assert.Equal(t, "rule_gamma", gammaDef.Name, "hot-reloaded Definition must carry the rule name")
}

// TestHotReload_WatcherPicksUpNewRule drives the whole path: the config
// manager's `rules` family delivers a CRUD write to the rule manager, which
// reconciles it into the running processor, and a delete removes it again.
func TestHotReload_WatcherPicksUpNewRule(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKV())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	proc := buildHotReloadProcessor(t, tc.Client)
	require.NoError(t, proc.Start(ctx))
	defer proc.Stop(context.Background()) //nolint:errcheck

	rcm, _ := startRulesFamily(t, ctx, tc)
	require.NoError(t, rcm.Start(ctx, []HotReloadTarget{proc}))
	defer rcm.Stop() //nolint:errcheck

	require.True(t, pollRulesCount(proc, 2, time.Second), "processor must start with 2 file-loaded rules")

	require.NoError(t, rcm.SaveRule(ctx, "rule_gamma", gammaRule("rule_gamma")))
	assert.True(t, pollRulesCount(proc, 3, 2*time.Second),
		"processor must have 3 rules after the family delivers the KV write")

	require.NoError(t, rcm.DeleteRule(ctx, "rule_gamma"))
	assert.True(t, pollRulesCount(proc, 2, 2*time.Second),
		"processor must drop back to 2 rules after the family delivers the KV delete")

	stopped := make(chan struct{})
	go func() {
		_ = rcm.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(6 * time.Second):
		t.Error("rule manager Stop did not return — the reconcile loop may have leaked")
	}
}

// TestHotReload_DebounceCoalescing asserts that N rapid KV writes within the
// 250ms debounce window produce exactly one additional reconcile, not N.
func TestHotReload_DebounceCoalescing(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKV())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	proc := buildHotReloadProcessor(t, tc.Client)
	require.NoError(t, proc.Start(ctx))
	defer proc.Stop(context.Background()) //nolint:errcheck

	rcm, _ := startRulesFamily(t, ctx, tc)
	require.NoError(t, rcm.Start(ctx, []HotReloadTarget{proc}))
	defer rcm.Stop() //nolint:errcheck

	// Start reconciles once synchronously. Its own seed writes are delivered
	// back by the family and coalesce into exactly one more reconcile, which
	// must settle before the burst is measured.
	require.Equal(t, 1, rcm.ReconcileCount(), "Start must reconcile once before returning")
	initialCount := pollReconcileCount(rcm, 2, 2*time.Second)
	require.Equal(t, 2, initialCount, "the seed writes must coalesce into one reconcile")

	newRuleIDs := []string{"burst_1", "burst_2", "burst_3", "burst_4", "burst_5"}
	for _, id := range newRuleIDs {
		require.NoError(t, rcm.SaveRule(ctx, id, gammaRule(id)))
		// 15ms between writes — 5 × 15ms = 75ms total, well inside the 250ms debounce.
		time.Sleep(15 * time.Millisecond)
	}

	// debounce(250ms) + slack(250ms).
	time.Sleep(500 * time.Millisecond)

	finalCount := rcm.ReconcileCount()
	assert.Equal(t, initialCount+1, finalCount,
		"exactly one additional reconcile must fire for all 5 burst writes (got %d total, want %d)",
		finalCount, initialCount+1)
	assert.True(t, pollRulesCount(proc, 7, 200*time.Millisecond),
		"processor must have 7 rules after coalesced reconcile")
}
