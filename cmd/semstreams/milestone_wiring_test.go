package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/metric"
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
		types.PlatformMeta{Org: "acme", Platform: "ops"}, nil,
	))

	_, registered := manager.GetService("milestone")
	require.True(t, registered, "the milestone service is not under the ServiceManager")
	require.True(t, registry.Unregister("agentrun", "milestone_decisions_total"),
		"this root did not publish semstreams_agentrun_milestone_decisions_total")
}
