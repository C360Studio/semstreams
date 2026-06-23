package natsclient

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/nats-io/nats.go"

	"github.com/c360studio/semstreams/pkg/errs"
)

func TestClassForHeader_AllClasses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "invalid",
			err:  errs.WrapInvalid(errors.New("bad input"), "C", "M", "validate"),
			want: ErrorClassInvalid,
		},
		{
			name: "fatal",
			err:  errs.WrapFatal(errors.New("kv gone"), "C", "M", "store"),
			want: ErrorClassFatal,
		},
		{
			name: "transient_explicit",
			err:  errs.WrapTransient(errors.New("timeout"), "C", "M", "request"),
			want: ErrorClassTransient,
		},
		{
			name: "unclassified_defaults_transient",
			err:  errors.New("plain error — pkg/errs Classify defaults to transient"),
			want: ErrorClassTransient,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classForHeader(tc.err); got != tc.want {
				t.Fatalf("classForHeader(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func TestClassifiedFromHeader_RoundTrips(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		class       string
		code        string
		msg         string
		isInvalid   bool
		isTransient bool
		isFatal     bool
		wantCode    string
	}{
		{name: "invalid", class: ErrorClassInvalid, msg: "bad input", isInvalid: true},
		{name: "fatal", class: ErrorClassFatal, msg: "kv gone", isFatal: true},
		{name: "transient", class: ErrorClassTransient, msg: "timeout", isTransient: true},
		{name: "unknown_class_falls_back_invalid", class: "weird", msg: "x", isInvalid: true},
		{name: "empty_message_synthesized", class: ErrorClassInvalid, msg: "", isInvalid: true},
		// ADR-060: a coded header sets ce.Code on the reconstructed error.
		{name: "coded_not_found", class: ErrorClassInvalid, code: "entity_not_found", msg: "not found: x", isInvalid: true, wantCode: "entity_not_found"},
		{name: "coded_revision_mismatch", class: ErrorClassInvalid, code: "revision_mismatch", msg: "revision mismatch", isInvalid: true, wantCode: "revision_mismatch"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := classifiedFromHeader(tc.class, tc.code, tc.msg)
			if err == nil {
				t.Fatal("expected non-nil error")
			}
			if errs.IsInvalid(err) != tc.isInvalid {
				t.Errorf("IsInvalid = %v, want %v (err=%v)", errs.IsInvalid(err), tc.isInvalid, err)
			}
			if errs.IsTransient(err) != tc.isTransient {
				t.Errorf("IsTransient = %v, want %v (err=%v)", errs.IsTransient(err), tc.isTransient, err)
			}
			if errs.IsFatal(err) != tc.isFatal {
				t.Errorf("IsFatal = %v, want %v (err=%v)", errs.IsFatal(err), tc.isFatal, err)
			}
			var ce *errs.ClassifiedError
			if !errors.As(err, &ce) {
				t.Fatalf("err is not a *errs.ClassifiedError: %v", err)
			}
			if ce.Code != tc.wantCode {
				t.Errorf("ce.Code = %q, want %q", ce.Code, tc.wantCode)
			}
		})
	}
}

func TestClassifyReply_NilMessage(t *testing.T) {
	t.Parallel()
	data, err := ClassifyReply(nil)
	if err != nil || data != nil {
		t.Fatalf("ClassifyReply(nil) = (%v, %v), want (nil, nil)", data, err)
	}
}

func TestClassifyReply_SuccessBody(t *testing.T) {
	t.Parallel()
	msg := &nats.Msg{Data: []byte(`{"ok":true}`), Header: nats.Header{}}
	data, err := ClassifyReply(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != `{"ok":true}` {
		t.Fatalf("data = %q, want %q", data, `{"ok":true}`)
	}
}

func TestClassifyReply_HeaderClassifiedError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		class       string
		isInvalid   bool
		isTransient bool
		isFatal     bool
	}{
		{name: "invalid", class: ErrorClassInvalid, isInvalid: true},
		{name: "transient", class: ErrorClassTransient, isTransient: true},
		{name: "fatal", class: ErrorClassFatal, isFatal: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := &nats.Msg{
				Header: nats.Header{
					HeaderStatus:     []string{HeaderStatusError},
					HeaderErrorClass: []string{tc.class},
				},
				Data: []byte("error: original handler message"),
			}
			data, err := ClassifyReply(msg)
			if data != nil {
				t.Errorf("data should be nil on error reply; got %q", data)
			}
			if err == nil {
				t.Fatal("expected non-nil error")
			}
			if errs.IsInvalid(err) != tc.isInvalid {
				t.Errorf("IsInvalid = %v, want %v", errs.IsInvalid(err), tc.isInvalid)
			}
			if errs.IsTransient(err) != tc.isTransient {
				t.Errorf("IsTransient = %v, want %v", errs.IsTransient(err), tc.isTransient)
			}
			if errs.IsFatal(err) != tc.isFatal {
				t.Errorf("IsFatal = %v, want %v", errs.IsFatal(err), tc.isFatal)
			}
			// The original message should survive into the reconstructed error.
			if !strings.Contains(err.Error(), "original handler message") {
				t.Errorf("err = %q, expected to contain original message", err)
			}
		})
	}
}

func TestClassifyReply_LegacyBodyPrefix_DefaultsInvalid(t *testing.T) {
	t.Parallel()
	// No X-Status header — caller is a pre-#93 handler. The body
	// prefix sniff treats it as invalid (the conservative default,
	// since pre-#93 has no class signal).
	msg := &nats.Msg{
		Header: nats.Header{},
		Data:   []byte("error: not found: foo.bar.baz"),
	}
	data, err := ClassifyReply(msg)
	if data != nil {
		t.Errorf("data should be nil on error reply; got %q", data)
	}
	if !errs.IsInvalid(err) {
		t.Errorf("legacy body-prefix should classify invalid; got err=%v IsInvalid=%v", err, errs.IsInvalid(err))
	}
	if !strings.Contains(err.Error(), "not found: foo.bar.baz") {
		t.Errorf("expected body message to survive; got %q", err)
	}
}

func TestRespondError_NilErr_NoOp(t *testing.T) {
	t.Parallel()
	msg := &nats.Msg{Reply: "_INBOX.fake"}
	if err := RespondError(msg, nil); err != nil {
		t.Fatalf("RespondError(_, nil) = %v, want nil no-op", err)
	}
}

func TestRespondError_MissingReply_ReturnsSentinel(t *testing.T) {
	t.Parallel()
	msg := &nats.Msg{}
	err := RespondError(msg, errors.New("any error"))
	if !errors.Is(err, errMissingReplySubject) {
		t.Fatalf("RespondError on msg without Reply = %v, want errMissingReplySubject", err)
	}
}

func TestReplyError_NilErr_NoOp(t *testing.T) {
	t.Parallel()
	// Method receiver with nil client is fine because we short-circuit
	// before touching c — no panic.
	var c *Client
	if err := c.ReplyError(nil, "any", nil); err != nil { //nolint:staticcheck // deliberately nil receiver
		t.Fatalf("ReplyError(_, _, nil) = %v, want nil no-op", err)
	}
}

func TestReplyError_EmptyReplyTo_NoOp(t *testing.T) {
	t.Parallel()
	var c *Client
	if err := c.ReplyError(nil, "", errors.New("ignored")); err != nil { //nolint:staticcheck
		t.Fatalf("ReplyError(_, \"\", _) = %v, want nil no-op", err)
	}
}

// TestLegacyBodyPrefixStable pins the wire-format byte sequence —
// any future change here is a wire break and must go through Phase 4.
func TestLegacyBodyPrefixStable(t *testing.T) {
	t.Parallel()
	want := []byte("error: ")
	if !bytes.Equal(legacyErrorBodyPrefix, want) {
		t.Fatalf("legacyErrorBodyPrefix = %q, want %q — wire break!", legacyErrorBodyPrefix, want)
	}
}

// TestClassifyReply_ReconstructedErrorPreservesInnerMessage pins the
// load-bearing Phase 2-polish contract: the reconstructed
// *errs.ClassifiedError's Error() string is the handler's clean inner
// message — NOT a leaky framework-attribution wrap. Phase 2's
// reviewer R2/R3 surfaced that gateway/graph-gateway and
// agentic-tools/executors were leaking
// "natsclient.ClassifyReply: handler error failed: <real msg>" to
// external surfaces; classifiedFromHeader now uses errs.Classified
// (bare constructor) so the inner message round-trips verbatim.
func TestClassifyReply_ReconstructedErrorPreservesInnerMessage(t *testing.T) {
	t.Parallel()
	msg := &nats.Msg{
		Header: nats.Header{
			HeaderStatus:     []string{HeaderStatusError},
			HeaderErrorClass: []string{ErrorClassInvalid},
		},
		Data: []byte("error: not found: test.entity.001"),
	}
	_, err := ClassifyReply(msg)
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	// errors.As recovery still works (class round-trip is intact).
	var ce *errs.ClassifiedError
	if !errors.As(err, &ce) {
		t.Fatalf("expected reconstructed error to be a *errs.ClassifiedError; got %T (%v)", err, err)
	}
	// Class survives.
	if !errs.IsInvalid(err) {
		t.Errorf("expected IsInvalid(err) == true; got false")
	}
	// Inner message survives verbatim — no framework attribution.
	if err.Error() != "not found: test.entity.001" {
		t.Errorf("err.Error() = %q, want %q (no framework attribution)",
			err.Error(), "not found: test.entity.001")
	}
	if strings.Contains(err.Error(), "natsclient") {
		t.Errorf("err.Error() must NOT leak natsclient attribution; got %q", err.Error())
	}
	if strings.Contains(err.Error(), "ClassifyReply") {
		t.Errorf("err.Error() must NOT leak ClassifyReply attribution; got %q", err.Error())
	}
}
