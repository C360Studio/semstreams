//go:build integration

package graphingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// TestIntegration_QueryEntityNATS_WireContract drives the registered
// graph.ingest.query.entity handler end-to-end and asserts on both
// the ADR-060 headers AND the JSON body envelope. Closes the gh#93
// Phase 3 reviewer B1/B2 regression class — this test asserts on
// actual wire bytes, not synthetic fakes, so body changes are caught
// immediately.
//
// ADR-060 PR-D wire contract (retired "error: " prefix):
//
//	X-Status:      error
//	X-Error-Class: invalid | transient | fatal
//	X-Error-Code:  entity_not_found | invalid_request | ...
//	body:          {"message": "<text>"}
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
		// ADR-060 wire shape:
		//   - X-Status: error
		//   - X-Error-Class: invalid  (so IsInvalid(err) == true at the gateway)
		//   - X-Error-Code: entity_not_found (stable machine code for 404 routing)
		//   - body: {"message":"not found: <id>"} — JSON envelope, not plain text
		const absentID = "no.such.entity.does.not.exist.xyz"
		req, _ := json.Marshal(map[string]string{"id": absentID})

		msg, err := natsClient.RequestWithHeaders(ctx, "graph.ingest.query.entity", req, nil, 2*time.Second)
		require.NoError(t, err, "transport must succeed")
		require.NotNil(t, msg)

		// 1. X-Status must be "error".
		if msg.Header.Get(natsclient.HeaderStatus) != natsclient.HeaderStatusError {
			t.Errorf("X-Status = %q, want %q", msg.Header.Get(natsclient.HeaderStatus), natsclient.HeaderStatusError)
		}

		// 2. X-Error-Class must be "invalid".
		gotClass := msg.Header.Get(natsclient.HeaderErrorClass)
		if gotClass != natsclient.ErrorClassInvalid {
			t.Errorf("X-Error-Class = %q, want %q (gateway depends on this for HTTP 404 routing)",
				gotClass, natsclient.ErrorClassInvalid)
		}

		// 3. X-Error-Code must be "entity_not_found" — stable machine code.
		gotCode := msg.Header.Get(natsclient.HeaderErrorCode)
		if gotCode != "entity_not_found" {
			t.Errorf("X-Error-Code = %q, want %q", gotCode, "entity_not_found")
		}

		// 4. Body is the JSON envelope with "not found:" message text.
		//    The entity ID must survive in the body for 404 payload context.
		if !bytes.HasPrefix(msg.Data, []byte("{")) {
			t.Fatalf("ADR-060: body must be JSON envelope; got %q", msg.Data)
		}
		if !bytes.Contains(msg.Data, []byte("not found:")) {
			t.Errorf("body message must contain %q; got %q", "not found:", msg.Data)
		}
		if !bytes.Contains(msg.Data, []byte(absentID)) {
			t.Errorf("entity ID missing from body; got %q", msg.Data)
		}

		// 5. RequestClassified round-trip — callers using this path must
		//    see errs.IsInvalid(err) == true and ce.Code == entity_not_found.
		_, classifiedErr := natsClient.RequestClassified(ctx, "graph.ingest.query.entity", req, 2*time.Second)
		if classifiedErr == nil {
			t.Fatal("RequestClassified must surface classified error")
		}
		if !errs.IsInvalid(classifiedErr) {
			t.Errorf("RequestClassified err class wrong: IsInvalid=%v want true; err=%v",
				errs.IsInvalid(classifiedErr), classifiedErr)
		}
		var ce *errs.ClassifiedError
		if !errors.As(classifiedErr, &ce) || ce.Code != graph.ErrorCodeEntityNotFound {
			t.Errorf("RequestClassified err must carry Code=entity_not_found; got %+v", classifiedErr)
		}
	})

	t.Run("invalid_request_path", func(t *testing.T) {
		// ADR-060: malformed JSON → invalid class, JSON body.
		// Note: uses errs.Classified (no Code), so X-Error-Code is absent.
		req := []byte("this is not valid JSON {{{")

		msg, err := natsClient.RequestWithHeaders(ctx, "graph.ingest.query.entity", req, nil, 2*time.Second)
		require.NoError(t, err)
		require.NotNil(t, msg)

		gotClass := msg.Header.Get(natsclient.HeaderErrorClass)
		if gotClass != natsclient.ErrorClassInvalid {
			t.Errorf("X-Error-Class = %q, want %q", gotClass, natsclient.ErrorClassInvalid)
		}

		if !bytes.HasPrefix(msg.Data, []byte("{")) {
			t.Errorf("ADR-060: body must be JSON envelope; got %q", msg.Data)
		}
		if !bytes.Contains(msg.Data, []byte("invalid request:")) {
			t.Errorf("body message must contain %q; got %q", "invalid request:", msg.Data)
		}
	})

	t.Run("empty_id_path", func(t *testing.T) {
		// Validation error before KV lookup: empty id → invalid class, JSON body.
		// Note: uses errs.Classified (no Code), so X-Error-Code is absent.
		req, _ := json.Marshal(map[string]string{"id": ""})

		msg, err := natsClient.RequestWithHeaders(ctx, "graph.ingest.query.entity", req, nil, 2*time.Second)
		require.NoError(t, err)

		gotClass := msg.Header.Get(natsclient.HeaderErrorClass)
		if gotClass != natsclient.ErrorClassInvalid {
			t.Errorf("X-Error-Class = %q, want %q", gotClass, natsclient.ErrorClassInvalid)
		}

		if !bytes.HasPrefix(msg.Data, []byte("{")) {
			t.Errorf("ADR-060: body must be JSON envelope; got %q", msg.Data)
		}
		if !bytes.Contains(msg.Data, []byte("empty id")) {
			t.Errorf("body must describe empty-id validation failure; got %q", msg.Data)
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
		if !bytes.Contains(msg.Data, []byte(entityID)) {
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
