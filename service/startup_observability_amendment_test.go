package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/storage/storeregistry"
)

func TestStartupMetricWriterInitializesExactlyNinePrivateSeries(t *testing.T) {
	registry := metric.NewMetricsRegistry()
	writer, err := newStartupMetricWriter(
		registry,
		func() serviceStartupCounts { return serviceStartupCounts{Admitted: 3} },
		func() startupUnitCounts {
			return startupUnitCounts{Admitted: 4, LifecycleParticipants: 2}
		},
	)
	require.NoError(t, err)
	require.NotNil(t, writer)

	family := requireMetricFamily(t, registry, "semstreams_startup_units")
	require.Len(t, family.Metric, 9)
	require.Equal(t, float64(3), requireGauge(t, registry, "semstreams_startup_units", map[string]string{
		"owner": "services", "stage": "admitted",
	}))
	require.Equal(t, float64(4), requireGauge(t, registry, "semstreams_startup_units", map[string]string{
		"owner": "components", "stage": "admitted",
	}))
	require.Equal(t, float64(2), requireGauge(t, registry, "semstreams_startup_units", map[string]string{
		"owner": "components", "stage": "lifecycle_participants",
	}))
}

func TestStartupMetricWriterRejectsForeignCollectorInsteadOfAdoptingIt(t *testing.T) {
	for _, labels := range [][]string{{"owner", "stage"}, {"owner", "stage", "unit"}} {
		t.Run(labels[len(labels)-1], func(t *testing.T) {
			registry := metric.NewMetricsRegistry()
			foreign := prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: "semstreams", Subsystem: "startup", Name: "units",
				Help: "Process-local startup unit counts by manager owner and fixed lifecycle stage",
			}, labels)
			require.NoError(t, registry.PrometheusRegistry().Register(foreign))

			writer, err := newStartupMetricWriter(
				registry,
				func() serviceStartupCounts { return serviceStartupCounts{} },
				func() startupUnitCounts { return startupUnitCounts{} },
			)
			require.Error(t, err)
			require.Nil(t, writer)
		})
	}
}

func TestMetricCoreExportsNoStartupLifecycleWriter(t *testing.T) {
	metricsType := reflect.TypeOf((*metric.Metrics)(nil))
	_, exists := metricsType.MethodByName("RecordStartupUnits")
	require.False(t, exists)
}

func TestStartupGaugeVecHasOnlyPrivateServicePackageOwner(t *testing.T) {
	for _, path := range []string{"../metric/core.go", "../metric/registry.go"} {
		source, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NotContains(t, string(source), "RecordStartupUnits")
		require.NotContains(t, string(source), "startupUnits")
	}

	paths, err := filepath.Glob("*.go")
	require.NoError(t, err)
	holders := make([]string, 0, 1)
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		source, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		if strings.Contains(string(source), "units             *prometheus.GaugeVec") {
			holders = append(holders, path)
		}
	}
	require.Equal(t, []string{"startup_metrics.go"}, holders)
}

func TestStartupMetricRegistrationConflictFailsBeforeListenersOrChildren(t *testing.T) {
	for _, labels := range [][]string{{"owner", "stage"}, {"owner", "stage", "unit"}} {
		t.Run(labels[len(labels)-1], func(t *testing.T) {
			registry := metric.NewMetricsRegistry()
			foreign := prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: "semstreams", Subsystem: "startup", Name: "units",
				Help: "Process-local startup unit counts by manager owner and fixed lifecycle stage",
			}, labels)
			require.NoError(t, registry.PrometheusRegistry().Register(foreign))
			manager := createTestServiceManager(ManagerConfig{}, &Dependencies{
				Logger: slog.Default(), MetricsRegistry: registry,
			})
			componentManager := newGatedStartupService("component-manager", nil)
			require.NoError(t, manager.RegisterInstance("component-manager", componentManager))

			err := manager.StartAll(t.Context())
			require.Error(t, err)
			require.Contains(t, err.Error(), "register fresh startup metric collector")
			require.Zero(t, componentManager.starts.Load())
			manager.mu.RLock()
			require.False(t, manager.httpUsed)
			require.Nil(t, manager.httpListener)
			manager.mu.RUnlock()
		})
	}
}

func TestPreparedComponentIsNotInvokedUntilLaunchBoundary(t *testing.T) {
	gated := newBarrierTestComponent("prepared")
	gated.entered = make(chan struct{})
	gated.release = make(chan struct{})
	cm := newBarrierTestManager(t, gated)
	prepared := cm.prepareComponentsPhase(t.Context(), []string{"prepared"})
	require.Len(t, prepared, 1)
	require.Zero(t, cm.startupSnapshot().StartsInvoked)

	done := make(chan error, 1)
	go func() { done <- cm.launchComponentsPhase(prepared) }()
	select {
	case <-gated.entered:
	case <-t.Context().Done():
		t.Fatal("prepared component did not enter Start")
	}
	require.Equal(t, 1, cm.startupSnapshot().StartsInvoked)
	close(gated.release)
	require.NoError(t, <-done)
}

func TestReadinessRequiresCommitAndClearsBeforeChildStop(t *testing.T) {
	stopEntered := make(chan struct{})
	stopRelease := make(chan struct{})
	componentManager := &stopGatedStartupService{
		MockService: MockService{name: "component-manager", status: StatusRunning, healthy: true},
		entered:     stopEntered,
		release:     stopRelease,
	}
	manager := createTestServiceManager(ManagerConfig{}, nil)
	require.NoError(t, manager.RegisterInstance("component-manager", componentManager))
	services, err := manager.sealComposition()
	require.NoError(t, err)
	for _, admitted := range services {
		manager.recordServiceStartInvoked(admitted.name)
		manager.recordServiceStartCompleted(admitted.name, nil)
	}

	notCommitted := httptest.NewRecorder()
	manager.handleReadiness(notCommitted, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusServiceUnavailable, notCommitted.Code)
	require.Equal(t, "NOT READY", notCommitted.Body.String())

	fullMux := http.NewServeMux()
	fullMux.HandleFunc("/committed", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	manager.commitStartup(fullMux)
	ready := httptest.NewRecorder()
	manager.handleReadiness(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusOK, ready.Code)

	stopDone := make(chan error, 1)
	go func() { stopDone <- manager.StopAll(t.Context()) }()
	select {
	case <-stopEntered:
	case <-t.Context().Done():
		t.Fatal("child Stop was not invoked")
	}
	stopping := httptest.NewRecorder()
	manager.handleReadiness(stopping, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusServiceUnavailable, stopping.Code)
	require.Equal(t, "NOT READY", stopping.Body.String())
	close(stopRelease)
	require.NoError(t, <-stopDone)
}

func TestPreparedFullMuxStaysInvisibleUntilCommit(t *testing.T) {
	service := &gatedHTTPStartupService{
		MockService: MockService{name: "component-manager", status: StatusRunning, healthy: true},
		registered:  make(chan struct{}),
		release:     make(chan struct{}),
	}
	manager := createTestServiceManager(ManagerConfig{}, nil)
	require.NoError(t, manager.RegisterInstance("component-manager", service))
	services, err := manager.sealComposition()
	require.NoError(t, err)
	for _, admitted := range services {
		manager.recordServiceStartInvoked(admitted.name)
		manager.recordServiceStartCompleted(admitted.name, nil)
	}
	require.NoError(t, manager.initializeHTTPInfrastructure())
	manager.mu.Lock()
	manager.httpUsed = true
	manager.httpServer = &http.Server{}
	manager.mu.Unlock()
	handler := manager.buildHTTPHandler()

	prepared := make(chan *http.ServeMux, 1)
	prepareErr := make(chan error, 1)
	go func() {
		mux, buildErr := manager.prepareCompleteHTTPMux(t.Context())
		prepared <- mux
		prepareErr <- buildErr
	}()
	<-service.registered
	assertStartupOnly(t, manager, handler)
	close(service.release)
	fullMux := <-prepared
	require.NoError(t, <-prepareErr)
	assertStartupOnly(t, manager, handler)

	manager.commitStartup(fullMux)
	ready := httptest.NewRecorder()
	manager.handleReadiness(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusOK, ready.Code)
	full := httptest.NewRecorder()
	handler.ServeHTTP(full, httptest.NewRequest(http.MethodGet, "/components/committed", nil))
	require.Equal(t, http.StatusNoContent, full.Code)
}

func TestHealthPublisherFailureNeverCommitsPreparedRoutes(t *testing.T) {
	service := &gatedHTTPStartupService{
		MockService: MockService{name: "component-manager", status: StatusRunning, healthy: true},
		registered:  make(chan struct{}),
		release:     make(chan struct{}),
	}
	close(service.release)
	manager := createTestServiceManager(ManagerConfig{}, nil)
	require.NoError(t, manager.RegisterInstance("component-manager", service))
	services, err := manager.sealComposition()
	require.NoError(t, err)
	for _, admitted := range services {
		manager.recordServiceStartInvoked(admitted.name)
		manager.recordServiceStartCompleted(admitted.name, nil)
	}
	require.NoError(t, manager.initializeHTTPInfrastructure())
	manager.mu.Lock()
	manager.httpUsed = true
	manager.httpServer = &http.Server{}
	manager.healthPublisherUsed = true
	manager.mu.Unlock()
	fullMux, err := manager.prepareCompleteHTTPMux(t.Context())
	require.NoError(t, err)
	require.NotNil(t, fullMux)
	require.Error(t, manager.startHealthPublisher(t.Context()))
	assertStartupOnly(t, manager, manager.buildHTTPHandler())
}

func TestComponentMetricPublicationRemainsMonotonicUnderReverseCompletion(t *testing.T) {
	first := newBarrierTestComponent("first")
	first.entered, first.release = make(chan struct{}), make(chan struct{})
	second := newBarrierTestComponent("second")
	second.entered, second.release = make(chan struct{}), make(chan struct{})
	second.startErr = errors.New("second failed")
	cm := newBarrierTestManager(t, first, second)
	registry := metric.NewMetricsRegistry()
	writer, err := newStartupMetricWriter(
		registry,
		func() serviceStartupCounts { return serviceStartupCounts{} },
		cm.startupSnapshot,
	)
	require.NoError(t, err)
	cm.setStartupMetricWriter(writer)

	done := make(chan error, 1)
	go func() { done <- cm.Start(t.Context()) }()
	<-first.entered
	<-second.entered
	cm.mu.RLock()
	secondDone := cm.runtimes["second"].startDone
	cm.mu.RUnlock()
	require.Equal(t, float64(2), requireGauge(t, registry, "semstreams_startup_units", map[string]string{
		"owner": "components", "stage": "starts_invoked",
	}))
	close(second.release)
	<-secondDone
	require.Equal(t, float64(1), requireGauge(t, registry, "semstreams_startup_units", map[string]string{
		"owner": "components", "stage": "starts_completed",
	}))
	require.Equal(t, float64(1), requireGauge(t, registry, "semstreams_startup_units", map[string]string{
		"owner": "components", "stage": "starts_failed",
	}))
	close(first.release)
	require.Error(t, <-done)
	require.Equal(t, float64(2), requireGauge(t, registry, "semstreams_startup_units", map[string]string{
		"owner": "components", "stage": "starts_completed",
	}))
	require.Equal(t, float64(1), requireGauge(t, registry, "semstreams_startup_units", map[string]string{
		"owner": "components", "stage": "starts_failed",
	}))
	require.NoError(t, cm.Stop(t.Context()))
}

func TestManagerPrebindPublishesConcreteComponentCountsBeforeComponentManagerStart(t *testing.T) {
	httpPort := freePort(t)
	metricsPort := freePort(t)
	registry := metric.NewMetricsRegistry()
	deps := &Dependencies{Logger: slog.Default(), MetricsRegistry: registry}
	manager := createTestServiceManager(ManagerConfig{HTTPPort: httpPort}, deps)
	firstRelease := make(chan struct{})
	first := newGatedStartupService("first", firstRelease)
	componentRelease := make(chan struct{})
	lifecycleComponent := newBarrierTestComponent("lifecycle")
	lifecycleComponent.entered = make(chan struct{})
	lifecycleComponent.release = componentRelease
	plain := newStartupDiscoverable()
	cm := &ComponentManager{
		BaseService:   NewBaseServiceWithOptions("component-manager", nil),
		components:    make(map[string]*component.ManagedComponent),
		registry:      component.NewRegistry(),
		storeRegistry: storeregistry.New(),
		storeProvided: make(map[string][]string),
	}
	cm.components["lifecycle"] = &component.ManagedComponent{
		Component: lifecycleComponent, State: component.StateInitialized,
	}
	cm.components["plain"] = &component.ManagedComponent{Component: plain, State: component.StateCreated}
	cm.initialized.Store(true)
	metricsService, err := NewMetrics(
		json.RawMessage(fmt.Sprintf(`{"port":%d,"path":"/metrics"}`, metricsPort)), deps,
	)
	require.NoError(t, err)
	require.NoError(t, manager.RegisterInstance("first", first))
	require.NoError(t, manager.RegisterInstance("component-manager", cm))
	require.NoError(t, manager.RegisterInstance("metrics", metricsService))

	startDone := make(chan error, 1)
	go func() { startDone <- manager.StartAll(t.Context()) }()
	<-first.entered
	response, err := (&http.Client{}).Get(fmt.Sprintf("http://127.0.0.1:%d/metrics", metricsPort))
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	text := string(body)
	require.Contains(t, text, `semstreams_startup_units{owner="components",stage="admitted"} 2`)
	require.Contains(t, text, `semstreams_startup_units{owner="components",stage="lifecycle_participants"} 1`)
	require.Contains(t, text, `semstreams_startup_units{owner="components",stage="starts_invoked"} 0`)
	require.Contains(t, text, `semstreams_startup_units{owner="components",stage="starts_completed"} 0`)

	close(firstRelease)
	<-lifecycleComponent.entered
	close(componentRelease)
	require.NoError(t, <-startDone)
	require.NoError(t, manager.StopAll(t.Context()))
}

func TestConcreteMetricsLifecycleRetainsRegistrationAndReverseStopOrder(t *testing.T) {
	httpPort := freePort(t)
	metricsPort := freePort(t)
	registry := metric.NewMetricsRegistry()
	deps := &Dependencies{Logger: slog.Default(), MetricsRegistry: registry}
	manager := createTestServiceManager(ManagerConfig{HTTPPort: httpPort}, deps)
	componentManager := newOrderGateService("component-manager")
	later := newOrderGateService("later")
	metricsServiceValue, err := NewMetrics(
		json.RawMessage(fmt.Sprintf(`{"port":%d,"path":"/metrics"}`, metricsPort)), deps,
	)
	require.NoError(t, err)
	metricsService := metricsServiceValue.(*Metrics)
	require.NoError(t, manager.RegisterInstance("component-manager", componentManager))
	require.NoError(t, manager.RegisterInstance("metrics", metricsService))
	require.NoError(t, manager.RegisterInstance("later", later))

	startDone := make(chan error, 1)
	go func() { startDone <- manager.StartAll(t.Context()) }()
	<-componentManager.startEntered
	metricsService.lifecycleMu.Lock()
	require.False(t, metricsService.used)
	metricsService.lifecycleMu.Unlock()
	select {
	case <-later.startEntered:
		t.Fatal("later service started before first registered service released")
	default:
	}
	close(componentManager.startRelease)
	<-later.startEntered
	metricsService.lifecycleMu.Lock()
	require.True(t, metricsService.running)
	metricsService.lifecycleMu.Unlock()
	close(later.startRelease)
	require.NoError(t, <-startDone)

	stopDone := make(chan error, 1)
	go func() { stopDone <- manager.StopAll(t.Context()) }()
	<-later.stopEntered
	metricsService.lifecycleMu.Lock()
	require.False(t, metricsService.terminal)
	metricsService.lifecycleMu.Unlock()
	select {
	case <-componentManager.stopEntered:
		t.Fatal("component-manager stopped before later service released")
	default:
	}
	close(later.stopRelease)
	<-componentManager.stopEntered
	metricsService.lifecycleMu.Lock()
	require.True(t, metricsService.terminal)
	metricsService.lifecycleMu.Unlock()
	close(componentManager.stopRelease)
	require.NoError(t, <-stopDone)
}

type gatedHTTPStartupService struct {
	MockService
	registered chan struct{}
	release    chan struct{}
	once       sync.Once
}

type orderGateService struct {
	MockService
	startEntered chan struct{}
	startRelease chan struct{}
	stopEntered  chan struct{}
	stopRelease  chan struct{}
}

func newOrderGateService(name string) *orderGateService {
	return &orderGateService{
		MockService:  MockService{name: name, status: StatusRunning, healthy: true},
		startEntered: make(chan struct{}),
		startRelease: make(chan struct{}),
		stopEntered:  make(chan struct{}),
		stopRelease:  make(chan struct{}),
	}
}

func (s *orderGateService) Start(context.Context) error {
	close(s.startEntered)
	<-s.startRelease
	return nil
}

func (s *orderGateService) Stop(context.Context) error {
	close(s.stopEntered)
	<-s.stopRelease
	return nil
}

func (s *gatedHTTPStartupService) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	s.once.Do(func() { close(s.registered) })
	<-s.release
	mux.HandleFunc(prefix+"/committed", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}

func (*gatedHTTPStartupService) OpenAPISpec() *OpenAPISpec { return nil }

func assertStartupOnly(t *testing.T, manager *Manager, handler http.Handler) {
	t.Helper()
	ready := httptest.NewRecorder()
	manager.handleReadiness(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusServiceUnavailable, ready.Code)
	require.Equal(t, "NOT READY", ready.Body.String())
	full := httptest.NewRecorder()
	handler.ServeHTTP(full, httptest.NewRequest(http.MethodGet, "/components/committed", nil))
	require.Equal(t, http.StatusServiceUnavailable, full.Code)
	require.Equal(t, "NOT READY", full.Body.String())
}

type stopGatedStartupService struct {
	MockService
	entered chan struct{}
	release chan struct{}
}

func requireMetricFamily(t *testing.T, registry *metric.MetricsRegistry, name string) *dto.MetricFamily {
	t.Helper()
	families, err := registry.PrometheusRegistry().Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() == name {
			return family
		}
	}
	t.Fatalf("metric family %s not found", name)
	return nil
}

func (s *stopGatedStartupService) Stop(context.Context) error {
	close(s.entered)
	<-s.release
	return nil
}
