//go:build integration

package boot

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/internal/bootstrapobservability"
	"github.com/c360studio/semstreams/natsclient"
	rulepkg "github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// observedRuleProcessor is a real rule processor whose ApplyConfigUpdate also
// reports the rule set it applied, so the test synchronizes on the apply
// itself rather than polling or sleeping.
type observedRuleProcessor struct {
	*rulepkg.Processor
	applied chan map[string]any
}

func (o *observedRuleProcessor) ApplyConfigUpdate(changes map[string]any) error {
	err := o.Processor.ApplyConfigUpdate(changes)
	if err == nil {
		rules, _ := changes["rules"].(map[string]any)
		o.applied <- rules
	}
	return err
}

func hotReloadRule(id string) rulepkg.Definition {
	return rulepkg.Definition{
		ID:      id,
		Type:    "expression",
		Name:    id,
		Enabled: true,
		Conditions: []expression.ConditionExpression{
			{Field: "$message.kind", Operator: "eq", Value: id, Required: true},
		},
		Logic: "and",
	}
}

func startedRuleProcessor(t *testing.T, ctx context.Context, client *natsclient.Client) *rulepkg.Processor {
	t.Helper()
	cfg, err := rulepkg.NewConfig("root-hot-reload-test")
	require.NoError(t, err)
	cfg.Ports = &component.PortConfig{
		Inputs: []component.PortDefinition{{
			Name: "entity_events", Required: true,
			Config: component.NATSPort{Subject: "events.graph.entity.>", Interface: &component.InterfaceContract{Type: "core.entity.v1"}},
		}},
		Outputs: []component.PortDefinition{{
			Name: "rule_events", Required: true,
			Config: component.NATSPort{Subject: "events.rule.triggered", Interface: &component.InterfaceContract{Type: "core.rule.v1"}},
		}},
	}
	cfg.InlineRules = []rulepkg.Definition{hotReloadRule("file_rule")}
	proc, err := rulepkg.NewProcessor(client, &cfg)
	require.NoError(t, err)
	proc.SetPlatform(component.PlatformMeta{Org: "c360", Platform: "platform1"})
	require.NoError(t, proc.Initialize())
	require.NoError(t, proc.Start(ctx))
	t.Cleanup(func() { _ = proc.Stop(context.Background()) })
	return proc
}

// nextApplied waits for the processor's next successful apply.
func nextApplied(t *testing.T, applied <-chan map[string]any) map[string]any {
	t.Helper()
	select {
	case rules := <-applied:
		return rules
	case <-time.After(5 * time.Second):
		t.Fatal("the rule processor's ApplyConfigUpdate was not called within 5s")
		return nil
	}
}

// TestRootRuleManagerHotReloadsIntoTheProcessor proves the parts the root
// composes, assembled by hand in the root's shape: the rule ConfigManager
// registers its family with the config manager through
// StartValidatedConfigManager, a ruleHotReloadRuntime starts it after the
// services, and a rule saved through the manager's CRUD reaches the running
// processor's ApplyConfigUpdate. It passes the processor as the target
// directly, so it does not prove how the production root finds its targets;
// TestBinaryBootOrder (internal/maxdelivery) pins that wiring in run.go and
// TestComponentsImplementingReturnsTheBuiltComponentsThatImplementTheSeam
// (service) proves the walk it uses.
//
// spec: component-runtime-config / Config Manager delivers a registered key family to its owner
func TestRootRuleManagerHotReloadsIntoTheProcessor(t *testing.T) {
	testNATS := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ruleManager, err := rulepkg.NewConfigManager(logger)
	require.NoError(t, err)
	configManager, _, err := bootstrapobservability.StartValidatedConfigManager(
		ctx, bootstrapIntegrationConfig(testNATS.URL), testNATS.Client, logger,
		config.WithKeyFamily(ruleManager.KeyFamily()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = configManager.Stop(5 * time.Second) })

	proc := &observedRuleProcessor{
		Processor: startedRuleProcessor(t, ctx, testNATS.Client),
		applied:   make(chan map[string]any, 8),
	}
	runtime := &ruleHotReloadRuntime{
		runtimeManager: recordingServices{&orderRecorder{}},
		rules:          ruleManager,
		targets:        []rulepkg.HotReloadTarget{proc},
	}
	require.NoError(t, runtime.StartAll(ctx))

	// Start seeds the file rule, then reconciles the full family once.
	seeded := nextApplied(t, proc.applied)
	require.Contains(t, seeded, "file_rule", "the first reconcile must carry the seeded file rule")
	stored, err := ruleManager.GetRule(ctx, "file_rule")
	require.NoError(t, err)
	require.Equal(t, "file_rule", stored.ID)

	require.NoError(t, ruleManager.SaveRule(ctx, "x", hotReloadRule("x")))
	for {
		rules := nextApplied(t, proc.applied)
		if _, ok := rules["x"]; ok {
			require.Contains(t, rules, "file_rule", "a reconcile is a full replace and must keep the file rule")
			break
		}
	}
	_, loaded := proc.LoadedRuleDefinitions()["x"]
	require.True(t, loaded, "the running processor must hold the hot-reloaded rule")

	require.NoError(t, runtime.StopAll(ctx))
}
