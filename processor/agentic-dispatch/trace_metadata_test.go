package agenticdispatch

import (
	"context"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
)

// TestStampTraceIDFromCtx_Stamps verifies the happy path: an inbound
// ctx carrying a trace_id (the shape natsclient produces at every
// Subscribe site) propagates to TaskMessage.Metadata. The downstream
// agentic-loop component copies TaskMessage.Metadata onto
// LoopEntity.Metadata at creation time, so /loops/{id} surfaces the
// value for the wedge-investigation use case semspec described.
func TestStampTraceIDFromCtx_Stamps(t *testing.T) {
	const want = "0af7651916cd43dd8448eb211c80319c"
	tc := &natsclient.TraceContext{TraceID: want}
	ctx := natsclient.ContextWithTrace(context.Background(), tc)

	task := &agentic.TaskMessage{}
	stampTraceIDFromCtx(ctx, task)

	if got, ok := task.Metadata[MetadataKeyTraceID]; !ok {
		t.Fatalf("Metadata[%q] not set", MetadataKeyTraceID)
	} else if got != want {
		t.Errorf("Metadata[%q] = %v, want %q", MetadataKeyTraceID, got, want)
	}
}

// TestStampTraceIDFromCtx_NoCtxTrace_NoOp verifies that a ctx with
// NO trace context (synthetic in-process dispatch, no upstream
// instrumentation) leaves the TaskMessage untouched. The downstream
// /loops/{id} response simply won't carry a trace_id field — no
// crash, no empty-string poisoning of the metadata map.
func TestStampTraceIDFromCtx_NoCtxTrace_NoOp(t *testing.T) {
	task := &agentic.TaskMessage{}
	stampTraceIDFromCtx(context.Background(), task)

	if task.Metadata != nil {
		t.Errorf("Metadata = %v, want nil (no ctx trace = no Metadata mutation)", task.Metadata)
	}
}

// TestStampTraceIDFromCtx_EmptyTraceID_NoOp covers the edge where a
// TraceContext is present but its TraceID field is empty (a
// pathological upstream that injected the trace context object but
// didn't populate it). Symmetric with the no-ctx case: don't write
// an empty trace_id key.
func TestStampTraceIDFromCtx_EmptyTraceID_NoOp(t *testing.T) {
	ctx := natsclient.ContextWithTrace(context.Background(), &natsclient.TraceContext{})

	task := &agentic.TaskMessage{}
	stampTraceIDFromCtx(ctx, task)

	if task.Metadata != nil {
		t.Errorf("Metadata = %v, want nil for empty TraceID", task.Metadata)
	}
}

// TestStampTraceIDFromCtx_DoesNotClobberExplicit pins the
// don't-clobber guard: a workflow command that wants to attribute
// work to a parent trace (rather than the inbound submission's
// trace) sets task.Metadata["trace_id"] explicitly before calling
// the dispatch helper, and that value must survive.
func TestStampTraceIDFromCtx_DoesNotClobberExplicit(t *testing.T) {
	const explicit = "explicit-parent-trace"
	const fromCtx = "0af7651916cd43dd8448eb211c80319c"

	tc := &natsclient.TraceContext{TraceID: fromCtx}
	ctx := natsclient.ContextWithTrace(context.Background(), tc)

	task := &agentic.TaskMessage{
		Metadata: map[string]any{
			MetadataKeyTraceID: explicit,
			"deliverable_type": "report", // unrelated key must also survive
		},
	}
	stampTraceIDFromCtx(ctx, task)

	if got := task.Metadata[MetadataKeyTraceID]; got != explicit {
		t.Errorf("Metadata[%q] = %v, want explicit %q (don't-clobber violated)", MetadataKeyTraceID, got, explicit)
	}
	if got := task.Metadata["deliverable_type"]; got != "report" {
		t.Errorf("unrelated metadata key was modified: deliverable_type = %v", got)
	}
}

// TestStampTraceIDFromCtx_InitialisesNilMetadata verifies the
// constructor path: a TaskMessage with nil Metadata gets the map
// initialised before the trace_id write.
func TestStampTraceIDFromCtx_InitialisesNilMetadata(t *testing.T) {
	tc := &natsclient.TraceContext{TraceID: "abc"}
	ctx := natsclient.ContextWithTrace(context.Background(), tc)

	task := &agentic.TaskMessage{Metadata: nil}
	stampTraceIDFromCtx(ctx, task)

	if task.Metadata == nil {
		t.Fatal("Metadata still nil after stamp; expected initialised map")
	}
	if got := task.Metadata[MetadataKeyTraceID]; got != "abc" {
		t.Errorf("Metadata[%q] = %v, want %q", MetadataKeyTraceID, got, "abc")
	}
}
