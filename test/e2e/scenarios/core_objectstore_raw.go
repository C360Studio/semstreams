package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/test/e2e/client"
	e2econfig "github.com/c360studio/semstreams/test/e2e/config"
)

const (
	coreRawStorageBucket       = "streamkit_test_store"
	coreRawStorageStream       = "MAPPED"
	coreRawStorageConsumer     = "objectstore-objectstore-mapped-messages"
	coreRawStorageMarkerField  = "_e2e_objectstore_raw_marker"
	coreRawStorageMessageType  = "core.json.v1"
	coreRawStorageMaxAttempts  = 6
	coreRawStoragePollInterval = 50 * time.Millisecond
	coreRawStoragePollTimeout  = 20 * time.Second
	coreRawStorageSetupTimeout = 10 * time.Second
	coreRawStorageCallTimeout  = 5 * time.Second
	coreRawStorageStageTimeout = 2 * time.Minute
	coreRawStorageCloseTimeout = 5 * time.Second
	coreMaxDeliveryCapture     = "MAX_DELIVERY_EVENTS"
	coreMaxDeliveryMetric      = "semstreams_nats_max_delivery_exhaustions_total"
	coreMaxDeliveryPollTimeout = 45 * time.Second
)

type rawStorageEnvelope struct {
	wireID      string
	messageType string
	marker      string
}

// executeVerifyMaxDeliveryVisibility proves #742 against the assembled
// production binary and shipped ObjectStore lane. Test-side NATS administration
// updates the existing durable to MaxDeliver=1, then seals the already-open
// ObjectStore backing stream. This makes Put through the component's held handle
// fail deterministically without a production fault knob. The server occurrence
// must remain in the bounded capture ledger and the fixed observer must emit its
// bounded-label Prometheus counter.
//
// This stage is last because sealing is irreversible for this disposable E2E
// stack. The subsequent core graph-roundtrip scenario does not use ObjectStore.
func (s *CoreDataflowScenario) executeVerifyMaxDeliveryVisibility(ctx context.Context, result *Result) error {
	// ObjectStore's production transient disposition is NakWithDelay(30s).
	// NATS emits MAX_DELIVERIES when that delayed schedule matures and discovers
	// the MaxDeliver=1 ceiling, so the E2E budget must cover the real delay.
	stageCtx, cancelStage := context.WithTimeout(ctx, coreMaxDeliveryPollTimeout)
	defer cancelStage()

	natsClient, err := natsclient.NewClient(e2econfig.DefaultEndpoints.NATS)
	if err != nil {
		return fmt.Errorf("create NATS client for MaxDeliver proof: %w", err)
	}
	if err := natsClient.Connect(stageCtx); err != nil {
		return fmt.Errorf("connect to E2E NATS for MaxDeliver proof: %w", err)
	}
	defer func() {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), coreRawStorageCloseTimeout)
		defer cancelCleanup()
		_ = natsClient.Close(cleanupCtx)
	}()

	js, err := natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("open JetStream for MaxDeliver proof: %w", err)
	}
	capture, err := js.Stream(stageCtx, coreMaxDeliveryCapture)
	if err != nil {
		return fmt.Errorf("open framework MaxDeliver capture stream: %w", err)
	}
	captureInfo, err := capture.Info(stageCtx)
	if err != nil {
		return fmt.Errorf("read MaxDeliver capture baseline: %w", err)
	}
	baselineSequence := captureInfo.State.LastSeq

	if err := prepareMaxDeliveryFailure(stageCtx, js); err != nil {
		return err
	}

	marker := uuid.NewString()
	publishAck, err := js.Publish(stageCtx, "mapped.messages", []byte(`{"_e2e_max_delivery_marker":"`+marker+`"}`))
	if err != nil {
		return fmt.Errorf("publish through shipped objectstore raw lane: %w", err)
	}

	advisorySubject := "$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES." +
		coreRawStorageStream + "." + coreRawStorageConsumer
	var event struct {
		Type       string `json:"type"`
		ID         string `json:"id"`
		Stream     string `json:"stream"`
		Consumer   string `json:"consumer"`
		StreamSeq  uint64 `json:"stream_seq"`
		Deliveries uint64 `json:"deliveries"`
	}
	poll := time.NewTicker(coreRawStoragePollInterval)
	defer poll.Stop()
	for {
		captured, getErr := capture.GetLastMsgForSubject(stageCtx, advisorySubject)
		if getErr == nil && captured.Sequence > baselineSequence {
			if err := json.Unmarshal(captured.Data, &event); err != nil {
				return fmt.Errorf("decode captured MaxDeliver advisory: %w", err)
			}
			break
		}
		select {
		case <-stageCtx.Done():
			return fmt.Errorf("MaxDeliver advisory was not retained after sealed ObjectStore failure: %w", stageCtx.Err())
		case <-poll.C:
		}
	}
	if event.Type != "io.nats.jetstream.advisory.v1.max_deliver" || event.ID == "" ||
		event.Stream != coreRawStorageStream || event.Consumer != coreRawStorageConsumer ||
		event.StreamSeq != publishAck.Sequence || event.Deliveries != 1 {
		return fmt.Errorf("captured MaxDeliver advisory has unexpected typed fields: %+v", event)
	}

	metrics := client.NewMetricsClient(e2econfig.DefaultEndpoints.Metrics)
	for {
		snapshot, metricErr := metrics.FetchSnapshot(stageCtx)
		if metricErr == nil && maxDeliveryMetricObserved(snapshot, coreRawStorageStream, coreRawStorageConsumer) {
			break
		}
		select {
		case <-stageCtx.Done():
			return fmt.Errorf("MaxDeliver occurrence was retained but operator metric was not emitted: %w", stageCtx.Err())
		case <-poll.C:
		}
	}

	result.Details["max_delivery_advisory_id"] = event.ID
	result.Details["max_delivery_stream"] = event.Stream
	result.Details["max_delivery_consumer"] = event.Consumer
	result.Metrics["max_delivery_deliveries"] = event.Deliveries
	return nil
}

func prepareMaxDeliveryFailure(ctx context.Context, js jetstream.JetStream) error {
	consumer, err := js.Consumer(ctx, coreRawStorageStream, coreRawStorageConsumer)
	if err != nil {
		return fmt.Errorf("open shipped objectstore consumer for MaxDeliver proof: %w", err)
	}
	consumerInfo, err := consumer.Info(ctx)
	if err != nil {
		return fmt.Errorf("read shipped objectstore consumer policy: %w", err)
	}
	consumerConfig := consumerInfo.Config
	consumerConfig.MaxDeliver = 1
	inputStream, err := js.Stream(ctx, coreRawStorageStream)
	if err != nil {
		return fmt.Errorf("open shipped objectstore input stream: %w", err)
	}
	consumer, err = inputStream.CreateOrUpdateConsumer(ctx, consumerConfig)
	if err != nil {
		return fmt.Errorf("set test-only ObjectStore MaxDeliver=1: %w", err)
	}
	consumerInfo, err = consumer.Info(ctx)
	if err != nil {
		return fmt.Errorf("verify test-only ObjectStore consumer policy: %w", err)
	}
	if consumerInfo.Config.MaxDeliver != 1 {
		return fmt.Errorf("test-side ObjectStore consumer MaxDeliver=%d after update, want 1", consumerInfo.Config.MaxDeliver)
	}

	backing, err := js.Stream(ctx, "OBJ_"+coreRawStorageBucket)
	if err != nil {
		return fmt.Errorf("open shipped objectstore backing stream: %w", err)
	}
	backingInfo, err := backing.Info(ctx)
	if err != nil {
		return fmt.Errorf("read shipped objectstore backing stream: %w", err)
	}
	sealed := backingInfo.Config
	sealed.Sealed = true
	if _, err := js.UpdateStream(ctx, sealed); err != nil {
		return fmt.Errorf("seal shipped objectstore backing stream: %w", err)
	}
	return nil
}

func maxDeliveryMetricObserved(snapshot *client.MetricsSnapshot, stream, consumer string) bool {
	if snapshot == nil {
		return false
	}
	for _, metric := range snapshot.Metrics {
		if metric.Name != coreMaxDeliveryMetric || metric.Value < 1 {
			continue
		}
		if metric.Labels["stream"] == stream && metric.Labels["consumer"] == consumer {
			return true
		}
	}
	return false
}

type rawStoredObject struct {
	name    string
	nonce   string
	modTime time.Time
}

// executeVerifyRawObjectStore proves #741 against the exact core stack booted
// from configs/protocol-flow.json. JSONMap emits a decodable core.json.v1
// envelope that is not ContentStorable, so objectstore's primary input takes
// the raw write lane whose key used to collide for same-second messages.
func (s *CoreDataflowScenario) executeVerifyRawObjectStore(ctx context.Context, result *Result) error {
	stageCtx, cancelStage := context.WithTimeout(ctx, coreRawStorageStageTimeout)
	defer cancelStage()

	setupCtx, cancelSetup := context.WithTimeout(stageCtx, coreRawStorageSetupTimeout)
	defer cancelSetup()

	natsClient, err := natsclient.NewClient(e2econfig.DefaultEndpoints.NATS)
	if err != nil {
		return fmt.Errorf("create NATS client: %w", err)
	}
	if err := natsClient.Connect(setupCtx); err != nil {
		return fmt.Errorf("connect to E2E NATS: %w", err)
	}
	defer func() {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), coreRawStorageCloseTimeout)
		defer cancelCleanup()
		_ = natsClient.Close(cleanupCtx)
	}()

	js, err := natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("open JetStream: %w", err)
	}
	consumer, err := js.Consumer(setupCtx, coreRawStorageStream, coreRawStorageConsumer)
	if err != nil {
		return fmt.Errorf("open shipped objectstore consumer: %w", err)
	}
	store, err := js.ObjectStore(setupCtx, coreRawStorageBucket)
	if err != nil {
		return fmt.Errorf("open shipped objectstore bucket: %w", err)
	}

	conn, err := (&net.Dialer{}).DialContext(setupCtx, "udp", s.udpAddr)
	if err != nil {
		return fmt.Errorf("connect to UDP input: %w", err)
	}
	defer conn.Close()

	for attempt := 1; attempt <= coreRawStorageMaxAttempts; attempt++ {
		baseline, err := waitForRawStorageConsumerIdle(stageCtx, consumer)
		if err != nil {
			return err
		}

		pairID := uuid.NewString()
		markers := []string{pairID + "-a", pairID + "-b"}
		for sequence, marker := range markers {
			wire, err := json.Marshal(map[string]any{
				coreRawStorageMarkerField: marker,
				"sequence":                sequence,
				"value":                   61 + sequence,
			})
			if err != nil {
				return fmt.Errorf("marshal raw storage datagram: %w", err)
			}
			if _, err := conn.Write(wire); err != nil {
				return fmt.Errorf("send raw storage datagram %d: %w", sequence, err)
			}
		}

		targetAckFloor := baseline + uint64(len(markers))
		if err := waitForRawStorageAcks(stageCtx, consumer, targetAckFloor); err != nil {
			return err
		}

		objects, err := findRawStoredObjects(stageCtx, store, markers)
		if err != nil {
			return err
		}
		if len(objects) != len(markers) {
			return fmt.Errorf(
				"objectstore consumer acked both messages but found %d/%d independently retrievable marked objects",
				len(objects), len(markers),
			)
		}

		if objects[0].modTime.Unix() != objects[1].modTime.Unix() {
			// The server put the pair on opposite sides of a wall-clock second.
			// Retry with fresh markers: that pair cannot discriminate the old
			// seconds-derived collision even though both writes correctly survived.
			continue
		}
		if objects[0].nonce == objects[1].nonce {
			return fmt.Errorf("raw storage keys reused nonce %q", objects[0].nonce)
		}

		result.Metrics["raw_objectstore_objects"] = len(objects)
		result.Metrics["raw_objectstore_attempts"] = attempt
		result.Details["raw_objectstore_keys"] = []string{objects[0].name, objects[1].name}
		result.Details["raw_objectstore_markers"] = markers
		result.Details["raw_objectstore_modtime_unix"] = objects[0].modTime.Unix()
		return nil
	}

	return fmt.Errorf(
		"could not obtain a same-server-second raw storage pair after %d attempts",
		coreRawStorageMaxAttempts,
	)
}

func waitForRawStorageConsumerIdle(ctx context.Context, consumer jetstream.Consumer) (uint64, error) {
	deadlineCtx, cancel := context.WithTimeout(ctx, coreRawStoragePollTimeout)
	defer cancel()

	ticker := time.NewTicker(coreRawStoragePollInterval)
	defer ticker.Stop()

	for {
		info, err := consumer.Info(deadlineCtx)
		if err != nil {
			return 0, fmt.Errorf("read objectstore consumer baseline: %w", err)
		}
		if info.NumAckPending == 0 && info.NumPending == 0 {
			return info.AckFloor.Consumer, nil
		}

		select {
		case <-deadlineCtx.Done():
			return 0, fmt.Errorf(
				"objectstore consumer did not become idle: ack_floor=%d ack_pending=%d pending=%d: %w",
				info.AckFloor.Consumer, info.NumAckPending, info.NumPending, deadlineCtx.Err(),
			)
		case <-ticker.C:
		}
	}
}

func waitForRawStorageAcks(ctx context.Context, consumer jetstream.Consumer, target uint64) error {
	deadlineCtx, cancel := context.WithTimeout(ctx, coreRawStoragePollTimeout)
	defer cancel()

	ticker := time.NewTicker(coreRawStoragePollInterval)
	defer ticker.Stop()

	for {
		info, err := consumer.Info(deadlineCtx)
		if err != nil {
			return fmt.Errorf("read objectstore consumer progress: %w", err)
		}
		if info.AckFloor.Consumer >= target && info.NumAckPending == 0 && info.NumPending == 0 {
			return nil
		}

		select {
		case <-deadlineCtx.Done():
			return fmt.Errorf(
				"objectstore consumer did not reach ack floor %d with no pending messages: ack_floor=%d ack_pending=%d pending=%d: %w",
				target, info.AckFloor.Consumer, info.NumAckPending, info.NumPending, deadlineCtx.Err(),
			)
		case <-ticker.C:
		}
	}
}

func findRawStoredObjects(
	ctx context.Context,
	store jetstream.ObjectStore,
	markers []string,
) ([]rawStoredObject, error) {
	expected := make(map[string]struct{}, len(markers))
	for _, marker := range markers {
		expected[marker] = struct{}{}
	}

	listCtx, cancelList := context.WithTimeout(ctx, coreRawStorageCallTimeout)
	entries, err := store.List(listCtx)
	cancelList()
	if err != nil {
		return nil, fmt.Errorf("list shipped objectstore bucket: %w", err)
	}

	found := make(map[string]rawStoredObject, len(markers))
	for _, entry := range entries {
		getCtx, cancelGet := context.WithTimeout(ctx, coreRawStorageCallTimeout)
		wire, err := store.GetBytes(getCtx, entry.Name)
		cancelGet()
		if err != nil {
			return nil, fmt.Errorf("retrieve object %q: %w", entry.Name, err)
		}
		envelope, err := extractRawStorageEnvelope(wire, coreRawStorageMarkerField)
		if err != nil {
			continue
		}
		if _, ok := expected[envelope.marker]; !ok {
			continue
		}
		if envelope.messageType != coreRawStorageMessageType {
			return nil, fmt.Errorf(
				"marked object %q has type %q, want %q",
				entry.Name, envelope.messageType, coreRawStorageMessageType,
			)
		}
		nonce, err := rawStorageKeyNonce(entry.Name, envelope.wireID)
		if err != nil {
			return nil, fmt.Errorf("marked object key %q: %w", entry.Name, err)
		}
		if _, duplicate := found[envelope.marker]; duplicate {
			return nil, fmt.Errorf("marker %q is stored in more than one object", envelope.marker)
		}
		found[envelope.marker] = rawStoredObject{
			name:    entry.Name,
			nonce:   nonce,
			modTime: entry.ModTime,
		}
	}

	objects := make([]rawStoredObject, 0, len(markers))
	for _, marker := range markers {
		if object, ok := found[marker]; ok {
			objects = append(objects, object)
		}
	}
	return objects, nil
}

func extractRawStorageEnvelope(wire []byte, markerField string) (rawStorageEnvelope, error) {
	var envelope struct {
		ID      string       `json:"id"`
		Type    message.Type `json:"type"`
		Payload struct {
			Data map[string]json.RawMessage `json:"data"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(wire, &envelope); err != nil {
		return rawStorageEnvelope{}, fmt.Errorf("decode BaseMessage envelope: %w", err)
	}
	if envelope.ID == "" {
		return rawStorageEnvelope{}, fmt.Errorf("BaseMessage envelope has empty id")
	}
	if !envelope.Type.IsValid() {
		return rawStorageEnvelope{}, fmt.Errorf("BaseMessage envelope has incomplete type")
	}
	rawMarker, ok := envelope.Payload.Data[markerField]
	if !ok {
		return rawStorageEnvelope{}, fmt.Errorf("BaseMessage payload.data has no %q marker", markerField)
	}
	var marker string
	if err := json.Unmarshal(rawMarker, &marker); err != nil {
		return rawStorageEnvelope{}, fmt.Errorf("decode %q marker: %w", markerField, err)
	}
	if marker == "" {
		return rawStorageEnvelope{}, fmt.Errorf("BaseMessage payload.data has empty %q marker", markerField)
	}
	return rawStorageEnvelope{
		wireID:      envelope.ID,
		messageType: envelope.Type.String(),
		marker:      marker,
	}, nil
}

func rawStorageKeyNonce(key, wireID string) (string, error) {
	const prefix = coreRawStorageMessageType + "/"
	if !strings.HasPrefix(key, prefix) {
		return "", fmt.Errorf("key does not have %q prefix", prefix)
	}
	base := path.Base(key)
	wantBasePrefix := wireID + "_"
	if !strings.HasPrefix(base, wantBasePrefix) {
		return "", fmt.Errorf("basename %q does not start with wire id %q", base, wireID)
	}
	nonce := strings.TrimPrefix(base, wantBasePrefix)
	parsed, err := uuid.Parse(nonce)
	if err != nil || parsed.String() != nonce {
		return "", fmt.Errorf("basename %q does not end in a canonical UUID nonce", base)
	}
	return nonce, nil
}
