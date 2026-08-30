//go:build integration

package graphingest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// startPrefixTestComponent boots a graph-ingest component against a real
// testcontainer NATS instance and returns it. The caller is responsible for
// stopping it.
func startPrefixTestComponent(t *testing.T, opts ...testComponentOption) (*Component, *natsclient.Client) {
	t.Helper()
	ctx := context.Background()

	streams := []natsclient.TestStreamConfig{
		{Name: "ENTITY", Subjects: []string{"entity.>"}},
	}
	testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))
	nc := testClient.Client

	config := DefaultConfig()
	deps := testDependencies(t, nc, opts...)
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	comp, err := CreateGraphIngest(configJSON, deps)
	require.NoError(t, err)

	c := comp.(*Component)
	require.NoError(t, c.Initialize())
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() { _ = c.Stop(context.Background()) })

	// Allow subscriptions to stabilise.
	time.Sleep(100 * time.Millisecond)
	return c, nc
}

// seedPrefixEntity writes a minimal entity to the KV bucket via the mutation
// handler so it shows up in prefix queries.
func seedPrefixEntity(t *testing.T, ctx context.Context, c *Component, id string) {
	t.Helper()
	entity := &graph.EntityState{
		ID:          id,
		MessageType: testEntityType(),
		Triples: []message.Triple{
			{Subject: id, Predicate: "test.entity.attribute", Object: "val", Timestamp: time.Now()},
		},
		Version:   1,
		UpdatedAt: time.Now(),
	}
	require.NoError(t, c.CreateEntity(ctx, entity))
}

// TestIntegration_PrefixQuery_PaginationCoverage drives the registered
// graph.ingest.query.prefix handler end-to-end via NATS request/reply and
// asserts:
//   - Multiple pages with a cursor return sorted, disjoint results.
//   - The union of all pages covers the full seeded set.
//   - A single no-cursor page has the "entities" key.
//   - No "next_cursor" key appears on the final page.
func TestIntegration_PrefixQuery_PaginationCoverage(t *testing.T) {
	ctx := context.Background()
	c, nc := startPrefixTestComponent(t, withAuthority("pagtest", "ops"))

	// Seed 5 entities with a sortable ID suffix.
	const count = 5
	const prefix = "pagtest.ops.dom.sys.type"
	for i := 1; i <= count; i++ {
		seedPrefixEntity(t, ctx, c, fmt.Sprintf("%s.entity-%03d", prefix, i))
	}

	// Collect pages with limit=2 until exhausted.
	var allEntities []graph.EntityState
	var cursor string
	var pageCount int
	var lastPageHasCursor bool

	for {
		req := graph.PrefixQueryRequest{
			Prefix: "pagtest",
			Limit:  2,
			Cursor: cursor,
		}
		reqData, err := json.Marshal(req)
		require.NoError(t, err)

		respData, err := nc.RequestClassified(ctx, "graph.ingest.query.prefix", reqData, 5*time.Second)
		require.NoError(t, err, "transport must succeed")

		var resp graph.PrefixQueryResponse
		require.NoError(t, json.Unmarshal(respData, &resp))
		require.NotEmpty(t, resp.Entities, "each page must return at least one entity")

		allEntities = append(allEntities, resp.Entities...)
		pageCount++
		cursor = resp.NextCursor
		lastPageHasCursor = cursor != ""

		if cursor == "" {
			break
		}
		if pageCount > count {
			t.Fatal("pagination loop did not terminate after expected pages")
		}
	}

	assert.False(t, lastPageHasCursor, "last page must have empty next_cursor")
	assert.Equal(t, count, len(allEntities), "union of pages must cover all seeded entities")

	// Verify sorted order across all pages.
	for i := 1; i < len(allEntities); i++ {
		assert.LessOrEqual(t, allEntities[i-1].ID, allEntities[i].ID,
			"entities must be in lexicographic order across pages")
	}

	// Verify disjointness.
	seen := map[string]bool{}
	for _, e := range allEntities {
		assert.False(t, seen[e.ID], "duplicate entity across pages: %s", e.ID)
		seen[e.ID] = true
	}
}

// TestIntegration_PrefixQuery_ErrorClassification verifies that an invalid
// cursor produces a classified error via RequestClassified, not a silent empty
// response. This closes the "errors travel as body payloads" failure class for
// the prefix handler.
func TestIntegration_PrefixQuery_ErrorClassification(t *testing.T) {
	ctx := context.Background()
	_, nc := startPrefixTestComponent(t)

	req := graph.PrefixQueryRequest{
		Prefix: "acme",
		Limit:  10,
		Cursor: "!!!this-is-not-valid-base64!!!",
	}
	reqData, err := json.Marshal(req)
	require.NoError(t, err)

	_, classifiedErr := nc.RequestClassified(ctx, "graph.ingest.query.prefix", reqData, 5*time.Second)
	require.Error(t, classifiedErr, "invalid cursor must produce a classified error")
	assert.True(t, errs.IsInvalid(classifiedErr),
		"invalid cursor must be classified as invalid, not transient; got: %v", classifiedErr)
}

// TestIntegration_PrefixQuery_IndivisibleEntityTooLarge drives the production
// request subscription and classified wire contract. The entity authority is
// an in-memory KV seam so it can hold a value larger than the broker carrier;
// the responder itself uses the real connected NATS MaxPayload observation.
func TestIntegration_PrefixQuery_IndivisibleEntityTooLarge(t *testing.T) {
	ctx := context.Background()
	testClient := natsclient.NewTestClient(t)
	comp := createTestComponentWithMockKV(t, withAuthority("oversize", "ops"))
	comp.natsClient = testClient.Client

	maxPayload, err := testClient.Client.MaxPayload()
	require.NoError(t, err)
	const id = "oversize.ops.dom.sys.type.entity-001"
	entity := &graph.EntityState{
		ID:          id,
		MessageType: testEntityType(),
		Triples: []message.Triple{{
			Subject:   id,
			Predicate: "test.entity.attribute",
			Object:    strings.Repeat("x", int(maxPayload)),
			Timestamp: time.Now(),
		}},
		Version:   1,
		UpdatedAt: time.Now(),
	}
	require.NoError(t, comp.CreateEntity(ctx, entity))
	require.NoError(t, comp.setupQueryHandlers(ctx))
	require.NoError(t, testClient.Client.GetConnection().Flush())

	request, err := json.Marshal(graph.PrefixQueryRequest{Prefix: "oversize", Limit: 1})
	require.NoError(t, err)
	response, requestErr := testClient.Client.RequestClassified(
		ctx,
		"graph.ingest.query.prefix",
		request,
		5*time.Second,
	)
	require.Error(t, requestErr)
	assert.Nil(t, response)
	assert.True(t, errs.IsInvalid(requestErr))
	var classified *errs.ClassifiedError
	require.ErrorAs(t, requestErr, &classified)
	assert.Equal(t, "response_too_large", classified.Code)
	responseBytes, ok := classified.Detail["response_bytes"].(float64)
	require.True(t, ok, "response_bytes detail must survive the JSON wire contract as a number")
	assert.Greater(t, responseBytes, float64(maxPayload))
	assert.Equal(t, float64(maxPayload), classified.Detail["max_payload"])
}

// TestIntegration_PrefixQuery_ExhaustedPageOmitsCursor verifies that an
// exhausted single-page response has "entities" and no continuation token.
func TestIntegration_PrefixQuery_ExhaustedPageOmitsCursor(t *testing.T) {
	ctx := context.Background()
	c, nc := startPrefixTestComponent(t, withAuthority("singlepage", "ops"))

	const id = "singlepage.ops.dom.sys.type.entity-001"
	seedPrefixEntity(t, ctx, c, id)

	reqData, err := json.Marshal(graph.PrefixQueryRequest{Prefix: "singlepage", Limit: 100})
	require.NoError(t, err)

	respData, err := nc.RequestClassified(ctx, "graph.ingest.query.prefix", reqData, 5*time.Second)
	require.NoError(t, err)

	// 1. Must decode the typed page's entities.
	var pageResp struct {
		Entities []map[string]any `json:"entities"`
	}
	require.NoError(t, json.Unmarshal(respData, &pageResp), "must parse the entity page")
	require.Len(t, pageResp.Entities, 1, "must return the seeded entity")

	// 2. Must NOT include "next_cursor" on the last/only page.
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(respData, &raw))
	_, hasCursor := raw["next_cursor"]
	assert.False(t, hasCursor, "next_cursor must be absent on an exhausted page")

	// 3. Must decode into typed PrefixQueryResponse.
	var typedResp graph.PrefixQueryResponse
	require.NoError(t, json.Unmarshal(respData, &typedResp))
	require.Len(t, typedResp.Entities, 1)
	assert.Equal(t, id, typedResp.Entities[0].ID)
}
