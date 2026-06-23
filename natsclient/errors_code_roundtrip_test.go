package natsclient

import (
	"errors"
	"testing"

	"github.com/nats-io/nats.go"

	"github.com/c360studio/semstreams/pkg/errs"
)

// ADR-060: the X-Error-Code header round-trips the stable machine Code
// through ClassifyReply onto the reconstructed *errs.ClassifiedError, so
// errors.Is(err, ErrRevisionMismatch) and ce.Code discrimination work
// caller-side. These tests build the reply msg directly (no connection),
// mirroring TestClassifyReply_HeaderClassifiedError.

func TestClassifyReply_CodeHeader_RevisionMismatchSentinel(t *testing.T) {
	t.Parallel()
	msg := &nats.Msg{
		Header: nats.Header{
			HeaderStatus:     []string{HeaderStatusError},
			HeaderErrorClass: []string{ErrorClassInvalid},
			HeaderErrorCode:  []string{"revision_mismatch"},
		},
		Data: []byte(`{"message":"revision mismatch: expected 5, current 7"}`),
	}
	data, err := ClassifyReply(msg)
	if data != nil {
		t.Fatalf("data = %q, want nil on error reply", data)
	}
	if !errs.IsInvalid(err) {
		t.Errorf("IsInvalid = false, want true (err=%v)", err)
	}
	if !errors.Is(err, errs.ErrRevisionMismatch) {
		t.Errorf("errors.Is(err, ErrRevisionMismatch) = false, want true — the sentinel must round-trip the wire")
	}
	// The inner handler text must survive verbatim (no framework prefix).
	if errStr := err.Error(); errStr != "revision mismatch: expected 5, current 7" {
		t.Errorf("err.Error() = %q, want the verbatim handler text", errStr)
	}
}

func TestClassifyReply_CodeHeader_EntityNotFoundDiscriminator(t *testing.T) {
	t.Parallel()
	msg := &nats.Msg{
		Header: nats.Header{
			HeaderStatus:     []string{HeaderStatusError},
			HeaderErrorClass: []string{ErrorClassInvalid},
			HeaderErrorCode:  []string{"entity_not_found"},
		},
		Data: []byte(`{"message":"not found: acme.ops.robotics.gcs.drone.001"}`),
	}
	_, err := ClassifyReply(msg)
	if !errs.IsInvalid(err) {
		t.Errorf("IsInvalid = false, want true (err=%v)", err)
	}
	// entity_not_found is a ce.Code discriminator, NOT the sentinel.
	if errors.Is(err, errs.ErrRevisionMismatch) {
		t.Error("errors.Is(entity_not_found, ErrRevisionMismatch) = true, want false")
	}
	var ce *errs.ClassifiedError
	if !errors.As(err, &ce) {
		t.Fatalf("err is not *errs.ClassifiedError: %v", err)
	}
	if ce.Code != "entity_not_found" {
		t.Errorf("ce.Code = %q, want entity_not_found", ce.Code)
	}
}

// No X-Error-Code header → reconstructed error is uncoded. Pins that an
// uncoded handler error (e.g. a query exemplar that returns errs.Classified
// without a code) carries an empty ce.Code.
func TestClassifyReply_NoCodeHeader_UncodedUnchanged(t *testing.T) {
	t.Parallel()
	msg := &nats.Msg{
		Header: nats.Header{
			HeaderStatus:     []string{HeaderStatusError},
			HeaderErrorClass: []string{ErrorClassInvalid},
		},
		Data: []byte(`{"message":"bad input"}`),
	}
	_, err := ClassifyReply(msg)
	if !errs.IsInvalid(err) {
		t.Errorf("IsInvalid = false, want true (err=%v)", err)
	}
	var ce *errs.ClassifiedError
	if !errors.As(err, &ce) {
		t.Fatalf("err is not *errs.ClassifiedError: %v", err)
	}
	if ce.Code != "" {
		t.Errorf("ce.Code = %q, want empty (no code header)", ce.Code)
	}
	if errors.Is(err, errs.ErrRevisionMismatch) {
		t.Error("errors.Is(uncoded, ErrRevisionMismatch) = true, want false")
	}
}

// codeForHeader extracts the Code only when err carries a *ClassifiedError
// with a non-empty Code, so RespondError/ReplyError stamp X-Error-Code
// only then (uncoded errors stay byte-for-byte unchanged on the wire).
func TestCodeForHeader(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"coded", errs.ClassifiedCode(errs.ErrorInvalid, "entity_not_found", errors.New("x")), "entity_not_found"},
		{"uncoded_classified", errs.Classified(errs.ErrorInvalid, errors.New("x")), ""},
		{"plain", errors.New("x"), ""},
		{"wrapped_coded", errors.New("x"), ""}, // overwritten below
	}
	cases[3].err = errWrap(errs.ClassifiedCode(errs.ErrorInvalid, "revision_mismatch", errors.New("x")))
	cases[3].want = "revision_mismatch"
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := codeForHeader(tc.err); got != tc.want {
				t.Errorf("codeForHeader(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func errWrap(err error) error {
	return &wrapErr{err}
}

type wrapErr struct{ inner error }

func (w *wrapErr) Error() string { return "wrapped: " + w.inner.Error() }
func (w *wrapErr) Unwrap() error { return w.inner }
