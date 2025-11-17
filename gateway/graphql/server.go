package graphql

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/c360/semstreams/errors"
)

// Server manages the HTTP server for GraphQL endpoint
// Phase 1: Provides HTTP server setup and playground
// Phase 2: Will add gqlgen-generated schema and resolvers
type Server struct {
	config     Config
	resolver   *BaseResolver
	logger     *slog.Logger
	httpServer *http.Server
	mux        *http.ServeMux

	// Lifecycle
	running  bool
	mu       sync.RWMutex
	stopChan chan struct{}
	stopOnce sync.Once // Ensures stopChan is closed exactly once
}

// NewServer creates a new GraphQL HTTP server
func NewServer(config Config, resolver *BaseResolver, logger *slog.Logger) (*Server, error) {
	if err := config.Validate(); err != nil {
		return nil, errors.WrapInvalid(err, "Server", "NewServer", "config validation")
	}

	if resolver == nil {
		return nil, errors.WrapFatal(fmt.Errorf("resolver is nil"), "Server", "NewServer",
			"resolver is required")
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &Server{
		config:   config,
		resolver: resolver,
		logger:   logger,
		mux:      http.NewServeMux(),
		stopChan: make(chan struct{}),
	}, nil
}

// Setup configures the HTTP server and routes
func (s *Server) Setup() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Phase 1: Setup basic routes
	// Phase 2: Will add gqlgen handler

	// Health check endpoint
	s.mux.HandleFunc("/health", s.handleHealth)

	// GraphQL Playground (if enabled)
	if s.config.EnablePlayground {
		s.mux.Handle("/", playground.Handler("GraphQL Playground", s.config.Path))
		s.logger.Info("GraphQL Playground enabled",
			"url", fmt.Sprintf("http://%s/", s.config.BindAddress))
	}

	// CORS middleware wrapper
	var handler http.Handler = s.mux
	if s.config.EnableCORS {
		handler = s.corsMiddleware(handler)
	}

	// Create HTTP server
	s.httpServer = &http.Server{
		Addr:         s.config.BindAddress,
		Handler:      handler,
		ReadTimeout:  s.config.Timeout(),
		WriteTimeout: s.config.Timeout(),
		IdleTimeout:  60 * time.Second,
	}

	s.logger.Info("Server configured",
		"address", s.config.BindAddress,
		"path", s.config.Path,
		"timeout", s.config.Timeout())

	return nil
}

// Start starts the HTTP server
// The ready channel is closed when the server is ready to accept connections
func (s *Server) Start(ctx context.Context, ready chan<- struct{}) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return errors.WrapFatal(errors.ErrAlreadyStarted, "Server", "Start", "server already running")
	}
	s.running = true
	server := s.httpServer
	s.mu.Unlock()

	// Start HTTP server in goroutine
	errChan := make(chan error, 1)
	go func() {
		defer close(errChan) // Signal goroutine exit
		s.logger.Info("Server starting", "address", s.config.BindAddress)

		// Signal ready when server starts listening
		// Note: ListenAndServe blocks after binding the socket,
		// so we signal ready immediately before the call
		if ready != nil {
			close(ready)
		}

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("HTTP server error", "error", err)
			// Non-blocking send - ensures goroutine doesn't leak if select is on another case
			select {
			case errChan <- err:
			case <-ctx.Done():
				// Context cancelled, exit gracefully
			case <-s.stopChan:
				// Stop called, exit gracefully
			}
		}
	}()

	// Wait for context cancellation or error
	select {
	case <-ctx.Done():
		s.logger.Info("Server context cancelled, shutting down")
		return s.Stop(30 * time.Second)

	case <-s.stopChan:
		s.logger.Info("Server stop requested")
		return nil

	case err := <-errChan:
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
		return errors.WrapFatal(err, "Server", "Start", "HTTP server failed")
	}
}

// Stop gracefully shuts down the HTTP server
func (s *Server) Stop(timeout time.Duration) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil // Already stopped
	}
	server := s.httpServer
	s.mu.Unlock()

	s.logger.Info("Server stopping")

	// Signal stop channel (idempotent - safe to call multiple times)
	s.stopOnce.Do(func() {
		close(s.stopChan)
	})

	// Create shutdown context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Shutdown HTTP server gracefully
	if err := server.Shutdown(ctx); err != nil {
		s.logger.Error("Failed to shutdown server gracefully", "error", err)
		return errors.WrapTransient(err, "Server", "Stop", "graceful shutdown failed")
	}

	s.mu.Lock()
	s.running = false
	s.mu.Unlock()

	s.logger.Info("Server stopped")
	return nil
}

// handleHealth handles health check requests
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	running := s.running
	s.mu.RUnlock()

	if !running {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"unavailable"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"healthy"}`))
}

// corsMiddleware adds CORS headers to responses
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Check if origin is allowed
		allowed := false
		for _, allowedOrigin := range s.config.CORSOrigins {
			if allowedOrigin == "*" || allowedOrigin == origin {
				allowed = true
				break
			}
		}

		if allowed {
			if origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			} else {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Max-Age", "3600")
		}

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// IsRunning returns whether the server is currently running
func (s *Server) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}
