//go:build integration

package graphingest

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// TestIntegration_QueryEntityNATS_WireContract drives the registered
// graph.ingest.query.entity handler end-to-end and asserts on BOTH
// the X-Error-Class header AND the legacy body prefix shape that
// downstream consumers depend on. Closes the gh#93 Phase 3 reviewer
// B1/B2 regression class — the previous Phase 3 commit changed the
// body message text and broke production HasPrefix sniffers in
// processor/agentic-loop/todos.go and semconnect's
// classifyEntityQueryError; this test would have caught both because
// it asserts on the actual wire bytes, not synthetic fakes.
func TestIntegration_QueryEntityNATS_WireContract(t *testing.T) {
	ctx := context.Background()

	streams := []natsclient.TestStreamConfig{
		{Name: "ENTITY", Subjects: []string{"entity.>"}},
	}
	testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))
	natsClient := testClient.Client

	config := DefaultConfig()
	deps := component.Dependencies{NATSClient: natsClient}

	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	comp, err := CreateGraphIngest(configJSON, deps)
	require.NoError(t, err)

	c := comp.(*Component)
	require.NoError(t, c.Initialize())
	require.NoError(t, c.Start(ctx))
	defer func() { _ = c.Stop(5 * time.Second) }()
	time.Sleep(100 * time.Millisecond)

	t.Run("not_found_path", func(t *testing.T) {
		// The wire-shape contracts this case pins:
		//   - Body has prefix "error: not found:" so downstream
		//     HasPrefix sniffers (agentic-loop/todos.go,
		//     semconnect/cs-api/systems.go) keep routing correctly.
		//   - The entity ID survives in the body tail for HTTP 404
		//     payload context.
		//   - X-Error-Class header is "invalid" so Phase 2 callers
		//     using RequestClassified see errs.IsInvalid(err) == true.
		req, _ := json.Marshal(map[string]string{"id": "no.such.entity.does.not.exist.xyz"})

		msg, err := natsClient.RequestWithHeaders(ctx, "graph.ingest.query.entity", req, nil, 2*time.Second)
		require.NoError(t, err, "transport must succeed")
		require.NotNil(t, msg)

		// 1. Header: X-Error-Class must be "invalid".
		gotClass := msg.Header.Get(natsclient.HeaderErrorClass)
		if gotClass != natsclient.ErrorClassInvalid {
			t.Errorf("X-Error-Class = %q, want %q (Phase 2 callers depend on this)",
				gotClass, natsclient.ErrorClassInvalid)
		}

		// 2. Body prefix: legacy "error: " present.
		if !bytes.HasPrefix(msg.Data, []byte("error: ")) {
			t.Fatalf("body missing legacy 'error: ' prefix; got %q", msg.Data)
		}

		// 3. Body has "not found:" right after the legacy prefix
		//    — agentic-loop/todos.go:31 HasPrefix-matches
		//    []byte("error: not found"), and semconnect's
		//    classifyEntityQueryError HasPrefix-matches "not found:"
		//    after stripping the "error: " prefix.
		if !bytes.HasPrefix(msg.Data, []byte("error: not found:")) {
			t.Errorf("body does not match downstream sniffer contract\n"+
				"  got:  %q\n"+
				"  want prefix: %q (agentic-loop/todos.go + semconnect/cs-api/systems.go)",
				msg.Data, "error: not found:")
		}

		// 4. Entity ID survives in the body for 404 payload context.
		if !bytes.Contains(msg.Data, []byte("no.such.entity.does.not.exist.xyz")) {
			t.Errorf("entity ID missing from body; got %q", msg.Data)
		}

		// 5. RequestClassified round-trip — Phase 2 callers using
		//    this path must see errs.IsInvalid(err) == true.
		_, classifiedErr := natsClient.RequestClassified(ctx, "graph.ingest.query.entity", req, 2*time.Second)
		if classifiedErr == nil {
			t.Fatal("RequestClassified must surface classified error")
		}
		if !errs.IsInvalid(classifiedErr) {
			t.Errorf("RequestClassified err class wrong: IsInvalid=%v want true; err=%v",
				errs.IsInvalid(classifiedErr), classifiedErr)
		}
	})

	t.Run("invalid_request_path", func(t *testing.T) {
		// The wire-shape contract this case pins:
		//   - Body has prefix "error: invalid request:" so
		//     semconnect's classifyEntityQueryError HasPrefix-matches
		//     after stripping the "error: " prefix → HTTP 400.
		//   - X-Error-Class header is "invalid".
		req := []byte("this is not valid JSON {{{")

		msg, err := natsClient.RequestWithHeaders(ctx, "graph.ingest.query.entity", req, nil, 2*time.Second)
		require.NoError(t, err)
		require.NotNil(t, msg)

		gotClass := msg.Header.Get(natsclient.HeaderErrorClass)
		if gotClass != natsclient.ErrorClassInvalid {
			t.Errorf("X-Error-Class = %q, want %q", gotClass, natsclient.ErrorClassInvalid)
		}

		if !bytes.HasPrefix(msg.Data, []byte("error: invalid request:")) {
			t.Errorf("body does not match semconnect's HasPrefix(\"invalid request:\") contract\n"+
				"  got: %q\n"+
				"  want prefix: %q",
				msg.Data, "error: invalid request:")
		}
	})

	t.Run("empty_id_path", func(t *testing.T) {
		// Validation error before KV lookup. Body must read
		// "error: invalid request: empty id" — semconnect's
		// classifyEntityQueryError treats the "invalid request:"
		// prefix as HTTP 400.
		req, _ := json.Marshal(map[string]string{"id": ""})

		msg, err := natsClient.RequestWithHeaders(ctx, "graph.ingest.query.entity", req, nil, 2*time.Second)
		require.NoError(t, err)

		gotClass := msg.Header.Get(natsclient.HeaderErrorClass)
		if gotClass != natsclient.ErrorClassInvalid {
			t.Errorf("X-Error-Class = %q, want %q", gotClass, natsclient.ErrorClassInvalid)
		}

		want := []byte("error: invalid request: empty id")
		if !bytes.Equal(msg.Data, want) {
			t.Errorf("body = %q, want %q", msg.Data, want)
		}
	})

	t.Run("success_path_unchanged", func(t *testing.T) {
		// Success body byte invariant — Phase 3 must NOT introduce
		// any header on the success path, NOT change the success
		// body bytes. Closes the Phase 1 reviewer R1 lock at this
		// handler.
		entityID := "test.org.platform.domain.system.entity.success-fixture"
		seedEntity(t, ctx, c, entityID, []byte(`{"id":"`+entityID+`","triples":[]}`))

		req, _ := json.Marshal(map[string]string{"id": entityID})
		msg, err := natsClient.RequestWithHeaders(ctx, "graph.ingest.query.entity", req, nil, 2*time.Second)
		require.NoError(t, err)

		if msg.Header.Get(natsclient.HeaderStatus) != "" {
			t.Errorf("X-Status must be absent on success path; got %q",
				msg.Header.Get(natsclient.HeaderStatus))
		}
		if bytes.HasPrefix(msg.Data, []byte("error: ")) {
			t.Errorf("success body must NOT carry legacy error prefix; got %q", msg.Data)
		}
		// Body should be the seeded entity JSON.
		if !strings.Contains(string(msg.Data), entityID) {
			t.Errorf("success body missing entity ID; got %q", msg.Data)
		}
	})
}

// seedEntity writes a raw entity JSON to the ENTITY_STATES KV bucket
// so the not-found path tests can be distinguished from the success
// path. Uses the bucket directly to bypass mutation handler complexity.
func seedEntity(t *testing.T, ctx context.Context, c *Component, id string, body []byte) {
	t.Helper()
	if c.entityBucket == nil {
		t.Fatal("entityBucket not initialized")
	}
	_, err := c.entityBucket.Put(ctx, id, body)
	require.NoError(t, err)
}
