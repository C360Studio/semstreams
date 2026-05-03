package governance

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMsg returns an empty nats.Msg suitable for header injection tests.
// Mirrors auth/headers_test.go newMsg helper exactly.
func newMsg() *nats.Msg {
	return &nats.Msg{Subject: "test.subject"}
}

// TestInjectAndExtract_RoundTrip verifies that a Context placed in ctx,
// injected onto a NATS message, and then extracted produces field-by-field
// equality.
func TestInjectAndExtract_RoundTrip(t *testing.T) {
	original := &Context{
		HasPii:               true,
		HasInjection:         true,
		HasContentModeration: false,
		HasRateLimiting:      true,
		ViolationCount:       3,
		MaxSeverity:          SeverityCritical,
		Allowed:              false,
	}
	ctx := WithContext(context.Background(), original)
	msg := newMsg()

	InjectGovernance(ctx, msg)
	got := ExtractGovernance(msg)

	require.NotNil(t, got)
	assert.Equal(t, original.HasPii, got.HasPii)
	assert.Equal(t, original.HasInjection, got.HasInjection)
	assert.Equal(t, original.HasContentModeration, got.HasContentModeration)
	assert.Equal(t, original.HasRateLimiting, got.HasRateLimiting)
	assert.Equal(t, original.ViolationCount, got.ViolationCount)
	assert.Equal(t, original.MaxSeverity, got.MaxSeverity)
	assert.Equal(t, original.Allowed, got.Allowed)
}

// TestInjectGovernance_NoContextInCtx verifies that InjectGovernance is a
// complete no-op when no governance context is attached to the Go context.
func TestInjectGovernance_NoContextInCtx(t *testing.T) {
	msg := newMsg()
	InjectGovernance(context.Background(), msg)

	// No headers should have been written.
	assert.Nil(t, msg.Header, "InjectGovernance must not allocate a Header map when ctx is empty")
}

// TestInjectGovernance_ZeroValueContext verifies the behavior when a
// zero-value *Context{} is in ctx. All headers are written (with
// false/0/SeverityNone values). Unlike auth/Identity where all-empty strings
// cause Extract to return nil, governance uses X-Gov-Allowed as a presence
// sentinel — so ExtractGovernance returns a zero-value Context{} (not nil)
// because the sentinel header IS present.
//
// This is a deliberate design choice: a zero-value context means "filter
// chain ran, detected nothing, result=blocked (Allowed=false)" — a real
// signal distinct from "no governance evaluation ran" (nil).
func TestInjectGovernance_ZeroValueContext(t *testing.T) {
	zero := &Context{}
	ctx := WithContext(context.Background(), zero)
	msg := newMsg()

	InjectGovernance(ctx, msg)

	// Headers were allocated and written.
	require.NotNil(t, msg.Header, "InjectGovernance must allocate Header map for zero-value context")
	assert.Equal(t, "false", msg.Header.Get(XGovAllowedHeader), "X-Gov-Allowed must be written as 'false' for zero-value context")
	assert.Equal(t, SeverityNone, msg.Header.Get(XGovMaxSeverityHeader), "empty MaxSeverity must default to SeverityNone on the wire")

	// ExtractGovernance sees the presence sentinel → returns zero-value Context, not nil.
	got := ExtractGovernance(msg)
	require.NotNil(t, got, "ExtractGovernance must return Context (not nil) when X-Gov-Allowed sentinel is present")
	assert.Equal(t, &Context{MaxSeverity: SeverityNone}, got)
}

// TestExtractGovernance_NoHeaders verifies that ExtractGovernance returns nil
// when the message has no X-Gov-* headers (specifically no X-Gov-Allowed
// sentinel).
func TestExtractGovernance_NoHeaders(t *testing.T) {
	msg := newMsg()
	// No headers set.
	assert.Nil(t, ExtractGovernance(msg))
}

// TestExtractGovernance_NilMsg verifies that ExtractGovernance returns nil
// for a nil message pointer.
func TestExtractGovernance_NilMsg(t *testing.T) {
	assert.Nil(t, ExtractGovernance(nil))
}

// TestExtractGovernance_PartialHeaders verifies that a message with only
// X-Gov-Has-Pii and X-Gov-Allowed set returns a Context with
// HasPii=true and Allowed=true; all other fields at zero/SeverityNone.
func TestExtractGovernance_PartialHeaders(t *testing.T) {
	msg := newMsg()
	msg.Header = make(nats.Header)
	msg.Header.Set(XGovAllowedHeader, "true")
	msg.Header.Set(XGovHasPiiHeader, "true")

	got := ExtractGovernance(msg)

	require.NotNil(t, got)
	assert.True(t, got.Allowed)
	assert.True(t, got.HasPii)
	assert.False(t, got.HasInjection)
	assert.False(t, got.HasContentModeration)
	assert.False(t, got.HasRateLimiting)
	assert.Equal(t, 0, got.ViolationCount)
	assert.Equal(t, SeverityNone, got.MaxSeverity, "missing MaxSeverity header must default to SeverityNone")
}

// TestExtractGovernanceFromJetStream_RoundTrip verifies the JetStream variant
// using raw nats.Header (as returned by JetStream consumers).
func TestExtractGovernanceFromJetStream_RoundTrip(t *testing.T) {
	original := &Context{
		HasPii:         false,
		HasInjection:   true,
		ViolationCount: 1,
		MaxSeverity:    SeverityMedium,
		Allowed:        false,
	}
	ctx := WithContext(context.Background(), original)
	msg := newMsg()
	InjectGovernance(ctx, msg)

	// Simulate the JetStream consumer handing us nats.Header directly.
	got := ExtractGovernanceFromJetStream(msg.Header)

	require.NotNil(t, got)
	assert.Equal(t, original.HasPii, got.HasPii)
	assert.Equal(t, original.HasInjection, got.HasInjection)
	assert.Equal(t, original.ViolationCount, got.ViolationCount)
	assert.Equal(t, original.MaxSeverity, got.MaxSeverity)
	assert.Equal(t, original.Allowed, got.Allowed)
}

// TestExtractGovernanceFromJetStream_NilHeaders verifies nil is returned for
// a nil nats.Header map.
func TestExtractGovernanceFromJetStream_NilHeaders(t *testing.T) {
	assert.Nil(t, ExtractGovernanceFromJetStream(nil))
}

// TestRoundTrip_ContextToHeadersToContext is a full end-to-end pin test of
// the wire contract: ctx → InjectGovernance onto msg → ExtractGovernance from
// msg → re-attach via WithContext → FromContext. Pins the wire contract
// end-to-end so any accidental header rename or field-mapping change breaks
// this test immediately.
func TestRoundTrip_ContextToHeadersToContext(t *testing.T) {
	// Step 1: start with a known Context in ctx.
	original := &Context{
		HasPii:               true,
		HasInjection:         false,
		HasContentModeration: true,
		HasRateLimiting:      false,
		ViolationCount:       2,
		MaxSeverity:          SeverityHigh,
		Allowed:              false,
	}
	sendCtx := WithContext(context.Background(), original)

	// Step 2: sender injects onto a NATS message.
	msg := newMsg()
	InjectGovernance(sendCtx, msg)

	// Step 3: receiver extracts from the NATS message.
	extracted := ExtractGovernance(msg)
	require.NotNil(t, extracted, "extracted Context must not be nil")

	// Step 4: receiver re-attaches extracted context to its own context.
	recvCtx := WithContext(context.Background(), extracted)

	// Step 5: downstream code reads from receiver's ctx.
	got, ok := FromContext(recvCtx)
	require.True(t, ok)
	require.NotNil(t, got)

	// Pin the wire contract field-by-field.
	assert.Equal(t, original.HasPii, got.HasPii, "HasPii must survive ctx → header → ctx")
	assert.Equal(t, original.HasInjection, got.HasInjection, "HasInjection must survive ctx → header → ctx")
	assert.Equal(t, original.HasContentModeration, got.HasContentModeration, "HasContentModeration must survive ctx → header → ctx")
	assert.Equal(t, original.HasRateLimiting, got.HasRateLimiting, "HasRateLimiting must survive ctx → header → ctx")
	assert.Equal(t, original.ViolationCount, got.ViolationCount, "ViolationCount must survive ctx → header → ctx")
	assert.Equal(t, original.MaxSeverity, got.MaxSeverity, "MaxSeverity must survive ctx → header → ctx")
	assert.Equal(t, original.Allowed, got.Allowed, "Allowed must survive ctx → header → ctx")
}
