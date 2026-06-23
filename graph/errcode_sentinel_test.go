package graph

import (
	"testing"

	"github.com/c360studio/semstreams/pkg/errs"
)

// TestErrRevisionMismatchCodeMatchesConstant locks the ADR-060 sentinel's
// Code literal to graph.ErrorCodeRevisionMismatch. pkg/errs cannot import
// graph (graph imports errs — an import cycle), so the sentinel's Code is
// the bare string "revision_mismatch". This test lives in the graph
// package (which CAN see both) and fails if the two ever drift, e.g. if
// the graph constant is renamed/revalued without updating the errs literal.
func TestErrRevisionMismatchCodeMatchesConstant(t *testing.T) {
	t.Parallel()
	if errs.ErrRevisionMismatch.Code != ErrorCodeRevisionMismatch {
		t.Fatalf("errs.ErrRevisionMismatch.Code = %q, want graph.ErrorCodeRevisionMismatch = %q — "+
			"the literal in pkg/errs drifted from the graph constant (pkg/errs cannot import graph; keep them in sync)",
			errs.ErrRevisionMismatch.Code, ErrorCodeRevisionMismatch)
	}
}
