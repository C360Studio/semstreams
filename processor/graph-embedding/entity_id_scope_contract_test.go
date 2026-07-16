package graphembedding

import (
	"context"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type queryCallSpyEmbedder struct {
	queryCalls int
}

func (s *queryCallSpyEmbedder) Generate(context.Context, []string) ([][]float32, error) {
	return [][]float32{{1}}, nil
}

func (s *queryCallSpyEmbedder) GenerateQuery(context.Context, []string) ([][]float32, error) {
	s.queryCalls++
	return [][]float32{{1}}, nil
}

func (*queryCallSpyEmbedder) Dimensions() int { return 1 }
func (*queryCallSpyEmbedder) Model() string   { return "spy" }
func (*queryCallSpyEmbedder) Close() error    { return nil }

func TestHandleQuerySearchNATS_InvalidScopeFailsBeforeEmbedding(t *testing.T) {
	t.Parallel()

	spy := &queryCallSpyEmbedder{}
	component := &Component{embedder: spy}
	_, err := component.handleQuerySearchNATS(
		context.Background(),
		[]byte(`{"query":"find a widget","scope":["acme.*"]}`),
	)
	require.Error(t, err)
	var classified *errs.ClassifiedError
	require.True(t, errors.As(err, &classified))
	assert.Equal(t, semtypes.ErrorCodeEntityIDPrefixInvalid, classified.Code)
	assert.Zero(t, spy.queryCalls, "invalid scope must fail before paid embedding generation")
}

func TestFindSimilarEntities_InvalidScopeFailsBeforeStorage(t *testing.T) {
	t.Parallel()

	component := &Component{}
	_, err := component.findSimilarEntities(context.Background(), "", []float32{1}, []string{"acme.*"}, 10)
	require.Error(t, err)
	assert.True(t, errs.IsInvalid(err))
	var classified *errs.ClassifiedError
	require.True(t, errors.As(err, &classified))
	assert.Equal(t, semtypes.ErrorCodeEntityIDPrefixInvalid, classified.Code)
}

func TestValidateEntityIDScope_PreservesExplicitEmptyMatchAll(t *testing.T) {
	t.Parallel()
	require.NoError(t, validateEntityIDScope([]string{""}))
	assert.True(t, graph.MatchesAnyIDPrefix("acme.ops.robotics.gcs.drone.001", []string{""}))
}
