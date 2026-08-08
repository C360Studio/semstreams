package metric

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/security"
	"github.com/c360studio/semstreams/pkg/tlsutil"
)

// Server represents the metrics HTTP server
type Server struct {
	port      int
	path      string
	server    *http.Server
	listener  net.Listener
	serveDone chan error
	registry  *MetricsRegistry
	security  security.Config
	mu        sync.Mutex // serializes server lifecycle fields
}

// NewServer creates a new metrics server with the provided registry
func NewServer(port int, path string, registry *MetricsRegistry, securityCfg security.Config) *Server {
	if path == "" {
		path = "/metrics"
	}
	if port == 0 {
		port = 9090
	}

	return &Server{
		port:     port,
		path:     path,
		registry: registry,
		security: securityCfg,
	}
}

// Start starts the metrics HTTP server. It binds synchronously and returns only
// after this Server owns its listener; request serving continues in a managed
// goroutine until Stop is called.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if server is already running
	if s.server != nil {
		return errs.WrapInvalid(
			fmt.Errorf("server already running"),
			"Server", "Start", "cannot start server that is already running")
	}

	// Validate that we have a registry
	if s.registry == nil {
		return errs.WrapFatal(
			fmt.Errorf("nil registry"),
			"Server", "Start", "metrics registry not provided")
	}

	mux := http.NewServeMux()

	// Create Prometheus HTTP handler
	handler := promhttp.HandlerFor(
		s.registry.PrometheusRegistry(),
		promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		},
	)

	// Register the handler
	mux.Handle(s.path, handler)

	// Add a health endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Add a root handler with information
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprintf(w, `<html>
<head><title>SemStreams Metrics</title></head>
<body>
<h1>SemStreams Metrics Server</h1>
<p><a href="%s">Metrics</a></p>
<p><a href="/health">Health</a></p>
</body>
</html>`, s.path)
	})

	// Create the server
	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
	}

	// Configure TLS if enabled at platform level
	if s.security.TLS.Server.Enabled {
		tlsConfig, err := tlsutil.LoadServerTLSConfig(s.security.TLS.Server)
		if err != nil {
			s.server = nil
			return errs.WrapFatal(err, "Server", "Start", "load TLS config")
		}
		s.server.TLSConfig = tlsConfig
	}

	httpServer := s.server
	listener, err := net.Listen("tcp", httpServer.Addr)
	if err != nil {
		s.server = nil
		return errs.WrapFatal(err, "Server", "Start",
			fmt.Sprintf("failed to start server on port %d", s.port))
	}
	if s.security.TLS.Server.Enabled {
		listener = tls.NewListener(listener, httpServer.TLSConfig)
	}

	serveDone := make(chan error, 1)
	s.listener = listener
	s.serveDone = serveDone
	go func() {
		serveDone <- httpServer.Serve(listener)
		close(serveDone)
	}()

	return nil
}

// Stop stops the metrics server
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server == nil {
		return nil
	}

	httpServer := s.server
	listener := s.listener
	serveDone := s.serveDone

	var stopErr error
	if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		stopErr = errors.Join(stopErr, errs.WrapTransient(err, "Server", "Stop",
			"failed to close metrics listener"))
	}
	if err := httpServer.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
		stopErr = errors.Join(stopErr, errs.WrapTransient(err, "Server", "Stop",
			"failed to stop HTTP server"))
	}
	if err := <-serveDone; err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
		stopErr = errors.Join(stopErr, errs.WrapTransient(err, "Server", "Stop",
			"metrics server exited with an error"))
	}
	s.server = nil
	s.listener = nil
	s.serveDone = nil
	return stopErr
}

// Address returns the server address
func (s *Server) Address() string {
	scheme := "http"
	if s.security.TLS.Server.Enabled {
		scheme = "https"
	}
	return fmt.Sprintf("%s://localhost:%d%s", scheme, s.port, s.path)
}
