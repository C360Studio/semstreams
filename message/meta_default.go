package message

import (
	"time"

	"github.com/c360/semstreams/pkg/timestamp"
)

// DefaultMeta provides the standard implementation of the Meta interface.
// It tracks when an event occurred, when it was received by the system,
// and where it originated from.
type DefaultMeta struct {
	createdAt  int64 // Unix milliseconds
	receivedAt int64 // Unix milliseconds
	source     string
}

// NewDefaultMeta creates a new DefaultMeta instance with the given
// creation time and source. The received time is automatically set
// to the current time.
func NewDefaultMeta(createdAt time.Time, source string) *DefaultMeta {
	return &DefaultMeta{
		createdAt:  timestamp.ToUnixMs(createdAt),
		receivedAt: timestamp.Now(),
		source:     source,
	}
}

// NewDefaultMetaWithReceivedAt creates a new DefaultMeta instance with
// explicit creation and received times. This is useful for testing
// or when importing historical data.
func NewDefaultMetaWithReceivedAt(createdAt, receivedAt time.Time, source string) *DefaultMeta {
	return &DefaultMeta{
		createdAt:  timestamp.ToUnixMs(createdAt),
		receivedAt: timestamp.ToUnixMs(receivedAt),
		source:     source,
	}
}

// CreatedAt returns when the original event occurred.
func (m *DefaultMeta) CreatedAt() time.Time {
	return timestamp.ToTime(m.createdAt)
}

// ReceivedAt returns when the system received the message.
func (m *DefaultMeta) ReceivedAt() time.Time {
	return timestamp.ToTime(m.receivedAt)
}

// Source returns the origin of the message.
func (m *DefaultMeta) Source() string {
	return m.source
}
