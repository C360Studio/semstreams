package graphingest

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/errs"
)

// The Graphable lane validates a decoded payload before extraction and fences
// a panic raised by the payload's identity methods (#1112, design D1/D2).
// These drive the production decoder and decodeEntity/handleMessage, with the
// payload registered in a test registry the way a product type would be.

// fenceTestMode selects how a decoded fenceTestPayload misbehaves. It travels
// on the wire, so the decoded copy carries it.
const (
	fenceModeConforming   = "conforming"
	fenceModeInvalid      = "invalid"
	fenceModePanicID      = "panic-entity-id"
	fenceModePanicTriples = "panic-triples"
)

// fenceCallRecorder counts the identity calls made on decoded payloads. The
// registry factory hands every decoded instance the same recorder.
type fenceCallRecorder struct {
	entityIDCalls atomic.Int64
	triplesCalls  atomic.Int64
}

type fenceTestPayload struct {
	ID   string `json:"id"`
	Mode string `json:"mode"`

	rec *fenceCallRecorder
}

func fenceTestType() message.Type {
	return message.Type{Domain: "test", Category: "fence", Version: "v1"}
}

func (p *fenceTestPayload) Schema() message.Type { return fenceTestType() }

func (p *fenceTestPayload) Validate() error {
	if p.Mode == fenceModeInvalid {
		return errors.New("fence test payload: mode invalid")
	}
	return nil
}

func (p *fenceTestPayload) EntityID() string {
	if p.rec != nil {
		p.rec.entityIDCalls.Add(1)
	}
	if p.Mode == fenceModePanicID {
		panic("fence test payload: EntityID boom")
	}
	return p.ID
}

func (p *fenceTestPayload) Triples() []message.Triple {
	if p.rec != nil {
		p.rec.triplesCalls.Add(1)
	}
	if p.Mode == fenceModePanicTriples {
		panic("fence test payload: Triples boom")
	}
	return []message.Triple{{Subject: p.ID, Predicate: "test.fence.mode", Object: p.Mode}}
}

func (p *fenceTestPayload) MarshalJSON() ([]byte, error) {
	type alias fenceTestPayload
	return json.Marshal((*alias)(p))
}

func (p *fenceTestPayload) UnmarshalJSON(data []byte) error {
	type alias fenceTestPayload
	return json.Unmarshal(data, (*alias)(p))
}

// newFenceTestComponent returns a component whose decoder resolves the fence
// test type, and the recorder every decoded instance reports to.
func newFenceTestComponent(t *testing.T) (*Component, *fenceCallRecorder, *mockKVBucket) {
	t.Helper()
	comp, bucket := createTestComponentWithMockKVBucket(t)
	rec := &fenceCallRecorder{}
	reg := payloadregistry.New()
	typ := fenceTestType()
	require.NoError(t, reg.Register(&payloadregistry.Registration{
		Domain:      typ.Domain,
		Category:    typ.Category,
		Version:     typ.Version,
		Description: "graph-ingest validate-then-fence test payload",
		Factory:     func() any { return &fenceTestPayload{rec: rec} },
	}))
	comp.decoder = message.NewDecoder(reg)
	return comp, rec, bucket
}

func storedKeyCount(bucket *mockKVBucket) int {
	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	return len(bucket.data)
}

// fenceWireJSON hand-writes the BaseMessage wire form. The invalid mode cannot
// be produced through BaseMessage.MarshalJSON (it validates), which is exactly
// the producer that bypasses the envelope this lane must still refuse.
func fenceWireJSON(t *testing.T, mode string) []byte {
	t.Helper()
	typ := fenceTestType()
	wire := map[string]any{
		"id":   "fence-msg-001",
		"type": map[string]string{"domain": typ.Domain, "category": typ.Category, "version": typ.Version},
		"payload": map[string]string{
			"id":   flParentID,
			"mode": mode,
		},
		"meta": map[string]any{"created_at": 1, "received_at": 1, "source": "fence-test"},
	}
	data, err := json.Marshal(wire)
	require.NoError(t, err)
	return data
}

func TestDecodeEntity_ValidateFailureRejectsBeforeIdentity(t *testing.T) {
	comp, rec, _ := newFenceTestComponent(t)

	entity, err := comp.decodeEntity("test.fence", fenceWireJSON(t, fenceModeInvalid))

	require.Error(t, err)
	assert.Nil(t, entity)
	assert.True(t, errs.IsInvalid(err), "validation failure must be a classified invalid error: %v", err)
	assert.Contains(t, err.Error(), "payload validation failed")
	assert.Contains(t, err.Error(), "mode invalid")
	assert.Zero(t, rec.entityIDCalls.Load(), "EntityID must not run on a payload that failed Validate")
	assert.Zero(t, rec.triplesCalls.Load(), "Triples must not run on a payload that failed Validate")
}

func TestDecodeEntity_FencesIdentityPanics(t *testing.T) {
	tests := []struct {
		mode      string
		recovered string
	}{
		{mode: fenceModePanicID, recovered: "EntityID boom"},
		{mode: fenceModePanicTriples, recovered: "Triples boom"},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			comp, _, _ := newFenceTestComponent(t)
			data := fenceWireJSON(t, tt.mode)

			var (
				entity any
				err    error
			)
			require.NotPanics(t, func() {
				entity, err = comp.decodeEntity("test.fence", data)
			})
			require.Error(t, err)
			assert.Nil(t, entity)
			assert.True(t, errs.IsInvalid(err), "a fenced panic must be a classified error: %v", err)
			assert.Contains(t, err.Error(), fenceTestType().String(), "the error names the message type")
			assert.Contains(t, err.Error(), tt.recovered, "the error names the recovered value")
		})
	}
}

// The poison accounting is the lane's existing path: handleMessage counts the
// rejection and returns; nothing escapes and nothing is persisted. The
// conforming row is the control that makes "nothing persisted" observable.
func TestHandleMessage_RejectionsLandOnPoisonAccounting(t *testing.T) {
	tests := []struct {
		mode       string
		wantErrors int64
		wantStored int
	}{
		{mode: fenceModeConforming, wantErrors: 0, wantStored: 1},
		{mode: fenceModeInvalid, wantErrors: 1, wantStored: 0},
		{mode: fenceModePanicID, wantErrors: 1, wantStored: 0},
		{mode: fenceModePanicTriples, wantErrors: 1, wantStored: 0},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			comp, _, bucket := newFenceTestComponent(t)
			data := fenceWireJSON(t, tt.mode)

			require.NotPanics(t, func() {
				comp.handleMessage(context.Background(), "test.fence", data)
			})

			assert.Equal(t, tt.wantErrors, atomic.LoadInt64(&comp.errors), "poison count for mode %s", tt.mode)
			assert.Equal(t, tt.wantStored, storedKeyCount(bucket), "persisted entities for mode %s", tt.mode)
		})
	}
}
