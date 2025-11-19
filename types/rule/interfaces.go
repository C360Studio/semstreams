// Package rule provides interfaces for rule processing
package rule

import (
	"github.com/c360/semstreams/message"
	gtypes "github.com/c360/semstreams/types/graph"
)

// Rule interface defines the event-based contract for processing rules
// Rules return GraphEvents instead of using publisher pattern
type Rule interface {
	Name() string
	Subscribe() []string
	Evaluate(messages []message.Message) bool
	ExecuteEvents(messages []message.Message) ([]gtypes.Event, error)
}

