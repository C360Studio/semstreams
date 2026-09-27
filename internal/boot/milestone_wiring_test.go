package boot

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic/agentrun"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/service"
	"github.com/c360studio/semstreams/types"
)

// TestRegisterMilestoneServicePublishesTheDecisionsCounter pins the WIRING, not
// the primitive: MilestoneSubscriber.RegisterMetrics works on its own, but the
// counter only reaches /metrics if THIS root calls it. Dropping that one call
// leaves every milestone decision counted on a vec no scrape can see, and
// nothing else in the binary notices.
func TestRegisterMilestoneServicePublishesTheDecisionsCounter(t *testing.T) {
	registry := metric.NewMetricsRegistry()
	manager := service.NewServiceManager(service.NewServiceRegistry())

	require.NoError(t, registerMilestoneService(
		manager, &service.Dependencies{}, nil, registry,
		types.PlatformMeta{Org: "acme", Platform: "ops"}, nil, nil,
	))

	_, registered := manager.GetService("milestone")
	require.True(t, registered, "the milestone service is not under the ServiceManager")
	require.True(t, registry.Unregister("agentrun", "milestone_decisions_total"),
		"this root did not publish semstreams_agentrun_milestone_decisions_total")
}

// TestRegisterMilestoneServiceRunsHooksOnTheSubscriberBeforeRegistration pins
// the milestone-hook extension phase: each hook receives the subscriber the
// service will start, and a failing hook fails boot before the service is
// registered.
func TestRegisterMilestoneServiceRunsHooksOnTheSubscriberBeforeRegistration(t *testing.T) {
	manager := service.NewServiceManager(service.NewServiceRegistry())
	var seen *agentrun.MilestoneSubscriber
	wantErr := errors.New("hook refused")

	err := registerMilestoneService(
		manager, &service.Dependencies{}, nil, metric.NewMetricsRegistry(),
		types.PlatformMeta{Org: "acme", Platform: "ops"}, nil,
		[]func(*agentrun.MilestoneSubscriber, *natsclient.Client, *slog.Logger) error{
			func(subscriber *agentrun.MilestoneSubscriber, _ *natsclient.Client, _ *slog.Logger) error {
				seen = subscriber
				return wantErr
			},
		},
	)

	require.ErrorIs(t, err, wantErr)
	require.NotNil(t, seen, "the hook did not receive the milestone subscriber")
	_, registered := manager.GetService("milestone")
	require.False(t, registered, "the milestone service was registered past a failing hook")
}
