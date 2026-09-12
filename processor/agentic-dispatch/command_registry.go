// Package agenticdispatch provides message routing between users and agentic loops.
// It handles command parsing, permission checking, current ownership lookup, and message dispatch.
package agenticdispatch

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sync"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// CommandConfig defines a command's configuration
type CommandConfig struct {
	Pattern     string `json:"pattern"`      // Regex pattern to match
	Permission  string `json:"permission"`   // Required permission
	RequireLoop bool   `json:"require_loop"` // Requires an active loop
	Help        string `json:"help"`         // Help text
}

// CommandHandler is a function that handles a command
type CommandHandler func(ctx context.Context, msg agentic.UserMessage, args []string, loopID string) (agentic.UserResponse, error)

// CommandContext provides services to command executors
type CommandContext struct {
	NATSClient      *natsclient.Client
	LookupLoopOwner LoopOwnerLookup
	Logger          *slog.Logger
	HasPermission   func(userID, permission string) bool
}

// LoopOwner is the current ownership answer for one canonical loop ID.
type LoopOwner struct {
	LoopID string
	UserID string
}

// LoopOwnerLookup reads current ownership. Refusals use classified codes
// invalid_loop_id, loop_not_found, loop_owner_absent, loop_record_invalid,
// or loop_state_unavailable. It never exposes a store or mutable projection.
type LoopOwnerLookup func(context.Context, string) (LoopOwner, error)

// CommandExecutor is the interface for command implementations
type CommandExecutor interface {
	Execute(ctx context.Context, cmdCtx *CommandContext, msg agentic.UserMessage, args []string, loopID string) (agentic.UserResponse, error)
	Config() CommandConfig
}

// RegisteredCommand contains a command's config and handler
type RegisteredCommand struct {
	Name    string
	Config  CommandConfig
	Pattern *regexp.Regexp
	Handler CommandHandler
}

// CommandRegistry manages command registration and matching
type CommandRegistry struct {
	mu       sync.RWMutex
	commands map[string]*RegisteredCommand
}

// NewCommandRegistry creates a new CommandRegistry
func NewCommandRegistry() *CommandRegistry {
	return &CommandRegistry{
		commands: make(map[string]*RegisteredCommand),
	}
}

// Register adds a command to the registry
func (r *CommandRegistry) Register(name string, config CommandConfig, handler CommandHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.commands[name]; exists {
		return errs.WrapInvalid(fmt.Errorf("command %s already registered", name), "CommandRegistry", "Register", "check duplicate command")
	}

	pattern, err := regexp.Compile(config.Pattern)
	if err != nil {
		return errs.WrapInvalid(err, "CommandRegistry", "Register", fmt.Sprintf("compile pattern for command %s", name))
	}

	r.commands[name] = &RegisteredCommand{
		Name:    name,
		Config:  config,
		Pattern: pattern,
		Handler: handler,
	}

	return nil
}

// Match finds a command matching the input
// Returns the command name, matched command, captured groups, and whether a match was found
func (r *CommandRegistry) Match(input string) (string, *RegisteredCommand, []string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for name, cmd := range r.commands {
		matches := cmd.Pattern.FindStringSubmatch(input)
		if matches != nil {
			// Return captured groups (excluding the full match)
			args := []string{}
			if len(matches) > 1 {
				args = matches[1:]
			}
			return name, cmd, args, true
		}
	}

	return "", nil, nil, false
}

// Get retrieves a registered command by name
func (r *CommandRegistry) Get(name string) (*RegisteredCommand, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cmd, ok := r.commands[name]
	return cmd, ok
}

// All returns all registered commands
func (r *CommandRegistry) All() map[string]CommandConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]CommandConfig, len(r.commands))
	for name, cmd := range r.commands {
		result[name] = cmd.Config
	}
	return result
}

// Count returns the number of registered commands
func (r *CommandRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.commands)
}
