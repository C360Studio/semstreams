package auth

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMsg returns an empty nats.Msg suitable for header injection tests.
func newMsg() *nats.Msg {
	return &nats.Msg{Subject: "test.subject"}
}

// TestInjectAndExtract_RoundTrip verifies that an identity placed in ctx,
// injected onto a NATS message, and then extracted produces field-by-field
// equality (modulo Source, which ExtractIdentity stamps as SourceHTTPHeader
// to mark wire-origin for downstream audit triples).
func TestInjectAndExtract_RoundTrip(t *testing.T) {
	original := &Identity{ID: "alice", Role: "admin", Org: "acme", Source: SourceCtx}
	ctx := WithIdentity(context.Background(), original)
	msg := newMsg()

	InjectIdentity(ctx, msg)
	got := ExtractIdentity(msg)

	require.NotNil(t, got)
	assert.Equal(t, "alice", got.ID)
	assert.Equal(t, "admin", got.Role)
	assert.Equal(t, "acme", got.Org)
	// ExtractIdentity always stamps SourceHTTPHeader to mark wire-origin.
	assert.Equal(t, SourceHTTPHeader, got.Source)
}

// TestInjectIdentity_NoIdentityInCtx verifies that InjectIdentity is a
// complete no-op when no identity is attached to the context.
func TestInjectIdentity_NoIdentityInCtx(t *testing.T) {
	msg := newMsg()
	InjectIdentity(context.Background(), msg)

	// No headers should have been written.
	assert.Nil(t, msg.Header, "InjectIdentity must not allocate a Header map when ctx is empty")
}

// TestInjectIdentity_ZeroValueIdentity verifies the behavior when a
// zero-value *Identity{} is in ctx. All four headers are written (with
// empty-string values) so receivers see "caller was present but had no
// claims" rather than "no identity header at all". This is a deliberate
// design choice: writing empty-string headers allows ExtractIdentity to
// return nil (all fields empty → no meaningful identity) while still
// distinguishing "header present, empty" from "header absent".
//
// NOTE: because all three non-Source fields are empty, ExtractIdentity
// returns nil per its "nil when all fields empty" contract, even though
// InjectIdentity wrote the headers. This test documents both behaviors.
func TestInjectIdentity_ZeroValueIdentity(t *testing.T) {
	zero := &Identity{}
	ctx := WithIdentity(context.Background(), zero)
	msg := newMsg()

	InjectIdentity(ctx, msg)

	// Headers were allocated and written (even if empty strings).
	require.NotNil(t, msg.Header, "InjectIdentity must allocate Header map for zero-value identity")

	// ExtractIdentity sees all-empty fields → returns nil.
	got := ExtractIdentity(msg)
	assert.Nil(t, got, "ExtractIdentity returns nil when all header values are empty")
}

// TestExtractIdentity_NoHeaders verifies that ExtractIdentity returns nil
// when the message has no X-Caller-* headers.
func TestExtractIdentity_NoHeaders(t *testing.T) {
	msg := newMsg()
	// No headers set.
	assert.Nil(t, ExtractIdentity(msg))
}

// TestExtractIdentity_NilMsg verifies that ExtractIdentity returns nil
// for a nil message pointer.
func TestExtractIdentity_NilMsg(t *testing.T) {
	assert.Nil(t, ExtractIdentity(nil))
}

// TestExtractIdentity_PartialHeaders verifies that a message with only
// X-Caller-Id set returns an Identity with ID populated and Role/Org
// empty, with Source = SourceHTTPHeader.
func TestExtractIdentity_PartialHeaders(t *testing.T) {
	msg := newMsg()
	msg.Header = make(nats.Header)
	msg.Header.Set(XCallerIDHeader, "partial-user")

	got := ExtractIdentity(msg)

	require.NotNil(t, got)
	assert.Equal(t, "partial-user", got.ID)
	assert.Empty(t, got.Role)
	assert.Empty(t, got.Org)
	assert.Equal(t, SourceHTTPHeader, got.Source)
}

// TestExtractIdentityFromJetStream_RoundTrip verifies the JetStream
// variant using raw nats.Header (as returned by JetStream consumers).
func TestExtractIdentityFromJetStream_RoundTrip(t *testing.T) {
	original := &Identity{ID: "svc-acct", Role: "viewer", Org: "widgets"}
	ctx := WithIdentity(context.Background(), original)
	msg := newMsg()
	InjectIdentity(ctx, msg)

	// Simulate the JetStream consumer handing us nats.Header directly.
	got := ExtractIdentityFromJetStream(msg.Header)

	require.NotNil(t, got)
	assert.Equal(t, "svc-acct", got.ID)
	assert.Equal(t, "viewer", got.Role)
	assert.Equal(t, "widgets", got.Org)
	assert.Equal(t, SourceHTTPHeader, got.Source)
}

// TestExtractIdentityFromJetStream_NilHeaders verifies nil is returned
// for a nil nats.Header map.
func TestExtractIdentityFromJetStream_NilHeaders(t *testing.T) {
	assert.Nil(t, ExtractIdentityFromJetStream(nil))
}

// TestRoundTrip_ContextToHeadersToContext is a full end-to-end pin test
// of the wire contract: ctx → InjectIdentity onto msg → ExtractIdentity
// from msg → re-attach via WithIdentity → IdentityFromContext.
// It pins the wire contract end-to-end so any accidental header rename
// or field-mapping change breaks this test immediately.
func TestRoundTrip_ContextToHeadersToContext(t *testing.T) {
	// Step 1: start with a known identity in ctx.
	original := &Identity{ID: "end-to-end-user", Role: "operator", Org: "test-org"}
	sendCtx := WithIdentity(context.Background(), original)

	// Step 2: sender injects onto a NATS message.
	msg := newMsg()
	InjectIdentity(sendCtx, msg)

	// Step 3: receiver extracts from the NATS message.
	extracted := ExtractIdentity(msg)
	require.NotNil(t, extracted, "extracted identity must not be nil")

	// Step 4: receiver re-attaches extracted identity to its own context.
	recvCtx := WithIdentity(context.Background(), extracted)

	// Step 5: downstream code reads from receiver's ctx.
	got, ok := IdentityFromContext(recvCtx)
	require.True(t, ok)
	require.NotNil(t, got)

	// Pin the wire contract field-by-field.
	assert.Equal(t, "end-to-end-user", got.ID, "ID must survive ctx → header → ctx")
	assert.Equal(t, "operator", got.Role, "Role must survive ctx → header → ctx")
	assert.Equal(t, "test-org", got.Org, "Org must survive ctx → header → ctx")
	// Source is stamped as SourceHTTPHeader by ExtractIdentity (wire-origin marker).
	assert.Equal(t, SourceHTTPHeader, got.Source, "Source must be SourceHTTPHeader after wire transit")
}
