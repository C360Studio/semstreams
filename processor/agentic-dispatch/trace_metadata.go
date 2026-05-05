package agenticdispatch

import (
	"context"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
)

// MetadataKeyTraceID is the TaskMessage.Metadata key under which the
// inbound trace_id propagates to LoopEntity.Metadata. agentic-loop's
// LoopManager copies TaskMessage.Metadata onto the LoopEntity at
// creation time, so /loops/{id} responses surface the value for free.
//
// Wedge-investigation use case: an agent loop wedges; an operator
// curls /agentic-loop/loops/<id>, reads metadata.trace_id, then curls
// /message-logger/trace/<trace_id> to walk the entire NATS message
// history that led to the wedge. Without this stamp, the trace
// chain stops at agentic-dispatch's NATS-header read site.
const MetadataKeyTraceID = "trace_id"

// stampTraceIDFromCtx copies the inbound NATS message's trace_id
// (extracted via natsclient.TraceContextFromContext) onto the
// TaskMessage's Metadata so the downstream LoopEntity.Metadata
// surfaces it for observability.
//
// No-op when:
//   - The ctx has no trace context (e.g. a synthetic in-process test
//     dispatch with no upstream trace).
//   - The ctx's trace context has an empty TraceID (pathological
//     upstream that injected the context object but didn't populate
//     the field).
//   - Metadata already has a `trace_id` key (don't-clobber an
//     explicit upstream value, e.g. a workflow command that wants to
//     attribute work to a parent trace rather than the inbound
//     submission's trace). Key-presence is what gates this — an
//     upstream that explicitly set Metadata["trace_id"] = "" is
//     respected as a deliberate empty assignment (matches the
//     don't-clobber convention used in
//     processor/agentic-loop/handlers.go:dispatchToolCall).
//
// The natsclient layer extracts trace context at every Subscribe /
// SubscribeForRequests / ConsumeStreamWithConfig site automatically,
// so any handler reached via NATS sees the trace context on its ctx.
// HTTP-originated dispatch (processTaskSubmissionSync) inherits the
// same propagation: the gateway / dispatch HTTP layer attaches
// trace context to ctx before calling into this code.
func stampTraceIDFromCtx(ctx context.Context, task *agentic.TaskMessage) {
	tc, ok := natsclient.TraceContextFromContext(ctx)
	if !ok || tc == nil || tc.TraceID == "" {
		return
	}
	if task.Metadata == nil {
		task.Metadata = map[string]any{}
	}
	if _, present := task.Metadata[MetadataKeyTraceID]; present {
		return
	}
	task.Metadata[MetadataKeyTraceID] = tc.TraceID
}
