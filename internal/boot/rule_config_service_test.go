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
// StopAll call it on the test goroutine.
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

func (c *componentsStandIn) Start(ctx context.Context) error {
	c.started <- c.ruleConfigStatus()
	return c.BaseService.Start(ctx)
}

func (c *componentsStandIn) Stop(ctx context.Context) error {
	c.stopped <- c.ruleConfigStatus()
	return c.BaseService.Stop(ctx)
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
