//go:build integration

package objectstore_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/examples/processors/document"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/storage/objectstore"
)

// #727: handleJetStreamWriteRequest previously acked the JetStream delivery
// UNCONDITIONALLY after processWriteMessage, so a transient ObjectStore failure
// (backend down, capacity rejection) produced an ERROR log and a positive ack —
// the message was permanently gone with no redelivery, defeating the durability
// purpose of the JetStream consumer. These tests drive the PRODUCTION wire
// (real NATS, real stream, the component's own durable consumer) and pin the
// ack decision table:
//
//	store+emit succeed        -> Ack
//	invalid (entity ID)       -> Term  (no poison redelivery loop)
//	transient store failure   -> NakWithDelay(30s) -> redelivery
//	required emit failure     -> NakWithDelay(30s) -> redelivery (store committed)
//
// Delivery timing note: the transient NAK delay is the 30s heartbeat-precedent
// constant (natsclient.ConsumeWithHeartbeat), so redelivery-observing tests
// poll consumer info with a 90s deadline (3x headroom) instead of sleeping.

// writeConsumerName mirrors the durable-consumer naming in
// Component.setupJetStreamConsumer: "objectstore-<instance>-<sanitized subject>"
// with the instance name fixed to "objectstore" by NewComponent.
func writeConsumerName(subject string) string {
	return "objectstore-objectstore-" + strings.ReplaceAll(subject, ".", "-")
}

// startWriteComponent boots a real objectstore Component whose "write" input is
// a JetStream port bound to streamName/writeSubject. The caller must have
// created the stream already (production stream creation is owned by
// config.StreamsManager, which is not in play here). Outputs and registry are
// optional (nil = raw-bytes-only component with no "stored" emit).
func startWriteComponent(
	t *testing.T,
	ctx context.Context,
	client *natsclient.Client,
	bucket, streamName, writeSubject string,
	reg *payloadregistry.Registry,
	outputs []component.PortDefinition,
) *objectstore.Component {
	t.Helper()

	cfg := objectstore.Config{
		BucketName: bucket,
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{{
				Name: "write", Config: component.JetStreamPort{StreamName: streamName, Subjects: []string{writeSubject}},
			}},
			Outputs: outputs,
		},
	}
	cfgJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	disc, err := objectstore.NewComponent(cfgJSON, component.Dependencies{
		NATSClient:      client,
		PayloadRegistry: reg,
	})
	require.NoError(t, err)
	comp, ok := disc.(*objectstore.Component)
	require.True(t, ok)
	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(ctx))
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })
	return comp
}

// consumerInfo fetches fresh info for the component's durable write consumer.
func consumerInfo(
	t *testing.T, ctx context.Context, js jetstream.JetStream, streamName, consumerName string,
) *jetstream.ConsumerInfo {
	t.Helper()
	stream, err := js.Stream(ctx, streamName)
	require.NoError(t, err)
	cons, err := stream.Consumer(ctx, consumerName)
	require.NoError(t, err)
	ci, err := cons.Info(ctx)
	require.NoError(t, err)
	return ci
}

// TestIntegration_JetStreamWrite_TransientStoreFailureNaksForRedelivery is THE
// discriminating test for #727: a real transient store failure must leave the
// delivery un-acked and redeliverable. Against the pre-fix unconditional ack it
// fails fast (ack floor advances past the message); against the fix it observes
// the ~30s NakWithDelay redelivery with the ack floor still at zero.
//
// Forcing function: the ObjectStore backing stream (OBJ_<bucket>) is shrunk to
// MaxBytes=1024 AFTER Component.Start (the boot-time D2 retention guard strips
// any pre-Start MaxBytes, so the shrink must come after), then an oversized raw
// payload is written — the server rejects the object chunk (DiscardNew), which
// surfaces from Store.Store as a wrapped transient error.
func TestIntegration_JetStreamWrite_TransientStoreFailureNaksForRedelivery(t *testing.T) {
	client := getSharedNATSClient(t)
	ctx := context.Background()

	const (
		streamName   = "OS727TRANS"
		writeSubject = "os727trans.write"
		bucket       = "OS727_TRANS"
	)

	js, err := client.JetStream()
	require.NoError(t, err)
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name: streamName, Subjects: []string{writeSubject},
	})
	require.NoError(t, err)

	startWriteComponent(t, ctx, client, bucket, streamName, writeSubject, nil, nil)

	// Shrink the backing stream post-Start so the next Put is rejected by the
	// server — a REAL storage-capacity failure, not a mocked one.
	objStream, err := js.Stream(ctx, "OBJ_"+bucket)
	require.NoError(t, err)
	info, err := objStream.Info(ctx)
	require.NoError(t, err)
	streamCfg := info.Config
	streamCfg.MaxBytes = 1024
	_, err = js.UpdateStream(ctx, streamCfg)
	require.NoError(t, err)

	// Oversized and NOT BaseMessage-decodable -> raw Store path -> PutBytes
	// rejected by the 1024-byte cap.
	payload := []byte(fmt.Sprintf(`{"filler":%q}`, strings.Repeat("x", 8192)))
	require.NoError(t, client.PublishToStream(ctx, writeSubject, payload))

	consumerName := writeConsumerName(writeSubject)
	deadline := time.Now().Add(90 * time.Second)
	for {
		ci := consumerInfo(t, ctx, js, streamName, consumerName)
		require.Zero(t, ci.AckFloor.Consumer,
			"a failed store must NOT positively ack the delivery (#727: unconditional ack permanently loses the message)")
		if ci.NumRedelivered >= 1 || ci.Delivered.Consumer >= 2 {
			break // redelivered without an ack — the durability contract held
		}
		if time.Now().After(deadline) {
			t.Fatalf("no redelivery observed within deadline: delivered=%d redelivered=%d ack_floor=%d",
				ci.Delivered.Consumer, ci.NumRedelivered, ci.AckFloor.Consumer)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// TestIntegration_JetStreamWrite_InvalidEntityIDTerminatesDelivery pins the
// poison-message branch: a ContentStorable whose entity ID fails
// ValidateEntityID is structurally invalid — retrying can never succeed — so
// the delivery must be TERMINATED (MSG_TERMINATED advisory, pending drains,
// zero redeliveries, nothing stored), not acked and not redelivery-looped.
func TestIntegration_JetStreamWrite_InvalidEntityIDTerminatesDelivery(t *testing.T) {
	client := getSharedNATSClient(t)
	ctx := context.Background()

	const (
		streamName   = "OS727INV"
		writeSubject = "os727inv.write"
		bucket       = "OS727_INV"
	)

	js, err := client.JetStream()
	require.NoError(t, err)
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name: streamName, Subjects: []string{writeSubject},
	})
	require.NoError(t, err)

	reg := payloadbuiltins.NewTestRegistry(t)
	require.NoError(t, document.RegisterPayloads(reg))
	startWriteComponent(t, ctx, client, bucket, streamName, writeSubject, reg, nil)

	// Subscribe to the server's termination advisory BEFORE publishing — the
	// authoritative, race-free signal that the consumer Term'd the delivery.
	consumerName := writeConsumerName(writeSubject)
	advSub, err := client.GetConnection().SubscribeSync(
		"$JS.EVENT.ADVISORY.CONSUMER.MSG_TERMINATED." + streamName + "." + consumerName)
	require.NoError(t, err)
	t.Cleanup(func() { _ = advSub.Unsubscribe() })

	// OrgID "-poison" passes Document.Validate (non-empty) but makes
	// Document.EntityID() start with '-', which fails ValidateEntityID's
	// first-byte rule (classified Invalid) inside StoreContent.
	doc := &document.Document{
		ID: "bad-001", Title: "Poison", Description: "invalid entity id",
		Body: "body", Category: "docs", OrgID: "-poison", Platform: "plat",
	}
	baseMsg := message.NewBaseMessage(doc.Schema(), doc, "test-source")
	msgBytes, err := baseMsg.MarshalJSON()
	require.NoError(t, err)
	require.NoError(t, client.PublishToStream(ctx, writeSubject, msgBytes))

	_, err = advSub.NextMsg(30 * time.Second)
	require.NoError(t, err,
		"expected a MSG_TERMINATED advisory: structurally invalid entity IDs must Term, not ack (message silently lost) and not NAK (poison redelivery loop)")

	// Pending drains without success and without redelivery growth.
	deadline := time.Now().Add(20 * time.Second)
	for {
		ci := consumerInfo(t, ctx, js, streamName, consumerName)
		require.Zero(t, ci.NumRedelivered, "a terminated delivery must not be redelivered")
		if ci.NumAckPending == 0 && ci.NumPending == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("consumer pending did not drain: ack_pending=%d pending=%d",
				ci.NumAckPending, ci.NumPending)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Nothing was stored — the invalid payload never committed.
	obs, err := js.ObjectStore(ctx, bucket)
	require.NoError(t, err)
	_, err = obs.List(ctx)
	require.ErrorIs(t, err, jetstream.ErrNoObjectsFound,
		"invalid payload must not leave a stored object behind")
}

// TestIntegration_JetStreamWrite_SuccessAcksDelivery pins the happy path on the
// production wire: store succeeds (no required emit configured), the delivery
// is positively acked exactly once, and no redelivery occurs.
func TestIntegration_JetStreamWrite_SuccessAcksDelivery(t *testing.T) {
	client := getSharedNATSClient(t)
	ctx := context.Background()

	const (
		streamName   = "OS727OK"
		writeSubject = "os727ok.write"
		bucket       = "OS727_OK"
	)

	js, err := client.JetStream()
	require.NoError(t, err)
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name: streamName, Subjects: []string{writeSubject},
	})
	require.NoError(t, err)

	startWriteComponent(t, ctx, client, bucket, streamName, writeSubject, nil, nil)

	require.NoError(t, client.PublishToStream(ctx, writeSubject, []byte(`{"hello":"os727"}`)))

	consumerName := writeConsumerName(writeSubject)
	deadline := time.Now().Add(20 * time.Second)
	for {
		ci := consumerInfo(t, ctx, js, streamName, consumerName)
		if ci.AckFloor.Consumer == 1 && ci.NumAckPending == 0 {
			assert.Zero(t, ci.NumRedelivered, "a clean store must ack on first delivery")
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("delivery was not positively acked: ack_floor=%d ack_pending=%d",
				ci.AckFloor.Consumer, ci.NumAckPending)
		}
		time.Sleep(100 * time.Millisecond)
	}

	obs, err := js.ObjectStore(ctx, bucket)
	require.NoError(t, err)
	entries, err := obs.List(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 1, "exactly one object stored for one delivery")
}

// TestIntegration_JetStreamWrite_StoredEmitFailureNaksAfterCommit pins the
// required-reference-publication gate: when the "stored" port is configured and
// the StorageReference publish fails, downstream never receives the ref — the
// same loss shape as a failed store — so the delivery must NAK for redelivery
// even though the object itself committed. Forcing function: the "stored"
// output is a jetstream port whose subject no stream covers, so
// PublishToStream fails with no-responders. Redelivery duplicates the stored
// object (time-keyed content key) — the documented at-least-once trade:
// duplicates beat loss.
func TestIntegration_JetStreamWrite_StoredEmitFailureNaksAfterCommit(t *testing.T) {
	client := getSharedNATSClient(t)
	ctx := context.Background()

	const (
		streamName   = "OS727EMIT"
		writeSubject = "os727emit.write"
		bucket       = "OS727_EMIT"
	)

	js, err := client.JetStream()
	require.NoError(t, err)
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name: streamName, Subjects: []string{writeSubject},
	})
	require.NoError(t, err)

	reg := payloadbuiltins.NewTestRegistry(t)
	require.NoError(t, document.RegisterPayloads(reg))
	startWriteComponent(t, ctx, client, bucket, streamName, writeSubject, reg,
		[]component.PortDefinition{{
			Name: "stored", Config: component.JetStreamPort{Subjects: []string{"os727emitstored.nostream"}}, // deliberately uncovered by any stream
		}})

	doc := &document.Document{
		ID: "ok-001", Title: "Valid", Description: "emit must gate the ack",
		Body: "body", Category: "docs", OrgID: "acme", Platform: "ops",
	}
	baseMsg := message.NewBaseMessage(doc.Schema(), doc, "test-source")
	msgBytes, err := baseMsg.MarshalJSON()
	require.NoError(t, err)
	require.NoError(t, client.PublishToStream(ctx, writeSubject, msgBytes))

	// First: the store itself committed (the failure is strictly downstream).
	obs, err := js.ObjectStore(ctx, bucket)
	require.NoError(t, err)
	commitDeadline := time.Now().Add(20 * time.Second)
	for {
		entries, listErr := obs.List(ctx)
		if listErr == nil && len(entries) >= 1 {
			break
		}
		if time.Now().After(commitDeadline) {
			t.Fatalf("object never committed to bucket %s (list err: %v)", bucket, listErr)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Then: the delivery must NOT be acked, and must be redelivered.
	consumerName := writeConsumerName(writeSubject)
	deadline := time.Now().Add(90 * time.Second)
	for {
		ci := consumerInfo(t, ctx, js, streamName, consumerName)
		require.Zero(t, ci.AckFloor.Consumer,
			"a failed required StorageReference publish must NOT positively ack the delivery (#727)")
		if ci.NumRedelivered >= 1 || ci.Delivered.Consumer >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no redelivery observed within deadline: delivered=%d redelivered=%d",
				ci.Delivered.Consumer, ci.NumRedelivered)
		}
		time.Sleep(250 * time.Millisecond)
	}
}
