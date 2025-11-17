# Errors

Standardized error handling patterns for SemStreams components with error classification, standard error variables, and helper functions.

## Overview

The errors package provides a comprehensive error handling framework designed for distributed systems and component-based architectures. It includes error classification for retry logic, standard error variables for common conditions, and helper functions for consistent error wrapping across the entire SemStreams platform.

The package categorizes errors into three classes: Transient (temporary, retryable), Invalid (bad input, non-retryable), and Fatal (unrecoverable, should stop processing). This classification enables intelligent error handling strategies throughout the system.

Beyond classification, the package provides standardized error wrapping patterns, retry configuration management, and detection helpers that make error handling consistent and predictable across all components.

## Installation

```go
import "github.com/c360/semstreams/errors"
```

## Core Concepts

### Error Classification

Errors are classified into three categories that determine handling strategy: Transient errors are temporary and should be retried, Invalid errors indicate bad input and should not be retried, and Fatal errors are unrecoverable and should stop processing.

### Standard Error Variables

Pre-defined error variables for common conditions like component lifecycle, connection issues, data processing problems, and resource constraints. These provide consistent error handling across components.

### Error Wrapping

Standardized patterns for wrapping errors with contextual information following the format "component.method: action failed: %w" for consistent error messages and debugging.

### Retry Configuration

Built-in support for retry logic with configurable backoff strategies, maximum attempts, and error-specific retry rules based on error classification.

## Usage

### Basic Example

```go
import "github.com/c360/semstreams/errors"

// Using standard error variables
func connectToService() error {
    if !serviceAvailable {
        return errors.ErrConnectionTimeout
    }
    return nil
}

// Check error classification
if err := connectToService(); err != nil {
    if errors.IsTransient(err) {
        // Retry the operation
        log.Printf("Transient error, will retry: %v", err)
    } else if errors.IsFatal(err) {
        // Stop processing
        log.Printf("Fatal error, stopping: %v", err)
        return err
    }
}

// Wrap errors with context
func (c *Component) processMessage(msg Message) error {
    if err := c.validateMessage(msg); err != nil {
        return errors.Wrap(err, "Component", "processMessage", "message validation")
    }
    return nil
}
```

### Advanced Usage - Classification and Retry

```go
// Create classified errors
func (s *Service) Start(ctx context.Context) error {
    if err := s.initialize(); err != nil {
        // Wrap as fatal error
        return errors.WrapFatal(err, "Service", "Start", "initialization")
    }
    return nil
}

// Custom retry configuration
retryConfig := errors.RetryConfig{
    MaxRetries:    5,
    InitialDelay:  100 * time.Millisecond,
    MaxDelay:      30 * time.Second,
    BackoffFactor: 2.0,
    RetryableErrors: []error{
        errors.ErrConnectionTimeout,
        errors.ErrStorageUnavailable,
    },
}

// Retry with backoff
func retryOperation(operation func() error) error {
    var lastErr error

    for attempt := 0; attempt < retryConfig.MaxRetries; attempt++ {
        if err := operation(); err != nil {
            lastErr = err

            if !retryConfig.ShouldRetry(err, attempt) {
                return err
            }

            delay := retryConfig.BackoffDelay(attempt)
            log.Printf("Operation failed (attempt %d/%d), retrying in %v: %v",
                attempt+1, retryConfig.MaxRetries, delay, err)
            time.Sleep(delay)
            continue
        }
        return nil
    }

    return errors.WrapTransient(lastErr, "RetryHandler", "retryOperation", "max retries exceeded")
}
```

## API Reference

### Types

#### `ErrorClass`

Enumeration of error classifications for handling strategies.

```go
type ErrorClass int

const (
    ErrorTransient ErrorClass = iota  // Temporary, retryable
    ErrorInvalid                     // Bad input, non-retryable
    ErrorFatal                       // Unrecoverable, stop processing
)

func (ec ErrorClass) String() string  // Returns human-readable name
```

#### `ClassifiedError`

Error wrapper that includes classification and context.

```go
type ClassifiedError struct {
    Class     ErrorClass  // Error classification
    Err       error      // Underlying error
    Message   string     // Custom message
    Component string     // Component that generated error
    Operation string     // Operation that failed
}

func (ce *ClassifiedError) Error() string    // Implements error interface
func (ce *ClassifiedError) Unwrap() error   // Returns underlying error
```

#### `RetryConfig`

Configuration for retry operations with backoff.

```go
type RetryConfig struct {
    MaxRetries      int           // Maximum retry attempts
    InitialDelay    time.Duration // Initial backoff delay
    MaxDelay        time.Duration // Maximum backoff delay
    BackoffFactor   float64       // Exponential backoff multiplier
    RetryableErrors []error       // Specific errors to retry (nil = all transient)
}

func (rc RetryConfig) ShouldRetry(err error, attempt int) bool  // Retry decision
func (rc RetryConfig) BackoffDelay(attempt int) time.Duration  // Calculate delay
```

### Standard Err Variables

#### Component Lifecycle Errors

```go
var (
    ErrAlreadyStarted  = errors.New("component already started")
    ErrNotStarted      = errors.New("component not started")
    ErrAlreadyStopped  = errors.New("component already stopped")
    ErrShuttingDown    = errors.New("component is shutting down")
)
```

#### Connection and Networking Errors

```go
var (
    ErrNoConnection       = errors.New("no connection available")
    ErrConnectionLost     = errors.New("connection lost")
    ErrConnectionTimeout  = errors.New("connection timeout")
    ErrSubscriptionFailed = errors.New("subscription failed")
)
```

#### Data Processing Errors

```go
var (
    ErrInvalidData    = errors.New("invalid data format")
    ErrDataCorrupted  = errors.New("data corrupted")
    ErrChecksumFailed = errors.New("checksum validation failed")
    ErrParsingFailed  = errors.New("parsing failed")
)
```

#### Storage and Persistence Errors

```go
var (
    ErrStorageFull        = errors.New("storage full")
    ErrStorageUnavailable = errors.New("storage unavailable")
    ErrBucketNotFound     = errors.New("bucket not found")
    ErrKeyNotFound        = errors.New("key not found")
)
```

### Functions

#### `IsTransient(err error) bool`

Checks if an error is transient and should be retried.

#### `IsFatal(err error) bool`

Checks if an error is fatal and should stop processing.

#### `IsInvalid(err error) bool`

Checks if an error is due to invalid input.

#### `Classify(err error) ErrorClass`

Returns the error class for any error.

#### `NewClassified(class ErrorClass, err error, component, operation, message string) *ClassifiedError`

Creates a new classified error with context.

#### `Wrap(err error, component, method, action string) error`

Wraps an error with standardized context format.

#### `WrapTransient(err error, component, method, action string) error`

Wraps an error as transient with context.

#### `WrapFatal(err error, component, method, action string) error`

Wraps an error as fatal with context.

#### `WrapInvalid(err error, component, method, action string) error`

Wraps an error as invalid with context.

#### `DefaultRetryConfig() RetryConfig`

Returns sensible default retry configuration.

## Architecture

### Design Decisions

**Three-Class System**: Chose Transient/Invalid/Fatal over more granular classifications because these three classes cover the primary error handling strategies needed in distributed systems.

- Trade-off: Gained simplicity and clear handling patterns but gave up fine-grained categorization

**String Pattern Matching**: Included fallback string pattern matching for error classification to handle third-party errors that don't use our standard variables.

- Rationale: Provides resilient classification even for external errors
- Alternative considered: Strict type-only classification (too fragile for real-world usage)

**Context Wrapping Format**: Standardized on "component.method: action failed: %w" format for consistent error messages across the entire system.

- Chose structured format over free-form messages because it enables automated log parsing and debugging
- Trade-off: Gained consistency and tooling support but requires discipline from developers

### Integration Points

- **Dependencies**: Standard library errors package and context for timeout detection
- **Used By**: All SemStreams components for consistent error handling
- **Data Flow**: `Error Creation → Classification → Wrapping → Retry Decision → Handling`

## Configuration

### Error Retry Configuration

```yaml
# Example retry configuration
retry:
  max_retries: 5
  initial_delay: 100ms
  max_delay: 30s
  backoff_factor: 2.0
  retryable_errors:
    - "connection timeout"
    - "storage unavailable"
```

### Error Classification Patterns

The package automatically classifies errors based on known patterns:

```yaml
# Transient patterns
transient_patterns:
  - "timeout"
  - "connection"
  - "network"
  - "temporary"
  - "unavailable"

# Fatal patterns
fatal_patterns:
  - "fatal"
  - "corrupted"
  - "out of memory"
  - "disk full"
```

## Error Handling

### Error Detection Patterns

```go
// Check specific error types
if errors.Is(err, errors.ErrConnectionTimeout) {
    // Handle connection timeout
}

// Check error classification
switch {
case errors.IsTransient(err):
    // Retry operation
case errors.IsFatal(err):
    // Stop processing
case errors.IsInvalid(err):
    // Fix input and try again
}

// Check classified error details
var ce *errors.ClassifiedError
if errors.As(err, &ce) {
    log.Printf("Error in %s.%s: %s (class: %s)",
        ce.Component, ce.Operation, ce.Error(), ce.Class)
}
```

### Best Practices

```go
// DO: Use standard error variables
return errors.ErrConnectionTimeout

// DO: Wrap with context
return errors.Wrap(err, "ComponentName", "methodName", "what failed")

// DO: Classify appropriately
return errors.WrapTransient(err, "Service", "Connect", "network connection")

// DON'T: Create raw errors for common cases
return errors.New("connection timeout") // Use ErrConnectionTimeout instead

// DON'T: Over-classify
return errors.WrapFatal(validationErr, ...) // Validation errors should be Invalid
```

## Migration Guide

### From fmt.Errorf to Classified Errors

This guide walks through migrating existing error handling code to use the errors package classification system.

**Step 1: Identify Error Types**

Review your existing error handling and classify each error:

```go
// OLD CODE
func (s *Service) Connect() error {
    if err := s.dial(); err != nil {
        return fmt.Errorf("failed to connect: %w", err)
    }
    return nil
}

// Step 1: Is this error transient, invalid, or fatal?
// Answer: Connection errors are typically TRANSIENT
```

**Step 2: Use Standard Error Variables**

Replace custom error strings with standard variables:

```go
// BEFORE
if !connected {
    return errors.New("connection timeout")
}

// AFTER
if !connected {
    return errors.ErrConnectionTimeout
}
```

**Step 3: Apply Classification Wrappers**

Replace `fmt.Errorf` with classification-aware wrappers:

```go
// BEFORE
func (s *Service) Connect() error {
    if err := s.dial(); err != nil {
        return fmt.Errorf("Service.Connect: dial failed: %w", err)
    }
    return nil
}

// AFTER
func (s *Service) Connect() error {
    if err := s.dial(); err != nil {
        return errors.WrapTransient(err, "Service", "Connect", "dial")
    }
    return nil
}
```

**Step 4: Update Error Handling**

Replace manual error inspection with classification checks:

```go
// BEFORE
if err != nil {
    if strings.Contains(err.Error(), "timeout") {
        // Retry logic
    }
}

// AFTER
if err != nil {
    if errors.IsTransient(err) {
        // Retry logic with proper backoff
        config := errors.DefaultRetryConfig()
        if config.ShouldRetry(err, attempt) {
            time.Sleep(config.BackoffDelay(attempt))
        }
    }
}
```

**Step 5: Add Retry Logic**

Convert manual retry loops to configuration-based retry:

```go
// BEFORE
for i := 0; i < 3; i++ {
    if err := operation(); err != nil {
        time.Sleep(time.Second * time.Duration(i))
        continue
    }
    return nil
}

// AFTER
config := errors.DefaultRetryConfig()
for attempt := 0; attempt < config.MaxRetries; attempt++ {
    if err := operation(); err != nil {
        if !config.ShouldRetry(err, attempt) {
            return err
        }
        time.Sleep(config.BackoffDelay(attempt))
        continue
    }
    return nil
}
```

### Common Migration Patterns

| Old Pattern | New Pattern | Classification |
|-------------|-------------|----------------|
| `errors.New("invalid input")` | `errors.ErrInvalidData` | Invalid |
| `errors.New("connection failed")` | `errors.ErrConnectionLost` | Transient |
| `errors.New("storage full")` | `errors.ErrStorageFull` | Fatal |
| `fmt.Errorf("x: %w", err)` | `errors.Wrap(err, "Comp", "method", "x")` | Preserves original |

### Migration Checklist

- [ ] Identify all error creation sites (`errors.New`, `fmt.Errorf`)
- [ ] Replace with standard error variables where applicable
- [ ] Wrap third-party errors with classification wrappers
- [ ] Update error handling to use `IsTransient()`, `IsFatal()`, `IsInvalid()`
- [ ] Replace manual retry loops with `RetryConfig`
- [ ] Add tests for error classification behavior

## Testing

### Test Utilities

```go
// Test error classification
func TestErrorClassification(t *testing.T) {
    testCases := []struct {
        name     string
        err      error
        expected errors.ErrorClass
    }{
        {"timeout error", errors.ErrConnectionTimeout, errors.ErrorTransient},
        {"invalid data", errors.ErrInvalidData, errors.ErrorInvalid},
        {"corrupted data", errors.ErrDataCorrupted, errors.ErrorFatal},
    }

    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            class := errors.Classify(tc.err)
            assert.Equal(t, tc.expected, class)
        })
    }
}

// Test retry logic
func TestRetryConfig(t *testing.T) {
    config := errors.DefaultRetryConfig()

    // Test transient error should retry
    assert.True(t, config.ShouldRetry(errors.ErrConnectionTimeout, 1))

    // Test fatal error should not retry
    assert.False(t, config.ShouldRetry(errors.ErrDataCorrupted, 1))

    // Test max retries exceeded
    assert.False(t, config.ShouldRetry(errors.ErrConnectionTimeout, config.MaxRetries))
}
```

### Testing Patterns

- Use table-driven tests for error classification scenarios
- Test retry logic with different error types and attempt counts
- Verify error wrapping preserves underlying error information
- Test backoff delay calculations with various attempt numbers

## Performance Considerations

- **Classification**: Error classification uses type assertions and string matching - O(1) for known types, O(n) for pattern matching
- **Memory**: ClassifiedError adds minimal overhead with structured context
- **String Matching**: Pattern matching is cached internally for performance
- **Retry Logic**: Exponential backoff prevents thundering herd problems

## Examples

### Example 1: Service Connection Handler

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/c360/semstreams/errors"
)

type ServiceClient struct {
    endpoint string
    retryConfig errors.RetryConfig
}

func NewServiceClient(endpoint string) *ServiceClient {
    return &ServiceClient{
        endpoint: endpoint,
        retryConfig: errors.RetryConfig{
            MaxRetries:    3,
            InitialDelay:  100 * time.Millisecond,
            MaxDelay:      5 * time.Second,
            BackoffFactor: 2.0,
        },
    }
}

func (c *ServiceClient) Connect(ctx context.Context) error {
    return c.retryOperation(func() error {
        if err := c.attemptConnection(); err != nil {
            return errors.Wrap(err, "ServiceClient", "Connect", "connection attempt")
        }
        return nil
    })
}

func (c *ServiceClient) attemptConnection() error {
    // Simulate connection logic
    if !serviceIsAvailable() {
        return errors.ErrConnectionTimeout
    }
    return nil
}

func (c *ServiceClient) retryOperation(operation func() error) error {
    var lastErr error

    for attempt := 0; attempt < c.retryConfig.MaxRetries; attempt++ {
        if err := operation(); err != nil {
            lastErr = err

            if !c.retryConfig.ShouldRetry(err, attempt) {
                return err
            }

            delay := c.retryConfig.BackoffDelay(attempt)
            log.Printf("Operation failed (attempt %d/%d), retrying in %v: %v",
                attempt+1, c.retryConfig.MaxRetries, delay, err)

            select {
            case <-time.After(delay):
                continue
            case <-ctx.Done():
                return ctx.Err()
            }
        }
        return nil
    }

    return errors.WrapTransient(lastErr, "ServiceClient", "retryOperation",
        "maximum retries exceeded")
}

func serviceIsAvailable() bool {
    // Simulate service availability check
    return time.Now().UnixNano()%2 == 0
}
```

### Example 2: Data Processing Pipeline

```go
type DataProcessor struct {
    validator  DataValidator
    storage    DataStorage
    retryCount int
}

func (p *DataProcessor) ProcessBatch(ctx context.Context, batch []Data) error {
    for i, data := range batch {
        if err := p.processItem(ctx, data); err != nil {
            switch {
            case errors.IsInvalid(err):
                // Skip invalid data, continue processing
                log.Printf("Skipping invalid item %d: %v", i, err)
                continue
            case errors.IsFatal(err):
                // Fatal error, stop entire batch
                return errors.WrapFatal(err, "DataProcessor", "ProcessBatch",
                    "fatal error processing item")
            case errors.IsTransient(err):
                // Transient error, retry item
                if retryErr := p.retryItem(ctx, data); retryErr != nil {
                    return errors.WrapTransient(retryErr, "DataProcessor", "ProcessBatch",
                        "retry failed for item")
                }
            }
        }
    }
    return nil
}

func (p *DataProcessor) processItem(ctx context.Context, data Data) error {
    // Validation
    if err := p.validator.Validate(data); err != nil {
        return errors.WrapInvalid(err, "DataProcessor", "processItem", "validation")
    }

    // Storage
    if err := p.storage.Store(ctx, data); err != nil {
        // Classify storage errors
        if errors.Is(err, errors.ErrStorageFull) {
            return errors.WrapFatal(err, "DataProcessor", "processItem", "storage")
        }
        return errors.WrapTransient(err, "DataProcessor", "processItem", "storage")
    }

    return nil
}

func (p *DataProcessor) retryItem(ctx context.Context, data Data) error {
    config := errors.DefaultRetryConfig()

    for attempt := 0; attempt < config.MaxRetries; attempt++ {
        if err := p.processItem(ctx, data); err != nil {
            if !config.ShouldRetry(err, attempt) {
                return err
            }

            delay := config.BackoffDelay(attempt)
            select {
            case <-time.After(delay):
                continue
            case <-ctx.Done():
                return ctx.Err()
            }
        }
        return nil
    }

    return errors.ErrMaxRetriesExceeded
}

// Mock interfaces for example
type DataValidator interface {
    Validate(data Data) error
}

type DataStorage interface {
    Store(ctx context.Context, data Data) error
}

type Data struct {
    ID      string
    Payload []byte
}
```

## Related Packages

- [`pkg/retry`](../retry): Advanced retry mechanisms building on this error classification
- [`pkg/component`](../component): Component lifecycle using standard error patterns
- [`pkg/service`](../service): Service framework using classified errors
- [`pkg/util`](../util): Utility functions for error handling helpers

## License

MIT
