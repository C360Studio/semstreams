package errors

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestErrorClass_String(t *testing.T) {
	tests := []struct {
		class    ErrorClass
		expected string
	}{
		{ErrorTransient, "transient"},
		{ErrorInvalid, "invalid"},
		{ErrorFatal, "fatal"},
		{ErrorClass(999), "unknown"},
	}

	for _, test := range tests {
		t.Run(test.expected, func(t *testing.T) {
			result := test.class.String()
			if result != test.expected {
				t.Errorf("expected %s, got %s", test.expected, result)
			}
		})
	}
}

func TestIsTransient(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"connection timeout", ErrConnectionTimeout, true},
		{"connection lost", ErrConnectionLost, true},
		{"storage unavailable", ErrStorageUnavailable, true},
		{"rate limited", ErrRateLimited, true},
		{"circuit open", ErrCircuitOpen, true},
		{"context deadline exceeded", context.DeadlineExceeded, true},
		{"context canceled", context.Canceled, true},
		{"invalid data", ErrInvalidData, false},
		{"fatal error", ErrResourceExhausted, false},
		{"timeout in message", fmt.Errorf("operation timeout occurred"), true},
		{"network error", fmt.Errorf("network connection failed"), true},
		{"classified transient", &ClassifiedError{Class: ErrorTransient, Err: fmt.Errorf("test")}, true},
		{"classified fatal", &ClassifiedError{Class: ErrorFatal, Err: fmt.Errorf("test")}, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := IsTransient(test.err)
			if result != test.expected {
				t.Errorf("expected %v, got %v for error: %v", test.expected, result, test.err)
			}
		})
	}
}

func TestIsFatal(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"invalid config", ErrInvalidConfig, true},
		{"missing config", ErrMissingConfig, true},
		{"data corrupted", ErrDataCorrupted, true},
		{"storage full", ErrStorageFull, true},
		{"resource exhausted", ErrResourceExhausted, true},
		{"quota exceeded", ErrQuotaExceeded, true},
		{"connection timeout", ErrConnectionTimeout, false},
		{"invalid data", ErrInvalidData, false},
		{"fatal in message", fmt.Errorf("fatal system error occurred"), true},
		{"panic in message", fmt.Errorf("panic: system failure"), true},
		{"classified fatal", &ClassifiedError{Class: ErrorFatal, Err: fmt.Errorf("test")}, true},
		{"classified transient", &ClassifiedError{Class: ErrorTransient, Err: fmt.Errorf("test")}, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := IsFatal(test.err)
			if result != test.expected {
				t.Errorf("expected %v, got %v for error: %v", test.expected, result, test.err)
			}
		})
	}
}

func TestIsInvalid(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"invalid data", ErrInvalidData, true},
		{"parsing failed", ErrParsingFailed, true},
		{"checksum failed", ErrChecksumFailed, true},
		{"connection timeout", ErrConnectionTimeout, false},
		{"fatal error", ErrResourceExhausted, false},
		{"classified invalid", &ClassifiedError{Class: ErrorInvalid, Err: fmt.Errorf("test")}, true},
		{"classified transient", &ClassifiedError{Class: ErrorTransient, Err: fmt.Errorf("test")}, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := IsInvalid(test.err)
			if result != test.expected {
				t.Errorf("expected %v, got %v for error: %v", test.expected, result, test.err)
			}
		})
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected ErrorClass
	}{
		{"nil error", nil, ErrorTransient},
		{"connection timeout", ErrConnectionTimeout, ErrorTransient},
		{"invalid config", ErrInvalidConfig, ErrorFatal},
		{"invalid data", ErrInvalidData, ErrorInvalid},
		{"unknown error", fmt.Errorf("unknown error"), ErrorTransient},
		{"classified error", &ClassifiedError{Class: ErrorFatal, Err: fmt.Errorf("test")}, ErrorFatal},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := Classify(test.err)
			if result != test.expected {
				t.Errorf("expected %v, got %v for error: %v", test.expected, result, test.err)
			}
		})
	}
}

func TestClassifiedError(t *testing.T) {
	baseErr := fmt.Errorf("base error")
	ce := newClassified(ErrorTransient, baseErr, "testComponent", "testOperation", "custom message")

	if ce.Class != ErrorTransient {
		t.Errorf("expected ErrorTransient, got %v", ce.Class)
	}

	if ce.Component != "testComponent" {
		t.Errorf("expected testComponent, got %s", ce.Component)
	}

	if ce.Operation != "testOperation" {
		t.Errorf("expected testOperation, got %s", ce.Operation)
	}

	if ce.Error() != "custom message" {
		t.Errorf("expected 'custom message', got %s", ce.Error())
	}

	if !errors.Is(ce, baseErr) {
		t.Error("classified error should unwrap to base error")
	}
}

func TestClassifiedError_NoMessage(t *testing.T) {
	baseErr := fmt.Errorf("base error")
	ce := newClassified(ErrorTransient, baseErr, "testComponent", "testOperation", "")

	if ce.Error() != "base error" {
		t.Errorf("expected 'base error', got %s", ce.Error())
	}
}

func TestWrap(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		component string
		method    string
		action    string
		expected  string
	}{
		{
			"nil error",
			nil,
			"component",
			"method",
			"action",
			"",
		},
		{
			"basic wrap",
			fmt.Errorf("original error"),
			"RoboticsProcessor",
			"processMessage",
			"decode MAVLink",
			"RoboticsProcessor.processMessage: decode MAVLink failed: original error",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := Wrap(test.err, test.component, test.method, test.action)
			if test.expected == "" {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
			} else {
				if result == nil || result.Error() != test.expected {
					t.Errorf("expected '%s', got '%v'", test.expected, result)
				}
			}
		})
	}
}

func TestWrapClassified(t *testing.T) {
	baseErr := fmt.Errorf("original error")

	tests := []struct {
		name     string
		wrapFunc func(error, string, string, string) error
		class    ErrorClass
	}{
		{"WrapTransient", WrapTransient, ErrorTransient},
		{"WrapFatal", WrapFatal, ErrorFatal},
		{"WrapInvalid", WrapInvalid, ErrorInvalid},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := test.wrapFunc(baseErr, "component", "method", "action")

			var ce *ClassifiedError
			if !errors.As(result, &ce) {
				t.Error("result should be a ClassifiedError")
				return
			}

			if ce.Class != test.class {
				t.Errorf("expected %v, got %v", test.class, ce.Class)
			}

			if ce.Component != "component" {
				t.Errorf("expected 'component', got %s", ce.Component)
			}

			if ce.Operation != "method" {
				t.Errorf("expected 'method', got %s", ce.Operation)
			}

			if !strings.Contains(ce.Error(), "component.method: action failed") {
				t.Errorf("error should contain standard format, got: %s", ce.Error())
			}
		})
	}
}

func TestRetryConfig_ShouldRetry(t *testing.T) {
	config := DefaultRetryConfig()

	tests := []struct {
		name     string
		err      error
		attempt  int
		expected bool
	}{
		{"nil error", nil, 0, false},
		{"max retries exceeded", ErrConnectionTimeout, 3, false},
		{"transient error within limit", ErrConnectionTimeout, 1, true},
		{"fatal error", ErrInvalidConfig, 1, false},
		{"invalid error", ErrInvalidData, 1, false},
		{"custom transient", fmt.Errorf("connection timeout"), 1, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := config.ShouldRetry(test.err, test.attempt)
			if result != test.expected {
				t.Errorf("expected %v, got %v for error: %v, attempt: %d",
					test.expected, result, test.err, test.attempt)
			}
		})
	}
}

func TestRetryConfig_ShouldRetry_WithSpecificErrors(t *testing.T) {
	config := RetryConfig{
		MaxRetries:      3,
		InitialDelay:    100 * time.Millisecond,
		BackoffFactor:   2.0,
		RetryableErrors: []error{ErrConnectionTimeout},
	}

	// Should retry connection timeout
	if !config.ShouldRetry(ErrConnectionTimeout, 1) {
		t.Error("should retry connection timeout")
	}

	// Should not retry other transient errors not in the list
	if config.ShouldRetry(ErrConnectionLost, 1) {
		t.Error("should not retry connection lost when not in retryable list")
	}
}

func TestRetryConfig_BackoffDelay(t *testing.T) {
	config := RetryConfig{
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      1 * time.Second,
		BackoffFactor: 2.0,
	}

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{0, 100 * time.Millisecond},
		{1, 200 * time.Millisecond},
		{2, 400 * time.Millisecond},
		{3, 800 * time.Millisecond},
		{4, 1 * time.Second}, // Capped at MaxDelay
		{5, 1 * time.Second}, // Still capped
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("attempt_%d", test.attempt), func(t *testing.T) {
			result := config.BackoffDelay(test.attempt)
			if result != test.expected {
				t.Errorf("expected %v, got %v", test.expected, result)
			}
		})
	}
}

func TestRetryConfig_ToRetryConfig(t *testing.T) {
	// Test conversion from errors.RetryConfig to retry.Config
	errorsConfig := RetryConfig{
		MaxRetries:      5,
		InitialDelay:    200 * time.Millisecond,
		MaxDelay:        10 * time.Second,
		BackoffFactor:   1.5,
		RetryableErrors: nil,
	}

	retryConfig := errorsConfig.ToRetryConfig()

	// Verify conversion
	if retryConfig.MaxAttempts != 6 { // MaxRetries + 1
		t.Errorf("expected MaxAttempts %d, got %d", 6, retryConfig.MaxAttempts)
	}
	if retryConfig.InitialDelay != 200*time.Millisecond {
		t.Errorf("expected InitialDelay %v, got %v", 200*time.Millisecond, retryConfig.InitialDelay)
	}
	if retryConfig.MaxDelay != 10*time.Second {
		t.Errorf("expected MaxDelay %v, got %v", 10*time.Second, retryConfig.MaxDelay)
	}
	if retryConfig.Multiplier != 1.5 {
		t.Errorf("expected Multiplier %f, got %f", 1.5, retryConfig.Multiplier)
	}
	if !retryConfig.AddJitter {
		t.Error("expected AddJitter to be true")
	}
}

func TestStandardErrors(t *testing.T) {
	// Test that standard errors are defined
	standardErrors := []error{
		ErrAlreadyStarted,
		ErrNotStarted,
		ErrAlreadyStopped,
		ErrShuttingDown,
		ErrNoConnection,
		ErrConnectionLost,
		ErrConnectionTimeout,
		ErrSubscriptionFailed,
		ErrInvalidData,
		ErrDataCorrupted,
		ErrChecksumFailed,
		ErrParsingFailed,
		ErrStorageFull,
		ErrStorageUnavailable,
		ErrBucketNotFound,
		ErrKeyNotFound,
		ErrInvalidConfig,
		ErrMissingConfig,
		ErrConfigNotFound,
		ErrResourceExhausted,
		ErrRateLimited,
		ErrQuotaExceeded,
		ErrCircuitOpen,
		ErrMaxRetriesExceeded,
		ErrRetryTimeout,
	}

	for i, err := range standardErrors {
		if err == nil {
			t.Errorf("standard error at index %d is nil", i)
		}
		if err.Error() == "" {
			t.Errorf("standard error at index %d has empty message", i)
		}
	}
}

// Benchmark error classification performance
func BenchmarkIsTransient(b *testing.B) {
	err := ErrConnectionTimeout
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsTransient(err)
	}
}

func BenchmarkClassify(b *testing.B) {
	err := ErrConnectionTimeout
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Classify(err)
	}
}

func BenchmarkWrap(b *testing.B) {
	err := fmt.Errorf("base error")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Wrap(err, "component", "method", "action")
	}
}
