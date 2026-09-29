//go:build integration

package boot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/internal/bootstrapobservability"
	"github.com/c360studio/semstreams/natsclient"
	rulepkg "github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/service"
)

// serviceStopDeadlineUnderTest is the Stop bound exercised through the
// adapter: short, so a Stop that honors it returns long before
// serviceStopReturnTolerance.
const serviceStopDeadlineUnderTest = 100 * time.Millisecond

// serviceStopReturnTolerance is how long the test waits for a bounded Stop
// before declaring it unbounded, generous against CI scheduling.
const serviceStopReturnTolerance = 5 * time.Second

// heldApplyTarget holds its first ApplyConfigUpdate until released, ignoring
// every context, the way a wedged rule processor would.
type heldApplyTarget struct {
	entered chan struct{}
	release chan struct{}
}

func (h *heldApplyTarget) LoadedRuleDefinitions() map[string]rulepkg.Definition { return nil }
func (h *heldApplyTarget) ValidateConfigUpdate(map[string]any) error            { return nil }
func (h *heldApplyTarget) ApplyConfigUpdate(map[string]any) error {
	close(h.entered)
	<-h.release
	return nil
}

// The "rule-config" service's Stop is the rule ConfigManager's bounded Stop:
// when its context ends while the manager's work is held, it returns that
// error and does not report stopped; once the work is released, a second Stop
// completes. Review round 4 on #1188, MEDIUM-1.
//
// spec: component-runtime-config / Config Manager delivers a registered key family to its owner
func TestRuleConfigServiceStopIsBoundedAndReportsStoppedOnlyWhenJoined(t *testing.T) {
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

	target := &heldApplyTarget{entered: make(chan struct{}), release: make(chan struct{})}
	released := false
	defer func() {
		if !released {
			close(target.release)
		}
	}()
	ruleConfig := newRuleConfigService(ruleManager, []rulepkg.HotReloadTarget{target}, logger)

	// Start's initial reconcile reaches the target's apply and is held there.
	startErr := make(chan error, 1)
	go func() { startErr <- ruleConfig.Start(ctx) }()
	select {
	case <-target.entered:
	case <-time.After(serviceStopReturnTolerance):
		t.Fatalf("Start's initial reconcile did not reach the target within %s", serviceStopReturnTolerance)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), serviceStopDeadlineUnderTest)
	defer stopCancel()
	stopErr := make(chan error, 1)
	go func() { stopErr <- ruleConfig.Stop(stopCtx) }()
	select {
	case err := <-stopErr:
		require.True(t, errors.Is(err, context.DeadlineExceeded),
			"a Stop whose bound wins returns its context's error, got %v", err)
	case <-time.After(serviceStopReturnTolerance):
		t.Fatalf("Stop did not return within %s of its %s deadline", serviceStopReturnTolerance, serviceStopDeadlineUnderTest)
	}
	require.NotEqual(t, service.StatusStopped, ruleConfig.Status(),
		"the service must not report stopped while its reconcile loop is still running")

	close(target.release)
	released = true
	require.NoError(t, <-startErr)
	finalCtx, finalCancel := context.WithTimeout(context.Background(), ruleConfigStopBudget)
	defer finalCancel()
	require.NoError(t, ruleConfig.Stop(finalCtx), "a Stop after the work is released completes")
	require.Equal(t, service.StatusStopped, ruleConfig.Status())
}
