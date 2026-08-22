package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFlowServiceStopWaitsForOverrideReporterCompletion(t *testing.T) {
	fs := newRunningFlowServiceLifecycleTest(t)
	reporterStarted := make(chan struct{})
	reporterCanceled := make(chan struct{})
	releaseReporter := make(chan struct{})
	reporterCompleted := make(chan struct{})
	fs.startOverrideExpiryReporter(context.Background(), func(ctx context.Context) {
		close(reporterStarted)
		<-ctx.Done()
		close(reporterCanceled)
		<-releaseReporter
		close(reporterCompleted)
	})
	reporterCancel := fs.overrideExpiryCancel
	reporterDone := fs.overrideExpiryDone
	var releaseOnce sync.Once
	releaseAndJoinReporter := func() {
		reporterCancel()
		releaseOnce.Do(func() { close(releaseReporter) })
		<-reporterDone
	}
	t.Cleanup(releaseAndJoinReporter)
	<-reporterStarted
	baseDone := fs.BaseService.done

	var stopWG sync.WaitGroup
	stopWG.Add(1)
	stopDone := make(chan struct{})
	var stopErr error
	go func() {
		defer stopWG.Done()
		defer close(stopDone)
		stopErr = fs.Stop(context.Background())
	}()
	joinStop := func() {
		<-stopDone
		stopWG.Wait()
	}
	t.Cleanup(func() {
		releaseAndJoinReporter()
		joinStop()
	})

	<-reporterCanceled
	<-baseDone
	select {
	case <-stopDone:
		joinStop()
		t.Fatalf("Stop returned before reporter completion: %v", stopErr)
	default:
	}

	releaseAndJoinReporter()
	joinStop()
	require.NoError(t, stopErr)

	select {
	case <-reporterCompleted:
	default:
		t.Fatal("successful Stop did not observe reporter completion")
	}
}

func TestFlowServiceStopDeadlineReportsUnfinishedOverrideReporter(t *testing.T) {
	fs := newRunningFlowServiceLifecycleTest(t)
	reporterStarted := make(chan struct{})
	reporterCanceled := make(chan struct{})
	releaseReporter := make(chan struct{})
	reporterCompleted := make(chan struct{})
	fs.startOverrideExpiryReporter(context.Background(), func(ctx context.Context) {
		defer close(reporterCompleted)
		close(reporterStarted)
		<-ctx.Done()
		close(reporterCanceled)
		<-releaseReporter
	})
	reporterCancel := fs.overrideExpiryCancel
	reporterDone := fs.overrideExpiryDone
	var releaseOnce sync.Once
	releaseAndJoinReporter := func() {
		reporterCancel()
		releaseOnce.Do(func() { close(releaseReporter) })
		<-reporterDone
	}
	t.Cleanup(releaseAndJoinReporter)
	<-reporterStarted
	baseDone := fs.BaseService.done

	stopCtx, stopCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stopCancel()
	err := fs.Stop(stopCtx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t,
		"wait for BaseService runtime: context deadline exceeded\n"+
			"wait for stream override expiry reporter: context deadline exceeded",
		err.Error(),
	)

	<-reporterCanceled
	releaseAndJoinReporter()
	<-reporterCompleted
	<-baseDone
	require.NoError(t, fs.Stop(context.Background()), "completed failed Stop must remain one-shot")
}

func TestFlowServiceFailedStartDoesNotLaunchOverrideReporter(t *testing.T) {
	base := NewBaseServiceWithOptions("flow-service-start-failure", nil, WithHealthInterval(0))
	require.NoError(t, base.Stop(context.Background()))
	fs := &FlowService{BaseService: base}

	err := fs.Start(context.Background())
	require.Error(t, err)
	require.Nil(t, fs.overrideExpiryCancel)
	require.Nil(t, fs.overrideExpiryDone)
}

func newRunningFlowServiceLifecycleTest(t *testing.T) *FlowService {
	t.Helper()
	base := NewBaseServiceWithOptions("flow-service-lifecycle", nil, WithHealthInterval(0))
	runtimeCtx, runtimeCancel := context.WithCancel(context.Background())
	require.NoError(t, base.Start(runtimeCtx))
	baseDone := base.done
	t.Cleanup(func() {
		runtimeCancel()
		<-baseDone
	})
	return &FlowService{BaseService: base}
}
