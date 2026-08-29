package natsclient

import (
	"context"
	"errors"
	"go/ast"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

func TestDeliveryDecisionConstants(t *testing.T) {
	require.Equal(t, DeliveryDecision(0), DeliveryDecisionInvalid)
	require.Equal(t, DeliveryDecision(1), DeliveryDecisionAck)
	require.Equal(t, DeliveryDecision(2), DeliveryDecisionRetry)
	require.Equal(t, DeliveryDecision(3), DeliveryDecisionTerminate)
	require.Equal(t, DeliveryDecision(4), DeliveryDecisionQuarantine)
	var work DeliveryWork = func(context.Context, DeliveryAttempt, []byte) (DeliveryDecision, error) {
		return DeliveryDecisionAck, nil
	}
	decision, err := work(t.Context(), DeliveryAttempt{}, nil)
	require.NoError(t, err)
	require.Equal(t, DeliveryDecisionAck, decision)
}

func TestDeliveryAttemptReportsFirstAndRedeliveredAttempts(t *testing.T) {
	zero := DeliveryAttempt{}
	first := DeliveryAttempt{number: 1}
	retry := DeliveryAttempt{number: 2}

	require.Equal(t, uint64(0), zero.Number())
	require.False(t, zero.MetadataAvailable())
	require.False(t, zero.IsRedelivery())
	require.Equal(t, uint64(1), first.Number())
	require.True(t, first.MetadataAvailable())
	require.False(t, first.IsRedelivery())
	require.Equal(t, uint64(2), retry.Number())
	require.True(t, retry.MetadataAvailable())
	require.True(t, retry.IsRedelivery())
}

func TestDeliveryAttemptHasOpaqueValueShapeAndNoFactory(t *testing.T) {
	attemptType := reflect.TypeOf(DeliveryAttempt{})
	require.Equal(t, reflect.Struct, attemptType.Kind())
	require.Equal(t, 1, attemptType.NumField())
	field := attemptType.Field(0)
	require.Equal(t, "number", field.Name)
	require.False(t, field.IsExported())
	require.Equal(t, reflect.Uint64, field.Type.Kind())

	original := DeliveryAttempt{number: 1}
	copyOfOriginal := original
	copyOfOriginal.number = 2
	require.Equal(t, uint64(1), original.Number(), "attempt copies must not share mutable state")
	require.Equal(t, uint64(2), copyOfOriginal.Number())

	for _, parsed := range parseProductionGoFiles(t, ".") {
		for _, declaration := range parsed.file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if functionReturnsDeliveryAttempt(function) {
				t.Fatalf("DeliveryAttempt constructor/factory is forbidden: %s:%s", parsed.rel, function.Name.Name)
			}
			if deliveryAttemptReceiverIsPointer(function) {
				t.Fatalf("DeliveryAttempt method must use value semantics: %s:%s", parsed.rel, function.Name.Name)
			}
		}
	}
}

func functionReturnsDeliveryAttempt(function *ast.FuncDecl) bool {
	if function.Type.Results == nil {
		return false
	}
	for _, result := range function.Type.Results.List {
		found := false
		ast.Inspect(result.Type, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && identifier.Name == "DeliveryAttempt" {
				found = true
			}
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

func deliveryAttemptReceiverIsPointer(function *ast.FuncDecl) bool {
	if function.Recv == nil || len(function.Recv.List) != 1 {
		return false
	}
	pointer, ok := function.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	receiver, ok := pointer.X.(*ast.Ident)
	return ok && receiver.Name == "DeliveryAttempt"
}

func TestValidateHeartbeatDeliveryPolicy(t *testing.T) {
	work := func(context.Context, DeliveryAttempt, []byte) (DeliveryDecision, error) {
		return DeliveryDecisionAck, nil
	}
	immediate := ImmediateDeliveryRetry()
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	tests := []struct {
		name      string
		ctx       context.Context
		cfg       StreamConsumerConfig
		heartbeat time.Duration
		retry     DeliveryRetryPolicy
		work      DeliveryWork
		wantErr   string
	}{
		{name: "ackwait equality", ctx: t.Context(), cfg: StreamConsumerConfig{AckWait: 30 * time.Second}, heartbeat: 15 * time.Second, retry: immediate, work: work},
		{name: "default equality", ctx: t.Context(), cfg: StreamConsumerConfig{}, heartbeat: 15 * time.Second, retry: immediate, work: work},
		{name: "shortest backoff", ctx: t.Context(), cfg: StreamConsumerConfig{AckWait: time.Hour, BackOff: []time.Duration{20 * time.Second, 5 * time.Second}}, heartbeat: 2500 * time.Millisecond, retry: immediate, work: work},
		{name: "nil context", cfg: StreamConsumerConfig{}, heartbeat: time.Second, retry: immediate, work: work, wantErr: "context"},
		{name: "ended context", ctx: cancelled, cfg: StreamConsumerConfig{}, heartbeat: time.Second, retry: immediate, work: work, wantErr: "ended"},
		{name: "nil work", ctx: t.Context(), cfg: StreamConsumerConfig{}, heartbeat: time.Second, retry: immediate, wantErr: "work"},
		{name: "zero retry", ctx: t.Context(), cfg: StreamConsumerConfig{}, heartbeat: time.Second, work: work, wantErr: "retry"},
		{name: "zero heartbeat", ctx: t.Context(), cfg: StreamConsumerConfig{}, retry: immediate, work: work, wantErr: "positive"},
		{name: "negative ackwait", ctx: t.Context(), cfg: StreamConsumerConfig{AckWait: -time.Second}, heartbeat: time.Second, retry: immediate, work: work, wantErr: "ack wait"},
		{name: "zero backoff", ctx: t.Context(), cfg: StreamConsumerConfig{BackOff: []time.Duration{time.Second, 0}}, heartbeat: time.Millisecond, retry: immediate, work: work, wantErr: "back_off[1]"},
		{name: "above half", ctx: t.Context(), cfg: StreamConsumerConfig{AckWait: 30 * time.Second}, heartbeat: 15*time.Second + 1, retry: immediate, work: work, wantErr: "ceiling"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidateHeartbeatDeliveryPolicy(tt.ctx, tt.cfg, tt.heartbeat, tt.retry, tt.work)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestHeartbeatDeliveryPolicyDefensivelyCopiesBackOff(t *testing.T) {
	cfg := StreamConsumerConfig{BackOff: []time.Duration{10 * time.Second}}
	policy, err := ValidateHeartbeatDeliveryPolicy(t.Context(), cfg, 5*time.Second,
		ImmediateDeliveryRetry(), func(context.Context, DeliveryAttempt, []byte) (DeliveryDecision, error) {
			return DeliveryDecisionAck, nil
		})
	require.NoError(t, err)
	cfg.BackOff[0] = time.Nanosecond
	require.Equal(t, []time.Duration{10 * time.Second}, policy.backOff)
	msg := &mockMsg{subject: "copy"}
	result := ConsumeDeliveryWithHeartbeat(t.Context(), msg, policy)
	require.NoError(t, result.Err())
	require.Equal(t, int32(1), msg.ackCount.Load())
}

func TestHeartbeatDeliveryPolicyReusesWorkWithCurrentPayload(t *testing.T) {
	bodies := make(chan []byte, 2)
	policy, err := ValidateHeartbeatDeliveryPolicy(t.Context(), StreamConsumerConfig{}, time.Second,
		ImmediateDeliveryRetry(), func(_ context.Context, _ DeliveryAttempt, data []byte) (DeliveryDecision, error) {
			bodies <- append([]byte(nil), data...)
			return DeliveryDecisionAck, nil
		})
	require.NoError(t, err)

	first := &mockMsg{subject: "first", data: []byte("one")}
	second := &mockMsg{subject: "second", data: []byte("two")}
	require.NoError(t, ConsumeDeliveryWithHeartbeat(t.Context(), first, policy).Err())
	require.NoError(t, ConsumeDeliveryWithHeartbeat(t.Context(), second, policy).Err())
	require.Equal(t, []byte("one"), <-bodies)
	require.Equal(t, []byte("two"), <-bodies)
	require.Equal(t, int32(1), first.dataCount.Load())
	require.Equal(t, int32(1), second.dataCount.Load())
}

func TestConsumeDeliveryWithHeartbeatObservesAttemptBeforePayloadAndWork(t *testing.T) {
	tests := []struct {
		name       string
		number     uint64
		redelivery bool
	}{
		{name: "first", number: 1},
		{name: "second", number: 2, redelivery: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var order atomic.Int64
			var workOrder atomic.Int64
			var observed DeliveryAttempt
			var observedData []byte
			msg := &mockMsg{
				subject: "attempt", data: []byte("body"), order: &order,
				metadata: &jetstream.MsgMetadata{NumDelivered: tt.number},
			}
			policy, err := ValidateHeartbeatDeliveryPolicy(t.Context(), StreamConsumerConfig{}, time.Second,
				ImmediateDeliveryRetry(), func(_ context.Context, attempt DeliveryAttempt, data []byte) (DeliveryDecision, error) {
					observed = attempt
					observedData = data
					workOrder.Store(order.Add(1))
					return DeliveryDecisionAck, nil
				})
			require.NoError(t, err)

			result := ConsumeDeliveryWithHeartbeat(t.Context(), msg, policy)

			require.NoError(t, result.Err())
			require.Equal(t, tt.number, observed.Number())
			require.Equal(t, []byte("body"), observedData)
			require.True(t, observed.MetadataAvailable())
			require.Equal(t, tt.redelivery, observed.IsRedelivery())
			require.Equal(t, int32(1), msg.metadataCount.Load())
			require.Equal(t, int32(1), msg.dataCount.Load())
			require.Equal(t, int64(1), msg.metadataOrder.Load())
			require.Equal(t, int64(2), msg.dataOrder.Load())
			require.Equal(t, int64(3), workOrder.Load())
		})
	}
}

func TestConsumeDeliveryWithHeartbeatMetadataFailureFailsClosedBeforeWork(t *testing.T) {
	transportErr := errors.New("metadata transport")
	tests := []struct {
		name      string
		configure func(*mockMsg)
		wantCause error
	}{
		{name: "error", configure: func(msg *mockMsg) { msg.metadataErr = transportErr }, wantCause: transportErr},
		{name: "nil", configure: func(msg *mockMsg) { msg.metadataNil = true }},
		{name: "zero", configure: func(msg *mockMsg) { msg.metadata = &jetstream.MsgMetadata{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &mockMsg{subject: "metadata"}
			tt.configure(msg)
			var workCalls atomic.Int32
			policy, err := ValidateHeartbeatDeliveryPolicy(t.Context(), StreamConsumerConfig{}, time.Millisecond,
				ImmediateDeliveryRetry(), func(context.Context, DeliveryAttempt, []byte) (DeliveryDecision, error) {
					workCalls.Add(1)
					return DeliveryDecisionAck, nil
				})
			require.NoError(t, err)

			result := ConsumeDeliveryWithHeartbeat(t.Context(), msg, policy)

			require.Equal(t, DeliveryDecisionQuarantine, result.Decision())
			require.True(t, result.Quarantined())
			require.True(t, result.OwnerStopRequired())
			require.False(t, result.SettlementAttempted())
			require.Equal(t, int32(1), msg.metadataCount.Load())
			require.Zero(t, msg.dataCount.Load())
			require.Zero(t, workCalls.Load())
			require.Zero(t, msg.inProgressCount.Load())
			require.Zero(t, msg.ackCount.Load()+msg.nakCount.Load()+msg.termCount.Load())
			var metadataErr *DeliveryMetadataUnavailableError
			require.ErrorAs(t, result.Cause(), &metadataErr)
			require.ErrorContains(t, result.Cause(), "delivery_metadata_unavailable")
			if tt.wantCause != nil {
				require.ErrorIs(t, result.Cause(), tt.wantCause)
			}
		})
	}
}

func TestConsumeDeliveryWithHeartbeatPassesNilPayloadOnce(t *testing.T) {
	var observed []byte
	policy, err := ValidateHeartbeatDeliveryPolicy(t.Context(), StreamConsumerConfig{}, time.Second,
		ImmediateDeliveryRetry(), func(_ context.Context, _ DeliveryAttempt, data []byte) (DeliveryDecision, error) {
			observed = data
			return DeliveryDecisionAck, nil
		})
	require.NoError(t, err)
	msg := &mockMsg{subject: "nil"}
	require.NoError(t, ConsumeDeliveryWithHeartbeat(t.Context(), msg, policy).Err())
	require.Nil(t, observed)
	require.Equal(t, int32(1), msg.dataCount.Load())
}

func TestDelayedDeliveryRetryValidation(t *testing.T) {
	_, err := DelayedDeliveryRetry(0)
	require.Error(t, err)
	_, err = DelayedDeliveryRetry(-time.Second)
	require.Error(t, err)
	_, err = DelayedDeliveryRetry(30 * time.Second)
	require.NoError(t, err)
}

func TestImmediateDeliveryRetryUsesPlainNak(t *testing.T) {
	cause := errors.New("retry")
	policy, err := ValidateHeartbeatDeliveryPolicy(t.Context(), StreamConsumerConfig{}, time.Second,
		ImmediateDeliveryRetry(), func(context.Context, DeliveryAttempt, []byte) (DeliveryDecision, error) {
			return DeliveryDecisionRetry, cause
		})
	require.NoError(t, err)
	msg := &mockMsg{subject: "immediate"}
	result := ConsumeDeliveryWithHeartbeat(t.Context(), msg, policy)
	require.ErrorIs(t, result.Err(), cause)
	require.Equal(t, int32(1), msg.nakCount.Load())
	require.Zero(t, msg.nakDelay.Load())

	settlementErr := errors.New("nak unknown")
	failed := &mockMsg{subject: "immediate-failed", nakErr: settlementErr}
	failedResult := ConsumeDeliveryWithHeartbeat(t.Context(), failed, policy)
	require.True(t, failedResult.SettlementMethodFailed())
	require.ErrorIs(t, failedResult.SettlementError(), settlementErr)
	require.ErrorIs(t, failedResult.Err(), cause)
	require.ErrorIs(t, failedResult.Err(), settlementErr)
	require.False(t, failedResult.OwnerStopRequired())
}

func TestConsumeDeliveryWithHeartbeatValidDecisionTruthTable(t *testing.T) {
	cause := errors.New("semantic")
	settlementErr := errors.New("settlement")
	delayed, err := DelayedDeliveryRetry(30 * time.Second)
	require.NoError(t, err)
	tests := []struct {
		name       string
		decision   DeliveryDecision
		cause      error
		configure  func(*mockMsg)
		attempted  bool
		succeeded  bool
		failed     bool
		quarantine bool
		stop       bool
		wantErr    error
	}{
		{name: "ack", decision: DeliveryDecisionAck, attempted: true, succeeded: true},
		{name: "ack method failure", decision: DeliveryDecisionAck, configure: func(m *mockMsg) { m.ackErr = settlementErr }, attempted: true, failed: true, wantErr: settlementErr},
		{name: "retry", decision: DeliveryDecisionRetry, cause: cause, attempted: true, succeeded: true, wantErr: cause},
		{name: "retry method failure", decision: DeliveryDecisionRetry, cause: cause, configure: func(m *mockMsg) { m.nakErr = settlementErr }, attempted: true, failed: true, wantErr: settlementErr},
		{name: "terminate", decision: DeliveryDecisionTerminate, cause: cause, attempted: true, succeeded: true, wantErr: cause},
		{name: "terminate method failure", decision: DeliveryDecisionTerminate, cause: cause, configure: func(m *mockMsg) { m.termErr = settlementErr }, attempted: true, failed: true, wantErr: settlementErr},
		{name: "quarantine", decision: DeliveryDecisionQuarantine, cause: cause, quarantine: true, stop: true, wantErr: cause},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &mockMsg{subject: "typed"}
			if tt.configure != nil {
				tt.configure(msg)
			}
			policy, policyErr := ValidateHeartbeatDeliveryPolicy(t.Context(), StreamConsumerConfig{}, time.Second, delayed,
				func(context.Context, DeliveryAttempt, []byte) (DeliveryDecision, error) { return tt.decision, tt.cause })
			require.NoError(t, policyErr)
			result := ConsumeDeliveryWithHeartbeat(t.Context(), msg, policy)
			require.Equal(t, tt.decision, result.Decision())
			require.ErrorIs(t, result.Cause(), tt.cause)
			require.Equal(t, tt.attempted, result.SettlementAttempted())
			require.Equal(t, tt.succeeded, result.SettlementMethodSucceeded())
			require.Equal(t, tt.failed, result.SettlementMethodFailed())
			require.False(t, result.ServerConfirmed())
			require.Equal(t, tt.quarantine, result.Quarantined())
			require.Equal(t, tt.stop, result.OwnerStopRequired())
			if tt.wantErr == nil {
				require.NoError(t, result.Err())
			} else {
				require.ErrorIs(t, result.Err(), tt.wantErr)
			}
		})
	}
}

func TestConsumeDeliveryWithHeartbeatInvalidDecisionTuplesFailClosed(t *testing.T) {
	supplied := errors.New("supplied")
	tests := []struct {
		name     string
		decision DeliveryDecision
		cause    error
	}{
		{name: "invalid nil", decision: DeliveryDecisionInvalid},
		{name: "invalid error", decision: DeliveryDecisionInvalid, cause: supplied},
		{name: "ack error", decision: DeliveryDecisionAck, cause: supplied},
		{name: "retry nil", decision: DeliveryDecisionRetry},
		{name: "terminate nil", decision: DeliveryDecisionTerminate},
		{name: "quarantine nil", decision: DeliveryDecisionQuarantine},
		{name: "unknown nil", decision: DeliveryDecision(200)},
		{name: "unknown error", decision: DeliveryDecision(200), cause: supplied},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &mockMsg{subject: "invalid"}
			policy, err := ValidateHeartbeatDeliveryPolicy(t.Context(), StreamConsumerConfig{}, time.Second,
				ImmediateDeliveryRetry(), func(context.Context, DeliveryAttempt, []byte) (DeliveryDecision, error) { return tt.decision, tt.cause })
			require.NoError(t, err)
			result := ConsumeDeliveryWithHeartbeat(t.Context(), msg, policy)
			require.Equal(t, tt.decision, result.Decision())
			var invalid *InvalidDeliveryDecisionError
			require.ErrorAs(t, result.Cause(), &invalid)
			if tt.cause != nil {
				require.ErrorIs(t, result.Cause(), tt.cause)
			}
			require.True(t, result.Quarantined())
			require.True(t, result.OwnerStopRequired())
			require.False(t, result.SettlementAttempted())
			require.Zero(t, msg.ackCount.Load()+msg.nakCount.Load()+msg.termCount.Load())
		})
	}
}

func TestConsumeDeliveryWithHeartbeatControlLossPreservesJoinedMeaning(t *testing.T) {
	controlErr := errors.New("heartbeat transport")
	cause := errors.New("retry after cleanup")
	msg := &mockMsg{subject: "typed", inProgressErr: controlErr}
	policy, err := ValidateHeartbeatDeliveryPolicy(t.Context(), StreamConsumerConfig{}, time.Millisecond,
		ImmediateDeliveryRetry(), func(ctx context.Context, _ DeliveryAttempt, _ []byte) (DeliveryDecision, error) {
			<-ctx.Done()
			return DeliveryDecisionRetry, cause
		})
	require.NoError(t, err)

	result := ConsumeDeliveryWithHeartbeat(t.Context(), msg, policy)
	require.Equal(t, DeliveryDecisionRetry, result.Decision())
	require.ErrorIs(t, result.Cause(), cause)
	require.ErrorIs(t, result.ControlError(), controlErr)
	require.True(t, result.OwnerStopRequired())
	require.False(t, result.SettlementAttempted())
	require.Zero(t, msg.ackCount.Load()+msg.nakCount.Load()+msg.termCount.Load())
	require.Equal(t, int32(1), msg.dataCount.Load())
}

func TestConsumeDeliveryWithHeartbeatOwnerCancellationJoinsThenSettles(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	entered := make(chan struct{})
	policy, err := ValidateHeartbeatDeliveryPolicy(ctx, StreamConsumerConfig{}, time.Second,
		ImmediateDeliveryRetry(), func(workCtx context.Context, _ DeliveryAttempt, _ []byte) (DeliveryDecision, error) {
			close(entered)
			<-workCtx.Done()
			return DeliveryDecisionAck, nil
		})
	require.NoError(t, err)
	msg := &mockMsg{subject: "cancel"}
	returned := make(chan DeliveryResult, 1)
	go func() { returned <- ConsumeDeliveryWithHeartbeat(ctx, msg, policy) }()
	<-entered
	cancel()
	result := <-returned
	require.Equal(t, DeliveryDecisionAck, result.Decision())
	require.NoError(t, result.Err())
	require.Equal(t, int32(1), msg.ackCount.Load())
}

func TestConsumeDeliveryWithHeartbeatControlLossNormalizesInvalidAndPanic(t *testing.T) {
	controlErr := errors.New("heartbeat transport")
	tests := []struct {
		name         string
		work         DeliveryWork
		wantDecision DeliveryDecision
		assertCause  func(*testing.T, error)
	}{
		{
			name: "invalid tuple", wantDecision: DeliveryDecisionAck,
			work: func(ctx context.Context, _ DeliveryAttempt, _ []byte) (DeliveryDecision, error) {
				<-ctx.Done()
				return DeliveryDecisionAck, errors.New("ack cannot carry cause")
			},
			assertCause: func(t *testing.T, err error) {
				var target *InvalidDeliveryDecisionError
				require.ErrorAs(t, err, &target)
			},
		},
		{
			name: "panic", wantDecision: DeliveryDecisionQuarantine,
			work: func(ctx context.Context, _ DeliveryAttempt, _ []byte) (DeliveryDecision, error) {
				<-ctx.Done()
				panic("cleanup panic")
			},
			assertCause: func(t *testing.T, err error) {
				var target *DeliveryWorkPanicError
				require.ErrorAs(t, err, &target)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &mockMsg{subject: tt.name, data: []byte("body"), inProgressErr: controlErr}
			policy, err := ValidateHeartbeatDeliveryPolicy(t.Context(), StreamConsumerConfig{}, time.Millisecond,
				ImmediateDeliveryRetry(), tt.work)
			require.NoError(t, err)
			result := ConsumeDeliveryWithHeartbeat(t.Context(), msg, policy)
			require.Equal(t, tt.wantDecision, result.Decision())
			tt.assertCause(t, result.Cause())
			require.ErrorIs(t, result.ControlError(), controlErr)
			require.True(t, result.Quarantined())
			require.True(t, result.OwnerStopRequired())
			require.False(t, result.SettlementAttempted())
			require.Equal(t, int32(1), msg.dataCount.Load())
		})
	}
}

func TestConsumeDeliveryWithHeartbeatPanicAndZeroPolicyFailClosed(t *testing.T) {
	t.Run("panic", func(t *testing.T) {
		msg := &mockMsg{subject: "panic"}
		policy, err := ValidateHeartbeatDeliveryPolicy(t.Context(), StreamConsumerConfig{}, time.Second,
			ImmediateDeliveryRetry(), func(context.Context, DeliveryAttempt, []byte) (DeliveryDecision, error) { panic("boom") })
		require.NoError(t, err)
		result := ConsumeDeliveryWithHeartbeat(t.Context(), msg, policy)
		require.Equal(t, DeliveryDecisionQuarantine, result.Decision())
		var panicErr *DeliveryWorkPanicError
		require.ErrorAs(t, result.Cause(), &panicErr)
		require.True(t, result.OwnerStopRequired())
		require.Zero(t, msg.ackCount.Load()+msg.nakCount.Load()+msg.termCount.Load())
	})

	t.Run("zero policy before message IO", func(t *testing.T) {
		msg := &mockMsg{subject: "zero"}
		result := ConsumeDeliveryWithHeartbeat(t.Context(), msg, HeartbeatDeliveryPolicy{})
		require.Equal(t, DeliveryDecisionInvalid, result.Decision())
		require.True(t, result.OwnerStopRequired())
		require.Zero(t, msg.inProgressCount.Load()+msg.ackCount.Load()+msg.nakCount.Load()+msg.termCount.Load())
		require.Zero(t, msg.metadataCount.Load())
		require.Zero(t, msg.dataCount.Load())
	})
}
