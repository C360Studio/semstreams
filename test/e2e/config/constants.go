// Package config provides configuration for StreamKit core component E2E tests
package config

import "time"

// DefaultEndpoints provides default StreamKit service endpoints
var DefaultEndpoints = struct {
	HTTP string
	UDP  string
}{
	HTTP: "http://localhost:8080",
	UDP:  "localhost:14550",
}

// ComponentPaths defines API paths for component endpoints
var ComponentPaths = struct {
	Base   string
	Health string
	List   string
}{
	Base:   "/components",
	Health: "/components/health",
	List:   "/components/list",
}

// ServicePaths defines API paths for service endpoints
var ServicePaths = struct {
	Health string
}{
	Health: "/health",
}

// DefaultTestConfig provides default test configuration values
var DefaultTestConfig = struct {
	// Test execution
	Timeout         time.Duration
	RetryInterval   time.Duration
	MaxRetries      int
	ValidationDelay time.Duration

	// core data test config
	MessageCount    int
	MessageInterval time.Duration
	MinProcessed    int
}{
	// Test execution
	Timeout:         10 * time.Second,
	RetryInterval:   1 * time.Second,
	MaxRetries:      30,
	ValidationDelay: 5 * time.Second,

	// core data testing
	MessageCount:    10,
	MessageInterval: 100 * time.Millisecond,
	MinProcessed:    5,
}
