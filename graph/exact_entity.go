package graph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
)

const exactEntityQuerySubject = "graph.ingest.query.entity"

// ExactEntity is one validated authority value and the revision of the same
// ENTITY_STATES entry. EntityState.Version is logical metadata and is not a
// substitute for KVRevision.
type ExactEntity struct {
	Entity     *EntityState `json:"entity"`
	KVRevision uint64       `json:"kvRevision"`
}

// ExactEntityReader is the operation-specific embedded authority-read adapter.
type ExactEntityReader interface {
	ReadExactEntity(context.Context, string) (*ExactEntity, error)
}

type natsExactEntityReader struct {
	requester interface {
		RequestClassified(context.Context, string, []byte, time.Duration) ([]byte, error)
	}
	timeout time.Duration
}

// NewExactEntityReader constructs the narrow embedded authority-read adapter.
// It exposes no raw subject, KV handle, or aggregate graph-client capability.
func NewExactEntityReader(
	requester interface {
		RequestClassified(context.Context, string, []byte, time.Duration) ([]byte, error)
	},
	timeout time.Duration,
) ExactEntityReader {
	if timeout <= 0 {
		timeout = natsclient.DefaultRequestTimeout
	}
	return &natsExactEntityReader{requester: requester, timeout: timeout}
}

func (r *natsExactEntityReader) ReadExactEntity(ctx context.Context, entityID string) (*ExactEntity, error) {
	if r == nil || r.requester == nil {
		return nil, errors.New("exact entity reader is unavailable")
	}
	if err := semtypes.ValidateEntityID(entityID); err != nil {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, ErrorCodeInvalidRequest,
			fmt.Errorf("exact entity ID: %w", err))
	}
	request, err := json.Marshal(struct {
		ID string `json:"id"`
	}{ID: entityID})
	if err != nil {
		return nil, fmt.Errorf("marshal exact entity request: %w", err)
	}
	response, err := r.requester.RequestClassified(ctx, exactEntityQuerySubject, request, r.timeout)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Entity     json.RawMessage `json:"entity"`
		KVRevision uint64          `json:"kvRevision"`
	}
	if err := json.Unmarshal(response, &envelope); err != nil {
		return nil, exactResponseError(fmt.Errorf("decode exact entity response: %w", err))
	}
	if len(envelope.Entity) == 0 || string(envelope.Entity) == "null" {
		return nil, exactResponseError(errors.New("exact entity response has no entity"))
	}
	if envelope.KVRevision == 0 {
		return nil, exactResponseError(errors.New("exact entity response has zero KV revision"))
	}
	var entity EntityState
	if err := UnmarshalEntityState(envelope.Entity, &entity); err != nil {
		return nil, ClassifyStateContractError(err)
	}
	if entity.ID != entityID {
		return nil, ClassifyStateContractError(&StateContractError{
			Reason:   GraphStateReasonNoncanonicalEntityID,
			EntityID: entityID,
			Err:      fmt.Errorf("authority key contains entity %q", entity.ID),
		})
	}
	return &ExactEntity{Entity: entity.Clone(), KVRevision: envelope.KVRevision}, nil
}

func exactResponseError(err error) error {
	return errs.ClassifiedCode(errs.ErrorFatal, ErrorCodeInternal, err)
}
