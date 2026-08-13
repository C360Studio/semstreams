package agenticdispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/agentterminal"
	"github.com/c360studio/semstreams/message"
	"github.com/nats-io/nats.go/jetstream"
)

const terminalResponseIDPrefix = "terminal-user-response:"

type permanentTerminalError struct{ err error }

func (e *permanentTerminalError) Error() string { return e.err.Error() }
func (e *permanentTerminalError) Unwrap() error { return e.err }

func permanentTerminal(format string, args ...any) error {
	return &permanentTerminalError{err: fmt.Errorf(format, args...)}
}

func isPermanentTerminal(err error) bool {
	var target *permanentTerminalError
	return errors.As(err, &target)
}

type terminalRoute struct {
	ChannelType string
	ChannelID   string
	UserID      string
}

func mergeRouteField(name string, values ...string) (string, error) {
	var merged string
	for _, value := range values {
		if value == "" {
			continue
		}
		if merged == "" {
			merged = value
			continue
		}
		if merged != value {
			return "", permanentTerminal("conflicting nonempty %s values", name)
		}
	}
	return merged, nil
}

func reconcileTerminalRoute(tracker *LoopInfo, event agentterminal.Event, persisted *agentic.LoopEntity) (terminalRoute, error) {
	var trackerRoute, persistedRoute terminalRoute
	if tracker != nil {
		trackerRoute = terminalRoute{ChannelType: tracker.ChannelType, ChannelID: tracker.ChannelID, UserID: tracker.UserID}
	}
	if persisted != nil {
		persistedRoute = terminalRoute{ChannelType: persisted.ChannelType, ChannelID: persisted.ChannelID, UserID: persisted.UserID}
	}

	channelType, err := mergeRouteField("channel_type", trackerRoute.ChannelType, event.ChannelType, persistedRoute.ChannelType)
	if err != nil {
		return terminalRoute{}, err
	}
	channelID, err := mergeRouteField("channel_id", trackerRoute.ChannelID, event.ChannelID, persistedRoute.ChannelID)
	if err != nil {
		return terminalRoute{}, err
	}
	userID, err := mergeRouteField("user_id", trackerRoute.UserID, event.UserID, persistedRoute.UserID)
	if err != nil {
		return terminalRoute{}, err
	}
	if (channelType == "") != (channelID == "") {
		return terminalRoute{}, permanentTerminal("malformed partial terminal route")
	}
	return terminalRoute{ChannelType: channelType, ChannelID: channelID, UserID: userID}, nil
}

func (c *Component) loadPersistedLoop(ctx context.Context, loopID string) (*agentic.LoopEntity, error) {
	if c.loadPersistedLoopFn != nil {
		return c.loadPersistedLoopFn(ctx, loopID)
	}
	if c.natsClient == nil {
		return nil, fmt.Errorf("AGENT_LOOPS client unavailable")
	}
	kv, err := c.natsClient.GetKeyValueBucket(ctx, agentLoopsBucket)
	if err != nil {
		return nil, fmt.Errorf("access AGENT_LOOPS: %w", err)
	}
	entry, err := kv.Get(ctx, loopID)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) || errors.Is(err, jetstream.ErrKeyDeleted) {
			return nil, fmt.Errorf("loop state %q not yet observable: %w", loopID, err)
		}
		return nil, fmt.Errorf("read AGENT_LOOPS/%s: %w", loopID, err)
	}
	var persisted agentic.LoopEntity
	if err := json.Unmarshal(entry.Value(), &persisted); err != nil {
		return nil, permanentTerminal("malformed AGENT_LOOPS/%s: %w", loopID, err)
	}
	if persisted.ID != loopID {
		return nil, permanentTerminal("AGENT_LOOPS/%s contains loop id %q", loopID, persisted.ID)
	}
	return &persisted, nil
}

func terminalResponse(event agentterminal.Event, route terminalRoute) agentic.UserResponse {
	responseType := agentic.ResponseTypeStatus
	content := fmt.Sprintf("Loop %s cancelled.", event.LoopID)
	switch event.Class {
	case agentterminal.ClassSucceeded:
		responseType = agentic.ResponseTypeResult
		content = event.Result
		if content == "" {
			content = fmt.Sprintf("Loop %s completed.", event.LoopID)
		}
	case agentterminal.ClassFailed:
		responseType = agentic.ResponseTypeError
		content = fmt.Sprintf("Loop %s failed: %s", event.LoopID, event.Error)
	}
	return agentic.UserResponse{
		ResponseID:  terminalResponseIDPrefix + event.SourceMessageID,
		ChannelType: route.ChannelType,
		ChannelID:   route.ChannelID,
		UserID:      route.UserID,
		InReplyTo:   event.LoopID,
		Type:        responseType,
		Content:     content,
		Timestamp:   event.TerminalAt,
	}
}

func (c *Component) publishTerminalResponse(ctx context.Context, response agentic.UserResponse, msgID string) error {
	if c.sendTerminalResponseFn != nil {
		return c.sendTerminalResponseFn(ctx, response, msgID)
	}
	responseMessage := message.NewBaseMessage(response.Schema(), &response, "agentic-dispatch")
	data, err := json.Marshal(responseMessage)
	if err != nil {
		return permanentTerminal("marshal terminal response: %w", err)
	}
	subject, err := component.ResolveSubject(c.outputPortDefs(), "user.response", response.ChannelType+"."+response.ChannelID)
	if err != nil {
		return permanentTerminal("resolve terminal response subject: %w", err)
	}
	if err := c.natsClient.PublishToStreamWithMsgID(ctx, subject, data, msgID); err != nil {
		return fmt.Errorf("publish terminal response: %w", err)
	}
	return nil
}

func (c *Component) settleAgentTerminal(ctx context.Context, data []byte) (settleErr error) {
	reason := ""
	defer func() { c.metrics.recordTerminalSettlement(reason) }()

	event, err := agentterminal.Decode(c.decoder, data)
	if err != nil {
		reason = string(agentterminal.ErrorReason(err))
		return permanentTerminal("normalize terminal: %w", err)
	}

	tracker := c.loopTracker.getSnapshot(event.LoopID)
	persisted, err := c.loadPersistedLoop(ctx, event.LoopID)
	if err != nil {
		if isPermanentTerminal(err) {
			reason = "routing_malformed"
		} else {
			reason = "routing_read_transient"
		}
		return err
	}
	route, err := reconcileTerminalRoute(tracker, event, persisted)
	if err != nil {
		reason = "routing_collision_or_malformed"
		return err
	}

	trackerChanged := false
	if tracker != nil {
		trackerChanged, err = c.loopTracker.updateCompletionAt(event.LoopID, event.Outcome, event.Result, event.Error, event.TerminalAt)
		if err != nil {
			reason = "tracker_projection_collision"
			return permanentTerminal("project tracker terminal: %w", err)
		}
	}
	if trackerChanged {
		c.metrics.recordLoopEnded()
	}
	if route.ChannelType == "" {
		c.metrics.recordCompletionReceived(event.Outcome)
		reason = "route_less_settled"
		return nil
	}

	response := terminalResponse(event, route)
	if err := c.publishTerminalResponse(ctx, response, response.ResponseID); err != nil {
		if isPermanentTerminal(err) {
			reason = "routing_malformed"
			return err
		}
		reason = "response_publish_transient"
		return err
	}
	c.metrics.recordCompletionReceived(event.Outcome)
	reason = "response_settled"
	return nil
}
