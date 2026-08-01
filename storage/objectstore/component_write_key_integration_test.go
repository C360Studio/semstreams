//go:build integration

package objectstore_test

// #741 on the production wire: the raw write lane keyed every message as
// message/YYYY/MM/DD/HH/unknown_<unixSeconds>, so two DISTINCT messages
// arriving in the same wall-clock second produced the IDENTICAL key —
// ObjectStore Put replaces and the first message was silently lost. These
// tests prove against REAL NATS that (a) same-second raw writes now persist
// as separate retrievable objects and (b) the JetStream write path keys
// decodable-but-not-ContentStorable messages from their decoded envelope.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/storage/objectstore"
)

// TestIntegration_741_SameSecondRawWritesBothRetrievable is the
// discriminating replace-semantics test: two DISTINCT raw-path writes inside
// one wall-clock second must persist as TWO objects, each retrievable with
// its own content. Against the unfixed code the keys are identical and the
// second Put silently replaces the first.
//
// Same-second window guard: the pair only discriminates when both Store
// calls land inside one wall-clock second, so a crossed boundary retries
// with fresh payloads rather than passing vacuously.
func TestIntegration_741_SameSecondRawWritesBothRetrievable(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	store, err := objectstore.NewStoreWithConfig(ctx, natsClient, objectstore.Config{
		BucketName: "TEST_741_RAW_COLLIDE",
	})
	require.NoError(t, err)
	defer store.Close()

	var keyA, keyB string
	var msgA, msgB []byte
	sameSecond := false
	for i := 0; i < 20 && !sameSecond; i++ {
		msgA = []byte(fmt.Sprintf(`{"seq":"a","attempt":%d}`, i))
		msgB = []byte(fmt.Sprintf(`{"seq":"b","attempt":%d}`, i))
		before := time.Now().Unix()
		keyA, err = store.Store(ctx, msgA)
		require.NoError(t, err)
		keyB, err = store.Store(ctx, msgB)
		require.NoError(t, err)
		sameSecond = time.Now().Unix() == before
	}
	require.True(t, sameSecond,
		"could not land two Store calls inside one wall-clock second")

	require.NotEqual(t, keyA, keyB,
		"same-second raw writes must get distinct keys: ObjectStore Put replaces "+
			"and the first message is silently lost (#741)")

	gotA, err := store.Get(ctx, keyA)
	require.NoError(t, err)
	assert.Equal(t, msgA, gotA, "first same-second write must survive intact")
	gotB, err := store.Get(ctx, keyB)
	require.NoError(t, err)
	assert.Equal(t, msgB, gotB, "second same-second write must survive intact")
}

// TestIntegration_741_JetStreamWrite_DecodedEnvelopeKeys drives the
// production JetStream write wire (real stream, the component's own durable
// consumer) with two decodable-but-not-ContentStorable messages — the
// protocol-flow shape (JSONMap output is core.json.v1). Both must persist as
// separate objects whose keys carry the decoded envelope's type and message
// ID, with the stored bytes remaining the ORIGINAL wire bytes.
func TestIntegration_741_JetStreamWrite_DecodedEnvelopeKeys(t *testing.T) {
	client := getSharedNATSClient(t)
	ctx := context.Background()

	const (
		streamName   = "OS741KEY"
		writeSubject = "os741key.write"
		bucket       = "OS741_KEY"
	)

	js, err := client.JetStream()
	require.NoError(t, err)
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name: streamName, Subjects: []string{writeSubject},
	})
	require.NoError(t, err)

	reg := payloadbuiltins.NewTestRegistry(t)
	startWriteComponent(t, ctx, client, bucket, streamName, writeSubject, reg, nil)

	type published struct {
		msg  *message.BaseMessage
		wire []byte
	}
	mkWire := func(seq int) published {
		p := message.NewGenericJSON(map[string]any{"seq": seq})
		bm := message.NewBaseMessage(p.Schema(), p, "test-741")
		wire, err := bm.MarshalJSON()
		require.NoError(t, err)
		return published{msg: bm, wire: wire}
	}
	msgs := []published{mkWire(1), mkWire(2)}
	for _, p := range msgs {
		require.NoError(t, client.PublishToStream(ctx, writeSubject, p.wire))
	}

	// Wait until both deliveries are positively acked (clean processing).
	consumerName := writeConsumerName(writeSubject)
	deadline := time.Now().Add(20 * time.Second)
	for {
		ci := consumerInfo(t, ctx, js, streamName, consumerName)
		if ci.AckFloor.Consumer == 2 && ci.NumAckPending == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("deliveries not acked: ack_floor=%d ack_pending=%d",
				ci.AckFloor.Consumer, ci.NumAckPending)
		}
		time.Sleep(100 * time.Millisecond)
	}

	obs, err := js.ObjectStore(ctx, bucket)
	require.NoError(t, err)
	entries, err := obs.List(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 2,
		"two DISTINCT messages must persist as two objects — before #741 the "+
			"colliding unknown_<seconds> key silently replaced the first")

	for _, p := range msgs {
		found := false
		for _, e := range entries {
			if !strings.Contains(e.Name, p.msg.ID()+"_") {
				continue
			}
			found = true
			assert.True(t, strings.HasPrefix(e.Name, "core.json.v1/"),
				"key must carry the decoded envelope's type, got %q", e.Name)
			got, err := obs.GetBytes(ctx, e.Name)
			require.NoError(t, err)
			assert.Equal(t, p.wire, got,
				"stored bytes must be the original wire bytes (no base64 double-encoding)")
		}
		assert.True(t, found, "no stored object keyed by message ID %s", p.msg.ID())
	}
}
