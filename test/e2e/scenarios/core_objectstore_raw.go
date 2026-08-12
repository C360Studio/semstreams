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
)

type rawStorageEnvelope struct {
	wireID      string
	messageType string
	marker      string
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
