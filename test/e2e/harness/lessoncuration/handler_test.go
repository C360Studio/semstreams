package lessoncuration

import (
	"context"
	"encoding/json"
	"testing"
)

type recordingPromoter struct {
	entityID string
}

func (p *recordingPromoter) Promote(_ context.Context, entityID string) error {
	p.entityID = entityID
	return nil
}

func TestHandlerPromotesValidLesson(t *testing.T) {
	t.Parallel()
	const entityID = "acme.ops.agent.lesson.record.11111111-1111-5111-8111-111111111111"
	promoter := &recordingPromoter{}
	data, err := json.Marshal(PromoteRequest{EntityID: entityID})
	if err != nil {
		t.Fatal(err)
	}

	responseData, err := Handler(promoter)(context.Background(), data)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if promoter.entityID != entityID {
		t.Fatalf("promoted entity = %q, want %q", promoter.entityID, entityID)
	}
	var response PromoteResponse
	if err := json.Unmarshal(responseData, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.Promoted {
		t.Fatal("response did not confirm promotion")
	}
}

func TestHandlerRejectsMissingOrInvalidEntityID(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		data []byte
	}{
		{name: "missing", data: []byte(`{}`)},
		{name: "invalid", data: []byte(`{"entity_id":"not-an-entity"}`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			promoter := &recordingPromoter{}
			if _, err := Handler(promoter)(context.Background(), test.data); err == nil {
				t.Fatal("Handler returned nil error")
			}
			if promoter.entityID != "" {
				t.Fatalf("invalid request invoked promoter with %q", promoter.entityID)
			}
		})
	}
}
