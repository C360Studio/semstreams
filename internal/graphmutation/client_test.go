package graphmutation

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	semerrs "github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
)

const clientTestEntity = "acme.ops.test.system.widget.001"

type requesterCall struct {
	subject string
	payload []byte
}

type fakeRequester struct {
	reply []byte
	err   error
	calls []requesterCall
}

func (f *fakeRequester) RequestClassified(
	_ context.Context,
	subject string,
	payload []byte,
	_ time.Duration,
) ([]byte, error) {
	f.calls = append(f.calls, requesterCall{subject: subject, payload: append([]byte(nil), payload...)})
	return f.reply, f.err
}

func TestClientAppendUsesCanonicalSubjectAndOneRequest(t *testing.T) {
	request := graph.AppendTriplesRequest{Triples: []message.Triple{{
		Subject: clientTestEntity, Predicate: "test.value.name", Object: "one",
	}}}
	fake := &fakeRequester{reply: mustJSON(t, graph.AppendTriplesResponse{Results: []graph.AppendSubjectResult{{
		EntityID: clientTestEntity, Outcome: graph.MutationApplied, KVRevision: 12,
	}}})}
	client, err := NewClient(fake, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Append(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 1 || fake.calls[0].subject != "graph.mutation.triple.append" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if response.Results[0].KVRevision != 12 {
		t.Fatalf("revision = %d", response.Results[0].KVRevision)
	}
}

func TestClientDoesNotRetryAmbiguousTransportFailure(t *testing.T) {
	want := errors.New("timeout after possible delivery")
	fake := &fakeRequester{err: want}
	client, err := NewClient(fake, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Append(context.Background(), graph.AppendTriplesRequest{Triples: []message.Triple{{
		Subject: clientTestEntity, Predicate: "test.value.name", Object: "one",
	}}})
	if !errors.Is(err, want) || len(fake.calls) != 1 {
		t.Fatalf("error = %v, calls = %d", err, len(fake.calls))
	}
	var transport *TransportError
	if !errors.As(err, &transport) || transport.Outcome != TransportCommitUnknown || !IsCommitUnknown(err) {
		t.Fatalf("error = %#v, want commit_unknown", err)
	}
}

func TestClientClassifiesDefiniteNonDelivery(t *testing.T) {
	t.Run("no responder", func(t *testing.T) {
		fake := &fakeRequester{err: nats.ErrNoResponders}
		client, _ := NewClient(fake, time.Second)
		_, err := client.Delete(context.Background(), graph.DeleteEntityRequest{
			EntityID: clientTestEntity, ExpectedRevision: 1,
		})
		var transport *TransportError
		if !errors.As(err, &transport) || transport.Outcome != TransportUnavailable ||
			!IsDefinitelyNotCommitted(err) || len(fake.calls) != 1 {
			t.Fatalf("error = %#v, calls = %d", err, len(fake.calls))
		}
	})

	t.Run("context done before request", func(t *testing.T) {
		fake := &fakeRequester{}
		client, _ := NewClient(fake, time.Second)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := client.Delete(ctx, graph.DeleteEntityRequest{
			EntityID: clientTestEntity, ExpectedRevision: 1,
		})
		var transport *TransportError
		if !errors.As(err, &transport) || transport.Outcome != TransportDeadline ||
			!IsDefinitelyNotCommitted(err) || len(fake.calls) != 0 {
			t.Fatalf("error = %#v, calls = %d", err, len(fake.calls))
		}
	})
}

func TestClientPreservesDefiniteClassifiedRejection(t *testing.T) {
	rejection := semerrs.ClassifiedCode(semerrs.ErrorInvalid, graph.ErrorCodeEntityNotFound, errors.New("missing"))
	fake := &fakeRequester{err: rejection}
	client, _ := NewClient(fake, time.Second)
	_, err := client.Append(context.Background(), graph.AppendTriplesRequest{Triples: []message.Triple{{
		Subject: clientTestEntity, Predicate: "test.value.name", Object: "one",
	}}})
	var classified *semerrs.ClassifiedError
	var transport *TransportError
	if !errors.As(err, &classified) || classified.Code != graph.ErrorCodeEntityNotFound ||
		errors.As(err, &transport) || IsCommitUnknown(err) || len(fake.calls) != 1 {
		t.Fatalf("error = %#v, calls = %d", err, len(fake.calls))
	}
}

func TestClientClassifiesInvalidSuccessReplyAsCommitUnknown(t *testing.T) {
	tests := []struct {
		name  string
		reply []byte
	}{
		{name: "malformed JSON", reply: []byte(`{"outcome":`)},
		{name: "invalid success shape", reply: mustJSON(t, graph.CreateEntityResponse{Outcome: graph.MutationApplied})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeRequester{reply: tt.reply}
			client, _ := NewClient(fake, time.Second)
			_, err := client.Create(context.Background(), graph.CreateEntityRequest{
				Entity: &graph.EntityState{ID: clientTestEntity},
			})
			if !IsCommitUnknown(err) || len(fake.calls) != 1 {
				t.Fatalf("error = %#v, calls = %d", err, len(fake.calls))
			}
		})
	}
}

func TestClientAppendPreservesPartialOutcomeAndFailure(t *testing.T) {
	other := "acme.ops.test.system.widget.002"
	request := graph.AppendTriplesRequest{Triples: []message.Triple{
		{Subject: clientTestEntity, Predicate: "test.value.name", Object: "one"},
		{Subject: other, Predicate: "test.value.name", Object: "two"},
	}}
	want := graph.AppendTriplesResponse{Results: []graph.AppendSubjectResult{
		{EntityID: clientTestEntity, Outcome: graph.MutationApplied, KVRevision: 9},
		{EntityID: other, Outcome: graph.MutationFailed,
			Error: &graph.MutationFailure{Class: "fatal", Code: graph.ErrorCodeGraphStateResetRequired}},
	}}
	fake := &fakeRequester{reply: mustJSON(t, want)}
	client, _ := NewClient(fake, time.Second)
	got, err := client.Append(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("response = %#v, want %#v", *got, want)
	}
}

func TestClientAppendRejectsErrorOnAppliedOutcome(t *testing.T) {
	fake := &fakeRequester{reply: mustJSON(t, graph.AppendTriplesResponse{Results: []graph.AppendSubjectResult{{
		EntityID: clientTestEntity,
		Outcome:  graph.MutationApplied,
		Error:    &graph.MutationFailure{Class: "fatal", Code: graph.ErrorCodeInternal},
	}}})}
	client, _ := NewClient(fake, time.Second)
	_, err := client.Append(context.Background(), graph.AppendTriplesRequest{Triples: []message.Triple{{Subject: clientTestEntity}}})
	if err == nil {
		t.Fatal("Append() accepted error on applied outcome")
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
