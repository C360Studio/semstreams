package natsclient

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNewDurableHandlerValidatesEffectiveAckWait(t *testing.T) {
	work := func(context.Context, []byte) error { return nil }
	maxDuration := time.Duration(1<<63 - 1)
	tests := []struct {
		name      string
		cfg       StreamConsumerConfig
		heartbeat time.Duration
		wantErr   bool
		contains  string
	}{
		{name: "ack wait below half", cfg: StreamConsumerConfig{AckWait: 30 * time.Second}, heartbeat: 5 * time.Second},
		{name: "ack wait equality", cfg: StreamConsumerConfig{AckWait: 30 * time.Second}, heartbeat: 15 * time.Second},
		{name: "ack wait one over", cfg: StreamConsumerConfig{AckWait: 30 * time.Second}, heartbeat: 15*time.Second + 1, wantErr: true, contains: "computed ceiling"},
		{name: "default ack wait", cfg: StreamConsumerConfig{}, heartbeat: 15 * time.Second},
		{name: "default one over", cfg: StreamConsumerConfig{}, heartbeat: 15*time.Second + 1, wantErr: true, contains: "computed ceiling"},
		{name: "backoff uses shorter later entry", cfg: StreamConsumerConfig{AckWait: time.Hour, BackOff: []time.Duration{20 * time.Second, 5 * time.Second, 10 * time.Second}}, heartbeat: 2500 * time.Millisecond},
		{name: "backoff shorter later entry rejects", cfg: StreamConsumerConfig{AckWait: time.Hour, BackOff: []time.Duration{20 * time.Second, 5 * time.Second, 10 * time.Second}}, heartbeat: 2500*time.Millisecond + 1, wantErr: true, contains: "computed ceiling"},
		{name: "zero backoff identifies index", cfg: StreamConsumerConfig{BackOff: []time.Duration{time.Second, 0}}, heartbeat: time.Millisecond, wantErr: true, contains: "back_off[1]"},
		{name: "negative backoff identifies index", cfg: StreamConsumerConfig{BackOff: []time.Duration{-time.Second}}, heartbeat: time.Millisecond, wantErr: true, contains: "back_off[0]"},
		{name: "non-positive heartbeat", cfg: StreamConsumerConfig{AckWait: 30 * time.Second}, heartbeat: 0, wantErr: true, contains: "positive"},
		{name: "overflow scale equality", cfg: StreamConsumerConfig{AckWait: maxDuration}, heartbeat: maxDuration / 2},
		{name: "overflow scale one over", cfg: StreamConsumerConfig{AckWait: maxDuration}, heartbeat: maxDuration/2 + 1, wantErr: true, contains: "computed ceiling"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewDurableHandler(tt.cfg, tt.heartbeat, work)
			if tt.wantErr && err == nil {
				t.Fatal("want error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("want nil, got %v", err)
			}
			if tt.contains != "" && !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("error %q does not contain %q", err, tt.contains)
			}
		})
	}
}

func TestNewDurableHandlerRejectsNilWork(t *testing.T) {
	handler, err := NewDurableHandler(StreamConsumerConfig{}, time.Second, nil)
	if err == nil || handler != nil {
		t.Fatalf("nil work returned handler=%t error=%v, want nil handler/error", handler != nil, err)
	}
}

func TestNewDurableHandlerPreservesSettlementAndPayload(t *testing.T) {
	var got []byte
	handler, err := NewDurableHandler(StreamConsumerConfig{}, time.Second, func(_ context.Context, data []byte) error {
		got = append([]byte(nil), data...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	msg := &mockMsg{subject: "durable.input", data: []byte("payload")}
	handler(context.Background(), msg)
	if !reflect.DeepEqual(got, []byte("payload")) || !msg.ackCalled.Load() || msg.nakCalled.Load() {
		t.Fatalf("payload/settlement = (%q, ack=%v, nak=%v)", got, msg.ackCalled.Load(), msg.nakCalled.Load())
	}
}

func TestNewDurableHandlerPreservesOperatorWarning(t *testing.T) {
	logs := &policyLogHandler{}
	previous := slog.Default()
	slog.SetDefault(slog.New(logs))
	defer slog.SetDefault(previous)

	workErr := errors.New("work failed")
	handler, err := NewDurableHandler(
		StreamConsumerConfig{StreamName: "WORK", ConsumerName: "worker"},
		time.Second,
		func(context.Context, []byte) error { return workErr },
	)
	if err != nil {
		t.Fatal(err)
	}
	msg := &mockMsg{subject: "durable.input", data: []byte("payload")}
	handler(context.Background(), msg)
	if !msg.nakCalled.Load() {
		t.Fatal("work error did not preserve NAK settlement")
	}

	logs.mu.Lock()
	defer logs.mu.Unlock()
	if len(logs.records) != 1 {
		t.Fatalf("warning records = %d, want 1", len(logs.records))
	}
	record := logs.records[0]
	if record.message != "ConsumeDurable handler error" {
		t.Fatalf("warning message = %q", record.message)
	}
	want := map[string]any{"stream": "WORK", "consumer": "worker", "error": workErr.Error()}
	if !reflect.DeepEqual(record.attrs, want) {
		t.Fatalf("warning attrs = %#v, want %#v", record.attrs, want)
	}
}
