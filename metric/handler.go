package metric

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/security"
	"github.com/c360studio/semstreams/pkg/tlsutil"
)

const forcedServeJoinTimeout = time.Second

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
	used      bool
	stopping  bool
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

// Start starts the one-shot metrics HTTP server. It binds synchronously and
// returns only after this Server owns its listener. ctx is the exact base
// context for served requests. Restart requires a freshly constructed Server.
func (s *Server) Start(ctx context.Context) error {
	return s.start(ctx, nil, false, "Start")
}

// StartWithListener starts the one-shot metrics HTTP server on a caller-bound
// raw TCP listener. A successful return transfers listener ownership to Server;
// every returned error leaves it with the caller. Server applies configured TLS
// itself, so callers must not pass a TLS-wrapped listener.
func (s *Server) StartWithListener(ctx context.Context, listener net.Listener) error {
	return s.start(ctx, listener, true, "StartWithListener")
}

func (s *Server) start(ctx context.Context, supplied net.Listener, provided bool, operation string) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Server", operation, "nil context")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Server", operation, "context already ended")
	}
	if provided {
		if supplied == nil {
			return errs.WrapInvalid(errs.ErrInvalidData, "Server", operation, "nil listener")
		}
		addr, ok := supplied.Addr().(*net.TCPAddr)
		if !ok || addr.Port <= 0 {
			return errs.WrapInvalid(errs.ErrInvalidData, "Server", operation, "listener has no assigned TCP endpoint")
		}
		if socket, ok := supplied.(syscall.Conn); ok {
			raw, err := socket.SyscallConn()
			if err == nil {
				err = raw.Control(func(uintptr) {})
			}
			if err != nil {
				return errs.WrapInvalid(err, "Server", operation, "listener is closed")
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.used {
		return errs.WrapInvalid(
			errs.ErrAlreadyStarted,
			"Server", operation, "server instance already used")
	}
	s.used = true

	// Validate that we have a registry
	if s.registry == nil {
		return errs.WrapFatal(
			fmt.Errorf("nil registry"),
			"Server", operation, "metrics registry not provided")
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
		Addr:        fmt.Sprintf(":%d", s.port),
		Handler:     mux,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	// Configure TLS if enabled at platform level
	if s.security.TLS.Server.Enabled {
		tlsConfig, err := tlsutil.LoadServerTLSConfig(s.security.TLS.Server)
		if err != nil {
			s.server = nil
			return errs.WrapFatal(err, "Server", operation, "load TLS config")
		}
		s.server.TLSConfig = tlsConfig
	}

	httpServer := s.server
	listener := supplied
	if !provided {
		var err error
		listener, err = net.Listen("tcp", httpServer.Addr)
		if err != nil {
			s.server = nil
			return errs.WrapFatal(err, "Server", operation,
				fmt.Sprintf("failed to start server on port %d", s.port))
		}
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

// Stop attempts graceful shutdown within ctx. If that budget ends or graceful
// shutdown fails, Stop force-closes the server and gives the exact serving
// goroutine a separate fixed one-second join bound. A completed repeat is a nil
// no-op. Concurrent Stop is unsupported and returns a typed transient error.
func (s *Server) Stop(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Server", "Stop", "nil context")
	}
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return errs.WrapTransient(errors.New("metrics server stop already in progress"),
			"Server", "Stop", "concurrent Stop is unsupported")
	}
	if s.server == nil {
		s.used = true
		s.mu.Unlock()
		return nil
	}

	httpServer := s.server
	listener := s.listener
	serveDone := s.serveDone
	s.stopping = true
	s.mu.Unlock()

	var stopErr error
	shutdownErr := httpServer.Shutdown(ctx)
	if shutdownErr != nil {
		stopErr = errors.Join(stopErr, errs.WrapTransient(shutdownErr, "Server", "Stop",
			"gracefully shut down metrics server"))
	}
	completed := false
	if shutdownErr == nil {
		select {
		case err := <-serveDone:
			completed = true
			stopErr = errors.Join(stopErr, classifyServeError(err))
		case <-ctx.Done():
			stopErr = errors.Join(stopErr, ctx.Err())
		}
	}

	if shutdownErr != nil || !completed {
		if err := httpServer.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			stopErr = errors.Join(stopErr, errs.WrapTransient(err, "Server", "Stop",
				"force close metrics HTTP server"))
		}
		if listener != nil {
			if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				stopErr = errors.Join(stopErr, errs.WrapTransient(err, "Server", "Stop",
					"force close metrics listener"))
			}
		}

		joinCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), forcedServeJoinTimeout)
		select {
		case err := <-serveDone:
			stopErr = errors.Join(stopErr, classifyServeError(err))
		case <-joinCtx.Done():
			stopErr = errors.Join(stopErr,
				fmt.Errorf("wait for forced metrics serve completion: %w", joinCtx.Err()))
		}
		cancel()
	}

	s.mu.Lock()
	s.server = nil
	s.listener = nil
	s.serveDone = nil
	s.stopping = false
	s.mu.Unlock()
	return stopErr
}

func classifyServeError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return errs.WrapTransient(err, "Server", "Stop", "metrics server exited with an error")
}

// Address reports the accepted listener's endpoint while Server owns it.
// Before startup and after completed Stop, it reports the configured endpoint.
// The address does not assert that Serve is ready to accept requests.
func (s *Server) Address() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	host := "localhost"
	port := s.port
	if s.listener != nil {
		if addr, ok := s.listener.Addr().(*net.TCPAddr); ok {
			port = addr.Port
			if addr.IP != nil && !addr.IP.IsUnspecified() {
				host = addr.IP.String()
				if addr.Zone != "" {
					host += "%" + addr.Zone
				}
			}
		}
	}
	scheme := "http"
	if s.security.TLS.Server.Enabled {
		scheme = "https"
	}
	return (&url.URL{Scheme: scheme, Host: net.JoinHostPort(host, strconv.Itoa(port)), Path: s.path}).String()
}
