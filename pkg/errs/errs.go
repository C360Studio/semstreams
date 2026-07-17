// Package errs provides standardized error handling patterns for SemStreams components.
// It includes error classification, standard error variables, and helper functions
// for consistent error wrapping and classification across the system.
package errs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semstreams/pkg/retry"
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

// ClassifiedError wraps an error with its classification.
//
// Code and Detail (added by ADR-060, the unified RPC error contract)
// carry the machine-readable failure shape across the natsclient wire:
// Code is a stable discriminator (the graph.ErrorCode* values —
// "entity_not_found", "revision_mismatch", ...); Detail carries
// structured context (entity id, revisions). Both are zero (empty / nil)
// for errors that predate or don't participate in the contract — every
// existing construction path leaves them empty, which is exactly what
// keeps the Is method below collision-free.
type ClassifiedError struct {
	Class     ErrorClass
	Err       error
	Message   string
	Component string
	Operation string
	Code      string         // ADR-060: stable machine code; "" = uncoded.
	Detail    map[string]any // ADR-060: structured detail; nil = none.
}

// Error implements the error interface.
//
// Nil-safe on Err: control-flow sentinels (ErrRevisionMismatch) carry a
// Message but no wrapped Err, so Error() must not deref a nil Err.
func (ce *ClassifiedError) Error() string {
	if ce.Message != "" {
		return ce.Message
	}
	if ce.Err != nil {
		return ce.Err.Error()
	}
	if ce.Code != "" {
		return ce.Code
	}
	return "classified error"
}

// Unwrap returns the underlying error
func (ce *ClassifiedError) Unwrap() error {
	return ce.Err
}

// Is reports whether ce matches target as a sentinel, BY CODE. Added for
// ADR-060 so control-flow sentinels (ErrRevisionMismatch) round-trip the
// natsclient wire: ClassifyReply sets Code on the reconstructed error,
// and errors.Is(err, ErrRevisionMismatch) resolves here.
//
// Two guards are LOAD-BEARING and locked by the TestClassifiedError_Is_*
// tests — do not remove them:
//
//  1. target must resolve to a *ClassifiedError (errors.As). Otherwise
//     errors.Is(err, context.Canceled), sql.ErrNoRows, and any plain
//     sentinel (e.g. a gateway's local errEntityNotFound) correctly
//     return false here and fall through to the normal Unwrap walk.
//  2. the target's Code must be NON-EMPTY. Without this, two uncoded
//     ClassifiedErrors ("" == "") would false-match — and every
//     wire-reconstructed error was uncoded before ADR-060, so an
//     unguarded Is would silently collide across unrelated errors.
//
// A sentinel is shaped {Code: non-empty, Err: nil}; matching is Code
// equality. Real (non-sentinel) errors carry a non-nil Err, so two real
// same-code errors never match each other as sentinels.
func (ce *ClassifiedError) Is(target error) bool {
	var t *ClassifiedError
	if !errors.As(target, &t) {
		return false
	}
	return t.Code != "" && t.Err == nil && ce.Code == t.Code
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

func inheritMachineContract(classified *ClassifiedError, err error) *ClassifiedError {
	// Wrapping adds handling class and operator context; it must not erase an
	// existing machine-readable contract. Explicit ClassifiedCode constructors
	// do not call this helper because their new code/detail intentionally replace
	// any inner contract.
	var inner *ClassifiedError
	if errors.As(err, &inner) && inner.Code != "" {
		classified.Code = inner.Code
		classified.Detail = inner.Detail
	}
	return classified
}

// Classified wraps err with the given class WITHOUT adding the
// "<component>.<method>: <action> failed: " attribution prefix that
// WrapTransient/WrapFatal/WrapInvalid layer on. Use when callers
// downstream rely on the inner error's text being the verbatim
// Error() string (e.g. wire-format consumers parsing the body for
// a known prefix).
//
// Prefer WrapTransient/WrapFatal/WrapInvalid for new code — the
// attribution prefix is operator-visible in logs and worth the cost
// when no caller is parsing the message. This bare constructor exists
// for the gh#93 dual-encoding window: handlers that need to preserve
// the historic "<kind>: <detail>" body shape (e.g. "not found: <id>",
// "invalid request: <reason>") so the body-prefix sniffers downstream
// keep working while the X-Error-Class header layer rolls out. Once
// Phase 4 retires the legacy body shape, the Wrap* family becomes
// preferred uniformly.
//
// Returns nil when err is nil.
func Classified(class ErrorClass, err error) *ClassifiedError {
	if err == nil {
		return nil
	}
	return inheritMachineContract(newClassified(class, err, "", "", err.Error()), err)
}

// ClassifiedCode is Classified plus a stable machine Code (ADR-060 — the
// graph.ErrorCode* values). Like Classified it preserves err's verbatim
// text (no attribution prefix) so the message survives the wire clean.
// Returns nil when err is nil.
func ClassifiedCode(class ErrorClass, code string, err error) *ClassifiedError {
	if err == nil {
		return nil
	}
	ce := newClassified(class, err, "", "", err.Error())
	ce.Code = code
	return ce
}

// ClassifiedCodeDetail is ClassifiedCode plus structured Detail (entity
// id, revisions, ...). graph-ingest mutation handlers attach
// entity/revision context here; the detail rides the wire in the ADR-060
// standard error body (landed with the breaking PR — until then Detail is
// carried on the error value but not serialized). Returns nil when err is
// nil.
//
// Detail is stored by reference, not copied — pass a map you do not mutate
// after construction (handlers build a fresh literal per error).
func ClassifiedCodeDetail(class ErrorClass, code string, detail map[string]any, err error) *ClassifiedError {
	if err == nil {
		return nil
	}
	ce := newClassified(class, err, "", "", err.Error())
	ce.Code = code
	ce.Detail = detail
	return ce
}

// ErrRevisionMismatch is the ADR-060 optimistic-concurrency (CAS)
// control-flow sentinel — the ONLY sentinel in the unified RPC error
// contract (every other code is a plain ce.Code discriminator, reached
// via errors.As).
//
// natsclient.ClassifyReply reconstructs a *ClassifiedError carrying
// Code == "revision_mismatch" from the wire; errors.Is(err,
// ErrRevisionMismatch) matches it via (*ClassifiedError).Is. CAS-retry
// consumers write:
//
//	if errors.Is(err, errs.ErrRevisionMismatch) { re-read; retry }
//
// ORDERING: check this sentinel BEFORE IsInvalid(err). Its class is
// ErrorInvalid (a revision mismatch is a bad-precondition write), so an
// IsInvalid-first branch would mis-handle a retry signal as a hard 400.
//
// The Code literal is the string "revision_mismatch" rather than
// graph.ErrorCodeRevisionMismatch because pkg/errs cannot import graph
// (import cycle: graph imports errs). The graph package locks the two
// equal with a compile-time assertion test (graph/errcode_sentinel_test.go).
var ErrRevisionMismatch = &ClassifiedError{
	Class:   ErrorInvalid,
	Code:    "revision_mismatch",
	Message: "revision_mismatch",
}

// Wrap creates a standardized error with context following the pattern:
// "component.method: action failed: %w"
func Wrap(err error, component, method, action string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s.%s: %s failed: %w", component, method, action, err)
}

// WrapTransient wraps an error as transient with context.
// When err is nil and action is non-empty, synthesizes the error from action
// so synchronous validation paths emit a non-nil classified error rather than
// silently returning nil. nil+empty-action still returns nil.
func WrapTransient(err error, component, method, action string) error {
	if err == nil {
		if action == "" {
			return nil
		}
		err = errors.New(action)
	}
	wrappedErr := Wrap(err, component, method, action)
	return inheritMachineContract(newClassified(ErrorTransient, wrappedErr, component, method, wrappedErr.Error()), err)
}

// WrapFatal wraps an error as fatal with context.
// When err is nil and action is non-empty, synthesizes the error from action
// so synchronous validation paths emit a non-nil classified error rather than
// silently returning nil. nil+empty-action still returns nil.
func WrapFatal(err error, component, method, action string) error {
	if err == nil {
		if action == "" {
			return nil
		}
		err = errors.New(action)
	}
	wrappedErr := Wrap(err, component, method, action)
	return inheritMachineContract(newClassified(ErrorFatal, wrappedErr, component, method, wrappedErr.Error()), err)
}

// WrapInvalid wraps an error as invalid with context.
// When err is nil and action is non-empty, synthesizes the error from action
// so synchronous validation paths emit a non-nil classified error rather than
// silently returning nil. nil+empty-action still returns nil.
func WrapInvalid(err error, component, method, action string) error {
	if err == nil {
		if action == "" {
			return nil
		}
		err = errors.New(action)
	}
	wrappedErr := Wrap(err, component, method, action)
	return inheritMachineContract(newClassified(ErrorInvalid, wrappedErr, component, method, wrappedErr.Error()), err)
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
