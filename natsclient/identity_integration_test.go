//go:build integration

package natsclient

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/auth"
)

// testIdentity is a reusable fixture for identity propagation tests.
var testIdentity = &auth.Identity{
	ID:     "alice",
	Role:   "admin",
	Org:    "acme",
	Source: auth.SourceHTTPHeader,
}

// TestIntegration_Subscribe_ExtractsIdentityToCtx publishes a NATS message
// with X-Caller-* headers set manually, then verifies the Subscribe handler's
// context has the Identity attached via auth.IdentityFromContext.
func TestIntegration_Subscribe_ExtractsIdentityToCtx(t *testing.T) {
	ctx := context.Background()

	natsContainer, natsURL := startNATSContainer(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	subject := "identity.extract.subscribe"

	var (
		mu         sync.Mutex
		receivedID *auth.Identity
		received   bool
	)

	_, err = client.Subscribe(ctx, subject, func(msgCtx context.Context, _ *nats.Msg) {
		id, ok := auth.IdentityFromContext(msgCtx)
		mu.Lock()
		if ok {
			receivedID = id
		}
		received = true
		mu.Unlock()
	})
	require.NoError(t, err)

	// Publish a message with X-Caller-* headers set directly (simulates
	// a remote publisher that has already injected identity).
	nc := client.conn
	require.NotNil(t, nc)
	msg := nats.NewMsg(subject)
	msg.Header = make(nats.Header)
	msg.Header.Set(auth.XCallerIDHeader, testIdentity.ID)
	msg.Header.Set(auth.XCallerRoleHeader, testIdentity.Role)
	msg.Header.Set(auth.XCallerOrgHeader, testIdentity.Org)
	msg.Header.Set(auth.XCallerSourceHeader, testIdentity.Source)
	err = nc.PublishMsg(msg)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return received
	}, 3*time.Second, 20*time.Millisecond)

	mu.Lock()
	gotID := receivedID
	mu.Unlock()

	require.NotNil(t, gotID, "identity should be extracted from NATS headers into handler ctx")
	assert.Equal(t, testIdentity.ID, gotID.ID)
	assert.Equal(t, testIdentity.Role, gotID.Role)
	assert.Equal(t, testIdentity.Org, gotID.Org)
	// ExtractIdentity always stamps SourceHTTPHeader as the wire-origin marker.
	assert.Equal(t, auth.SourceHTTPHeader, gotID.Source)
}

// TestIntegration_Publish_InjectsIdentity publishes via Client.Publish from a
// ctx populated with *Identity, subscribes to the subject directly on the
// underlying nats.Conn (bypassing the extract path), and verifies X-Caller-*
// headers are present on the wire.
func TestIntegration_Publish_InjectsIdentity(t *testing.T) {
	ctx := context.Background()

	natsContainer, natsURL := startNATSContainer(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	subject := "identity.inject.publish"
	done := make(chan *nats.Msg, 1)

	// Subscribe directly on the underlying nats.Conn to capture raw headers.
	_, err = client.conn.Subscribe(subject, func(msg *nats.Msg) {
		done <- msg
	})
	require.NoError(t, err)

	ctxWithID := auth.WithIdentity(ctx, testIdentity)
	err = client.Publish(ctxWithID, subject, []byte("payload"))
	require.NoError(t, err)

	select {
	case msg := <-done:
		require.NotNil(t, msg.Header, "message should carry headers")
		assert.Equal(t, testIdentity.ID, msg.Header.Get(auth.XCallerIDHeader))
		assert.Equal(t, testIdentity.Role, msg.Header.Get(auth.XCallerRoleHeader))
		assert.Equal(t, testIdentity.Org, msg.Header.Get(auth.XCallerOrgHeader))
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for published message")
	}
}

// TestIntegration_PublishToStream_InjectsIdentity verifies that
// Client.PublishToStream writes X-Caller-* headers when the ctx carries an
// *Identity.
func TestIntegration_PublishToStream_InjectsIdentity(t *testing.T) {
	ctx := context.Background()

	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	subject := "identity.inject.stream"
	streamCfg := jetstream.StreamConfig{
		Name:     "IDENTITY_INJECT_STREAM",
		Subjects: []string{subject},
		Storage:  jetstream.MemoryStorage,
	}
	_, err = client.EnsureStream(ctx, streamCfg)
	require.NoError(t, err)

	ctxWithID := auth.WithIdentity(ctx, testIdentity)
	err = client.PublishToStream(ctxWithID, subject, []byte("payload"))
	require.NoError(t, err)

	js, err := client.JetStream()
	require.NoError(t, err)

	consumer, err := js.CreateOrUpdateConsumer(ctx, "IDENTITY_INJECT_STREAM", jetstream.ConsumerConfig{
		FilterSubject: subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)

	msg, err := consumer.Next(jetstream.FetchMaxWait(3 * time.Second))
	require.NoError(t, err)
	_ = msg.Ack()

	headers := msg.Headers()
	require.NotNil(t, headers, "message should carry headers")
	assert.Equal(t, testIdentity.ID, headers.Get(auth.XCallerIDHeader))
	assert.Equal(t, testIdentity.Role, headers.Get(auth.XCallerRoleHeader))
	assert.Equal(t, testIdentity.Org, headers.Get(auth.XCallerOrgHeader))
}

// TestIntegration_PublishToStreamWithAck_InjectsIdentity mirrors the
// PublishToStream test using the WithAck variant (stream.go path).
func TestIntegration_PublishToStreamWithAck_InjectsIdentity(t *testing.T) {
	ctx := context.Background()

	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	subject := "identity.inject.streamack"
	streamCfg := jetstream.StreamConfig{
		Name:     "IDENTITY_INJECT_STREAMACK",
		Subjects: []string{subject},
		Storage:  jetstream.MemoryStorage,
	}
	_, err = client.EnsureStream(ctx, streamCfg)
	require.NoError(t, err)

	ctxWithID := auth.WithIdentity(ctx, testIdentity)
	ack, err := client.PublishToStreamWithAck(ctxWithID, subject, []byte("payload"))
	require.NoError(t, err)
	assert.Greater(t, ack.Sequence, uint64(0))

	js, err := client.JetStream()
	require.NoError(t, err)

	consumer, err := js.CreateOrUpdateConsumer(ctx, "IDENTITY_INJECT_STREAMACK", jetstream.ConsumerConfig{
		FilterSubject: subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)

	msg, err := consumer.Next(jetstream.FetchMaxWait(3 * time.Second))
	require.NoError(t, err)
	_ = msg.Ack()

	headers := msg.Headers()
	require.NotNil(t, headers, "message should carry headers")
	assert.Equal(t, testIdentity.ID, headers.Get(auth.XCallerIDHeader))
	assert.Equal(t, testIdentity.Role, headers.Get(auth.XCallerRoleHeader))
	assert.Equal(t, testIdentity.Org, headers.Get(auth.XCallerOrgHeader))
}

// TestIntegration_ConsumeStreamWithConfig_ExtractsIdentityFromJetStream
// exercises the JetStream-consumer extract path (nats.Header shape, distinct
// from the *nats.Msg path used by Subscribe). Publishes with identity headers
// set directly, consumes via ConsumeStreamWithConfig, and verifies the handler
// ctx carries the restored Identity.
func TestIntegration_ConsumeStreamWithConfig_ExtractsIdentityFromJetStream(t *testing.T) {
	ctx := context.Background()

	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	subject := "identity.extract.jetstream"
	streamCfg := jetstream.StreamConfig{
		Name:     "IDENTITY_EXTRACT_JS",
		Subjects: []string{subject},
		Storage:  jetstream.MemoryStorage,
	}
	_, err = client.EnsureStream(ctx, streamCfg)
	require.NoError(t, err)

	// Publish a message with identity headers set directly (bypasses inject
	// path — tests the extract path in isolation).
	js, err := client.JetStream()
	require.NoError(t, err)

	rawMsg := &nats.Msg{Subject: subject, Data: []byte("payload")}
	rawMsg.Header = make(nats.Header)
	rawMsg.Header.Set(auth.XCallerIDHeader, testIdentity.ID)
	rawMsg.Header.Set(auth.XCallerRoleHeader, testIdentity.Role)
	rawMsg.Header.Set(auth.XCallerOrgHeader, testIdentity.Org)
	_, err = js.PublishMsg(ctx, rawMsg)
	require.NoError(t, err)

	var (
		mu         sync.Mutex
		receivedID *auth.Identity
	)
	done := make(chan struct{})

	cfg := StreamConsumerConfig{
		StreamName:    "IDENTITY_EXTRACT_JS",
		ConsumerName:  "identity-extract-consumer",
		FilterSubject: subject,
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
	}
	err = client.ConsumeStreamWithConfig(ctx, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		id, ok := auth.IdentityFromContext(msgCtx)
		mu.Lock()
		if ok {
			receivedID = id
		}
		mu.Unlock()
		_ = msg.Ack()
		close(done)
	})
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for JetStream message")
	}

	mu.Lock()
	gotID := receivedID
	mu.Unlock()

	require.NotNil(t, gotID, "identity should be extracted from JetStream headers into handler ctx")
	assert.Equal(t, testIdentity.ID, gotID.ID)
	assert.Equal(t, testIdentity.Role, gotID.Role)
	assert.Equal(t, testIdentity.Org, gotID.Org)
	assert.Equal(t, auth.SourceHTTPHeader, gotID.Source)
}

// TestIntegration_PublishWithoutIdentity_NoHeadersWritten verifies that
// InjectIdentity is a true no-op when no *Identity is in the ctx: no
// X-Caller-* headers appear on the wire.
func TestIntegration_PublishWithoutIdentity_NoHeadersWritten(t *testing.T) {
	ctx := context.Background()

	natsContainer, natsURL := startNATSContainer(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	subject := "identity.noop.publish"
	done := make(chan *nats.Msg, 1)

	_, err = client.conn.Subscribe(subject, func(msg *nats.Msg) {
		done <- msg
	})
	require.NoError(t, err)

	// Publish from a plain context — no WithIdentity call.
	err = client.Publish(ctx, subject, []byte("no-identity"))
	require.NoError(t, err)

	select {
	case msg := <-done:
		if msg.Header != nil {
			assert.Empty(t, msg.Header.Get(auth.XCallerIDHeader), "X-Caller-Id should be absent")
			assert.Empty(t, msg.Header.Get(auth.XCallerRoleHeader), "X-Caller-Role should be absent")
			assert.Empty(t, msg.Header.Get(auth.XCallerOrgHeader), "X-Caller-Org should be absent")
		}
		// msg.Header == nil is also acceptable (no headers at all)
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for published message")
	}
}

// TestIntegration_RoundTrip_PublishWithIdentity_SubscribeRecoversCtx
// is the full end-to-end contract test: ctx with *Identity → Publish →
// Subscribe → ctx with Identity restored. Pins the wire contract through
// the natsclient layer without manual header manipulation.
func TestIntegration_RoundTrip_PublishWithIdentity_SubscribeRecoversCtx(t *testing.T) {
	ctx := context.Background()

	natsContainer, natsURL := startNATSContainer(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close(ctx)

	subject := "identity.roundtrip"

	var (
		mu         sync.Mutex
		receivedID *auth.Identity
	)
	done := make(chan struct{})

	_, err = client.Subscribe(ctx, subject, func(msgCtx context.Context, _ *nats.Msg) {
		id, ok := auth.IdentityFromContext(msgCtx)
		mu.Lock()
		if ok {
			receivedID = id
		}
		mu.Unlock()
		close(done)
	})
	require.NoError(t, err)

	// Publish from a ctx that carries the identity. The inject path stamps
	// X-Caller-* headers; the extract path on the subscribe side reads
	// them back into a new ctx.
	ctxWithID := auth.WithIdentity(ctx, testIdentity)
	err = client.Publish(ctxWithID, subject, []byte("hello"))
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for round-trip message")
	}

	mu.Lock()
	gotID := receivedID
	mu.Unlock()

	require.NotNil(t, gotID, "identity should survive the full publish→subscribe round-trip")
	assert.Equal(t, testIdentity.ID, gotID.ID)
	assert.Equal(t, testIdentity.Role, gotID.Role)
	assert.Equal(t, testIdentity.Org, gotID.Org)
	// Source is re-stamped as SourceHTTPHeader by ExtractIdentity (wire-origin marker).
	assert.Equal(t, auth.SourceHTTPHeader, gotID.Source)
}
