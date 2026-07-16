package query

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodePrefixQueryResponseRejectsPoisonedEntityBeforeReturn(t *testing.T) {
	t.Parallel()

	validID := "acme.ops.test.system.widget.001"
	invalidEntityID := "bad"
	tests := []graph.EntityState{
		{ID: invalidEntityID},
		{ID: validID, Triples: []message.Triple{{Subject: invalidEntityID, Predicate: "test.state.value"}}},
		{ID: validID, Triples: []message.Triple{{
			Subject: validID, Predicate: "test.state.target", Object: invalidEntityID, Datatype: message.EntityReferenceDatatype,
		}}},
	}
	for index, poison := range tests {
		data, err := json.Marshal(graph.PrefixQueryResponse{Entities: []graph.EntityState{{ID: validID}, poison}})
		require.NoError(t, err)
		got, err := decodePrefixQueryResponse(data)
		require.Error(t, err, "poison case %d", index)
		assert.True(t, graph.IsStateContractError(err))
		assert.Empty(t, got.Entities, "no partial aggregate may escape")
	}
}

// scriptedPage is one page a fake fetcher hands back.
type scriptedPage struct {
	entities []graph.EntityState
	cursor   string // NextCursor; "" = exhausted
}

func ents(ids ...string) []graph.EntityState {
	out := make([]graph.EntityState, len(ids))
	for i, id := range ids {
		out[i] = graph.EntityState{ID: id}
	}
	return out
}

func idsOf(es []graph.EntityState) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.ID
	}
	return out
}

// scriptedFetch returns a fetch func that replays pages in order and records
// every request it received (so tests can assert cursor threading + per-page cap).
func scriptedFetch(pages []scriptedPage, got *[]graph.PrefixQueryRequest) func(context.Context, graph.PrefixQueryRequest) (graph.PrefixQueryResponse, error) {
	i := 0
	return func(_ context.Context, req graph.PrefixQueryRequest) (graph.PrefixQueryResponse, error) {
		*got = append(*got, req)
		if i >= len(pages) {
			return graph.PrefixQueryResponse{}, nil // no more pages
		}
		p := pages[i]
		i++
		return graph.PrefixQueryResponse{Entities: p.entities, NextCursor: p.cursor}, nil
	}
}

func TestPagePrefixAll_RejectsNonPositiveMax(t *testing.T) {
	for _, max := range []int{0, -1, -100} {
		_, _, err := pagePrefixAll(context.Background(), graph.PrefixQueryRequest{Prefix: "p"}, max, nil)
		require.Error(t, err, "maxEntities=%d must error", max)
		assert.Contains(t, err.Error(), "no unbounded mode")
	}
}

func TestQueryPrefix_InvalidPrefixFailsBeforeNATS(t *testing.T) {
	t.Parallel()

	client := &natsClient{}
	var err error
	require.NotPanics(t, func() {
		_, err = client.QueryPrefix(context.Background(), graph.PrefixQueryRequest{Prefix: "acme.*"})
	})
	require.Error(t, err)
	var classified *errs.ClassifiedError
	require.True(t, errors.As(err, &classified))
	assert.Equal(t, semtypes.ErrorCodeEntityIDPrefixInvalid, classified.Code)
}

func TestPagePrefixAll_InvalidPrefixFailsBeforeFetch(t *testing.T) {
	t.Parallel()

	requests := 0
	fetch := func(context.Context, graph.PrefixQueryRequest) (graph.PrefixQueryResponse, error) {
		requests++
		return graph.PrefixQueryResponse{}, nil
	}
	_, _, err := pagePrefixAll(
		context.Background(),
		graph.PrefixQueryRequest{Prefix: "acme.*"},
		10,
		fetch,
	)
	require.Error(t, err)
	assert.Zero(t, requests, "invalid prefix must fail before the first request")
}

func TestPagePrefixAll_EmptyPrefixRemainsMatchAll(t *testing.T) {
	t.Parallel()

	requests := 0
	fetch := func(context.Context, graph.PrefixQueryRequest) (graph.PrefixQueryResponse, error) {
		requests++
		return graph.PrefixQueryResponse{}, nil
	}
	_, _, err := pagePrefixAll(context.Background(), graph.PrefixQueryRequest{}, 10, fetch)
	require.NoError(t, err)
	assert.Equal(t, 1, requests)
}

func TestPagePrefixAll_SinglePageExhausted(t *testing.T) {
	var got []graph.PrefixQueryRequest
	fetch := scriptedFetch([]scriptedPage{{entities: ents("a", "b"), cursor: ""}}, &got)

	all, truncated, err := pagePrefixAll(context.Background(), graph.PrefixQueryRequest{Prefix: "p"}, 100, fetch)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, idsOf(all))
	assert.False(t, truncated, "exhausted set under the cap is not truncated")
	assert.Len(t, got, 1, "one page fetched")
}

func TestPagePrefixAll_MultiPageExhausted(t *testing.T) {
	var got []graph.PrefixQueryRequest
	fetch := scriptedFetch([]scriptedPage{
		{entities: ents("a", "b"), cursor: "c1"},
		{entities: ents("c", "d"), cursor: "c2"},
		{entities: ents("e"), cursor: ""},
	}, &got)

	all, truncated, err := pagePrefixAll(context.Background(), graph.PrefixQueryRequest{Prefix: "p"}, 100, fetch)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c", "d", "e"}, idsOf(all))
	assert.False(t, truncated)
	// Cursor threading: req 1 empty (start), req 2 = c1, req 3 = c2.
	require.Len(t, got, 3)
	assert.Equal(t, "", got[0].Cursor)
	assert.Equal(t, "c1", got[1].Cursor)
	assert.Equal(t, "c2", got[2].Cursor)
}

func TestPagePrefixAll_TruncatedWhenCapHitMidStream(t *testing.T) {
	var got []graph.PrefixQueryRequest
	fetch := scriptedFetch([]scriptedPage{
		{entities: ents("a", "b"), cursor: "c1"},
		{entities: ents("c"), cursor: "c2"}, // server still has more (c2)
	}, &got)

	all, truncated, err := pagePrefixAll(context.Background(), graph.PrefixQueryRequest{Prefix: "p"}, 3, fetch)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c"}, idsOf(all))
	assert.True(t, truncated, "cap reached while the server still had a cursor -> truncated")
}

func TestPagePrefixAll_NotTruncatedWhenCapEqualsExhaustion(t *testing.T) {
	var got []graph.PrefixQueryRequest
	fetch := scriptedFetch([]scriptedPage{
		{entities: ents("a", "b"), cursor: "c1"},
		{entities: ents("c"), cursor: ""}, // lands exactly at cap AND exhausted
	}, &got)

	all, truncated, err := pagePrefixAll(context.Background(), graph.PrefixQueryRequest{Prefix: "p"}, 3, fetch)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c"}, idsOf(all))
	assert.False(t, truncated, "exactly at cap but exhausted -> not truncated")
}

func TestPagePrefixAll_PerPageBudgetCapsLimit(t *testing.T) {
	var got []graph.PrefixQueryRequest
	fetch := scriptedFetch([]scriptedPage{
		{entities: ents("a", "b"), cursor: "c1"},
		{entities: ents("c"), cursor: "c2"},
	}, &got)

	// maxEntities=3, caller asks for big pages; helper must shrink the 2nd
	// page request to the remaining budget (3-2=1) rather than over-fetch.
	_, _, err := pagePrefixAll(context.Background(), graph.PrefixQueryRequest{Prefix: "p", Limit: 1000}, 3, fetch)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, 3, got[0].Limit, "first page capped to remaining budget (3)")
	assert.Equal(t, 1, got[1].Limit, "second page capped to remaining budget (1)")
}

func TestPagePrefixAll_IgnoresEntryCursor(t *testing.T) {
	var got []graph.PrefixQueryRequest
	fetch := scriptedFetch([]scriptedPage{{entities: ents("a"), cursor: ""}}, &got)

	_, _, err := pagePrefixAll(context.Background(), graph.PrefixQueryRequest{Prefix: "p", Cursor: "caller-supplied"}, 10, fetch)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "", got[0].Cursor, "QueryPrefixAll starts from the beginning; entry Cursor is ignored")
}

func TestPagePrefixAll_PropagatesFetchError(t *testing.T) {
	sentinel := errors.New("boom")
	fetch := func(_ context.Context, _ graph.PrefixQueryRequest) (graph.PrefixQueryResponse, error) {
		return graph.PrefixQueryResponse{}, sentinel
	}
	all, truncated, err := pagePrefixAll(context.Background(), graph.PrefixQueryRequest{Prefix: "p"}, 10, fetch)
	require.ErrorIs(t, err, sentinel)
	assert.Nil(t, all)
	assert.False(t, truncated)
}

func TestPagePrefixAll_DefensiveEmptyPageWithCursor(t *testing.T) {
	var got []graph.PrefixQueryRequest
	// Anomalous: a page with a cursor but no entities. Must stop, not spin.
	fetch := scriptedFetch([]scriptedPage{
		{entities: ents("a"), cursor: "c1"},
		{entities: nil, cursor: "c2"}, // empty page WITH cursor -> defensive stop
	}, &got)

	all, truncated, err := pagePrefixAll(context.Background(), graph.PrefixQueryRequest{Prefix: "p"}, 100, fetch)
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, idsOf(all))
	assert.False(t, truncated)
	assert.Len(t, got, 2, "stopped after the empty page rather than spinning")
}

// entity-id-audit:classify intentional-malformed "bad" line=21 column=21 surface=go-assignment:invalidEntityID prefix aggregate poison fixture
// entity-id-audit:classify intentional-malformed "acme.*" line=90 column=86 surface=go-field:PrefixQueryRequest.Prefix entity_id_prefix_invalid:first_byte verifies wildcard prefix rejection before NATS
// entity-id-audit:classify intentional-malformed "acme.*" line=108 column=36 surface=go-field:PrefixQueryRequest.Prefix entity_id_prefix_invalid:first_byte verifies wildcard prefix rejection before fetch
