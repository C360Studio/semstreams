//go:build integration

// Cron-scheduler end-to-end tests. Build-tagged `integration` so they
// only run when Docker + testcontainers is available — they spin up a
// real NATS JetStream, register cron rules through the Processor's
// public API, and assert on observable side-effects (NATS messages
// landing, RULE_SCHEDULES KV records persisting, missed-fire metrics
// incrementing across restart).
//
// Schedules use `@every 1s` so the test wallclock stays small. The
// `@every Ns` descriptor was added by robfig/cron specifically for
// fast-cycling smoke tests; it is not a recommended production
// schedule.
//
// Lives in `package rule` (not rule_test) so the cron metrics
// internals are reachable for direct counter inspection — matches the
// kv_hot_reload_integration_test.go pattern.
package rule

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
)

// getIntegrationNATSClient creates a NATS client for cron integration
// tests. Mirrors getTestNATSClient in rule_integration_test.go but
// scoped to this package's test file (which is `package rule` rather
// than `package rule_test`).
func getIntegrationNATSClient(t *testing.T) *natsclient.Client {
	t.Helper()
	testClient, err := natsclient.NewSharedTestClient(
		natsclient.WithJetStream(),
		natsclient.WithKV(),
		natsclient.WithTestTimeout(5*time.Second),
		natsclient.WithStartTimeout(30*time.Second),
	)
	if err != nil {
		t.Fatalf("Failed to create test client: %v", err)
	}
	t.Cleanup(func() { testClient.Terminate() })
	return testClient.Client
}

// startCronProcessorForTest builds a rule.Processor wired with the
// supplied inline rules and brings it through Initialize → Start. The
// caller can stop the processor explicitly (cross-restart tests need
// that control); a t.Cleanup handler also Stops it as a backstop.
func startCronProcessorForTest(t *testing.T, natsClient *natsclient.Client, rules []Definition) (*Processor, *metric.MetricsRegistry) {
	t.Helper()

	cfg := mustTestConfig(t, "rule-test-pack")
	cfg.Ports = &component.PortConfig{
		Inputs: []component.PortDefinition{
			{Name: "entity_events", Config: component.NATSPort{Subject: "events.graph.entity.>", Interface: &component.InterfaceContract{Type: "core.entity.v1"}}, Required: false},
		},
		Outputs: []component.PortDefinition{
			{Name: "rule_events", Config: component.NATSPort{Subject: "events.rule.triggered", Interface: &component.InterfaceContract{Type: "core.rule.v1"}}, Required: false},
		},
	}
	cfg.InlineRules = rules
	cfg.EnableGraphIntegration = false

	registry := metric.NewMetricsRegistry()
	proc, err := NewProcessorWithMetrics(natsClient, &cfg, registry)
	require.NoError(t, err)
	// No SetDecoder call: cron rules fire on a clock, not on incoming
	// messages, so the decoder path is unused. Same posture as
	// kv_hot_reload_integration_test.go.
	require.NoError(t, proc.Initialize())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	require.NoError(t, proc.Start(ctx))
	t.Cleanup(func() { _ = proc.Stop(context.Background()) })

	return proc, registry
}

// TestIntegration_CronRule_FiresOnSchedule registers an @every 1s
// rule, subscribes to the publish subject, and asserts at least one
// message arrives within 3 seconds. Smoke test for the full path:
// scheduler tick → fire → ActionExecutor → NATS publish.
func TestIntegration_CronRule_FiresOnSchedule(t *testing.T) {
	natsClient := getIntegrationNATSClient(t)
	subject := fmt.Sprintf("test.cron.heartbeat.%s", t.Name())

	received := make(chan *nats.Msg, 4)
	sub, err := natsClient.Subscribe(context.Background(), subject, func(_ context.Context, msg *nats.Msg) {
		select {
		case received <- msg:
		default:
		}
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	rules := []Definition{{
		ID:       "every-second-heartbeat-" + t.Name(),
		Type:     CronRuleType,
		Name:     "every-second heartbeat",
		Enabled:  true,
		Schedule: "@every 1s",
		Actions: []Action{{
			Type:    ActionTypePublish,
			Subject: subject,
		}},
	}}
	startCronProcessorForTest(t, natsClient, rules)

	select {
	case msg := <-received:
		// actions.go injects subject + timestamp into the published
		// JSON. Confirms the publish path executed end-to-end.
		var body map[string]any
		require.NoError(t, json.Unmarshal(msg.Data, &body))
		require.Equal(t, subject, body["subject"])
	case <-time.After(3 * time.Second):
		t.Fatal("cron rule did not fire within 3s for @every 1s schedule")
	}
}

// TestIntegration_CronRule_PersistsLastFiredToKV waits for one fire,
// then reads RULE_SCHEDULES directly to confirm the persistence path
// landed. Closes the loop on Chunk 3's KV-write semantics through
// real JetStream (not the mock bucket the unit tests use).
func TestIntegration_CronRule_PersistsLastFiredToKV(t *testing.T) {
	natsClient := getIntegrationNATSClient(t)
	subject := fmt.Sprintf("test.cron.persist.%s", t.Name())
	ruleID := "persist-test-" + t.Name()

	received := make(chan struct{}, 1)
	sub, err := natsClient.Subscribe(context.Background(), subject, func(_ context.Context, _ *nats.Msg) {
		select {
		case received <- struct{}{}:
		default:
		}
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	rules := []Definition{{
		ID:       ruleID,
		Type:     CronRuleType,
		Name:     "persist-fire test",
		Enabled:  true,
		Schedule: "@every 1s",
		Actions: []Action{{
			Type:    ActionTypePublish,
			Subject: subject,
		}},
	}}
	startCronProcessorForTest(t, natsClient, rules)

	select {
	case <-received:
	case <-time.After(3 * time.Second):
		t.Fatal("first fire did not arrive")
	}

	// Poll RULE_SCHEDULES — the RecordFire call is async with the
	// publish (both fire from inside the dispatch loop), so a brief
	// retry loop avoids flakes on slow CI.
	js, err := natsClient.JetStream()
	require.NoError(t, err)
	bucket, err := js.KeyValue(context.Background(), ScheduleBucketName)
	require.NoError(t, err)

	var rec LastFireRecord
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		entry, err := bucket.Get(context.Background(), ruleID)
		if err == nil {
			require.NoError(t, json.Unmarshal(entry.Value(), &rec))
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.NotZero(t, rec.LastFiredAt, "RULE_SCHEDULES has no persisted record after fire")
	require.Equal(t, ruleID, rec.RuleID)
	require.Equal(t, "@every 1s", rec.ScheduleSpec)
}

// TestIntegration_CronRule_DetectsMissedFiresAcrossRestart proves the
// restart-recovery path end-to-end: a fire on the first processor
// persists to RULE_SCHEDULES; we Stop, seed a stale timestamp to
// avoid waiting wallclock, then Start a second processor against the
// same NATS instance and assert restoreFromTracker increments the
// missed_fires_total counter on the second registry.
func TestIntegration_CronRule_DetectsMissedFiresAcrossRestart(t *testing.T) {
	natsClient := getIntegrationNATSClient(t)
	ruleID := "missed-fire-test-" + t.Name()
	subject := fmt.Sprintf("test.cron.missed.%s", t.Name())

	rules := []Definition{{
		ID:       ruleID,
		Type:     CronRuleType,
		Name:     "missed-fire test",
		Enabled:  true,
		Schedule: "@every 1s",
		Actions: []Action{{
			Type:    ActionTypePublish,
			Subject: subject,
		}},
	}}

	received := make(chan struct{}, 1)
	sub, err := natsClient.Subscribe(context.Background(), subject, func(_ context.Context, _ *nats.Msg) {
		select {
		case received <- struct{}{}:
		default:
		}
	})
	require.NoError(t, err)
	defer func() { _ = sub.Unsubscribe() }()

	proc1, _ := startCronProcessorForTest(t, natsClient, rules)
	select {
	case <-received:
	case <-time.After(3 * time.Second):
		t.Fatal("first run did not fire")
	}
	require.NoError(t, proc1.Stop(context.Background()))

	// Seed a stale timestamp so missed-fire detection sees several
	// expected fires without us waiting real seconds. Six seconds of
	// "downtime" against an @every 1s schedule yields >= 5 expected
	// fires — well above the cap and well above any flake threshold.
	js, err := natsClient.JetStream()
	require.NoError(t, err)
	bucket, err := js.KeyValue(context.Background(), ScheduleBucketName)
	require.NoError(t, err)

	stale := LastFireRecord{
		RuleID:       ruleID,
		ScheduleSpec: "@every 1s",
		LastFiredAt:  time.Now().Add(-6 * time.Second),
	}
	staleData, err := json.Marshal(stale)
	require.NoError(t, err)
	_, err = bucket.Put(context.Background(), ruleID, staleData)
	require.NoError(t, err)

	// Second run: fresh processor against the same NATS. The new
	// metrics registry sees missed_fires_total > 0 only if
	// restoreFromTracker walked the stale record + emitted to the
	// counter.
	_, registry2 := startCronProcessorForTest(t, natsClient, rules)

	cronM := getCronMetrics(registry2)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if testutil.ToFloat64(cronM.missedFiresTotal.WithLabelValues(ruleID)) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	missed := testutil.ToFloat64(cronM.missedFiresTotal.WithLabelValues(ruleID))
	require.Greater(t, missed, float64(0), "missed_fires_total = 0; restoreFromTracker did not increment the counter for the seeded stale record")
}

// TestIntegration_CronRule_CooldownAcrossRestart proves the cross-
// restart cooldown fix from Chunk 4: a rule with a 1h cooldown that
// fired moments before "restart" must skip its first post-restart
// fire on the cooldown gate. Without lastFiredNanos hydration in
// restoreFromTracker, the gauge would treat the first post-restart
// fire as "never fired" and double-dispatch.
func TestIntegration_CronRule_CooldownAcrossRestart(t *testing.T) {
	natsClient := getIntegrationNATSClient(t)
	ruleID := "cooldown-restart-" + t.Name()
	subject := fmt.Sprintf("test.cron.cooldown.%s", t.Name())

	rules := []Definition{{
		ID:       ruleID,
		Type:     CronRuleType,
		Name:     "cooldown across restart",
		Enabled:  true,
		Schedule: "@every 1s",
		Cooldown: "1h",
		Actions: []Action{{
			Type:    ActionTypePublish,
			Subject: subject,
		}},
	}}

	var (
		mu       sync.Mutex
		received int
	)
	sub, err := natsClient.Subscribe(context.Background(), subject, func(_ context.Context, _ *nats.Msg) {
		mu.Lock()
		received++
		mu.Unlock()
	})
	require.NoError(t, err)
	defer func() { _ = sub.Unsubscribe() }()

	// First run: expect one fire (cooldown=1h, so subsequent ticks
	// within the same run are cooldown-skipped).
	proc1, _ := startCronProcessorForTest(t, natsClient, rules)
	time.Sleep(2500 * time.Millisecond)
	require.NoError(t, proc1.Stop(context.Background()))

	mu.Lock()
	firstRunCount := received
	mu.Unlock()
	require.Equal(t, 1, firstRunCount, "first run should fire exactly once (1h cooldown after first tick)")

	// Second run: same NATS, same rule, no further @every 1s ticks
	// should dispatch because the persisted last-fired is < 1h old.
	// Hydration in restoreFromTracker is what makes this work — without
	// it, the post-restart cooldown gate would treat the rule as "never
	// fired" and dispatch immediately.
	proc2, _ := startCronProcessorForTest(t, natsClient, rules)
	time.Sleep(2500 * time.Millisecond)
	require.NoError(t, proc2.Stop(context.Background()))

	mu.Lock()
	totalCount := received
	mu.Unlock()
	require.Equal(t, 1, totalCount, "post-restart fires within cooldown window: got %d, want 1 (= unchanged from first run)", totalCount)
}
