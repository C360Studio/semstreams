package lessoncuration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
)

// Promoter is the narrow lesson-curation capability exposed to the E2E
// request handler.
type Promoter interface {
	Promote(context.Context, string) error
}

// Handler adapts a Promoter to the E2E lesson-curation request/reply contract.
func Handler(promoter Promoter) func(context.Context, []byte) ([]byte, error) {
	return func(ctx context.Context, data []byte) ([]byte, error) {
		if promoter == nil {
			return nil, errors.New("lesson curation promoter is required")
		}
		var request PromoteRequest
		if err := json.Unmarshal(data, &request); err != nil {
			return nil, errs.WrapInvalid(err, "e2e-lesson-curation", "promote", "decode request")
		}
		if !message.IsValidEntityID(request.EntityID) {
			return nil, errs.WrapInvalid(
				fmt.Errorf("entity_id %q is not a well-formed 6-part entity ID", request.EntityID),
				"e2e-lesson-curation",
				"promote",
				"validate request",
			)
		}
		if err := promoter.Promote(ctx, request.EntityID); err != nil {
			return nil, fmt.Errorf("promote lesson %s: %w", request.EntityID, err)
		}
		response, err := json.Marshal(PromoteResponse{Promoted: true})
		if err != nil {
			return nil, fmt.Errorf("encode promotion response: %w", err)
		}
		return response, nil
	}
}
