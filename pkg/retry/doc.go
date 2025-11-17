// Package retry provides simple exponential backoff retry logic for transient failures.
//
// # Overview
//
// This package offers a minimal retry mechanism with exponential backoff, designed to handle
// transient failures in network operations, resource initialization, and component startup.
//
// # core Functions
//
//   - Do: Execute function with retry and exponential backoff
//   - DoWithResult: Execute function with retry, returns both result and error
//
// # Configuration Presets
//
//   - DefaultConfig(): 3 attempts, 100ms-5s delay (normal operations)
//   - Quick(): 10 attempts, 50ms-1s delay (component startup)
//   - Persistent(): 30 attempts, 200ms-10s delay (critical resources)
//
// # Usage Examples
//
// Basic retry with defaults:
//
//	err := retry.Do(ctx, retry.DefaultConfig(), func() error {
//	    return client.Connect()
//	})
//
// Component startup with quick retries:
//
//	cfg := retry.Quick()
//	err := retry.Do(ctx, cfg, func() error {
//	    return component.Initialize()
//	})
//
// Retry with result:
//
//	bucket, err := retry.DoWithResult(ctx, retry.DefaultConfig(), func() (jetstream.KeyValue, error) {
//	    return js.KeyValue(ctx, bucketName)
//	})
//
// Custom configuration:
//
//	cfg := retry.Config{
//	    MaxAttempts:  5,
//	    InitialDelay: 200 * time.Millisecond,
//	    MaxDelay:     10 * time.Second,
//	    Multiplier:   2.0,
//	    AddJitter:    true,
//	}
//	err := retry.Do(ctx, cfg, operation)
//
// # Design Philosophy
//
// This package is intentionally minimal:
//
//   - No circuit breakers (use service mesh or separate package)
//   - No metrics collection (use instrumentation at call site)
//   - No complex error classification (caller decides what to retry)
//   - Just exponential backoff with jitter
//
// # Context Cancellation
//
// All retry operations respect context cancellation and will immediately stop retrying
// when the context is cancelled, either during operation execution or during backoff delay.
//
// # Thread Safety
//
// All functions are safe for concurrent use. The jitter mechanism uses a thread-safe
// random source to avoid contention.
package retry
