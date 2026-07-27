//go:build integration

package lessoncuration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
)

func TestIntegration_HandlerUnsubscribesOnShutdown(t *testing.T) {
	client := natsclient.NewTestClient(t).Client
	ctx, cancel := context.WithCancel(context.Background())
	subscription, err := client.SubscribeForRequests(
		ctx, SubjectPromote, Handler(&recordingPromoter{}),
	)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	requestData, err := json.Marshal(PromoteRequest{
		EntityID: "acme.ops.agent.lesson.record.11111111-1111-5111-8111-111111111111",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.RequestClassified(ctx, SubjectPromote, requestData, time.Second); err != nil {
		t.Fatalf("request before shutdown: %v", err)
	}

	cancel()
	if err := subscription.Unsubscribe(); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	_, err = client.RequestClassified(context.Background(), SubjectPromote, requestData, 100*time.Millisecond)
	if err == nil {
		t.Fatal("request after shutdown unexpectedly found a responder")
	}
}
