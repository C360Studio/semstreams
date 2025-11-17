// Package errors provides standardized error handling patterns for SemStreams components.
// It includes error classification, standard error variables, and helper functions
// for consistent error wrapping and classification across the system.
package errors

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/c360/semstreams/pkg/retry"
)

// ErrorClass represents the classification of errors for handling purposes
type ErrorClass int

const (
	// ErrorTransient represents temporary errors that may be retried
	ErrorTransient ErrorClass = iota
	// ErrorInvalid represents errors due to invalid input or configuration
	ErrorInvalid
	// ErrorFatal represents unrecoverable errors that should stop processing
	ErrorFatal
)

// String returns the string representation of ErrorClass
func (ec ErrorClass) String() string {
	switch ec {
	case ErrorTransient:
		return "transient"
	case ErrorInvalid:
		return "invalid"
	case ErrorFatal:
		return "fatal"
	default:
		return "unknown"
	}
}

// Standard error variables for common conditions
var (
	// Component lifecycle errors
	ErrAlreadyStarted = errors.New("component already started")
	ErrNotStarted     = errors.New("component not started")
	ErrAlreadyStopped = errors.New("component already stopped")
	ErrShuttingDown   = errors.New("component is shutting down")

	// Connection and networking errors
	ErrNoConnection       = errors.New("no connection available")
	ErrConnectionLost     = errors.New("connection lost")
	ErrConnectionTimeout  = errors.New("connection timeout")
	ErrSubscriptionFailed = errors.New("subscription failed")

	// Data processing errors
	ErrInvalidData    = errors.New("invalid data format")
	ErrDataCorrupted  = errors.New("data corrupted")
	ErrChecksumFailed = errors.New("checksum validation failed")
	ErrParsingFailed  = errors.New("parsing failed")

	// Storage and persistence errors
	ErrStorageFull        = errors.New("storage full")
	ErrStorageUnavailable = errors.New("storage unavailable")
	ErrBucketNotFound     = errors.New("bucket not found")
	ErrKeyNotFound        = errors.New("key not found")

	// Configuration errors
	ErrInvalidConfig  = errors.New("invalid configuration")
	ErrMissingConfig  = errors.New("missing required configuration")
	ErrConfigNotFound = errors.New("configuration not found")

	// Resource errors
	ErrResourceExhausted = errors.New("resource exhausted")
	ErrRateLimited       = errors.New("rate limited")
	ErrQuotaExceeded     = errors.New("quota exceeded")

	// Circuit breaker and retry errors
	ErrCircuitOpen        = errors.New("circuit breaker open")
	ErrMaxRetriesExceeded = errors.New("maximum retries exceeded")
	ErrRetryTimeout       = errors.New("retry timeout exceeded")
)

// ClassifiedError wraps an error with its classification
type ClassifiedError struct {
	Class     ErrorClass
	Err       error
	Message   string
	Component string
	Operation string
}

// Error implements the error interface
func (ce *ClassifiedError) Error() string {
	if ce.Message != "" {
		return ce.Message
	}
	return ce.Err.Error()
}

// Unwrap returns the underlying error
func (ce *ClassifiedError) Unwrap() error {
	return ce.Err
}

// IsTransient checks if an error is transient and should be retried
func IsTransient(err error) bool {
	if err == nil {
		return false
	}

	// Check for classified error
	var ce *ClassifiedError
	if errors.As(err, &ce) {
		return ce.Class == ErrorTransient
	}

	// Check for known transient errors
	if errors.Is(err, ErrConnectionTimeout) ||
		errors.Is(err, ErrConnectionLost) ||
		errors.Is(err, ErrStorageUnavailable) ||
		errors.Is(err, ErrRateLimited) ||
		errors.Is(err, ErrCircuitOpen) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) {
		return true
	}

	// Check error message for common transient patterns
	errStr := strings.ToLower(err.Error())
	transientPatterns := []string{
		"timeout",
		"connection",
		"network",
		"temporary",
		"unavailable",
		"busy",
		"retry",
	}

	for _, pattern := range transientPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// IsFatal checks if an error is fatal and should stop processing
func IsFatal(err error) bool {
	if err == nil {
		return false
	}

	// Check for classified error
	var ce *ClassifiedError
	if errors.As(err, &ce) {
		return ce.Class == ErrorFatal
	}

	// Check for known fatal errors
	if errors.Is(err, ErrInvalidConfig) ||
		errors.Is(err, ErrMissingConfig) ||
		errors.Is(err, ErrDataCorrupted) ||
		errors.Is(err, ErrStorageFull) ||
		errors.Is(err, ErrResourceExhausted) ||
		errors.Is(err, ErrQuotaExceeded) {
		return true
	}

	// Check error message for fatal patterns
	errStr := strings.ToLower(err.Error())
	fatalPatterns := []string{
		"fatal",
		"panic",
		"corrupted",
		"invalid config",
		"missing config",
		"out of memory",
		"disk full",
	}

	for _, pattern := range fatalPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// IsInvalid checks if an error is due to invalid input
func IsInvalid(err error) bool {
	if err == nil {
		return false
	}

	// Check for classified error
	var ce *ClassifiedError
	if errors.As(err, &ce) {
		return ce.Class == ErrorInvalid
	}

	// Check for known invalid errors
	if errors.Is(err, ErrInvalidData) ||
		errors.Is(err, ErrParsingFailed) ||
		errors.Is(err, ErrChecksumFailed) {
		return true
	}

	return false
}

// Classify returns the error class for an error
func Classify(err error) ErrorClass {
	if err == nil {
		return ErrorTransient // Default for nil
	}

	if IsTransient(err) {
		return ErrorTransient
	}
	if IsFatal(err) {
		return ErrorFatal
	}
	if IsInvalid(err) {
		return ErrorInvalid
	}

	// Default to transient for unknown errors to allow retry
	return ErrorTransient
}

// newClassified creates a new classified error
// This is an internal helper - use WrapTransient(), WrapFatal(), or WrapInvalid() instead.
func newClassified(class ErrorClass, err error, component, operation, message string) *ClassifiedError {
	return &ClassifiedError{
		Class:     class,
		Err:       err,
		Message:   message,
		Component: component,
		Operation: operation,
	}
}

// Wrap creates a standardized error with context following the pattern:
// "component.method: action failed: %w"
func Wrap(err error, component, method, action string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s.%s: %s failed: %w", component, method, action, err)
}

// WrapTransient wraps an error as transient with context
func WrapTransient(err error, component, method, action string) error {
	if err == nil {
		return nil
	}
	wrappedErr := Wrap(err, component, method, action)
	return newClassified(ErrorTransient, wrappedErr, component, method, wrappedErr.Error())
}

// WrapFatal wraps an error as fatal with context
func WrapFatal(err error, component, method, action string) error {
	if err == nil {
		return nil
	}
	wrappedErr := Wrap(err, component, method, action)
	return newClassified(ErrorFatal, wrappedErr, component, method, wrappedErr.Error())
}

// WrapInvalid wraps an error as invalid with context
func WrapInvalid(err error, component, method, action string) error {
	if err == nil {
		return nil
	}
	wrappedErr := Wrap(err, component, method, action)
	return newClassified(ErrorInvalid, wrappedErr, component, method, wrappedErr.Error())
}

// RetryConfig defines configuration for retry operations
type RetryConfig struct {
	MaxRetries      int
	InitialDelay    time.Duration
	MaxDelay        time.Duration
	BackoffFactor   float64
	RetryableErrors []error
}

// DefaultRetryConfig returns a sensible default retry configuration
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:      3,
		InitialDelay:    100 * time.Millisecond,
		MaxDelay:        5 * time.Second,
		BackoffFactor:   2.0,
		RetryableErrors: nil, // Empty list means retry all transient errors
	}
}

// ShouldRetry determines if an error should be retried based on config
func (rc RetryConfig) ShouldRetry(err error, attempt int) bool {
	if err == nil || attempt >= rc.MaxRetries {
		return false
	}

	// Check if error is transient
	if !IsTransient(err) {
		return false
	}

	// Check specific retryable errors if configured
	if len(rc.RetryableErrors) > 0 {
		for _, retryableErr := range rc.RetryableErrors {
			if errors.Is(err, retryableErr) {
				return true
			}
		}
		return false
	}

	return true
}

// ToRetryConfig converts the errors package RetryConfig to the retry framework's
// Config type for framework consistency. This enables seamless integration with
// the streamkit/retry package while maintaining error classification logic.
//
// The conversion adds 1 to MaxRetries (converting "additional attempts" to "total attempts")
// and enables jitter by default for production resilience.
func (rc RetryConfig) ToRetryConfig() retry.Config {
	return retry.Config{
		MaxAttempts:  rc.MaxRetries + 1, // MaxRetries is additional attempts beyond first
		InitialDelay: rc.InitialDelay,
		MaxDelay:     rc.MaxDelay,
		Multiplier:   rc.BackoffFactor,
		AddJitter:    true, // Enable jitter for production use
	}
}

// BackoffDelay calculates the delay for a retry attempt using framework logic
func (rc RetryConfig) BackoffDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return rc.InitialDelay
	}

	// Use framework calculation for consistency
	delay := rc.InitialDelay
	for i := 0; i < attempt; i++ {
		delay = time.Duration(float64(delay) * rc.BackoffFactor)
		if delay > rc.MaxDelay {
			delay = rc.MaxDelay
			break
		}
	}

	return delay
}
