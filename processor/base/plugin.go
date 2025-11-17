package plugin

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/c360/semstreams/component"
	"github.com/nats-io/nats.go"
)

// HealthStatus represents the current health status of a processor
type HealthStatus struct {
	Status       string // "healthy", "degraded", "unhealthy"
	LastActivity time.Time
	ErrorCount   int64
	Message      string
}

// Processor represents a simplified processor that processes raw data and outputs processed data
type Processor interface {
	// Identification
	Name() string   // Processor name (e.g., "sensors", "robotics")
	Domain() string // Domain identifier (e.g., "sensors", "robotics")

	// Lifecycle
	Initialize(ctx context.Context, nc *nats.Conn) error // Initialize processor with NATS connection
	Shutdown(ctx context.Context) error                  // Shutdown processor

	// Raw Data Processing
	ProcessRawData(ctx context.Context, subject string, data []byte) error // Process raw data from subject

	// Health Monitoring
	Health() component.HealthStatus // Get current health status

	// Configuration Management
	Configuration() any                                        // Get current configuration
	ValidateConfiguration(config any) error                    // Validate configuration
	ReloadConfiguration(ctx context.Context, config any) error // Reload configuration

	// Metrics
	Metrics() map[string]any // Get processor metrics

	// Subscription Management
	SubscriptionPatterns() []string // Get raw.* patterns to subscribe
}

// ProcessorInfo holds basic processor metadata
type ProcessorInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Domain      string `json:"domain"`
	Author      string `json:"author,omitempty"`
	License     string `json:"license,omitempty"`
	Website     string `json:"website,omitempty"`
}

// BaseProcessor provides default implementations for common processor functionality
type BaseProcessor struct {
	info         ProcessorInfo
	nc           *nats.Conn
	lastActivity time.Time
	errorCount   int64
	status       string
}

// NewBaseProcessor creates a new base processor
func NewBaseProcessor(info ProcessorInfo) *BaseProcessor {
	return &BaseProcessor{
		info:         info,
		lastActivity: time.Now(),
		status:       "healthy",
	}
}

// Name returns the processor name
func (bp *BaseProcessor) Name() string {
	return bp.info.Name
}

// Domain returns the processor domain
func (bp *BaseProcessor) Domain() string {
	return bp.info.Domain
}

// Version returns the processor version
func (bp *BaseProcessor) Version() string {
	return bp.info.Version
}

// Description returns the processor description
func (bp *BaseProcessor) Description() string {
	return bp.info.Description
}

// Initialize provides default initialization
func (bp *BaseProcessor) Initialize(ctx context.Context, nc *nats.Conn) error {
	// Check for cancellation before initialization
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	bp.nc = nc
	bp.lastActivity = time.Now()
	return nil
}

// Shutdown provides default shutdown
func (bp *BaseProcessor) Shutdown(ctx context.Context) error {
	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	bp.nc = nil
	return nil
}

// NATSConnection returns the NATS connection for use by implementations
func (bp *BaseProcessor) NATSConnection() *nats.Conn {
	return bp.nc
}

// Health returns the current health status
func (bp *BaseProcessor) Health() component.HealthStatus {
	return component.HealthStatus{
		Healthy:    bp.status == "healthy",
		LastCheck:  time.Now(),
		ErrorCount: int(atomic.LoadInt64(&bp.errorCount)),
		LastError:  "",
		Uptime:     time.Since(bp.lastActivity),
	}
}

// Configuration returns the default configuration (empty)
func (bp *BaseProcessor) Configuration() any {
	return map[string]any{
		"enabled": true,
		"name":    bp.info.Name,
		"domain":  bp.info.Domain,
	}
}

// ValidateConfiguration validates configuration (default implementation accepts any)
func (bp *BaseProcessor) ValidateConfiguration(_ any) error {
	// Default implementation accepts any configuration
	return nil
}

// ReloadConfiguration reloads configuration (default implementation does nothing)
func (bp *BaseProcessor) ReloadConfiguration(ctx context.Context, _ any) error {
	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Default implementation does nothing
	return nil
}

// Metrics returns basic metrics
func (bp *BaseProcessor) Metrics() map[string]any {
	return map[string]any{
		"name":           bp.info.Name,
		"version":        bp.info.Version,
		"error_count":    atomic.LoadInt64(&bp.errorCount),
		"last_activity":  bp.lastActivity,
		"uptime_seconds": time.Since(bp.lastActivity).Seconds(),
	}
}

// SubscriptionPatterns returns empty patterns (processors should override)
func (bp *BaseProcessor) SubscriptionPatterns() []string {
	return []string{}
}

// UpdateActivity updates the last activity timestamp
func (bp *BaseProcessor) UpdateActivity() {
	bp.lastActivity = time.Now()
}

// IncrementErrorCount increments the error counter
func (bp *BaseProcessor) IncrementErrorCount() {
	atomic.AddInt64(&bp.errorCount, 1)
}

// SetStatus updates the processor status
func (bp *BaseProcessor) SetStatus(status string) {
	bp.status = status
}
