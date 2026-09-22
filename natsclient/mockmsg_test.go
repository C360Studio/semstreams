package natsclient

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// mockMsg is the package's in-memory jetstream.Msg: it records which terminal
// method a settlement contract applied, in what order relative to the metadata
// and payload reads, and can be configured to fail any of them.
//
// It lived in heartbeat_test.go until ConsumeWithHeartbeat was removed
// (#1249/#759); it moved here rather than going with that file because
// delivery_settlement_test.go drives the typed path through it.
type mockMsg struct {
	subject         string
	data            []byte
	dataCount       atomic.Int32
	metadata        *jetstream.MsgMetadata
	metadataErr     error
	metadataNil     bool
	metadataCount   atomic.Int32
	ackCalled       atomic.Bool
	nakCalled       atomic.Bool
	ackCount        atomic.Int32
	nakCount        atomic.Int32
	nakDelay        atomic.Int64 // stored as nanoseconds
	inProgressCount atomic.Int32
	termCalled      atomic.Bool
	termCount       atomic.Int32
	order           *atomic.Int64
	metadataOrder   atomic.Int64
	dataOrder       atomic.Int64
	settlementOrder atomic.Int64

	mu            sync.Mutex
	inProgressErr error
	ackErr        error
	nakErr        error
	termErr       error
}

func (m *mockMsg) Data() []byte {
	m.dataCount.Add(1)
	if m.order != nil {
		m.dataOrder.CompareAndSwap(0, m.order.Add(1))
	}
	return m.data
}
func (m *mockMsg) Subject() string      { return m.subject }
func (m *mockMsg) Reply() string        { return "" }
func (m *mockMsg) Headers() nats.Header { return nil }
func (m *mockMsg) Metadata() (*jetstream.MsgMetadata, error) {
	m.metadataCount.Add(1)
	if m.order != nil {
		m.metadataOrder.CompareAndSwap(0, m.order.Add(1))
	}
	if m.metadataErr != nil {
		return nil, m.metadataErr
	}
	if m.metadataNil {
		return nil, nil
	}
	if m.metadata != nil {
		return m.metadata, nil
	}
	return &jetstream.MsgMetadata{NumDelivered: 1}, nil
}

func (m *mockMsg) Ack() error {
	m.ackCalled.Store(true)
	m.ackCount.Add(1)
	m.recordSettlement()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ackErr
}

func (m *mockMsg) DoubleAck(_ context.Context) error { return nil }

func (m *mockMsg) Nak() error {
	m.nakCalled.Store(true)
	m.nakCount.Add(1)
	m.recordSettlement()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.nakErr
}

func (m *mockMsg) NakWithDelay(delay time.Duration) error {
	m.nakCalled.Store(true)
	m.nakCount.Add(1)
	m.recordSettlement()
	m.nakDelay.Store(int64(delay))
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.nakErr
}

func (m *mockMsg) InProgress() error {
	m.inProgressCount.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.inProgressErr
}

func (m *mockMsg) Term() error {
	m.termCalled.Store(true)
	m.termCount.Add(1)
	m.recordSettlement()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.termErr
}

func (m *mockMsg) TermWithReason(_ string) error {
	m.termCalled.Store(true)
	m.termCount.Add(1)
	m.recordSettlement()
	return nil
}

func (m *mockMsg) recordSettlement() {
	if m.order != nil {
		m.settlementOrder.CompareAndSwap(0, m.order.Add(1))
	}
}
