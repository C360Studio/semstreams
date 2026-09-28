package boot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/pkg/errs"
	rulepkg "github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/service"
)

// ruleConfigServiceName is the rule ConfigManager's name among the framework
// services, so it appears in the service health listing.
const ruleConfigServiceName = "rule-config"

// ruleConfigService runs the root's one rule ConfigManager as a registered
// framework service (#1188; owner ruling, docket 6 Q11 (b-full)).
//
// service.Manager starts services in registration order and stops them in
// exact reverse order, under its shutdown context. Registered right after the
// component manager, this service therefore seeds and reconciles only once
// the rule processors have started, and stops before any of them stops, so
// no reconcile races a processor's teardown. Its health is the service's own
// lifecycle state.
type ruleConfigService struct {
	*service.BaseService
	rules   *rulepkg.ConfigManager
	targets []rulepkg.HotReloadTarget
}

func newRuleConfigService(
	rules *rulepkg.ConfigManager, targets []rulepkg.HotReloadTarget, logger *slog.Logger,
) *ruleConfigService {
	return &ruleConfigService{
		BaseService: service.NewBaseServiceWithOptions(ruleConfigServiceName, nil, service.WithLogger(logger)),
		rules:       rules,
		targets:     targets,
	}
}

// Start seeds the targets' loaded rules and reconciles the family into them,
// then keeps reconciling until Stop.
func (s *ruleConfigService) Start(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "ruleConfigService", "Start", "context cannot be nil")
	}
	if err := s.BaseService.Start(ctx); err != nil {
		return err
	}
	if err := s.rules.Start(ctx, s.targets); err != nil {
		return errors.Join(fmt.Errorf("start rule configuration manager: %w", err), s.BaseService.Stop(ctx))
	}
	return nil
}

// Stop stops the rule ConfigManager within ctx. If ctx ends before its
// reconcile loop exits, Stop returns that error and the service does not
// report stopped; a later Stop waits again. A completed repeated Stop is a nil
// no-op.
func (s *ruleConfigService) Stop(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "ruleConfigService", "Stop", "context cannot be nil")
	}
	if err := s.rules.Stop(ctx); err != nil {
		return err
	}
	return s.BaseService.Stop(ctx)
}

// registerRuleConfigService registers the rule ConfigManager as the
// "rule-config" service, bound to the rule processors the component manager
// built. It must run after the component manager is registered: before it,
// there are no targets to bind, and the service would start first and stop
// last. So it refuses rather than register a manager that reconciles nothing.
func registerRuleConfigService(manager *service.Manager, rules *rulepkg.ConfigManager, logger *slog.Logger) error {
	if _, ok := manager.GetService("component-manager"); !ok {
		return errors.New("register rule-config service: the component manager is not registered yet")
	}
	targets := service.ComponentsImplementing[rulepkg.HotReloadTarget](manager)
	if err := manager.RegisterInstance(ruleConfigServiceName, newRuleConfigService(rules, targets, logger)); err != nil {
		return fmt.Errorf("register rule-config service: %w", err)
	}
	return nil
}
