package udp

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/buffer"
	"github.com/c360studio/semstreams/pkg/retry"
	"github.com/stretchr/testify/require"
)

type blockingCloseBuffer struct {
	buffer.Buffer[[]byte]
	closeEntered chan struct{}
	releaseClose chan struct{}
	closeCalls   atomic.Int32
}

type observedAfterFuncContext struct {
	done            chan struct{}
	afterRegistered chan struct{}
	stopCalled      chan struct{}
	registerOnce    sync.Once
	stopOnce        sync.Once
}

func (c *observedAfterFuncContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *observedAfterFuncContext) Done() <-chan struct{}       { return c.done }
func (c *observedAfterFuncContext) Err() error                  { return nil }
func (c *observedAfterFuncContext) Value(any) any               { return nil }

func (c *observedAfterFuncContext) AfterFunc(func()) func() bool {
	c.registerOnce.Do(func() { close(c.afterRegistered) })
	return func() bool {
		c.stopOnce.Do(func() { close(c.stopCalled) })
		return true
	}
}

func (b *blockingCloseBuffer) Close() error {
	if b.closeCalls.Add(1) == 1 {
		close(b.closeEntered)
	}
	<-b.releaseClose
	return b.Buffer.Close()
}

// createTestComponent creates a test instance for lifecycle testing.
func createTestComponent() component.LifecycleComponent {
	return createLifecycleInput()
}

func createLifecycleInput() *Input {
	// Find an available port for testing
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		panic("failed to reserve lifecycle UDP port: " + err.Error())
	}
	port := conn.LocalAddr().(*net.UDPAddr).Port
	if err := conn.Close(); err != nil {
		panic("failed to release lifecycle UDP port: " + err.Error())
	}

	mockClient := &natsclient.Client{}
	deps := InputDeps{
		Config:          testUDPConfig(port, "127.0.0.1", "test.subject"),
		NATSClient:      mockClient,
		MetricsRegistry: nil,
		Logger:          nil,
	}

	input, err := NewInput(deps)
	if err != nil {
		panic("failed to create test component: " + err.Error())
	}

	return input
}

// TestUDPInput_ComprehensiveLifecycle runs the complete lifecycle test suite
func TestUDPInput_ComprehensiveLifecycle(t *testing.T) {
	component.StandardLifecycleTests(t, createTestComponent)
}

func TestUDPInput_ControlledStopWithLiveParentFinalizesOnce(t *testing.T) {
	input := createLifecycleInput()
	closeReleased := make(chan struct{})
	close(closeReleased)
	countingBuffer := &blockingCloseBuffer{
		Buffer:       input.buffer,
		closeEntered: make(chan struct{}),
		releaseClose: closeReleased,
	}
	input.buffer = countingBuffer

	require.NoError(t, input.Initialize())
	startCtx, cancelStart := context.WithCancel(context.Background())
	defer cancelStart()
	require.NoError(t, input.Start(startCtx))
	require.True(t, input.Health().Healthy)

	stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Second)
	require.NoError(t, input.Stop(stopCtx))
	cancelStop()
	require.NoError(t, startCtx.Err(), "controlled Stop must not cancel its parent Start context")
	input.wg.Wait()

	require.False(t, input.running.Load())
	input.mu.RLock()
	require.Nil(t, input.conn)
	require.False(t, input.socketOpen)
	input.mu.RUnlock()
	require.False(t, input.Health().Healthy)
	require.EqualValues(t, 1, countingBuffer.closeCalls.Load())

	repeatCtx, cancelRepeat := context.WithTimeout(context.Background(), time.Second)
	require.NoError(t, input.Stop(repeatCtx))
	cancelRepeat()
	require.EqualValues(t, 1, countingBuffer.closeCalls.Load(), "completed repeat must not teardown again")
}

func TestUDPInput_AcceptedStartParentCancellationIsObservable(t *testing.T) {
	input := createLifecycleInput()
	require.NoError(t, input.Initialize())
	startCtx, cancelStart := context.WithCancel(context.Background())
	require.NoError(t, input.Start(startCtx))

	input.mu.RLock()
	completion := input.completion
	input.mu.RUnlock()
	require.NotNil(t, completion)
	cancelStart()

	select {
	case <-completion:
	case <-time.After(time.Second):
		t.Fatal("Start owner did not publish completion after parent cancellation")
	}
	input.wg.Wait()
	require.False(t, input.running.Load())
	require.False(t, input.Health().Healthy)

	stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Second)
	require.NoError(t, input.Stop(stopCtx))
	cancelStop()
}

func TestUDPInput_NaturalOwnerExitReleasesDerivedCancel(t *testing.T) {
	input := createLifecycleInput()
	require.NoError(t, input.Initialize())
	parent := &observedAfterFuncContext{
		done:            make(chan struct{}),
		afterRegistered: make(chan struct{}),
		stopCalled:      make(chan struct{}),
	}
	require.NoError(t, input.Start(parent))

	select {
	case <-parent.afterRegistered:
	case <-time.After(time.Second):
		t.Fatal("Start did not link derived cancellation to its parent")
	}
	input.mu.RLock()
	conn := input.conn
	completion := input.completion
	input.mu.RUnlock()
	require.NotNil(t, conn)
	require.NotNil(t, completion)
	input.running.Store(false)
	require.NoError(t, conn.Close())

	select {
	case <-completion:
	case <-time.After(time.Second):
		t.Fatal("Start owner did not complete after the socket ended")
	}
	select {
	case <-parent.stopCalled:
	case <-time.After(time.Second):
		t.Fatal("Start owner did not release the derived parent-cancellation linkage")
	}
	input.wg.Wait()

	stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Second)
	require.NoError(t, input.Stop(stopCtx))
	cancelStop()
}

func TestUDPInput_StopDoesNotRejoinAfterCallerBoundWins(t *testing.T) {
	input := createTestComponent().(*Input)
	blockingBuffer := &blockingCloseBuffer{
		Buffer:       input.buffer,
		closeEntered: make(chan struct{}),
		releaseClose: make(chan struct{}),
	}
	input.buffer = blockingBuffer

	require.NoError(t, input.Initialize())
	startCtx, cancelStart := context.WithCancel(context.Background())
	t.Cleanup(cancelStart)
	require.NoError(t, input.Start(startCtx))

	input.mu.RLock()
	completion := input.completion
	input.mu.RUnlock()
	require.NotNil(t, completion)

	firstStopCtx, cancelFirstStop := context.WithCancel(context.Background())
	cancelFirstStop()
	require.ErrorIs(t, input.Stop(firstStopCtx), context.Canceled)

	secondStopCtx, cancelSecondStop := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancelSecondStop()
	secondResult := make(chan error, 1)
	go func() {
		secondResult <- input.Stop(secondStopCtx)
	}()

	select {
	case err := <-secondResult:
		require.NoError(t, err, "later Stop must be an immediate no-op")
	case <-secondStopCtx.Done():
		close(blockingBuffer.releaseClose)
		<-secondResult
		t.Fatal("later Stop waited for the running generation")
	}

	select {
	case <-blockingBuffer.closeEntered:
	case <-time.After(time.Second):
		t.Fatal("Start owner did not reach synchronous resource finalization")
	}
	require.False(t, input.Health().Healthy,
		"a closed socket must not remain healthy while owner completion is blocked")

	close(blockingBuffer.releaseClose)
	select {
	case <-completion:
	case <-time.After(time.Second):
		t.Fatal("Start owner did not publish completion after finalization")
	}
	input.wg.Wait()

	require.False(t, input.running.Load())
	input.mu.RLock()
	require.Nil(t, input.conn)
	input.mu.RUnlock()
	require.False(t, input.Health().Healthy)
	require.EqualValues(t, 1, blockingBuffer.closeCalls.Load())
	require.NoError(t, input.Stop(context.Background()))
	require.EqualValues(t, 1, blockingBuffer.closeCalls.Load())
}

func TestUDPInput_FailedBindLeavesNoRuntimeAuthority(t *testing.T) {
	conflict, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	require.NoError(t, err)
	defer conflict.Close()
	port := conflict.LocalAddr().(*net.UDPAddr).Port

	input, err := NewInput(InputDeps{
		Config:     testUDPConfig(port, "127.0.0.1", "test.subject"),
		NATSClient: &natsclient.Client{},
	})
	require.NoError(t, err)
	input.retryConfig = retry.Config{MaxAttempts: 1}
	require.NoError(t, input.Initialize())

	startCtx, cancelStart := context.WithTimeout(context.Background(), time.Second)
	err = input.Start(startCtx)
	cancelStart()
	require.Error(t, err)
	require.False(t, input.running.Load())
	input.mu.RLock()
	require.Nil(t, input.cancel)
	require.Nil(t, input.completion)
	require.Nil(t, input.conn)
	require.False(t, input.socketOpen)
	input.mu.RUnlock()
	input.wg.Wait()
	require.False(t, input.Health().Healthy)
	require.NoError(t, input.Stop(context.Background()))
}
