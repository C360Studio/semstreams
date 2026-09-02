package agenticdispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/google/uuid"
)

// registerBuiltinCommands registers all built-in commands
func (c *Component) registerBuiltinCommands() {
	// /cancel [loop_id] - Cancel a loop
	c.registry.Register("cancel", CommandConfig{
		Pattern:     `^/cancel\s*(\S*)$`,
		Permission:  "cancel_own",
		RequireLoop: false,
		Help:        "/cancel [loop_id] - Cancel current or specified loop",
	}, c.handleCancelCommand)

	// /status [loop_id] - Show loop status
	c.registry.Register("status", CommandConfig{
		Pattern:     `^/status\s*(\S*)$`,
		Permission:  "view",
		RequireLoop: false,
		Help:        "/status [loop_id] - Show loop status",
	}, c.handleStatusCommand)

	// /loops - List active loops
	c.registry.Register("loops", CommandConfig{
		Pattern:     `^/loops$`,
		Permission:  "view",
		RequireLoop: false,
		Help:        "/loops - List your active loops",
	}, c.handleLoopsCommand)

	// /help - Show help
	c.registry.Register("help", CommandConfig{
		Pattern:     `^/help$`,
		Permission:  "",
		RequireLoop: false,
		Help:        "/help - Show available commands",
	}, c.handleHelpCommand)
}

// commandRefusalResponse turns a gate refusal into the chat lane's answer. The
// content is the refusal's own message, which names the field and says what was
// wrong; the previous "Permission denied: cannot cancel this loop" said
// "permission" for a loop that did not exist and for a token that was never a
// loop id.
//
// It never counts anything: the refusal was metered and logged exactly once
// where it was built.
func commandRefusalResponse(msg agentic.UserMessage, refusal error) agentic.UserResponse {
	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeError,
		Content:     refusal.Error(),
		Timestamp:   time.Now(),
	}
}

// loopStatusFromTracker renders the live status line: this process is running
// the loop, so iteration counts and age exist.
func loopStatusFromTracker(info *LoopInfo) string {
	age := time.Since(info.CreatedAt).Truncate(time.Second)
	return fmt.Sprintf("Loop: %s\nState: %s\nIterations: %d/%d\nAge: %s\nUser: %s",
		info.LoopID, info.State, info.Iterations, info.MaxIterations, age, info.UserID)
}

// loopStatusFromFacts renders what /status can say about a loop the gate
// admitted from the durable record alone — this process never tracked it, so
// iteration counts and age do not exist here. Naming the fields it does have
// beats the "Loop %s not found" this seam answered before existence was merged,
// which contradicted the admission that had just succeeded.
func loopStatusFromFacts(facts loopFacts) string {
	state := "running"
	if facts.Terminal {
		state = "settled"
	}
	return fmt.Sprintf("Loop: %s\nState: %s (from the durable record; this process is not running it)\nUser: %s",
		facts.LoopID, state, facts.UserID)
}

// handleCancelCommand handles the /cancel command
func (c *Component) handleCancelCommand(ctx context.Context, msg agentic.UserMessage, args []string, loopID string) (agentic.UserResponse, error) {
	// Use provided loop ID or active loop
	targetLoopID := loopID
	if len(args) > 0 && args[0] != "" {
		targetLoopID = args[0]
	}

	if targetLoopID == "" {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "No loop to cancel. Specify a loop_id or have an active loop.",
			Timestamp:   time.Now(),
		}, nil
	}

	// Form, existence, and ownership in one place. The command's own declared
	// permission (cancel_own) was already checked by the dispatcher before this
	// handler ran and keeps that single home; the gate never consults it
	// (owner ruling R2).
	facts, err := c.admitLoopRequest(ctx, loopAdmissionRequest{
		Seam:      seamCancelCommand,
		Field:     "loop_id",
		Operation: loopOpCancel,
		LoopID:    targetLoopID,
		Requester: msg.UserID,
	})
	if err != nil {
		return commandRefusalResponse(msg, err), nil
	}

	// A settled loop is not a refusal on this operation — cancelling something
	// already finished is a no-op the caller should simply be told about. The
	// gate reports terminality from BOTH sources, so this answers correctly for
	// a loop this process never tracked.
	if facts.Terminal {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeStatus,
			Content:     fmt.Sprintf("Loop %s has already settled", targetLoopID),
			Timestamp:   time.Now(),
		}, nil
	}

	// Send cancel signal
	signal := agentic.UserSignal{
		SignalID:    uuid.New().String(),
		Type:        agentic.SignalCancel,
		LoopID:      targetLoopID,
		UserID:      msg.UserID,
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		Timestamp:   time.Now(),
	}

	signalData, err := json.Marshal(signal)
	if err != nil {
		return agentic.UserResponse{}, errs.Wrap(err, "Component", "handleCancelCommand", "marshal signal")
	}

	subject, err := component.ResolveSubject(c.outputPortDefs(), "agent.signal", targetLoopID)
	if err != nil {
		return agentic.UserResponse{}, errs.WrapInvalid(err, "Component", "handleCancelCommand", "resolve signal subject")
	}
	if err := c.natsClient.Publish(ctx, subject, signalData); err != nil {
		return agentic.UserResponse{}, errs.WrapTransient(err, "Component", "handleCancelCommand", "publish signal")
	}

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		InReplyTo:   targetLoopID,
		Type:        agentic.ResponseTypeStatus,
		Content:     fmt.Sprintf("Cancel signal sent to loop %s", targetLoopID),
		Timestamp:   time.Now(),
	}, nil
}

// handleStatusCommand handles the /status command
func (c *Component) handleStatusCommand(ctx context.Context, msg agentic.UserMessage, args []string, loopID string) (agentic.UserResponse, error) {
	// Use provided loop ID or active loop
	targetLoopID := loopID
	if len(args) > 0 && args[0] != "" {
		targetLoopID = args[0]
	}

	c.logger.DebugContext(ctx, "Status command executed",
		slog.String("target_loop", targetLoopID),
		slog.String("user_id", msg.UserID))

	if targetLoopID == "" {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeStatus,
			Content:     "No active loop. Start a task or specify a loop_id.",
			Timestamp:   time.Now(),
		}, nil
	}

	// Reading is gated for form and existence only — ownership is deliberately
	// not consulted, exactly as GET /loops/{id} is not (the ownership model's
	// read row). Existence is merged, so a loop this process never tracked is
	// still found.
	facts, err := c.admitLoopRequest(ctx, loopAdmissionRequest{
		Seam:      seamStatusCommand,
		Field:     "loop_id",
		Operation: loopOpRead,
		LoopID:    targetLoopID,
		Requester: msg.UserID,
	})
	if err != nil {
		return commandRefusalResponse(msg, err), nil
	}

	content := loopStatusFromFacts(facts)
	if loopInfo := c.loopTracker.Get(targetLoopID); loopInfo != nil {
		content = loopStatusFromTracker(loopInfo)
	}

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		InReplyTo:   targetLoopID,
		Type:        agentic.ResponseTypeStatus,
		Content:     content,
		Timestamp:   time.Now(),
	}, nil
}

// handleLoopsCommand handles the /loops command
func (c *Component) handleLoopsCommand(ctx context.Context, msg agentic.UserMessage, _ []string, _ string) (agentic.UserResponse, error) {
	loops := c.loopTracker.GetUserLoops(msg.UserID)

	c.logger.DebugContext(ctx, "Loops command executed",
		slog.String("user_id", msg.UserID),
		slog.Int("loop_count", len(loops)))

	if len(loops) == 0 {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeStatus,
			Content:     "No active loops.",
			Timestamp:   time.Now(),
		}, nil
	}

	var lines []string
	lines = append(lines, "LOOP         STATE       ITER  AGE")
	for _, loop := range loops {
		age := time.Since(loop.CreatedAt).Truncate(time.Second)
		iter := fmt.Sprintf("%d/%d", loop.Iterations, loop.MaxIterations)
		lines = append(lines, fmt.Sprintf("%-12s %-11s %-5s %s",
			truncateID(loop.LoopID),
			loop.State,
			iter,
			formatAge(age)))
	}

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeText,
		Content:     strings.Join(lines, "\n"),
		Timestamp:   time.Now(),
	}, nil
}

// handleHelpCommand handles the /help command
func (c *Component) handleHelpCommand(ctx context.Context, msg agentic.UserMessage, _ []string, _ string) (agentic.UserResponse, error) {
	commands := c.registry.All()

	c.logger.DebugContext(ctx, "Help command executed",
		slog.String("user_id", msg.UserID),
		slog.Int("command_count", len(commands)))

	var lines []string
	lines = append(lines, "Available commands:")
	lines = append(lines, "")

	for _, config := range commands {
		if config.Permission == "" || c.hasPermission(msg.UserID, config.Permission) {
			lines = append(lines, "  "+config.Help)
		}
	}

	lines = append(lines, "")
	lines = append(lines, "Type any other text to submit a task.")

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeText,
		Content:     strings.Join(lines, "\n"),
		Timestamp:   time.Now(),
	}, nil
}

// Helper functions

func truncateID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func formatAge(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}
