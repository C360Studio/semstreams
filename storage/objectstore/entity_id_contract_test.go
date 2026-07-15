package objectstore

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type invalidContentStorable struct {
	entityID     string
	binaryCalls  atomic.Int64
	rawCalls     atomic.Int64
	contentCalls atomic.Int64
}

type invalidTextContentStorable struct {
	entityID     string
	rawCalls     atomic.Int64
	contentCalls atomic.Int64
}

func (s *invalidTextContentStorable) EntityID() string                    { return s.entityID }
func (*invalidTextContentStorable) Triples() []message.Triple             { return nil }
func (*invalidTextContentStorable) StorageRef() *message.StorageReference { return nil }
func (s *invalidTextContentStorable) RawContent() map[string]string {
	s.rawCalls.Add(1)
	return nil
}
func (s *invalidTextContentStorable) ContentFields() map[string]string {
	s.contentCalls.Add(1)
	return nil
}

func (s *invalidContentStorable) EntityID() string                    { return s.entityID }
func (*invalidContentStorable) Triples() []message.Triple             { return nil }
func (*invalidContentStorable) StorageRef() *message.StorageReference { return nil }
func (s *invalidContentStorable) RawContent() map[string]string {
	s.rawCalls.Add(1)
	return nil
}
func (s *invalidContentStorable) ContentFields() map[string]string {
	s.contentCalls.Add(1)
	return nil
}
func (s *invalidContentStorable) BinaryFields() map[string]message.BinaryContent {
	s.binaryCalls.Add(1)
	return map[string]message.BinaryContent{"payload": {Data: []byte("orphan")}}
}

func TestStoreContent_InvalidEntityIDHasNoSideEffects(t *testing.T) {
	t.Parallel()

	writes := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "entity_contract_writes_total"}, []string{"operation"})
	store := &Store{metrics: &storeMetrics{writeOps: writes}}
	content := &invalidContentStorable{
		entityID: "a.a.a.a.a." + strings.Repeat("x", semtypes.MaxEntityIDBytes-9),
	}
	require.Len(t, content.entityID, semtypes.MaxEntityIDBytes+1)

	var ref *message.StorageReference
	var err error
	require.NotPanics(t, func() {
		ref, err = store.StoreContent(context.Background(), content)
	})
	require.Error(t, err)
	assert.Nil(t, ref)
	assert.True(t, errs.IsInvalid(err))
	assert.Zero(t, content.binaryCalls.Load())
	assert.Zero(t, content.rawCalls.Load())
	assert.Zero(t, content.contentCalls.Load())
	assert.Zero(t, testutil.ToFloat64(writes.WithLabelValues("store_content")))
}

func TestStoreContent_InvalidNonBinaryEntityIDHasNoSideEffects(t *testing.T) {
	t.Parallel()

	store := &Store{}
	content := &invalidTextContentStorable{entityID: "a.b.c.d.e.-bad"}
	ref, err := store.StoreContent(context.Background(), content)
	require.Error(t, err)
	assert.Nil(t, ref)
	assert.True(t, errs.IsInvalid(err))
	assert.Zero(t, content.rawCalls.Load())
	assert.Zero(t, content.contentCalls.Load())
}
