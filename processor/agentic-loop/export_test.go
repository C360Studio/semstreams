package agenticloop

import (
	"context"
	"log/slog"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TrajectoryManager is a test-only view of active-loop execution mechanics.
// Production builds expose no aggregate trajectory manager or read method.
type TrajectoryManager struct {
	manager *trajectoryManager
}

func NewTrajectoryManager() *TrajectoryManager {
	return &TrajectoryManager{manager: newTrajectoryManager()}
}

func (m *TrajectoryManager) StartTrajectory(loopID string) (agentic.Trajectory, error) {
	return m.manager.startTrajectory(loopID)
}

func (m *TrajectoryManager) AddStep(loopID string, step agentic.TrajectoryStep) (agentic.Trajectory, error) {
	return m.manager.addStep(loopID, step)
}

func (m *TrajectoryManager) GetTrajectory(loopID string) (agentic.Trajectory, error) {
	return m.manager.getTrajectory(loopID)
}

// GetTrajectory exposes active-loop state only to external-package tests.
func (h *MessageHandler) GetTrajectory(loopID string) (agentic.Trajectory, error) {
	return h.trajectoryManager.getTrajectory(loopID)
}

// SetTestPublishHook wires a capturing function onto c.testPublishHook so
// unit tests can observe wire-level publishes from publishApprovalResponseToWire
// without a real NATS connection. The hook is only ever set from test code;
// production components always have a nil hook.
func (c *Component) SetTestPublishHook(fn func(subject string, data []byte)) {
	c.testPublishHook = fn
}

// GraphWriterForTest exposes graphWriter for integration testing.
// This type wraps the unexported graphWriter so that the _test package
// can exercise the full NATS round-trip without duplicating construction logic.
type GraphWriterForTest struct {
	w *graphWriter
}

// NewGraphWriterForTest creates a graphWriter for integration tests.
func NewGraphWriterForTest(client *natsclient.Client, reg model.RegistryReader, platform types.PlatformMeta) *GraphWriterForTest {
	return &GraphWriterForTest{
		w: &graphWriter{
			natsClient:    client,
			modelRegistry: reg,
			platform:      platform,
			logger:        slog.Default(),
		},
	}
}

// SetLogger replaces the graphWriter's logger for integration tests that need
// to capture log output (e.g. verifying divergent task_id warnings).
func (g *GraphWriterForTest) SetLogger(logger *slog.Logger) {
	g.w.logger = logger
}

func (g *GraphWriterForTest) WriteModelEndpoints(ctx context.Context) { g.w.WriteModelEndpoints(ctx) }
func (g *GraphWriterForTest) WriteLoopCompletion(
	ctx context.Context, e *agentic.LoopCompletedEvent, evidenceIncomplete bool,
) {
	g.w.WriteLoopCompletion(ctx, e, evidenceIncomplete)
}
func (g *GraphWriterForTest) WriteLoopFailure(
	ctx context.Context, e *agentic.LoopFailedEvent, evidenceIncomplete bool,
) {
	g.w.WriteLoopFailure(ctx, e, evidenceIncomplete)
}
func (g *GraphWriterForTest) WriteLoopCancellation(
	ctx context.Context, e *agentic.LoopCancelledEvent, evidenceIncomplete bool,
) {
	g.w.WriteLoopCancellation(ctx, e, evidenceIncomplete)
}
func (g *GraphWriterForTest) WriteLineageTriples(ctx context.Context, loopID string, related map[string]any) error {
	return g.w.WriteLineageTriples(ctx, loopID, related)
}
func (g *GraphWriterForTest) WriteSpawnIdentity(ctx context.Context, loopID string, task *agentic.TaskMessage) error {
	return g.w.WriteSpawnIdentity(ctx, loopID, task)
}

// OutstandingRequestForTest returns the one model request the loop is waiting
// on. External-package fixtures use it to answer the request the loop actually
// published: production routes a response to its loop BY its RequestID, so a
// response naming a request the loop never minted is a state production cannot
// produce, and one naming a superseded request is refused as stale.
func (h *MessageHandler) OutstandingRequestForTest(loopID string) string {
	return h.loopManager.OutstandingRequest(loopID)
}

// CurrentRequestForTest returns the request the loop's record names, which is
// the identity the superseded-response guard compares against. A fixture that
// must reach the handler PAST that guard — a redelivery, or a response
// arriving while the loop waits on tools, where the outstanding mark is empty
// and reads "" — names this one. Asking for the outstanding request there
// would build a response naming no request at all, which production cannot
// route.
//
// It reads LoopEntity.PublishedRequestID (#1330) rather than the process-local
// mint map that used to answer this: the durable name is what the guard now
// compares against, and a test helper that answered from a different source
// could pass while production failed.
func (h *MessageHandler) CurrentRequestForTest(loopID string) string {
	entity, err := h.loopManager.GetLoop(loopID)
	if err != nil {
		return ""
	}
	return entity.PublishedRequestID
}

// CarrierStampForTest applies the CARRIER's PublishedRequestID stamp to a
// handler result, and reports the request it named.
//
// Since the owner Codex round's finding 3 (#1330 Q1) the two iteration mint
// sites no longer name the request on the loop: the Component does, after
// publishResults has PubAck'd it and before the record write, so a record can
// never name a request the stream does not retain. A fixture that drives the
// MessageHandler alone across more than one model turn has no Component, so it
// stands in for that one step; without it the next response is classified as
// naming a request the record has not reached, which is what a real
// replacement correctly retries.
//
// The request it stamps is chosen by the PRODUCTION selector (mintedRequestID)
// off the result's own published messages, never by the fixture. What it does
// not reproduce is loopRecordMu, which orders this stamp against the other
// lanes of a live process — that ordering is the subject of
// TestARecordNeverNamesARequestBeforeItsPubAck, at the carrier itself.
func (h *MessageHandler) CarrierStampForTest(result HandlerResult) (string, error) {
	minted, err := mintedRequestID(result)
	if err != nil || minted == "" {
		return "", err
	}
	return minted, h.loopManager.SetPublishedRequest(result.LoopID, minted)
}

// EnableDropCountingForTest gives the handler a metrics set, so an
// external-package fixture can observe that a refusal was COUNTED and under
// which reason — a drop nobody can see is a drop an operator cannot act on.
func (h *MessageHandler) EnableDropCountingForTest() {
	h.SetMetrics(getMetrics(metric.NewMetricsRegistry()))
}

// ModelResponseDropsForTest reads the drop counter for one reason label.
// getMetrics is a package singleton, so this value accumulates across the test
// binary: read it before and after and assert the DELTA.
func (h *MessageHandler) ModelResponseDropsForTest(reason string) float64 {
	if h.metrics == nil {
		return 0
	}
	return testutil.ToFloat64(h.metrics.modelResponsesDropped.WithLabelValues(reason))
}

// HasPendingContinuationForTest reports whether the loop still has a turn no
// request carries. External-package fixtures use it to tell "the marker is
// set" (which survives the carrying publish on purpose) from "the next
// completion will carry this turn again".
func (h *MessageHandler) HasPendingContinuationForTest(loopID string) bool {
	return h.loopManager.HasPendingContinuation(loopID)
}

// ErrResponseSupersededForTest is the sentinel HandleModelResponse returns for
// a response the loop has already moved past (#1330). External-package tests
// match on it so the refusal is asserted by identity rather than by a message.
var ErrResponseSupersededForTest = errResponseSuperseded
