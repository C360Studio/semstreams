package agenticdispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/graphview"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// spec: agentic-dispatch / Dispatch uses one authority-backed current-state projection
func TestLoopOwnerLookupReturnsOnlyCurrentOwnerOrClassifiedRefusal(t *testing.T) {
	for _, tc := range []struct {
		name, id, code string
		readErr        error
		mutate         func(*agentic.LoopEntity)
	}{
		{name: "current owner", id: admissionLoopA},
		{name: "invalid token", id: "authored", code: "invalid_loop_id"},
		{name: "missing", id: admissionLoopA, readErr: jetstream.ErrKeyNotFound, code: "loop_not_found"},
		{name: "deleted", id: admissionLoopA, readErr: fmt.Errorf("read: %w", jetstream.ErrKeyDeleted), code: "loop_not_found"},
		{name: "unavailable", id: admissionLoopA, readErr: errors.New("unavailable"), code: "loop_state_unavailable"},
		{name: "canceled read", id: admissionLoopA, readErr: context.Canceled, code: "loop_state_unavailable"},
		{name: "malformed bytes", id: admissionLoopA, readErr: permanentTerminal("malformed record"), code: "loop_record_invalid"},
		{name: "wrong key identity", id: admissionLoopA, code: "loop_record_invalid", mutate: func(e *agentic.LoopEntity) { e.ID = admissionLoopB }},
		{name: "invalid typed state", id: admissionLoopA, code: "loop_record_invalid", mutate: func(e *agentic.LoopEntity) { e.State = "invented" }},
		{name: "owner absent", id: admissionLoopA, code: "loop_owner_absent", mutate: func(e *agentic.LoopEntity) { e.UserID = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestComponent(t)
			record := &agentic.LoopEntity{ID: admissionLoopA, UserID: "owner-a", State: agentic.LoopStateExecuting, MaxIterations: 3}
			if tc.mutate != nil {
				tc.mutate(record)
			}
			before := *record
			reads := 0
			c.loadPersistedLoopFn = func(ctx context.Context, id string) (*agentic.LoopEntity, error) {
				require.Same(t, t.Context(), ctx)
				require.Equal(t, admissionLoopA, id)
				reads++
				return record, tc.readErr
			}
			var lookup LoopOwnerLookup = c.lookupLoopOwner
			owner, err := lookup(t.Context(), tc.id)
			if tc.code == "" {
				require.NoError(t, err)
				require.Equal(t, LoopOwner{LoopID: admissionLoopA, UserID: "owner-a"}, owner)
			} else {
				require.Error(t, err)
				var classified *errs.ClassifiedError
				require.ErrorAs(t, err, &classified)
				require.Equal(t, tc.code, classified.Code)
				require.Equal(t, LoopOwner{}, owner)
				require.Equal(t, tc.code == "loop_state_unavailable", errs.IsTransient(err))
			}
			if tc.code == "invalid_loop_id" {
				require.Zero(t, reads)
			} else {
				require.Equal(t, 1, reads)
			}
			require.Equal(t, before, *record)
		})
	}
}

// spec: agentic-dispatch / Dispatch uses one authority-backed current-state projection
func TestHTTPProjectionRefusesHeldBootstrapBeforeRequestCancellation(t *testing.T) {
	for _, endpoint := range []string{"list", "debug", "auto_continue"} {
		t.Run(endpoint, func(t *testing.T) {
			source := newFakeActivitySource()
			c := newActivityTestComponent(t, source, graphview.Hooks{})
			c.config.AutoContinue = true
			c.modelRegistry = newTestRegistry()
			c.taskEvidence = emptyRetainedTaskEvidenceReader{}
			view, err := c.ensureActivityView(t.Context())
			require.NoError(t, err)
			source.waitWatcher(t, 1) // No initial nil marker: real view remains bootstrapping.
			_, _, err = view.SnapshotAndSubscribe(t.Context())
			require.ErrorIs(t, err, graphview.ErrNotReady)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel() // Failure bound only; the endpoint must return while it is live.
			request := httptest.NewRequest(http.MethodGet, "/"+endpoint, nil).WithContext(ctx)
			response := httptest.NewRecorder()
			switch endpoint {
			case "list":
				c.handleListLoops(response, request)
			case "debug":
				c.handleDebugState(response, request)
			case "auto_continue":
				request = httptest.NewRequest(http.MethodPost, "/message", strings.NewReader(
					`{"content":"continue","user_id":"user","channel_type":"http","channel_id":"channel"}`)).WithContext(ctx)
				c.handleHTTPMessage(response, request)
			}
			require.NoError(t, ctx.Err(), "unavailable must be observable before request cancellation")
			require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
			if endpoint == "debug" {
				var state DebugState
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &state))
				require.False(t, state.LoopProjectionReady)
				require.Empty(t, state.Loops)
			}
			require.Zero(t, testutil.ToFloat64(c.metrics.tasksSubmitted))
			require.Equal(t, 1, source.calls(), "bootstrap refusal cannot construct a second view")
		})
	}
}

// spec: agentic-dispatch / Dispatch uses one authority-backed current-state projection
func FuzzLoopOwnerLookup(f *testing.F) {
	for _, seed := range []string{"", "authored", admissionLoopA, admissionLoopB,
		"00000000-0000-0000-0000-000000000000", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		"AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA", "{" + admissionLoopA + "}", "urn:uuid:" + admissionLoopA,
		"COMPLETE_" + admissionLoopA, "research.request.received." + admissionLoopA, "*", ">", "\x00", strings.Repeat("a", 257)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, id string) {
		c := newTestComponent(t)
		reads := 0
		c.loadPersistedLoopFn = func(_ context.Context, key string) (*agentic.LoopEntity, error) {
			reads++
			return &agentic.LoopEntity{ID: key, UserID: "owner", State: agentic.LoopStateExecuting, MaxIterations: 1}, nil
		}
		var lookup LoopOwnerLookup = c.lookupLoopOwner
		owner, err := lookup(t.Context(), id)
		if err != nil {
			var classified *errs.ClassifiedError
			require.ErrorAs(t, err, &classified)
			require.Equal(t, "invalid_loop_id", classified.Code)
			require.Zero(t, reads, "invalid external tokens cannot reach authority I/O")
			require.Equal(t, LoopOwner{}, owner)
			return
		}
		require.Equal(t, 1, reads)
		require.Equal(t, LoopOwner{LoopID: id, UserID: "owner"}, owner)
	})
}

// spec: agentic-dispatch / Dispatch uses one authority-backed current-state projection
func TestHTTPAutoContinueRefusesUnavailableOrAmbiguousViewWithoutEffects(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		t.Run(map[bool]string{false: "unavailable", true: "ambiguous"}[ambiguous], func(t *testing.T) {
			c := newTestComponent(t)
			records := []*agentic.LoopEntity{
				{ID: admissionLoopA, UserID: "user", ChannelType: "http", ChannelID: "channel", State: agentic.LoopStateExecuting, MaxIterations: 3},
				{ID: admissionLoopB, UserID: "user", ChannelType: "http", ChannelID: "channel", State: agentic.LoopStateExecuting, MaxIterations: 3},
			}
			if ambiguous {
				c = newCurrentLoopTestComponent(t, records...)
			}
			c.config.AutoContinue = true
			c.taskEvidence = emptyRetainedTaskEvidenceReader{}
			before, err := json.Marshal(records)
			require.NoError(t, err)
			body := []byte(`{"content":"continue","user_id":"user","channel_type":"http","channel_id":"channel"}`)
			response := httptest.NewRecorder()
			c.handleHTTPMessage(response, httptest.NewRequest(http.MethodPost, "/message", bytes.NewReader(body)))
			want := http.StatusServiceUnavailable
			if ambiguous {
				want = http.StatusConflict
			}
			require.Equal(t, want, response.Code, response.Body.String())
			var result HTTPMessageResponse
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
			require.Equal(t, agentic.ResponseTypeError, result.Type)
			require.Empty(t, result.InReplyTo, "refusal cannot invent a new loop")
			require.Zero(t, testutil.ToFloat64(c.metrics.tasksSubmitted))
			after, err := json.Marshal(records)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}
