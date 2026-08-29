//go:build integration

package document

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

func TestIntegrationDocumentConsumerReplaysMessagePublishedBeforeStart(t *testing.T) {
	const (
		inputStream   = "GH963_DOCUMENT_INPUT"
		inputSubject  = "gh963.document.raw"
		outputStream  = "GH963_DOCUMENT_OUTPUT"
		outputSubject = "gh963.document.processed"
		documentID    = "gh963-doc-before-start"
	)

	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: inputStream, Subjects: []string{inputSubject}},
		natsclient.TestStreamConfig{Name: outputStream, Subjects: []string{outputSubject}},
	))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)

	js, err := tc.Client.JetStream()
	require.NoError(t, err)
	output, err := js.Stream(ctx, outputStream)
	require.NoError(t, err)
	observer, err := output.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       "gh963-document-output-observer",
		FilterSubject: outputSubject,
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)
	type delivery struct {
		data   []byte
		ackErr error
	}
	delivered := make(chan delivery, 1)
	observerContext, err := observer.Consume(func(msg jetstream.Msg) {
		result := delivery{
			data:   append([]byte(nil), msg.Data()...),
			ackErr: msg.Ack(),
		}
		select {
		case delivered <- result:
		default:
		}
	})
	require.NoError(t, err)
	t.Cleanup(observerContext.Stop)

	raw := []byte(`{"type":"document","id":"` + documentID + `","title":"Published before start"}`)
	_, err = tc.Client.PublishToStreamWithAck(ctx, inputSubject, raw)
	require.NoError(t, err)

	rawConfig, err := json.Marshal(ComponentConfig{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{{
				Name: "doc_in",
				Config: component.JetStreamPort{
					StreamName:    inputStream,
					Subjects:      []string{inputSubject},
					MaxAckPending: 7,
				},
			}},
			Outputs: []component.PortDefinition{{
				Name: "doc_out",
				Config: component.JetStreamPort{
					StreamName: outputStream,
					Subjects:   []string{outputSubject},
					Interface:  &component.InterfaceContract{Type: "content.document.v1"},
				},
			}},
		},
	})
	require.NoError(t, err)
	discoverable, err := NewComponent(rawConfig, component.Dependencies{
		NATSClient: tc.Client,
		Platform:   component.PlatformMeta{Org: "c360", Platform: "integration"},
	})
	require.NoError(t, err)
	processor := discoverable.(*Component)
	require.NoError(t, processor.Start(ctx))
	t.Cleanup(func() { require.NoError(t, processor.Stop(context.Background())) })

	select {
	case result := <-delivered:
		require.NoError(t, result.ackErr)
		require.True(t, bytes.Contains(result.data, []byte(documentID)),
			"the retained pre-start document must be transformed and published: %s", result.data)
	case <-ctx.Done():
		t.Fatalf("pre-start document was not processed: %v", ctx.Err())
	}

	input, err := js.Stream(ctx, inputStream)
	require.NoError(t, err)
	consumer, err := input.Consumer(ctx, "document-processor-gh963-document-raw")
	require.NoError(t, err)
	info, err := consumer.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, jetstream.DeliverAllPolicy, info.Config.DeliverPolicy)
	require.Equal(t, jetstream.AckExplicitPolicy, info.Config.AckPolicy)
	require.Equal(t, 5, info.Config.MaxDeliver)
	require.Equal(t, 7, info.Config.MaxAckPending)
}
