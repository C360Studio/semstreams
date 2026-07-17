package objectstore

import (
	"errors"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type storedMessageTestGraphable struct {
	entityID string
}

// entity-id-audit:classify intentional-malformed "" line=51 column=14 surface=go-field:.entityID entity_id_invalid:empty empty stored-message ID rejection fixture
// entity-id-audit:classify intentional-malformed "legacy-id" line=56 column=16 surface=go-field:.entityID entity_id_invalid:arity legacy stored-message ID rejection fixture
// entity-id-audit:classify intentional-malformed "legacy-id" line=100 column=41 surface=go-field:storedMessageTestGraphable.entityID entity_id_invalid:arity legacy Graphable ID rejection fixture

func (g *storedMessageTestGraphable) EntityID() string        { return g.entityID }
func (*storedMessageTestGraphable) Triples() []message.Triple { return nil }

func TestStoredMessageValidateEntityIDContract(t *testing.T) {
	t.Parallel()

	validBoundaryID := "a.b.c.d.e." + strings.Repeat("x", semtypes.MaxEntityIDBytes-10)
	require.Len(t, validBoundaryID, semtypes.MaxEntityIDBytes)

	tests := []struct {
		name       string
		entityID   string
		storageRef *message.StorageReference
		wantCode   string
		wantErr    string
	}{
		{
			name:       "canonical ID",
			entityID:   "acme.ops.plan.fanout.unit.1",
			storageRef: &message.StorageReference{},
		},
		{
			name:       "256 byte canonical ID",
			entityID:   validBoundaryID,
			storageRef: &message.StorageReference{},
		},
		{
			name:     "empty ID rejected before storage ref",
			entityID: "",
			wantCode: semtypes.ErrorCodeEntityIDInvalid,
		},
		{
			name:       "noncanonical ID",
			entityID:   "legacy-id",
			storageRef: &message.StorageReference{},
			wantCode:   semtypes.ErrorCodeEntityIDInvalid,
		},
		{
			name:       "257 byte ID",
			entityID:   validBoundaryID + "x",
			storageRef: &message.StorageReference{},
			wantCode:   semtypes.ErrorCodeEntityIDInvalid,
		},
		{
			name:     "missing storage ref",
			entityID: "acme.ops.plan.fanout.unit.1",
			wantErr:  "storage_ref is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg := &StoredMessage{entityID: tt.entityID, storageRef: tt.storageRef}
			err := msg.Validate()
			if tt.wantCode == "" && tt.wantErr == "" {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			if tt.wantErr != "" {
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			var classified *errs.ClassifiedError
			require.True(t, errors.As(err, &classified), "entity contract error type must be preserved")
			assert.Equal(t, tt.wantCode, classified.Code)
		})
	}
}

func TestStoredMessageProductionMarshalPreservesEntityIDCode(t *testing.T) {
	t.Parallel()

	stored := NewStoredMessage(
		&storedMessageTestGraphable{entityID: "legacy-id"},
		&message.StorageReference{StorageInstance: "objectstore", Key: "stored-key"},
		"test.payload.v1",
	)
	wire := message.NewBaseMessage(stored.Schema(), stored, "objectstore")

	_, err := wire.MarshalJSON()
	require.Error(t, err)
	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	assert.Equal(t, semtypes.ErrorCodeEntityIDInvalid, classified.Code)
	assert.Contains(t, err.Error(), "BaseMessage.MarshalJSON")
}
