package agenticmodel

import (
	"context"
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

type retainedResponseEvidence struct {
	subject string
	data    []byte
}

type retainedResponseEvidenceReader interface {
	ReadRetainedResponse(context.Context, string, string) (retainedResponseEvidence, bool, error)
}

type natsRetainedResponseEvidenceReader struct {
	client *natsclient.Client
}

func (r natsRetainedResponseEvidenceReader) ReadRetainedResponse(
	ctx context.Context,
	streamName string,
	subject string,
) (retainedResponseEvidence, bool, error) {
	stream, err := r.client.GetStream(ctx, streamName)
	if err != nil {
		return retainedResponseEvidence{}, false, fmt.Errorf("read response stream %s: %w", streamName, err)
	}
	raw, err := stream.GetLastMsgForSubject(ctx, subject)
	if errors.Is(err, jetstream.ErrMsgNotFound) {
		return retainedResponseEvidence{}, false, nil
	}
	if err != nil {
		return retainedResponseEvidence{}, false, fmt.Errorf("read retained response %s: %w", subject, err)
	}
	return retainedResponseEvidence{
		subject: raw.Subject,
		data:    append([]byte(nil), raw.Data...),
	}, true, nil
}

func responseAddress(ports []component.PortDefinition, requestID string) (string, string, error) {
	subject, err := component.ResolveSubject(ports, "agent.response", requestID)
	if err != nil {
		return "", "", err
	}
	for _, definition := range ports {
		if definition.Name != "agent.response" {
			continue
		}
		port, err := definition.Resolve(component.DirectionOutput)
		if err != nil {
			return "", "", err
		}
		facts, err := port.Facts()
		if err != nil {
			return "", "", err
		}
		stream, ok := facts.Stream()
		if !ok || stream.Name() == "" {
			return "", "", fmt.Errorf("agent.response output does not declare a JetStream stream")
		}
		return subject, stream.Name(), nil
	}
	return "", "", fmt.Errorf("agent.response output not found")
}

func (c *Component) readRetainedAgentResponse(
	ctx context.Context,
	requestID string,
) (agentic.AgentResponse, bool, error) {
	subject, streamName, err := responseAddress(c.outputPortDefs(), requestID)
	if err != nil {
		return agentic.AgentResponse{}, false, errs.WrapFatal(
			err, "Component", "readRetainedAgentResponse", "resolve response address")
	}
	reader := c.responseEvidence
	if reader == nil {
		reader = natsRetainedResponseEvidenceReader{client: c.natsClient}
	}
	evidence, found, err := reader.ReadRetainedResponse(ctx, streamName, subject)
	if err != nil {
		return agentic.AgentResponse{}, false, errs.WrapTransient(
			err, "Component", "readRetainedAgentResponse", "read retained response evidence")
	}
	if !found {
		return agentic.AgentResponse{}, false, nil
	}

	decoded, err := c.decoder.Decode(evidence.data)
	if err != nil {
		return agentic.AgentResponse{}, false, errs.WrapFatal(
			err, "Component", "readRetainedAgentResponse", "response correlation conflict: decode retained response")
	}
	if err := decoded.Validate(); err != nil {
		return agentic.AgentResponse{}, false, errs.WrapFatal(
			err, "Component", "readRetainedAgentResponse", "response correlation conflict: invalid retained response")
	}
	response, ok := decoded.Payload().(*agentic.AgentResponse)
	if !ok {
		return agentic.AgentResponse{}, false, errs.WrapFatal(
			fmt.Errorf("unexpected retained payload type %T", decoded.Payload()),
			"Component", "readRetainedAgentResponse", "response correlation conflict")
	}
	if evidence.subject != subject || response.RequestID != requestID {
		return agentic.AgentResponse{}, false, errs.WrapFatal(
			fmt.Errorf(
				"subject request ID %q, payload request ID %q, and source request ID %q must agree",
				evidence.subject, response.RequestID, requestID,
			),
			"Component", "readRetainedAgentResponse", "response correlation conflict")
	}
	return *response, true, nil
}
