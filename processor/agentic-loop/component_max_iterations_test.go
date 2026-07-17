package agenticloop

import (
	"errors"
	"fmt"
	"testing"

	"github.com/c360studio/semstreams/pkg/errs"
)

// TestFailureReasonForHandlerError covers the gh#529 reason-mapping
// contract: Component.handleResponseMessage's generic failure path must
// classify HandleModelResponse errors by type (errors.Is against the
// typed sentinel ErrMaxIterationsReached), never by matching error text.
// Table-driven over the exact production wrapping shape (errs.WrapFatal,
// mirroring the real call at handlers.go's model-response guard), a
// plain unrelated error, and a different unrelated sentinel — the last
// case guards against a loose match (e.g. accidental substring check on
// "max_iterations" in an unrelated error's Error() string).
func TestFailureReasonForHandlerError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "sentinel wrapped exactly as the production guard wraps it",
			err: errs.WrapFatal(
				fmt.Errorf("%w: loop loop-001 at 5/5 iterations", ErrMaxIterationsReached),
				"agentic-loop", "HandleModelResponse", "check max iterations",
			),
			want: "max_iterations",
		},
		{
			name: "bare sentinel",
			err:  ErrMaxIterationsReached,
			want: "max_iterations",
		},
		{
			name: "unrelated plain error falls through to handler_error",
			err:  errors.New("model endpoint returned 500"),
			want: "handler_error",
		},
		{
			name: "unrelated error whose text happens to contain the reason string does NOT match by substring",
			err:  errors.New("this is not a max_iterations failure, just text that mentions it"),
			want: "handler_error",
		},
		{
			name: "unrelated sentinel from another subsystem does not false-match",
			err:  errs.WrapFatal(ErrGovernancePublishFailed, "agentic-loop", "handleResponseMessage", "governance"),
			want: "handler_error",
		},
		{
			name: "nil error is defensively handler_error (should not be called with nil in practice)",
			err:  nil,
			want: "handler_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := failureReasonForHandlerError(tt.err)
			if got != tt.want {
				t.Errorf("failureReasonForHandlerError(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}
