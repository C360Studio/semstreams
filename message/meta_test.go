package message

import (
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestDefaultMeta_ImplementsInterface(_ *testing.T) {
	// Test that DefaultMeta implements Meta interface
	var _ Meta = (*DefaultMeta)(nil)
}

func TestDefaultMeta_CreatedAt(t *testing.T) {
	// Test CreatedAt returns the creation time (with millisecond precision)
	created := time.Now().Add(-1 * time.Hour)
	meta := NewDefaultMeta(created, "test-source")
	// Timestamps lose nanosecond precision due to millisecond storage
	assert.WithinDuration(t, created, meta.CreatedAt(), time.Millisecond)
}

func TestDefaultMeta_ReceivedAt(t *testing.T) {
	// Test ReceivedAt is set to approximately now (within millisecond precision)
	meta := NewDefaultMeta(time.Now(), "test-source")

	received := meta.ReceivedAt()
	assert.WithinDuration(t, time.Now(), received, 100*time.Millisecond)
}

func TestDefaultMeta_Source(t *testing.T) {
	// Test Source returns the provided source
	meta := NewDefaultMeta(time.Now(), "sensor.gps")
	assert.Equal(t, "sensor.gps", meta.Source())
}

func TestNewDefaultMeta(t *testing.T) {
	// Test constructor creates valid instance
	created := time.Now().Add(-30 * time.Minute)
	source := "robotics.mavlink"

	meta := NewDefaultMeta(created, source)

	assert.NotNil(t, meta)
	// Timestamps lose nanosecond precision due to millisecond storage
	assert.WithinDuration(t, created, meta.CreatedAt(), time.Millisecond)
	assert.Equal(t, source, meta.Source())
	assert.WithinDuration(t, time.Now(), meta.ReceivedAt(), 100*time.Millisecond)
}

func TestNewDefaultMetaWithReceivedAt(t *testing.T) {
	// Test creating meta with explicit received time
	created := time.Now().Add(-1 * time.Hour)
	received := time.Now().Add(-30 * time.Minute)
	source := "ocean.drifter"

	meta := NewDefaultMetaWithReceivedAt(created, received, source)

	// Timestamps lose nanosecond precision due to millisecond storage
	assert.WithinDuration(t, created, meta.CreatedAt(), time.Millisecond)
	assert.WithinDuration(t, received, meta.ReceivedAt(), time.Millisecond)
	assert.Equal(t, source, meta.Source())
}
