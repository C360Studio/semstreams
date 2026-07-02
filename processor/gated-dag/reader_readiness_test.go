package gateddagexec

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/stretchr/testify/require"
)

// fakeRequester records which request family the reader used per attempt.
type fakeRequester struct {
	readyCalls    int
	classifyCalls int
	readyErr      error // when set, RequestReadyClassified fails
	resp          []byte
}

func (f *fakeRequester) RequestReadyClassified(_ context.Context, _ string, _ []byte, _, _ time.Duration) ([]byte, error) {
	f.readyCalls++
	if f.readyErr != nil {
		return nil, f.readyErr
	}
	return f.resp, nil
}

func (f *fakeRequester) RequestClassified(_ context.Context, _ string, _ []byte, _ time.Duration) ([]byte, error) {
	f.classifyCalls++
	return f.resp, nil
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
