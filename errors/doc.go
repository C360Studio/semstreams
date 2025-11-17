// Package errors provides standardized error handling patterns for StreamKit components.
//
// # Overview
//
// The errors package implements a three-class error classification system designed for
// distributed stream processing systems: Transient (temporary, retryable), Invalid
// (bad input, non-retryable), and Fatal (unrecoverable, stop processing).
//
// This classification enables intelligent error handling strategies throughout StreamKit,
// allowing components to make informed decisions about retries, graceful degradation,
// and failure recovery without hardcoded error string matching.
//
// # Error Classification
//
// Errors are automatically classified based on their type or content:
//
//   - Transient: Network timeouts, connection issues, temporary unavailability (retry recommended)
//   - Invalid: Malformed input, validation failures, bad configuration (do not retry)
//   - Fatal: Resource exhaustion, data corruption, unrecoverable states (stop processing)
//
// The classification system integrates seamlessly with Go's standard error handling patterns,
// supporting errors.Is(), errors.As(), and error wrapping chains.
//
// # Quick Start
//
// Use standard error variables for common conditions:
//
//	// Return standard error for known conditions
//	if !serviceAvailable {
//	    return errors.ErrConnectionTimeout
//	}
//
// Wrap errors with context for debugging:
//
//	// Wrap third-party errors with component context
//	if err := component.Process(data); err != nil {
//	    return errors.Wrap(err, "DataProcessor", "Process", "data validation")
//	}
//
// Check classification for retry logic:
//
//	// Make retry decisions based on error class
//	if err := operation(); err != nil {
//	    if errors.IsTransient(err) {
//	        // Retry with exponential backoff
//	        config := errors.DefaultRetryConfig()
//	        if config.ShouldRetry(err, attempt) {
//	            time.Sleep(config.BackoffDelay(attempt))
//	            // retry operation
//	        }
//	    } else if errors.IsFatal(err) {
//	        // Stop processing, escalate to operator
//	        log.Fatalf("Unrecoverable error: %v", err)
//	    }
//	}
//
// # Error Wrapping Pattern
//
// All error wrapping follows the standardized format:
//
//	"component.method: action failed: %w"
//
// This format enables consistent log parsing, debugging, and operational monitoring
// across the entire StreamKit platform. The Wrap family of functions automatically
// applies this pattern while preserving error classification through the chain.
//
// Three wrapper functions provide classification-aware wrapping:
//
//	errors.WrapTransient(err, "Component", "Method", "action")  // For retryable errors
//	errors.WrapInvalid(err, "Component", "Method", "action")    // For validation errors
//	errors.WrapFatal(err, "Component", "Method", "action")      // For unrecoverable errors
//
// The generic Wrap() function preserves the original error's classification:
//
//	errors.Wrap(err, "Component", "Method", "action")  // Preserves original class
//
// # Standard Error Variables
//
// The package provides pre-defined error variables for common conditions, organized by category:
//
//   - Component lifecycle: ErrAlreadyStarted, ErrNotStarted, ErrAlreadyStopped
//   - Connection issues: ErrConnectionLost, ErrConnectionTimeout, ErrConnectionRefused
//   - Data processing: ErrInvalidData, ErrDataCorrupted, ErrProcessingFailed
//   - Resource constraints: ErrResourceExhausted, ErrStorageFull, ErrMemoryExhausted
//   - NATS streaming: ErrStreamNotFound, ErrConsumerNotFound, ErrInvalidSubject
//
// Use these variables instead of creating custom error messages for consistency:
//
//	// Good - uses standard variable
//	if diskFull {
//	    return errors.ErrStorageFull
//	}
//
//	// Avoid - custom error message
//	if diskFull {
//	    return errors.New("storage full")
//	}
//
// # Retry Configuration
//
// The package includes built-in retry support with exponential backoff:
//
//	config := errors.DefaultRetryConfig()
//
//	for attempt := 0; attempt < config.MaxRetries; attempt++ {
//	    if err := operation(); err != nil {
//	        if !config.ShouldRetry(err, attempt) {
//	            return err  // Non-retryable or max attempts reached
//	        }
//	        delay := config.BackoffDelay(attempt)
//	        time.Sleep(delay)
//	        continue
//	    }
//	    return nil  // Success
//	}
//
// The retry configuration integrates with StreamKit's retry package:
//
//	retryConfig := errorConfig.ToRetryConfig()
//	// Use with streamkit/retry framework
//
// # Migration from fmt.Errorf
//
// Replace manual error wrapping with classification-aware wrappers:
//
//	// Before
//	return fmt.Errorf("component: operation failed: %w", err)
//
//	// After - preserves classification
//	return errors.Wrap(err, "Component", "method", "operation")
//
//	// After - sets classification
//	return errors.WrapTransient(err, "Component", "method", "operation")
//
// Replace string-based error inspection with classification checks:
//
//	// Before
//	if strings.Contains(err.Error(), "timeout") {
//	    // retry logic
//	}
//
//	// After
//	if errors.IsTransient(err) {
//	    // retry logic with proper backoff
//	}
//
// # Integration with errors.As/Is
//
// All error types support standard library error inspection:
//
//	// Check error classification
//	var ce *errors.ClassifiedError
//	if errors.As(err, &ce) {
//	    log.Printf("Component: %s, Class: %s", ce.Component, ce.Class)
//	}
//
//	// Check for specific standard errors
//	if errors.Is(err, errors.ErrConnectionTimeout) {
//	    // Handle timeout specifically
//	}
//
//	// Classification is preserved through error chains
//	wrapped := errors.Wrap(errors.ErrConnectionTimeout, "Service", "Connect", "dial")
//	if errors.IsTransient(wrapped) {  // true - classification preserved
//	    // Retry logic
//	}
//
// # Context Cancellation
//
// Context errors (context.DeadlineExceeded, context.Canceled) are automatically
// classified as Transient, enabling consistent handling of context-based timeouts:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//
//	if err := operation(ctx); err != nil {
//	    if errors.IsTransient(err) {
//	        // Handles both network timeouts AND context timeouts
//	        log.Printf("Transient error (retry recommended): %v", err)
//	    }
//	}
//
// # Performance Considerations
//
// Error classification is efficient for error paths:
//
//   - Classification: ~40ns per operation (1 allocation) for known types
//   - Wrapping: ~107ns per operation (2 allocations)
//   - Memory: 80 bytes per wrapped error
//
// The overhead is negligible compared to the actual error condition being handled.
// Classification uses type assertions for known types (O(1)) and falls back to
// pattern matching for unknown errors (O(n) where n is pattern count).
//
// # Thread Safety
//
// All classification and wrapping operations are thread-safe. Error variables
// are immutable constants safe for concurrent access. The ClassifiedError type
// is safe to share across goroutines after creation.
//
// # Architecture Integration
//
// The errors package integrates with other StreamKit components:
//
//   - service: Services use error classification for retry and circuit breaker logic
//   - component: Components wrap errors with component context for debugging
//   - natsclient: NATS client uses standard connection error variables
//   - retry: Retry package uses error classification for retry decisions
//
// # Design Philosophy
//
// The errors package follows these design principles:
//
//   - Classification over string matching: Errors are classified by type, not content
//   - Wrapping over replacement: Preserve original errors, add context via wrapping
//   - Standards over invention: Use Go's error handling idioms (Is/As/Unwrap)
//   - Simplicity over completeness: Three classes cover 95% of use cases
//   - Integration over isolation: Work seamlessly with standard library and other packages
//
// # Examples
//
// Complete service integration example:
//
//	package main
//
//	import (
//	    "context"
//	    "log"
//	    "time"
//
//	    "github.com/c360/semstreams/errors"
//	)
//
//	type Service struct {
//	    connected bool
//	}
//
//	func (s *Service) Connect() error {
//	    if s.connected {
//	        return errors.ErrAlreadyStarted
//	    }
//
//	    if err := s.dial(); err != nil {
//	        return errors.WrapTransient(err, "Service", "Connect", "dial")
//	    }
//
//	    s.connected = true
//	    return nil
//	}
//
//	func (s *Service) dial() error {
//	    // Simulate connection attempt
//	    return errors.ErrConnectionTimeout
//	}
//
//	func (s *Service) Process(ctx context.Context, data []byte) error {
//	    if !s.connected {
//	        return errors.ErrNotStarted
//	    }
//
//	    if len(data) == 0 {
//	        return errors.WrapInvalid(
//	            errors.ErrInvalidData,
//	            "Service", "Process", "empty data")
//	    }
//
//	    // Process with context timeout
//	    ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
//	    defer cancel()
//
//	    select {
//	    case <-ctx.Done():
//	        return errors.WrapTransient(ctx.Err(), "Service", "Process", "processing")
//	    case <-time.After(100 * time.Millisecond):
//	        return nil
//	    }
//	}
//
//	func main() {
//	    service := &Service{}
//
//	    // Retry connection with backoff
//	    config := errors.DefaultRetryConfig()
//	    for attempt := 0; attempt < config.MaxRetries; attempt++ {
//	        if err := service.Connect(); err != nil {
//	            if errors.IsTransient(err) && config.ShouldRetry(err, attempt) {
//	                log.Printf("Connection attempt %d failed, retrying...", attempt+1)
//	                time.Sleep(config.BackoffDelay(attempt))
//	                continue
//	            }
//	            log.Fatalf("Connection failed: %v", err)
//	        }
//	        break
//	    }
//
//	    // Process data with error handling
//	    ctx := context.Background()
//	    if err := service.Process(ctx, []byte("test data")); err != nil {
//	        if errors.IsInvalid(err) {
//	            log.Printf("Invalid input (do not retry): %v", err)
//	        } else if errors.IsTransient(err) {
//	            log.Printf("Transient error (retry recommended): %v", err)
//	        } else if errors.IsFatal(err) {
//	            log.Fatalf("Fatal error (stop processing): %v", err)
//	        }
//	    }
//	}
//
// For more examples and detailed API documentation, see README.md in this directory.
package errors
