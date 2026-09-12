//go:build integration

package agenticdispatch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

// spec: agentic-dispatch / The shared view separates current authority from activity
// This exercises real KV replay and the started dispatch control owner. The
// records are native persisted fixtures, not a claimed agent conversation.
func TestIntegrationMixedLoopBucketSharesOneCurrentView(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	tc := natsclient.NewTestClient(t, natsclient.WithKVBuckets(defaultAgentLoopsBucket(t)), natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>"}},
		natsclient.TestStreamConfig{Name: "VIEW_USER_INPUT", Subjects: []string{"user.message.>"}},
		natsclient.TestStreamConfig{Name: "VIEW_USER_OUTPUT", Subjects: []string{"callback.response.>"}},
	))
	kv, err := tc.GetKVBucket(ctx, defaultAgentLoopsBucket(t))
	require.NoError(t, err)
	for _, id := range []string{admissionLoopA, admissionLoopB} {
		putLoopRecord(t, ctx, kv, agentic.LoopEntity{ID: id, UserID: "user", ChannelType: "http", ChannelID: "channel", State: agentic.LoopStateExecuting, MaxIterations: 3})
	}
	_, err = kv.Put(ctx, "COMPLETE_"+admissionLoopA, loopCompletionJSON(t, admissionLoopA))
	require.NoError(t, err)
	unsupported := terminalEnvelopeForDispatch(t, &agentic.LoopCreatedEvent{LoopID: admissionLoopB, TaskID: "task"})
	_, err = kv.Put(ctx, "COMPLETE_"+admissionLoopB, unsupported)
	require.NoError(t, err)
	for _, key := range []string{"research.request.received." + admissionLoopA, "other.current." + admissionLoopA, "unknown_record"} {
		_, err = kv.Put(ctx, key, []byte("not loop JSON"))
		require.NoError(t, err)
	}
	hooks := newActivityHookChans()
	c := startProductionTerminalDispatch(t, ctx, tc, "AGENT", "VIEW_USER_INPUT", "VIEW_USER_OUTPUT", "mixed-view", func(c *Component) {
		c.activityTestHooks = hooks.hooks()
		c.config.AutoContinue = true
	})
	decoded, err := c.decoder.Decode(unsupported)
	require.NoError(t, err, "the unsupported completion must be a valid registered envelope")
	require.NoError(t, decoded.Validate())
	require.IsType(t, &agentic.LoopCreatedEvent{}, decoded.Payload())
	view, err := c.ensureActivityView(ctx)
	require.NoError(t, err)
	require.NoError(t, view.WaitCaughtUp(ctx))
	assertNativeLoopList(t, c, http.StatusOK, 2)
	clients := []*activityClient{startActivityClient(t, c), startActivityClient(t, c)}
	for _, client := range clients {
		client.rec.waitFor(t, `"result":"42"`)
		client.rec.waitFor(t, "Malformed loop entry: COMPLETE_"+admissionLoopB)
	}
	snapshot, err := c.currentLoopSnapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snapshot.Poisoned, 1)
	require.Contains(t, snapshot.Poisoned, "COMPLETE_"+admissionLoopB)
	require.Len(t, snapshot.Entries, 3, "only two current records and one valid activity completion")
	completion := snapshot.Entries["COMPLETE_"+admissionLoopA].Value
	require.Nil(t, completion.entity, "completion activity never supplies current authority")
	require.Equal(t, admissionLoopA, completion.loop.LoopID)
	require.Equal(t, "success", completion.loop.Outcome)
	require.Equal(t, "42", completion.loop.Result)
	require.Equal(t, 3, completion.loop.Iterations)
	kvStream, err := tc.Client.GetStream(ctx, "KV_"+defaultAgentLoopsBucket(t))
	require.NoError(t, err)
	before, err := kv.Get(ctx, admissionLoopA)
	require.NoError(t, err)
	assertNativeAutoContinueRefusal(t, c, http.StatusConflict)
	info, err := kvStream.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, info.State.Consumers, "list, AutoContinue and both SSE clients share one native watcher")
	after, err := kv.Get(ctx, admissionLoopA)
	require.NoError(t, err)
	require.Equal(t, before.Revision(), after.Revision())
	require.Equal(t, before.Value(), after.Value())
	_, err = kv.Put(ctx, admissionLoopA, []byte("{malformed"))
	require.NoError(t, err)
	hooks.waitPoisonKey(t, admissionLoopA)
	assertNativeLoopList(t, c, http.StatusServiceUnavailable, 0)
	assertNativeAutoContinueRefusal(t, c, http.StatusServiceUnavailable)
	_, err = kv.Put(ctx, admissionLoopA, before.Value())
	require.NoError(t, err)
	hooks.waitAppliedKey(t, admissionLoopA)
	require.Eventually(t, func() bool {
		snapshot, err := c.currentLoopSnapshot(ctx)
		return err == nil && len(snapshot.Poisoned) == 1 && snapshot.Poisoned[admissionLoopA] == nil
	}, time.Second, time.Millisecond)
	assertNativeLoopList(t, c, http.StatusOK, 2)
	assertNativeMixedPoisonHealing(t, ctx, c, tc)
	agentStream, err := tc.Client.GetStream(ctx, "AGENT")
	require.NoError(t, err)
	agentInfo, err := agentStream.Info(ctx)
	require.NoError(t, err)
	require.Zero(t, agentInfo.State.Msgs, "refused HTTP requests publish no task or invented loop")
	require.NoError(t, c.Stop(ctx))
	for _, client := range clients {
		client.waitDone(t)
	}
	assertNativeAutoContinueRefusal(t, c, http.StatusServiceUnavailable)
	require.Eventually(t, func() bool { info, err := kvStream.Info(ctx); return err == nil && info.State.Consumers == 0 }, time.Second, time.Millisecond)
}

func assertNativeLoopList(t *testing.T, c *Component, status, count int) {
	t.Helper()
	response := httptest.NewRecorder()
	c.handleListLoops(response, httptest.NewRequest(http.MethodGet, "/loops", nil))
	require.Equal(t, status, response.Code, response.Body.String())
	if status == http.StatusOK {
		var loops []Loop
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &loops))
		require.Len(t, loops, count)
	}
}

func assertNativeAutoContinueRefusal(t *testing.T, c *Component, status int) {
	t.Helper()
	response := httptest.NewRecorder()
	c.handleHTTPMessage(response, httptest.NewRequest(http.MethodPost, "/message", strings.NewReader(
		`{"content":"continue","user_id":"user","channel_type":"http","channel_id":"channel"}`)))
	require.Equal(t, status, response.Code, response.Body.String())
	var result HTTPMessageResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Equal(t, agentic.ResponseTypeError, result.Type)
	require.Empty(t, result.InReplyTo)
}

func assertNativeMixedPoisonHealing(t *testing.T, ctx context.Context, c *Component, tc *natsclient.TestClient) {
	t.Helper()
	kv, err := tc.GetKVBucket(ctx, defaultAgentLoopsBucket(t))
	require.NoError(t, err)
	_, err = kv.Put(ctx, admissionLoopB, []byte("{malformed"))
	require.NoError(t, err)
	require.Eventually(t, func() bool { _, err := c.currentLoopSnapshot(ctx); return err != nil }, time.Second, time.Millisecond)
	assertNativeLoopList(t, c, http.StatusServiceUnavailable, 0)
	require.NoError(t, kv.Delete(ctx, admissionLoopB))
	require.Eventually(t, func() bool { _, err := c.currentLoopSnapshot(ctx); return err == nil }, time.Second, time.Millisecond)
	assertNativeLoopList(t, c, http.StatusOK, 1)
	_, err = kv.Put(ctx, "COMPLETE_"+admissionLoopA, []byte("{malformed"))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		snapshot, err := c.currentLoopSnapshot(ctx)
		return err == nil && snapshot.Poisoned["COMPLETE_"+admissionLoopA] != nil
	}, time.Second, time.Millisecond)
	assertNativeLoopList(t, c, http.StatusOK, 1)
}
