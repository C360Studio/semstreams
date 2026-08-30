package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/health"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/storage/storeregistry"
	"github.com/c360studio/semstreams/types"
	"github.com/stretchr/testify/require"
)

type gatedStartupService struct {
	name    string
	entered chan struct{}
	release <-chan struct{}
	status  atomic.Int32
	healthy atomic.Bool
	starts  atomic.Int64
	stops   atomic.Int64
}

func newGatedStartupService(name string, release <-chan struct{}) *gatedStartupService {
	s := &gatedStartupService{name: name, entered: make(chan struct{}), release: release}
	s.status.Store(int32(StatusStopped))
	s.healthy.Store(true)
	return s
}

func (s *gatedStartupService) Name() string { return s.name }
func (s *gatedStartupService) Start(ctx context.Context) error {
	s.starts.Add(1)
	s.status.Store(int32(StatusStarting))
	close(s.entered)
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.status.Store(int32(StatusRunning))
	return nil
}
func (s *gatedStartupService) Stop(context.Context) error {
	s.stops.Add(1)
	s.status.Store(int32(StatusStopped))
	return nil
}
func (s *gatedStartupService) Status() Status  { return Status(s.status.Load()) }
func (s *gatedStartupService) IsHealthy() bool { return s.healthy.Load() }
func (s *gatedStartupService) GetStatus() Info { return Info{Name: s.name, Status: s.Status()} }
func (s *gatedStartupService) Health() health.Status {
	if s.IsHealthy() {
		return health.NewHealthy(s.name, "healthy")
	}
	return health.NewUnhealthy(s.name, "unhealthy")
}
func (*gatedStartupService) RegisterMetrics(metric.MetricsRegistrar) error { return nil }

type startupDiscoverable struct{ healthy atomic.Bool }

func newStartupDiscoverable() *startupDiscoverable {
	c := &startupDiscoverable{}
	c.healthy.Store(true)
	return c
}

func (*startupDiscoverable) Meta() component.Metadata {
	return component.Metadata{Name: "plain", Type: "processor", Version: "1.0.0"}
}
func (*startupDiscoverable) InputPorts() []component.Port         { return nil }
func (*startupDiscoverable) OutputPorts() []component.Port        { return nil }
func (*startupDiscoverable) ConfigSchema() component.ConfigSchema { return component.ConfigSchema{} }
func (c *startupDiscoverable) Health() component.HealthStatus {
	return component.HealthStatus{Healthy: c.healthy.Load()}
}
func (*startupDiscoverable) DataFlow() component.FlowMetrics { return component.FlowMetrics{} }

func TestComponentStartupSnapshotDistinguishesAdmittedAndLifecycleStarts(t *testing.T) {
	gated := newBarrierTestComponent("gated")
	gated.entered = make(chan struct{})
	gated.release = make(chan struct{})
	plain := newStartupDiscoverable()
	metricsRegistry := metric.NewMetricsRegistry()
	cm := &ComponentManager{
		BaseService:   NewBaseServiceWithOptions("component-manager", nil, WithMetrics(metricsRegistry)),
		components:    make(map[string]*component.ManagedComponent),
		registry:      component.NewRegistry(),
		storeRegistry: storeregistry.New(),
		storeProvided: make(map[string][]string),
	}
	cm.components["gated"] = &component.ManagedComponent{Component: gated, State: component.StateInitialized}
	cm.components["plain"] = &component.ManagedComponent{Component: plain, State: component.StateCreated}
	cm.initialized.Store(true)
	writer, err := newStartupMetricWriter(
		metricsRegistry,
		func() serviceStartupCounts { return serviceStartupCounts{} },
		cm.startupSnapshot,
	)
	require.NoError(t, err)
	cm.setStartupMetricWriter(writer)

	done := make(chan error, 1)
	go func() { done <- cm.Start(t.Context()) }()
	<-gated.entered

	starting := cm.startupSnapshot()
	require.Equal(t, startupUnitCounts{
		Admitted:              2,
		LifecycleParticipants: 1,
		StartsInvoked:         1,
		StartsCompleted:       0,
		StartsFailed:          0,
	}, starting)
	require.Equal(t, float64(2), requireGauge(t, metricsRegistry, "semstreams_startup_units", map[string]string{
		"owner": "components", "stage": "admitted",
	}))
	require.Equal(t, float64(1), requireGauge(t, metricsRegistry, "semstreams_startup_units", map[string]string{
		"owner": "components", "stage": "starts_invoked",
	}))
	require.Equal(t, float64(0), requireGauge(t, metricsRegistry, "semstreams_startup_units", map[string]string{
		"owner": "components", "stage": "starts_completed",
	}))

	close(gated.release)
	require.NoError(t, <-done)
	completed := cm.startupSnapshot()
	require.Equal(t, 1, completed.StartsCompleted)
	require.Equal(t, 0, completed.StartsFailed)
	require.Equal(t, float64(1), requireGauge(t, metricsRegistry, "semstreams_startup_units", map[string]string{
		"owner": "components", "stage": "starts_completed",
	}))
	require.NoError(t, cm.Stop(t.Context()))
}

func TestStartupReadinessAndAtomicPromotion(t *testing.T) {
	componentManager := &MockService{name: "component-manager", status: StatusRunning, healthy: true}
	manager := createTestServiceManager(ManagerConfig{}, nil)
	require.NoError(t, manager.RegisterInstance("component-manager", componentManager))
	_, err := manager.sealComposition()
	require.NoError(t, err)
	require.NoError(t, manager.initializeHTTPInfrastructure())

	preStart := httptest.NewRecorder()
	manager.handleReadiness(preStart, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusServiceUnavailable, preStart.Code)
	require.Equal(t, "NOT READY", preStart.Body.String())

	startupHandler := manager.buildHTTPHandler()
	nonDiagnostic := httptest.NewRecorder()
	startupHandler.ServeHTTP(nonDiagnostic, httptest.NewRequest(http.MethodGet, "/graph/triples", nil))
	require.Equal(t, http.StatusServiceUnavailable, nonDiagnostic.Code)
	require.Equal(t, "NOT READY", nonDiagnostic.Body.String())

	manager.recordServiceStartInvoked("component-manager")
	manager.recordServiceStartCompleted("component-manager", nil)
	fullMux := http.NewServeMux()
	fullMux.HandleFunc("/promoted", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	manager.commitStartup(fullMux)

	promoted := httptest.NewRecorder()
	startupHandler.ServeHTTP(promoted, httptest.NewRequest(http.MethodGet, "/promoted", nil))
	require.Equal(t, http.StatusNoContent, promoted.Code)

	ready := httptest.NewRecorder()
	manager.handleReadiness(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusOK, ready.Code)
	require.Equal(t, "READY", ready.Body.String())

	componentManager.healthy = false
	notReady := httptest.NewRecorder()
	manager.handleReadiness(notReady, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusServiceUnavailable, notReady.Code)
}

func TestReadinessIncludesHealthyNonLifecycleDiscoverables(t *testing.T) {
	const testSafetyBound = 2 * time.Second

	lifecycleComponent := newBarrierTestComponent("lifecycle")
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
	healthyObserved := make(chan struct{})
	var healthyObservedOnce sync.Once
	cm.OnHealthChange(func(healthy bool) {
		if healthy {
			healthyObservedOnce.Do(func() { close(healthyObserved) })
		}
	})
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), testSafetyBound)
		defer stopCancel()
		require.NoError(t, cm.Stop(stopCtx))
	})
	require.NoError(t, cm.Start(t.Context()))
	select {
	case <-healthyObserved:
	case <-time.After(testSafetyBound):
		t.Fatal("component-manager did not publish its initial healthy observation")
	}
	require.True(t, cm.IsHealthy())

	manager := createTestServiceManager(ManagerConfig{}, nil)
	require.NoError(t, manager.RegisterInstance("component-manager", cm))
	services, err := manager.sealComposition()
	require.NoError(t, err)
	for _, admitted := range services {
		manager.recordServiceStartInvoked(admitted.name)
		manager.recordServiceStartCompleted(admitted.name, nil)
	}
	manager.commitStartup(http.NewServeMux())

	ready := httptest.NewRecorder()
	manager.handleReadiness(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusOK, ready.Code)

	plain.healthy.Store(false)
	notReady := httptest.NewRecorder()
	manager.handleReadiness(notReady, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusServiceUnavailable, notReady.Code)
}

func TestReadinessWaitsForInitialServiceHealthObservation(t *testing.T) {
	const testSafetyBound = 2 * time.Second

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions(
			"component-manager", nil, WithLogger(logger),
		),
		components:    make(map[string]*component.ManagedComponent),
		registry:      component.NewRegistry(),
		storeRegistry: storeregistry.New(),
		storeProvided: make(map[string][]string),
	}
	cm.initialized.Store(true)

	healthCheckEntered := make(chan struct{})
	healthCheckRelease := make(chan struct{})
	healthyObserved := make(chan struct{})
	var enteredOnce sync.Once
	var releaseOnce sync.Once
	var healthyObservedOnce sync.Once
	cm.SetHealthCheck(func() error {
		enteredOnce.Do(func() { close(healthCheckEntered) })
		<-healthCheckRelease
		return cm.healthCheck()
	})
	cm.OnHealthChange(func(healthy bool) {
		if healthy {
			healthyObservedOnce.Do(func() { close(healthyObserved) })
		}
	})

	manager := createTestServiceManager(ManagerConfig{HTTPPort: 0}, nil)
	manager.BaseService = NewBaseServiceWithOptions(
		"service-manager", nil, WithLogger(logger),
	)
	require.NoError(t, manager.RegisterInstance("component-manager", cm))

	lifecycleCtx, lifecycleCancel := context.WithCancel(t.Context())
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(healthCheckRelease) })
		stopCtx, stopCancel := context.WithTimeout(context.Background(), testSafetyBound)
		stopErr := manager.StopAll(stopCtx)
		stopCancel()
		lifecycleCancel()
		require.NoError(t, stopErr)
	})

	require.NoError(t, manager.StartAll(lifecycleCtx))
	require.True(t, manager.bootCommitted.Load())
	select {
	case <-healthCheckEntered:
	case <-time.After(testSafetyBound):
		t.Fatal("component-manager initial health check did not enter")
	}

	notReady := httptest.NewRecorder()
	manager.handleReadiness(notReady, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusServiceUnavailable, notReady.Code)
	require.Equal(t, "NOT READY", notReady.Body.String())

	releaseOnce.Do(func() { close(healthCheckRelease) })
	select {
	case <-healthyObserved:
	case <-time.After(testSafetyBound):
		t.Fatal("component-manager did not publish health after the initial check completed")
	}
	require.True(t, cm.IsHealthy())

	ready := httptest.NewRecorder()
	manager.handleReadiness(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusOK, ready.Code)
	require.Equal(t, "READY", ready.Body.String())
}

func TestServicesResponseIncludesStartupCounts(t *testing.T) {
	manager := createTestServiceManager(ManagerConfig{}, nil)
	require.NoError(t, manager.RegisterInstance("component-manager", &MockService{
		name: "component-manager", status: StatusStarting, healthy: true,
	}))
	_, err := manager.sealComposition()
	require.NoError(t, err)
	manager.recordServiceStartInvoked("component-manager")

	recorder := httptest.NewRecorder()
	manager.handleServiceList(recorder, httptest.NewRequest(http.MethodGet, "/services", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Startup startupSnapshot `json:"startup"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "starting", response.Startup.Status)
	require.Equal(t, 1, response.Startup.Services.Admitted)
	require.Equal(t, 1, response.Startup.Services.StartsInvoked)
	require.Equal(t, 0, response.Startup.Services.StartsCompleted)
}

func TestStartupMuxAdmitsOnlyReadOnlyComponentDiagnostics(t *testing.T) {
	plain := newStartupDiscoverable()
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		components: map[string]*component.ManagedComponent{
			"plain": {Component: plain, State: component.StateCreated},
		},
		componentConfigs: map[string]types.ComponentConfig{
			"plain": {Name: "plain", Type: types.ComponentTypeProcessor, Enabled: true},
		},
		registry: component.NewRegistry(),
	}
	manager := createTestServiceManager(ManagerConfig{}, nil)
	require.NoError(t, manager.RegisterInstance("component-manager", cm))
	_, err := manager.sealComposition()
	require.NoError(t, err)
	require.NoError(t, manager.initializeHTTPInfrastructure())
	handler := manager.buildHTTPHandler()

	for _, path := range []string{"/components/health", "/components/list", "/components/status/plain"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, response.Code, path)
	}
	for _, path := range []string{"/components/types", "/components/config/plain", "/components/flowgraph"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusServiceUnavailable, response.Code, path)
		require.Equal(t, "NOT READY", response.Body.String(), path)
	}
}

func TestStartupMetricsExposeFixedOwnerStagePairs(t *testing.T) {
	registry := metric.NewMetricsRegistry()
	_, err := newStartupMetricWriter(
		registry,
		func() serviceStartupCounts { return serviceStartupCounts{Admitted: 3} },
		func() startupUnitCounts { return startupUnitCounts{LifecycleParticipants: 7} },
	)
	require.NoError(t, err)

	families, err := registry.PrometheusRegistry().Gather()
	require.NoError(t, err)
	var pairs = map[string]float64{}
	for _, family := range families {
		if family.GetName() != "semstreams_startup_units" {
			continue
		}
		for _, sample := range family.Metric {
			labels := map[string]string{}
			for _, label := range sample.Label {
				labels[label.GetName()] = label.GetValue()
			}
			pairs[labels["owner"]+"/"+labels["stage"]] = sample.GetGauge().GetValue()
		}
	}
	require.Equal(t, float64(3), pairs["services/admitted"])
	require.Equal(t, float64(7), pairs["components/lifecycle_participants"])
}

func TestBuiltinMetricsRetainsRegistrationOrder(t *testing.T) {
	manager := createTestServiceManager(ManagerConfig{}, nil)
	for _, name := range []string{"component-manager", "later"} {
		require.NoError(t, manager.RegisterInstance(name, &MockService{name: name, status: StatusRunning, healthy: true}))
	}
	require.NoError(t, manager.RegisterInstance("metrics", &Metrics{
		BaseService: NewBaseServiceWithOptions("metrics", nil),
	}))
	services, err := manager.sealComposition()
	require.NoError(t, err)
	require.Equal(t, []string{"component-manager", "later", "metrics"}, admittedServiceNames(services))

	customManager := createTestServiceManager(ManagerConfig{}, nil)
	for _, name := range []string{"component-manager", "later", "metrics"} {
		require.NoError(t, customManager.RegisterInstance(name, &MockService{
			name: name, status: StatusRunning, healthy: true,
		}))
	}
	customServices, err := customManager.sealComposition()
	require.NoError(t, err)
	require.Equal(t, []string{"component-manager", "later", "metrics"}, admittedServiceNames(customServices))
}

func admittedServiceNames(services []admittedService) []string {
	names := make([]string, 0, len(services))
	for _, admitted := range services {
		names = append(names, admitted.name)
	}
	return names
}

func TestStartupMuxCommitIsCausallyAtomic(t *testing.T) {
	manager := createTestServiceManager(ManagerConfig{}, nil)
	require.NoError(t, manager.initializeHTTPInfrastructure())
	handler := manager.buildHTTPHandler()
	fullMux := http.NewServeMux()
	fullMux.HandleFunc("/probe", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	prepared := make(chan struct{})
	release := make(chan struct{})
	manager.testCommitPrepared = prepared
	manager.testCommitRelease = release

	committed := make(chan struct{})
	go func() {
		manager.commitStartup(fullMux)
		close(committed)
	}()
	<-prepared
	before := httptest.NewRecorder()
	handler.ServeHTTP(before, httptest.NewRequest(http.MethodGet, "/probe", nil))
	require.Equal(t, http.StatusServiceUnavailable, before.Code)
	require.Equal(t, "NOT READY", before.Body.String())

	close(release)
	<-committed
	after := httptest.NewRecorder()
	handler.ServeHTTP(after, httptest.NewRequest(http.MethodGet, "/probe", nil))
	require.Equal(t, http.StatusNoContent, after.Code)
}

func TestStartAllBindsSharedAndMetricsBeforeBlockedService(t *testing.T) {
	httpPort := freePort(t)
	metricsPort := freePort(t)
	metricsRegistry := metric.NewMetricsRegistry()
	deps := &Dependencies{
		Logger:          slog.Default(),
		MetricsRegistry: metricsRegistry,
	}
	manager := createTestServiceManager(ManagerConfig{HTTPPort: httpPort}, deps)

	release := make(chan struct{})
	componentManager := newGatedStartupService("component-manager", release)
	later := newGatedStartupService("later", nil)
	metricsService, err := NewMetrics(
		json.RawMessage(fmt.Sprintf(`{"port":%d,"path":"/metrics"}`, metricsPort)),
		deps,
	)
	require.NoError(t, err)
	require.NoError(t, manager.RegisterInstance("component-manager", componentManager))
	require.NoError(t, manager.RegisterInstance("later", later))
	require.NoError(t, manager.RegisterInstance("metrics", metricsService))

	// metrics is the only real BaseService here, so it is the only unit whose
	// health is an asynchronous observation: BaseService leaves healthy at the
	// zero value until the monitor goroutine publishes the first check, which
	// readiness deliberately waits for (see
	// TestReadinessWaitsForInitialServiceHealthObservation). The gated fakes
	// report healthy from construction.
	const initialHealthBound = 2 * time.Second
	metricsHealthy := make(chan struct{})
	var metricsHealthyOnce sync.Once
	metricsConcrete, isMetrics := metricsService.(*Metrics)
	require.True(t, isMetrics, "metrics service must expose its health-change seam")
	metricsConcrete.OnHealthChange(func(healthy bool) {
		if healthy {
			metricsHealthyOnce.Do(func() { close(metricsHealthy) })
		}
	})

	var middlewareCalls atomic.Int64
	manager.UseHTTPMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			middlewareCalls.Add(1)
			next.ServeHTTP(w, r)
		})
	})

	startDone := make(chan error, 1)
	go func() { startDone <- manager.StartAll(t.Context()) }()
	select {
	case <-componentManager.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("component-manager service did not enter Start")
	}
	select {
	case <-later.entered:
		t.Fatal("later service started while component-manager Start was blocked")
	default:
	}

	client := &http.Client{Timeout: 2 * time.Second}
	readyResponse, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/readyz", httpPort))
	require.NoError(t, err)
	readyBody, err := io.ReadAll(readyResponse.Body)
	require.NoError(t, err)
	require.NoError(t, readyResponse.Body.Close())
	require.Equal(t, http.StatusServiceUnavailable, readyResponse.StatusCode)
	require.Equal(t, "NOT READY", string(readyBody))

	earlyRoute, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/openapi.json", httpPort))
	require.NoError(t, err)
	earlyBody, err := io.ReadAll(earlyRoute.Body)
	require.NoError(t, err)
	require.NoError(t, earlyRoute.Body.Close())
	require.Equal(t, http.StatusServiceUnavailable, earlyRoute.StatusCode)
	require.Equal(t, "NOT READY", string(earlyBody))
	require.EqualValues(t, 2, middlewareCalls.Load())

	metricsResponse, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/metrics", metricsPort))
	require.NoError(t, err)
	metricsBody, err := io.ReadAll(metricsResponse.Body)
	require.NoError(t, err)
	require.NoError(t, metricsResponse.Body.Close())
	require.Equal(t, http.StatusOK, metricsResponse.StatusCode)
	metricsText := string(metricsBody)
	require.Contains(t, metricsText, `semstreams_startup_units{owner="services",stage="admitted"} 3`)
	require.Contains(t, metricsText, `semstreams_startup_units{owner="services",stage="starts_completed"} 0`)
	require.Contains(t, metricsText, `semstreams_startup_units{owner="services",stage="starts_invoked"} 1`)

	close(release)
	// StartAll starts services sequentially, so its return already establishes
	// that later.Start ran to completion — assert that outcome rather than
	// re-waiting later.entered, which Start closes before the gate and which
	// therefore proves nothing here (#1189).
	require.NoError(t, <-startDone)
	require.EqualValues(t, 1, later.starts.Load(), "later service did not start after gate release")
	require.Equal(t, StatusRunning, later.Status())

	// A successful StartAll is not yet readiness: readiness also requires every
	// service's first health observation, which the metrics monitor goroutine
	// publishes off the Start path. Asserting an instantaneous 200 here raced
	// that goroutine and lost under CI contention, and the next observation was
	// a whole healthInterval away, so the single GET could never recover (#1189).
	select {
	case <-metricsHealthy:
	case <-time.After(initialHealthBound):
		t.Fatal("metrics service did not publish its initial healthy observation")
	}

	readyResponse, err = client.Get(fmt.Sprintf("http://127.0.0.1:%d/readyz", httpPort))
	require.NoError(t, err)
	readyBody, err = io.ReadAll(readyResponse.Body)
	require.NoError(t, err)
	require.NoError(t, readyResponse.Body.Close())
	require.Equal(t, http.StatusOK, readyResponse.StatusCode)
	require.Equal(t, "READY", string(readyBody))

	fullRoute, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/openapi.json", httpPort))
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, fullRoute.Body)
	require.NoError(t, err)
	require.NoError(t, fullRoute.Body.Close())
	require.Equal(t, http.StatusOK, fullRoute.StatusCode)
	require.NoError(t, manager.StopAll(t.Context()))
}

func TestMetricsBindFailureClosesSharedAndStartsNoLaterService(t *testing.T) {
	occupied, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = occupied.Close() })
	metricsPort := occupied.Addr().(*net.TCPAddr).Port
	httpPort := freePort(t)
	metricsRegistry := metric.NewMetricsRegistry()
	deps := &Dependencies{Logger: slog.Default(), MetricsRegistry: metricsRegistry}
	manager := createTestServiceManager(ManagerConfig{HTTPPort: httpPort}, deps)
	sharedBound := make(chan struct{})
	metricsBindRelease := make(chan struct{})
	handlerEntered := make(chan struct{})
	handlerRelease := make(chan struct{})
	var releaseMetricsOnce sync.Once
	var releaseHandlerOnce sync.Once
	t.Cleanup(func() {
		releaseMetricsOnce.Do(func() { close(metricsBindRelease) })
		releaseHandlerOnce.Do(func() { close(handlerRelease) })
	})
	manager.testSharedHTTPBound = sharedBound
	manager.testMetricsBindRelease = metricsBindRelease
	manager.UseHTTPMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/services" {
				close(handlerEntered)
				<-handlerRelease
			}
			next.ServeHTTP(w, r)
		})
	})
	componentManager := newGatedStartupService("component-manager", nil)
	later := newGatedStartupService("later", nil)
	metricsService, err := NewMetrics(
		json.RawMessage(fmt.Sprintf(`{"port":%d,"path":"/metrics"}`, metricsPort)), deps,
	)
	require.NoError(t, err)
	require.NoError(t, manager.RegisterInstance("component-manager", componentManager))
	require.NoError(t, manager.RegisterInstance("later", later))
	require.NoError(t, manager.RegisterInstance("metrics", metricsService))

	startDone := make(chan error, 1)
	go func() { startDone <- manager.StartAll(t.Context()) }()
	<-sharedBound
	shutdownStarted := make(chan struct{})
	manager.mu.RLock()
	sharedServer := manager.httpServer
	manager.mu.RUnlock()
	require.NotNil(t, sharedServer)
	sharedServer.RegisterOnShutdown(func() { close(shutdownStarted) })

	type servicesResult struct {
		startup startupSnapshot
		err     error
	}
	servicesDone := make(chan servicesResult, 1)
	go func() {
		request, requestErr := http.NewRequestWithContext(
			t.Context(), http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/services", httpPort), nil,
		)
		if requestErr != nil {
			servicesDone <- servicesResult{err: requestErr}
			return
		}
		response, requestErr := http.DefaultClient.Do(request)
		if requestErr != nil {
			servicesDone <- servicesResult{err: requestErr}
			return
		}
		defer response.Body.Close()
		var body struct {
			Startup startupSnapshot `json:"startup"`
		}
		requestErr = json.NewDecoder(response.Body).Decode(&body)
		servicesDone <- servicesResult{startup: body.Startup, err: requestErr}
	}()
	<-handlerEntered
	releaseMetricsOnce.Do(func() { close(metricsBindRelease) })
	<-shutdownStarted
	require.Equal(t, "stopping", manager.currentStartupSnapshot().Status)
	select {
	case earlyErr := <-startDone:
		t.Fatalf("StartAll returned before the blocked diagnostic handler drained: %v", earlyErr)
	default:
	}
	releaseHandlerOnce.Do(func() { close(handlerRelease) })
	servicesResponse := <-servicesDone
	require.NoError(t, servicesResponse.err)
	require.Equal(t, "stopping", servicesResponse.startup.Status)

	err = <-startDone
	require.Error(t, err)
	require.Contains(t, err.Error(), "start metrics diagnostics")
	require.Zero(t, componentManager.starts.Load())
	require.Zero(t, later.starts.Load())
	manager.mu.RLock()
	require.True(t, manager.httpTerminal)
	require.Nil(t, manager.httpListener)
	manager.mu.RUnlock()

	connection, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", httpPort), 250*time.Millisecond)
	if connection != nil {
		require.NoError(t, connection.Close())
	}
	require.Error(t, err, "failed metrics Start must release the early shared listener")
}

type stopOrderService struct {
	MockService
	order *[]string
	mu    *sync.Mutex
}

func (s *stopOrderService) Stop(context.Context) error {
	s.mu.Lock()
	*s.order = append(*s.order, s.name)
	s.mu.Unlock()
	return nil
}

func TestStopAllReversesRegistrationOrder(t *testing.T) {
	manager := createTestServiceManager(ManagerConfig{}, nil)
	var mu sync.Mutex
	var stopped []string
	for _, name := range []string{"component-manager", "later", "metrics"} {
		require.NoError(t, manager.RegisterInstance(name, &stopOrderService{
			MockService: MockService{name: name, status: StatusRunning, healthy: true},
			order:       &stopped,
			mu:          &mu,
		}))
	}
	require.NoError(t, manager.StopAll(t.Context()))
	require.Equal(t, []string{"metrics", "later", "component-manager"}, stopped)
}

func TestStartupMetricHasNoUnitIdentityLabel(t *testing.T) {
	registry := metric.NewMetricsRegistry()
	_, err := newStartupMetricWriter(
		registry,
		func() serviceStartupCounts { return serviceStartupCounts{Admitted: 1} },
		func() startupUnitCounts { return startupUnitCounts{} },
	)
	require.NoError(t, err)
	families, err := registry.PrometheusRegistry().Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != "semstreams_startup_units" {
			continue
		}
		for _, sample := range family.Metric {
			for _, label := range sample.Label {
				require.NotContains(t, strings.ToLower(label.GetName()), "name")
				require.NotContains(t, strings.ToLower(label.GetName()), "unit")
			}
		}
		return
	}
	t.Fatal("startup metric family missing")
}

func TestStartAllRejectsEndedContextBeforeCompositionOrListenerMutation(t *testing.T) {
	for _, test := range []struct {
		name string
		ctx  func() context.Context
	}{
		{name: "nil", ctx: func() context.Context { return nil }},
		{name: "pre-canceled", ctx: func() context.Context {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			return ctx
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := createTestServiceManager(ManagerConfig{HTTPPort: 18080}, nil)
			err := manager.StartAll(test.ctx())
			require.Error(t, err)
			manager.mu.RLock()
			defer manager.mu.RUnlock()
			require.False(t, manager.sealed)
			require.False(t, manager.httpUsed)
			require.Empty(t, manager.services)
		})
	}
}
