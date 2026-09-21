package agenticdispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two current loops on ONE user/channel route. This is the world the change's
// own edge-gateway requirement accepts as reachable: between a task's PubAck
// and its first durable `LoopEntity`, a second route-only message mints
// another loop, so a route can carry two nonterminal records and `activeLoop`
// refuses to pick between them.
const (
	routeLoopA = "7c9e6679-7425-40de-944b-e07fc1f90ae7"
	routeLoopB = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"
)

// routeLoop is one nonterminal record on the shared cli/channel-1 route.
func routeLoop(loopID string) *agentic.LoopEntity {
	return &agentic.LoopEntity{
		ID: loopID, TaskID: "task-" + loopID, UserID: "user-1",
		ChannelType: "cli", ChannelID: "channel-1",
		State: agentic.LoopStateExecuting, MaxIterations: 5,
	}
}

// newInstalledCommandLane binds the PRODUCTION user.message callback and hands
// back the deliver function it installed. The settlement decision these tests
// assert is taken inside that callback, not by handleUserMessage's return, so
// a test that called the handler directly could not see an Ack at all.
//
// When capture is non-nil the response publish is taken by the seam and always
// succeeds, which is what isolates the settlement decision from a publish
// failure; a nil capture leaves the production sendResponse over an
// unconnected client, so the publish fails and the same lane shows the other
// half of the gate.
func newInstalledCommandLane(t *testing.T, capture *[]agentic.UserResponse) (
	*Component, func(context.Context, jetstream.Msg), map[string]*causalConsumeHandle, context.Context,
) {
	t.Helper()
	deps := componentDependenciesForCausalTest()
	deps.PayloadRegistry = payloadbuiltins.NewTestRegistry(t)
	// A per-component metrics registry, supplied at construction rather than
	// assigned after: the shared view's hooks read c.metrics from their own
	// goroutine, so a later write is a data race (caught by -race). The shared
	// process-global instance every other nil-registry test uses could not
	// prove a counter did not move.
	deps.MetricsRegistry = metric.NewMetricsRegistry()
	discoverable, err := NewComponent([]byte(`{}`), deps)
	require.NoError(t, err)
	c := discoverable.(*Component)
	c.modelRegistry = newTestRegistry()
	// resolveConfig does not apply the advertised auto_continue default
	// (#1348), so a component built from `{}` has it false while the schema
	// says true. Every case here is about the resolution branch that flag
	// turns on, so it is set deliberately rather than inherited.
	c.config.AutoContinue = true
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	if capture != nil {
		c.sendResponseFn = func(resp agentic.UserResponse) { *capture = append(*capture, resp) }
	}
	callbacks := make(map[string]func(context.Context, jetstream.Msg))
	handles := make(map[string]*causalConsumeHandle)
	c.consumeStream = func(_ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		handle := &causalConsumeHandle{closed: make(chan struct{}), closedCalls: make(chan struct{}, 1)}
		callbacks[owner.Port] = callback
		handles[owner.Port] = handle
		return handle, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, c.setupSubscriptions(ctx))
	t.Cleanup(func() {
		cancel()
		for _, binding := range c.consumers {
			if binding.observerDone != nil {
				<-binding.observerDone
			}
		}
	})
	deliver, ok := callbacks["user.message"]
	require.True(t, ok, "production setup did not bind user.message")
	return c, deliver, handles, ctx
}

// commandDelivery encodes one command arriving on the cli/channel-1 route.
func commandDelivery(t *testing.T, messageID, content string) *dispatchSettlementMsg {
	t.Helper()
	return &dispatchSettlementMsg{data: mustMarshalDispatchSettlementPayload(t, &agentic.UserMessage{
		MessageID: messageID, ChannelType: "cli", ChannelID: "channel-1", UserID: "user-1",
		Content: content, Timestamp: time.Now().UTC(),
	})}
}

// requireLaneIntact asserts no owner was drained and no delivery-ownership
// fatal latched, so a later user message is still admitted.
func requireLaneIntact(t *testing.T, c *Component, handles map[string]*causalConsumeHandle) {
	t.Helper()
	require.Empty(t, c.Health().LastError, "a command refusal must not latch a delivery-ownership fatal")
	for port, handle := range handles {
		require.Zero(t, handle.drains.Load(), "an answered refusal must not drain owner %s", port)
	}
}

// A refusal the user can act on is an ANSWER, not a retry.
//
// `loop_route_ambiguous` is `errs.ErrorInvalid` — nontransient — and returning
// it raw put the delivery on handleUserMessage's Retry arm. The user.message
// consumer runs `MaxDeliver: 3` (component.go:573), so a bare `/cancel` on an
// ambiguous route was redelivered three times and dropped, and the user was
// told nothing at all. The redeliveries cannot help: the world they re-read is
// the same ambiguous one.
//
// The teeth are the pairing. Subtest one says the refusal is published AND the
// delivery settles on it; subtest two says the settlement is conditioned on
// that publication rather than taken for granted; subtest three says a
// transient resolver failure still retries, because that one IS answerable by
// a redelivery and has no refusal to publish.
//
// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestAmbiguousCommandTargetIsAnsweredBeforeSettling(t *testing.T) {
	t.Run("the refusal is published and the delivery settles on it", func(t *testing.T) {
		published := make([]agentic.UserResponse, 0, 1)
		c, deliver, handles, ctx := newInstalledCommandLane(t, &published)
		seedCurrentLoops(t, c, routeLoop(routeLoopA), routeLoop(routeLoopB))
		withPersistedLoops(c, nil)

		msg := commandDelivery(t, "message-ambiguous-cancel", "/cancel")
		deliver(ctx, msg)

		require.Len(t, published, 1, "the delivery settled without telling the user anything")
		assert.Equal(t, agentic.ResponseTypeError, published[0].Type)
		assert.Contains(t, published[0].Content, "multiple current loops match the user/channel route")
		assert.Equal(t, int32(1), msg.acks.Load(),
			"an answered refusal is done: retrying it burns MaxDeliver on a world that cannot change the answer")
		assert.Zero(t, msg.naks.Load()+msg.terms.Load())
		requireLaneIntact(t, c, handles)
	})

	t.Run("an unpublished refusal retries rather than settling", func(t *testing.T) {
		c, deliver, handles, ctx := newInstalledCommandLane(t, nil)
		require.Nil(t, c.sendResponseFn,
			"this case must run the production sendResponse; the seam would skip the publish it exists to observe")
		seedCurrentLoops(t, c, routeLoop(routeLoopA), routeLoop(routeLoopB))
		withPersistedLoops(c, nil)

		msg := commandDelivery(t, "message-ambiguous-cancel-unpublished", "/cancel")
		deliver(ctx, msg)

		assert.Equal(t, int32(1), msg.naks.Load(),
			"the refusal did not reach the user, and this command published nothing else: the redelivery is what answers")
		assert.Zero(t, msg.acks.Load()+msg.terms.Load())
		requireLaneIntact(t, c, handles)
	})

	t.Run("an unreadable view retries and publishes no refusal", func(t *testing.T) {
		published := make([]agentic.UserResponse, 0, 1)
		c, deliver, handles, ctx := newInstalledCommandLane(t, &published)
		withPersistedLoops(c, nil)
		// No view is seeded, so currentLoopSnapshot refuses transiently — the
		// warm-up state, not an answer.

		msg := commandDelivery(t, "message-unreadable-cancel", "/cancel")
		deliver(ctx, msg)

		require.Empty(t, published,
			"a transient failure has no refusal to publish; the redelivery is the answer")
		assert.Equal(t, int32(1), msg.naks.Load())
		assert.Zero(t, msg.acks.Load()+msg.terms.Load())
		requireLaneIntact(t, c, handles)
	})
}

// httpCommand drives the production HTTP message handler with one command on
// the cli/channel-1 route and returns the recorder.
func httpCommand(t *testing.T, c *Component, content string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"content": content, "user_id": "user-1", "channel_type": "cli", "channel_id": "channel-1",
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/message", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	c.handleHTTPMessage(rec, req)
	return rec
}

// newAmbiguousHTTPRoute builds the HTTP seam over a route carrying two
// nonterminal loops, which is the exact condition `activeLoop` refuses.
func newAmbiguousHTTPRoute(t *testing.T) *Component {
	t.Helper()
	c, _, _ := newSeamTestComponent(t)
	c.config.AutoContinue = true
	seedCurrentLoops(t, c, routeLoop(routeLoopA), routeLoop(routeLoopB))
	return c
}

// A command that takes no target must not acquire one.
//
// Both command lanes resolved an active loop before EVERY argument-less
// command, so `/loops` and `/help` — neither of which reads the loopID
// argument at all — failed on a route with two current loops: 409 on HTTP, and
// on the bus the delivery that finding 1's arm now answers. `/loops` is what a
// user runs to find the competing loop IDs, so the one command that resolves
// the ambiguity was the one the ambiguity disabled, and `/help` took a
// dependency on view readiness it has no use for.
//
// The declaration is explicit rather than inferred: `RequireLoop` cannot carry
// it, because all four built-ins declare it false, and inferring from "the
// handler ignores its loopID parameter" is not observable from the registry an
// adopter registers into.
//
// spec: agentic-dispatch / Dispatch uses one authority-backed current-state projection
func TestCommandsThatConsumeNoTargetRunUnderRouteAmbiguity(t *testing.T) {
	t.Run("bus /loops lists the competing loops", func(t *testing.T) {
		published := make([]agentic.UserResponse, 0, 1)
		c, deliver, handles, ctx := newInstalledCommandLane(t, &published)
		seedCurrentLoops(t, c, routeLoop(routeLoopA), routeLoop(routeLoopB))
		withPersistedLoops(c, nil)

		msg := commandDelivery(t, "message-ambiguous-loops", "/loops")
		deliver(ctx, msg)

		require.Len(t, published, 1)
		assert.Contains(t, published[0].Content, truncateID(routeLoopA))
		assert.Contains(t, published[0].Content, truncateID(routeLoopB),
			"the listing the user needs to name a loop must show both of them")
		assert.Equal(t, int32(1), msg.acks.Load())
		assert.Zero(t, msg.naks.Load()+msg.terms.Load())
		requireLaneIntact(t, c, handles)
	})

	t.Run("bus /help answers", func(t *testing.T) {
		published := make([]agentic.UserResponse, 0, 1)
		c, deliver, handles, ctx := newInstalledCommandLane(t, &published)
		seedCurrentLoops(t, c, routeLoop(routeLoopA), routeLoop(routeLoopB))
		withPersistedLoops(c, nil)

		msg := commandDelivery(t, "message-ambiguous-help", "/help")
		deliver(ctx, msg)

		require.Len(t, published, 1)
		assert.Contains(t, published[0].Content, "Available commands:")
		assert.Equal(t, int32(1), msg.acks.Load())
		assert.Zero(t, msg.naks.Load()+msg.terms.Load())
		requireLaneIntact(t, c, handles)
	})

	t.Run("HTTP /loops lists the competing loops", func(t *testing.T) {
		rec := httpCommand(t, newAmbiguousHTTPRoute(t), "/loops")

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), truncateID(routeLoopA))
		assert.Contains(t, rec.Body.String(), truncateID(routeLoopB))
	})

	t.Run("HTTP /help answers", func(t *testing.T) {
		rec := httpCommand(t, newAmbiguousHTTPRoute(t), "/help")

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "Available commands:")
	})

	t.Run("HTTP /help needs no loop view at all", func(t *testing.T) {
		// No view is seeded, which is the warm-up state auto_continue's
		// default reaches on every boot. /help reads no loop state, so it must
		// not inherit the refusal that state's absence produces.
		c, _, _ := newSeamTestComponent(t)
		c.config.AutoContinue = true

		rec := httpCommand(t, c, "/help")

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "Available commands:")
	})

	t.Run("a command that does take a target still resolves one", func(t *testing.T) {
		// The negative half: /status consumes the target it is given, so it
		// keeps resolving — and keeps refusing when the route is ambiguous.
		// Without this a fix that simply stopped resolving would pass.
		rec := httpCommand(t, newAmbiguousHTTPRoute(t), "/status")

		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "multiple current loops match the user/channel route")
	})
}

// The route-ambiguity refusal is the one refusal in this component that is
// neither metered nor logged where it is built, and the two doc comments that
// used to claim otherwise now say so (commands.go:61-71, component.go:1068-1072).
//
// This pins the corrected claim rather than the absence: the refusal reaches
// the user on the delivery lane and moves no series on the admission gate's
// counter, because the gate never made it — nothing was named, no record was
// read, and `activeLoop` has no seam at its site to label. Wiring it into
// `loop_admission_refusals_total` would turn this test red, which is the point:
// the comment and the metric cannot drift apart silently. The HTTP lane's own
// count of the same condition is the second subtest, so the asymmetry the
// comment describes is observed rather than asserted.
//
// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestRouteAmbiguityRefusalIsAnsweredWithoutMeteringTheGate(t *testing.T) {
	t.Run("the delivery lane answers and the gate counter stays empty", func(t *testing.T) {
		published := make([]agentic.UserResponse, 0, 1)
		c, deliver, _, ctx := newInstalledCommandLane(t, &published)
		seedCurrentLoops(t, c, routeLoop(routeLoopA), routeLoop(routeLoopB))
		withPersistedLoops(c, nil)
		require.Zero(t, testutil.CollectAndCount(c.metrics.loopAdmissionRefusals),
			"the isolated registry must start empty, or the assertion below proves nothing")

		msg := commandDelivery(t, "message-ambiguous-metering", "/cancel")
		deliver(ctx, msg)

		require.Len(t, published, 1, "the refusal must have happened for the absence below to mean anything")
		require.Equal(t, int32(1), msg.acks.Load())
		// The absence below would also hold on a component whose metrics were
		// never wired, so this delivery's own positive count comes first:
		// handleUserMessage meters every message it receives (component.go:824)
		// on the same registry, and it is the last counter this path moves —
		// the refusal returns before recordCommandExecuted.
		require.Equal(t, float64(1),
			testutil.ToFloat64(c.metrics.messagesReceived.WithLabelValues("cli")),
			"this lane's registry must be live, or the absence below proves nothing")
		require.Zero(t, testutil.CollectAndCount(c.metrics.loopAdmissionRefusals),
			"the admission gate's counter must not grow a series for a refusal the gate never made")
	})

	t.Run("the HTTP lane counts the same condition as a 409", func(t *testing.T) {
		// newSeamTestComponent already builds on a per-component registry.
		c := newAmbiguousHTTPRoute(t)

		rec := httpCommand(t, c, "/status")

		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
		require.Equal(t, float64(1),
			testutil.ToFloat64(c.metrics.httpRequestsTotal.WithLabelValues("/message", "POST", "409")),
			"the HTTP lane meters this refusal through its request counter, which the delivery lane has no analogue of")
		require.Zero(t, testutil.CollectAndCount(c.metrics.loopAdmissionRefusals))
	})
}
