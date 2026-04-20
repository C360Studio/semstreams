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

// buildHotReloadProcessor creates a minimal rule.Processor with two inline
// rules suitable for hot-reload integration tests. The caller is responsible
// for calling Stop.
func buildHotReloadProcessor(t *testing.T, natsClient *natsclient.Client) *Processor {
	t.Helper()

	cfg := DefaultConfig()
	cfg.Ports = &component.PortConfig{
		Inputs: []component.PortDefinition{
			{
				Name:      "entity_events",
				Type:      "nats",
				Subject:   "events.graph.entity.>",
				Interface: "core.entity.v1",
				Required:  true,
			},
		},
		Outputs: []component.PortDefinition{
			{
				Name:      "rule_events",
				Type:      "nats",
				Subject:   "events.rule.triggered",
				Interface: "core.rule.v1",
				Required:  true,
			},
		},
	}
	cfg.InlineRules = []Definition{
		{
			ID:   "rule_alpha",
			Type: "expression",
			Name: "Alpha",
			Conditions: []expression.ConditionExpression{
				{Field: "test.field", Operator: "eq", Value: "alpha", Required: true},
			},
			Logic:   "and",
			Enabled: true,
		},
		{
			ID:   "rule_beta",
			Type: "expression",
			Name: "Beta",
			Conditions: []expression.ConditionExpression{
				{Field: "test.field", Operator: "eq", Value: "beta", Required: true},
			},
			Logic:   "and",
			Enabled: true,
		},
	}

	proc, err := NewProcessor(natsClient, &cfg)
	require.NoError(t, err)
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

// TestHotReload_SeedIdempotency calls SeedFromRuntime twice and asserts that
// no duplicate KV keys are created and that no error is returned.
func TestHotReload_SeedIdempotency(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKV())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	proc := buildHotReloadProcessor(t, tc.Client)
	// Start so ruleConfigs is populated.
	require.NoError(t, proc.Start(ctx))
	defer proc.Stop(5 * time.Second) //nolint:errcheck

	rcm := NewConfigManager(proc, nil, nil)
	require.NoError(t, rcm.InitializeKVStore(tc.Client))

	// First seed.
	require.NoError(t, rcm.SeedFromRuntime(ctx))
	// Second seed — must not fail even if keys already exist.
	require.NoError(t, rcm.SeedFromRuntime(ctx))

	rules, err := rcm.ListRules(ctx)
	require.NoError(t, err)
	// Exactly two rules seeded — no duplicates possible (KV keys are unique).
	assert.Len(t, rules, 2)
	_, hasAlpha := rules["rule_alpha"]
	_, hasBeta := rules["rule_beta"]
	assert.True(t, hasAlpha, "rule_alpha must be in KV after seed")
	assert.True(t, hasBeta, "rule_beta must be in KV after seed")
}

// TestHotReload_SeedRespectsOperatorEdits verifies that SeedFromRuntime does
// not overwrite a rule that an operator has already placed in KV.
func TestHotReload_SeedRespectsOperatorEdits(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKV())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	proc := buildHotReloadProcessor(t, tc.Client)
	require.NoError(t, proc.Start(ctx))
	defer proc.Stop(5 * time.Second) //nolint:errcheck

	rcm := NewConfigManager(proc, nil, nil)
	require.NoError(t, rcm.InitializeKVStore(tc.Client))

	// Pre-populate KV with an operator-modified version of rule_alpha before seeding.
	operatorDef := Definition{
		ID:      "rule_alpha",
		Type:    "expression",
		Name:    "Alpha-OperatorEdit",
		Enabled: false,
	}
	data, err := json.Marshal(operatorDef)
	require.NoError(t, err)
	_, err = rcm.kvStore.Put(ctx, "rules.rule_alpha", data)
	require.NoError(t, err)

	// SeedFromRuntime must not overwrite the operator edit.
	require.NoError(t, rcm.SeedFromRuntime(ctx))

	got, err := rcm.GetRule(ctx, "rule_alpha")
	require.NoError(t, err)
	assert.Equal(t, "Alpha-OperatorEdit", got.Name, "operator edit must be preserved after seed")
	assert.False(t, got.Enabled, "operator edit (disabled) must be preserved after seed")
}

// TestHotReload_ReconcileFromKV writes a rule via a separate KVStore handle,
// calls reconcileFromKV directly, and asserts the processor's rules map is
// updated without going through the watcher goroutine.
func TestHotReload_ReconcileFromKV(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKV())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	proc := buildHotReloadProcessor(t, tc.Client)
	require.NoError(t, proc.Start(ctx))
	defer proc.Stop(5 * time.Second) //nolint:errcheck

	rcm := NewConfigManager(proc, nil, nil)
	require.NoError(t, rcm.InitializeKVStore(tc.Client))

	// Seed the two file-loaded rules so KV is not empty.
	require.NoError(t, rcm.SeedFromRuntime(ctx))

	// Write a third rule via a separate manager (simulating the CRUD tool path).
	crudMgr := NewConfigManager(nil, nil, nil)
	require.NoError(t, crudMgr.InitializeKVStore(tc.Client))
	require.NoError(t, crudMgr.SaveRule(ctx, "rule_gamma", Definition{
		ID:      "rule_gamma",
		Type:    "expression",
		Name:    "Gamma",
		Enabled: true,
		Conditions: []expression.ConditionExpression{
			{Field: "test.field", Operator: "eq", Value: "gamma", Required: true},
		},
		Logic: "and",
	}))

	// Directly call reconcileFromKV (not via watcher).
	require.NoError(t, rcm.reconcileFromKV(ctx))

	proc.mu.RLock()
	_, hasGamma := proc.rules["rule_gamma"]
	rulesCount := len(proc.rules)
	proc.mu.RUnlock()

	assert.True(t, hasGamma, "rule_gamma must be in processor.rules after reconcile")
	assert.Equal(t, 3, rulesCount, "processor must have 3 rules after reconcile")
}

// TestHotReload_WatcherPicksUpNewRule is the primary end-to-end hot-reload
// integration test. It boots a Processor, writes a rule via a separate KVStore
// handle, and asserts the processor.rules map updates within 500 ms. It then
// deletes the rule and asserts the map shrinks back.
func TestHotReload_WatcherPicksUpNewRule(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKV())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	proc := buildHotReloadProcessor(t, tc.Client)
	require.NoError(t, proc.Start(ctx))
	defer proc.Stop(5 * time.Second) //nolint:errcheck

	// Allow the hot-reload watcher to settle (seed + initial snapshot).
	time.Sleep(400 * time.Millisecond)

	proc.mu.RLock()
	initialCount := len(proc.rules)
	proc.mu.RUnlock()
	require.Equal(t, 2, initialCount, "processor must start with 2 file-loaded rules")

	// Write a third rule via a separate CRUD manager (simulates agent tool write).
	crudMgr := NewConfigManager(nil, nil, nil)
	require.NoError(t, crudMgr.InitializeKVStore(tc.Client))
	require.NoError(t, crudMgr.SaveRule(ctx, "rule_gamma", Definition{
		ID:      "rule_gamma",
		Type:    "expression",
		Name:    "Gamma",
		Enabled: true,
		Conditions: []expression.ConditionExpression{
			{Field: "test.field", Operator: "eq", Value: "gamma", Required: true},
		},
		Logic: "and",
	}))

	// Poll up to 500 ms for the processor to pick up the new rule.
	// Debounce is 250 ms so expect delivery by ~300–400 ms.
	assert.True(t, pollRulesCount(proc, 3, 500*time.Millisecond),
		"processor must have 3 rules within 500ms of KV write")

	// Delete the third rule and assert it disappears.
	require.NoError(t, crudMgr.DeleteRule(ctx, "rule_gamma"))

	assert.True(t, pollRulesCount(proc, 2, 500*time.Millisecond),
		"processor must drop back to 2 rules within 500ms of KV delete")

	// Stop and assert the watcher goroutine exits cleanly (Stop waits via WaitGroup).
	doneCh := make(chan struct{})
	go func() {
		proc.Stop(5 * time.Second) //nolint:errcheck
		close(doneCh)
	}()
	select {
	case <-doneCh:
		// clean exit
	case <-time.After(6 * time.Second):
		t.Error("Stop() did not return within 6 seconds — watcher goroutine may have leaked")
	}
}
