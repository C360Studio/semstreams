package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

type retainedLoopMessage struct {
	subject string
	data    []byte
}

// loopSettlementEvidenceReader is private and operation-specific: task 4
// needs only the exact current request and originating response. It is not a
// stream query surface and cannot enumerate or scan AGENT.
type loopSettlementEvidenceReader interface {
	ReadAgentRequest(context.Context, string, string) (retainedLoopMessage, bool, error)
	ReadAgentResponse(context.Context, string, string) (retainedLoopMessage, bool, error)
}

type natsLoopSettlementEvidenceReader struct {
	client *natsclient.Client
}

func (r natsLoopSettlementEvidenceReader) ReadAgentRequest(
	ctx context.Context, streamName, subject string,
) (retainedLoopMessage, bool, error) {
	return r.readExact(ctx, streamName, subject)
}

func (r natsLoopSettlementEvidenceReader) ReadAgentResponse(
	ctx context.Context, streamName, subject string,
) (retainedLoopMessage, bool, error) {
	return r.readExact(ctx, streamName, subject)
}

func (r natsLoopSettlementEvidenceReader) readExact(
	ctx context.Context, streamName, subject string,
) (retainedLoopMessage, bool, error) {
	stream, err := r.client.GetStream(ctx, streamName)
	if err != nil {
		return retainedLoopMessage{}, false, err
	}
	raw, err := stream.GetLastMsgForSubject(ctx, subject)
	if errors.Is(err, jetstream.ErrMsgNotFound) {
		return retainedLoopMessage{}, false, nil
	}
	if err != nil {
		return retainedLoopMessage{}, false, err
	}
	return retainedLoopMessage{subject: raw.Subject, data: append([]byte(nil), raw.Data...)}, true, nil
}

func agentRequestAddress(definitions []component.PortDefinition, loopID string) (string, string, error) {
	subject, err := component.ResolveSubject(definitions, "agent.request", loopID)
	if err != nil {
		return "", "", err
	}
	for _, definition := range definitions {
		if definition.Name != "agent.request" {
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
			return "", "", errors.New("agent.request output does not declare a JetStream stream")
		}
		return subject, stream.Name(), nil
	}
	return "", "", errors.New("agent.request output not found")
}

func agentResponseAddress(definitions []component.PortDefinition, requestID string) (string, string, error) {
	subject, err := component.ResolveSubject(definitions, "agent.response", requestID)
	if err != nil {
		return "", "", err
	}
	for _, definition := range definitions {
		if definition.Name != "agent.response" {
			continue
		}
		port, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			return "", "", err
		}
		facts, err := port.Facts()
		if err != nil {
			return "", "", err
		}
		stream, ok := facts.Stream()
		if !ok || stream.Name() == "" {
			return "", "", errors.New("agent.response input does not declare a JetStream stream")
		}
		return subject, stream.Name(), nil
	}
	return "", "", errors.New("agent.response input not found")
}

func (c *Component) readLoopEntity(ctx context.Context, loopID string) (agentic.LoopEntity, bool, error) {
	if c.loopsBucket == nil {
		return agentic.LoopEntity{}, false, errors.New("AGENT_LOOPS is unavailable")
	}
	entry, err := c.loopsBucket.Get(ctx, loopID)
	if errors.Is(err, jetstream.ErrKeyNotFound) || errors.Is(err, jetstream.ErrKeyDeleted) {
		return agentic.LoopEntity{}, false, nil
	}
	if err != nil {
		return agentic.LoopEntity{}, false, fmt.Errorf("read AGENT_LOOPS/%s: %w", loopID, err)
	}
	var entity agentic.LoopEntity
	if err := json.Unmarshal(entry.Value(), &entity); err != nil {
		return agentic.LoopEntity{}, false, errs.WrapFatal(
			err, "agentic-loop", "readLoopEntity", "decode loop correlation",
		)
	}
	if err := entity.Validate(); err != nil {
		return agentic.LoopEntity{}, false, errs.WrapFatal(
			err, "agentic-loop", "readLoopEntity", "validate loop correlation",
		)
	}
	if entity.ID != loopID || entity.TaskID == "" {
		return agentic.LoopEntity{}, false, errs.WrapFatal(
			fmt.Errorf("key loop ID %q, payload loop ID %q, and non-empty task ID must agree", loopID, entity.ID),
			"agentic-loop", "readLoopEntity", "loop correlation conflict",
		)
	}
	return entity, true, nil
}

func (c *Component) readRetainedAgentRequest(
	ctx context.Context, loopID string,
) (agentic.AgentRequest, bool, error) {
	subject, streamName, err := agentRequestAddress(c.config.Ports.Outputs, loopID)
	if err != nil {
		return agentic.AgentRequest{}, false, errs.WrapFatal(
			err, "agentic-loop", "readRetainedAgentRequest", "resolve exact request address",
		)
	}
	reader := c.settlementEvidence
	if reader == nil {
		if c.natsClient == nil {
			return agentic.AgentRequest{}, false, errors.New("AGENT request reader is unavailable")
		}
		reader = natsLoopSettlementEvidenceReader{client: c.natsClient}
	}
	evidence, found, err := reader.ReadAgentRequest(ctx, streamName, subject)
	if err != nil {
		return agentic.AgentRequest{}, false, fmt.Errorf("read exact request %s: %w", subject, err)
	}
	if !found {
		return agentic.AgentRequest{}, false, nil
	}
	decoded, err := c.decoder.Decode(evidence.data)
	if err != nil {
		return agentic.AgentRequest{}, false, errs.WrapFatal(
			err, "agentic-loop", "readRetainedAgentRequest", "request correlation conflict: decode",
		)
	}
	request, ok := decoded.Payload().(*agentic.AgentRequest)
	if !ok {
		return agentic.AgentRequest{}, false, errs.WrapFatal(
			fmt.Errorf("unexpected retained payload type %T", decoded.Payload()),
			"agentic-loop", "readRetainedAgentRequest", "request correlation conflict",
		)
	}
	if err := request.Validate(); err != nil {
		return agentic.AgentRequest{}, false, errs.WrapFatal(
			err, "agentic-loop", "readRetainedAgentRequest", "request correlation conflict: validate",
		)
	}
	if evidence.subject != subject || request.LoopID != loopID ||
		!strings.HasPrefix(request.RequestID, loopID+":req:") {
		return agentic.AgentRequest{}, false, errs.WrapFatal(
			fmt.Errorf("subject %q, request loop %q, request ID %q, and source loop %q must agree",
				evidence.subject, request.LoopID, request.RequestID, loopID),
			"agentic-loop", "readRetainedAgentRequest", "request correlation conflict",
		)
	}
	return *request, true, nil
}

func (c *Component) readRetainedAgentResponse(
	ctx context.Context, requestID string,
) (agentic.AgentResponse, bool, error) {
	subject, streamName, err := agentResponseAddress(c.config.Ports.Inputs, requestID)
	if err != nil {
		return agentic.AgentResponse{}, false, errs.WrapFatal(
			err, "agentic-loop", "readRetainedAgentResponse", "resolve exact response address",
		)
	}
	reader := c.settlementEvidence
	if reader == nil {
		if c.natsClient == nil {
			return agentic.AgentResponse{}, false, errors.New("AGENT response reader is unavailable")
		}
		reader = natsLoopSettlementEvidenceReader{client: c.natsClient}
	}
	evidence, found, err := reader.ReadAgentResponse(ctx, streamName, subject)
	if err != nil {
		return agentic.AgentResponse{}, false, fmt.Errorf("read exact response %s: %w", subject, err)
	}
	if !found {
		return agentic.AgentResponse{}, false, nil
	}
	decoded, err := c.decoder.Decode(evidence.data)
	if err != nil {
		return agentic.AgentResponse{}, false, errs.WrapFatal(
			err, "agentic-loop", "readRetainedAgentResponse", "response correlation conflict: decode",
		)
	}
	response, ok := decoded.Payload().(*agentic.AgentResponse)
	if !ok || evidence.subject != subject || response.RequestID != requestID {
		return agentic.AgentResponse{}, false, errs.WrapFatal(
			fmt.Errorf("subject %q, retained request ID, and source request ID %q must agree", evidence.subject, requestID),
			"agentic-loop", "readRetainedAgentResponse", "response correlation conflict",
		)
	}
	if err := response.Validate(); err != nil {
		return agentic.AgentResponse{}, false, errs.WrapFatal(
			err, "agentic-loop", "readRetainedAgentResponse", "response correlation conflict: validate",
		)
	}
	return *response, true, nil
}

func loopIDFromRequestID(requestID string) (string, error) {
	loopID, suffix, ok := strings.Cut(requestID, ":req:")
	if !ok || loopID == "" || suffix == "" || strings.Contains(suffix, ":req:") {
		return "", fmt.Errorf("request ID %q is not a loop request identity", requestID)
	}
	return loopID, nil
}

func (c *Component) recoverTaskDelivery(
	ctx context.Context,
	task agentic.TaskMessage,
) (HandlerResult, error) {
	if c.loopsBucket == nil {
		return HandlerResult{}, nil
	}
	entity, found, err := c.readLoopEntity(ctx, task.LoopID)
	if err != nil {
		return HandlerResult{}, err
	}
	if !found {
		return HandlerResult{}, nil
	}
	if entity.TaskID != task.TaskID || entity.Role != task.Role || entity.Model != task.Model {
		return HandlerResult{}, errs.WrapFatal(
			fmt.Errorf("durable loop %q owns task %q/%q/%q but delivery carries %q/%q/%q",
				entity.ID, entity.TaskID, entity.Role, entity.Model, task.TaskID, task.Role, task.Model),
			"agentic-loop", "recoverTaskDelivery", "task correlation conflict",
		)
	}
	if entity.State.IsTerminal() {
		return HandlerResult{LoopID: entity.ID, State: entity.State}, nil
	}

	request, retained, err := c.readRetainedAgentRequest(ctx, entity.ID)
	if err != nil {
		return HandlerResult{}, err
	}
	if retained {
		if request.Role != task.Role || request.Model != task.Model {
			return HandlerResult{}, errs.WrapFatal(
				fmt.Errorf("retained request %q conflicts with task role/model", request.RequestID),
				"agentic-loop", "recoverTaskDelivery", "task correlation conflict",
			)
		}
	} else {
		assembled := c.handler.assembleSystemPrompt(ctx, task)
		messages := c.handler.buildInitialMessagesWithPrompt(task, assembled)
		messages = c.handler.prependIterationContext(ctx, entity.ID, 1, entity.MaxIterations, messages)
		tools := task.Tools
		if tools == nil {
			tools = c.handler.discoverTools()
		}
		request = c.handler.newTaskRequest(entity.ID, task, messages, tools)
	}

	if err := c.handler.loopManager.restoreLoopFromRequest(entity, request); err != nil {
		return HandlerResult{}, errs.WrapFatal(
			err, "agentic-loop", "recoverTaskDelivery", "restore task correlation",
		)
	}
	keepLoop := false
	defer func() {
		if !keepLoop {
			_ = c.handler.loopManager.DeleteLoop(entity.ID)
		}
	}()
	if _, err := c.handler.trajectoryManager.startTrajectory(entity.ID); err != nil {
		return HandlerResult{}, fmt.Errorf("restore task trajectory for loop %q: %w", entity.ID, err)
	}
	keepTrajectory := false
	defer func() {
		if !keepTrajectory {
			c.handler.trajectoryManager.discardTrajectory(entity.ID)
		}
	}()
	c.handler.loopManager.CacheTaskPrompt(entity.ID, task.Prompt)
	result, err := c.handler.buildTaskResultFromRequest(entity.ID, task, entity, request)
	if err != nil {
		return HandlerResult{}, err
	}
	keepTrajectory = true
	keepLoop = true
	return result, nil
}

func (c *Component) ensureResponseLoop(
	ctx context.Context, response agentic.AgentResponse,
) (agentic.LoopEntity, string, error) {
	mappedLoopID := c.findLoopIDForRequest(response.RequestID)
	var (
		entity agentic.LoopEntity
		loopID string
		cold   bool
		err    error
	)
	if mappedLoopID != "" {
		if derivedLoopID, err := loopIDFromRequestID(response.RequestID); err == nil && derivedLoopID != mappedLoopID {
			return agentic.LoopEntity{}, "", errs.WrapFatal(
				fmt.Errorf("request %q maps to loop %q but encodes loop %q", response.RequestID, mappedLoopID, derivedLoopID),
				"agentic-loop", "ensureResponseLoop", "response correlation conflict",
			)
		}
		loopID = mappedLoopID
		entity, err = c.handler.GetLoop(loopID)
		if err != nil {
			return agentic.LoopEntity{}, "", err
		}
	} else {
		loopID, err = loopIDFromRequestID(response.RequestID)
		if err != nil {
			return agentic.LoopEntity{}, "", errs.WrapFatal(
				err, "agentic-loop", "ensureResponseLoop", "response correlation conflict",
			)
		}
		var found bool
		entity, found, err = c.readLoopEntity(ctx, loopID)
		if err != nil {
			return agentic.LoopEntity{}, "", err
		}
		if !found {
			return agentic.LoopEntity{}, "", fmt.Errorf("loop %q is not yet observable", loopID)
		}
		cold = true
	}

	// The exact current retained request is the response authority on warm and
	// cold deliveries alike. A process mapping only routes the delivery; it
	// does not prove that a historical or fabricated RequestID is current.
	request, found, err := c.readRetainedAgentRequest(ctx, loopID)
	if err != nil {
		return agentic.LoopEntity{}, "", err
	}
	if !found {
		return agentic.LoopEntity{}, "", fmt.Errorf("request for loop %q is not yet observable", loopID)
	}
	if request.RequestID != response.RequestID {
		return agentic.LoopEntity{}, "", errs.WrapFatal(
			fmt.Errorf("retained request %q conflicts with response request %q", request.RequestID, response.RequestID),
			"agentic-loop", "ensureResponseLoop", "response correlation conflict",
		)
	}
	if request.Role != entity.Role || request.Model != entity.Model {
		return agentic.LoopEntity{}, "", errs.WrapFatal(
			fmt.Errorf("retained request %q conflicts with loop role/model", request.RequestID),
			"agentic-loop", "ensureResponseLoop", "response correlation conflict",
		)
	}
	if !cold {
		return entity, loopID, nil
	}
	if err = c.handler.loopManager.restoreLoopFromRequest(entity, request); err != nil {
		return agentic.LoopEntity{}, "", errs.WrapFatal(
			err, "agentic-loop", "ensureResponseLoop", "restore response correlation",
		)
	}
	if _, err := c.handler.trajectoryManager.getTrajectory(loopID); err != nil {
		if _, startErr := c.handler.trajectoryManager.startTrajectory(loopID); startErr != nil {
			return agentic.LoopEntity{}, "", fmt.Errorf("restore trajectory for loop %q: %w", loopID, startErr)
		}
	}
	return entity, loopID, nil
}

func (c *Component) validateColdToolResult(
	ctx context.Context, result agentic.ToolResult,
) error {
	if result.RequestID == "" || result.ExecutionID == "" || result.CallOrdinal == 0 {
		return errs.WrapFatal(
			fmt.Errorf("tool result requires request_id, execution_id, and positive call_ordinal"),
			"agentic-loop", "validateColdToolResult", "tool correlation conflict",
		)
	}
	requestLoopID, err := loopIDFromRequestID(result.RequestID)
	if err != nil || (result.LoopID != "" && requestLoopID != result.LoopID) {
		return errs.WrapFatal(
			fmt.Errorf("tool result loop %q and request %q conflict", result.LoopID, result.RequestID),
			"agentic-loop", "validateColdToolResult", "tool correlation conflict",
		)
	}
	result.LoopID = requestLoopID
	entity, found, err := c.readLoopEntity(ctx, result.LoopID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("loop %q is not yet observable", result.LoopID)
	}
	response, found, err := c.readRetainedAgentResponse(ctx, result.RequestID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("originating response %q is not yet observable", result.RequestID)
	}
	if response.Status != agentic.StatusToolCall {
		return errs.WrapFatal(
			fmt.Errorf("originating response %q has status %q, not tool_call", result.RequestID, response.Status),
			"agentic-loop", "validateColdToolResult", "tool correlation conflict",
		)
	}
	calls := append([]agentic.ToolCall(nil), response.Message.ToolCalls...)
	if err := stampToolExecutionCorrelation(response.RequestID, calls); err != nil {
		return errs.WrapFatal(err, "agentic-loop", "validateColdToolResult", "stamp originating execution identity")
	}
	for _, call := range calls {
		if call.ExecutionID != result.ExecutionID {
			continue
		}
		if call.ID != result.CallID || call.CallOrdinal != result.CallOrdinal || call.Name != result.Name {
			return errs.WrapFatal(
				fmt.Errorf("execution %q conflicts with retained call correlation", result.ExecutionID),
				"agentic-loop", "validateColdToolResult", "tool correlation conflict",
			)
		}
		if entity.State.IsTerminal() {
			return fmt.Errorf("tool result %q is durably correlated but terminal state is not execution-specific applied proof; task 5 owns that proof", result.ExecutionID)
		}
		return fmt.Errorf("tool result %q is durably correlated; ordered batch recovery belongs to task 5", result.ExecutionID)
	}
	return errs.WrapFatal(
		fmt.Errorf("execution %q is absent from originating response %q", result.ExecutionID, result.RequestID),
		"agentic-loop", "validateColdToolResult", "tool correlation conflict",
	)
}

func loopSettlementDecision(err error) natsclient.DeliveryDecision {
	switch {
	case errs.IsFatal(err):
		return natsclient.DeliveryDecisionQuarantine
	case errs.IsInvalid(err):
		return natsclient.DeliveryDecisionTerminate
	default:
		return natsclient.DeliveryDecisionRetry
	}
}
