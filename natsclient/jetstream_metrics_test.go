package natsclient

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/metric"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestJetStreamPolicyMetricsShareCanonicalCollectorsAcrossOwners(t *testing.T) {
	registry := metric.NewMetricsRegistry()
	first, err := newJetStreamMetrics(registry)
	if err != nil {
		t.Fatal(err)
	}
	second, err := newJetStreamMetrics(registry)
	if err != nil {
		t.Fatal(err)
	}

	if first.policyRequested != second.policyRequested {
		t.Fatal("requested policy collector identity differs across JetStream metrics owners")
	}
	if first.policyEffective != second.policyEffective {
		t.Fatal("effective policy collector identity differs across JetStream metrics owners")
	}
	if first.policyAvailable != second.policyAvailable {
		t.Fatal("availability policy collector identity differs across JetStream metrics owners")
	}

	labels := []string{"worker", "events", "EVENTS", "worker-events", policySourcePort}
	second.policyRequested.WithLabelValues(labels...).Set(7)
	second.policyEffective.WithLabelValues(labels...).Set(5)
	second.policyAvailable.WithLabelValues(labels...).Set(1)

	families, err := registry.PrometheusRegistry().Gather()
	if err != nil {
		t.Fatal(err)
	}
	wantValues := map[string]float64{
		"semstreams_jetstream_consumer_max_ack_pending_requested":             7,
		"semstreams_jetstream_consumer_max_ack_pending_effective":             5,
		"semstreams_jetstream_consumer_max_ack_pending_observation_available": 1,
	}
	gotNames := make([]string, 0, len(wantValues))
	for _, family := range families {
		name := family.GetName()
		if strings.HasPrefix(name, "semstreams_jetstream_") &&
			(strings.Contains(name, "queue") || strings.Contains(name, "drop")) {
			t.Fatalf("unexpected queue/drop metric added with consumer-policy observation: %q", name)
		}
		want, isPolicy := wantValues[name]
		if !isPolicy {
			continue
		}
		gotNames = append(gotNames, name)
		if len(family.Metric) != 1 {
			t.Fatalf("metric family %q has %d series, want 1", name, len(family.Metric))
		}
		if got := family.Metric[0].GetGauge().GetValue(); got != want {
			t.Fatalf("metric %q = %v, want %v", name, got, want)
		}
	}
	sort.Strings(gotNames)
	wantNames := make([]string, 0, len(wantValues))
	for name := range wantValues {
		wantNames = append(wantNames, name)
	}
	sort.Strings(wantNames)
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("policy metric names = %v, want exact names %v", gotNames, wantNames)
	}
}

type policyInfoSequence struct {
	results []policyInfoResult
	next    int
}

type policyInfoResult struct {
	info *jetstream.ConsumerInfo
	err  error
}

func (s *policyInfoSequence) Info(context.Context) (*jetstream.ConsumerInfo, error) {
	result := s.results[s.next]
	if s.next < len(s.results)-1 {
		s.next++
	}
	return result.info, result.err
}

func TestJetStreamPolicyMetricsRemoveStaleEffectiveAndRecover(t *testing.T) {
	metrics, err := newJetStreamMetrics(metric.NewMetricsRegistry())
	if err != nil {
		t.Fatal(err)
	}
	handle := &policyInfoSequence{results: []policyInfoResult{
		{err: errors.New("unavailable")},
		{info: &jetstream.ConsumerInfo{Config: jetstream.ConsumerConfig{MaxAckPending: 8}}},
	}}
	record := &consumerPolicyRecord{
		component: "worker", port: "events", stream: "EVENTS", consumer: "worker-events",
		policySource: policySourcePort, requested: 8, handle: handle, available: true, active: true,
	}
	key := record.key()
	metrics.trackPolicy(key, record, 8)
	labels := record.labels()

	metrics.updateStats(context.Background())
	if got := testutil.ToFloat64(metrics.policyRequested.WithLabelValues(labels...)); got != 8 {
		t.Fatalf("requested = %v, want 8", got)
	}
	if got := testutil.ToFloat64(metrics.policyAvailable.WithLabelValues(labels...)); got != 0 {
		t.Fatalf("availability after failure = %v, want 0", got)
	}
	if count := testutil.CollectAndCount(metrics.policyEffective); count != 0 {
		t.Fatalf("effective series count after failure = %d, want 0", count)
	}

	metrics.updateStats(context.Background())
	if got := testutil.ToFloat64(metrics.policyAvailable.WithLabelValues(labels...)); got != 1 {
		t.Fatalf("availability after recovery = %v, want 1", got)
	}
	if got := testutil.ToFloat64(metrics.policyEffective.WithLabelValues(labels...)); got != 8 {
		t.Fatalf("effective after recovery = %v, want 8", got)
	}

	metrics.forgetPolicy(key)
	count := testutil.CollectAndCount(metrics.policyRequested) +
		testutil.CollectAndCount(metrics.policyEffective) +
		testutil.CollectAndCount(metrics.policyAvailable)
	if count != 0 {
		t.Fatalf("policy series count after forget = %d, want 0", count)
	}
}

type blockingPolicyInfo struct {
	started chan struct{}
	release chan struct{}
}

type blockingTrackedConsumer struct {
	jetstream.Consumer
	started chan struct{}
	release chan struct{}
}

func (b *blockingTrackedConsumer) Info(context.Context) (*jetstream.ConsumerInfo, error) {
	close(b.started)
	<-b.release
	return &jetstream.ConsumerInfo{Stream: "EVENTS", Name: "internal-events"}, nil
}

func TestJetStreamConsumerClosedCannotBeUndoneByInflightRefresh(t *testing.T) {
	metrics, err := newJetStreamMetrics(metric.NewMetricsRegistry())
	if err != nil {
		t.Fatal(err)
	}
	handle := &blockingTrackedConsumer{started: make(chan struct{}), release: make(chan struct{})}
	metrics.trackConsumer("EVENTS", "internal-events", handle)
	done := make(chan struct{})
	go func() {
		metrics.updateStats(context.Background())
		close(done)
	}()
	<-handle.started
	metrics.forgetConsumer("EVENTS", "internal-events")
	close(handle.release)
	<-done

	count := testutil.CollectAndCount(metrics.consumerPending) +
		testutil.CollectAndCount(metrics.consumerDelivered) +
		testutil.CollectAndCount(metrics.consumerAcked) +
		testutil.CollectAndCount(metrics.consumerRedelivered)
	if count != 0 {
		t.Fatalf("in-flight refresh recreated %d Closed consumer series", count)
	}
}

func (b *blockingPolicyInfo) Info(context.Context) (*jetstream.ConsumerInfo, error) {
	close(b.started)
	<-b.release
	return &jetstream.ConsumerInfo{Config: jetstream.ConsumerConfig{MaxAckPending: 5}}, nil
}

func TestJetStreamPolicyStopCannotBeUndoneByInflightRefresh(t *testing.T) {
	metrics, err := newJetStreamMetrics(metric.NewMetricsRegistry())
	if err != nil {
		t.Fatal(err)
	}
	handle := &blockingPolicyInfo{started: make(chan struct{}), release: make(chan struct{})}
	record := &consumerPolicyRecord{
		component: "worker", port: "events", stream: "EVENTS", consumer: "worker-events",
		policySource: policySourcePort, requested: 5, handle: handle, available: true, active: true,
	}
	key := record.key()
	metrics.trackPolicy(key, record, 5)

	done := make(chan struct{})
	go func() {
		metrics.updateStats(context.Background())
		close(done)
	}()
	<-handle.started
	metrics.forgetPolicy(key)
	close(handle.release)
	<-done

	count := testutil.CollectAndCount(metrics.policyRequested) +
		testutil.CollectAndCount(metrics.policyEffective) +
		testutil.CollectAndCount(metrics.policyAvailable)
	if count != 0 {
		t.Fatalf("in-flight refresh recreated %d stopped policy series", count)
	}
}

func TestJetStreamPolicyReplacementCannotBeUndoneByInflightRefresh(t *testing.T) {
	metrics, err := newJetStreamMetrics(metric.NewMetricsRegistry())
	if err != nil {
		t.Fatal(err)
	}
	oldHandle := &blockingPolicyInfo{started: make(chan struct{}), release: make(chan struct{})}
	oldRecord := &consumerPolicyRecord{
		component: "worker", port: "events", stream: "EVENTS", consumer: "worker-events",
		policySource: policySourcePort, requested: 5, handle: oldHandle, available: true, active: true,
	}
	key := oldRecord.key()
	metrics.trackPolicy(key, oldRecord, 5)

	done := make(chan struct{})
	go func() {
		metrics.updateStats(context.Background())
		close(done)
	}()
	<-oldHandle.started
	newRecord := &consumerPolicyRecord{
		component: "worker", port: "events", stream: "EVENTS", consumer: "worker-events",
		policySource: policySourcePort, requested: 9, available: true, active: true,
		handle: &policyInfoSequence{results: []policyInfoResult{
			{info: &jetstream.ConsumerInfo{Config: jetstream.ConsumerConfig{MaxAckPending: 9}}},
		}},
	}
	metrics.trackPolicy(key, newRecord, 9)
	close(oldHandle.release)
	<-done

	labels := newRecord.labels()
	if got := testutil.ToFloat64(metrics.policyRequested.WithLabelValues(labels...)); got != 9 {
		t.Fatalf("requested after replacement = %v, want 9", got)
	}
	if got := testutil.ToFloat64(metrics.policyEffective.WithLabelValues(labels...)); got != 9 {
		t.Fatalf("effective after replacement = %v, want 9", got)
	}
}

func TestJetStreamPolicyIdentityKeepsSiblingAndReplacesOnlyExactRecord(t *testing.T) {
	metrics, err := newJetStreamMetrics(metric.NewMetricsRegistry())
	if err != nil {
		t.Fatal(err)
	}
	newRecord := func(stream, consumer string, requested int) *consumerPolicyRecord {
		return &consumerPolicyRecord{
			component: "worker", port: "events", stream: stream, consumer: consumer,
			policySource: policySourcePort, requested: requested, available: true, active: true,
			handle: &policyInfoSequence{results: []policyInfoResult{
				{info: &jetstream.ConsumerInfo{Config: jetstream.ConsumerConfig{MaxAckPending: requested}}},
			}},
		}
	}
	first := newRecord("EVENTS_A", "worker-a", 5)
	sibling := newRecord("EVENTS_B", "worker-b", 7)
	metrics.trackPolicy(first.key(), first, 5)
	metrics.trackPolicy(sibling.key(), sibling, 7)
	if len(metrics.policies) != 2 {
		t.Fatalf("policy records = %d, want two coexisting instances", len(metrics.policies))
	}

	replacement := newRecord("EVENTS_A", "worker-a", 9)
	metrics.trackPolicy(replacement.key(), replacement, 9)
	if len(metrics.policies) != 2 {
		t.Fatalf("policy records after exact replacement = %d, want 2", len(metrics.policies))
	}
	if first.active {
		t.Fatal("exact prior record remains active after replacement")
	}
	if !sibling.active {
		t.Fatal("sibling instance was deactivated by another identity replacement")
	}
	if got := testutil.ToFloat64(metrics.policyRequested.WithLabelValues(sibling.labels()...)); got != 7 {
		t.Fatalf("sibling requested = %v, want 7", got)
	}
	if got := testutil.ToFloat64(metrics.policyRequested.WithLabelValues(replacement.labels()...)); got != 9 {
		t.Fatalf("replacement requested = %v, want 9", got)
	}
}
