package boot

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	rulepkg "github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/service"
)

// ruleConfigStopBudget bounds StopAll in these tests: the production root's
// --shutdown-timeout default (internal/boot/flags.go, 30s), the context
// service.Manager stops the "rule-config" service under.
const ruleConfigStopBudget = 30 * time.Second

// componentsStandIn stands in for the component manager. At its own Start and
// Stop it records the "rule-config" service's status: where the rule
// ConfigManager sits in the manager's start and stop sequence. StartAll and
// StopAll call it on the test goroutine. It owns no runtime, so its Start and
// Stop only record: the embedded BaseService supplies the rest of the Service
// surface and is never started, which keeps this recorder out of the cleanup
// root census as a lifecycle owner (scripts/check-cleanup-roots.sh).
type componentsStandIn struct {
	*service.BaseService
	t       *testing.T
	manager *service.Manager
	started chan service.Status
	stopped chan service.Status
}

func newComponentsStandIn(t *testing.T, manager *service.Manager) *componentsStandIn {
	return &componentsStandIn{
		BaseService: service.NewBaseServiceWithOptions("component-manager", nil),
		t:           t,
		manager:     manager,
		started:     make(chan service.Status, 1),
		stopped:     make(chan service.Status, 1),
	}
}

func (c *componentsStandIn) ruleConfigStatus() service.Status {
	svc, ok := c.manager.GetService(ruleConfigServiceName)
	require.True(c.t, ok, "the rule-config service must be registered before StartAll")
	return svc.Status()
}

func (c *componentsStandIn) Start(context.Context) error {
	c.started <- c.ruleConfigStatus()
	return nil
}

func (c *componentsStandIn) Stop(context.Context) error {
	c.stopped <- c.ruleConfigStatus()
	return nil
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// Design D3 of config-bucket-authority-namespace by service registration:
// registered right after the component manager, the rule ConfigManager starts
// after the components and stops before them. Owner ruling on #1188, docket 6
// Q11 (b-full).
//
// spec: component-runtime-config / Config Manager delivers a registered key family to its owner
func TestRuleConfigServiceStartsAfterAndStopsBeforeTheComponents(t *testing.T) {
	manager := service.NewServiceManager(service.NewServiceRegistry())
	components := newComponentsStandIn(t, manager)
	require.NoError(t, manager.RegisterInstance("component-manager", components))
	rules, err := rulepkg.NewConfigManager(quietLogger())
	require.NoError(t, err)
	require.NoError(t, registerRuleConfigService(manager, rules, quietLogger()))

	require.NoError(t, manager.StartAll(t.Context()))
	require.NotEqual(t, service.StatusRunning, <-components.started,
		"start order must be [components, rules]: rule-config was already running when the components started")
	ruleConfig, ok := manager.GetService(ruleConfigServiceName)
	require.True(t, ok)
	require.Equal(t, service.StatusRunning, ruleConfig.Status())

	stopCtx, cancel := context.WithTimeout(context.Background(), ruleConfigStopBudget)
	defer cancel()
	require.NoError(t, manager.StopAll(stopCtx))
	require.Equal(t, service.StatusStopped, <-components.stopped,
		"stop order must be [rules, components]: rule-config had not stopped when the components stopped")
}

// Registered before the component manager, the rule ConfigManager would bind
// no processors and start first, so registration refuses and admits nothing.
func TestRegisterRuleConfigServiceRefusesBeforeTheComponentManager(t *testing.T) {
	manager := service.NewServiceManager(service.NewServiceRegistry())
	rules, err := rulepkg.NewConfigManager(quietLogger())
	require.NoError(t, err)
	require.Error(t, registerRuleConfigService(manager, rules, quietLogger()))
	_, registered := manager.GetService(ruleConfigServiceName)
	require.False(t, registered, "a refused registration must admit nothing")
}

// adapterRepeatedStopTrials is how many canceled-context Stops the adapter
// regression makes after a completed Stop. The defect is a select race that Go
// resolves at random, so each trial fails with probability about one half
// when the rule manager's nonblocking fence check is missing; 200 trials leave
// a false pass at about 2^-200.
const adapterRepeatedStopTrials = 200

// idleTarget is a hot-reload target that accepts every update.
type idleTarget struct{}

func (idleTarget) LoadedRuleDefinitions() map[string]rulepkg.Definition { return nil }
func (idleTarget) ValidateConfigUpdate(map[string]any) error            { return nil }
func (idleTarget) ApplyConfigUpdate(map[string]any) error               { return nil }

// A completed rule-config Stop repeated under a canceled context is a nil
// no-op through the adapter, so the service manager never sees a false
// "reconcile loop still running" error. Codex round 2 on #1188.
//
// spec: component-runtime-config / Config Manager delivers a registered key family to its owner
func TestRuleConfigServiceRepeatedStopAfterCompletionIgnoresACanceledContext(t *testing.T) {
	rules, err := rulepkg.NewConfigManager(quietLogger())
	require.NoError(t, err)
	ruleConfig := newRuleConfigService(rules, []rulepkg.HotReloadTarget{idleTarget{}}, quietLogger())
	startCtx, cancelStart := context.WithCancel(context.Background())
	defer cancelStart()
	require.NoError(t, ruleConfig.Start(startCtx))
	stopCtx, cancelStop := context.WithTimeout(context.Background(), ruleConfigStopBudget)
	defer cancelStop()
	require.NoError(t, ruleConfig.Stop(stopCtx))

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for trial := range adapterRepeatedStopTrials {
		require.NoError(t, ruleConfig.Stop(canceled),
			"trial %d of %d: completed repeated Stop with a canceled context", trial+1, adapterRepeatedStopTrials)
	}
	require.Equal(t, service.StatusStopped, ruleConfig.Status())
}
