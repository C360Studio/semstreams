package gateddagexec

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/stretchr/testify/require"
)

// fakeRequester records which request family the reader used per attempt.
type fakeRequester struct {
	readyCalls    int
	classifyCalls int
	readyErr      error // when set, RequestReadyClassified fails
	resp          []byte
	responses     [][]byte
	responseIndex int
}

func (f *fakeRequester) nextResponse() []byte {
	if f.responseIndex >= len(f.responses) {
		return f.resp
	}
	response := f.responses[f.responseIndex]
	f.responseIndex++
	return response
}

func (f *fakeRequester) RequestReadyClassified(_ context.Context, _ string, _ []byte, _, _ time.Duration) ([]byte, error) {
	f.readyCalls++
	if f.readyErr != nil {
		return nil, f.readyErr
	}
	return f.nextResponse(), nil
}

func (f *fakeRequester) RequestClassified(_ context.Context, _ string, _ []byte, _ time.Duration) ([]byte, error) {
	f.classifyCalls++
	return f.nextResponse(), nil
}

func newTestReader(nc requester, onNeverReady func(error)) *natsGraphReader {
	return &natsGraphReader{
		nc:           nc,
		prefix:       "org.plat.dom.sys.type",
		maxUnits:     100,
		timeout:      5 * time.Second,
		readyProbe:   time.Second,
		readyBudget:  2 * time.Second,
		onNeverReady: onNeverReady,
	}
}

// gh#420: the FIRST read is readiness-gated (cold start), then the reader warms
// and subsequent reads are steady-state so a later hung responder isn't masked.
func TestReader_ColdStartUsesReadinessThenSteadyState(t *testing.T) {
	emptyResp, err := json.Marshal(graph.PrefixQueryResponse{})
	require.NoError(t, err)
	fr := &fakeRequester{resp: emptyResp}
	r := newTestReader(fr, nil)

	// First read → readiness path.
	_, err = r.ReadUnitSet(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, fr.readyCalls, "cold-start read must use the readiness-gated path")
	require.Equal(t, 0, fr.classifyCalls)
	require.True(t, r.warmed)

	// Second read → steady-state path (responder is proven up).
	_, err = r.ReadUnitSet(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, fr.readyCalls, "readiness must not be used again after warming")
	require.Equal(t, 1, fr.classifyCalls, "warmed reads use the steady-state path")
}

// A cold-start read that exhausts its readiness budget fires the distinct
// signal and stays UNWARMED so the next attempt retries readiness (not
// steady-state) — graph-ingest still hasn't answered.
func TestReader_ColdStartNeverReadyFiresSignalAndStaysUnwarmed(t *testing.T) {
	fr := &fakeRequester{readyErr: errors.New("readiness budget exhausted: no responders")}
	fired := 0
	r := newTestReader(fr, func(error) { fired++ })

	_, err := r.ReadUnitSet(context.Background())
	require.Error(t, err)
	require.Equal(t, 1, fired, "onNeverReady must fire on cold-start budget exhaustion")
	require.False(t, r.warmed, "must stay unwarmed so the next read retries readiness")

	// Next read still uses readiness (never warmed), NOT steady-state.
	_, _ = r.ReadUnitSet(context.Background())
	require.Equal(t, 2, fr.readyCalls)
	require.Equal(t, 0, fr.classifyCalls)
	require.Equal(t, 2, fired)
}

func TestReader_PoisonedAggregateHasNoWarmOrPartialSideEffect(t *testing.T) {
	t.Parallel()

	validID := "acme.ops.test.system.widget.001"
	invalidEntityID := "bad"
	resp, err := json.Marshal(graph.PrefixQueryResponse{Entities: []graph.EntityState{
		{ID: validID},
		{ID: validID, Triples: []message.Triple{{
			Subject: validID, Predicate: "test.state.target", Object: invalidEntityID, Datatype: message.EntityReferenceDatatype,
		}}},
	}})
	require.NoError(t, err)
	fr := &fakeRequester{resp: resp}
	r := newTestReader(fr, nil)

	states, err := r.ReadUnitSet(context.Background())
	require.Error(t, err)
	require.True(t, graph.IsStateContractError(err))
	require.Nil(t, states, "the valid prefix of a poisoned page must not escape")
	require.False(t, r.warmed, "poison must not change readiness state")
	require.Equal(t, 1, fr.readyCalls)
	require.Zero(t, fr.classifyCalls)
}

func TestReader_PoisonedSecondPageDoesNotPersistWarmState(t *testing.T) {
	t.Parallel()

	validID := "acme.ops.test.system.widget.001"
	invalidSecondPageEntityID := "bad"
	pageOne, err := json.Marshal(graph.PrefixQueryResponse{
		Entities:   []graph.EntityState{{ID: validID}},
		NextCursor: "page-two",
	})
	require.NoError(t, err)
	pageTwo, err := json.Marshal(graph.PrefixQueryResponse{Entities: []graph.EntityState{{
		ID: validID,
		Triples: []message.Triple{{
			Subject: validID, Predicate: "test.state.target", Object: invalidSecondPageEntityID, Datatype: message.EntityReferenceDatatype,
		}},
	}}})
	require.NoError(t, err)
	empty, err := json.Marshal(graph.PrefixQueryResponse{})
	require.NoError(t, err)
	fr := &fakeRequester{responses: [][]byte{pageOne, pageTwo, empty}}
	r := newTestReader(fr, nil)

	states, err := r.ReadUnitSet(context.Background())
	require.Error(t, err)
	require.True(t, graph.IsStateContractError(err))
	require.Nil(t, states, "page one must not escape when page two poisons the aggregate")
	require.False(t, r.warmed, "a canonical first page must not persist readiness before aggregate completion")
	require.Equal(t, 1, fr.readyCalls, "the first cold page uses readiness")
	require.Equal(t, 1, fr.classifyCalls, "later pages in the same call use steady-state requests")

	_, err = r.ReadUnitSet(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, fr.readyCalls, "the next call must retry readiness after aggregate poison")
	require.Equal(t, 1, fr.classifyCalls)
	require.True(t, r.warmed)
}

func TestReader_ColdStartTruncationDoesNotPersistWarmState(t *testing.T) {
	t.Parallel()

	pageOne, err := json.Marshal(graph.PrefixQueryResponse{
		Entities:   []graph.EntityState{{ID: "acme.ops.test.system.widget.001"}},
		NextCursor: "unread-page-two",
	})
	require.NoError(t, err)
	empty, err := json.Marshal(graph.PrefixQueryResponse{})
	require.NoError(t, err)
	fr := &fakeRequester{responses: [][]byte{pageOne, empty}}
	r := newTestReader(fr, nil)
	r.maxUnits = 1

	states, err := r.ReadUnitSet(context.Background())
	require.NoError(t, err)
	require.Len(t, states, 1)
	require.False(t, r.warmed, "cursor truncation leaves an unread page and cannot persist readiness")
	require.Equal(t, 1, fr.readyCalls)
	require.Zero(t, fr.classifyCalls)

	_, err = r.ReadUnitSet(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, fr.readyCalls, "the next call must restart through readiness after truncation")
	require.Zero(t, fr.classifyCalls)
	require.True(t, r.warmed)
}

// entity-id-audit:classify intentional-malformed "bad" line=105 column=21 surface=go-assignment:invalidEntityID gated DAG aggregate reference poison fixture
// entity-id-audit:classify intentional-malformed "bad" line=129 column=31 surface=go-assignment:invalidSecondPageEntityID gated DAG second-page aggregate poison fixture
